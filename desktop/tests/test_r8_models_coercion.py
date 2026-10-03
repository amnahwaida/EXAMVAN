"""H5 — Field yang akan sampai ke Qt harus TEKS, apa pun yang dikirim server.

`models.py` meneruskan `status`, `message`, `error`, `job_id`,
`congrats_message`, dan `error_code` apa adanya. Server yang rusak (proxy,
WAF, captive portal, atau_payload tangan yang salah bentuk) bisa mengirim
angka atau objek di kolom itu, dan hasilnya sampai ke Qt tanpa di-coerce.

Bukti eksekusi (fungsi asli, sebelum perbaikan):

    HealthResponse.from_json({"status": 12345}).status      -> 12345   (int)
    SubmitResponse.from_json({"congrats_message": [1,2]})
        .congrats_message                                    -> [1, 2]  (list)
    RequestApprovalResponse.from_json({"status": 7}).status  -> 7      (int)

Dampaknya bukan kosmetik:

  * `ui/congratulations.py:238` memanggil `.strip()` pada
    `congrats_message` -> `AttributeError` untuk int/list/dict.
  * Di jalur auto-submit, `AttributeError` itu tertelan `log.debug` di
    `exam_viewer.py:1674`, jadi `_on_auto_submit_done` TIDAK PERNAH
    dijalankan, `_auto_submit_pending` tetap `True`, dan siswa terkunci di
    layar progres sampai watchdog 110 detik.
  * Di jalur manual, siswa
    diberi tahu submit-nya GAGAL padahal sukses.

Perbaikannya di satu titik: `models.py` adalah choke point tunggal semua
`from_json`, jadi coerce di sana membuat semua konsumen aman.

Coercion ini BUKAN filter truthiness. `"false"` harus tetap `"false"` dan
`12345` harus menjadi `"12345"` — bukan `""` — supaya siswa dan log masih
membaca apa yang sebenarnya dikirim server.
"""

from __future__ import annotations

import json
import os
import unittest

os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")

from examvan.models import (
    Exam,
    HealthResponse,
    IdentityField,
    RequestApprovalResponse,
    SubmitResponse,
    TokenExamResponse,
)

# Nilai yang harus TETAP utuh setelah coercion: string yang salah bentuk
# tetap salah bentuk, dan tidak boleh dihapus jadi kosong.
PRESERVED = ("false", "0", "no", "off", "", "  ", "0.0", "None", "12345")


class _Numbers(unittest.TestCase):
    """int dan float harus jadi teks representasinya."""

    def test_health_status_int(self):
        self.assertIsInstance(
            HealthResponse.from_json({"status": 12345}).status, str)
        self.assertEqual(
            HealthResponse.from_json({"status": 12345}).status, "12345")

    def test_health_version_int(self):
        self.assertEqual(
            HealthResponse.from_json({"version": 3}).version, "3")

    def test_health_fingerprint_int(self):
        self.assertEqual(
            HealthResponse.from_json({"certificate_fingerprint": 1}).certificate_fingerprint,
            "1")

    def test_health_server_time_int(self):
        self.assertEqual(
            HealthResponse.from_json({"server_time_utc": 5}).server_time_utc, "5")

    def test_submit_message_int(self):
        self.assertEqual(
            SubmitResponse.from_json({"message": 500}).message, "500")

    def test_submit_status_int(self):
        r = SubmitResponse.from_json({"status": 202})
        self.assertIsInstance(r.status, str)
        self.assertEqual(r.status, "202")

    def test_submit_job_id_int(self):
        self.assertEqual(SubmitResponse.from_json({"job_id": 7}).job_id, "7")

    def test_submit_congrats_message_int(self):
        r = SubmitResponse.from_json({"congrats_message": 99})
        self.assertIsInstance(r.congrats_message, str)
        self.assertEqual(r.congrats_message, "99")

    def test_submit_error_code_int(self):
        r = SubmitResponse.from_json({"error_code": 7})
        self.assertIsInstance(r.error_code, str)
        # `retryable` membandingkan dengan TEKS, jadi int akan selalu salah.
        self.assertEqual(r.error_code, "7")

    def test_token_exam_error_and_message(self):
        r = TokenExamResponse.from_json({"error": 1, "message": [1, 2]})
        self.assertEqual(r.error, "1")
        self.assertEqual(r.message, "[1, 2]")

    def test_approval_status_and_message(self):
        r = RequestApprovalResponse.from_json({"status": 7, "message": 403})
        self.assertIsInstance(r.status, str)
        self.assertEqual(r.status, "7")
        self.assertEqual(r.message, "403")

    def test_exam_status_and_security_level(self):
        e = Exam.from_json({"id": 1, "name": "Ujian", "status": 1,
                            "security_level": 9})
        self.assertIsInstance(e.status, str)
        self.assertEqual(e.status, "1")
        self.assertIsInstance(e.security_level, str)
        self.assertEqual(e.security_level, "9")


class _Containers(unittest.TestCase):
    """list dan dict harus jadi teks, TIDAK jadi string kosong."""

    def test_submit_congrats_message_list(self):
        r = SubmitResponse.from_json({"congrats_message": [1, 2]})
        self.assertIsInstance(r.congrats_message, str)

    def test_submit_congrats_message_dict(self):
        r = SubmitResponse.from_json({"congrats_message": {"a": 1}})
        self.assertIsInstance(r.congrats_message, str)

    def test_submit_message_dict(self):
        r = SubmitResponse.from_json({"message": {"error": "x"}})
        self.assertIsInstance(r.message, str)
        self.assertIn("error", r.message)

    def test_approval_message_list(self):
        r = RequestApprovalResponse.from_json({"message": ["a", "b"]})
        self.assertIsInstance(r.message, str)

    def test_exam_status_list(self):
        e = Exam.from_json({"id": 1, "name": "X", "status": ["live"]})
        self.assertIsInstance(e.status, str)


class _NotAFilter(unittest.TestCase):
    """Coercion, bukan filter: isi aslinya harus utuh."""

    def test_false_like_strings_are_preserved(self):
        for value in PRESERVED:
            with self.subTest(value=value):
                r = SubmitResponse.from_json({"congrats_message": value})
                self.assertEqual(r.congrats_message, value)

    def test_zero_is_not_dropped(self):
        self.assertEqual(
            SubmitResponse.from_json({"job_id": 0}).job_id, "0")
        self.assertEqual(
            HealthResponse.from_json({"status": 0}).status, "0")

    def test_non_string_error_code_is_not_empty(self):
        # `retryable` hanya menolak error_code == "non_json_response";
        # nilai non-teks yang jadi "" akan lolos sebagai retryable dan
        # membuat client mengulang submit Empat kali ke halaman yang sama.
        r = SubmitResponse.from_json({"error_code": False})
        self.assertIsInstance(r.error_code, str)
        self.assertNotEqual(r.error_code, "")

    def test_booleans_become_readable_text(self):
        self.assertEqual(
            SubmitResponse.from_json({"success": True}).message, "")
        self.assertEqual(
            SubmitResponse.from_json({"job_id": True}).job_id, "True")


class _NoneHandling(unittest.TestCase):
    """None eksplisit harus jadi default yang didokumentasikan."""

    def test_submit_optional_text_becomes_none(self):
        r = SubmitResponse.from_json({
            "congrats_message": None, "error_code": None, "job_id": None,
            "status": None,
        })
        self.assertIsNone(r.congrats_message)
        self.assertIsNone(r.error_code)
        self.assertIsNone(r.job_id)
        self.assertIsNone(r.status)

    def test_token_exam_none_message(self):
        r = TokenExamResponse.from_json({"error": None, "message": None})
        self.assertIsNone(r.error)
        self.assertIsNone(r.message)

    def test_health_none_status_is_empty_text(self):
        # Default HealthResponse.status adalah "" (bukan None) — default
        # dataclass harus tetap berlaku, bukan jadi "None".
        self.assertEqual(
            HealthResponse.from_json({"status": None}).status, "")
        self.assertEqual(
            HealthResponse.from_json({"version": None}).version, "")

    def test_missing_fields_keep_the_documented_defaults(self):
        r = SubmitResponse.from_json({})
        self.assertEqual(r.message, "")
        self.assertIsNone(r.status)
        self.assertIsNone(r.job_id)
        self.assertIsNone(r.congrats_message)
        self.assertIsNone(r.error_code)

        h = HealthResponse.from_json({})
        self.assertEqual(h.status, "")
        self.assertEqual(h.version, "")
        self.assertIsNone(h.certificate_fingerprint)

        a = RequestApprovalResponse.from_json({})
        self.assertEqual(a.status, "pending")
        self.assertEqual(a.message, "")

        t = TokenExamResponse.from_json({})
        self.assertIsNone(t.error)
        self.assertIsNone(t.message)

    def test_explicit_null_uses_the_documented_default_not_none(self):
        # `RequestApprovalResponse.status` bertipe `str` dengan default
        # "pending", jadi `null` diperlakukan sebagai "tidak ada jawaban"
        # — sama seperti `_as_dict` untuk payload yang salah bentuk. Kalau
        # dibiarkan `None`, `ui/waiting_approval.py:243`
        # (`resp.status in ("pending", "error")`) tidak pernah cocok dan
        # dialog tidak pernah menampilkan pesan "Mencoba menghubungkan
        # ulang..." saat server sedang menolak.
        a = RequestApprovalResponse.from_json({"status": None})
        self.assertEqual(a.status, "pending")
        self.assertIsInstance(a.status, str)

    def test_explicit_null_error_becomes_empty_message(self):
        a = RequestApprovalResponse.from_json({"error": None, "message": None})
        self.assertEqual(a.message, "")
        self.assertIsInstance(a.message, str)


class _WellFormedUnchanged(unittest.TestCase):
    """Input yang benar tidak boleh berubah sama sekali."""

    def test_health_well_formed(self):
        h = HealthResponse.from_json({
            "success": True, "status": "ok", "version": "1.2.3",
            "certificate_fingerprint": "AA:BB", "server_time_utc": "2026-01-01T00:00:00Z",
        })
        self.assertTrue(h.success)
        self.assertEqual(h.status, "ok")
        self.assertEqual(h.version, "1.2.3")
        self.assertEqual(h.certificate_fingerprint, "AA:BB")
        self.assertEqual(h.server_time_utc, "2026-01-01T00:00:00Z")

    def test_submit_well_formed(self):
        r = SubmitResponse.from_json({
            "success": True, "message": "Berhasil", "status": "queued",
            "job_id": "abc-123", "score": 87.5,
            "congrats_message": "<b>Hebat!</b>", "error_code": "none",
        })
        self.assertTrue(r.success)
        self.assertEqual(r.message, "Berhasil")
        self.assertEqual(r.status, "queued")
        self.assertEqual(r.job_id, "abc-123")
        self.assertEqual(r.score, 87.5)
        self.assertEqual(r.congrats_message, "<b>Hebat!</b>")
        self.assertEqual(r.error_code, "none")

    def test_submit_retryable_semantics_unchanged(self):
        self.assertTrue(SubmitResponse.from_json({}).retryable)
        self.assertFalse(SubmitResponse.from_json({
            "error_code": "non_json_response"}).retryable)
        self.assertFalse(SubmitResponse.from_json({
            "error_code": "non_json_response", "http_status": 200}).retryable)

    def test_approval_well_formed(self):
        a = RequestApprovalResponse.from_json({
            "success": True, "status": "approved", "message": "OK"})
        self.assertTrue(a.success)
        self.assertEqual(a.status, "approved")
        self.assertEqual(a.message, "OK")

    def test_approval_success_defaults_to_status_presence(self):
        # `success=bool(data.get("success", "status" in data))` — absent
        # `success` berarti True kalau ada `status`. Tidak boleh berubah.
        self.assertTrue(RequestApprovalResponse.from_json({"status": "pending"}).success)
        self.assertFalse(RequestApprovalResponse.from_json({"message": "x"}).success)

    def test_exam_well_formed(self):
        e = Exam.from_json({
            "id": 12, "name": "Matematika", "status": "active",
            "security_level": "high", "strict_mode": True,
            "public_results": False, "end_time": "2026-10-03T12:00:00Z",
            "size_mb": 1.5,
            "identity_fields": [
                {"key": "nama", "label": "Nama", "required": True},
                {"key": "kelas", "label": "Kelas", "required": False},
            ],
        })
        self.assertEqual(e.id, 12)
        self.assertEqual(e.name, "Matematika")
        self.assertEqual(e.status, "active")
        self.assertEqual(e.level, "strict")
        self.assertTrue(e.is_strict)
        self.assertFalse(e.public_results)
        self.assertEqual(e.size_mb, 1.5)
        self.assertEqual(e.end_time, "2026-10-03T12:00:00Z")
        self.assertEqual(
            [(f.key, f.label, f.required) for f in e.identity_fields],
            [("nama", "Nama", True), ("kelas", "Kelas", False)])

    def test_exam_name_markup_is_kept_verbatim(self):
        # Nama ujian boleh berisi markup dari config guru; sanitizing-nya
        # milik lapisan render, bukan parser.
        e = Exam.from_json({"id": 1, "name": "<b>Ujian</b> &amp;别的"})
        self.assertEqual(e.name, "<b>Ujian</b> &amp;别的")

    def test_non_dict_payload_still_becomes_defaults(self):
        for payload in (None, [], 123, "ok"):
            with self.subTest(payload=payload):
                self.assertEqual(SubmitResponse.from_json(payload).message, "")
                self.assertEqual(HealthResponse.from_json(payload).status, "")
                self.assertEqual(
                    RequestApprovalResponse.from_json(payload).status, "pending")


class RequiredIsStringDecoded(unittest.TestCase):
    """`required` harus konsisten dengan `strict_mode` dan `public_results`.

    `bool("false")` adalah True, jadi field opsional yang server kirim
    sebagai string — persis seperti yang dilakukan server untuk
    `strict_mode`/`public_results` — menjadi WAJIB. Siswa lalu dipaksa
    mengisi kolom yang memang opsional, dan tidak ada jalan keluar dari
    dialog.
    """

    def _required(self, raw):
        return Exam.from_json({
            "id": 1, "name": "X",
            "identity_fields": [{"key": "kelas", "label": "Kelas",
                                 "required": raw}],
        }).identity_fields[0].required

    def test_false_like_strings_are_optional(self):
        for raw in ("false", "0", "", "  ", "no", "off", "FALSE", "Off"):
            with self.subTest(raw=raw):
                self.assertIs(self._required(raw), False)

    def test_true_like_strings_are_required(self):
        for raw in ("true", "1", "yes", "on", "TRUE", "On"):
            with self.subTest(raw=raw):
                self.assertIs(self._required(raw), True)

    def test_absent_stays_optional(self):
        e = Exam.from_json({
            "id": 1, "name": "X",
            "identity_fields": [{"key": "kelas", "label": "Kelas"}]})
        self.assertIs(e.identity_fields[0].required, False)

    def test_real_booleans_still_work(self):
        self.assertIs(self._required(True), True)
        self.assertIs(self._required(False), False)

    def test_identity_field_defaults_unaffected(self):
        self.assertIs(IdentityField(key="a", label="A").required, False)
        self.assertIs(IdentityField(key="a", label="A").key_is_text, True)


class RoundTripTest(unittest.TestCase):
    """Payload harus tetap bisa diserialisasi (tidak ada nilai aneh)."""

    def test_non_string_payloads_round_trip_as_json(self):
        payloads = (
            {"status": 12345},
            {"message": [1, 2], "congrats_message": {"a": 1}, "job_id": 7},
            {"status": "queued", "message": "ok", "error_code": False},
        )
        for payload in payloads:
            with self.subTest(payload=payload):
                resp = SubmitResponse.from_json(payload)
                for name in ("message", "status", "job_id",
                             "congrats_message", "error_code"):
                    value = getattr(resp, name)
                    self.assertTrue(
                        value is None or isinstance(value, str),
                        f"{name}={value!r} bukan teks",
                    )
                json.dumps({"resp": payload})  # payload harus tetap serializable

    def test_congrats_message_supports_the_str_call_the_ui_makes(self):
        # `ui/congratulations.py:238` melakukan `(congrats_message or
        # "").strip()`. Setelah parse, itu tidak boleh melempar.
        for payload in ({"congrats_message": 1}, {"congrats_message": [1]},
                        {"congrats_message": {"a": 1}},
                        {"congrats_message": None}):
            with self.subTest(payload=payload):
                r = SubmitResponse.from_json(payload)
                try:
                    (r.congrats_message or "").strip()
                except AttributeError as exc:
                    self.fail(f"{payload} -> AttributeError: {exc}")


class FalseCommentClaimsTest(unittest.TestCase):
    """models.py:118-135 mengklaim dua hal yang SALAH."""

    def test_str_of_an_int_is_not_a_float(self):
        # Komentarnya mengklaim angka 123 jadi "123.0". Padahal str(123)
        # adalah "123" — jadi pemetaan yang diklaim itu tidak terjadi, dan
        # pembaca yang percaya komentarnya akan salah menghitung kegagalannya.
        self.assertEqual(str(123), "123")
        field = Exam.from_json({
            "id": 1, "name": "X",
            "identity_fields": [{"key": 123, "label": "Kode"}],
        }).identity_fields[0]
        self.assertEqual(field.key, "123")
        self.assertIs(field.key_is_text, False)


if __name__ == "__main__":
    unittest.main()