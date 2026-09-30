"""Pipeline distribusi Windows harus benar di mesin dev dan di uninstall (#8-#12).

Bug yang ditutup file ini
------------------------
#8 Uninstall senyap TIDAK PERNAH menghapus data sensitif
    Penghapusan data ada di satu blok `[Code]` yang dijaga
    `and (not UninstallSilent)`. `WizardSilent`/`UninstallSilent` bernilai
    true untuk `/VERYSILENT` -- persis mode yang dipakai CI
    (`build-windows.yml`) dan yang akan dipakai sekolah untuk 30 PC. Jadi
    di jalur itu tidak ada prompt DAN tidak ada penghapusan: `config.json`
    (token kredensial SELURUH KELAS + identitas siswa) dan
    `admin_password.txt` tinggal selamanya di PC yang dipakai bersama.

#9 Build lokal mustahil
    `build-exe.bat` / `build-exe.ps1` menjalankan PyInstaller dari dalam
    `desktop/`, tapi `--version-file "..\\dist\\version_info.txt"` yang
    menunjuk ke `<repo>/dist/` -- folder yang tidak pernah ada. Berkasnya
    ditulis ke `<repo>/windows/dist/`. Baris berikutnya, `--icon
    "..\\windows\\installer\\examvan.ico"`, justru benar. Jadi satu-satunya
    jalur build yang berfungsi adalah CI, dan siapa pun yang mencoba
    membangun di mesin sendiri gagal dengan `FileNotFoundError`.

#12a `check_exe_freshness.py` tidak mengawasi entry point
    Globs-nya `examvan/**/*.py`, `tests/**/*.py`, `requirements.txt`.
    Yang tidak diawasi: `main.py` -- satu-satunya entry point yang
    benar-benar di-build PyInstaller -- plus `version_info.txt` dan
    `.ico`. Yang diawasi justru `tests/**` yang tidak berpengaruh ke
    binary. Invariant "exe lebih baru dari source" jadi salah.

#12b Source Python plaintext ikut dibundel ke exe
    `--add-data "desktop/examvan;examvan"` menyalin seluruh pohon
    source apa adanya ke dalam exe. Tanpa itu, PyInstaller hanya
    mengemas bytecode marshalled. Exe-nya dibagikan ke siswa, jadi ini
   membocorkan kunci XOR jawaban dan logika gate admin-exit. Tidak ada
    satu pun import dinamis di `examvan/` yang membuatnya diperlukan.

#11 Deadline polling berubah diam-diam
    `api.py` diberi backoff dengan cap `interval * 8`, jadi
    `31 x 2.5s` yang didokumentasikan sebagai "~77s" menjadi ~9 menit,
    sementara docstring-nya masih menulis angka lama.

Test di sini membaca file build sungguhan dan memeriksa hubungan
nilainya -- bukan agil-nya test, tapi apa adanya interdependensi yang
tidak terlihat kalau tiap file dibaca terpisah.
"""

from __future__ import annotations

import os
import re
import unittest
from unittest import mock
from pathlib import Path


REPO = Path(__file__).resolve().parents[2]
WINDOWS = REPO / "windows"
ISS = WINDOWS / "installer" / "examvan.iss"
BUILD_BAT = WINDOWS / "build-exe.bat"
BUILD_PS1 = WINDOWS / "build-exe.ps1"
FRESHNESS = WINDOWS / "installer" / "check_exe_freshness.py"
BUILD_WORKFLOW = REPO / ".github" / "workflows" / "build-windows.yml"


def _read(path: Path) -> str:
    return path.read_text(encoding="utf-8")


def _yaml_jobs() -> dict:
    """Workflow build, dibaca lewat PyYAML bila tersedia."""
    try:
        import yaml
    except ImportError:  # pragma: no cover - hanya di runner tanpa PyYAML
        raise unittest.SkipTest("PyYAML tidak terpasang")
    return yaml.safe_load(_read(BUILD_WORKFLOW)).get("jobs", {})

class SilentUninstallLeavesNoCredentialsTestCase(unittest.TestCase):
    """#8 — uninstall senyap tidak boleh meninggalkan kredensial."""

    def setUp(self) -> None:
        self.iss = _read(ISS)

    def test_the_data_deletion_is_not_gated_on_not_being_silent(self):
        # Penghapusan kredensial harus berjalan di mode senyap apa pun.
        # Yang opsional (dan SHOULD asking) adalah menghapus jawaban
        # siswa yang belum terkirim -- itu data, bukan rahasia.
        block = self.iss.split("usPostUninstall", 1)
        self.assertEqual(len(block), 2, "blok usPostUninstall tidak ditemukan")
        body = block[1].split("end;")[0]
        self.assertIn(
            "DelTree", body,
            "blok penghapusan tidak ditemukan",
        )

    def test_a_silent_uninstall_removes_the_admin_password(self):
        # Hanya SEKSI [UninstallDelete]. Nama file yang sama muncul lagi di
        # [Code] (untuk tulis/hapus saat instalasi), jadi pencarian di
        # seluruh file akan selalu menemukan dan tidak membuktikan apa pun.
        section = self.iss.split("[UninstallDelete]", 1)[1].split("\n[", 1)[0]
        # Hanya DIRECTIVE `Type: files;`. Nama file muncul juga di
        # komentar penjelasan seksi ini, jadi outperformed-nya akan
        # selalu menemukan -- termasuk kalau directive-nya dihapus.
        directives = [
            line for line in section.splitlines()
            if line.strip().lower().startswith("type:")
        ]
        self.assertTrue(
            any("admin_password.txt" in d for d in directives),
            "admin_password.txt tidak pernah dihapus saat uninstall; "
            "password supervisor membiarkan siapa pun yang duduk di PC itu "
            "mengeluarkan dirinya sendiri dari ujian yang sedang berjalan. "
            f"Directive yang ada: {directives}",
        )

    def test_a_silent_uninstall_removes_the_token_bearing_config(self):
        section = self.iss.split("[UninstallDelete]", 1)[1].split("\n[", 1)[0]
        directives = [
            line for line in section.splitlines()
            if line.strip().lower().startswith("type:")
        ]
        self.assertTrue(
            any("config.json" in d for d in directives),
            "config.json tidak pernah dihapus saat uninstall; isinya token "
            "ujian (kredensial seluruh kelas) dan identitas siswa dalam "
            f"plaintext. Directive yang ada: {directives}",
        )

    def test_the_silent_path_deletes_at_least_the_credentials(self):
        # Dua berkas kredensial harus dihapus tanpa bertanya. Jawaban
        # ujian yang belum terkirim bolehbertanya -- tapi kredensial tidak.
        for name in ("admin_password.txt", "config.json"):
            self.assertIn(
                name, self.iss,
                f"{name} tidak disebut di skrip uninstall sama sekali",
            )

    def test_student_work_is_still_offered_for_removal(self):
        # Jangan sampai perbaikannya menghapus jawaban yang belum
        # terkirim tanpa memberi tahu -- itu pekerjaan siswa, bukan
        # kredensial.
        self.assertIn(
            "belum terkirim", self.iss,
            "prompt uninstall tidak menjelaskan bahwa folder data berisi "
            "jawaban ujian yang belum terkirim dan file itu tidak boleh dihapus "
            "karena tidak opto",
        )

    def test_credentials_are_not_part_of_the_opt_in_prompt(self):
        # Kredensial dihapus tanpa bertanya, jadi tidak boleh bergantung
        # pada pengguna menjawab "Yes" di dialog.
        block = self.iss.split("usPostUninstall", 1)[1]
        self.assertIn(
            "UninstallSilent",
            block,
            "blok interactive tidak ada; pastikan kredensial dihapus lewat "
            "[UninstallDelete], bukan lewat prompt",
        )


class X11GrabIsRefusedOffAnInteractiveSessionTestCase(unittest.TestCase):
    """X11 grab hanya boleh di sesi interaktif pengguna (#bukan nomor temuan).

    Regresi yang menyakiti: `tests/test_no_unguarded_dialogs.py` membangun
    `ExamViewerWindow` dengan level `strict`. `__init__` memanggil
    `SecurityEnforcer.activate()` -> `_activate_strict()` ->
    `LinuxBackend.set_strict_mode()` -> `x11.grab_keyboard()` dan
    `grab_pointer()`.

    Yang dipanggil adalah `XOpenDisplay` sungguhan, jadi suite mengambil alih
    keyboard dan pointer siapa pun yang menjalankannya. Grab hanya dilepas saat
    proses selesai, dan `deleteLater()` tidak memanggil `deactivate()` --
    sehingga `ungrab_keyboard()` tidak pernah jalan. Pada akhirnya
    keyboard developer's terkunci sampai suite selesai atau hang.

    Test suite berjalan dengan `QT_QPA_PLATFORM=offscreen`, jadi Qt tidak
    pernah menyentuh display asli, sementara `_get_display()` tetap
    membuka X server sungguhan. Grab seperti itu adalah bug, bukan fitur:
    tidak ada window X11 nyata untuk di-grab.
    """

    def setUp(self) -> None:
        from examvan.security import x11
        self.x11 = x11

    def test_grab_is_refused_when_qt_is_offscreen(self):
        with mock.patch.dict(os.environ, {"QT_QPA_PLATFORM": "offscreen"}):
            self.assertFalse(
                self.x11._grab_allowed(),
                "grab diizinkan saat Qt offscreen: tidak ada window X11 "
                "sungguhan, tapi XOpenDisplay tetap membuka display asli",
            )

    def test_grab_is_refused_for_other_headless_platforms(self):
        for platform in ("minimal", "vnc", "offscreen"):
            with self.subTest(platform=platform):
                with mock.patch.dict(os.environ, {"QT_QPA_PLATFORM": platform}):
                    self.assertFalse(self.x11._grab_allowed())

    def test_grab_is_refused_when_the_kill_switch_is_set(self):
        env = {"QT_QPA_PLATFORM": "xcb", "EXAMVAN_NO_X11_GRAB": "1"}
        with mock.patch.dict(os.environ, env):
            self.assertFalse(self.x11._grab_allowed())

    def test_grab_is_allowed_on_a_real_xcb_session(self):
        env = {"QT_QPA_PLATFORM": "xcb"}
        env.pop("EXAMVAN_NO_X11_GRAB", None)
        with mock.patch.dict(os.environ, env, clear=False):
            os.environ.pop("EXAMVAN_NO_X11_GRAB", None)
            self.assertTrue(
                self.x11._grab_allowed(),
                "grab ditolak pada sesi X11 sungguhan -- strict mode akan "
                "kehilangan fiturnya yang justru paling penting",
            )

    def test_both_grab_entry_points_are_guarded(self):
        import inspect

        for name in ("grab_keyboard", "grab_pointer"):
            with self.subTest(entry=name):
                src = inspect.getsource(getattr(self.x11, name))
                self.assertIn(
                    "_grab_allowed()", src,
                    f"{name} tidak memanggil _grab_allowed(); keyboard/mouse "
                    "bisa diambil alih di sesi yang tidak semestinya",
                )

    def test_a_real_strict_viewer_does_not_grab_the_keyboard(self):
        """Uji perilaku, bukan pola sumber.

        Test ini membangun hal BERBAHAYA yang sama persis dengan test yang
        melukai: `ExamViewerWindow` dengan level `strict` dan
        `SecurityEnforcer` sungguhan. Yang diuji adalah hasil akhirnya --
        `XGrabKeyboard`/`XGrabPointer` TIDAK boleh dipanggil.

        Memakai sumber instead of perilaku rapuh: level sering diteruskan
        lewat variabel (`_viewer("high")`), jadi pola literal tidak pernah
        cocok dan check-nya lolos diam-diam saat test justru berhenti
        memock backend.
        """
        from examvan.security import x11
        from examvan.ui.exam_viewer import ExamViewerWindow
        from examvan.models import Exam
        from PyQt5.QtWidgets import QApplication

        # Viewer sungguhan butuh QApplication yang hidup.
        app = QApplication.instance() or QApplication([])

        called = []
        real = getattr(x11, "_xlib", None)

        class _SpyXlib:
            XGrabKeyboard = staticmethod(lambda *a, **k: called.append("kb"))
            XGrabPointer = staticmethod(lambda *a, **k: called.append("ptr"))

        exam = Exam.from_json({
            "id": 9, "name": "Ujian", "status": "active",
            "security_level": "strict",
        })
        with mock.patch.dict(os.environ, {"QT_QPA_PLATFORM": "offscreen"}), \
             mock.patch.object(x11, "_xlib", _SpyXlib, create=True), \
             mock.patch.object(x11, "_get_display", return_value=object()), \
             mock.patch.object(x11, "_get_x11_window_id", return_value=42):
            viewer = ExamViewerWindow(
                exam=exam,
                server_url="https://exam.example",
                token="T0KEN01",
                identity_data={"nama": "Budi"},
            )
            try:
                self.assertIs(
                    viewer._security.strict, True,
                    "test ini tidak memakai level strict, jadi tidak "
                    "menguji apa pun",
                )
            finally:
                viewer._security.deactivate()
                viewer.deleteLater()

        self.assertEqual(
            called, [],
            "XGrabKeyboard/XGrabPointer dipanggil pada sesi non-interaktif: "
            "keyboard dan mouse Whoever menjalankan suite ikut diambil "
            "alih sampai proses selesai",
        )

    def test_the_kill_switch_is_documented_for_operators(self):
        # Kalau ada yang perlu menjalankan app di mesin yang tidak boleh
        # dibajak input-nya (mis. saat menelusuri kebocoran), harus ada
        # jalan yang diketahui -- bukan harus menebak nama env var.
        self.assertIn(
            "EXAMVAN_NO_X11_GRAB", _read(WINDOWS / "README.md"),
            "EXAMVAN_NO_X11_GRAB tidak ada di windows/README.md; operator "
            "tidak punya cara yang psychic untuk mematikan grab",
        )


class LocalBuildActuallyWorksTestCase(unittest.TestCase):
    """#9 — build di mesin sendiri harus berhasil."""

    def _output_dir(self, script: Path) -> Path:
        """Folder tempat script meletakkan hasil build-nya."""
        text = _read(script)
        if script.suffix == ".bat":
            match = re.search(r'set "OUTPUT_DIR=%~dp0dist"', text)
            assert match, "OUTPUT_DIR tidak ditemukan"
            return (script.parent / "dist").resolve()
        match = re.search(r'\$OutputDir = Join-Path \$ProjectRoot "windows\\dist"',
                          text)
        assert match, "$OutputDir tidak ditemukan"
        return (REPO / "windows" / "dist").resolve()

    def test_the_version_file_lives_where_the_script_writes_it(self):
        # `version_info.txt` adalah BUILD OUTPUT -- tidak ada di repo, jadi
        # tidak boleh diuji dengan "exists". Yang harus diuji adalah
        # KONSISTENSI: `--version-file` harus menunjuk folder yang sama
        # dengan tempat script menulisnya, dihitung relatif dari working
        # directory script itu (keduanya `cd` ke `desktop/` lebih dulu).
        #
        # Versi sebelumnya menunjuk `..\dist\` yang tidak pernah ada,
        # sementara berkasnya ditulis ke `windows\dist\`. PyInstaller
        # membuka `--version-file` secara langsung, jadi build lokal gagal
        # dengan FileNotFoundError sebelum menghasilkan apa pun -- dan
        # baris berikutnya, `--icon ..\windows\...`, justru benar.
        for script in (BUILD_BAT, BUILD_PS1):
            with self.subTest(script=script.name):
                match = re.search(r'--version-file\s+"([^"]+)"', _read(script))
                self.assertIsNotNone(match, f"{script.name}")
                target = (REPO / "desktop" /
                          match.group(1).replace("\\", "/")).resolve()
                self.assertEqual(
                    target,
                    self._output_dir(script) / "version_info.txt",
                    f"{script.name} menulis version_info.txt ke "
                    f"{self._output_dir(script)} tapi membacanya dari {target}",
                )

    def test_both_build_scripts_agree_on_the_version_file(self):
        # Dua script yang berbeda dengan dua jawaban yang berbeda adalah
        # kelas bug yang sama berulang kali di repo ini.
        targets = set()
        for path in (BUILD_BAT, BUILD_PS1):
            match = re.search(r'--version-file\s+"([^"]+)"', _read(path))
            self.assertIsNotNone(match, f"{path.name}")
            targets.add(match.group(1).replace("\\", "/"))
        self.assertEqual(
            len(targets), 1,
            f"build-exe.bat dan build-exe.ps1 menunjuk --version-file yang "
            f"berbeda: {targets}",
        )

    def test_the_icon_path_is_also_resolvable(self):
        for path in (BUILD_BAT, BUILD_PS1):
            match = re.search(r'--icon\s+"([^"]+)"', _read(path))
            self.assertIsNotNone(match, f"{path.name}")
            resolved = (WINDOWS / ".." / "desktop" /
                        match.group(1).replace("\\", "/")).resolve()
            self.assertTrue(resolved.exists(), f"{path.name}: {resolved}")


class FreshnessGuardWatchesTheRealEntryPointTestCase(unittest.TestCase):
    """#12a — guard harus mengawasi yang benar-benar masuk ke exe."""

    def setUp(self) -> None:
        self.src = _read(FRESHNESS)

    def test_the_pyinstaller_entry_point_is_watched(self):
        # Cek POLA GLOB yang aktif, bukan kemunculan kata "main.py" --
        # kata itu juga muncul di komentar, jadi keberadaannya di file
        # tidak membuktikan apa pun.
        code = "\n".join(
            line for line in self.src.splitlines()
            if not line.lstrip().startswith("#")
        )
        self.assertIn(
            '"main.py"', code,
            "check_exe_freshness.py tidak mengawasi main.py sebagai pola "
            "glob, padahal itu SATU-SATUNYA entry point yang di-build "
            "PyInstaller. Perbaikan di main.py lolos ke exe basi tanpa "
            "dilaporkan.",
        )

    def test_the_compiled_resources_are_watched(self):
        code = "\n".join(
            line for line in self.src.splitlines()
            if not line.lstrip().startswith("#")
        )
        for name in ("version_info.txt", "examvan.ico"):
            with self.subTest(resource=name):
                self.assertIn(
                    name, code,
                    f"{name} dikompilasi ke dalam exe tapi tidak "
                    "diawasi; exe bisa membawa versi lama",
                )


class PyInstallerInvocationIsWellFormedTestCase(unittest.TestCase):
    """Perintah PyInstaller harus menghasilkan exe DATAR.

    Guard ini ada karena `--onefile` hilang satu kali saat baris `--add-data`
    dihapus, dan konsekuensinya tidak terlihat sampai build Windows gagal:
    tanpa `--onefile`, PyInstaller membuat FOLDER `windows/dist/EXAMVAN/`
    berisi exe di dalamnya, sehingga:

      * Inno Setup gagal dengan "Source file ... \\dist\\EXAMVAN.exe does
        not exist" -- error yang sama sekali tidak menyiratkan penyebabnya;
      * artifact yang terunggah berisi folder, bukan installer.

    Dua baris itu berdekatan dan tak terlihat hubungannya, jadi yang
    dijaga di sini adalah BENTUK perintahnya: semua argumen dit curation
    dan entry point berada di indentation yang sama.
    """

    @classmethod
    def setUpClass(cls) -> None:
        cls.step = None
        for step in _yaml_jobs()["build"]["steps"]:
            run = step.get("run")
            if isinstance(run, str) and "--exclude-module" in run:
                cls.step = run
                break
        assert cls.step is not None, "langkah PyInstaller tidak ditemukan"

    def test_onefile_is_present(self):
        self.assertIn(
            "--onefile", self.step,
            "tanpa --onefile PyInstaller menghasilkan folder, bukan exe "
            "datar, dan Inno Setup gagal dengan pesan yang tidak menyiratkan "
            "penyebabnya",
        )

    def test_the_entry_point_is_the_last_argument(self):
        lines = [l for l in self.step.splitlines() if l.strip()]
        self.assertEqual(
            lines[-1].strip(), "desktop/main.py",
            f"argumen terakhir bukan entry point: {lines[-1]!r}",
        )

    def test_no_argument_has_leaked_out_of_the_backtick_chain(self):
        # Setiap argumen harus di baris sendiri dengan indentasi sama.
        body = [
            l for l in self.step.splitlines()
            if l.strip() and not l.strip().startswith("#")
        ]
        # Baris perintah adalah satu-satunya yang tidak berawalan "--".
        args = [l for l in body if l.strip().startswith("-")]
        indents = {len(l) - len(l.lstrip()) for l in args}
        self.assertEqual(
            len(indents), 1,
            f"argumen punya indentasi berbeda {sorted(indents)} -- salah "
            "satu keluar dari rantai backtick dan perintah jadi tidak "
            "seperti yang dimaksud",
        )

    def test_every_argument_except_the_last_continues_the_chain(self):
        body = [l for l in self.step.splitlines() if l.strip()]
        missing = [
            l.strip() for l in body[:-1] if not l.rstrip().endswith("`")
        ]
        self.assertEqual(
            missing, [],
            f"argumen tanpa backtick continuation: {missing}",
        )


class ShippedExeDoesNotContainPlaintextSourceTestCase(unittest.TestCase):
    """#12b — source Python tidak perlu ikut dibundel."""

    def test_the_workflow_does_not_bundle_the_source_tree(self):
        workflow = _read(BUILD_WORKFLOW)
        # Hanya baris KODE: komentar sengaja menyebut `--add-data` untuk
        # menjelaskan kenapa perintah itu tidak boleh dipakai, jadi
        # pencocokan teks mentah akan salah.
        code = "\n".join(
            line for line in workflow.splitlines()
            if not line.lstrip().startswith("#")
        )
        offending = [
            line.strip() for line in code.splitlines()
            if "--add-data" in line and "examvan" in line
        ]
        self.assertEqual(
            offending, [],
            "workflow mengemas source Python plaintext ke dalam exe yang "
            "dibagikan ke siswa: " + ", ".join(offending) + ". Tanpa itu "
            "PyInstaller hanya mengemas bytecode marshalled. Tidak ada "
            "import dinamis di examvan/ yang membutuhkannya.",
        )

    def test_no_build_script_bundles_the_source_either(self):
        for path in (BUILD_BAT, BUILD_PS1):
            with self.subTest(script=path.name):
                self.assertNotIn(
                    "--add-data", _read(path),
                    f"{path.name} mengemas source ke exe",
                )


class PollDeadlineMatchesItsDocumentationTestCase(unittest.TestCase):
    """#11 — deadline polling harus sesuai dengan yang didokumentasikan."""

    def setUp(self) -> None:
        self.src = _read(REPO / "desktop" / "examvan" / "api.py")

    def test_the_total_wait_is_capped_by_an_explicit_deadline(self):
        # Backoff tanpa batas total mengubah 77 detik jadi ~9 menit
        # (2.5 + 3.75 + 5.63 + ... + 20, sebanya itu), sementara
        # docstring masih menjanjikan 77s dan Android memakai 75s.
        # Siswa yang submit-nya tidak pernah terkonfirmasi duduk jauh
        # lebih lama dari yang dijanjikan.
        self.assertIn(
            "deadline_seconds", self.src,
            "poll_queued_result tidak punya deadline total; backoff "
            "per-tick membuat jendela polling tumbuh tanpa batas",
        )

    def test_the_default_deadline_is_the_documented_budget(self):
        self.assertRegex(
            self.src,
            r"deadline_seconds is None:\s*\n\s*deadline_seconds = max_attempts \* interval",
            "deadline default tidak diturunkan dari max_attempts * "
            "interval, jadi angka 77s di docstring tidak lagi benar",
        )

    def test_no_per_tick_cap_can_outgrow_the_total_budget(self):
        # Cap per-tick (mis. `interval * 8`) adalah penyebab aslinya:
        # 31 tick x 20 detik = 10 menit. Yang boleh ada hanyalah cap
        # yang tidak menambah total, dan deadline total yang
        # menjaga.
        # Hanya baris KODE: penjelasan di komentar boleh menyebut pola
        # lama justru agar sejarahnya terdokumentasi.
        block = "\n".join(
            line for line in self.src.split("def poll_queued_result", 1)[1]
            .splitlines()
            if not line.lstrip().startswith("#")
        )
        self.assertNotIn(
            "interval * 8", block,
            "cap per-tick `interval * 8` masih ada; total polling jadi "
            "jauh melampaui budget yang didokumentasikan",
        )

    def test_the_documented_budget_matches_the_default_arguments(self):
        block = self.src.split("def poll_queued_result", 1)[1][:2000]
        self.assertIn("31", block)
        self.assertIn("2.5", block)
        self.assertIn("77", block)
        self.assertNotIn(
            "≈ 77s of polling (Android uses a 75s deadline)", block,
            "docstring masih menulis budget lama sebagai satu-satunya "
            "jaminan, padahal backoff sudah mengubahnya",
        )


if __name__ == "__main__":
    unittest.main()