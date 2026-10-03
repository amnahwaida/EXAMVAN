"""Item MEDIUM di `examvan/config.py`: kegagalan yang tidak terlihat, dan
data yang hilang tanpa jejak.

Enam hal yang diverifikasi terhadap modul aslinya:

1. `save_answers` menelan SEMUA kegagalan (`os.replace` ditolak Defender,
   ENOSPC) tanpa satu baris log pun — Verified: nol record log untuk
   `PermissionError` maupun `ENOSPC` paksa. Siswa tidak melihat apa-apa,
   autosave berhenti diam-diam, dan `ExamViewerWindow._save_answers` tetap
   membersihkan `_answers_dirty` sehingga UI terlihat sehat.
2. `save_answers_owner` menulis dengan `open(tmp, "w")` + `chmod` sesudahnya,
   jadi selama jendela tulis isinya ada di mode umask — padahal
   `save_answers` sengaja `os.open(..., 0o600)` dan mendokumentasikan
   alasannya.
3. `_load()` me-reset `config.json` yang rusak ke default dengan TANPA log
   dan tanpa cadangan. Yang hilang permanen: `exam_token_history` (satu-
   satunya cara membaca `answers_*.dat` versi lama), `start_time_*`, dan
   marker `submitted_*`.
4. `_save()` melakukan fsync pada file tapi TIDAK pada direktorinya, jadi
   `replace()` tidak durable terhadap mati listrik — persis ancaman yang
   dicatat di docstring modul.
5. `set("exam_token", ...)` melakukan read-modify-write riwayat token di
   LUAR `_save_lock` (fix M7 ronde 7 hanya menutup `set_identity_session`),
   jadi dua rotasi berbarengan bisa.drop satu entri riwayat.
6. `PROTECTED_DACL_SECURITY_INFORMATION` bernilai `0x80000`, yang itu
   `WRITE_OWNER` di `ACCESS_MASK` — bukan bit PROTECTED_DACL yang
   terdokumentasi (`0x80000000`). Akibatnya DACL di-MERGE dengan ACL
   warisan, kebalikan dari yang dijanjikan docstring.
"""

from __future__ import annotations

import errno
import json
import logging
import os
import shutil
import sys
import tempfile
import threading
import unittest
from pathlib import Path
from unittest import mock

from examvan import config


class _ConfigSandbox(unittest.TestCase):
    def setUp(self) -> None:
        self._base = tempfile.mkdtemp(prefix="examvan-r8-dur-")
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
        self._old_cache = config._cache
        config._cache = None
        self.addCleanup(self._restore_cache)

    def _restore_cache(self) -> None:
        config._cache = self._old_cache


# ---------------------------------------------------------------------------
# 1 — save_answers tidak boleh menelan kegagalan
# ---------------------------------------------------------------------------


class SaveAnswersMustReportFailureTest(_ConfigSandbox):
    def test_forced_permission_error_is_logged(self):
        # `replace` ditolak: itulah yang terjadi saat Defender memindai
        # file yang baru ditulis dan `_answers_lock` menahan sebentar.
        def deny(src, target):
            raise PermissionError(errno.EACCES, "Permission denied", str(src))

        with mock.patch.object(Path, "replace", deny):
            with self.assertLogs(config._log, level=logging.WARNING) as caught:
                config.save_answers(7, {"1": "A"})
        self.assertTrue(
            [r for r in caught.records if getattr(r, "exc_info", None)],
            "gagal menyimpan jawaban tidak menghasilkan satu pun log: "
            "autosave berhenti tanpa jejak dan UI tetap terlihat sehat",
        )

    def test_forced_disk_full_is_logged(self):
        # ENOSPC: `write`/`fsync` gagal, cleanup `unlink` justru berhasil
        # (selalu berhasil untuk file yang baru dibuat), jadi tidak ada
        # satu pun cabang yang pernah melog.

        def no_space(fd):
            raise OSError(errno.ENOSPC, "No space left on device")

        with mock.patch.object(os, "fsync", no_space):
            with self.assertLogs(config._log, level=logging.WARNING) as caught:
                config.save_answers(7, {"1": "A"})
        self.assertTrue(
            [r for r in caught.records if getattr(r, "exc_info", None)],
            "disk penuh tidak dilaporkan apa pun",
        )

    def test_caller_can_tell_success_from_failure(self):
        # `_answers_dirty` di `ExamViewerWindow` hanya boleh dibersihkan
        # kalau jawabannya benar-benar ada di disk; itu butuh nilai
        # balik, bukan tebakan.
        with mock.patch.object(Path, "replace", side_effect=PermissionError("x")):
            self.assertFalse(config.save_answers(7, {"1": "A"}))
        self.assertTrue(config.save_answers(7, {"1": "A"}))
        self.assertEqual(config.load_answers(7), {"1": "A"})

    def test_a_failed_write_leaves_no_readable_garbage(self):
        with mock.patch.object(Path, "replace", side_effect=PermissionError("x")):
            with self.assertLogs(config._log, level=logging.WARNING):
                config.save_answers(7, {"1": "A"})
        leftovers = [p.name for p in self.dir.iterdir()
                     if p.name.startswith("answers_7")]
        self.assertEqual(leftovers, [], f"file sementara tertinggal: {leftovers}")


# ---------------------------------------------------------------------------
# 2 — sidecar owner harus ketat sejak create
# ---------------------------------------------------------------------------


class OwnerSidecarIsTightFromCreateTest(_ConfigSandbox):
    def test_the_temp_file_is_never_group_or_world_readable(self):
        # `open(tmp, "w")` membuat file dengan mode 0666 & ~umask = 0644
        # pada umask 022; `chmod` berikutnya terlambat karena isinya sudah
        # ada di disk selama jendela itu.
        seen = []
        real_open = os.open

        def spy(path, flags, mode=0o777, **kwargs):
            fd = real_open(path, flags, mode, **kwargs)
            if "answers_9.owner.tmp" in str(path):
                seen.append(Path(path).stat().st_mode & 0o777)
            return fd

        with mock.patch.object(os, "open", spy):
            config.save_answers_owner(9, "0812", "exam_number=0812")
        self.assertEqual(len(seen), 1, "sidecar tidak dibuat lewat os.open")
        self.assertEqual(
            seen[0] & 0o077, 0,
            f"file sementara sidecar punya mode {seen[0]:o} saat create — "
            "PII siswa world-readable selama jendela tulis",
        )

    def test_the_written_sidecar_is_private(self):
        config.save_answers_owner(9, "0812", "exam_number=0812")
        mode = (self.dir / "answers_9.owner").stat().st_mode & 0o777
        self.assertEqual(mode & 0o077, 0, f"mode sidecar akhir {mode:o}")

    def test_the_sidecar_temp_file_is_fsynced(self):
        with mock.patch.object(os, "fsync", wraps=os.fsync) as spy:
            config.save_answers_owner(9, "0812", "exam_number=0812")
        self.assertGreaterEqual(
            spy.call_count, 1,
            "isi sidecar tidak di-fsync sebelum replace: jawaban + pemiliknya "
            "bisa hilang terpisah saat mati listrik",
        )


# ---------------------------------------------------------------------------
# 3 — config.json rusak: log + cadangan
# ---------------------------------------------------------------------------


CORRUPT_SHAPES = {
    "truncated": b'{"exam_token": "ABCD1234", "exam_token_his',
    "not_an_object": b'["exam_token"]',
    "json_null": b"null",
    "binary": b"\xff\xfe\x00\x00not json at all",
    "empty": b"",
}


class CorruptConfigMustBeVisibleTest(_ConfigSandbox):
    def _write_corrupt(self, payload: bytes) -> None:
        (self.dir / "config.json").write_bytes(payload)
        config._cache = None

    def test_every_corrupt_shape_is_reported(self):
        for name, payload in CORRUPT_SHAPES.items():
            with self.subTest(shape=name):
                self._write_corrupt(payload)
                with self.assertLogs(config._log, level=logging.WARNING) as c:
                    config.get("exam_token")
                self.assertTrue(
                    [r for r in c.records
                     if getattr(r, "exc_info", None)
                     or "config.json" in r.getMessage()],
                    f"{name}: file rusak direset ke default tanpa satu pun "
                    "log -- pemeriksa tidak akan tahu config hilang",
                )

    def test_the_corrupt_bytes_are_kept_for_recovery(self):
        # Yang hilang saat reset: `exam_token_history` (satu-satunya cara
        # membaca `answers_*.dat` versi lama), `start_time_*`, dan marker
        # `submitted_*`. Tanpa cadangan, semua itu hilang permanen.
        payload = json.dumps({
            "exam_token": "ABCD1234",
            "exam_token_history": ["QQkADGZxfm4="],
            "submitted_7": {"abc123": "exam_number=0812"},
        }).encode("utf-8")
        self._write_corrupt(payload[: len(payload) // 2])
        config.get("exam_token")
        bak = self.dir / "config.json.bak"
        self.assertTrue(
            bak.exists(),
            "config.json yang rusak dihapus tanpa cadangan; isinya tidak "
            "bisa dipulihkan",
        )
        self.assertEqual(bak.read_bytes(), payload[: len(payload) // 2])

    def test_the_backup_is_not_world_readable(self):
        self._write_corrupt(CORRUPT_SHAPES["truncated"])
        with self.assertLogs(config._log, level=logging.WARNING):
            config.get("exam_token")
        bak = self.dir / "config.json.bak"
        self.assertTrue(bak.exists())
        self.assertEqual(bak.stat().st_mode & 0o077, 0)

    def test_a_valid_file_leaves_no_backup_and_no_warning(self):
        config.set("exam_token", "ABCD1234")
        with mock.patch.object(config._log, "warning") as warn:
            config._cache = None
            config.get("exam_token")
        warn.assert_not_called()
        self.assertFalse((self.dir / "config.json.bak").exists())

    def test_a_backup_failure_does_not_break_startup(self):
        self._write_corrupt(CORRUPT_SHAPES["truncated"])
        with mock.patch.object(Path, "write_bytes",
                               side_effect=OSError("read-only")):
            with mock.patch.object(os, "open",
                                   side_effect=OSError("read-only")):
                with self.assertLogs(config._log, level=logging.WARNING):
                    self.assertEqual(config.get("exam_token"), "")
                    self.assertEqual(config.get_all()["remember_url"], True)


# ---------------------------------------------------------------------------
# 4 — rename harus durable: fsync direktorinya juga
# ---------------------------------------------------------------------------


class DirectoryFsyncTest(_ConfigSandbox):
    def _record_open_fsync(self, run):
        """Kembalikan daftar (path, fd) untuk setiap `os.fsync` yang terjadi.

        Path direkam SAAT fsync dipanggil, bukan saat fd dibuat: nomor fd
        dipakai ulang begitu file ditutup, jadi fd dari file sementara bisa
        sama dengan fd direktori yang dibuka belakangan.
        """
        real_open, real_fsync = os.open, os.fsync
        opened, synced = {}, []

        def spy_open(path, flags, mode=0o777, **kwargs):
            fd = real_open(path, flags, mode, **kwargs)
            opened[fd] = str(path)
            return fd

        def spy_fsync(fd):
            synced.append((opened.get(fd, "?"), fd))
            return real_fsync(fd)

        with mock.patch.object(os, "open", spy_open), \
                mock.patch.object(os, "fsync", spy_fsync):
            run()
        return synced

    def test_the_directory_is_fsynced_after_the_rename(self):
        # fsync(file) hanya menjamin isi file; entri direktori hasil
        # `replace()` baru durable kalau direktorinya ikut di-fsync.
        # Tanpa itu, mati listrik setelah submit bisa mengembalikan
        # `config.json` versi lama — dan `exam_token_history` ikut
        # terputus, jadi `answers_*.dat` versi lama tidak terbaca lagi.
        synced = self._record_open_fsync(
            lambda: config.set("exam_token", "ABCD1234"),
        )
        self.assertTrue(
            [path for path, _ in synced if Path(path) == self.dir],
            f"direktori config tidak di-fsync setelah replace; yang di-fsync "
            f"hanya: {synced}",
        )

    def test_the_file_itself_is_still_fsynced(self):
        synced = self._record_open_fsync(
            lambda: config.set("exam_token", "ABCD1234"),
        )
        self.assertTrue(
            [path for path, _ in synced
             if "config.json" in path and ".tmp." in path],
            f"isi config.json tidak lagi di-fsync; yang di-fsync: {synced}",
        )

    def test_the_directory_fsync_is_skipped_on_windows(self):
        # Windows tidak punya padanan fsync untuk direktori; ini no-op
        # yang disengaja, bukan kegagalan diam-diam.
        with mock.patch.object(os, "name", "nt"), \
                mock.patch.object(os, "open", side_effect=OSError("no")):
            config._fsync_dir(self.dir)
        os.name = "posix"


# ---------------------------------------------------------------------------
# 5 — rotasi token harus di bawah lock
# ---------------------------------------------------------------------------


class _RecordingLock:
    """RLock yang mencatat kapan lock sedang dipegang."""

    def __init__(self, real):
        self._real = real
        self._depth = 0
        self.peak = 0

    def __enter__(self):
        self._real.acquire()
        self._depth += 1
        self.peak = max(self.peak, self._depth)
        return self

    def __exit__(self, *exc):
        self._depth -= 1
        self._real.release()
        return False

    def acquire(self, *a, **k):
        return self._real.acquire(*a, **k)

    def release(self):
        return self._real.release()

    def held(self) -> bool:
        return self._depth > 0


class TokenHistoryMutationUnderLockTest(_ConfigSandbox):
    def test_history_read_modify_write_happens_under_the_save_lock(self):
        # `set("exam_token", ...)` mutate riwayat di luar lock, lalu
        # memanggil `_save()` yang lock-nya baru diambil di dalam. Dua
        # rotasi dari thread berbeda bisa sama-sama membaca riwayat yang
        # sama lalu menimpa satu sama lain — entri hilang, dan entri itu
        # satu-satunya kunci yang masih bisa dipakai untuk membaca
        # `answers_*.dat` versi lama.
        lock = _RecordingLock(threading.RLock())
        depth_when_encoding = []

        real_encode = config._encode_secret

        def spy_encode(text):
            depth_when_encoding.append(lock.held())
            return real_encode(text)

        config.set("exam_token", "AAAA0001")
        config._cache = None
        config._load()

        with mock.patch.object(config, "_save_lock", lock), \
                mock.patch.object(config, "_encode_secret", spy_encode):
            config.set("exam_token", "BBBB0002")

        self.assertTrue(
            depth_when_encoding,
            "riwayat token tidak pernah di-encode: test tidak menguji apa "
            "yang ia kira mengujinya",
        )
        self.assertTrue(
            all(depth_when_encoding),
            "mutasi riwayat token berjalan DI LUAR _save_lock: dua rotasi "
            "berbarengan bisa drop satu entri riwayat",
        )
        self.assertEqual(
            config.get("exam_token_history") and
            config._decode_secret(config.get("exam_token_history")[0]),
            "AAAA0001",
            "riwayat tidak menyimpan token sebelumnya sama sekali",
        )

    def test_the_cache_write_itself_is_under_the_lock(self):
        lock = _RecordingLock(threading.RLock())
        depths = []

        def spy_save():
            depths.append(lock.held())

        with mock.patch.object(config, "_save_lock", lock), \
                mock.patch.object(config, "_save", spy_save):
            config.set("exam_token", "CCCC0003")

        self.assertTrue(depths and all(depths),
                        "cache ditulis di luar _save_lock")


# ---------------------------------------------------------------------------
# 6 — konstanta Windows
# ---------------------------------------------------------------------------


class ProtectedDaclConstantTest(unittest.TestCase):
    """REASONED FROM DOCS, TIDAK DIJALANKAN di host Linux.

    Nilai `PROTECTED_DACL_SECURITY_INFORMATION` di MS-DTYP
    `SECURITY_INFORMATION` adalah `0x80000000`. Nilai `0x80000` yang
    terpasang adalah `WRITE_OWNER` di `ACCESS_MASK` —disalirkan ke
    `SetNamedSecurityInfoW` berarti DACL di-merge dengan ACL warisan,
    kebalikan dari yang dijanjikan docstring di `_windows_restrict_to_owner`.
    """

    def test_the_constant_is_the_documented_protected_dacl_bit(self):
        self.assertEqual(
            config.PROTECTED_DACL_SECURITY_INFORMATION, 0x80000000,
        )

    def test_it_is_not_an_access_mask_bit(self):
        # 0x80000 == WRITE_OWNER; menyalakan WRITE_OWNER untuk
        # pengaman berkas berarti mengubah pemilik, bukan melindungi DACL.
        self.assertNotEqual(config.PROTECTED_DACL_SECURITY_INFORMATION,
                            0x80000)

    def test_the_flag_handed_to_SetSecurityInfo_is_the_protected_one(self):
        # Ditangkap dari argumen yang benar-benar dikirim ke advapi32;
        # tidak ada Win32 di host ini, jadi yang diperiksa adalah
        # OPSI kombinasinya dan bahwa flag itu terpakai di jalur
        # sebenarnya — bukan hasil panggilan API sungguhan.
        recorded = {}

        def set_entries_acl(count, entries, old, new_dacl_ptr):
            # Yang dilakukan Win32: mengisi DACL baru. Di sini kita hanya
            # membuat penunjuk itu non-null supaya sisi aplikasi
            # (_windows_restrict_to_owner) lanjut ke SetNamedSecurityInfoW.
            new_dacl_ptr._obj.value = 0x1234
            return 0

        def set_named(name, objtype, security_information, owner, group,
                      dacl, sacl):
            recorded["security_information"] = security_information
            return 0

        import ctypes

        advapi32 = mock.MagicMock()
        advapi32.SetEntriesInAclW.side_effect = set_entries_acl
        advapi32.SetNamedSecurityInfoW.side_effect = set_named
        fake_windll = mock.MagicMock()
        fake_windll.advapi32 = advapi32

        ctypes_mod = sys.modules["ctypes"]
        with mock.patch.object(ctypes_mod, "windll", fake_windll,
                               create=True), \
                mock.patch.dict(os.environ, {"USERNAME": "siswa"}):
            ok = config._windows_restrict_to_owner(Path("/tmp/config.json"))

        self.assertTrue(ok, "jalur ACL Windows tidak sampai ke panggilan API")
        self.assertIn(
            "security_information", recorded,
            "SetNamedSecurityInfoW tidak pernah dipanggil: konstanta yang "
            "diperiksa tidak ikut terpakai di jalur sebenarnya",
        )
        self.assertEqual(
            recorded["security_information"] & 0x80000000, 0x80000000,
            "bit PROTECTED_DACL tidak dikirim ke SetNamedSecurityInfoW",
        )
        self.assertEqual(
            recorded["security_information"] & 0x80000, 0,
            "WRITE_OWNER (0x80000) ikut dikirim sebagai SECURITY_INFORMATION "
            "-- DACL akan di-merge dengan ACL warisan, bukan diganti",
        )


if __name__ == "__main__":
    unittest.main()
