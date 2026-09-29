"""Rantai build Windows: packaging bisa diam-diam rusak atau basi.

Sembilan temuan dari review_windows_2026-09-29.md Bagian 6, masih terbuka
sampai ronde ini. Semuanya "latent" — tidak merusak apa pun hari ini — tapi
kelas bug yang sama sudah berulang kali menggigit proyek ini di Linux, dan
semuanya bisa dicegah dengan pemeriksaan murah.

Kenapa diuji dari Python, bukan dijalankan
------------------------------------------
Tidak ada host Windows, tidak ada ISCC, tidak ada PowerShell di lingkungan
ini. Test di sini membaca skrip dan workflow sebagai TEKS dan hanya
mengeksekusikan bagian yang memang murni Python. Yang dijaga adalah
kontraknya (guard ada, exit code diperiksa, dependency tidak di-hardcode),
bukan implementasi cmd.exe — dan itu yang biasanya salah.

R6  Smoke test hanya assert proses tidak exit 12 detik.              :185-197
    Job Windows tidak punya padanan apa pun dengan job Linux yang
    memverifikasi isi paket (`ci.yml:129-152`) — dan paket .deb sudah
    pernah beberapa kali terkirim tanpa notify.py / ws.py /
    security/base.py / ui/waiting_approval.py.
R7  build-setup mengemas exe basi. Hanya menguji `-exist`.          build-setup.*
    Bukti di working tree: windows/dist/EXAMVAN.exe mtime 27 Sep,
    source terbaru 30 Sep — build hari ini akan mencetak BUILD SUCCESS
    dan mengemas kode 3 hari lalu.
R8  Rantai Windows mengabaikan desktop/requirements.txt.            semua skrip
    Menambah dependency ke sana tidak berefek pada artefak Windows mana pun.
R9  AppMutex tidak pernah bisa menyala — tidak ada CreateMutex di app.
R10 VCRedistPresent tidak mengecek vcruntime140_1.dll.
R11 run.bat / run.ps1 jalan meski pip install gagal.
R12 build-exe.ps1 tidak cek exit code pip; pesan errornya salah ketik.
R13 --add-data tidak dibaca apa pun; ia meng-embed __pycache__ stale.
"""

from __future__ import annotations

import re
import subprocess
import sys
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]
WINDOWS = REPO / "windows"
CI = REPO / ".github/workflows"
REQUIREMENTS = REPO / "desktop/requirements.txt"


def _read(rel: str) -> str:
    return (REPO / rel).read_text(encoding="utf-8")


def _bat(rel: str) -> str:
    return _read(rel)


# ---------------------------------------------------------------------------
# R6 — smoke test harus menguji sesuatu yang bisa rusak
# ---------------------------------------------------------------------------


class SmokeTestDepthTest(unittest.TestCase):
    def setUp(self):
        self.workflow = _read(".github/workflows/build-windows.yml")
        # Potong mulai dari STEP smoke test-nya, bukan dari kata "smoke"
        # pertama di file — komentar dan step lain juga memuat kata itu.
        marker = "- name: Smoke test"
        self.assertIn(marker, self.workflow, "step smoke test tidak ditemukan")
        self.smoke = self.workflow[self.workflow.index(marker):]

    def test_smoke_test_checks_the_log_not_just_liveness(self):
        # Yang terjadi dalam 12 detik itu: dialog konfigurasi + Qt event
        # loop. Tidak ada network, tidak ada api.py, tidak ada ws.py, tidak
        # ada fitz, tidak ada security backend. Semua komponen yang paling
        # rawan rusak oleh kesalahan packaging TIDAK pernah dieksekusi.
        self.assertRegex(
            self.smoke, r"app\.log",
            "smoke test tidak membaca app.log — lazy import yang rusak lolos",
        )

    def test_smoke_test_asserts_init_completed(self):
        self.assertRegex(
            self.smoke, r"Get-Content.*app\.log",
            "smoke test tidak memeriksa isi log",
        )

    def test_shortcut_absence_fails_the_build(self):
        # Dulu hanya Write-Host "WARN: ..." — build tetap hijau.
        self.assertNotRegex(
            self.smoke, r"WARN:\s*shortcut",
            "shortcut hilang hanya memberi peringatan, bukan kegagalan",
        )

    def test_uninstaller_exit_code_is_asserted(self):
        self.assertRegex(
            self.smoke, r"\$u\.ExitCode",
            "exit code uninstaller tidak di-assert",
        )

    def test_windows_job_has_a_content_verification_step(self):
        # Padanan job Linux (ci.yml:129-152) yang selama ini tidak ada di
        # Windows sama sekali.
        self.assertRegex(
            self.workflow, r"Verify EXAMVAN\.exe contents",
            "tidak ada job verifikasi isi exe di Windows",
        )


# ---------------------------------------------------------------------------
# R7 — build-setup tidak boleh mengemas exe basi
# ---------------------------------------------------------------------------


class StaleExeGuardTest(unittest.TestCase):
    def test_build_setup_bat_invokes_the_freshness_check(self):
        # Baris yang DI-COMMENT tidak dihitung — kalau tidak, guard yang
        # sudah dinonaktifkan akan tetap "lolos" karena namanya masih ada.
        code = _code_only(_bat("windows/build-setup.bat"))
        self.assertIn(
            "check_exe_freshness", code,
            "build-setup.bat hanya menguji -exist",
        )

    def test_build_setup_ps1_invokes_the_freshness_check(self):
        code = _code_only(_bat("windows/build-setup.ps1"))
        self.assertIn(
            "check_exe_freshness", code,
            "build-setup.ps1 hanya menguji Test-Path",
        )

    def test_the_invocation_is_guarded_by_an_exit_code_check(self):
        # CMD pakai `errorlevel`, PowerShell pakai `$LASTEXITCODE`.
        for rel, needle in (
            ("windows/build-setup.bat", "errorlevel"),
            ("windows/build-setup.ps1", "LASTEXITCODE"),
        ):
            code = _code_only(_bat(rel))
            self.assertIn(needle, code, rel)

    def test_powershell_scripts_define_every_variable_they_use(self):
        # bug nyata yang bisa masuk diam-diam: memakai $ProjectRoot di
        # build-setup.ps1, yang hanya mendefinisikan $ScriptDir.
        for rel in ("windows/build-setup.ps1", "windows/build-exe.ps1",
                    "windows/run.ps1", "windows/install.ps1"):
            code = _code_only(_bat(rel))
            defined = set(re.findall(r"^\$(\w+)\s*=", code, re.MULTILINE))
            used = set(re.findall(r"\$ProjectRoot\b", code))
            if used and "ProjectRoot" not in defined:
                self.fail(f"{rel} memakai $ProjectRoot tapi tidak mendefinisikannya")

    def test_the_checker_compares_against_the_newest_source(self):
        src = _bat("windows/installer/check_exe_freshness.py")
        self.assertIn("newest_source_mtime", src)
        self.assertIn("return 1", src)

    def test_comparison_is_against_the_newest_source(self):
        for rel in ("windows/build-setup.bat", "windows/build-setup.ps1"):
            src = _bat(rel)
            self.assertRegex(
                src, r"examvan|desktop",
                f"{rel} tidak tahu di mana source-nya",
            )

    def test_guard_actually_fails_the_build(self):
        for rel in ("windows/build-setup.bat", "windows/build-setup.ps1"):
            src = _bat(rel)
            self.assertRegex(
                src, r"exit /b 1|exit 1",
                f"{rel} tidak keluar dengan kode error saat guard gagal",
            )

    def test_checked_in_exe_is_not_older_than_the_sources(self):
        # Bukti langsung di working tree. Kalau ini gagal, artefak yang
        # tersimpan memang sudah basi dan TIDAK BOLEH dibagikan.
        exe = WINDOWS / "dist" / "EXAMVAN.exe"
        if not exe.exists():
            self.skipTest("windows/dist/EXAMVAN.exe tidak ada di checkout ini")
        exe_mtime = exe.stat().st_mtime
        newest = 0.0
        for path in (REPO / "desktop/examvan").rglob("*.py"):
            newest = max(newest, path.stat().st_mtime)
        self.assertGreater(
            newest, 0, "tidak menemukan source .py")
        self.assertLess(
            exe_mtime, newest,
            "EXAMVAN.exe lebih tua dari source — build-setup akan mengemas "
            "kode lama dan mencetak BUILD SUCCESS",
        )


# ---------------------------------------------------------------------------
# R8 — requirements.txt harus dipakai
# ---------------------------------------------------------------------------


class RequirementsAreUsedTest(unittest.TestCase):
    SCRIPTS = [
        "windows/build-exe.bat",
        "windows/build-exe.ps1",
        "windows/install.bat",
        "windows/install.ps1",
        ".github/workflows/build-windows.yml",
    ]

    def test_requirements_file_exists(self):
        self.assertTrue(REQUIREMENTS.exists())

    def test_no_windows_script_hardcodes_the_dependency_list(self):
        for rel in self.SCRIPTS:
            src = _read(rel)
            self.assertNotIn("pip install PyQt5 PyMuPDF", src, rel)

    def test_windows_scripts_consume_the_requirements_file(self):
        # Tidak semua harus memakainya, tapi yang memasang dependency
        # wajib memakai satu sumber, kalau tidak menambah dependency ke
        # requirements.txt tidak berefek di Windows.
        for rel in self.SCRIPTS:
            src = _read(rel)
            if "pip install" in src or "requirements.txt" in src:
                self.assertIn("requirements.txt", src, rel)

    def test_linux_chain_still_uses_it(self):
        for rel in ("desktop/install.sh", "desktop/run.sh", "desktop/build-deb.sh"):
            self.assertIn("requirements.txt", _read(rel), rel)


# ---------------------------------------------------------------------------
# R9 — AppMutex tidak pernah bisa menyala
# ---------------------------------------------------------------------------


def _sibling(name: str):
    """Muat modul test lain dari direktori yang sama.

    `discover -s tests` menaruh `tests` di sys.path, tapi
    `python -m unittest tests.test_...` tidak — jadi impor langsung tidak
    selalu bekerja. Muat lewat path supaya dua cara pemanggilan sama.
    """
    import importlib.util

    path = Path(__file__).resolve().parent / f"{name}.py"
    spec = importlib.util.spec_from_file_location(name, path)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


def _code_only(src: str) -> str:
    """Buang baris komentar dari skrip CMD/PowerShell.

    Tanpa ini test "guard dipanggil" akanatisfied oleh baris yang
    di-comment — persis hasil mutasi yang harus tertangkap.
    """
    out = []
    for line in src.splitlines():
        stripped = line.strip()
        if stripped.startswith(("rem ", "REM ", ";", "#")):
            continue
        out.append(line)
    return "\n".join(out)


class AppMutexTest(unittest.TestCase):
    """AppMutex tidak pernah bisa menyala; SetupMutex yang benar.

    Yang diuji adalah DIRECTIVE-nya, bukan kata "AppMutex" di mana pun —
    komentar yang menjelaskan kenapa directive itu salah tetap berguna dan
    harus boleh ada.
    """

    def test_iss_does_not_use_the_app_mutex_directive(self):
        # AppMutex membuat installer MENOLAK jalan selama aplikasi memegang
        # mutex itu, dan mewajibkan aplikasi memanggil CreateMutex dengan
        # nama yang cocok. Tidak ada CreateMutex di desktop/ — saya sudah
        # grep seluruh repo — jadi pemeriksaan itu tidak pernah bisa menyala.
        # Dan AppMutex BUKAN mekanisme mencegah dua installer berjalan
        # bersamaan; itu SetupMutex.
        src = _read("windows/installer/examvan.iss")
        directives = [
            line for line in src.splitlines()
            if re.match(r"^\s*AppMutex\s*=", line)
        ]
        self.assertEqual(directives, [], f"AppMutex masih dipakai: {directives}")

    def test_single_instance_protection_uses_setup_mutex(self):
        src = _read("windows/installer/examvan.iss")
        self.assertTrue(
            re.search(r"^\s*SetupMutex\s*=", src, re.MULTILINE),
            "SetupMutex tidak diset — tidak ada pencegahan dua installer "
            "jalan bersamaan",
        )

    def test_setup_mutex_name_is_defined(self):
        src = _read("windows/installer/examvan.iss")
        self.assertRegex(src, r'#define\s+SetupMutexName\s+"')
        self.assertIn("{#SetupMutexName}", src)


# ---------------------------------------------------------------------------
# R10 — VCRedist check
# ---------------------------------------------------------------------------


class VCRedistTest(unittest.TestCase):
    def test_all_three_runtime_dlls_are_checked(self):
        src = _read("windows/installer/examvan.iss")
        for dll in ("vcruntime140.dll", "msvcp140.dll", "vcruntime140_1.dll"):
            with self.subTest(dll=dll):
                self.assertIn(dll, src, f"{dll} tidak diperiksa")

    def test_the_check_function_tests_all_three(self):
        body_of = _sibling("test_installer_password")._procedure_body

        body = body_of("VCRedistPresent")
        for dll in ("vcruntime140.dll", "msvcp140.dll", "vcruntime140_1.dll"):
            self.assertIn(dll, body, dll)

    def test_the_check_is_actually_called(self):
        src = _read("windows/installer/examvan.iss")
        self.assertRegex(src, r"if not VCRedistPresent\(\) then")


# ---------------------------------------------------------------------------
# R11 / R12 — exit code pip
# ---------------------------------------------------------------------------


class PipExitCodeTest(unittest.TestCase):
    def test_run_bat_checks_pip(self):
        src = _bat("windows/run.bat")
        self.assertRegex(src, r"errorlevel", "run.bat tidak cek pip exit code")

    def test_run_ps1_checks_pip(self):
        src = _bat("windows/run.ps1")
        self.assertIn("$LASTEXITCODE", src, "run.ps1 tidak cek pip exit code")

    def test_build_exe_ps1_checks_pip(self):
        src = _bat("windows/build-exe.ps1")
        self.assertIn("$LASTEXITCODE", src)

    def test_install_ps1_checks_pip(self):
        src = _bat("windows/install.ps1")
        self.assertIn("$LASTEXITCODE", src)

    def test_error_message_names_the_right_exe(self):
        # Dulu: "Build failed — EXAVAN.exe not found" ( salah ketik), yang
        # menyembunyikan penyebab sebenarnya.
        for rel in ("windows/build-exe.bat", "windows/build-exe.ps1"):
            self.assertNotIn("EXAVAN", _read(rel), rel)

    def test_error_action_preference_alone_is_not_enough(self):
        # `$ErrorActionPreference = "Stop"` tidak mengubah exit code non-nol
        # dari program native di Windows PowerShell 5.1.
        src = _bat("windows/build-exe.ps1")
        self.assertIn("$LASTEXITCODE", src)


# ---------------------------------------------------------------------------
# R13 — --add-data yang tidak dibaca apa pun
# ---------------------------------------------------------------------------


class DeadAddDataTest(unittest.TestCase):
    def test_build_exe_bat_does_not_bundle_the_package(self):
        self.assertNotIn("--add-data", _bat("windows/build-exe.bat"))

    def test_build_exe_ps1_does_not_bundle_the_package(self):
        self.assertNotIn("--add-data", _bat("windows/build-exe.ps1"))

    def test_nothing_in_the_package_reads_its_own_bundle(self):
        # `--add-data` ada karena `_MEIPASS` dulu dipakai kiosk.py, yang
        # sekarang dikecualikan dari build. Kalau ini masih ada, jangan
        # pernah dikembalikan tanpa test.
        for path in (REPO / "desktop/examvan").rglob("*.py"):
            if path.name == "kiosk.py":
                continue
            src = path.read_text(encoding="utf-8")
            self.assertNotIn("_MEIPASS", src, str(path))


if __name__ == "__main__":
    unittest.main()
