#!/usr/bin/env python3
"""Gagalkan build-setup mengemas EXAMVAN.exe yang lebih tua dari source.

Kenapa ini perlu: `build-setup.bat` / `.ps1` dulu hanya menguji `-exist`.
Bukti di working tree: `windows/dist/EXAMVAN.exe` ber-mtime 27 Sep jam 11:58
sementara source terbaru 30 Sep jam 05:13. Menjalankan build-setup hari
itu mencetak **BUILD SUCCESS** dan menghasilkan `EXAMVAN-Setup.exe` berisi
kode tiga hari lalu — tanpa perbaikan ronde 1, ronde 2, maupun ronde 3 —
lalu dibagikan ke siswa. Tidak ada apa pun yang mengatakannya.

Kelas staleness yang sama baru saja diperbaiki untuk `.deb` di `ci.yml`.
Script ini adalah padanannya untuk Windows, dan bisa dieksekusi di mana saja
karena murni Python.

Keluar dengan kode 0 bila exe lebih baru dari seluruh source (build
segar), kode 1 bila basi (build HARUS gagal), kode 2 bila sumber tidak
ditemukan (build gagal: kita tidak bisa membuktikan apa pun).
"""

from __future__ import annotations

import argparse
import sys
from pathlib import Path


def newest_source_mtime(root: Path) -> tuple[float, Path | None]:
    """mtime file source terbaru di bawah `root` (python, bukan bytecode)."""
    newest = 0.0
    newest_path: Path | None = None
    # `main.py` WAJIB masuk daftar: itu satu-satunya entry point yang
    # benar-benar di-build PyInstaller (`build-exe.bat` / `build-windows.yml`).
    # Versi sebelumnya hanya melihatexamvan/`, `tests/`, `requirements.txt` --
    # jadi perbaikan di main.py lolos ke exe basi tanpa dilaporkan, persis
    # skenario yang guard ini dibuat untuk cegah.
    #
    # `version_info.txt` dan `examvan.ico` juga dikompilasi ke dalam exe, jadi
    # exe bisa membawa versi/ikon lama meski source-nya sudah berubah.
    #
    # Sisi lain: `tests/**` tidak berpengaruh apa pun ke binary. Diawasi
    # hanya karena tidak salah -- tapi jangan sampai ia menggantikan
    # entry point yang benar-benar penting.
    for pattern in (
        "main.py",
        "examvan/**/*.py",
        "requirements.txt",
        "../windows/installer/version_info.txt",
        "../windows/installer/examvan.ico",
    ):
        for path in root.glob(pattern):
            if "__pycache__" in path.parts:
                continue
            mtime = path.stat().st_mtime
            if mtime > newest:
                newest, newest_path = mtime, path
    return newest, newest_path


def check(exe: Path, source_root: Path) -> tuple[int, str]:
    """(exit_code, pesan) — 0 berarti aman."""
    if not exe.exists():
        return 2, f"EXAMVAN.exe tidak ada: {exe}"
    newest, newest_path = newest_source_mtime(source_root)
    if newest_path is None:
        return 2, f"tidak ada source di bawah {source_root} — tidak bisa memastikan"
    exe_mtime = exe.stat().st_mtime
    if exe_mtime >= newest:
        return 0, f"OK: EXAMVAN.exe ({exe_mtime:.0f}) >= source ({newest:.0f})"
    return 1, (
        f"EXAMVAN.exe ({exe_mtime:.0f}) lebih tua dari source "
        f"({newest:.0f}, {newest_path.name}).\n"
        f"       Build akan mengemas kode LAMA dan student menerimanya.\n"
        f"       Jalankan dulu:  windows\\build-exe.bat"
    )


def main(argv: list[str] | None = None) -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--exe", required=True, type=Path)
    ap.add_argument("--source", required=True, type=Path)
    args = ap.parse_args(argv)
    code, message = check(args.exe, args.source)
    if code == 0:
        print(f"[OK]   {message}")
    else:
        print(f"[WARN] {message}", file=sys.stderr)
    return code


if __name__ == "__main__":
    sys.exit(main())
