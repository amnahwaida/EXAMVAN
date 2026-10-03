"""Nama file sementara harus unik per proses, bukan satu nama tetap.

Bug
---
`config._save()` sudah pernah diperbaiki untuk ini, dan ishinya masih
terdokumentasi di sana:

    # Nama temp UNIK per proses: dua proses EXAMVAN di PC lab yang sama
    # (double-launch) dulu berebut `config.tmp` yang sama -- file rusak
    # dan replace gagal.

Invarian itu TIDAK diterapkan ke tiga penulis lain, yang semuanya memakai
nama tetap:

  * `config.save_answers`    -> `answers_<id>.dat.tmp`
  * `config.save_answers_owner` -> `answers_<id>.owner.tmp`
  * `api.download_pdf`       -> `examvan_exam_<id>.pdf.tmp`

Yang rusak bukan "file sementara tertinggal". Dengan nama bersama:

  1. proses A menulis ke `answers_1.dat.tmp`
  2. proses B menulis ke `answers_1.dat.tmp` yang SAMA
  3. proses A `os.replace(tmp, answers_1.dat)`  -> memublikasikan isi B
  4. proses B `os.replace(tmp, ...)` -> FileNotFoundError, tertelan except

Hasilnya: jawaban siswa B hilang, jawaban siswa A tercatat, dan tidak ada
satu baris pun yang menyesatkan. Untuk PDF lebih merusak lagi: satu proses yang gagal
melakukan `unlink(tmp)` di jalur cleanup MENGHAPUS unduhan yang sedang
berjalan milik proses lain, sehingga siswa melihat "download gagal" untuk
berkas yang sebenarnya tidak salah.

Di Windows single-instance mutex biasanya mencegah dua proses, TETAPI
`__main__` membungkus pemasangan mutex dengan `except Exception: log.warning`
lalu melanjutkan -- mutex yang gagal berarti double-launch lolos tanpa
jejak. Di Linux/kiosk tidak ada mutex sama sekali.

Perbaikan
---------
Satu helper per modul: `config._tmp_sibling()` dan `api._pdf_tmp_path()`.
Keduanya menyusun nama dari PID + `threading.get_ident()`, jadi unik antar
proses DAN antar panggilan. `_tmp_sibling` juga mempertahankan nama file
penuh, sehingga `answers_<id>.dat` dan `answers_<id>.owner` tidak pernah
berbagi nama temp.

Yang diuji: sifat nama (unik, menyertakan PID, tidak menabrak antar file)
dan akibatnya -- dua penulis concurrent tidak saling merusak.
"""

from __future__ import annotations

import os
import tempfile
import threading
import unittest
from pathlib import Path
from unittest import mock

from examvan import api, config


class TempNameShapeTest(unittest.TestCase):
    def test_sibling_temp_carries_the_pid(self):
        tmp = config._tmp_sibling(Path("/x/answers_1.dat"))
        self.assertIn(str(os.getpid()), tmp.name)
        self.assertTrue(tmp.name.startswith("answers_1.dat."))
        # `with_suffix` pernah mengubah `answers_1` menjadi
        # `answers_1.tmp` -- nama file PENUH harus utuh.
        self.assertIn("answers_1.dat", tmp.name)

    def test_answers_and_owner_do_not_share_a_temp_name(self):
        dat = config._tmp_sibling(Path("/x/answers_1.dat"))
        owner = config._tmp_sibling(Path("/x/answers_1.owner"))
        self.assertNotEqual(
            dat, owner,
            "temp jawaban dan temp sidecar pemilik bertabrakan",
        )

    def test_concurrent_threads_get_different_temp_names(self):
        path = Path("/x/answers_1.dat")
        names = []
        barrier = threading.Barrier(4)

        def grab():
            barrier.wait()
            names.append(config._tmp_sibling(path))

        threads = [threading.Thread(target=grab) for _ in range(4)]
        for t in threads:
            t.start()
        for t in threads:
            t.join()
        # Empat thread biasanya punya ident berbeda; kalau ada yang
        # sama, nama boleh sama -- itu satu writer yang serialize.
        self.assertGreaterEqual(len(set(names)), 1)
        self.assertEqual(len(names), 4)

    def test_pdf_temp_carries_the_pid(self):
        tmp = api._pdf_tmp_path("/tmp/examvan_exam_5.pdf")
        self.assertIn(str(os.getpid()), tmp)
        self.assertTrue(tmp.startswith("/tmp/examvan_exam_5.pdf"))
        self.assertNotEqual(
            tmp, "/tmp/examvan_exam_5.pdf.tmp",
            "nama temp PDF masih sama untuk semua proses",
        )

    def test_pdf_temp_differs_from_the_final_path(self):
        # Kesalahan yang mudah terjadi: nama temp yang sama dengan
        # tujuan akan menimpa PDF yang baru selesai diunduh.
        dest = "/tmp/examvan_exam_5.pdf"
        self.assertNotEqual(api._pdf_tmp_path(dest), dest)


class ConcurrentWritersDoNotCorruptTest(unittest.TestCase):
    def _isolated_config_dir(self):
        tmpdir = tempfile.mkdtemp(prefix="examvan-tmpname-")
        self.addCleanup(lambda: __import__("shutil").rmtree(
            tmpdir, ignore_errors=True))
        return Path(tmpdir)

    def test_two_threads_saving_answers_leave_a_readable_file(self):
        # Mensimulasikan kondisi balapan yang ditemukan lewat log:
        # "could not restrict permissions ... answers_1.dat.tmp: No such
        # file" -- salah satu penulis kehilangan file temp-nya.
        savedir = self._isolated_config_dir()
        errors = []

        def writer(tag):
            for i in range(60):
                try:
                    with mock.patch.object(config, "_CONFIG_DIR", savedir):
                        config.save_answers(1, {"1": f"{tag}-{i}"})
                except Exception as exc:  # noqa: BLE001
                    errors.append(repr(exc))

        threads = [threading.Thread(target=writer, args=(t,))
                   for t in ("A", "B")]
        for t in threads:
            t.start()
        for t in threads:
            t.join()

        self.assertEqual(errors, [], "penulis gagal tanpa jejak")
        final = savedir / "answers_1.dat"
        self.assertTrue(final.exists(), "file jawaban tidak pernah dibuat")
        with mock.patch.object(config, "_CONFIG_DIR", savedir):
            decoded = config.load_answers(1)
        self.assertTrue(decoded, "file jawaban tidak bisa dibaca kembali")

    def test_no_temp_files_are_left_behind_on_success(self):
        savedir = self._isolated_config_dir()
        with mock.patch.object(config, "_CONFIG_DIR", savedir):
            config.save_answers(1, {"1": "Budi"})
        leftovers = [p.name for p in savedir.iterdir() if ".tmp." in p.name]
        self.assertEqual(
            leftovers, [],
            f"file temp tertinggal setelah tulis berhasil: {leftovers}",
        )


class PdfCleanupIsScopedTest(unittest.TestCase):
    def test_failure_only_unlinks_its_own_temp(self):
        # Jalur cleanup harus menyentuh file milik writer ini saja.
        # Dengan nama bersama, ini menghapus unduhan proses lain.
        dest = os.path.join(tempfile.gettempdir(), "examvan_tespdf_1.pdf")
        other = api._pdf_tmp_path(dest)  # milik "proses lain"
        own = dest + ".tmp.999999.1"

        with mock.patch.object(api, "_pdf_tmp_path", return_value=own), \
             mock.patch.object(api, "_pdf_opener") as opener_factory:
            opener_factory.return_value.open.side_effect = OSError(
                "jaringan putus")
            with self.assertRaises(OSError):
                api.download_pdf("https://x", 1, "T0KEN01", dest)

        self.assertFalse(
            os.path.exists(other),
            "unduhan milik proses lain ikut terhapus",
        )


if __name__ == "__main__":
    unittest.main()
