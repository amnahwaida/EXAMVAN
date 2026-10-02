"""Review putaran kedua aplikasi Windows (30 September 2026).

Tiga temuan, semuanya direproduksi dulu sebelum ditulis testnya:

1. Hash admin password yang korup/diedit siswa melempar ValueError
   keluar dari `_password_matches()` — persis di jalur dialog admin
   exit. File `admin_password.txt` ada di profil yang sama dengan akun
   siswa (dilaporkan sendiri di komentar exam_viewer.py), jadi "file
   rusak" adalah input yang HARUS ditolak, bukan crash. Repro:
   `_password_matches("pbkdf2_sha256$abc$QQ==$QQ==", "x")` → ValueError.

2. DoS via iterations tak terbatas: `int(iterations)` dari file dipakai
   apa adanya. Repro terukur: klaim 20.000.000 iterasi = 10,13 detik per
   ketikan supervisor — siswa yang menulis ulang file menggantung
   pengawas ~8 menit di dialog keluar. Ada cap.

3. `_machine_fingerprint()` memakai USERNAME — mengikat hash supervisor
   ke nama akun Windows. Di lab yang akunnya di-rotate tiap semester
   (siswa2026 → siswa2027), password supervisor resmi berhenti bekerja:
   supervisor terkunci di luar ujian. Fingerprint mesin tidak boleh
   turun ke faktor per-pengguna.

Plus: notifikasi Windows — balloon tip menerima teks dari server
(`congrats_message` guru) lewat string PowerShell; smoke test di sini
menjaga escaping kutip + pemilihan ikon + kontrak best-effort.
"""

from __future__ import annotations

import os
import subprocess
import time
import unittest
from unittest import mock

import examvan.ui.exam_viewer as ev
from examvan import notify


def _with_env(**overrides):
    """mock.patch.dict environment dengan kamus penuh yang ditentukan."""
    return mock.patch.dict(os.environ, overrides, clear=False)


class HashAdminPasswordRobustnessTestCase(unittest.TestCase):
    """File admin_password.txt adalah input musuh, bukan input tepercaya."""

    def test_non_numeric_iterations_returns_false_not_raises(self):
        # RED terbukti: ValueError lepas dari _password_matches() —
        # tepat di tangan handler dialog admin exit.
        self.assertIs(
            ev._password_matches("pbkdf2_sha256$abc$QQ==$QQ==", "x"), False
        )

    def test_garbage_base64_returns_false_not_raises(self):
        for broken in (
            "pbkdf2_sha256$120000$!!!$###",   # salt/digest bukan b64
            "pbkdf2_sha256$120000$QQ=",       # struktur tidak lengkap
            "pbkdf2_sha256$120000",           # hanya dua field
        ):
            with self.subTest(stored=broken):
                self.assertIs(ev._password_matches(broken, "x"), False)

    def test_oversized_iterations_are_capped(self):
        # Klaim 20 juta iterasi dulu memakan 10+ detik per panggilan.
        # Setelah cap, verify dengan klaim ses besar harus selesai cepat.
        stored = f"pbkdf2_sha256${ev._ITERATIONS * 167}$QQ==$QQ=="
        start = time.monotonic()
        result = ev._password_matches(stored, "x")
        elapsed = time.monotonic() - start
        self.assertFalse(result)
        self.assertLess(
            elapsed, 2.0,
            f"verify dengan klaim iterations besar memakan {elapsed:.1f}s — "
            "int(iterations) dari file tidak di-cap (DoS supervisor)",
        )

    def test_real_hash_still_verifies_after_the_cap(self):
        # Cap tidak boleh merusak jalur normal: hash yang BENAR (120k)
        # tetap terverifikasi.
        stored = ev._hash_admin_password("rahasia-supervisor")
        self.assertTrue(ev._password_matches(stored, "rahasia-supervisor"))
        self.assertFalse(ev._password_matches(stored, "salah"))

    def test_plaintext_legacy_still_verifies(self):
        # Kompatibilitas instalasi lama tidak boleh rusak oleh fix hash.
        self.assertTrue(ev._password_matches("plaintext-lama", "plaintext-lama"))
        self.assertFalse(ev._password_matches("plaintext-lama", "beda"))


class MachineFingerprintStabilityTestCase(unittest.TestCase):
    """Password supervisor adalah milik PC, bukan milik nama akun."""

    LAB = {
        "COMPUTERNAME": "LAB-PC-01",
        "USERNAME": "siswa2026",
        "PROCESSOR_IDENTIFIER": "AMD64 Family 23",
    }

    def test_hash_survives_a_username_rotation(self):
        # Skenario lapangan: lab TANPA COMPUTERNAME (beberapa build Windows
        # OEM + lingkungan non-domain), akun di-rotate tiap semester.
        # Fingerprint dulu jatuh ke USERNAME, jadi hash terikat ke nama
        # akun — setahun kemudian password supervisor resmi mati.
        # (Dengan COMPUTERNAME terisi, test ini tidak diskriminatif: faktor
        # pertama selalu menang sebelum USERNAME sempat dibaca.)
        with _with_env(**{"USERNAME": "siswa2026"}):
            stored = ev._hash_admin_password("rahasia-supervisor")
        with _with_env(**{"USERNAME": "siswa2027"}):
            self.assertTrue(
                ev._verify_admin_password(stored, "rahasia-supervisor"),
                "ganti nama akun Windows membatalkan hash supervisor — "
                "USERNAME tidak boleh masuk fingerprint mesin",
            )

    def test_username_rotation_does_not_cross_unbind_machines(self):
        # Arah yang tetap harus gagal: dua MESIN berbeda tidak boleh cocok
        # walau nama akunnya sama (menyalin file hash antar PC tetap percuma).
        with _with_env(**{"COMPUTERNAME": "PC-1", "USERNAME": "siswa"}):
            stored = ev._hash_admin_password("rahasia-supervisor")
        with _with_env(**{"COMPUTERNAME": "PC-2", "USERNAME": "siswa"}):
            self.assertFalse(ev._verify_admin_password(stored, "rahasia-supervisor"))

    def test_fingerprint_is_stable_within_a_session(self):
        with _with_env(**self.LAB):
            self.assertEqual(ev._machine_salt(), ev._machine_salt())

    def test_fingerprint_differs_across_machines(self):
        # Arah satunya juga wajib: mesin BERBEDA tidak boleh menghasilkan
        # salt yang sama (menyalin file hash antar PC tetap tidak berguna).
        with _with_env(**self.LAB):
            salt_a = ev._machine_salt()
        with _with_env(**dict(self.LAB, COMPUTERNAME="LAB-PC-02")):
            salt_b = ev._machine_salt()
        self.assertNotEqual(salt_a, salt_b)

    def test_fingerprint_survives_empty_environment(self):
        env = {k: "" for k in self.LAB}
        with _with_env(**env):
            first = ev._machine_salt()
            second = ev._machine_salt()
        self.assertEqual(first, second, "fallback harus deterministik")
        # Dan hash yang dibuat di lingkungan kosong tetap terverifikasi
        # di lingkungan kosong yang sama.
        with _with_env(**env):
            stored = ev._hash_admin_password("pw")
            self.assertTrue(ev._verify_admin_password(stored, "pw"))


class WindowsNotificationSmokeTestCase(unittest.TestCase):
    """Balloon tip membawa teks dari server — kontrak & env dijaga.

    Judul/pesan TIDAK di-interpolasi ke skrip PowerShell (lewat
    environment), jadi tidak ada escaping yang bisa salah — teks guru
    bebas isinya. Ikon dipilih dari urgency, returncode diperiksa.
    """

    def _capture_popen(self, rc=0):
        calls = []

        def fake_popen(args, **kwargs):
            calls.append((args, kwargs))
            proc = mock.Mock()
            proc.communicate.return_value = (b"", b"")
            proc.returncode = rc
            proc.poll.return_value = rc
            return proc

        return calls, fake_popen

    def _send(self, title, message, urgency="normal", rc=0):
        calls, fake_popen = self._capture_popen(rc)
        with mock.patch.object(notify.sys, "platform", "win32"), \
             mock.patch.object(notify.subprocess, "Popen", fake_popen):
            ok = notify.send_notification(title, message, urgency)
        return ok, calls

    def test_server_text_travels_via_environment_not_the_script(self):
        # congrats_message guru bebas isinya — tidak ada lagi string yang
        # perlu di-escape karena tidak masuk ke skrip sama sekali.
        ok, calls = self._send("Judul 'aneh'", "Pesan $(x) `y`")
        self.assertTrue(ok)
        self.assertEqual(len(calls), 1)
        args, kwargs = calls[0]
        script = args[-1]
        self.assertNotIn("Judul", script)
        self.assertNotIn("Pesan", script)
        env = kwargs.get("env", {})
        self.assertEqual(env.get("EXAMVAN_NOTIFY_TITLE"), "Judul 'aneh'")
        self.assertEqual(env.get("EXAMVAN_NOTIFY_BODY"), "Pesan $(x) `y`")

    def test_critical_uses_the_warning_icon(self):
        ok, calls = self._send("Gagal", "Jawaban tetap di disk", "critical")
        self.assertTrue(ok)
        env = calls[0][1].get("env", {})
        self.assertEqual(env.get("EXAMVAN_NOTIFY_ICON"), "Warning")

    def test_normal_uses_the_information_icon(self):
        ok, calls = self._send("Terkumpul", "Sukses")
        self.assertTrue(ok)
        args, kwargs = calls[0]
        script = args[-1]
        self.assertIn("NotifyIcon", script)
        env = kwargs.get("env", {})
        self.assertEqual(env.get("EXAMVAN_NOTIFY_ICON"), "Information")
        self.assertNotEqual(env.get("EXAMVAN_NOTIFY_ICON"), "Warning")

    def test_nonzero_returncode_returns_false(self):
        ok, _ = self._send("Terkumpul", "Sukses", rc=1)
        self.assertFalse(ok)

    def test_spawn_failure_returns_false_not_raises(self):
        # Kontrak best-effort: PowerShell diblokir policy → False,
        # jawaban tetap aman di disk untuk recovery re-entry.
        with mock.patch.object(notify.sys, "platform", "win32"), \
             mock.patch.object(
                 notify.subprocess, "Popen",
                 side_effect=OSError("powershell blocked")):
            self.assertFalse(
                notify.send_notification("Terkumpul", "Sukses")
            )

    def test_linux_missing_helper_returns_false(self):
        with mock.patch.object(notify.shutil, "which", return_value=None):
            self.assertFalse(
                notify.send_notification("Terkumpul", "Sukses")
            )


if __name__ == "__main__":
    unittest.main()
