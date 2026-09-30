"""Password admin exit tidak boleh plainly readable dan easily replaceable (#10).

Batas jujur yang harus dinyatakan terbuka
-----------------------------------
Di lab sekolah, semua siswa sering memakai SATU akun Windows. Pada
situasi itu tidak ada rahasia lokal yang bisa dilindungi dari siswa:
siapa pun yang memiliki file tersebut bisa menulis ulang isinya.
`icacls` dari installer tidak menolong -- pemilik file SELALU dapat
memberikan akses pada dirinya sendiri, dan `PrivilegesRequired=lowest`
berarti installer tidak punya hak untuk mengubah ACL secara bermakna.

Jadi ini bukan "menutup" celah; ini menaikkan palang dari "buka
Notepad, ketik apa saja" menjadi "harus memahami formatnya", DAN
mendeteksi penyuntingan sehingga percobaan login
tercatat.

Yang dikerjakan test ini
------------------------
1. Password tidak disimpan plaintext. Berkas berisi
   PBKDF2-HMAC-SHA256 dengan salt acak.
2. Isi berkas diverifikasi terhadap machine-binding, jadi menyalin
   `admin_password.txt` dari PC lain tidak langsung berhasil.
3. Password yang sudah ada dalam format plaintext LAMA tetap diterima
   -- kalau tidak, setiap upgrade akan mengunci supervisor di luar
   kelas saat ujian berjalan.
"""

from __future__ import annotations

import os
import pathlib
import tempfile
import unittest
from unittest import mock

from examvan.ui import exam_viewer as ev


class AdminPasswordIsNotStoredInPlaintextTestCase(unittest.TestCase):
    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        # App membaca {LOCALAPPDATA}\EXAMVAN\admin_password.txt -- subfolder
        # EXAMVAN wajib ada, bukan hanya LOCALAPPDATA.
        self.pw_dir = pathlib.Path(self._tmp.name) / "EXAMVAN"
        self.pw_dir.mkdir()
        self.pw_file = self.pw_dir / "admin_password.txt"
        patcher = mock.patch.dict(
            os.environ, {"LOCALAPPDATA": self._tmp.name}, clear=False
        )
        patcher.start()
        self.addCleanup(patcher.stop)
        os.environ.pop("EXAMVAN_ADMIN_PASSWORD", None)

    def test_the_environment_variable_still_wins(self):
        os.environ["EXAMVAN_ADMIN_PASSWORD"] = "dari-env"
        self.addCleanup(os.environ.pop, "EXAMVAN_ADMIN_PASSWORD", None)
        self.assertEqual(ev._load_admin_password(), "dari-env")

    def test_a_new_password_can_be_hashed_and_verified(self):
        stored = ev._hash_admin_password("rahasia-supervisor")
        self.assertTrue(stored)
        self.assertNotIn(
            "rahasia-supervisor", stored,
            "password masih tersimpan plaintext di file",
        )
        self.assertTrue(ev._verify_admin_password(stored, "rahasia-supervisor"))
        self.assertFalse(ev._verify_admin_password(stored, "salah"))

    def test_a_written_hash_verifies_on_reload(self):
        # `_load_admin_password()` mengembalikan bentuk yang TERSIMPAN
        # (hash untuk format baru), bukan plaintextnya -- plaintextnya
        # memang sudah tidak ada di mana pun. Pencocokan dilakukan
        # `_password_matches`.
        self.pw_file.write_text(
            ev._hash_admin_password("rahasia-supervisor"), encoding="utf-8"
        )
        stored = ev._load_admin_password()
        self.assertTrue(stored.startswith(ev._HASH_PREFIX))
        self.assertNotIn("rahasia-supervisor", stored)
        self.assertTrue(ev._password_matches(stored, "rahasia-supervisor"))
        self.assertFalse(ev._password_matches(stored, "salah"))

    def test_the_plaintext_password_itself_is_not_what_gets_written(self):
        ev._store_admin_password("rahasia-supervisor")
        raw = self.pw_file.read_text(encoding="utf-8")
        self.assertNotIn("rahasia-supervisor", raw)

    def test_a_legacy_plaintext_file_still_works(self):
        # Upgrade tidak boleh mengunci supervisor di luar kelas saat ujian
        # sedang berjalan.
        self.pw_file.write_text("rahasia-lama", encoding="utf-8")
        self.assertEqual(ev._load_admin_password(), "rahasia-lama")

    def test_an_empty_file_means_no_password_configured(self):
        self.pw_file.write_text("", encoding="utf-8")
        self.assertIsNone(ev._load_admin_password())

    def test_a_missing_file_means_no_password_configured(self):
        self.assertIsNone(ev._load_admin_password())

    def test_a_hash_from_another_machine_is_rejected(self):
        # Machine-binding: menyalin file dari PC lain tidak langsung
        # berhasil, karena frappeSid-nya ikut terikat.
        stored = ev._hash_admin_password("rahasia")
        with mock.patch.object(ev, "_machine_fingerprint",
                               return_value="GUID-BAWAH-YANG-BERBEDA"):
            self.assertFalse(ev._verify_admin_password(stored, "rahasia"))
        self.assertTrue(ev._verify_admin_password(stored, "rahasia"))

    def test_a_tampered_hash_does_not_verify(self):
        stored = ev._hash_admin_password("rahasia")
        broken = stored[:-4] + "0000"
        self.assertFalse(
            ev._verify_admin_password(broken, "rahasia"),
            "hash yang diubah isinya tetap dianggap valid",
        )

    def test_two_machines_get_different_salts(self):
        a = ev._hash_admin_password("rahasia")
        b = ev._hash_admin_password("rahasia")
        self.assertNotEqual(
            a.split("$")[-2], b.split("$")[-2],
            "salt sama untuk dua mesin; hash bisa dibandingkan langsung",
        )


if __name__ == "__main__":
    unittest.main()