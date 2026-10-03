"""M6 (MEDIUM) — sidecar pemilik jawaban TIDAK boleh hilang duluan.

Bug
---
`config.clear_answers` menghapus tiga berkas berurutan:

    answers_<id>.dat   -> answers_<id>.json   -> answers_<id>.owner

Docstring fungsi itu sendiri menyatakan bahwa `PermissionError` dari `unlink()`
bukan hipotesis (Defender memindai file tepat setelah ditulis dan memegang
handle beberapa ratus milidetik; `unlink()` di Windows tidak bisa menghapus
file yang sedang dibuka; OneDrive/Dropbox mengunci `%USERPROFILE%\\.config`).

Jadi ketika `answers_<id>.dat` GAGAL dihapus, `.owner` tetap ikut terhapus.
Hasilnya: ada jawaban di disk dengan TIDAK ADA pemilik — dan KEDUA gerbang
konsumennya gagal-TERBUKA saat sidecar tidak ada:

  * `config.resolve_submit_answers` -> `owner is None` berarti "milik siapa
    pun", jadi jawaban orang lain ikut dikirim atas nama pengetik sekarang;
  * `ServerConfigDialog._offer_pending_recovery` -> `owner is None` berarti
    recovery sah, jadi jawaban orang lain ditawarkan sebagai milik siswa yang
    baru saja mengetik identitasnya.

Persis kebocoran lintas siswa yang sidecar itu ada untuk mencegahnya.

Arah yang benar: sidecar yatim TIDAK harmful (tanpa jawaban tidak ada
restore, tidak ada recovery), sedangkan jawaban-tanpa-pemilik ADALAH kebocoran.
Jadi `.owner` hanya boleh dihapus setelah `.dat`/`.json` benar-benar hilang.

Test di bawah memakai fungsi config yang sungguhan (direktori sementara) dan
meniru kondisi Defender: `Path.unlink` untuk `.dat` melempar PermissionError.
"""

from __future__ import annotations

import os
import shutil
import tempfile
import unittest
from pathlib import Path
from unittest import mock

os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")

from examvan import config
from examvan.utils import build_student_key

EXAM_ID = 77

IDENTITY_A = {"nama": "Andi", "nomor_ujian": "N01", "kelas": "9A"}
IDENTITY_B = {"nama": "Budi", "nomor_ujian": "N02", "kelas": "9A"}

TOKEN = "ABCD1234"
KEY_A = build_student_key(IDENTITY_A, TOKEN)
KEY_B = build_student_key(IDENTITY_B, TOKEN)

PAYLOAD = {"1": "A", "2": "B", "3": "C"}


class _Sandbox(unittest.TestCase):
    """config diarahkan ke direktori sementara supaya config.json asli utuh."""

    def setUp(self) -> None:
        self._tmp = Path(tempfile.mkdtemp(prefix="examvan-r7-ownerlast-"))
        self.addCleanup(shutil.rmtree, self._tmp, True)
        for p in (
            mock.patch.object(config, "_CONFIG_DIR", self._tmp),
            mock.patch.object(config, "_CONFIG_FILE", self._tmp / "config.json"),
        ):
            p.start()
            self.addCleanup(p.stop)
        config._cache = None
        self.addCleanup(setattr, config, "_cache", None)

    # -- helper ------------------------------------------------------------

    def _write_answers_with_owner(self, student_key: str = KEY_A) -> None:
        config.save_answers(EXAM_ID, dict(PAYLOAD))
        config.save_answers_owner(EXAM_ID, student_key, "Andi")

    @staticmethod
    def _lock_unlink(suffix: str):
        """`unlink` gagal HANYA untuk akhiran tertentu (iru Defender)."""
        real = Path.unlink

        def _unlink(self, *args, **kwargs):
            if self.name.endswith(suffix):
                raise PermissionError(13, "file is being used by another process")
            return real(self, *args, **kwargs)

        return mock.patch.object(Path, "unlink", _unlink)


class OwnerSurvivesFailedAnswersUnlinkTestCase(_Sandbox):
    """`.owner` hanya boleh hilang kalau jawabannya benar-benar hilang."""

    def test_owner_sidecar_survives_when_dat_cannot_be_removed(self):
        self._write_answers_with_owner()
        dat = self._tmp / f"answers_{EXAM_ID}.dat"
        owner = self._tmp / f"answers_{EXAM_ID}.owner"
        self.assertTrue(dat.exists())
        self.assertTrue(owner.exists())

        with self._lock_unlink(".dat"):
            config.clear_answers(EXAM_ID)

        self.assertTrue(
            dat.exists(),
            "jawaban hilang padahal unlink-nya gagal — simulasi ini tidak "
            "menguji apa pun",
        )
        self.assertIsNotNone(
            config.load_answers_owner(EXAM_ID),
            "sidecar pemilik dihapus padahal answers_<id>.dat masih ada di disk: "
            "jawaban kini TIDAK PUNYA PEMILIK, dan kedua gerbang konsumen "
            "gagal-TERBUKA saat sidecar hilang — ini kebocoran lintas siswa yang "
            "persis harus dicegah. Sidecar yatim tidak berbahaya (tanpa jawaban "
            "tidak ada restore), jawaban tanpa pemilik berbahaya.",
        )
        self.assertEqual(
            config.load_answers_owner(EXAM_ID)["student_key"], KEY_A,
            "isi sidecar berubah, bukan cuma umurnya",
        )

    def test_clear_answers_never_raises_when_unlink_is_denied(self):
        # Jalur ini jalan di statement pertama setelah submit sukses, dari slot
        # Qt: exception apa pun = qFatal() lalu SIGABRT.
        self._write_answers_with_owner()
        with self._lock_unlink(".dat"), self._lock_unlink(".json"):
            config.clear_answers(EXAM_ID)  # tidak boleh melempar

    def test_owner_is_removed_when_the_answers_really_are_gone(self):
        # Penjagaan tidak boleh membekukan sidecar selamanya: kalau `.dat`
        # benar-benar hilang, owner juga dibersihkan supaya re-entry berikutnya
        # tidak membaca owner untuk jawaban yang sudah tidak ada.
        self._write_answers_with_owner()
        config.clear_answers(EXAM_ID)
        self.assertIsNone(config.load_answers(EXAM_ID))
        self.assertIsNone(
            config.load_answers_owner(EXAM_ID),
            "sidecar pemilik tidak ikut dibersihkan setelah jawaban benar-benar "
            "hilang — file yatim menumpuk dan membingungkan recovery berikutnya",
        )

    def test_owner_survives_when_the_legacy_json_cannot_be_removed(self):
        # Jalur migrasi `.json` punya aturan yang sama: kalau `.json` masih
        # ada (dan `load_answers` masih bisa memembacanya), `.owner` wajib
        # ikut bertahan.
        self._tmp.mkdir(parents=True, exist_ok=True)
        (self._tmp / f"answers_{EXAM_ID}.json").write_text(
            '{"1": {"answer": "A"}}', encoding="utf-8"
        )
        config.save_answers_owner(EXAM_ID, KEY_A, "Andi")
        with self._lock_unlink(".json"):
            config.clear_answers(EXAM_ID)
        self.assertTrue((self._tmp / f"answers_{EXAM_ID}.json").exists())
        self.assertIsNotNone(
            config.load_answers_owner(EXAM_ID),
            "sidecar pemilik dihapus padahal berkas jawaban legacy masih ada",
        )


class BothGatesFailClosedTestCase(_Sandbox):
    """Setelah clear yang gagal, kedua gerbang harus perlakukan answers itu
    sebagai milik orang lain."""

    def test_resolve_submit_answers_refuses_another_students_payload(self):
        self._write_answers_with_owner()
        with self._lock_unlink(".dat"):
            config.clear_answers(EXAM_ID)

        self.assertEqual(
            config.load_answers(EXAM_ID), PAYLOAD,
            "jawaban tidak ada di disk — gerbang tidak bisa diuji",
        )
        # Gerbang 1: config.resolve_submit_answers (jalur submit/recovery).
        self.assertEqual(
            config.resolve_submit_answers({}, EXAM_ID, KEY_B), {},
            "resolve_submit_answers mengirim jawaban siswa A atas nama siswa B — "
            "sidecar hilang saat clear gagal, jadi gerbang gagal-terbuka",
        )
        # Pelaku yang SEBENARNYA tetap boleh mengambil jawabannya sendiri.
        self.assertEqual(
            config.resolve_submit_answers({}, EXAM_ID, KEY_A), PAYLOAD,
            "pemilik sah harus tetap bisa mengambil jawabannya sendiri",
        )

    def test_recovery_is_not_offered_to_another_student(self):
        self._write_answers_with_owner()
        with self._lock_unlink(".dat"):
            config.clear_answers(EXAM_ID)

        from PyQt5.QtWidgets import QApplication
        from examvan.models import Exam
        from examvan.ui.server_config import ServerConfigDialog

        app = QApplication.instance() or QApplication([])
        self.assertIsNotNone(app)

        dlg = ServerConfigDialog()
        self.addCleanup(dlg.hide)
        dlg._exam = Exam.from_json({
            "id": EXAM_ID, "name": "Ujian", "status": "active",
            "security_level": "low",
        })
        dlg._validated_token = TOKEN
        # `_show_recovery` (slot produksi) membuka halaman hasil lewat
        # jaringan; yang diuji GERBANGNYA, bukan unduhan.
        try:
            dlg._sig_recovery_available.disconnect(dlg._show_recovery)
        except (TypeError, RuntimeError):
            pass

        offered = []
        dlg._sig_recovery_available.connect(lambda exam: offered.append(exam))
        result = dlg._offer_pending_recovery(IDENTITY_B)

        self.assertFalse(
            offered,
            "recovery jawaban siswa A ditawarkan ke siswa B setelah clear gagal "
            "menghapus sidecar pemilik — jawaban A akan dikirim atas nama B",
        )
        self.assertTrue(result, "recovery milik orang lain tidak boleh memblokir")


if __name__ == "__main__":
    unittest.main()