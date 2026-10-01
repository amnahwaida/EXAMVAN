"""Render tangkapan layar app untuk dokumentasi (offscreen, tanpa X server).

Output ke `docs/screenshots/`:
  identity-form.png    — IdentityDialog (form identitas siswa)
  congratulations.png  — CongratulationsWindow (halaman selamat setelah submit)

Dijalankan manual saat UI berubah:
    cd desktop
    QT_QPA_PLATFORM=offscreen .venv/bin/python tools/render_screenshots.py
"""

from __future__ import annotations

import os
import sys

os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")

from pathlib import Path

# Script ini hidup di desktop/tools/; paket examvan ada di desktop/.
sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from PyQt5.QtWidgets import QApplication

REPO_ROOT = Path(__file__).resolve().parents[2]
OUT_DIR = REPO_ROOT / "docs" / "screenshots"


def _apply_dark_theme() -> None:
    """Paksa tema gelap agar kartu dan warna teks KONSISTEN.

    Kartu-kartu (loginCard/identityCard/congratsCard) memilih warna latar
    lewat `is_system_dark()`, sedangkan warna teks global datang dari
    `apply_theme()`. Keduanya HARUS sepakat -- di app nyata memang sepakat
    (sama-sama memanggil is_system_dark()). Screenshot yang memaksa tema
    berbeda dari deteksi sistem menghasilkan teks gelap di kartu gelap,
    yang tidak pernah terjadi bagi pengguna.
    """
    from examvan.ui.styles import apply_theme

    apply_theme(dark=True)


def _grab(widget, path: Path, width: int = 1280, height: int = 800) -> None:
    widget.resize(width, height)
    widget.show()
    APP.processEvents()
    pixmap = widget.grab()
    pixmap.save(str(path))
    widget.close()
    print(f"wrote {path} ({pixmap.width()}x{pixmap.height()})")


def main() -> None:
    global APP
    APP = QApplication(sys.argv)
    _apply_dark_theme()

    from examvan.models import Exam
    from examvan.ui.congratulations import CongratulationsWindow
    from examvan.ui.identity_dialog import IdentityDialog

    OUT_DIR.mkdir(parents=True, exist_ok=True)

    # --- Form identitas ---
    exam = Exam(id=1, name="Ujian Matematika", status="active")
    dlg = IdentityDialog(exam, saved_data={})
    _grab(dlg, OUT_DIR / "identity-form.png")

    # --- Halaman selamat ---
    page = CongratulationsWindow(
        server_url="https://examvan.my.id",
        exam_token="ABCD1234",
        exam_name="Ujian Matematika",
        student_name="Siti Aminah",
        student_number="N-2026-042",
        student_class="9B",
        congrats_message=(
            "Hebat! Jawabanmu lengkap dan tepat waktu. "
            "Sampai jumpa di ujian berikutnya."
        ),
    )
    # Tampilkan di tengah viewport: _grab resize+show, lalu grab.
    _grab(page, OUT_DIR / "congratulations.png")

    # Matikan referensi supaya tidak menggantung di interpreter interaktif.
    del dlg, page


if __name__ == "__main__":
    main()
