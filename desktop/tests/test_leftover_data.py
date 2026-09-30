"""Berkas jawaban dan PDF tidak boleh tertinggal di disk (#6, #7).

#6 — `_discard_pdf` tidak bisa menghentikan download yang sedang jalan
----------------------------------------------------------------------
`self._pdf_path` baru di-assign SETELAH `api.download_pdf` selesai:

    self._pdf_path = api.download_pdf(..., dest, ...)

`_discard_pdf()` dimulai dengan:

    path = getattr(self, "_pdf_path", None)
    if not path:
        return

Siswa menekan "Keluar Ujian -> Ya" (atau pengawas memakai admin exit)
saat PDF 20 MB masih diunduh di Wi-Fi lab -> `_pdf_path is None` ->
early return. Download pun selesai dan menulis berkas utuh ke
`%TEMP%\\examvan_exam_<id>.pdf`.

Yang memperburuk: proses MASIH HIDUP setelah jendela ditutup
(`__main__._on_viewer_closed` brings the config dialog back), jadi berkas
nya bertahan sampai giliran siswa berikutnya -- dengan lockdown sudah
dilepas dan tidak ada lagi yang membersihkannya.

#7 — fallback legacy tidak bisa membaca berkas yang justru ditulis
     dengan token lama
-----------------------------------------------------------
`_legacy_xor_obfuscate` menurunkan kuncinya dari `exam_token` YANG
SAAT INI. Pada dynamic-token exam token berputar, dan
`_connect_thread` menimpanya ke config sebelum apa pun memanggil
`load_answers()`. Maka kunci barunya salah, kedua decode gagal, dan:

    token lama ABCD1234 -> load: {'1': 'A', '2': 'B', '3': 'C'}
    token baru  WXYZ5678 -> load: None      <- fallback gagal

Persis skenario yang dijanjikan docstring `_xor_obfuscate` untuk
diselamatkan: dynamic-token exam, mesin siswa mati atau jaringan putus
di tengah, siswa kembali dengan token baru, jawaban yang tersisa hilang
tanpa pesan -- lalu autosave berikutnya menimpanya.

Penyebabnya struktural: kunci tidak pernah disimpan bersama berkasnya,
jadi mustahil menurunkan kunci lama dari state sekarang. Yang bisa
dilakukan adalah mencoba SEMUA kunci yang pernah dipakai pada berkas itu
-- artinya mencoba token yang cocok dengan exam yang sedang dikerjakan.
"""

from __future__ import annotations

import base64
import json
import os
import pathlib
import tempfile
import unittest
from unittest import mock

from examvan import config
from examvan.models import Exam
from examvan.ui import exam_viewer as ev


# ---------------------------------------------------------------------------
# #6 — PDF yang diunduh setelah siswa keluar tidak boleh tinggal
# ---------------------------------------------------------------------------


class PdfAbandonedMidDownloadTestCase(unittest.TestCase):
    """#6 — keluar saat download berjalan tetap harus membersihkan berkas."""

    def _viewer(self):
        win = ev.ExamViewerWindow.__new__(ev.ExamViewerWindow)
        win._exam = Exam(id=7, name="Ujian", status="active")
        win._token = "ABCD1234"
        win._identity_data = {}
        win._security = None
        win._stop_presence = mock.Mock()
        win._pdf_viewer = mock.Mock()
        win.isVisible = lambda: True
        win._device_label = "DESKTOP:abc"
        win._server_url = "https://exam.example"
        win._sig_pdf_ready = mock.Mock()
        win._sig_pdf_error = mock.Mock()
        win._sig_status = mock.Mock()
        return win

    def test_the_target_path_is_known_before_the_download_starts(self):
        # `_pdf_path` harus dihitung SEBELUM memanggil download, kalau
        # tidak, `_discard_pdf()` tidak punya apa-apa untuk dihapus.
        win = self._viewer()
        win._pdf_path = None
        win._abandoned = False
        recorded = {}

        def fake_download(*args, **kwargs):
            # Selama download berjalan, `_discard_pdf` harus sudah tahu
            # ke mana berkas akan ditulis.
            recorded["path_during_download"] = win._pdf_path
            return win._pdf_path

        with mock.patch.object(ev.api, "download_pdf", side_effect=fake_download):
            win._load_pdf_thread()
        self.assertTrue(
            recorded.get("path_during_download"),
            "berkas tujuan baru diketahui SETELAH download selesai, jadi "
            "keluar di tengah download tidak bisa membersihkannya",
        )

    def test_a_finished_download_after_exit_is_deleted_immediately(self):
        # Race yang sebenarnya: exit terjadi, baru download selesai.
        win = self._viewer()
        win._pdf_path = None
        win._abandoned = True   # set oleh _discard_pdf / closeEvent

        tmp = tempfile.mkdtemp()
        self.addCleanup(lambda: [os.remove(p) for p in
                                 (pathlib.Path(tmp) / "x.pdf",
                                  pathlib.Path(tmp) / "x.pdf.tmp")
                                 if os.path.exists(p)])
        win._pdf_path = str(pathlib.Path(tmp) / "x.pdf")

        def fake_download(*args, **kwargs):
            # Download selesai SETELAH siswa keluar.
            dest = win._pdf_path
            pathlib.Path(dest).write_bytes(b"%PDF-1.4 leaked paper")
            return dest

        with mock.patch.object(ev.api, "download_pdf", side_effect=fake_download):
            win._load_pdf_thread()

        self.assertFalse(
            os.path.exists(win._pdf_path),
            "download yang selesai setelah siswa keluar meninggalkan naskah "
            "ujian utuh di TEMP; proses masih hidup dan siswa berikutnya "
            "membacanya",
        )

    def test_a_normal_download_is_kept(self):
        # Jangan buang air out with the bathwater: download yang
        # sukses saat ujian berjalan HARUS tetap ada.
        win = self._viewer()
        win._pdf_path = None
        win._abandoned = False
        tmp = tempfile.mkdtemp()
        dest = str(pathlib.Path(tmp) / "ok.pdf")
        win._pdf_path = dest

        with mock.patch.object(ev.api, "download_pdf", return_value=dest):
            win._load_pdf_thread()
        self.assertEqual(win._pdf_path, dest)

    def test_exit_during_download_marks_the_window_abandoned(self):
        win = self._viewer()
        win._pdf_path = None
        win._abandoned = False
        win._discard_pdf()
        self.assertTrue(
            getattr(win, "_abandoned", False),
            "keluar tidak menandai download sebagai ditinggalkan, jadi "
            "berkas yang telat selesai tidak akan dihapus",
        )


# ---------------------------------------------------------------------------
# #7 — berkas jawaban lama harus terbaca meski token sudah berputar
# ---------------------------------------------------------------------------


class _ConfigSandbox(unittest.TestCase):
    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        self.dir = pathlib.Path(self._tmp.name)
        for attr, value in (
            ("_CONFIG_DIR", self.dir),
            ("_CONFIG_FILE", self.dir / "config.json"),
        ):
            patcher = mock.patch.object(config, attr, value)
            patcher.start()
            self.addCleanup(patcher.stop)
        config._cache = None
        self.addCleanup(setattr, config, "_cache", None)


class LegacyAnswersSurviveARotatingTokenTestCase(_ConfigSandbox):
    """#7 — kunci legacy diturunkan dari token yang BARU, bukan yang sekarang."""

    def _write_legacy_answers(self, answers, token):
        raw = json.dumps(answers, separators=(",", ":")).encode("utf-8")
        mixed = bytes(
            b ^ ord(token[i % len(token)])
            for i, b in enumerate(config._OBFUSCATE_KEY)
        )
        payload = bytes(b ^ mixed[i % len(mixed)] for i, b in enumerate(raw))
        (self.dir / "answers_7.dat").write_text(
            base64.urlsafe_b64encode(payload).decode("ascii"), encoding="ascii"
        )

    def test_answers_written_with_an_old_token_load_after_rotation(self):
        # Token dipakai saat jawaban ditulis, lalu BERPUTAR sebelum siswa
        # kembali -- persis alur dynamic-token exam.
        config.set("exam_token", "ABCD1234")
        self._write_legacy_answers({"1": "A", "2": "B", "3": "C"}, "ABCD1234")

        config.set("exam_token", "WXYZ5678")
        self.assertEqual(
            config.load_answers(7), {"1": "A", "2": "B", "3": "C"},
            "jawaban yang ditulis dengan token lama tidak terbaca setelah "
            "token berputar -- precisely the population the legacy fallback "
            "was written for. Tidak ada prompt 'Kirim Lagi', tidak ada yang "
            "dipulihkan, dan autosave berikutnya menimpanya.",
        )

    def test_the_matching_exam_token_still_decodes(self):
        # Token yang diketik siswa untuk ujian yang SAMA harus berhasil
        # walau berbeda dari token saat jawaban ditulis.
        self._write_legacy_answers({"1": "A"}, "OLDTOKEN")
        config.set("exam_token", "OLDTOKEN")
        self.assertEqual(config.load_answers(7), {"1": "A"})

    def test_new_format_files_are_unaffected(self):
        config.set("exam_token", "ABCD1234")
        config.save_answers(7, {"1": "A"})
        config.set("exam_token", "WXYZ5678")
        self.assertEqual(config.load_answers(7), {"1": "A"})

    def test_garbage_does_not_decode_to_a_wrong_answer_set(self):
        # Berkas acak harus ditolak, bukan "berhasil" dengan isi ngawur
        # yang akan terkirim ke server dan dinilai salah semua.
        (self.dir / "answers_7.dat").write_text(
            base64.urlsafe_b64encode(b"not really answers at all").decode("ascii"),
            encoding="ascii",
        )
        config.set("exam_token", "ABCD1234")
        loaded = config.load_answers(7)
        self.assertIsNone(
            loaded,
            f"berkas sampah terbaca sebagai jawaban {loaded!r}",
        )

    def test_an_empty_answer_file_is_still_recognised_as_empty(self):
        # Autosave menulis placeholder kosong; itu harus tetap terbaca
        # supayaexam bisa membedakan "belum menjawab" dari "hilang".
        config.set("exam_token", "ABCD1234")
        config.save_answers(7, {"1": ""})
        self.assertEqual(config.load_answers(7), {"1": ""})


if __name__ == "__main__":
    unittest.main()