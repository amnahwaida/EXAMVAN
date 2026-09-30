"""Tidak ada dialog di window ujian yang boleh memicu auto-submit (#2).

Bug yang ditutup file ini
------------------------
`_modal_dialog_guard()` diperkenalkan di `9f984e6` untuk membungkus
dialog konfirmasi submit, dialog "Keluar Ujian", dan prompt admin exit.
Itu memperbaiki TIGA pemicunya -- tapi hanya tiga.

Tiga dialog lain di `exam_viewer.py` tetap berjalan di luar guard:

    423  QMessageBox.warning("Gagal", "Silakan coba lagi")
    1109 QMessageBox.warning("Tidak Diizinkan", ... admin exit)
    1126 QMessageBox.warning("Akses Ditolak", "Password salah")

`QMessageBox.warning` adalah dialog modal, jadi ia menjalankan nested
event loop dengan window ujian `isActiveWindow() == False`. Persis kondisi
yang membuat `_poll_focus` memulai countdown 3 detik -- dan yang
`_modal_dialog_guard()` ada untuk mencegahnya. Verifikasi pada enforcer:

    _focus_guard_paused default      : False
    saat dialog konfirmasi terbuka   : True
    setelah dialog ditutup           : False -> fokus dihitung lagi

Artinya ketiga dialog itu berjalan dengan `_focus_guard_paused == False`,
dan `_focus_timer` tetap aktif. Tiga detik kemudian:
`auto_submit.emit()` -> `_auto_submit_and_exit()` -> jendela tertutup
sambil siswa masih membaca pesannya.

Yang paling merusak adalah yang di line 423: itu dialog "Silakan coba
lagi" yang muncul di SETIAP submit yang gagal, pada level `medium` yang
adalah default. `_submitting` baru saja di-reset ke False di line 415,
jadi gate auto-submit melepaskannya.

Untuk admin exit (1109/1126) konsekuensinya justru berbalik: supervisor
salah ketik password, 3 detik kemudian ujian siswa justruTERKUMPUL.

Test di sini membuktikannya lewat perilaku: dialog sungguhan dibuka
dengan event loop berjalan, lalu auto-submit harus tidak pernah sampai --
untuk SETIAP dialog di window ujian, bukan hanya tiga yang sudah
diperbaiki.
"""

from __future__ import annotations

import contextlib
import time
import unittest
from unittest import mock

from PyQt5.QtWidgets import QApplication, QMessageBox

from examvan.models import Exam
from examvan.security.enforcer import SecurityEnforcer
from examvan.ui.exam_viewer import ExamViewerWindow


def _exam(level="medium"):
    return Exam.from_json({
        "id": 9, "name": "Ujian", "status": "active",
        "security_level": level,
    })


class _FakeSecurity:
    """SecurityEnforcer yang tidak pernah menyentuh X11.

    Sedejauh(test)lock-dialog tidak butuhGrab keyboard sungguhan --
    bahkan di level strict. Yang dibutuhkan hanya flag `_focus_guard_paused`,
    karena itulah yang sedang diuji.
    """

    def __init__(self, *args, **kwargs):
        self.auto_submit = _FakeAutoSubmit()
        self._focus_guard_paused = False
        self._focus_guard_depth = 0
        self._focus_timer = _FakeTimer()
        self._focus_guard_resume = False
        self.window = kwargs.get("window")

    def activate(self):
        pass

    def deactivate(self):
        pass

    @contextlib.contextmanager
    def pause_focus_guard(self):
        self._focus_guard_depth += 1
        if self._focus_guard_depth == 1:
            self._focus_guard_resume = self._focus_timer.isActive()
            self._focus_timer.stop()
            self._focus_guard_paused = True
        try:
            yield
        finally:
            self._focus_guard_depth -= 1
            if self._focus_guard_depth == 0:
                self._focus_guard_paused = False
                if self._focus_guard_resume:
                    self._focus_timer.start()
                self._focus_guard_resume = False

    def _poll_focus(self):
        pass

    def set_capture_protection(self, window):
        pass

    def release_capture_protection(self, window):
        pass

    def release_strict_mode(self, window):
        pass

    def allow_sleep(self):
        pass

    def prevent_sleep(self):
        pass

    def clear_clipboard_now(self):
        pass


class _FakeTimer:
    def __init__(self):
        self._running = False
        self._interval = 3000

    def setInterval(self, ms):
        self._interval = ms

    def setSingleShot(self, _flag):
        pass

    def start(self, ms=None):
        self._running = True

    def stop(self):
        self._running = False

    def isActive(self):
        return self._running

    def isSingleShot(self):
        return True


class _FakeAutoSubmit:
    def __init__(self):
        self._slots = []

    def connect(self, slot):
        self._slots.append(slot)

    def emit(self, *args):
        for slot in self._slots:
            slot(*args)


class EveryModalDialogIsGuardedTestCase(unittest.TestCase):
    """Tidak boleh ada dialog modal yang lepas dari `_modal_dialog_guard`."""

    @classmethod
    def setUpClass(cls) -> None:
        cls.app = QApplication.instance() or QApplication([])

    def _viewer(self, level="medium"):
        # BACKEND DI-MOCK. `ExamViewerWindow.__init__` memanggil
        # `SecurityEnforcer.activate()`, dan level `strict` (alias "high")
        # memanggil `set_strict_mode()` -> `x11.grab_keyboard()` +
        # `grab_pointer()`.
        #
        # Yang dipanggil adalah `XOpenDisplay` sungguhan -- jadi test ini
        # mengambil alih keyboard dan pointer whoever yang menjalankan suite.
        # Grab hanya dilepas saat proses impugn selesai, jadi developer's
        # keyboard tersangkut sampai suite selesai ATAU hang. Dan `deleteLater`
        # TIDAK memanggil `deactivate()`, jadi `ungrab_keyboard()` tidak pernah
        # jalan.
        #
        # Tes ini tidak butuh grab sama sekali -- yang diuji adalah dialog.
        with mock.patch("examvan.ui.exam_viewer.SecurityEnforcer",
                        _FakeSecurity):
            viewer = ExamViewerWindow(
                exam=_exam(level),
                server_url="https://exam.example",
                token="T0KEN01",
                identity_data={"nama": "Budi", "nomor_ujian": "N01",
                               "kelas": "9A"},
            )
        # Colek `_security` supaya dialog-guard behavior tetap bisa dibaca.
        self.addCleanup(viewer.deleteLater)
        return viewer

    def test_no_message_box_call_sits_outside_a_guard(self):
        # Bentuk statis: setiap `QMessageBox.warning/information/question`
        # dan `QInputDialog.getText` di file harus berada di dalam blok
        # `_modal_dialog_guard`. Ini yang menangkap pemicu BARU yang
        # ditambahkan di kemudian hari -- bug #2 ini adalah akibat
        # persis dari berkali-kali menambahkan dialog tanpa mengingat
        # guard-nya.
        import inspect
        import re

        from examvan.ui import exam_viewer as ev

        src = inspect.getsource(ev)
        offenders = []
        for match in re.finditer(
            r"(QMessageBox\.(?:warning|information|question)"
            r"|QInputDialog\.getText)\(",
            src,
        ):
            # Mundur ke awal baris pemanggil dan cek indentasi/guard
            # di blok yang mengapitnya.
            start = src.rfind("\n    def ", 0, match.start())
            if start == -1:
                start = 0
            body = src[start:match.start()]
            if "_modal_dialog_guard" not in body:
                line = src[: match.start()].count("\n") + 1
                offenders.append(f"line ~{line}: {match.group(1)}")

        self.assertEqual(
            offenders, [],
            "dialog modal di luar _modal_dialog_guard: "
            + ", ".join(offenders)
            + ". Dialog modal membuat window tidak aktif, dan tanpa guard "
            "focus monitor memulai countdown 3 detik yang menutup "
            "jendela di tengah siswa membaca pesannya.",
        )

    def _guard_seen_during_dialog(self, run, viewer):
        #  diambil sebagai callable TANPA argumen: sebagian jalur
        # sudah berupa bound method, sebagian perlu lambda.
        """Catat `_focus_guard_paused` pada setiap dialog yang terbuka.

        Di offscreen, `raise_()` pada mode strict membuat `isActiveWindow()`
        tetap True, jadi rantai `isActiveWindow() -> _focus_timer -> 
        auto_submit` tidak bisa direproduksi penuh di sini. Yang
        reproducible, dan merupakan AKAR masalahnya, adalah apakah focus
        guard sedang ditangguhkan saat dialog modal terbuka. Kalau tidak,
        di PC Windows nyata countdown 3 detik pasti berjalan.
        """
        enforcer = viewer._security
        seen = []
        enforcer._active = True

        def record():
            seen.append(enforcer._focus_guard_paused)

        def fake_box(*a, **k):
            record()
            return QMessageBox.No

        with mock.patch.object(QMessageBox, "warning", fake_box), \
             mock.patch.object(QMessageBox, "information", fake_box), \
             mock.patch.object(QMessageBox, "question", fake_box):
            run()
        return seen

    def _assert_dialogs_are_all_guarded(self, run, viewer, how_many=1):
        seen = self._guard_seen_during_dialog(run, viewer)
        self.assertEqual(
            len(seen), how_many,
            f"dialog tidak pernah tampil (sudah {how_many} expected); "
            "test ini tidak menguji apa yang diklaimnya",
        )
        self.assertEqual(
            seen, [True] * how_many,
            "focus guard tidak ditangguhkan saat dialog modal terbuka: "
            "di PC Windows nyata `_poll_focus` akan melihat "
            "isActiveWindow() == False, memulai countdown 3 detik, lalu "
            "auto-submit menutup jendela di tengah siswa membaca.",
        )

    def test_the_submit_failure_dialog_is_guarded(self):
        # Line 423: muncul di SETIAP submit yang gagal, dan level default
        # adalah medium. Jalur pemulihan yang dialog ini tawarkan justru
        # hilang kalau jendela tertutup sendiri.
        viewer = self._viewer("medium")
        self._assert_dialogs_are_all_guarded(
            lambda: viewer._on_submit_result(False, "server unreachable"), viewer
        )

    def test_the_admin_exit_unconfigured_dialog_is_guarded(self):
        viewer = self._viewer("high")
        with mock.patch("examvan.ui.exam_viewer._ADMIN_PASSWORD", None), \
             mock.patch("PyQt5.QtWidgets.QInputDialog.getText",
                        return_value=("apa saja", True)):
            self._assert_dialogs_are_all_guarded(
                viewer._admin_exit_prompt, viewer
            )

    def test_the_wrong_password_dialog_is_guarded(self):
        viewer = self._viewer("high")
        with mock.patch("examvan.ui.exam_viewer._ADMIN_PASSWORD", "rahasia"), \
             mock.patch("PyQt5.QtWidgets.QInputDialog.getText",
                        return_value=("salah", True)):
            self._assert_dialogs_are_all_guarded(
                viewer._admin_exit_prompt, viewer
            )

    def test_the_recovery_path_survives_a_failed_submit(self):
        # Yang membuatpelajar benar-benar mencoba lagi harus tetap ada: tombol
        # hidup, ujian belum ditandai selesai.
        viewer = self._viewer("medium")
        fired = []
        viewer._auto_submit_and_exit = mock.Mock(
            side_effect=lambda: fired.append("submit")
        )
        viewer._security.auto_submit.connect(viewer._auto_submit)
        viewer._security._active = True

        seen = []

        def fake_box(*a, **k):
            # Simulasikan 3 detik penuh: countdown fokus punya waktu
            # untuk-timer fokus sempat meletus.
            deadline = time.monotonic() + 1.2
            while time.monotonic() < deadline:
                QApplication.instance().processEvents()
                seen.append(viewer._security._focus_timer.isActive())
            return QMessageBox.No

        with mock.patch.object(QMessageBox, "warning", fake_box):
            viewer._on_submit_result(False, "server unreachable")

        self.assertEqual(fired, [], "jendela tertutup saat dialog kegagalan terbuka")
        self.assertFalse(
            any(seen),
            "countdown focus-loss aktif di balik dialog 'Silakan coba lagi'",
        )
        self.assertTrue(viewer._btn_submit.isEnabled())


class SubmitFailureKeepsTheRecoveryPathTestCase(unittest.TestCase):
    """Dialog kegagalan harus membiarkan siswa benar-benar mencoba lagi."""

    @classmethod
    def setUpClass(cls) -> None:
        cls.app = QApplication.instance() or QApplication([])

    def test_the_submit_button_stays_enabled_after_a_failure(self):
        viewer = ExamViewerWindow(
            exam=_exam("medium"), server_url="https://exam.example",
            token="T0KEN01", identity_data={"nama": "Budi"},
        )
        self.addCleanup(viewer.deleteLater)
        viewer._auto_submit = mock.Mock()

        with mock.patch.object(QMessageBox, "warning"):
            viewer._on_submit_result(False, "server unreachable")

        self.assertFalse(viewer._submitted)
        self.assertFalse(viewer._submitting)
        self.assertTrue(
            viewer._btn_submit.isEnabled(),
            "tombol submit mati padahal dialog menawarkan untuk mencoba lagi",
        )

    def test_the_exam_is_not_marked_submitted_on_failure(self):
        viewer = ExamViewerWindow(
            exam=_exam("medium"), server_url="https://exam.example",
            token="T0KEN01", identity_data={"nama": "Budi"},
        )
        self.addCleanup(viewer.deleteLater)
        viewer._auto_submit = mock.Mock()

        with mock.patch.object(QMessageBox, "warning"):
            viewer._on_submit_result(False, "boom")

        viewer._auto_submit.assert_not_called()


if __name__ == "__main__":
    unittest.main()