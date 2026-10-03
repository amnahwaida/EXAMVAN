"""Utility functions: MAC address, device ID, clipboard helpers.

Cross-platform — works on Linux and Windows.
"""

from __future__ import annotations

import hashlib
import logging
import platform
import re
import socket
import urllib.parse
import uuid
from pathlib import Path
from typing import Dict, Optional

from PyQt5.QtWidgets import QApplication

_log = logging.getLogger(__name__)

# Cached device labels, satu per attempt_key. Lihat get_device_label() untuk
# alasan nilainya harus stabil sepanjang proses, bukan hanya per panggilan.
_device_label_cache: Dict[str, str] = {}

# Machine id mentah (tanpa scope). Dibaca SATU kali lalu dipakai ulang, supaya
# enumerasi adapter yang berubah-ubah di tengah sesi tidak mengubah label.
_machine_id_cache: Optional[str] = None


# Kata penentu slot. Pencocokan batas-kata di SELURUH kunci (bukan hanya
# kata pertama, bukan substring): `id_kelas` adalah kelas, `kode_ujian`
# adalah nomor ujian. Dulu hanya kata PERTAMA yang dibaca dan kunci yang
# tidak cocok "dilewati supaya server menolak dengan pesan yang jelas" --
# padahal server membaca `body.IdentityData[field.Key]` dengan key yang
# TERSIMPAN mentah dan fallback kanonik hanya menyala untuk tiga ejaan
# kanonik, jadi kunci yang tidak cocok tidak pernah 400'd: ia mendarat
# diam-diam di kolom DB yang kosong.
#
# Kata TIER-1 (spesifik) mengalahkan TIER-2 (umum) kapan pun keduanya
# muncul di satu kunci: `studentClass` = kelas (bukan nama, walau
# `student` ada di tier-2), `nomor_ujian` mengalahkan `kode_ujian` untuk
#_slot number. Tanpa tier itu `examNumber` tidak akan pernah cocok
# (`exam` tier-2, `number` tier-1) dan `studentClass` akan mencatat
# kelas sebagai nama.
#
# `no` TIDAK lagi ada di sini tanpa syarat. Dia hanya berarti "nomor"
# kalau seluruh kuncinya `no`, atau kata berikutnya benar-benar menyebut
# identitas (lihat `_NO_FOLLOWERS`). Alasannya,Tier-1 bisa berada di
# posisi 0 dan perebutnya diputuskan lexicografis: `'_' (0x5F) < 'm'`,
# jadi `no_hp` SELALU mengalahkan `nomor_ujian`. Terbukti:
#
#     {'nama','nomor_ujian':'01','kelas','no_hp':'0812'} -> exam_number='0812'
#
# Nomor telepon siswa menjadi nomor ujiannya — kolom yang dibaca PERTAWA
# `repeat_grant.go`, dicetak di halaman selamat, dan ditampilkan di
# tabel hasil publik — dan `build_student_key` ikut menjadi 0812.
# `no_telp`, `no_telpon`, `no_wa`, dan `no_hp_siswa` behaves identically.
#
# Konsekuensi yang DOKUMENTASIKAN (bukan tebakan): tidak ada perubahan
# server yang dibutuhkan untuk kunci yang sekarang terpetakan. `api.py`
# sudah meneruskan `identity_data` apa adanya, dan `api/exams.go` membacanya
# lebih dulu sebelum kolom top-level — jadi begitu pemetaan client benar,
# kolom DB ikut benar. Yang tersisa adalah konfigurasi yang TIDAK punya
# kata slot sama sekali (`alamat`, `kode_pos`, `field_<n>`); itu tidak bisa
# ditebak tanpa bahaya, dan ditutup oleh `identity_data_with_canonical` di
# bawah, bukan dengan tebakan atau 400 paksa.
#
# camelCase ikut terpetakan hanya karena batas `namaSiswa` dipecah lebih dulu
# di _key_words; tanpa itu kata tier-1 di dalamnya tidak pernah terlihat.
_NUMBER_TIER1 = frozenset({"nomor", "number", "nis", "nisn", "nip", "nim"})
_NAME_TIER1 = frozenset({"nama", "name"})
_CLASS_TIER1 = frozenset({"kelas", "class", "rombel", "kelompok"})
# Tier-2: menyebut SLOT ATAU entitas saja. `ujian`/`exam` benar-benar
# nomor ujian, tapi `kode_ujian` dan `examNumber` harus kalah oleh kata
# yang lebih spesifik.
_NUMBER_TIER2 = frozenset({"ujian", "exam"})
_NAME_TIER2 = frozenset({"siswa", "student", "peserta"})

_SLOT_TIERS = (
    ("exam_number", (_NUMBER_TIER1, _NUMBER_TIER2)),
    ("student_name", (_NAME_TIER1, _NAME_TIER2)),
    ("student_class", (_CLASS_TIER1, frozenset())),
)

# Kata tanggal/jam/waktu. Kunci yang memuat salah satunya TIDAK PERNAH
# mengklaim slot standar, meski kata slot-nya ikut ada: `jam_ujian` bukan
# nomor ujian, `exam_date` bukan nomor ujian. Ini yang menjaga
# `tanggal_lahir` tidak pernah tercatat sebagai nomor ujian (N4).
_NON_IDENTITY_WORDS = frozenset({
    "tanggal", "date", "lahir", "birth", "birthday",
    "waktu", "jam", "time", "mulai", "start", "selesai", "end",
})

# Kata yang membuat `no` tetap berarti "nomor" (lihat catatan `_NUMBER_TIER1`).
# Kalau kata berikutnya TIDAK ada di sini, `no` bukan kata nomor: `no_hp`,
# `no_telp`, `no_telpon`, `no_wa`, `no_hp_siswa`, `no_hp_ortu` semuanya
# berarti nomor kontak, bukan nomor ujian. Daftar ini berisi kata yang
# benar-benar menyebut identitas — `no_induk`, `no_nis`, `no_absen`,
# `no_ujian`, `no_peserta`, `no_siswa`, `no_kelas` — sehingga tidak ada
# nomor ujian yang ikut kehilangan slotnya.
_NO_FOLLOWERS = frozenset({
    "absen", "exam", "induk", "kelas", "kelompok", "murid", "name",
    "nama", "nim", "nip", "nis", "nisn", "nomor", "number", "peserta",
    "rombel", "siswa", "student", "ujian",
})

# Kata tier-1 yang hanya SINGKATAN dan menempel pada kata apa saja, jadi
# bisa berada di depan kata yang sama sekali tidak menyebut identitas.
# Di posisi dan tier yang sama, `nomor_ujian` (kata yang menyebut slot)
# harus mengalahkan `no_ujian` (singkatan umum) — bukan sebaliknya.
# Ditentukan lewat `len(key)` sebagai pemutus berikutnya supaya dua kunci
# yang sama spesifiknya tetap punya pemenang yang total dan tidak pernah
# bergantung urutan dict.
_GENERIC_TIER1_WORDS = frozenset({"no"})

# Tier-1 -> slot, untuk pencarian cepat.
_TIER1_SLOT = {}
for _slot, (_t1, _t2) in _SLOT_TIERS:
    for _w in _t1:
        _TIER1_SLOT[_w] = _slot
_TIER2_SLOT = {}
for _slot, (_t1, _t2) in _SLOT_TIERS:
    for _w in _t2:
        _TIER2_SLOT[_w] = _slot
del _slot, _t1, _t2, _w


def _key_words(key: object) -> list:
    """Kata-kata kunci, lowercase, batas kata non-alfanumerik.

    Batas camelCase (`namaSiswa` -> `nama_Siswa`) dipecah SEBELUM
    lowercase. Dulu lowercase dulu: `namaSiswa` jadi satu token
    `namasiswa` dan tidak cocok dengan kata apa pun, sehingga ketiga slot
    kosong dan `build_student_key` jatuh ke token ujian -- satu kunci untuk
    seluruh kelas. Regex-nya pure ASCII; aksen non-ASCII tersaring sebagai
    pemisah, sama seperti perilaku lama.
    """
    raw = re.sub(r"(?<=[a-z0-9])(?=[A-Z])", "_", str(key or ""))
    return [w for w in re.split(r"[^a-z0-9]+", raw.lower()) if w]


def _first_word(key: object) -> str:
    """Kata pertama yang menentukan slot, atau "" kalau tidak ada.

    Tier-1 menang atas tier-2 di mana pun posisinya; kalau tidak ada
    tier-1, tier-2 pertama yang menentukan.
    """
    words = _key_words(key)
    for w in words:
        if w in _TIER1_SLOT:
            return w
    for w in words:
        if w in _TIER2_SLOT:
            return w
    return ""


def _slot_candidates(key: object) -> Dict[str, tuple]:
    """`{slot: (tier, posisi_kata, generik)}` kandidat slot dari satu kunci.

    Dict kosong = kunci ini tidak identitas (kata tanggal/jam, atau tidak
    ada kata slot sama sekali). Kunci seperti itu TIDAK PERNAH diisi ke
    kolom standar -- menebak lebih buruk daripada satu error yang jelas.

    Satu kunci boleh menjadi kandidat LEBIH dari satu slot, tapi HANYA di
    antara kata tier-1. Dulu `_dispatch` mengembalikan hit PERTAMA lalu
    berhenti, jadi `nama_kelas` (kata tier-1 `nama` di posisi 0 dan
    `kelas` di posisi 1) hanya bisa mengisi kolom nama: '9A' dibuang,
    `submissions.student_class` kosong, halaman selamat dan tabel hasil
    publik tidak menampilkan kelas, dan `build_attempt_key` kehilangan
    komponen kelasnya — dua siswa nama sama di kelas berbeda lalu memakai
    satu perangkat yang sama pada percobaan yang sama.

    Tier-2 tetap diabaikan begitu kunci punya hit tier-1, persis seperti
    `_dispatch` lama: `nama_ujian` adalah NAMA (bukan nomor ujian),
    `kelas_siswa` adalah KELAS (bukan nama), `studentClass` adalah kelas.
    `no_hp` tidak mengklaim apa pun sama sekali.
    """
    words = _key_words(key)
    if any(w in _NON_IDENTITY_WORDS for w in words):
        return {}
    tier1: Dict[str, tuple] = {}
    tier2: Dict[str, tuple] = {}

    def _offer(bucket: Dict[str, tuple], slot: str, pos: int,
               word: str) -> None:
        rank = (pos, 1 if word in _GENERIC_TIER1_WORDS else 0)
        current = bucket.get(slot)
        if current is None or rank < current:
            bucket[slot] = rank

    for idx, w in enumerate(words):
        if w == "no":
            # `no` hanya sah sebagai kata nomor di awal kunci, dan hanya
            # kalau kata berikutnya (kalau ada) menyebut identitas.
            if idx == 0 and (len(words) == 1 or words[1] in _NO_FOLLOWERS):
                _offer(tier1, "exam_number", idx, w)
        elif w in _TIER1_SLOT:
            _offer(tier1, _TIER1_SLOT[w], idx, w)
        elif w in _TIER2_SLOT:
            _offer(tier2, _TIER2_SLOT[w], idx, w)
    if tier1:
        return {slot: (0,) + rank for slot, rank in tier1.items()}
    return {slot: (1,) + rank for slot, rank in tier2.items()}


_CANONICAL_SLOTS = ("student_name", "exam_number", "student_class")


def _canonical_rank(key: str, canonical: str):
    """Kunci urutan untuk menyelesaikan tabrakan case-insensitive.

    Ronde 6 (item 4). Urutannya SELALU sama, tidak pernah bergantung urutan
    sisipan dict:

    1. ejaan PERSIS kanonik (`student_name`) menang — itu satu-satunya
       bentuk yang dibaca `canonical` di sisi server
       (`api/exams.go:firstMissingRequiredIdentity`), jadi paling mungkin
       yang dimaksud guru;
    2. lalu key terpendek;
    3. lalu lexicografis sebagai pemutus terakhir (total, jadi dua kunci
       yang beda hanya huruf besar/kecil SELALU punya pemenang yang sama).
    """
    return (0 if key == canonical else 1, len(key), key)


def map_identity_to_standard(identity_data: Dict[str, str]) -> Dict[str, str]:
    """Map dynamic identity field keys to standard Go backend keys.

    The Go backend always expects these keys in identity_data:
      student_name, exam_number, student_class

    But exams can use custom keys like 'nama', 'nomor_ujian', 'kelas', etc.
    Pencocokan tiga lapis, deterministik terhadap urutan dict:

    (a) kunci kanonik cocok persis dulu (case-insensitive), tanpa menebak;
        kalau lebih dari satu key hanya berbeda huruf besar/kecil, tabrakan
        diselesaikan lewat `_canonical_rank` dan ambiguity-nya di-log
        (lihat catatan panjang di bawah);
    (b) kata penentu slot di SELURUH kunci (_slot_candidates/_TIER*):
        batas kata, bukan substring; satu kunci boleh jadi kandidat untuk
        lebih dari satu slot; tier-1 mengalahkan tier-2;
        kata tanggal/jam/waktu tidak pernah mengklaim slot;
    (c) nilai yang sudah terpakai tidak dipakai ulang untuk slot lain.

    Ronde 6 (item 4) — Determinisme
    --------------------------------
    Lapis (a) tadinya berjalan di urutan sisipan `items` dan menang-pertama:

        map_identity_to_standard({"Student_Name": "A", "student_name": "B"})
            -> {"student_name": "A"}
        map_identity_to_standard({"student_name": "B", "Student_Name": "A"})
            -> {"student_name": "B"}

    Data yang sama, hasil berbeda — padahal docstring modul menjanjikan
    "deterministik terhadap urutan dict", dan lapis (b) memang sudah
    menyortir kandidat. Dampaknya bukan kosmetik: hasil fungsi ini adalah
    `student_name` untuk submit, `build_student_key` (kunci marker submit),
    `build_attempt_key` dan `get_device_label` (label perangkat). Config
    yang urutan key-nya berbeda — `IdentityDialog` menyusun ulang field
    menurut urutan respons API, jalur recovery dan halaman hasil membaca
    ulang `identity_data` yang sudah tersimpan — maka seluruh kunci itu
    berubah: recovery sah tidak lagi cocok dengan `answers_<id>.owner`, dan
    gate unduhan PDF tidak match baris approval.

    Sekarang `items` disortir SEKALI di awal, sehingga tidak ada lapisan
    mana pun yang melihat urutan sisipan.
    """
    result: Dict[str, str] = {}
    items = sorted(
        identity_data.items(),
        # `str(key)` supaya kunci non-string (JSON bertipe salah) tetap
        # punya urutan total dan tidak memicu TypeError saat dibandingkan.
        key=lambda kv: (str(kv[0]),),
    )
    if not items:
        return result

    # (a) Kunci kanonik cocok persis dulu — tanpa menebak.
    #
    # Kandidat per slot dikumpulkan lebih dulu, baru dipilih satu.
    # Menyisir `items` sambil langsung mengisi `result` (perilaku lama)
    # membuat pemenang slot hanya bergantung urutan sisipan — lihat catatan
    # determinisme di docstring.
    #
    # Nilai BLANK tidak pernah menjadi kandidat (M5). Go melompatinya di
    # lapisan kanonik maupun di lapisan dispatch
    # (`student_key.go`), sedangkan Python dulu membiarkan `student_name: ''`
    # memblokir slot nama supaya kunci students jatuh ke KELAS:
    # {'student_name':'','nama':'Andi','kelas':'9A'} -> py '9a', go 'andi'.
    canonical_candidates: Dict[str, list] = {}
    for key, val in items:
        kl = str(key or "").lower()
        if kl not in _CANONICAL_SLOTS:
            continue
        if not str(val if val is not None else "").strip():
            continue
        canonical_candidates.setdefault(kl, []).append((key, val))
    for std_key in _CANONICAL_SLOTS:
        candidates = canonical_candidates.get(std_key)
        if not candidates:
            continue
        chosen = min(
            candidates, key=lambda kv: _canonical_rank(str(kv[0]), std_key)
        )
        if len(candidates) > 1:
            _log.warning(
                "kunci kanonik %r ambigu — kandidat %s; dipakai %r "
                "(ejaan kanonik, lalu key terpendek, lalu lexicografis). "
                "Konfigurasi kolom identitas sebaiknya tidak memakai "
                "huruf besar/kecil yang berbeda untuk satu kolom.",
                std_key, sorted(str(k) for k, _v in candidates), chosen[0],
            )
        result[std_key] = chosen[1]

    # (b) Dispatch kata. Kandidat per slot dikumpulkan lalu dipilih dalam
    # urutan (tier, posisi_kata, generik, nama_kunci) supaya hasilnya TIDAK
    # bergantung urutan dict — setiap kunci memetakan deterministik dan
    # kunci yang paling spesifik selalu menang.
    #
    # `claimed` dipakai DUA hal sekaligus: mencatat nilai yang sudah
    # dipakai slot lain (c), dan mencatat kunci mana yang benar-benar
    # mengisi sebuah slot supaya sisa nilainya bisa di-log.
    assigned = set(result.values())  # track assigned values to avoid duplicates
    claimed = {
        str(k) for k, v in canonical_candidates.items() for _key, _v in v
    }
    for std_key, _tiers in _SLOT_TIERS:
        if std_key in result:
            continue
        candidates = []
        for key, val in items:
            if not str(val if val is not None else "").strip():
                # M5: nilai kosong bukan nilai. Go melompatinya, jadi kalau
                # tidak dilompat di sini kunci dan server berbeda.
                continue
            rank = _slot_candidates(key).get(std_key)
            if rank is None:
                continue
            candidates.append((rank[0], rank[1], rank[2], str(key), val))
        for _tier, _pos, _gen, key_str, val in sorted(
            candidates, key=lambda c: c[:4]
        ):
            # (c) Nilai yang sudah diklaim slot lain tidak dipakai ulang.
            if val in assigned:
                continue
            result[std_key] = val
            assigned.add(val)
            claimed.add(key_str)
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
    #    Sekarang slot yang tidak terpetakan dibiarkan kosong. Satu data
    #    salah diam-diam lebih buruk daripada satu error yang jelas, dan
    #    menebak di client menghapus satu-satunya sinyal itu.
    #
    #    Catatan jujur soal "error yang jelas": untuk kunci yang TIDAK
    #    contains kata slot (alamat, kode_pos, field_0) tidak ada 400 dari
    #    server — kolomnya akan kosong. undetected itu yang ditutup
    #    `identity_data_with_canonical` di bawah: payload selalu membawa
    #    kunci kanonik hasil pemetaan ini.
    #
    #    Yang bisa dilakukan client adalah tidak menghilangkan apa pun
    #    tanpa jejak: setiap kunci yang tidak mengisi slot mana pun
    #    dicatat sekali per pemanggilan, supaya `no_hp` yang tidak lagi
    #    jadi nomor ujian (dan `kode_siswa` yang kalah dari `nama`) bisa
    #    ditelusuri dari log alih-alih hilang. Level INFO, bukan WARNING:
    #    `alamat`/`agama`/`tanggal_lahir` memang tidak pernah mengisi
    #    slot dan itu perilaku yang benar, jadi WARNING akan
    #    menjatuhkan ketiadaan yang bukan salah.
    unclaimed = sorted(str(k) for k, _v in items if str(k) not in claimed)
    if unclaimed:
        _log.info(
            "kunci identitas tanpa slot standar: %s", unclaimed,
        )

    # Remove empty values — Go backend rejects empty student_name/number/class
    return {k: v for k, v in result.items() if str(v or "").strip()}


def identity_data_with_canonical(identity_data: Optional[Dict[str, str]]) -> Dict[str, str]:
    """Payload identitas + `student_name`/`exam_number`/`student_class`.

    Pemetaan di atas SENGAJA tidak menebak, jadi konfigurasi yang tidak
    lazim (kunci `alamat`, `kode_pos`, atau kunci sintetis `field_<n>`)
    tetap submit dengan HTTP 200 sementara kolom DB kosong. Server membaca
    `identity_data` lebih dulu sebelum kolom top-level
    (`api/exams.go:1006-1033`), jadi menyisipkan kunci kanonik di sini
    membuat kolom DB otoritatif untuk SETIAP konfigurasi tanpa perubahan
    server sama sekali.

    Key asli tidak pernah hilang: validasi server memakai key yang TERSIMPAN
    (`body.IdentityData[field.Key]`), jadi kunci kanonik hanya tambahan.
    Nilai kanonik yang sudah ada tidak ditimpa, dan input tidak dimutasi.

    Call site: SATU, di `ui/identity_dialog.py`
    (`IdentityDialog.get_identity_data`) — tempat identitas siswa pertama
    kali jadi payload. Dari sana satu dict yang sama dipakai submit,
    dialog persetujuan, penyimpanannya ke config, dan recovery, jadi
    menyambungkannya di titik lain justru membuat dua sumber kebenaran.

    Ini bukan detail kecil: kunci kanonik juga yang menyamakan kembali
    kunci yang dihitung client dengan yang dihitung
    `webui/internal/helpers/student_key.go`. Go membaca `identity_data`
    MULAI DARI kunci kanonik sebelum ia menebak kata, jadi payload yang
    membawa `exam_number` hasil pemetaan client membuat kedua sisi
    berpakat walau Go masih menebak `no_hp` sebagai nomor ujian.
    """
    data = dict(identity_data or {})
    for key, val in map_identity_to_standard(data).items():
        # Blank (whitespace-only) bukan nilai: menyisipkannya hanya
        # memindahkan kolom kosong dari "tidak ada" ke "ada tapi kosong".
        if str(val).strip() and not str(data.get(key, "") or "").strip():
            data[key] = val
    return data


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
    lab bisa dipakai. Marker "sudah dikumpulkan" perlu treatment yang
    BERBEDA: dia harus memblokir percobaan ULANG oleh siswa yang sama,
    tapi TIDAK boleh memblokir siswa berikutnya di PC yang sama.

    Nomor ujian jadi kunci utama karena itu yang paling stabil dan paling
    unik di kelas. Kalau ujian tidak mengumpulkan nomor, turun ke nama.
    Kalau tidak ada keduanya, jatuh ke token -- perilaku lama, dan aman
    (konservatif: memblokir daripada melepas).

    Sengaja TIDAK memakai label perangkat: itu per-kursi, jadi seluruh
    kelas yang berbagi satu token akan saling memblokir.

    CATATAN: kunci ini bisa MENURUN ke nama atau kelas (lihat
    `build_student_key_source`). Dua siswa bernama sama di kelas berbeda
    lalu berbagi satu kunci, jadi jangan pernah jadikan dasar keputusan
    yang dilihat siswa — pakai `build_attempt_key` untuk keputusan
    per-perangkat.
    """
    value, _ = build_student_key_source(identity_data, token)
    return value


def build_student_key_source(
    identity_data: Optional[Dict[str, str]], token: str = ""
):
    """`(kunci, asal)` kunci siswa + slot yang menghasilkannya.

    `asal` adalah `"exam_number"`, `"student_name"`, `"student_class"`, atau
    `"token"` — dan itulah informasi yang tidak bisa ditebak dari
    kuncinya sendiri: nomor ujian unik per siswa, sedangkan nama, kelas,
    dan token BERBAGI. Config madrasah yang paling biasa
    (`nama`, `kelas`, `agama`, `jenis_kelamin`, tanpa kolom nomor)
    menghasilkan kunci `'ahmad'` untuk siapa pun bernama Ahmad, sehingga
    dua teman sekelas di kelas berbeda bertabrakan.

    Karena itu pemanggil yang menampilkan dialog ke siswa WAJIB memeriksa
    `asal` dulu. Melewatkannya berarti bertanya "kamu sudah mengumpulkan?"
    kepada orang yang salah (lihat
    `ServerConfigDialog._offer_resubmit_choice`).
    """
    std = map_identity_to_standard(identity_data or {})
    for key in ("exam_number", "student_name", "student_class"):
        value = str(std.get(key, "")).strip()
        if value:
            return value.lower(), key
    return (token or "").strip(), "token"


def _student_key_and_source(
    identity_data: Optional[Dict[str, str]], token: str = ""
):
    """Alias lama untuk `build_student_key_source` (dipakai internal/test)."""
    return build_student_key_source(identity_data, token)


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
