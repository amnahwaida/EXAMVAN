"""PDF viewer widget using PyMuPDF (fitz)."""

from __future__ import annotations

import contextlib
import logging
import os
import sys
from typing import Optional

# Bujet render ~12 megapiksel: halaman besar pada zoom tinggi bisa menjadi
# ratusan MB sebagai QImage dan OOM di PC lab — zoom efektif dikecilkan
# agar hasil render tidak melewati batas ini.
_MAX_RENDER_PIXELS = 12_000_000

# Suppress MuPDF warnings before importing fitz
os.environ["MUPDF_LOG_LEVEL"] = "E"
import fitz  # PyMuPDF


@contextlib.contextmanager
def _suppress_mupdf_warnings():
    """Silence MuPDF's own diagnostic output for the duration of the block.

    Previously this redirected the PROCESS stderr file descriptor with
    a `os.dup2()`-style redirect of file descriptor 2. That is global,
    not thread-safe, and not
    reentrant: two overlapping calls make the inner `finally` restore a file
    descriptor that already points at devnull, and stderr is then lost for
    the rest of the process. Nothing on the GUI thread should be writing to
    fd 2, but a log line from any other thread during a page render would
    simply disappear.

    `fitz.TOOLS.mupdf_display_errors` is the supported switch and only
    affects MuPDF's own output.
    """
    # `previous` diinisialisasi DULUAN dan blok except TIDAK menyentuhnya:
    # kalau `mupdf_display_errors(False)` sendiri melempar SETELAH previous
    # terbaca, menimpanya dengan None akan membuat finally me-restore
    # "tidak tahu" dan peringatan MuPDF mati selamanya untuk proses ini.
    previous = None
    try:
        import fitz as _fitz

        tools = getattr(_fitz, "TOOLS", None)
        if tools is not None:
            previous = getattr(tools, "mupdf_display_errors", None)
            tools.mupdf_display_errors(False)
    except Exception:
        pass
    try:
        yield
    finally:
        try:
            import fitz as _fitz

            tools = getattr(_fitz, "TOOLS", None)
            if tools is not None and previous is not None:
                tools.mupdf_display_errors(previous)
        except Exception:
            pass


from PyQt5.QtCore import Qt, QSize, QPoint, QEvent
from PyQt5.QtGui import QImage, QPixmap, QMouseEvent
from PyQt5.QtWidgets import (
    QLabel,
    QPushButton,
    QHBoxLayout,
    QVBoxLayout,
    QWidget,
    QScrollArea,
    QSizePolicy,
)


class _DragScrollLabel(QLabel):
    """QLabel that supports left-click drag to scroll the parent QScrollArea."""

    def __init__(self, parent=None):
        super().__init__(parent)
        self._dragging = False
        self._drag_start = QPoint()
        self._scroll_start = QPoint()
        self.setCursor(Qt.OpenHandCursor)

    def mousePressEvent(self, event: QMouseEvent) -> None:
        if event.button() == Qt.LeftButton:
            self._dragging = True
            self._drag_start = event.globalPos()
            scroll = self._find_scroll_area()
            if scroll:
                h = scroll.horizontalScrollBar()
                v = scroll.verticalScrollBar()
                self._scroll_start = QPoint(h.value(), v.value())
            self.setCursor(Qt.ClosedHandCursor)
            event.accept()
        else:
            super().mousePressEvent(event)

    def mouseMoveEvent(self, event: QMouseEvent) -> None:
        if self._dragging:
            delta = event.globalPos() - self._drag_start
            scroll = self._find_scroll_area()
            if scroll:
                h = scroll.horizontalScrollBar()
                v = scroll.verticalScrollBar()
                h.setValue(self._scroll_start.x() - delta.x())
                v.setValue(self._scroll_start.y() - delta.y())
            event.accept()
        else:
            super().mouseMoveEvent(event)

    def mouseReleaseEvent(self, event: QMouseEvent) -> None:
        if event.button() == Qt.LeftButton and self._dragging:
            self._dragging = False
            self.setCursor(Qt.OpenHandCursor)
            event.accept()
        else:
            super().mouseReleaseEvent(event)

    def _find_scroll_area(self):
        p = self.parent()
        while p:
            if isinstance(p, QScrollArea):
                return p
            p = p.parent()
        return None


class PdfWidget(QWidget):
    """PDF page viewer with zoom and navigation."""

    def __init__(self, panel_color: str = "#6366f1", parent=None):
        super().__init__(parent)
        self._panel_color = panel_color
        self._doc: Optional[fitz.Document] = None
        self._current_page = 0
        self._total_pages = 0
        self._zoom = 1.5  # Default zoom scale
        self._setup_ui()

    def _setup_ui(self) -> None:
        layout = QVBoxLayout(self)
        layout.setContentsMargins(0, 0, 0, 0)
        layout.setSpacing(4)

        pc = self._panel_color
        btn_style = (f"QPushButton {{ background-color: {pc}; color: #ffffff; "
                     f"border: none; border-radius: 4px; font-weight: bold; }}")

        # Navigation bar
        nav = QHBoxLayout()
        nav.setSpacing(8)

        self._btn_prev = QPushButton("◀ Sebelumnya")
        self._btn_prev.setFixedHeight(32)
        self._btn_prev.setStyleSheet(btn_style)
        self._btn_prev.clicked.connect(self.prev_page)
        nav.addWidget(self._btn_prev)

        self._lbl_page = QLabel("Halaman 0 / 0")
        self._lbl_page.setAlignment(Qt.AlignCenter)
        self._lbl_page.setStyleSheet("font-weight: bold;")
        nav.addWidget(self._lbl_page, 1)

        self._btn_next = QPushButton("Berikutnya ▶")
        self._btn_next.setFixedHeight(32)
        self._btn_next.setStyleSheet(btn_style)
        self._btn_next.clicked.connect(self.next_page)
        nav.addWidget(self._btn_next)

        layout.addLayout(nav)

        # Zoom controls — icon buttons
        zoom_layout = QHBoxLayout()
        zoom_layout.setSpacing(4)

        btn_zoom_out = QPushButton("−")
        btn_zoom_out.setFixedSize(32, 32)
        btn_zoom_out.setStyleSheet(btn_style)
        btn_zoom_out.clicked.connect(self.zoom_out)
        zoom_layout.addWidget(btn_zoom_out)

        self._lbl_zoom = QLabel("150%")
        self._lbl_zoom.setFixedWidth(50)
        self._lbl_zoom.setAlignment(Qt.AlignCenter)
        self._lbl_zoom.setStyleSheet("font-size: 11px;")
        zoom_layout.addWidget(self._lbl_zoom)

        btn_zoom_in = QPushButton("+")
        btn_zoom_in.setFixedSize(32, 32)
        btn_zoom_in.setStyleSheet(btn_style)
        btn_zoom_in.clicked.connect(self.zoom_in)
        zoom_layout.addWidget(btn_zoom_in)

        zoom_layout.addStretch()
        layout.addLayout(zoom_layout)

        # Page display in scroll area
        self._scroll = QScrollArea()
        self._scroll.setWidgetResizable(False)
        self._scroll.setAlignment(Qt.AlignCenter)
        self._scroll.setObjectName("pdfScrollArea")
        self._scroll.setStyleSheet("#pdfScrollArea { border: none; }")

        self._page_label = _DragScrollLabel()
        self._page_label.setAlignment(Qt.AlignCenter)
        self._scroll.setWidget(self._page_label)
        # Viewport QScrollArea memakan wheel event sebelum sampai ke
        # `wheelEvent` widget — tanpa ini Ctrl+scroll zoom mati total.
        self._scroll.viewport().installEventFilter(self)

        layout.addWidget(self._scroll, 1)

    def eventFilter(self, obj, event) -> bool:
        # Intersepsi wheel di viewport: Ctrl+scroll = zoom, selainnya
        # teruskan ke scroll normal.
        if obj is self._scroll.viewport() and event.type() == QEvent.Wheel:
            if event.modifiers() & Qt.ControlModifier:
                delta = event.angleDelta().y()
                if delta > 0:
                    self.zoom_in()
                elif delta < 0:
                    self.zoom_out()
                return True
        return super().eventFilter(obj, event)

    def load_pdf(self, path: str) -> bool:
        """Load a PDF file. Returns True on success."""
        try:
            with _suppress_mupdf_warnings():
                doc = fitz.open(path)
            # PDF terkunci (password): objek terbuka tapi halaman tidak
            # bisa di-render — gagalkan di awal dengan pesan yang bisa
            # ditindaklanjuti, bukan placeholder "gagal merender" per
            # halaman. `needs_pass` = password diminta; `is_encrypted`
            # cadangan untuk varian API lama.
            if getattr(doc, "needs_pass", False) or getattr(doc, "is_encrypted", False):
                try:
                    doc.close()
                except Exception:
                    pass
                self._fail_load("PDF terkunci — hubungi pengawas")
                return False
            if self._doc is not None:
                try:
                    self._doc.close()
                except Exception:
                    pass
            self._doc = doc
            self._total_pages = len(self._doc)
            self._current_page = 0
            self._render_current_page()
            return True
        except Exception as e:
            self._fail_load(f"Gagal memuat PDF:\n{e}")
            return False

    def _fail_load(self, message: str) -> None:
        """Reset state ke kosong + matikan navigasi + tampilkan pesan.

        Tanpa ini, PDF gagal-load meninggalkan total_pages/navigasi dari
        dokumen SEBELUMNYA — tombol aktif tapi tidak ada yang di-render.
        """
        if self._doc is not None:
            try:
                self._doc.close()
            except Exception:
                pass
        self._doc = None
        self._total_pages = 0
        self._current_page = 0
        self._page_label.clear()
        self._page_label.setText(message)
        self._lbl_page.setText("Halaman 0 / 0")
        self._btn_prev.setEnabled(False)
        self._btn_next.setEnabled(False)

    def prev_page(self) -> None:
        if self._current_page > 0:
            self._current_page -= 1
            self._render_current_page()

    def next_page(self) -> None:
        if self._current_page < self._total_pages - 1:
            self._current_page += 1
            self._render_current_page()

    def go_to_page(self, page: int) -> None:
        if 0 <= page < self._total_pages:
            self._current_page = page
            self._render_current_page()

    def zoom_in(self) -> None:
        self._zoom = min(self._zoom + 0.25, 4.0)
        self._render_current_page()

    def zoom_out(self) -> None:
        self._zoom = max(self._zoom - 0.25, 0.5)
        self._render_current_page()

    def _render_current_page(self) -> None:
        if not self._doc or self._total_pages == 0:
            return

        # Audit 2 Okt 2026 (HIGH H11): get_pixmap() dapat gagal jika dokumen
        # rusak, halaman kosong, atau MuPDF lepas dari memori. Sekantan sebelumnya
        # melempar ke event loop Qt → aplikasi crash saat navigasi halaman. Bungkam
        # dan tampilkan placeholder agar ujian tetap berlanjut.
        try:
            with _suppress_mupdf_warnings():
                page = self._doc[self._current_page]
                zoom = self._zoom
                # Bujet piksel ~12MP: halaman besar pada zoom tinggi bisa
                # menjadi ratusan MB sebagai QImage dan OOM di PC lab.
                try:
                    rect = page.rect
                    area = rect.width * rect.height * zoom * zoom
                    if area > _MAX_RENDER_PIXELS:
                        zoom *= (_MAX_RENDER_PIXELS / area) ** 0.5
                except Exception:
                    zoom = self._zoom
                mat = fitz.Matrix(zoom, zoom)
                pix = page.get_pixmap(matrix=mat, alpha=False)

            # Convert fitz pixmap to QImage
            img = QImage(pix.samples, pix.width, pix.height, pix.stride, QImage.Format_RGB888)
            pixmap = QPixmap.fromImage(img)

            self._page_label.setPixmap(pixmap)
            # Set label size hint to pixmap size so scrollbar appears
            self._page_label.setMinimumSize(pixmap.size())
            self._page_label.resize(pixmap.size())
            self._lbl_page.setText(f"Halaman {self._current_page + 1} / {self._total_pages}")
            if zoom < self._zoom:
                self._lbl_zoom.setText(f"{int(self._zoom * 100)}% (dibatasi)")
            else:
                self._lbl_zoom.setText(f"{int(self._zoom * 100)}%")

            # Update button states
            self._btn_prev.setEnabled(self._current_page > 0)
            self._btn_next.setEnabled(self._current_page < self._total_pages - 1)
        except Exception as e:
            logging.getLogger(__name__).warning(
                "Gagal render halaman %d: %s", self._current_page, e
            )
            # Bersihkan pixmap yang mungkin masih terpasang, lalu
            # tunjukkan teks placeholder. clear()+setText memastikan
            # label menampilkan teks, bukan pixmap kosong.
            self._page_label.clear()
            self._page_label.setText(
                f"Halaman {self._current_page + 1} / {self._total_pages}\n"
                "(gagal merender)"
            )

    def wheelEvent(self, event) -> None:
        """Ctrl+scroll to zoom, otherwise scroll normally."""
        if event.modifiers() & Qt.ControlModifier:
            delta = event.angleDelta().y()
            if delta > 0:
                self.zoom_in()
            elif delta < 0:
                self.zoom_out()
            event.accept()
        else:
            super().wheelEvent(event)

    @property
    def total_pages(self) -> int:
        return self._total_pages

    def cleanup(self) -> None:
        if self._doc:
            self._doc.close()
            self._doc = None
