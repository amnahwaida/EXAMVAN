"""Security Enforcer — dispatches low/medium/strict mode enforcement.

Cross-platform: delegates platform-specific operations to a backend
obtained from get_backend().
"""

from __future__ import annotations

import logging
import sys
import threading
from contextlib import contextmanager
from typing import Iterator, Optional

from PyQt5.QtCore import QObject, Qt, QTimer, pyqtSignal
from PyQt5.QtWidgets import QApplication, QWidget

from ..security_levels import (
    LEVEL_LOW,
    is_effective_strict,
    normalize_level,
)
from . import get_backend

log = logging.getLogger(__name__)

# How often the clipboard is wiped while an exam is open.
#
# 3000 ms was chosen back when clearing it also spawned `cmd.exe /c
# echo.|clip` (windows_backend.clear_clipboard). That fork is gone — the
# Win32 EmptyClipboard call is microseconds — but the interval stayed, so
# low-end machines paid a needless timer wake-up every 3 seconds. 10 s is
# still far tighter than the time a student needs to copy an answer out
# and paste it somewhere else.
CLIPBOARD_INTERVAL_MS = 10000

# Focus poll cadence. Only runs from medium up (medium needs it to detect
# focus loss; strict re-raises the window on top of that). At 500 ms the
# strict path also calls raise_() + activateWindow(), which forces a DWM
# recomposite every tick — cheap on a desktop, visible on a low-end laptop.
FOCUS_POLL_INTERVAL_MS = 500


class SecurityEnforcer(QObject):
    """Enforces exam security based on mode."""

    auto_submit = pyqtSignal()

    def __init__(
        self,
        security_level: str = "low",
        strict_mode: bool = False,
        window: Optional[QWidget] = None,
        kiosk_mode: bool = False,
        parent=None,
    ):
        super().__init__(parent)
        # Canonicalise the level ONCE, here, so nothing downstream has to
        # remember that the server says "high" and the client says
        # "strict". Before this, three call sites compared the raw string
        # and a "high" exam matched none of them.
        self._level = normalize_level(security_level)
        self._strict = is_effective_strict(security_level, strict_mode)
        self._window = window
        self._kiosk = kiosk_mode
        self._active = False

        # Platform backend
        self._backend = get_backend()

        # Timers
        self._clipboard_timer = QTimer(self)
        self._clipboard_timer.timeout.connect(self._clear_clipboard)
        self._focus_timer = QTimer(self)
        self._focus_timer.setSingleShot(True)
        self._focus_timer.setInterval(3000)
        self._focus_timer.timeout.connect(self._on_focus_timeout)

        # Clipboard clearing runs on a worker thread (see _clear_clipboard).
        self._clipboard_lock = threading.Lock()
        self._clipboard_busy = False
        self._clipboard_thread: Optional[threading.Thread] = None

        # Focus-guard suspension state. See pause_focus_guard().
        self._focus_guard_paused = False
        self._focus_guard_depth = 0
        self._focus_guard_resume = False

    # ------------------------------------------------------------------
    # Modal-dialog suspension
    # ------------------------------------------------------------------

    @contextmanager
    def pause_focus_guard(self) -> Iterator[None]:
        """Tangguhkan deteksi focus-loss selama dialog modal terbuka.

        Kenapa ini wajib ada
        --------------------
        `QMessageBox.question()` dan `QInputDialog.getText()` menjalankan
        NESTED EVENT LOOP. Selama itu berjalan, dialog adalah window aktif
        dan window ujian menjadi `isActiveWindow() == False` -- bukan
        karena siswaixa Meganleave the exam, tapi karena Qt sedang
        menaruh dialog di atasnya.

        `_poll_focus()` sendiri persis menanyakan `isActiveWindow()`
        setiap 500 ms, jadi tanpa penangguhan:

            dialog terbuka
              -> _poll_focus melihat window tidak aktif
              -> _focus_timer.start()  (3 detik, single shot)
              -> _on_focus_timeout()
              -> auto_submit.emit()
              -> ExamViewerWindow._auto_submit_and_exit()

        Answers terkirim dan jendela tertutup sementara siswa masih
        membaca "Yakin ingin mengumpulkan?". Jalur kedua, yang lebih
        cepat, adalah `_on_window_active_changed` yang terpasang di
        `windowHandle().activeChanged`.

        Efeknyarunner-up: di strict, `_poll_focus` juga memanggil
        `raise_()` + `activateWindow()` pada window induk tiap 500 ms, jadi
        jendela ujian bertarung dengan dialog soal z-order -- dialog
        berkedip atau tertimpa.

        Yang dipulihkan
        ---------------
        Countdown focus-loss yang SEDANG BERJALAN saat dialog dibuka ikut
        dibekukan dan dilanjutkan lagi setelahnya. Kalau tidak, siswa
        yang sedang menjawab dialog konfirmasi tepat saat timer-nya
        Kedaluwarsa akan kehilangan auto-submit yang seharusnya terjadi --
        dan `_submitted` sudah preventif, jadi auto-submit kedua
        ditolak begitu saja.

        `low` tidak punya focus guard sama sekali, jadi tidak ada yang
        perlu ditangguhkan.
        """
        if self._level == LEVEL_LOW and not self._strict:
            yield
            return

        self._focus_guard_depth += 1
        if self._focus_guard_depth == 1:
            self._focus_guard_resume = self._focus_timer.isActive()
            self._focus_timer.stop()
            self._focus_guard_paused = True
        try:
            yield
        finally:
            self._focus_guard_depth -= 1
            if self._focus_guard_depth == 0:
                self._focus_guard_paused = False
                if self._focus_guard_resume:
                    self._focus_timer.start()
                self._focus_guard_resume = False

    # ------------------------------------------------------------------
    # Public API
    # ------------------------------------------------------------------

    @property
    def level(self) -> str:
        """Canonical tier: "low" | "medium" | "strict"."""
        return self._level

    @property
    def strict(self) -> bool:
        """True when the strictest lockdown is engaged."""
        return self._strict

    def activate(self) -> None:
        """Activate security enforcement based on mode."""
        if self._active:
            return
        self._active = True
        self._backend.activate()
        log.info("Activating security: level=%s, strict=%s", self._level, self._strict)

        # Check multi-monitor (Windows: display warning; Linux: log)
        if self._backend.has_multiple_monitors():
            log.warning("Multiple monitors detected — security risk")
            if self._strict:
                # In strict mode, the app stays fullscreen on primary monitor
                log.warning("Strict mode active — secondary monitor not covered")

        # Low mode features (always active)
        self._activate_low()

        # Medium-or-above. Uses the canonical level, so the server's "high"
        # lands here instead of falling through to a lockdown-free exam.
        if self._level != LEVEL_LOW or self._strict:
            self._activate_medium()

        if self._strict:
            self._activate_strict()

    def deactivate(self) -> None:
        """Deactivate all security enforcement."""
        if not self._active:
            return
        self._active = False
        log.info("Deactivating security")

        self._clipboard_timer.stop()
        self._focus_timer.stop()
        if hasattr(self, '_poll_timer'):
            self._poll_timer.stop()
        self.wait_for_clipboard_clear()

        self._backend.release_strict_mode(self._window)
        self._backend.release_capture_protection(self._window)
        self._backend.allow_sleep()
        self._backend.deactivate()

        # Linux specific GNOME restore (if Linux)
        if sys.platform != "win32":
            self._recover_gnome_settings()

    # ------------------------------------------------------------------
    # GNOME crash recovery — Linux only
    # ------------------------------------------------------------------

    @staticmethod
    def restore_gnome_settings() -> None:
        """Restore GNOME settings from crash backup. No-op on Windows."""
        if sys.platform == "win32":
            return
        try:
            from .linux_backend import LinuxBackend
            LinuxBackend.restore_gnome_settings()
        except ImportError:
            pass

    def _recover_gnome_settings(self) -> None:
        """Instance-level GNOME restore (deactivate helper)."""
        try:
            from .linux_backend import LinuxBackend
            LinuxBackend.restore_gnome_settings()
        except ImportError:
            pass

    # ------------------------------------------------------------------
    # Low mode
    # ------------------------------------------------------------------

    def _activate_low(self) -> None:
        # Anti-screenshot (Linux X11 only)
        if sys.platform != "win32":
            self._x11_anti_screenshot()

        # Clipboard clear, on a slow cadence and off the GUI thread.
        self._clipboard_timer.start(CLIPBOARD_INTERVAL_MS)
        self._clear_clipboard()

        # Screen wake lock
        self._backend.prevent_sleep()

    def _x11_anti_screenshot(self) -> None:
        """Apply X11 bypass-compositor hint (Linux only, no-op on Windows)."""
        try:
            from . import x11
            if x11.is_x11() and self._window:
                x11.set_bypass_compositor(self._window)
                log.info("Anti-screenshot: X11 bypass compositor set")
            elif x11.is_wayland():
                log.info("Anti-screenshot: Wayland inherently protected")
        except ImportError:
            pass

    def _clear_clipboard(self) -> None:
        """Wipe the clipboard without stalling the UI.

        Two halves, deliberately on different threads:

          * QApplication.clipboard() must be touched from the GUI thread —
            Qt enforces this — but it is a cheap in-process call, so it
            stays inline.
          * The platform clear (Win32 EmptyClipboard / X11 xsel) goes to a
            worker. It is normally microseconds, but on a clipboard holding
            OLE data (an image copied out of Word, a file drop) EmptyClipboard
            makes Windows serialise that data to the new owner first, which
            can block for a long time. Doing that on the GUI thread froze
            the cursor on the low-end machines in the lab.

        Overlapping runs are skipped rather than queued: a clipboard wipe
        that has not finished yet means the next one would find the same
        content, and queueing would only build up stale threads.
        """
        if not self._active:
            return

        try:
            app = QApplication.instance()
            if app:
                app.clipboard().clear()
        except Exception:
            log.debug("Qt clipboard clear failed", exc_info=True)

        with self._clipboard_lock:
            if self._clipboard_busy:
                log.debug("Clipboard clear still running — skipping this tick")
                return
            self._clipboard_busy = True

        thread = threading.Thread(
            target=self._clear_clipboard_worker, name="clipboard-clear", daemon=True
        )
        self._clipboard_thread = thread
        thread.start()

    def _clear_clipboard_worker(self) -> None:
        try:
            self._backend.clear_clipboard()
        except Exception:
            log.warning("Platform clipboard clear failed", exc_info=True)
        finally:
            with self._clipboard_lock:
                self._clipboard_busy = False

    def clear_clipboard_now(self) -> None:
        """Wipe the clipboard immediately, on the calling thread's terms.

        Public entry point for event-driven clears (the PrintScreen handler)
        that must not wait for the next timer tick. Same threading split as
        the timer: Qt inline, platform side on the worker.
        """
        self._clear_clipboard()

    def wait_for_clipboard_clear(self, timeout: float = 2.0) -> None:
        """Block until any in-flight clipboard clear has finished.

        Used by deactivate() so a wipe cannot still be running when the
        process tears the backend down, and by tests so they never assert
        against a half-applied clear.
        """
        thread = self._clipboard_thread
        if thread is not None and thread.is_alive():
            thread.join(timeout)

    # ------------------------------------------------------------------
    # Medium mode
    # ------------------------------------------------------------------

    def _activate_medium(self) -> None:
        # Screen-capture resistance starts at medium, not strict. It used to
        # live inside set_strict_mode(), which meant a medium exam had none.
        if self._window:
            self._backend.set_capture_protection(self._window)

        app = QApplication.instance()
        if app:
            app.applicationStateChanged.connect(self._on_app_state_changed)
            log.info("Focus loss monitoring activated")

        if self._window:
            try:
                self._window.windowHandle().activeChanged.connect(
                    self._on_window_active_changed
                )
            except Exception:
                pass

        self._poll_timer = QTimer(self)
        self._poll_timer.setInterval(FOCUS_POLL_INTERVAL_MS)
        self._poll_timer.timeout.connect(self._poll_focus)
        self._poll_timer.start()
        log.info("Focus poll timer started (%dms interval)", FOCUS_POLL_INTERVAL_MS)

    def _app_popup_open(self) -> bool:
        """True selagi popup MILIK APLIKASI INI terbuka.

        Contohnya daftar pilihan QComboBox di lembar jawaban (soal
        menjodohkan) dan menu konteks. Popup itu window top-level
        TERPISAH, jadi guard fokus harus memperlakukannya sebagai "siswa
        sedang memakai app", bukan "siswa keluar dari ujian":

          * di strict, `raise_()` + `activateWindow()` pada window ujian
            mengembalikan aktivasi ke window induk -- popup kehilangan
            fokus dan QComboBox menutup dirinya sendiri. Siswa klik
            dropdown, popup berkedip lalu hilang sebelum sempat memilih:
            laporan lapangan "jawaban yang ada dropdownnya susah diklik";
          * di medium, aktivasi yang berpindah ke popup bisa terbaca
            sebagai ApplicationInactive -- countdown 3 detik berjalan dan
            auto-submit menembak saat siswa masih memilih opsi.

        Popup selalu berumur pendek dan hanya bisa dibuka dari dalam
        window ujian (klik atau keyboard), jadi menutup mata selama popup
        terbuka tidak membuka jalan keluar: begitu popup ditutup, polling
        berikutnya (<=500 ms) menilai fokus seperti biasa lagi.
        """
        try:
            app = QApplication.instance()
            return bool(app is not None and app.activePopupWidget() is not None)
        except Exception:
            return False

    def _on_app_state_changed(self, state: Qt.ApplicationState) -> None:
        # `isActiveWindow() == False` selama dialog modal terbuka itu
        # NORMAL, bukan tanda murid keluar dari ujian. Tanpa cek ini
        # countdown 3 detik berjalan di belakang dialog dan auto-submit
        # terjadi tanpaPressed. Lihat pause_focus_guard().
        if not self._active or self._focus_guard_paused:
            return
        if state == Qt.ApplicationInactive:
            # Popup aplikasi sendiri (dropdown QComboBox, menu) juga
            # membuat window induk terlihat "tidak aktif" di sebagian
            # platform -- itu bukan alasan memulai countdown auto-submit.
            # Lihat _app_popup_open().
            if self._app_popup_open():
                return
            log.warning("Focus lost — starting 3s auto-submit countdown")
            self._focus_timer.start()
        elif state == Qt.ApplicationActive:
            if self._focus_timer.isActive():
                log.info("Focus regained — cancelling auto-submit countdown")
                self._focus_timer.stop()

    def _on_window_active_changed(self) -> None:
        if not self._active or not self._window or self._focus_guard_paused:
            return
        if not self._window.isActiveWindow():
            self._on_app_state_changed(Qt.ApplicationInactive)
        else:
            self._on_app_state_changed(Qt.ApplicationActive)

    def _poll_focus(self) -> None:
        if not self._active or not self._window or self._focus_guard_paused:
            return

        # Popup aplikasi sendiri sedang terbuka: JANGAN raise/activate
        # (menutup popup yang sedang dipakai siswa) dan jangan mulai
        # countdown. Lihat _app_popup_open().
        if self._app_popup_open():
            return

        if self._strict:
            self._window.raise_()
            self._window.activateWindow()

        if not self._window.isActiveWindow():
            if not self._focus_timer.isActive():
                log.warning("Poll: window not active — starting 3s countdown")
                self._focus_timer.start()
        else:
            if self._focus_timer.isActive():
                log.info("Poll: window active again — cancelling countdown")
                self._focus_timer.stop()

    def _on_focus_timeout(self) -> None:
        if not self._active or self._focus_guard_paused:
            return
        if self._app_popup_open():
            # Sabuk pengaman terakhir: jangan pernah mengirim jawaban
            # siswa di tengah dia memilih opsi dari dropdown. Tunda satu
            # siklus; kalau fokusnya memang benar-benar lepas, popup
            # sudah tertutup sendiri saat itu dan countdown menembak
            # seperti biasa.
            log.info("Focus timeout tertunda: popup aplikasi masih terbuka")
            self._focus_timer.start()
            return
        log.warning("Focus lost timeout — triggering auto-submit")
        self.auto_submit.emit()

    # ------------------------------------------------------------------
    # Strict mode
    # ------------------------------------------------------------------

    def _activate_strict(self) -> None:
        if not self._window:
            return

        # Fullscreen frameless
        self._window.setWindowFlags(
            self._window.windowFlags()
            | Qt.FramelessWindowHint
            | Qt.WindowStaysOnTopHint
        )
        self._window.showFullScreen()

        # Platform-specific strict mode (keyboard hook on Windows,
        # X11 grabs + GNOME workspace lock on Linux)
        self._backend.set_strict_mode(self._window)

        # Kiosk session setup (Linux-only)
        if self._kiosk:
            try:
                from .kiosk import setup_kiosk_environment
                setup_kiosk_environment()
            except ImportError:
                pass
