"""Ronde 8 (H2) — "Kirim Lagi" hanya boleh ditawarkan kalau kunci soalnya
BENAR-BENAR milik satu siswa.

Bug yang ditutup file ini
-------------------------
`utils.build_student_key_source` mengembalikan `(token, "token")` bila
tidak ada kolom identitas yang memetakan ke nomor/nama/kelas. Itu bukan
hipotesis: kunci field di webui dibangun dengan
`label.toLowerCase().replace(/[^a-z0-9]/g, '_')`, jadi SETIAP label
yang memuat karakter non-Latin menjadi `field_1`, `field_2`, ... dan
`map_identity_to_standard` tidak mengenali satu pun sebagai identitas.

`_offer_pending_recovery` hanya membandingkan `owner_key !=
current_key`. Kalau keduanya jatuh ke token, perbandingan itu SELALU
sama untuk siapa pun yang memakai PC itu: recovery tetap ditawarkan,
dan menjawab "Ya" mengirim `answers_<id>.dat` milik siswa A dengan
identitas siswa B. Diaudit dengan kode produksi:

    [A] answers on disk : {'1':'jawaban-A'}
    [A] owner sidecar   : {'student_key':'ABCD1234'}      (label: token)
    [B] identitas       : {'field_1':'SISWA DUA'}
    -> student_key = 'ABCD1234'  (source=token)
    -> recovery ditawarkan, dan "Ya" mengirim jawaban A atas nama B

Yang DIPERTAHANKAN: siswa yang jawabannya memang belum terkirim tetap
bisa mengirim ulang. Penjaga ini menolak Tawaran, bukan ujian — siswa
tetap boleh masuk, dan berkasnya sendiri tidak pernah dihapus.
"""

from __future__ import annotations

import os
import shutil
import tempfile
import unittest
from pathlib import Path
from unittest import mock

os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")

from PyQt5.QtWidgets import QApplication, QMessageBox

from examvan import config
from examvan.models import Exam, IdentityField
from examvan.ui.server_config import ServerConfigDialog
from examvan.utils import build_student_key, student_label

APP = QApplication.instance() or QApplication([])

EXAM_ID = 7
TOKEN = "ABCD1234"

# Config ujian yang kuncinya TIDAK memetakan ke slot mana pun — persis
# bentuk yang webui hasilkan untuk label non-Latin.
UNMAPPABLE_FIELDS = [
    IdentityField(key="field_1", label="Nama Lengkap", required=True),
    IdentityField(key="field_2", label="Kelas", required=True),
]
MAPPABLE_FIELDS = [
    IdentityField(key="nama", label="Nama", required=True),
    IdentityField(key="nomor_ujian", label="Nomor Ujian", required=True),
    IdentityField(key="kelas", label="Kelas", required=True),
]

SISWA_A = {"field_1": "SISWA SATU", "field_2": "9A"}
SISWA_B = {"field_1": "SISWA DUA", "field_2": "9B"}
BUDI = {"nama": "Budi", "nomor_ujian": "N01", "kelas": "9A"}
SITI = {"nama": "Siti", "nomor_ujian": "N02", "kelas": "9A"}

ANSWERS = {"1": "jawaban-A", "2": "pilihan-B"}


class _Sandbox(unittest.TestCase):
    def setUp(self) -> None:
        self._tmp = tempfile.mkdtemp(prefix="examvan-r8-h2-")
        self._dir_patch = mock.patch.object(
            config, "_CONFIG_DIR", Path(self._tmp))
        self._file_patch = mock.patch.object(
            config, "_CONFIG_FILE", Path(self._tmp) / "config.json")
        self._dir_patch.start()
        self._file_patch.start()
        self.addCleanup(self._dir_patch.stop)
        self.addCleanup(self._file_patch.stop)
        self.addCleanup(shutil.rmtree, self._tmp, True)
        config._cache = None
        self.addCleanup(setattr, config, "_cache", None)

    def _dlg(self, fields=MAPPABLE_FIELDS) -> ServerConfigDialog:
        dlg = ServerConfigDialog()
        self.addCleanup(dlg.deleteLater)
        dlg._exam = Exam(
            id=EXAM_ID, name="Ujian", status="active",
            security_level="low", identity_fields=fields,
        )
        dlg._validated_token = TOKEN
        # `_show_recovery` (slot produksi) membuka QMessageBox modal dan
        # menjalankan worker lewat jaringan. Yang diuji di sini GERBANG
        #-nya, jadi slot itu dilepas persis seperti
        # `test_r5_answers_owner._real_config_dialog` melakukannya —
        # tanpa itu, tawaran yang lolos gerbang akan menggantung test
        # sampai runner dibunuh.
        try:
            dlg._sig_recovery_available.disconnect(dlg._show_recovery)
        except (TypeError, RuntimeError):
            pass
        return dlg

    def _save_answers(self, identity, answers=None) -> None:
        """Tulis jawaban + sidecar pemilik dengan helper produksi."""
        config.set("exam_token", TOKEN)
        config.save_answers(EXAM_ID, dict(answers or ANSWERS))
        config.save_answers_owner(
            EXAM_ID,
            build_student_key(identity, TOKEN),
            student_label(identity, TOKEN),
        )

    def _offered(self, dlg, identity):
        offered = []
        dlg._sig_recovery_available.connect(lambda exam: offered.append(exam))
        allowed = dlg._offer_pending_recovery(identity)
        return allowed, offered


class TokenSourceRecoveryTestCase(_Sandbox):
    """Kunci yang jatuh ke token bukan milik satu siswa."""

    def test_recovery_is_not_offered_to_the_next_student(self):
        self._save_answers(SISWA_A)
        dlg = self._dlg(UNMAPPABLE_FIELDS)
        allowed, offered = self._offered(dlg, SISWA_B)
        self.assertEqual(
            offered, [],
            "recovery jawaban siswa A ditawarkan ke siswa B karena kunci "
            "keduanya sama-sama token — guard owner_key != current_key "
            "tidak pernah berlaku di config ini",
        )
        self.assertTrue(
            allowed,
            "menolak tawaran recovery BUKAN berarti menolak ujian — siswa "
            "tetap harus boleh masuk",
        )
        self.assertEqual(
            config.load_answers(EXAM_ID), ANSWERS,
            "jawaban di disk tidak boleh hilang karena tawaran ditolak",
        )

    def test_nothing_is_submitted_when_the_offer_is_refused(self):
        self._save_answers(SISWA_A)
        dlg = self._dlg(UNMAPPABLE_FIELDS)
        with mock.patch("examvan.ui.server_config.api") as api:
            self._offered(dlg, SISWA_B)
            api.submit_with_retry.assert_not_called()
        self.assertEqual(config.load_answers(EXAM_ID), ANSWERS)

    def test_the_student_gets_an_honest_explanation(self):
        self._save_answers(SISWA_A)
        dlg = self._dlg(UNMAPPABLE_FIELDS)
        self._offered(dlg, SISWA_B)
        status = dlg.lbl_status.text()
        self.assertTrue(
            status.strip(),
            "tawaran ditolak tanpa penjelasan sama sekali — siswa menekan "
            "tombol dan tidak terjadi apa-apa",
        )
        self.assertIn(
            "pengawas", status.lower(),
            f"penjelasan harus memberi jalan keluar yang bisa dipakai "
            f"siswa: {status!r}",
        )
        # Dan penjelasan itu TIDAK boleh membocorkan siapa pemiliknya.
        self.assertNotIn("SISWA SATU", status)

    def test_the_refusal_is_logged_with_the_source(self):
        self._save_answers(SISWA_A)
        dlg = self._dlg(UNMAPPABLE_FIELDS)
        with self.assertLogs("examvan.ui.server_config", level="WARNING") as cm:
            self._offered(dlg, SISWA_B)
        joined = " ".join(cm.output).lower()
        self.assertIn("token", joined)

    def test_a_shared_owner_key_is_refused_even_with_a_per_student_key(self):
        # Sidecar yang labelnya menandai slot bersama: kunci owners-nya
        # tidak kidding per-siswa, jadi tidak boleh dipakai sebagai bukti
        # kepemilikan walau kebetulan sama dengan kunci per-siswa.
        config.save_answers(EXAM_ID, dict(ANSWERS))
        config.save_answers_owner(
            EXAM_ID, build_student_key(BUDI, TOKEN),
            "token (identitas kosong)",
        )
        dlg = self._dlg(MAPPABLE_FIELDS)
        allowed, offered = self._offered(dlg, BUDI)
        self.assertEqual(offered, [])
        self.assertTrue(allowed)


class PerStudentKeyRecoveryStillWorksTestCase(_Sandbox):
    """Penjaga baru TIDAK boleh menutup recovery yang sah."""

    def test_the_right_owner_is_still_offered(self):
        self._save_answers(BUDI)
        dlg = self._dlg(MAPPABLE_FIELDS)
        with mock.patch.object(
            QMessageBox, "question", return_value=QMessageBox.No
        ):
            allowed, offered = self._offered(dlg, BUDI)
        self.assertEqual(
            [exam.id for exam in offered], [EXAM_ID],
            "siswa yang jawabannya sendiri belum terkirim tidak boleh "
            "kehilangan jalan mengirim ulang — itu fitur inti recovery",
        )
        self.assertFalse(allowed, "UI recovery dibangun untuk pemiliknya")

    def test_a_classmate_still_gets_nothing(self):
        self._save_answers(BUDI)
        dlg = self._dlg(MAPPABLE_FIELDS)
        with mock.patch.object(
            QMessageBox, "question", return_value=QMessageBox.No
        ):
            allowed, offered = self._offered(dlg, SITI)
        self.assertEqual(offered, [])
        self.assertTrue(allowed)

    def test_no_answers_on_disk_means_no_offer_and_no_complaint(self):
        dlg = self._dlg(MAPPABLE_FIELDS)
        allowed, offered = self._offered(dlg, BUDI)
        self.assertEqual(offered, [])
        self.assertTrue(allowed)


class NameOnlyKeyTestCase(_Sandbox):
    """Kunci berbasis nama: bukan unik, jadi bukan bukti kepemilikan."""

    FIELDS = [
        IdentityField(key="nama", label="Nama", required=True),
        IdentityField(key="kelas", label="Kelas", required=True),
    ]

    def test_a_name_key_is_not_a_usable_owner_proof(self):
        # `_offer_resubmit_choice` sudah memakai aturan yang sama untuk
        # modal "sudah terkumpul": hanya `exam_number` yang unik per
        # siswa. Aturan yang berbeda di dua tempat menghasilkan dua
        # perilaku berbeda untuk kasus yang sama.
        a = {"nama": "Ahmad", "kelas": "7A"}
        b = dict(a, kelas="8B")
        self._save_answers(a)
        dlg = self._dlg(self.FIELDS)
        allowed, offered = self._offered(dlg, b)
        self.assertEqual(
            offered, [],
            "kunci 'ahmad' dipakai dua siswa berbeda di kelas berbeda",
        )
        self.assertTrue(allowed)


if __name__ == "__main__":
    unittest.main()
