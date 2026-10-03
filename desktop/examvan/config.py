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


def _tmp_sibling(path: Path) -> Path:
    """Nama file sementara milik SATU proses, di samping `path`.

    Dua proses EXAMVAN di PC lab yang sama akan berebut nama temp yang
    sama kalau nama itu tetap. Yang terjadi bukan "file sementara
    tertinggal", tapi jawaban TERTIMPA: proses A menulis ke tmp, proses
    B menulis ke tmp yang sama, lalu proses A `replace()`-kan isi
    milik B ke `answers_<id>.dat`. Untuk jawaban siswa itu berarti
    jawaban satu siswa hilang dan milik siswa lain, dan `os.replace`
    proses kedua melempar FileNotFoundError yang tertelan `except`.

    Satu PID membuat nama temp unik antar proses; satu proses membuat
    nama temp unik antar PANGGILAN (dua `save_answers` untuk ujian yang
    sama bisa saling tumpang tindih juga). `threading.get_ident()`
    menutup keduanya tanpa perlu lock tambahan.

    Nama file penuh ikut dipertahankan: `answers_<id>.dat` dan
    `answers_<id>.owner` tidak boleh berbagi nama temp.
    """
    return path.with_name(
        f"{path.name}.tmp.{os.getpid()}.{threading.get_ident()}"
    )

_CONFIG_FILE = _CONFIG_DIR / "config.json"

_defaults: Dict[str, Any] = {
    "server_url": "",
    "exam_token": "",
    "remember_url": True,
    "identity_data": {},
    # Ronde 6 (item 3): pasangan {identity_data, context} disimpan sebagai
    # SATU kunci supaya tidak bisa terpisah. Lihat set_identity_session.
    "identity_session": {},
    # Konteks (exam_id + token) yang mengikat `identity_data`; penjaga
    # anti-prefill-silangan antar siswa (H8). Dictionari di bawah sudah
    # deprecated — accessor menurunkannya dari `identity_session`.
    "identity_context": {},
    # Token ujian yang pernah berlaku di mesin ini; hanya untuk membaca
    # berkas jawaban versi lama. Lihat `set()`.
    "exam_token_history": [],
}

# Bentuk penulisan `exam_token` yang dipakai SEKARANG (audit 2 Okt 2026,
# HIGH H12). Token adalah kredensial SELURUH KELAS pada mode static, jadi
# tidak boleh ada di `config.json` sebagai teks biasa — padahal
# `exam_token_history` dan marker `submitted_*` di berkas yang sama sudah
# ter-obfuscate sejak ronde sebelumnya. Nilainya tetap `_encode_secret`
# (XOR + base64, kunci tetap di modul ini).
#
# Kenapa kunci terpisah, bukan `exam_token` berisi nilai ter-obfuscate:
# bentuk lama (plaintext) harus dibaca apa adanya tanpa tebakan.
# `_decode_secret` bersifat LENGAK — nilai yang "kebetulan" base64+XOR
# valid akan diterjemahkan jadi sampah, dan pada token 8 karakter
# alnum peluangnya sekitar 7% (diuji di test_r8_config_secret_at_rest).
# Kalau `exam_token` yang dipaksakan ter-decode, kelas yang mendapat
# kredensial rusak tidak bisa membuka ujian sama sekali. Dengan kunci
# terpisah, "nilai ini sudah ter-encode atau belum" diketahui dari
# NAMANYA, jadi tidak ada tebakan sama sekali.
_SECRET_TOKEN_KEY = "exam_token_obf"

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
# RLock, bukan Lock: `set_identity_session` memegang `_save_lock` selama
# mutate + satu `_save()`, jadi lock-nya masuk dua kali di thread yang sama
# (lihat fungsi itu). `threading.Lock` tidak reentrant dan akan DEADLOCK di
# sana. Scope pemakaiannya tetap satu blok kritis yang pendek.
_save_lock = threading.RLock()

# Lock untuk berkas jawaban (answers_<id>.dat + sidecar ownernya).
# Reentrant dengan sengaja: `clear_answers_if_unchanged` memegang lock ini
# selama read-decide-delete dan memanggil `clear_answers` di dalamnya.
# `Lock` biasa akan menjadi deadlock yang muncul sebagai "aplikasi hang
# setelah submit" -- sulit didiagnosis dan tidak ada test yang gagal jelas.
_answers_lock = threading.RLock()


# Konstanta `SECURITY_INFORMATION` Win32 (MS-DTYP), dipisah ke modul agar
# bisa diperiksa tanpa menjalankan Win32 sama sekali.
#
# Koreksi audit 2 Okt 2026: PROTECTED_DACL_SECURITY_INFORMATION adalah
# `0x80000000`. Nilai lama `0x80000` itu `WRITE_OWNER` di `ACCESS_MASK`
# (cf. `golang/sys/windows/security_windows.go`), yang artinya "ubah
# pemilik", BUKAN "ganti DACL dengan yang baru". Disalirkan ke
# `SetNamedSecurityInfoW`, ACL warisan ikut ter-MERGE — persis kebalikan
# dari yang dijanjikan docstring `_windows_restrict_to_owner`, dan tanpa
# itu file kredensial tetap world-readable di PC lab.
DACL_SECURITY_INFORMATION_ = 0x4
PROTECTED_DACL_SECURITY_INFORMATION = 0x80000000


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
            DACL_SECURITY_INFORMATION_
            | PROTECTED_DACL_SECURITY_INFORMATION,
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
    # Sama untuk `identity_context` dan `identity_session`: config.json
    # ada di profil akun yang sama dengan akun siswa (lihat
    # _restrict_to_owner), jadi isinya adalah input musuh. Salah bentuk di
    # sini tidak boleh melempar dari `_load()` (dipanggil di mana-mana).
    if not isinstance(config_data.get("identity_context"), dict):
        config_data["identity_context"] = {}
    if not isinstance(config_data.get("identity_session"), dict):
        config_data["identity_session"] = {}
    # Kunci yang dipahami app dicoerce ke bentuknya: null/angka dari file
    # yang diedit manual tidak boleh mengalir sebagai None ke pemanggil
    # (mis. None.rstrip di WS connect, atau "None" literal sebagai token).
    config_data["server_url"] = str(config_data.get("server_url") or "")
    # exam_token (H12): bentuk sekarang (ter-obfuscate, kunci terpisah)
    # menang; bentuk lama dibaca apa adanya. Cache SELALU menyimpan token
    # polos — semua pemanggil (`server_config`, `ws`, `api`) memakai
    # `get("exam_token")` dan tidak boleh ikut tobakan.
    _encoded_token = config_data.get(_SECRET_TOKEN_KEY)
    if isinstance(_encoded_token, str) and _encoded_token:
        config_data["exam_token"] = _decode_secret(_encoded_token)
    else:
        config_data["exam_token"] = str(config_data.get("exam_token") or "")
    # Bentuk ter-obfuscate hanya bentuk di DISK; jangan ikut di-cache
    # (kalau tidak, `get_all()` menumpahkan bentuk mentah ke pemanggil
    # dan `_save` akan menulis dua salinan).
    config_data.pop(_SECRET_TOKEN_KEY, None)
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


def _backup_corrupt_config(reason: Any) -> Optional[Path]:
    """Salin `config.json` yang tidak terbaca ke `config.json.bak`.

    Audit 2 Okt 2026 (MEDIUM): `_load()` me-reset file rusak ke default
    dengan TANPA log dan tanpa cadangan. Yang hilang permanen bukan cuma
    URL/token — `exam_token_history` ikut hilang, dan itu satu-satunya
    cara membaca `answers_*.dat` versi lama (lihat
    `_legacy_token_candidates`), begitu pula `start_time_*` dan marker
    `submitted_*`. Setelah itu autosave berikutnya menimpa kertas yang
    sebenarnya masih bisa dipulihkan, dan tidak ada yang tahu.

    Isi cadangan apa adanya (termasuk token plaintext kalau config-nya
    memang versi lama) — tujuannya pemulihan, bukan hal lain; file
    cadangan dibuat 0600 supaya tidak memperluas kebocoran.

    Tidak pernah melempar: dipanggil dari `_load()`, yang dipanggil dari
    mana-mana. Kegagalan menyalin tetap dilog.
    """
    bak = _CONFIG_FILE.with_name(_CONFIG_FILE.name + ".bak")
    try:
        _fd = os.open(str(bak), os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
        with os.fdopen(_fd, "wb") as f:
            f.write(_CONFIG_FILE.read_bytes())
    except OSError as exc:
        _log.warning(
            "could not back up unreadable %s to %s: %s",
            _CONFIG_FILE, bak, exc, exc_info=True,
        )
        bak = None
    return bak


def _reset_corrupt_config(reason: Any) -> None:
    """Log keras + simpan cadangan, lalu kembalikan default.

    Dipanggil hanya dari `_load()`, dan hanya saat file ADA tapi tidak
    bisa dipakai. Melempar di sini akan mematikan seluruh app di setiap
    peluncuran, jadi funcinya melog DAN memulihkan file yang bisa
    dipulihkan, tidak menebak isinya.
    """
    bak = _backup_corrupt_config(reason)
    _log.warning(
        "%s is unreadable (%s); starting from defaults%s",
        _CONFIG_FILE, reason,
        f"; bytes kept in {bak}" if bak else "",
        exc_info=reason if isinstance(reason, BaseException) else None,
    )


def _load() -> Dict[str, Any]:
    global _cache
    if _cache is not None:
        return _cache
    if _CONFIG_FILE.exists():
        # Sentinel supaya "gagal parse" (sudah dilog + dicadangkan) tidak
        # tertukar dengan "JSON-nya `null`" — bentuk terakhir juga tidak
        # bisa dipakai, dan tetap harus meninggalkan jejak.
        unreadable = object()
        data: Any = unreadable
        try:
            with open(_CONFIG_FILE, "r", encoding="utf-8") as f:
                data = json.load(f)
        except (json.JSONDecodeError, UnicodeDecodeError) as exc:
            # Terpotong (mati listrik di tengah tulis), byte bukan-UTF-8,
            # atau JSON rusak. Semua itu berarti isi yang hilang tidak
            # bisa dibaca — dan tidak akan pernah dicatat kalau diam saja.
            _reset_corrupt_config(exc)
        except OSError as exc:
            # Tidak bisa dibaca (izin/OneDrive mengunci). Defaults tetap,
            # tapi kejadian ini harus kelihatan: siswa akan mengetik token
            # ulang dan menganggap tidak pernah masuk. Tidak ada salinan:
            # file-nya sendiri utuh, yang tidak bisa dibuka.
            _log.warning("could not read %s: %s", _CONFIG_FILE, exc,
                         exc_info=True)
        # JSON valid tapi bukan object ("[]", "123", "null") juga ditolak:
        # {**defaults, **[]} melempar TypeError (repro nyata, audit 30 Sep).
        # Sekarang ikut dicadangkan + dilog: bentuk-bentuk ini menghapus
        # isi yang sebenarnya masih utuh di dalam filenya.
        if not isinstance(data, dict):
            if data is not unreadable:
                _reset_corrupt_config("JSON valid tapi bukan object")
            _cache = dict(_defaults)
        else:
            _cache = _sanitize({**_defaults, **data})
    else:
        _cache = dict(_defaults)
    return _cache


def _save_payload() -> Dict[str, Any]:
    """Bentuk yang benar-benar ditulis ke `config.json`.

    Sama dengan cache, kecuali `exam_token` diganti `_SECRET_TOKEN_KEY`
    berisi `_encode_secret` (H12). Cache sendiri tidak pernah diubah —
    semua pemanggil membaca token polos dari `get("exam_token")`.
    """
    payload = dict(_cache or {})
    token = str(payload.get("exam_token", "") or "")
    payload.pop("exam_token", None)
    payload.pop(_SECRET_TOKEN_KEY, None)
    if token:
        payload[_SECRET_TOKEN_KEY] = _encode_secret(token)
    return payload


def _fsync_dir(path: Path) -> None:
    """`fsync` sebuah direktori supaya rename di dalamnya jadi durable.

    POSIX saja: Windows tidak punya yang setara untuk direktori (dan
    `FlushFileBuffers` pada directory handle tidak didukung), jadi di
    sana ini no-op — bukan kegagalan, karena target durability
    dijamin oleh mekanisme lain dari NTFS.

    Best-effort: beberapa filesystem (network share tertentu) menolak
    `fsync` pada direktori; itu bukan alasan menggagalkan penulisan yang
    sudah berhasil, tapi harus meninggalkan jejak.
    """
    if os.name == "nt":
        return
    try:
        fd = os.open(str(path), os.O_RDONLY)
    except OSError as exc:
        _log.warning("could not open %s to fsync: %s", path, exc)
        return
    try:
        os.fsync(fd)
    except OSError as exc:
        _log.warning("could not fsync %s: %s", path, exc)
    finally:
        os.close(fd)


def _ensure_config_dir() -> None:
    """Buat direktori config kalau belum ada; biarkan OSError ke pemanggil.

    Sengaja dibiarkan melempar: pemanggilnya (`_save`, `save_answers`,
    `save_answers_owner`) memanggilnya DI DALAM daerah yang dijaga, jadi
    kegagalan mkdir ikut tertangkap dan dilog — bukan lolos ke slot Qt.
    """
    _CONFIG_DIR.mkdir(parents=True, exist_ok=True)


def _save() -> None:
    if _cache is None:
        return
    # Serialisasi penulis: dua `config.set()` dari thread berbeda tidak
    # boleh membaca cache yang sama lalu menimpa file yang sama — lihat
    # catatan `_save_lock` di atas. Lock diambil SETELAH cek cache, karena
    # cache tidak pernah ditulis oleh _save() (hanya dibaca).
    with _save_lock:
        # Nama temp UNIK per proses: dua proses EXAMVAN di PC lab yang sama
        # (double-launch) dulu berebut `config.tmp` yang sama — file rusak
        # dan replace gagal. Dalam satu proses, lock di atas cukup.
        tmp = _tmp_sibling(_CONFIG_FILE)
        try:
            # H7: `mkdir` WAJIB di dalam try. `exist_ok=True` hanya
            # melewati pembuatan kalau direktori sudah ada; kalau belum
            # (%USERPROFILE% roaming yang redirected, folder OneDrive yang
            # terkunci, image lab yang dikunci polisi) justru INI yang
            # melempar EACCES, dan dulu dilempar di luar try.
            _ensure_config_dir()
            # Mode 0600 sejak create: isi (token + identitas) tidak pernah
            # world-readable walau sesaat.
            _fd = os.open(str(tmp), os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
            try:
                with os.fdopen(_fd, "w", encoding="utf-8") as f:
                    json.dump(_save_payload(), f, indent=2, ensure_ascii=False)
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
            # fsync DIREKTORI setelah `replace`. `fsync(file)` di atas hanya
            # menjamin isi file; entri direktori hasil rename baru durable
            # kalau direktorinya ikut di-fsync. Tanpa ini, mati listrik
            # tepat setelah autosave bisa mengembalikan `config.json`
            # versi lama — dan karena `exam_token_history` ikut terputus,
            # `answers_*.dat` versi lama yang tadinya bisa dibaca menjadi
            # tidak terbaca. Persis ancaman yang dicatat di docstring modul.
            _fsync_dir(_CONFIG_DIR)
            # file bisa sudah ada dari versi lama dengan mode longgar;
            # `set()` menulis ulang berkali-kali jadi harus dijaga tiap kali.
            _restrict_to_owner(_CONFIG_FILE)
        except OSError as exc:
            # Config yang gagal ditulis berarti token/URL belum tersimpan --
            # sesuatu yang bisa diamati (log di bawah) dan dicoba lagi.
            #
            # Koreksi komentar (audit 2 Okt 2026): ini dulu ditulis "exception
            # dari sini mematikan seluruh app lewat qFatal". Itu SALAH pada
            # PyQt5 yang dipakai (5.15.10): exception di dalam slot Qt
            # hanya mencetak traceback (PyQt5 menghentikan propagasi
            # exception lewat virtual machine), app terus berjalan. Dan
            # pada build PyInstaller `--windowed` stderr dibuang, jadi
            # hasil nyatanya adalah KEHENINGAN, bukan abort. Jadi yang
            # harus tercatat di sini adalah kegagalannya sendiri: tanpa warning
            # + traceback, config yang gagal ditulis tidak terlihat sama
            # sekali.
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
    #
    # Audit 2 Okt 2026: read-modify-write ini sekarang DI BAWAH
    # `_save_lock`. Sebelumnya mutate + `_save()` terpisah, dan lock hanya
    # diambil DI DALAM `_save()` — jadi dua rotasi dari thread berbeda
    # bisa membaca riwayat yang sama lalu menimpanya, dan satu entri
    # hilang. Kehilangan entri itu tidak bisa dipulihkan: entri itulah
    # satu-satunya kandidat yang masih bisa dipakai untuk membaca
    # `answers_*.dat` versi lama, jadi jawaban siswa yang tersisa jadi
    # tidak terbaca selamanya.
    with _save_lock:
        store = _load()
        if key == "exam_token":
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
                    # Riwayat dulu berisi 8 token plaintext — kredensial
                    # kelas yang lama tersusun rapi di config.json bahkan
                    # setelah token berputar. Simpan ter-obfuscated (lihat
                    # _encode_secret); pembacaannya lewat
                    # _legacy_token_candidates yang menerima kedua bentuk.
                    history.append(encoded_previous)
                store["exam_token_history"] = history[-3:]
        store[key] = value
        _save()


def get_all() -> Dict[str, Any]:
    return dict(_load())


def save_answers(exam_id: int, answers: Dict[str, Any]) -> bool:
    """Save answers to disk for crash recovery (obfuscated).

    Mengembalikan True kalau jawabannya benar-benar ada di disk. Kegagalan
    TIDAK pernah melempar (pemanggilnya slot Qt + timer autosave), tapi juga
    tidak pernah diam: tanpa log, autosave berhenti diam-diam sementara
    UI tetap terlihat sehat dan siswa mengira jawabannya aman.
    """
    path = _CONFIG_DIR / f"answers_{exam_id}.dat"
    # Unik per proses DAN per panggilan: lihat `_tmp_sibling`. Nama tetap
    # membuat dua proses di PC lab yang sama saling menimpa isi jawaban,
    # dan `replace` salah satunya gagal.
    tmp = _tmp_sibling(path)
    try:
        # H7: mkdir di dalam try — `exist_ok=True` tidak menolong kalau
        # direktori config belum pernah dibuat (induk read-only).
        _ensure_config_dir()
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
        return True
    except Exception as exc:
        # Kegagalan apa pun (mkdir ditolak, `os.replace` ditolak Defender
        # yang sedang memegang handle, ENOSPC, fsync gagal) harus KELIHATAN.
        # Sebelumnya blok ini hanya melog kalau cleanup unlink ikut gagal,
        # jadi praktis semua kegagalan menulis hilang tanpa jejak: autosave
        # berhenti, `_answers_dirty` tetap dibersihkan, dan tidak ada siapa
        # pun yang tahu jawaban menit terakhir tidak ada di disk.
        _log.warning(
            "could not save answers for exam %s to %s: %s",
            exam_id, path, exc, exc_info=True,
        )
        # If obfuscation fails, don't write anything readable
        try:
            if tmp.exists():
                tmp.unlink()
        except OSError as cleanup_exc:
            _log.warning("could not remove temp answers file %s: %s",
                         tmp, cleanup_exc)
        return False


def _decode_secret_strict(value: Any) -> Optional[str]:
    """`_decode_secret` TANPA fallback ke nilai mentah. None kalau rusak.

    Dipakai kalau kita TAHU nilai di disk sudah `_encode_secret`, jadi
    "gagal decode" berarti isinya rusak/berubah bentuk — bukan "mungkin
    saja plaintext versi lama". Menebak di sini berarti mengarang
    kredensial atau kunci siswa.
    """
    if not isinstance(value, str):
        return None
    try:
        raw = base64.urlsafe_b64decode(value.encode("ascii"))
        return _xor_obfuscate(raw).decode("utf-8")
    except Exception:
        return None


# Bentuk sidecar `.owner` (H12). Field `v` menandai isi yang sudah
# ter-obfuscate: tanpa penanda ini, pembaca harus menebak apakah
# "0812" itu plaintext atau base64+XOR, dan tebakan yang salah mengubah
# kunci siswa jadi sampah — gerbang kepemilikan lalu menolak recovery
# yang sah (fail-CLOSED) atau, lebih buruk, membukanya untuk orang
# lain. Sidecar versi lama (tanpa `v`) dibaca apa adanya.
_OWNER_FORMAT = 2


def _owner_payload(student_key: str, label: str) -> Dict[str, Any]:
    """Sidecar yang AMAN ditulis ke disk (H12).

    `student_key` dan `label` disamarkan dengan `_encode_secret` yang
    sama seperti `exam_token_history` dan marker `submitted_*` — bukan
    karena sidecar ini perlu dirahasiakan dari pemiliknya, tapi karena
    `clear_answers` tidak selalu sempat berjalan: siswa yang prosesnya
    DIBUNUH meninggalkan sidecar ini, dan isinya adalah nomor ujian /
    nama (lihat `utils.build_student_key`, yang nilainya apa adanya,
    bukan hash seperti yang dulu dikira docstring fungsi ini).
    """
    return {
        "v": _OWNER_FORMAT,
        "student_key": _encode_secret(student_key),
        "label": _encode_secret(label),
    }


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

    Sidecar ini menyimpan kunci siswa (hasil `utils.build_student_key`) dan
    label baca-manusia, KEDUANYA ter-obfuscate di disk (lihat
    `_owner_payload`; koreksi docstring lama yang menyebutnya "hash
    identitas+token, bukan data pribadi" — `build_student_key` mengembalikan
    nomor ujian/nama apa adanya, `.lower()` saja).
    `ServerConfigDialog._offer_pending_recovery` membandingkannya dengan
    identitas yang BARU SAJA diketik: cocok = recovery sah; tidak cocok =
    jawaban itu milik orang lain dan TIDAK boleh dikirim atas nama
    pengetik sekarang.
    """
    path = _CONFIG_DIR / f"answers_{exam_id}.owner"
    # Lihat save_answers dan `_tmp_sibling`: nama temp tidak boleh sama
    # dengan temp jawaban maupun dengan milik proses lain.
    tmp = _tmp_sibling(path)
    try:
        # H7: mkdir di dalam try (lihat save_answers).
        _ensure_config_dir()
        # 0600 sejak create, sama seperti save_answers: sidecar ini berisi
        # kunci + label siswa. `open(tmp, "w")` dulu membuatnya dengan mode
        # umask (0644 pada umask 022), jadi selama jendela antara create dan
        # `chmod` berikutnya isinya sudah world-readable — persis celah yang
        # save_answers sengaja tutup.
        _fd = os.open(str(tmp), os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
        try:
            with os.fdopen(_fd, "w", encoding="utf-8") as f:
                json.dump(
                    _owner_payload(student_key, label),
                    f,
                    ensure_ascii=False,
                )
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
    except OSError:
        _log.warning(
            "could not write answers owner marker for exam %s",
            exam_id,
            exc_info=True,
        )


def load_answers_owner(exam_id: int) -> Optional[Dict[str, str]]:
    """Pemilik jawaban tersimpan ({student_key, label}) atau None.

    None berarti sidecar tidak ada (entri versi lama tanpa sidecar) atau
    isinya rusak — untuk yang kedua tidak ada tebakan yang aman, jadi
    dikembalikan None + warning, sama dengan policy fail-open pemanggil
    (lihat `ServerConfigDialog._offer_pending_recovery`).

    Nilai yang dikembalikan SELALU bentuk polos: konsumennya
    (`exam_viewer`, `server_config`, `resolve_submit_answers`) membandingkan
    `student_key` dengan hasil `utils.build_student_key` yang baru dihitung,
    jadi yang dikembalikan harus bisa dibandingkan, bukan hash.
    """
    path = _CONFIG_DIR / f"answers_{exam_id}.owner"
    try:
        with open(path, "r", encoding="utf-8") as f:
            data = json.load(f)
    except (OSError, ValueError):
        return None
    if not isinstance(data, dict):
        return None
    if data.get("v") != _OWNER_FORMAT:
        # Sidecar versi lama: plaintext, dibaca apa adanya.
        return {
            "student_key": str(data.get("student_key", "") or ""),
            "label": str(data.get("label", "") or ""),
        }
    student_key = _decode_secret_strict(data.get("student_key", ""))
    if student_key is None:
        _log.warning(
            "answers owner marker for exam %s is corrupt; "
            "owner treated as unknown",
            exam_id,
        )
        return None
    label = _decode_secret_strict(data.get("label", "")) or ""
    return {"student_key": student_key, "label": label}


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
    Qt.

    Koreksi komentar (audit 2 Okt 2026): dua alasan salah yang lalu dipakai
    di sini dihapus.
    (1) "exception apa pun jadi `qFatal()` lalu SIGABRT" — tidak benar pada
    PyQt5 5.15.10 yang dipakai: exception di dalam slot hanya mencetak
    traceback dan app lanjut. (2) Konsekuensinya, pada build PyInstaller
    `--windowed` stderr dibuang, jadi kegagalan di sini yang nyata adalah
    KEHENINGAN, bukan abort. Yang tetap benar: config/berkas yang gagal
    dibersihkan setelah jawaban sudah sampai server berarti siswa tidak
    melihat konfirmasi dan tetap tampil ONLINE di dashboard pengawas --
    makanya setiap kegagalan di bawah WAJIB meninggalkan jejak di log.

    `PermissionError` bukan hipotesis: jawaban ditulis ulang tiap ~500ms,
    Defender sering memindai file tepat setelah ditulis dan memegang handle
    beberapa ratus milidetik, dan `unlink()` di Windows tidak bisa
    menghapus file yang sedang dibuka. OneDrive/Dropbox di lab sekolah juga
    routinely mengunci %USERPROFILE%\.config.

    Sidecar `.owner` dihapus TERAKHIR dan HANYA kalau berkas jawabannya
    benar-benar tidak ada lagi. Alasannya dua arah, dan tidak simetris
    (audit M6):

      * jawaban YATIM tanpa sidecar = tidak ada yang bisa di-restore dan
        tidak ada recovery yang bisa dijalankan -- file sia-sia saja;
      * jawaban TANPA sidecar = kedua gerbang konsumen gagal-TERBUKA (`owner
        is None` artinya "milik siapa pun"), jadi jawaban siswa berikutnya
        bisa di-restore ke layar dan dikirim atas namanya -- persis
        kebocoran lintas siswa yang sidecar ini ada untuk mencegahnya.

    Dulu ketiganya dihapus berurutan tanpa syarat, jadi `PermissionError`
    pada `.dat` cukup untuk menghasilkan jawaban tanpa pemilik. Sidecar
    yatim hanya dibersihkan pada pemanggilan berikutnya yang berhasil.

    Kalau file tidak terhapus, akibatnya file yatim -- tidak fatal, dan
    bisa dibersihkan nanti. Yang fatal adalah aplikasi mati tepat setelah
    jawaban sudah sampai server: siswa tidak melihat konfirmasi, dan tetap
    tampil ONLINE di dashboard pengawas.
    """
    answers_paths = [
        _CONFIG_DIR / f"answers_{exam_id}.dat",
        # Warisan plaintext sebelum obfuscation; `load_answers` masih memakainya
        # kalau `.dat` tidak ada, jadi ikut jadi syarat "jawaban benar-benar
        # hilang".
        _CONFIG_DIR / f"answers_{exam_id}.json",
    ]
    for path in answers_paths:
        try:
            if path.exists():
                path.unlink()
        except OSError:
            # Sengaja ditelan, tapi harus meninggalkan jejak di log.
            _log.warning("could not remove saved answers: %s", path,
                         exc_info=True)

    # GATED: sidecar hanya boleh ikut hilang kalau tidak ada satu pun berkas
    # jawaban yang tersisa. `exists()` yang tersisa artinya unlink di atas
    # gagal (PermissionError) -- `.owner` lalu WAJIB bertahan.
    leftovers = [p.name for p in answers_paths if p.exists()]
    if leftovers:
        _log.warning(
            "sidecar pemilik jawaban exam %s DIPERTAHANKAN: %s",
            exam_id, ", ".join(leftovers),
        )
        return

    owner = _CONFIG_DIR / f"answers_{exam_id}.owner"
    try:
        if owner.exists():
            owner.unlink()
    except OSError:
        _log.warning("could not remove answers owner marker: %s", owner,
                     exc_info=True)


def clear_answers_if_unchanged(
    exam_id: int, expected: Optional[Dict[str, Any]]
) -> bool:
    """Hapus jawaban `exam_id` HANYA kalau isinya masih `expected`.

    Mengembalikan True kalau pemanggil boleh menganggap jawapannya sudah
    dibersihkan (file tidak ada, atau isinya persis `expected`).

    Kenapa ini bukan `if answers_match_disk(...)` lalu `clear_answers(...)`
    di luar (audit ronde 8, H9)
    -----------------------------------------------------
    Pasangan "baca → putuskan → hapus" itu raced: `clear_answers()` sendiri
    TIDAK memegang `_answers_lock`, jadi di antara pembacaan dan
    `unlink()` Autosave percobaan berikutnya bisa menimpa `answers_<id>.dat`
    beserta sidecar owner-nya. Terverifikasi dengan interleaving sungguhan:

        [B] menulis answers {'1':'X','2':'Y'} + owner=studentB
        [A] clear_answers() berjalan untuk exam 77
        => file jawaban: False,  sidecar: False   (jawaban B hilang)

    Jadi pasangan itu harus_atomik_ terhadap penulis jawaban, dan lock yang
    benar hidup di modul ini — `save_answers`/`save_answers_owner` sudah
    memakainya. Karena itu helper-nya di sini, bukan di lapisan UI: ketiga
    call site (auto-submit, cleanup manual, recovery) memakai aturan yang
    sama, dan `server_config` ikut memakainya tanpa perlu tahu detail
    lock-nya.

    Fail-closed di semua arah: lock tidak bisa diambil, `load_answers`
    melempar, atau isinya berbeda ⇒ TIDAK menghapus. Menghapus jawaban yang
    bukan milik percobaan yang sedang selesai jauh lebih merusak daripada
    membiarkannya (jawaban siswa berikutnya hilang tanpa jejak).

    `RLock`, bukan `Lock`: helper ini memanggil `clear_answers`, dan suatu
    saat `clear_answers` sendiri bisa butuh lock yang sama. `Lock` biasa
    akan menjadi deadlock, dan itu akan muncul sebagai "aplikasi hang
    setelah submit" — sulit didiagnosis dan tidak ada test yang gagal
    jelas. Reentrant lock membuat salah urus lock menjadi error cepat,
    bukan hang.
    """
    try:
        _answers_lock.acquire()
    except Exception:  # pragma: no cover - lock ini tidak pernah gagal
        _log.warning(
            "could not take answers lock; keeping answers for exam %s",
            exam_id, exc_info=True,
        )
        return False
    try:
        if expected is None:
            # Tidak tahu apa yang di disk: JANGAN hapus.
            return False
        try:
            current = load_answers(exam_id)
        except Exception:
            _log.warning(
                "could not read answers for exam %s before clearing; "
                "keeping them", exam_id, exc_info=True,
            )
            return False
        if current is None:
            # Sudah tidak ada (percobaan sebelumnya yang membersihkannya).
            # True: pemanggil boleh lanjut, tidak ada yang perlu dihapus.
            return True
        if current != expected:
            _log.warning(
                "answers for exam %s changed underneath this submit; "
                "NOT deleting them", exam_id,
            )
            return False
        clear_answers(exam_id)
        return True
    finally:
        _answers_lock.release()


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


def get_identity_session() -> Dict[str, Any]:
    """Pasangan `{identity_data, context}` yang tersimpan, atau kosong.

    Ronde 6 (item 3). Kunci `identity_session` adalah bentuk kanonik;
    bentuk lama (dua kunci terpisah di root) tetap dibaca supaya config
    yang sudah tertulis tidak kehilangan identitas saat update — tapi hanya
    kalau KEDUA bagiannya ada.

    Melemahkan bentuk setengah secara sengaja. Identitas tanpa konteks
    adalah kondisi yang tidak boleh terlihat oleh siapa pun: yang memakai
    prefill-silangan H8 justru butuh konteks, dan konteks yang hilang/
    salah akan membuat identitas siswa A mengisi form siswa B. Karena itu
    bentuk yang tidak lengkap — yang dipisahkan oleh crash di tengah dua
    `_save()` — dikembalikan sebagai "tidak ada" ({}).

    Cocok/tidaknya konteks dengan ujian yang sedang dibuka tetap diputuskan
    PEMAKAI (lihat `ServerConfigDialog._prefill_identity_if_same_exam`):
    accessor ini hanya menjamin kedua bagian utuh.
    """
    store = _load()
    session = store.get("identity_session")
    if isinstance(session, dict) and session:
        identity = session.get("identity_data")
        context = session.get("context")
    else:
        # Bentuk legacy: dua kunci terpisah. config.json yang ditulis
        # versi lama tidak punya `identity_session` sama sekali.
        identity = store.get("identity_data")
        context = store.get("identity_context")
    if not isinstance(identity, dict) or not identity:
        return {"identity_data": {}, "context": {}}
    if not isinstance(context, dict) or not context:
        return {"identity_data": {}, "context": {}}
    return {"identity_data": dict(identity), "context": dict(context)}


def get_identity_data() -> Dict[str, Any]:
    """Identitas tersimpan, atau {} bila konteksnya tidak utuh."""
    return get_identity_session()["identity_data"]


def get_identity_context() -> Dict[str, Any]:
    """Konteks (exam_id + token) dari pasangan tersimpan, atau {}."""
    return get_identity_session()["context"]


def set_identity_session(
    identity: Optional[Dict[str, Any]], context: Optional[Dict[str, Any]]
) -> None:
    """Simpan identitas + konteksnya dalam SATU `_save()`.

    Ronde 6 (item 3). `ui/server_config.py` tadinya melakukan dua
    `config.set()` terpisah, dan setiap `set()` menulis SELURUH cache ke
    disk. Crash di antara keduanya menyisakan identitas siswa A dengan
    konteks yang menunjuk ujian lain — persis kondisi yang tidak boleh terjadi,
    karena konteks itulah penjaga anti-prefill-silangan.

    Bentuk kanonik (`identity_session`) TIDAK bisa terpisah: satu kunci,
    satu dump, satu `replace`. Dua kunci lama tetap dicerminkan di
    penulisan yang sama supaya pembaca versi lama — termasuk
    `__main__.py`, yang tidak boleh disentuh di ronde ini — membaca data
    yang sama persis.

    M7: `_save_lock` kini dipegang selama mutate DAN save. `_load()`
    mengembalikan dict `_cache` yang HIDUP (bukan salinan), jadi tiga
    mutasi yang terjadi di luar lock menyisakan jeda di mana penulis lain
    (`config.set("exam_token", ...)` dari `_connect_thread`) bisa menjalankan
    `_save()`-nya sendiri dan mem-persist `identity_session` BARU berdampingan
    dengan cermin `identity_data`/`identity_context` LAMA. State setengah
    seperti itu persis yang tidak boleh terlihat: `ServerConfigDialog`
    membacanya untuk prefill, dan konteksnya adalah penjaga anti-prefill-
    silang (H8) — siswa berikutnya bisa mendapat form terisi identitas
    orang lain.

    Tidak pernah melempar: pemanggilnya adalah slot Qt di tengah alur
    join, dan config yang gagal ditulis berarti prefill dilewati (bisa
    diamati), bukan crash.
    """
    try:
        payload_identity = dict(identity or {})
        payload_context = dict(context or {})
        with _save_lock:
            store = _load()
            store["identity_session"] = {
                "identity_data": payload_identity,
                "context": payload_context,
            }
            # Cermin bentuk lama, satu dump di bawah dan di bawah lock yang
            # sama — tidak pernah boleh terlihat setengah tertulis.
            store["identity_data"] = payload_identity
            store["identity_context"] = payload_context
            # RLock (bukan Lock) supaya pemanggilan `_save()` di sini
            # reentrant aman: kontraknya tetap "satu penulisan penuh".
            _save()
    except Exception:
        _log.warning("could not persist identity session",
                     exc_info=True)


def clear_identity() -> None:
    """Hapus identitas siswa yang tersimpan BESERTA konteksnya.

    WAJIB dipanggil saat jendela ujian ditutup, di samping
    `ServerConfigDialog.input_token.clear()`, dan saat app start.

    Kenapa: `identity_data` dibaca lagi di
    `ServerConfigDialog._prefill_identity_if_same_exam` lalu dipakai untuk
    MENGISI form `IdentityDialog`. Kalau tidak dihapus, siswa berikutnya
    mendapat form yang sudah terisi nama siswa sebelumnya, dan karena
    `last_input.returnPressed` terikat ke submit, dia bisa menekan Enter
    tanpa membaca apa pun — jawabannya lalu tercatat atas nama orang lain.
    Tidak ada dialog, tidak ada warning, tidak ada log.

    Ronde 6 (item 3): dulu fungsi ini hanya menghapus `identity_data` dan
    TIDAK menyentuh `identity_context`. Konteks yang tertinggal praktis
    tidak berbahaya (prefill membutuhkannya berdua), tapi tetap data sisa
    yang tidak perlu ada — dan membiarkan `identity_session` utuh sementara
    `identity_data` dikosongkan justru menghasilkan state setengah jadi yang
    tidak boleh terbaca. Ketiganya sekarang dibersihkan dalam satu
    `_save()`.

    SENGAJA TIDAK menyentuh `answers_<id>.owner`: fungsi ini juga dipanggil
    saat jendela ujian ditutup, yaitu saat ujian SISWA itu masih berjalan.
    Sidecar yang masih menopangi jawaban yang ada justru satu-satunya
    penjaga answers milik-siapa (lihat `load_answers_owner`), jadi
    menghapusnya di sini membuka jawaban siswa A untuk recovery siswa B.
    Sapuan sidecar ada di `clear_stale_identity_on_startup` (H12).

    Tidak pernah melempar: pemanggilnya slot Qt. Exception dari slot Qt
    tidak mematikan app — PyQt5 mencetak traceback lalu melanjutkan
    (koreksi komentar: dulu ditulis "qFatal → SIGABRT"; pada build
    `--windowed` stderr justru dibuang, jadi hasilnya keheningan).
    """
    try:
        store = _load()
        store["identity_data"] = {}
        store["identity_context"] = {}
        store["identity_session"] = {}
        _save()
    except Exception:
        _log.warning("could not clear persisted identity",
                     exc_info=True)


def _sweep_orphan_owner_markers() -> int:
    """Hapus `answers_<id>.owner` yang jawabannya sudah tidak ada. Jumlahnya.

    H12: `clear_answers` menghapus owner HANYA kalau berkas jawabannya
    benar-benar hilang, dan proses yang DIBUNUH (listrik mati, task
    manager, crash) tidak menjalankan satu pun jalur keluar bersih. Hasilnya
    sidecar berisi identitas siswa menetap tanpa ada jawaban yang
    menopanginya — tidak berguna, dan tetap data pribadi.

    Yang TIDAK boleh disapu: owner yang jawabannya masih ada. Sidecar itu
    masih benar-benar dipakai (gerbang restore + gerbang recovery), dan
    menghapusnya membuat keduanya gagal-TERBUKA — jawaban siswa yang
    dibunuh di tengah ujian lalu bisa dikirim atas nama siswa berikutnya.
    Jadi syaratnya persis kebalikan dari `clear_answers`: hapus hanya yang
    sudah yatim.

    Hanya dipanggil saat start (tidak ada ujian yang hidup pada saat itu),
    jadi "sedang dipakai" tidak mungkin terjadi di tengah operasi.

    Tidak pernah melempar; kegagalan per berkas dilog.
    """
    removed = 0
    try:
        owners = sorted(_CONFIG_DIR.glob("answers_*.owner"))
    except OSError as exc:
        _log.warning("could not list owner markers: %s", exc, exc_info=True)
        return 0
    for owner in owners:
        stem = owner.name[: -len(".owner")]
        survivors = [
            _CONFIG_DIR / f"{stem}.dat",
            # Warisan plaintext sebelum obfuscation; `load_answers` masih
            # memakainya kalau `.dat` tidak ada.
            _CONFIG_DIR / f"{stem}.json",
        ]
        try:
            if any(p.exists() for p in survivors):
                continue
            owner.unlink()
            removed += 1
        except OSError:
            _log.warning("could not remove orphan owner marker: %s", owner,
                         exc_info=True)
    return removed


def clear_stale_identity_on_startup() -> bool:
    """Bersihkan identitas + konteks dari proses sebelumnya. True bila ada.

    Ronde 6 (item 3). Prinsipnya sama dengan `_cleanup_at_exit`/jalur
    `__main__`: identitas adalah data pribadi yang hanya boleh bertahan
    selama ujiannya benar-benar berjalan. Proses yang DIBUNUH (listrik
    mati, task manager kill, crash) tidak pernah menjalankan satu pun
    jalur keluar itu, jadi identitas siswa A tertinggal di `config.json`.
    Siswa berikutnya di PC yang sama, dengan token yang sama, mendapat form
    yang sudah terisi — dan `last_input.returnPressed` terikat ke submit,
    jadi SATU Enter sudah cukup untuk tercatat sebagai A. Tanpa dialog,
    tanpa warning, tanpa log.

    Karena itu pemanggil harus memanggil ini saat launch. Sengaja TIDAK
    dipanggil sendiri dari modul ini: hanya pemanggil yang tahu bahwa
    belum ada dialog yang perlu menampilkan identity dari config —
    memanggilnya di `import` akan menghapus prefill yang sah untuk
    recovery yang masih berjalan.

    H12: sekalian menyapu `answers_<id>.owner` yang sudah yatim. Ini
    jalur start, jadi tidak ada ujian yang sedang hidup — maka "sapu"
    di sini tidak mungkin menyentuh sidecar yang sedang dipakai. Jalur
    `clear_identity()` (dipanggil saat jendela ditutup, yaitu saat ujian
    masih berjalan) sengaja TIDAK menyapunya.

    Idempoten dan tidak pernah melempar. `True` berarti ada identitas sisa
    yang dibersihkan (berguna untuk logging).
    """
    swept = 0
    try:
        swept = _sweep_orphan_owner_markers()
    except Exception:
        _log.warning("could not sweep orphan owner markers",
                     exc_info=True)
    try:
        store = _load()
        session = store.get("identity_session")
        if isinstance(session, dict) and session:
            had = bool(session.get("identity_data"))
        else:
            had = bool(store.get("identity_data"))
        if not had and not store.get("identity_context"):
            if swept:
                _log.info(
                    "%s sidecar owner yatim dibersihkan saat start", swept,
                )
            return False
        clear_identity()
        _log.info("identitas sisa dari proses sebelumnya dibersihkan saat start")
        if swept:
            _log.info(
                "%s sidecar owner yatim dibersihkan saat start", swept,
            )
        return True
    except Exception:
        _log.warning("could not clear stale identity on startup",
                     exc_info=True)
        return False

