"""Temuan review aplikasi Windows (30 September 2026) — regresi dijaga di sini.

Empat temuan, satu file, satu aturan: setiap test di bawah ini GAGAL pada
kode lama dan HIJAU setelah perbaikan (TDD RED → GREEN).

1. Hook zombie — `_start_keyboard_hook()` yang gagal meninggalkan thread
   pump + `_hook_thread_id` global menggantung. `release_strict_mode()`
   hanya menghentikan hook ketika `_hook_installed` True, jadi percobaan
   yang gagal mengotori state global dan percobaan berikutnya bisa
   memasang hook KEDUA sementara yang pertama tidak pernah dilepas.

2. Backup hangus saat restore gagal — `restore_windows_settings()`
   menghapus `windows_state.json` bahkan ketika `_set_screen_saver_active()`
   gagal. Kelas bug yang sama (persis) baru saja ditemukan di sisi Linux:
   `linux_backend._gnome_ws_restore()` dulu menghapus `gnome_backup.json`
   tanpa memulihkan apa pun. Aturannya harus sama di dua platform:
   backup hanya boleh dihapus SETELAH nilai asli benar-benar kembali.

3. Installer senyap menggantung — mismatch password di `CurStepChanged`
   memunculkan `MsgBox` TANPA guard `WizardSilent`. Pada `/VERYSILENT`
   (CI smoke test, deploy 30 PC) MsgBox tidak pernah bisa dijawab:
   instalasi menggantung selamanya. Kontras dengan blok uninstall yang
   sudah dijaga `UninstallSilent` — install path-nya terlewat.

4. Celah blokir keyboard — Win+Enter (Narrator), Win+C (Copilot),
   Win+J (picker), dan Win+F6..F12 lolos padahal Win+F1 diblokir.
   Keputusan produk: SEMUA harus diblokir — Copilot adalah asisten AI
   yang bisa menjawab soal. Test lama yang mendokumentasikan pass-through
   Win+C/Win+J diperbarui di test_windows_backend.py.
"""

from __future__ import annotations

import json
import tempfile
import threading
import unittest
from pathlib import Path
from unittest import mock

import examvan.security.windows_backend as wb

REPO_ROOT = Path(__file__).resolve().parents[2]
ISS_PATH = REPO_ROOT / "windows" / "installer" / "examvan.iss"


# ---------------------------------------------------------------------------
# 1. Hook zombie
# ---------------------------------------------------------------------------


class _LivePump:
    """Thread palsu yang 'hidup' sampai stop() dipanggil."""

    def __init__(self) -> None:
        self.stopped = False

    def is_alive(self) -> bool:
        return not self.stopped

    def join(self, timeout=None) -> None:
        self.stopped = True


class ZombieHookThreadTestCase(unittest.TestCase):
    """Hook yang gagal dipasang tidak boleh meninggalkan state menggantung."""

    def setUp(self):
        self.backend = wb.WindowsBackend()
        # State global bersih di awal setiap test.
        patchers = [
            mock.patch.object(wb, "_hook_id", None),
            mock.patch.object(wb, "_hook_thread", None),
            mock.patch.object(wb, "_hook_thread_id", None),
        ]
        for p in patchers:
            p.start()
            self.addCleanup(p.stop)

    def _start_with_failing_install(self):
        """Jalankan _start_keyboard_hook() seolah SetWindowsHookExW gagal.

        Jalur yang disimulasikan: thread berjalan, menandai ready, TAPI
        `_hook_id` tidak pernah terisi (install gagal di tengah) — dan ia
        meninggalkan `_hook_thread_id` + thread pump yang masih hidup.
        Persis jejak yang ditinggalkan kegagalan di dunia nyata.
        """
        ready = threading.Event()  # kosong: produksi memanggil clear() dulu
        pump = _LivePump()

        def failing_hook_thread_func():
            # Mimpi buruknya: kegagalan meninggalkan jejak, lalu 'selesai'
            # tanpa pernah memasang hook dan tanpa pump yang hidup.
            wb._hook_thread_id = 4242
            wb._hook_ready.set()

        started_threads = []

        def fake_thread_factory(target=None, daemon=None):
            t = mock.Mock()
            t.is_alive.return_value = True
            t.join.side_effect = lambda timeout=None: setattr(
                pump, "stopped", True
            )
            # Jalankan target SYNCHRONOUS — seperti thread yang selesai
            # sebelum caller sempat menghentikannya.
            target()
            started_threads.append(t)
            t.native_id = 4242
            t._pump = pump
            return t

        with mock.patch.object(wb, "_hook_ready", ready), \
             mock.patch.object(wb, "_hook_thread_func", failing_hook_thread_func), \
             mock.patch.object(threading, "Thread", fake_thread_factory):
            ok = self.backend._start_keyboard_hook()
        return ok, started_threads, pump

    def test_failed_install_cleans_up_the_thread_state(self):
        posted = []
        with mock.patch.object(wb, "_PostThreadMessageW", create=True,
                               side_effect=lambda tid, msg, w, l: posted.append(msg)):
            ok, threads, pump = self._start_with_failing_install()

        self.assertFalse(
            ok, "install gagal harus dilaporkan gagal ke pemanggil"
        )
        self.assertIsNone(
            wb._hook_thread_id,
            "kegagalan install meninggalkan _hook_thread_id global; "
            "stop berikutnya menembak thread yang sudah mati dan state "
            "sesi berikutnya kotor",
        )
        self.assertIsNone(
            wb._hook_thread,
            "thread pump zombie tidak dijadwalkan berhenti saat start gagal",
        )
        self.assertTrue(
            pump.stopped or not threads[0].is_alive(),
            "thread pump yang tertinggal dari install gagal tidak pernah "
            "dihentikan",
        )
        self.assertEqual(
            posted, [wb.WM_QUIT],
            "cleanup harus mengirim WM_QUIT ke thread yang tertinggal",
        )

    def test_release_stops_a_failed_install_leftover(self):
        # State kotor persis seperti yang ditinggalkan start gagal pada
        # kode LAMA: flag False tapi thread + id global masih menggantung.
        # release harus tetap membereskannya — bukan skip karena flag False.
        pump = _LivePump()
        self.backend._hook_installed = False
        with mock.patch.object(wb, "_PostThreadMessageW", create=True) as post, \
             mock.patch.object(wb, "_hook_thread_id", 4242), \
             mock.patch.object(wb, "_hook_thread", pump):
            self.backend.release_strict_mode(None)

            post.assert_called_once()
            self.assertEqual(post.call_args[0][1], wb.WM_QUIT)
            self.assertIsNone(wb._hook_thread_id)
            self.assertIsNone(wb._hook_thread)

    def test_release_still_stops_a_live_hook(self):
        # Guard: perbaikan tidak boleh mematikan jalur yang sudah benar.
        self.backend._hook_installed = True
        pump = _LivePump()
        with mock.patch.object(wb, "_PostThreadMessageW", create=True) as post, \
             mock.patch.object(wb, "_hook_thread_id", 7), \
             mock.patch.object(wb, "_hook_thread", pump):
            self.backend.release_strict_mode(None)
        post.assert_called_once()
        self.assertEqual(post.call_args[0][1], wb.WM_QUIT)


# ---------------------------------------------------------------------------
# 2. Backup hangus saat restore gagal
# ---------------------------------------------------------------------------


class RestoreFailureKeepsBackupTestCase(unittest.TestCase):
    """windows_state.json hanya boleh dihapus SETELAH restore sukses."""

    def setUp(self):
        self._tmp = tempfile.mkdtemp(prefix="examvan-winrestore-")
        self._dir = Path(self._tmp)
        self._state_file = self._dir / "windows_state.json"
        for attr, value in (
            ("_STATE_DIR", self._dir),
            ("_STATE_FILE", self._state_file),
        ):
            p = mock.patch.object(wb, attr, value)
            p.start()
            self.addCleanup(p.stop)

    def _seed_backup(self):
        self._state_file.write_text(
            json.dumps({"screen_saver_active": True}), encoding="utf-8"
        )

    def test_failed_restore_keeps_the_backup_file(self):
        # SPI ditolak OS (mis. policy kiosk) -> nilai asli BELUM kembali;
        # menghapus backup di kondisi ini = screensaver siswa mati selamanya.
        self._seed_backup()
        with mock.patch.object(
            wb, "_set_screen_saver_active", return_value=False
        ) as set_mock:
            wb.restore_windows_settings()

        set_mock.assert_called_once_with(True)
        self.assertTrue(
            self._state_file.exists(),
            "backup dihapus padahal restore gagal — kehilangan data senyap, "
            "kelas bug yang sama dengan _gnome_ws_restore() lama",
        )

    def test_successful_restore_clears_the_backup(self):
        self._seed_backup()
        with mock.patch.object(
            wb, "_set_screen_saver_active", return_value=True
        ):
            wb.restore_windows_settings()
        self.assertFalse(self._state_file.exists())

    def test_prevent_sleep_keeps_backup_when_disable_fails(self):
        # Mirror dari kontrak Linux: nilai yang GAGAL diubah tidak boleh
        # dicatat sebagai "sudah dibackup aman".
        backend = wb.WindowsBackend()
        with mock.patch.object(
            wb, "_get_screen_saver_active", return_value=True
        ), mock.patch.object(
            wb, "_set_screen_saver_active", return_value=False
        ):
            backend.prevent_sleep()
        self.assertFalse(
            self._state_file.exists(),
            "gagal menonaktifkan screensaver tetap menulis backup; restore "
            "berikutnya akan menimpa setelan pengguna dengan nilai yang "
            "bukan hasil perubahan kita",
        )


# ---------------------------------------------------------------------------
# 3. Installer senyap tidak boleh menampilkan dialog apa pun
# ---------------------------------------------------------------------------


def _code_section(source: str, func_name: str) -> str:
    """Ambil badan procedure Pascal sampai 'end;' di kolom 0 berikutnya."""
    start = source.index(f"procedure {func_name}")
    rest = source[start:]
    return rest[: rest.index("\nend;") + len("\nend;")]


_IF_THEN_RE = None  # dikompilasi malas di bawah


def _unguarded_msgboxes(source: str) -> list[tuple[str, str]]:
    """Cari MsgBox tanpa guard silent di seluruh section [Code].

    Konvensi yang ditegakkan: dialog apa pun di installer harus berada
    DI BAWAH `if` yang menyebut `WizardSilent` / `UninstallSilent` di
    prosedur yang sama — MsgBox PASTI tampil saat /VERYSILENT, jadi satu
    saja yang lolos berarti CI smoke test / deploy lab menggantung.

    Return list (nama_prosedur, baris) untuk setiap pelanggaran.
    """
    global _IF_THEN_RE
    import re

    if _IF_THEN_RE is None:
        # Indentasi diperbolehkan: semua if di [Code] menjorok, dan versi
        # pertama regex ini (^if tanpa [ \t]*) tidak pernah cocok — scanner
        # menandai bahkan dialog yang SUDAH dijaga.
        _IF_THEN_RE = re.compile(r"^[ \t]*if\b.*\bthen\s*$", re.MULTILINE)

    offenders: list[tuple[str, str]] = []
    # Section [Code]: mulai dari baris [Code] sampai section berikutnya/EOF.
    code_start = source.find("\n[Code]")
    if code_start == -1:
        return offenders
    code = source[code_start:]

    procedures = re.split(r"(?m)^procedure\s+", code)[1:]
    for proc in procedures:
        name = proc.split("(", 1)[0].strip()
        offset_so_far = 0
        for raw_line in proc.splitlines():
            # Buang komentar: MsgBox yang disebut komentar bukan dialog.
            code_line = raw_line.split("//")[0]
            if "MsgBox(" not in code_line:
                offset_so_far += len(raw_line) + 1
                continue
            before = proc[:offset_so_far]
            guardeds = [
                m.group(0)
                for m in _IF_THEN_RE.finditer(before)
                if "wizardsilent" in m.group(0).lower()
                or "uninstallsilent" in m.group(0).lower()
            ]
            if not guardeds:
                offenders.append((name, raw_line.strip()))
            offset_so_far += len(raw_line) + 1
    return offenders


class SilentInstallNeverBlocksTestCase(unittest.TestCase):
    """/VERYSILENT tidak boleh pernah menampilkan MsgBox."""

    def test_mismatch_msgbox_is_guarded_against_silent_installs(self):
        # RED: kode lama memunculkan MsgBox mismatch tanpa guard —
        # instalasi senyap menggantung selamanya di situ.
        src = ISS_PATH.read_text(encoding="utf-8")
        body = _code_section(src, "CurStepChanged")
        self.assertIn(
            "WizardSilent", body,
            "MsgBox mismatch password tidak dijaga WizardSilent: "
            "instalasi /VERYSILENT (CI, deploy lab) menggantung",
        )
        # Urutan yang diwajibkan: guard dulu, baru MsgBox.
        guard_pos = body.index("WizardSilent")
        msgbox_pos = body.index("MsgBox('Dua password tidak sama")
        self.assertLess(
            guard_pos, msgbox_pos,
            "guard WizardSilent harus dievaluasi SEBELUM MsgBox mismatch",
        )

    def test_mismatch_recovery_still_recovers_stored_password(self):
        # Perbaikan guard tidak boleh mematikan pemulihan password lama:
        # mismatch TETAP harus mengembalikan isi file, hanya tanpa dialog
        # saat senyap.
        src = ISS_PATH.read_text(encoding="utf-8")
        body = _code_section(src, "CurStepChanged")
        self.assertIn("ReadPasswordFromFile(PwFile)", body)

    def test_every_msgbox_in_install_path_is_silent_safe(self):
        # Struktural: tak ada MsgBox lain di jalur install yang lolos
        # dari guard. (Jalur uninstall dijaga test Windows pipeline.)
        src = ISS_PATH.read_text(encoding="utf-8")
        body = _code_section(src, "CurStepChanged")
        for line in body.splitlines():
            if "MsgBox(" in line:
                window = body[: body.index(line)]
                self.assertIn(
                    "WizardSilent", window,
                    f"MsgBox tanpa guard WizardSilent: {line.strip()!r}",
                )


class EveryMsgBoxInInstallerIsSilentSafeTestCase(unittest.TestCase):
    """SMOKE SELURUH .iss: tidak boleh ada MsgBox tanpa guard silent.

    Tiga situs dialog yang diketahui (mismatch password, konfirmasi hapus
    data saat uninstall, peringatan VCRedist) masing-masing sudah dijaga
    test spesifik. Test ini menutup sisanya: MsgBox BARU yang ditambahkan
    tanpa guard akan menggagalkan build test, bukan menggantung CI.
    """

    def test_scanner_actually_catches_an_unguarded_dialog(self):
        # Bukti bahwa scanner ini bukan vacuous: sample yang DISENGAJA
        # bermasalah harus ditandai, sample yang dijaga tidak.
        guarded = '''
[Code]
procedure A();
begin
  if not WizardSilent then
    MsgBox('halo', mbError, MB_OK);
end;
'''
        unguarded = '''
[Code]
procedure B();
begin
  if PwValue <> '' then
    MsgBox('lupa guard', mbError, MB_OK);
end;
'''
        self.assertEqual(_unguarded_msgboxes(guarded), [])
        offenders = _unguarded_msgboxes(unguarded)
        self.assertEqual(len(offenders), 1, offenders)
        self.assertEqual(offenders[0][0], "B")

    def test_the_real_installer_has_no_unguarded_msgbox(self):
        src = ISS_PATH.read_text(encoding="utf-8")
        offenders = _unguarded_msgboxes(src)
        self.assertEqual(
            offenders, [],
            "MsgBox tanpa guard WizardSilent/UninstallSilent ditemukan — "
            "instalasi/uninstall /VERYSILENT akan menggantung di dialog "
            "yang tidak pernah bisa dijawab",
        )

    def test_the_scan_is_not_vacuous(self):
        # Kalau suatu hari semua dialog dihapus, test ini memaksa seseorang
        # memutuskan ulang konvensinya — bukan diam-diam hijau selamanya.
        src = ISS_PATH.read_text(encoding="utf-8")
        total = sum(
            1
            for line in src.splitlines()
            if "MsgBox(" in line.split("//")[0]
        )
        self.assertGreaterEqual(
            total, 3,
            "jumlah MsgBox di examvan.iss turun drastis; kalau memang "
            "sengaja dihapus, perbarui test ini bersama keputusan itu",
        )


# ---------------------------------------------------------------------------
# 4. Celah blokir keyboard
# ---------------------------------------------------------------------------


class BlocklistGapsTestCase(unittest.TestCase):
    """Kombinasi Win yang ditemukan lolos oleh review."""

    def blocked(self, vk, **mods):
        return wb.should_block_key(vk, **mods)

    def test_win_enter_narrator_blocked(self):
        self.assertTrue(
            self.blocked(0x0D, win_down=True),
            "Win+Enter meluncurkan Narrator (pembaca layar + slot keluar)",
        )

    def test_win_c_copilot_blocked(self):
        # Keputusan produk (konfirmasi pengguna, 30 Sep 2026):
        # Copilot = asisten AI yang bisa menjawab soal ujian. Dulu
        # sengaja lolos; test lama yang menegaskan pass-through telah
        # diperbarui di test_windows_backend.py.
        self.assertTrue(self.blocked(0x43, win_down=True), "Win+C (Copilot)")

    def test_win_j_blocked(self):
        self.assertTrue(self.blocked(0x4A, win_down=True), "Win+J")

    def test_win_f6_through_f12_blocked(self):
        # Win+F1 diblokir; F6..F12 dulu lolos. F-keys + Win membuka
        # permukaan yang tidak ada alasannya untuk tersedia saat ujian.
        for vk in range(0x75, 0x7C):
            self.assertTrue(self.blocked(vk, win_down=True), hex(vk))

    def test_plain_f_keys_still_reach_the_app(self):
        # F5 (refresh PDF viewer) dst. TANPA Win tetap harus sampai UI.
        for vk in (0x70, 0x75, 0x7B):  # F1, F6, F12
            self.assertFalse(self.blocked(vk), hex(vk))


if __name__ == "__main__":
    unittest.main()
