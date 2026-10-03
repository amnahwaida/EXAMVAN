"""H10 (TINGGI) — sidecar pemilik ditulis SESUDAH jawaban yang dilindunginya.

Bug
---
Tiga penulis jawaban di `exam_viewer.py` melakukan

    config.save_answers(exam.id, answers)      # (1) jawaban lebih dulu
    self._write_answers_owner(...)             # (2) sidecar belakangan

dan komentar di `_save_answers` mengklaim "Sidecar menutup keduanya". Tidak
menutup selama sidecar BELUM ADA — dan KEDUA gerbang konsumennya gagal-TERBUKA
saat tidak ada:

  * gerbang restore (`ExamViewerWindow.__init__`): `owner is None` berarti
    "milik siapa pun" → jawaban siswa sebelumnya dipulihkan ke layar siswa
    berikutnya;
  * gerbang recovery (`ServerConfigDialog._offer_pending_recovery`):
    `owner is None` berarti recovery sah → jawaban orang lain ditawarkan
    sebagai milik siswa yang baru saja mengetik identitasnya.

Autosave menembak tiap 0,5–2 detik sepanjang satu naskah, jadi jendela ini
dilewati RIBUAN kali. Task Manager di dalamnya meninggalkan jawaban tanpa
pemilik di PC lab — persis kebocoran lintas siswa yang sidecar itu ada untuk
mencegahnya.

Arah yang benar: sidecar DULU. Kedua gerbang selalu mengecek jawaban lebih
 dulu (`load_answers`), jadi sidecar tanpa jawaban itu INERT — sedangkan
jawaban tanpa sidecar itu bocor. `config.clear_answers` sudah benar begitu
(owner dihapus TERAKHIR, hanya setelah jawabannya hilang).

Test di bawah tidak menebak: jawaban ditulis ke disk sungguhan, lalu proses
"mati" tepat setelahnya (KeyboardInterrupt) — persis Task Manager — dan
assertion-nya asks "apakah ada jawaban yang TIDAK punya pemilik?".
"""

from __future__ import annotations

import contextlib
import importlib
import os
import shutil
import tempfile
import threading
import unittest
from pathlib import Path
from unittest import mock

os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")

from PyQt5.QtCore import QObject, pyqtSignal

from examvan import api, config
from examvan.models import Exam, SubmitResponse

EXAM_ID = 512

KEY_A = "kunci-siswa-A"
KEY_B = "kunci-siswa-B"

PAYLOAD = {"1": "A", "2": "B"}

IDENTITY = {"nama": "Andi", "nomor_ujian": "N01", "kelas": "9A"}


class _Sandbox(unittest.TestCase):
    def setUp(self) -> None:
        tmp = Path(tempfile.mkdtemp(prefix="examvan-r8-ownerfirst-"))
        self.addCleanup(shutil.rmtree, tmp, True)
        for p in (
            mock.patch.object(config, "_CONFIG_DIR", tmp),
            mock.patch.object(config, "_CONFIG_FILE", tmp / "config.json"),
        ):
            p.start()
            self.addCleanup(p.stop)
        config._cache = None
        self.addCleanup(setattr, config, "_cache", None)


class _KillAfterAnswersLanded:
    """`save_answers` yang menulis sungguhan lalu process-nya "mati".

    Sisi klien: jawaban sudah ada di disk. Sisi server: tidak ada yang
    pernah mengonfirmasi. Ini persis apa yang terjadi kalau Task Manager
    kill process sedetik setelah autosave.
    """

    def __init__(self, real) -> None:
        # `real` diambil SEBELUM patch dipasang — kalau diambil di dalam
        # `__call__`, yang diambil adalah fungsi yang sedang dipalsukan.
        self._real = real
        self.calls = 0

    def __call__(self, exam_id, answers):
        self.calls += 1
        self._real(exam_id, answers)
        raise KeyboardInterrupt("process dibunuh setelah jawaban mendarat")


class _FakeSecurityEnforcer(QObject):
    auto_submit = pyqtSignal()

    def __init__(self, *args, **kwargs) -> None:
        super().__init__()
        self.deactivate_calls = 0

    def activate(self) -> None:
        pass

    def deactivate(self) -> None:
        self.deactivate_calls += 1

    @contextlib.contextmanager
    def pause_focus_guard(self):
        yield

    def reassert_capture_protection(self) -> None:
        pass

    def clear_clipboard_now(self) -> None:
        pass

    def protect_window(self, window) -> bool:
        return True


class AutosaveOwnerFirstTestCase(_Sandbox):
    """`_save_answers` — jalur yang paling sering menolak (tiap 0,5–2 dtk)."""

    def _viewer(self, module):
        cls = module.ExamViewerWindow
        viewer = cls.__new__(cls)
        viewer._exam = Exam(id=EXAM_ID, name="Ujian", status="active")
        viewer._student_key = KEY_A
        viewer._identity_data = dict(IDENTITY)
        viewer._token = "ABCD1234"
        viewer._submitted = False
        viewer._answers_dirty = True
        return viewer

    def _run_save_answers(self, module):
        viewer = self._viewer(module)
        sheet = mock.Mock()
        sheet.get_answers.return_value = dict(PAYLOAD)
        viewer._answer_sheet = sheet
        killer = _KillAfterAnswersLanded(config.save_answers)
        with mock.patch.object(config, "save_answers", killer):
            with self.assertRaises(KeyboardInterrupt):
                viewer._save_answers()
        return killer

    def test_no_ownerless_answers_when_the_process_dies_mid_autosave(self):
        module = importlib.import_module("examvan.ui.exam_viewer")
        killer = self._run_save_answers(module)

        self.assertEqual(killer.calls, 1, "autosave tidak pernah menulis")
        on_disk = config.load_answers(EXAM_ID)
        if on_disk is None:
            self.skipTest(
                "jawaban tidak sempat mendarat di disk — tidak ada yang "
                "bisa diuji"
            )
        owner = config.load_answers_owner(EXAM_ID)
        self.assertIsNotNone(
            owner,
            "ADA jawaban di disk tanpa sidecar pemilik: gerbang restore dan "
            "gerbang 'Kirim Lagi' sama-sama gagal-TERBUKA saat owner None, "
            "jadi jawaban ini bisa dipulihkan dan dikirim atas nama siswa "
            "berikutnya. Tulis sidecar SEBELUM jawaban.",
        )
        self.assertEqual(owner.get("student_key"), KEY_A)

    def test_a_late_success_of_another_student_cannot_take_these_answers(self):
        # Gerbang yang diaktifkan sidecar: jawaban yatim TIDAK boleh
        # di-resolve untuk pupil lain.
        module = importlib.import_module("examvan.ui.exam_viewer")
        self._run_save_answers(module)
        if config.load_answers(EXAM_ID) is None:
            self.skipTest("jawaban tidak sempat mendarat di disk")
        self.assertEqual(
            config.resolve_submit_answers({}, EXAM_ID, KEY_B), {},
            "jawaban yatim dikirim atas nama siswa lain (resolve_submit_answers "
            "gagal-terbuka karena owner None)",
        )
        self.assertEqual(
            config.resolve_submit_answers({}, EXAM_ID, KEY_A), PAYLOAD,
            "pemilik sah harus tetap bisa mengambil jawabannya sendiri",
        )


class AutoSubmitOwnerFirstTestCase(_Sandbox):
    """`_auto_submit_and_exit` — flush payload sebelum thread submit jalan."""

    def _viewer(self, module):
        cls = module.ExamViewerWindow
        viewer = cls.__new__(cls)
        viewer._exam = Exam(id=EXAM_ID, name="Ujian", status="active")
        viewer._identity_data = dict(IDENTITY)
        viewer._token = "ABCD1234"
        viewer._student_key = KEY_A
        viewer._submitted = False
        viewer._submitting = False
        viewer._abandoned = False
        viewer._submit_lock = threading.Lock()
        viewer._answer_sheet = mock.Mock()
        viewer._answer_sheet.get_answers.return_value = dict(PAYLOAD)
        return viewer

    def test_owner_is_written_before_the_answers_are_flushed(self):
        module = importlib.import_module("examvan.ui.exam_viewer")
        viewer = self._viewer(module)
        order = []
        real_save_answers = config.save_answers
        real_save_owner = config.save_answers_owner

        def _save(exam_id, answers):
            order.append("answers")
            return real_save_answers(exam_id, answers)

        def _owner(exam_id, key, label=""):
            order.append("owner")
            return real_save_owner(exam_id, key, label)

        # Sisipkan kegagalan tepat SETELAH jawaban mendarat: proses mati di
        # sana, persis seperti Task Manager.
        def _save_then_die(exam_id, answers):
            _save(exam_id, answers)
            raise KeyboardInterrupt("mati setelah jawaban mendarat")

        with mock.patch.object(config, "mark_submitted"), \
             mock.patch.object(config, "save_answers", _save_then_die), \
             mock.patch.object(config, "save_answers_owner", _owner):
            with self.assertRaises(KeyboardInterrupt):
                viewer._auto_submit_and_exit()

        self.assertEqual(order, ["owner", "answers"],
                         "urutan efek samping yang diam-diam menentukan kebocoran: "
                         "sidecar harus ditulis SEBELUM jawaban")
        if config.load_answers(EXAM_ID) is not None:
            self.assertIsNotNone(
                config.load_answers_owner(EXAM_ID),
                "jawaban mendarat tanpa sidecar pemilik — tidak boleh "
                "terjadi",
            )


class ManualSubmitOwnerFirstTestCase(_Sandbox):
    """`_do_submit` — flush payload yang persis dikirim."""

    def _viewer(self, module):
        cls = module.ExamViewerWindow
        viewer = cls.__new__(cls)
        viewer._exam = Exam(id=EXAM_ID, name="Ujian", status="active")
        viewer._identity_data = dict(IDENTITY)
        viewer._token = "ABCD1234"
        viewer._student_key = KEY_A
        viewer._submitted = False
        viewer._submitting = False
        viewer._answer_sheet = mock.Mock()
        viewer._answer_sheet.get_answers.return_value = dict(PAYLOAD)
        viewer._btn_submit = mock.Mock()
        return viewer

    def test_owner_is_written_before_the_answers_are_flushed(self):
        module = importlib.import_module("examvan.ui.exam_viewer")
        viewer = self._viewer(module)
        order = []
        real_save_answers = config.save_answers
        real_save_owner = config.save_answers_owner

        def _save_then_die(exam_id, answers):
            order.append("answers")
            real_save_answers(exam_id, answers)
            raise KeyboardInterrupt("mati setelah jawaban mendarat")

        def _owner(exam_id, key, label=""):
            order.append("owner")
            return real_save_owner(exam_id, key, label)

        viewer._submit_lock = threading.Lock()
        with mock.patch.object(config, "resolve_submit_answers",
                               side_effect=lambda m, *a, **k: dict(m)), \
             mock.patch.object(config, "save_answers", _save_then_die), \
             mock.patch.object(config, "save_answers_owner", _owner):
            with self.assertRaises(KeyboardInterrupt):
                viewer._do_submit()

        self.assertEqual(order, ["owner", "answers"],
                         "urutan efek samping yang menentukan kebocoran: "
                         "sidecar harus ditulis lebih dulu")
        self.assertIsNotNone(
            config.load_answers_owner(EXAM_ID),
            "sidecar pemilik tidak ditulis sama sekali sebelum jawaban",
        )


if __name__ == "__main__":
    unittest.main()