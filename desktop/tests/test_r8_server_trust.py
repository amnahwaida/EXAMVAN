"""Ronde 8 (H3 + medium) — server yang dipakai siswa harus DIPERCAYAKAN
sebelum token kelas dikirim, dan tokennya tidak boleh terbaca diam-diam.

Bug yang ditutup file ini
-------------------------
`_on_connect` hanya menolak `http://`. `https://attacker.example` lolos,
`_connect_thread` menyimpannya dengan `config.set("server_url", url)`, dan
`_load_saved` mengisi ulang kotak itu di peluncuran berikutnya — lengkap
dengan token kelas yang tersimpan. Siswa berikutnya mengetik token 8
karakter miliknya, dan token itu dikirim ke
`GET /api/exams/token/<token>` lalu menjadi `X-Exam-Token` pada
submit/approval/presence. Pada mode static-token itu kredensial hasil
SELURUH KELAS.

Diperparah oleh `_check_certificate_fingerprint`: kunjungan PERTAMA ke
host asing langsung menyimpan sidik jari sertifikat tanpa peringatan apa
pun, jadi kunjungan berikutnya "terbukti" oleh sidik jari yang sama dengan
impostor itu sendiri.

Kebijakan yang dipilih
---------------------
1. Server sekolah (`DEFAULT_SERVER_URL`) dipercaya tanpa tanya. Server
   swakelola yang sah tetap bisa dipakai: begitu dikonfirmasi, host itu
   disimpan sebagai `trusted_server_url` dan tidak pernah ditanyakan lagi.
2. Host yang berbeda dari default DAN belum pernah dikonfirmasi ->
   dialog konfirmasi eksplisit yang menjelaskan apa yang akan dikirim.
   Default tombolnya BATAL. Menolak berarti tidak ada yang dikirim dan
   tidak ada yang disimpan.
3. URL tersimpan yang tidak dipercaya TETAP di-prefill — lab yang memakai
   server swakelola tidak boleh setiap pagi mengetik ulang — tapi dialog
   menampilkan catatan terlihat bahwa host itu bukan server sekolah dan
   akan ditanyakan lagi.
4. Perubahan sidik jari sertifikat untuk host yang sudah dipercaya
   muncul sebagai dialog yang harus disadari, dengan kedua sidik jari di
   dalamnya: "tidak terlihat" bukan "terlihat".

Catatan lain yang ditutup file ini: checkbox "Simpan" tidak
menghentikan penyimpanan token, sementara alasan yang diberikannya di
komentar (`_xor_obfuscate` memakai kunci tetap, bukan token) memang
tidak benar. Kotak token yang dulu dimasking kini sengaja dibuka lagi
(ronde 10) — lihat `TokenFieldIsReadableTestCase` di bawah.
"""

from __future__ import annotations

import os
import shutil
import tempfile
import time
import unittest
from pathlib import Path
from unittest import mock

os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")

from PyQt5.QtWidgets import QApplication, QLineEdit, QMessageBox

from examvan import config
from examvan.models import Exam, HealthResponse, TokenExamResponse
from examvan.ui.server_config import DEFAULT_SERVER_URL, ServerConfigDialog

APP = QApplication.instance() or QApplication([])

SCHOOL = "https://examvan.my.id"
SELF_HOSTED = "https://ujian.sekolah.sch.id"
IMPOSTOR = "https://attacker.example"
TOKEN = "ABCD1234"


def _exam(exam_id=7):
    return Exam(
        id=exam_id, name="Ujian", status="active", security_level="low")


class _Sandbox(unittest.TestCase):
    def setUp(self) -> None:
        self._tmp = tempfile.mkdtemp(prefix="examvan-r8-trust-")
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

    def _dlg(self) -> ServerConfigDialog:
        dlg = ServerConfigDialog()
        self.addCleanup(dlg.deleteLater)
        # `_show_identity_dialog` menjalankan `IdentityDialog.exec_()`,
        # yaitu nested event loop modal. Yang diuji di sini adalah
        # gerbang KONEKSI, jadi slot itu dilepas — tanpa ini, koneksi
        # yang sukses akan menggantung test sampai runner dibunuh.
        try:
            dlg._sig_show_identity.disconnect(dlg._show_identity_dialog)
        except (TypeError, RuntimeError):
            pass
        return dlg

    def _worker_api(self):
        """`api` palsu untuk `_connect_thread` yang dipanggil langsung."""
        return mock.patch.multiple(
            "examvan.ui.server_config.api",
            check_health=mock.Mock(return_value=HealthResponse(success=True)),
            get_exam_by_token=mock.Mock(
                return_value=TokenExamResponse(success=True, exam=_exam())),
        )

    def _pump(self, predicate, timeout=5.0) -> bool:
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            APP.processEvents()
            if predicate():
                return True
            time.sleep(0.01)
        return False

    def _press_connect(self, dlg, url, token=TOKEN, health=None):
        """`_on_connect` sungguhan + tunggu worker selesai memproses.

        `health` adalah mock yang boleh dipasang pemanggil supaya
        "apakah health check benar-benar dipanggil" bisa diuji, bukan
       Diam saja diam saja.
        """
        dlg.input_url.setText(url)
        dlg.input_token.setText(token)
        dlg._exam = None
        dlg._connect_in_flight = False
        dlg.btn_connect.setEnabled(True)
        dlg.input_url.setEnabled(True)
        dlg.input_token.setEnabled(True)
        hc = health if health is not None else mock.Mock(
            return_value=HealthResponse(success=True))
        lookup = mock.Mock(
            return_value=TokenExamResponse(success=True, exam=_exam()))
        with mock.patch("examvan.ui.server_config.api.check_health", hc), \
                mock.patch(
                    "examvan.ui.server_config.api.get_exam_by_token", lookup):
            dlg._on_connect()
            # `self._exam` hanya terisi SETELAH `config.set` di worker,
            # jadi menunggu itu berarti config sudah ditulis juga.
            if hc.called:
                self._pump(lambda: dlg._exam is not None, 3.0)
            APP.processEvents()
        return hc


class _BoxRecorder:
    """Rekam isi dialog tanpa memblokir runner headless.

    Dipakai sebagai pengganti `QMessageBox.exec_`, jadi harus berupa
    FUNCTION biasa: atribut kelas yang bukan descriptor dipanggil tanpa
    `self`, dan `box`-nya akan hilang.
    """

    def __init__(self, reply):
        self.reply = reply
        self.boxes = []

        def fake(box):
            self.boxes.append({
                "title": box.windowTitle(),
                "text": box.text(),
                "informative": box.informativeText(),
                "buttons": [b.text() for b in box.buttons()],
                "default": (box.defaultButton().text()
                            if box.defaultButton() is not None else ""),
            })
            return self.reply

        self.func = fake

    @property
    def asked(self) -> bool:
        return bool(self.boxes)

    @property
    def joined(self) -> str:
        return " ".join(
            f"{b['title']} {b['text']} {b['informative']}"
            for b in self.boxes
        ).lower()


class UntrustedServerTestCase(_Sandbox):
    def test_an_arbitrary_host_is_questioned_before_anything_is_sent(self):
        dlg = self._dlg()
        recorder = _BoxRecorder(QMessageBox.No)
        with mock.patch("examvan.ui.server_config.api.check_health") as hc, \
                mock.patch.object(QMessageBox, "exec_", recorder.func):
            self._press_connect(dlg, IMPOSTOR)
            APP.processEvents()

        self.assertTrue(
            recorder.asked,
            "host asing connects tanpa konfirmasi: token kelas siswa "
            "langsung dikirim ke server itu",
        )
        hc.assert_not_called()
        self.assertEqual(
            config.get("server_url", ""), "",
            "URL yang ditolak tetap disimpan dan akan di-prefill ke siswa "
            "berikutnya",
        )
        self.assertEqual(config.get("exam_token", ""), "")
        self.assertTrue(
            dlg.btn_connect.isEnabled(),
            "setelah menolak, tombol connect harus hidup lagi",
        )

    def test_the_confirmation_says_what_will_be_sent(self):
        dlg = self._dlg()
        recorder = _BoxRecorder(QMessageBox.No)
        with mock.patch("examvan.ui.server_config.api.check_health"), \
                mock.patch.object(QMessageBox, "exec_", recorder.func):
            self._press_connect(dlg, IMPOSTOR)
            APP.processEvents()
        text = recorder.joined
        self.assertIn(IMPOSTOR, text, "dialog harus menyebut host tujuan")
        self.assertIn("token", text)
        self.assertIn("kelas", text)

    def test_the_default_button_is_cancel(self):
        dlg = self._dlg()
        recorder = _BoxRecorder(QMessageBox.No)
        with mock.patch("examvan.ui.server_config.api.check_health"), \
                mock.patch.object(QMessageBox, "exec_", recorder.func):
            self._press_connect(dlg, IMPOSTOR)
            APP.processEvents()
        self.assertTrue(recorder.asked)
        self.assertIn(
            "batal", recorder.boxes[0]["default"].lower(),
            "satu Enter yang tidak sengaja harus membatalkan, bukan "
            "mengirim token kelas ke host asing",
        )

    def test_confirming_trusts_the_host_and_then_stops_asking(self):
        dlg = self._dlg()
        recorder = _BoxRecorder(QMessageBox.Yes)
        with mock.patch.object(QMessageBox, "exec_", recorder.func):
            self._press_connect(dlg, SELF_HOSTED)
        self.assertEqual(config.get("server_url", ""), SELF_HOSTED)
        self.assertEqual(config.get("trusted_server_url", ""), SELF_HOSTED)

        dlg2 = self._dlg()
        with mock.patch.object(QMessageBox, "exec_", recorder.func):
            self._press_connect(dlg2, SELF_HOSTED)
        self.assertEqual(
            len(recorder.boxes), 1,
            "host yang sudah dikonfirmasi ditanyakan lagi — lab swakelola "
            "akan tersangkut dialog tiap peluncuran",
        )

    def test_the_school_server_is_never_questioned(self):
        dlg = self._dlg()
        recorder = _BoxRecorder(QMessageBox.No)
        with mock.patch.object(QMessageBox, "exec_", recorder.func):
            self._press_connect(dlg, SCHOOL)
        self.assertFalse(recorder.asked)
        self.assertEqual(config.get("server_url", ""), SCHOOL)

    def test_a_confirmed_host_is_remembered_across_launches(self):
        config.set("trusted_server_url", SELF_HOSTED)
        config.set("server_url", SELF_HOSTED)
        dlg = self._dlg()
        recorder = _BoxRecorder(QMessageBox.No)
        with mock.patch.object(QMessageBox, "exec_", recorder.func):
            self._press_connect(dlg, SELF_HOSTED)
        self.assertFalse(recorder.asked)


class SavedUntrustedUrlIsNotSilentTestCase(_Sandbox):
    """URL tersimpan boleh ter-prefill, tapi TIDAK boleh diam-diam."""

    def test_an_untrusted_saved_url_carries_a_visible_warning(self):
        config.set("server_url", IMPOSTOR)
        config.set("remember_url", True)
        dlg = self._dlg()
        self.assertEqual(dlg.input_url.text(), IMPOSTOR)
        warning = dlg.server_url_warning_text()
        self.assertTrue(
            warning.strip(),
            "URL yang bukan server sekolah ter-prefill tanpa catatan — "
            "siswa dan pengawas tidak bisa membedakannya dari server sekolah",
        )
        self.assertIn("sekolah", warning.lower())

    def test_the_school_server_has_no_warning(self):
        config.set("server_url", SCHOOL)
        dlg = self._dlg()
        self.assertEqual(dlg.input_url.text(), SCHOOL)
        self.assertEqual(dlg.server_url_warning_text(), "")

    def test_the_warning_is_about_the_current_field_not_the_saved_one(self):
        # Siswa yang mengetik host lain secara manual harus melihat
        # catatan yang sama:abar yang sedang dipakai, bukan yang tersimpan.
        config.set("server_url", SCHOOL)
        dlg = self._dlg()
        dlg.input_url.setText(IMPOSTOR)
        APP.processEvents()
        self.assertIn("sekolah", dlg.server_url_warning_text().lower())


class TokenFieldIsReadableTestCase(_Sandbox):
    """Token ujian tampil apa adanya.

    Berubah dari ronde 10, atas permintaan pengguna: token itu bukan
    password — 8 karakter dari Amplop Lembar Jawaban yang sering diketik
    ulang, dan server menolaknya kalau satu karakter salah. Disamarkan,
    siswa tidak bisa memastikan O vs 0 dan tidak bisa membacanya dari
    layar ruang ujian.
    """

    def test_the_token_field_shows_plaintext(self):
        config.set("exam_token", TOKEN)
        config.set("remember_url", True)
        dlg = self._dlg()
        self.assertEqual(
            dlg.input_token.echoMode(), QLineEdit.Normal,
            "token ujian disamarkan seperti password — siswa tidak bisa "
            "memastikan huruf O vs angka 0 dan tidak bisa membaca token "
            "dari layar",
        )
        self.assertEqual(dlg.input_token.text(), TOKEN)

    def test_the_plain_field_still_connects(self):
        dlg = self._dlg()
        recorder = _BoxRecorder(QMessageBox.No)
        with mock.patch.object(QMessageBox, "exec_", recorder.func):
            self._press_connect(dlg, SCHOOL)
        self.assertFalse(recorder.asked)
        self.assertEqual(dlg.validated_token, TOKEN)


class RememberUrlIsHonestTestCase(_Sandbox):
    """Checkbox "Simpan" harus melakukan apa yang dijanjikan."""

    def test_unticking_it_does_not_leave_the_token_on_disk(self):
        dlg = self._dlg()
        with self._worker_api():
            dlg._connect_thread(SCHOOL, TOKEN, remember_url=False)
        self.assertFalse(config.get("remember_url", True))
        self.assertEqual(
            config.get("exam_token", ""), "",
            "checkbox 'jangan simpan' tidak menghentikan penyimpanan "
            "token — dan token itu kredensial seluruh kelas",
        )

    def test_keeping_it_checked_still_saves_the_token(self):
        dlg = self._dlg()
        with self._worker_api():
            dlg._connect_thread(SCHOOL, TOKEN, remember_url=True)
        self.assertTrue(config.get("remember_url", True))
        self.assertEqual(config.get("exam_token", ""), TOKEN)

    def test_the_recovery_worker_still_has_the_token_when_disk_is_empty(self):
        # Kalau token tidak lagi tersimpan, jalur "Kirim Lagi" TIDAK boleh
        # ikut jadi rusak: worker memakai token yang dicapture di UI thread.
        dlg = self._dlg()
        dlg._server_url = SCHOOL
        dlg._validated_token = TOKEN
        seen = {}

        def fake_submit(url, exam_id, name, number, klass, answers,
                        start_time, mac, identity, token=None, **kw):
            seen["token"] = token
            return type(
                "Resp", (), {"success": False, "status": "error",
                             "message": "ditolak", "job_id": None,
                             "congrats_message": ""},
            )()

        with mock.patch("examvan.ui.server_config.api.submit_with_retry",
                        side_effect=fake_submit):
            dlg._recovery_submit_thread(_exam(), {"nama": "Budi"},
                                        token=TOKEN)
        self.assertEqual(
            seen.get("token"), TOKEN,
            "worker memakai token dari config yang sengaja dikosongkan — "
            "kirim ulang jadi 401 dan fitur recovery mati total",
        )

    def test_the_false_reason_about_the_xor_key_is_gone(self):
        src = (
            Path(__file__).resolve().parents[1]
            / "examvan" / "ui" / "server_config.py"
        ).read_text(encoding="utf-8")
        lines = src.splitlines()
        # Alasannya adalah "`_xor_obfuscate` memakai token sebagai kunci
        # decode", dengan sitasinya `config.py:32-39` — dan baris itu ada
        # di dalam docstring `_tmp_sibling`, bukan `_xor_obfuscate`.
        # `_xor_obfuscate` memakai HANYA `_OBFUSCATE_KEY`; docstring-nya
        # sendiri menyatakan itu.
        self.assertNotIn("config.py:32-39", src)
        # Yang boleh tersisa hanya koreksinya: setiap penyebutan "kunci
        # decode" harus berada di dekat penolakan eksplisit.
        offenders = []
        for i, line in enumerate(lines):
            if "kunci decode" not in line:
                continue
            window = "\n".join(lines[max(0, i - 4):i + 5]).lower()
            if "tidak benar" not in window:
                offenders.append(line.strip())
        self.assertEqual(
            offenders, [],
            "komentar masih memakai '_xor_obfuscate memakai token sebagai "
            "kunci decode' sebagai alasan, bukan sebagai kesalahan yang "
            "ditegaskan: " + repr(offenders),
        )

    def test_the_checkbox_label_names_the_token(self):
        dlg = self._dlg()
        self.assertIn("token", dlg.chk_remember.text().lower())


class CertificateChangeIsVisibleTestCase(_Sandbox):
    """Sidik jari sertifikat yang berubah harus terlihat, bukan hanya log."""

    def test_a_changed_fingerprint_opens_a_visible_dialog(self):
        dlg = self._dlg()
        config.set(f"cert_fp_{SCHOOL}", "AA:BB:CC:11:22:33")
        recorder = _BoxRecorder(QMessageBox.Ok)
        with mock.patch.object(QMessageBox, "exec_", recorder.func):
            dlg._check_certificate_fingerprint(SCHOOL, "DD:EE:FF:44:55:66")
            APP.processEvents()
        self.assertTrue(
            recorder.asked,
            "perubahan sertifikat hanya masuk log — di build --windowed "
            "log itu dibuang, jadi yang terjadi adalah keheningan",
        )
        self.assertIn("aa:bb:cc", recorder.joined)
        self.assertIn("dd:ee:ff", recorder.joined)

    def test_the_first_visit_of_an_untrusted_host_is_not_silently_pinned(self):
        # Host asing tidak pernah "di-pin" diam-diam: `_on_connect`
        # menolak connects sebelum konfirmasi, jadi sidik jarinya tidak
        # pernah masuk config tanpa ada yang menyetujui.
        dlg = self._dlg()
        recorder = _BoxRecorder(QMessageBox.No)
        hc = mock.Mock(return_value=HealthResponse(success=True))
        with mock.patch.object(QMessageBox, "exec_", recorder.func):
            self._press_connect(dlg, IMPOSTOR, health=hc)
            APP.processEvents()
        hc.assert_not_called()
        self.assertEqual(
            config.get(f"cert_fp_{IMPOSTOR}", ""), "",
            "sidik jari host asing tersimpan padahal tidak pernah "
            "disetujui siapa pun",
        )


if __name__ == "__main__":
    unittest.main()
