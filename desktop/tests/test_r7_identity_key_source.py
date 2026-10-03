"""Ronde 7 (H3) — modal "sudah terkumpul" tidak boleh bergantung kunci yang bisa degrade.

Bug
---
`ServerConfigDialog._offer_resubmit_choice` (commit 05d41c8) adalah
pembaca PERTAMA di sisi client dari `build_student_key`, dan
ambang-enter-nya adalah nilai yang mengembalikan `True`. Padahal kunci itu bisa DEGRADE:

    map_identity_to_standard({'nama','kelas','agama','jenis_kelamin'})
        -> student_name ada, exam_number TIDAK ada
    build_student_key(...) -> 'ahmad'

Config itu sangat biasa untuk madrasah: tidak ada kolom nomor ujian.
Maka dua siswa bernama sama — siswa 7 dan siswa 19 — memakai satu kunci,
dan `_offer_resubmit_choice` menampilkan modal "Jawaban Sudah Terkumpul"
kepada orang yang SALAH. `_cleanup_after_submit` juga menulis marker di
bawah kunci yang sama, jadi penandaan dan penanya ikut salah sasaran.

Perbaikan
---------
1. Sumber kunci dibuka ke luar (`utils.build_student_key_source`), supaya
   pemanggil bisa membedakan "kunci dari kolom identitas yang benar" dari
   "kunci yang turun ke fallback".
2. Modal hanya tampil kalau kuncinya `exam_number` — satu-satunya slot
   yang unik per siswa di kelas. Fallback ke `student_name` /
   `student_class` / `token` di-`log` dan dilewati: lebih baik tidak
   memberi tahu daripada memberi tahu orang yang salah. Kalau ujiannya
   memang tidak punya kolom nomor maupun nama, level log diturunkan ke
   INFO (tidak ada yang hilang — konfigurasinya memang begitu).
3. Keputusan BER-SCOPE PERANGKAT tetap memakai `build_attempt_key`
   (token + ketiga slot), yang unik per kursi menurut konstruksi — bukan
   kunci siswa yang bisa degrade. Ini yang dipakai untuk label perangkat
   di jalur recovery.
"""

from __future__ import annotations

import logging
import shutil
import tempfile
import unittest
from pathlib import Path
from unittest import mock

from PyQt5.QtWidgets import QApplication, QMessageBox

from examvan import config
from examvan.models import Exam, IdentityField
from examvan.ui.server_config import ServerConfigDialog
from examvan.utils import (
    build_attempt_key,
    build_student_key,
    build_student_key_source,
    get_device_label,
    reset_device_label_cache,
)

APP = QApplication.instance() or QApplication([])

LOGGER = "examvan.ui.server_config"
TOKEN = "TOKLAB01"

# Config madrasah yang sangat biasa: ada nama, TIDAK ada nomor ujian.
NO_NUMBER_CONFIG = {
    "nama": "Ahmad",
    "kelas": "7A",
    "agama": "Islam",
    "jenis_kelamin": "L",
}
OTHER_CLASS_SAME_NAME = dict(NO_NUMBER_CONFIG, kelas="8B")
WITH_NUMBER = {"nama": "Ahmad", "nomor_ujian": "01", "kelas": "7A"}


def _exam(fields, exam_id=7):
    return Exam(id=exam_id, name="Ujian", status="active",
                security_level="low", identity_fields=fields)


class _SubmitMarkerTestCase(unittest.TestCase):
    def setUp(self):
        self._tmp = tempfile.mkdtemp(prefix="examvan-r7-keysource-")
        for attr, value in (
            ("_CONFIG_DIR", Path(self._tmp)),
            ("_CONFIG_FILE", Path(self._tmp) / "config.json"),
        ):
            patcher = mock.patch.object(config, attr, value)
            patcher.start()
            self.addCleanup(patcher.stop)
        config._cache = None
        self.addCleanup(setattr, config, "_cache", None)
        self.addCleanup(shutil.rmtree, self._tmp, True)
        reset_device_label_cache()
        self.addCleanup(reset_device_label_cache)

    def _dlg(self, fields):
        dlg = ServerConfigDialog()
        dlg._exam = _exam(fields)
        dlg._server_url = "https://exam.example"
        dlg.input_token.setText(TOKEN)
        dlg._validated_token = TOKEN
        return dlg


NO_NUMBER_FIELDS = [
    IdentityField(key="nama", label="Nama", required=True),
    IdentityField(key="kelas", label="Kelas", required=True),
    IdentityField(key="agama", label="Agama", required=False),
    IdentityField(key="jenis_kelamin", label="Jenis Kelamin", required=False),
]
WITH_NUMBER_FIELDS = [
    IdentityField(key="nama", label="Nama", required=True),
    IdentityField(key="nomor_ujian", label="Nomor Ujian", required=True),
    IdentityField(key="kelas", label="Kelas", required=True),
]


class StudentKeySourceIsExposedTest(unittest.TestCase):
    def test_source_names_the_slot_the_key_came_from(self):
        self.assertEqual(
            build_student_key_source(WITH_NUMBER, TOKEN),
            ("01", "exam_number"),
        )

    def test_source_reports_the_degraded_fallback(self):
        key, source = build_student_key_source(NO_NUMBER_CONFIG, TOKEN)
        self.assertEqual(source, "student_name")
        self.assertEqual(key, "ahmad")

    def test_source_reports_the_token_fallback(self):
        key, source = build_student_key_source({"alamat": "Jl. Mawar"}, TOKEN)
        self.assertEqual(source, "token")
        self.assertEqual(key, TOKEN)

    def test_key_still_matches_build_student_key(self):
        for identity in (WITH_NUMBER, NO_NUMBER_CONFIG, {"kelas": "7A"}):
            with self.subTest(identity=identity):
                self.assertEqual(
                    build_student_key_source(identity, TOKEN)[0],
                    build_student_key(identity, TOKEN),
                )


class NoPromptOnADegradedKeyTest(_SubmitMarkerTestCase):
    def _offer(self, dlg, identity):
        boxes = []

        def fake_exec(self):
            boxes.append(self)
            return QMessageBox.Yes

        with mock.patch.object(QMessageBox, "exec_", fake_exec):
            allowed = dlg._offer_resubmit_choice(dict(identity))
        return allowed, boxes

    def test_name_only_fallback_is_never_prompted(self):
        dlg = self._dlg(NO_NUMBER_FIELDS)
        dlg.close()
        # Marker untuk kunci degraded sudah ada — persis keadaan yang
        # membuat dua classmates berbagi satu prompt.
        config.mark_submitted(7, "ahmad", label="student_name=Ahmad")
        allowed, boxes = self._offer(dlg, NO_NUMBER_CONFIG)
        self.assertEqual(boxes, [],
                         "kunci 'ahmad' dipakai dua siswa berbeda; modal ini "
                         "ditunjukkan ke orang yang salah")
        self.assertTrue(allowed, "menolak join bukan kebijakan ronde ini")

    def test_class_only_fallback_is_never_prompted(self):
        dlg = self._dlg([IdentityField(key="kelas", label="Kelas",
                                       required=True)])
        dlg.close()
        config.mark_submitted(7, "7a", label="student_class=7A")
        allowed, boxes = self._offer(dlg, {"kelas": "7A"})
        self.assertEqual(boxes, [])
        self.assertTrue(allowed)

    def test_token_fallback_is_never_prompted(self):
        dlg = self._dlg([IdentityField(key="alamat", label="Alamat",
                                       required=True)])
        dlg.close()
        config.mark_submitted(7, TOKEN, label="token")
        allowed, boxes = self._offer(dlg, {"alamat": "Jl. Mawar"})
        self.assertEqual(boxes, [])
        self.assertTrue(allowed)

    def test_real_exam_number_is_prompted(self):
        dlg = self._dlg(WITH_NUMBER_FIELDS)
        dlg.close()
        config.mark_submitted(7, "01", label="exam_number=01")
        allowed, boxes = self._offer(dlg, WITH_NUMBER)
        self.assertEqual(len(boxes), 1,
                         "marker untuk nomor ujian nyata tidak lagi "
                         "dikonsumsi — siswa tidak diberi tahu jawabannya "
                         "sudah tercatat")
        self.assertTrue(allowed)

    def test_no_marker_means_no_prompt_even_with_a_number(self):
        dlg = self._dlg(WITH_NUMBER_FIELDS)
        dlg.close()
        allowed, boxes = self._offer(dlg, WITH_NUMBER)
        self.assertEqual(boxes, [])
        self.assertTrue(allowed)

    def test_two_same_named_students_never_share_a_prompt(self):
        dlg = self._dlg(NO_NUMBER_FIELDS)
        dlg.close()
        # Siswa 7 (kelas 7A) selesai lebih dulu dan menulis marker.
        config.mark_submitted(7, "ahmad", label="student_name=Ahmad")
        _first_ok, first_boxes = self._offer(dlg, NO_NUMBER_CONFIG)
        _second_ok, second_boxes = self._offer(dlg, OTHER_CLASS_SAME_NAME)
        self.assertEqual(first_boxes, [])
        self.assertEqual(second_boxes, [])

    def test_the_degraded_skip_is_logged(self):
        dlg = self._dlg(NO_NUMBER_FIELDS)
        dlg.close()
        config.mark_submitted(7, "ahmad", label="student_name=Ahmad")
        with self.assertLogs(LOGGER, level="WARNING") as cm:
            self._offer(dlg, NO_NUMBER_CONFIG)
        joined = " ".join(cm.output).lower()
        self.assertIn("student_name", joined)

    def test_a_config_without_number_nor_name_is_not_alarmed(self):
        dlg = self._dlg([IdentityField(key="alamat", label="Alamat",
                                       required=True)])
        dlg.close()
        config.mark_submitted(7, TOKEN, label="token")
        records = []

        class _Catch(logging.Handler):
            def emit(self, record):
                records.append((record.levelno, record.getMessage()))

        logger = logging.getLogger(LOGGER)
        handler = _Catch()
        logger.addHandler(handler)
        previous = logger.level
        logger.setLevel(logging.INFO)
        try:
            self._offer(dlg, {"alamat": "Jl. Mawar"})
        finally:
            logger.removeHandler(handler)
            logger.setLevel(previous)
        self.assertTrue(records, "melewati modal harus meninggalkan jejak")
        loud = [msg for level, msg in records
                if level >= logging.WARNING]
        self.assertEqual(loud, [], loud)


class DeviceScopedDecisionsUseTheAttemptKeyTest(_SubmitMarkerTestCase):
    """Kursi yang dipakai dua siswa nama-sama harus tetap dua perangkat."""

    def test_attempt_key_separates_same_named_students_in_two_classes(self):
        a = build_attempt_key(TOKEN, NO_NUMBER_CONFIG)
        b = build_attempt_key(TOKEN, OTHER_CLASS_SAME_NAME)
        self.assertNotEqual(a, b)
        self.assertNotEqual(get_device_label(a), get_device_label(b))

    def test_attempt_key_is_used_for_the_recovery_device_label(self):
        dlg = self._dlg(NO_NUMBER_FIELDS)
        dlg.close()
        seen = []

        def _fake_submit(base_url, exam_id, name, number, klass, answers,
                         start_time, mac, identity_data=None, delays=None,
                         on_retry=None, token=""):
            seen.append(mac)
            from examvan.models import SubmitResponse
            return SubmitResponse(success=False, message="tidak jadi")

        with mock.patch(
            "examvan.ui.server_config.api.submit_with_retry", _fake_submit
        ):
            dlg._recovery_identity = dict(NO_NUMBER_CONFIG)
            dlg._server_url = "https://exam.example"
            dlg._recovery_submit_thread(dlg._exam, dict(NO_NUMBER_CONFIG))
        self.assertEqual(len(seen), 1)
        self.assertNotEqual(
            seen[0], get_device_label(build_attempt_key(TOKEN, {})),
            "label perangkat recovery tidak boleh memakai kunci tanpa "
            "identitas",
        )


if __name__ == "__main__":  # pragma: no cover
    unittest.main()