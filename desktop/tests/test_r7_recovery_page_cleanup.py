"""Ronde 7 (H1) — halaman hasil RECOVERY harus membersihkan sesi identitas.

Bug
---
`CongratulationsWindow` dibangun di DUA tempat:

1. `ui/exam_viewer.py:_show_congratulations` (dipakai `_cleanup_after_submit`
   DAN `_on_auto_submit_done`), yang menyambungkan
   `congrats.page_closed` → viewer menutup diri → `closed` →
   `__main__._after_viewer_gone` → `config.clear_identity()` +
   `input_token.clear()` + tombol connect hidup lagi;
2. `ui/server_config.py:_recovery_done_slot`, yang TIDAK punya
   `page_closed` sama sekali (`grep -c page_closed server_config.py` → 0).

Jalur kedua adalah jalur yang dipakai justru ketika auto-submit background
GAGAL dan siswa menekan "Kirim Lagi" — dan di jalur itu tidak pernah ada
`ExamViewerWindow`, jadi MEMBERSIHKAN `__main__` juga tidak pernah jalan.
Konsekuensi, berurutan:

* `identity AFTER recovery: {'identity_data': {'student_name': 'SANTOSO', …}}`
  masih ada di config;
* `_prefill_identity_if_same_exam` mengisinya lagi ke form siswa berikutnya,
  dan `last_input.returnPressed` terikat ke submit — jadi SATU Enter sudah
  cukup untuk menjawab atas nama orang sebelumnya;
* `mark_submitted` juga tidak pernah dipanggil di sini, jadi
  `_offer_resubmit_choice` selalu `return True` lebih awal dan siswa
  berikutnya tidak diberi tahu bahwa percobaan itu memang ada.

Perbaikan
---------
`_recovery_done_slot` diberi kontrak yang sama dengan jalur ujian: saat
halaman ditutup, identitas dibersihkan, marker submit ditulis dengan
pasangan `build_student_key`/`student_label` yang sama seperti
`_cleanup_after_submit`, dan UI koneksi dikembalikan agar PC lab bisa
dipakai siswa berikutnya.

Body "bangun halaman + pasang pembersihannya" dipindah ke SATU helper
modul-level (`_present_result_page`) supaya site berikutnya tidak bisa
lupa lagi. Site-site yang sudah ada di `exam_viewer.py` tidak bisa ikut
dipindah ronde ini karena file itu di luar daftar edit — lihat catatan
`NO_PAGE_CLOSED_LEFT_BEHIND` di bawah.
"""

from __future__ import annotations

import pathlib
import tempfile
import unittest
from unittest import mock

import os

os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")

from PyQt5.QtWidgets import QApplication

from examvan import config
from examvan.models import Exam, SubmitResponse
from examvan.utils import build_student_key, student_label

APP = QApplication.instance() or QApplication([])

SERVER_CONFIG_PY = (
    pathlib.Path(__file__).resolve().parents[1]
    / "examvan" / "ui" / "server_config.py"
)

EXAM = Exam(id=7, name="Ujian Matematika", status="active")
# Ditangkap saat import: dipakai penjaga diri di bawah untuk membuktikan
# tidak ada test di file ini yang meninggalkan Mock di `config`.
_REAL_MARK_SUBMITTED = config.mark_submitted
IDENTITY = {"nama": "SANTOSO", "nomor_ujian": "N02", "kelas": "9B"}
TOKEN = "TOKLAB01"


class _ConfigSandbox(unittest.TestCase):
    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        sandbox = pathlib.Path(self._tmp.name) / "examvan"
        sandbox.mkdir(parents=True, exist_ok=True)
        for attr, value in (
            ("_CONFIG_DIR", sandbox),
            ("_CONFIG_FILE", sandbox / "config.json"),
        ):
            patcher = mock.patch.object(config, attr, value)
            patcher.start()
            self.addCleanup(patcher.stop)
        config._cache = None
        self.addCleanup(setattr, config, "_cache", None)


class _OkBackend:
    def set_capture_protection(self, window):
        pass

    def __getattr__(self, name):
        raise AssertionError(f"backend dipanggil untuk {name!r}, tak terduga")


class RecoveryPageCleansUpTheSessionTest(_ConfigSandbox):
    def setUp(self):
        super().setUp()
        self._dlg, self._page = self._run_recovery_success()

    def _run_recovery_success(self):
        from examvan.ui.server_config import ServerConfigDialog

        dlg = ServerConfigDialog()
        self.addCleanup(dlg.deleteLater)
        dlg._exam = EXAM
        dlg._server_url = "https://exam.example"
        dlg._validated_token = TOKEN
        dlg._recovery_identity = dict(IDENTITY)
        dlg.input_token.setText(TOKEN)
        dlg.btn_connect.setEnabled(False)
        dlg._connect_in_flight = True
        config.set("exam_token", TOKEN)
        # Identitas seperti tertinggal setelah `_show_identity_dialog`.
        config.set_identity_session(dict(IDENTITY), {
            "exam_id": EXAM.id, "token": TOKEN,
        })

        fake_api = mock.Mock()
        fake_api.submit_with_retry.return_value = SubmitResponse(
            success=True, status="ok", message="ok",
            congrats_message="Kerja bagus!",
        )
        fake_api.complete_exam.return_value = None
        patcher = mock.patch("examvan.ui.server_config.api", fake_api)
        patcher.start()
        self.addCleanup(patcher.stop)

        backend_patcher = mock.patch(
            "examvan.security.enforcer.get_backend", lambda: _OkBackend())
        backend_patcher.start()
        self.addCleanup(backend_patcher.stop)

        answers_patcher = mock.patch(
            "examvan.ui.server_config.config.load_answers", return_value=None)
        answers_patcher.start()
        self.addCleanup(answers_patcher.stop)

        dlg._recovery_submit_thread(EXAM, dict(IDENTITY))
        page = getattr(dlg, "_congrats_ref", None)
        self.assertIsNotNone(page, "halaman hasil recovery tidak dibuat")
        self.addCleanup(page.deleteLater)
        return dlg, page

    def test_the_page_wires_page_closed(self):
        receivers = self._page.receivers(self._page.page_closed)
        self.assertGreater(
            receivers, 0,
            "halaman hasil recovery tidak punya penerima `page_closed` — "
            "grep -c page_closed server_config.py = 0 adalah bug ini",
        )

    def test_closing_the_page_clears_the_identity(self):
        self.assertNotEqual(
            config.get_identity_data(), {},
            "identitas seharusnya masih ada sebelum halaman ditutup",
        )
        self._page.close()
        self.assertEqual(
            config.get_identity_data(), {},
            "identitas siswa TIDAK dibersihkan oleh halaman hasil recovery — "
            "siswa berikutnya mendapat form terisi dan satu Enter sudah "
            "cukup menjawab atas namanya",
        )
        self.assertEqual(config.get_identity_session()["context"], {})

    def test_closing_the_page_writes_the_submit_marker(self):
        # `mock.patch.object` (bukan `config.mark_submitted = Mock()`):
        # menimpa atribut modul tanpa memulihkannya akan mematikan
        # `mark_submitted` untuk SEMUA test yang jalan sesudahnya.
        with mock.patch.object(config, "mark_submitted") as mark:
            self._page.close()
        self.assertEqual(mark.call_count, 1)
        _args = mark.call_args[0]
        self.assertEqual(_args[0], EXAM.id)
        self.assertEqual(_args[1], build_student_key(IDENTITY, TOKEN))
        self.assertEqual(_args[2], student_label(IDENTITY, TOKEN))

    def test_closing_the_page_does_not_leave_the_marker_mocked(self):
        # Penjaga diri: kalau ada test lain di file ini yang menimpa
        # `mark_submitted` tanpa memulihkannya, seluruh test marker yang
        # jalan SESUDAHNYA ikut hancur — dan gejalanya jauh dari file ini.
        self.assertIs(
            config.mark_submitted, _REAL_MARK_SUBMITTED,
            "config.mark_submitted masih berupa Mock hasil penimpaan",
        )

    def test_closing_the_page_re_enables_the_connect_ui(self):
        self._page.close()
        self.assertTrue(self._dlg.btn_connect.isEnabled())
        self.assertTrue(self._dlg.input_token.isEnabled())
        self.assertTrue(self._dlg.input_url.isEnabled())
        self.assertFalse(self._dlg._connect_in_flight,
                         "guard in-flight tidak pernah dilepas — PC lab "
                         "terkunci selamanya setelah satu recovery")

    def test_closing_the_page_clears_the_token_input(self):
        self.assertEqual(self._dlg.input_token.text(), "")
        self._page.close()
        self.assertEqual(self._dlg.input_token.text(), "")

    def test_the_marker_is_written_with_the_viewer_pair(self):
        # `_cleanup_after_submit` (exam_viewer) memakai pasangan yang sama;
        # kalau server_config memakai pasangan lain, penandaan dari kedua
        # jalur tidak akan saling mengenal.
        calls = []
        real_mark = config.mark_submitted
        with mock.patch.object(
            config, "mark_submitted",
            side_effect=lambda *a, **k: (
                calls.append(a), real_mark(*a, **k))[1],
        ):
            self._page.close()
        self.assertEqual(
            calls[0][1:], (build_student_key(IDENTITY, TOKEN),
                          student_label(IDENTITY, TOKEN)),
        )


class NoPageClosedLeftBehindTest(unittest.TestCase):
    """Setiap tempat yang membangun halaman hasil harus memasang tirinya."""

    def test_server_config_builds_and_wires_in_one_helper(self):
        lines = SERVER_CONFIG_PY.read_text(encoding="utf-8").splitlines()
        self.assertTrue(
            any("page_closed" in line for line in lines),
            "server_config.py tidak punya satu pun `page_closed`",
        )
        # Hanya SATU tempat yang MEMBANGUN halaman hasil: kalau ada site
        # baru yang memanggil konstruktor secara langsung, ia bisa
        # melewati wiring `page_closed`.
        call_sites = [
            (i, line.strip()) for i, line in enumerate(lines, 1)
            if "= CongratulationsWindow(" in line
        ]
        self.assertEqual(
            len(call_sites), 1, call_sites,
        )
        self.assertIn("page_closed", "\n".join(lines))

    def test_the_helper_takes_the_cleanup_pieces(self):
        lines = SERVER_CONFIG_PY.read_text(encoding="utf-8").splitlines()
        self.assertTrue(
            any(line.startswith("def _present_result_page(") for line in lines),
            "helper `_present_result_page` tidak ada di server_config.py",
        )


if __name__ == "__main__":  # pragma: no cover
    unittest.main()
