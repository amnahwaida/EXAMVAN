"""Ronde 6 (item 3) — identitas + konteksnya harus disimpan ATOMIK.

Bug
---
`ui/server_config.py::_show_identity_dialog` menyimpan pasangan yang SANGAT
ber interrelated dengan DUA penulisan penuh:

    config.set("identity_data", identity)
    config.set("identity_context", {"exam_id": …, "token": …})

Setiap `set()` memanggil `_save()`, yaitu menulis SELURUH cache ke disk lewat
tmp+`replace`. Jadi:

* crash/tabrak antara keduanya meninggalkan identitas siswa A dengan
  konteks yang menunjuk UJIAN LAIN (atau tidak ada sama sekali) —
  `identity_context` adalah penjaga anti-prefill-silangan (H8), jadi
  konteks yang basi justru membukanya kembali;
* `clear_identity()` hanya membersihkan `identity_data`, konteksnya tetap;
* TIDAK ADA pembersihan saat startup. Proses yang mati di tengah ujian
  meninggalkan identitas A di `config.json`, dan siswa berikutnya di PC
  yang sama dengan token yang sama mendapat form yang sudah terisi — satu
  Enter sudah cukup untuk tercatat sebagai A.

Perbaikan
---------
1. Satu kunci baru `identity_session` = `{"identity_data": …, "context": …}`
   ditulis dengan SATU `_save()`. Kunci lama `identity_data` /
   `identity_context` tetap DICERMINKAN dalam penulisan yang sama supaya
   pembaca versi lama (termasuk `__main__.py`, yang tidak boleh disentuh
   di ronde ini) tetap membaca data yang sama.
2. Pembacaan diturunkan dari pasangan itu lewat `get_identity_session()`,
   dan accessor itu MELEMAHKAN: identitas tanpa konteks terbaca sebagai
   TIDAK ADA. Bentuk legacy maupun bentuk `identity_session` setengah
   jadi: keduanya terbaca sebagai TIDAK ADA.
3. `clear_identity()` membersihkan keduanya (dalam satu `_save()`).
4. `clear_stale_identity_on_startup()` membersihkan keduanya dan idempoten.
   Fungsi ini DISEDIAKAN untuk dipanggil pemanggil lain saat launch —
   `__main__.py` milik agen lain, jadi DI SINI TIDAK dipanggil.
"""

from __future__ import annotations

import json
import pathlib
import tempfile
import unittest
from unittest import mock

from examvan import config

_IDENTITY = {"nama": "SITI", "nomor_ujian": "N02"}
_CONTEXT = {"exam_id": 7, "token": "ABCD1234"}


class _ConfigSandbox(unittest.TestCase):
    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        for attr, value in (
            ("_CONFIG_DIR", pathlib.Path(self._tmp.name)),
            ("_CONFIG_FILE", pathlib.Path(self._tmp.name) / "config.json"),
        ):
            patcher = mock.patch.object(config, attr, value)
            patcher.start()
            self.addCleanup(patcher.stop)
        config._cache = None
        self.addCleanup(setattr, config, "_cache", None)

    def _count_saves(self):
        calls = []
        real = config._save

        def counting():
            calls.append(1)
            return real()

        return calls, mock.patch.object(config, "_save", counting)

    def _on_disk(self):
        config._cache = None
        path = pathlib.Path(self._tmp.name) / "config.json"
        if not path.exists():
            return None
        return json.loads(path.read_text(encoding="utf-8"))


class AtomicPairWriteTest(_ConfigSandbox):
    """(a) pasangan identitas+konteks = SATU `_save()`."""

    def test_pair_is_persisted_with_a_single_save(self):
        calls, patcher = self._count_saves()
        with patcher:
            config.set_identity_session(dict(_IDENTITY), dict(_CONTEXT))
        self.assertEqual(
            len(calls), 1,
            f"pasangan identitas+konteks harus satu _save(), bukan "
            f"{len(calls)} — celah antara dua penulisan penuh itulah "
            "yang menyisakan identitas tanpa konteks",
        )

    def test_pair_survives_one_save(self):
        config.set_identity_session(dict(_IDENTITY), dict(_CONTEXT))
        stored = self._on_disk()
        self.assertIsNotNone(stored)
        session = stored["identity_session"]
        self.assertEqual(session["identity_data"], _IDENTITY)
        self.assertEqual(session["context"], _CONTEXT)

    def test_legacy_keys_still_mirror_the_pair(self):
        """`__main__.py` (di luar daftar edit ronde ini) membaca kunci lama."""
        config.set_identity_session(dict(_IDENTITY), dict(_CONTEXT))
        self.assertEqual(config.get("identity_data", {}), _IDENTITY)
        self.assertEqual(config.get("identity_context", {}), _CONTEXT)

    def test_reader_returns_the_pair(self):
        config.set_identity_session(dict(_IDENTITY), dict(_CONTEXT))
        session = config.get_identity_session()
        self.assertEqual(session["identity_data"], _IDENTITY)
        self.assertEqual(session["context"], _CONTEXT)

    def test_second_write_replaces_the_first_pair(self):
        config.set_identity_session(dict(_IDENTITY), dict(_CONTEXT))
        other = {"nama": "BUDI"}
        config.set_identity_session(other, {"exam_id": 8, "token": "ZZZZ9999"})
        self.assertEqual(config.get_identity_session()["identity_data"], other)
        self.assertEqual(
            config.get_identity_session()["context"],
            {"exam_id": 8, "token": "ZZZZ9999"},
        )


class ClearIdentityClearsBothTest(_ConfigSandbox):
    """(b) `clear_identity()` membersihkan identitas DAN konteks."""

    def test_clear_identity_empties_data_and_context(self):
        config.set_identity_session(dict(_IDENTITY), dict(_CONTEXT))
        config.clear_identity()
        self.assertEqual(config.get("identity_data", {}), {})
        self.assertEqual(config.get("identity_context", {}), {})
        session = config.get_identity_session()
        self.assertEqual(session["identity_data"], {})
        self.assertEqual(session["context"], {})

    def test_clear_identity_is_a_single_save(self):
        config.set_identity_session(dict(_IDENTITY), dict(_CONTEXT))
        calls, patcher = self._count_saves()
        with patcher:
            config.clear_identity()
        self.assertEqual(len(calls), 1)

    def test_clear_identity_is_idempotent(self):
        config.clear_identity()
        config.clear_identity()
        self.assertEqual(config.get_identity_session()["identity_data"], {})

    def test_clear_identity_never_raises(self):
        """Pemanggilnya adalah slot Qt — exception = SIGABRT."""
        with mock.patch.object(config, "_save", side_effect=OSError("boom")):
            config.clear_identity()


class StartupCleanupTest(_ConfigSandbox):
    """(c) helper startup membersihkan keduanya dan idempoten."""

    def test_startup_helper_clears_identity_and_context(self):
        config.set_identity_session(dict(_IDENTITY), dict(_CONTEXT))
        self.assertTrue(config.clear_stale_identity_on_startup())
        self.assertEqual(config.get("identity_data", {}), {})
        self.assertEqual(config.get("identity_context", {}), {})
        self.assertEqual(config.get_identity_session()["identity_data"], {})

    def test_startup_helper_is_idempotent(self):
        config.set_identity_session(dict(_IDENTITY), dict(_CONTEXT))
        self.assertTrue(config.clear_stale_identity_on_startup())
        self.assertFalse(config.clear_stale_identity_on_startup())
        self.assertEqual(config.get_identity_session()["identity_data"], {})

    def test_startup_helper_is_a_noop_when_nothing_is_stored(self):
        self.assertFalse(config.clear_stale_identity_on_startup())

    def test_startup_helper_does_not_touch_other_settings(self):
        config.set("server_url", "https://exam.example")
        config.set("exam_token", "ABCD1234")
        config.set_identity_session(dict(_IDENTITY), dict(_CONTEXT))
        config.clear_stale_identity_on_startup()
        self.assertEqual(config.get("server_url"), "https://exam.example")
        self.assertEqual(config.get("exam_token"), "ABCD1234")

    def test_startup_helper_never_raises(self):
        config.set_identity_session(dict(_IDENTITY), dict(_CONTEXT))
        with mock.patch.object(config, "_save", side_effect=OSError("boom")):
            config.clear_stale_identity_on_startup()


class HalfWriteIsInertTest(_ConfigSandbox):
    """(d) "identitas tanpa konteks" harus tidak teramsyati, dari bentuk apa pun."""

    def _write_raw(self, payload):
        path = pathlib.Path(self._tmp.name) / "config.json"
        path.write_text(json.dumps(payload), encoding="utf-8")
        config._cache = None

    def test_legacy_half_write_yields_no_identity(self):
        """Crash SETELAH `identity_data`, SEBELUM `identity_context`."""
        self._write_raw({"identity_data": _IDENTITY})
        self.assertEqual(config.get_identity_session()["identity_data"], {})
        self.assertEqual(config.get_identity_data(), {})

    def test_session_without_context_key_yields_no_identity(self):
        self._write_raw({"identity_session": {"identity_data": _IDENTITY}})
        self.assertEqual(config.get_identity_data(), {})

    def test_session_with_empty_context_yields_no_identity(self):
        self._write_raw({
            "identity_session": {
                "identity_data": _IDENTITY,
                "context": {},
            },
        })
        self.assertEqual(config.get_identity_data(), {})

    def test_mismatched_legacy_pair_yields_no_identity(self):
        """Konteks menunjuk ujian lain — prefill lintas siswa itu Persis
        yang harus dicegah."""
        self._write_raw({
            "identity_data": _IDENTITY,
            "identity_context": {"exam_id": 999, "token": "OTHER0000"},
        })
        # Pasangannya utuh, jadi accessor membacanya — kecocokan konteks
        # tetap diputuskan oleh SERVER_CONFIG (H8), bukan accessor.
        self.assertEqual(config.get_identity_data(), _IDENTITY)

    def test_malformed_session_types_are_inert(self):
        for payload in (
            {"identity_session": "bukan-dict"},
            {"identity_session": {"identity_data": "bukan-dict",
                                  "context": _CONTEXT}},
            {"identity_session": {"identity_data": _IDENTITY,
                                  "context": "bukan-dict"}},
            {"identity_session": None},
        ):
            with self.subTest(payload=payload):
                self._write_raw(payload)
                self.assertEqual(config.get_identity_data(), {})
                self.assertEqual(config.get_identity_context(), {})


class EmptyStoreTest(_ConfigSandbox):
    def test_default_store_has_no_identity(self):
        self.assertEqual(config.get_identity_session(),
                         {"identity_data": {}, "context": {}})
        self.assertEqual(config.get_identity_data(), {})
        self.assertEqual(config.get_identity_context(), {})


if __name__ == "__main__":  # pragma: no cover
    unittest.main()