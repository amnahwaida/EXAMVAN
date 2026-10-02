"""Utility functions: MAC address, device ID, clipboard helpers.

Cross-platform — works on Linux and Windows.
"""

from __future__ import annotations

import hashlib
import platform
import re
import socket
import urllib.parse
import uuid
from pathlib import Path
from typing import Dict, Optional

from PyQt5.QtWidgets import QApplication

# Cached device labels, satu per attempt_key. Lihat get_device_label() untuk
# alasan nilainya harus stabil sepanjang proses, bukan hanya per panggilan.
_device_label_cache: Dict[str, str] = {}

# Machine id mentah (tanpa scope). Dibaca SATU kali lalu dipakai ulang, supaya
# enumerasi adapter yang berubah-ubah di tengah sesi tidak mengubah label.
_machine_id_cache: Optional[str] = None


# Kata PERTAMA (setelah split non-alfanumerik) yang menandai tiap slot.
# Hanya kata pertama yang dibaca — dispatch, bukan substring matching.
# Dulu grup keyword dipindai sebagai substring di SELURUH kunci dengan
# kata bersama (`peserta`/`exam`/`ujian` di banyak grup) sehingga
# `nama_peserta` jatuh ke exam_number karena grup number diproses dulu.
# Kata seperti ujian/exam/kode/tanggal/lahir/date/time SENGAJA tidak ada
# di himpunan mana pun: kunci yang diawali kata tak dikenal DILEWATI
# (tidak pernah ditebak) supaya server menolak dengan pesan yang jelas.
_NUMBER_FIRST_WORDS = frozenset({"nomor", "number", "no", "nis", "nisn", "nip"})
_NAME_FIRST_WORDS = frozenset({"nama", "name", "siswa", "student", "peserta"})
_CLASS_FIRST_WORDS = frozenset({"kelas", "class", "rombel", "kelompok"})


def _first_word(key: object) -> str:
    """Kata pertama kunci, lowercase, pecah pada non-alfanumerik."""
    words = [w for w in re.split(r"[^a-z0-9]+", str(key or "").lower()) if w]
    return words[0] if words else ""


def map_identity_to_standard(identity_data: Dict[str, str]) -> Dict[str, str]:
    """Map dynamic identity field keys to standard Go backend keys.

    The Go backend always expects these keys in identity_data:
      student_name, exam_number, student_class

    But exams can use custom keys like 'nama', 'nomor_ujian', 'kelas', etc.
    Pencocokan tiga lapis, deterministik terhadap urutan dict:

    (a) kunci kanonik cocok persis dulu (case-insensitive), tanpa menebak;
    (b) dispatch kata PERTAMA saja (lihat _NUMBER/_NAME/_CLASS_FIRST_WORDS);
        kata pertama tak dikenal → kunci dilewati, tidak pernah ditebak;
    (c) nilai yang sudah terpakai tidak dipakai ulang untuk slot lain.
    """
    result: Dict[str, str] = {}
    items = list(identity_data.items())
    if not items:
        return result

    # (a) Kunci kanonik cocok persis dulu — tanpa menebak.
    for key, val in items:
        kl = str(key or "").lower()
        if kl == "student_name" and "student_name" not in result:
            result["student_name"] = val
        elif kl == "exam_number" and "exam_number" not in result:
            result["exam_number"] = val
        elif kl == "student_class" and "student_class" not in result:
            result["student_class"] = val

    # (b) Dispatch kata pertama. Kandidat per slot dikumpulkan lalu
    # dipilih dalam urutan kunci ter-sortir supaya hasilnya TIDAK
    # tergantung urutan dict (setiap kunci memetakan deterministik).
    slots = (
        ("exam_number", _NUMBER_FIRST_WORDS),
        ("student_name", _NAME_FIRST_WORDS),
        ("student_class", _CLASS_FIRST_WORDS),
    )
    assigned = set(result.values())  # track assigned values to avoid duplicates
    for std_key, first_words in slots:
        if std_key in result:
            continue
        candidates = sorted(
            ((k, v) for k, v in items if _first_word(k) in first_words),
            key=lambda kv: str(kv[0]),
        )
        for _key, val in candidates:
            # (c) Nilai yang sudah diklaim slot lain tidak dipakai ulang.
            if val in assigned:
                continue
            result[std_key] = val
            assigned.add(val)
            break

    # 2. Sisa field yang TIDAK terpetakan TIDAK boleh dipaksakan ke slot
    #    standar.
    #
    #    Dulu: `for std_key in (...): if std_key not in result and
    #    remaining: result[std_key] = remaining.pop(0)` — nilai yang tidak
    #    cocok keyword apa pun diisi ke slot identitas menurut urutan dict.
    #    Terbukti: {'nama','kelas','tanggal_lahir'} -> exam_number =
    #    '2010-05-05'. Tanggal lahir tercatat sebagai nomor ujian, tanpa
    #    error dan tanpa log.
    #
    #    Sekarang slot yang tidak terpetakan dibiarkan kosong supaya sisi
    #    server menolak dengan pesan yang bisa dibaca guru
    #    (`exams.go` "Identitas '%s' wajib diisi", HTTP 400). Satu data
    #    salah diam-diam lebih buruk daripada satu error yang jelas, dan
    #    menebak di client justru menghapus satu-satunya sinyal itu.

    # Remove empty values — Go backend rejects empty student_name/number/class
    return {k: v for k, v in result.items() if v}


def get_mac_address() -> str:
    """Get first non-loopback MAC address.

    Linux: reads /sys/class/net/*/address.
    Windows/fallback: uuid.getnode().
    """
    # Linux: sysfs is fastest and most reliable
    if not platform.system() == "Windows":
        # Lewati interface virtual/loopback; utamakan fisik pertama,
        # jatuh ke non-lo pertama bila hanya virtual yang ada.
        _VIRTUAL_PREFIXES = (
            "lo", "docker", "br-", "veth", "virbr", "vnet",
            "tun", "tap", "vmnet",
        )
        try:
            first_fallback = None
            for addr_path in sorted(Path("/sys/class/net").glob("*/address")):
                mac = addr_path.read_text().strip()
                iface = addr_path.parent.name
                if mac == "00:00:00:00:00:00":
                    continue
                if any(iface == p or iface.startswith(p) for p in _VIRTUAL_PREFIXES):
                    continue
                return mac.upper()
            # Fallback: non-lo pertama apa pun (termasuk virtual).
            for addr_path in sorted(Path("/sys/class/net").glob("*/address")):
                mac = addr_path.read_text().strip()
                iface = addr_path.parent.name
                if iface != "lo" and mac != "00:00:00:00:00:00":
                    first_fallback = mac.upper()
                    break
            if first_fallback:
                return first_fallback
        except (OSError, ValueError, UnicodeDecodeError):
            pass

    # Cross-platform fallback via uuid
    mac = uuid.getnode()
    return ":".join(f"{(mac >> i) & 0xFF:02X}" for i in range(40, -1, -8))


def get_device_id(scope: Optional[str] = None) -> str:
    """Stable device identifier: SHA256(MAC + hostname), di-scope opsional.

    `scope` mengikat identitas ke satu percobaan ujian (lihat
    `build_attempt_key`). Tanpa scope, hasilnya identitas MESIN seperti
    sebelumnya. Dengan scope, hasilnya "kursi yang sedang dipakai satu
    siswa pada satu sesi ujian" -- tetap stabil sepanjang satu percobaan,
    tapi berbeda antar siswa.
    """
    global _machine_id_cache
    if _machine_id_cache is None:
        mac = get_mac_address()
        hostname = socket.gethostname()
        raw = f"{mac}:{hostname}"
        _machine_id_cache = hashlib.sha256(raw.encode()).hexdigest()[:32]
    if scope:
        # Machine id di-hash ulang dengan scope, bukan diabaikan: dengan
        # begitu dua PC berbeda yang kebetulan memakai token dan identitas
        # yang sama tidak pernah terlihat sebagai satu perangkat.
        return hashlib.sha256(
            f"{_machine_id_cache}:{scope}".encode()
        ).hexdigest()[:32]
    return _machine_id_cache


def build_attempt_key(
    token: str, identity_data: Optional[Dict[str, str]] = None
) -> str:
    """Kunci per percobaan: token ujian + identitas siswa.

    Ini yang membuat lab sekolah bisa dipakai. Label perangkat lama
    melekat pada MESIN (`SHA256(MAC + hostname)`), sehingga satu PC hanya
    boleh satu kali Percobaan ujian selamanya -- tidak peduli berapa siswa
    yang memakai PC itu. Server menegakkan "satu perangkat satu percobaan",
    dan dalam lab keenam siswa berikutnya diblokir.

    Yang benar adalah "perangkat" = kursi yang SEDANG DIPAKAI. Kuncinya
    sudah tersedia di keempat call site (token dan identity_data keduanya
    sampai ke `ExamViewer` dan `WaitingApprovalDialog`), dan karena hanya
    machine-id + kunci yang di-hash, mengetik ulang nama dengan spasi
    berbeda tidak mengubah label di tengah sesi.
    """
    std = map_identity_to_standard(identity_data or {})
    # Selaras dengan _student_key_and_source: bagian identitas di-strip +
    # lower, token di-strip tapi huruf dipertahankan.
    parts = [(token or "").strip()]
    for key in ("student_name", "exam_number", "student_class"):
        parts.append(str(std.get(key, "")).strip().lower())
    return "|".join(parts)


def build_student_key(
    identity_data: Optional[Dict[str, str]], token: str = ""
) -> str:
    """Kunci "percobaan" berbasis SISWA, dipakai untuk marker submit.

    Label perangkat (lihat `get_device_label`) sengaja per-kursi supaya
    lab bisa dipakai. Marker "sudah dikumpulkan" perlu Treatment yang
    BERBEDA: dia harus memblokir percobaan ULANG oleh siswa yang sama,
    tapi TIDAK boleh memblokir siswa berikutnya di PC yang sama.

    Nomor ujian jadi kunci utama karena itu yang paling stabil dan paling
    unik di kelas. Kalau ujian tidak mengumpulkan nomor, turun ke nama.
    Kalau tidak ada keduanya, jatuh ke token -- perilaku lama, dan aman
    (konservatif: memblokir daripada melepas).

    Sengaja TIDAK memakai label perangkat: itu per-kursi, jadi seluruh
    kelas yang berbagi satu token akan saling memblokir.
    """
    value, _ = _student_key_and_source(identity_data, token)
    return value


def _student_key_and_source(
    identity_data: Optional[Dict[str, str]], token: str = ""
):
    """`(kunci, asal)` — asal dipakai untuk pesan diagnostik gerbang submit.

    "token" di sini berarti identitas TIDAK terbaca dan kunci jatuh ke
    token. Kalau itu yang terjadi, seluruh kelas yang berbagi token akan
    saling memblokir -- jadi harus terlihat di pesan, bukan tersembunyi.
    """
    std = map_identity_to_standard(identity_data or {})
    for key in ("exam_number", "student_name", "student_class"):
        value = str(std.get(key, "")).strip()
        if value:
            return value.lower(), key
    return (token or "").strip(), "token"


def student_label(
    identity_data: Optional[Dict[str, str]], token: str = ""
) -> str:
    """Teks yang bisa dibaca manusia untuk pesan gerbang submit.

    Berbeda dari `build_student_key`, ini bukan kunci pencocokan — ini
    hanya keterangan. Kalau identitasnya tidak terbaca, dikembalikan
    penjelasan singkat bahwa pencocokan jatuh ke token; nomor token
    sendiri tidak pernah ditampilkan karena sudah jadi kredensial.
    """
    std = map_identity_to_standard(identity_data or {})
    for key in ("exam_number", "student_name", "student_class"):
        value = str(std.get(key, "")).strip()
        if value:
            return f"{key}={value}"
    return "token (identitas kosong)"


def build_result_link(server_url: str, exam_token: str) -> str:
    """Short-link halaman hasil: `{base}/{token}` -> server redirect ke
    `/hasil/<token>`.

    PADANAN PERSIS dengan `ResultsLinkPolicy.build()` di Android. Format
    URL hasil harus didefinisikan di SATU tempat per klien: begitu ada dua
    salinan, satu akan tertinggal saat yang lain berubah, dan siswa
    menyalin link yang salah.

    Token dipakai sebagai PATH SEGMENT, bukan query param, supaya tidak
    ikut terkirim lewat header `Referer` ke pihak ketiga. Path prefix
    base dipertahankan -- instalasi di balik reverse proxy pada
    `/examvan/` harus tetap menghasilkan link yang benar.

    Mengembalikan string KOSONG, bukan `None` atau `"None"`, saat
    base/token kosong: pemanggil bisa membedakannya dari URL yang benar
    dan mematikan tombolnya, alih-alih menyalin teks tak berguna ke
    clipboard siswa.
    """
    base = str(server_url or "").strip().rstrip("/")
    token = str(exam_token or "").strip()
    if not base or not token:
        return ""
    # Buang query/fragment dari base (config yang diedit manual bisa
    # membawa '?x=1'/'#frag' yang ikut ke link hasil).
    base = base.split("?", 1)[0].split("#", 1)[0].rstrip("/")
    if not base:
        return ""
    return f"{base}/{urllib.parse.quote(token, safe='')}"


def get_device_label(attempt_key: Optional[str] = None) -> str:
    """Return 'DESKTOP:<device_id>' (universal label for desktop clients).

    Cached untuk setiap attempt_key sepanjang proses. Label ini bukan
    kosmetik — ia kunci di empat tempat: `X-Device-Id` untuk unduhan PDF,
    serta `mac_address` untuk request-approval, submit, dan presence. Kalau
    berubah di tengah sesi, gate PDF tidak match baris approval (tidak ada
    unduhan) dan approval lama tidak pernah di-revoke.

    `attempt_key` (lihat `build_attempt_key`) mengikat label ke satu
    percobaan ujian, bukan ke mesin fisik. Tanpa itu, satu PC lab hanya
    bisa dipakai satu kali. Empat call site WAJIB memakai kunci yang sama
    — itu sebabnya label dihitung sekali di constructor, bukan di setiap
    tempat.

    Caching menutup seluruh kelas masalah: tidak ada lagi yang bisa membuat
    keempat call site berbeda. Caching juga mengisolasi sesi dari
    `uuid.getnode()` yang bisa berubah di tengah jalan (MAC acak di Windows
    10/11, dan ia mengembalikan adapter mana pun yang ter-enumerate duluan
    di mesin dengan Wi-Fi + Ethernet + Bluetooth + VPN).
    """
    key = attempt_key or ""
    if key not in _device_label_cache:
        _device_label_cache[key] = f"DESKTOP:{get_device_id(key or None)}"
    return _device_label_cache[key]


def reset_device_label_cache() -> None:
    """Drop the cached device labels. For tests only."""
    global _machine_id_cache
    _machine_id_cache = None
    _device_label_cache.clear()
