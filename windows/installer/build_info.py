#!/usr/bin/env python3
"""Print Inno Setup /D defines for EXAMVAN-Setup.exe.

Satu sumber kebenaran untuk nilai yang dibutuhkan build-setup.bat,
build-setup.ps1, dan .github/workflows/build-windows.yml. Ketiga
script itu sebelumnya masing-masing menghitung versi sendiri dan tidak
pernah diuji — persis tempat bug biasanya muncul.

KENAPA FILE INI ADA (bukan sekadar biar rapi):

    VersionInfoVersion di [Setup] HANYA menerima format numerik
    ("1.2.3.4"). Nilai aslinya datang dari `git describe --tags`,
    yang bisa mengembalikan apa saja: "v2.5.0", "release-2026.09",
    "v1.0.0-rc1". Kirim "v2.5.0" ke ISCC dan compile-nya GAGAL
    dengan "Invalid version number" — dan itu baru ketahuan di
    runner Windows, 5 menit setelah push.

Jadi nilai di sini sudah dinormalisasi dan dijamin bisa dipakai:

    /DAppVersion=1.0.0        -> AppVersion, AppVerName (string bebas)
    /DAppVersionInfo=1.0.0.0  -> VersionInfoVersion (WAJIB numerik)
    /DAppBuild=412            -> nomor commit, untuk lacak build
    /DAppCommit=0ca2137       -> short sha

Pemakaian (keluaran = satu baris, tinggal pakai utuh):

    build-setup.bat:
        for /f "usebackq delims=" %%D in (`python build_info.py`) do set "ISCC_DEFINES=%%D"

    build-setup.ps1 / CI:
        $defines = python windows/installer/build_info.py
        & $iscc $defines.Split(' ') windows/installer/examvan.iss

Dijalankan dari root repo ATAU dari folder windows/ — path repo
ditemukan relatif ke file ini, bukan ke CWD.
"""

from __future__ import annotations

import re
import subprocess
import sys
from typing import Optional
from pathlib import Path

# desktop/examvan/__init__.py
REPO_ROOT = Path(__file__).resolve().parents[2]
APP_INIT = REPO_ROOT / "desktop" / "examvan" / "__init__.py"

# Dipakai kalau git tidak tersedia (mis. ZIP yang diunduh tanpa .git).
FALLBACK_VERSION = "1.0.0"
FALLBACK_BUILD = "0"
FALLBACK_COMMIT = "local"


def _git(*args: str) -> str:
    """Return stripped stdout of a git command, or "" on any failure."""
    try:
        out = subprocess.run(
            ["git", *args],
            cwd=str(REPO_ROOT),
            capture_output=True,
            text=True,
            timeout=15,
        )
    except (OSError, subprocess.SubprocessError):
        return ""
    return out.stdout.strip() if out.returncode == 0 else ""


def version_from_source() -> str:
    """Ambil APP_VERSION dari examvan/__init__.py (mis. "2.5.0")."""
    try:
        text = APP_INIT.read_text(encoding="utf-8")
    except OSError:
        return FALLBACK_VERSION
    # Dua bentuk yang mungkin muncul di examvan/__init__.py:
    #   APP_VERSION = "2.5.0"        -> literal
    #   APP_VERSION = __version__    -> alias ke __version__ (yang literal)
    # build_info.py TIDAK mengeksekusi source itu, jadi keduanya harus
    # didukung. Dulu hanya bentuk pertama yang dibaca; begitu __init__.py
    # berubah jadi alias, fungsi ini diam-diam jatuh ke FALLBACK_VERSION
    # 1.0.0 dan installer ikut berlabel 1.0.0.
    m = re.search(r'^\s*APP_VERSION\s*=\s*["\']([^"\']+)["\']', text, re.M)
    if m:
        return m.group(1)
    m = re.search(r'^\s*__version__\s*=\s*["\']([^"\']+)["\']', text, re.M)
    if m:
        return m.group(1)
    return FALLBACK_VERSION


def numeric_version(raw: str, parts: int = 4) -> str:
    """Jamin '1.2.3.4' dari input bebas seperti 'v2.5.0' atau 'v1.0.0-rc1'.

    Dua langkah, dan urutan itu penting:

    1. Cari pola versi yang menempel di AWAL string, opsional 'v'/'V'
       ("v2.5.0", "2.5.0.1", "1.2"). Kalau tidak ada, baru cari pola
       versi di tengah string ("release-2026.09").
    2. Isi sisanya dengan 0.

    Kenapa tidak sekadar memotong digit: input "v1.0.0-rc1" akan
    memberi 1.0.0 lalu angka "1" dari "rc1" ikut jadi komponen
    keempat — installer jadi berlabel "1.0.0.1" untuk rilis yang
    sebenarnya 1.0.0. Pola di Anchor biar sufiks rilis tidak ikut
    terhitung. Kalau sama sekali tidak ada pola versi, jatuh ke
    FALLBACK_VERSION: versi salah lebih baik daripada build gagal.
    """
    text = (raw or "").strip()
    # Dua pola dengan STRUKTUR GRUP YANG SAMA (4 grup) supaya
    # m.groups() bisa di-split uniform di bawah. Versi "longgar"
    # (tengah string, mis. "release-2026.09") harus punya grup
    # per bagian juga — kalau cuma satu grup berisi "2026.09",
    # int() di bawah akan gagal.
    tail = r"(?:\.(\d{1,5}))?(?:\.(\d{1,5}))?(?:\.(\d{1,5}))?"
    patterns = (
        re.compile(r"^v?(\d{1,5})" + tail),                      # v2.5.0 / 1.2.3.4
        re.compile(r"(?<![\d.])(\d{1,5})" + tail + r"(?![\d.])"),  # release-2026.09
    )
    nums: list[int] = []
    for pattern in patterns:
        m = pattern.search(text)
        if m:
            nums = [int(g) for g in m.groups() if g]
            break
    if not nums:
        nums = [int(x) for x in FALLBACK_VERSION.split(".")[:parts]]
    # Inno menerima tiap bagian 0..255 untuk VersionInfoVersion, jadi
    # komponen yang lebih besar dari itu harus DIBUANG — bukan disaturasi
    # jadi 255.
    #
    # `min(n, 255)` dulu membuat 'release-2026.09' menjadi 255.9.0: bukan
    # versi, bukan tahun, dan tampak "sudah di-clamp dengan benar" padahal
    # 255 tidak pernah muncul pada nomor versi mana pun. Komponen yang
    # melebihi batas biasanya yang PERTAMA (tahun), jadi buang dari depan
    # sampai muat: 'release-2026.09' -> 9.0.0, bukan 255.9.0.
    while len(nums) > 1 and nums[0] > 255:
        nums.pop(0)
    while len(nums) > parts:
        nums.pop()

    while len(nums) < parts:
        nums.append(0)
    return ".".join(str(n) for n in nums[:parts])


def resolve() -> dict[str, str]:
    """Tentukan keempat nilai define.

    Sumber versi SENGAJA bukan `git describe --tags`: yang jadi acuan
    adalah APP_VERSION di examvan/__init__.py, karena angka itulah yang
    dilaporkan app ke server lewat header X-App-Version. Kalau installer
    mengiklankan versi tag git sementara app melaporkan angka lain,
    laporan bug dari guru ("versi berapa di PC saya?") jadi tidak
    bisa dipercaya. Git hanya dipakai untuk build + commit.
    """
    display = version_from_source()
    build = _git("rev-list", "--count", "HEAD")
    commit = _git("rev-parse", "--short", "HEAD")
    return {
        "AppVersion": numeric_version(display, parts=3),
        "AppVersionInfo": numeric_version(display, parts=4),
        "AppBuild": (build if build.isdigit() else FALLBACK_BUILD),
        "AppCommit": re.sub(r"[^0-9A-Za-z]", "", commit) or FALLBACK_COMMIT,
    }



def stamp_version_info(
    source: Path,
    dest: Path,
    version: str,
    build: str = "",
    commit: str = "",
) -> Path:
    """Tulis salinan `version_info.txt` dengan SEMUA field versi di-stempel.

    Kenapa fungsi ini perlu ada, dan tidak cukup di-patch di CI saja:

    * `FixedFileInfo.filevers/prodvers` tidak pernah disentuh. CI hanya
      men-*patch* dua nilai `StringStruct`, jadi biner setiap EXAMVAN.exe
      adalah 1.0.0.0 selamanya sementara `VersionInfoVersion` di `.iss`
      adalah 2.5.0.0. Add/Remove Programs menampilkan 2.5.0, Properties
      exe berbunyi 1.0.0.0.
    * Build lokal (`build-exe.bat` / `build-exe.ps1`) mengirim file itu
      tanpa perubahan sama sekali, jadi exe lokal melaporkan 1.0.0 dan
      tidak membawa build maupun commit.

    Dua implementasi dari aturan yang sama, dan yang lokal diam-diam
    salah. Sekarang ketiganya — CI, .bat, .ps1 — lewat fungsi ini.
    """
    text = source.read_text(encoding="utf-8")
    display = numeric_version(version, parts=3)
    # FixedFileInfo butuh TUPLE, bukan string bertitik: (2, 5, 0, 0).
    four = ", ".join(numeric_version(version, parts=4).split("."))

    product = display + (f" (build {build}, commit {commit})" if build and commit else "")

    text = re.sub(
        r"(StringStruct\('FileVersion',\s*')[^']*(')",
        rf"\g<1>{display}\g<2>",
        text,
    )
    text = re.sub(
        r"(StringStruct\('ProductVersion',\s*')[^']*(')",
        rf"\g<1>{product}\g<2>",
        text,
    )
    # FixedFileInfo — bagian yang dulu terlewat.
    text = re.sub(r"filevers=\([^)]*\)", f"filevers=({four})", text)
    text = re.sub(r"prodvers=\([^)]*\)", f"prodvers=({four})", text)

    dest.parent.mkdir(parents=True, exist_ok=True)
    dest.write_text(text, encoding="utf-8")
    return dest


def _cmd_stamp(argv: Optional[list] = None) -> int:
    """`build_info.py stamp <src> <dest> [--version ...]` — dipakai semua jalur build.

    `argv` diterima sebagai parameter (bukan dibaca dari `sys.argv` di dalam
    fungsi) supaya jalur CLI — yang justru yang dipanggil CI dan
    build-exe.bat — bisa diuji seperti fungsi biasa. Versi sebelumnya
    tidak menerima argumen tapi dipanggil `_cmd_stamp(argv[1:])`, jadi build
    CI langsung gagal dengan TypeError. Test sebelumnya hanya memanggil
    `stamp_version_info()` langsung, sehingga jalur yang benar-benar dipakai
    tidak pernah dieksekusi.
    """
    import argparse

    ap = argparse.ArgumentParser(prog="build_info.py stamp")
    ap.add_argument("source", type=Path)
    ap.add_argument("dest", type=Path)
    ap.add_argument("--version", default=None)
    ap.add_argument("--build", default=None)
    ap.add_argument("--commit", default=None)
    args = ap.parse_args(list(sys.argv[1:] if argv is None else argv))

    values = resolve()
    stamp_version_info(
        source=args.source,
        dest=args.dest,
        version=args.version or values["AppVersion"],
        build=args.build if args.build is not None else values["AppBuild"],
        commit=args.commit if args.commit is not None else values["AppCommit"],
    )
    return 0


def main(argv: Optional[list] = None) -> int:
    argv = list(sys.argv[1:] if argv is None else argv)
    if argv and argv[0] == "stamp":
        return _cmd_stamp(argv[1:])
    values = resolve()
    print(" ".join(f"/D{k}={v}" for k, v in values.items()))
    return 0


if __name__ == "__main__":
    sys.exit(main())
