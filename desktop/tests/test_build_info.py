"""`windows/installer/build_info.py` — logikanya harus benar dan diuji.

Modul ini mengklaim di docstring-nya (`:6-7`) bahwa logikanya sudah ada
test-nya. Tidak ada. Tiga kelas bug yang tidak ketahuan karena itu:

1. `numeric_version` menyaturasi ke 255 untuk versi dengan tahun di tengah.
   `'release-2026.09'` -> `255.9.0` karena `min(n, 255)`
   menyaturasi 2026 ke 255. Jadi installer untuk branch bernama `release-2026.09`
   akan berlabel 255.9.0 — bukan 0.9.0, dan bukan 2026.9.0.

2. `FixedFileInfo` (filevers/prodvers) di `version_info.txt` tidak pernah
   di-stamp. CI hanya men-*patch* dua nilai `StringStruct` (`:34`, `:38`),
   jadi biner setiap exe adalah `1.0.0.0` selamanya sementara
   `VersionInfoVersion` di `.iss` adalah 2.5.0.0. Add/Remove Programs
   menampilkan 2.5.0; Properties exe berbunyi 1.0.0.0.

3. Build lokal mengirim `version_info.txt` tanpa perubahan sama sekali
   (`build-exe.bat:86`, `build-exe.ps1:64`), jadi exe lokal melaporkan
   1.0.0 dan tidak membawa build maupun commit. Klaim di
   `windows/README.md:482-483` ("Properties -> Details ... di situ ada
   versi, build, dan commit") hanya benar untuk exe yang dibangun CI.

Test di sini mengunci (1) dan, bersama `test_version_and_hook_owner.py`,
(2) dan (3): semua artefak harus berasal dari `APP_VERSION` yang sama.
"""

from __future__ import annotations

import importlib.util
import re
import tempfile
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]
BUILD_INFO_PY = REPO / "windows/installer/build_info.py"
VERSION_INFO = REPO / "windows/installer/version_info.txt"


def _load():
    spec = importlib.util.spec_from_file_location("build_info", BUILD_INFO_PY)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


build_info = _load()


class NumericVersionTest(unittest.TestCase):
    def test_plain_version(self):
        self.assertEqual(build_info.numeric_version("2.5.0", parts=3), "2.5.0")

    def test_four_parts_are_preserved(self):
        self.assertEqual(build_info.numeric_version("1.2.3.4", parts=4), "1.2.3.4")

    def test_v_prefix_is_accepted(self):
        self.assertEqual(build_info.numeric_version("v2.5.0", parts=3), "2.5.0")

    def test_release_suffix_is_not_counted_as_a_component(self):
        # "rc1" tidak boleh jadi komponen keempat.
        self.assertEqual(build_info.numeric_version("v1.0.0-rc1", parts=4), "1.0.0.0")

    def test_short_version_is_zero_padded(self):
        self.assertEqual(build_info.numeric_version("1.2", parts=4), "1.2.0.0")

    def test_year_in_the_middle_does_not_saturate_to_255(self):
        # Bug yang dikunci: 'release-2026.09' -> 255.9.0 karena min(n, 255).
        # 255 bukan versi, dan 2026.9.0 juga tidak valid untuk Inno.
        out = build_info.numeric_version("release-2026.09", parts=3)
        self.assertNotEqual(out, "255.9.0")
        for part in out.split("."):
            self.assertLessEqual(int(part), 255, f"komponen tidak valid: {out}")

    def test_year_forms_stay_valid(self):
        for raw in ("release-2026.09", "v2026.09.30", "2026.09"):
            with self.subTest(raw=raw):
                out = build_info.numeric_version(raw, parts=4)
                for part in out.split("."):
                    self.assertLessEqual(int(part), 255, f"{raw} -> {out}")

    def test_unparseable_falls_back_rather_than_raising(self):
        out = build_info.numeric_version("no-digits-here", parts=3)
        self.assertEqual(len(out.split(".")), 3)
        for part in out.split("."):
            self.assertLessEqual(int(part), 255)

    def test_empty_string_falls_back(self):
        out = build_info.numeric_version("", parts=3)
        self.assertEqual(len(out.split(".")), 3)

    def test_every_component_is_always_in_range(self):
        samples = ["2.5.0", "1.0.0.0", "v999.1.1", "release-2026.09", "300.2.1"]
        for raw in samples:
            with self.subTest(raw=raw):
                out = build_info.numeric_version(raw, parts=4)
                self.assertEqual(len(out.split(".")), 4)
                for part in out.split("."):
                    self.assertLessEqual(int(part), 255)


class StampVersionInfoTest(unittest.TestCase):
    """Satu fungsi untuk menyetempel SEMUA field versi."""

    def setUp(self):
        self.tmp = tempfile.mkdtemp(prefix="examvan-versioninfo-")
        self.src = Path(self.tmp) / "version_info.txt"
        self.dst = Path(self.tmp) / "out.txt"
        self.src.write_text(VERSION_INFO.read_text(encoding="utf-8"), encoding="utf-8")

    def tearDown(self):
        import shutil

        shutil.rmtree(self.tmp, ignore_errors=True)

    def _stamp(self, **kwargs):
        params = dict(
            source=self.src, dest=self.dst,
            version="2.5.0", build="695", commit="769f463",
        )
        params.update(kwargs)
        return build_info.stamp_version_info(**params)

    def _read(self):
        return self.dst.read_text(encoding="utf-8")

    def test_writes_the_destination(self):
        self._stamp()
        self.assertTrue(self.dst.exists())

    def test_stamps_string_struct_file_version(self):
        self._stamp()
        m = re.search(r"StringStruct\('FileVersion', '([^']*)'\)", self._read())
        self.assertIsNotNone(m)
        self.assertEqual(m.group(1), "2.5.0")

    def test_stamps_string_struct_product_version(self):
        self._stamp()
        m = re.search(r"StringStruct\('ProductVersion', '([^']*)'\)", self._read())
        self.assertIsNotNone(m)
        self.assertIn("2.5.0", m.group(1))
        self.assertIn("build 695", m.group(1))
        self.assertIn("769f463", m.group(1))

    def test_stamps_fixed_file_info_filevers(self):
        # Inilah yang tidak pernah terjadi sebelumnya: biner exe selalu
        # 1.0.0.0 walaupun ProductVersion sudah 2.5.0.
        self._stamp()
        m = re.search(r"filevers=\(([^)]*)\)", self._read())
        self.assertIsNotNone(m)
        parts = [p.strip() for p in m.group(1).split(",")]
        self.assertNotEqual(parts[:3], ["1", "0", "0"])
        for p in parts:
            self.assertLessEqual(int(p), 255)

    def test_stamps_fixed_file_info_prodvers(self):
        self._stamp()
        m = re.search(r"prodvers=\(([^)]*)\)", self._read())
        self.assertIsNotNone(m)
        parts = [p.strip() for p in m.group(1).split(",")]
        self.assertNotEqual(parts[:3], ["1", "0", "0"])

    def test_produces_four_numeric_components(self):
        self._stamp()
        for key in ("filevers", "prodvers"):
            m = re.search(rf"{key}=\(([^)]*)\)", self._read())
            self.assertEqual(len(m.group(1).split(",")), 4, key)

    def test_source_is_not_modified(self):
        before = self.src.read_text(encoding="utf-8")
        self._stamp()
        self.assertEqual(self.src.read_text(encoding="utf-8"), before)

    def test_all_five_fields_now_agree(self):
        # Kontrak yang harus dibaca guru: label di layar, installer, dan
        # Properties exe semuanya nomor yang sama.
        self._stamp()
        out = self._read()
        file_struct = re.search(r"StringStruct\('FileVersion', '([^']*)'\)", out).group(1)
        filevers = re.search(r"filevers=\(([^)]*)\)", out).group(1)
        shown = ".".join(
            str(int(p.strip())) for p in filevers.split(",")[:3]
        )
        self.assertEqual(file_struct, shown)


class BuildScriptsUseTheStamperTest(unittest.TestCase):
    """Semua jalur build harus lewat fungsi yang sama.

    Sebelumnya CI men-*patch* dengan regex PowerShell，sedangkan build lokal tidak
    men-*patch* sama sekali — dua implementasi dari aturan yang sama, dan
    yang lokal diam-diam salah.
    """

    def _script(self, rel):
        return (REPO / rel).read_text(encoding="utf-8")

    def test_local_bat_uses_the_stamper(self):
        self.assertIn("build_info.py", self._script("windows/build-exe.bat"))

    def test_local_ps1_uses_the_stamper(self):
        self.assertIn("build_info.py", self._script("windows/build-exe.ps1"))

    def test_ci_uses_the_stamper(self):
        self.assertIn("build_info.py", self._script(".github/workflows/build-windows.yml"))

    def test_no_script_patches_version_regexes_by_hand(self):
        for rel in ("windows/build-exe.bat", "windows/build-exe.ps1",
                    ".github/workflows/build-windows.yml"):
            src = self._script(rel)
            self.assertNotIn("StringStruct\\('ProductVersion'", src, rel)
            self.assertNotIn("StringStruct\\('FileVersion'", src, rel)


if __name__ == "__main__":
    unittest.main()
