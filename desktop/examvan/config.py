"""Persistent configuration stored in ~/.config/examvan/config.json.

Answers saved to disk are obfuscated (XOR) to prevent casual tampering.
"""

from __future__ import annotations

import base64
import json
import logging
import os
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


def _restrict_to_owner(path: Path) -> None:
    """Tetapkan mode 0600: hanya pemilik yang boleh membaca.

    Best-effort: di Windows `chmod` hanya read-only
    bit, jadi mode POSIX tidak sepenuhnya berlaku di sana. Yang penting
    Unix -- dan Windows Lab -- tidak menyimpan file kredensial yang bisa
    dibaca user lain. Kegagalan tidak boleh mematikan app.
    """
    try:
        path.chmod(0o600)
    except OSError as exc:
        _log.warning("could not restrict permissions on %s: %s", path, exc)

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
            token = str(value or "").strip()
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
    _CONFIG_DIR.mkdir(parents=True, exist_ok=True)
    tmp = _CONFIG_FILE.with_suffix(".tmp")
    try:
        with open(tmp, "w", encoding="utf-8") as f:
            json.dump(_cache, f, indent=2, ensure_ascii=False)
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
        #alfabet file bisa sudah ada dari versi lama dengan mode longgar;
        # `set()` menulis ulang berkali-kali jadi harus dijaga tiap kali.
        _restrict_to_owner(_CONFIG_FILE)
    except OSError as exc:
        # Sama seperti clear_answers: `set()` dipanggil dari thread GUI
        # (mis. `_connect_thread` yang menyimpan URL+token sebelum gate),
        # dan exception dari sana mematikan seluruh app lewat qFatal.
        # Config yang gagal ditulis berarti token/URL belum tersimpan --
        # sesuatu yang bisa diamati dan dicoba lagi, bukan crash.
        _log.warning("could not write config: %s", _CONFIG_FILE, exc_info=True)


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
            # Bukan arsip: hanya kandidat yang masih mungkin dipakai.
            history = [h for h in history if isinstance(h, str) and h][-7:]
            if previous not in history:
                history.append(previous)
            store["exam_token_history"] = history
    _load()[key] = value
    _save()


def get_all() -> Dict[str, Any]:
    return dict(_load())


def save_answers(exam_id: int, answers: Dict[str, Any]) -> None:
    """Save answers to disk for crash recovery (obfuscated)."""
    path = _CONFIG_DIR / f"answers_{exam_id}.dat"
    _CONFIG_DIR.mkdir(parents=True, exist_ok=True)
    tmp = path.with_suffix(".tmp")
    try:
        encoded = _encode_answers(answers)
        with open(tmp, "w", encoding="ascii") as f:
            f.write(encoded)
        _restrict_to_owner(tmp)
        tmp.replace(path)
    except Exception:
        # If obfuscation fails, don't write anything readable
        if tmp.exists():
            tmp.unlink()


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
            legacy.unlink()
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
                 _CONFIG_DIR / f"answers_{exam_id}.json"):
        try:
            if path.exists():
                path.unlink()
        except OSError:
        # Sengaja ditelan, tapi harus meninggalkan jejak di log.
            _log.warning("could not remove saved answers: %s", path,
                         exc_info=True)


def resolve_submit_answers(memory_answers: Dict[str, Any], exam_id: int) -> Dict[str, Any]:
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
    return {
        key: (val if isinstance(val, str) else "identitas tidak diketahui")
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
    config.json; nomor dan nama siswa disimpan sebagai label karena
    datanya sudah ada plaintext di `identity_data` pada file yang sama,
    jadi tidak menambah risiko.
    """
    store = dict(_submitted_map(exam_id))
    store[_submitted_key(attempt_key)] = label or "identitas tidak diketahui"
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

