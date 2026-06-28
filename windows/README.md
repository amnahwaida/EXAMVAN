# EXAMVAN Windows Desktop Client

## Cara 1: Install Otomatis (recommended)

Jalankan PowerShell sebagai **user biasa** (tidak perlu admin):

```powershell
cd windows
powershell -ExecutionPolicy Bypass -File install.ps1
```

Script akan:
- Cek Python sudah terinstall
- Buat virtual environment
- Install PyQt5 + PyMuPDF
- Buat shortcut `EXAMVAN.bat` di desktop
- Test import PyQt5

Setelah selesai, klik dua kali `EXAMVAN.bat` di desktop, atau:

```powershell
cd windows
powershell -ExecutionPolicy Bypass -File run.ps1
```

## Cara 2: Jalankan Langsung (tanpa install)

```powershell
cd windows
powershell -ExecutionPolicy Bypass -File run.ps1
```

Script otomatis buat venv + install deps saat pertama jalan.

## Cara 3: Build .exe (portable, no Python needed)

```powershell
cd windows
powershell -ExecutionPolicy Bypass -File build-exe.ps1
```

Output: `windows\dist\EXAMVAN.exe` — bisa di-copy ke komputer lain.

## Prerequisites

| Kebutuhan | Keterangan |
|-----------|------------|
| **Windows 10/11** | 64-bit |
| **Python 3.10+** | Download dari [python.org](https://www.python.org/downloads/) — centang **"Add Python to PATH"** |
| **Visual C++ Redistributable** | Diinstall otomatis sama Python, atau download [vc_redist.x64.exe](https://aka.ms/vs/17/release/vc_redist.x64.exe) |

## Fitur Keamanan

| Fitur | Windows 10/11 |
|-------|---------------|
| Fullscreen frameless | ✅ |
| Block Alt+Tab, Win key | ✅ (keyboard hook) |
| Block PrintScreen | ✅ (keyboard hook + SetWindowDisplayAffinity) |
| Block Task Manager (Ctrl+Shift+Esc) | ✅ |
| Block Ctrl+Esc (Start menu) | ✅ |
| Block Escape | ✅ (di strict mode) |
| Clipboard clear tiap 3 detik | ✅ (Win32 API) |
| Prevent sleep/monitor mati | ✅ (SetThreadExecutionState) |
| Dark mode detection | ✅ (registry) |
| Ctrl+Alt+Del | ❌ (SAS — OS level, tidak bisa di-intercept) |

## Uninstall

Hapus folder `linux\.venv` dan shortcut `EXAMVAN.bat` dari desktop.

## Troubleshooting

### "Python tidak ditemukan"
Install Python dari python.org, centang "Add Python to PATH".

### "ImportError: No module named PyQt5"
```powershell
cd linux
.venv\Scripts\pip install PyQt5 PyMuPDF
```

### "Failed to load PyQt5"
Install Visual C++ Redistributable:
```
https://aka.ms/vs/17/release/vc_redist.x64.exe
```

### Keyboard hook tidak memblokir
Jalankan sebagai Administrator untuk jaminan blocking penuh. Tanpa admin, `WH_KEYBOARD_LL` tetap jalan untuk window foreground.

### Window tidak fullscreen (multi-monitor)
Set aplikasi sebagai foreground window sebelum klik fullscreen.
