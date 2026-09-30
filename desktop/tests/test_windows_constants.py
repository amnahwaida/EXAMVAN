"""Konstanta Win32 non-prototype harus bisa diakses setelah _bind().

Bug yang menutup file ini
------------------------
`_win32_prototypes()` me-return dict dengan filter

    if k.startswith("_") and not k.startswith("__")

Yang burying: `WM_QUIT` dan `SM_CMONITORS` didefinisikan sebagai lokal di
dalam fungsi itu, dan KEDUANYA tidak diawali `_`. Jadi keduanya tidak
pernah masuk ke `globals()`, padahal `_bind()` hanya melakukan
`globals().update(...)`.

Dua akibat, keduanya senyap karena tertelan `except`:

* `WM_QUIT` -> `_stop_keyboard_hook()` memanggil `_PostThreadMessageW(
  ..., WM_QUIT, ...)` di dalam `try/except Exception: pass`. NameError
  -> triplet diabaikan -> WM_QUIT TIDAK PERNAH terkirim -> thread hook
  tidak keluar dari `GetMessageW` -> `UnhookWindowsHookEx` di blok
  `finally` tidak pernah jalan. Keyboard hook (blokir Alt+Tab / Win /
  Ctrl+Shift+Esc) TETAP TERPASANG setelah ujian selesai, sampai process
 .Exit. Dan karena `release_strict_mode` tetap men-set `_hook_installed
  = False`, sesi berikutnya memasang hook KEDUA di thread baru.

* `SM_CMONITORS` -> `_has_multiple_monitors()` memanggil
  `_GetSystemMetrics(SM_CMONITORS)` di dalam `try/except Exception:
  return False`. NameError -> return False SELALU, di setiap PC Windows
  termasuk yang benar-benar punya dua monitor. Peringatan multi-monitor
  di enforcer tidak pernah muncul.

Kenapa test yang ada tidak menangkap
-----------------------------------
`tests/test_windows_backend_binding.py` punya `REQUIRED_PROTOTYPES`
yang hanya berisi nama berawalan `_`. Semua entri-nya memang prototype
fungsi, jadi `WM_QUIT`/`SM_CMONITORS` tidak pernah ikut dicek -- dan
docstring file itu justru mengklaim "deteksi multi-monitor selalu False"
sudah diperbaiki. Test itu hijau sementara bug-nya masih ada.

Test di sini menutupnya: setelah `_bind()` dengan `ctypes.windll` palsu
yang BERHASIL, setiap konstanta yang dipakai kode harus benar-benar ada
sebagai global modul -- persis kondisi di PC Windows.
"""

from __future__ import annotations

import ctypes
import sys
import types
import unittest
from unittest import mock

from examvan.security import windows_backend as wb


def _fake_dll(recorder=None, returns=None):
    """DLL palsu yang mencatat panggilan, seperti windll sungguhan.

    `returns` memetakan nama fungsi ke nilai yang dikembalikan, supaya test
    bisa menjalankan jalur produksi apa adanya -- bukan mencocokkan callback
    yang sudah di-mock, yang membuat assertion "dipanggil" jadi tidak
    berarti apa-apa.
    """
    returns = returns or {}

    class _Fake:
        def __init__(self, lib: str) -> None:
            self._lib = lib

        def __getattr__(self, name):
            def fn(*args, **kwargs):
                if recorder is not None:
                    recorder.append((self._lib, name, args))
                return returns.get(name, 0)

            fn.restype = None
            fn.argtypes = None
            return fn

    return types.SimpleNamespace(
        user32=_Fake("user32"),
        kernel32=_Fake("kernel32"),
        advapi32=_Fake("advapi32"),
    )


class Win32ConstantsBoundTestCase(unittest.TestCase):
    """Setiap konstanta yang dibaca kode harus ada sebagai global."""

    def setUp(self) -> None:
        calls: list = []
        self.calls = calls
        patcher = mock.patch.object(ctypes, "windll", _fake_dll(calls), create=True)
        patcher.start()
        self.addCleanup(patcher.stop)
        wb._bind()

    # -- konstanta yang dipakai kode, bukan cuma prototype -------------

    def test_every_module_level_constant_is_reachable(self):
        missing = [
            name for name in ("WM_QUIT", "SM_CMONITORS")
            if not hasattr(wb, name)
        ]
        self.assertEqual(
            missing, [],
            f"{missing} tidak terpasang sebagai global, jadi setiap pembacaan "
            "nya berakhir NameError yang tertelan except. Pindahkan ke "
            "konstanta level modul seperti VK_*/ES_*/WDA_* yang lain.",
        )

    def test_constants_have_the_documented_windows_values(self):
        # Nilai salah di sini berarti tidak ada yang gagal diam-diam:
        # WM_QUIT yang keliru tidak akan menghentikan thread, dan
        # SM_CMONITORS yang keliru akan mengukur hal yang lain.
        self.assertEqual(wb.WM_QUIT, 0x0012)
        self.assertEqual(wb.SM_CMONITORS, 80)

    # -- akibat #1: keyboard hook harus benar-benar bisa dihentikan ---

    def test_stopping_the_hook_actually_posts_wm_quit(self):
        posted = []
        backend = wb.WindowsBackend()

        class _LivePump:
            """Thread yang hanya keluar kalau WM_QUIT benar-benar sampai."""

            def __init__(self) -> None:
                self.exited = False

            def is_alive(self) -> bool:
                return not self.exited

            def join(self, timeout=None) -> None:
                if posted:
                    self.exited = True

        pump = _LivePump()
        with mock.patch.object(wb, "_PostThreadMessageW",
                               side_effect=lambda tid, msg, w, l: posted.append(msg)), \
             mock.patch.object(wb, "_hook_thread_id", 1234), \
             mock.patch.object(wb, "_hook_thread", pump):
            backend._stop_keyboard_hook()

        self.assertTrue(
            posted,
            "WM_QUIT tidak pernah terkirim, jadi thread hook tidak keluar "
            "dan UnhookWindowsHookEx di blok finally tidak pernah jalan: "
            "keyboard hook tetap memblokir Alt+Tab/Win setelah ujian selesai.",
        )
        self.assertEqual(posted, [wb.WM_QUIT])
        self.assertTrue(
            pump.exited,
            "thread hook masih hidup setelah _stop_keyboard_hook()",
        )

    # -- akibat #2: multi-monitor harus benar-benar diukur -------------

    def test_multi_monitor_check_really_queries_the_display_count(self):
        for reported, expected in ((1, False), (2, True), (4, True)):
            with self.subTest(monitors=reported):
                # Jalur produksi apa adanya: GetSystemMetrics yang dipakai
                # adalah versi dari DLL palsu, jadi assertion "dipanggil"
                # di bawah benar-benar berarti sesuatu.
                self.calls.clear()
                wb._GetSystemMetrics = _fake_dll(
                    self.calls, returns={"GetSystemMetrics": reported}
                ).user32.GetSystemMetrics

                self.assertIs(
                    wb.WindowsBackend().has_multiple_monitors(), expected
                )
                self.assertTrue(
                    any(c[1] == "GetSystemMetrics" for c in self.calls),
                    "GetSystemMetrics tidak pernah dipanggil -- SM_CMONITORS "
                    "tidak terpasang dan hasilnya hardcoded False.",
                )
                self.assertEqual(self.calls[0][2], (wb.SM_CMONITORS,))


class Win32ConstantsNotShadowedByBindTestCase(unittest.TestCase):
    """`_bind()` tidak boleh menimpa konstanta yang sudah level modul."""

    def test_bind_leaves_module_constants_untouched(self):
        patcher = mock.patch.object(ctypes, "windll", _fake_dll(), create=True)
        patcher.start()
        self.addCleanup(patcher.stop)
        wb._bind()
        # import ulang modul harus menghasilkan nilai yang sama
        for name, expected in (("WM_QUIT", 0x0012), ("SM_CMONITORS", 80)):
            with self.subTest(name=name):
                self.assertEqual(getattr(wb, name), expected)


if __name__ == "__main__":
    unittest.main()