"""Persistent configuration stored in ~/.config/examvan/config.json.

Answers saved to disk are obfuscated (XOR) to prevent casual tampering.
"""

from __future__ import annotations

import base64
import json
import logging
import os
import threading
from pathlib import Path
from typing import Any, Dict, Optional

_log = logging.getLogger(__name__)


_CONFIG_DIR = Path.home() / ".config" / "examvan"
_CONFIG_FILE = _CONFIG_DIR / "config.json"

_defaults: Dict[str, Any] = {
    "server_url": "",
    "exam_token": "",
    "remember_url": True,
    "identity_data": {},
    # Token ujian yang pernah berlaku di mesin ini; hanya untuk membaca
    # berkas jawaban versi lama. Lihat `set()`.
    "exam_token_history": [],
}

_cache: Optional[Dict[str, Any]] = None

# Penulis config bisa datang dari beberapa thread sekaligus: thread GUI
# (dialog, autosave), worker `_connect_thread`, dan worker submit/recovery.
#
# Audit 2 Okt 2026 (HIGH H4): tanpa lock, dua penulis bisa membaca cache
# yang sama lalu menulis file yang sama bersamaan — JSON bercampur antar
# dua dump, dan `exam_token`/`identity_data` bisa hilang dari hasil
# akhirnya. Karena `_save()` selalu menulis SELURUH cache (bukan hanya satu
# kunci), satu lock global di sini cukup: penulisan menjadi serial dan
# seluruh isi konsisten.
_save_lock = threading.Lock()

# Lock untuk berkas jawaban (answers_<id>.dat + sidecar ownernya).
_answers_lock = threading.Lock()


def _restrict_to_owner(path: Path) -> None:
    """Tetapkan mode 0600: hanya pemilik yang boleh membaca.

    Best-effort. Di Unix `chmod` benar-benar membatasi; di Windows
    `chmod` hanya menyentuh bit read-only, jadi ACL Windows diatur lewat
    `_windows_restrict_to_owner` (lihat di bawah). Kegagalan tidak boleh
    mematikan app — yang penting dicatat di log.
    """
    try:
        path.chmod(0o600)
    except OSError as exc:
        _log.warning("could not restrict permissions on %s: %s", path, exc)
    if os.name == "nt":
        # Di Windows chmod tidak cukup: file masih group/world-readable
        # sesuai ACL folder induk. Percobaan ACL best-effort; kegagalan
        # sudah dilog di dalamnya.
        _windows_restrict_to_owner(path)


def _windows_restrict_to_owner(path: Path) -> bool:
    """Windows: batasi file ke pemilik + SYSTEM + Administrators.

    Audit 2 Okt 2026: `chmod` di Windows tidak mengubah ACL, jadi
    config.json (exam_token kredensial kelas + identitas siswa) tetap
    terbaca oleh akun lain di PC lab yang sama. Penggantinya bukan
    subprocess `icacls` — `_save()` dipanggil berkali-kali (autosave,
    setiap `set()`), spawn proses di situ terlalu mahal. Yang dipakai
    ctypes langsung: bangun DACL baru berisi grant penuh untuk user saat
    ini + SYSTEM + Administrators, lalu pasang dengan
    PROTECTED_DACL (menggantikan ACL warisan, bukan menambah).

    Best-effort: setiap langkah gagal → False + log warning, app tetap
    jalan (perilaku lama: ACL folder induk berlaku).
    """
    try:
        import ctypes
        from ctypes import wintypes

        advapi32 = ctypes.windll.advapi32
        SE_FILE_OBJECT = 1
        DACL_SECURITY_INFORMATION = 0x4
        PROTECTED_DACL_SECURITY_INFORMATION = 0x80000
        GRANT_ACCESS = 1
        TRUSTEE_IS_NAME = 1
        TRUSTEE_IS_UNKNOWN = 0
        GENERIC_ALL = 0x10000000

        class _Trustee(ctypes.Structure):
            _fields_ = [
                ("pMultipleTrustee", wintypes.LPVOID),
                ("MultipleTrusteeOperation", ctypes.c_int),
                ("TrusteeForm", ctypes.c_int),
                ("TrusteeType", ctypes.c_int),
                ("ptstrName", wintypes.LPWSTR),
            ]

        class _ExplicitAccess(ctypes.Structure):
            _fields_ = [
                ("grfAccessPermissions", wintypes.DWORD),
                ("grfAccessMode", ctypes.c_int),
                ("grfInheritance", wintypes.DWORD),
                ("Trustee", _Trustee),
            ]

        user = str(os.environ.get("USERNAME", "") or "").strip()
        if not user:
            return False
        # SYSTEM + Administrators ikut diberi akses agar housekeeping
        # (backup/antivirus kelola korporat) tidak rusak, tapi AKUN SISWA
        # LAIN tidak dapat apa-apa.
        names = [user, "SYSTEM", "Administrators"]
        entries = (_ExplicitAccess * len(names))()
        for i, name in enumerate(names):
            entries[i].grfAccessPermissions = GENERIC_ALL
            entries[i].grfAccessMode = GRANT_ACCESS
            entries[i].grfInheritance = 0
            entries[i].Trustee.pMultipleTrustee = None
            entries[i].Trustee.MultipleTrusteeOperation = 0
            entries[i].Trustee.TrusteeForm = TRUSTEE_IS_NAME
            entries[i].Trustee.TrusteeType = TRUSTEE_IS_UNKNOWN
            entries[i].Trustee.ptstrName = name

        new_dacl = wintypes.LPVOID()
        advapi32.SetEntriesInAclW.restype = wintypes.DWORD
        advapi32.SetEntriesInAclW.argtypes = [
            ctypes.c_ulong,
            ctypes.POINTER(_ExplicitAccess),
            wintypes.LPVOID,
            ctypes.POINTER(wintypes.LPVOID),
        ]
        advapi32.SetNamedSecurityInfoW.restype = wintypes.DWORD
        advapi32.SetNamedSecurityInfoW.argtypes = [
            wintypes.LPWSTR,
            ctypes.c_int,
            wintypes.DWORD,
            wintypes.LPVOID,
            wintypes.LPVOID,
            wintypes.LPVOID,
            wintypes.LPVOID,
        ]
        ret = advapi32.SetEntriesInAclW(
            len(names),
            ctypes.byref(entries),
            None,
            ctypes.byref(new_dacl),
        )
        if ret != 0 or not new_dacl:
            _log.warning(
                "SetEntriesInAclW failed (%s) on %s", ret, path
            )
            return False
        ret = advapi32.SetNamedSecurityInfoW(
            str(path),
            SE_FILE_OBJECT,
            DACL_SECURITY_INFORMATION | PROTECTED_DACL_SECURITY_INFORMATION,
            None,
            None,
            new_dacl,
            None,
        )
        if ret != 0:
            _log.warning(
                "SetNamedSecurityInfoW failed (%s) on %s", ret, path
            )
            return False
        return True
    except Exception as exc:
        _log.warning("could not set Windows ACL on %s: %s", path, exc)
        return False

# Simple XOR obfuscation key for answer files — prevents casual reading.
# Not cryptographic security (answers stay on disk only during exam).
_OBFUSCATE_KEY = b"EXAMVAN_OBF_2024!!"


def _xor_obfuscate(data: bytes) -> bytes:
    """XOR obfuscation. Same function for encrypt and decrypt.

    Kunci TETAP dan tidak lagi dicampur dari `exam_token`.

    Sebelumnya kunci enkripsi jawaban adalah nilai yang bisa diedit
    pengguna. Pada ujian dynamic-token, `MaybeResetActiveToken` memutar
    token; siswa yang kembali dengan token barumembuat `_connect_thread`
    menimpanya sebelum pemeriksaan recovery, dan `load_answers()` lalu
    mengembalikan None: tidak ada prompt "Kirim Lagi", tidak ada yang
    dipulihkan, dan autosave berikutnya menimpa berkas itu. Pekerjaan
    siswa sebelumnya hilang tanpa pesan apa pun.

    Kredensial juga tidak boleh menjadi kunci berkas kredensial yang
    bersebelahan: siapa pun yang bisa membaca `config.json` akan otomatis
    bisa mendekripsi `answers_<id>.dat`.
    """
    key = _OBFUSCATE_KEY
    return bytes(b ^ key[i % len(key)] for i, b in enumerate(data))


def _legacy_xor_obfuscate(data: bytes, token: str = "") -> bytes:
    """XOR dengan kunci yang dicampur dari token -- hanya untuk MIGRASI.

    Semua `answers_*.dat` yang ditulis versi lama memakai skema ini, jadi
    `load_answers` harus tetap bisa membacanya. Jangan dipakai untuk
    menulis: lihat `_xor_obfuscate`.

    `token` WAJIB diteruskan eksplisit. Versi sebelumnya mengambil
    `exam_token` yang sedang berlaku, dan itulah yang membuatnya tidak
    berguna justru untuk kasus yang paling membutuhkan: dynamic-token
    exam sudah Memutar token, `_connect_thread` menimpanya ke config
    sebelum `load_answers()` dipanggil, jadi kuncinya salah dan kedua
    decode gagal:

        token lama ABCD1234 -> load: {'1': 'A', ...}
        token baru  WXYZ5678 -> load: None

    Kunci lama tidak pernah disimpan bersama berkasnya, jadi tidak bisa
    diturunkan dari state sekarang. Yang bisa dilakukan adalah mencoba
    setiap token yang mungkin dipakai untuk ujian ini -- lihat
    `_legacy_token_candidates()`.
    """
    token = token or get("exam_token") or ""
    if token:
        mixed_key = bytes(b ^ ord(token[i % len(token)]) for i, b in enumerate(_OBFUSCATE_KEY))
    else:
        mixed_key = _OBFUSCATE_KEY
    return bytes(b ^ mixed_key[i % len(mixed_key)] for i, b in enumerate(data))


def _legacy_token_candidates() -> list:
    """Token yang mungkin dipakai untuk menulis berkas jawaban ini.

    Urutan dari yang paling mungkin. Falls back ke token saat ini supaya
    jalur biasa tidak berubah.
    """
    candidates = []
    current = str(get("exam_token", "") or "").strip()
    if current:
        candidates.append(current)
    # Token yang pernah berlaku di mesin ini. `config.set("exam_token", ...)`
    # menyimpan nilai lama ke sini setiap kali token berputar, jadi
    # jawaban yang ditulis dengan token sebelumnya masih bisa dibaca.
    history = get("exam_token_history") or []
    if isinstance(history, list):
        for value in reversed(history):
            token = str(_decode_secret(value) or "").strip()
            if token and token not in candidates:
                candidates.append(token)
    return candidates


def _encode_answers(answers: Dict[str, Any]) -> str:
    """Serialize + obfuscate + base64 encode."""
    raw = json.dumps(answers, separators=(",", ":"), ensure_ascii=False).encode("utf-8")
    obfuscated = _xor_obfuscate(raw)
    return base64.urlsafe_b64encode(obfuscated).decode("ascii")


def _decode_answers(data: str) -> Optional[Dict[str, Any]]:
    """Base64 decode + deobfuscate + parse.

    Mencoba skema saat ini dulu, lalu skema lama (kunci dicampur token)
    sebagai fallback. Tanpa fallback ini, setiap jawaban yang tersimpan
    sebelum update hilang begitu saja saat pertama kali app dibuka
    sesudahnya.
    """
    try:
        obfuscated = base64.urlsafe_b64decode(data.encode("ascii"))
    except Exception:
        return None

    # Skema saat ini dulu (kunci tetap).
    try:
        parsed = json.loads(_xor_obfuscate(obfuscated).decode("utf-8"))
        if isinstance(parsed, dict):
            return parsed
    except Exception:
        pass

    # Lalu skema lama. Setiap kandidat token dicoba satu per satu: kunci
    # lama tidak tersimpan, jadi satu-satunya cara adalah mencoba yang
    # masih mungkin dipakai.
    for token in _legacy_token_candidates():
        try:
            parsed = json.loads(
                _legacy_xor_obfuscate(obfuscated, token).decode("utf-8")
            )
        except Exception:
            continue
        if isinstance(parsed, dict) and _looks_like_answers(parsed):
            return parsed
    return None


def _looks_like_answers(parsed: dict) -> bool:
    """Hasil decode dengan kunci salah HANYA diterima kalau masuk akal.

    XOR dengan kunci yang keliru menghasilkan byte acak; menerima
    JSON-acak yang kebetulan_valid_json berarti "berhasil" dengan isi
    ngawur yang akan terkirim ke server dan dinilai salah semua -- lebih
    buruk daripada melaporkan "tidak terbaca", karena siswa tidak pernah
    diberi tahu jawabannya hilang.

    Jawaban yang sah berisi nomor soal (kunci string) dengan nilai
    string/list/dict.
    """
    if not parsed:
        return False
    for key, value in parsed.items():
        if not str(key).strip():
            return False
        if not isinstance(value, (str, list, dict)):
            return False
    return True


def _sanitize(config_data: Dict[str, Any]) -> Dict[str, Any]:
    """Paksa bentuk kunci yang dipahami app; biarkan kunci lain apa adanya.

    config.json ada di profil akun yang SAMA dengan akun siswa (lihat
    _restrict_to_owner) — isinya adalah input musuh. JSON valid tapi
    bukan object, atau kunci berbentuk salah, tidak boleh melempar:
    `_load()` dijalankan di mana-mana (get/set/get_all), jadi satu TypeError
    di sini mematikan seluruh app di SETIAP peluncuran — cache hanya
    terisi setelah baris merge sukses, sehingga crash-nya berulang sampai
    seseorang menghapus file manual di PC lab.
    """
    # identity_data dibaca ulang untuk MENGISI form identitas siswa
    # berikutnya; list/str di sini berarti AttributeError di dialog
    # ("x".get) atau form terisi data palsu. Salah bentuk = kosongkan.
    if not isinstance(config_data.get("identity_data"), dict):
        config_data["identity_data"] = {}
    # Kunci yang dipahami app dicoerce ke bentuknya: null/angka dari file
    # yang diedit manual tidak boleh mengalir sebagai None ke pemanggil
    # (mis. None.rstrip di WS connect, atau "None" literal sebagai token).
    config_data["server_url"] = str(config_data.get("server_url") or "")
    config_data["exam_token"] = str(config_data.get("exam_token") or "")
    _remember = config_data.get("remember_url", True)
    if _remember is True:
        config_data["remember_url"] = True
    elif isinstance(_remember, str):
        config_data["remember_url"] = _remember.strip().lower() in (
            "1", "true", "yes", "on",
        )
    else:
        config_data["remember_url"] = False
    _hist = config_data.get("exam_token_history")
    if isinstance(_hist, list):
        config_data["exam_token_history"] = [str(h) for h in _hist]
    else:
        config_data["exam_token_history"] = []
    return config_data


def _load() -> Dict[str, Any]:
    global _cache
    if _cache is not None:
        return _cache
    if _CONFIG_FILE.exists():
        try:
            with open(_CONFIG_FILE, "r", encoding="utf-8") as f:
                data = json.load(f)
        except (json.JSONDecodeError, OSError):
            data = None
        # JSON valid tapi bukan object ("[]", "123", "null") juga ditolak:
        # {**defaults, **[]} melempar TypeError (repro nyata, audit 30 Sep).
        if isinstance(data, dict):
            _cache = _sanitize({**_defaults, **data})
        else:
            _cache = dict(_defaults)
    else:
        _cache = dict(_defaults)
    return _cache


def _save() -> None:
    if _cache is None:
        return
    # Serialisasi penulis: dua `config.set()` dari thread berbeda tidak
    # boleh membaca cache yang sama lalu menimpa file yang sama — lihat
    # catatan `_save_lock` di atas. Lock diambil SETELAH cek cache, karena
    # cache tidak pernah ditulis oleh _save() (hanya dibaca).
    with _save_lock:
        _CONFIG_DIR.mkdir(parents=True, exist_ok=True)
        # Nama temp UNIK per proses: dua proses EXAMVAN di PC lab yang sama
        # (double-launch) dulu berebut `config.tmp` yang sama — file rusak
        # dan replace gagal. Dalam satu proses, lock di atas cukup.
        tmp = _CONFIG_FILE.with_suffix(f".tmp.{os.getpid()}")
        try:
            # Mode 0600 sejak create: isi (token + identitas) tidak pernah
            # world-readable walau sesaat.
            _fd = os.open(str(tmp), os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
            try:
                with os.fdopen(_fd, "w", encoding="utf-8") as f:
                    json.dump(_cache, f, indent=2, ensure_ascii=False)
                    f.flush()
                    os.fsync(f.fileno())
            except Exception:
                try:
                    os.close(_fd)
                except OSError:
                    pass
                raise
            # 0600 SEBELUM replace: file sementara harus ketat selama ia ada,
            # dan `replace` mempertahankan mode dari file sumber, jadi chmod
            # sesudahnya akan terlambat -- file kredensial sudah terbuka di
            # disk selama jendela di antaranya.
            #
            # Isinya adalah exam_token (kredensial seluruh kelas pada mode
            # static) dan identitas siswa. Mode bawaan umask membuatnya
            # world/group-readable, jadi akun lain di PC lab bisa membacanya,
            # mengambil PDF, dan mengirim jawaban sebagai siapa saja.
            _restrict_to_owner(tmp)
            tmp.replace(_CONFIG_FILE)
            # file bisa sudah ada dari versi lama dengan mode longgar;
            # `set()` menulis ulang berkali-kali jadi harus dijaga tiap kali.
            _restrict_to_owner(_CONFIG_FILE)
        except OSError as exc:
            # Sama seperti clear_answers: `set()` dipanggil dari thread GUI
            # (mis. `_connect_thread` yang menyimpan URL+token sebelum gate),
            # dan exception dari sana mematikan seluruh app lewat qFatal.
            # Config yang gagal ditulis berarti token/URL belum tersimpan --
            # sesuatu yang bisa diamati dan dicoba lagi, bukan crash.
            try:
                tmp.unlink()
            except OSError:
                pass
            _log.warning("could not write config: %s", _CONFIG_FILE, exc_info=True)


def _encode_secret(text: str) -> str:
    """Obfuscate string pendek (token/label) utk disimpan di config.json.

    Bukan kriptografi — siapa pun yang bisa membaca kode ini bisa
    men-decode-nya. Tujuannya sama seperti obfuscation jawaban: menaikkan
    palang dari "buka file, baca token ke-9 di baris mana pun" menjadi
    "harus tahu skemanya". Nilai di-decode dengan fallback ke bentuk
    aslinya, jadi config yang ditulis versi lama (plaintext) tetap terbaca.
    """
    raw = str(text or "").encode("utf-8")
    return base64.urlsafe_b64encode(_xor_obfuscate(raw)).decode("ascii")


def _decode_secret(value: Any) -> str:
    """Kebalikan `_encode_secret`, dengan fallback ke nilai mentah.

    Nilai yang bukan hasil encode (ditulis versi lama sebagai plaintext,
    atau diedit siswa) dikembalikan apa adanya — pemanggil hanya butuh
    string yang sama dengan yang disimpan dulu.
    """
    if not isinstance(value, str) or not value:
        return ""
    try:
        raw = base64.urlsafe_b64decode(value.encode("ascii"))
        decoded = _xor_obfuscate(raw).decode("utf-8")
        if decoded:
            return decoded
    except Exception:
        pass
    return value


def get(key: str, default: Any = None) -> Any:
    return _load().get(key, default)


def set(key: str, value: Any) -> None:
    # Riwayat token ujian.
    #
    # Berkas jawaban versi lama di-XOR dengan kunci yang dicampur dari
    # `exam_token` SAAT DITULIS, dan kunci itu tidak pernah disimpan
    # bersama berkasnya. Satu-satunya cara membacanya kembali adalah
    # mencoba token yang mungkin dipakai. Pada dynamic-token exam token
    # berputar (`MaybeResetActiveToken`), dan siswa yang kembali dengan
    # token baru menimpanya sebelum `load_answers()` dipanggil -- sehingga
    # jawaban yang tersisa tidak terbaca, tidak ada prompt "Kirim Lagi",
    # dan autosave berikutnya menimpanya.
    #
    # Menyimpan nilai lama membuat kunci itu masih bisa dicoba.
    if key == "exam_token":
        store = _load()
        previous = str(store.get("exam_token", "") or "").strip()
        current = str(value or "").strip()
        if previous and current and previous != current:
            history = store.get("exam_token_history") or []
            if not isinstance(history, list):
                history = []
            # Bukan arsip: hanya kandidat yang masih mungkin dipakai
            # (maksimal 3).
            history = [h for h in history if isinstance(h, str) and h]
            # Bandingkan dalam bentuk tersimpan (ter-obfuscated) supaya
            # rotasi bolak-balik token yang sama tidak menduplikasi entri.
            encoded_previous = _encode_secret(previous)
            if encoded_previous not in history:
                # Audit 2 Okt 2026: riwayat dulu berisi 8 token plaintext —
                # kredensial kelas yang lama tersusun rapi di config.json
                # bahkan setelah token berputar. Simpan ter-obfuscated
                # (lihat _encode_secret); pembacaannya lewat
                # _legacy_token_candidates yang menerima kedua bentuk.
                history.append(encoded_previous)
            store["exam_token_history"] = history[-3:]
    _load()[key] = value
    _save()


def get_all() -> Dict[str, Any]:
    return dict(_load())


def save_answers(exam_id: int, answers: Dict[str, Any]) -> None:
    """Save answers to disk for crash recovery (obfuscated)."""
    path = _CONFIG_DIR / f"answers_{exam_id}.dat"
    _CONFIG_DIR.mkdir(parents=True, exist_ok=True)
    # Nama temp BERBEDA dari save_answers_owner: with_suffix(".tmp") membuat
    # keduanya "answers_<id>.tmp" dan saling menimpa (jawaban hilang atau
    # sidecar korup). Suffix ditempel di belakang nama penuh.
    tmp = path.with_name(path.name + ".tmp")
    try:
        encoded = _encode_answers(answers)
        _fd = os.open(str(tmp), os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
        try:
            with os.fdopen(_fd, "w", encoding="ascii") as f:
                f.write(encoded)
                f.flush()
                os.fsync(f.fileno())
        except Exception:
            try:
                os.close(_fd)
            except OSError:
                pass
            raise
        _restrict_to_owner(tmp)
        with _answers_lock:
            tmp.replace(path)
    except Exception:
        # If obfuscation fails, don't write anything readable
        try:
            if tmp.exists():
                tmp.unlink()
        except OSError as exc:
            _log.warning("could not remove temp answers file %s: %s", tmp, exc)


def save_answers_owner(
    exam_id: int, student_key: str, label: str = ""
) -> None:
    """Catat PEMILIK jawaban yang tersimpan untuk ujian ini.

    Audit 2 Okt 2026 (HIGH H14): berkas jawaban dulu di-scope per UJIAN
    saja (`answers_<id>.dat`), tanpa jejak siapa yang menulisnya. Satu PC
    lab dipakai bergantian: siswa A mati mendadak, siswa B masuk, layar
    recovery menawarkan jawaban A untuk dikirim ulang — dan `_recovery_submit_thread`
    mengirimnya dengan identitas B yang baru saja diketik. Jawaban A
    tercatat atas nama B, nilai A hilang, dan tidak ada yang sadar.

    Sidecar ini menyimpan kunci siswa (hash identitas+token, bukan data
    pribadi — lihat `utils.build_student_key`) beserta label baca-manusia.
    `ServerConfigDialog._offer_pending_recovery` membandingkannya dengan
    identitas yang BARU SAJA diketik: cocok = recovery sah; tidak cocok =
    jawaban itu milik orang lain dan TIDAK boleh dikirim atas nama
    pengetik sekarang.
    """
    path = _CONFIG_DIR / f"answers_{exam_id}.owner"
    _CONFIG_DIR.mkdir(parents=True, exist_ok=True)
    # Lihat save_answers: suffix ditempel di belakang nama penuh supaya
    # tidak tabrakan dengan temp jawaban.
    tmp = path.with_name(path.name + ".tmp")
    try:
        with open(tmp, "w", encoding="utf-8") as f:
            json.dump(
                {
                    "student_key": str(student_key or ""),
                    "label": str(label or ""),
                },
                f,
                ensure_ascii=False,
            )
        _restrict_to_owner(tmp)
        with _answers_lock:
            tmp.replace(path)
    except OSError:
        _log.warning(
            "could not write answers owner marker for exam %s",
            exam_id,
            exc_info=True,
        )


def load_answers_owner(exam_id: int) -> Optional[Dict[str, str]]:
    """Pemilik jawaban tersimpan ({student_key, label}) atau None.

    None berarti berkas owner tidak ada — entri yang ditulis versi lama
    tanpa sidecar. Pemanggil memutuskan sendiri kebijakan fail-open/closed
    (lihat `ServerConfigDialog._offer_pending_recovery`).
    """
    path = _CONFIG_DIR / f"answers_{exam_id}.owner"
    try:
        with open(path, "r", encoding="utf-8") as f:
            data = json.load(f)
    except (OSError, ValueError):
        return None
    if not isinstance(data, dict):
        return None
    return {
        "student_key": str(data.get("student_key", "") or ""),
        "label": str(data.get("label", "") or ""),
    }


def load_answers(exam_id: int) -> Optional[Dict[str, Any]]:
    """Load saved answers for crash recovery."""
    path = _CONFIG_DIR / f"answers_{exam_id}.dat"
    if not path.exists():
        # Try legacy .json path (migration from unencrypted format)
        legacy = path.with_suffix(".json")
        if legacy.exists():
            try:
                with open(legacy, "r", encoding="utf-8") as f:
                    data = json.load(f)
            except (json.JSONDecodeError, OSError):
                return None
            # Format .dat sudah disaring _looks_like_answers(); jalur
            # migrasinya yang terlewat: array/angka dari file legacy
            # dulu lolos dan mengalir ke payload submit.
            if not isinstance(data, dict) or not _looks_like_answers(data):
                return None
            # Migrate to new format
            save_answers(exam_id, data)
            try:
                legacy.unlink()
            except OSError as exc:
                # Migrasi sudah selesai (.dat tertulis); file legacy yatim
                # bukan alasan menggagalkan recovery.
                _log.warning("could not remove legacy answers file %s: %s",
                             legacy, exc)
            return data
        return None
    try:
        with open(path, "r", encoding="ascii") as f:
            return _decode_answers(f.read())
    except Exception:
        return None


def clear_answers(exam_id: int) -> None:
    r"""Remove saved answers after successful submit.

    NEVER melempar. Panggilan ini berjalan di `_cleanup_after_submit`, yaitu
    statement pertama setelah submit sukses, dan pemanggilnya adalah slot
    Qt -- exception apa pun di sini jadi `qFatal()` lalu SIGABRT.

    `PermissionError` bukan hipotesis: jawaban ditulis ulang tiap ~500ms,
    Defender sering memindai file tepat setelah ditulis dan memegang handle
    beberapa ratus milidetik, dan `unlink()` di Windows tidak bisa
    menghapus file yang sedang dibuka. OneDrive/Dropbox di lab sekolah juga
    routinely mengunci %USERPROFILE%\.config.

    Kalau file tidak terhapus, akibatnya file yatim -- tidak fatal, dan
    bisa dibersihkan nanti. Yang fatal adalah aplikasi mati tepat setelah
    jawaban sudah sampai server: siswa tidak melihat konfirmasi, dan tetap
    tampil ONLINE di dashboard pengawas.
    """
    for path in (_CONFIG_DIR / f"answers_{exam_id}.dat",
                 _CONFIG_DIR / f"answers_{exam_id}.json",
                 # Sidecar pemilik ikut dihapus: membiarkannya membuat
                 # re-entry berikutnya membaca owner untuk jawaban yang
                 # sudah tidak ada (lihat save_answers_owner).
                 _CONFIG_DIR / f"answers_{exam_id}.owner"):
        try:
            if path.exists():
                path.unlink()
        except OSError:
        # Sengaja ditelan, tapi harus meninggalkan jejak di log.
            _log.warning("could not remove saved answers: %s", path,
                         exc_info=True)


def _submitted_raw(exam_id: int) -> dict:
    """Dict marker tersimpan apa adanya (nilai masih ter-encode).

    Dipakai mark_submitted untuk merge: membaca lewat _submitted_map
    (yang men-DECODE label) lalu menulis kembali hanya entri baru akan
    menyimpan label lama sebagai PLAINTEXT. Lihat mark_submitted.
    """
    value = get(f"submitted_{exam_id}", {}) or {}
    if isinstance(value, bool):
        return {} if not value else {_submitted_key(""): _encode_secret("identitas tidak diketahui")}
    if not isinstance(value, dict):
        return {}
    return dict(value)


def resolve_submit_answers(
    memory_answers: Dict[str, Any],
    exam_id: int,
    attempt_key: Optional[str] = None,
) -> Dict[str, Any]:
    """Jawaban efektif untuk submit: utamakan memori, fallback ke disk.

    Mirror fix Android F1: deadline bisa menembak SEBELUM jawaban dipulihkan
    dari disk (re-entry setelah proses mati; timer fire saat konstruktor,
    restore dijadwalkan 500ms kemudian). Tanpa fallback, submit kosong dalam
    window grace server (end_time + 60 dtk) MENIMPA jawaban asli dan
    clear_answers menghapusnya. Memori tidak kosong → dipakai apa adanya
    (siswa mungkin baru mengubah jawaban setelah auto-save terakhir).
    """
    if memory_answers:
        return memory_answers
    # attempt_key menjaga milik: bila sidecar owner ada dan milik orang
    # lain, jawaban disk BUKAN milik percobaan ini — jangan kirim atas
    # nama pengetik sekarang. Kembalikan memori (kosong) apa adanya.
    if attempt_key:
        try:
            owner = load_answers_owner(exam_id)
        except Exception:
            owner = None
        if owner is not None:
            owner_key = str(owner.get("student_key", "") or "")
            if owner_key and owner_key != str(attempt_key or ""):
                return memory_answers if memory_answers else {}
    saved = load_answers(exam_id)
    return saved if saved else {}


def save_start_time(exam_id: int, iso: str) -> None:
    """Persist start_time ujian untuk recovery re-entry (mirror Android
    AppPrefs.KEY_EXAM_START_TIME).

    Dipakai layar recovery (ServerConfigDialog) saat mengirim ulang jawaban
    dari disk — server menghitung durasi dari start_time, jadi harus sama
    dengan nilai yang dipakai submit asli.
    """
    set(f"start_time_{exam_id}", iso)


def load_start_time(exam_id: int) -> str:
    """Return the persisted start_time for [exam_id] ("" bila belum ada)."""
    return str(get(f"start_time_{exam_id}", "") or "")


def _submitted_key(attempt_key: str) -> str:
    """Hash satu kunci percobaan, dipakai sebagai key di dict marker."""
    import hashlib

    return hashlib.sha256(
        (attempt_key or "").strip().lower().encode()
    ).hexdigest()[:16]


def _submitted_map(exam_id: int) -> dict:
    """Dict {hash kunci: label} untuk satu ujian. Label hanya untuk pesan."""
    value = get(f"submitted_{exam_id}", {}) or {}
    # Rantai ke versi sebelum label ada: nilai lama adalah boolean.
    if isinstance(value, bool):
        return {} if not value else {_submitted_key(""): "identitas tidak diketahui"}
    if not isinstance(value, dict):
        return {}
    # Label bukan string (hasil tampering) tetap dihitung sebagai marker —
    # marker sticky tidak boleh hilang karena isi file diedit — tapi
    # labelnya jatuh ke fallback, supaya sorted() di submitted_labels()
    # tidak pernah mencampur str dan int (TypeError) saat dialog
    # re-entry "Kirim Lagi" tampil.
    #
    # Label ter-obfuscated sejak audit 2 Okt 2026 (lihat mark_submitted);
    # _decode_secret menerima juga plaintext dari config versi lama.
    return {
        key: (
            _decode_secret(val) if isinstance(val, str)
            else "identitas tidak diketahui"
        )
        for key, val in value.items()
    }


def mark_submitted(
    exam_id: int, attempt_key: str = "", label: str = ""
) -> None:
    """Catat bahwa percobaan (ujian + siswa) sudah mengumpulkan jawaban.

    Disimpan sebagai dict {hash kunci: label}, bukan boolean, supaya pesan
    penolakan bisa menyebut identitas mana yang sudah tercatat. Selama ini
    user hanya melihat "sudah dikumpulkan" dan tidak bisa memastikan itu
    dirinya sendiri atau bug.

    Kuncinya di-hash supaya token tidak pernah tersimpan mentah di
    config.json.

    Audit 2 Okt 2026: labelnya juga tidak lagi plaintext. Alasan lama
    ("identity_data juga plaintext di file yang sama") tidak berlaku lagi:
    `identity_data` sekarang dibersihkan saat app dimulai dan setiap sesi
    selesai, sedangkan marker submit menetap SELAMANYA. Setelah kelas
    berlalu, satu-satunya sisa identitas siswa di mesin lab justru label
    label ini. Disimpan ter-obfuscated (_encode_secret), dibaca dengan
    fallback (lihat _submitted_map).
    """
    # Merge ke RAW (masih ter-encode): membaca via _submitted_map yang
    # men-decode lalu menulis kembali akan menyimpan label lama sebagai
    # plaintext. _submitted_map tetap untuk pembaca display.
    store = _submitted_raw(exam_id)
    store[_submitted_key(attempt_key)] = _encode_secret(
        label or "identitas tidak diketahui"
    )
    set(f"submitted_{exam_id}", store)


def submitted_labels(exam_id: int) -> list:
    """Label identitas yang sudah submit ujian ini di perangkat ini."""
    return sorted(_submitted_map(exam_id).values())


def is_submitted(exam_id: int, attempt_key: str = "") -> bool:
    """True bila percobaan ini (exam + siswa) sudah mengumpulkan jawaban."""
    return _submitted_key(attempt_key) in _submitted_map(exam_id)


def clear_identity() -> None:
    """Hapus identitas siswa yang tersimpan.

    WAJIB dipanggil saat jendela ujian ditutup, di samping
    `ServerConfigDialog.input_token.clear()`.

    Kenapa: `identity_data` dibaca lagi di
    `ServerConfigDialog._show_identity_dialog` (`:356`) lalu dipakai untuk
    MENGISI form `IdentityDialog`. Kalau tidak dihapus, siswa berikutnya
    mendapat form yang sudah terisi nama siswa sebelumnya, dan karena
    `last_input.returnPressed` terikat ke submit, dia bisa menekan Enter
    tanpa membaca apa pun — jawabannya lalu tercatat atas nama orang lain.
    Tidak ada dialog, tidak ada warning, tidak ada log.

    Dulu fungsi ini tidak ada sama sekali: yang dibersihkan hanya token,
    padahal yang paling sensitif justru identitas. Lihat
    review_windows_2026-09-30.md Bagian 1.
    """
    set("identity_data", {})

