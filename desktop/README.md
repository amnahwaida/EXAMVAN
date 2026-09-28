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
| Clipboard clear tiap 10 detik | `xsel` (worker thread) | `wl-copy --clear` (worker thread) | `EmptyClipboard()` (worker thread) |
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
- **Clipboard** dibersihkan tiap 10 detik. Sisi Qt di-clear inline di GUI
  thread, sisi platform (Win32/X11) di worker thread — `EmptyClipboard` pada
  clipboard berisi data OLE bisa blocking, dan menjalankannya di GUI thread
  membekukan kursor di PC low-end.

## Struktur Direktori (Linux after install)

```
/opt/examvan/
├── .venv/                    ← virtual environment
│   └── bin/python3           ← venv Python
├── main.py                   ← stub entry point build .exe Windows (PyInstaller)
├── examvan/                  ← source code (cross-platform)
│   ├── __main__.py           ← entry point
│   ├── api.py                ← HTTP client (urllib)
│   ├── config.py             ← konfigurasi + answer cache (XOR obfuscated)
│   ├── models.py             ← data models
│   ├── notify.py             ← notifikasi hasil auto-submit (Linux notify-send / Windows balloon)
│   ├── utils.py              ← MAC, clipboard, device identity
│   ├── ws.py                 ← WebSocket real-time (exam_terminated)
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
   └─ Health check → Token lookup → Gate "sudah selesai" (sticky) → Identity dialog

2. Identity Dialog
   ├─ Isi identitas siswa (dinamis dari server)
   └─ Data tersimpan untuk penggunaan berikutnya

3. Waiting Approval
   └─ Request-approval dengan identitas perangkat DESKTOP:<hash>

4. Exam Viewer
   ├─ Security diaktifkan SEBELUM PDF download
   ├─ PDF viewer per halaman (drag scroll, zoom)
   ├─ Lembar jawaban (5 tipe soal)
   ├─ Timer monotonic countdown / elapsed (refresh saat window aktif kembali)
   ├─ Auto-save jawaban tiap perubahan (500ms debounce)
   ├─ Focus loss → auto-submit 3 detik
   └─ Submit manual (interaktif) / auto-submit (tutup segera + background)
```

## Konsistensi Perilaku dengan Android (fix 16 Agustus 2026)

Kode ini diselaraskan dengan klien Android agar kontrak submit & re-entry
identik (server idempoten: upsert per exam+perangkat):

### 1. Satu identitas perangkat untuk semua endpoint
Approval (`request-approval`), unduhan PDF (`X-Device-Id`) dan submit
(`mac_address`) semuanya memakai **`DESKTOP:<hash>`** yang sama (dulu:
approval/PDF pakai MAC mentah, submit pakai `DESKTOP:<hash>` → baris approval
dan submission tidak match → siswa tampil 2× di monitoring dan approval tidak
di-revoke). Pola yang sama dengan Android (`DEVICE:<AndroidId>`).

### 2. Submit ter-antri (202) TIDAK dianggap sukses final
Server mengembalikan `202 queued + job_id` saat Redis aktif — jawaban hanya
DIAANTRI, belum durable. Klien kini mem-poll `GET /result` (tiap 2,5 dtk,
±77 dtk) sampai worker mengonfirmasi `done`; **copy jawaban lokal tidak
dihapus pada 202 mentah** (mirror Android `QueuedResultPolling`). Jika worker
gagal / timeout, submit dilaporkan gagal dan jawaban tetap tersimpan di disk
untuk dicoba lagi.

### 3. Fallback jawaban disk saat submit kosong (F1)
Deadline bisa menembak sebelum jawaban dipulihkan dari disk (re-entry setelah
proses mati) → memori kosong. Submit kini memakai `resolve_submit_answers`:
memori lebih dulu, **fallback ke copy disk** — mencegah submit kosong menimpa
jawaban asli dalam window grace server (end_time + 60 dtk).

### 4. Ujian tanpa soal → lembar jawaban KOSONG
Tidak ada lagi fabrikasi 40 soal single-choice dummy saat exam tanpa
konfigurasi soal (PDF-only). Lembar jawaban tetap kosong (0/0) — siswa tidak
bisa menjawab soal yang tidak ada (mirror Android: `totalQuestions = 0` +
overlay disembunyikan).

### 5. Marker sticky "ujian sudah selesai" (F2)
Setelah submit sukses durable, `mark_submitted` menyimpan penanda sticky.
Re-entry ujian yang sama diblokir di gate join (`is_submitted`) — mencegah
re-entry dalam window grace mengirim submit kosong yang MENIMPA jawaban asli
(mirror Android: flag `submittedOrExited` sticky).

### 6. Server time skew untuk countdown
`GET /api/health` mengembalikan `server_time_utc`; klien menghitung selisih
jam perangkat vs server (`api.get_server_skew_ms`, mirror Android
`ApiClient.serverTimeSkewMs`) dan memakainya saat mengonversi deadline
`end_time` ke monotonic clock. Countdown akurat walau jam perangkat meleset
dari jam server.

### 7. Presence siswa di dashboard monitoring (access-log)
Klien kini mengirim `POST /access-log` — `login` saat ujian dibuka, `heartbeat`
tiap 60 detik, `logout` saat keluar — dan `POST /complete` setelah submit
sukses (mirror Android `WebSocketManager` + `ApiClient.sendAccessLog`).
Siswa desktop tampil **online/offline** di dashboard monitoring pengawas
(presence Redis TTL 5 menit), bukan lagi "tidak terlihat".

### 8. WebSocket real-time + event pengawas `exam_terminated`
Klien menghubungkan `wss://server/ws/<exam_id>` (token via query param,
reconnect backoff 1→30 dtk — mirror Android `WebSocketManager`) dan menerima
siaran pengawas. Event **`exam_terminated`** (pengawas menghentikan ujian)
langsung memicu auto-submit — siswa desktop tidak lagi dibiarkan berjalan
setelah pengawas mengakhiri ujian. Socket receive-only (presence via HTTP,
sama seperti Android).

### 9. Auto-submit menutup window SEGERA + hasil via notifikasi + recovery
Dulu auto-submit (deadline / `exam_terminated` / fokus hilang medium / close
medium-strict) menahan window tetap terbuka sampai hasil jaringan tiba — di
strict mode, jaringan mati = siswa terjebak di layar terkunci tanpa jalan
keluar. Kini mirror Android `autoSubmitAndExit`:

1. **Gate submit tunggal** (lock) — deadline berbarengan submit manual tidak
   menghasilkan double POST;
2. **Marker sticky + flush jawaban terbaru ke disk** (memakai F1: memori
   kosong tidak menimpa copy disk);
3. **Lepas kunci + tutup window SEGERA** — siswa bebas, tidak menunggu
   jaringan;
4. **Submit + polling `/result` di background** (thread murni, tanpa sentuh
   Qt);
5. **Sukses** → clear jawaban lokal + `complete` presence + notifikasi sistem
   (Linux `notify-send`; Windows balloon tip via PowerShell `NotifyIcon` —
   best-effort, tanpa dependency baru; termasuk `congrats_message` custom
   guru); **Gagal** → jawaban TETAP di disk + notifikasi — re-entry
   menampilkan layar recovery **"Kirim Lagi"** (`is_submitted` + jawaban
   pending → tawarkan resubmit; server idempoten).

Di Windows notifikasi dikirim via PowerShell + `System.Windows.Forms.
NotifyIcon` (bawaan .NET Framework di Windows 10/11) sehingga hasil
auto-submit tidak "hilang" setelah window ditutup; bila notifikasi gagal
(best-effort), hasil tetap aman di disk dan re-entry tetap menawarkan
recovery.

Pola sama persis dengan Android: submit manual tetap interaktif (window
tetap terbuka, error → dialog retry), hanya jalur AUTO yang menutup segera.

## Test

Test headless (tanpa display):

```bash
cd desktop
QT_QPA_PLATFORM=offscreen .venv/bin/python -m unittest discover -s tests
```

198 test: parsing submit queued/sync (termasuk `congrats_message`), polling
`/result` (done/pending/failure/timeout), persistensi & migrasi jawaban,
fallback F1, marker sticky F2, identitas perangkat, pemetaan identitas,
lembar jawaban tanpa soal dummy, server time skew, presence
(access-log/complete), parsing event WebSocket (termasuk `exam_terminated`),
alur auto-submit-and-exit (flush F1, clear saat sukses, simpan saat gagal,
gate submit tunggal, queued 202 → polling), notifikasi lintas platform,
gate recovery re-entry di ServerConfigDialog, dan refresh deadline setelah
suspend. Termasuk gate mode ujian (`security_level` "high" dari server
dipetakan ke tier "strict" client — pertama_ titik yang dulu membuat mode
Tinggi bisa keluar bebas) dan jaminan clipboard clear tidak menjalankan
proses baru maupun memblokir GUI thread.

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

### Melihat log aplikasi (diagnosis lapangan)
App menulis log rotating (1 MB × 3) ke `~/.config/examvan/app.log` —
berisi jejak aktivasi security, download PDF, submit, dan error. Ini satu-
satunya jejak di Windows karena proses `--windowed` tidak punya console.
