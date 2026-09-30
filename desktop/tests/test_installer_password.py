"""Installer Windows: password admin exit tidak boleh hilang diam-diam.

Temuan R1 (CRITICAL) dari review_windows_2026-09-29.md Bagian 1, masih
terbuka sampai ronde ini. Lihat juga review_windows_2026-09-30.md Bagian 8.

Bug aslinya di `windows/installer/examvan.iss` (`NextButtonClick`):

    if FileExists(PwFile) then
      DeleteFile(PwFile);          // tanpa syarat

    if PwValue <> '' then
      SaveStringToFile(PwValue, PwFile, False);   // bersyarat

Hapus TANPA syarat, tulis BERSYARAT. Dua kondisi membuat `PwValue` kosong,
dan keduanya tanpa warning apa pun:

  1. Instalasi senyap. `/VERYSILENT` tidak pernah menampilkan halaman
     password, jadi `AdminPasswordPage.Values[0]` tetap `''`. File dihapus,
     tidak ada yang ditulis, exit code 0.
  2. Upgrade interaktif yang diklik "Next" saja. Teks halamannya sendiri
     menulis "Boleh dikosongkan" dan "Bisa diubah kapan saja dengan install
     ulang" — yang tersirat tersirat adalah "tidak berubah". Yang terjadi
     sebenarnya adalah HAPUS.

Dampaknya (`desktop/examvan/ui/exam_viewer.py`):

    _ADMIN_PASSWORD = None  ->  "Admin exit tidak dikonfigurasi"

Menurut desain fail-closed proyek sendiri, supervisor tidak bisa lagi menutup
ujian yang sedang berjalan. Tidak ada pesan, tidak ada baris log, tidak ada
exit code non-nol. Dan ini persis skenario yang jadi alasan fitur ini ada:
mend deploying build baru ke seluruh ruang kelas. Setiap `/VERYSILENT` membuat
seluruh lab kehilangan password.

Kenapa file `.iss` diuji dari Python
-----------------------------------
Tidak ada unit test untuk Inno Setup, tapi bagian `[Code]` adalah Pascal
yang bisa dibaca. Test di bawah menguji STRUKTUR prosedurnya — bukan
sekadar string — supaya regression "hapus tanpa syarat" kembali lagi akan
gagal, dan supaya CI bisa menjaganya tanpa host Windows.

`DeleteFile` sebelum tulis memang dibutuhkan: `SaveStringToFile` membuka
file tanpa truncate, jadi password lama 20 karakter diganti yang 8 menyisakan
12 byte lama di akhir file sehingga password baru ikut salah baca. Yang salah
adalah hapus yang tidak diikat pada kondisi menulis.
"""

from __future__ import annotations

import re
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]
ISS = REPO / "windows/installer/examvan.iss"
README = REPO / "windows/README.md"
VIEWER = REPO / "desktop/examvan/ui/exam_viewer.py"


def _strip_pascal_comments(text: str) -> str:
    """Buang komentar `//`, sisakan kode saja.

    `{...}` TIDAK disentuh: di Inno Setup itu bentuk KONSTANTA
    (`{localappdata}`), dan menghapusnya membuat analisis salah baca.
    Komentar `{ ... }` yang asli di file ini selalu berupa prosa berbaris
    banyak, dan struktur barisnya tidak boleh ikut berubah — makanya
    penggantiannya mempertahankan panjang.
    """
    out = []
    for line in text.splitlines(keepends=True):
        cut = line.find("//")
        if cut >= 0:
            line = line[:cut] + " " * (len(line) - cut)
        out.append(line)
    return "".join(out)


def _procedure_lines(name: str) -> list:
    """Baris kode tubuh procedure/function, dari header sampai `end;`-nya.

    Bekerja per baris pada teks yang komentarnya sudah dibuang, dan menghitung
    `begin`/`case`/`try` vs `end` — Delphi memakai satu `end` untuk semuanya,
    jadi berhenti di `end;` paling dalam akan salah.
    """
    code = _strip_pascal_comments(ISS.read_text(encoding="utf-8")).splitlines()
    start_idx = None
    for i, line in enumerate(code):
        if re.match(
            rf"^[ \t]*(?:procedure|function)[ \t]+{re.escape(name)}\b", line,
            re.IGNORECASE,
        ) and line.rstrip().endswith(";"):
            start_idx = i
            break
    if start_idx is None:
        raise AssertionError(f"procedure/function {name} not found in {ISS}")

    # Lewati deklarasi var/const/type hingga `begin` pertama.
    i = start_idx + 1
    while i < len(code) and not re.match(r"^\s*begin\b", code[i], re.IGNORECASE):
        i += 1
    if i >= len(code):
        raise AssertionError(f"no `begin` in {name}()")

    depth = 1
    collected = [code[i].strip()]
    i += 1
    while i < len(code) and depth > 0:
        line = code[i]
        for m in re.finditer(r"\b(begin|case|try|end)\b", line, re.IGNORECASE):
            depth += 1 if m.group(1).lower() != "end" else -1
            if depth == 0:
                return collected
        collected.append(line.strip())
        i += 1
    raise AssertionError(f"unterminated {name}()")


def _procedure_body(name: str) -> str:
    """`_procedure_lines` sebagai satu string, untuk assertion substring."""
    return "\n".join(_procedure_lines(name))


def _guard_chain(body: str, statement: str) -> list:
    """Semua kondisi `if` yang BENAR-BENAR MENGAWANI `statement`.

    Delphi punya dua bentuk: `if C then <stmt>` (satu baris, tanpa begin) dan
    `if C then begin ... end;`. Keduanya dihitung. Yang sudah tertutup
    beforehand TIDAK dihitung — `if ... then begin ... end;` yang selesai 20
    baris lalu posisi statement adalah blok cousin, bukan penjaga.

    Rantai inilah yang diuji: bug aslinya menaruh DeleteFile di luar rantai
    `PwValue <> ''` sama sekali, bukan di rantai yang salah.
    """
    idx = body.index(statement)
    before = body[:idx]
    chain = []

    for m in re.finditer(
        r"^[ \t]*(if\b[^\n]*?\bthen)(?!\s*begin\b)[ \t]*$",
        before, re.MULTILINE | re.IGNORECASE,
    ):
        chain.append((m.end(), m.group(1).split(" then")[0].strip()))

    for m in re.finditer(
        r"^[ \t]*(if\b[^\n]*?\bthen)[ \t]*$", before, re.MULTILINE | re.IGNORECASE
    ):
        tail = before[m.end():]
        if not re.match(r"\s*begin\b", tail):
            continue
        if _net_block_depth(tail) > 0:
            chain.append((m.end(), m.group(1).split(" then")[0].strip()))

    return [cond for _, cond in sorted(chain, reverse=True)]


def _net_block_depth(text: str) -> int:
    """`begin`/`case`/`try` dikurangi `end`, dihitung per token."""
    depth = 0
    for m in re.finditer(r"\b(begin|case|try|end)\b", text, re.IGNORECASE):
        depth += 1 if m.group(1).lower() != "end" else -1
    return depth


class PasswordWriteBranchTest(unittest.TestCase):
    """Hapus dan tulis harus berada di cabang yang sama."""

    def setUp(self):
        self.body = _procedure_body("CurStepChanged")

    def test_procedure_exists(self):
        self.assertIn("PwFile", self.body)

    def test_delete_is_guarded_by_a_non_empty_password(self):
        # Regression R1: `if FileExists(PwFile) then DeleteFile(PwFile);`
        # berada di luar semua kondisi, sementara tulisnya bersyarat. Dua
        # kondisi membuat password lama hilang tanpa jejak:
        #   * /VERYSILENT -> halaman password tidak pernah tampil
        #   * upgrade interaktif yang hanya diklik "Next"
        chain = _guard_chain(self.body, "DeleteFile(PwFile)")
        self.assertTrue(chain, "DeleteFile(PwFile) tidak dibungkus kondisi apa pun")
        self.assertTrue(
            any(re.search(r"PwValue\s*<>\s*''", g) for g in chain),
            f"hapus tidak terikat pada password yang tidak kosong: {chain}",
        )

    def test_delete_and_save_share_one_guard(self):
        delete_guards = [g for g in _guard_chain(self.body, "DeleteFile(PwFile)")
                         if "PwValue" in g]
        save_guards = [g for g in _guard_chain(self.body, "SaveStringToFile")
                       if "PwValue" in g]
        self.assertEqual(
            delete_guards, save_guards,
            "hapus dan tulis harus dijaga kondisi yang sama",
        )

    def test_delete_precedes_save(self):
        # Wajib: SaveStringToFile tidak men-truncate, jadi file lama harus
        # dihapus lebih dulu agar sisa byte lama tidak ikut terbaca.
        self.assertLess(
            self.body.index("DeleteFile(PwFile)"), self.body.index("SaveStringToFile")
        )

    def test_every_delete_path_is_guarded(self):
        # Sekarang ada DUA jalur hapus yang sah:
        #   1. cabang tulis  -- huluanya, karena SaveStringToFile tidak
        #      men-truncate, jadi file lama dihapus dulu sebelum ditulis
        #   2. cabang interaktif + kolom dikosongkan -- supervisor yang
        #      sengaja menonaktifkan password exit
        #
        # Assertion lama ("hapus harus muncul sekali") dibuat ketika hanya
        # ada jalur pertama; begitu jalur kedua ditambahkan untuk
        # memperbaiki "password tidak bisa dinonaktifkan", assertion itu
        # sendiri jadi rusak. Yang diuji sekarang bukan jumlahnya tapi
        # setiap jalur hapus memang di dalam kondisi.
        # Dua kemunculan, dua jalur yang memang sah. Yang mana
        # harus dijaga; `_guard_chain` mengembalikan seluruh rantai kondisi
        # di procedure, bukan satu per kemunculan, jadi tidak bisa dipakai
        # untuk mencocokkan jumlah. Yang dicek di sini: kedua jalur punya
        # penjaga, dan penjaga interaktifnya WizardSilent.
        self.assertEqual(
            self.body.count("DeleteFile(PwFile)"), 2,
            "harus ada tepat dua jalur hapus: pra-tulis (truncate) dan "
            "interaktif + dikosongkan",
        )
        guards = _guard_chain(self.body, "DeleteFile(PwFile)")
        self.assertTrue(
            any("PwValue" in g for g in guards),
            f"tidak ada jalur hapus yang terikat pada PwValue: {guards}",
        )
        self.assertTrue(
            any("WizardSilent" in g for g in guards),
            "cabang hapus interaktif harus dijaga WizardSilent, kalau tidak "
            f"setiap upgrade senyap menghapus password: {guards}",
        )

    def test_save_is_guarded_too(self):
        chain = _guard_chain(self.body, "SaveStringToFile")
        self.assertTrue(
            any(re.search(r"PwValue\s*<>\s*''", g) for g in chain), chain
        )


class SilentInstallTest(unittest.TestCase):
    """`/VERYSILENT` tidak boleh menghapus password lama."""

    def test_empty_page_value_falls_back_to_the_stored_password(self):
        body = _procedure_body("CurStepChanged")
        # Values[0] kosong -> muat password yang sudah ada. Tanpa ini,
        # instalasi senyap punya satu jalur: hapus.
        self.assertIn("ReadPasswordFromFile(PwFile)", body)
        self.assertRegex(
            body, r"AdminPasswordPage\.Values\[0\]\s*:=",
            "password lama tidak dimuat kembali",
        )

    def test_reader_helper_exists(self):
        text = ISS.read_text(encoding="utf-8")
        self.assertIn("function ReadPasswordFromFile", text)
        # Aman untuk file kosong / tidak bisa dibaca.
        reader = _procedure_body("ReadPasswordFromFile")
        self.assertIn("FileExists", reader)
        self.assertIn("Trim", reader)

    def test_no_unguarded_delete_anywhere_in_the_installer(self):
        for name in ("CurStepChanged",):
            body = _procedure_body(name)
            for m in re.finditer(r"^[ \t]*DeleteFile\(", body, re.MULTILINE):
                chain = _guard_chain(body, m.group(0))
                self.assertTrue(
                    chain, f"DeleteFile tak terjaga di {name}: {m.group(0)!r}"
                )


class UserFacingInstructionsTest(unittest.TestCase):
    """R2: petunjuk yang menunjuk ke jalan buntu harus hilang.

    Dialog aplikasi dan README sama-sama menyuruh user "centang Konfigurasi
    password admin exit" — checkbox itu sudah DIHAPUS dari `[Tasks]`. Guru
    yang baru saja terkunci di luar ujian diberi tahu untuk membuka installer
    dan mencentang sesuatu yang tidak ada di halaman mana pun.
    """

    def test_no_removed_checkbox_is_referenced_in_the_app(self):
        src = VIEWER.read_text(encoding="utf-8")
        # Only the guidance string matters; a historical note is fine.
        self.assertNotIn('centang \\"Konfigurasi password admin exit', src)
        self.assertNotIn("Konfigurasi password admin exit", src)

    def test_no_removed_checkbox_is_referenced_in_the_readme(self):
        text = README.read_text(encoding="utf-8")
        self.assertNotIn("Konfigurasi password admin exit", text)

    def test_guidance_points_at_the_real_place_to_set_it(self):
        src = VIEWER.read_text(encoding="utf-8")
        self.assertIn("admin_password.txt", src)
        self.assertIn("EXAMVAN_ADMIN_PASSWORD", src)

    def test_iss_does_not_offer_the_removed_checkbox(self):
        text = ISS.read_text(encoding="utf-8")
        # [Tasks] hanya boleh berisi desktopicon.
        tasks = re.search(r"^\[Tasks\](.*?)(?=^\[)", text, re.MULTILINE | re.DOTALL)
        self.assertIsNotNone(tasks)
        # Bentuknya satu baris: Name: "x"; Description: "y"; ...
        names = re.findall(r'Name:\s*"([^"]+)"', tasks.group(1))
        self.assertEqual(names, ["desktopicon"])


class UninstallPromptTest(unittest.TestCase):
    """R3: prompt uninstall harus menyebut folder dan isi yang benar."""

    def setUp(self):
        self.body = _procedure_body("CurUninstallStepChanged")

    def test_mentions_the_real_data_folder(self):
        # Yang sebenarnya holding jawaban, log, dan config:
        # %USERPROFILE%\.config\examvan (config.py:15-16, __main__.py:24).
        #
        # Yang diuji adalah KONSTAN Inno yang dipakai kode, yaitu
        # ExpandConstant('{userprofile}\...'), bukan `%USERPROFILE%`.
        #
        # Assertion sebelumnya mencari "USERPROFILE" huruf besar dan hanya
        # lulus karena satu baris penjelasan di [Code] kehilangan awalan
        # `//` — jadi baris itu diperlakukan sebagai kode dan teksnya
        # ikut terbaca. Begitu `//` dikembalikan, assertion ini benar
        # (dan test-nya jadi salah).
        low = self.body.lower()
        self.assertIn("{userprofile}", low)
        self.assertIn(".config", low)
        self.assertIn("examvan", low)

    def test_does_not_claim_the_wrong_folder_holds_the_answers(self):
        # `{localappdata}\EXAMVAN` hanya berisi admin_password.txt.
        # Menyebutnya "jawaban ujian yang belum terkirim" adalah salah.
        wrong_claims = [
            "konfigurasi server, password admin exit,",
            "jawaban ujian yang belum terkirim, dan app.log",
        ]
        for claim in wrong_claims:
            self.assertNotIn(claim, self.body)

    def test_offers_both_folders(self):
        low = self.body.lower()
        self.assertIn("localappdata", low)
        self.assertIn("{userprofile}", low)

    def test_readme_matches(self):
        text = README.read_text(encoding="utf-8")
        self.assertNotIn(
            "jawaban ujian yang belum terkirim, dan app.log", text
        )


if __name__ == "__main__":
    unittest.main()
