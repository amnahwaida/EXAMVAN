"""H11 — Satu kunci yang menyebut DUA slot harus bisa mengisi keduanya.

Bukti eksekusi (fungsi asli, sebelum perbaikan):

    {"nama_kelas": "9A"}            -> {'student_name': '9A'}   class: None
    {"rombel_nama": "9A"}           -> {'student_name': '9A'}   class: None
    {"kelas_nama": "9A"}            -> {'student_name': '9A'}   class: None
    {"nama": "Andi", "nama_kelas": "9A"} -> student_name 'Andi', class '9A'

`_slot_candidates` sudah mengembalikan kandidat untuk kedua slot, jadi
mekanisme "satu kunci, dua slot" itu ADA — tapi `map_identity_to_standard`
memblokirnya:

    assigned = set(result.values())
    ...
    if val in assigned:
        continue

Begitu kunci dua-slot mengisi slot nama, nilai yang sama DILARANG mengisi
slot kelas, dan karena tier diiterasi `exam_number -> student_name ->
student_class`, slot nama selalu menang. Jadi kelas `'9A'` hilang, dan
`submissions.student_class` kosong untuk satu kelas penuh.

Pilihan desain
--------------
Dua opsi, dan hanya satu yang bisa dipilih tanpa sengaja membatalkan desain
ronde 7 yang dikunci `test_r7_identity_slots.py` (`:183-185`irmewa
`{"nama":"Andi","nama_kelas":"9A"}` harus menghasilkan class '9A', dan
`:514` `({"nama_kelas":"9A"}, "9a")`):

  (A) Persempit `_slot_candidates`: kunci yang menyebut >1 slot di-DEKLARASI
      ambigu dan tidak mengisi apa pun.
  (B) Satu nilai_raw boleh mengisi dua slot — tapi HANYA kalau keduanya
      berasal dari kunci yang SAMA.

Yang dipilih: **(B)**.

Alasannya:
  1. (A) menghapus class '9A' dari `{"nama_kelas": "9A"}` — kolom jadi kosong
     BERKURANG dari sekarang, bukan bertambah. Dampak yang dilaporkan
     ("class kosong untuk satu kelas") justru makin buruk.
  2. Aturan "nilai yang sudah dipakai tidak dipakai ulang" (invariant yang
     dibangun ronde 6 untuk determinisme) tetap JAGA kalau larangannya
     digeser jadi "belum dipakai oleh KUNCI LAIN": `{"nama":"Andi",
     "kelas":"Andi"}` tetap tidak mengisi dua kolom (lihat test di bawah).
  3. Nilai yang mengisi dua slot itu memang milik keduanya: `nama_kelas`
     = "class name" — satu field, satu nilai, dan guru yang menamainya
     sedang bermaksud mengisi kolom KELAS. Yang terjadi pada (A) adalah
     menghapus data yang sudah ada, dan tidak ada yang bisa memperbaikinya
     dari sisi siswa.

Yang benar-benar rusak pada (B) adalah pengamatan di dalam kasus "siswa
tidak punya field nama sama sekali": slot nama memakai nilai kelas. Itu
konsekuensi config yang memang tidak punya nama, dan tidak bisa ditebak
mana yang benar tanpa menebak. Yang bisa dijamin: kelas tidak lagi hilang.

Serta di sini: gate `no` Python vs Go, dan indefensi silang lewat tabel
fixture Go yang dibaca langsung.
"""

from __future__ import annotations

import json
import logging
import os
import re
import shutil
import subprocess
import unittest
from pathlib import Path

os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")

from examvan.utils import (
    build_attempt_key,
    build_student_key_source,
    identity_data_with_canonical,
    map_identity_to_standard,
)

REPO_ROOT = Path(__file__).resolve().parents[2]
GO_TEST = (
    REPO_ROOT / "webui" / "internal" / "helpers"
    / "student_key_whole_word_test.go"
)

TOKEN = "TOK1"


def _map(data):
    """Pemetaan dengan logging dimatikan supaya output test tetap bersih."""
    return map_identity_to_standard(data)


class _Capture(logging.Handler):
    def __init__(self):
        super().__init__(level=logging.INFO)
        self.lines = []

    def emit(self, record):
        self.lines.append(record.getMessage())


def _capture_info(fn):
    """Jalankan `fn` sambil mengumpulkan record INFO dari examvan.utils.

    Berbeda dari `assertLogs`, ini TIDAK gagal kalau tidak ada satu pun
    record — sebagian pengujian justru memeriksa ketiadaan sebuah pesan.
    """
    log = logging.getLogger("examvan.utils")
    handler = _Capture()
    previous = log.level
    log.addHandler(handler)
    log.setLevel(logging.INFO)
    try:
        fn()
    finally:
        log.removeHandler(handler)
        log.setLevel(previous)
    return "\n".join(handler.lines)


class TwoSlotKeyTest(unittest.TestCase):
    """Satu kunci, dua slot — harus benar-benar mengisi keduanya."""

    def test_lone_nama_kelas_fills_the_class_slot_too(self):
        # Kasus yang dilaporkan: kelas '9A' hilang begitu slot nama memakainya.
        out = _map({"nama_kelas": "9A"})
        self.assertEqual(out.get("student_class"), "9A", out)

    def test_lone_rombel_nama_fills_the_class_slot_too(self):
        self.assertEqual(_map({"rombel_nama": "9A"}).get("student_class"), "9A")

    def test_lone_kelas_nama_fills_the_class_slot_too(self):
        self.assertEqual(_map({"kelas_nama": "9A"}).get("student_class"), "9A")

    def test_lone_class_name_fills_the_class_slot_too(self):
        self.assertEqual(_map({"class_name": "9A"}).get("student_class"), "9A")

    def test_value_is_not_stored_twice_across_different_keys(self):
        # Yang tetap dilindungi: satu NILAI dari KUNCI BERBEDA tidak boleh
        # mengisi dua kolom. Ini yang menjaga "satu siswa tidak punya dua
        # nama" dan membuat kunci bisa diandalkan.
        out = _map({"nama": "Andi", "kelas": "Andi"})
        self.assertEqual(out.get("student_name"), "Andi", out)
        self.assertIsNone(out.get("student_class"), out)

    def test_dedicated_keys_still_win_over_the_shared_two_slot_key(self):
        # `nama` dan `kelas` khusus tetap menang, dan keduanya tercatat.
        out = _map({"nama": "Andi", "nama_kelas": "9A", "kelas": "9B"})
        self.assertEqual(out.get("student_name"), "Andi", out)
        self.assertEqual(out.get("student_class"), "9B", out)

    def test_two_slot_key_alone_still_feeds_the_class_column(self):
        # Cek jalur yang benar-benar rusak: payload yang dikirim ke server
        # harus membawa student_class, karena `api/exams.go` membaca HANYA
        # kunci kanonik dari identity_data sebelum kolom top-level.
        data = identity_data_with_canonical({"nama_kelas": "9A"})
        self.assertEqual(data.get("student_class"), "9A", data)
        self.assertEqual(data.get("nama_kelas"), "9A",
                         "key asli tidak boleh hilang (validasi server memakainya)")
        json.dumps(data)  # payload harus tetap bisa diserialisasi

    def test_no_two_slot_key_no_collision_with_other_students_data(self):
        # Dua kelas berbeda dengan field yang sama tetap terpisah.
        a = _map({"nama_kelas": "9A"})
        b = _map({"nama_kelas": "8B"})
        self.assertNotEqual(a.get("student_class"), b.get("student_class"))
        self.assertNotEqual(
            build_attempt_key(TOKEN, {"nama": "Andi", "nama_kelas": "9A"}),
            build_attempt_key(TOKEN, {"nama": "Andi", "nama_kelas": "8B"}),
        )

    def test_input_dict_is_not_mutated(self):
        src = {"nama_kelas": "9A"}
        _map(src)
        self.assertEqual(src, {"nama_kelas": "9A"})


class TwoSlotKeyIsReportedTest(unittest.TestCase):
    """Kunci yang mengisi dua slot harus terlihat di log.

    Config seperti `nama_kelas`/`rombel_nama` bekerja sekarang, tapi ia
    menyiratkan sesuatu yang tidak bisa ditebak: satu nilai mengisi "siapa"
    dan "kelas" sekaligus. Kalau guru tidak sadar, tabel hasil publik akan
    menampilkan nama kelas di kolom nama. Itu harus terlihat di log, bukan
    hanya benar secara diam-diam.
    """

    def test_two_slot_key_is_reported(self):
        # `nama_kelas` TANPA field nama lain: satu kunci mengisi dua slot.
        joined = _capture_info(lambda: _map({"nama_kelas": "9A"}))
        self.assertIn("nama_kelas", joined)
        self.assertIn("2", joined, "harus menyebut jumlah slot yang terisi")

    def test_two_slot_key_alongside_a_real_name_is_reported(self):
        # `nama_kelas` + `nama`: kunci dua-slot hanya mengisi kelas, jadi
        # bukan fill ganda — tidak boleh dilaporkan sebagai ambigu.
        joined = _capture_info(lambda: _map({"nama": "Andi", "nama_kelas": "9A"}))
        self.assertNotIn("2 slot", joined, joined)

    def test_single_slot_keys_are_not_reported_as_multi_slot(self):
        joined = _capture_info(lambda: _map(
            {"nama": "Andi", "kelas": "9A", "nomor_ujian": "N01"}))
        self.assertNotIn(
            "dua slot", joined,
            f"kunci satu-slot tidak boleh dilaporkan ambigu: {joined!r}",
        )


class NoWordGateTest(unittest.TestCase):
    """`no` hanya berarti "nomor" kalau berdiri sendiri atau diikuti identitas.

    Ini gate yang sudah benar di Python (ronde 7) tapi TIDAK ada di Go —
    lihat `test_r8_go_parity_identity.py`. Di sini dipincolok supaya tidak
    regressionsaat tim Go menyentuh dispatch-nya lagi.
    """

    def test_no_followed_by_non_identity_word_claims_nothing(self):
        for key in ("no_hp", "no_telp", "no_telpon", "no_wa", "no_hp_siswa",
                    "no_hp_ortu", "no_telp_siswa"):
            with self.subTest(key=key):
                out = _map({"nama": "Andi", "kelas": "9A", key: "0812"})
                self.assertNotIn("exam_number", out, out)
                self.assertEqual(out.get("student_name"), "Andi", out)
                self.assertEqual(out.get("student_class"), "9A", out)

    def test_no_not_at_first_position_claims_nothing(self):
        for key in ("hp_no", "telp_no", "kode_no"):
            with self.subTest(key=key):
                out = _map({"nama": "Andi", key: "0812"})
                self.assertNotIn("exam_number", out, out)

    def test_no_standalone_or_with_identity_follower_claims_number(self):
        for key, value in (("no", "7"), ("no_peserta", "N02"),
                           ("no_ujian", "N01"), ("no_absen", "12"),
                           ("no_nis", "S1"), ("no_induk", "NI1"),
                           ("no_kelas", "9A"), ("no_siswa", "S2"),
                           ("no_nomor", "N3"), ("no_nama", "Andi")):
            with self.subTest(key=key):
                out = _map({"nama": "Andi", "kelas": "9A", key: value})
                self.assertEqual(out.get("exam_number"), value, out)

    def test_slot_word_beats_the_generic_no_abbreviation(self):
        # Posisi dan tier sama, jadi pemutusnya bukan nama kunci:
        # `no_ujian` < `nomor_ujian` secara lexicografis, tapi kunci yang
        # menyebut slot harus menang.
        out = _map({"no_ujian": "N01", "nomor_ujian": "N02", "nama": "Andi"})
        self.assertEqual(out.get("exam_number"), "N02", out)
        out = _map({"no_peserta": "N01", "nomor_peserta": "N02",
                    "nama": "Andi"})
        self.assertEqual(out.get("exam_number"), "N02", out)

    def test_legitimate_number_keys_are_unaffected(self):
        # Kunci yang sah tetap memetakan exam_number — gate `no` tidak boleh
        # menyapanya.
        for key in ("nomor", "nomor_ujian", "nomor_kursi", "number",
                    "nis", "nisn", "nim", "nip", "no_absen"):
            with self.subTest(key=key):
                out = _map({key: "X1"})
                self.assertEqual(out.get("exam_number"), "X1", out)


class DeterminismTest(unittest.TestCase):
    """Urutan sisipan dict tidak boleh mengubah hasil."""

    CASES = (
        {"nama_kelas": "9A", "nama": "Andi", "kode_ujian": "U-7",
         "no_hp": "0812", "alamat": "Jl. Mawar"},
        {"rombel_nama": "9A", "nomor_ujian": "N01", "no_wa": "08"},
        {"kelas_nama": "9A", "kelas_siswa": "9B", "nama": "Andi"},
        {"nama_ujian": "01", "no_peserta": "N02"},
        {"nama": "Andi", "kelas": "Andi"},
    )

    def test_reversed_insertion_order_gives_the_same_result(self):
        for case in self.CASES:
            with self.subTest(case=sorted(case)):
                forward = _map(dict(case))
                backward = _map(dict(reversed(list(case.items()))))
                self.assertEqual(
                    forward, backward,
                    f"urutan sisipan mengubah hasil: {forward} != {backward}",
                )

    def test_repeated_calls_are_stable(self):
        for case in self.CASES:
            with self.subTest(case=sorted(case)):
                first = _map(dict(case))
                for _ in range(20):
                    self.assertEqual(_map(dict(case)), first)


class ExistingGoodCasesTest(unittest.TestCase):
    """Kasus yang sudah benar sebelumnya tidak boleh regress."""

    def test_custom_three_key_config(self):
        out = _map({"nama": "Andi", "nomor_ujian": "N01", "kelas": "9A"})
        self.assertEqual(out, {
            "student_name": "Andi", "exam_number": "N01",
            "student_class": "9A",
        })

    def test_tier2_ignored_when_tier1_present(self):
        self.assertEqual(
            _map({"nama_ujian": "01"}).get("student_name"), "01")
        self.assertEqual(_map({"kelas_siswa": "9A"}).get("student_class"), "9A")
        self.assertEqual(
            _map({"studentClass": "9A"}).get("student_class"), "9A")

    def test_tier2_only_keys(self):
        self.assertEqual(_map({"kode_ujian": "U-7"}).get("exam_number"), "U-7")
        self.assertEqual(_map({"ujian": "U-8"}).get("exam_number"), "U-8")

    def test_date_and_time_words_never_claim_a_slot(self):
        for key in ("tanggal_lahir", "exam_date", "jam_ujian", "waktu_mulai"):
            with self.subTest(key=key):
                self.assertEqual(_map({key: "2010-05-05"}), {})

    def test_unknown_keys_claim_nothing(self):
        for key in ("alamat", "kode_pos", "agama", "nilai", "email",
                    "jurusan", "angkatan", "field_0"):
            with self.subTest(key=key):
                self.assertEqual(_map({key: "X"}), {})

    def test_canonical_keys_win_exactly(self):
        out = _map({"Student_Name": "A", "student_name": "B"})
        self.assertEqual(out.get("student_name"), "B", out)

    def test_blank_values_are_never_candidates(self):
        out = _map({"student_name": "  ", "nama": "Andi", "kelas": "9A"})
        self.assertEqual(out.get("student_name"), "Andi", out)
        self.assertEqual(out.get("student_class"), "9A", out)

    def test_student_key_source_falls_back_in_the_documented_order(self):
        self.assertEqual(
            build_student_key_source({"nama_ujian": "01"}),
            ("01", "student_name"),
        )
        self.assertEqual(
            build_student_key_source({"nomor_ujian": "N01"}),
            ("n01", "exam_number"),
        )
        self.assertEqual(
            build_student_key_source({"kelas": "9A"}), ("9a", "student_class"))
        self.assertEqual(build_student_key_source({}), ("", "token"))


class GoParityFromPythonTest(unittest.TestCase):
    """Tabel fixture Go dibaca LANGSUNG dari file test Go.

    Tabel di `webui/internal/helpers/student_key_whole_word_test.go` adalah
    satu-satunya daftar kasus yang sudah disepakati dua sisi. Test ini
    mem-parse-nya dan menjalankan setiap kasus lewat implementasi Python,
    sehingga salah satu sisi yang berubah tanpa sisi lain langsung terlihat
    di pytest — tanpa menyalin tabel, karena menyalinnya justru penyebab
    drift yang harus ditutup.

    Parser sengaja KETAT: kalau tidak menemukan satu pun kasus, test gagal.
    `test_go_fixture_table_was_parsed` menjaga itu.
    """

    ENTRY_RE = re.compile(
        r'\{\s*"(?P<name>(?:[^"\\]|\\.)*)"\s*,\s*'
        r'map\[string\]interface\{\}\{(?P<kv>.*?)\}\s*,\s*'
        r'"(?P<want>(?:[^"\\]|\\.)*)"\s*\},',
        re.DOTALL,
    )
    PAIR_RE = re.compile(
        r'"(?P<key>(?:[^"\\]|\\.)*)"\s*:\s*'
        r'(?:float64\(\s*(?P<num>-?\d+(?:\.\d+)?)\s*\)'
        r'|"(?P<val>(?:[^"\\]|\\.)*)")',
    )

    @classmethod
    def setUpClass(cls):
        if not GO_TEST.exists():
            raise unittest.SkipTest(f"file test Go tidak ada: {GO_TEST}")
        cls.source = GO_TEST.read_text(encoding="utf-8")
        cls.cases = []
        for m in cls.ENTRY_RE.finditer(cls.source):
            kv = {}
            for p in cls.PAIR_RE.finditer(m.group("kv")):
                key = p.group("key").replace('\\"', '"')
                if p.group("num") is not None:
                    kv[key] = float(p.group("num"))
                else:
                    kv[key] = (p.group("val") or "").replace('\\"', '"')
            cls.cases.append((
                m.group("name"), kv, m.group("want").replace('\\"', '"')))

    def test_go_fixture_table_was_parsed(self):
        self.assertGreaterEqual(
            len(self.cases), 25,
            "tabel fixture Go gagal di-parse — parser harus ikut diperbarui",
        )

    def test_go_implementation_agrees_with_its_own_table(self):
        """Tutup lingkaran ke-3: Go implementation -> tabel Go.

        Parser di atas hanya membuktikan "Python cocok dengan apa yang tabel
        Go KLAIM". Tanpa test ini, implementasi Go bisa berubahTotal
        (`no` dibuang lagi dari gerbang, dll) sementara tabelnya tetap —
        lalu kedua sisi berbeda persis seperti semula dan pytest tetap hijau.
        Yang membuat dua sisi benar-benar terkunci adalah tiga titik yang
        harusnya menunjuk nilai yang sama:

            Go impl --(go test)--> tabel Go --(test ini)--> Python impl
        """
        if shutil.which("go") is None:
            self.skipTest("toolchain Go tidak ada di PATH")
        webui = GO_TEST.parents[2]  # .../webui
        try:
            proc = subprocess.run(
                ["go", "test", "./internal/helpers/", "-count=1",
                 "-run", "TestStudentKeyFromIdentityData"],
                cwd=str(webui), capture_output=True, text=True, timeout=300,
            )
        except (OSError, subprocess.SubprocessError) as exc:
            self.skipTest(f"tidak bisa menjalankan go test: {exc}")
        self.assertEqual(
            proc.returncode, 0,
            "implementasi Go tidak cocok dengan tabel fixture-nya:\n"
            f"{proc.stdout}\n{proc.stderr}",
        )

    def test_python_agrees_with_every_go_fixture_case(self):
        mismatches = []
        for name, data, want in self.cases:
            got, _source = build_student_key_source(data)
            if got != want:
                mismatches.append(f"{name}: python={got!r} go={want!r} {data}")
        self.assertEqual(
            mismatches, [],
            "client dan server tidak sepakat:\n  " + "\n  ".join(mismatches),
        )

    def test_python_agrees_with_every_go_fixture_case_on_slots(self):
        """Bukan cuma kunci siswa: slot yang DIHITUNG Go harus sama isinya.

        Go hanya mengembalikan satu kunci, jadi tabelnya tidak bisa mengunci
        peta lengkap. Yang bisa dikunci: slot yang jadi sumber kunci Go harus
        berisi nilai yang sama seperti di Python, setelah normalisasi yang
        sama (lower + trim) seperti `StudentKey`.
        """
        mismatches = []
        for name, data, want in self.cases:
            std = map_identity_to_standard(data)
            got, source = build_student_key_source(data)
            if source == "token":
                continue
            raw = std.get(source)
            if isinstance(raw, str) and raw.strip().lower() != want:
                mismatches.append(
                    f"{name}: slot {source}={raw!r} != kunci Go {want!r}")
            if not isinstance(raw, str):
                mismatches.append(
                    f"{name}: slot {source} bukan teks ({raw!r}) — "
                    f"Go mengabaikannya, jadi kunci Go pasti berbeda")
        self.assertEqual(mismatches, [], "\n  ".join(mismatches))


if __name__ == "__main__":
    unittest.main()