"""PDF viewer widget using PyMuPDF (fitz)."""

from __future__ import annotations

import contextlib
import io
import os
import sys
from typing import Optional

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
    try:
        import fitz as _fitz

        tools = getattr(_fitz, "TOOLS", None)
        previous = getattr(tools, "mupdf_display_errors", None) if tools else None
        if tools is not None:
            tools.mupdf_display_errors(False)
    except Exception:
        previous = None
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


from PyQt5.QtCore import Qt, QSize, QPoint
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

        layout.addWidget(self._scroll, 1)

    def load_pdf(self, path: str) -> bool:
        """Load a PDF file. Returns True on success."""
        try:
            with _suppress_mupdf_warnings():
                self._doc = fitz.open(path)
            self._total_pages = len(self._doc)
            self._current_page = 0
            self._render_current_page()
            return True
        except Exception as e:
            self._page_label.setText(f"Gagal memuat PDF:\n{e}")
            return False

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

        with _suppress_mupdf_warnings():
            page = self._doc[self._current_page]
            mat = fitz.Matrix(self._zoom, self._zoom)
            pix = page.get_pixmap(matrix=mat, alpha=False)

        # Convert fitz pixmap to QImage
        img = QImage(pix.samples, pix.width, pix.height, pix.stride, QImage.Format_RGB888)
        pixmap = QPixmap.fromImage(img)

        self._page_label.setPixmap(pixmap)
        # Set label size hint to pixmap size so scrollbar appears
        self._page_label.setMinimumSize(pixmap.size())
        self._page_label.resize(pixmap.size())
        self._lbl_page.setText(f"Halaman {self._current_page + 1} / {self._total_pages}")
        self._lbl_zoom.setText(f"{int(self._zoom * 100)}%")

        # Update button states
        self._btn_prev.setEnabled(self._current_page > 0)
        self._btn_next.setEnabled(self._current_page < self._total_pages - 1)

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
