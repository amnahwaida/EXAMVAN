"""Theme-aware QSS stylesheets — auto-detect system dark/light mode."""

from __future__ import annotations

import os
import subprocess

from PyQt5.QtGui import QPalette, QColor
from PyQt5.QtWidgets import QApplication


def is_system_dark() -> bool:
    """Detect if the system is using a dark theme.

    Checks multiple sources (in order):
    1. GTK_THEME env var (e.g. 'Adwaita:dark')
    2. gsettings org.gnome.desktop.interface color-scheme
    3. gsettings org.gnome.desktop.interface gtk-theme
    4. Qt palette fallback
    """
    # 1. GTK_THEME env var
    gtk_theme = os.environ.get("GTK_THEME", "")
    if ":dark" in gtk_theme.lower():
        return True
    if ":light" in gtk_theme.lower():
        return False

    # 2. GNOME color-scheme setting (GNOME 42+)
    try:
        result = subprocess.run(
            ["gsettings", "get", "org.gnome.desktop.interface", "color-scheme"],
            capture_output=True, text=True, timeout=3,
        )
        scheme = result.stdout.strip().lower()
        if "prefer-dark" in scheme:
            return True
        if "prefer-light" in scheme:
            return False
    except (FileNotFoundError, subprocess.TimeoutExpired):
        pass

    # 3. GNOME gtk-theme name
    try:
        result = subprocess.run(
            ["gsettings", "get", "org.gnome.desktop.interface", "gtk-theme"],
            capture_output=True, text=True, timeout=3,
        )
        theme_name = result.stdout.strip().strip("'").lower()
        dark_keywords = ("dark", "mocha", "frappe", "macchiato")
        light_keywords = ("light", "latte")
        if any(kw in theme_name for kw in dark_keywords):
            return True
        if any(kw in theme_name for kw in light_keywords):
            return False
    except (FileNotFoundError, subprocess.TimeoutExpired):
        pass

    # 4. Qt palette fallback
    app = QApplication.instance()
    if app is not None:
        palette = app.palette()
        bg = palette.color(QPalette.Window)
        return bg.lightness() < 128

    return True  # Default to dark


def apply_theme(dark: bool = True) -> None:
    """Apply comprehensive dark or light theme to the application."""
    app = QApplication.instance()
    if app is None:
        return

    if dark:
        app.setStyleSheet(_DARK_STYLESHEET)
    else:
        app.setStyleSheet(_LIGHT_STYLESHEET)


# ---------------------------------------------------------------------------
# Dark theme (Catppuccin Mocha inspired)
# ---------------------------------------------------------------------------

_DARK_STYLESHEET = """
/* === Base === */
QWidget {
    background-color: #1e1e2e;
    color: #cdd6f4;
    font-size: 13px;
}

QMainWindow {
    background-color: #1e1e2e;
}

QDialog {
    background-color: #1e1e2e;
    color: #cdd6f4;
}

/* === Text === */
QLabel {
    color: #cdd6f4;
    background-color: transparent;
}

QToolTip {
    background-color: #313244;
    color: #cdd6f4;
    border: 1px solid #45475a;
    padding: 4px;
}

/* === Inputs === */
QLineEdit {
    background-color: #313244;
    color: #cdd6f4;
    border: 1px solid #45475a;
    border-radius: 6px;
    padding: 8px 12px;
    font-size: 14px;
    selection-background-color: #89b4fa;
}

QLineEdit:focus {
    border: 1px solid #89b4fa;
}

QLineEdit:disabled {
    background-color: #282838;
    color: #6c7086;
}

QTextEdit {
    background-color: #313244;
    color: #cdd6f4;
    border: 1px solid #45475a;
    border-radius: 6px;
}

QPlainTextEdit {
    background-color: #313244;
    color: #cdd6f4;
    border: 1px solid #45475a;
    border-radius: 6px;
}

QWidget#loginCard, QWidget#identityCard {
    background-color: #313244;
    border: 1px solid #45475a;
    border-radius: 12px;
}

QSpinBox, QDoubleSpinBox {
    background-color: #313244;
    color: #cdd6f4;
    border: 1px solid #45475a;
    border-radius: 6px;
    padding: 4px 8px;
}

/* === Buttons === */
QPushButton {
    background-color: #89b4fa;
    color: #1e1e2e;
    border: none;
    border-radius: 6px;
    padding: 10px 24px;
    font-size: 14px;
    font-weight: bold;
}

QPushButton:hover {
    background-color: #74c7ec;
}

QPushButton:pressed {
    background-color: #89dceb;
}

QPushButton:disabled {
    background-color: #45475a;
    color: #6c7086;
}

/* === Check/Radio === */
QCheckBox {
    color: #cdd6f4;
    spacing: 8px;
    background-color: transparent;
}

QCheckBox::indicator {
    width: 18px;
    height: 18px;
    border: 2px solid #89b4fa;
    border-radius: 3px;
    background-color: transparent;
}

QCheckBox::indicator:checked {
    background-color: #89b4fa;
    border: 2px solid #89b4fa;
}

QCheckBox::indicator:hover {
    border: 2px solid #b4d0fb;
}

QRadioButton {
    color: #cdd6f4;
    spacing: 8px;
    background-color: transparent;
}

QRadioButton::indicator {
    width: 18px;
    height: 18px;
    border: 2px solid #89b4fa;
    border-radius: 10px;
    background-color: transparent;
}

QRadioButton::indicator:checked {
    background-color: #89b4fa;
    border: 2px solid #89b4fa;
}

QRadioButton::indicator:hover {
    border: 2px solid #b4d0fb;
}

/* === GroupBox === */
QGroupBox {
    color: #cdd6f4;
    border: 1px solid #45475a;
    border-radius: 6px;
    margin-top: 12px;
    padding-top: 16px;
    font-weight: bold;
    background-color: transparent;
}

QGroupBox::title {
    subcontrol-origin: margin;
    left: 12px;
    padding: 0 6px;
}

/* === ComboBox === */
QComboBox {
    background-color: #313244;
    color: #cdd6f4;
    border: 1px solid #45475a;
    border-radius: 6px;
    padding: 6px 12px;
    font-size: 13px;
}

QComboBox::drop-down {
    border: none;
    width: 24px;
}

QComboBox QAbstractItemView {
    background-color: #313244;
    color: #cdd6f4;
    border: 1px solid #45475a;
    selection-background-color: #45475a;
}

/* === Progress === */
QProgressBar {
    background-color: #313244;
    border: 1px solid #45475a;
    border-radius: 6px;
    text-align: center;
    color: #cdd6f4;
    height: 20px;
}

QProgressBar::chunk {
    background-color: #89b4fa;
    border-radius: 5px;
}

/* === ScrollArea === */
QScrollArea {
    border: none;
    background-color: transparent;
}

QScrollArea > QWidget > QWidget {
    background-color: transparent;
}

/* === ScrollBar === */
QScrollBar:vertical {
    background-color: #1e1e2e;
    width: 10px;
    border-radius: 5px;
}

QScrollBar::handle:vertical {
    background-color: #45475a;
    border-radius: 5px;
    min-height: 30px;
}

QScrollBar::handle:vertical:hover {
    background-color: #585b70;
}

QScrollBar::add-line:vertical, QScrollBar::sub-line:vertical {
    height: 0;
}

QScrollBar:horizontal {
    background-color: #1e1e2e;
    height: 10px;
    border-radius: 5px;
}

QScrollBar::handle:horizontal {
    background-color: #45475a;
    border-radius: 5px;
    min-width: 30px;
}

QScrollBar::handle:horizontal:hover {
    background-color: #585b70;
}

QScrollBar::add-line:horizontal, QScrollBar::sub-line:horizontal {
    width: 0;
}

/* === Login/Identity Card === */
QWidget#loginCard, QWidget#identityCard {
    background-color: #ffffff;
    border: 1px solid #ccd0da;
    border-radius: 12px;
}

/* === Splitter === */
QSplitter::handle {
    background-color: #ccd0da;
}

QSplitter::handle:horizontal {
    width: 3px;
}

QSplitter::handle:vertical {
    height: 3px;
}

/* === MessageBox / Dialog === */
QMessageBox {
    background-color: #1e1e2e;
}

QMessageBox QLabel {
    color: #cdd6f4;
    background-color: transparent;
}

QMessageBox QPushButton {
    min-width: 80px;
}

QInputDialog {
    background-color: #1e1e2e;
}

QInputDialog QLabel {
    color: #cdd6f4;
    background-color: transparent;
}

QInputDialog QLineEdit {
    background-color: #313244;
    color: #cdd6f4;
}

/* === Menu === */
QMenuBar {
    background-color: #181825;
    color: #cdd6f4;
    border-bottom: 1px solid #313244;
}

QMenuBar::item:selected {
    background-color: #313244;
}

QMenu {
    background-color: #1e1e2e;
    color: #cdd6f4;
    border: 1px solid #45475a;
}

QMenu::item:selected {
    background-color: #45475a;
}

/* === Tab === */
QTabWidget::pane {
    border: 1px solid #45475a;
    background-color: #1e1e2e;
}

QTabBar::tab {
    background-color: #181825;
    color: #a6adc8;
    padding: 8px 16px;
    border: 1px solid #313244;
    border-bottom: none;
}

QTabBar::tab:selected {
    background-color: #1e1e2e;
    color: #cdd6f4;
}

/* === Status bar === */
QStatusBar {
    background-color: #181825;
    color: #a6adc8;
    border-top: 1px solid #313244;
}

/* === Toolbar === */
QToolBar {
    background-color: #181825;
    border: none;
}

/* === Frame (used by many containers) === */
QFrame {
    background-color: transparent;
}
"""


# ---------------------------------------------------------------------------
# Light theme (Catppuccin Latte inspired)
# ---------------------------------------------------------------------------

_LIGHT_STYLESHEET = """
/* === Base === */
QWidget {
    background-color: #eff1f5;
    color: #4c4f69;
    font-size: 13px;
}

QMainWindow {
    background-color: #eff1f5;
}

QDialog {
    background-color: #eff1f5;
    color: #4c4f69;
}

/* === Text === */
QLabel {
    color: #4c4f69;
    background-color: transparent;
}

QToolTip {
    background-color: #e6e9ef;
    color: #4c4f69;
    border: 1px solid #ccd0da;
    padding: 4px;
}

/* === Inputs === */
QLineEdit {
    background-color: #ffffff;
    color: #4c4f69;
    border: 1px solid #ccd0da;
    border-radius: 6px;
    padding: 8px 12px;
    font-size: 14px;
    selection-background-color: #1e66f5;
}

QLineEdit:focus {
    border: 1px solid #1e66f5;
}

QLineEdit:disabled {
    background-color: #e6e9ef;
    color: #8c8fa1;
}

QTextEdit {
    background-color: #ffffff;
    color: #4c4f69;
    border: 1px solid #ccd0da;
    border-radius: 6px;
}

QPlainTextEdit {
    background-color: #ffffff;
    color: #4c4f69;
    border: 1px solid #ccd0da;
    border-radius: 6px;
}

QSpinBox, QDoubleSpinBox {
    background-color: #ffffff;
    color: #4c4f69;
    border: 1px solid #ccd0da;
    border-radius: 6px;
    padding: 4px 8px;
}

/* === Buttons === */
QPushButton {
    background-color: #1e66f5;
    color: #ffffff;
    border: none;
    border-radius: 6px;
    padding: 10px 24px;
    font-size: 14px;
    font-weight: bold;
}

QPushButton:hover {
    background-color: #2a7ae9;
}

QPushButton:pressed {
    background-color: #1558d4;
}

QPushButton:disabled {
    background-color: #ccd0da;
    color: #8c8fa1;
}

/* === Check/Radio === */
QCheckBox {
    color: #4c4f69;
    spacing: 8px;
    background-color: transparent;
}

QCheckBox::indicator {
    width: 18px;
    height: 18px;
    border: 2px solid #1e66f5;
    border-radius: 3px;
    background-color: transparent;
}

QCheckBox::indicator:checked {
    background-color: #1e66f5;
    border: 2px solid #1e66f5;
}

QCheckBox::indicator:hover {
    border: 2px solid #2a7ae9;
}

QRadioButton {
    color: #4c4f69;
    spacing: 8px;
    background-color: transparent;
}

QRadioButton::indicator {
    width: 18px;
    height: 18px;
    border: 2px solid #1e66f5;
    border-radius: 10px;
    background-color: transparent;
}

QRadioButton::indicator:checked {
    background-color: #1e66f5;
    border: 2px solid #1e66f5;
}

QRadioButton::indicator:hover {
    border: 2px solid #2a7ae9;
}

/* === GroupBox === */
QGroupBox {
    color: #4c4f69;
    border: 1px solid #ccd0da;
    border-radius: 6px;
    margin-top: 12px;
    padding-top: 16px;
    font-weight: bold;
    background-color: transparent;
}

QGroupBox::title {
    subcontrol-origin: margin;
    left: 12px;
    padding: 0 6px;
}

/* === ComboBox === */
QComboBox {
    background-color: #ffffff;
    color: #4c4f69;
    border: 1px solid #ccd0da;
    border-radius: 6px;
    padding: 6px 12px;
    font-size: 13px;
}

QComboBox::drop-down {
    border: none;
    width: 24px;
}

QComboBox QAbstractItemView {
    background-color: #ffffff;
    color: #4c4f69;
    border: 1px solid #ccd0da;
    selection-background-color: #e6e9ef;
}

/* === Progress === */
QProgressBar {
    background-color: #e6e9ef;
    border: 1px solid #ccd0da;
    border-radius: 6px;
    text-align: center;
    color: #4c4f69;
    height: 20px;
}

QProgressBar::chunk {
    background-color: #1e66f5;
    border-radius: 5px;
}

/* === ScrollArea === */
QScrollArea {
    border: none;
    background-color: transparent;
}

QScrollArea > QWidget > QWidget {
    background-color: transparent;
}

/* === ScrollBar === */
QScrollBar:vertical {
    background-color: #eff1f5;
    width: 10px;
    border-radius: 5px;
}

QScrollBar::handle:vertical {
    background-color: #ccd0da;
    border-radius: 5px;
    min-height: 30px;
}

QScrollBar::handle:vertical:hover {
    background-color: #bcc0cc;
}

QScrollBar::add-line:vertical, QScrollBar::sub-line:vertical {
    height: 0;
}

QScrollBar:horizontal {
    background-color: #eff1f5;
    height: 10px;
    border-radius: 5px;
}

QScrollBar::handle:horizontal {
    background-color: #ccd0da;
    border-radius: 5px;
    min-width: 30px;
}

QScrollBar::handle:horizontal:hover {
    background-color: #bcc0cc;
}

QScrollBar::add-line:horizontal, QScrollBar::sub-line:horizontal {
    width: 0;
}

/* === Splitter === */
QSplitter::handle {
    background-color: #ccd0da;
}

QSplitter::handle:horizontal {
    width: 3px;
}

QSplitter::handle:vertical {
    height: 3px;
}

/* === MessageBox / Dialog === */
QMessageBox {
    background-color: #eff1f5;
}

QMessageBox QLabel {
    color: #4c4f69;
    background-color: transparent;
}

QMessageBox QPushButton {
    min-width: 80px;
}

QInputDialog {
    background-color: #eff1f5;
}

QInputDialog QLabel {
    color: #4c4f69;
    background-color: transparent;
}

QInputDialog QLineEdit {
    background-color: #ffffff;
    color: #4c4f69;
}

/* === Menu === */
QMenuBar {
    background-color: #e6e9ef;
    color: #4c4f69;
    border-bottom: 1px solid #ccd0da;
}

QMenuBar::item:selected {
    background-color: #ccd0da;
}

QMenu {
    background-color: #eff1f5;
    color: #4c4f69;
    border: 1px solid #ccd0da;
}

QMenu::item:selected {
    background-color: #ccd0da;
}

/* === Tab === */
QTabWidget::pane {
    border: 1px solid #ccd0da;
    background-color: #eff1f5;
}

QTabBar::tab {
    background-color: #e6e9ef;
    color: #6c6f85;
    padding: 8px 16px;
    border: 1px solid #ccd0da;
    border-bottom: none;
}

QTabBar::tab:selected {
    background-color: #eff1f5;
    color: #4c4f69;
}

/* === Status bar === */
QStatusBar {
    background-color: #e6e9ef;
    color: #6c6f85;
    border-top: 1px solid #ccd0da;
}

/* === Toolbar === */
QToolBar {
    background-color: #e6e9ef;
    border: none;
}

/* === Frame === */
QFrame {
    background-color: transparent;
}
"""


# ---------------------------------------------------------------------------
# Security banner colors (shared between themes)
# ---------------------------------------------------------------------------

SECURITY_COLORS = {
    "low": ("#455A64", "#FFFFFF"),
    "medium": ("#D32F2F", "#FFFFFF"),
    "strict": ("#B71C1C", "#FFD700"),
}
