"""Unit tests for the admin-exit password resolver in examvan.ui.exam_viewer.

The password is what lets a supervisor close a running exam. It is
fail-closed: with no password configured, admin exit does NOT work at all
(there is no "any password" fallback), so a missing/misread file silently
locks the supervisor out mid-exam. These tests pin the resolution order and
the failure modes that would cause that.

Resolution order (first hit wins):
  1. env EXAMVAN_ADMIN_PASSWORD  — the long-standing manual way
  2. %LOCALAPPDATA%\\EXAMVAN\\admin_password.txt — written by
     EXAMVAN-Setup.exe so "install and go" needs no env var juggling
  3. None -> admin exit disabled
"""

from __future__ import annotations

import importlib
import os
import shutil
import tempfile
import unittest
from pathlib import Path
from unittest import mock


def _load_exam_viewer(env: dict):
    """(Re)import examvan.ui.exam_viewer with a controlled environment.

    The module reads the password at import time into the module-level
    _ADMIN_PASSWORD, so the environment has to be in place BEFORE the
    import — patching os.environ afterwards would come too late.
    """
    with mock.patch.dict(os.environ, env, clear=False):
        # LOCALAPPDATA must be ABSENT for the "no file, no env" case,
        # and mock.patch.dict cannot delete, so strip it explicitly.
        if "LOCALAPPDATA" not in env and "LOCALAPPDATA" in os.environ:
            saved = os.environ.pop("LOCALAPPDATA")
            try:
                mod = importlib.reload(
                    importlib.import_module("examvan.ui.exam_viewer")
                )
            finally:
                os.environ["LOCALAPPDATA"] = saved
        else:
            mod = importlib.reload(importlib.import_module("examvan.ui.exam_viewer"))
    return mod


class AdminPasswordResolverTestCase(unittest.TestCase):
    def setUp(self):
        self._tmp = tempfile.mkdtemp(prefix="examvan-adminpw-test-")
        self._local_appdata = Path(self._tmp)
        self._pw_file = self._local_appdata / "EXAMVAN" / "admin_password.txt"

    def tearDown(self):
        shutil.rmtree(self._tmp, ignore_errors=True)
        # Leave the module in its default (no password) state for other tests.
        _load_exam_viewer({"EXAMVAN_ADMIN_PASSWORD": "", "LOCALAPPDATA": str(self._local_appdata)})

    def _write_pw_file(self, content: str) -> None:
        self._pw_file.parent.mkdir(parents=True, exist_ok=True)
        self._pw_file.write_text(content, encoding="utf-8")

    # ---- env var (existing behaviour, must not regress) ----

    def test_env_var_is_used(self):
        mod = _load_exam_viewer(
            {"EXAMVAN_ADMIN_PASSWORD": "dari-env", "LOCALAPPDATA": str(self._local_appdata)}
        )
        self.assertEqual(mod._ADMIN_PASSWORD, "dari-env")

    # ---- installer-written file (new) ----

    def test_reads_password_from_installer_file(self):
        self._write_pw_file("rahasia123")
        mod = _load_exam_viewer(
            {"EXAMVAN_ADMIN_PASSWORD": "", "LOCALAPPDATA": str(self._local_appdata)}
        )
        self.assertEqual(mod._ADMIN_PASSWORD, "rahasia123")

    def test_file_password_is_stripped(self):
        # SaveStringToFile writes exactly what was typed, but a file edited
        # by hand in Notepad carries a trailing CRLF. Comparing the raw
        # string against QInputDialog input would then always fail.
        self._write_pw_file("rahasia123\r\n")
        mod = _load_exam_viewer(
            {"EXAMVAN_ADMIN_PASSWORD": "", "LOCALAPPDATA": str(self._local_appdata)}
        )
        self.assertEqual(mod._ADMIN_PASSWORD, "rahasia123")

    def test_env_var_wins_over_file(self):
        # Ordering matters: a teacher who exports the env var expects it to
        # take effect, even if an old password file is still on disk.
        self._write_pw_file("dari-file")
        mod = _load_exam_viewer(
            {"EXAMVAN_ADMIN_PASSWORD": "dari-env", "LOCALAPPDATA": str(self._local_appdata)}
        )
        self.assertEqual(mod._ADMIN_PASSWORD, "dari-env")

    # ---- fail-closed cases ----

    def test_no_password_anywhere_is_fail_closed(self):
        mod = _load_exam_viewer(
            {"EXAMVAN_ADMIN_PASSWORD": "", "LOCALAPPDATA": str(self._local_appdata)}
        )
        self.assertIsNone(mod._ADMIN_PASSWORD)

    def test_empty_file_is_treated_as_unset(self):
        # The installer deletes the file when the field is left blank, but
        # an empty leftover file must not resolve to "" — an empty password
        # would match an empty QInputDialog submission.
        self._write_pw_file("   \n")
        mod = _load_exam_viewer(
            {"EXAMVAN_ADMIN_PASSWORD": "", "LOCALAPPDATA": str(self._local_appdata)}
        )
        self.assertIsNone(mod._ADMIN_PASSWORD)

    def test_missing_file_is_fail_closed(self):
        # No file at all — the common case for portable EXAMVAN.exe.
        mod = _load_exam_viewer(
            {"EXAMVAN_ADMIN_PASSWORD": "", "LOCALAPPDATA": str(self._local_appdata)}
        )
        self.assertIsNone(mod._ADMIN_PASSWORD)

    def test_no_localappdata_is_fail_closed(self):
        # Linux, and any Windows process with a stripped environment:
        # no LOCALAPPDATA means no file lookup, and no crash either.
        mod = _load_exam_viewer({"EXAMVAN_ADMIN_PASSWORD": "hanya-env"})
        self.assertEqual(mod._ADMIN_PASSWORD, "hanya-env")

    def test_unreadable_file_does_not_raise(self):
        # A directory where the file should be raises IsADirectoryError,
        # which is an OSError subclass — it must be swallowed, because an
        # unhandled exception at import time means the app never starts.
        self._pw_file.parent.mkdir(parents=True, exist_ok=True)
        self._pw_file.mkdir()
        mod = _load_exam_viewer(
            {"EXAMVAN_ADMIN_PASSWORD": "", "LOCALAPPDATA": str(self._local_appdata)}
        )
        self.assertIsNone(mod._ADMIN_PASSWORD)


if __name__ == "__main__":
    unittest.main()
