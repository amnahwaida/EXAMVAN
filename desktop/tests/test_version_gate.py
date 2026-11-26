"""Desktop version gate: health payload + comparison + UI blocking."""

from __future__ import annotations

import os
import shutil
import tempfile
import unittest
from pathlib import Path
from unittest import mock

os.environ.setdefault("QT_QPA_PLATFORM", "offscreen")

from PyQt5.QtWidgets import QApplication, QMessageBox

from examvan import APP_VERSION, config
from examvan.models import Exam, HealthResponse, TokenExamResponse
from examvan.ui.server_config import ServerConfigDialog
from examvan.versioning import compare_versions, is_version_outdated

APP = QApplication.instance() or QApplication([])


class VersionCompareTest(unittest.TestCase):
    def test_padding_and_prefixes(self):
        self.assertEqual(compare_versions("2.7", "2.7.0"), 0)
        self.assertEqual(compare_versions("v2.7.20", "2.7.20"), 0)
        self.assertEqual(compare_versions("2.7.19", "2.7.20"), -1)
        self.assertEqual(compare_versions("2.10.0", "2.9.9"), 1)
        self.assertEqual(compare_versions("2.x.1", "2.0.1"), 0)

    def test_outdated_requires_a_real_requirement(self):
        self.assertFalse(is_version_outdated("2.7.20", ""))
        self.assertFalse(is_version_outdated("2.7.20", "2.7.20"))
        self.assertTrue(is_version_outdated("2.7.19", "2.7.20"))


class HealthPayloadTest(unittest.TestCase):
    def test_desktop_requirement_is_parsed(self):
        health = HealthResponse.from_json(
            {"success": True, "required_desktop_version": "2.7.20"}
        )
        self.assertEqual(health.required_desktop_version, "2.7.20")

    def test_android_requirement_is_not_used(self):
        health = HealthResponse.from_json(
            {"success": True, "required_app_version": "9.9.9"}
        )
        self.assertIsNone(health.required_desktop_version)


class _Sandbox(unittest.TestCase):
    def setUp(self):
        self._tmp = tempfile.mkdtemp(prefix="examvan-version-gate-")
        self._dir_patch = mock.patch.object(
            config, "_CONFIG_DIR", Path(self._tmp))
        self._file_patch = mock.patch.object(
            config, "_CONFIG_FILE", Path(self._tmp) / "config.json")
        self._dir_patch.start()
        self._file_patch.start()
        config._cache = None
        self.addCleanup(self._dir_patch.stop)
        self.addCleanup(self._file_patch.stop)
        self.addCleanup(shutil.rmtree, self._tmp, True)
        self.addCleanup(setattr, config, "_cache", None)

    def _dialog(self):
        dlg = ServerConfigDialog()
        self.addCleanup(dlg.deleteLater)
        try:
            dlg._sig_show_identity.disconnect(dlg._show_identity_dialog)
        except (TypeError, RuntimeError):
            pass
        return dlg


class DesktopVersionGateUiTest(_Sandbox):
    def test_outdated_desktop_is_blocked_before_token_lookup(self):
        dlg = self._dialog()
        health = HealthResponse(
            success=True, required_desktop_version="99.0.0",
        )
        lookup = mock.Mock(return_value=TokenExamResponse(
            success=True, exam=Exam(id=1, name="U", status="active")))
        boxes = []
        with mock.patch(
            "examvan.ui.server_config.api.check_health",
            return_value=health,
        ), mock.patch(
            "examvan.ui.server_config.api.get_exam_by_token", lookup,
        ), mock.patch.object(
            QMessageBox, "exec_",
            lambda box: boxes.append(
                (box.windowTitle(), box.text(), box.informativeText())),
        ):
            dlg._connect_thread("https://exam.example", "ABCD1234")
            APP.processEvents()
        lookup.assert_not_called()
        self.assertEqual(len(boxes), 1)
        self.assertIn("Versi", boxes[0][0])
        self.assertIn("99.0.0", boxes[0][2])

    def test_current_desktop_is_not_blocked(self):
        dlg = self._dialog()
        health = HealthResponse(
            success=True, required_desktop_version=APP_VERSION,
        )
        lookup = mock.Mock(return_value=TokenExamResponse(success=True, exam=None))
        with mock.patch(
            "examvan.ui.server_config.api.check_health",
            return_value=health,
        ), mock.patch(
            "examvan.ui.server_config.api.get_exam_by_token", lookup,
        ):
            dlg._connect_thread("https://exam.example", "ABCD1234")
        lookup.assert_called_once()

    def test_android_only_requirement_does_not_block_desktop(self):
        dlg = self._dialog()
        health = HealthResponse(
            success=True, required_app_version="99.0.0",
        )
        lookup = mock.Mock(return_value=TokenExamResponse(success=True, exam=None))
        with mock.patch(
            "examvan.ui.server_config.api.check_health",
            return_value=health,
        ), mock.patch(
            "examvan.ui.server_config.api.get_exam_by_token", lookup,
        ):
            dlg._connect_thread("https://exam.example", "ABCD1234")
        lookup.assert_called_once()
