"""Status baterai desktop — ambang dan parsing platform.

Ambang harus sama dengan Android (`DeviceStatusPolicy.kt`): <15% kritis,
15–29% rendah, >=30% normal.
"""

from __future__ import annotations

import os
import tempfile
import unittest

from examvan.device_status import (
    BATTERY_CRITICAL,
    BATTERY_LOW,
    BATTERY_NORMAL,
    BatteryStatus,
    _read_linux_battery,
    battery_category,
    battery_from_windows_status,
    battery_text,
)


class BatteryCategoryTest(unittest.TestCase):
    def test_thresholds_match_android(self):
        self.assertEqual(battery_category(14), BATTERY_CRITICAL)
        self.assertEqual(battery_category(15), BATTERY_LOW)
        self.assertEqual(battery_category(29), BATTERY_LOW)
        self.assertEqual(battery_category(30), BATTERY_NORMAL)
        self.assertEqual(battery_category(100), BATTERY_NORMAL)

    def test_out_of_range_values_are_clamped(self):
        self.assertEqual(battery_category(-5), BATTERY_CRITICAL)
        self.assertEqual(battery_category(150), BATTERY_NORMAL)

    def test_text_is_compact_and_mentions_charging(self):
        self.assertEqual(battery_text(BatteryStatus(45, False)), "Baterai 45%")
        self.assertEqual(
            battery_text(BatteryStatus(45, True)),
            "Baterai 45% (mengisi)",
        )


class WindowsBatteryParsingTest(unittest.TestCase):
    def test_no_battery_is_reported_as_none(self):
        self.assertIsNone(battery_from_windows_status(255, 128, 255))
        self.assertIsNone(battery_from_windows_status(255, 128, 100))
        self.assertIsNone(battery_from_windows_status(255, 128, 0))

    def test_unknown_percent_is_none(self):
        self.assertIsNone(battery_from_windows_status(255, 0, 255))

    def test_ac_online_counts_as_charging(self):
        status = battery_from_windows_status(1, 0, 80)
        self.assertIsNotNone(status)
        self.assertTrue(status.charging)

    def test_charging_flag_counts_even_when_ac_status_is_unknown(self):
        status = battery_from_windows_status(255, 8, 80)
        self.assertIsNotNone(status)
        self.assertTrue(status.charging)

    def test_percent_is_preserved(self):
        status = battery_from_windows_status(0, 0, 12)
        self.assertEqual(status, BatteryStatus(12, False))


class LinuxBatteryParsingTest(unittest.TestCase):
    def _write_battery(self, root, name, percent, state):
        base = os.path.join(root, name)
        os.makedirs(base)
        with open(os.path.join(base, "capacity"), "w", encoding="ascii") as f:
            f.write(str(percent))
        with open(os.path.join(base, "status"), "w", encoding="ascii") as f:
            f.write(state)

    def test_reads_capacity_and_charging_state(self):
        with tempfile.TemporaryDirectory() as root:
            self._write_battery(root, "BAT0", 42, "Charging")
            self.assertEqual(
                _read_linux_battery(root),
                BatteryStatus(42, True),
            )

    def test_full_counts_as_charging(self):
        with tempfile.TemporaryDirectory() as root:
            self._write_battery(root, "BAT0", 100, "Full")
            self.assertEqual(
                _read_linux_battery(root),
                BatteryStatus(100, True),
            )

    def test_missing_directory_is_none(self):
        self.assertIsNone(_read_linux_battery("/nonexistent/examvan"))

    def test_no_battery_entry_is_none(self):
        with tempfile.TemporaryDirectory() as root:
            os.makedirs(os.path.join(root, "AC"))
            self.assertIsNone(_read_linux_battery(root))


if __name__ == "__main__":
    unittest.main()
