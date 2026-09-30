"""Prototype Win32 harus benar-benar terpasang setelah _bind().

Bug yang menutup file ini
-------------------------
`_bind()` hanya meng-assign `_user32`, `_kernel32`, `_advapi32` lalu
selesai. Semua ~17 prototype ada di modul `__getattr__` SETELAH
`raise AttributeError`, jadi tidak pernah dieksekusi, dan `_bound` tidak
pernah jadi True.

Akibatnya di setiap PC Windows:
  * keyboard hook (Alt+Tab, Win, Ctrl+Shift+Esc) tidak terpasang
  * capture protection / anti-screenshot tidak aktif
  * prevent_sleep tidak aktif
  * clipboard Win32 tidak pernah dipanggil
  * deteksi multi-monitor selalu False

Semua gagal dengan `NameError` yang tertelan `log.warning`, dan baner
tetap menulis "STRICT". Tidak ada error yang sampai ke siswa atau ke log
pada level INFO.

Kenapa test suite tidak menangkapnya
------------------------------------
* Test Windows yang ada meng-mock atribut yang tidak ada
  (`mock.patch.object(wb, "_OpenClipboard", ..., create=True)`).
  `create=True` berarti test MENGARANG nama yang hilang, jadi justru
 -that membuat test hijau.
* `ImportabilityTestCase` justru menegakkan `assertFalse(wb._bound)` --
  suite menganggap modul tidak ter-bind itu benar.
* Di Linux `ctypes.windll` tidak ada, jadi `_bind()` tidak pernah jalan
  tanpa stub.

Test di sini menutup semua itu: ia mengarang `ctypes.windll` yang
BERHASIL, memanggil `_bind()`, lalu memeriksa setiap prototype benar-benar
ada sebagai global -- persis yang terjadi di PC Windows.
"""

from __future__ import annotations

import ctypes
import sys
import types
import unittest
from unittest import mock

from examvan.security import windows_backend as wb


# Nama prototype yang WAJIB ada setelah _bind(). Daftar ini bukan
# catatan: kalau ada yang hilang, proteksinya mati diam-diam.
REQUIRED_PROTOTYPES = [
    # keyboard hook
    "_SetWindowsHookExW", "_CallNextHookEx", "_UnhookWindowsHookEx",
    "_GetMessageW", "_PostThreadMessageW",
    # clipboard
    "_OpenClipboard", "_EmptyClipboard", "_CloseClipboard",
    # sleep
    "_SetThreadExecutionState",
    # anti-screenshot
    "_SetWindowDisplayAffinity",
    # window style
    "_GetWindowLongW", "_SetWindowLongW",
    # screen saver
    "_SystemParametersInfoW",
    # registry
    "_RegOpenKeyExW", "_RegQueryValueExW", "_RegCloseKey",
    # multi-monitor
    "_GetSystemMetrics",
]


def _fake_windll() -> types.SimpleNamespace:
    """windll yang mengembalikan objek callable untuk semua fungsi Win32."""
    def lib() -> types.SimpleNamespace:
        def _make(name):
            def fn(*_a, **_k):
                return 0
            fn.__name__ = name
            return fn
        return types.SimpleNamespace(
            **{n: _make(n) for n in [
                "SetWindowsHookExW", "CallNextHookEx", "UnhookWindowsHookEx",
                "GetMessageW", "PostThreadMessageW", "OpenClipboard",
                "EmptyClipboard", "CloseClipboard", "SetThreadExecutionState",
                "SetWindowDisplayAffinity", "GetWindowLongW", "SetWindowLongW",
                "SystemParametersInfoW", "RegOpenKeyExW", "RegQueryValueExW",
                "RegCloseKey", "GetSystemMetrics",
            ]}
        )
    return types.SimpleNamespace(user32=lib(), kernel32=lib(), advapi32=lib())


class BindInstallsPrototypesTest(unittest.TestCase):
    def setUp(self):
        # Windll palsu, jadi _bind() benar-benar jalan seperti di Windows.
        # Di Linux `ctypes.windll` tidak ada sama sekali -- itu justru
        # yang membuat semua test Win32 di repo ini bisa hijau tanpa pernah
        # menguji apa pun.
        self._bound_before = wb._bound
        wb._bound = False
        for name in REQUIRED_PROTOTYPES:
            if name in wb.__dict__:
                delattr(wb, name)

    def tearDown(self):
        for name in REQUIRED_PROTOTYPES:
            wb.__dict__.pop(name, None)
        wb._bound = self._bound_before

    def _bind_with_fake_windll(self):
        with mock.patch.object(ctypes, "windll", _fake_windll(), create=True):
            wb._bind()

    def test_bind_marks_the_module_as_bound(self):
        # Tanpa ini, __getattr__ mencoba _bind() lagi setiap akses dan
        # tidak pernah menemukan prototype.
        self._bind_with_fake_windll()
        self.assertTrue(
            wb._bound,
            "_bound tidak pernah di-set True: setiap pemanggilan Win32 akan "
            "menyerah di AttributeError dan tidak pernah memasang apa pun",
        )

    def test_every_prototype_exists_after_bind(self):
        self._bind_with_fake_windll()
        missing = [
            n for n in REQUIRED_PROTOTYPES
            if n not in wb.__dict__
        ]
        self.assertEqual(
            missing, [],
            f"prototype hilang setelah _bind(): {missing}. Setiap yang "
            f"hilang = satu proteksi Windows yang mati diam-diam.",
        )

    def test_prototypes_are_reachable_through_getattr(self):
        # Akses normal di call site adalah nama telanjang, yang Cara
        # bytecode-nya lewat module globals -- bukan lewat __getattr__
        # (yang hanya dipanggil untuk atribut yang benar-benar hilang).
        # Jadi yang diuji di sini: setelah bind, nama-nama benar-benar
        # ada sebagai global.
        self._bind_with_fake_windll()
        for name in REQUIRED_PROTOTYPES:
            with self.subTest(name=name):
                self.assertIsNotNone(
                    getattr(wb, name),
                    f"{name} tidak ada sebagai global setelah _bind()",
                )

    def test_keyboard_hook_can_actually_be_installed(self):
        # Uji AKHIR: panggil fitur yang paling penting dengan windll palsu
        # yang sukses. Sebelumnya ini gagal dengan
        # "Keyboard hook failed -- running without low-level key blocking".
        self._bind_with_fake_windll()
        backend = wb.WindowsBackend()
        with mock.patch.object(backend, "_hook_thread_func", create=True):
            # _start_keyboard_hook menjalankan thread; cukup pastikan tidak
            # lempar NameError dan _hook_installed tidak False karena
            # prototype hilang.
            try:
                backend._start_keyboard_hook()
            except Exception as exc:  # noqa: BLE001
                self.fail(f"_start_keyboard_hook melempar: {exc!r}")
        backend._stop_keyboard_hook()
        self.assertIsNotNone(
            getattr(backend, "_hook_installed", None),
            "backend harus punya atribut _hook_installed setelah hook dijalankan",
        )

    def test_the_required_list_is_not_empty(self):
        # Kalau daftar ini dikosongkan, semua test di atas jadi tidak
        # berguna. Koscengkan sendiri kalau menambah fitur Win32 baru.
        self.assertGreaterEqual(len(REQUIRED_PROTOTYPES), 17)
        # Semua nama yang wajib harus benar-benar punya wrapper di modul --
        # kalau ada yang baru ditambah ke REQUIRED_PROTOTYPES tapi tidak
        # ada di _win32_prototypes, test ini yang akan bilang.
        doc = (wb._win32_prototypes.__doc__ or "") + "\n".join(
            REQUIRED_PROTOTYPES)
        for name in REQUIRED_PROTOTYPES:
            with self.subTest(name=name):
                self.assertTrue(name.startswith("_") and name[1].isupper())


class BindFailsLoudlyOutsideWindowsTest(unittest.TestCase):
    def test_no_windll_raises_attribute_error_not_name_error(self):
        # Di Linux, `from ctypes import windll` harus gagal dan __getattr__
        # harus mengubahnya jadi AttributeError yang wajar -- bukan
        # NameError dari assignment prototype yang tidak pernah jalan.
        self.assertNotEqual(sys.platform, "win32", "test ini untuk non-Windows")
        wb._bound = False
        with mock.patch.object(ctypes, "windll", None, create=True):
            with self.assertRaises(AttributeError):
                wb._OpenClipboard


if __name__ == "__main__":
    unittest.main()
