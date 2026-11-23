"""DPAPI storage layer for Windows answers/secrets.

The real DPAPI calls can only run on Windows, so these tests simulate the
platform branch: `_dpapi_available()` is True and the two ctypes wrappers are
replaced with a reversible fake. That still exercises the on-disk envelope,
the plaintext fallback, legacy XOR files, and the non-Windows failure path.
"""

from __future__ import annotations

import base64
import json
import logging
import shutil
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

from examvan import config


class _FakeDpapi:
    def __init__(self):
        self._counter = 0

    def protect(self, raw: bytes) -> bytes:
        self._counter += 1
        return b"FAKE:" + str(self._counter).encode("ascii") + b":" + raw[::-1]

    def unprotect(self, blob: bytes):
        if not blob.startswith(b"FAKE:"):
            return None
        try:
            _marker, _counter, payload = blob.split(b":", 2)
        except ValueError:
            return None
        return payload[::-1]

    def install(self, testcase: unittest.TestCase) -> None:
        for target, value in (
            ("_dpapi_available", lambda: True),
            ("_dpapi_protect", self.protect),
            ("_dpapi_unprotect", self.unprotect),
        ):
            patcher = mock.patch.object(config, target, value)
            patcher.start()
            testcase.addCleanup(patcher.stop)


class DpapiEnvelopeTest(unittest.TestCase):
    def setUp(self) -> None:
        self.fake = _FakeDpapi()
        self.fake.install(self)

    def test_answers_are_wrapped_and_round_trip(self):
        encoded = config._encode_answers({"1": "A", "2": ["B", "C"]})
        self.assertTrue(encoded.startswith(config._DPAPI_PREFIX))
        self.assertEqual(
            config._decode_answers(encoded),
            {"1": "A", "2": ["B", "C"]},
        )

    def test_secrets_are_wrapped_and_round_trip(self):
        encoded = config._encode_secret("ABCD1234")
        self.assertTrue(encoded.startswith(config._DPAPI_PREFIX))
        self.assertEqual(config._decode_secret(encoded), "ABCD1234")
        self.assertEqual(config._decode_secret_strict(encoded), "ABCD1234")

    def test_dpapi_failure_falls_back_to_xor(self):
        with mock.patch.object(config, "_dpapi_protect", return_value=None):
            encoded = config._encode_secret("ABCD1234")
        self.assertFalse(encoded.startswith(config._DPAPI_PREFIX))
        self.assertEqual(config._decode_secret(encoded), "ABCD1234")

    def test_legacy_xor_answers_still_decode(self):
        raw = json.dumps({"1": "A"}, separators=(",", ":")).encode("utf-8")
        legacy = base64.urlsafe_b64encode(
            config._xor_obfuscate(raw)
        ).decode("ascii")
        self.assertEqual(config._decode_answers(legacy), {"1": "A"})

    def test_plaintext_secret_fallback_is_unchanged(self):
        self.assertEqual(
            config._decode_secret("token with spaces"),
            "token with spaces",
        )


class DpapiUnavailableTest(unittest.TestCase):
    def test_dpapi_envelope_is_not_treated_as_raw_text(self):
        blob = base64.urlsafe_b64encode(b"not-really-dpapi").decode("ascii")
        value = config._DPAPI_PREFIX + blob
        with mock.patch.object(config, "_dpapi_available", return_value=False):
            self.assertEqual(config._decode_secret(value), "")
            self.assertIsNone(config._decode_secret_strict(value))
            self.assertIsNone(config._decode_answers(value))

    def test_non_windows_encryption_keeps_using_xor(self):
        with mock.patch.object(config, "_dpapi_available", return_value=False):
            encoded = config._encode_secret("ABCD1234")
        self.assertFalse(encoded.startswith(config._DPAPI_PREFIX))
        self.assertEqual(config._decode_secret(encoded), "ABCD1234")


class _ConfigSandbox(unittest.TestCase):
    def setUp(self) -> None:
        self._base = tempfile.mkdtemp(prefix="examvan-dpapi-")
        self.addCleanup(shutil.rmtree, self._base, ignore_errors=True)
        self.dir = Path(self._base) / "examvan"
        self.dir.mkdir()
        for attr, value in (
            ("_CONFIG_DIR", self.dir),
            ("_CONFIG_FILE", self.dir / "config.json"),
        ):
            patcher = mock.patch.object(config, attr, value)
            patcher.start()
            self.addCleanup(patcher.stop)
        old_cache = config._cache
        config._cache = None
        self.addCleanup(setattr, config, "_cache", old_cache)
        self.fake = _FakeDpapi()
        self.fake.install(self)

    def restart(self) -> None:
        config._cache = None

    def raw_config(self) -> str:
        return (self.dir / "config.json").read_text(encoding="utf-8")


class DpapiConfigSandboxTest(_ConfigSandbox):
    def test_token_survives_restart_and_is_not_plaintext_on_disk(self):
        config.set("exam_token", "TOKENSECRET01")
        self.assertNotIn("TOKENSECRET01", self.raw_config())
        self.assertIn(config._DPAPI_PREFIX, self.raw_config())
        self.restart()
        self.assertEqual(config.get("exam_token"), "TOKENSECRET01")

    def test_answers_and_owner_survive_restart(self):
        config.save_answers(7, {"1": "A", "2": "B"})
        config.save_answers_owner(7, "0812", label="exam_number=0812")
        self.assertTrue(
            (self.dir / "answers_7.dat").read_text(encoding="ascii").startswith(
                config._DPAPI_PREFIX
            )
        )
        self.restart()
        self.assertEqual(config.load_answers(7), {"1": "A", "2": "B"})
        owner = config.load_answers_owner(7)
        self.assertIsNotNone(owner)
        self.assertEqual(owner["student_key"], "0812")
        self.assertEqual(owner["label"], "exam_number=0812")

    def test_history_deduplicates_by_plaintext_not_ciphertext(self):
        for token in ("AAAA0001", "BBBB0002", "AAAA0001", "BBBB0002"):
            config.set("exam_token", token)
        history = config.get("exam_token_history")
        self.assertEqual(len(history), 2)
        self.assertEqual(
            {config._decode_secret(h) for h in history},
            {"AAAA0001", "BBBB0002"},
        )

    def test_windows_file_becomes_unreadable_on_a_non_windows_host(self):
        config.set("exam_token", "TOKENSECRET01")
        self.restart()
        with mock.patch.object(config, "_dpapi_available", return_value=False):
            with self.assertLogs(config._log, level=logging.WARNING):
                self.assertEqual(config.get("exam_token"), "")

    def test_unreadable_dpapi_token_is_not_written_back_as_ciphertext(self):
        config.set("exam_token", "TOKENSECRET01")
        self.restart()
        with mock.patch.object(config, "_dpapi_available", return_value=False):
            config.get("exam_token")
            config.set("remember_url", False)
        data = json.loads(self.raw_config())
        self.assertNotIn(config._SECRET_TOKEN_KEY, data)


@unittest.skipUnless(sys.platform == "win32", "DPAPI hanya di Windows")
class RealDpapiRoundTripTest(unittest.TestCase):
    """Jalankan jalur ctypes asli saat test suite berjalan di Windows."""

    def test_protect_then_unprotect_returns_the_same_bytes(self):
        raw = b"examvan-dpapi-roundtrip"
        blob = config._dpapi_protect(raw)
        self.assertIsNotNone(blob, "CryptProtectData gagal di Windows")
        self.assertEqual(config._dpapi_unprotect(blob), raw)


if __name__ == "__main__":
    unittest.main()
