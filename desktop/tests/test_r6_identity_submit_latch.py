"""Ronde 6 (item 6) — `IdentityDialog._on_submit` butuh latch re-entran.

Bug / asimetri
--------------
`ServerConfigDialog._on_connect` punya `_connect_in_flight` (audit HIGH H3):
Enter pada QLineEdit tetap terhubung meski tombolnya sudah disabled, jadi
satu klik ganda + Enter menghasilkan dua `_connect_thread` paralel. Dialog
identitas punya sumber pemicu yang persis sama — `btn_connect` tidak, tapi
`_submit_btn.clicked` DAN `last_input.returnPressed` keduanya terhubung ke
`_on_submit`, dan `returnPressed` MEMANCARKAN ULANG (auto-repeat) bila
tombol ditahan. `_on_submit` tidak punya latch apa pun.

Hari ini `accept()` kedua tidak merusak apa pun: `QDialog::done()` pada
dialog yang sudah ditutup itu no-op. Dan itulah justru bahayanya — sifat
"harmless" itu tidak dijamin oleh kode mana pun, hanya oleh detail
implementasi Qt. Tidak ada bedanya dengan `_connect_in_flight`, yang
akibatnya NYATA (dua dialog identitas, dua `config.set` yang saling
menimpa) sebelum lantainya ditambahkan.

Latch dipasang SESUDA validasi, bukan sebelumnya: validasi yang gagal harus
meninggalkan latch BERSIH supaya siswa bisa memperbaiki isian dan menekan
lagi. Latch yang dipasang di muka hanya memindahkan "hanging form" ke
"form yang tidak bisa dikirim ulang" — lebih buruk, karena siswa tidak
pernah tahu kenapa.

Semua `QMessageBox` di-stub supaya tidak pernah memblokir di headless.
"""

from __future__ import annotations

import unittest
from unittest import mock

from PyQt5.QtWidgets import QApplication

APP = QApplication.instance() or QApplication([])

from examvan.models import Exam, IdentityField
from examvan.ui.identity_dialog import IdentityDialog

EXAM = Exam(id=7, name="Ujian", status="active")
FIELDS = [
    IdentityField(key="nama", label="Nama", required=True),
    IdentityField(key="nomor_ujian", label="Nomor", required=True),
]



def _shown_errors(dlg):
    """Key field yang pesannya sedang aktif (label error inline)."""
    return [k for k, e in dlg._error_labels.items() if not e.isHidden()]


def _dialog(fields=None):
    exam = Exam(
        id=EXAM.id,
        name=EXAM.name,
        status=EXAM.status,
        identity_fields=list(fields if fields is not None else FIELDS),
    )
    with mock.patch(
        "examvan.ui.identity_dialog.QDialog.exec_", return_value=1
    ):
        return IdentityDialog(exam)


def _fill(dlg, **values):
    for key, val in values.items():
        dlg._inputs[key].setText(val)


class SubmitLatchTest(unittest.TestCase):
    def test_two_rapid_submits_accept_once(self):
        dlg = _dialog()
        try:
            _fill(dlg, nama="Andi", nomor_ujian="N01")
            with mock.patch.object(dlg, "accept") as accept, \
                    mock.patch(
                        "examvan.ui.identity_dialog.QMessageBox.warning"
                    ) as warn:
                dlg._on_submit()
                dlg._on_submit()
            accept.assert_called_once()
            warn.assert_not_called()
            self.assertEqual(_shown_errors(dlg), [])
        finally:
            dlg.close()

    def test_ten_rapid_submits_accept_once(self):
        dlg = _dialog()
        try:
            _fill(dlg, nama="Andi", nomor_ujian="N01")
            with mock.patch.object(dlg, "accept") as accept, \
                    mock.patch(
                        "examvan.ui.identity_dialog.QMessageBox.warning"
                    ):
                for _ in range(10):
                    dlg._on_submit()
            self.assertEqual(accept.call_count, 1)
        finally:
            dlg.close()

    def test_validation_still_runs_before_the_latch(self):
        """Latch tidak boleh mendahului validasi."""
        dlg = _dialog()
        try:
            # Kosong → ditolak, latch harus tetap BERSIH.
            with mock.patch.object(dlg, "accept") as accept, \
                    mock.patch(
                        "examvan.ui.identity_dialog.QMessageBox.warning"
                    ) as warn:
                dlg._on_submit()
            accept.assert_not_called()
            # Field kosong kini menandai dirinya sendiri, bukan lewat
            # dialog modal (lihat ValidationUXTest). Yang dijaga di sini
            # tetap: isian ditolak, dan tidak ada dialog yang muncul.
            warn.assert_not_called()
            self.assertEqual(_shown_errors(dlg), ["nama", "nomor_ujian"])
            self.assertFalse(getattr(dlg, "_submitting", False))

            # Murahan: perbaiki isiannya, coba lagi — harus bisa masuk.
            _fill(dlg, nama="Andi", nomor_ujian="N01")
            with mock.patch.object(dlg, "accept") as accept, \
                    mock.patch(
                        "examvan.ui.identity_dialog.QMessageBox.warning"
                    ) as warn:
                dlg._on_submit()
            accept.assert_called_once()
            warn.assert_not_called()
            self.assertEqual(_shown_errors(dlg), [])
        finally:
            dlg.close()

    def test_failed_validation_does_not_consume_the_second_attempt(self):
        """Kegagalan berulang tetap menyisakan jalan bagi siswa."""
        dlg = _dialog()
        try:
            with mock.patch.object(dlg, "accept") as accept, \
                    mock.patch(
                        "examvan.ui.identity_dialog.QMessageBox.warning"
                    ) as warn:
                for _ in range(3):
                    dlg._on_submit()
            accept.assert_not_called()
            # Tiga percobaan, tiga kali ditolak karena isian kosong --
            # dan latch tetap BERSIH, jadi siswa masih bisa memperbaiki
            # isian lalu menekan lagi.
            warn.assert_not_called()
            self.assertEqual(_shown_errors(dlg), ["nama", "nomor_ujian"])
            self.assertFalse(getattr(dlg, "_submitting", False))
        finally:
            dlg.close()

    def test_latch_is_set_before_accept(self):
        """`_on_submit` yang sedang berjalan melihat latch menyala."""
        dlg = _dialog()
        try:
            _fill(dlg, nama="Andi", nomor_ujian="N01")
            seen = []

            def fake_accept():
                seen.append(dlg._submitting)

            with mock.patch.object(dlg, "accept", fake_accept), \
                    mock.patch(
                        "examvan.ui.identity_dialog.QMessageBox.warning"
                    ):
                dlg._on_submit()
            self.assertEqual(seen, [True])
        finally:
            dlg.close()

    def test_broken_config_refusal_also_leaves_the_latch_clear(self):
        """Penolakan konfigurasi (key tidak terbaca) bukan alasan mengunci.

        `key_is_text=False` meniru `{"key": 123}` dari API: angka JSON
        tidak bisa di-decode ke `Key string` di Go. Ronde 7 (M2) mengganti
        tebakan "key harus punya huruf" dengan fakta itu, jadi string
        `"123.0"` yang diketik tangan kini key SAH dan tidak lagi ditolak.
        """
        dlg = _dialog([
            IdentityField(key="123.0", label="Absen", required=True,
                          key_is_text=False),
        ])
        try:
            _fill(dlg, **{"123.0": "42"})
            with mock.patch.object(dlg, "accept") as accept, \
                    mock.patch(
                        "examvan.ui.identity_dialog.QMessageBox.warning"
                    ) as warn:
                dlg._on_submit()
                dlg._on_submit()
            accept.assert_not_called()
            self.assertEqual(warn.call_count, 2,
                             "penolakan konfigurasi tidak boleh memakai "
                             "latch — tidak ada yang bisa diperbaiki siswa "
                             "dan tombol harus tetap bisa ditekan ulang "
                             "untuk melihat pesan")
        finally:
            dlg.close()

    def test_a_fresh_dialog_starts_with_a_clear_latch(self):
        dlg = _dialog()
        try:
            self.assertFalse(dlg._submitting)
        finally:
            dlg.close()


if __name__ == "__main__":  # pragma: no cover
    unittest.main()