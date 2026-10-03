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
# `\w{2,}`: typecode CArchive ("s", "m", "b", "z") juga berdiri sendiri
# di antara dua NUL, dan ikutnya hanya menambah derau — nama modul
# satu huruf tidak pernah ada.
_TOC_NAME_RE = re.compile(
    rb"\x00([A-Za-z_]\w{1,}(?:\.[A-Za-z_]\w+)*)(?=\x00)"
)

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

    Dua sumber, karena formatnya berbeda antar versi:

    * path `<pkg>/__init__.pyc` (CArchive lama);
    * entri TOC berbasis NUL, `\x00fitz\x00` (PyInstaller 4+, termasuk 6).

    Hanya level teratas yang dikembalikan karena itulah yang dipakai sebagai
    jarum di build-windows.yml.
    """
    out: set[str] = set()
    for regex, group in ((_PY_PKG_RE, 1), (_TOC_NAME_RE, 1)):
        for m in regex.finditer(blob):
            try:
                name = m.group(group).decode("ascii")
            except UnicodeDecodeError:
                continue
            out.add(name.split(".", 1)[0])
    return sorted(out)


def read_blob(exe: Path) -> bytes:
    # Exe PyInstaller onefile 60-90 MB; ini alat verifikasi sekali jalan,
    # jadi seluruh file dibaca ke memori sekaligus supaya tidak ada seek yang
    # salah across batas chunk.
    return exe.read_bytes()


def main(argv: list[str] | None = None) -> int:
    argv = list(sys.argv[1:] if argv is None else argv)
    raw = False
    require: list[str] = []
    if argv and argv[0] == "--raw":
        raw = True
        argv = argv[1:]
    if argv and argv[0] == "--require":
        if len(argv) < 3:
            print(
                "pemakaian: list_exe_modules.py [--raw] "
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
