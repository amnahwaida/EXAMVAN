# EXAMVAN Desktop Client (Linux & Windows)

Aplikasi ujian digital EXAMVAN untuk Linux dan Windows dalam satu codebase.
Mendukung 3 mode keamanan: **Low**, **Medium**, dan **Strict**.

## Persyaratan Sistem

### Linux
- **Python 3.8+** dengan `venv`
- **X11** (disarankan) — fitur keamanan lengkap
- **Wayland** — fitur keamanan terbatas (keyboard grab tidak tersedia)

### Windows
- **Windows 10/11** 64-bit
- **Python 3.10+** 64-bit — download dari [python.org](https://python.org)
- **Visual C++ Redistributable** (biasanya sudah terinstall dengan Python)

## Instalasi

### Linux

**Cara 1: Install via curl (satu perintah)**

```bash
curl -fsSL https://raw.githubusercontent.com/amnahwaida/EXAMVAN/main/desktop/install.sh | sudo bash
```

**Cara 2: Install lokal (dari clone)**

```bash
cd desktop
sudo ./install.sh
```

**Jalankan:**

```bash
examvan
```

Atau cari **EXAMVAN** di menu aplikasi.

**Uninstall:**

```bash
sudo ./install.sh --uninstall
```

### Windows

**Cara 1: Install otomatis (venv + shortcut desktop)**

Jalankan PowerShell sebagai **user biasa** (tidak perlu admin):

```powershell
cd windows
powershell -ExecutionPolicy Bypass -File install.ps1
```

Setelah selesai, klik dua kali `EXAMVAN.bat` di desktop.

**Cara 2: Jalankan langsung (tanpa install)**

```powershell
cd windows
powershell -ExecutionPolicy Bypass -File run.ps1
```

**Cara 3: Build .exe portable (no Python needed)**

```powershell
cd windows
powershell -ExecutionPolicy Bypass -File build-exe.ps1
```

Output: `windows\dist\EXAMVAN.exe`

## Perintah (Linux)

| Perintah | Fungsi |
|----------|--------|
| `examvan` | Jalankan mode normal |
| `examvan --kiosk` | Jalankan mode kiosk (Xephyr + Openbox) |
| `./run.sh` | Jalankan tanpa install (dari direktori) |
| `./run.sh --kiosk` | Mode kiosk tanpa install |
| `./run.sh --setup` | Paksa buat ulang virtual environment |
| `./run.sh --clean` | Hapus virtual environment |

## Mode Keamanan

### Low Mode (semua platform)
| Fitur | Linux X11 | Linux Wayland | Windows |
|-------|-----------|---------------|---------|
| Anti-screenshot | Bypass compositor | Inherently protected | `WDA_MONITOR` |
| Clipboard clear tiap 3 detik | `xsel` + Qt | `wl-copy --clear` + Qt | `EmptyClipboard()` + Qt |
| Screen wake lock | `systemd-inhibit` / `xset` | `systemd-inhibit` | `SetThreadExecutionState` |
| Sleep prevention | ✅ | ✅ | ✅ |
| Dark mode detection | `gsettings` + env | `gsettings` + env | Registry + Qt palette |

### Medium Mode (Low +)
- Focus loss detection → auto-submit 3 detik
- Window focus polling tiap 500ms
- Force raise window di strict mode
- Sama di semua platform

### Strict Mode (Medium +)
| Fitur | Linux X11 | Linux Kiosk | Windows |
|-------|-----------|-------------|---------|
| Fullscreen frameless | ✅ | ✅ | ✅ |
| Block Alt+Tab | `XGrabKeyboard` | Openbox config | Keyboard hook ✅ |
| Block Win key | `XGrabKeyboard` | Openbox config | Keyboard hook ✅ |
| Block PrintScreen | `XGrabKeyboard` | Openbox config | Hook + `WDA_MONITOR` ✅ |
| Block Task Manager | — | — | ✅ `Ctrl+Shift+Esc` |
| Block Win+L (Lock) | — | — | ✅ |
| Block Win+Tab/G/P/S/R | — | — | ✅ (20+ Win combos) |
| Block Magnifier/OSK | — | — | ✅ |
| Multi-monitor warning | `xrandr` | — | `GetSystemMetrics` |
| Pointer grab | `XGrabPointer` | Openbox | — |
| Kiosk session | — | Xephyr + Openbox | — |
| **Total keyboard block** | ~5 | ~5 | **40 combos** |
| Admin exit | Ctrl+Shift+Alt+Q × 3 + password | Sama | Sama |

> **Catatan Windows**: `Ctrl+Alt+Del` tidak bisa di-intercept (SAS — OS level).

## Admin Exit Password

**WAJIB** diatur sebelum menjalankan app. Tanpa ini, admin exit tidak bisa digunakan.

```bash
# Linux
export EXAMVAN_ADMIN_PASSWORD=rahasia123
examvan
```

```cmd
:: Windows (CMD)
set EXAMVAN_ADMIN_PASSWORD=rahasia123
powershell -ExecutionPolicy Bypass -File run.ps1
```

```powershell
# Windows (PowerShell)
$env:EXAMVAN_ADMIN_PASSWORD = "rahasia123"
.\windows\run.ps1
```

Cara pakai: tekan `Ctrl+Shift+Alt+Q` tiga kali di strict mode, masukkan password.

## Timer

- Menggunakan **monotonic clock** — tidak bisa dimanipulasi dengan mengubah jam sistem.
- Countdown via server `end_time` dikonversi ke monotonic timestamp.
- Start time dikirim ke server derived dari monotonic clock.

## Keamanan Data

- **Jawaban tersimpan** di disk dalam format **XOR-obfuscated + base64**, bukan plaintext.
- **PDF file** di `%TEMP%` langsung dihapus setelah submit.
- **Window title** generic ("EXAMVAN") — tidak bocor nama ujian.
- **Clipboard** dibersihkan tiap 3 detik + clipboard history di-overwrite.

## Struktur Direktori (Linux after install)

```
/opt/examvan/
├── .venv/                    ← virtual environment
│   └── bin/python3           ← venv Python
├── examvan/                  ← source code (cross-platform)
│   ├── __main__.py           ← entry point
│   ├── api.py                ← HTTP client (urllib)
│   ├── config.py             ← konfigurasi + answer cache (XOR obfuscated)
│   ├── models.py             ← data models
│   ├── utils.py              ← MAC, clipboard, device identity
│   ├── security/
│   │   ├── base.py           ← abstract SecurityBackend
│   │   ├── __init__.py       ← factory: get_backend()
│   │   ├── enforcer.py       ← security enforcer (cross-platform)
│   │   ├── linux_backend.py  ← X11 grabs, GNOME lock, systemd-inhibit
│   │   ├── windows_backend.py← Win32 API hook, clipboard, WDA_MONITOR
│   │   ├── x11.py            ← ctypes X11 bindings
│   │   └── kiosk.py          ← Xephyr + Openbox session
│   └── ui/                   ← PyQt5 widgets
│       ├── answer_sheet.py
│       ├── exam_viewer.py
│       ├── identity_dialog.py
│       ├── pdf_viewer.py
│       ├── server_config.py
│       ├── styles.py
│       └── timer.py          ← monotonic clock, immune to time manipulation
├── run.sh                    ← launcher
└── requirements.txt
```

## Windows Build Output

```
windows/
├── build-exe.ps1             ← PyInstaller build script
├── run.ps1                   ← auto venv + run
├── install.ps1               ← setup + shortcut desktop
├── dist/
│   └── EXAMVAN.exe           ← standalone executable (~50 MB)
└── README.md
```

## Alur Kerja

```
1. Server Config Dialog
   ├─ Masukkan URL server + token ujian (8 karakter)
   └─ Health check → Token lookup → Identity dialog

2. Identity Dialog
   ├─ Isi identitas siswa (dinamis dari server)
   └─ Data tersimpan untuk penggunaan berikutnya

3. Exam Viewer
   ├─ Security diaktifkan SEBELUM PDF download
   ├─ PDF viewer per halaman (drag scroll, zoom)
   ├─ Lembar jawaban (5 tipe soal)
   ├─ Timer monotonic countdown / elapsed
   ├─ Auto-save jawaban tiap perubahan (500ms debounce)
   ├─ Focus loss → auto-submit 3 detik
   └─ Submit manual / auto-submit saat waktu habis
```

## Troubleshooting

### "python3-venv not found"
```bash
sudo apt install python3-venv   # Ubuntu/Debian
```

### "Cannot load libX11"
```bash
sudo apt install libx11-6       # Ubuntu/Debian
sudo dnf install libX11         # Fedora
```

### Keyboard grab gagal (Linux)
```bash
# Nonaktifkan compositor (KDE)
qdbus org.kde.KWin /Compositor suspend

# Atau gunakan kiosk session
examvan --kiosk
```

### Mode kiosk error "Xephyr not found" (Linux)
```bash
sudo apt install xserver-xephyr openbox  # Ubuntu/Debian
```

### Keyboard hook tidak memblokir (Windows)
Jalankan sebagai Administrator untuk jaminan blocking penuh.

### "PyQt5 import error" (Windows)
Install Visual C++ Redistributable:
```
https://aka.ms/vs/17/release/vc_redist.x64.exe
```

### Timer tidak sinkron
Timer menggunakan monotonic clock — tidak terpengaruh perubahan jam sistem.
Pastikan server container punya `tzdata` yang benar.

### Proses tetap berjalan setelah close
Jika ada masalah dengan session, taskkill manual:
```bash
# Linux
pkill -f examvan

# Windows
taskkill /f /im EXAMVAN.exe
```
