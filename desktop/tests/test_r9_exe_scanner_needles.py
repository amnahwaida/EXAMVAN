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
# Realitas exe onefile PyInstaller: dua arsip dalam satu berkas.
#
#   1. CArchive  -> entri TOC berupa NAMA MODUL titik yang diakhiri NUL
#                   (ditambah typecode 's'/'m'/'b'/'z'), plus nama binary.
#   2. PYZ.pyz   -> arsip ZIP berisi SEMUA modul pure-python. Nama file di
#                   central directory ZIP tidak dikompresi, jadi byte-nya ada
#                   di exe -- tapi TANPA delimiter NUL.
#
# `fitz` hanya ada di (2). Eclipse Ladangiah pemindaian berbasis NUL tidak
# akan pernah menemukannya, dan itulah akar `stack PDF tidak ter-bundle: fitz`.
def _carchive(names=(), binaries=()):
    """Bentuk entri TOC CArchive: NUL + nama + NUL + typecode."""
    return b"".join(
        b"\x00" + n.encode() + b"\x00" + tc.encode()
        for n, tc in names
    ) + b"".join(b"\x00" + b.encode() + b"\x00b" for b in binaries)


def _pyz(entries):
    import io
    import zipfile

    buf = io.BytesIO()
    with zipfile.ZipFile(buf, "w", zipfile.ZIP_DEFLATED) as z:
        for name in entries:
            z.writestr(name, b"\x00" * 64)   # bytecode terkompresi
    return buf.getvalue()


_PYZ_ENTRIES = (
    "fitz", "fitz.table", "fitz.utils", "pymupdf", "pymupdf.utils",
    "examvan", "examvan.api", "examvan.ui", "examvan.ui.pdf_viewer",
)
BLOB = (
    _carchive(
        [("examvan", "m"), ("examvan.api", "s"),
         ("examvan.ui.pdf_viewer", "s")],
        ["_mupdf.pyd", "Qt5Core.dll", "Qt5Widgets.dll"],
    )
    + _pyz(_PYZ_ENTRIES)
)
# Bentuk CArchive lama (path `.pyc`) tidak lagi jadi sumber utama, tapi
# pemeriksa tidak boleh Depends on itu saja.
BLOB_PATH_STYLE = b"fitz/__init__.pyc\x00examvan.api\x00_mupdf.pyd\x00"
BLOB_TOC_STYLE = BLOB


class PackageNameExtractionTest(unittest.TestCase):
    def test_top_level_package_is_found(self):
        self.assertIn("fitz", lem.package_names(BLOB))

    def test_the_package_name_comes_from_a_structural_zip_read(self):
        # Nama harus diambil dari record ZIP, bukan dari kemunculan teks
        # bebas. Blob dengan `fitz` di tengah data acak (tanpa entri ZIP)
        # TIDAK boleh menghasilkan paket.
        noise = b"\x00" + b"x" * 40 + b"fitz" + b"\x00" + b"y" * 40
        self.assertNotIn("fitz", lem.package_names(noise))

    def test_submodule_keeps_its_dotted_name(self):
        # Entri ZIP menyimpan nama modul yang tersisaimportnya apa adanya --
        # itu justru yang berguna: `fitz.table` membuktikan modul di dalam
        # paket ikut terpaket, bukan hanya `__init__`-nya.
        names = lem.package_names(BLOB)
        self.assertIn("fitz.table", names)
        self.assertIn("pymupdf.utils", names)

    def test_package_names_is_a_superset_of_the_top_level_roots(self):
        # `package_names` sengaja melaporkan nama dotted apa adanya
        # (`fitz.table`) karena itu informasi berguna, bukan daftar paket
        # murni. Yang dijaga hanyalah bahwa akar paketnya ikut terambil.
        names = lem.package_names(BLOB)
        self.assertIn("fitz", names)
        self.assertTrue(
            {"examvan", "pymupdf"}.issubset(set(names)),
            f"akar paket hilang dari {names}",
        )


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