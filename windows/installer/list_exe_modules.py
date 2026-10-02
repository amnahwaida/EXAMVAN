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
        for name in modules + native_names(blob):
            print(name)
    else:
        for name in modules:
            print(name)
    return 0


if __name__ == "__main__":
    sys.exit(main())
