"""Regresi ronde 8 — item yang dikerjakan langsung di coordinator.

Tiga kelompok:

1. `clear_answers_if_unchanged` dipindah ke `config` dan `_answers_lock`
   jadi `RLock`. Dua konsekuensi yang harus terkunci:
   - helper Config_bersama dipakai KETIGA call site (auto-submit, cleanup
     manual, recovery), supaya tidak ada lagi dua implementasi read-then-delete
     yang bisa melenceng;
   - `RLock` wajib, karena helper memegang lock itu sambil memanggil
     `clear_answers`. Dengan `Lock` biasa itu deadlock yang muncul sebagai
     "aplikasi hang setelah submit".

2. Token kelas tidak boleh masuk `app.log`. `build_student_key` bisa turun ke
   token; pada mode static-token itu kredensial hasil SELURUH KELAS, dan
   log tidak dibersihkan antar siswa di PC lab yang dipakai bersama.

3. Plafon jumlah soal: body 32MB masih bisa memuat ratusan ribu entri soal,
   dan `build_from_questions` membuat satu widget per soal di GUI thread —
   satu respons membekukan seluruh jendela ujian.
"""

from __future__ import annotations

import json
import os
import shutil
import sys
import tempfile
import threading
import time
import unittest
from pathlib import Path
from unittest import mock

os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")

from examvan import api, config


# ---------------------------------------------------------------------------
# 1 — helper atomik milik config, dipakai semua call site
# ---------------------------------------------------------------------------


class _ConfigSandbox(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.mkdtemp(prefix="examvan-r8-shared-")
        self._patches = [
            mock.patch.object(config, "_CONFIG_DIR", Path(self.tmp)),
            mock.patch.object(
                config, "_CONFIG_FILE", Path(self.tmp) / "config.json"),
        ]
        for p in self._patches:
            p.start()
        config._cache = None

    def tearDown(self):
        for p in self._patches:
            p.stop()
        config._cache = None
        shutil.rmtree(self.tmp, ignore_errors=True)


class SharedAtomicClearTestCase(_ConfigSandbox):
    EXAM = 4242

    def test_the_helper_lives_in_config_not_in_the_ui_layer(self):
        # Alasannya lock: `_answers_lock` di `config` yang dipegang
        # `save_answers`/`save_answers_owner`, jadi hanya dari sana
        # read-decide-delete bisa atomik terhadap penulis jawaban.
        self.assertTrue(
            hasattr(config, "clear_answers_if_unchanged"),
            "helper atomik harus ada di config; meletakkannya di lapisan UI "
            "membuat `server_config` tidak bisa memakainya dan menyisakan dua "
            "implementasi yang bisa melenceng",
        )

    def test_the_viewer_helper_is_only_a_thin_wrapper(self):
        from examvan.ui import exam_viewer as ev

        self.assertTrue(
            hasattr(ev, "clear_answers_if_unchanged"),
            "call site di viewer harus tetap punya nama yang dipanggil",
        )
        # Kalau viewer punya implementasi sendiri, ada dua salinan aturan.
        source = Path(ev.__file__).read_text(encoding="utf-8")
        self.assertEqual(
            source.count("config.clear_answers_if_unchanged("),
            1,
            "viewer harus meneruskan ke config, bukan mengimplementasikan "
            "ulang (dua implementasi = dua aturan)",
        )

    def test_recovery_path_uses_the_atomic_helper(self):
        from examvan.ui import server_config

        src = Path(server_config.__file__).read_text(encoding="utf-8")
        self.assertIn(
            "config.clear_answers_if_unchanged(",
            src,
            "jalur recovery masih read-then-delete non-atomik: di antara "
            "pembacaan dan unlink, autosave percobaan berikutnya bisa ikut "
            "terhapus (sudah ~84 detik worker ini berjalan)",
        )

    def test_own_payload_is_cleared(self):
        payload = {"1": "C", "2": "A"}
        config.save_answers(self.EXAM, dict(payload))
        config.save_answers_owner(self.EXAM, student_key="0812",
                                  label="exam_number=0812")
        self.assertTrue(config.clear_answers_if_unchanged(self.EXAM, payload))
        self.assertIsNone(config.load_answers(self.EXAM))

    def test_a_newer_payload_is_never_deleted(self):
        mine = {"1": "C", "2": "A"}
        theirs = {"1": "X", "2": "Y"}
        config.save_answers(self.EXAM, dict(theirs))
        config.save_answers_owner(self.EXAM, student_key="0813",
                                  label="exam_number=0813")
        self.assertFalse(
            config.clear_answers_if_unchanged(self.EXAM, mine),
            "jawaban percobaan yang lebih baru ikut terhapus",
        )
        self.assertEqual(config.load_answers(self.EXAM), theirs)

    def test_an_unknown_payload_never_deletes(self):
        # Fail-closed: tidak ada bukti isi disk milik kita → jangan hapus.
        config.save_answers(self.EXAM, {"1": "C"})
        self.assertFalse(config.clear_answers_if_unchanged(self.EXAM, None))
        self.assertEqual(config.load_answers(self.EXAM), {"1": "C"})

    def test_absent_answers_report_success(self):
        # Tidak ada yang perlu dihapus; pemanggil boleh lanjut.
        self.assertTrue(config.clear_answers_if_unchanged(self.EXAM, {"1": "C"}))

    def test_a_throwing_read_never_deletes(self):
        config.save_answers(self.EXAM, {"1": "C"})
        with mock.patch.object(config, "load_answers",
                               side_effect=OSError("disk error")):
            self.assertFalse(config.clear_answers_if_unchanged(
                self.EXAM, {"1": "C"}))
        self.assertEqual(config.load_answers(self.EXAM), {"1": "C"})

    def test_a_writer_cannot_slip_in_between_decision_and_delete(self):
        """Interleaving sungguhan: penulis lain = THREAD LAIN.

        Race yang ditutup lock ini bukan "pemanggilan bersarang di thread
        yang sama": yang nyata adalah worker submit di satu thread
        (`_background_submit_thread`) membacanya sementara thread GUI
        autosave menulis. Thread yang sama tidak relevan karena `_answers_lock`
        sengaja reentrant — jadi simulasi memakai thread sungguhan, kalau tidak
        test ini hanya mengukur reentensi RLock dan bukan atomisitas.
        """
        mine = {"1": "C", "2": "A"}
        theirs = {"1": "X", "2": "Y"}
        real_load = config.load_answers
        for attempt in range(15):
            with self.subTest(attempt=attempt):
                config.save_answers(self.EXAM, dict(mine))
                config.save_answers_owner(self.EXAM, student_key="0812",
                                          label="exam_number=0812")
                state = {"done": False}

                def fake_load(exam_id, _real=real_load):
                    current = _real(exam_id)
                    if state["done"] or current != mine:
                        return current
                    state["done"] = True
                    writer = threading.Thread(
                        target=lambda: (
                            config.save_answers(self.EXAM, dict(theirs)),
                            config.save_answers_owner(
                                self.EXAM, student_key="0813",
                                label="exam_number=0813"),
                        ),
                        daemon=True,
                    )
                    writer.start()
                    # Beri waktu penulis masuk ke `os.replace`-nya. Kalau
                    # helper memegang `_answers_lock`, penulis terblokir di sini
                    # dan baru menulis SETELAH keputusan dibuat.
                    writer.join(0.05)
                    return current

                with mock.patch.object(config, "load_answers", fake_load):
                    config.clear_answers_if_unchanged(self.EXAM, dict(mine))
                # Beri penulis kesempatan menyelesaikan `os.replace`-nya kalau
                # memang terblokir tadi (balik dari transaksi kita).
                time.sleep(0.02)
                self.assertEqual(
                    config.load_answers(self.EXAM), theirs,
                    f"jawaban percobaan yang lebih baru terhapus pada "
                    f"percobaan {attempt}",
                )

    def test_the_lock_is_reentrant(self):
        # Kalau `Lock` biasa, helper yang memegang lock ini sambil memanggil
        # `clear_answers` (yang someday butuh lock sama) akan deadlock.
        lock = config._answers_lock
        self.assertTrue(
            hasattr(lock, "_is_owned") or hasattr(lock, "locked"),
            "lock harus bisa diinspeksi",
        )
        acquired = []
        done = threading.Event()

        def holder():
            with lock:
                acquired.append(True)
                # Count 2 pada lock yang sama: harus berhasil kalau reentrant.
                with lock:
                    pass
                done.set()

        t = threading.Thread(target=holder, daemon=True)
        t.start()
        self.assertTrue(done.wait(5), "lock tidak reentrant — akan deadlock")
        t.join(2)
        self.assertEqual(acquired, [True])

    def test_different_threads_still_serialise(self):
        # Reentrant TIDAK boleh berarti dua thread bisa masuk bersamaan.
        lock = config._answers_lock
        inside = []
        overlap = []
        active = threading.Semaphore(1)

        def worker():
            for _ in range(20):
                with lock:
                    if not active.acquire(blocking=False):
                        overlap.append(True)
                    inside.append(1)
                    active.release()

        threads = [threading.Thread(target=worker, daemon=True)
                   for _ in range(3)]
        for t in threads:
            t.start()
        for t in threads:
            t.join(10)
        self.assertEqual(overlap, [],
                         "dua writer masuk ke critical section bersamaan")
        self.assertEqual(len(inside), 60)


# ---------------------------------------------------------------------------
# 2 — token kelas tidak boleh masuk log
# ---------------------------------------------------------------------------


class TokenNeverReachesTheLogTestCase(_ConfigSandbox):
    def test_the_token_key_is_redacted_for_logging(self):
        from examvan.ui import server_config

        redacted = server_config._redact_key_for_log("ABCD1234", "token")
        self.assertNotIn(
            "ABCD1234", redacted,
            "token kelas masuk app.log; pada mode static-token itu kredensial "
            "hasil seluruh kelas dan log tidak dibersihkan antar siswa",
        )
        self.assertIn("token", redacted.lower())

    def test_a_student_key_is_still_logged(self):
        from examvan.ui import server_config

        # Nama/nomor bukan kredensial dan sangat berguna saat menelusuri
        # jawaban salah orang — jadi harus tetap muncul.
        self.assertIn("0812", server_config._redact_key_for_log("0812",
                                                                "exam_number"))

    def test_no_module_logs_the_token_value_itself(self):
        from examvan.ui import server_config

        src = Path(server_config.__file__).read_text(encoding="utf-8")
        self.assertNotIn("(kunci=%r)", src,
                         "kunci identitas masih dicetak mentah; saat "
                         "sumbernya `token`, itu kredensial kelas")

    def test_main_does_not_log_the_token_value(self):
        from examvan import __main__ as main_mod

        src = Path(main_mod.__file__).read_text(encoding="utf-8")
        self.assertNotIn(
            'QLineEdit (%s)',
            src,
            "token dicetak apa adanya ke log saat validated_token kosong",
        )


# ---------------------------------------------------------------------------
# 3 — plafon jumlah soal
# ---------------------------------------------------------------------------


class QuestionCountCeilingTestCase(unittest.TestCase):
    @staticmethod
    def _response(n: int):
        return {"questions": [{"number": str(i), "options": []}
                              for i in range(n)]}

    def _get(self, n: int):
        with mock.patch.object(api, "_make_request",
                               return_value=self._response(n)):
            return api.get_exam_by_token("https://s", "ABCD1234")

    def test_a_normal_exam_is_untouched(self):
        for n in (0, 1, 40, 200, api._MAX_QUESTIONS):
            with self.subTest(n=n):
                r = self._get(n)
                self.assertNotEqual(r.error, "too_many_questions")

    def test_one_over_the_limit_is_refused_honestly(self):
        r = self._get(api._MAX_QUESTIONS + 1)
        self.assertFalse(r.success)
        self.assertEqual(r.error, "too_many_questions")
        self.assertIn("pengawas", r.message,
                      "pesan harus jelas dan bisa dibaca siswa, bukan traceback")

    def test_an_absurd_count_never_reaches_the_viewer(self):
        # 500k entri akan membuat `build_from_questions` membuat 500k widget
        # di GUI thread dan membekukan jendela sebelum soal pertama tampil.
        r = self._get(500_000)
        self.assertFalse(r.success)
        self.assertEqual(r.error, "too_many_questions")

    def test_a_missing_or_wrong_typed_questions_field_is_not_a_refusal(self):
        for payload in ({}, {"questions": None}, {"questions": "banyak"},
                        {"questions": {"1": {}}}):
            with self.subTest(payload=payload):
                with mock.patch.object(api, "_make_request",
                                       return_value=payload):
                    r = api.get_exam_by_token("https://s", "ABCD1234")
                self.assertNotEqual(r.error, "too_many_questions")


if __name__ == "__main__":
    sys.exit(unittest.main())