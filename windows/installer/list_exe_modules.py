#!/usr/bin/env python3
"""Daftar apa yang benar-benar ada di dalam PyInstaller onefile exe.

PyInstaller `--onefile` menghasilkan satu exe yang isinya arsip. Modul yang
tidak ikut terpaket baru ketahuan saat aplikasi memanggilnya — di PC siswa,
bukan di runner. Job Linux sudah memverifikasi isi `.deb` (`ci.yml`), dan
paket `.deb` sudah beberapa kali terkirim tanpa notify.py / ws.py /
security/base.py / ui/waiting_approval.py. Windows tidak punya padanannya
sama sekali, dan kelas bug yang sama tidak akan hilang hanya karena tidak
diperiksa.

Jadi: baca isi exe-nya, bukan source tree. Yang diuji adalah apa yang
benar-benar terpaket.

Cara kerja
----------
Tabel simbol PYZ berisi entri bytecode marshalled yang diakhiri nama modul
sebagai byte ASCII — jadi nama modul bisa diambil dengan pola, tanpa perlu parser
format biner PyInstaller:

    examvan
    examvan.api
    examvan.ui.answer_sheet
    ...

Pemakaian:
    list_exe_modules.py windows/dist/EXAMVAN.exe         # nama modul
    list_exe_modules.py --raw windows/dist/EXAMVAN.exe   # + nama file native
    list_exe_modules.py --require examvan.api,examvan.ui.pdf_viewer windows/dist/EXAMVAN.exe

Keluar dengan kode 0 kalau ada nama modul, 1 kalau tidak ada (arsip rusak
atau bukan exe PyInstaller) atau kalau ada modul --require yang hilang,
2 kalau file tidak bisa dibaca.
"""

from __future__ import annotations

import re
import struct
import sys
from pathlib import Path

# `examvan` lalu segmen beridentifier, dipisah titik. Lookbehind/lookahead
# supaya tidak ikut memotong nama yang lebih panjang atau salah memotong di
# tengah kata.
_MODULE_RE = re.compile(rb"(?<![\w.])examvan(?:\.[A-Za-z_]\w*)*")

# Nama file native yang dipaketkan (DLL Qt, _mupdf.pyd, ...). Muncul sebagai
# entri NUL-terminated di CArchive, jadi cukup dibaca dari nama file itu.
_NATIVE_RE = re.compile(rb"[\w.\-]+\.(?:dll|pyd|so|dylib|zip|manifest)(?![\w.])")

# Paket PYTHON pihak ketiga (PyMuPDF -> `fitz`, Pillow, ...). Entri CArchive
# PyInstaller untuk sebuah paket bernama `<pkg>/__init__.pyc` (atau `<pkg>.pyd`
# untuk yang native). Regex modul di atas HANYA menangkap `examvan.*`, jadi
# tanpa fungsi ini jarum "fitz" tidak pernah bisa cocok -- pemeriksaan
# "stack PDF ter-bundle" di build-windows.yml akan selalu gagal meski
# PyMuPDF benar-benar ikut terpaket.
_PY_PKG_RE = re.compile(
    rb"(?<![\w.\\/])([A-Za-z_]\w*)(?:[\\/][A-Za-z_]\w*)*[\\/]__init__\.pyc(?![\w.])"
)

# Bentuk KEDUA yang dipakai PyInstaller: sejak CArchive tidak lagi menyimpan
# pathBerkas `.pyc`, entri tabelnya adalah NAMA MODUL titik yang diakhiri NUL,
# disusul typecode ('s' modul, 'm' paket, 'b' native, 'z' zip). Itu sebabnya
# `fitz/__init__.pyc` tidak pernah ada di exe PyInstaller 6 -- yang ada
# `\x00fitz\x00`. Keduanya harus dicocokkan supaya pemeriksaan "stack PDF
# ter-bundle" bekerja pada kedua format.
# Nama modul python TIDAK ada di TOC CArchive sebagai nama yang bisa dipindai:
# PyInstallersemua modul pure-python dikemas ke `PYZ.pyz`, yaitu arsip ZIP. Nama file di central directory ZIP memang TIDAK dikompresi, jadi
# byte-nya ada di dalam exe -- tapi tanpa NUL di sekitar nama, sehingga
# pemindaian berbasis NUL tidak akan pernah menemukannya. Itu sebabnya
# jarum "fitz" tidak cocok padahal PyMuPDF benar-benar terpaket.
#
# Karena itu entri ZIP diparse STRUKTURAL (signature + panjang nama), bukan
# dengan tebakan pola. Signature `PK\x03\x04` = local file header,
# `PK\x01\x02` = central directory.
_ZIP_LOCAL_SIG = b"PK\x03\x04"
_ZIP_CENTRAL_SIG = b"PK\x01\x02"

_CHUNK = 8 * 1024 * 1024


def module_names(blob: bytes) -> list[str]:
    """Nama modul examvan yang tercatat di dalam arsip."""
    names: set[str] = set()
    for m in _MODULE_RE.finditer(blob):
        name = m.group(0).decode("ascii")
        # Buang sisa nama file packaging bila ikut terbawa.
        for suffix in (".pyc", ".py"):
            if name.endswith(suffix):
                name = name[: -len(suffix)]
        if all(seg.isidentifier() for seg in name.split(".")):
            names.add(name)
    return sorted(names)


def native_names(blob: bytes) -> list[str]:
    """Nama file native (.dll/.pyd/...) yang dipaketkan."""
    out: set[str] = set()
    for m in _NATIVE_RE.finditer(blob):
        try:
            out.add(m.group(0).decode("ascii"))
        except UnicodeDecodeError:
            continue
    return sorted(out)


def package_names(blob: bytes) -> list[str]:
    """Nama paket python yang tercatat di arsip (level teratas).

    Sumbernya entri ZIP di `PYZ.pyz`, karena di situlah modul pure-python
    (termasuk `fitz`) benar-benar berada. Nama entri CArchive untuk binary
    (`_mupdf.pyd`) ditangani `native_names`, dan nama modul `examvan.*`
    ditangani `module_names` -- keduanya tidak perlu diulang di sini.
    """
    out: set[str] = set()
    for name in zip_entry_names(blob):
        head = name.replace("\\", "/").split("/", 1)[0]
        if head and all(seg.isidentifier() for seg in head.split(".")):
            out.add(head)
    return sorted(out)


def zip_entry_names(blob: bytes) -> list[str]:
    """Nama file dari semua entri ZIP yang tertanam di dalam blob.

    Diparse struktural dari signature + panjang nama, bukan lewat pola teks:
    nama modul python hanya ada di `PYZ.pyz` dan tidak punya delimiter NUL,
    jadi satu-satunya cara yang benar adalah membaca record ZIP-nya.
    """
    out: set[str] = set()

    # Central directory: header 46 byte, nama mulai di offset 46.
    pos = blob.find(_ZIP_CENTRAL_SIG)
    while pos != -1:
        try:
            name_len = struct.unpack_from("<H", blob, pos + 28)[0]
            name = blob[pos + 46: pos + 46 + name_len]
            if name and not name.startswith((b"PK",)):
                out.add(name.decode("utf-8", "replace"))
        except struct.error:
            pass
        pos = blob.find(_ZIP_CENTRAL_SIG, pos + 4)

    # Local file header: nama mulai di offset 30. Dicoba juga supaya exe
    # yang central directory-nya tidak utuh tetap terbaca.
    pos = blob.find(_ZIP_LOCAL_SIG)
    while pos != -1:
        try:
            name_len = struct.unpack_from("<H", blob, pos + 26)[0]
            name = blob[pos + 30: pos + 30 + name_len]
            if name and not name.startswith((b"PK",)):
                out.add(name.decode("utf-8", "replace"))
        except struct.error:
            pass
        pos = blob.find(_ZIP_LOCAL_SIG, pos + 4)

    return sorted(out)


def bundle_module_names(exe: Path) -> list[str] | None:
    """Nama modul dari arsip PyInstaller, DIBACA DENGAN PEMBACA RESMI.

    Pendekatan byte-scanning di file ini adalah tebakan format: entri
    CArchive berupa nama titik yang diakhiri NUL, sedangkan modul pure-python
    ada di `PYZ.pyz` yang central directory ZIP-nya tidak dikompresi tapi juga
    tidak punya delimiter. Dua-duanya sudah terbukti salah di build-windows.

    PyInstaller sendiri menyediakannya: `pkg_archive_contents(exe,
    recursive=True)` membuka CArchive lalu MENELUSURI PYZ dan mengembalikan
    seluruh nama modul. Itu pembaca otoritatif -- immune terhadap perubahan
    format, dan tidak bisa salah karena "nama modul tak pernah punya
    delimiter".

    Mengembalikan None kalau PyInstaller tidak bisa di-import (mis. saat
    pemeriksa ini dijalankan lokal di luar lingkungan build), supaya pemanggil
    bisa jatuh ke pemindaian byte dan tetap melihat sesuatu.
    """
    try:
        from PyInstaller.archive.readers import pkg_archive_contents
    except Exception:
        return None
    try:
        return sorted(set(pkg_archive_contents(str(exe), recursive=True)))
    except Exception as exc:
        print(f"pembaca arsip PyInstaller gagal: {exc}", file=sys.stderr)
        return None


def read_blob(exe: Path) -> bytes:
    # Exe PyInstaller onefile 60-90 MB; ini alat verifikasi sekali jalan,
    # jadi seluruh file dibaca ke memori sekaligus supaya tidak ada seek yang
    # salah across batas chunk.
    return exe.read_bytes()


def main(argv: list[str] | None = None) -> int:
    argv = list(sys.argv[1:] if argv is None else argv)
    raw = False
    bundle = False
    require: list[str] = []
    if argv and argv[0] == "--bundle-list":
        bundle = True
        argv = argv[1:]
        if not argv:
            print(
                "pemakaian: list_exe_modules.py --bundle-list <path-to-exe>",
                file=sys.stderr,
            )
            return 2
        exe_path = Path(argv[0])
        if not exe_path.exists():
            print(f"tidak ada: {exe_path}", file=sys.stderr)
            return 2
        names = bundle_module_names(exe_path)
        if names is None:
            print(
                "PyInstaller tidak tersedia; --bundle-list hanya bisa jalan "
                "di lingkungan build",
                file=sys.stderr,
            )
            return 3
        for name in names:
            print(name)
        return 0
    if argv and argv[0] == "--raw":
        raw = True
        argv = argv[1:]
    if argv and argv[0] == "--require":
        if len(argv) < 3:
            print(
                "pemakaian: list_exe_modules.py [--raw|--bundle-list] "
                "[--require mod1,mod2,...] <path-to-exe>",
                file=sys.stderr,
            )
            return 2
        require = [m.strip() for m in argv[1].split(",") if m.strip()]
        argv = argv[2:]
    if not argv:
        print(
            "pemakaian: list_exe_modules.py [--raw] "
            "[--require mod1,mod2,...] <path-to-exe>",
            file=sys.stderr,
        )
        return 2

    exe = Path(argv[0])
    if not exe.exists():
        print(f"tidak ada: {exe}", file=sys.stderr)
        return 2

    try:
        blob = read_blob(exe)
    except OSError as exc:
        print(f"gagal membaca {exe}: {exc}", file=sys.stderr)
        return 2

    modules = module_names(blob)
    if not modules:
        print(
            f"tidak ada modul examvan di {exe} — bukan arsip PyInstaller, "
            f"atau exe rusak",
            file=sys.stderr,
        )
        return 1

    if require:
        missing = [m for m in require if m not in modules]
        if missing:
            for m in missing:
                print(f"modul hilang dari exe: {m}", file=sys.stderr)
            return 1

    if raw:
        # Paket pihak ketiga ikut dicetak: `--raw` dipakai workflow hanya
        # untuk pencarian jarum, dan `--require` tetap memakai `modules`
        # sehingga daftar wajib tidak ikut berubah.
        for name in modules + package_names(blob) + native_names(blob):
            print(name)
    else:
        for name in modules:
            print(name)
    return 0


if __name__ == "__main__":
    sys.exit(main())
