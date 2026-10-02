"""Pin putaran hardening 2 Okt 2026 (workstream bug-fix EXAMVAN desktop).

Setiap kelas di sini mengunci SATU work item supaya regresi tertangkap di
sisi yang mengubahnya:

  U-A  map_identity_to_standard: exact-first, word-boundary, NUMBER dulu,
       kata kunci negatif (nomor_peserta / exam_date).
  U-B  build_attempt_key: strip + lower bagian identitas, token keep-case.
  U-C  build_result_link: strip query/fragment + quote token.
  U-D  get_mac_address: lewati interface virtual, tangkap
       (OSError, ValueError, UnicodeDecodeError).
  P-A  tidak ada lagi header X-App-Version di request desktop.
  P-B  token di-quote ke path get_exam_by_token.
  P-C  _make_request menolak body >32MB.
  P-D  download_pdf chmod 0600 tmp + dest.
  S-A  reset _reconnect_attempts hanya setelah koneksi stabil >=10 dtk + cap.
  S-B  token WS di-quote ke query URL.
  M-A  IdentityField key/label None -> str.
  M-B  strict_mode string "false"/"0"/... -> False.
  M-C  name str + panel_color allow-list.
  C-A  _sanitize: koersi server_url/exam_token/remember_url/history.
  C-D  resolve_submit_answers(attempt_key): owner beda -> memori/kosong.
  C-E  mark_submitted merge ke RAW (label lama tidak jadi plaintext).
  C-H  exam_token_history cap 3.
  V-B  _load_admin_password: fallback encoding non-UTF8 + migrasi hash.
"""

from __future__ import annotations

import os
import shutil
import tempfile
import unittest
from pathlib import Path
from unittest import mock

from PyQt5.QtCore import QCoreApplication

import examvan.api as api
import examvan.config as config
from examvan.models import Exam, IdentityField
from examvan.utils import (
    build_attempt_key,
    build_result_link,
    get_mac_address,
    map_identity_to_standard,
)
from examvan.ws import ExamWebSocket


# ---------------------------------------------------------------------------
# U-A — anchoring
# ---------------------------------------------------------------------------


class IdentityAnchoringTest(unittest.TestCase):
    def test_nomor_peserta_is_a_number_not_a_name(self):
        # 'nomor_peserta' mengandung kata 'peserta' (grup nama) TAPI juga
        # 'nomor' (grup number); number diproses dulu jadi menang.
        out = map_identity_to_standard(
            {"nomor_peserta": "N01", "nama": "Andi", "kelas": "9A"}
        )
        self.assertEqual(out.get("exam_number"), "N01")
        self.assertEqual(out.get("student_name"), "Andi")

    def test_exam_date_is_never_claimed(self):
        out = map_identity_to_standard(
            {"exam_date": "2026-10-02", "nama": "Andi"}
        )
        self.assertNotIn("2026-10-02", out.values())
        self.assertEqual(out.get("student_name"), "Andi")
        self.assertIsNone(out.get("exam_number"))

    def test_negative_keywords_block_date_time_code_fields(self):
        for key in ("tanggal_lahir", "kode_kelas", "waktu_mulai", "jam_ujian"):
            with self.subTest(key=key):
                out = map_identity_to_standard(
                    {"nama": "Andi", key: "X1", "kelas": "9A"}
                )
                self.assertNotIn("X1", out.values())

    def test_substring_is_not_a_match(self):
        # 'myexam' mengandung substring 'exam' tapi bukan kata 'exam'.
        out = map_identity_to_standard({"myexam": "N01", "nama": "Andi"})
        self.assertNotIn("N01", out.values())

    def test_canonical_keys_win_without_keyword_guessing(self):
        out = map_identity_to_standard(
            {"student_name": "Budi", "exam_number": "N01",
             "student_class": "9A", "nama": "Orang Lain"}
        )
        self.assertEqual(out["student_name"], "Budi")

    def test_empty_values_are_dropped(self):
        out = map_identity_to_standard(
            {"nama": "Budi", "nomor_ujian": "", "kelas": "  "}
        )
        self.assertNotIn("exam_number", out)
        self.assertEqual(out.get("student_name"), "Budi")

    def test_approval_and_submit_agree_on_custom_keys(self):
        # Dialog approval (waiting_approval) dan submit (exam_viewer) memakai
        # fungsi yang sama: kunci kustom harus memberi hasil identik dengan
        # kunci standar.
        from examvan.utils import build_student_key

        custom = {"nama": "Andi", "nomor_ujian": "N01", "kelas": "9A"}
        standard = {"student_name": "Andi", "exam_number": "N01",
                    "student_class": "9A"}
        self.assertEqual(
            map_identity_to_standard(custom),
            map_identity_to_standard(standard),
        )
        self.assertEqual(
            build_student_key(custom, "TOK"), build_student_key(standard, "TOK")
        )


# ---------------------------------------------------------------------------
# U-B / U-C / U-D
# ---------------------------------------------------------------------------


class AttemptKeyNormalizationTest(unittest.TestCase):
    def test_identity_parts_are_stripped_and_lowered(self):
        a = build_attempt_key("TOK", {"nama": "  Andi ", "nomor": " N01 "})
        b = build_attempt_key("TOK", {"nama": "andi", "nomor": "n01"})
        self.assertEqual(a, b)

    def test_token_keeps_case_but_is_stripped(self):
        a = build_attempt_key("  TOKen123  ", {"nama": "Andi"})
        b = build_attempt_key("TOKen123", {"nama": "andi"})
        self.assertEqual(a, b)
        c = build_attempt_key("token123", {"nama": "andi"})
        self.assertNotEqual(a, c)


class ResultLinkTest(unittest.TestCase):
    def test_query_and_fragment_are_stripped(self):
        self.assertEqual(
            build_result_link("https://srv/examvan/?x=1#frag", "ABCD1234"),
            "https://srv/examvan/ABCD1234",
        )

    def test_token_is_percent_encoded(self):
        link = build_result_link("https://srv", "A B/C?d")
        self.assertEqual(link, "https://srv/A%20B%2FC%3Fd")


class MacAddressVirtualSkipTest(unittest.TestCase):
    def _fake_sysfs(self, entries):
        """entries: {iface: mac}. Patch Path.glob di examvan.utils."""

        class _FakePath:
            def __init__(self, iface):
                self.parent = mock.Mock()
                self.parent.name = iface
                self._mac = entries[iface]
                self._iface = iface

            def __lt__(self, other):
                return self._iface < other._iface

            def read_text(self):
                if isinstance(self._mac, Exception):
                    raise self._mac
                return self._mac

        return [_FakePath(iface) for iface in sorted(entries)]

    def test_virtual_interfaces_are_skipped(self):
        from examvan import utils

        fakes = self._fake_sysfs({
            "docker0": "02:42:ac:11:00:02",
            "eth0": "AA:BB:CC:DD:EE:01",
            "lo": "00:00:00:00:00:00",
            "veth123": "02:42:ac:11:00:03",
        })
        with mock.patch.object(utils, "Path") as mp:
            mp.return_value.glob.return_value = fakes
            with mock.patch.object(utils.platform, "system",
                                   return_value="Linux"):
                self.assertEqual(get_mac_address(), "AA:BB:CC:DD:EE:01")

    def test_decode_error_falls_back_to_uuid(self):
        from examvan import utils

        fakes = self._fake_sysfs({"eth0": UnicodeDecodeError("u", b"x", 0, 1, "r")})
        with mock.patch.object(utils, "Path") as mp:
            mp.return_value.glob.return_value = fakes
            with mock.patch.object(utils.platform, "system",
                                   return_value="Linux"):
                mac = get_mac_address()
        self.assertRegex(mac, r"^([0-9A-F]{2}:){5}[0-9A-F]{2}$")


# ---------------------------------------------------------------------------
# P-A / P-B / P-C / P-D
# ---------------------------------------------------------------------------


class NoAppVersionHeaderTest(unittest.TestCase):
    def _capture(self, fn, *args, **kwargs):
        seen = {}

        def fake(url, method="GET", headers=None, body=None, timeout=30):
            seen["headers"] = dict(headers or {})
            seen["url"] = url
            return {"success": True, "exam": {"id": 1, "name": "X",
                                             "status": "active"}}

        with mock.patch.object(api, "_make_request", side_effect=fake):
            try:
                fn(*args, **kwargs)
            except Exception:
                pass
        return seen

    def test_join_sends_no_app_version(self):
        seen = self._capture(api.get_exam_by_token, "https://srv", "T")
        self.assertNotIn("X-App-Version", seen.get("headers", {}))

    def test_submit_sends_no_app_version(self):
        seen = self._capture(
            api.submit_exam, "https://srv", 1, "A", "N", "C", {}, "t",
            "M", {"nama": "A"}, "T")
        self.assertNotIn("X-App-Version", seen.get("headers", {}))

    def test_access_log_sends_no_app_version(self):
        seen = self._capture(
            api.send_access_log, "https://srv", 1, "T", "M", "heartbeat")
        self.assertNotIn("X-App-Version", seen.get("headers", {}))

    def test_complete_sends_no_app_version(self):
        seen = self._capture(api.complete_exam, "https://srv", 1, "T", "M")
        self.assertNotIn("X-App-Version", seen.get("headers", {}))

    def test_token_is_percent_encoded_in_path(self):
        seen = self._capture(api.get_exam_by_token, "https://srv", "A B/C")
        self.assertIn("A%20B%2FC", seen.get("url", ""))


class MakeRequestSizeCapTest(unittest.TestCase):
    def _opener_with(self, body: bytes, headers=None):
        class _Resp:
            def __init__(self):
                self.headers = headers or {}

            def read(self, size=-1):
                return body[:size] if size and size >= 0 else body

            def __enter__(self):
                return self

            def __exit__(self, *exc):
                return False

        opener = mock.MagicMock()
        opener.open.return_value = _Resp()
        return mock.patch.object(api, "_pdf_opener", return_value=opener)

    def test_declared_huge_body_is_rejected(self):
        with self._opener_with(
            b'{"success": true}',
            {"Content-Length": str(33 * 1024 * 1024)},
        ):
            out = api._make_request("https://srv/x")
        self.assertFalse(out.get("success"))
        self.assertEqual(out.get("error_code"), "non_json_response")

    def test_undeclared_huge_body_is_rejected(self):
        with self._opener_with(b"x" * (32 * 1024 * 1024 + 5)):
            out = api._make_request("https://srv/x")
        self.assertFalse(out.get("success"))
        self.assertEqual(out.get("error_code"), "non_json_response")

    def test_normal_body_still_parses(self):
        with self._opener_with(b'{"success": true}'):
            out = api._make_request("https://srv/x")
        self.assertTrue(out.get("success"))


class DownloadPdfChmodTest(unittest.TestCase):
    def test_tmp_and_dest_are_0600(self):
        import examvan.api as api_mod

        tmp = tempfile.mkdtemp(prefix="examvan-pdf-")
        self.addCleanup(shutil.rmtree, tmp, True)
        dest = os.path.join(tmp, "exam.pdf")
        pdf = (b"%PDF-1.4\n" + b"0" * 100)

        class _Resp:
            headers = {}

            def read(self, size=-1):
                return b""  # EOF langsung setelah head sudah ditulis

            def __enter__(self):
                return self

            def __exit__(self, *exc):
                return False

        # Simulasikan opener yang menulis satu chunk PDF lalu EOF.
        chunks = [pdf]

        class _Resp2(_Resp):
            def read(self, size=-1):
                return chunks.pop(0) if chunks else b""

        opener = mock.MagicMock()
        opener.open.return_value = _Resp2()
        with mock.patch.object(api_mod, "_pdf_opener", return_value=opener):
            got = api_mod.download_pdf("https://srv", 7, "T", dest)
        self.assertEqual(got, dest)
        if os.name != "nt":
            mode = os.stat(dest).st_mode & 0o777
            self.assertEqual(mode, 0o600)


# ---------------------------------------------------------------------------
# S-A / S-B
# ---------------------------------------------------------------------------


class WsStableReconnectTest(unittest.TestCase):
    def setUp(self):
        if QCoreApplication.instance() is None:
            self._app = QCoreApplication([])
        self.ws = ExamWebSocket()
        self.ws._should_reconnect = True
        self.addCleanup(self.ws.disconnect)

    def test_flapping_does_not_reset_the_counter(self):
        import time as _time

        self.ws._reconnect_attempts = 5
        with mock.patch.object(_time, "monotonic", return_value=1000.0):
            self.ws._on_connected()
        with mock.patch.object(_time, "monotonic", return_value=1002.0):
            with mock.patch.object(self.ws, "_schedule_reconnect"):
                self.ws._on_disconnected()
        self.assertEqual(self.ws._reconnect_attempts, 5)

    def test_stable_connection_resets_the_counter(self):
        import time as _time

        self.ws._reconnect_attempts = 5
        with mock.patch.object(_time, "monotonic", return_value=1000.0):
            self.ws._on_connected()
        with mock.patch.object(_time, "monotonic", return_value=1015.0):
            with mock.patch.object(self.ws, "_schedule_reconnect"):
                self.ws._on_disconnected()
        self.assertEqual(self.ws._reconnect_attempts, 0)

    def test_counter_is_capped(self):
        self.ws._reconnect_attempts = 100
        with mock.patch.object(self.ws._reconnect_timer, "start"):
            self.ws._schedule_reconnect()
        self.assertLessEqual(self.ws._reconnect_attempts, 12)

    def test_token_is_quoted_in_ws_url(self):
        from PyQt5.QtWebSockets import QWebSocket

        self.ws._base_url = "https://srv.example"
        self.ws._exam_id = 7
        self.ws._token = "A B/C"
        self.ws._should_reconnect = True
        with mock.patch.object(QWebSocket, "open") as _open:
            self.ws._do_connect()
            # toEncoded: bentuk yang benar-benar dikirim ke server (QUrl
            # menormalkan tampilan toString, mis. spasi tampil literal).
            url = bytes(
                _open.call_args.args[0].toEncoded()).decode("ascii")
        self.assertIn("token=A%20B%2FC", url)


# ---------------------------------------------------------------------------
# M-A / M-B / M-C
# ---------------------------------------------------------------------------


class IdentityFieldNullTest(unittest.TestCase):
    def test_none_key_and_label_become_empty_strings(self):
        exam = Exam.from_json({"id": 1, "name": "X", "status": "a",
                               "identity_fields": [
                                   {"key": None, "label": None},
                                   {"key": "nis", "label": None},
                                   "bukan-dict",
                               ]})
        self.assertEqual(exam.identity_fields[0].key, "")
        self.assertEqual(exam.identity_fields[0].label, "")
        self.assertEqual(exam.identity_fields[1].key, "nis")
        self.assertEqual(exam.identity_fields[1].label, "nis")
        self.assertEqual(len(exam.identity_fields), 2)


class StrictModeCoercionTest(unittest.TestCase):
    def _strict(self, v):
        return Exam.from_json({"id": 1, "name": "X", "status": "a",
                               "strict_mode": v}).strict_mode

    def test_falsy_strings_are_false(self):
        for v in ("false", "False", " FALSE ", "0", "", "no", "off", "NO"):
            with self.subTest(v=v):
                self.assertFalse(self._strict(v))

    def test_truthy_strings_are_true(self):
        for v in ("1", "true", "True", " yes ", "on"):
            with self.subTest(v=v):
                self.assertTrue(self._strict(v))

    def test_non_strings_keep_bool(self):
        self.assertTrue(self._strict(True))
        self.assertFalse(self._strict(False))
        self.assertTrue(self._strict(1))
        self.assertFalse(self._strict(0))


class PanelColorAndNameTest(unittest.TestCase):
    def test_name_is_always_str(self):
        exam = Exam.from_json({"id": 1, "name": None, "status": "a"})
        self.assertEqual(exam.name, "")

    def test_valid_colors_pass(self):
        for color in ("#6366f1", "#fff", "#ABC"):
            with self.subTest(color=color):
                exam = Exam.from_json({"id": 1, "name": "X", "status": "a",
                                       "panel_color": color})
                self.assertEqual(exam.panel_color, color)

    def test_invalid_colors_fall_back(self):
        for color in ("red", "#12", "#gggggg", "", None, "javascript:1"):
            with self.subTest(color=color):
                exam = Exam.from_json({"id": 1, "name": "X", "status": "a",
                                       "panel_color": color})
                self.assertEqual(exam.panel_color, "#6366f1")


# ---------------------------------------------------------------------------
# C-A / C-D / C-E / C-H
# ---------------------------------------------------------------------------


class SanitizeCoercionTest(unittest.TestCase):
    def test_untrusted_types_are_coerced(self):
        out = config._sanitize({
            "server_url": None, "exam_token": 12345,
            "remember_url": "yes", "exam_token_history": [1, None, "a"],
            "identity_data": {},
        })
        self.assertEqual(out["server_url"], "")
        self.assertEqual(out["exam_token"], "12345")
        self.assertIs(out["remember_url"], True)
        self.assertEqual(out["exam_token_history"], ["1", "None", "a"])

    def test_remember_url_truthy_strings(self):
        for v in ("1", "true", "yes", "on", " YES "):
            with self.subTest(v=v):
                out = config._sanitize({"remember_url": v,
                                        "identity_data": {}})
                self.assertIs(out["remember_url"], True)

    def test_remember_url_other_values_are_false(self):
        for v in ("0", "false", "no", "", None, 0, 123):
            with self.subTest(v=v):
                out = config._sanitize({"remember_url": v,
                                        "identity_data": {}})
                self.assertIs(out["remember_url"], False)

    def test_non_list_history_becomes_empty(self):
        out = config._sanitize({"exam_token_history": "tok",
                                "identity_data": {}})
        self.assertEqual(out["exam_token_history"], [])


class _ConfigTmpTestCase(unittest.TestCase):
    def setUp(self):
        self._tmp = tempfile.mkdtemp(prefix="examvan-r4-")
        self._dir_patch = mock.patch.object(config, "_CONFIG_DIR",
                                            Path(self._tmp))
        self._file_patch = mock.patch.object(config, "_CONFIG_FILE",
                                              Path(self._tmp) / "config.json")
        self._dir_patch.start()
        self._file_patch.start()
        config._cache = None

    def tearDown(self):
        self._dir_patch.stop()
        self._file_patch.stop()
        config._cache = None
        shutil.rmtree(self._tmp, ignore_errors=True)


class ResolveOwnerGateTest(_ConfigTmpTestCase):
    def test_other_owners_disk_answers_are_not_returned(self):
        config.save_answers(42, {"1": "MILIK-A"})
        config.save_answers_owner(42, "kunci-A", label="exam_number=a")
        got = config.resolve_submit_answers({}, 42, attempt_key="kunci-B")
        self.assertEqual(got, {})

    def test_same_owner_gets_the_disk_copy(self):
        config.save_answers(42, {"1": "MILIK-A"})
        config.save_answers_owner(42, "kunci-A", label="exam_number=a")
        got = config.resolve_submit_answers({}, 42, attempt_key="kunci-A")
        self.assertEqual(got, {"1": "MILIK-A"})

    def test_no_sidecar_still_falls_back(self):
        config.save_answers(42, {"1": "LAMA"})
        got = config.resolve_submit_answers({}, 42, attempt_key="kunci-B")
        self.assertEqual(got, {"1": "LAMA"})

    def test_memory_wins_when_nonempty(self):
        config.save_answers(42, {"1": "LAMA"})
        got = config.resolve_submit_answers({"1": "BARU"}, 42,
                                            attempt_key="kunci-B")
        self.assertEqual(got, {"1": "BARU"})


class MarkSubmittedRawMergeTest(_ConfigTmpTestCase):
    def test_second_mark_does_not_plaintext_the_first_label(self):
        config.mark_submitted(42, "kunci-A", label="exam_number=a")
        raw_before = dict(config._submitted_raw(42))
        config.mark_submitted(42, "kunci-B", label="exam_number=b")
        raw_after = dict(config._submitted_raw(42))
        # Label pertama tidak berubah (tetap ter-encode, bukan plaintext).
        key_a = config._submitted_key("kunci-A")
        self.assertEqual(raw_after[key_a], raw_before[key_a])
        self.assertNotEqual(raw_after[key_a], "exam_number=a")
        # Display tetap bisa membaca keduanya.
        self.assertEqual(sorted(config.submitted_labels(42)),
                         ["exam_number=a", "exam_number=b"])


class HistoryCapTest(_ConfigTmpTestCase):
    def test_history_keeps_at_most_three(self):
        config.set("exam_token", "T1")
        for tok in ("T2", "T3", "T4", "T5"):
            config.set("exam_token", tok)
        hist = config.get("exam_token_history") or []
        self.assertLessEqual(len(hist), 3)


# ---------------------------------------------------------------------------
# V-B — admin password fallback + migrasi
# ---------------------------------------------------------------------------


class AdminPasswordFallbackMigrationTest(unittest.TestCase):
    """Panggil _load_admin_password langsung (bukan reload modul).

    Helper reload (import + reload = eksekusi ganda) akan mengeksekusi
    _load dua kali: eksekusi pertama memigrasi plaintext -> hash, eksekusi
    kedua membaca hash itu — nilai yang diamati jadi hash, bukan perilaku
    produksi (satu import). Pemanggilan fungsi langsung deterministik.
    """

    def setUp(self):
        self._tmp = tempfile.mkdtemp(prefix="examvan-adminpw-r4-")
        self._local = Path(self._tmp)
        self._pw_file = self._local / "EXAMVAN" / "admin_password.txt"
        from examvan.ui import exam_viewer as ev

        self._ev = ev

    def tearDown(self):
        shutil.rmtree(self._tmp, ignore_errors=True)

    def _load(self):
        env = {"EXAMVAN_ADMIN_PASSWORD": "",
               "LOCALAPPDATA": str(self._local)}
        with mock.patch.dict(os.environ, env):
            return self._ev._load_admin_password()

    def test_non_utf8_file_falls_back_to_local_encoding(self):
        # Berkas ANSI (latin-1) yang bukan UTF-8 valid — mis. 'é' tunggal.
        self._pw_file.parent.mkdir(parents=True, exist_ok=True)
        self._pw_file.write_bytes("supervisi\xe9\n".encode("latin-1"))
        with mock.patch("locale.getpreferredencoding",
                        return_value="latin-1"):
            pw = self._load()
        # Tidak crash; password terbaca via fallback.
        self.assertEqual(pw, "supervisi\xe9")

    def test_unreadable_encoding_is_fail_closed(self):
        self._pw_file.parent.mkdir(parents=True, exist_ok=True)
        self._pw_file.write_bytes("supervisi\xe9\n".encode("latin-1"))
        with mock.patch("locale.getpreferredencoding",
                        return_value="ascii"):
            pw = self._load()
        self.assertIsNone(pw)

    def test_plaintext_file_is_migrated_to_hash(self):
        self._pw_file.parent.mkdir(parents=True, exist_ok=True)
        self._pw_file.write_text("rahasia123", encoding="utf-8")
        pw = self._load()
        # Nilai yang dipakai tetap plaintext (kompatibel), tapi berkas
        # ditulis ulang sebagai hash.
        self.assertEqual(pw, "rahasia123")
        stored = self._pw_file.read_text(encoding="utf-8").strip()
        self.assertTrue(stored.startswith(self._ev._HASH_PREFIX),
                        f"berkas tidak termigrasi: {stored[:20]!r}")
        # Hash hasil migrasi memverifikasi password aslinya.
        self.assertTrue(
            self._ev._verify_admin_password(stored, "rahasia123"))
        self.assertFalse(
            self._ev._verify_admin_password(stored, "salah"))


if __name__ == "__main__":
    unittest.main()
