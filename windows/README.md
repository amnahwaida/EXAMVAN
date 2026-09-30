# EXAMVAN Windows Desktop Client

Aplikasi ujian digital EXAMVAN untuk Windows. Satu codebase dengan versi Linux — source ada di folder `desktop/examvan/`.

---

## CARA PENTING: Untuk Guru & Siswa

**Tidak perlu install Python. Tidak perlu install apa pun.**

1. Download **`EXAMVAN-Setup.exe`** dari [release `exe-latest`](https://github.com/amnahwaida/EXAMVAN/releases/tag/exe-latest)
2. Double-click
3. Selesai — EXAMVAN langsung ada di Start Menu + Desktop

Setelah sekali terinstall, cukup klik ikon EXAMVAN.

<details>
<summary>Kalau muncul "Windows protected your PC" (SmartScreen)</summary>

Installer ini **belum code-signed** — belum ada sertifikat Authenticode, jadi
Windows menampilkan peringatan kuning. Ini normal, bukan file rusak.

Klik **More info** → **Run anyway**.

Kalau ragu, verifikasi checksum dulu (harus cocok dengan yang tertulis di
catatan release):

```cmd
certutil -hashfile EXAMVAN-Setup.exe SHA256
```

</details>

<details>
<summary>Kalau installer gagal / klik tidak merespons</summary>

Gunakan **`EXAMVAN.exe`** (juga ada di release yang sama) — ini versi portable.
Tinggal double-click, tidak perlu install apa pun, bisa langsung dari USB.
Bedanya: tidak ada shortcut Start Menu dan tidak ada uninstaller.

</details>

---

## Untuk Pengembang: Build & Install dari Source

### Prasyarat

| Kebutuhan | Cara Cek | Link Download |
|-----------|----------|--------------|
| **Windows 10/11** 64-bit | Settings → About | — |
| **Python 3.10+** 64-bit (dites di 3.12; hindari 3.14) | Buka CMD, ketik `python --version` | [python.org](https://www.python.org/downloads/) |
| **Visual C++ Redistributable** | Biasanya sudah ada | [vc_redist.x64.exe](https://aka.ms/vs/17/release/vc_redist.x64.exe) |
| **Inno Setup 6** (khusus build installer) | `where iscc` | [jrsoftware.org](https://jrsoftware.org/isdl.php) |

> **Saat install Python, PASTIKAN centang "Add Python to PATH"** di halaman pertama installer.
>
> **Catatan versi:** build & testing proyek ini memakai **Python 3.12** (lihat CI). Python 3.10–3.13 umumnya jalan, tetapi **Python 3.14 belum pernah dites** — kalau install gagal di 3.14, gunakan 3.12 sebelum melapor.

### Build EXAMVAN-Setup.exe (installer yang dibagikan ke siswa)

```cmd
cd EXAMVAN\windows
build-exe.bat          :: 1. build EXAMVAN.exe (portable) — butuh PyInstaller
build-setup.bat       :: 2. bungkus jadi EXAMVAN-Setup.exe — butuh Inno Setup 6
```

Hasil di `windows\dist\`:

| File | Untuk |
|------|-------|
| `EXAMVAN-Setup.exe` | **yang dibagikan ke siswa** — install shortcut, Start Menu, uninstaller |
| `EXAMVAN.exe` | portable — jalankan langsung dari USB, tanpa install |

### Ikon

Ikon EXAMVAN berasal dari ikon launcher Android
(`android/app/src/main/res/mipmap-xxxhdpi/ic_launcher.png`), dikonversi
jadi `windows\installer\examvan.ico` — satu file multi-ukuran
(16/24/32/48/64/128/256) karena Windows memilih ukuran sendiri untuk
shell, taskbar, Start Menu, dan Explorer.

Ikon itu dipakai di tiga tempat: `EXAMVAN.exe` (lewat `--icon` PyInstaller),
`EXAMVAN-Setup.exe` (lewat `SetupIconFile`), dan semua shortcut yang
dibuat installer (lewat `IconFilename`).

Karena `IconFilename` menunjuk ke `{app}\examvan.ico`, ikon itu **harus
benar-benar ikut ter-install** ke folder aplikasi — jangan pakai flag
`dontcopy` di `[Files]`. Smoke test CI memverifikasi file-nya ada dan
headernya sah, bukan cuma shortcut-nya terbentuk.

Regenerasi **hanya** saat ikon brand berubah:

```cmd
python windows\installer\make_icon.py
python windows\installer\make_icon.py --check   :: verifikasi saja
```

Skripnya butuh Pillow atau ImageMagick. **Build tidak pernah memanggil
skrip ini** — `.ico` yang sudah jadi ikut di-commit, jadi tidak ada syarat
tambahan di PC guru, PC siswa, atau runner CI.

Setelan installer:

- **Per-user, tanpa hak admin** — install ke `%LOCALAPPDATA%\Programs\EXAMVAN`, tidak ada dialog UAC. PC sekolah yang memblokir instalasi ke `Program Files` tetap bisa jalan.
- **Password admin exit opsional** — bisa diisi saat instalasi, tersimpan di `%LOCALAPPDATA%\EXAMVAN\admin_password.txt` (bukan di Roaming, supaya tidak ikut ter-sync ke PC lain).
- **Cek Visual C++ Redistributable** — kalau belum ada, installer memperingatkan sebelum aplikasi sempat crash dengan "DLL load failed".
- **Uninstall menawarkan hapus data** — folder config/jawaban/log ditandai, tapi TIDAK dihapus tanpa konfirmasi (jawaban yang belum terkirim sering masih dibutuhkan).

Build ini juga otomatis jalan di CI (`.github/workflows/build-windows.yml`), termasuk **smoke test**: installer benar-benar di-install, exe dijalankan, lalu di-uninstall di runner. Release `exe-latest` selalu berisi SHA256 di catatan release.

### Dapatkan Source Code

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
### Install & Jalankan dari Source

**Cara 0: CMD murni (tanpa PowerShell — paling tahan banting)**

Semua langkah bisa dari CMD biasa:

```cmd
cd EXAMVAN\windows
install.bat
```

`install.bat` melakukan: cek Python → venv (dibuat hanya kalau belum ada /
rusak) → install PyQt5 + PyMuPDF → test import → buat shortcut `EXAMVAN.bat`
di desktop. Error Python/pip tampil apa adanya di layar, jadi gagal install
gampang diagnosis. Shortcut yang dibuat memanggil `run.bat` langsung — **tanpa
PowerShell sama sekali**. Untuk menjalankan sehari-hari: `windows\run.bat`
(setara `run.ps1`, auto-setup venv saat pertama jalan).

Atau jalankan manual tanpa script sama sekali:

```cmd
cd EXAMVAN
python -m venv desktop\.venv
desktop\.venv\Scripts\python.exe -m pip install --upgrade pip
desktop\.venv\Scripts\python.exe -m pip install PyQt5 PyMuPDF
cd desktop
.venv\Scripts\python.exe -m examvan
```

**Cara 1: Install Otomatis via PowerShell**

Buka **PowerShell** sebagai user biasa (tidak perlu admin):

```powershell
# Dari folder EXAMVAN
cd windows
powershell -ExecutionPolicy Bypass -File install.ps1
```

> Catatan: `install.ps1` **selalu** membuat ulang venv dari nol, berbeda dengan
> `install.bat` yang memakai venv yang sudah sehat. Untuk pemakaian berulang,
> pakai `install.bat` saja — tidak perlu unduh ulang PyQt5 tiap kali.

**Cara 2: Jalankan Langsung (tanpa install)**

```powershell
cd EXAMVAN\windows
powershell -ExecutionPolicy Bypass -File run.ps1
```

Script auto buat venv + install deps saat pertama jalan.

### Setup Admin Exit Password (WAJIB)

Tanpa ini, admin exit tidak bisa digunakan (fail-closed — tidak ada password
fallback apa pun).

**Cara tercepat (mode installer):** isi kolom *Password supervisor* di halaman
**Password Admin Exit** — halamannya selalu tampil, tidak ada checkbox.
Password disimpan di `%LOCALAPPDATA%\EXAMVAN\admin_password.txt`.

> Instalasi senyap (`/VERYSILENT`) tidak menampilkan halaman itu. Password yang
> sudah ada **tidak** akan terhapus; ia dibiarkan apa adanya.

**Manual, di CMD** (env var menang atas file — cara lama tetap didukung):

```cmd
set EXAMVAN_ADMIN_PASSWORD=rahasia123
cd EXAMVAN\windows
run.bat
```

**Manual, di PowerShell**:

```powershell
$env:EXAMVAN_ADMIN_PASSWORD = "rahasia123"
cd windows
.\run.ps1
```

---

## Alur Kerja Aplikasi

```
1. Server Config Dialog
   ├─ Masukkan URL server (contoh: https://examvan.my.id)
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
| Block Win+Enter (Narrator), Win+C (Copilot), Win+J, Win+F2..F12 | ✅ (review 30 Sep 2026) |
| Block PrintScreen + Alt+PrintScreen | ✅ (hook + WDA_MONITOR) |
| Block Ctrl+Shift+Esc (Task Manager) | ✅ |
| Block Ctrl+Esc (Start menu) | ✅ |
| Block Escape (alone) | ✅ |
| **Total: 64 blocked key combos** (50 + 14 dari review 30 Sep 2026) | ✅ |
| Clipboard clear tiap 10 detik | ✅ Win32 API |
| Clipboard history di-overwrite | ✅ `EmptyClipboard()` |
| Prevent sleep / monitor mati | ✅ SetThreadExecutionState |
| Dark mode detection | ✅ Registry |
| Multi-monitor detection | ✅ warning log |
| Admin exit password | ✅ fail-closed |
| **Ctrl+Alt+Del** | ❌ SAS — OS level |

---

## Perbaikan mode & performa (29 September 2026)

Dua masalah lapangan di PC Windows low-end, keduanya sudah diperbaiki.

### 1. Mode ujian low/medium/strict "tidak berjalan" — selalu bisa keluar

Server dan client memakai **kosakata level yang berbeda**:

| Tier | Server (`webui`) | Client (`desktop`, SEBELUMYA) |
|------|------------------|-------------------------------|
| rendah | `low` | `low` |
| sedang | `medium` | `medium` |
| tinggi | **`high`** | **`strict`** |

Server tidak pernah mengirim kata `"strict"`
(`internal/database/schema.sql` CHECK `'low','medium','high'`; validasi di
`internal/handlers/admin/exams.go:1524`), sedangkan tiga titik client
membandingkannya secara literal. Untuk mode "Tinggi" dengan `strict_mode=0`,
ketiganya sekaligus gagal: fitur medium **dan** strict tidak aktif, dan
`closeEvent` jatuh ke cabang low → dialog konfirmasi → `event.accept()`.
Ujian yang dijanjikan "TIDAK BISA Keluar" justru bisa keluar tanpa submit.

Perbaikannya: satu kosakata client di `desktop/examvan/security_levels.py`
(`normalize_level` memetakan `high` → `strict`), dipakai oleh
`Exam.level` / `Exam.is_strict` / `Exam.blocks_free_exit` /
`Exam.display_level`. Tidak ada lagi perbandingan literal terhadap string
level di client. Default juga diubah ke `medium` (bukan `low`) supaya
respons API yang rusak tidak diam-diam melepas seluruh proteksi.

Klien Android tidak terpengaruh — `ExamModePolicy` sudah memakai
`securityLevel != LEVEL_LOW`, sehingga tier `high` otomatis tertangani.

### 1b. Keyboard atau mouse tidak bisa dipakai (Linux/X11 saja)

Gejalanya: setelah menjalankan app atau test suite, sebagian tombol tidak
m撼er jadi, kursor tidak bergerak, atau sama sekali tidak ada respon.

Penyebabnya **bukan Windows**. Di Linux mode `strict` memanggil
`x11.grab_keyboard()` dan `grab_pointer()`, dan `XGrabKeyboard`
mengambil alih input **seluruh display** — tidak hanya jendela app. Grab
baru dilepas kalau proses melakukan `XUngrabKeyboard` **atau** koneksi
X-nya terputus. Kalau proses masih hidup (atau suite masih berjalan),
keyboard siapa pun yang menjalankan ikut tersangkut.

Dua hal memperburuk:

* `deleteLater()` pada jendela **tidak** memanggil `deactivate()`, jadi
  `ungrab_keyboard()` tidak pernah jalan;
* `XOpenDisplay` di `x11.py` membuka display **sungguhan** walau Qt
  berjalan di platform `offscreen`, sehingga test pun bisa mengambil alih
  keyboard dan mouse developer's.

Cara melepas sekarang (grab tidak bisa dilepas dari dalam app):

```bash
# 1. Buka terminal dengan MOUSE (Activities -> ketik "Terminal").
# 2. Matikan proses yang grabbed:
pkill -f "python.*unittest"
pkill -f "python.*pytest"
pkill -f EXAMVAN
# 3. Pastikan sudah bersih:
xset q | grep -i grab      # harus: no grabs
```

Kalau terminal tidak bisa dibuka: tekan **Ctrl+Alt+F2** untuk masuk TTY,
login di sana, jalankan `pkill` di atas, lalu `Ctrl+Alt+F1` kembali.
Atau pakai tetikus: **Activities** -> ketik *Terminal*.

Mematikan grab sepenuhnya (berguna saat menelusuri kebocoran, atau
running app di mesin yang tidak boleh dibajak input-nya):

```bash
EXAMVAN_NO_X11_GRAB=1 ./run.sh
```

Grab juga otomatis ditolak kalau `QT_QPA_PLATFORM` bukan `xcb`
(mis. `offscreen`), karena di sana tidak ada window X11 sungguhan untuk
di-grab — test suite desktop berjalan dengan nilai itu.

### 2. Kursor membeku setiap beberapa detik

Bukan karena mengirim data: heartbeat berjalan **60 detik**
(`exam_viewer.py`), dan aplikasi tidak pernah mengambil screenshot.

Penyebabnya `windows_backend.clear_clipboard()` menjalankan
`subprocess.run(["cmd.exe", "/c", "echo.|clip"])` pada timer **3 detik**
di GUI thread. Tiap 3 detik aplikasi me-fork `cmd.exe` yang lalu
me-spawn `clip.exe`, dan `subprocess.run` yang *blocking* membekukan
seluruh event loop Qt selama itu.

Perbaikannya:
- Blok `cmd.exe` dihapus. Win32 `OpenClipboard`/`EmptyClipboard` di
  atasnya sudah mengosongkan clipboard yang sama, jadi blok itu redundan
  murni. Modul `windows_backend` tidak lagi menjalankan proses apa pun.
- Platform clear dipindah ke **worker thread**. `EmptyClipboard` pada
  clipboard berisi data OLE (gambar dari Word, file drop) membuat Windows
  menserialisasi data itu lebih dulu dan bisa blocking lama — di GUI thread
  itu berarti kursor beku.
- Sisi Qt (`QApplication.clipboard()`, main-thread only) tetap inline di
  GUI thread; itu murah.
- Interval timer 3 detik → **10 detik**, dan clear yang masih berjalan
  di-*skip*, bukan di-queue, supaya tidak menumpuk thread basi.

### 3. Strict sekarang benar-benar fullscreen

`_maximize_window()` dipanggil `showMaximized()` **setelah** enforcer
memanggil `showFullScreen()`, sehingga state fullscreen langsung ditimpa.
Sekarang pemanggil menyatakan niatnya lewat `fullscreen=`, dan
`ExamViewerWindow._enforce_fullscreen()` menegakkan ulang fullscreen pada
setiap perubahan state window selama ujian strict — jadi tidak bergantung
pada urutan pemanggilan di satu tempat.

Yang **tetap tidak bisa** diblokir: `Ctrl+Alt+Del` (SAS, level OS) dan
Task Manager yang sudah terbuka sebelum ujian dimulai.

---

## Konsistensi Perilaku dengan Android (fix 16 Agustus 2026)

Klien Windows berbagi codebase dengan Linux (`desktop/examvan/`) dan telah
diselaraskan dengan klien Android agar kontrak submit & re-entry identik:

1. **Satu identitas perangkat** (`DESKTOP:<hash>`) untuk approval, unduhan PDF
   dan submit — dulu approval/PDF memakai MAC mentah sehingga baris approval
dan submission tidak match di server.
2. **Submit 202 queued TIDAK dianggap sukses final** — klien mem-poll
   `GET /result` sampai worker mengonfirmasi `done`; jawaban lokal tidak
dihapus pada 202 mentah.
3. **Fallback jawaban disk** saat submit kosong (deadline menembak sebelum
   restore) — mencegah submit kosong menimpa jawaban asli di window grace.
4. **Tanpa soal → lembar jawaban kosong** — tidak ada fabrikasi 40 soal dummy.
5. **Marker sticky "ujian sudah selesai"** — re-entry ujian yang sama diblokir
   agar tidak mengirim submit kosong menimpa jawaban yang sudah terkirim.
6. **Server time skew** — countdown memakai selisih jam perangkat vs server
   (`server_time_utc` dari `/api/health`), akurat walau jam lokal meleset.
7. **Presence siswa** — kirim `login`/`heartbeat`/`logout` via `/access-log`
   dan `/complete` setelah submit, sehingga siswa tampil online/offline di
   dashboard monitoring pengawas.
8. **WebSocket real-time** — koneksi `/ws/<exam_id>` menerima event pengawas;
   **`exam_terminated`** langsung memicu auto-submit.
9. **Auto-submit menutup window SEGERA** — deadline / `exam_terminated` /
   fokus hilang medium / close medium-strict: jawaban di-flush ke disk +
   marker sticky, kunci dilepas dan window ditutup langsung (tidak menunggu
   jaringan, tidak ada lagi siswa terjebak di layar terkunci saat jaringan
   mati). Submit berjalan di background; hasil dilaporkan via notifikasi
   (Linux `notify-send`; Windows balloon tip via PowerShell `NotifyIcon` —
   bawaan .NET Framework, tanpa dependency baru). **Sukses** → jawaban lokal
   dihapus + `complete` presence; **gagal** → jawaban tetap di disk dan
   re-entry menawarkan **"Kirim Lagi"** (server idempoten). Bila notifikasi
   gagal dipicu (PowerShell diblokir policy, helper tidak ada) → fallback
   aman ke recovery re-entry: jawaban tetap tersimpan, tidak ada jalan buntu.
10. **`congrats_message` custom guru** ditampilkan saat sukses (submit manual
    maupun auto) — bukan hanya pesan bawaan server.
11. **Countdown akurat setelah suspend** — deadline dihitung ulang dari
    `end_time` absolut + skew saat window aktif kembali (monotonic clock
    tidak termasuk waktu tidur laptop).

## Uninstall

**Mode installer (paling umum):** Settings → Apps → EXAMVAN → Uninstall.
Installer menanyakan apakah **kedua** folder data ini ikut dihapus:

| Folder | Isi |
|---|---|
| `%LOCALAPPDATA%\EXAMVAN` | `admin_password.txt` saja |
| `%USERPROFILE%\.config\examvan` | jawaban ujian yang belum terkirim, `config.json` (URL server, token, identitas), `app.log`, `windows_state.json` |

Pilih **No** kalau masih ada jawaban yang belum terkirim — folder kedua
adalah satu-satunya tempat jawaban itu disimpan.

**Mode source:**

1. Hapus shortcut `EXAMVAN.bat` dari desktop
2. Hapus folder `desktop\.venv\`
3. (Opsional) Hapus folder `EXAMVAN\`
4. (Opsional) Hapus `%USERPROFILE%\.config\examvan\`

**Mode portable:** hapus file `EXAMVAN.exe`. Tidak ada yang ter-install.
Folder `.config\examvan` tetap ada (jawaban + log) — hapus manual bila
memang tidak diperlukan.

---

## Troubleshooting

### Installer berhenti di tengah / "Windows protected your PC"

Lihat [bagian SmartScreen](#kalau-muncul-windows-protected-your-pc-smartscreen)
di atas. Peringatan itu muncul karena exe belum code-signed.

### Error saat mengetik `powershell -ExecutionPolicy Bypass -File install.ps1`

**Parse error `Unexpected token '-bit'`** — terjadi di versi lama `install.ps1`:
ada string tanpa tanda kutip yang mengandung `(64-bit)`, sehingga PowerShell
masuk mode ekspresi saat ketemu `(` dan gagal parse **seluruh file** sebelum
eksekusi (makanya error muncul seketika saat perintah diketik). Sudah diperbaiki
— update repo, atau langsung pakai `install.bat`.

Kalau gagal karena sebab lain (diblokir policy, PowerShell lambat/rusak, atau
error `File ... cannot be loaded`), pakai **CMD murni**:

```cmd
cd EXAMVAN\windows
install.bat
```

atau jalankan manual tanpa script:

```cmd
cd EXAMVAN
python -m venv desktop\.venv
desktop\.venv\Scripts\python.exe -m pip install PyQt5 PyMuPDF
desktop\.venv\Scripts\python.exe -m pip install --upgrade pip
cd desktop
.venv\Scripts\python.exe -m examvan
```

### Error saat klik `EXAMVAN.bat` di desktop

Shortcut buatan versi lama memanggil `run.ps1` **lewat PowerShell** — kalau
PowerShell di komputer bermasalah (policy, PowerShell 7, env korporat korup),
ikon desktop ikut gagal walau `install.bat` sukses. Perbaikan:

1. `git pull` (ambil perbaikan),
2. jalankan ulang `windows\install.bat` — shortcut dibuat ulang dan kini
   memanggil `run.bat` (CMD murni, tanpa PowerShell).

Alternatif cepat tanpa shortcut: double-click `windows\run.bat` langsung.

### `build-setup.bat` bilang "Inno Setup 6 tidak ditemukan"

Sudah terpasang tapi tidak ketemu karena `iscc.exe` tidak pernah masuk PATH.
Script sudah cek lokasi umum + registry, jadi biasanya memang belum
terpasang. Install dari https://jrsoftware.org/isdl.php, lalu jalankan ulang.

### "Python tidak ditemukan"

Install Python dari [python.org](https://www.python.org/downloads/).  
**Centang "Add Python to PATH"** saat install.

> Tidak relevan untuk guru/siswa — installer `EXAMVAN-Setup.exe` tidak
> butuh Python sama sekali.

### "ImportError: No module named PyQt5"

Hanya untuk mode source:

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

Installer `EXAMVAN-Setup.exe` sudah memperingatkan hal ini di akhir proses
kalau Redistributable terdeteksi belum ada.

### "python" tidak dikenali setelah install

Tutup dan buka ulang PowerShell/CMD. Cek `where python` — kalau hasilnya
`...\WindowsApps\python.exe`, yang kedetect adalah stub Microsoft Store:
PATH python.org belum masuk. Install ulang Python dengan centang
"Add Python to PATH", atau panggil langsung:
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

### Melihat log aplikasi

App menulis log rotating ke `%USERPROFILE%\.config\examvan\app.log`
(1 MB × 3 file). Buka file ini saat melapor masalah — proses `--windowed`
tidak punya console, jadi ini satu-satunya jejak error (security hook,
download PDF, submit, notifikasi).

Saat melapor, sertakan juga **Properties → Details** dari `EXAMVAN.exe` di
folder `%LOCALAPPDATA%\Programs\EXAMVAN` — di situ ada versi, build, dan commit
yang dipakai di PC tersebut.
