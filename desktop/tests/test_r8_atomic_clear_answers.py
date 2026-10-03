"""H9 (TINGGI) — `answers_match_disk()` → `clear_answers()` bukan satu transaksi.

Bug
---
Jadi urutan di dua jalur yang memanggil `clear_answers` setelah submit
terkonfirmasi:

    current = config.load_answers(exam_id)      # (1) baca
    if current == answers:                      # (2) putuskan
        config.clear_answers(exam_id)           # (3) hapus

`config._answers_lock` hanya wraps SETIAP `os.replace`/`unlink` sendiri-
sendiri (lihat `save_answers`/`save_answers_owner`), jadi PASANGAN
baca-then-hapus itu TIDAK atomik terhadap penulis jawaban.

Interleaving nyata (auditor, config asli + direktori sementara):

    [B] wrote answers {'1':'X','2':'Y'} + owner=studentB
    [A] clear_answers() running for exam 77
    answers file on disk : False
    owner sidecar        : False
    => jawaban B yang baru di-autosave DAN sidecar-nya: HILANG

Sukses yang TERLAMBAT dari percobaan sebelumnya menghapus pekerjaan
percobaan berikutnya — diam-diam dan tidak bisa dipulihkan. Jendelanya
~0,1 ms per kejadian, tapi ini jalur kehilangan data persis di skenario
yang penjaga ini dibuat untuk menutupnya.

Test di bawah mendriver interleaving sungguhan (thread + fungsi config
sungguhan), lalu memeriksa: payload yang lebih baru beserta sidecar-nya
HARUS selamat, sedangkan payload milik sendiri yang masih utuh di disk
masih boleh dihapus.
"""

from __future__ import annotations

import contextlib
import importlib
import os
import shutil
import tempfile
import threading
import time
import unittest
from pathlib import Path
from unittest import mock

os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")

from PyQt5.QtCore import QObject, pyqtSignal

from examvan import api, config
from examvan.models import Exam, SubmitResponse
from examvan.ui import exam_viewer as ev_mod

EXAM_ID = 77

# Payload yang dikirim attempt A (dikonfirmasi server).
OLD_PAYLOAD = {"1": "A", "2": "B"}
# Payload + sidecar attempt BERIKUTNYA (siswa B) — harus selamat.
NEW_PAYLOAD = {"1": "X", "2": "Y"}
KEY_A = "kunci-siswa-A"
KEY_B = "kunci-siswa-B"

# Batas tunggu penulis B yang benar-benar terblokir kunci config.
_BLOCKED_WRITER_JOIN = 0.5


class _Sandbox(unittest.TestCase):
    """config diarahkan ke direktori sementara + penulis B yang sungguhan."""

    def setUp(self) -> None:
        tmp = Path(tempfile.mkdtemp(prefix="examvan-r8-atomicclear-"))
        self.addCleanup(shutil.rmtree, tmp, True)
        for p in (
            mock.patch.object(config, "_CONFIG_DIR", tmp),
            mock.patch.object(config, "_CONFIG_FILE", tmp / "config.json"),
        ):
            p.start()
            self.addCleanup(p.stop)
        config._cache = None
        self.addCleanup(setattr, config, "_cache", None)
        config.save_answers(EXAM_ID, dict(OLD_PAYLOAD))
        config.save_answers_owner(EXAM_ID, KEY_A, "Andi")
        self._writer_threads = []

    def _writer_thread(self) -> threading.Thread:
        thread = threading.Thread(target=self._write_newer_attempt, daemon=True)
        self._writer_threads.append(thread)
        return thread

    def _write_newer_attempt(self) -> None:
        """Autosave siswa B: dua panggilan config yang sungguhan."""
        config.save_answers(EXAM_ID, dict(NEW_PAYLOAD))
        config.save_answers_owner(EXAM_ID, KEY_B, "Siswa B")

    def _join_writers(self) -> None:
        for thread in self._writer_threads:
            thread.join(5.0)

    @staticmethod
    def _writer_lock_held() -> bool:
        """Apakah thread SAAT INI memegang `_answers_lock`?

        `config._answers_lock` sengaja `RLock` supaya
        `clear_answers_if_unchanged` boleh memegangnya sambil memanggil
        `clear_answers`. `RLock` tidak punya `.locked()` publik, jadi
        `getattr(..., "locked", lambda: False)` selalu melaporkan False dan
        test diam-diam masuk ke cabang "tidak ada kunci" -- padahal kunci
        sedang dipegang. `_is_owned()` adalah penanda yang benar untuk
        `RLock` (benar-benar per-thread), dan `locked()` untuk `Lock` biasa.
        """
        lock = config._answers_lock
        owned = getattr(lock, "_is_owned", None)
        if owned is not None:
            return bool(owned())
        locked = getattr(lock, "locked", None)
        return bool(locked()) if locked is not None else False

    def _arm_interleaving(self) -> None:
        """Pasang sisipan pada decision-read A (`load_answers`).

Penulis B dijalankan sungguhan, hanya TIPIKNYA yang disisipkan:
        bukan `save_answers` palsu, jadi kunci `config._answers_lock` yang
        dipakai produksi benar-benar yang diuji.
        """
        real_load = config.load_answers
        state = {"done": False}

        def fake_load(exam_id):
            current = real_load(exam_id)
            if state["done"] or current != OLD_PAYLOAD:
                return current
            state["done"] = True
            if self._writer_lock_held():
                # A MEMEGANG kunci penulis: writer nyata terblokir di
                # `os.replace` dan baru menulis setelah A selesai — ini
                # perilaku yang benar. Jalankan di thread lain supaya
                # tidak ada deadlock.
                self._writer_thread().start()
                self._writer_threads[-1].join(_BLOCKED_WRITER_JOIN)
            else:
                # A tidak memegang kunci: B menulis DI DALAM jendela
                # baca-then-hapus. Ini justru reproduksi bug-nya.
                self._write_newer_attempt()
            return current

        patcher = mock.patch.object(config, "load_answers", fake_load)
        patcher.start()
        self.addCleanup(patcher.stop)


class AtomicClearHelperTestCase(_Sandbox):
    def test_a_late_clear_keeps_the_newer_payload_and_its_owner(self):
        self._arm_interleaving()
        # `_answers_lock` sengaja `RLock` (reentrant) supaya
        # `clear_answers_if_unchanged` boleh memegangnya sambil memanggil
        # `clear_answers` di dalamnya, dan `RLock` tidak punya `.locked()`
        # publik — jadi yang dicek di sini adalah GEJALA:: transaksi lain
        # yang sedang berjalan akan membuat pemanggilan di bawah mengembalikan
        # False tanpa menghapus apa pun, bukan karena status kunci.
        if hasattr(config._answers_lock, "locked"):
            self.assertFalse(
                config._answers_lock.locked(),
                "kunci config sedang dipegang transaksi lain — test tidak "
                "menguji apa pun",
            )

        # Entry point yang dipakai semua jalur clear setelah submit sukses.
        if hasattr(ev_mod, "clear_answers_if_unchanged"):
            ev_mod.clear_answers_if_unchanged(EXAM_ID, dict(OLD_PAYLOAD))
        else:
            # Bentuk lama: baca lalu hapus tanpa satu transaksi.
            if ev_mod.answers_match_disk(EXAM_ID, dict(OLD_PAYLOAD)):
                config.clear_answers(EXAM_ID)
        self._join_writers()

        self.assertEqual(
            config.load_answers(EXAM_ID), NEW_PAYLOAD,
            "berkas jawaban percobaan yang lebih baru dihapus oleh sukses yang "
            "TERLAMBAT dari percobaan lama: nilai siswa berikutnya hilang "
            "permanen, dan tidak ada yang tersisa untuk dipulihkan",
        )
        self.assertEqual(
            (config.load_answers_owner(EXAM_ID) or {}).get("student_key"),
            KEY_B,
            "sidecar pemilik jawaban yang lebih baru ikut hilang — tanpa "
            "sidecar, KEDUA gerbang konsumen gagal-TERBUKA (restore dan "
            "'Kirim Lagi' sama-sama menganggap jawaban itu milik siapa pun)",
        )

    def test_our_own_payload_is_still_cleared(self):
        # Kontrol positif: penjagaan tidak boleh membekukan jawaban yang
        # benar-benar sudah terkirim.
        if hasattr(ev_mod, "clear_answers_if_unchanged"):
            cleared = ev_mod.clear_answers_if_unchanged(
                EXAM_ID, dict(OLD_PAYLOAD))
        else:
            cleared = bool(ev_mod.answers_match_disk(EXAM_ID, OLD_PAYLOAD))
            if cleared:
                config.clear_answers(EXAM_ID)
        self._join_writers()
        self.assertTrue(cleared, "helper melaporkan 'tidak dihapus' untuk "
                                 "payload miliknya sendiri yang masih utuh")
        self.assertIsNone(
            config.load_answers(EXAM_ID),
            "jawaban milik sendiri tidak dihapus setelah submit sukses — PC "
            "lab mewarisi jawaban lama untuk siswa berikutnya",
        )
        self.assertIsNone(
            config.load_answers_owner(EXAM_ID),
            "sidecar yatim tidak ikut dibersihkan setelah jawabannya hilang",
        )

    def test_an_undeterminable_disk_is_never_deleted(self):
        # Fail-safe: kalau tidak bisa dipastikan, JANGAN menghapus.
        if not hasattr(ev_mod, "clear_answers_if_unchanged"):
            self.fail(
                "exam_viewer tidak punya helper clear yang atomik — "
                "pasangan baca-hapus masih bisa mengenai jawaban orang lain"
            )
        with mock.patch.object(config, "load_answers",
                               side_effect=OSError("file terkunci")):
            self.assertFalse(
                ev_mod.clear_answers_if_unchanged(EXAM_ID, dict(OLD_PAYLOAD)),
                "kegagalan membaca disk dianggap 'sama' — jawaban orang lain "
                "hilang tanpa jejak",
            )
        self.assertEqual(config.load_answers(EXAM_ID), OLD_PAYLOAD)


class AutoSubmitWorkerAtomicClearTestCase(_Sandbox):
    """Jalur auto-submit (worker background) — call site `exam_viewer`.py."""

    def _run_worker(self):
        fake_api = mock.Mock()
        fake_api.submit_with_retry.return_value = SubmitResponse(
            success=True, status="done", message="ok", congrats_message="Selesai")
        with mock.patch.object(ev_mod, "api", fake_api), \
             mock.patch.object(ev_mod, "notify"):
            ev_mod.ExamViewerWindow._background_submit_thread(
                None, "https://exam.example", EXAM_ID, "ABCD1234",
                "Andi", "N01", "9A", dict(OLD_PAYLOAD),
                "2026-01-01T00:00:00Z", "DESKTOP:h",
                {"nama": "Andi", "nomor_ujian": "N01", "kelas": "9A"},
            )

    def test_the_worker_does_not_delete_the_interleaved_newer_attempt(self):
        self._arm_interleaving()
        self._run_worker()
        self._join_writers()
        self.assertEqual(
            config.load_answers(EXAM_ID), NEW_PAYLOAD,
            "worker auto-submit menghapus jawaban yang menimpanya di tengah "
            "jendela baca-then-hapus",
        )
        self.assertEqual(
            (config.load_answers_owner(EXAM_ID) or {}).get("student_key"),
            KEY_B,
            "sidecar pemilik jawaban yang lebih baru ikut hilang",
        )


class _FakeSecurityEnforcer(QObject):
    """Antarmuka yang sama dengan SecurityEnforcer, tanpa sentuhan platform."""

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


class ManualSubmitAtomicClearTestCase(_Sandbox):
    """Jalur submit manual — `_cleanup_after_submit`."""

    def setUp(self) -> None:
        super().setUp()
        from PyQt5.QtWidgets import QApplication

        self.app = QApplication.instance() or QApplication([])
        module = importlib.import_module("examvan.ui.exam_viewer")
        self._live_viewer_class = module.ExamViewerWindow
        for patcher in (
            mock.patch.object(module, "SecurityEnforcer",
                              _FakeSecurityEnforcer),
            mock.patch.object(module, "ExamWebSocket"),
            mock.patch.object(api, "download_pdf",
                              side_effect=OSError("offline")),
            mock.patch.object(api, "send_access_log", return_value=False),
        ):
            patcher.start()
            self.addCleanup(patcher.stop)

    def _make_viewer(self):
        exam = Exam.from_json({
            "id": EXAM_ID, "name": "Ujian", "status": "active",
            "security_level": "low",
            "questions": [{"number": 1, "type": "single_choice",
                           "choices": ["A", "B"]}],
        })
        viewer = self._live_viewer_class(
            exam=exam,
            server_url="https://exam.example",
            token="ABCD1234",
            identity_data={"nama": "Andi", "nomor_ujian": "N01", "kelas": "9A"},
        )
        self.addCleanup(viewer.hide)
        return viewer

    def test_manual_cleanup_keeps_the_interleaved_newer_attempt(self):
        viewer = self._make_viewer()
        viewer._submitted_payload = dict(OLD_PAYLOAD)
        viewer._submitted = True
        self._arm_interleaving()

        with mock.patch.object(config, "mark_submitted"), \
             mock.patch.object(api, "send_access_log", return_value=False), \
             mock.patch.object(config, "load_start_time", return_value=""):
            viewer._cleanup_after_submit("Selesai")
        self._join_writers()

        self.assertEqual(
            config.load_answers(EXAM_ID), NEW_PAYLOAD,
            "`_cleanup_after_submit` menghapus jawaban percobaan yang lebih "
            "baru pada jendela baca-then-hapus yang sama",
        )
        self.assertEqual(
            (config.load_answers_owner(EXAM_ID) or {}).get("student_key"),
            KEY_B,
            "sidecar pemilik jawaban yang lebih baru ikut hilang",
        )
        self.assertIsNone(
            getattr(viewer, "_congrats_ref", None),
            "halaman selamat tetap muncul di atas layar percobaan berikutnya "
            "walaupun isinya sudah bukan milik percobaan ini",
        )


if __name__ == "__main__":
    unittest.main()