"""Ronde 7 — pemetaan slot identitas: satu kunci boleh jadi lebih dari satu slot.

Lima temuan yang satu akar: `map_identity_to_standard` memutuskan slot dengan
`_dispatch` yang mengembalikan SATU `(slot, tier, pos)` per kunci, lalu
memilih kandidat dengan urutan `(tier, pos, nama_kunci)` — nilai yang diklaim
slot pertama ikut dihitung sebagai "sudah terpakai" untuk slot berikutnya.

H2 (HIGH) — `no` membajak slot nomor ujian
    `no` ada di `_NUMBER_TIER1`, jadi `no_hp`, `no_telp`, `no_wa`,
    `no_telpon`, `no_hp_siswa` semuapencerai slot `exam_number`. Saat
    `nomor_ujian` juga ada, keduanya tier-1 di posisi 0 dan perebutnya
    diputuskan lexicografis — `'_' (0x5F) < 'm'`, jadi `no_hp` menang:

        {'nama','nomor_ujian','kelas','no_hp'} -> exam_number = '0812'

    Kolom itu dibaca PERTAWA oleh `repeat_grant.go`, dicetak di halaman
    selamat, dan ditampilkan di tabel hasil publik. Nomor telepon siswa
    menjadi nomor ujiannya, dan `build_student_key` ikut menjadi 0812.

M3 (MEDIUM) — `Nama Kelas` jadi `student_name`, kolom kelas kosong
    `nama_kelas` memuat kata tier-1 `nama` (posisi 0) DAN `kelas`
    (posisi 1). `_dispatch` hanya mengembalikan hit yang pertama, jadi
    '9A' dibuang. `class_name` (Inggris) kebetulan aman karena `nama` dan
    `class_name` tidak berebut slot yang sama.

M4 (MEDIUM) — satu hit per kunci, dan kandidat unique bisa hilang
    `{'student_id','nama','kelas'}` -> `student_id` (tier-2, posisi 0)
    kalah dari `nama` (tier-1, posisi 0), lalu dibuang. Nilai unik
    `S001` hilang tanpa satu baris log pun.

M5 (MEDIUM) — Python dan Go berbeda saat nilai kosong
    Go (`webui/internal/helpers/student_key.go`) MELOMPATI kandidat yang
    nilainya blank, di lapisan kanonik maupun di lapisan dispatch.
    Python membiarkan nilai kosong klaim slot dulu dan baru memfilter di
    akhir (`if v`), sehingga `student_name: ''` memblokir `nama` dan kunci
    jatuh ke KELAS:

        {'student_name':'','nama':'Andi','kelas':'9A'}
            py -> '9a'   (kelas)     go -> 'andi'

Perbaikan
---------
* `_dispatch` -> `_slot_candidates`: satu kunci boleh menjadi kandidat
  lebih dari satu slot; tiap slot menyimpan (tier, posisi) terbaiknya.
* `no` hanya dianggap kata nomor kalau dia seluruh kunci atau diikuti
  kata identitas (`induk`, `nis`, `absen`, `ujian`, `peserta`, `siswa`).
* Kata tier-1 yang hanya singkatan umum (`no`) kalah dari kata yang
  menyebut slot secara khusus pada posisi yang sama.
* Kandidat blank dilewati di loop (persis Go), dan kunci kanonik blank
  tidak pernah mengisi slot.
* Setiap kunci yang TIDAK diklaim slot mana pun di-log, jadi tidak ada
  kolom yang hilang diam-diam.
"""

from __future__ import annotations

import json
import logging
import os
import re
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

from examvan.utils import (
    build_attempt_key,
    build_student_key,
    identity_data_with_canonical,
    map_identity_to_standard,
)

REPO = Path(__file__).resolve().parents[2]
GO_SOURCE = REPO / "webui" / "internal" / "helpers" / "student_key.go"
LOGGER = "examvan.utils"

TOKEN = "ABCD1234"


def _map(data):
    """Bungkus agar pesan kegagalan menyebut kasusnya."""
    out = map_identity_to_standard(data)
    return {k: v for k, v in out.items() if v}


def _permutations(data: dict) -> list:
    keys = list(data)
    out = []

    def walk(prefix, rest):
        if not rest:
            out.append({k: data[k] for k in prefix})
            return
        for i, k in enumerate(rest):
            walk(prefix + [k], rest[:i] + rest[i + 1:])

    walk([], keys)
    return out


# ---------------------------------------------------------------------------
# H2 — `no_hp` bukan nomor ujian
# ---------------------------------------------------------------------------


class PhoneNumberNeverBecomesExamNumberTest(unittest.TestCase):
    def test_no_hp_does_not_hijack_the_exam_number(self):
        out = _map({"nama": "Andi", "nomor_ujian": "01", "kelas": "9A",
                    "no_hp": "0812"})
        self.assertEqual(out.get("exam_number"), "01", out)

    def test_no_hp_alone_claims_no_slot_at_all(self):
        out = _map({"nama": "Andi", "no_hp": "0812"})
        self.assertNotIn("exam_number", out, out)
        self.assertEqual(out.get("student_name"), "Andi")

    def test_every_phone_flavour_claims_no_slot(self):
        for key in ("no_hp", "no_telp", "no_telpon", "no_wa",
                    "no_hp_siswa", "no_telp_siswa", "no_hp_ortu"):
            with self.subTest(key=key):
                out = _map({"nama": "Andi", "kelas": "9A", key: "0812"})
                self.assertNotIn("exam_number", out, out)
                self.assertEqual(out.get("student_class"), "9A", out)

    def test_no_hp_never_becomes_the_student_key(self):
        typed = {"nama": "Andi", "nomor_ujian": "01", "kelas": "9A",
                 "no_hp": "0812"}
        self.assertEqual(build_student_key(typed), "01")
        # Nomor ujian berbeda harus tetap menghasilkan kunci berbeda, walau
        # nomor teleponnya sama.
        other = dict(typed, nomor_ujian="02")
        self.assertNotEqual(build_student_key(typed),
                            build_student_key(other))

    def test_number_word_after_no_still_claims_the_slot(self):
        # `no` + kata identitas tetap kata nomor (regresi).
        for key, value in (("no_induk_siswa", "NI1"), ("no_absen", "12"),
                           ("no_ujian", "N01"), ("no_peserta", "N02"),
                           ("no_nis", "S1"), ("no_siswa", "S2"),
                           ("no", "7")):
            with self.subTest(key=key):
                out = _map({"nama": "Andi", "kelas": "9A", key: value})
                self.assertEqual(out.get("exam_number"), value, out)

    def test_nomor_ujian_wins_over_no_key_of_equal_specificity(self):
        # `nomor_ujian` menyebut slot secara langsung; `no` hanya
        # singkatan umum yang bisa menempel pada kata apa saja
        # (`no_hp`, `no_hp` saja sudah cukup untuk memicu bug ini).
        out = _map({"no_hp": "0812", "nomor_ujian": "N01", "nama": "Andi"})
        self.assertEqual(out.get("exam_number"), "N01", out)

    def test_phone_key_is_logged_as_unclaimed(self):
        records = []

        class _Catch(logging.Handler):
            def emit(self, record):
                records.append(record.getMessage())

        logger = logging.getLogger(LOGGER)
        handler = _Catch()
        logger.addHandler(handler)
        previous = logger.level
        logger.setLevel(logging.INFO)
        try:
            _map({"nama": "Andi", "nomor_ujian": "01", "kelas": "9A",
                  "no_hp": "0812"})
        finally:
            logger.removeHandler(handler)
            logger.setLevel(previous)
        joined = " ".join(records)
        self.assertIn("no_hp", joined,
                      "kunci yang tidak diklaim slot mana pun harus di-log")


# ---------------------------------------------------------------------------
# M3 — `nama_kelas` mengisi kelas DAN nama
# ---------------------------------------------------------------------------


class OneKeyMayFillMoreThanOneSlotTest(unittest.TestCase):
    def test_nama_kelas_fills_the_class_column(self):
        out = _map({"nama": "Andi", "nama_kelas": "9A"})
        self.assertEqual(out.get("student_name"), "Andi", out)
        self.assertEqual(out.get("student_class"), "9A", out)

    def test_english_class_name_still_fills_the_class_column(self):
        out = _map({"nama": "Andi", "class_name": "9A"})
        self.assertEqual(out.get("student_name"), "Andi", out)
        self.assertEqual(out.get("student_class"), "9A", out)

    def test_kelas_siswa_still_fills_class_and_not_name(self):
        # `siswa` adalah tier-2 nama di posisi 1; nilainya sudah diklaim
        # kelas, jadi `assigned` yang mencegah pemakaian ulang.
        out = _map({"nama": "Andi", "kelas_siswa": "9A"})
        self.assertEqual(out.get("student_name"), "Andi", out)
        self.assertEqual(out.get("student_class"), "9A", out)

    def test_class_column_survives_in_the_attempt_key(self):
        a = build_attempt_key(TOKEN, {"nama": "Andi", "nama_kelas": "9A"})
        b = build_attempt_key(TOKEN, {"nama": "Andi", "nama_kelas": "8B"})
        self.assertNotEqual(
            a, b,
            "komponen kelas hilang dari attempt key: dua siswa nama sama "
            "di kelas berbeda bentrok pada satu perangkat",
        )

    def test_same_named_students_in_different_classes_get_different_keys(self):
        a = _map({"nama": "Andi", "nama_kelas": "9A"})
        b = _map({"nama": "Andi", "nama_kelas": "8B"})
        self.assertNotEqual(
            build_attempt_key(TOKEN, a), build_attempt_key(TOKEN, b))


# ---------------------------------------------------------------------------
# M4 — kandidat per slot, tidak ada kunci yang hilang diam-diam
# ---------------------------------------------------------------------------


class UniqueIdentifierIsNotDestroyedTest(unittest.TestCase):
    def test_student_id_is_either_used_or_logged(self):
        data = {"student_id": "S001", "nama": "Andi", "kelas": "9A"}
        out = _map(data)
        if "S001" in out.values():
            # Kalau dipakai, harus masuk ke slot yang masuk akal.
            self.assertIn("S001", (out.get("exam_number"), out.get("student_name")))
        else:
            records = []

            class _Catch(logging.Handler):
                def emit(self, record):
                    records.append(record.getMessage())

            logger = logging.getLogger(LOGGER)
            handler = _Catch()
            logger.addHandler(handler)
            previous = logger.level
            logger.setLevel(logging.INFO)
            try:
                _map(data)
            finally:
                logger.removeHandler(handler)
                logger.setLevel(previous)
            self.assertIn(
                "student_id", " ".join(records),
                "nilai unik dibuang tanpa jejak di log",
            )

    def test_the_identifiers_of_a_real_config_all_survive(self):
        out = _map({"student_id": "S001", "nama": "Andi", "kelas": "9A"})
        self.assertEqual(out.get("student_name"), "Andi", out)
        self.assertEqual(out.get("student_class"), "9A", out)

    def test_tier2_and_whole_key_words_do_not_steal_from_tier1(self):
        # Lebih banyak kunci ikut bermain setelah pemindaian SELURUH kata
        # masuk ronde 5: `kode_siswa`, `id_siswa`, `exam_id`, `nilai_ujian`
        # semuanya punya hak klaim. Yang menyebut slot secara langsung
        # harus tetap menang, dan tier-2 tidak boleh menggeser tier-1.
        out = _map({
            "kode_siswa": "S9", "id_siswa": "S8", "exam_id": "E1",
            "nilai_ujian": "95",
            "nama": "Andi", "nomor_ujian": "N01", "kelas": "9A",
        })
        self.assertEqual(out, {
            "student_name": "Andi", "exam_number": "N01",
            "student_class": "9A",
        }, out)

    def test_two_tier1_number_keys_are_broken_deterministically(self):
        # `nomor_kursi` dan `nomor_ujian` sama-sama tier-1 di posisi 0:
        # keduanya sah, dan tidak ada yang lebih spesifik. Perebutnya
        # adalah kunci terpendek lalu lexicografis — total, jadi dua
        # siswa dengan config urutan key berbeda tidak pernah dapat kunci
        # berbeda. Go memecah dengan urutan yang sama.
        data = {"nama": "Andi", "nomor_kursi": "K7", "nomor_ujian": "N01"}
        self.assertEqual(build_student_key(data), "k7")
        self.assertEqual(
            build_student_key(dict(reversed(list(data.items())))), "k7")

    def test_resolution_is_identical_under_dict_reversal(self):
        data = {
            "student_id": "S001", "nama": "Andi", "nama_kelas": "9A",
            "kode_ujian": "U-7", "no_hp": "0812", "alamat": "Jl. Mawar",
        }
        forward = _map(data)
        for perm in _permutations(dict(data))[:40]:
            self.assertEqual(_map(perm), forward, perm)

    def test_every_permutation_of_a_two_slot_key_agrees(self):
        data = {"nama": "Andi", "nama_kelas": "9A", "nomor_ujian": "N01"}
        first = _map(dict(data))
        for perm in _permutations(dict(data)):
            self.assertEqual(_map(perm), first, perm)


# ---------------------------------------------------------------------------
# M5 — nilai blank tidak boleh mengklaim slot
# ---------------------------------------------------------------------------


class BlankValueNeverClaimsASlotTest(unittest.TestCase):
    """Tiga kasus yang dihitung Go != Python sebelum fix."""

    # (nama kasus, identity_data, kunci Go)
    GO_AGREEMENT = [
        ("student_name kosong string",
         {"student_name": "", "nama": "Andi", "kelas": "9A"}, "andi"),
        ("student_name whitespace",
         {"student_name": "   ", "nama": "Andi", "kelas": "9A"}, "andi"),
        ("ketiga slot kanonik kosong",
         {"student_name": "", "exam_number": "", "student_class": "",
          "nama": "Andi"}, "andi"),
        ("ketiga slot kanonik whitespace",
         {"student_name": "   ", "exam_number": "  ", "student_class": " ",
          "nama": "Andi"}, "andi"),
        ("nilai custom kosong",
         {"nama": "", "nomor_ujian": "N01", "kelas": "9A"}, "n01"),
        ("semua custom kosong",
         {"nama": "  ", "kelas": ""}, ""),
    ]

    def test_blank_values_produce_the_go_key(self):
        for label, data, want in self.GO_AGREEMENT:
            with self.subTest(case=label):
                self.assertEqual(build_student_key(data), want)

    def test_blank_canonical_key_is_never_returned(self):
        out = _map({"student_name": "   ", "nama": "Andi", "kelas": "9A"})
        self.assertEqual(out.get("student_name"), "Andi", out)

    def test_blank_custom_key_never_lands_in_a_slot(self):
        out = _map({"nama": "Andi", "kelas": "9A", "rombel": "  "})
        self.assertEqual(out.get("student_class"), "9A", out)

    def test_whitespace_only_class_does_not_become_the_student_key(self):
        self.assertEqual(
            build_student_key({"nama": "Andi", "kelas": "  ",
                               "nomor_ujian": "N01"}),
            "n01",
        )


# ---------------------------------------------------------------------------
# Regresi — kasus lama yang sudah benar
# ---------------------------------------------------------------------------


class ExistingGoodCasesMustNotRegressTest(unittest.TestCase):
    GOOD = [
        ({"student_name": "Budi", "exam_number": "N01", "student_class": "9A"},
         {"student_name": "Budi", "exam_number": "N01",
          "student_class": "9A"}),
        ({"nama": "Budi", "nomor_ujian": "N01", "kelas": "9A"},
         {"student_name": "Budi", "exam_number": "N01",
          "student_class": "9A"}),
        ({"nama_peserta": "Budi", "nomor_peserta": "N01", "kelas_siswa": "9A"},
         {"student_name": "Budi", "exam_number": "N01",
          "student_class": "9A"}),
        ({"kelasSiswa": "9A", "namaSiswa": "Budi", "nomorUjian": "N01"},
         {"student_name": "Budi", "exam_number": "N01",
          "student_class": "9A"}),
        ({"studentClass": "9A", "studentName": "Budi", "examNumber": "N01"},
         {"student_name": "Budi", "exam_number": "N01",
          "student_class": "9A"}),
        ({"nama": "Andi", "nomor_ujian": "N01", "kelas": "9A",
          "tanggal_lahir": "2010-05-05", "alamat": "Jl. Mawar 1",
          "agama": "Islam", "nilai": "95"},
         {"student_name": "Andi", "exam_number": "N01",
          "student_class": "9A"}),
        ({"id_kelas": "9A", "nama": "Andi"},
         {"student_name": "Andi", "student_class": "9A"}),
        ({"nama": "Andi", "kode_ujian": "U-7"},
         {"student_name": "Andi", "exam_number": "U-7"}),
        ({"kode_ujian": "U-7", "nomor_ujian": "N01", "nama": "Andi"},
         {"student_name": "Andi", "exam_number": "N01"}),
        ({"nama": "Budi", "nim": "Budi", "kelas": "9A"},
         {"exam_number": "Budi", "student_class": "9A"}),
        ({"alamat": "Jl. Mawar 1", "kode_pos": "12345"}, {}),
        ({"nama": "Budi", "tanggal_lahir": "2010-05-05"},
         {"student_name": "Budi"}),
        ({"nama": "   ", "kelas": "9A"}, {"student_class": "9A"}),
        ({"Student_Name": "A", "student_name": "B"},
         {"student_name": "B"}),
        ({"STUDENT_NAME": "A", "Student_Name": "B", "student_name": "C"},
         {"student_name": "C"}),
        ({"nis": "12345", "nama": "Andi"},
         {"student_name": "Andi", "exam_number": "12345"}),
        ({"nisn": "12345", "nama": "Andi"},
         {"student_name": "Andi", "exam_number": "12345"}),
        ({"kelompok": "9A", "nama": "Andi"},
         {"student_name": "Andi", "student_class": "9A"}),
        ({"rombel": "9B"}, {"student_class": "9B"}),
        ({"no_peserta": "N02"}, {"exam_number": "N02"}),
        ({"nama_lengkap": "Andi Pratama"},
         {"student_name": "Andi Pratama"}),
        ({"Nama": "Budi", "Kelas": "9A"},
         {"student_name": "Budi", "student_class": "9A"}),
        ({"nama_ujian": "Andi"}, {"student_name": "Andi"}),
        ({"exam": "E1", "nama": "Andi"},
         {"student_name": "Andi", "exam_number": "E1"}),
    ]

    def test_good_table(self):
        for data, want in self.GOOD:
            with self.subTest(data=data):
                self.assertEqual(_map(data), want)

    def test_unknown_keys_still_claim_nothing(self):
        for key in ("gelombang", "myexam", "sekolah", "field_0", "field_1",
                    "agama", "alamat", "nilai", "email", "angkatan",
                    "jurusan", "kode_pos", "exam_date", "waktu_mulai",
                    "jam_ujian", "kode_kelas"):
            with self.subTest(key=key):
                self.assertEqual(_map({key: "X1", "nama": "Andi",
                                       "kelas": "9A"}),
                                 {"student_name": "Andi",
                                  "student_class": "9A"})


# ---------------------------------------------------------------------------
# ---------------------------------------------------------------------------
# Paritas dengan Go — dijaga oleh kode Go yang BENAR-BENAR ADA
# ---------------------------------------------------------------------------
#
# PENTING caraivismenya: paritas diuji terhadap payload yang benar-benar
# dibaca server, yaitu `identity_data` hasil submit. Go menghitung kuncinya
# dari map itu (`repeat_grant.go` -> `helpers.StudentKeyFromIdentityData`),
# jadi yang dibandingkan adalah:
#
#     client: build_student_key(apa yang siswa ketik)
#     server: StudentKeyFromIdentityData(payload yang diterima)
#
# Client mengirim `nama`/`nomor_ujian`/`kelas`, bukan `student_name`/
# `exam_number`/`student_class`. Itulah sebabnya `identity_data_with_canonical`
# tidak boleh tetap jadi kode mati: begitu payload membawa kunci kanonik,
# lapisan kanonik Go langsung menang sebelum ia sampai ke tebakan kata, dan
# kedua sisi mendapat hasilnya yang sama tanpa perlu Go ikut diubah.
#
# Paritas terhadap map MENTAH tidak bisa dipakai sebagai bukti kebenaran:
# sebelum fix, client dan Go sama-sama mengembalikan nomor telepon sebagai
# nomor ujian, jadi "cocok" justru berarti dua-duanya salah. Karena itu
# tabel di bawah menyimpan nilai yang BENAR, dan kecocokan dengan Go
# diuji lewat harness yang menjalankan `student_key.go` asli.
PARITY_TABLE = [
    ({"nama": "Andi", "nomor_ujian": "N01", "kelas": "9A"}, "n01"),
    ({"nama": "Andi", "kelas": "9A"}, "andi"),
    ({"exam_number": "N01", "student_name": "Andi"}, "n01"),
    ({"nama_lengkap": "Andi Pratama"}, "andi pratama"),
    ({"no_peserta": "N02"}, "n02"),
    ({"rombel": "9B"}, "9b"),
    ({"id_kelas": "9A"}, "9a"),
    ({"nama": "Andi", "id_kelas": "9A"}, "andi"),
    ({"nama": "Andi", "kode_ujian": "U-7"}, "u-7"),
    ({"nama": "Andi", "ujian": "U-8"}, "u-8"),
    ({"nama": "Andi", "nim": "12345"}, "12345"),
    ({"nama": "Andi", "tanggal_lahir": "2010-05-05"}, "andi"),
    ({"nama": "Andi", "exam_date": "2026-10-02"}, "andi"),
    ({"nama": "Andi", "jam_ujian": "09:00"}, "andi"),
    ({"alamat": "Jl. Mawar 1"}, ""),
    ({"nilai": "95"}, ""),
    ({"email": "a@b.c"}, ""),
    ({"nama": "Andi", "kode_pos": "12345"}, "andi"),
    ({"nama": "Andi", "jurusan": "IPA"}, "andi"),
    ({"nama": "Andi", "angkatan": "2026"}, "andi"),
    ({"nama": "Andi", "agama": "Islam"}, "andi"),
    ({"kode_ujian": "U-7", "nomor_ujian": "N01", "nama": "Andi"}, "n01"),
    ({"studentName": "Andi", "studentClass": "9A", "examNumber": "N03"},
     "n03"),
    ({"namaSiswa": "Andi", "nomorUjian": "N04", "kelasSiswa": "9A"}, "n04"),
    ({"nama": "   ", "kelas": "9A"}, "9a"),
    ({"alamat": "Jl. Mawar", "kode_pos": "12345"}, ""),
    ({"exam": "E1", "nama": "Andi"}, "e1"),
    ({"nama": "   "}, ""),
    ({"nomor_ujian": "N01", "kode_ujian": "U-7", "ujian": "U-8",
      "nama": "Andi", "kelas": "9A", "id_kelas": "9B", "rombel": "9C"},
     "n01"),
    ({"student_name": "Siti", "nama": "Andi", "exam_number": "N09",
      "nomor_ujian": "N01"}, "n09"),
    ({"Student_Name": "A", "student_name": "B"}, "b"),
    ({"STUDENT_NAME": "A", "Student_Name": "B", "student_name": "C"}, "c"),
    ({"nama_peserta": "Budi", "nomor_peserta": "N01", "kelas_siswa": "9A"},
     "n01"),
    ({"nis": "12345", "nama": "Andi"}, "12345"),
    ({"nisn": "12345", "nama": "Andi"}, "12345"),
    ({"kelompok": "9A", "nama": "Andi"}, "andi"),
    ({"no_ujian": "N01"}, "n01"),
    ({"exam_number": "  N01  ", "student_name": "Andi"}, "n01"),
    ({"exam_number": "   ", "student_name": "Andi"}, "andi"),
    ({"kode_siswa": "S9", "nama": "Andi"}, "andi"),
    ({"id_siswa": "S8", "nama": "Andi"}, "andi"),
    ({"exam_id": "E1", "nama": "Andi"}, "e1"),
    ({"nomor_kursi": "K7", "nama": "Andi"}, "k7"),
    ({"nilai_ujian": "95", "nama": "Andi"}, "95"),
    ({"Nama": "Budi", "Kelas": "9A"}, "budi"),
    ({"KELAS": "9A", "nomor_ujian": "N01"}, "n01"),
    ({"kode_ujian": "U-7", "nama": "Andi"}, "u-7"),
    ({"ujian": "U-7"}, "u-7"),
    ({"nama_ujian": "Andi"}, "andi"),
    ({"no_ujian": "N01", "nomor_ujian": "N01"}, "n01"),
    ({"nomor_induk": "NI1", "nama": "Andi"}, "ni1"),
    ({"nomor_peserta": "NP", "nama": "Andi"}, "np"),
    ({"nomor": "1", "nama": "Andi"}, "1"),
    ({"number": "1", "nama": "Andi"}, "1"),
    ({"nip": "1", "nama": "Andi"}, "1"),
    ({"nim": "1", "nama": "Andi"}, "1"),
    ({"kelompok": "K1", "nama": "Andi"}, "andi"),
    ({"rombel": "R1", "nama": "Andi"}, "andi"),
    ({"kelas": "9A"}, "9a"),
    ({"nama": "Andi"}, "andi"),
    ({"nama_peserta": "Budi"}, "budi"),
    ({"examNumber": "N03"}, "n03"),
    ({"studentClass": "9A"}, "9a"),
    ({"kelas_siswa": "9A", "nama": "Andi"}, "andi"),
    ({"nama_kelas": "9A"}, "9a"),
    ({"no_kelas": "K1"}, "k1"),
    ({"kelas_nama": "K", "nama": "Andi"}, "andi"),
    ({"kode_ujian": "U-7", "no_ujian": "N01"}, "n01"),
    ({"myexam": "X", "nama": "Andi"}, "andi"),
    ({"gelombang": "B1", "nama": "Andi", "kelas": "9A"}, "andi"),
    ({"field_0": "X", "nama": "Andi"}, "andi"),
    ({"nomor_induk_siswa": "NI1", "nama": "Andi", "kelas": "9A"}, "ni1"),
    ({"no_induk": "NI1", "nama": "Andi", "kelas": "9A"}, "ni1"),
    ({"nomor_hp": "0812", "nama": "Andi"}, "0812"),
    ({"kelas_siswa": "9A"}, "9a"),
    ({"namaSiswa": "Andi", "kelas": "9A"}, "andi"),
    ({"nomor_peserta": "N01"}, "n01"),
    ({"peserta": "Andi"}, "andi"),
    ({"ujian_peserta": "U1"}, "u1"),
    ({"nama_orang_tua": "Budi", "nama": "Andi"}, "andi"),
    ({"alamat_orang_tua": "Jl. Mawar", "nama": "Andi"}, "andi"),
    ({"nama_sekolah": "SMP 1", "kelas": "9A"}, "smp 1"),
    ({"kode_kelas": "9A", "nama": "Andi"}, "andi"),
    ({"nama_siswa": "Andi", "nomor_siswa": "S1"}, "s1"),
    ({"nama_siswa": "Andi", "nomor_siswa": "S1", "kelas_siswa": "9A"},
     "s1"),
    ({"nama_siswa": "Andi", "nis_siswa": "S1", "kelas_siswa": "9A"}, "s1"),
    ({"nama_siswa": "Andi", "no_siswa": "S1", "kelas_siswa": "9A"}, "s1"),
    ({"nama_siswa": "Andi", "no_nis": "S1", "kelas_siswa": "9A"}, "s1"),
    ({"nama_siswa": "Andi", "no_induk_siswa": "S1", "kelas_siswa": "9A"},
     "s1"),
    ({"nama_siswa": "Andi", "no_absen": "S1", "kelas_siswa": "9A"}, "s1"),
    ({"nama_siswa": "Andi", "no_ujian": "S1", "kelas_siswa": "9A"}, "s1"),
    ({"nama_siswa": "Andi", "no_peserta": "S1", "kelas_siswa": "9A"},
     "s1"),
    ({"nama_siswa": "Andi", "no_kelas": "9A"}, "9a"),
    ({"nama_siswa": "Andi", "kelas_siswa": "9A", "nomor_induk_siswa": "NI1"},
     "ni1"),
    ({"nama": "Andi", "nomor_ujian": "N01", "kelas": "9A", "no": "7"},
     "n01"),
    ({"nama": "Andi", "nomor_ujian": "01", "kelas": "9A", "no_hp": "0812",
      "alamat": "Jl"}, "01"),
    ({"nama": "Andi", "nomor_ujian": "01", "kelas": "9A", "no_absen": "12",
      "no_hp": "0812"}, "01"),
    ({"nama": "Andi", "nomor_ujian": "01", "kelas": "9A", "no_absen": "12",
      "no_hp": "0812", "no_telp": "0813"}, "01"),
    ({"nama": "Andi", "nomor_ujian": "01", "kelas": "9A",
      "no_induk_siswa": "NI1", "no_hp": "0812"}, "01"),
    ({"nomor_ujian": "01", "kelas": "9A", "no_hp": "0812"}, "01"),
    ({"nomor_ujian": "01", "nama_kelas": "9A", "no_hp": "0812"}, "01"),
    ({"nomor_ujian": "01", "nama_kelas": "9A"}, "01"),
    ({"nomor_ujian": "01", "class_name": "9A"}, "01"),
    ({"nomor_ujian": "01", "rombel": "9A", "nama": "Andi"}, "01"),
    ({"nama": "Andi", "no_hp": "0812", "nomor_ujian": "01"}, "01"),
]

# Kasus di mana client sudah benar dan `student_key.go` BELUM. Go tidak
# boleh diedit ronde ini, jadi daftar ini adalah tiket yang terbuka, bukan
# test yang dilewati: begitu Go ikut diperbaiki, harness di bawah
# melaporkan Go tidak menyimpang lagi dan kasusnya bisa naik ke
# `PARITY_TABLE`.
#
# Catatan soal risiko: `repeat_grant.go` di sisi pengawas membaca
# `identity_data` yang SUDAH dipetakan ke kunci kanonik untuk baris yang
# dikirim client versi ini, jadi halaman pengawas tidak salah orang.
# Yang tersisa adalah data lama / payload yang tidak lewat dialog.
KNOWN_GO_DIVERGENCE = [
    # Client: tidak ada slot yang bisa diisi (nama saja + telepon) ->
    # kunci identitas kosong. Go masih menebak '0812' dari `no_hp`.
    ({"nama": "Andi", "no_hp": "0812"}, "0812"),
    ({"nama": "Andi", "kelas": "9A", "no_hp": "0812"}, "0812"),
    ({"nama": "Andi", "kelas": "9A", "no_telp": "0812"}, "0812"),
    ({"nama": "Andi", "kelas": "9A", "no_wa": "0812"}, "0812"),
    ({"nama": "Andi", "kelas": "9A", "no_telpon": "0812"}, "0812"),
    ({"nama": "Andi", "kelas": "9A", "no_hp_siswa": "0812"}, "0812"),
    ({"nama": "Andi", "kelas": "9A", "no_telp_siswa": "0812"}, "0812"),
    ({"nama": "Andi", "kelas": "9A", "no_hp_ortu": "0812"}, "0812"),
    ({"nama": "Andi", "nomor_ujian": "01", "kelas": "9A", "no_hp": "0812"},
     "0812"),
]


def _go_student_keys(cases):
    """Jalankan `student_key.go` apa adanya terhadap daftar kasus.

    Disalin ke direktori sementara sebagai `package main` + driver kecil,
    lalu `go run`. Tidak ada file repo yang ditulis dan tidak ada salinan
    yang bisa basi: sumbernya dibaca dari webui saat test berjalan, jadi
    tabel ini selalu bicara tentang kode Go yang benar-benar ada.
    """
    if shutil.which("go") is None or not GO_SOURCE.exists():
        return None
    src = GO_SOURCE.read_text(encoding="utf-8")
    src = src.replace("package helpers", "package main", 1)
    src = src.replace('import (\n\t"sort"',
                      'import (\n\t"encoding/json"\n\t"os"\n\t"sort"', 1)
    src += (
        "\nfunc main() {\n"
        "\tvar raw []map[string]interface{}\n"
        "\tif err := json.NewDecoder(os.Stdin).Decode(&raw); err != nil {\n"
        "\t\tpanic(err)\n\t}\n"
        "\tout := make([]string, 0, len(raw))\n"
        "\tfor _, m := range raw {\n"
        "\t\tout = append(out, StudentKeyFromIdentityData(m, \"\", \"\", \"\"))\n"
        "\t}\n"
        "\t_ = json.NewEncoder(os.Stdout).Encode(out)\n"
        "}\n"
    )
    with tempfile.TemporaryDirectory() as tmp:
        (Path(tmp) / "main.go").write_text(src, encoding="utf-8")
        (Path(tmp) / "go.mod").write_text(
            "module goparity\n\ngo 1.24\n", encoding="utf-8")
        env = dict(os.environ, GOCACHE=os.path.join(tmp, "cache"),
                   GOFLAGS="-mod=mod", HOME=tmp)
        proc = subprocess.run(
            ["go", "run", "."], cwd=tmp, input=json.dumps(cases),
            capture_output=True, text=True, env=env, timeout=300,
        )
        if proc.returncode != 0:
            raise AssertionError("go run gagal:\n" + proc.stderr)
    return json.loads(proc.stdout)


class GoParityTest(unittest.TestCase):
    """Kunci client harus sama dengan kunci Go untuk payload yang sama."""

    def test_parity_table_holds_the_right_keys(self):
        wrong = [
            (data, build_student_key(data), want)
            for data, want in PARITY_TABLE
            if build_student_key(data) != want
        ]
        self.assertEqual(
            wrong, [],
            f"{len(wrong)} dari {len(PARITY_TABLE)} kunci client salah",
        )

    def test_wire_parity_with_go_is_100_percent(self):
        """Client vs Go, dihitung atas payload yang benar-benar diterima.

        Ini yang menjaga izin mengulang dari pengawas tetap berlaku: begitu
        kedua sisi memakai kunci yang berbeda, dan gejalanya hanya "siswa
        ditolak terus" tanpa sebab yang bisa ditunjuk.
        """
        typed = [c for c, _ in PARITY_TABLE]
        got = _go_student_keys(
            [identity_data_with_canonical(c) for c in typed])
        if got is None:
            self.skipTest("go / student_key.go tidak tersedia")
        mismatched = [
            (data, build_student_key(data), g)
            for (data, _want), g in zip(PARITY_TABLE, got)
            if build_student_key(data) != g
        ]
        self.assertEqual(
            mismatched, [],
            f"{len(mismatched)} dari {len(PARITY_TABLE)} tidak cocok dengan "
            "student_key.go pada payload yang dikirim",
        )

    def test_wire_payload_carries_the_canonical_keys(self):
        data = {"nama": "Andi", "nomor_ujian": "01", "kelas": "9A",
                "no_hp": "0812"}
        wire = identity_data_with_canonical(data)
        self.assertEqual(wire.get("exam_number"), "01", wire)
        got = _go_student_keys([wire])
        if got is None:
            self.skipTest("go / student_key.go tidak tersedia")
        self.assertEqual(got[0], build_student_key(data))


class KnownGoDivergenceTest(unittest.TestCase):
    """Tiket terbuka: `student_key.go` masih menebak nomor telepon."""

    def test_client_no_longer_returns_the_phone_number(self):
        for data, go_still_says in KNOWN_GO_DIVERGENCE:
            with self.subTest(data=data):
                self.assertNotEqual(build_student_key(data), go_still_says)

    def test_go_still_reports_the_bug(self):
        # Kalau ini gagal, Go sudah ikut diperbaiki: pindahkan kasusnya
        # dari KNOWN_GO_DIVERGENCE ke PARITY_TABLE.
        typed = [c for c, _ in KNOWN_GO_DIVERGENCE]
        got = _go_student_keys(typed)
        if got is None:
            self.skipTest("go / student_key.go tidak tersedia")
        wrong = [
            (data, want, g)
            for (data, want), g in zip(KNOWN_GO_DIVERGENCE, got)
            if want != g
        ]
        self.assertEqual(wrong, [], "student_key.go sudah berubah")


if __name__ == "__main__":  # pragma: no cover
    unittest.main()
