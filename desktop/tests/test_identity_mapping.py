"""Pemetaan identitas tidak boleh MENEBAK, dan device label harus stabil.

Dua temuan review_windows_2026-09-30.md Bagian 4 (N4) dan Bagian 5 (N5).

N4 — field yang tidak dikenali dipaksakan ke slot standar
-------------------------------------------------------
Setelah pencocokan keyword gagal, `map_identity_to_standard` dulu melakukan:

    remaining = [val for _, val in items if val not in assigned]
    for std_key in ("student_name", "exam_number", "student_class"):
        if std_key not in result and remaining:
            result[std_key] = remaining.pop(0)

Nilai yang TIDAK cocok keyword apa pun dipop ke slot identitas berdasar urutan
dict. Terbukti:

    {'nama': 'Budi', 'kelas': '9A', 'tanggal_lahir': '2010-05-05'}
      -> {'student_name': 'Budi', 'exam_number': '2010-05-05', ...}

Tanggal lahir tercatat sebagai nomor ujian. Tidak ada error, tidak ada
warning, tidak ada log — hanya rekap hasil yang salah. Server sudah
memvalidasi terhadap `expectedFields` (`exams.go:1074-1104`) dan mengembalikan
400 eksplisit, jadi menebak di client justru menghilangkan satu-satunya
sinyal yang berguna itu.

N5 — device label harus sama sepanjang satu proses
--------------------------------------------------
Label `DESKTOP:<hash>` adalah kunci di empat tempat: `X-Device-Id` untuk
download PDF, `mac_address` untuk approval, `mac_address` untuk submit, dan
presence/heartbeat. Kalau label berubah di tengah sesi, gate PDF tidak match
baris approval -> PDF tidak terunduh, dan approval lama tidak pernah
ter-revoke.

`_on_connect` (exam_viewer) sengaja mengirim `device_id=get_device_label()`
komentar di `api.py:155-160` menjelaskan akibatnya. Yang belum dijamin:
nilai itu DIAMBIL ULANG setiap kali dipanggil, jadi tidak ada yang
menjamin keempat pemanggilan itu melihat nilai yang sama.
"""

from __future__ import annotations

import unittest
from unittest import mock

from examvan.utils import (
    get_device_label,
    map_identity_to_standard,
    reset_device_label_cache,
)


# ---------------------------------------------------------------------------
# N4 — tidak boleh menebak
# ---------------------------------------------------------------------------


class IdentityMappingTest(unittest.TestCase):
    def test_standard_keys_map_directly(self):
        out = map_identity_to_standard(
            {"student_name": "Budi", "exam_number": "N01", "student_class": "9A"}
        )
        self.assertEqual(out["student_name"], "Budi")
        self.assertEqual(out["exam_number"], "N01")
        self.assertEqual(out["student_class"], "9A")

    def test_custom_keys_map_by_keyword(self):
        out = map_identity_to_standard(
            {"nama": "Budi", "nomor_ujian": "N01", "kelas": "9A"}
        )
        self.assertEqual(out["student_name"], "Budi")
        self.assertEqual(out["exam_number"], "N01")
        self.assertEqual(out["student_class"], "9A")

    def test_date_of_birth_is_never_recorded_as_exam_number(self):
        # Kasus yang membuat N4: tanggal lahir tidak cocok keyword
        # mana pun, dan dulu jatuh ke exam_number.
        out = map_identity_to_standard(
            {"nama": "Budi", "kelas": "9A", "tanggal_lahir": "2010-05-05"}
        )
        self.assertNotEqual(out.get("exam_number"), "2010-05-05")

    def test_unmapped_fields_do_not_fill_standard_slots(self):
        out = map_identity_to_standard(
            {"nama": "Budi", "kelas": "9A", "gelombang": "B1", "sekolah": "SMP 1"}
        )
        self.assertEqual(out["student_name"], "Budi")
        self.assertEqual(out["student_class"], "9A")
        # Tidak ada slot yang boleh diisi dari field tak dikenal.
        self.assertNotIn("gelombang", out.values())
        self.assertNotIn("SMP 1", out.values())

    def test_missing_slot_is_left_empty_not_filled(self):
        # Server yang menolak, dengan pesan yang bisa dibaca guru. Lebih baik
        # 400 "Identitas 'Nomor Ujian' wajib diisi" daripada nomor ujian
        # yang salah.
        out = map_identity_to_standard({"nama": "Budi", "tanggal_lahir": "2010-05-05"})
        self.assertEqual(out.get("student_name"), "Budi")
        self.assertIsNone(out.get("exam_number"))

    def test_empty_values_are_dropped(self):
        out = map_identity_to_standard(
            {"nama": "Budi", "nomor_ujian": "", "kelas": "9A"}
        )
        self.assertNotIn("exam_number", out)
        self.assertEqual(out["student_class"], "9A")

    def test_no_input_gives_no_output(self):
        self.assertEqual(map_identity_to_standard({}), {})

    def test_a_value_is_never_reused_for_two_slots(self):
        out = map_identity_to_standard(
            {"nama": "Budi", "nis": "Budi", "kelas": "9A"}
        )
        self.assertEqual(out["student_name"], "Budi")
        self.assertIsNone(out.get("exam_number"))

    def test_keys_are_matched_case_insensitively(self):
        out = map_identity_to_standard({"Nama": "Budi", "Kelas": "9A"})
        self.assertEqual(out["student_name"], "Budi")
        self.assertEqual(out["student_class"], "9A")


# ---------------------------------------------------------------------------
# N5 — device label stabil
# ---------------------------------------------------------------------------


class DeviceLabelStabilityTest(unittest.TestCase):
    def setUp(self):
        reset_device_label_cache()

    def tearDown(self):
        reset_device_label_cache()

    def test_repeated_calls_return_the_same_label(self):
        # Empat call site bergantung pada ini: X-Device-Id (PDF),
        # mac_address (approval), mac_address (submit), presence.
        first = get_device_label()
        for _ in range(5):
            self.assertEqual(get_device_label(), first)

    def test_label_survives_a_changing_underlying_mac(self):
        # Menggambarkan Windows: uuid.getnode() bisa mengembalikan adapter
        # lain setelah enumerasi berubah / MAC acak aktif. Nilai yang sudah
        # dipakai tidak boleh ikut berubah di tengah sesi.
        seen = []

        def _mac():
            seen.append(1)
            return ["AA:BB:CC:DD:EE:01", "AA:BB:CC:DD:EE:02"][len(seen) - 1]

        with mock.patch("examvan.utils.get_mac_address", side_effect=_mac), \
             mock.patch("examvan.utils.socket.gethostname",
                        return_value="LAB-PC-01"):
            first = get_device_label()
            self.assertEqual(get_device_label(), first)
            self.assertEqual(get_device_label(), first)
        # Dan hanya satu MAC yang pernah dibaca.
        self.assertEqual(len(seen), 1)

    def test_label_format_is_unchanged(self):
        label = get_device_label()
        self.assertTrue(label.startswith("DESKTOP:"))
        self.assertEqual(len(label), len("DESKTOP:") + 32)

    def test_cache_can_be_reset_for_tests(self):
        with mock.patch("examvan.utils.get_mac_address",
                        return_value="AA:BB:CC:DD:EE:01"), \
             mock.patch("examvan.utils.socket.gethostname",
                        return_value="LAB-PC-01"):
            first = get_device_label()
        reset_device_label_cache()
        with mock.patch("examvan.utils.get_mac_address",
                        return_value="AA:BB:CC:DD:EE:99"), \
             mock.patch("examvan.utils.socket.gethostname",
                        return_value="LAB-PC-01"):
            self.assertNotEqual(get_device_label(), first)

    def test_windows_backend_shares_the_same_implementation(self):
        # `WindowsBackend.get_device_label` menduplikasi logika ini
        # (windows_backend.py:801-806). Dua implementasi device identity
        # dalam satu repo berarti dua jawaban yang bisa berbeda.
        from examvan.security.windows_backend import WindowsBackend

        with mock.patch("examvan.utils.get_mac_address",
                        return_value="AA:BB:CC:DD:EE:01"), \
             mock.patch("examvan.utils.socket.gethostname",
                        return_value="LAB-PC-01"):
            expected = get_device_label()
        self.assertEqual(WindowsBackend().get_device_label(), expected)


if __name__ == "__main__":
    unittest.main()
