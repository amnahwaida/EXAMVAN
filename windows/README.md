# EXAMVAN Windows Desktop Client

Aplikasi ujian digital EXAMVAN untuk Windows. Satu codebase dengan versi Linux — source ada di folder `desktop/examvan/`.

---

## Step-by-Step Instalasi

### Langkah 0: Prasyarat

| Kebutuhan | Cara Cek | Link Download |
|-----------|----------|--------------|
| **Windows 10/11** 64-bit | Settings → About | — |
| **Python 3.10+** 64-bit | Buka CMD, ketik `python --version` | [python.org](https://www.python.org/downloads/) |
| **Visual C++ Redistributable** | Biasanya sudah ada | [vc_redist.x64.exe](https://aka.ms/vs/17/release/vc_redist.x64.exe) |

> **Saat install Python, PASTIKAN centang "Add Python to PATH"** di halaman pertama installer.

### Langkah 1: Dapatkan Source Code

**Opsi A — Clone repo (recommended, untuk update mudah):**

```powershell
# Buka cmd atau powershell, ketik:
git clone https://github.com/amnahwaida/EXAMVAN.git
cd EXAMVAN
```

**Opsi B — Download ZIP (tanpa git):**

1. Buka https://github.com/amnahwaida/EXAMVAN
2. Klik tombol hijau **"Code"** → **"Download ZIP"**
3. Extract ZIP
4. Buka folder hasil extract, masuk ke `windows\`

### Langkah 2: Install & Jalankan

**Cara 1: Install Otomatis (recommended)**

Buka **PowerShell** sebagai user biasa (tidak perlu admin):

```powershell
# Dari folder EXAMVAN
cd windows
powershell -ExecutionPolicy Bypass -File install.ps1
```

Script akan:
- Cek Python
- Buat virtual environment di `desktop/.venv/`
- Install PyQt5 + PyMuPDF
- Test import
- Buat shortcut `EXAMVAN.bat` di desktop

Setelah selesai, klik dua kali `EXAMVAN.bat` di desktop — app langsung jalan.

Atau jalankan manual:
```powershell
cd EXAMVAN
cd windows
powershell -ExecutionPolicy Bypass -File run.ps1
```

**Cara 2: Jalankan Langsung (tanpa shortcut)**

```powershell
cd EXAMVAN
cd windows
powershell -ExecutionPolicy Bypass -File run.ps1
```

Script auto buat venv + install deps saat pertama jalan.

**Cara 3: Build .exe Portable (bisa dibawa kemana-mana)**

```powershell
cd EXAMVAN
cd windows
powershell -ExecutionPolicy Bypass -File build-exe.ps1
```

Hasil: `windows\dist\EXAMVAN.exe` — file .exe standalone ~50 MB.  
Bisa di-copy ke USB, jalankan di komputer lain **tanpa install Python**.

### Langkah 3: Setup Admin Exit Password (WAJIB)

Tanpa ini, admin exit tidak bisa digunakan.

Di **CMD**:
```cmd
set EXAMVAN_ADMIN_PASSWORD=rahasia123
cd EXAMVAN
cd windows
powershell -ExecutionPolicy Bypass -File run.ps1
```

Di **PowerShell**:
```powershell
$env:EXAMVAN_ADMIN_PASSWORD = "rahasia123"
cd windows
.\run.ps1
```

---

## Alur Kerja Aplikasi

```
1. Server Config Dialog
   ├─ Masukkan URL server (contoh: http://192.168.1.100:80)
   └─ Masukkan token ujian (8 karakter) → connect

2. Identity Dialog
   └─ Isi identitas siswa (nama, nomor ujian, kelas)

3. Exam Viewer
   ├─ PDF soal ditampilkan per halaman
   ├─ Lembar jawaban digital (5 tipe soal)
   ├─ Timer countdown / elapsed
   ├─ Auto-save jawaban tiap perubahan
   ├─ Auto-submit saat waktu habis / fokus hilang
   └─ Kumpulkan manual → konfirmasi → selesai
```

---

## Fitur Keamanan Windows

| Fitur | Windows 10/11 |
|-------|---------------|
| Fullscreen frameless | ✅ |
| Block Alt+Tab | ✅ |
| Block Alt+F4 | ✅ |
| Block Win key (Start menu) | ✅ |
| Block Win+L (Lock screen) | ✅ |
| Block Win+Tab (Task View) | ✅ |
| Block Win+G (Game Bar / record) | ✅ |
| Block Win+Shift+S (Snipping Tool) | ✅ |
| Block Win+R, Win+E, Win+D, Win+I | ✅ |
| Block Win+1..9 (taskbar apps) | ✅ |
| Block Win+Space, Win+, Win+. | ✅ |
| Block PrintScreen + Alt+PrintScreen | ✅ (hook + WDA_MONITOR) |
| Block Ctrl+Shift+Esc (Task Manager) | ✅ |
| Block Ctrl+Esc (Start menu) | ✅ |
| Block Escape (alone) | ✅ |
| **Total: 50 blocked key combos** | ✅ |
| Clipboard clear tiap 3 detik | ✅ Win32 API |
| Clipboard history di-overwrite | ✅ `echo.\|clip` |
| Prevent sleep / monitor mati | ✅ SetThreadExecutionState |
| Dark mode detection | ✅ Registry |
| Multi-monitor detection | ✅ warning log |
| Admin exit password | ✅ fail-closed |
| **Ctrl+Alt+Del** | ❌ SAS — OS level |

---

## Uninstall

1. Hapus shortcut `EXAMVAN.bat` dari desktop
2. Hapus folder `desktop\.venv\`
3. (Opsional) Hapus folder `EXAMVAN\`

---

## Troubleshooting

### "Python tidak ditemukan"

Install Python dari [python.org](https://www.python.org/downloads/).  
**Centang "Add Python to PATH"** saat install.

### "ImportError: No module named PyQt5"

```powershell
cd EXAMVAN
cd desktop
.venv\Scripts\pip install PyQt5 PyMuPDF
```

### "Failed to load PyQt5" atau "DLL load failed"

Install Visual C++ Redistributable:
```
https://aka.ms/vs/17/release/vc_redist.x64.exe
```

### "python" tidak dikenali setelah install

Tutup dan buka ulang PowerShell/CMD. Atau jalanin:
```cmd
"%LOCALAPPDATA%\Programs\Python\Python312\python.exe" --version
```

### Install.ps1 error "execution policy"

```powershell
Set-ExecutionPolicy -Scope CurrentUser -ExecutionPolicy RemoteSigned
```
Jalankan sekali, lalu ulangi install.

### Keyboard hook tidak memblokir

Jalankan PowerShell sebagai **Administrator**. Tanpa admin, hook tetap jalan tapi kurang reliable untuk blocking global.

### Window tidak fullscreen di 2 monitor

Aplikasi fullscreen di primary monitor saja. Monitor kedua tidak terkunci — ini limitasi yang diketahui.

### Timer tidak sinkron

Timer pakai monotonic clock — tidak terpengaruh perubahan jam sistem.  
Pastikan server punya `tzdata` yang benar.

### Proses tetap berjalan setelah close

```cmd
taskkill /f /im EXAMVAN.exe
```
