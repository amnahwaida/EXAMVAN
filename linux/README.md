# EXAMVAN Linux Desktop Client

Aplikasi ujian digital EXAMVAN untuk Linux. Mendukung 3 mode keamanan: **Low**, **Medium**, dan **Strict (Kiosk)**.

## Persyaratan Sistem

- **Python 3.8+** dengan `venv` module
- **X11** (recommended) — untuk fitur keamanan lengkap
- **Wayland** — berjalan dengan fitur keamanan terbatas

## Instalasi

### 1. Install Python 3 + venv

**Ubuntu / Debian:**
```bash
sudo apt install python3 python3-venv python3-pip
```

**Fedora:**
```bash
sudo dnf install python3 python3-pip
```

**Arch Linux:**
```bash
sudo pacman -S python python-pip
```

### 2. Jalankan launcher

Launcher akan **otomatis membuat virtual environment** dan menginstall dependencies saat pertama kali dijalankan:

```bash
cd linux
./run.sh
```

Output pertama kali:
```
[INFO]  Creating virtual environment at /path/to/linux/.venv ...
[INFO]  Installing dependencies into venv ...
[INFO]  Virtual environment ready.
```

Virtual environment disimpan di `linux/.venv/` — tidak perlu install manual.

## Perintah Launcher

| Perintah | Fungsi |
|----------|--------|
| `./run.sh` | Jalankan mode normal (auto-setup venv jika belum ada) |
| `./run.sh --kiosk` | Jalankan mode kiosk (strict, isolated X session) |
| `./run.sh --setup` | Paksa buat ulang virtual environment |
| `./run.sh --clean` | Hapus virtual environment |

## Manual Setup (Tanpa Launcher)

Jika ingin menjalankan tanpa `run.sh`:

```bash
cd linux

# Buat virtual environment
python3 -m venv .venv

# Aktifkan
source .venv/bin/activate

# Install dependencies
pip install -r requirements.txt

# Jalankan
python -m examvan

# Deactivate saat selesai
deactivate
```

## Struktur Virtual Environment

```
linux/
├── .venv/               ← virtual environment (auto-created, di-gitignore)
│   ├── bin/python       ← venv Python
│   ├── bin/pip          ← venv pip
│   └── lib/python3.x/   ← packages terinstall
├── run.sh               ← launcher (auto-manage venv)
├── requirements.txt     ← PyQt5, PyMuPDF
└── examvan/             ← source code
```

## Mode Keamanan

### Low Mode
| Fitur | X11 | Wayland |
|-------|-----|---------|
| Anti-screenshot | `_NET_WM_BYPASS_COMPOSITOR` | Inherently protected |
| Clipboard clear | `xsel` + Qt clipboard | `wl-copy --clear` |
| Screen wake lock | `systemd-inhibit` / `xset` | `systemd-inhibit` |

### Medium Mode (Low +)
| Fitur | X11 | Wayland |
|-------|-----|---------|
| Focus loss detection | `QApplication.applicationStateChanged` | Sama |
| Auto-submit (3 detik) | Timer + submit | Sama |
| Visual countdown | Banner merah | Sama |

### Strict Mode (Medium + Kiosk)
| Fitur | X11 | Wayland |
|-------|-----|---------|
| Keyboard grab | `XGrabKeyboard` (block Alt-Tab, Super, dll) | Tidak tersedia |
| Pointer grab | `XGrabPointer` (cursor terkunci di window) | Tidak tersedia |
| Fullscreen frameless | `Qt.FramelessWindowHint` | Sama |
| Block close event | `closeEvent` ignored | Sama |
| Kiosk session | Openbox kiosk + isolated X session | Tidak tersedia |
| Admin exit | Ctrl+Shift+Alt+Q × 3 + password | Sama |

## Alur Kerja

```
1. Server Config Dialog
   └─ Masukkan URL server + token ujian (8 karakter)
   └─ Health check → Token lookup → Identity dialog

2. Identity Dialog
   └─ Isi identitas siswa (nama, nomor ujian, kelas)
   └─ Data tersimpan untuk penggunaan berikutnya

3. Exam Viewer
   └─ PDF diunduh dan di-render per halaman
   └─ Lembar jawaban di panel kanan (5 tipe soal)
   └─ Timer elapsed time di pojok kanan atas
   └─ Jawaban auto-save setiap 500ms
   └─ Submit manual atau auto-submit (medium/strict)
```

## Install ke Sistem (opsional)

```bash
sudo cp -r linux /opt/examvan
sudo chmod +x /opt/examvan/run.sh
sudo cp linux/examvan_kiosk.desktop /usr/share/xsessions/
```

Siswa bisa memilih sesi "EXAMVAN Kiosk" dari login manager (GDM/LightDM/SDDM).

## Troubleshooting

### Venv gagal dibuat
```bash
# Pastikan python3-venv terinstall
sudo apt install python3-venv   # Ubuntu/Debian

# Atau paksa buat ulang
./run.sh --setup
```

### "Cannot load libX11"
```bash
sudo apt install libx11-6       # Ubuntu/Debian
sudo dnf install libX11         # Fedora
```

### Package hilang di venv
```bash
# Rebuild venv dari nol
./run.sh --setup
```

### Keyboard grab gagal
Pastikan tidak ada compositor yang mengambil alih:
```bash
# Disable compositor (KDE)
qdbus org.kde.KWin /Compositor suspend

# Atau gunakan kiosk session (paling aman)
./run.sh --kiosk
```

### Wayland — fitur keamanan terbatas
Untuk fitur keamanan lengkap (keyboard grab, pointer grab), gunakan X11:
```bash
# Di login screen, pilih session "X11" atau "GNOME on Xorg"
# Atau gunakan kiosk session
./run.sh --kiosk
```
