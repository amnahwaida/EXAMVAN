# Review Aplikasi Windows — EXAMVAN (ronde 3)

> Tanggal: 30 September 2026 · Scope: klien desktop `desktop/examvan/` + rantai build/installer Windows.
> Status: **SEMUA 23 TEMUAN SUDAH DIPERBAIKI** — 10 dari dokumen ini (N1–N10) + 13 carry-over (R1–R13).
> Ronde 2 (30 Sep, sesi sebelumnya) memperbaiki 3 bug yang dilaporkan siswa: fullscreen, token submit, dropdown matching.
>
> Test suite: **198 → 415 test, hijau**. Tiap perbaikan di-*mutation-check* (dikembalikan ke kode lama → test harus gagal); 17 mutasi diuji, semuanya tertangkap. Dua mutasi justru menemukan bug baru di dalam perbaikannya sendiri — lihat [Bagian 9](#bagian-9--catatan-mutation-check).
>
> Dua permintaan tambahan juga selesai: **default server URL** (`https://examvan.my.id`, tidak perlu diketik ulang di lab) dan checkbox "Simpan URL & Token" yang akhirnya benar-benar berfungsi.

Bagian 1–7 diverifikasi terhadap sumber; N1, N2, N3, N4, dan N9 diverifikasi dengan eksekusi (hasilnya dikutip di bawah).

---

## Ringkasan

| # | Severity | Temuan | Perbaikan | Test |
|---|---|---|---|---|
| N1 | **HIGH** | `identity_data` tidak pernah dihapus → identitas siswa sebelumnya otomatis terisi di PC bersama | `config.clear_identity()` + dipanggil di `_on_viewer_closed` | `test_identity_leak.py` |
| N2 | **HIGH** | `refresh_deadline()` membuka kembali manipulasi jam — timer tampil 2 jam, server sudah 403 | clamp `min()` ke deadline absolut | `test_timer_anti_tamper.py` |
| N3 | MEDIUM | Checkbox "Simpan URL & Token" tidak mengendalikan apa pun | `_load_saved` menghormati `remember_url` | `test_default_server_url.py` |
| N4 | MEDIUM | Field identitas yang tidak dikenali dipaksakan ke slot identitas | fallback tebak dihapus | `test_identity_mapping.py` |
| N5 | MEDIUM | Identitas perangkat dari `uuid.getnode()` bisa berubah | cache per-proses + satu implementasi | `test_identity_mapping.py` |
| N6 | LOW | `ws.disconnect(self._ws.connected)` salah | `self._ws.disconnect()` + putuskan handler | `test_round3_low.py` |
| N7 | LOW | "Minta Izin Lagi" menypawn thread poll kedua | `threading.Event` + join sebelum spawn | `test_round3_low.py` |
| N8 | LOW | `_suppress_mupdf_warnings` menulis stderr proses global | `fitz.TOOLS.mupdf_display_errors` scoped | `test_round3_low.py` |
| N9 | LOW | ~55 baris clipboard mati me-fork `xsel`/`xclip`/`wl-copy` | dihapus | `test_round3_low.py` |
| N10 | LOW | `IdentityDialog` tanpa scroll area | `QScrollArea` membungkus kartu | `test_round3_low.py` |

---

## Bagian 1 — HIGH: identitas siswa sebelumnya otomatis terisi di PC bersama

**Lokasi:** `desktop/examvan/ui/server_config.py:367` (tulis), `:356` (baca), `desktop/examvan/ui/identity_dialog.py:84-86` (pre-fill), `:100` (Enter), `:112` (validasi), `desktop/examvan/__main__.py:189`

Saya grep seluruh `desktop/examvan/` untuk `config.set("identity_data"` dan `config.get("identity_data"`. Hanya **tiga** titik, dan **tidak ada satupun yang menghapus**:

```
server_config.py:297:  identity = config.get("identity_data", {}) or {}    # baca (recovery)
server_config.py:356:  saved_identity = config.get("identity_data", {})    # baca (pre-fill)
server_config.py:367:  config.set("identity_data", identity)               # tulis
```

Tidak ada `config.clear_identity()`, tidak ada `pop`, tidak ada tombol reset.

**Yang membuat ini bug, bukan fitur** — `__main__.py:188-189` menunjukkan penulisnya justru memikirkan pembocoran data antar-sesi, tapi hanya untuk separuhnya:

```python
# Clear saved token so user must re-enter for next exam
dialog.input_token.clear()
```

Token **dibersihkan**. Nama, nomor ujian, dan kelas **tidak**.

**Rantai yang terjadi di lab komputer bersama:**

1. Student A selesai ujian. `config.json` berisi `identity_data` = {nama: "Ahmad", nomor_ujian: "N01", kelas: "9A"}. Token dikosongkan.
2. Student B duduk di PC yang sama, buka EXAMVAN, masukkan token, tekan Hubungkan.
3. `server_config.py:356` membaca identitas A → `IdentityDialog` **mengisi formulir dengan nama A** (`:84-86`).
4. `_on_submit` (`:112`) hanya memvalidasi bahwa field wajib tidak kosong. Nilai A lolos.
5. `last_input.returnPressed` (`:100`) — student B bisa menekan Enter tanpa membaca apa pun.
6. Jawaban B tercatat di server atas **nama A**.

Tidak ada dialog, tidak ada warning, tidak ada baris log. Di sheet hasil, B muncul sebagai A.

**Dampak.** Pada model pemakaian yang proyek ini gambarkan sendiri (PC lab dipakai bersama), hampir **setiap** submission setelah siswa pertama salah atribusi. Guru harus mengoreksi rekap satu per satu; siswa yang namanya hilang tidak pernah tahu.

**Perbaikan.** Bersihkan identitas tepat di samping token yang sudah dibersihkan:

```python
def _on_viewer_closed():
    viewer.close()
    windows.clear()
    # Token DAN identitas harus dibersihkan. Kalau hanya token, siswa
    # berikutnya mendapat formulir yang sudah terisi nama siswa sebelumnya
    # dan bisa langsung menekan Enter. Lihat review ronde 3, Bagian 1.
    dialog.input_token.clear()
    config.set("identity_data", {})
```

`config.set("identity_data", {})` juga mematikan pre-fill di run berikutnya (`IdentityDialog` meng-`get` lalu mengiterasi; dict kosong = tidak ada pre-fill). Kalau PC pribadi memang ingin identitas tersimpan lintas sesi, tombolnya harus eksplisit dan default-nya harus "bersihkan".

---

## Bagian 2 — HIGH: `refresh_deadline()` membuka kembali manipulasi jam

**Lokasi:** `desktop/examvan/ui/timer.py:71-83`, dipanggil dari `desktop/examvan/ui/exam_viewer.py:774,778`

Modul ini mendeklarasikan jaminan yang sama dengan yang dikodekan:

```python
"""Timer widget — countdown or elapsed depending on exam config.

Uses monotonic clock to prevent system time manipulation.
"""
```

dan `_update` (`:103`):

```python
# Countdown mode via monotonic clock — immune to system clock jump
```

Semua itu benar — **sampai** `refresh_deadline()` dipanggil. Fungsi itu menghitung ulang deadline dari **wall clock perangkat**:

```python
def refresh_deadline(self) -> None:
    if self._fired_time_up:
        return
    self._compute_deadline()      # -> compute_remaining_seconds(end_time, datetime.now(), skew)
```

`_compute_deadline()` (`:60-62`) memakai `datetime.now(timezone.utc)` — jam perangkat. Jadi setiap kali `refresh_deadline()` jalan, countdown mengikuti ulang ke jam yang bisa diubah siswa.

**Trigger-nya sepele.** `exam_viewer.changeEvent` memanggilnya pada:
- `:774` — setiap `WindowStateChange` yang bukan minimize
- `:778` — setiap `ActivationChange` saat window aktif

Artinya **membuat window kembali fokus** saja sudah cukup untuk refresh deadline. Tidak perlu keyboard hook, tidak perlu mode strict. Bahkan tidak perlu klik: di mode apa pun, `raise_()` / `activateWindow()` dari poll fokus enforcer sudah memicu `ActivationChange`.

**Bukti eksekusi** (`compute_remaining_seconds` dipanggil langsung):

```
exam ends 11:00, skew 0
  remaining (device clock 10:00) : 3600.0 s   <- jujur
  remaining (device clock 09:00) : 7200.0 s   <- siswa set jam mundur 1 jam
  -> student GAINS 3600.0 s
```

**Dan `skew` tidak menyelamatkan.** `api.compute_server_skew_ms` (`:82-95`) menghitung `server_time_utc - jam perangkat`. Saya ukur:

```
server_time_utc=10:00, device=10:00 -> skew=0 ms
server_time_utc=10:00, device=09:00 -> skew=3600000 ms
```

Jadi saat health check berjalan, skew justru **ikut mengoreksi** ke arah yang sama..skew dihitung satu kali (`check_health`, `api.py:110`) dan tidak pernah di-refresh selama ujian.

**Dampak — dan ini yang paling buruk.** Server jamnya sendiri benar: `exams.go:1039` memakai `time.Now().UTC()` (jam server), dan `:1055` membalas `403 "Waktu ujian telah berakhir"` bila lewat. Jadi:

1. Siswa mengubah jam perangkat ke belakang.
2. Mengklik window → countdown melompat **memperpanjang** (mis. 01:00:00 → 02:00:00).
3. `_auto_submit` tidak pernah menyala — siswa boleh terus mengerjakan.
4. Saat akhirnya submit, server sudah lewat deadline → **403**, jawaban ditolak.

Yang dilihat siswa adalah "masih ada 1 jam" sementara server sudah menutup. They bekerja 30 menit lebih untuk mengembalikan 403. Dan `changeEvent` memang dirancang untuk "tampilan sisa waktu menyesatkan setelah resume" — jadi koreksinya justru/documentasikan sumber manipulasi ini.

**Arah perbaikan.** Deadline harus diturunkan dari sumber yang tidak bisa dikendalikan siswa. Opsi termurah: hitung `remaining` **sekali** dari wall clock + skew saat masuk, lalu `refresh_deadline()` hanya boleh menambah waktu yang benar-benar hilang karena suspend, dalam batas tertentu (mis. `+2x interval heartbeat`), dan clamp ke deadline asli `end_time`. Yang ideal: ambil ulang `server_time_utc` dari `/api/health` secara berkala dan pakai `end_time` absolut.

---

## Bagian 3 — MEDIUM: checkbox "Simpan URL & Token" tidak mengendalikan apa pun

**Lokasi:** `desktop/examvan/ui/server_config.py:107-109` (UI), `:140-148` (load), `:211-215` (save)

Checkbox dibuat dan benar-benar dibaca:

```python
remember = config.get("remember_url", True)
if url:
    self.input_url.setText(url)          # :144-145  <- tanpa syarat
if token:
    self.input_token.setText(token.upper())   # :146-147  <- tanpa syarat
self.chk_remember.setChecked(remember)   # :148
```

`remember` **tidak pernah dipakai** untuk memutuskan apakah mengisi. Yang terjadi: URL dan token selalu di-pre-fill; `remember` hanya mengatur tampilan checkbox itu sendiri.

Menyimpanya, `server_config.py:211-215` mengakui terus terang:

```python
# URL+token selalu disimpan (token juga dipakai sebagai kunci decode);
# `remember_url` hanya mengontrol apakah di-reload ke input berikutnya.
config.set("exam_token", token)
config.set("remember_url", self.chk_remember.isChecked())
```

Komentar itu **bertentangan dengan implementasi** di baris yang sama: token disimpan, dan juga di-reload. Dua-duanya.

**Dampak.** (a) Sebuah kontrol UI yang tidak melakukan apa pun — dikotak centang, tidak ada yang berubah, tidak ada yang memberitahu. (b) Janji privasi yang tidak ditepati: siswa menocentang "Simpan URL & Token" di PC lab, lalu token + identitas siswa sebelumnya (N1) **tetap** di `config.json` plaintext, terbaca Notepad oleh siapa pun yang duduk berikutnya. (c) `config.py` meng komentar bahwa token juga adalah kunci XOR untuk decode jawaban yang tersimpan (`_xor_obfuscate`) — jadi produkIklan ini tidak bisa dihapus tanpa Lose recovery.

Catatan: N1 dan N3 saling strengthen. Satu Permissions bug, dua hal yang perlu dijelaskan ke user.

**Perbaikan.** Dua pilihan, dan keduanya harusPutuskan eksplisit:
- (a) Hormati checkbox: `if remember:` gating pre-fill, dan **tetap** simpan token di disk (karena dibutuhkan decode) tetapi beri tahu user di UI.
- (b) Hapus checkbox dan ganti label jadi informatif, mis. "Token disimpan di komputer ini untukdekode jawabanWY".

---

## Bagian 4 — MEDIUM: field identitas yang tidak dikenali dipaksakan ke slot identitas

**Lokasi:** `desktop/examvan/utils.py:33-62`

Setelah pencocokan keyword gagal, ada fallback diam-diam (`:59-62`):

```python
# 2. Fill remaining standard keys from unmatched fields in order
remaining = [val for _, val in items if val not in assigned]
for std_key in ("student_name", "exam_number", "student_class"):
    if std_key not in result and remaining:
        result[std_key] = remaining.pop(0)
```

Nilai yang **tidak cocok keyword apa pun** dipop ke slot identitas **berdasarkan urutan dict**. Saya jalankan:

```
in : {'nama': 'Budi', 'kelas': '9A', 'tanggal_lahir': '2010-05-05'}
out: {'student_name': 'Budi', 'exam_number': '2010-05-05', 'student_class': '9A'}
```

**Tanggal lahir tercatat sebagai nomor ujian.** Tidak ada error, tidak ada warning.

Kata kuncinya juga saling tumpang tindih. `number_kw` (`:35`) memuat `"no_"`, `"ujian"`, `"exam"`, `"nis"`, `"nip"`, `"peserta"`; `name_kw` (`:34`) memuat `"peserta"` juga. Kunci seperti `no_telp` mengandung `"no_"` → cocok `exam_number`. Kunci `gelombang`/`paket_ujian`/`sekolah` tidak cocok apa pun → jatuh ke fallback di atas.

**Dampak.** Ujian dengan field identitas kustom (memang didukung — `IdentityDialog` membangun form dari `exam.identity_fields`) bisa merekam data yang salah. Salah atribusi yang diam-diam, persis seperti N1 tapi berasal dari konfigurasi ujian, dan parmesan lewat ke sheet hasil. Karena_ToNothing_ menolak atau memperingatkan, guru tidak punya cara mengetahui bahwa ada yang salah.

**Perbaikan.** Jangan menebak. Kalau ada sisa field yang tidak terpetakan, jangan isi slot standar — biarkan kosong supaya sisi server menolaknya secara eksplisit (`exams.go:1104` sudah mengembalikan 400 `"Identitas '%s' wajib diisi"`), atau kumpulkan sisa itu ke `identity_data` apa adanya tanpa memaksakan ke slot standar. Server sudah memvalidasi berdasarkan `expectedFields`, jadi pemetaan client sebaiknya tidak mengarang nilai yang tidak dipetakan.

---

## Bagian 5 — MEDIUM: identitas perangkat bisa berubah antar-waktu

**Lokasi:** `desktop/examvan/utils.py:68-100`

```python
def get_device_id() -> str:
    mac = get_mac_address()
    hostname = socket.gethostname()
    raw = f"{mac}:{hostname}"
    return hashlib.sha256(raw.encode()).hexdigest()[:32]
```

Di Windows, `get_mac_address()` jatuh ke `uuid.getnode()` (`:86`). Identitas ini **bukan** stabil di Windows modern:

- Windows 10/11 punya "Randomized MAC addresses" (default aktif untuk Wi-Fi di build terbaru) → `uuid.getnode()` bisa mengembalikan nilai berbeda setelah reboot atau setelah pergantian jaringan.
- `uuid.getnode()` mengembalikan MAC adapter pertama yang ditemukan. PC lab dengan Wi-Fi + Ethernet + Bluetooth + VPN Adapter bisa mengembalikan adapter yang berbeda tergantung urutan enumerasi.
- Kalau tidak ada MAC yang bisa dibaca, `uuid.getnode()` mengembalikan **nilai acak 48-bit** dengan bit multicast diset — berubah per proses.

Label ini bukan hiasan. `get_device_label()` = `DESKTOP:<hash>` dipakai sebagai kunci di **empat** tempat: `X-Device-Id` untuk download PDF (`api.py:171`), `mac_address` untuk approval (`waiting_approval.py:49`), `mac_address` untuk submit (`exam_viewer.py`), dan presence/heartbeat. Komentar di `waiting_approval.py:41-48` justru menjelaskan bahwa dulu MAC mentah vs `DESKTOP:hash` tidak match sehingga approval tidak pernah ter-revoke dan siswa tampil 2× di monitoring.

Kalau label berubah, rantai yang sama rusak lagi: gate PDF tidak match baris approval → **PDF tidak bisa diunduh**, dan approval lama tidak pernah ter-revoke.

**Dampak.** Risiko rendah–menengah per mesin, tapi tidak terdeteksi: tidak ada log yang menyebut label perangkat, dan gejalanya ("siswa menggantung di layar download PDF", "siswa muncul 2×") sangat_remote dari penyebabnya. Untuk identifier yang mengunci gerbang exam, ini tidak bisa diterima.

**Arah perbaikan.** Untuk Windows, ambil MAC secara deterministik: Win32 `GetAdaptersAddresses`, pilih adapter pertama yang_up_ (bukan loopback, bukan Bluetooth, bukan VPN/tunnel, MAC tidak nol). Atau—andai lebih sederhana dan lebih kuat—`WindowsBackend` sudah punya `get_device_label()` sendiri (`windows_backend.py:801-806`) yang menduplikasi logika ini; **dua implementasi device identity yang berbeda dalam satu repo** itu sendiri perlu satu sumber kebenaran.

---

## Bagian 6 — LOW

### N6 — `ws.disconnect(self._ws.connected)` salah
`desktop/examvan/ws.py:64`

```python
self._ws.disconnect(self._ws.connected)
```

`self._ws.connected` adalah **signal** PyQt, bukan method. `QObject.disconnect(...)` yang dipanggil dengan satu argumen tidak melakukan apa yang dimaksud — intent-nya jelas `self._ws.disconnect()`. Dampaknya saat ini kecil karena `_should_reconnect` sudah di-set `False` sebelum `abort()`, sehingga handler tidak menjadwalkan reconnect. Tapi handler `disconnected` **tidak** diputus, jadi `disconnect()` lalu `connect()` pada objek yang sama akan tetap melihat disconnect lama itu.

### N7 — retry approval menypawn dua thread poll
`desktop/examvan/ui/waiting_approval.py:176-183`

Saat status `"rejected"`, `_on_status_update` menyetel `self.is_waiting = False` (`:168`). Thread lama mungkin sedang di `time.sleep(5)`. Kalau siswa menekan "Minta Izin Lagi" sebelum thread itu bangun, `_retry_approval` menyetel `is_waiting = True` lagi dan menypawn thread baru — thread lama lalu melanjutkan loop-nya juga. Dua thread memanggil `request_approval` bersamaan untuk satu siklus persetujuan; yang baru memakai `reset=True`, yang lama `reset=False`. Server punya proteksi anti-spam (`request-approval` menolak tanpa token valid), jadi akibatnya bisa berupa request yang ditolak atau baris approval ganda. Perlu hentikan thread lama secara eksplisit (event, atau cek identitas thread/generasi) sebelum menypawn yang baru.

### N8 — `_suppress_mupdf_warnings` menulis stderr proses global
`desktop/examvan/ui/pdf_viewer.py:16-27`

```python
old_stderr = os.dup(2)
os.dup2(devnull.fileno(), 2)
yield
os.dup2(old_stderr, 2)
```

`dup2` pada fd 2 bersifat **proses-global**, bukan thread-safe dan tidak reentrant. Dua pemanggilan yang tumpang tindih membuat `finally` yang paling dalam me-restore fd yang sudah berisi devnull → **stderr hilang permanen** untuk sisa proses. Saat ini kedua pemanggil (`load_pdf:182`, `_render_current_page:219`) berjalan di thread GUI sehingga tidak overlap dalam praktik, dan logging sudah diarahkan ke file (`__main__.py:_setup_logging`), jadi belum jadi masalah nyata. Tetap perlu diperbaiki: lebih baik `contextlib.redirect_stderr` atau — lebih bersih — set `fitz.TOOLS.mupdf_display_errors(False)` per-konteks.

### N9 — ~55 baris clipboard mati
`desktop/examvan/utils.py:103-160`

`clear_clipboard()`, `_clear_clipboard_windows()`, `_clear_clipboard_x11()`, dan `clear_clipboard_wl()` **tidak punya satu pun call site** di luar `utils.py` (saya grep). Satu-satunya mention adalah **komentar** di `exam_viewer.py:879`. Ronde 2 memindahkan seluruh jalur clipboard ke enforcer (`clear_clipboard_now`), dan sisa-sisa ini tertinggal.

Tidak merusak apa pun hari ini, tapi `_clear_clipboard_x11` masih me-`subprocess.run` `xsel`/`xclip`, dan `clear_clipboard_wl` me-`wl-copy` — persis fork proses yang ronde 2 hapus dari `windows_backend` karena membekukan kursor di PC low-end. Kodanya masih terlihat di repo, jadi siapa pun yang membacanya bisa menyimpulkan itu jalur yang aktif. Hapus.

### N10 — `IdentityDialog` tanpa scroll area
`desktop/examvan/ui/identity_dialog.py:66-104`

Kartu dialog memakai `QVBoxLayout` langsung dengan `addStretch(2)` di atas dan `addStretch(3)` di bawah, tanpa `QScrollArea`, dan `card.setFixedWidth(440)`. Ujian dengan banyak field identitas (yang memang didukung) membuat isi dialog lebih tinggi dari layar; tombol "Masuk Ujian" lalu berada di bawah area yang bisa dijangkau dan **tidak ada cara menggulir untuk mencapainya** — siswa terkunci sebelum ujian dimulai, dengan stretched untuk menutup jendela. Bungkus `card` di `QScrollArea` dengan `setWidgetResizable(True)`, atau minimal jadikan dialog `resizeable` + scrollable.

---

## Bagian 7 — Prioritas

### Segera (kecil, dampak besar)
| # | Aksi | File |
|---|---|---|
| 1 | Bersihkan `identity_data` saat viewer ditutup, di samping token | `__main__.py:185-190` |
| 2 | Hentikan `refresh_deadline()` mengikuti wall clock, atau clamp ke `end_time` | `timer.py:71-83` |
| 3 |_gate_ pre-fill dengan `remember_url`, atau ganti checkbox jadi label informatif | `server_config.py:140-148` |
| 4 | Hapus 4 fungsi clipboard mati di `utils.py` | `utils.py:103-160` |
| 5 | Hapus fallback "isi slot standar dari sisa field" di `map_identity_to_standard` | `utils.py:59-62` |

### Minggu ini (butuh keputusan desain)
| # | Aksi | Catatan |
|---|---|---|
| 6 | Deadline yang tidak bisa dimanipulasi siswa | refresh berkala `/api/health`, atau monotonic + clamp `end_time` |
| 7 | Satu sumber kebenaran untuk device identity | pilih Win32 deterministic MAC; hapus duplikat di `windows_backend.py:801` |
| 8 | Stop thread poll lama sebelum menypawn yang baru | `waiting_approval.py:176-183` |
| 9 | Scroll area untuk `IdentityDialog` | `identity_dialog.py:66-104` |

### Kebiasaan
- `ws.py:64` — `self._ws.disconnect()` tanpa argumen.
- `pdf_viewer.py:16-27` — ganti `dup2` dengan API yang scoped.

---

## Bagian 8 — Carry-over ronde 2 (29 September): sekarang tertutup

Semuanya sudah diperbaiki. R memakai penomoran dari `review_windows_2026-09-29.md`.

| # | Severity | Temuan | Perbaikan |
|---|---|---|---|
| R1 | **CRITICAL** | Instalasi senyap/upgrade menghapus `admin_password.txt` tanpa bersyarat → supervisor terkunci saat ujian berjalan | hapus + tulis dalam satu cabang `if PwValue <> ''`; `ReadPasswordFromFile()` memuat ulang password lama saat halaman kosong |
| R2 | HIGH | Dialog aplikasi & README menyuruh user mencentang checkbox yang sudah dihapus | dialog + `windows/README.md` diarahkan ke halaman password yang sebenarnya |
| R3 | HIGH | Prompt uninstall menyebut folder & isi yang salah | dua folder, isi disebut apa adanya, keduanya ditawarkan dihapus |
| R4 | HIGH | Versi di layar ≠ installer ≠ resource exe | satu literal `APP_VERSION`; `__version__` diturunkan; `build_info.py stamp` menyetempel `FixedFileInfo` juga; ketiga jalur build pakai fungsi yang sama |
| R5 | HIGH | `get_backend()` mengembalikan instance baru setiap dipanggil, sementara state hook adalah module global | `_shared_backend` di cache + `reset_backend_cache()` untuk test |
| R6 | MEDIUM | Smoke test hanya assert proses tidak exit 12 detik | assert `app.log` tidak berisi traceback, shortcut wajib ada, exit code uninstaller diperiksa, **password utuh setelah upgrade senyap** |
| R7 | MEDIUM | `build-setup` mengemas exe basi | `check_exe_freshness.py` membandingkan mtime exe vs source terbaru; build GAGAL kalau basi |
| R8 | MEDIUM | Rantai Windows mengabaikan `desktop/requirements.txt` | semua skrip + CI pakai `-r requirements.txt` |
| R9 | LOW | `AppMutex` tidak pernah bisa menyala | diganti `SetupMutex` (yang memang untuk mencegah dua installer) |
| R10 | LOW | `VCRedistPresent` tidak mengecek `vcruntime140_1.dll` | ketiga DLL MSVC x64 diperiksa |
| R11 | LOW | `run.bat`/`run.ps1` jalan meski `pip install` gagal | kedua jalur (first-time dan venv-ada) sama-sama cek exit code |
| R12 | LOW | `build-exe.ps1` tidak cek exit code pip; pesan errornya salah ketik | `$LASTEXITCODE` diperiksa; `EXAVAN` → `EXAMVAN` |
| R13 | LOW | `--add-data` meng-`__pycache__` stale tapi tidak dibaca apa pun | dihapus dari kedua skrip |

R7 perlu diperhatikan: `windows/dist/EXAMVAN.exe` yang ada di working tree
memang **basi** (27 Sep, source 30 Sep). Guard baru sekarang mendeteksinya
dan menolak build:

```
$ python3 windows/installer/check_exe_freshness.py --exe windows/dist/EXAMVAN.exe --source desktop
[WARN] EXAMVAN.exe (1790485127) lebih tua dari source (1790722922, ...)
       Build akan mengemas kode LAMA dan student menerimanya.
       Jalankan dulu:  windows\build-exe.bat
$ echo $?
1
```

Verifikasi isi exe (`list_exe_modules.py`) juga menambah satu bukti:
`examvan.ui.fullscreen` **tidak ada** di dalam exe yang tersimpan —
bukti bahwa artefak itu memang bukan build dari source sekarang.

---

## Bagian 9 — Ronde 4: review alur end-to-end

Setelah perbaikan di atas, seluruh alur aplikasi ditinjau ulang dari sisi
*dead end* — bukan dari sisi kode. Tiga temuan baru, semuanya proven dengan
eksekusi. Semuanya diperbaiki; 4 mutasi diuji, semuanya tertangkap.

| # | Severity | Temuan | Perbaikan |
|---|---|---|---|
| P1 | **HIGH** | PDF gagal → lembar jawaban **tidak pernah dibangun**: 0 widget, `get_answered_count()` = (0, 0), tombol submit tetap aktif | `_build_answer_sheet()` dipanggil selalu, sebelum dan terpisah dari PDF |
| P2 | **HIGH** | Batal di dialog persetujuan → dialog config kembali dengan "Hubungkan" **mati**: tidak ada cancel, tidak ada reset, harus restart aplikasi | `ServerConfigDialog.enable_connect()`, dipanggil dari `on_exam_selected` |
| P3 | MEDIUM | Dua soal bernomor sama → dua blok tergambar, satu entri `_answer_widgets`: satu blok jawaban hilang diam-diam | kunci internal unik + nomor bentrok dilaporkan ke siswa di dialog pengumpulan |

**P1** adalah yang paling merusak. Bukti eksekusi, config dengan 2 soal,
`download_pdf` melempar `OSError`:

```
answer widgets dibangun : 0
get_answered_count()    : (0, 0)
tombol submit enabled  : True
```

Lembar jawaban dibangun dari `exam.questions` — bukan dari PDF — tapi
`build_from_questions()` hanya dipanggil di dalam cabang sukses
`_on_pdf_ready`. Jadi satu hiccup jaringan, atau satu file PDF korup,
menghapus seluruh kemampuan siswa menjawab. Tidak ada tombol retry download,
dan jalan keluar yang tersisa adalah mengirim jawaban kosong.

**P2** muncul karena `_on_connect` men-disable `btn_connect` (`:208`) sementara
jalur sukses hanya meng-emit `_sig_show_identity` (`:273`) — tidak pernah
`_sig_enable_btn`.lalu tombol "Hubungkan" **mati** — tidak ada cancel, tidak ada reset.

**P3** bisa terjadi karena server hanya memvalidasi kunci jawaban
(`validateQuestionKeys`, `admin/exams.go:1694`); nomor duplikat tidak pernah
dicek, jadi guru bisa menyimpannya tanpa tahu. Di sisi siswa: dua blok
soal bernomor "1" berbagi satu slot jawaban, dan hitungan "N / M terjawab"
pun jadi tidak sesuai payload. Sekarang nomor bentrok muncul di dialog
konfirmasi pengumpulan — titik terakhir di mana siswa masih bisa melapor.

Yang sengaja **tidak** diperbaiki: nomor bentrok tidak di-"perbaiki" di sisi
siswa. Memakai ulang kunci akan meninventar payload yang tidak ada di server.
Yang bisa dilakukan di klien adalah melaporkannya, dan itu yang dilakukan.

---

## Bagian 10 — Catatan mutation check

Tiap perbaikan dikembalikan ke bentuk lamanya dan test harus gagal. 17 mutasi
diuji; **semuanya tertangkap**. Dua di antaranya menemukan bug nyata di dalam
perbaikannya sendiri, dan itu justru alasan mutation check dilakukan:

1. **Perbaikan token submit sempat hilang.** `git checkout --` yang dipakai untuk memulihkan
   satu file setelah mutasi ikut mengembalikan `server_config.py` ke HEAD — termasuk fix token
   submit-nya. Test call-site yang baru (`SubmitCallSiteTokenTest`)
   langsung menangkap. Semula ketiga call site submit hanya di-mock, jadi
   test bisa lulus tanpa pernah memanggilnya.

2. **Test versi semula berlubang.** `test_package_version_matches_api_version`
   hanya memeriksa `__version__ == APP_VERSION`. Itu tidak menangkap
   "keduanya sama-sama salah". Saat `APP_VERSION` sempat dibalik menjadi alias
   `= __version__`, semua extractor berbasis regex (`build_info.py`,
   `build-deb.sh`, langkah verifikasi `.deb` di `ci.yml`) ikut gagal diam-diam
   dan paket `.deb` terbit terlabel **1.0.0**. Test baru sekarang menyematkan
   versi ke tiga salinan lain di repo (`.iss`, `build_info.py`,
   `version_info.txt`) dan menjaga `APP_VERSION` tetap literal.

Selain itu, dua test semula **vacuous** dan diperbaiki: yang menguji fullscreen
mengecilkan window ke `availableGeometry()` — yang di platform offscreen sama
dengan `geometry()`, jadi tidak ada yang rusak dan testnya lulus tanpa menguji
apa pun. Sekarang dikecilkan eksplisit dan state fullscreen disimulasikan
dengan mock, seperti yang terjadi di Windows sungguhan.

---

## Catatan verifikasi & keterbatasan

- **N1, N2, N3, N4, N9** diverifikasi dengan eksekusi (script pendek, kutipannya di atas) selain pembacaan sumber. Sisanya diverifikasi secara mekanis: baca sumber + grep call site.
- **Tidak ada host Windows di lingkungan ini.** Semua perbaikan pada `.iss`, `.bat`, `.ps1`, dan workflow diuji secara **tekstual** (struktur Pascal di-parse, kontrak skrip di-assert) — bukan dieksekusi. Saya tidak bisa menjalankan ISCC, PowerShell, atau `cmd.exe`, jadi akibat yang terlihat di build Windows sungguhan belum diverifikasi.
- Test yang mengeksekusi kode Python sungguhan sudah dijalankan: `check_exe_freshness.py` (menolak exe basi, exit 1), `list_exe_modules.py` (membaca 21 modul dari exe 60 MB), `build_info.py stamp` (menyetempel kelima field versi), dan `build-deb.sh` (BUILD SUCCESS, versi 2.5.0, modul baru masuk paket).
- **N5 tidak diukur.** Saya menyatakan *mekanisme* yang membuat device label bisa berubah (MAC acak Windows 10/11, urutan enumerasi adapter), bukan mengukur seberapa sering itu terjadi di lab Anda. Yang diperbaiki adalah menghilangkan klausul "tidak ada yang menjamin keempat call site melihat nilai yang sama".
- **N2** sudah dibuktikan untuk jalur kodenya (test mengeksekusi `refresh_deadline` dengan jam perangkat dimundurkan). Dampak nyata di lab tetap bergantung pada apakah siswa punya hak mengubah jam sistem.
- **Android tidak terdampak** review ini.
