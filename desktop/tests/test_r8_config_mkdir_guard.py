"""H7 (HIGH): `mkdir()` di LUAR `try` -> `PermissionError` lolos ke slot Qt.

Empat fungsi menulis ke `_CONFIG_DIR` dengan bentuk ini:

    _CONFIG_DIR.mkdir(parents=True, exist_ok=True)   # <- di luar try
    try:
        ...
    except OSError:
        ...

`exist_ok=True` hanya men-short-circuit `mkdir` kalau direktori SUDAH ADA.
Kalau belum ada (first run di akun baru, `%USERPROFILE%` yang di-redirect ke
 roaming share, folder OneDrive yang sedang terkunci, image lab yang dikunci
polisi), `mkdir` justru thanksgiving yang melempar — dan itu terjadi di
luar `try`, jadi tidak pernah tertangkap.

Dampak yang sudah diverifikasi dengan modul aslinya: keempat fungsi
melempar `PermissionError`. `ExamViewerWindow.__init__` memanggil
`save_start_time`, jadi window tidak pernah muncul sama sekali;
autosave tiap 2 detik jadi loop traceback dan jawaban tidak pernah sampai
ke disk.

Perbaikan: `mkdir` masuk ke daerah yang dijaga, dan kegagalannya HARUS
terlihat (warning + `exc_info`) — autosave yang gagal menulis tidak boleh
berpura-pura berhasil.
"""

from __future__ import annotations

import logging
import os
import shutil
import stat
import tempfile
import unittest
from pathlib import Path
from unittest import mock

from examvan import config


class _ReadOnlyParentSandbox(unittest.TestCase):
    """Direktori config DI BELUM ADA, di bawah induk yang read-only."""

    def setUp(self) -> None:
        self._base = tempfile.mkdtemp(prefix="examvan-r8-ro-")
        self.addCleanup(self._restore_perms)
        self.addCleanup(shutil.rmtree, self._base, ignore_errors=True)
        # Induk read-only: mkdir anak harus gagal dengan EACCES.
        self.parent = Path(self._base) / "readonly"
        self.parent.mkdir()
        self._patches = [
            mock.patch.object(config, "_CONFIG_DIR", self.parent / "examvan"),
            mock.patch.object(
                config, "_CONFIG_FILE",
                self.parent / "examvan" / "config.json",
            ),
        ]
        for p in self._patches:
            p.start()
            self.addCleanup(p.stop)
        self._old_cache = config._cache
        config._cache = None
        self.addCleanup(self._restore_cache)

    def _restore_perms(self) -> None:
        for path in (self.parent / "examvan", self.parent):
            try:
                os.chmod(path, 0o755)
            except OSError:
                pass

    def _restore_cache(self) -> None:
        config._cache = self._old_cache

    def _calls(self):
        return {
            "config.set": lambda: config.set("exam_token", "ABCD1234"),
            "save_answers": lambda: config.save_answers(7, {"1": "A"}),
            "save_answers_owner": lambda: config.save_answers_owner(
                7, "0812", "exam_number=0812",
            ),
            "save_start_time": lambda: config.save_start_time(
                7, "2026-01-01T00:00:00Z",
            ),
        }


class MkdirOutsideTryRaisesTest(_ReadOnlyParentSandbox):
    """RED: keempat fungsi melempar ke pemanggil."""

    def test_no_write_function_raises_when_the_config_dir_is_absent(self):
        # Direktori config sengaja TIDAK dibuat: `exist_ok=True` tidak
        # menolong, jadi `mkdir` benar-benar mencoba menulis ke induk.
        self.assertFalse(config._CONFIG_DIR.exists())
        for name, call in self._calls().items():
            with self.subTest(fn=name):
                os.chmod(self.parent, 0o555)
                try:
                    call()      # tidak boleh melempar
                finally:
                    os.chmod(self.parent, 0o755)

    def test_startup_path_is_the_worst_case(self):
        # `save_start_time` dipanggil dari konstruktor window ujian:
        # satu PermissionError di sini = tidak ada window sama sekali.
        os.chmod(self.parent, 0o555)
        try:
            config.save_start_time(9, "2026-01-01T00:00:00Z")
        finally:
            os.chmod(self.parent, 0o755)


class MkdirFailureMustBeObservableTest(_ReadOnlyParentSandbox):
    """Gagal menulis harus kelihatan, bukan diam-diam."""

    def _assert_warned(self, name, call):
        with self.assertLogs(config._log, level=logging.WARNING) as caught:
            call()
        records = [
            r for r in caught.records
            if getattr(r, "exc_info", None)
        ]
        self.assertTrue(
            records,
            f"{name}: kegagalan tidak ada di log sebagai warning "
            "dengan exc_info -- autosave yang gagal menulis akan "
            "berpura-pura sukses dan siswa tidak pernah tahu jawabannya "
            "tidak tersimpan",
        )
        self.assertIn(
            "PermissionError",
            "".join(
                logging.Formatter().formatException(r.exc_info)
                for r in records
            ),
            f"{name}: traceback yang dilog bukan PermissionError yang "
            "menjadi alasan gagalnya",
        )

    def test_every_write_failure_is_logged_with_the_cause(self):
        for name, call in self._calls().items():
            with self.subTest(fn=name):
                os.chmod(self.parent, 0o555)
                try:
                    self._assert_warned(name, call)
                finally:
                    os.chmod(self.parent, 0o755)

    def test_nothing_is_written_when_the_directory_cannot_be_created(self):
        os.chmod(self.parent, 0o555)
        try:
            for call in self._calls().values():
                with self.assertLogs(config._log, level=logging.WARNING):
                    call()
        finally:
            os.chmod(self.parent, 0o755)
        self.assertFalse(
            config._CONFIG_DIR.exists(),
            "berkas sempat tertulis walau direktori config tidak bisa dibuat",
        )


class ExistingReadOnlyDirectoryStillBehavesTest(unittest.TestCase):
    """Kasus lama (direktori sudah ada, read-only) tidak boleh berubah."""

    def setUp(self) -> None:
        self._base = tempfile.mkdtemp(prefix="examvan-r8-ro2-")
        self.addCleanup(shutil.rmtree, self._base, ignore_errors=True)
        self.dir = Path(self._base) / "examvan"
        self.dir.mkdir()
        self._patches = [
            mock.patch.object(config, "_CONFIG_DIR", self.dir),
            mock.patch.object(config, "_CONFIG_FILE",
                              self.dir / "config.json"),
        ]
        for p in self._patches:
            p.start()
            self.addCleanup(p.stop)
        self._old_cache = config._cache
        config._cache = None

        def _restore():
            os.chmod(self.dir, 0o755)
            config._cache = self._old_cache

        self.addCleanup(_restore)

    def test_set_does_not_raise_and_still_logs_the_cause(self):
        os.chmod(self.dir, 0o555)
        try:
            with self.assertLogs(config._log, level=logging.WARNING) as caught:
                config.set("exam_token", "ABCD1234")
        finally:
            os.chmod(self.dir, 0o755)
        self.assertTrue(
            [r for r in caught.records if getattr(r, "exc_info", None)],
            "kegagalan config.json yang sudah ada harus tetap terlihat",
        )

    def test_answers_are_readable_again_once_the_directory_is_writable(self):
        os.chmod(self.dir, 0o555)
        try:
            with self.assertLogs(config._log, level=logging.WARNING):
                config.save_answers(9, {"1": "A"})
        finally:
            os.chmod(self.dir, 0o755)
        self.assertIsNone(config.load_answers(9))
        config.save_answers(9, {"1": "A"})
        self.assertEqual(config.load_answers(9), {"1": "A"})
        mode = stat.S_IMODE((self.dir / "answers_9.dat").stat().st_mode)
        self.assertFalse(mode & stat.S_IROTH)


if __name__ == "__main__":
    unittest.main()
