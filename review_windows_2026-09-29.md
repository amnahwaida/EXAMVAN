# Review Aplikasi Windows — EXAMVAN

> Tanggal: 29 September 2026 · Scope: rantai build/packaging/installer Windows (`windows/`), client desktop yang dibekukan jadi `.exe` (`desktop/examvan/`), dan integritas mode ujian di Windows.
> Status dokumen: **DITEMUKAN** — 13 temuan (1 critical, 4 high, 4 medium, 4 low). 5 di antaranya **live** (bukan laten).
> Review ini adalah **ronde kedua**. Ronde pertama (29 Sep 2026, di hari yang sama) menemukan 4 masalah mode-ujian & performa yang **sudah diperbaiki** — lihat [Bagian 7](#bagian-7--sudah-diperbaiki-ronde-1).

## Ringkasan

| Area | Status |
|---|---|
| Mode ujian (low/medium/strict) | ✅ **Selesai** ronde 1 — bug "selalu bisa keluar" sudah ditutup, 198 test hijau |
| Performa GUI (freeze kursor) | ✅ **Selesai** ronde 1 — penyebabnya `cmd.exe` tiap 3 detik, sudah dihapus |
| Fullscreen mode strict | ✅ **Selesai** ronde 1 — `showMaximized()` sempat menimpa `showFullScreen()` |
| Password admin exit | ❌ **1 critical** — instalasi senyap/upgrade menghapus password tanpa jejak (Bagian 1) |
| Petunjuk password ke user | ❌ **1 high** — dialog aplikasi & README menunjuk checkbox yang sudah dihapus (Bagian 2) |
| Uninstall & lokasi data | ❌ **1 high** — prompt menyebut folder & isi yang salah (Bagian 3) |
| Identitas versi | ❌ **1 high** — versi di layar ≠ versi installer ≠ versi resource exe (Bagian 4) |
| Siklus hidup keyboard hook | ❌ **1 high** — state global + instance per-enforcer bisa kehilangan hook (Bagian 5) |
| Rantai build & smoke test | ⚠️ **4 medium** — smoke test tidak menguji apa pun yang rawan rusak; build bisa mengemas exe basi (Bagian 6) |

---

## Bagian 1 — CRITICAL: instalasi senyap menghapus password supervisor

**Lokasi:** `windows/installer/examvan.iss:252-257`

```pascal
if FileExists(PwFile) then
  DeleteFile(PwFile);          // :254  — tanpa syarat

if PwValue <> '' then
  SaveStringToFile(PwValue, PwFile, False);   // :256-257  — bersyarat
```

Hapus **tanpa syarat**, tulis **bersyarat**. Dua kondisi membuat `PwValue` kosong, dan keduanya tanpa warning apa pun:

1. **Instalasi senyap/skrip.** `/VERYSILENT` (atau `/SILENT`) tidak pernah menampilkan halaman password, jadi `AdminPasswordPage.Values[0]` tetap `''` (`:236-237`). File dihapus, tidak ada yang ditulis, exit code 0.
2. **Upgrade interaktif yang di-klik "Next" saja.** Teks halaman sendiri (`:196`) menulis *"Boleh dikosongkan"* dan `:199` menulis *"Bisa diubah kapan saja dengan install ulang"* — yang tersirat tersirat adalah "tidak berubah". Yang terjadi sebenarnya adalah **hapus**.

**Dampak.** `desktop/examvan/ui/exam_viewer.py:25-47` → `_ADMIN_PASSWORD = None` → `:885-890` menampilkan "Admin exit tidak dikonfigurasi". Berdasarkan desain fail-closed proyek sendiri, **supervisor tidak bisa lagi menutup ujian yang sedang berjalan**. Tidak ada pesan, tidak ada baris log, tidak ada exit code non-nol.

Ini persis skenario yang jadi alasan fitur ini ada: **mend deploying build baru ke seluruh ruang kelas**. Setiap kali installer diperbarui dengan `/VERYSILENT`, seluruh lab kehilangan password.

**Mengapa CI tidak menangkap.** `build-windows.yml:175` memasang dengan `/VERYSILENT`. Di runner yang bersih tidak ada password untuk dihapus, dan smoke test tidak pernah memassert isi file itu. **Satu-satunya jalur yang punya bug adalah jalur yang tidak pernah diuji CI.**

**Catatan.** `DeleteFile` sebelum tulis memang **dibutuhkan** — `SaveStringToFile` membuka file tanpa truncate, jadi password lama 20 karakter diganti yang 8 akan menyisakan 12 byte lama di akhir file sehingga password baru ikut salah baca. Komentar `:249-252` menjelaskan ini dengan benar. Yang salah adalah hapus yang tidak bersyarat; perbaikannya adalah mengikat hapus pada kondisi menulis:

```pascal
if PwValue <> '' then
begin
  if FileExists(PwFile) then
    DeleteFile(PwFile);
  SaveStringToFile(PwValue, PwFile, False);
end;
```

`MsgBox` di `:244-247` (dua password tidak sama) tidak bisa muncul di silent mode — nilainya sama secara trivial — jadi saat ini **tidak ada sinyal apa pun** ke operator.

---

## Bagian 2 — HIGH: dua petunjuk yang mengarahkan user ke jalan buntu

**Lokasi:** `desktop/examvan/ui/exam_viewer.py:887` dan `windows/README.md:157-159`

Keduanya sama-sama memberi tahu user:

> • Saat instalasi, centang "Konfigurasi password admin exit"

Checkbox itu **sudah dihapus**. `windows/installer/examvan.iss:99-104` menyatakan sebaliknya secara eksplisit:

> Task "konfigurasi password admin exit" yang sebelumnya ada di sini DIHAPUS: halaman password sekarang selalu tampil … Checkbox yang tidak dibaca hanya menambah satu kondisi yang bisa salah tanpa untung nyata.

`[Tasks]` sekarang hanya berisi `desktopicon` (`:105`). Saya grep seluruh repo: string itu masih hidup hanya di dua tempat user-facing di atas plus komentar di `.iss`.

**Dampak.** Guru yang **baru saja terkunci di luar ujian** diberi tahu untuk membuka installer dan mencentang checkbox yang tidak ada di halaman mana pun. Jalur yang benar (halaman isian yang selalu tampil, atau tulis ulang `%LOCALAPPDATA%\EXAMVAN\admin_password.txt`) tidak disebutkan. Ini momen paling buruk untuk berupa dead end.

---

## Bagian 3 — HIGH: prompt uninstall menyebut folder & isi yang salah

**Lokasi:** `windows/installer/examvan.iss:279-286`, diulang di `windows/README.md:342-345`

Prompt mengatakan `%LOCALAPPDATA%\EXAMVAN` berisi *"konfigurasi server, password admin exit, jawaban ujian yang belum terkirim, dan app.log"*. Dari empat item itu, **hanya satu** yang benar.

| Data | Path sebenarnya | Sumber |
|---|---|---|
| `admin_password.txt` | `%LOCALAPPDATA%\EXAMVAN\` | `exam_viewer.py:36-38` ✅ |
| `config.json` (URL server, token, identitas) | `%USERPROFILE%\.config\examvan\` | `config.py:15-16` ❌ |
| `answers_<id>.dat` (jawaban belum terkirim) | `%USERPROFILE%\.config\examvan\` | `config.py:99` ❌ |
| `submitted_*`, `start_time_*` | `%USERPROFILE%\.config\examvan\` | `config.py:170-191` ❌ |
| `app.log` | `%USERPROFILE%\.config\examvan\` | `__main__.py:24` ❌ |
| `windows_state.json` | `%USERPROFILE%\.config\examvan\` | `windows_backend.py:559-560` ❌ |

**Dampak, dua arah.**

1. User menjawab **Ya** ("hapus data saya") → **jawaban ujian, log, dan konfigurasi server tidak terhapus**. Semuanya tetap di `%USERPROFILE%\.config\examvan`.
2. User menjawab **No** untuk melindungi jawaban yang belum terkirim → **tidak terlindungi apa pun**; hanya file password yang dipertahankan.

Dan folder yang benar-benar menampung jawaban + log **tidak pernah ditawarkan untuk dihapus sama sekali**, jadi diam-diam menumpuk di PC lab yang dipakai bersama — termasuk jawaban ujian dan log diagnosis.

---

## Bagian 4 — HIGH: versi di layar bertentangan dengan versi installer

**Lokasi:** `desktop/examvan/__init__.py:3-4`, `desktop/examvan/ui/server_config.py:135`, `desktop/examvan/__main__.py:131`

```python
# __init__.py:3-4
__version__ = "1.0.0"
APP_VERSION = "2.5.0"  # Sent as X-App-Version to pass backend version check

# server_config.py:135
ver_label = QLabel(f"v{__version__} (API {APP_VERSION})")   # → "v1.0.0 (API 2.5.0)"

# __main__.py:131
app.setApplicationVersion("1.0.0")   # literal ketiga
```

Angka yang **benar-benar dibaca guru di layar** adalah `1.0.0`. Installer meanwhile melaporkan `2.5.0` (`examvan.iss:53` ← `build_info.py`).

**Dampak.** Guru melapor "aplikasinya tulis v1.0.0" → installer bilang 2.5.0 → release notes bilang 2.5.0 → Properties exe bilang `1.0.0.0` (lihat M3). Laporan bug tidak bisa dicocokkan ke build — persis kegagalan yang `windows/installer/build_info.py:126-133` menyatakan jadi alasan modul itu dibuat.

---

## Bagian 5 — HIGH: mode strict bisa diam-diam kehilangan keyboard hook

Ini temuan yang saya periksa sendiri, bukan dari review build.

**Lokasi:** `desktop/examvan/security/__init__.py:13-21` vs `desktop/examvan/security/windows_backend.py:268-275`

Dua fakta yang tidak cocok:

1. `get_backend()` **mengembalikan instance `WindowsBackend()` baru setiap dipanggil**. Docstring-nya mengklaim "Called once at app startup", tapi tidak ada yang menegakkan itu — pemanggilnya `security/enforcer.py:68`, dipanggil sekali per `SecurityEnforcer`, dan `SecurityEnforcer` dibuat sekali per `ExamViewerWindow`.
2. `_hook_id`, `_hook_thread`, `_hook_thread_id`, `_hook_callback`, `_hook_proc_wrapper`, `_hook_ready` adalah **module globals** — dibagi ke semua instance.

Skenario. Siswa menyelesaikan ujian 1 (strict) → `deactivate()` → `release_strict_mode()` → `_stop_keyboard_hook()` yang melakukan `join(timeout=1.0)` (`windows_backend.py:927`). Thread hook yang sedang berhenti mungkin belum keluar dalam 1.0 detik. Siswa memulai ujian 2 → instance backend baru → `_start_keyboard_hook()` → thread baru menulis `_hook_id = <hook baru>`. **Lalu thread lama menyelesaikan `finally`-nya** (`windows_backend.py:524-528`):

```python
finally:
    if _hook_id:                      # <-- hook BARU, bukan hook miliknya
        _UnhookWindowsHookEx(_hook_id)
        _hook_id = None
```

Hook baru di-unhook dan `_hook_id` di-nolkan. Instance baru masih punya `_hook_installed = True`, jadi `release_strict_mode` nanti tidak mencoba apa-apa dan tidak ada retry.

**Dampak.** Alt+Tab, Win key, Win+L, Win+D, Ctrl+Shift+Esc **berhenti diblokir** sementara banner tetap menampilkan "STRICT" dan `closeEvent` masih auto-submit. Siswa bisa membuka Start menu atau Task Manager di tengah ujian. Ketidakseimbangan ini tidak pernah dikomunikasikan ke user maupun log.

**Catatan.** Jendelanya sempit (1.0 s join), jadi ini balapan, bukan kegagalan pasti. Tapi konsekuensinya adalah batas integritas ujian yang hilang **secara senyap**, dan kode sendiri sudah menyadarinya sensitif: komentar `:917` dan `:930` justru karena itu tidak meng-nolkan `_hook_id` di `_stop_keyboard_hook` ("race: we'd null it before the thread reads it, leaking the hook"). Lifecycle lintas-instance tidak pernah dipertimbangkan.

**Arah perbaikan.** (a) jadikan owner state hook eksplisit — satu proses/thread hook, di-refcount; atau (b) jadikan `_hook_id`+`_hook_ready` milik instance, bukan module global; atau (c) paling murah: jadikan `get_backend()` benar-benar shared instance (module-level cache), sesuai yang docstring-nya sudah janjikan.

---

## Bagian 6 — MEDIAH & LOW: rantai build, smoke test, dan version resource

### 6.1 M1 — Smoke test Windows tidak bisa mendeteksi kegagalan packaging yang justru ada tujuannya

`build-windows.yml:185-197` hanya memassert proses **tidak exit dalam 12 detik**. Itu seluruh tes fungsionalnya.

Yang benar-benar terjadi di 12 detik itu: `__main__.py:139-184` memunculkan `ServerConfigDialog` lalu masuk ke Qt event loop. **Tidak ada** network request, **tidak ada** `api.py`, **tidak ada** `ws.py`/QtWebSockets, **tidak ada** `pdf_viewer.py`/`fitz`, **tidak ada** `SecurityEnforcer`/`windows_backend` (hook ctypes). Setiap komponen yang paling rawan rusak oleh kesalahan packaging tidak pernah dieksekusi.

Ini bukan hipotetis. `ci.yml:110-117` mendokumentasikan bahwa paket `.deb` Linux **berkali-kali** shipped tanpa `notify.py`, `ws.py`, `security/base.py`, `linux_backend.py`, `ui/waiting_approval.py` — dan job Linux sekarang memverifikasi keberadaan modul + kesamaan versi terhadap `APP_VERSION` (`ci.yml:129-152`). **Job Windows tidak punya padanan sama sekali.** Kelas bug yang sudah pernah menggigit proyek ini terbuka penuh di Windows.

Langkah yang lolos diam-diam kalau gagal:
- `:182-184` komentar menyebut "Shortcut harus terbentuk (desktop + Start Menu)", tapi shortcut yang hilang hanya `Write-Host "WARN: …"` — tidak pernah menggagalkan build. Shortcut `{group}` Start Menu (`.iss:124-125`) tidak dicek sama sekali.
- `:224` exit code uninstaller hanya di-log, tidak di-assert. Hanya `EXAMVAN.exe` yang di-assert hilang (`:228`). Sisa `{group}\*.lnk`, `%USERPROFILE%\Desktop\EXAMVAN.lnk`, uninstall key HKCU, dan `%LOCALAPPDATA%\EXAMVAN` tidak pernah diverifikasi.
- `:98-107` "Stamp version info" mencetak `Select-Object -First 3` — tiga baris komentar `#` di `version_info.txt`. Dua substitusi regex tidak pernah diverifikasi; reformat file itu membuat stamping berhenti matches diam-diam.
- `install.bat` / `install.ps1` / `run.bat` / `run.ps1` tidak pernah dijalankan CI sama sekali, jadi setiap cacat di sana lolos tanpa terdeteksi.

### 6.2 M2 — `build-setup` akan dengan senang hati mengemas exe basi

`build-setup.bat:42-51` hanya menguji `-exist`; `build-setup.ps1:34-38` hanya `Test-Path`. Tidak ada cek mtime, tidak ada manifest.

Bukti langsung di working tree ini: `windows/dist/EXAMVAN.exe` ber-mtime **2026-09-27 11:58**, sementara delapan file sumber berubah **2026-09-29 05:08-05:20** (`security/windows_backend.py`, `models.py`, `security/enforcer.py`, `ui/exam_viewer.py`, `__main__.py`, `security/base.py`, `security/linux_backend.py`, `ui/styles.py`).

Artinya: menjalankan `build-setup.bat` hari ini mencetak **BUILD SUCCESS** dan menghasilkan `EXAMVAN-Setup.exe` berisi kode **dua hari lalu** — belum perbaikan ronde 1 — untuk dibagikan ke siswa, tanpa ada apa pun yang mengatakannya. Kelas staleness yang sama yang baru saja diperbaiki untuk `.deb` di `ci.yml`.

### 6.3 M3 — Version resource exe tidak pernah di-stamp

`windows/installer/version_info.txt:17-18` hardcode `filevers=(1, 0, 0, 0)` / `prodvers=(1, 0, 0, 0)`. `build-windows.yml:103-104` hanya men-*patch* dua nilai `StringStruct` (`:34`, `:38`) — sudah saya verifikasi look-behind-nya. `FixedFileInfo` tidak tersentuh.

Dampak:
- **Versi biner** setiap `EXAMVAN.exe` yang dibangun CI adalah `1.0.0.0`, selamanya, sementara `VersionInfoVersion` `EXAMVAN-Setup.exe` adalah `2.5.0.0` (`.iss:53`). Add/Remove Programs menampilkan 2.5.0; resource exe berbunyi 1.0.0.0.
- **Build lokal** (`build-exe.bat:86`, `build-exe.ps1:64`) mengirim file itu **tanpa perubahan** sama sekali, jadi exe lokal melaporkan `1.0.0` / `1.0.0.0` dan **tidak membawa build maupun commit**.

Ini langsung falsifikasi `build-exe.bat:75-77` (*"Properties di explorer konsisten antara build lokal dan build runner"*) dan `windows/README.md:482-483` (*"Properties → Details … di situ ada versi, build, dan commit"*). Keduanya hanya benar untuk exe built-CI, dan hanya untuk `ProductVersion`.

### 6.4 M4 — Rantai Windows mengabaikan `desktop/requirements.txt` sepenuhnya

`desktop/requirements.txt` (`PyQt5>=5.15`, `PyMuPDF>=1.23`) dikonsumsi rantai Linux (`install.sh:70,77,332`, `run.sh:45,65`, `build-deb.sh:46,93,212`) dan **tidak oleh apa pun** di Windows. Semua skrip dan workflow menghardcode daftarnya: `build-exe.bat:55`, `install.bat:73`, `build-windows.yml:93`, `ci.yml:104`. Tidak ada `-r requirements.txt` di satu pun skrip Windows.

Dampak: menambahkan dependency ke `requirements.txt` **tidak berefek** pada artefak Windows mana pun. Tidak ada lock, tidak ada floor → dua build berjarak seminggu bisa_mem bake PyQt5/PyMuPDF berbeda ke dalam artefak yang versinya identik.

Secara konkret dan masih laten: venv di repo ini punya **PyMuPDF 1.27.2.3**, dan `install.bat:51` / `install.ps1:78` memutuskan "venv sehat" lewat `import PyQt5, fitz`. `fitz` kini hanya shim re-export tipis. `import fitz` hari ini masih resolve (sudah saya verifikasi), jadi ini **laten, bukan kerusakan sekarang** — tapi PyMuPDF yang suatu saat membuang shim itu akan (a) membuat `install.bat` membuat ulang venv **selalu**, (b) menggagalkan `install.bat` di `:85`, dan (c) memecahkan `pdf_viewer.py:13` saat runtime — semuanya di PC siswa, tanpa pin yang mencegahnya.

### 6.5 L1 — `AppMutex` adalah konfigurasi mati, dan komentarnya menyebut mekanisme yang salah

`.iss:25` mendefinisikan `AppMutex="EXAMVAN_Setup_Install"`, dipakai di `:74`, dengan komentar "Cegah dua installer jalan bersamaan".

Menurut dokumentasi JrSoftware, `AppMutex` membuat Setup/Uninstall **menolak jalan selama aplikasi memegang mutex itu**, dan mewajibkan aplikasi memanggil `CreateMutex` dengan nama yang cocok. Saya grep seluruh `desktop/` — **tidak ada `CreateMutex` di mana pun** (hanya `SetWindowsHookEx` di `windows_backend.py:185-186,486`). Jadi pemeriksaan itu tidak pernah bisa menyala.

Dua kesalahan independen: (a) proteksi yang diklaim tidak ada; (b) `AppMutex` **bukan** mekanisme mencegah dua installer berjalan bersamaan — itu `SetupMutex`.

Tidak merusak fungsional hari ini (`CloseApplications=yes` via Restart Manager menutup kasus sebenarnya), tapi pengembang berikutnya akan mengira ada jaring pengaman.

### 6.6 L2 — Cek `VCRedistPresent` tidak lengkap

`.iss:165-171` hanya memeriksa `{sys}\vcruntime140.dll` dan `{sys}\msvcp140.dll`. Runtime MSVC x64 yang di-link `Qt5Core.dll` adalah `msvcp140.dll` + `vcruntime140.dll` + **`vcruntime140_1.dll`**. Ketiganya tidak di-bundle di `windows/dist/EXAMVAN.exe` (0 kemunculan tiap-tiganya), jadi redist memang dependency eksternal.

Mesin dengan redist lama/parsial punya dua yang pertama tapi tidak yang ketiga: installer tetap diam dan siswa tetap dapat *"DLL load failed"* — persis hasil yang dicegah oleh cek di `.iss:141-145`. **Ditandai sebagai belum terverifikasi:** saya tidak punya host Windows untuk mengujinya.

### 6.7 L3 — `run.bat` / `run.ps1` tetap jalan meski install dependency gagal

`run.bat:52-56` (identik `run.ps1:68-72`) menjalankan `pip install` **tanpa cek exit code**, lalu jatuh langsung ke blok launch. Jalur *first-time* justru benar (`run.bat:41-46`, `run.ps1:52-58`), jadi kedua jalur tidak konsisten.

Dampak: venv parsial/korup menghasilkan raw Python traceback, bukan pesan actionable "Gagal install dependencies. Coba manual…". Di PC sekolah di belakang proxy yang diblokir, traceback itulah yang dilihat guru.

### 6.8 L4 — `build-exe.ps1` tidak pernah cek exit code pip; pesan errornya salah ketik

`build-exe.ps1:39-40` menjalankan dua `pip install` tanpa cek `$LASTEXITCODE`. `$ErrorActionPreference = "Stop"` (`:8`) **tidak** mengubah exit code non-nol dari program native menjadi terminating error di Windows PowerShell 5.1. Pip yang gagal (proxy terblokir) jatuh ke PyInstaller, yang lalu gagal, dan user melihat `build-exe.ps1:92`:

> `Build failed — EXAVAN.exe not found in …`

— salah ketik (`EXAVAN`), dan menyembunyikan penyebab sebenarnya. `install.ps1:64` dan `run.ps1:52` sudah benar; `build-exe.ps1` tidak.

---

## Bagian 7 — SUDAH DIPERBAIKI (ronde 1)

Empat masalah yang ditemukan dan diperbaiki pada 29 September 2026, dengan TDD (8 siklus RED→GREEN→REFACTOR). Test suite: **137 → 198 test, hijau**. Setiap perbaikan di-*mutation-check* (dikembalikan ke kode lama → test harus gagal) untuk memastikan test-nya tidak vacuous. Rincian di `windows/README.md` § "Perbaikan mode & performa (29 September 2026)".

| # | Masalah | Perbaikan |
|---|---|---|
| 1 | **Mode "Tinggi" selalu bisa keluar.** Server kirim `security_level="high"`, client hanya kenal `"strict"`, sehingga tiga titik membandingkannya secara literal. Untuk `high` + `strict_mode=0`: fitur medium **dan** strict tidak aktif, dan `closeEvent` jatuh ke cabang low → dialog konfirmasi → `event.accept()`. | Modul kosakata tunggal `desktop/examvan/security_levels.py` (`normalize_level("high") → "strict"`), dipakai lewat `Exam.level` / `is_strict` / `blocks_free_exit` / `display_level`. Tidak ada lagi perbandingan literal string level. Default `low` → `medium` (fail-secure). |
| 2 | **Kursor membeku tiap 3 detik.** `windows_backend.clear_clipboard()` menjalankan `subprocess.run(["cmd.exe","/c","echo.|clip"])` di GUI thread tiap 3 detik — me-fork `cmd.exe` yang me-spawn `clip.exe`, membekukan event loop Qt. Bukan heartbeat (itu 60 detik) dan bukan screenshot (aplikasi tidak pernah mengambil layar). | Blok `cmd.exe` dihapus (redundan murni — Win32 `EmptyClipboard` di atasnya sudah mengosongkan clipboard yang sama). Platform clear dipindah ke worker thread, sisi Qt tetap inline, interval 3s → 10s, clear yang masih jalan di-*skip* bukan di-queue. |
| 3 | **Strict tidak benar-benar fullscreen.** `_maximize_window()` memanggil `showMaximized()` **setelah** enforcer memanggil `showFullScreen()`, sehingga state-nya langsung ditimpa. | `_maximize_window(widget, fullscreen=)`; `ExamViewerWindow._enforce_fullscreen()` menegakkan ulang fullscreen di tiap perubahan state window selama ujian strict. |
| 4 | **`SECURITY_COLORS` tidak punya key `"high"`** → banner jatuh ke warna low, dan `mode.upper()` pada string mentah membuat **seluruh window crash** kalau server kirim `"security_level": null`. | Dict jadi total untuk vocabulary server; banner memakai `Exam.display_level` sehingga mustahil melempar. |

Bug tambahan yang terungkap oleh test: `enforcer.py` kini punya `CLIPBOARD_INTERVAL_MS` / `FOCUS_POLL_INTERVAL_MS` sebagai konstanta bernama, dan `SecurityBackend.clear_clipboard()` didokumentasikan eksplisit sebagai **worker-thread + no-Qt + jangan spawn proses** di `security/base.py:56-70`.

---

## Bagian 8 — Yang sudah DIVERIFIKASI benar (jangan di-audit ulang)

- **`CreateInputQueryPage` 4 argumen** (`.iss:192-199`) — prototipe asli memang `(AfterID: Integer; ACaption, ADescription, ASubCaption: String)`. Komentar `:176-183` benar. `Add(Prompt, IsPassword)` 2 argumen valid.
- **`install.bat:101` `for /f "tokens=2,*"`.** Benar. Sesuai dokumentasi `for` Microsoft, `tokens=n,*` memberi token *n* ke variabel pertama dan *sisa setelah token n* ke variabel `*` — jadi `%%B` adalah path dengan prefiks `REG_EXPAND_SZ` yang sudah dibuang. (`tokens=3,*` akan salah.)
- **`pause` di `install.ps1` / `run.ps1`** — valid; `about_Built-in_Functions` mendokumentasikan `pause` sebagai built-in function yang mereplikasi PAUSE cmd.exe.
- **Tidak ada `.isl` Indonesia** — `Name: "english"; MessagesFile: "compiler:Default.isl"` (`.iss:97`) sudah benar, dan `.iss:99-100` mencadangkan slot untuk menambahkan ID saat waktunya tiba.
- **`[Run] postinstall` vs `CurPageChanged(wpFinished)`** — cek VCRedist jalan **sebelum** app dijalankan (entri `postinstall` diproses setelah halaman Setup Completed ditampilkan). Urutannya benar.
- **`AppId={{7C3F1B0E-…}`**, **`PrivilegesRequired=lowest`** + `DefaultDirName={localappdata}\…`, **`ArchitecturesAllowed=x64compatible`** (valid untuk Inno 6.3+) — konsisten.
- **Tidak ada kompresi UPX** di exe yang sudah dibangun — 7 section, 0 magic `UPX!`.
- **`--exclude-module examvan.security.kiosk|x11|linux_backend` aman** — satu-satunya jalur yang bisa mencapainya (`enforcer.py:360`) ada di balik `--kiosk`, yang tidak pernah dilewati installer, dan dibungkus `except ImportError`.
- **Set `--hidden-import` saat ini memadai.** Saya periksa TOC archive `windows/dist/EXAMVAN.exe`: `examvan.ui.waiting_approval`, `examvan.security.windows_backend`, `pymupdf.mupdf`, `pymupdf.pymupdf`, `_mupdf.pyd`, dan `Qt5WebSockets.dll` semuanya ada; tiga modul yang dikecualikan memang absen. `PyQt5.QtWebSockets` tidak punya `--hidden-import` tetapi `ws.py:19` mengimpornya di top level, jadi hook PyQt5 menutupinya.
- **`--add-data "…;examvan"`** adalah bobot mati (`:103`) — tidak ada kode yang membaca file bundel saat runtime (satu-satunya hit `__file__`/`_MEIPASS` ada di `kiosk.py:162`, dan `kiosk` dikecualikan). Semua state goes to `Path.home()`. Harmless, tapi build lokal ikut meng-*embed* `__pycache__` yang stale sementara CI (checkout bersih) tidak — artefak lokal dan CI tidak identik tanpa alasan.
- **`numeric_version` di `build_info.py:83-98` tidak benar** untuk versi dengan segmen tahun di tengah (`'release-2026.09'` → `255.9.0`, karena `min(n,255)` di `:121` menyaturasi 2026). **Hanya laten** — tidak terjangkau dengan `APP_VERSION = "2.5.0"` sekarang. Tidak ada `desktop/tests/test_build_info.py` padahal `build_info.py:6-7` mengklaim logikanya sudah dites.

---

## Bagian 9 — Rekomendasi prioritas

### 9.1 Langsung (hari ini — kecil, dampak besar)

| # | Aksi | File |
|---|---|---|
| 1 | Hapus file password **hanya** di cabang yang memang menulis (`if PwValue <> ''`). | `.iss:252-257` |
| 2 | Tambahkan assertion di smoke test: setelah `/VERYSILENT`, `admin_password.txt` dari install sebelumnya **masih ada dan isinya utuh**. | `build-windows.yml:175+` |
| 3 | Ganti teks dialog aplikasi & README: hapus "centang Konfigurasi password admin exit", ganti "isi password di halaman Password Admin Exit saat instalasi". | `exam_viewer.py:887`, `windows/README.md:157-159` |
| 4 | Perbaiki prompt uninstall: sebut `%USERPROFILE%\.config\examvan` dan tawarkan hapus **kedua** folder, dengan isi yang benar per tabel di Bagian 3. | `.iss:279-286`, `windows/README.md:342-345` |
| 5 | Samakan `__version__` dan `APP_VERSION`, dan hapus literal `"1.0.0"` di `__main__.py:131`. | `__init__.py:3-4`, `server_config.py:135`, `__main__.py:131` |
| 6 | Tambahkan guard `zip`/manifest di `build-setup`: gagal kalau `dist\EXAMVAN.exe` lebih tua dari sumber TERBARU. | `build-setup.bat:42-51`, `build-setup.ps1:34-38` |

### 9.2 Minggu ini (butuh keputusan desain kecil)

| # | Aksi | Catatan |
|---|---|---|
| 7 | Perbaiki siklus hidup keyboard hook (Bagian 5). | Opsi paling murah: jadikan `get_backend()` shared instance sesuai janji docstring-nya, supaya state global benar-benar punya satu owner. |
| 8 | Tambahkan job verifikasi konten Windows seperti padanan Linux di `ci.yml:129-152`: inventaris modul di dalam exe + kesamaan versi terhadap `APP_VERSION`. | Menutup kelas bug yang sudah pernah menggigit `.deb`. |
| 9 | Ganti semua `pip install PyQt5 PyMuPDF` hardcoded di skrip Windows dengan `-r requirements.txt`. | Menghapus M4 dan membuat adding dependency jadi effective di Windows. |
| 10 | Stamp `FixedFileInfo` juga, dan pakai `build_info.py` yang sama untuk build lokal (bukan hanya CI). | Menghapus M3 sekaligus membuat klaim `windows/README.md:482-483` jadi benar. |
| 11 | Perkuat smoke test: assert isi `%USERPROFILE%\.config\examvan\app.log` menunjukkan app selesai inisialisasi, bukan sekadar "proses hidup". | Menangkap kegagalan lazy-import yang tidak terlihat dari exit code. |

### 9.3 Rutinitas (low risk, kapan pun)

- Hapus `AppMutex` atau implementasikan `CreateMutex` di app; koreksi komentarnya soal `SetupMutex` (L1).
- Tambahkan `vcruntime140_1.dll` ke `VCRedistPresent`, atau bundle VCRedist (L2).
- Cek exit code `pip` di `build-exe.ps1` + perbaiki typo `EXAVAN.exe` (L4), dan di `run.bat`/`run.ps1` (L3).
- Tambahkan `desktop/tests/test_build_info.py` dan perbaiki cabang `numeric_version` yang menyaturasi ke 255.
- Hapus `--add-data "…;examvan"` yang tidak dibaca apa pun.

---

## Catatan verifikasi & keterbatasan

- **Semua temuan Bagian 1–5 sudah saya verifikasi sendiri** terhadap sumber, dengan `file:line` yang diambil ulang setelah perubahan ronde 1 (beberapa nomor baris bergeser karena itu). Salinan sementara tiap kutipan kodenya diperiksa.
- **Bagian 6 berasal dari review rantai build**, yang tidak bisa saya jalankan: tidak ada host Windows, tidak ada ISCC, tidak ada PowerShell di lingkungan ini. Temuan M1/M2/M3/M4/L1 diverifikasi secara **mekanis** (baca sumber, `build_info.py` dijalankan, TOC archive `EXAMVAN.exe` diperiksa, mtime dibandingkan). L2/L3/L4 **didasarkan padapenalIS semantics vendor documentation**, bukan eksekusi.
- Yang **tidak bisa diverifikasi** dan karena itu tidak diklaim sebagai bug: perilaku `--onefile` cold-start (ekstraksi 60 MB ke `%TEMP%` tiap launch — ORIGIN yang masuk akal sebagai masalah latensi di PC lab low-end yang README gambarkan, tapi **tidak saya ukur**); `build-windows.yml:74` menjalankan `python` sebelum `actions/setup-python` (bertahan hari ini di `windows-latest`, rapuh laten kalau image berubah); dan `numeric_version` clamping (terbukti salah untuk sebagian input, tidak terjangkau dengan `APP_VERSION` sekarang).
- **Android tidak terdampak** oleh review ini. `android/.../ExamModePolicy.kt` sudah memakai `securityLevel != LEVEL_LOW`, sehingga tier `high` dari server otomatis tertangani — justru desktop yang menyimpang.
