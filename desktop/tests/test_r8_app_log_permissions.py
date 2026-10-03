"""LOW — `app.log` satu-satunya berkas yang ditulis klien tanpa 0600/ACL.

Bug
---
`_setup_logging` (`__main__.py`) memasang `RotatingFileHandler` telanjang.
Semua berkas kredensial lain dibuat 0600 + DACL pemilik: `config.json`
(`os.open(..., 0o600)` lalu `_restrict_to_owner`), `answers_<id>.dat` dan
sidecar-ownernya, sampai `api._pdf_tmp_path`. Log tidak.

Di POSIX dengan umask 022, `open()` menghasilkan 0644 — world-readable.
Isinya bukan hal yang bisa diabaikan: `__main__._validated_token` mencatat
token di level WARNING saat jatuh ke fallback QLineEdit, dan
`server_config.build_student_key_source` mencatat token itu sendiri saat
degradasi ke sumber `"token"`. Jadi log bisa memuat kredensial KELAS,
bertahan sampai 1 MB + 3 backup, dan di PC lab multi-akun bisa dibaca akun
lain. Di Windows `chmod` tidak menyentuh ACL, jadi tanpa DACL eksplisit
berkas tetap terbuka untuk group Everyone.

Yang diperbaiki di sini adalah PERMISSION-nya saja. Isi log yang memuat
token ada di `server_config.py` dan `__main__.py:_validated_token`;
baris pencatatan token tersebut dilaporkan, tidak dihapus di ronde ini.
"""

from __future__ import annotations

import os
import shutil
import stat
import tempfile
import unittest
from pathlib import Path
from unittest import mock

from examvan import __main__ as main_mod
from examvan import config


class _LogSandbox(unittest.TestCase):
    def setUp(self) -> None:
        self.home = Path(tempfile.mkdtemp(prefix="examvan-r8-log-"))
        self.addCleanup(shutil.rmtree, self.home, True)
        import logging

        self.root = logging.getLogger()
        before = list(self.root.handlers)
        self.addCleanup(lambda: self._restore(before))

    def _restore(self, before):
        for handler in list(self.root.handlers):
            if handler not in before:
                self.root.removeHandler(handler)
                try:
                    handler.close()
                except Exception:
                    pass

    def _setup(self, **patches):
        import logging

        ctx = [mock.patch.object(Path, "home", return_value=self.home)]
        ctx.extend(patches)
        with ctx[0]:
            for extra in ctx[1:]:
                extra.start()
                self.addCleanup(extra.stop)
            main_mod._setup_logging()
        self.path = self.home / ".config" / "examvan" / "app.log"
        return logging.getLogger()


class ActiveLogIsOwnerOnlyTestCase(_LogSandbox):
    def test_the_log_file_is_not_world_readable(self):
        self._setup()
        self.assertTrue(self.path.exists(), "kontrol: log harus dibuat")
        if os.name == "nt":
            self.skipTest("mode POSIX tidak berlaku di Windows")
        mode = stat.S_IMODE(self.path.stat().st_mode)
        self.assertEqual(
            mode, 0o600,
            f"app.log mode {oct(mode)} — world-readable di PC lab, padahal "
            "berkas kredensial lain 0600",
        )

    def test_the_log_file_is_created_at_all(self):
        # Kontrol: kalau perbaikan nanti membuat log tidak pernah ditulis,
        # test lain harus gagal di sini, bukan diam.
        self._setup()
        self.assertTrue(self.path.is_file())


class RotatedBackupsAreAlsoRestrictedTestCase(_LogSandbox):
    @unittest.skipIf(os.name == "nt", "mode POSIX tidak berlaku di Windows")
    def test_backups_left_by_an_older_version_are_locked_down(self):
        log_dir = self.home / ".config" / "examvan"
        log_dir.mkdir(parents=True, exist_ok=True)
        for name in ("app.log", "app.log.1", "app.log.3"):
            target = log_dir / name
            target.write_text("lama\n", encoding="utf-8")
            os.chmod(target, 0o644)
        self._setup()
        for name in ("app.log", "app.log.1", "app.log.3"):
            mode = stat.S_IMODE((log_dir / name).stat().st_mode)
            self.assertEqual(
                mode, 0o600,
                f"{name} masih 0644 — log versi lama berisi token kelas "
                "tetap bisa dibaca akun lain di PC yang sama",
            )


class WindowsDaclIsRequestedTestCase(_LogSandbox):
    """Di Windows `chmod` tidak mengubah ACL, jadi DACL harus diminta."""

    def test_the_windows_acl_helper_is_called_for_the_log(self):
        with mock.patch.object(config, "_windows_restrict_to_owner",
                               return_value=True) as acl:
            with mock.patch.object(os, "name", "nt"):
                with mock.patch.object(Path, "home", return_value=self.home):
                    main_mod._setup_logging()
        self.assertTrue(
            acl.called,
            "tidak ada permintaan DACL untuk app.log — di Windows file tetap "
            "world-readable meski `chmod` sukses",
        )
        self.assertTrue(
            str(acl.call_args[0][0]).endswith("app.log"),
            f"ACL diminta untuk {acl.call_args[0][0]}, bukan app.log",
        )


if __name__ == "__main__":
    unittest.main()