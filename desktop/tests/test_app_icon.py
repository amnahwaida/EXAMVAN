"""Ikon EXAMVAN harus ikut ke .exe, installer, dan shortcut.

Permintaan lapangan (30 September 2026): "perbaiki icon setelah file .exe
berhasil terpasang, memakai icon EXAMVAN yang asli."

Keadaan SEBELUM perbaikan ini, diverifikasi terhadap repo:

  * Tidak ada file `.ico` sama sekali di repo.
  * Tidak ada satu pun `--icon` di build-exe.bat / build-exe.ps1 /
    build-windows.yml, jadi `EXAMVAN.exe` memakai ikon default PyInstaller.
  * `examvan.iss:117-118` menulis:

        Icon TIDAK dikopi terpisah. Shortcut mengambil icon dari
        EXAMVAN.exe itu sendiri (IconFilename tidak di-set = default)

    Pernyataan itu benar sebagai mekanika Inno Setup, tapi karena exe-nya
    sendiri tidak punya ikon, hasilnya: shortcut, Start Menu, desktop, dan
    Add/Remove Programs semuanya menampilkan ikon PyInstaller.
  * `EXAMVAN-Setup.exe` juga tanpa ikon (`SetupIconFile` tidak di-set).

Ikon aslinya sudah ADA di repo, di klien Android:
`android/app/src/main/res/mipmap-xxxhdpi/ic_launcher.png` — 192x192 RGBA,
putih + ungu EXAMVAN (#9958F5), dengan transparansi. Jadi tidak perlu
menggambar baru; yang perlu adalah menjadikannya `.ico` multi-resolusi
yang bisa dipakai Windows.

Kenapa `.ico` di-commit, bukan di-generate saat build
-----------------------------------------------------
Windows butuh `.ico` multi-ukuran (16/24/32/48/64/128/256). Generate-nya
butuh ImageMagick atau Pillow, dan KEDUA tidak boleh jadi syarat build di
PC guru/siswa maupun di runner CI. Jadi `.ico` yang sudah jadi di-commit
sbg sumber kebenaran, dan `make_icon.py` hanya perlu dijalankan saat ikon
brand berubah.
"""

from __future__ import annotations

import re
import struct
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]
ICO = REPO / "windows/installer/examvan.ico"
ISS = REPO / "windows/installer/examvan.iss"
ANDROID_ICON = REPO / "android/app/src/main/res/mipmap-xxxhdpi/ic_launcher.png"
MAKE_ICON = REPO / "windows/installer/make_icon.py"

# Shortcut yang dibuat installer. Semua harus memakai ikon EXAMVAN.
SHORTCUTS = (
    "{group}\\{#AppName}",
    "{autodesktop}\\{#AppName}",
    "{group}\\Uninstall {#AppName}",
)


def _read(rel: str) -> str:
    return (REPO / rel).read_text(encoding="utf-8")


def _ico_entries(data: bytes):
    """Baca directory sebuah file .ico: [(width, height, bpp, bytes), ...]."""
    if data[:4] != b"\x00\x00\x01\x00":
        raise AssertionError(f"bukan file .ico: {data[:4]!r}")
    (count,) = struct.unpack("<H", data[4:6])
    out = []
    for i in range(count):
        off = 6 + i * 16
        w, h, colors, _res, planes, bpp, size, offset = struct.unpack(
            "<BBBBHHII", data[off:off + 16]
        )
        # 0 berarti 256
        out.append({
            "w": w or 256, "h": h or 256, "bpp": bpp,
            "size": size, "offset": offset,
            "data": data[offset:offset + size],
        })
    return out


# ---------------------------------------------------------------------------
# 1. Berkas .ico-nya benar-benar ada dan berbentuk .ico yang sah
# ---------------------------------------------------------------------------


class IconFileTest(unittest.TestCase):
    def setUp(self):
        self.data = ICO.read_bytes()

    def test_ico_file_exists(self):
        self.assertTrue(ICO.exists(), f"{ICO} tidak ada")
        self.assertGreater(len(self.data), 1024, "file .ico terlalu kecil")

    def test_is_a_valid_ico_header(self):
        # reserved=0, type=1 (icon), >=1 image
        reserved, kind, count = struct.unpack("<HHH", self.data[:6])
        self.assertEqual(reserved, 0)
        self.assertEqual(kind, 1)
        self.assertGreaterEqual(count, 1)

    def test_has_every_size_windows_actually_uses(self):
        # 16/32/48 untuk shell & taskbar, 24 untuk bar besar, 64/128 untuk
        # daftar bertingkat, 256 untuk Explorer & DPI tinggi. Tanpa 256,
        # Windows akan MEMPERBESAR gambar piksel terdekat dari 192 dan
        # ikon terlihat pecah di layar 4K.
        sizes = {e["w"] for e in _ico_entries(self.data)}
        for size in (16, 24, 32, 48, 64, 128, 256):
            with self.subTest(size=size):
                self.assertIn(size, sizes)

    def test_entries_are_32bit_with_alpha(self):
        # 16-bitcolor ICONDIRENTRY = 32 bpp (BGRA). Kurang dari itu bikin
        # ikon tepi bergerigi di Aero/Windows 10+.
        for e in _ico_entries(self.data):
            self.assertGreaterEqual(e["bpp"], 32, f"{e['w']}px hanya {e['bpp']}bpp")

    def test_every_entry_is_non_trivially_sized(self):
        for e in _ico_entries(self.data):
            self.assertGreater(e["size"], 100, f"{e['w']}px kecil sekali ({e['bpp']}bpp)")

    def test_256_entry_is_not_just_the_192_upscaled_nearest(self):
        # Penjaga kasar: entri 256 harus benar-benar lebih besar dari
        # 192 agar tidak ada gambar piksel terdekat yang dipoles.
        e256 = next(e for e in _ico_entries(self.data) if e["w"] == 256)
        self.assertGreater(e256["size"], 4096)

    def test_not_a_placeholder_image(self):
        # Ikon default PyInstaller hanya beberapa ratus byte dan membuat
        # file kecil; ikon brand asli jelas lebih besar.
        self.assertGreater(len(self.data), 8000)

    def test_derived_from_the_android_launcher_icon(self):
        # Ikon harus milik EXAMVAN, bukan gambar arbitrary yang sengaja
        # ditumpuk. Sumbernya sudah ada di repo.
        self.assertTrue(ANDROID_ICON.exists(), f"sumber ikon hilang: {ANDROID_ICON}")


# ---------------------------------------------------------------------------
# 2. PyInstaller memakai ikon itu
# ---------------------------------------------------------------------------


class PyInstallerIconTest(unittest.TestCase):
    BUILD_PATHS = (
        "windows/build-exe.bat",
        "windows/build-exe.ps1",
        ".github/workflows/build-windows.yml",
    )

    def test_every_build_path_passes_the_icon(self):
        for rel in self.BUILD_PATHS:
            self.assertIn("--icon", _read(rel), f"{rel} tidak mengeset ikon")

    def test_every_build_path_points_at_the_committed_ico(self):
        for rel in self.BUILD_PATHS:
            src = _read(rel)
            self.assertIn("examvan.ico", src, f"{rel} tidak menunjuk .ico yang di-commit")

    def test_icon_path_is_relative_to_the_build_working_directory(self):
        # `--add-data`/path relatif PyInstaller relatif ke CWD (desktop\).
        # Path yang salah = build sukses dengan ikon default, tanpa error.
        src = _read("windows/build-exe.bat")
        m = re.search(r'--icon\s+"([^"]+)"', src)
        self.assertIsNotNone(m)
        self.assertIn("..\\windows\\installer\\examvan.ico", m.group(1))

    def test_ci_uses_the_same_ico(self):
        src = _read(".github/workflows/build-windows.yml")
        self.assertIn("windows/installer/examvan.ico", src)


# ---------------------------------------------------------------------------
# 3. Installer & shortcut
# ---------------------------------------------------------------------------


class InstallerIconTest(unittest.TestCase):
    def setUp(self):
        self.iss = _read("windows/installer/examvan.iss")

    def test_setup_exe_itself_gets_the_icon(self):
        # Tanpa SetupIconFile, EXAMVAN-Setup.exe — file yang benar-benar
        # dibagikan ke siswa dan diklik dua kali — menampilkan ikon default
        # Inno Setup, bukan ikon EXAMVAN.
        self.assertRegex(self.iss, r"(?m)^SetupIconFile\s*=")

    def test_setup_icon_file_is_bundled_by_the_build(self):
        # Inno hanya bisa memakai file yang ada di disk saat compile, dan
        # tidak boleh membawanya ke instalasi (dipakai dari [Setup], bukan
        # [Files]) — harus Flags: dontcopy.
        # [Files] memakai define yang sama seperti [Icons]/[Setup].
        m = re.search(
            r'(?m)^Source:\s*"\{#AppIconName\}";(?P<body>[^\n]*)', self.iss,
        )
        self.assertIsNotNone(m, "{#AppIconName} tidak ada di [Files]")
        self.assertRegex(m.group("body"), r"DestDir:\s*\"\{app\}\"")
        self.assertIn(
            "dontcopy", m.group("body"),
            "tanpa dontcopy, ikon jadi file aplikasi yang harus dibersihkan "
            "uninstaller padahal tidak ada yang memakainya saat runtime",
        )

    def test_icon_define_points_at_the_committed_file(self):
        # Shortcut & SetupIconFile memakai define, bukan literal, supaya
        # nama file ada di satu tempat. Rantainya harus sampai ke .ico
        # yang benar-benar di-commit.
        m = re.search(r'^#define\s+AppIconName\s+"([^"]+)"', self.iss, re.MULTILINE)
        self.assertIsNotNone(m, "#define AppIconName tidak ada")
        self.assertEqual(m.group(1), "examvan.ico")
        self.assertTrue(ICO.exists(), f"{m.group(1)} tidak ada di repo")

    def test_setup_icon_file_uses_the_define(self):
        m = re.search(r"^SetupIconFile\s*=\s*(\S+)", self.iss, re.MULTILINE)
        self.assertIsNotNone(m, "SetupIconFile tidak ada")
        self.assertIn("AppIconName", m.group(1))

    def test_every_shortcut_uses_the_icon_explicitly(self):
        # Shortcut mewarisi ikon dari exe-nya, jadi secara teknis tidak
        # wajib. Tapi IconFilename eksplisit lebih tahan: kalau build exe
        # gagal diam-diam sehingga ikon default ikut ter-bundle, shortcut
        # minimal masih memakai ikon EXAMVAN.
        for path in SHORTCUTS:
            with self.subTest(shortcut=path):
                # Baris Inno: Name: "<path>"; Filename: ...; IconFilename: ...
                m = re.search(
                    rf'^Name:\s*"{re.escape(path)}";(?P<body>[^\n]*)',
                    self.iss, re.MULTILINE,
                )
                self.assertIsNotNone(m, f"shortcut {path} tidak ada")
                self.assertRegex(
                    m.group("body"), r"IconFilename:\s*\"\{app\}\\?\{#AppIconName\}\"",
                    f"shortcut {path} tidak menyetel IconFilename ke ikon EXAMVAN",
                )

    def test_icon_file_is_actually_bundled(self):
        # Ikon harus ikut installer, kalau tidak `dontcopy` menulis icon
        # ke folder yang dihapus uninstaller.
        self.assertIn("examvan.ico", self.iss)

    def test_stale_icon_comment_is_gone(self):
        # Komentar lama mengklaim ikon sengaja tidak dikopi dan shortcut
        # "mengambil default" — sekarang tidak akurat dan akan membuat
        # pengembang berikutnya meniruJELEGANCE yang keliru.
        self.assertNotIn(
            "Icon TIDAK dikopi terpisah", self.iss,
            "komentar tentang ikon sudah usang",
        )


# ---------------------------------------------------------------------------
# 4. Skrip regenerasi
# ---------------------------------------------------------------------------


class MakeIconScriptTest(unittest.TestCase):
    def test_script_exists_and_is_documented(self):
        self.assertTrue(MAKE_ICON.exists(), f"{MAKE_ICON} tidak ada")
        src = MAKE_ICON.read_text(encoding="utf-8")
        self.assertIn("android/app/src/main/res", src,
                      "skrip harus menyebut sumber ikon Android")

    def test_script_targets_every_required_size(self):
        src = MAKE_ICON.read_text(encoding="utf-8")
        for size in (16, 24, 32, 48, 64, 128, 256):
            self.assertIn(str(size), src, f"ukuran {size} tidak ada di skrip")

    def test_script_never_gates_the_build(self):
        # Build TIDAK boleh memanggil skrip ini; kalau iya, gambar icon
        # menjadi syarat build.
        for rel in ("windows/build-exe.bat", "windows/build-exe.ps1",
                    "windows/build-setup.bat", "windows/build-setup.ps1",
                    ".github/workflows/build-windows.yml"):
            self.assertNotIn("make_icon", _read(rel), f"{rel} memanggil make_icon")


if __name__ == "__main__":
    unittest.main()
