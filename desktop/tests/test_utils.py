"""Unit tests for examvan.utils — device identity consistency.

Covers the desktop↔Android consistency fix (Agustus 2026): the desktop
client must present ONE device identity (DESKTOP:<hash>) for request-approval,
PDF download (X-Device-Id) AND submit (mac_address). Previously approval/PDF
used the raw MAC while submit used DESKTOP:<hash>, so the approval row (the
PDF gate key) and the submission row (the upsert key) never matched — the
student appeared twice in monitoring and the approval was never revoked.
"""

from __future__ import annotations

import unittest

from examvan.utils import get_device_id, get_device_label, map_identity_to_standard


class DeviceLabelTest(unittest.TestCase):
    def test_label_format(self):
        label = get_device_label()
        self.assertTrue(label.startswith("DESKTOP:"))
        self.assertEqual(label, f"DESKTOP:{get_device_id()}")

    def test_stable_across_calls(self):
        self.assertEqual(get_device_label(), get_device_label())
        self.assertEqual(get_device_id(), get_device_id())


class MapIdentityToStandardTest(unittest.TestCase):
    def test_maps_custom_keys(self):
        mapped = map_identity_to_standard({
            "nama": "Budi", "nomor_ujian": "N01", "kelas": "9A",
        })
        self.assertEqual(mapped["student_name"], "Budi")
        self.assertEqual(mapped["exam_number"], "N01")
        self.assertEqual(mapped["student_class"], "9A")

    def test_standard_keys_passthrough(self):
        mapped = map_identity_to_standard({
            "student_name": "Siti", "exam_number": "S02", "student_class": "8B",
        })
        self.assertEqual(mapped, {
            "student_name": "Siti", "exam_number": "S02", "student_class": "8B",
        })

    def test_empty_input(self):
        self.assertEqual(map_identity_to_standard({}), {})


if __name__ == "__main__":
    unittest.main()
