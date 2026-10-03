"""MEDIUM — sapu PDF basi tidak pernah menyentuh file temp `.tmp.<pid>.<tid>`.

Bug
---
`_sweep_stale_exam_pdfs` (`__main__.py`) hanya cocok dengan
`glob("examvan_exam_*.pdf")`, dan `*` di `glob` TIDAK melintasi titik:
`examvan_exam_7.pdf.tmp.4242.140234` tidak berakhiran `.pdf` lagi. Commit
`181e8eb` memberi nama temp unik per proses (`api._pdf_tmp_path`) tapi
pola sapu tidak pernah ikut diperbarui. `_discard_pdf` di `exam_viewer`
hanya menghapus `dest_path`, tidak pernah saudara `.tmp.*`-nya.

Dampak yang diukur:

    glob('examvan_exam_*.pdf') cocok: ['examvan_exam_7.pdf']
    tertinggal: ['examvan_exam_7.pdf',
                 'examvan_exam_7.pdf.tmp.4242.140234',
                 'examvan_exam_9.pdf.tmp.99.7']

SIGKILL atau listrik mati di tengah unduhan menyisakan salinan naskah
ujian yang utuh/near-complete di `%TEMP%` — selamanya, lintas reboot,
setelah lockdown dilepas.

Dua consequences yang harususyati berlawanan, jadi keduanya diuji:

  1. berkas dari proses yang SUDAH MATI harus terhapus (kebocoran naskah);
  2. berkas milik proses yang MASIH HIDUP tidak boleh dihapus instance lain
     — untuk inilah cek PID yang jadi mekanisme TUNGGAL (MEDIUM 2).
"""

from __future__ import annotations

import os
import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

from examvan import __main__ as main_mod
from examvan import api


class _SweepSandbox(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = Path(tempfile.mkdtemp(prefix="examvan-r8-sweep-"))
        self.addCleanup(shutil.rmtree, self.tmp, True)
        patcher = mock.patch("tempfile.gettempdir", return_value=str(self.tmp))
        patcher.start()
        self.addCleanup(patcher.stop)

    def _dest(self, exam_id: int = 7) -> Path:
        return self.tmp / f"examvan_exam_{exam_id}.pdf"

    # `.tmp.` ditulis LITERAL di sini, bukan diambil dari konstanta api:
    # laporan bug menyebut nama yang leftover apa adanya
    # (`examvan_exam_7.pdf.tmp.4242.140234`) dan fixture test tidak boleh
    # ikut berubah bentuk saat kodenya berubah. Bahwa format ini masih
    # sama dengan yang dihasilkan `api._pdf_tmp_path` diuji terpisah di
    # `OwnerPidParsingTestCase`.
    @staticmethod
    def _temp_of(dest: Path, pid: int, tid: int = 140234) -> Path:
        return dest.with_name(f"{dest.name}.tmp.{pid}.{tid}")

    @staticmethod
    def _dead_pid() -> int:
        """PID yang pasti tidak dipakai proses apa pun di runner ini."""
        return 4_000_000


class TempSiblingIsSweptTestCase(_SweepSandbox):
    def test_a_temp_file_from_a_previous_run_is_removed(self):
        # PID di nama berkas harus benar-benar mati supaya test ini
        # menguji "sapu sisa proses", bukan "sapu unduhan yang sedang
        # berjalan" (lihat kelas di bawah). 4.000.000 bukan PID proses
        # di runner ini, dan `api._pdf_temp_owner_pid` membacanya utuh.
        tmp = self._temp_of(self._dest(), self._dead_pid())
        tmp.write_bytes(b"%PDF-1.4\n" + b"0" * 4096)

        main_mod._sweep_stale_exam_pdfs()

        self.assertFalse(
            os.path.exists(tmp),
            "file temp unduhan yang tertinggal dari proses sebelumnya masih "
            "ada di %TEMP% setelah startup — naskah ujian utuh bertahan "
            "lintas reboot dan menunggu pupil berikutnya",
        )

    def test_the_sweep_pattern_reaches_both_shapes(self):
        # Bukti langsung pada polanya, supaya regresi pola (bukan
        # platform) tidak lolos hanya karena kebetulan Fortunately.
        dead = self._temp_of(self._dest(11), self._dead_pid())
        dead.write_bytes(b"%PDF")
        done = self._dest(12)
        done.write_bytes(b"%PDF")

        main_mod._sweep_stale_exam_pdfs()

        self.assertFalse(os.path.exists(dead))
        self.assertFalse(
            os.path.exists(done),
            "kasus `x.pdf` yang SUDAH benar sebelumnya ikut hilang — sapu "
            "baru harus tetap membersihkan naskah selesai",
        )


class LiveProcessTempIsKeptTestCase(_SweepSandbox):
    """Sapu tidak boleh menabrak unduhan instance lain yang masih hidup."""

    def test_a_temp_file_belonging_to_a_live_process_survives(self):
        # Proses nyata yang benar-benar hidup (bukan PID karangan), supaya
        # pengecekan "masih hidup" diuji dengan bukti, bukan asumsi.
        proc = subprocess.Popen(
            [sys.executable, "-c", "import time; time.sleep(30)"])
        self.addCleanup(proc.wait)
        self.addCleanup(proc.kill)
        live_tmp = self._temp_of(self._dest(), proc.pid)
        live_tmp.write_bytes(b"%PDF-1.4\n" + b"0" * 2048)
        # Satu file mati (PID yang pasti tidak ada) sebagai kontrol: kalau
        # kontrol ini ikut tertinggal, test hijau karena alasan lain.
        dead_tmp = self._temp_of(self._dest(), self._dead_pid())
        dead_tmp.write_bytes(b"%PDF-1.4\n")

        main_mod._sweep_stale_exam_pdfs()

        self.assertTrue(
            os.path.exists(live_tmp),
            "instance EXAMVAN lain sedang mengunduh file itu; menghapusnya "
            "membuat `os.replace` di proses itu gagal dan siswa melihat "
            "'download failed' untuk berkas yang tidak pernah rusak",
        )
        self.assertFalse(
            os.path.exists(dead_tmp),
            "kontrol: file dari proses yang sudah mati harus tetap dihapus",
        )

    def test_the_finished_pdf_of_a_live_process_is_still_swept(self):
        # `x.pdf` tidak membawa PID, jadi tidak ada yang bisa membuktikan
        # pemiliknya sedang mengunduh. Perilaku lama (hapus) yang benar di
        # sini: yang berbahaya adalah berkas TEMP, karena namanya membawa
        # PID pemilik dan bisa ditunjuk ke proses lain yang sedang menulis.
        proc = subprocess.Popen(
            [sys.executable, "-c", "import time; time.sleep(30)"])
        self.addCleanup(proc.wait)
        self.addCleanup(proc.kill)
        done = self._dest()
        done.write_bytes(b"%PDF-1.4\n")

        main_mod._sweep_stale_exam_pdfs()

        self.assertFalse(os.path.exists(done))

    def test_a_malformed_temp_name_is_removed_not_crashed_on(self):
        # `examvan_exam_7.pdf.tmp.abc.def` tidak punya PID yang bisa
        # dipercayai. Membuangnya BERSIH lebih baik daripada menahannya:
        # tidak ada proses yang bisa memiliki nama itu (nama selalu
        #digits dari `os.getpid()`).
        odd = self._dest(7).with_name("examvan_exam_7.pdf.tmp.abc.def")
        odd.write_bytes(b"%PDF")
        empty = self._dest(8).with_name("examvan_exam_8.pdf.tmp.")
        empty.write_bytes(b"%PDF")

        main_mod._sweep_stale_exam_pdfs()

        self.assertFalse(os.path.exists(odd))
        self.assertFalse(os.path.exists(empty))


class PidLivenessTestCase(unittest.TestCase):
    """`_process_is_alive` — mekanisme yang dipakai sapu."""

    def test_this_process_is_alive(self):
        self.assertTrue(main_mod._process_is_alive(os.getpid()))

    def test_pid_1_exists(self):
        self.assertTrue(main_mod._process_is_alive(1))

    def test_an_impossible_pid_is_dead(self):
        # Di Linux/Windows PID mungkin di-wrap, tapi 4_000_000 tidak akan
        # pernah jadi PID proses di runner ini.
        self.assertFalse(main_mod._process_is_alive(4_000_000))

    def test_zero_and_negative_are_not_pids(self):
        self.assertFalse(main_mod._process_is_alive(0))
        self.assertFalse(main_mod._process_is_alive(-5))


class OwnerPidParsingTestCase(unittest.TestCase):
    """Nama temp harus dibaca dari SATU sumber kebenaran (`api._pdf_tmp_path`)."""

    def test_the_producer_and_the_sweep_agree_on_the_format(self):
        name = os.path.basename(api._pdf_tmp_path("/tmp/examvan_exam_7.pdf"))
        pid, tid = api._pdf_temp_owner_pid(name)
        self.assertEqual((pid, tid), (os.getpid(), __import__("threading").get_ident()))

    def test_a_plain_pdf_name_has_no_owner(self):
        self.assertEqual(api._pdf_temp_owner_pid("examvan_exam_7.pdf"), (None, None))

    def test_a_temp_name_with_junk_digits_is_rejected(self):
        self.assertEqual(api._pdf_temp_owner_pid("examvan_exam_7.pdf.tmp.x.y"),
                         (None, None))


if __name__ == "__main__":
    unittest.main()