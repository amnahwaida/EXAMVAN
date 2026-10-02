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
import struct
import sys
from pathlib import Path

# Ukuran minimum exe onefile PyInstaller yang sah. Di bawah ini berarti
# file terpotong / stub / bukan hasil build.
_MIN_EXE_SIZE = 1 * 1024 * 1024

# Pola source yang diawasi, relatif ke folder desktop/.
# `../windows/build-exe.*` ikut diawasi: flag build (.bat/.ps1) menentukan
# apa yang masuk ke exe, jadi exe bisa basi hanya karena script build-nya
# berubah.
_WATCHED_PATTERNS = (
    "main.py",
    "examvan/**/*.py",
    "requirements.txt",
    "../windows/installer/version_info.txt",
    "../windows/installer/examvan.ico",
    "../windows/build-exe.*",
)


def verify_artifact(exe: Path) -> str | None:
    """Periksa exe-nya sendiri, bukan cuma jamnya.

    Kembalikan pesan kesalahan bila bukan biner PE Windows yang sah
    (magic MZ + signature PE\\0\\0 via e_lfanew + ukuran minimum),
    atau None bila lolos.
    """
    try:
        size = exe.stat().st_size
        if size < _MIN_EXE_SIZE:
            return (
                f"EXAMVAN.exe hanya {size} byte (< 1MB) — "
                f"bukan onefile PyInstaller yang sah"
            )
        with exe.open("rb") as f:
            if f.read(2) != b"MZ":
                return "EXAMVAN.exe tidak ber-magic MZ — bukan executable Windows"
            f.seek(0x3C)
            e_lfanew = struct.unpack("<I", f.read(4))[0]
            f.seek(e_lfanew)
            if f.read(4) != b"PE\0\0":
                return "EXAMVAN.exe tanpa signature PE — file rusak atau bukan exe"
    except OSError as exc:
        return f"EXAMVAN.exe tidak bisa dibaca: {exc}"
    return None


def newest_source_mtime(root: Path) -> tuple[float, Path | None, dict[str, int]]:
    """mtime file source terbaru di bawah `root` (python, bukan bytecode).

    Kembalikan (mtime, path, jumlah_cocok_per_pola).
    """
    newest = 0.0
    newest_path: Path | None = None
    counts: dict[str, int] = {}
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
    for pattern in _WATCHED_PATTERNS:
        count = 0
        for path in root.glob(pattern):
            if "__pycache__" in path.parts:
                continue
            count += 1
            mtime = path.stat().st_mtime
            if mtime > newest:
                newest, newest_path = mtime, path
        counts[pattern] = count
    return newest, newest_path, counts


def check(exe: Path, source_root: Path) -> tuple[int, str]:
    """(exit_code, pesan) — 0 berarti aman."""
    if not exe.exists():
        return 2, f"EXAMVAN.exe tidak ada: {exe}"
    bad = verify_artifact(exe)
    if bad is not None:
        return 2, bad
    newest, newest_path, counts = newest_source_mtime(source_root)
    watched = ", ".join(f"{pat}={counts[pat]}" for pat in _WATCHED_PATTERNS)
    empty = [pat for pat in _WATCHED_PATTERNS if counts[pat] == 0]
    if empty:
        return 2, (
            f"pola yang diawasi tidak cocok dengan file apa pun: "
            f"{', '.join(empty)} — guard buta terhadap perubahan di sana.\n"
            f"       Cocok per pola: {watched}"
        )
    if newest_path is None:
        return 2, f"tidak ada source di bawah {source_root} — tidak bisa memastikan"
    exe_mtime = exe.stat().st_mtime
    if exe_mtime >= newest:
        return 0, (
            f"OK: EXAMVAN.exe ({exe_mtime:.0f}) >= source ({newest:.0f}). "
            f"Cocok per pola: {watched}"
        )
    return 1, (
        f"EXAMVAN.exe ({exe_mtime:.0f}) lebih tua dari source "
        f"({newest:.0f}, {newest_path.name}).\n"
        f"       Build akan mengemas kode LAMA dan student menerimanya.\n"
        f"       Jalankan dulu:  windows\\build-exe.bat\n"
        f"       Cocok per pola: {watched}"
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
