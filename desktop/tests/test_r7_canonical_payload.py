"""Ronde 7 (H4) — `identity_data_with_canonical` harus benar-benar dipakai.

Bug
---
`utils.identity_data_with_canonical` adalah kode mati. `grep` non-test
hanya menemukan satu referensinya: docstring-nya sendiri. Docstring itu
justru menuliskan alasannya — "call site tidak diterapkan di ronde ini
karena file-nya di luar daftar edit" — alasan yang sudah basi
(`ui/identity_dialog.py` justru file yang disunting di commit
yang sama, +141 baris).

Selama ini tiga komentar di repo menyatakan seolah-olah payload selalu
membawa kunci kanonik, dan `tests/test_r5_canonical_payload.py` menguji
fungsi tularnya saja. Akibatnya suite hijau sementara payload yang
benar-benar melewati `api.submit_exam` tidak punya satu pun kunci kanonik:
konfigurasi yang tidak punya kata slot (`alamat`, `kode_pos`, `field_<n>`)
tetap submit dengan HTTP 200 sementara `student_name`/`exam_number`/
`student_class` kosong di DB — dan kunci siswa jatuh ke token ujian, satu
kunci untuk seluruh kelas.

Pilihan ronde ini: DIWIRINGKAN, bukan dihapus
---------------------------------------------
Dua opsi ada: menyambungkannya, atau menghapus fungsi + tiga komentar
palsu + file test-nya. Yang dipilih adalah menyambungkannya, di
`IdentityDialog.get_identity_data` — satu-satunya choke point tempat
identitas siswa jadi payload, dipakai oleh submit, approval, dialog
persetujuan, recovery, dan penyimpanan config.

Alasannya bukan sekadar "fungsi ini berguna": H2 (nomor telepon
menggantikan nomor ujian) diselesaikan di client, sedangkan
`webui/internal/helpers/student_key.go` yang menghitung kunci yang sama
di sisi pengawas masih menebak `no_hp` sebagai nomor ujian. Begitu payload
membawa `exam_number` hasil pemetaan client, lapisan kanonik Go menang
terlebih dulu sebelum ia sampai ke tebakan kata, dan kedua sisi kembali punya
kunci yang sama tanpa perlu Go diubah. Membuang fungsi ini akan
meninggalkan pagar itu.
"""

from __future__ import annotations

import json
import unittest
from unittest import mock

from PyQt5.QtWidgets import QApplication

APP = QApplication.instance() or QApplication([])

from examvan import api
from examvan.models import Exam, IdentityField
from examvan.ui.identity_dialog import IdentityDialog


def _dialog(fields, saved=None):
    exam = Exam(id=7, name="Ujian Matematika", status="active",
                identity_fields=fields)
    with mock.patch("examvan.ui.identity_dialog.QDialog.exec_",
                    return_value=1):
        return IdentityDialog(exam, saved_data=saved or {})


def _fill_and_accept(dlg, values):
    for key, value in values.items():
        dlg._inputs[key].setText(value)
    with mock.patch.object(dlg, "accept") as accept:
        dlg._on_submit()
    assert accept.call_count == 1, accept.call_count
    return dlg.get_identity_data()


class CanonicalKeysReachTheWireTest(unittest.TestCase):
    """Payload yang keluar dari dialog harus sudah kanonik."""

    def _fields(self):
        return [
            IdentityField(key="nama", label="Nama", required=True),
            IdentityField(key="nomor_ujian", label="Nomor Ujian",
                          required=True),
            IdentityField(key="kelas", label="Kelas", required=True),
        ]

    def test_dialog_payload_carries_the_three_canonical_keys(self):
        dlg = _dialog(self._fields())
        self.addCleanup(dlg.close)
        data = _fill_and_accept(dlg, {
            "nama": "Andi", "nomor_ujian": "01", "kelas": "9A"})
        self.assertEqual(data["student_name"], "Andi")
        self.assertEqual(data["exam_number"], "01")
        self.assertEqual(data["student_class"], "9A")

    def test_original_keys_are_still_sent(self):
        # Server memvalidasi terhadap key yang TERSIMPAN, jadi key asli
        # tidak boleh hilang — kunci kanonik hanya tambahan.
        dlg = _dialog(self._fields())
        self.addCleanup(dlg.close)
        data = _fill_and_accept(dlg, {
            "nama": "Andi", "nomor_ujian": "01", "kelas": "9A"})
        for key, value in (("nama", "Andi"), ("nomor_ujian", "01"),
                           ("kelas", "9A")):
            self.assertEqual(data[key], value)

    def test_blank_fields_never_produce_blank_canonical_keys(self):
        dlg = _dialog([
            IdentityField(key="nama", label="Nama", required=True),
            IdentityField(key="nomor_ujian", label="Nomor", required=False),
            IdentityField(key="kelas", label="Kelas", required=False),
        ])
        self.addCleanup(dlg.close)
        data = _fill_and_accept(dlg, {
            "nama": "Andi", "nomor_ujian": "   ", "kelas": ""})
        self.assertEqual(data["student_name"], "Andi")
        self.assertNotIn("exam_number", data)
        self.assertNotIn("student_class", data)

    def test_degenerate_config_is_not_invented(self):
        # Tidak ada kata slot -> tidak ada yang boleh dikarang; key
        # sintetis pun tetap terkirim apa adanya.
        dlg = _dialog([
            IdentityField(key="alamat", label="Alamat", required=True),
            IdentityField(key="kode_pos", label="Kode Pos", required=False),
        ])
        self.addCleanup(dlg.close)
        data = _fill_and_accept(dlg, {
            "alamat": "Jl. Mawar 1", "kode_pos": "12345"})
        self.assertNotIn("student_name", data)
        self.assertNotIn("exam_number", data)
        self.assertNotIn("student_class", data)

    def test_empty_form_adds_nothing(self):
        dlg = _dialog(self._fields())
        self.addCleanup(dlg.close)
        data = dlg.get_identity_data()
        self.assertEqual(
            sorted(k for k, v in data.items() if str(v).strip()),
            [], "form kosong tidak boleh menambah kunci kanonik",
        )


class SubmitPayloadContainsCanonicalKeysTest(unittest.TestCase):
    """Bukan hanya fungsi tularnya — payload yang lewat `api.submit_exam`.

    `test_r5_canonical_payload.py` hanya menguji `identity_data_with_
    canonical` secara langsung, dan itulah alasan suite bisa hijau
    sementara wire payload tidak punya kunci kanonik. Test ini menutup
    jalurnya: dialog -> payload -> body POST.
    """

    def _submit_body(self, identity, exam):
        captured = {}

        def _fake_request(url, method="GET", headers=None, body=None,
                          timeout=30):
            captured["body"] = json.loads(json.dumps(body or {}))
            return {"success": True, "status": "done"}

        with mock.patch.object(api, "_make_request", side_effect=_fake_request):
            api.submit_exam(
                base_url="https://exam.example",
                exam_id=exam.id,
                student_name=str(api.map_identity_to_standard(identity)
                                 .get("student_name", "")),
                exam_number=str(api.map_identity_to_standard(identity)
                                .get("exam_number", "")),
                student_class=str(api.map_identity_to_standard(identity)
                                  .get("student_class", "")),
                answers={"1": "A"},
                start_time="2026-10-03T07:00:00Z",
                mac_address="DESKTOP:abcd1234",
                identity_data=identity,
                token="ABCD1234",
            )
        return captured["body"]

    def test_body_identity_data_has_the_canonical_keys(self):
        dlg = _dialog([
            IdentityField(key="nama", label="Nama", required=True),
            IdentityField(key="nomor_ujian", label="Nomor Ujian",
                          required=True),
            IdentityField(key="kelas", label="Kelas", required=True),
        ])
        self.addCleanup(dlg.close)
        identity = _fill_and_accept(dlg, {
            "nama": "Andi", "nomor_ujian": "01", "kelas": "9A"})
        body = self._submit_body(identity, dlg._exam)
        wire = body["identity_data"]
        self.assertEqual(wire["student_name"], "Andi", wire)
        self.assertEqual(wire["exam_number"], "01", wire)
        self.assertEqual(wire["student_class"], "9A", wire)
        # Key asli ikut, supaya validasi server terhadap key tersimpan
        # tetap bisa berjalan.
        self.assertEqual(wire["nama"], "Andi")
        self.assertEqual(wire["nomor_ujian"], "01")
        self.assertEqual(wire["kelas"], "9A")

    def test_phone_number_does_not_reach_the_wire_as_exam_number(self):
        # H2 + H4 bersama: client tidak lagi memakai `no_hp`, dan payload
        # yang membawa kunci kanonik membuat Go menghitung kunci yang
        # sama meski `student_key.go` masih menebak `no_hp`.
        dlg = _dialog([
            IdentityField(key="nama", label="Nama", required=True),
            IdentityField(key="nomor_ujian", label="Nomor Ujian",
                          required=True),
            IdentityField(key="kelas", label="Kelas", required=True),
            IdentityField(key="no_hp", label="No. HP", required=False),
        ])
        self.addCleanup(dlg.close)
        identity = _fill_and_accept(dlg, {
            "nama": "Andi", "nomor_ujian": "01", "kelas": "9A",
            "no_hp": "0812"})
        body = self._submit_body(identity, dlg._exam)
        wire = body["identity_data"]
        self.assertEqual(wire["exam_number"], "01", wire)
        self.assertEqual(wire["no_hp"], "0812", wire)
        self.assertNotEqual(wire["exam_number"], wire["no_hp"])


class NoFalseCommentsLeftBehindTest(unittest.TestCase):
    """Tiga komentar yang menyatakan perilaku tidak ada harus hilang."""

    STALE = [
        "tidak diterapkan di ronde ini",
        "file-nya di luar daftar edit",
    ]

    def test_utils_docstring_no_longer_claims_the_call_site_is_missing(self):
        path = __import__("pathlib").Path(__file__).resolve().parents[1]
        for name in ("examvan/utils.py", "tests/test_r5_canonical_payload.py"):
            lines = (path / name).read_text(encoding="utf-8").splitlines()
            for lineno, line in enumerate(lines, 1):
                for needle in self.STALE:
                    self.assertNotIn(
                        needle, line,
                        f"{name}:{lineno} masih menyatakan call site belum "
                        "diterapkan",
                    )


if __name__ == "__main__":  # pragma: no cover
    unittest.main()