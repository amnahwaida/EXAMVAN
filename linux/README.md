# EXAMVAN Linux Desktop Client

Aplikasi ujian digital EXAMVAN untuk Linux. Mendukung 3 mode keamanan: **Low**, **Medium**, dan **Strict (Kiosk)**.

## Persyaratan Sistem

- **Python 3.8+** dengan `venv`
- **X11** (disarankan) — fitur keamanan lengkap
- **Wayland** — fitur keamanan terbatas (keyboard grab tidak tersedia)

## Instalasi

### Cara 1: Install via curl (satu perintah)

```bash
curl -fsSL https://raw.githubusercontent.com/amnahwaida/EXAMVAN/main/linux/install.sh | sudo bash
```

Script otomatis unduh source dari GitHub, install dependensi, setup venv, dan daftarkan launcher.

### Cara 2: Install lokal (dari clone)

```bash
cd linux
sudo ./install.sh
```

Script akan:
- Install dependensi sistem (python3-venv, PyQt5, xsel, dll)
- Salin file ke `/opt/examvan/`
- Buat virtual environment + install requirements
- Buat launcher `/usr/bin/examvan`
- Daftarkan menu aplikasi & sesi kiosk

Setelah selesai, jalankan dari terminal:

```bash
examvan
```

Atau cari **EXAMVAN** di menu aplikasi.

### Uninstall

**Via curl (jika install dari curl):**
```bash
curl -fsSL https://raw.githubusercontent.com/amnahwaida/EXAMVAN/main/linux/install.sh | sudo bash -s -- --uninstall
```

**Lokal (jika clone repo):**
```bash
sudo ./install.sh --uninstall
```

Keduanya hapus: `/opt/examvan/`, `/usr/bin/examvan`, menu aplikasi, sesi kiosk.

### Cara 2: Jalankan Langsung (tanpa install)

```bash
cd linux
./run.sh
```

Launcher otomatis buat virtual environment + install dependencies saat pertama jalan.

## Perintah

| Perintah | Fungsi |
|----------|--------|
| `examvan` | Jalankan mode normal |
| `examvan --kiosk` | Jalankan mode kiosk (Xephyr + Openbox) |
| `./run.sh` | Jalankan tanpa install (dari direktori) |
| `./run.sh --kiosk` | Mode kiosk tanpa install |
| `./run.sh --setup` | Paksa buat ulang virtual environment |
| `./run.sh --clean` | Hapus virtual environment |

## Mode Keamanan

### Low Mode
| Fitur | X11 | Wayland |
|-------|-----|---------|
| Anti-screenshot | Bypass compositor | Inherently protected |
| Clipboard clear | `xsel` | `wl-copy --clear` |
| Screen wake lock | `systemd-inhibit` / `xset` | `systemd-inhibit` |

### Medium Mode (Low +)
- Focus loss detection → auto-submit 3 detik
- Banner merah countdown
- Sama di X11 & Wayland

### Strict Mode (Medium + Kiosk)
| Fitur | X11 | Wayland |
|-------|-----|---------|
| Keyboard grab (Alt+Tab, Super, dll) | `XGrabKeyboard` | ❌ Tidak didukung |
| Pointer grab | `XGrabPointer` | ❌ Tidak didukung |
| Fullscreen frameless | `Qt.FramelessWindowHint` | Sama |
| Kiosk session (Xephyr + Openbox) | ✅ | ✅ (nested X server) |
| Admin exit | Ctrl+Shift+Alt+Q × 3 + password | Sama |

> **Wayland**: untuk fitur keamanan lengkap, gunakan mode kiosk: `examvan --kiosk`
> atau pilih sesi **EXAMVAN Kiosk** dari login manager (GDM/LightDM/SDDM).

## Instalasi Manual per Distribusi

### Ubuntu / Debian

```bash
sudo apt install python3 python3-venv python3-pip python3-pyqt5 xsel
sudo apt install xserver-xephyr openbox   # opsional untuk mode kiosk
```

### Fedora

```bash
sudo dnf install python3 python3-pip python3-qt5 xsel
sudo dnf install xorg-x11-server-Xephyr openbox   # opsional untuk mode kiosk
```

### Arch Linux

```bash
sudo pacman -S python python-pip python-pyqt5 xsel
sudo pacman -S xorg-server-xephyr openbox   # opsional untuk mode kiosk
```

## Struktur Direktori (setelah install)

```
/opt/examvan/
├── .venv/               ← virtual environment
│   └── bin/python3      ← venv Python
├── examvan/             ← source code
│   ├── __main__.py      ← entry point
│   ├── api.py
│   ├── config.py
│   ├── models.py
│   ├── utils.py
│   ├── security/        ← enforce, kiosk, x11 grab
│   └── ui/              ← PyQt5 widgets
├── run.sh               ← launcher
└── requirements.txt

/usr/bin/examvan         ← symlink → /opt/examvan/.venv/bin/python3 -m examvan
/usr/share/applications/examvan.desktop
/usr/share/xsessions/examvan-kiosk.desktop
```

## Alur Kerja

```
1. Server Config Dialog
   └─ Masukkan URL server + token ujian (8 karakter)
   └─ Health check → Token lookup → Identity dialog

2. Identity Dialog
   └─ Isi identitas siswa
   └─ Data tersimpan untuk penggunaan berikutnya

3. Exam Viewer
   └─ PDF viewer per halaman
   └─ Lembar jawaban (5 tipe soal)
   └─ Timer countdown / elapsed
   └─ Auto-save jawaban tiap 500ms
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

### Keyboard grab gagal
```bash
# Nonaktifkan compositor (KDE)
qdbus org.kde.KWin /Compositor suspend

# Atau gunakan kiosk session
examvan --kiosk
```

### Mode kiosk error "Xephyr not found"
```bash
sudo apt install xserver-xephyr openbox  # Ubuntu/Debian
```

### Timer salah (beda 7 jam)
Pastikan server container punya `tzdata`:

```bash
# Di server
docker exec examvan-go-server ls /usr/share/zoneinfo/Asia/Jakarta
```
