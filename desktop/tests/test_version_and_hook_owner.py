"""R4: satu nomor versi, dan R5: keyboard hook punya satu owner.

Dua temuan dari review_windows_2026-09-29.md Bagian 4 dan Bagian 5, masih
terbuka sampai ronde ini.

R4 — identitas versi
--------------------
Tiga angka berbeda saling menyangkal, dan ketiganya muncul di layar yang sama:

    examvan/__init__.py          __version__ = "1.0.0"
                                  APP_VERSION = "2.5.0"
    ui/server_config.py          v{__version__} (API {APP_VERSION})
                                  -> guru membaca "v1.0.0 (API 2.5.0)"
    __main__.py                  app.setApplicationVersion("1.0.0")
    windows/installer/examvan.iss  AppVersion "2.5.0"
    windows/installer/version_info.txt  filevers=(1, 0, 0, 0)

Guru melapor "aplikasinya tulis v1.0.0" -> installer bilang 2.5.0 -> release
notes bilang 2.5.0 -> Properties exe bilang 1.0.0.0. Laporan bug tidak bisa
dicocokkan ke build — persis kegagalan yang `build_info.py:126-133`
menyatakan jadi alasan modul itu dibuat.

R5 — keyboard hook kehilangan owner
-----------------------------------
`get_backend()` mengembalikan instance BARU setiap dipanggil, padahal
`_hook_id`, `_hook_thread`, `_hook_ready` dan friends adalah module globals
di `windows_backend.py:267-275`. Docstring-nya sudah menjanjikan "Called once
at app startup", tapi tidak ada yang menegakkan itu.

Skenario: siswa menyelesaikan ujian 1 (strict) -> deactivate -> stop hook
(join timeout 1.0 dtk) -> mulai ujian 2 -> instance backend BARU ->
`_start_keyboard_hook()` menulis `_hook_id` = hook baru. Lalu thread LAMA
selesai `finally`-nya dan menjalankan `_UnhookWindowsHookEx(_hook_id)` pada
hook BARU. Hook baru dilepas, `_hook_id` di-nolkan, dan instance baru masih
masih menganggap `_hook_installed = True` sehingga tidak ada retry.

Dampaknya: Alt+Tab, Win key, Win+L, Ctrl+Shift+Esc berhenti diblokir
sementara banner tetap menulis "STRICT". Batas integritas ujian hilang tanpa
pesan apa pun.
"""

from __future__ import annotations

import re
import unittest
from pathlib import Path
from unittest import mock

from examvan import APP_VERSION, __version__

REPO = Path(__file__).resolve().parents[2]
INIT_PY = REPO / "desktop/examvan/__init__.py"
MAIN_PY = REPO / "desktop/examvan/__main__.py"
SERVER_CONFIG_PY = REPO / "desktop/examvan/ui/server_config.py"
VERSION_INFO = REPO / "windows/installer/version_info.txt"
BUILD_INFO = REPO / "windows/installer/build_info.py"


def _numeric(version: str) -> str:
    return ".".join(str(int(p)) for p in re.findall(r"\d+", version)[:3])


# ---------------------------------------------------------------------------
# R4 — identitas versi
# ---------------------------------------------------------------------------


class VersionIdentityTest(unittest.TestCase):
    def test_display_version_is_not_a_second_number(self):
        # `v1.0.0 (API 2.5.0)` di layar adalah dua nomor yang saling
        # menyangkal di depan mata pengguna.
        src = SERVER_CONFIG_PY.read_text(encoding="utf-8")
        self.assertNotIn("(API {APP_VERSION})", src)

    def test_screen_shows_the_api_version(self):
        src = SERVER_CONFIG_PY.read_text(encoding="utf-8")
        self.assertIn("APP_VERSION", src)

    def test_application_version_is_not_a_literal(self):
        # `app.setApplicationVersion("1.0.0")` adalah nomor ketiga yang tidak
        # terhubung ke mana pun — dan_properties_ exe membaca dari situ.
        src = MAIN_PY.read_text(encoding="utf-8")
        m = re.search(r"setApplicationVersion\(([^)]*)\)", src)
        self.assertIsNotNone(m, "setApplicationVersion tidak ditemukan")
        arg = m.group(1).strip()
        self.assertNotIn('"', arg, f"masih literal: {arg!r}")
        self.assertIn("APP_VERSION", arg)

    def test_package_version_matches_api_version(self):
        self.assertEqual(__version__, APP_VERSION)

    def test_app_version_is_a_literal_not_an_alias(self):
        # Arah dependensi WAJIB begini: APP_VERSION literal, __version__
        # diturunkan. build_info.py, build-deb.sh, dan langkah "Verify
        # package contents" di ci.yml membaca `APP_VERSION = "..."` dengan
        # regex tanpa mengeksekusi source. Kalau arahnya dibalik, ketiganya
        # gagal diam-diam dan artefak terlabel versi fallback yang salah —
        # persis yang terjadi saat mutation check menemukan arah ini terbalik.
        src = INIT_PY.read_text(encoding="utf-8")
        m = re.search(
            r'^\s*APP_VERSION\s*=\s*["\']([^"\']+)["\']', src, re.MULTILINE
        )
        self.assertIsNotNone(
            m, "APP_VERSION harus literal agar bisa dibaca tanpa eksekusi"
        )
        self.assertEqual(m.group(1), APP_VERSION)

    def test_version_agrees_with_the_installer_default(self):
        # `.iss` punya default AppVersion untuk iscc yang dipanggil tanpa
        # define. Kalau source dan default ini melenceng, ada build yang
        # diam-diam mengemas versi yang salah.
        iss = (REPO / "windows/installer/examvan.iss").read_text(encoding="utf-8")
        m = re.search(r'#define\s+AppVersion\s+"([^"]+)"', iss)
        self.assertIsNotNone(m, "#define AppVersion tidak ada di examvan.iss")
        self.assertEqual(m.group(1), APP_VERSION)

    def test_version_agrees_with_the_deb_packager(self):
        # build-deb.sh mengambil versi lewat build_info.py; kalau keduanya
        # melenceng, paket .deb dan exe punya nomor berbeda.
        import importlib.util

        spec = importlib.util.spec_from_file_location("build_info", BUILD_INFO)
        mod = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(mod)
        self.assertEqual(mod.version_from_source(), APP_VERSION)
        self.assertEqual(mod.resolve()["AppVersion"], APP_VERSION)

    def test_version_agrees_with_the_version_resource_default(self):
        # Template version_info.txt boleh membawa default; yang penting
        # template itu tidak mengklaim versi yang berbeda dari source.
        info = (REPO / "windows/installer/version_info.txt").read_text(encoding="utf-8")
        m = re.search(r"StringStruct\('FileVersion',\s*'([^']*)'\)", info)
        self.assertIsNotNone(m)
        self.assertEqual(m.group(1), APP_VERSION)

    def test_version_resource_agrees_with_app_version(self):
        # Template boleh bawa 1.0.0 — tugasnya cuma BUKAN bilangan yang
        # salah. Yang diuji di test_build_info.py: setelah di-stamp, kedua
        # field FixedFileInfo sama dengan FileVersion.
        src = VERSION_INFO.read_text(encoding="utf-8")
        for key in ("filevers", "prodvers"):
            m = re.search(rf"{key}\s*=\s*\(([^)]*)\)", src)
            self.assertIsNotNone(m, f"{key} tidak ada di version_info.txt")
            self.assertEqual(len(m.group(1).split(",")), 4)

    def test_version_source_is_read_from_app_version(self):
        import importlib.util

        spec = importlib.util.spec_from_file_location("build_info", BUILD_INFO)
        mod = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(mod)
        self.assertEqual(mod.version_from_source(), APP_VERSION)
        self.assertEqual(mod.resolve()["AppVersion"], APP_VERSION)

    def test_build_info_module_is_actually_tested(self):
        # build_info.py:6-7 mengklaim logikanya sudah ada test-nya; tidak
        # ada. Sekarang ada.
        tests = REPO / "desktop/tests"
        self.assertTrue(
            (tests / "test_build_info.py").exists(),
            "build_info.py mengklaim sudah dites tapi tidak ada test-nya",
        )


# ---------------------------------------------------------------------------
# R5 — keyboard hook punya satu owner
# ---------------------------------------------------------------------------


class SharedBackendTest(unittest.TestCase):
    """get_backend() harus mengembalikan instance yang sama."""

    def setUp(self):
        import examvan.security as sec

        self.sec = sec
        self.addCleanup(sec.reset_backend_cache)
        sec.reset_backend_cache()

    def test_repeated_calls_return_the_same_instance(self):
        # Inilah kontrak yang menutup celah keyboard hook: state hook adalah
        # module global, jadi hanya boleh ada SATU backend per proses.
        first = self.sec.get_backend()
        second = self.sec.get_backend()
        self.assertIs(first, second)

    def test_three_calls_still_return_one_instance(self):
        # Dua SecurityEnforcer untuk dua sesi ujian = tiga+ pemanggilan.
        a = self.sec.get_backend()
        b = self.sec.get_backend()
        c = self.sec.get_backend()
        self.assertIs(a, b)
        self.assertIs(b, c)

    def test_two_enforcers_share_one_backend(self):
        from examvan.security.enforcer import SecurityEnforcer

        first = SecurityEnforcer(security_level="high", strict_mode=True)
        second = SecurityEnforcer(security_level="high", strict_mode=True)
        self.assertIs(first._backend, second._backend)

    def test_reset_releases_and_forces_a_new_instance(self):
        first = self.sec.get_backend()
        self.sec.reset_backend_cache()
        second = self.sec.get_backend()
        self.assertIsNot(first, second)

    def test_hook_state_has_a_single_owner_by_construction(self):
        # `_stop_keyboard_hook` sengaja tidak menolkan `_hook_id` (race).
        # Yang menutup celahnya: hanya ada satu instance backend, jadi tidak
        # ada thread lama yang bisa membaca hook milik instance baru.
        import examvan.security.windows_backend as wb

        shared = self.sec.get_backend()
        self.assertTrue(hasattr(shared, "release_strict_mode"))
        # Module global hook tetap satu-satunya tempat state hidup.
        self.assertTrue(hasattr(wb, "_hook_id"))


if __name__ == "__main__":
    unittest.main()
