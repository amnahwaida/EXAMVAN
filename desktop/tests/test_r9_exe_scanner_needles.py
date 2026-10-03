"""Scanner isi exe PyInstaller tidak boleh-butakan jarum yang sah.

Bug yang ditemukan di CI ronde 9
--------------------------------
`build-windows.yml` memverifikasi "stack PDF ter-bundle" dengan jarum
`"fitz"` dan `"_mupdf"` terhadap output `--raw` dari
`windows/installer/list_exe_modules.py`. Pemeriksaan itu gagal dengan
`stack PDF tidak ter-bundle: fitz` — padahal `examvan.ui.pdf_viewer` ada
di dalam exe (langkah sebelumnya melaporkannya) dan PyMuPDF terpasang di
lingkungan build.

Penyebabnya_by construction_, bukan isi exe: `module_names()` memakai
`_MODULE_RE` yang HANYA cocok dengan awalan `examvan`, dan `native_names()`
hanya menangkap `.dll/.pyd/.so/.dylib/.zip/.manifest`. Entri PyInstaller
untuk paket python bernama `fitz/__init__.pyc` tidak akan pernah muncul
di keduanya, dan native PyMuPDF bernama `_mupdf.pyd` — bukan `fitz.pyd`.
Jadi jarum `"fitz"` tidak bisa cocok pada exe mana pun, termasuk yang
lengkap.

Akibatnya pemeriksaan ini tidak pernah hijau sejak diperkenalkan, dan
build Windows rusak untuk semua commit berikutnya. Yang detrimental bukan
artefaknya: developer belajar bahwa pemeriksaan build ini tidak berguna,
jadi tidak akan mempercayainya.

Yang dijaga di sini
-------------------
* `package_names` membaca paket top-level dari entri `<pkg>/__init__.pyc`;
* `--raw` mencetak paket itu sehingga jarum `fitz` cocok;
* `--require` (daftar wajib modul) TIDAK ikut berubah, karena ia memakai
  `module_names` yang tetap hanya `examvan.*`;
* entri `examvan.*` sendiri tidak bocor ke `package_names` sebagai terotorisasi
  yang salahcalculate.
"""

from __future__ import annotations

import importlib.util
import subprocess
import sys
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]
SCANNER = REPO / "windows" / "installer" / "list_exe_modules.py"
WORKFLOW = REPO / ".github" / "workflows" / "build-windows.yml"


def _load():
    spec = importlib.util.spec_from_file_location("list_exe_modules", SCANNER)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


lem = _load()

# Blob tiruan sepotong CArchive PyInstaller: entri NUL-terminated.
# Bentuk mengikuti CArchive PyInstaller sungguhan: modul murni disimpan
# sebagai NAMA BERPIKAT (`examvan.api`), sedangkan paket memakai path
# (`fitz/__init__.pyc`) — itulah alasan `module_names` bisa membaca modul
# `examvan` tapi tidak pernah membaca paket pihak ketiga.
BLOB = (
    b"fitz\x00fitz/__init__.pyc\x00fitz/fitz.py\x00"
    b"examvan.api\x00examvan.ui.pdf_viewer\x00examvan/ui/__init__.pyc\x00"
    b"_mupdf.pyd\x00Qt5Core.dll\x00PyQt5/QtCore.pyd\x00"
)


class PackageNameExtractionTest(unittest.TestCase):
    def test_top_level_package_is_found(self):
        self.assertIn("fitz", lem.package_names(BLOB))

    def test_submodule_is_not_reported_as_its_own_package(self):
        # `fitz/fitz.py` adalah modul di dalam paket, bukan paket baru.
        self.assertNotIn("fitz.py", lem.package_names(BLOB))
        self.assertEqual(lem.package_names(BLOB).count("fitz"), 1)

    def test_examvan_itself_is_reported_because_it_is_a_package(self):
        # `examvan/__init__.pyc` memang paket, jadi HARUS muncul. Yang
        # penting adalah modul BERCACAH TIDAK ikut dihitung sebagai paket —
        # itulah yang menjaga `--require` tetap berarti.
        names = lem.package_names(BLOB)
        self.assertIn("examvan", names)
        self.assertNotIn("examvan.api", names,
                         "modul titik ikut dikira paket")

    def test_the_package_list_does_not_change_the_required_modules(self):
        # Sifat yang diandalkan workflow: daftar wajib berasal dari
        # `module_names` (hanya `examvan.*`), bukan dari `package_names`.
        required = {"examvan.api", "examvan.ui.pdf_viewer"}
        self.assertTrue(required.issubset(set(lem.module_names(BLOB))))
        self.assertFalse(
            required.issubset(set(lem.package_names(BLOB))),
            "kalau daftar wajib ikut dari daftar paket, pemeriksaan build "
            "kehilangan artinya",
        )

    def test_a_plain_module_without_init_is_not_a_package(self):
        self.assertEqual(lem.package_names(b"examvan.api\x00sys.pyc\x00"), [])


class NeedleReachabilityTest(unittest.TestCase):
    def test_the_pdf_needles_used_by_the_workflow_are_reachable(self):
        raw = " ".join(
            lem.module_names(BLOB)
            + lem.package_names(BLOB)
            + lem.native_names(BLOB)
        )
        for needle in ("fitz", "_mupdf"):
            with self.subTest(needle=needle):
                self.assertIn(
                    needle, raw,
                    f"jarum {needle!r} tidak bisa cocok pada output apa pun — "
                    "pemeriksaan build akan selalu gagal meski aman",
                )

    def test_raw_output_contains_the_package(self):
        raw = " ".join(
            lem.module_names(BLOB)
            + lem.package_names(BLOB)
            + lem.native_names(BLOB)
        )
        self.assertIn("fitz", raw)


class RequireListUnaffectedTest(unittest.TestCase):
    """`--require` tidak boleh ikut memuat paket pihak ketiga."""

    def test_require_still_checks_only_examvan_modules(self):
        exe = Path(self.enterContext(_tempdir())) / "fake.exe"
        exe.write_bytes(BLOB)
        # `--require` dengan modul examvan yang ADA harus lulus; kalau
        # `require` ikut memakai `package_names`, daftar wajib bisa tergeser
        # oleh entri paket dan pemeriksaan kehilangan artinya.
        rc = subprocess.run(
            [sys.executable, str(SCANNER), "--raw",
             "--require", "examvan.api,examvan.ui.pdf_viewer", str(exe)],
            capture_output=True, text=True,
        )
        self.assertEqual(
            rc.returncode, 0,
            f"require ditolak padahal modul ada:\n{rc.stdout}\n{rc.stderr}",
        )
        self.assertIn("fitz", rc.stdout)

    def test_a_missing_required_module_still_fails(self):
        exe = Path(self.enterContext(_tempdir())) / "fake.exe"
        exe.write_bytes(BLOB)
        rc = subprocess.run(
            [sys.executable, str(SCANNER),
             "--require", "examvan.tidak.ada", str(exe)],
            capture_output=True, text=True,
        )
        self.assertEqual(rc.returncode, 1, "modul wajib hilang harus gagal")


class WorkflowUsesReachableNeedlesTest(unittest.TestCase):
    def test_workflow_checks_the_pdf_stack(self):
        text = WORKFLOW.read_text(encoding="utf-8")
        self.assertIn('foreach ($needle in @("fitz", "_mupdf"))', text)

    def test_workflow_reads_raw_output(self):
        text = WORKFLOW.read_text(encoding="utf-8")
        self.assertIn("list_exe_modules.py --raw", text)


def _tempdir():
    import tempfile

    class _Ctx:
        def __enter__(self):
            self._d = tempfile.TemporaryDirectory()
            return self._d.name

        def __exit__(self, *a):
            self._d.cleanup()

    return _Ctx()


if __name__ == "__main__":
    unittest.main()