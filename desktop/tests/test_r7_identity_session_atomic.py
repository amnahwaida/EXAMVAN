"""M7 (MEDIUM) — mutate+save `set_identity_session` harus ATOMIK terhadap
penulis lain.

Bug
---
`_save()` menyerialisasi penulis di bawah `_save_lock`, tapi
`set_identity_session` melakukan tiga mutasi dict TERLEPASA dari lock itu:

    store = _load()          # _load()ellos keluar LIVE _cache, bukan salinan
    store["identity_session"] = {...}
    store["identity_data"]    = payload_identity
    store["identity_context"] = payload_context
    _save()                   # lock baru diambil DI SINI

`_connect_thread` bisa menjalankan `config.set("exam_token", ...)` di antara
mutasi pertama dan `_save()`, dan `set()` itu menjalankan `_save()`-nya sendiri.
Tercermin di disk: `identity_session` BARU berdampingan dengan `identity_mirror`
LAMA — persis state setengah jadi yang seharusnya mustahil.

Urutannya persis seperti `_cleanup_at_exit` yang sekarang memanggil tiga
`set()` terpisah, dan seperti `clear_stale_identity_on_startup`: jawaban yang
salah di sini tidak selalu salah untuk siswa yang sedang dijaga — kadang
hanya untuk siswa BERIKUTNYA, yang lalu menerima prefill milik orang lain
(anti-prefill-silangan H8).

Test di bawah dua-duanya murni config: satu deterministik (menah `_save_lock`
penulis lain dan membuktikan mutasi pun ikut tertahan) dan satu stress test
yang menyisipkan penulis kedua dari thread lain lalu memeriksa invarian disk
setiap kali ada dump.
"""

from __future__ import annotations

import json
import os
import shutil
import tempfile
import threading
import time
import unittest
from pathlib import Path
from unittest import mock

os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")

from examvan import config

IDENTITY_A = {"nama": "Andi", "nomor_ujian": "N01", "kelas": "9A"}
IDENTITY_B = {"nama": "Budi", "nomor_ujian": "N02", "kelas": "9A"}
CONTEXT_A = {"exam_id": 1, "token": "AAAA1111"}
CONTEXT_B = {"exam_id": 2, "token": "BBBB2222"}


class _Sandbox(unittest.TestCase):
    """config diarahkan ke direktori sementara supaya config.json asli utuh."""

    def setUp(self) -> None:
        self._tmp = Path(tempfile.mkdtemp(prefix="examvan-r7-atomic-"))
        self.addCleanup(shutil.rmtree, self._tmp, True)
        for p in (
            mock.patch.object(config, "_CONFIG_DIR", self._tmp),
            mock.patch.object(config, "_CONFIG_FILE", self._tmp / "config.json"),
        ):
            p.start()
            self.addCleanup(p.stop)
        config._cache = None
        self.addCleanup(setattr, config, "_cache", None)

    # -- helper ------------------------------------------------------------

    def _on_disk(self) -> dict:
        path = self._tmp / "config.json"
        if not path.exists():
            return {}
        try:
            with open(path, "r", encoding="utf-8") as f:
                data = json.load(f)
        except (OSError, ValueError):
            return {}
        return data if isinstance(data, dict) else {}

    def _mixed_state(self, data: dict) -> bool:
        """True bila state setengah jadi: session baru + mirror lama."""
        session = data.get("identity_session")
        if not isinstance(session, dict) or not session:
            return False
        sid = session.get("identity_data")
        sctx = session.get("context")
        # Bentuk kanonik dan cermin WAJIB identik. Yang tidak ada cermin
        # (versi lama) bukan state setengah jadi.
        if "identity_data" not in data and "identity_context" not in data:
            return False
        return (data.get("identity_data") != sid
                or data.get("identity_context") != sctx)


class SetIdentitySessionIsAtomicTestCase(_Sandbox):
    """`_save()` di dalam `set_identity_session` tidak boleh punya jeda."""

    def test_mutations_are_blocked_while_another_writer_holds_the_lock(self):
        """Deterministik: cache bersama tidak boleh tersentuh di luar lock.

        Sifat yang diuji langsung, tanpa mengandalkan}}: writer lain sedang
        memegang `_save_lock` (meniru `config.set()` dari `_connect_thread`),
        jadi `set_identity_session` belum boleh memutasikan cache yang sama --
        bukan hanya belum boleh menulis ke disk.
        """
        config.set_identity_session(IDENTITY_A, CONTEXT_A)
        before = dict(config._load())
        self.assertEqual(before["identity_data"], IDENTITY_A)

        started = threading.Event()
        done = threading.Event()

        def _writer():
            started.set()
            config.set_identity_session(IDENTITY_B, CONTEXT_B)
            done.set()

        t = threading.Thread(target=_writer)
        # Penulis lain memegang `_save_lock` -- persis kondisi `config.set()`
        # dari `_connect_thread` yang sedang di tengah jalan.
        config._save_lock.acquire()
        try:
            t.start()
            self.assertTrue(started.wait(10), "writer tidak pernah mulai")
            # Beri waktu penulis berjalan sampai titik di mana ia pasti
            # sudah selesai memutasikan cache kalau tidak ada lock.
            time.sleep(0.3)
            now = config._load()
            self.assertEqual(
                now["identity_data"], IDENTITY_A,
                "set_identity_session memutasi cache bersama SAHAM orang lain "
                f"sedang memegang _save_lock (sekarang {now['identity_data']!r}) -- "
                "penerbit lain bisa mem-persist session baru bersama mirror lama",
            )
            self.assertEqual(
                now["identity_session"],
                {"identity_data": IDENTITY_A, "context": CONTEXT_A},
                "cache bersama berubah di luar _save_lock",
            )
        finally:
            config._save_lock.release()

        self.assertTrue(done.wait(10), "penulis tidak pernah selesai")
        self.assertFalse(
            self._mixed_state(self._on_disk()),
            f"disk berisi state setengah jadi: {self._on_disk()}",
        )

    def test_no_cross_thread_writer_ever_sees_a_half_written_session(self):
        """Stress: dua penulis sungguhan, invarian diperiksa tiap dump."""
        config.set_identity_session(IDENTITY_A, CONTEXT_A)
        config._cache = None

        violations = []
        stop = threading.Event()

        real_save = config._save

        def _checked_save():
            real_save()
            data = self._on_disk()
            if self._mixed_state(data):
                violations.append(data)

        rounds = 400
        with mock.patch.object(config, "_save", _checked_save):
            def _session_writer():
                for i in range(rounds):
                    if stop.is_set():
                        return
                    config.set_identity_session(IDENTITY_B, CONTEXT_B)
                    config.set_identity_session(IDENTITY_A, CONTEXT_A)
                    i += 1

            def _token_writer():
                for i in range(rounds):
                    if stop.is_set():
                        return
                    config.set("exam_token", "TOK%04d" % (i % 100))
                    i += 1

            t1 = threading.Thread(target=_session_writer)
            t2 = threading.Thread(target=_token_writer)
            old_interval = __import__("sys").getswitchinterval()
            __import__("sys").setswitchinterval(1e-6)
            try:
                t1.start()
                t2.start()
                t1.join(120)
                t2.join(120)
            finally:
                __import__("sys").setswitchinterval(old_interval)
                stop.set()

        self.assertFalse(
            violations[:3],
            "state setengah jadi terekam di disk saat dua penulis berjalan "
            "bersamaan: session `identity_session` baru berdampingan dengan "
            "cermin `identity_data`/`identity_context` lama — inilah kebocoran "
            "prefill lintas siswa (H8) yang seharusnya mustahil",
        )
        self.assertFalse(self._mixed_state(self._on_disk()))

    def test_a_normal_write_still_lands_atomically(self):
        # Kontrol positif: mutasi + satu dump, tidak ada yang tertinggal.
        config.set_identity_session(IDENTITY_B, CONTEXT_B)
        data = self._on_disk()
        self.assertFalse(self._mixed_state(data), data)
        self.assertEqual(data["identity_session"]["identity_data"], IDENTITY_B)
        self.assertEqual(data["identity_data"], IDENTITY_B)
        self.assertEqual(data["identity_context"], CONTEXT_B)
        self.assertEqual(data["identity_session"]["context"], CONTEXT_B)
        # Dan accessor combined membaca pasangan yang utuh.
        session = config.get_identity_session()
        self.assertEqual(session["identity_data"], IDENTITY_B)
        self.assertEqual(session["context"], CONTEXT_B)


class ConcurrentSetStillSerialisesTestCase(_Sandbox):
    """`set_identity_session` tidak boleh mengunci terlalu lama."""

    def test_a_plain_set_is_not_starved_by_the_session_write(self):
        config.set_identity_session(IDENTITY_A, CONTEXT_A)
        config._cache = None

        errors = []

        def _other():
            try:
                for _ in range(50):
                    config.set("exam_token", "TOK")
            except Exception as exc:  # noqa: BLE001 — harus tercatat, bukan dibiarkan
                errors.append(exc)

        t = threading.Thread(target=_other)
        t.start()
        t.join(30)
        self.assertFalse(t.is_alive(), "config.set() hanya writer biasa, tidak boleh "
                                       "terkunci oleh set_identity_session")
        self.assertEqual(errors, [])


if __name__ == "__main__":
    unittest.main()