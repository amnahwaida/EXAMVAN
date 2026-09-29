#!/usr/bin/env python3
"""Regenerasi `windows/installer/examvan.ico` dari ikon launcher Android.

Jalankan HANYA saat ikon brand berubah (mis. setelah desain ulang):

    python windows/installer/make_icon.py

Kenapa `.ico` yang sudah jadi di-commit, bukan di-generate saat build
------------------------------------------------------------------------
Windows butuh satu file `.ico` multi-ukuran: 16 (shell/taskbar), 24 (bar
besar), 32 (Explorer), 48 (Explorer + Start Menu), 64/128 (daftar
bertingkat), dan 256 (penjelajah serta DPI 200-300%). Generate-nya butuh
ImageMagick atau Pillow — dan KEDUA tidak boleh menjadi syarat build di PC
guru/siswa maupun di runner CI. Jadi hasil akhir di-commit sebagai sumber
kebenaran, dan build TIDAK PERNAH memanggil skrip ini.

`tests/test_app_icon.py::MakeIconScriptTest::test_script_never_gates_the_build`
menjaga hal itu.

Sumber ikon
-----------
`android/app/src/main/res/mipmap-xxxhdpi/ic_launcher.png` — 192x192 RGBA,
putih + ungu EXAMVAN. Ikon yang sama dengan yang dipakai aplikasi Android,
jadi brand di kedua klien sama. Tidak ada gambar baru yang dibuat di sini.

192 -> 256 memakai Lanczos (bukan nearest-neighbour) supaya entri 256 px
tidak terlihat pecah di layar 4K. Setiap ukuran kecil diperkecil dari master
192, bukan dari 256, supaya tepinya tidak bergerigi dua kali.
"""

from __future__ import annotations

import argparse
import shutil
import struct
import subprocess
import sys
import tempfile
from pathlib import Path

HERE = Path(__file__).resolve().parent
REPO = HERE.parents[1]

SOURCE = REPO / "android/app/src/main/res/mipmap-xxxhdpi/ic_launcher.png"
TARGET = HERE / "examvan.ico"

# Semua ukuran yang benar-benar dipakai Windows. 256 WAJIB ada: tanpa itu
# shell akan memperbesar versi 192px dengan nearest-neighbour dan
# hasilnya terlihat pecah.
SIZES = (16, 24, 32, 48, 64, 128, 256)


def _require(path: Path, hint: str) -> None:
    if not path.exists():
        raise SystemExit(
            f"{path} tidak ditemukan.\n{hint}"
        )


def _with_imagemagick(source: Path, target: Path) -> bool:
    convert = shutil.which("magick") or shutil.which("convert")
    if not convert:
        return False
    _require(source, "Sumber ikon Android hilang.")
    args = [convert, str(source), "-background", "none", "-define",
            "icon:auto-extract=false"]
    for size in SIZES:
        args += ["(", "-clone", "0", "-filter", "Lanczos",
                 "-resize", f"{size}x{size}", ")"]
    args += ["-delete", "0", str(target)]
    result = subprocess.run(args, capture_output=True, text=True)
    if result.returncode != 0:
        raise SystemExit(f"ImageMagick gagal:\n{result.stderr}")
    return True


def _with_pillow(source: Path, target: Path) -> bool:
    try:
        from PIL import Image  # noqa: PLC0415
    except ImportError:
        return False
    _require(source, "Sumber ikon Android hilang.")
    with Image.open(source) as master:
        master = master.convert("RGBA")
        frames = [
            master.resize((size, size), Image.LANCZOS)
            for size in SIZES
        ]
        frames[-1].save(
            target, format="ICO",
            sizes=[(s, s) for s in SIZES], append_images=frames[:-1],
        )
    return True


def verify(target: Path) -> list[int]:
    """Baca directory .ico dan kembalikan ukuran yang benar-benar ada."""
    data = target.read_bytes()
    if data[:4] != b"\x00\x00\x01\x00":
        raise SystemExit(f"{target} bukan .ico yang sah")
    (count,) = struct.unpack("<H", data[4:6])
    found = []
    for i in range(count):
        off = 6 + i * 16
        w, h, _c, _r, _p, bpp, size, _o = struct.unpack(
            "<BBBBHHII", data[off:off + 16]
        )
        if bpp < 32:
            raise SystemExit(f"entri {w or 256}px hanya {bpp}bpp — perlu 32")
        found.append(w or 256)
    missing = [s for s in SIZES if s not in found]
    if missing:
        raise SystemExit(f"ukuran hilang dari {target.name}: {missing}")
    return sorted(found)


def main(argv: list[str] | None = None) -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--source", type=Path, default=SOURCE)
    ap.add_argument("--target", type=Path, default=TARGET)
    ap.add_argument("--check", action="store_true",
                    help="hanya verifikasi target, jangan menulis")
    args = ap.parse_args(argv)

    if args.check:
        print(f"OK  {args.target} berisi ukuran: {verify(args.target)}")
        return 0

    # Pillow dulu: hasilnya lebih deterministik antar-platform daripada
    # ImageMagick yang bisa berbeda versi.
    made = _with_pillow(args.source, args.target)
    if not made:
        made = _with_imagemagick(args.source, args.target)
    if not made:
        raise SystemExit(
            "Butuh Pillow (`pip install Pillow`) atau ImageMagick.\n"
            "Build TIDAK memerlukan keduanya — .ico sudah di-commit; skrip ini\n"
            "hanya perlu dijalankan saat ikon brand berubah."
        )

    print(f"OK  {args.target} berisi ukuran: {verify(args.target)}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
