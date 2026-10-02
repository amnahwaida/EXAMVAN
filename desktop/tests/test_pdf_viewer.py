"""Tests for PDF viewer render error handling (HIGH H11).

Verifies that _render_current_page catches exceptions during get_pixmap()
instead of crashing the GUI event loop.
"""

from __future__ import annotations

import unittest
from unittest import mock

from PyQt5.QtWidgets import QApplication

# Need a QApplication for QWidget operations.
APP = QApplication.instance() or QApplication([])

from examvan.ui import pdf_viewer as pv
from examvan.ui.pdf_viewer import PdfWidget


class _FakePixmap:
    width = 100
    height = 50
    stride = 300
    samples = b"\x00" * 300


class _FakePage:
    def get_pixmap(self, *args, **kwargs):
        return _FakePixmap()


class _FakeDoc:
    def __init__(self, page_count=1):
        self._pages = [_FakePage() for _ in range(page_count)]
        self._len = page_count

    def __getitem__(self, idx):
        return self._pages[idx]

    def __len__(self):
        return self._len


class RenderCurrentPageErrorTest(unittest.TestCase):
    """_render_current_page must not propagate exceptions from get_pixmap."""

    def setUp(self):
        self.widget = PdfWidget()

    def test_get_pixmap_failure_shows_placeholder(self):
        """If get_pixmap raises, the page label shows a placeholder and no
        crash propagates."""
        # Replace _doc with a mock that raises on __getitem__.
        self.widget._doc = mock.Mock()
        self.widget._doc.__getitem__ = mock.Mock(side_effect=RuntimeError("pdf broken"))
        self.widget._total_pages = 1
        self.widget._current_page = 0

        # Should NOT raise.
        self.widget._render_current_page()

        # Pixmap cleared, placeholder text shown.
        self.assertIsNone(
            self.widget._page_label.pixmap(),
            "pixmap should be cleared on error",
        )
        self.assertIn("gagal", self.widget._page_label.text().lower())

    def test_render_success_sets_pixmap(self):
        """Happy path: pixmap rendered and page info updated."""
        self.widget._doc = _FakeDoc()
        self.widget._total_pages = 1
        self.widget._current_page = 0

        with mock.patch.object(pv, "fitz") as mock_fitz:
            mock_fitz.Matrix = mock.Mock(return_value=mock.Mock())
            self.widget._render_current_page()

        self.assertIsNotNone(self.widget._page_label.pixmap())
        self.assertIn("Halaman", self.widget._lbl_page.text())

    def test_empty_doc_returns_early(self):
        """No doc → early return, no crash."""
        self.widget._doc = None
        self.widget._total_pages = 0
        self.widget._render_current_page()
        self.assertIsNone(self.widget._page_label.pixmap())

    def test_exception_does_not_clear_saved_state(self):
        """Even if rendering fails, the widget should remain usable —
        zoom/page values unchanged."""
        self.widget._doc = mock.Mock()
        self.widget._doc.__getitem__ = mock.Mock(side_effect=ValueError("boom"))
        self.widget._total_pages = 5
        self.widget._current_page = 2
        self.widget._zoom = 2.0

        self.widget._render_current_page()

        self.assertEqual(self.widget._current_page, 2)
        self.assertEqual(self.widget._zoom, 2.0)


class LoadPdfFailureStateTest(unittest.TestCase):
    """load_pdf yang gagal mereset state + mematikan navigasi."""

    def setUp(self):
        self.widget = PdfWidget()

    def test_failed_load_resets_pages_and_disables_navigation(self):
        self.widget._total_pages = 3
        self.widget._btn_prev.setEnabled(True)
        self.widget._btn_next.setEnabled(True)
        with mock.patch.object(pv, "fitz") as mock_fitz:
            mock_fitz.open.side_effect = RuntimeError("bukan PDF")
            ok = self.widget.load_pdf("/tmp/bukan-pdf.pdf")
        self.assertFalse(ok)
        self.assertEqual(self.widget.total_pages, 0)
        self.assertFalse(self.widget._btn_prev.isEnabled())
        self.assertFalse(self.widget._btn_next.isEnabled())
        self.assertIn("Halaman 0 / 0", self.widget._lbl_page.text())

    def test_locked_pdf_is_refused_with_a_guardian_message(self):
        doc = mock.Mock()
        doc.needs_pass = True
        doc.is_encrypted = True
        with mock.patch.object(pv, "fitz") as mock_fitz:
            mock_fitz.open.return_value = doc
            ok = self.widget.load_pdf("/tmp/terkunci.pdf")
        self.assertFalse(ok)
        self.assertEqual(self.widget.total_pages, 0)
        self.assertIn("terkunci", self.widget._page_label.text().lower())
        doc.close.assert_called_once()

    def test_huge_page_clamps_zoom_with_a_hint(self):
        """Halaman raksasa: zoom efektif dibatasi ~12MP + ada hint."""
        page = mock.Mock()
        rect = mock.Mock()
        rect.width = 10000.0
        rect.height = 10000.0
        page.rect = rect
        page.get_pixmap.return_value = _FakePixmap()
        doc = mock.Mock()
        doc.__getitem__ = mock.Mock(return_value=page)
        self.widget._doc = doc
        self.widget._total_pages = 1
        self.widget._current_page = 0
        self.widget._zoom = 4.0
        with mock.patch.object(pv, "fitz") as mock_fitz:
            mock_fitz.Matrix = mock.Mock(return_value=mock.Mock())
            self.widget._render_current_page()
            mat_arg = mock_fitz.Matrix.call_args.args
        # Zoom efektif < zoom yang diminta (10000*10000*16 >> 12MP).
        self.assertLess(mat_arg[0], 4.0)
        self.assertIn("dibatasi", self.widget._lbl_zoom.text())

    def test_ctrl_wheel_on_the_viewport_zooms(self):
        from PyQt5.QtCore import QPoint, Qt
        from PyQt5.QtGui import QWheelEvent

        self.widget._doc = _FakeDoc()
        self.widget._total_pages = 1
        start = self.widget._zoom
        event = QWheelEvent(
            QPoint(10, 10), QPoint(10, 10),
            QPoint(0, 0), QPoint(0, 120),
            Qt.NoButton, Qt.ControlModifier, Qt.NoScrollPhase, False,
        )
        with mock.patch.object(pv, "fitz") as mock_fitz:
            mock_fitz.Matrix = mock.Mock(return_value=mock.Mock())
            handled = self.widget.eventFilter(
                self.widget._scroll.viewport(), event)
        self.assertTrue(handled)
        self.assertGreater(self.widget._zoom, start)

    def test_no_stale_io_import(self):
        import ast
        from pathlib import Path

        src = Path(pv.__file__).read_text(encoding="utf-8")
        tree = ast.parse(src)
        imported = set()
        for node in ast.walk(tree):
            if isinstance(node, ast.Import):
                imported.update(a.asname or a.name for a in node.names)
            elif isinstance(node, ast.ImportFrom):
                imported.update(a.asname or a.name for a in node.names)
        for name in ("io",):
            if name in imported:
                self.assertIn(f"{name}.", src,
                              f"import {name} tidak dipakai")


if __name__ == "__main__":
    unittest.main()
