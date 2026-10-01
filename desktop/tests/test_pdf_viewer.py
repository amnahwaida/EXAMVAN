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


if __name__ == "__main__":
    unittest.main()
