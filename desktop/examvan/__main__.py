"""Entry point: python -m examvan [--kiosk] [--kiosk-session]."""

from __future__ import annotations

import atexit
import logging
import os
import signal
import sys
from logging.handlers import RotatingFileHandler


log = logging.getLogger(__name__)

# Handle CreateMutexW agar tetap hidup sepanjang proses (single instance).
_app_mutex_handle = None


def _setup_logging() -> None:
    """Pasang file logging — satu-satunya jejak saat app error di lapangan.

    Proses GUI (PyInstaller --windowed) tidak punya stderr yang terlihat:
    tanpa handler, semua log.info/warning dari security backend, download
    PDF, dan submit hilang begitu saja dan bug lapangan tidak bisa
    didiagnosis. Log di ~/.config/examvan/app.log (rotating 1 MB × 3)
    agar folder config tidak membengkak.

    PERMISSION (LOW): log ini dulu satu-satunya berkas yang ditulis klien
    TANPA 0600/DACL, padahal isinya bisa memuat token kelas
    (`_validated_token` mencatat token di level WARNING saat jatuh ke
    fallback QLineEdit) dan nomor ujian. Semua berkas kredensial lain sudah
    0600 + DACL pemilik; log harus sama. Rotation mewarisi mode berkas asal
    (rename mempertahankan inode), jadi satu panggilan untuk `app.log`
    cukup untuk `app.log.1..3` — tapi log versi lama sudah terlanjur 0644
    di disk, jadi backup yang masih ada ikut dikunci di sini.
    """
    try:
        from pathlib import Path
        log_dir = Path.home() / ".config" / "examvan"
        log_dir.mkdir(parents=True, exist_ok=True)
        log_file = log_dir / "app.log"
        handler = RotatingFileHandler(
            log_file,
            maxBytes=1_000_000,
            backupCount=3,
            encoding="utf-8",
        )
        handler.setFormatter(logging.Formatter(
            "%(asctime)s %(levelname)s %(name)s: %(message)s"
        ))
        # Dipakai helper yang SAMA dengan config.json/answers: `chmod`
        # untuk POSIX dan DACL pemilik untuk Windows (chmod tidak
        # menyentuh ACL). Best-effort di dalamnya.
        from . import config as _config

        for candidate in (log_file, *sorted(log_dir.glob("app.log.*"))):
            try:
                if candidate.is_file():
                    _config._restrict_to_owner(candidate)
            except OSError:
                continue
        root = logging.getLogger()
        root.setLevel(logging.INFO)
        root.addHandler(handler)
    except OSError:
        # Disk penuh / permission — jalan tanpa logging daripada gagal total.
        pass


def _maximize_window(widget, fullscreen: bool = False) -> None:
    """Show a dialog/window, maximized — or on the whole screen when asked.

    `fullscreen=True` is for the exam window. It must NOT also call
    showMaximized(): that call overrides the fullscreen state the security
    enforcer had just set, and a strict exam ended up merely maximized
    (frameless and always-on-top, but not fullscreen) — which is not what
    "kiosk" is supposed to mean.

    Two details that are easy to get wrong and were both wrong in the field:

    * The rect is `screen.geometry()` (the WHOLE screen, taskbar included),
      not `availableGeometry()` (the work area). Using the work area for a
      fullscreen window is what left the taskbar visible at the bottom.
    * `showFullScreen()` runs BEFORE the geometry is forced. The exam window
      arrives here already in the fullscreen state (the enforcer sets it from
      the viewer constructor), so re-requesting the state is a no-op and only
      the `setGeometry()` after it actually covers the screen. The other order
      lets the state call pull the window back into the work area.

    The non-fullscreen path is unchanged: dialogs keep `showMaximized()`.
    """
    from PyQt5.QtWidgets import QApplication

    from .ui.fullscreen import fullscreen_geometry

    screen = QApplication.primaryScreen()
    if fullscreen:
        widget.show()
        widget.showFullScreen()
        if screen is not None:
            widget.setGeometry(fullscreen_geometry(screen))
    else:
        if screen is not None:
            widget.setGeometry(screen.availableGeometry())
        widget.show()
        widget.showMaximized()
    QApplication.processEvents()


def _validated_token(dialog) -> str:
    """Token yang tervalidasi dari dialog konfigurasi, dibaca SEKALI.

    Kontrak sudah ditulis di `ServerConfigDialog.validated_token`
    (`server_config.py:66-70`): "token yang dipakai di seluruh alur PERSIS yang
    tervalidasi". Baca ulang `input_token` melanggar kontrak itu — dan
    `_enable_connect_ui()` sudah menjalankan SEBELUM `exam_selected.emit()`,
    jadi pada saat widget dibaca, inputnya hidup kembali dan isinya bisa saja
    bukan yang server akui.

    Fallback ke QLineEdit hanya bila properti `validated_token` benar-benar
    tidak ada/kosong (dialog versi lama, test double), dan fallback itu
    SELALU dicatat: memakai nilai lain tanpa jejak di log justru menutupi
    satu-satunya bukti token yang berbeda.
    """
    token = ""
    try:
        token = str(getattr(dialog, "validated_token", "") or "").strip().upper()
    except Exception:
        log.warning("tidak bisa membaca validated_token dari dialog konfigurasi",
                    exc_info=True)
        token = ""
    if not token:
        try:
            token = dialog.input_token.text().strip().upper()
            # Token TIDAK dicetak. Pada mode static-token itu kredensial hasil
            # SELURUH KELAS, dan `app.log` di PC lab tidak dibersihkan antar
            # siswa — jadi menuliskannya berarti kelas berikutnya punya token
            # hanya dengan membuka `%USERPROFILE%` + `app.log` di Notepad.
            # Panjang + dua karakter akhir sudah cukup untuk membedakan
            # "konektor tidak mengirim token" dari "siswa mengetik acak",
            # tanpa memperbesar kebocoran.
            log.warning(
                "validated_token kosong — memakai isi QLineEdit untuk dialog "
                "persetujuan dan jendela ujian (panjang=%d, akhir=%s)",
                len(token), token[-2:] if len(token) >= 2 else "-",
            )
        except Exception:
            log.warning("tidak bisa membaca token sama sekali", exc_info=True)
            return ""
    return token


def _exam_needs_monitor_notice(exam) -> bool:
    """True untuk ujian medium — satu-satunya tier yang BERI PERINGATAN layar.

    Dibaca lewat `security_levels.LEVEL_MEDIUM`, bukan literal `"medium"`:
    `Exam.level` sudah kanonik (`normalize_level`), dan konstanta itu yang
    membuat kata server "high" tidak pernah ikut dibandingkan sebagai
    "medium" di tempat yang tidak melakukan normalisasi.

    Strict TIDAK ikut masuk ke sini — dicek lewat `Exam.is_strict`, bukan
    `level`: `is_strict` juga true untuk ujian medium yang diberi flag strict
    oleh server, dan itulah yang sebenarnya ditolak oleh gate strict
    (`on_exam_selected` → blok multi-monitor fail-closed). Tanpa cek ini,
    ujian itu mendapat dialog informasi non-blocking "ujian tetap bisa
    dimulai" — dan DIKAWALIKI, kontras dengan dua dialog penolakan tepat
    di bawahnya: siswa diberi peringatan optimistic yang pasti tidak
    berlaku, lalu ditolak.
    """
    from .security_levels import LEVEL_MEDIUM, normalize_level

    try:
        if bool(getattr(exam, "is_strict", False)):
            return False
        return normalize_level(getattr(exam, "level", None)) == LEVEL_MEDIUM
    except Exception:
        log.warning("tidak bisa membaca level ujian", exc_info=True)
        return False


def _recover_gnome_settings() -> None:
    """Restore GNOME desktop settings after a crash (Linux only).

    Safe to call on Windows (no-op).
    Called at startup, on atexit, and on SIGTERM/SIGINT.
    """
    if sys.platform == "win32":
        return
    try:
        from examvan.security.enforcer import SecurityEnforcer
        SecurityEnforcer.restore_gnome_settings()
    except Exception:
        log.warning("could not restore GNOME settings", exc_info=True)


def _recover_windows_settings() -> None:
    """Restore Windows system settings after a crash (Windows only).

    Mirror of _recover_gnome_settings(). The security backend disables the
    screen saver for the duration of an exam; without this, a process that
    did not exit cleanly left the student's machine altered. Also a no-op on
    Linux, and a no-op when nothing was ever changed.
    """
    if sys.platform != "win32":
        return
    try:
        from examvan.security.windows_backend import restore_windows_settings
        restore_windows_settings()
    except Exception:
        log.warning("could not restore Windows settings", exc_info=True)



def _process_is_alive(pid: int) -> bool:
    """True kalau proses dengan PID itu masih hidup.

    Satu-satunya bukti, dipakai sapu PDF untuk membedakan "sisa proses
    yang sudah mati" dari "unduhan instance lain yang sedang berjalan".

    Arah bias: kalau tidak bisa MEMBUKTIKAN prosesnya sudah mati, jawab
    "hidup". Menghapus file milik proses yang masih menulis membuat
    `os.replace` di proses itu gagal → siswa melihat "download failed"
    untuk berkas yang tidak pernah rusak; menyisakan file basi hanya
    membuang sedikit ruang disk yang akan hilang di sapu berikutnya.

    PID yang dipakai ulang setelah proses mati membuat satu file basi
    tertinggal satu putaran startup — arah yang benar, karena satu
    berkas yang salah dihapus lebih mahal daripada satu berkas yang
    belum terhapus.
    """
    try:
        pid = int(pid)
    except (TypeError, ValueError):
        return False
    if pid <= 0:
        return False
    if os.name == "nt":
        return _windows_process_is_alive(pid)
    try:
        os.kill(pid, 0)
    except ProcessLookupError:
        return False
    except PermissionError:
        # Proses ADA, cuma milik user lain. "Hidup".
        return True
    except OSError:
        return False
    return True


def _windows_process_is_alive(pid: int) -> bool:
    """Versi Win32 dari `_process_is_alive` (best-effort, default "hidup").

    `chmod` sudah tidak berarti apa-apa di Windows, dan `os.kill(pid, 0)`
    tidak ada di sana. `OpenProcess` + `GetExitCodeProcess` adalah cara
    standar; `ERROR_ACCESS_DENIED` dari `OpenProcess` berarti proses ADA
    (milik akun lain), jadi dijawab "hidup" — bukan "mati".

    Kegagalan apa pun → True, supaya caller tidak menghapus file yang
    mungkin masih dipakai.
    """
    try:
        import ctypes
        from ctypes import wintypes

        PROCESS_QUERY_LIMITED_INFORMATION = 0x1000
        STILL_ACTIVE = 259
        ERROR_INVALID_PARAMETER = 87
        kernel32 = ctypes.windll.kernel32  # noqa: PLC0415 — hanya di Windows
        kernel32.GetLastError.restype = wintypes.DWORD
        handle = kernel32.OpenProcess(
            PROCESS_QUERY_LIMITED_INFORMATION, False, pid,
        )
        if not handle:
            # 87 = tidak ada proses dengan PID itu. Apa pun kode lain
            # (termasuk ACCESS_DENIED) = jangan buktikan apa pun, tahan file.
            return kernel32.GetLastError() != ERROR_INVALID_PARAMETER
        try:
            code = wintypes.DWORD()
            if not kernel32.GetExitCodeProcess(handle, ctypes.byref(code)):
                return True
            return code.value == STILL_ACTIVE
        finally:
            kernel32.CloseHandle(handle)
    except Exception:
        return True


def _stale_pdf_candidates(temp_dir) -> list:
    """Semua nama naskah ujian milik EXAMVAN di `temp_dir`, dua bentuknya.

    Bentuk pertama (`examvan_exam_<id>.pdf`) adalah hasil `os.replace`
    yang sudah selesai. Bentuk kedua (`...pdf.tmp.<pid>.<tid>`) adalah
    unduhan yang belum selesai — atau prosesnya DIBUNUH di tengah, dan
    itu justru kasus yang paling mahal: salinan naskah ujian utuh (atau
    hampir utuh) tertinggal di `%TEMP%` sampai proses berikutnya menyapunya.

    `glob("examvan_exam_*.pdf")` tidak akan pernah cocok dengan bentuk
    kedua: `*` di `glob` berhenti di `/`, bukan melintasi titik, dan nama
    itu sudah tidak berakhiran `.pdf`. Pola kedua karena itu dibuat
    eksplisit, bukan `*`.
    """
    from pathlib import Path

    from .api import _PDF_TMP_MARKER

    base = Path(temp_dir)
    return sorted(
        set(base.glob("examvan_exam_*.pdf"))
        | set(base.glob(f"examvan_exam_*.pdf{_PDF_TMP_MARKER}*"))
    )


def _sweep_stale_exam_pdfs() -> None:
    """Sapu PDF ujian basi di tempdir (L-3, best-effort).

    Naskah ujian diunduh ke `examvan_exam_<id>.pdf` di tempdir dan normalnya
    dihapus tiap jalur keluar viewer — tapi crash/kill proses melewatkan
    semuanya, termasuk saudara `.tmp.<pid>.<tid>` yang tidak pernah dihapus
    `_discard_pdf` (ia hanya tahu `dest_path`).

    BERJALAN SETELAH `_acquire_single_instance_lock()`. Dulu urutannya
    terbalik: sapu dipanggil sebelum mutex diambil, sementara docstring
    membenarkan keamanannya justru dengan mutex yang belum ada.
    Sekarang urutannya dijamin oleh `main()` dan, sebagai jaring kedua
    yang benar-benar bekerja lintas platform, `_process_is_alive`: berkas
    temp milik proses yang masih hidup tidak pernah disentuh.

    Catatan jujur tentang mutex (MEDIUM 2): `CreateMutexW` dengan nama
    tanpa awalan `Global`/`Local` hidup di namespace SESI Win32 pemanggil,
    jadi dua sesi di satu mesin (Fast User Switching, RDP/terminal server)
    masing-masing punya mutex sendiri dan keduanya tetap bisa jalan. Klaim
    itu disimpulkan dari semantik Win32 (dokumentasi object namespace),
    TIDAK bisa dieksekusi di runner Linux ini. Di Linux tidak ada mutex
    sama sekali.
    Itulah sebabnya cek PID — bukan mutex — yang jadi mekanisme keamanannya.

    Best-effort: kegagalan tidak boleh menggagalkan startup.

    WAJIB modul-level, bukan di tengah badan `main()`: fungsi yang
    didefinisikan di dalam badan fungsi lain mengakhiri badan itu, jadi
    `main()` akan berhenti setelah blokClosures dan tidak pernah membuat
    jendela sama sekali.
    """
    try:
        import tempfile

        from .api import _pdf_temp_owner_pid

        count = 0
        for path in _stale_pdf_candidates(tempfile.gettempdir()):
            try:
                if not path.is_file():
                    continue
                pid, _tid = _pdf_temp_owner_pid(path.name)
                if pid is not None and _process_is_alive(pid):
                    # Instance lain sedang mengunduh ke sana.
                    continue
                path.unlink()
                count += 1
            except OSError:
                continue
        log.debug("menyapu %d PDF ujian basi", count)
    except Exception:
        log.debug("sweep PDF ujian basi gagal", exc_info=True)


def _acquire_single_instance_lock() -> bool:
    """Kunci instansi-tunggal. True = lanjut; False = sudah ada proses lain.

    Dipisah dari `main()` supaya urutannya terhadap sapu PDF bisa diuji
    tanpa menjalankan UI, dan supaya penolakan (`sys.exit`) tetap milik
    `main()` saja -- fungsi ini tidak boleh mengeluarkan proses darinya.

    Batas jujur dari mekanisme ini, disimpulkan dari semantik Win32 dan
    TIDAK bisa dieksekusi di runner Linux ini:

    * Nama tanpa awalan `Global`/`Local` hidup di namespace SESI pemanggil.
      Dua sesi di satu mesin (Fast User Switching, RDP/terminal server)
      masing-masing mendapat mutex sendiri, jadi keduanya tetap bisa
      menjalankan EXAMVAN. Perbaikannya (awalan `Global`) menuntut nama
      yang sama dipakai di `examvan.iss` (`AppMutex`), jadi itu perubahan
      lintas berkas -- dilaporkan, tidak dikerjakan di sini.
    * Di Linux fungsi ini selalu True: tidak ada mutex. Itulah sebabnya
      `_sweep_stale_exam_pdfs` tidak bergantung pada mutex dan memeriksa
      PID pemilik tiap berkas temp satu per satu (`_process_is_alive`).

    Kegagalan memasang mutex = lanjut jalan (best-effort), seperti
    sebelumnya: PC yang salah konfigurasi lebih baik menampilkan jendela
    daripada diam-diam tidak bisa dibuka.
    """
    if sys.platform != "win32":
        return True
    try:
        from ctypes import windll  # noqa: PLC0415 — hanya ada di Windows

        global _app_mutex_handle
        _kernel32 = windll.kernel32
        _handle = _kernel32.CreateMutexW(None, False, "EXAMVAN_SingleInstance_v1")
        # GetLastError harus dibaca SEGERA setelah CreateMutexW.
        _err = _kernel32.GetLastError()
        if _err == 183:  # ERROR_ALREADY_EXISTS
            return False
        _app_mutex_handle = _handle
        return True
    except Exception:
        log.warning("single-instance mutex gagal dipasang", exc_info=True)
        return True


# Modul Qt yang di-hard-import oleh aplikasi.
#
# `examvan.ws` melakukan `from PyQt5.QtWebSockets import QWebSocket` di
# level modul tanpa fallback, dan `exam_viewer` meng-import `ws` di level
# modul juga -- jadi modul yang hilang berarti proses MATI dengan
# traceback, bukan degrade ke mode tanpa websocket.
#
# Distribution PyQt5 dari PyPI (yang dipakai `install.ps1` dan CI) selalu
# menyertakan QtWebSockets, jadi ini bukan masalah di jalur install
# resmi. Yang bermasalah adalah paket distro yang memecah PyQt5 jadi
# beberapa paket: Debian/Ubuntu menyediakan `python3-pyqt5.qtwebsockets`
# sebagai paket TERPISAH, dan mirror lab yang hanya menyajikan PyQt5 inti
# akan menghasilkan PC dengan gejala "klik shortcut, tidak terjadi
# apa-apa".
_REQUIRED_QT_MODULES = (
    ("PyQt5.QtCore", "pyqtcore"),
    ("PyQt5.QtWidgets", "pyqt5"),
    ("PyQt5.QtWebSockets", "pyqt5"),
)

_MISSING_QT_HELP = (
    "EXAMVAN tidak bisa berjalan di PC ini.\n\n"
    "Modul Python berikut tidak ditemukan: {modules}\n\n"
    "Perbaikan untuk siswa: hubungi pengawas.\n\n"
    "Perbaikan untuk teknisi PC lab:\n"
    "  .venv\\Scripts\\python -m pip install -r desktop\\requirements.txt\n\n"
    "Kalau PC ini memakai paket PyQt5 dari distro (Debian/Ubuntu), "
    "modul QtWebSockets ada di paket terpisah dan perlu dipasang "
    "juga: python3-pyqt5.qtwebsockets"
)


def _missing_qt_modules():
    """Nama modul Qt yang tidak bisa di-import, atau () kalau semua ada.

    Dipisah dari `main()` supaya bisa diuji tanpa menjalankan app --
    logika startup seharusnya punya test, bukan hanya dicoba di lapangan.
    """
    import importlib.util

    missing = []
    for name, _package in _REQUIRED_QT_MODULES:
        try:
            if importlib.util.find_spec(name) is None:
                missing.append(name)
        except (ImportError, ValueError):
            # ValueError: modul induk ada tapi bukan package -- sama
            # saja tidak bisa dipakai.
            missing.append(name)
    return tuple(missing)


def _abort_on_missing_qt(app) -> None:
    """Tampilkan pesan yang bisa dibaca lalu keluar dengan kode != 0.

    Tanpa guard ini, `from .ui.exam_viewer import ...` di bawah melempar
    ModuleNotFoundError: proses mati, tidak ada jendela, tidak ada
    message box -- siswa hanya melihat shortcut yang tidak pernah
    membuka apa pun.
    """
    missing = _missing_qt_modules()
    if not missing:
        return
    names = ", ".join(missing)
    log.error("Modul Qt hilang: %s -- EXAMVAN tidak bisa jalan", names)
    from PyQt5.QtWidgets import QMessageBox

    QMessageBox.critical(
        None,
        "EXAMVAN - tidak bisa dijalankan",
        _MISSING_QT_HELP.format(modules=names),
    )
    # 2 = "environment salah", bukan crash. Berguna untuk log lab.
    sys.exit(2)


def main() -> None:
    _setup_logging()

    # Register crash-recovery handlers
    def _exit_after_recovery() -> None:
        # Recovery best-effort: os._exit WAJIB jalan walau recovery raise.
        try:
            _recover_gnome_settings()
        finally:
            os._exit(1)

    def _exit_after_recovery_win() -> None:
        try:
            _recover_windows_settings()
        finally:
            os._exit(1)


    if sys.platform != "win32":
        atexit.register(_recover_gnome_settings)
        signal.signal(signal.SIGTERM, lambda *_: _exit_after_recovery())
        signal.signal(signal.SIGINT, lambda *_: _exit_after_recovery())
        _recover_gnome_settings()
    else:
        # Windows had `atexit.register(lambda: None)` here — a literal no-op,
        # so the screen-saver change made by the security backend was never
        # undone unless the exam viewer happened to close cleanly. This is the
        # atexit half of the recovery; the other half is WindowsBackend.activate()
        # at startup, which handles the case where the process never got to
        # run atexit at all.
        atexit.register(_recover_windows_settings)
        _recover_windows_settings()

    # Identitas + konteks dari proses SEBELUMNYA ikut disapu di sini.
    #
    # Semua jalur keluar bersih membersihkan identitas, tapi proses yang
    # DIBUNUH (listrik mati, task manager, crash) tidak menjalankan satu pun
    # dari mereka — jadi identitas siswa A tetap di `config.json`. Siswa
    # berikutnya di PC yang sama dengan token yang sama mendapat form yang
    # sudah terisi, dan `last_input.returnPressed` terikat ke submit: SATU
    # Enter sudah cukup untuk tercatat sebagai A.
    #
    # Aman di sini karena belum ada dialog yang perlu menampilkan identity
    # dari config — dan justru itu sebabnya helper-nya TIDAK dipanggil
    # sendiri dari modul config.
    from . import config as _config

    try:
        if _config.clear_stale_identity_on_startup():
            log.info("identitas sisa dari proses sebelumnya dibersihkan")
    except Exception:
        log.warning("sapu identitas sisa gagal", exc_info=True)

    kiosk = "--kiosk" in sys.argv or "--kiosk-session" in sys.argv

    from PyQt5.QtCore import Qt
    from PyQt5.QtWidgets import QApplication

    # High-DPI support
    QApplication.setAttribute(Qt.AA_EnableHighDpiScaling, True)
    QApplication.setAttribute(Qt.AA_UseHighDpiPixmaps, True)

    app = QApplication(sys.argv)
    app.setApplicationName("EXAMVAN")

    # Instansi tunggal (Windows saja): cegah dua proses ujian berjalan
    # bersamaan di PC yang sama. No-op di Linux.
    #
    # URUTAN PENTING (MEDIUM 2): kunci diambil SEBELUM sapu PDF. Dulu
    # sapu dipanggil 40 baris lebih awal, sementara docstring-nya
    # membenarkan keamanannya dengan mutex yang belum ada pada baris itu.
    # Di Linux tidak ada mutex sama sekali, jadi yang benar-benar menahan
    # sapu agar tidak menabrak unduhan instance lain adalah cek PID di
    # `_sweep_stale_exam_pdfs` — dua hal ini sengaja dipisah: mutex
    # mengurangi jumlah proses, cek PID yang membuat penghapusan aman.
    if not _acquire_single_instance_lock():
        from PyQt5.QtWidgets import QMessageBox

        QMessageBox.warning(
            None,
            "EXAMVAN",
            "EXAMVAN sudah berjalan. Tutup jendela yang ada "
            "sebelum membuka yang baru.",
        )
        sys.exit(0)

    # L-3: sapu PDF ujian basi di sebelah pemulihan crash — dan di
    # belakang kunci instansi-tunggal.
    _sweep_stale_exam_pdfs()

    # Dari APP_VERSION, bukan literal: literal ketiga yang tidak terhubung
    # ke mana pun adalah alasan Properties exe dan installer pernah
    # melaporkan nomor berbeda.
    from . import APP_VERSION
    app.setApplicationVersion(APP_VERSION)
    app.setOrganizationName("EXAMVAN")

    # `config` WAJIB di-import di scope `main()` ini, bukan hanya di dalam
    # badan tempat config dipakai: `_after_viewer_gone` dan `_back_to_config`
    # adalah closure di dalam `main()` yang memanggil `config.clear_identity()`
    # dan `config.set("identity_context", {})`, dan closure hanya melihat
    # nama yang terikat di scope `main()`.
    #
    # Import ini pernah hilang saat refactor tema (yang mengganti
    # `is_system_dark` dengan `app_theme_dark` dan kelewatan menghapus
    # baris `from . import config`). Akibatnya `NameError: name 'config' is
    # not defined` di kedua closure — dan `NameError` itu tertelan
    # `except Exception` + `log.warning`, jadi tidak terlihat: identitas
    # siswa sebelumnya TIDAK PERNAH dibersihkan dan siswa berikutnya
    # mendapat form terisi nama orang itu. Lihat
    # `tests/test_r7_main_config_binding.py`.
    from . import config

    # Apply theme. Tema aplikasi dikunci gelap (lihat `styles.app_theme_dark`),
    # bukan mengikuti Pengaturan Windows: ruang kelas sering punya PC dengan
    # tema sistem berbeda-beda, dan tampilan harus sama di semua layar.
    from .ui.styles import app_theme_dark, apply_theme
    apply_theme(dark=app_theme_dark())

    # WAJIB sebelum import di bawah: `examvan.ws` hard-import
    # PyQt5.QtWebSockets, dan `exam_viewer` meng-import `ws` di level
    # modul. Tanpa cek ini, PC tanpa modul tersebut mati dengan
    # traceback tanpa jendela sama sekali.
    _abort_on_missing_qt(app)

    # Launch server config dialog
    from .ui.server_config import ServerConfigDialog
    from .ui.exam_viewer import ExamViewerWindow

    windows = []  # prevent GC

    def on_exam_selected(exam, server_url, identity_data):
        dialog.hide()

        from .ui.waiting_approval import WaitingApprovalDialog
        from PyQt5.QtWidgets import QDialog, QMessageBox

        # SEKALI, di awal: satu-satunya sumber token untuk dialog persetujuan,
        # jendela ujian, dan (lewat viewer) PDF/submit/presence. Dibaca ulang
        # di dua tempat berarti keduanya bisa berbeda — widget-nya hidup lagi
        # sejak `_enable_connect_ui()` dijalankan sebelum emit.
        token = _validated_token(dialog)

        def _back_to_config(reason: str) -> None:
            """Kembalikan dialog konfigurasi siap-pakai (H5).

            Satu-satunya jalan kembali: identitas dibersihkan (anti
            prefill-silangan, beserta konteksnya), tombol+input dihidupkan
            lagi, dialog ditampilkan maximized. Dipakai SEMUA early-return
            di bawah supaya tidak ada jalur yang lupa satu langkah.

            Token SENGAJA TIDAK dikosongkan di sini: siswa yang salah ketik
            atau mendapat 5xx harus bisa mencoba ulang tanpa mengetik
            ulang. Token bukan data pribadi — identitas yang itu. Token baru
            dikosongkan setelah ujian benar-benar selesai (`_after_viewer_gone`),
            supaya siswa berikutnya tidak mewarisi kredensial kelas.
            """
            log.info("kembali ke konfigurasi: %s", reason)
            try:
                config.clear_identity()
            except Exception:
                log.warning("could not clear stored identity", exc_info=True)
            try:
                config.set("identity_context", {})
            except Exception:
                log.warning("could not clear identity context", exc_info=True)
            try:
                dialog.enable_connect()
            except Exception:
                log.warning("could not re-enable connect button", exc_info=True)
            dialog.show()
            _maximize_window(dialog)

        # M-5 (dipindah ke sini): medium + multi-monitor diperingatkan
        # SEBELUM persetujuan pengawas dikuras.
        #
        # Dulu blok ini berada SETELAH `waiting_dlg.exec_()`: siswa dengan
        # dua monitor memanggil pengawas, menunggu persetujuan, baru diberi
        # tahu PC-nya bermasalah. Strict punya gate awal
        # (`ServerConfigDialog._strict_monitor_ok`, fail-CLOSED); medium tidak
        # punya apa pun. Medium sengaja "warn and continue" — jadi gate
        # di sini NON-BLOCKING (`information`, bukan `question`), dan alur
        # tetap jalan setelah siswa menutupnya.
        #
        # Kegagalan detektor = FAIL-OPEN, mengikuti semantik gate strict di
        # `server_config`: "tidak diketahui" BUKAN "lebih dari satu layar",
        # dan menahan medium hanya karena EnumDisplayMonitors error akan
        # mengunci semua PC yang SMBus-nya bermasalah.
        if _exam_needs_monitor_notice(exam):
            try:
                from .security import get_backend

                _medium_multi = bool(get_backend().has_multiple_monitors())
            except Exception:
                log.warning("deteksi multi-monitor gagal (medium) — lanjut",
                            exc_info=True)
                _medium_multi = False
            if _medium_multi:
                QMessageBox.information(
                    dialog,
                    "Monitor Ganda Terdeteksi",
                    "Terdeteksi lebih dari satu layar. Ujian tetap bisa "
                    "dimulai, tetapi pastikan hanya mengerjakan di layar "
                    "utama — aktivitas di layar lain dapat tercatat.",
                )

        waiting_dlg = WaitingApprovalDialog(
            exam, server_url, identity_data,
            token=token,
            parent=dialog,
        )
        _maximize_window(waiting_dlg)

        if waiting_dlg.exec_() != QDialog.Accepted:
            # Siswa membatalkan / ditolak. Dialog konfigurasi kembali —
            # identitas yang baru disimpan `_show_identity_dialog` WAJIB
            # dibersihkan (kalau tidak, siswa berikutnya mendapat form
            # terisi dan Enter saja cukup menjawab atas nama orang lain).
            _back_to_config("approval-cancel")
            return

        viewer = None
        if exam.is_strict:
            # Strict + multi-monitor: tolak mulai — pengawas tidak ter-cover
            # di layar kedua. Detector exception = FAIL-CLOSED (tolak dengan
            # pesan yang bisa ditindak): melepas ujian strict tanpa tahu
            # berapa layar terpasang lebih buruk daripada menolak.
            try:
                from .security import get_backend

                _multi = bool(get_backend().has_multiple_monitors())
                _detect_ok = True
            except Exception:
                log.warning("deteksi multi-monitor gagal — tolak (strict)",
                            exc_info=True)
                _detect_ok = False
                _multi = False
            if not _detect_ok:
                QMessageBox.warning(
                    dialog,
                    "Tidak Dapat Memeriksa Layar",
                    "Ujian ini berjalan dalam mode ketat dan aplikasi tidak "
                    "dapat memastikan hanya satu layar yang terpasang.\n\n"
                    "Pastikan hanya satu layar terhubung lalu coba lagi, "
                    "atau hubungi pengawas.",
                )
                _back_to_config("strict-monitor-detect-error")
                return
            if _multi:
                QMessageBox.warning(
                    dialog,
                    "Monitor Ganda Terdeteksi",
                    "Ujian ini berjalan dalam mode ketat dan hanya boleh "
                    "menggunakan satu layar.\n\nLepaskan monitor kedua "
                    "lalu coba lagi.",
                )
                _back_to_config("strict-multi-monitor")
                return

        # (blok medium sudah dipindah ke SEBELUM dialog persetujuan —
        # lihat catatan M-5 di atas.)

        viewer = ExamViewerWindow(
            exam=exam,
            server_url=server_url,
            token=token,
            identity_data=identity_data,
            kiosk_mode=kiosk,
        )

        _gone_state = {"done": False}

        def _after_viewer_gone():
            # Idempoten: bisa dicapai via `closed` maupun `all_done`
            # (tergantung jalur submit) — langkah kembali hanya sekali.
            if _gone_state["done"]:
                return
            _gone_state["done"] = True
            viewer.close()
            windows.clear()
            # Token DAN identitas harus dibersihkan.
            #
            # Hanya token yang pernah dibersihkan, dan itu justru menyisakan
            # kebocoran yang lebih buruk: `identity_data` dibaca lagi di
            # ServerConfigDialog._show_identity_dialog lalu dipakai untuk
            # MENGISI form IdentityDialog. Siswa berikutnya akan mendapat
            # form terisi nama siswa sebelumnya dan bisa menekan Enter untuk
            # menjawab atas nama orang itu. Tanpa dialog, tanpa warning,
            # tanpa log. Lihat review_windows_2026-09-30.md Bagian 1.
            dialog.input_token.clear()
            try:
                config.clear_identity()
            except Exception:
                log.warning("could not clear stored identity", exc_info=True)
            try:
                config.set("identity_context", {})
            except Exception:
                log.warning("could not clear identity context", exc_info=True)
            try:
                dialog.enable_connect()
            except Exception:
                log.warning("could not re-enable connect button", exc_info=True)
            _maximize_window(dialog)

        def _on_viewer_closed():
            # C1: `closed` yang menembak saat auto-submit menutup window
            # BUKAN akhir alur (hasil background belum tiba; halaman selamat
            # belum tampil) — tahan sampai `all_done`. `is True` eksplisit
            # (bukan truthiness): Mock viewer di test tidak punya flag ini
            # sebagai bool sungguhan.
            if getattr(viewer, "_auto_submit_pending", False) is True:
                return
            _after_viewer_gone()

        viewer.closed.connect(_on_viewer_closed)
        viewer.all_done.connect(_after_viewer_gone)
        windows.append(viewer)
        # The exam window owns the WHOLE screen, in every security level.
        # Previously this was `fullscreen=viewer.is_strict`, so a medium or
        # low exam was merely maximized and the taskbar stayed visible — and
        # for strict it was still wrong, because _maximize_window used the
        # work area as the fullscreen rect. See examvan.ui.fullscreen.
        _maximize_window(viewer, fullscreen=True)

    dialog = ServerConfigDialog(kiosk_mode=kiosk)
    dialog.exam_selected.connect(on_exam_selected)
    _maximize_window(dialog)

    sys.exit(app.exec_())


if __name__ == "__main__":
    main()
