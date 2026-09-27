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
    # Baris komentar di APP_VERSION ("# Sent as X-App-Version ...")
    # harus ikut terbuang, makanya ambil yang di dalam kutip pertama.
    m = re.search(r'^\s*APP_VERSION\s*=\s*["\']([^"\']+)["\']', text, re.M)
    return m.group(1) if m else FALLBACK_VERSION


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
    while len(nums) < parts:
        nums.append(0)
    # Inno menerima tiap bagian 0..255 untuk VersionInfoVersion.
    return ".".join(str(min(n, 255)) for n in nums[:parts])


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



def main() -> int:
    values = resolve()
    print(" ".join(f"/D{k}={v}" for k, v in values.items()))
    return 0


if __name__ == "__main__":
    sys.exit(main())
