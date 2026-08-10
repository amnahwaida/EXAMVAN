# EXAMVAN — Sistem Ujian Digital Berbasis PDF (Secure — Cloud)

> Platform distribusi & pelaksanaan ujian digital aman berbasis cloud (HTTPS) untuk sekolah dan kampus dengan perlindungan anti-cheat berlapis di sisi Android.
>
> 🏷️ **Rilis terbaru:** tag **`v2.5.0`** (10 Agustus 2026) — changelog lengkap di [GitHub Releases](https://github.com/amnahwaida/EXAMVAN/releases).

---

## Tujuan & Manfaat Project

EXAMVAN diciptakan khusus untuk memenuhi kebutuhan instansi pendidikan dengan arsitektur *hybrid* yang sangat efisien untuk perangkat server minim resource (seperti STB).

- **Penyimpanan PDF Terpusat (Cloudflare R2 wajib):** File soal (PDF) dan APK di-upload ke Cloudflare R2 (Edge CDN) dan di-serve via signed URL, sehingga server lokal/STB tidak menyimpan file berat dan bandwidth egress terselamatkan — ideal untuk skala ribuan siswa secara simultan. Kredensial R2 (`R2_ACCESS_KEY_ID`, `R2_SECRET_ACCESS_KEY`, `R2_BUCKET`, `R2_ENDPOINT`) **wajib** diisi — server tidak akan berjalan tanpanya. Tidak ada fallback penyimpanan lokal. Mode R2 memerlukan koneksi internet.
- **Keamanan Tingkat Tinggi (Anti-Cheat):** Mengamankan berkas soal PDF dari penyebaran liar dan membatasi gerak-gerik siswa agar tidak dapat mencari jawaban di aplikasi lain.
- **Kemudahan Pengelolaan:** Memungkinkan guru untuk mengelola soal mereka sendiri secara terpisah, sementara Administrator memegang kontrol pengawasan penuh.

---

## Arsitektur Sistem

EXAMVAN terdiri dari dua komponen utama:

| Komponen | Stack | Lokasi |
|----------|-------|--------|
| **WebUI (Backend + Admin Panel)** | Go (Gin), PostgreSQL, Redis, Nginx | `webui/` |
| **Android (Aplikasi Siswa)** | Kotlin, Material Components | `android/` |

### Stack Backend (WebUI)

- **Bahasa:** Go 1.25 dengan framework Gin
- **Database:** PostgreSQL 16
- **Cache & Queue:** Redis 7 (submission queue, caching, rate limiting, heartbeat)
- **Reverse Proxy:** Nginx 1.27
- **Template:** Server-side HTML (Jinja-style) dengan Tailwind CSS
- **WebSocket:** Gorilla WebSocket untuk komunikasi real-time
- **Session:** Gin session + Gorilla securecookie
- **Deploy:** Docker Compose multi-container (Go server, PostgreSQL, Redis, Nginx, Cloudflare Tunnel)

---

## Fitur Utama

### 1. Panel Admin & Manajemen Guru (Multi-User)
* **Role Management:** Mendukung akun Super Admin, Guru, dan Pengawas.
* **Hak Akses Eksklusif:** Akun guru hanya dapat melihat, membuat, mengubah, dan menghapus ujian yang dibuatnya sendiri. Super Admin memiliki otorisasi penuh untuk mengawasi seluruh ujian dari semua guru.
* **Ubah Password Mandiri:** Setiap pengguna dapat memperbarui kata sandinya kapan saja melalui UI modal yang aman.

### 2. Lembar Jawaban Digital & Koreksi Otomatis
* **Mendukung 5 Tipe Soal:**
  1. *Pilihan Ganda (Single Choice)*
  2. *Pilihan Ganda Kompleks (Multiple Choice)*
  3. *Benar / Salah (True/False)*
  4. *Menjodohkan (Matching)*
  5. *Isian Singkat (Short Answer)*
* **Pengaturan Bobot & Penilaian Parsial:** Bobot nilai per soal dapat disesuaikan. Pilihan ganda kompleks mendukung opsi **Penilaian Parsial (Partial Scoring)** yang dinamis.
* **Rekalkulasi Nilai Otomatis:** Apabila guru mengubah bobot soal atau mengaktifkan/menonaktifkan opsi penilaian parsial *setelah* ujian disubmit oleh siswa, sistem secara otomatis menghitung ulang nilai siswa secara instan tanpa perlu submit ulang.

### 3. Keamanan Klien Seluler (Android)
* **Tiga Tingkat Keamanan Dinamis (Low, Medium, High/Strict):**
  1. **Low Mode:** Proteksi dasar berupa anti-screenshot (`FLAG_SECURE`) dan pembersihan papan klip (clipboard). Siswa bebas keluar masuk aplikasi tanpa konsekuensi.
  2. **Medium Mode:** Jika siswa menekan tombol Home, berpindah aplikasi, membuka laci notifikasi, atau meminimalkan aplikasi, sistem langsung mendeteksi kehilangan fokus dan melakukan **Auto-Submit (Kumpul Jawaban Otomatis)** dalam waktu 3 detik.
  3. **High/Strict Mode:** Aplikasi mengunci perangkat (lock task) sehingga siswa **tidak bisa keluar** dari aplikasi ujian; jawaban hanya terkumpul saat ujian selesai atau waktu habis.
* **Zero-Friction Launch:** Siswa tidak lagi dibebani dengan pengaturan rumit. Ujian langsung dimulai secara instan, menghemat waktu persiapan ujian hingga 100%.
* **Optimasi Layar Anti-Mati (FLAG_KEEP_SCREEN_ON):** Layar perangkat siswa akan tetap menyala terang secara konstan selama aplikasi dibuka.
* **Anti-Screenshot & Recording:** Layar aplikasi otomatis menjadi hitam jika siswa mencoba menangkap layar atau merekam layar.
* **Anti-Copy Text:** PDF dirender sebagai gambar raster dinamis tanpa lapisan teks, sehingga teks soal tidak dapat disalin.
* **Clipboard Cleanser:** Clipboard/papan klip otomatis dikosongkan saat memasuki ruang ujian.

---

## Mode Kiosk Sekolah (Device Owner - Penguncian Mutlak)

Untuk komputer tablet atau handphone inventaris sekolah (bukan HP pribadi siswa), Anda dapat mengaktifkan **Managed Kiosk Mode (Device Owner)**. Dalam mode ini, tombol Home, Recents, tombol Power Menu, panel notifikasi atas, dan gestur usap akan **dimatikan secara absolut di level sistem operasi**. Siswa secara fisik tidak akan bisa keluar dari aplikasi ujian sebelum lembar jawaban dikirimkan.

### Cara Mengaktifkan Mode Kiosk via ADB:
1. Hubungkan tablet/HP sekolah ke komputer menggunakan kabel USB (pastikan USB Debugging aktif).
2. Jalankan perintah berikut di Terminal/CMD komputer Anda:
   ```bash
   adb shell dpm set-device-owner com.examvan.app/com.examvan.app.receiver.MyDeviceAdminReceiver
   ```
3. Begitu sukses dijalankan, aplikasi EXAMVAN akan memegang otoritas admin penuh untuk mengunci perangkat tanpa memerlukan konfirmasi dialog apa pun pada layar siswa saat ujian dimulai.

---

## Download Aplikasi Siswa

### Perangkat Android
* **Rilis resmi diunduh dari halaman `/download` server** — kartu APK di halaman itu dibaca dari tabel **System Apps** (Cloudflare R2), bukan dari folder `static/` atau root repo.
* **Alternative Build:** Untuk pengembangan lokal, gunakan output Gradle langsung, mis. `android/app/build/outputs/apk/student/debug/app-student-debug.apk` (lihat *Panduan Build* di bawah).
* **Kompatibilitas:** Minimal **Android 7.0 (Nougat - API 24)** hingga versi terbaru. Membutuhkan API 24+ untuk dukungan `network_security_config.xml` dengan tag `<ip-range>`.

---

## Panduan Build Aplikasi Android (.APK)

Kompilasi dapat dilakukan di sistem operasi **Windows, macOS, maupun Linux**.

### Persyaratan Sistem
- **JDK 17 (Java Development Kit):** Pastikan variabel lingkungan `JAVA_HOME` mengarah ke JDK 17.
- **Android SDK:** Terpasang versi SDK 34 (Android 14) untuk target kompilasi.
- **Android Gradle Plugin (AGP):** Versi 8.x ke atas.

### Build dengan Command Line (CLI)
1. Buka terminal di direktori `./android`.
2. Jalankan perintah kompilasi sesuai flavor yang diinginkan:

   **Flavor Student (untuk HP pribadi siswa):**
   ```bash
   ./gradlew assembleStudentDebug
   ```
   APK output: `android/app/build/outputs/apk/student/debug/app-student-debug.apk`

   **Flavor Kiosk (untuk tablet sekolah dengan Device Admin):**
   ```bash
   ./gradlew assembleKioskDebug
   ```
   APK output: `android/app/build/outputs/apk/kiosk/debug/app-kiosk-debug.apk`

3. **Product Flavors:**
   - **`student`** — Aplikasi bersih untuk HP pribadi siswa (tanpa Device Admin)
   - **`kiosk`** — Aplikasi dengan Device Admin + lock-task untuk tablet sekolah (mengunci perangkat saat ujian)

### Merilis Versi APK Baru (Force Update)

Siswa menerima APK lewat halaman unduhan server (`/download`). Setelah versi APK baru dirilis, aplikasi versi lama otomatis **diblokir** (HTTP 426) dan diarahkan ke halaman unduhan. APK resmi disimpan di **Cloudflare R2** dan didaftarkan di tabel **System Apps** — bukan di `static/` server.

Langkah rilis:

1. **Build kedua flavor** (lihat perintah di atas).
2. **Upload APK ke R2 & daftarkan di System Apps** via panel admin → **System Apps → Tambah**:
   - Upload berkas `app-student-debug.apk` (dan `app-kiosk-debug.apk` untuk tablet sekolah) — server meng-upload ke bucket R2 dan mencatat versi + ukuran di database.
   - Versi APK yang dicatat (mis. `2.4.0`) otomatis menjadi **sumber utama** untuk:
     - `/api/health` → `required_app_version` (versi system_app android tertinggi).
     - Halaman `/download` → kartu APK resmi (link ke signed URL R2, bukan file server).
3. **Naikkan versi minimum** di panel admin → **SaaS Settings → Android Version** (mis. dari `2.2.0` ke `2.4.0`). Ini **wajib** agar rute API memblokir APK lama:
   - Rute API yang diproteksi menolak versi lama dengan **HTTP 426**.
   - Aplikasi siswa yang versinya lama menampilkan dialog **"Versi Aplikasi Kedaluwarsa"** dengan tombol *Buka Halaman Download* (membuka `/download`) dan *Keluar* — tidak bisa ikut ujian sampai update.
4. **Verifikasi**:
   - Buka `/download` → kartu APK resmi tampil dengan versi & ukuran dari R2.
   - `curl -I "https://<host>/download/apk?flavor=student"` → respons **302** ke signed URL Cloudflare R2.
   - `curl https://<host>/api/health` → `required_app_version` = versi terbaru.
   - `curl -H "X-App-Version: 0.0.1" https://<host>/api/exams` → harus respons **426**.

### Build dengan Android Studio (GUI)
1. Buka **Android Studio**.
2. Pilih **Open an Existing Project** dan arahkan ke folder `./android`.
3. Tunggu proses sinkronisasi Gradle selesai.
4. Klik menu **Build > Build Bundle(s) / APK(s) > Build APK(s)**.

---

## Panduan Deployment Server (WebUI)

### A. Deployment dengan Docker (Sangat Direkomendasikan)

Metode ini paling mudah dan aman karena semua dependensi sudah terisolasi di dalam container.

#### 1. Prasyarat
- Docker dan Docker Compose terpasang di komputer server.

#### 2. Konfigurasi Lingkungan
Masuk ke folder `webui/` dan salin file environment:
```bash
cd webui
cp .env.example .env
```

Buka file `.env` dan atur variabel berikut:

| Variabel | Keterangan | Wajib |
|----------|-----------|-------|
| `EXAMVAN_SECRET` | Secret key minimal 32 karakter untuk session encryption | Ya |
| `DB_PASSWORD` | Password untuk PostgreSQL | Ya |
| `EXAMVAN_ADMIN_USER` | Username super admin (default: `superadmin`) | Tidak |
| `EXAMVAN_ADMIN_PASS` | Password super admin (kosongkan untuk generate otomatis) | Tidak |
| `APP_ENV` | `production` atau `development` | Tidak |
| `TUNNEL_TOKEN` | Token Cloudflare Tunnel untuk akses internet | Ya |
| `R2_ACCESS_KEY_ID` | Access Key ID Cloudflare R2 | Ya |
| `R2_SECRET_ACCESS_KEY` | Secret Access Key Cloudflare R2 | Ya |
| `R2_BUCKET` | Nama bucket R2 (contoh: examvan-pdfs) | Ya |
| `R2_ENDPOINT` | Endpoint R2 Cloudflare Anda | Ya |
| `EXAMVAN_CORS_ORIGINS` | Origin yang diizinkan (kosongkan untuk allow all) | Tidak |

> **Catatan R2:** Keempat variabel `R2_*` **wajib** diisi — server gagal start (fail-fast) tanpa kredensial R2 lengkap. Semua PDF dan APK disimpan & di-serve via Cloudflare R2; tidak ada penyimpanan lokal.

#### 3. Jalankan Layanan
```bash
cd webui
docker compose up -d --build
```

Layanan yang berjalan:
- **webui-server** — Go backend (port 5000, internal only)
- **db** — PostgreSQL 16
- **redis** — Redis 7
- **nginx-lb** — Reverse proxy (port 80, localhost only)
- **cloudflare-tunnel** — Akses internet opsional

> ⚠️ **Aktifkan HTTPS wajib di dashboard Cloudflare:** Setelah tunnel aktif dan domain publik mengarah ke server, buka **Cloudflare Dashboard → SSL/TLS → Edge Certificates** dan nyalakan **Always Use HTTPS**.
> Ini lapisan kedua setelah redirect HTTP→HTTPS di nginx: semua request cleartext langsung di-redirect ke HTTPS di edge Cloudflare — request HTTP tidak pernah masuk ke tunnel, lebih cepat, dan tetap aman sekalipun konfigurasi nginx berubah. Tanpa setting ini, `http://<domain>` masih dapat diakses (dengan peringatan "Not Secure" di browser) sebelum redirect nginx bekerja.

#### 4. Persistensi Data
- **Database:** Volume Docker `postgres_data` untuk PostgreSQL.
- **File PDF & APK:** Tersimpan di bucket Cloudflare R2 (tidak ada penyimpanan lokal / volume `webui_storage`).

#### 5. Monitoring
```bash
# Log semua layanan
docker compose logs -f

# Log server saja
docker compose logs -f webui-server

# Log Cloudflare Tunnel
docker compose logs -f cloudflare-tunnel
```

#### 6. Hentikan Layanan
```bash
docker compose down
```

---

### B. Deployment Tanpa Docker

#### 1. Prasyarat
- Go 1.25+
- PostgreSQL 16+
- Redis 7+ (opsional, untuk queue dan caching)

#### 2. Setup Database
Buat database PostgreSQL:
```sql
CREATE DATABASE examvan;
CREATE USER examvan WITH PASSWORD 'your_password';
GRANT ALL PRIVILEGES ON DATABASE examvan TO examvan;
```

#### 3. Konfigurasi Environment
```bash
cd webui
cp .env.example .env
```
Atur `DATABASE_URL`, `EXAMVAN_SECRET`, dan variabel lainnya di `.env`.

#### 4. Build & Jalankan
```bash
cd webui
go build -o server ./cmd/server
./server
```

Server berjalan di port 5000 (default).

---

## Konfigurasi Masa Aktif Default (Trial)

Setiap akun **baru** (baik lewat registrasi publik `/register` maupun dibuat oleh admin/operator) mendapat **masa aktif awal 14 hari** — dihitung dari saat akun dibuat. Ini dikontrol oleh pengaturan SaaS **`default_active_days`**.

Setelah masa aktif habis, akun **tetap bisa login**, tetapi seluruh fitur terkunci kecuali halaman **Paket & Voucher** (`/admin/billing`) — tempat pemilik akun dapat mengklaim kode voucher atau menunggu perpanjangan manual oleh admin. SuperAdmin tidak pernah terpengaruh masa aktif.

> ⚠️ **Catatan:** pengaturan ini hanya memengaruhi **akun yang dibuat setelah perubahan** — akun yang sudah ada tidak diperpanjang mundur. Nilai yang tersimpan di database **menimpa** default di kode.

### Nasib Akun Sub saat Operator Habis Masa Aktif

Akun yang dibuat oleh **operator sekolah** (akun sub dalam satu `instansi`) tidak tertaut langsung ke operatornya, tetapi **mewarisi `expires_at` operator saat dibuat** — termasuk status **unlimited**: operator yang tidak punya masa aktif (`expires_at` NULL, mis. sudah di-clear admin) membuat akun sub yang **juga unlimited** (bukan trial 14 hari). Karena itu, ketika masa aktif operator habis, akun sub umumnya ikut habis di waktu yang hampir bersamaan:

- Akun sub **tidak dihapus** dan **tidak di-suspend otomatis** — status tetap `active`;
- Akun sub **tetap bisa login**, tetapi terkunci ke halaman **Paket & Voucher** (`/admin/billing`) — sama seperti operator (feature lock);
- **Ujian aktif yang belum dimulai** di-*tombstone* (jadi `inactive`) oleh job berkala; **ujian yang sedang berjalan** dibiarkan selesai (grace 24 jam).

Saat operator diperpanjang, perilaku akun sub bergantung pada jalur perpanjangannya:

| Jalur perpanjangan operator | Akun sub ikut diperpanjang? |
|---|---|
| Klaim/aktivasi **voucher paket sekolah** | ✅ Ya — sub tanpa paket aktif sendiri mengikuti expiry baru operator (hanya memanjang, tidak pernah memendekkan) |
| **Edit form** admin mengubah expiry operator | ✅ Ya — cascade ke seluruh user di instansi yang sama; bila expiry **dikosongkan** (`expires_at` dihapus → unlimited), sub tanpa paket sendiri ikut **unlimited** |
| Tombol **"Aktifkan"** (toggle-status, `+default_active_days`) | ✅ Ya — sub tanpa paket aktif sendiri ikut diperpanjang mengikuti expiry baru operator |

Sub-account yang **memiliki voucher/paket aktif sendiri** atau **ber-role operator** tidak disentuh oleh perpanjangan/cascade di atas — mereka memakai jam mandirinya masing-masing. Sub yang sudah **unlimited** (`expires_at` NULL) tetap unlimited (cascade tidak pernah menimpa status unlimited yang sudah ada).

### Kebijakan Klaim Voucher Akun Sub (Dibuat Operator)

Sejak kebijakan ini diberlakukan, **akun yang dibuat oleh operator** (akun sub dalam satu `instansi` sekolah) **tidak dapat menukar (klaim) kode voucher apa pun** — baik lewat halaman Paket & Voucher, lewat API, maupun lewat klien Android/desktop (semuanya memanggil API yang sama) — dan **tidak dapat mengaktifkan** paket voucher yang mungkin tertinggal di akunnya (mis. klaim sebelum kebijakan berlaku).

**Klien Android & Desktop — hasil audit (tidak ada penyesuaian UI yang diperlukan):** Kedua klien adalah aplikasi **ujian berbasis token**, bukan aplikasi manajemen akun. Klien Android (`android/app/src/main/java/com/examvan/app/api/ApiClient.kt`) dan klien desktop (`desktop/examvan/api.py`, termasuk salinan paket di `desktop/pkg-build/`) **tidak memiliki layar billing/voucher sama sekali** — di kode sumber klien (Java/Kotlin, layout & string `res/`, Python desktop, salinan `pkg-build/`) pencarian istilah `voucher`/`redeem`/`billing`/`claim`/`klaim` menghasilkan **0 kemunculan** (kata `paket` hanya muncul di skrip packaging `desktop/install.sh` dalam konteks manajer paket OS, bukan paket voucher). Endpoint yang mereka panggil hanyalah rute ujian publik (`/api/health`, `/api/exams`, `/api/exams/request-approval`, `/api/exams/token/{token}`, `/api/exams/{exam_id}/pdf`, `/api/exams/{exam_id}/submit`). Endpoint claim/aktivasi (`/admin/api/vouchers/*`) bersifat **session-based admin** dan tidak punya versi publik — klien token secara teknis pun tidak bisa memanggilnya. Karena itu perlindungan akun sub sepenuhnya di sisi server (403 `rejectOperatorCreatedAccount` **sebelum** pencarian voucher, yang juga menutup skenario andai suatu hari klien mencoba memanggil endpoint itu), dan kontrak routing ini dikunci oleh test `TestNoPublicVoucherRoutes`. Permukaan UI satu-satunya yang berisi form klaim adalah halaman web Paket & Voucher (`/admin/billing`) — yang sudah disesuaikan untuk akun sub (lihat bagian *Cara kerja* di bawah).

**Alasan:** paket, kuota, dan masa aktif akun sub dikelola secara terpusat oleh paket sekolah yang dipegang operator (kuota `max_users`, cascade masa aktif, dsb.). Jika akun sub bisa klaim voucher sendiri, ia dapat melewati kendali tersebut — misalnya mengklaim voucher paket sekolah yang memberinya role **Operator** (self-upgrade), atau memperpanjang/mengubah masa aktifnya sendiri secara independen dari jam sekolah.

**Cara kerja (implementasi):**

- Kolom `admin_users.operator_created` (`BOOLEAN NOT NULL DEFAULT FALSE`) menandai akun yang **dibuat oleh operator**. Di-set oleh handler `CreateUser` (`webui/internal/handlers/admin/users.go`) saat caller adalah operator — **berbasis asal (origin), bukan role saat ini**: flag tidak pernah berubah setelah akun dibuat, sehingga akun sub yang nanti rolenya dinaikkan tetap tidak bisa klaim.
- Kolom `admin_users.created_by` (`INTEGER REFERENCES admin_users(id) ON DELETE SET NULL`) mencatat **operator mana** yang membuat akun — atribusi per-operator yang presisi di balik flag `operator_created`. Kuota bucket `"personal"` (`loadOperatorAccountQuota`, `webui/internal/handlers/admin/users.go`) menghitung hanya `created_by = <id operator>` (bukan seluruh akun operator_created di bucket bersama), dan migrasi saat operator menetapkan instansi sekolah (`UpdateInstansi`) hanya memindahkan **sub-akun miliknya sendiri**. Baris legacy (dibuat sebelum kolom ini ada, `created_by` NULL) memakai fallback konservatif bucket bersama (dihitung terhadap semua operator personal, tidak dipindah migrasi). `ON DELETE SET NULL`: menghapus operator tidak pernah diblokir FK, sub-akun yang sudah dipindah SuperAdmin ke instansi lain jatuh ke fallback legacy. Migrasi idempoten + index pendukung: `ALTER TABLE admin_users ADD COLUMN IF NOT EXISTS created_by ...` dan `CREATE INDEX IF NOT EXISTS idx_admin_users_created_by ON admin_users(created_by)` (`webui/internal/database/schema.sql`) — index ini mengubah pencarian per-operator (kuota di setiap cek `CreateUser`/halaman billing + migrasi klaim sekolah) dari full-table scan menjadi index scan saat data sub-akun membesar.
- `RedeemVoucherHandler` dan `ActivateVoucherHandler` (`webui/internal/handlers/admin/vouchers.go`) menolak akun dengan `operator_created = true` dengan HTTP **403** dan pesan penjelas. Pengecekan dilakukan **sebelum pencarian voucher**, sehingga akun sub tidak dapat membedakan kode valid vs tidak valid dari responsnya — aturan anti-oracle `voucherInvalidMsg` (satu pesan generik untuk semua kegagalan kode) tetap berlaku untuk semua akun lain.
- Halaman **Paket & Voucher** (`/admin/billing`) menyembunyikan seluruh UI voucher untuk akun sub — form klaim maupun daftar "Paket yang Sudah Anda Klaim" — dan menampilkan penjelasan singkat (kuota & masa aktif dikelola Operator/Super Admin). Dengan begitu tidak ada tombol/tindakan voucher yang berujung buntu di UI, termasuk redemption lama (pra-kebijakan) yang tersisa di akun.
- Halaman **Kelola Users** (`/admin/users`) menampilkan badge **"Dibuat oleh Operator"** di samping username setiap akun sub, sehingga Super Admin bisa melihat asal akun sekilas. Implementasi: `ListUsers` (`webui/internal/handlers/admin/users.go`) menyertakan `operator_created` dalam JSON tiap baris user, dan `webui/static/js/admin.js` (`loadUsersList`) merender badge tersebut (tooltip menjelaskan bahwa paket/kuota/masa aktif akun dikelola via paket sekolah Operator).
- **Paket akun sub terkunci** — operator tidak bisa memilih/mengubah paket langganan akun yang dibuatnya: di `CreateUser` label paket dipaksa `"free"` (request tamper sekalipun ditimpa), dan di `EditUser` field `package` diabaikan saat caller operator (`webui/internal/handlers/admin/users.go`). UI-nya menyembunyikan pemilih paket di form Tambah User & modal Atur Limit (`users.html` + `admin.js`), karena kuota/masa aktif akun sub mengikuti paket sekolah operator.
- **Endpoint ubah instansi kini management-level:** `POST /admin/api/instansi/update` (pengganti lama `/admin/api/users/update-instansi`) terdaftar di bawah `AdminManagementRequired` (`webui/cmd/server/main.go`) — hanya **SuperAdmin & Operator** yang boleh mengganti nama instansi sekolah, karena rename berlaku untuk seluruh akun yang berbagi `instansi_id`. Guru/pengawas — yang sebelumnya dapat memanggil route ini (semua user terautentikasi) — kini ditolak **403**. UI disesuaikan: kartu **"Instansi (Klik untuk Ubah)"** di dashboard hanya dirender untuk SuperAdmin & Operator (`dashboard.html`), dan modal onboarding **"Wajib Atur Nama Instansi/Sekolah"** (`nav.html`, di-drive `needs_instansi` dari `helpers.go`) hanya muncul untuk **operator paket sekolah** yang instansinya belum ditetapkan (kosong / `"personal"` / `"Belum Ditetapkan"`) — guru yang kebetulan memegang label paket `sekolah_*` pemberian SuperAdmin tidak melihat modal yang pasti gagal (403).
- Migrasi skema aman dijalankan ulang: `ALTER TABLE admin_users ADD COLUMN IF NOT EXISTS operator_created BOOLEAN NOT NULL DEFAULT FALSE` (`webui/internal/database/schema.sql`).

**Yang TIDAK terkena dampak:**

- Operator itu sendiri (akun yang membuat sub-akun) — tetap bisa klaim voucher, termasuk voucher paket sekolahnya;
- Akun yang dibuat **SuperAdmin** lewat panel Users — tetap bisa klaim;
- Akun hasil **registrasi mandiri** (`/register`, `instansi = "personal"`) — tetap bisa klaim;
- Akun lama/import (flag `false`).

**Verifikasi (test integrasi otomatis, `webui/internal/handlers/admin/subaccount_voucher_policy_test.go`):**

- `TestSubAccountCannotRedeemVoucher` — sub-akun hasil handler `CreateUser` ditolak **403** untuk kode valid maupun tidak valid (tanpa oracle), tanpa baris redemption, dan kuota voucher tidak terpakai; operatornya sendiri tetap bisa klaim.
- `TestSubAccountCannotActivateVoucher` — redemption pra-kebijakan yang tertinggal di akun sub tidak dapat diaktifkan (403) dan tidak diubah.
- `TestDirectCreatedAccountsCanStillRedeem` — akun buatan SuperAdmin dan akun registrasi mandiri tetap bisa klaim (200).
- `TestBillingPageHidesRedeemFormForSubAccount` — me-render halaman **Paket & Voucher** yang asli (handler `BillingPage` + template nyata) dan memastikan form klaim serta daftar "Paket yang Sudah Anda Klaim" **tidak** dirender untuk akun sub (muncul kartu penjelasan "Akun Sub (Dibuat Operator)"), sedangkan akun biasa tetap mendapat form klaim.
- `TestUsersListAPIReportsOperatorCreated` — endpoint daftar user (produksi `GET /admin/api/users`) mengirim `operator_created: true` untuk akun sub hasil buatan operator dan `false` untuk akun buatan SuperAdmin/registrasi mandiri — kontrak API di balik badge "Dibuat oleh Operator" di halaman Kelola Users.
- `TestOperatorCannotCreateOperatorAccount` — operator (termasuk varian non-kanonik `" operator"` / `"OPERATOR"`) ditolak **400** saat mencoba membuat akun dengan role Operator (guard kini `strings.EqualFold` + `TrimSpace`, menyelaraskan `CreateUser` dengan `EditUser`).
- `TestOperatorCannotAssignPackageToSubAccount` — operator tidak bisa menetapkan paket langganan akun yang dibuatnya: `CreateUser` memaksa label `free`, `EditUser` mengabaikan field `package` untuk caller operator — hanya SuperAdmin yang boleh menetapkan paket.
- `TestOperatorQuotaEnforcedForPersonalInstansi` — kuota bucket `"personal"` (operator sudah redeem paket sekolah tapi belum menetapkan nama instansi) tetap ditegakkan; akun registrasi mandiri (`operator_created = false`) **tidak** ikut menghabiskan kuota sekolah.
- `TestInstansiUpdateRouteRequiresManagementRole` — `POST /admin/api/instansi/update` (wiring produksi) ditolak **403** untuk guru & pengawas (AdminManagementRequired), sukses untuk operator & superadmin.
- `TestUpdateInstansiMigratesPersonalBucketSubAccounts` — saat operator menetapkan instansi sekolah, sub-akun miliknya di bucket `"personal"` ikut dipindah (`instansi` + `instansi_id` + `instansi_code`); akun registrasi mandiri tidak tersentuh.
- `TestPersonalBucketQuotaAndMigrationScopedPerOperator` — **gagal sebelum fix `created_by`**: dua operator personal yang berbagi bucket menghitung sub-akun operator lain sebagai miliknya dan migrasi klaim sekolah menyapu sub-akun operator lain; setelah fix kuota & migrasi scoped per-operator.
- `TestCreatedByDeleteSetsNull` — FK `ON DELETE SET NULL`: menghapus operator tetap sukses walau sub-akunnya sudah dipindah SuperAdmin ke instansi lain; sub-akun selamat dengan `created_by` NULL (fallback legacy bucket bersama).
- `TestDashboardInstansiCardOnlyForManagementRoles` + `TestMandatoryInstansiModalOnlyForUnclaimedSchoolOperator` + `TestAllNavIncludesForwardNeedsInstansi` (`dashboard_page_test.go`) — merender halaman nyata: kartu **"Instansi (Klik untuk Ubah)"** hanya tampil untuk SuperAdmin & Operator (guru/pengawas tidak melihat kontrol yang akan 403), modal onboarding instansi hanya muncul untuk operator paket sekolah yang belum menetapkan instansi, dan partial `nav.html` meneruskan `needs_instansi` (template dicompile dengan data terpotong yang sama seperti produksi).
- `TestNoPublicVoucherRoutes` (`webui/cmd/server/routes_voucher_public_test.go`) — menginspeksi tabel rute hasil `registerRoutes` asli (fungsi yang dipakai `main()`): **tidak ada** jalur claim/aktivasi voucher yang terdaftar di luar prefix `/admin` (pencocokan path `voucher`/`redeem`/`activate`, jadi endpoint publik bernama lain pun tertangkap — claim/aktivasi hanya boleh hidup di `/admin/api/*`, tidak ada versi publik untuk klien token), sekaligus memastikan `POST /admin/api/vouchers/redeem`, `POST /admin/api/vouchers/activate`, dan `GET /admin/api/vouchers/mine` **tetap** terdaftar. Kontrak routing di balik temuan audit bahwa klien Android/desktop tidak punya UI claim voucher dan tidak memanggil endpoint ini. Test ini **tanpa database** — `registerRoutes` hanya membangun handler; pool dibaca dari konteks saat request.
- Test integrasi lama yang memerlukan skenario "sub-akun dengan paket sendiri" (uji suspend/restore cascade & guard masa aktif) kini menanam state tersebut sebagai **klaim pra-kebijakan** via helper `claimSubOwnVoucher`, karena jalur redeem asli sudah ditutup untuk akun sub.

Jalankan dengan:

```bash
TEST_DATABASE_URL=postgresql://user:pass@localhost:5432/examvan_test \
  go test ./internal/handlers/admin/ -run 'TestSubAccount|TestDirectCreatedAccounts|TestBillingPage|TestUsersListAPIReportsOperatorCreated|TestOperator|TestPersonalBucket|TestCreatedBy|TestInstansiUpdate|TestUpdateInstansi|TestDashboardInstansi|TestMandatoryInstansi|TestAllNav' -v
```

Test kontrak rute (tanpa database):

```bash
cd webui
go test ./cmd/server/ -run TestNoPublicVoucherRoutes -v
```

### Cara Mengubah

**Opsi A — Lewat UI Admin (disarankan):**
1. Login sebagai SuperAdmin.
2. Buka halaman **Users** (`/admin/users`) → panel **"SaaS & SMTP Email Settings"**.
3. Ubah field **"Masa Aktif"** (default: `14`, minimal `1`), lalu klik **Simpan Setelan SaaS**.
4. Berlaku langsung untuk pendaftaran/pembuatan akun berikutnya tanpa perlu deploy ulang.

**Opsi B — Langsung di database (mis. untuk sinkronisasi batch/instalasi baru):**

```sql
UPDATE saas_settings SET value = '14' WHERE key = 'default_active_days';
```

Ganti `14` dengan jumlah hari yang diinginkan. Lokasi default di kode: `webui/internal/models/settings.go` → `DefaultSettings` (dipakai hanya jika baris setting belum ada di database).

---

## Konfigurasi Default Paket Pendaftaran — Kuota Storage (Kapasitas Disk)

Setiap akun **baru** yang mendaftar via `/register` juga menerima **kuota storage default 50 MB** — dikontrol oleh pengaturan SaaS **`default_max_storage_size`** (disimpan dalam **byte**; 50 MB = `52428800`). Kuota ini membatasi total ukuran file soal (PDF) yang boleh di-upload akun tersebut; saat tercapai, upload ditolak dengan pesan *"Batas kapasitas storage tercapai"*.

Field **"Maks Storage (MB)"** ada di panel **Default Paket Pendaftaran** (halaman **Users** → panel **"SaaS & SMTP Email Settings"**), bersama Maks Total Ujian, Ujian Serentak, Maks Upload (MB), dan Masa Aktif.

**Semantik nilai:**
- `0` = **tidak terbatas** (enforcement kuota storage dimatikan untuk akun baru);
- nilai positif = batas kuota dalam MB.

**Validasi terhadap kapasitas disk server:**
- Nilai **tidak boleh melebihi sisa kapasitas disk** pada partisi penyimpanan (`STORAGE_PATH`, default `/app/storage`) — diukur via `syscall.Statfs` (`getFreeDiskSpace`, `webui/internal/handlers/admin/helpers.go`).
- Bila sisa disk **tidak dapat ditentukan** (mis. path belum tersedia → 0), validasi dilewati (fail-open) agar penyimpanan setelan lain tidak ikut terblokir.
- Validasi dijalankan **sebelum setelan lain ditulis** — penolakan tidak menyisakan perubahan setengah tersimpan (pola yang sama dengan validasi Turnstile).
- Di UI, input diberi batas `max` sesuai sisa disk + hint dinamis *"Sisa disk server: X GB"* — `GET /admin/api/saas-settings` mengembalikan `storage_free_mb` untuk itu.
- **Validasi yang sama berlaku di form "Tambah User" dan modal "Atur Limit" per-user** (`CreateUser`/`EditUser`, `webui/internal/handlers/admin/users.go`): nilai negatif ditolak, dan nilai positif yang melebihi sisa disk ditolak (HTTP 400) **sebelum akun dibuat / sebelum field lain diubah**. Halaman Users mengirim `storage_free_mb` ke template (`window.__storageFreeMb`) untuk membatasi `max` input kedua form + pre-check klien di `static/js/admin.js` (`createUser`/`submitEditUser`) — server tetap memvalidasi sebagai lapisan final.
- **Alur voucher & paket** menerapkan batas disk yang sama pada **kedua editor kuota**-nya — lihat [Cap Disk di Alur Voucher dan Paket](#cap-disk-di-alur-voucher-dan-paket).
- Dashboard admin menampilkan sisa disk fisik pada **kartu statistik atas → "Sisa Disk Server"** (`admin/dashboard`) — **khusus Super Admin** (role lain tidak melihat kartunya); `GET /admin/api/stats` juga menyertakan `server_disk_free_mb` hanya untuk superadmin (role lain tidak menerima nilai ini).
- **Test stats API & halaman Dashboard** (`webui/internal/handlers/admin/saas_settings_test.go` + `dashboard_page_test.go`): `TestStatsServerDiskFree` memverifikasi `GET /admin/api/stats` — auth-gate 401 tanpa sesi, `server_disk_free_mb > 0` dan konsisten dengan `getFreeDiskSpace` (±1 MB), serta aggregate benar; `TestStatsScopeByRole` memverifikasi scoping per-role — superadmin melihat semua exam, operator hanya exam se-`instansi`, guru hanya `created_by`/`delegated_to` miliknya, pengawas hanya exam yang ditugaskan lewat `exam_pengawas` (dan gabungan guru+pengawas = union), dengan `server_disk_free_mb` **hanya untuk superadmin** (role lain = 0/tidak disertakan). `TestDashboardRendersServerDiskFree` merender **halaman HTML nyata** `/admin/dashboard` dan memastikan kartu **"Sisa Disk Server"** menampilkan nilai sisa disk yang riil (bukan fallback "—", format `X.XX GB`/`X.X MB`), konsisten dengan `getFreeDiskSpace` pada partisi yang sama (bounding snapshot sebelum/sesudah request ± 8 MB — lebih tahan flake daripada toleransi tetap karena tampilan GB membulatkan ke 0.01 GB), serta redirect 302 ke login tanpa sesi; `TestDashboardHidesServerDiskForNonSuper` memastikan role non-superadmin **tidak** melihat kartu/label tersebut (halaman tetap ter-render normal).

> ⚠️ **Catatan:** seperti `default_active_days`, pengaturan ini hanya memengaruhi **akun yang dibuat setelah perubahan** — akun yang sudah ada tidak diubah; kuota storage per-akun diubah lewat tombol **"Atur limit"** di daftar user (nilai `0` di editor itu juga berarti tidak terbatas). Form **Tambah User** dan **Atur Limit** ikut menerapkan batas disk yang sama (lihat di atas).

### Cara Mengubah

**Opsi A — Lewat UI Admin (disarankan):**
1. Login sebagai SuperAdmin.
2. Buka halaman **Users** (`/admin/users`) → panel **"SaaS & SMTP Email Settings"** → bagian **Default Paket Pendaftaran**.
3. Ubah field **"Maks Storage (MB)"** (`0` = tidak terbatas; tidak boleh melebihi sisa disk server), lalu klik **Simpan Setelan SaaS**.
4. Berlaku langsung untuk pendaftaran berikutnya tanpa perlu deploy ulang.

**Opsi B — Langsung di database (mis. untuk sinkronisasi batch/instalasi baru):**

```sql
UPDATE saas_settings SET value = '262144000' WHERE key = 'default_max_storage_size';
```

Ganti `262144000` dengan batas dalam **byte** (contoh: 250 MB = `250 * 1024 * 1024`). Nilai `0` = tidak terbatas. Lokasi default di kode: `webui/internal/models/settings.go` → `DefaultSettings` (dipakai hanya jika baris setting belum ada di database).

---

## Cloudflare Turnstile (Anti-Bot) — Aktivasi & Pemecahan Masalah

Turnstile melindungi halaman publik **`/register`**, **`/login`**, **`/forgot-password`**, dan **`/reset-password`** dari bot/registrasi massal. Kebijakan verifikasi bersifat **fail-closed**: saat diaktifkan, submit tanpa token Turnstile yang valid akan ditolak server.

### Langkah Aktivasi

1. **Buat widget di Cloudflare dashboard** — buka `dash.cloudflare.com` → **Turnstile** → **Add Site**:
   - Beri nama widget (mis. "EXAMVAN"), pilih mode (disarankan **Managed**), dan isi **Hostname/Domain** situs Anda (mis. `examvan.school.id`). Domain yang tidak terdaftar di sini akan menampilkan kotak error merah pada widget.
   - Catat **Site Key** dan **Secret Key** (format `0x4AAAA...`).
2. **Aktifkan di panel admin** — login SuperAdmin → **Users** (`/admin/users`) → panel **"SaaS & SMTP Email Settings"** → bagian **Cloudflare Turnstile (Anti-Bot)**:
   - Centang **Aktifkan Turnstile**.
   - Tempel **Site Key** dan **Secret Key** (secret disimpan terenkripsi/masked, tidak pernah ditampilkan utuh).
   - Klik **Simpan Setelan SaaS**. Server menolak penyimpanan bila salah satu key kosong saat Turnstile diaktifkan.
3. **Pastikan CSP nginx mengizinkan domain Turnstile** — header `Content-Security-Policy` di `webui/nginx/nginx.conf` harus memuat `https://challenges.cloudflare.com` di `script-src`, `frame-src`, `connect-src`, dan `img-src` (sudah terpasang di versi saat ini; jika Anda memakai CSP kustom, tambahkan keempat direktif tersebut sesuai [dokumentasi Cloudflare](https://developers.cloudflare.com/turnstile/)).
4. **Reload nginx** setelah mengubah `nginx.conf`:
   ```bash
   docker exec examvan-webui-nginx nginx -s reload
   ```
5. **Verifikasi** — hard-refresh browser (Ctrl+Shift+R), lalu pastikan widget checkbox muncul di `/register`:
   ```bash
   curl -s -o /dev/null -D - -H 'X-Forwarded-Proto: https' http://localhost/register | grep -i content-security-policy
   # → header harus memuat: script-src ... https://challenges.cloudflare.com ... frame-src https://challenges.cloudflare.com
   ```

### Pemecahan Masalah

| Gejala | Penyebab | Solusi |
|--------|----------|--------|
| Widget tidak muncul sama sekali, submit diblokir dengan *"Harap selesaikan verifikasi keamanan di atas..."* | Script `api.js` Turnstile diblokir CSP (domain `challenges.cloudflare.com` tidak ada di `script-src`/`frame-src`) | Perbaiki CSP di `webui/nginx/nginx.conf` lalu reload nginx (langkah 3–4) |
| Widget tampil sebagai **kotak merah/error** | Site Key salah, atau domain situs belum terdaftar di daftar Domain widget di dashboard Turnstile | Perbaiki Site Key / tambahkan domain di dashboard Turnstile |
| Widget muncul & tercentang, tapi server tetap menolak | **Secret Key** salah di panel admin | Periksa Secret Key di **Users → SaaS Settings → Cloudflare Turnstile** |
| CSP memblokir resource lain (mis. di luar nginx) | Ada header CSP kedua dari aplikasi/proxy lain | Pastikan hanya satu sumber CSP, atau gabungkan keempat direktif Turnstile ke header kustom |

---

## Cap Disk di Alur Voucher dan Paket

> Mencakup **dua kuota**: Maks Storage dan Maks Ukuran PDF.

Validasi kapasitas disk yang sama dengan editor storage lainnya juga diterapkan pada **kedua editor kuota di alur voucher dan paket** — sehingga batas yang dijanjikan ke akun tidak pernah melebihi kapasitas fisik partisi penyimpanan server (`STORAGE_PATH`).

**Di mana editor-nya:**

- **Voucher kustom** — halaman **Vouchers** (`/admin/vouchers`) → paket **Custom** pada form *Tambah Voucher* (single) dan *Buat Batch*: field **"Maks Storage (MB)"** (`custom_max_storage_size_mb`) dan **"Maks Upload (MB)"** (`custom_max_pdf_size_mb`). Divalidasi di `parseCustomVoucherInto` (`webui/internal/handlers/admin/vouchers.go`) — jalur **single maupun batch**.
- **Pengaturan paket** — halaman **Paket** (`/admin/packages`): kolom **Storage (MB)** (`max_storage_mb`) dan **Maks. PDF (MB)** (`max_pdf_size_mb`) per paket. Divalidasi di `SavePackageSettingsHandler` (`webui/internal/handlers/admin/packages.go`).
- **Panel SaaS & form user** — "Maks Upload (MB)" (`default_max_pdf_size_mb`) di panel **Default Paket Pendaftaran** (halaman Users), input "Maks Upload (MB)" di form **Tambah User** (`max_pdf_size_mb`), dan modal **Atur Limit** per-user — semuanya tunduk pada cap disk yang sama (`handleSaasSettingsPost` di `settings.go`, `CreateUser`/`EditUser` di `users.go`).

**Aturan (identik di kedua alur):**

| Nilai | Perilaku |
|---|---|
| Negatif | Ditolak HTTP 400 — *"Maks Storage / Maks Ukuran PDF tidak boleh bernilai negatif."* |
| `0` | Voucher: **tidak terbatas** (enforcement `MaxStorageSize`/`MaxPDFSize` dimatikan). Paket: dibulatkan ke minimum 1 MB (paket wajib punya kuota minimal). |
| Positif `> sisa disk` | Ditolak HTTP 400 — *"…melebihi sisa kapasitas disk server (X GB)."* |
| Sisa disk tak dapat ditentukan | Fail-open (validasi dilewati), mengikuti pola editor lain |

**Detail implementasi:**

- **Penolakan sebelum menulis:** voucher tidak pernah dibuat (penolakan di `parseCustomVoucherInto`); paket divalidasi **di dalam transaksi sebelum commit** — seluruh payload dicek terhadap **satu snapshot `freeBytes`** (satu panggilan `statfs` per request, konsisten antar-paket), dan jika salah satu paket invalid seluruh transaksi di-`rollback` (tanpa partial-save).
- **Helper bersama:** `validateMBQuota(label, freeBytes, mb)` (`webui/internal/handlers/admin/users.go`) sebagai inti; dipanggil dengan label "Maks Storage" (storage) dan "Maks Ukuran PDF" (PDF).
- **UI:** halaman Vouchers & Paket menerima `storage_free_mb` → input diberi `max` + `title` sisa disk dan pre-check klien (`window.__storageFreeMb`); server tetap lapisan validasi final.
- **Batas upload global 100 MB:** upload PDF dibatasi keras **100 MB** (`maxFileSize`, `webui/internal/handlers/admin/exams.go`) — kuota PDF > 100 MB tidak akan pernah bisa dieksekusi, jadi di UI `max` input PDF dibatasi `min(sisa disk, 100 MB)` dan pre-check paket menolak `> 100 MB` dengan pesan khusus. Validasi server tetap disk-cap murni.

**Cara mengubah:** nilai diubah dari halaman **Vouchers** (form Custom / batch) dan **Paket** (tabel pengaturan paket) — kedua halaman hanya untuk Super Admin (`SuperAdminRequired`).

---

## Pengujian (Tes Otomatis)

### 1. Tes Unit (tanpa database)

Sebagian besar tes (model, entitlement, helper) adalah unit test murni dan berjalan tanpa PostgreSQL:

```bash
cd webui
go test ./...
```

> Semua tes integrasi yang memakai `database.NewPackageTestPool` (mis. `internal/handlers/admin/voucher_lifecycle_integration_test.go`) otomatis **di-skip** bila `TEST_DATABASE_URL` tidak diatur — jadi perintah di atas selalu hijau walau tanpa database.

### 2. Tes Integrasi (membutuhkan PostgreSQL)

Tes integrasi berjalan di atas database nyata. Setiap paket yang butuh akses database memakai **satu pola yang sama**: `database.NewPackageTestPool(t, "<nama_paket>")` (di `webui/internal/database/testdb.go`) — **bukan konstanta skema yang didaftarkan manual**. Fungsi ini:

- **Menurunkan skema isolasi dari nama paket** (`it_<nama_paket>` — mis. `it_admin`, `it_models`), jadi tidak ada daftar konstanta skema yang harus dirawat;
- **Menerapkan `schema.sql`** dan **me-truncate tabel data** otomatis di skema milik paket tersebut — setiap paket hanya menyentuh skemanya sendiri, sehingga `go test ./...` dapat menjalankan paket-paket secara paralel tanpa saling mengunci (deadlock TRUNCATE) di skema `public` bersama;
- **Skip (bukan gagal)** bila `TEST_DATABASE_URL` tidak diatur, jadi `go test ./...` polos (tanpa PostgreSQL) tetap hijau;
- Mendukung `TEST_DATABASE_RESET=1` untuk men-drop dan membuat ulang skema **sekali per proses `go test`** — dipakai saat `schema.sql` berubah bentuk (mis. rename kolom) agar artefak skema lama tidak tersisa.

Contoh di setup test sebuah paket:

```go
pool := database.NewPackageTestPool(t, "admin")
```

Pemakaian nyata ada di `internal/handlers/admin/voucher_lifecycle_integration_test.go` (`setupVoucherITDB`) dan `internal/models/authenticate_test.go` (`setupAuthTestDB`). Salah satunya, tes integrasi voucher, menguji alur lengkap sistem paket/voucher terhadap database nyata:

- operator membuat akun → pindah ke voucher guru → akun di-suspend → kembali ke sekolah → akun pulih (dengan clock-freeze);
- auto-fallback expiry job: paket sekolah habis masa → akun sub di-suspend → auto-aktif ke voucher guru;
- tanpa voucher cadangan → akun tetap expired dan login tetap terblokir;
- akun sub yang membeli voucher sendiri tetap ikut ter-suspend saat operator keluar dari paket sekolah, dan paket miliknya sendiri tidak ikut di-pause.

**Cara termudah** — jalankan PostgreSQL 16 sekali pakai via Docker, lalu arahkan `TEST_DATABASE_URL` ke database tersebut (tidak perlu diisi apa pun; `NewPackageTestPool` yang menerapkan skema dan membersihkan tabel data):

```bash
# 1) Jalankan PostgreSQL 16 sekali pakai
docker run -d --name examvan-test-pg \
  -e POSTGRES_USER=examvan -e POSTGRES_PASSWORD=examvan \
  -e POSTGRES_DB=examvan_test -p 5432:5432 postgres:16-alpine

# 2) Jalankan seluruh tes (termasuk integrasi)
cd webui
TEST_DATABASE_URL=postgresql://examvan:examvan@localhost:5432/examvan_test \
  go test ./...

# 3) Hapus container uji
docker rm -f examvan-test-pg
```

Hanya tes voucher (lebih cepat, verbose):

```bash
cd webui
TEST_DATABASE_URL=postgresql://examvan:examvan@localhost:5432/examvan_test \
  go test ./internal/handlers/admin/ -run 'TestVoucher' -v
```

> ⚠️ **Penting:** `TEST_DATABASE_URL` harus mengarah ke database **sekali pakai** — saat setup, tes menerapkan `schema.sql` dan me-truncate tabel data. Jangan pernah mengarahkannya ke database produksi.

#### Referensi Variabel Environment Tes

| Variabel | Status | Nilai | Perilaku |
|----------|--------|-------|----------|
| `TEST_DATABASE_URL` | Diperlukan untuk tes integrasi | URL koneksi PostgreSQL, mis. `postgresql://examvan:examvan@localhost:5432/examvan_test` | Database tempat `NewPackageTestPool` membuat skema isolasi `it_<paket>`, menerapkan `schema.sql`, dan me-truncate tabel data. **Bila kosong/tidak diatur, semua tes integrasi di-skip** (bukan gagal) — `go test ./...` polos tetap hijau tanpa PostgreSQL. Diatur via environment (bukan `.env`). |
| `TEST_DATABASE_RESET` | Opsional | `1` atau `true` (case-insensitive) | Mode fresh-start: **men-drop (CASCADE) dan membuat ulang skema `it_<paket>` sekali per proses `go test`** (dijaga `sync.Once` per skema — hanya setup test pertama yang mendapat slate bersih; setup berikutnya tetap TRUNCATE biasa). Dipakai setelah `schema.sql` berubah bentuk (mis. rename kolom) agar objek skema lama tidak tersisa. Tanpa variabel ini, skema dipertahankan antar-run — aman, karena `schema.sql` idempotent dan tiap test me-truncate tabel datanya sendiri. |

Contoh — reset skema (sekali) lalu jalankan seluruh suite termasuk integrasi:

```bash
cd webui
TEST_DATABASE_URL=postgresql://examvan:examvan@localhost:5432/examvan_test \
TEST_DATABASE_RESET=1 \
  go test ./...
```

Verifikasi bahwa tes integrasi benar-benar jalan (bukan di-skip) — output verbose akan menampilkan `=== RUN` (bukan `SKIP`):

```bash
cd webui
TEST_DATABASE_URL=postgresql://examvan:examvan@localhost:5432/examvan_test \
  go test ./internal/models/ -run TestAuthenticateUser -v
```

> **Lanjutan:** latar belakang & detail pola isolasi skema per-paket juga dibahas di [upgrade_arsitektur.md → Lampiran — Pola Test Database (NewPackageTestPool)](upgrade_arsitektur.md#lampiran--pola-test-database-newpackagetestpool).

### 3. CI (GitHub Actions)

Workflow `.github/workflows/ci.yml` (di root repo, bukan di `webui/.github`) otomatis menjalankan seluruh suite — termasuk tes integrasi — pada setiap push/pull request, menggunakan service `postgres:16-alpine` bawaan GitHub Actions. Tidak diperlukan konfigurasi tambahan.

### 4. Tes E2E Kuota "Ujian Serentak" (opsional, manual)

Dua skrip E2E di `webui/` memverifikasi perbaikan kuota "ujian serentak" secara end-to-end: membuat user & ujian uji sekali pakai (via SQL langsung), login lewat API admin asli (login + CSRF), menegakkan kuota, lalu membersihkan semuanya — termasuk menghapus objek PDF palsu di R2 lewat API delete:

- **`test_concurrent_quota.go`** — terhadap **server native dev** di `:5001` (`APP_ENV=development`: cookie tidak Secure, tanpa Redis). Base URL bisa di-override via env `BASE_URL`.
- **`test_concurrent_quota_prod.go`** — terhadap **stack Docker produksi** via nginx `:80`; transport-nya menambahkan header `X-Forwarded-Proto: https` (mensimulasikan hop Cloudflare Tunnel) dan menerima cookie `Secure` seperti browser di HTTPS. Base URL juga bisa di-override via env `BASE_URL` (default `http://localhost:80`).

Kedua skrip menjalankan **14 asersi**: enforce kuota ujian serentak (`start`/`toggle`/`bulk-toggle` ditolak **403** saat kuota penuh, `stop` membebaskan kuota, superadmin bypass) dan kuota `max_exams` saat upload (dari 5× upload → **tepat 3 sukses**, 2 ditolak, tanpa overshoot race).

> ✅ **Hasil verifikasi (9 Agustus 2026):** kedua skenario lulus **14 PASS / 0 FAIL** — dev native (`test_concurrent_quota.go`) dan stack produksi (`test_concurrent_quota_prod.go`).
> 🔧 **Override `BASE_URL` terverifikasi:** kedua skrip membaca env `BASE_URL` (default `http://localhost:5001` untuk dev, `http://localhost:80` untuk produksi). Diuji dengan `BASE_URL=http://127.0.0.1:59999` — request login mengarah ke URL custom tersebut (error connection-refused menyebut `127.0.0.1:59999`), bukan default.

**Menjalankan versi dev native:**

```bash
# 1) PostgreSQL 16 sekali pakai
docker run -d --name examvan-test-pg \
  -e POSTGRES_USER=examvan -e POSTGRES_PASSWORD=examvan \
  -e POSTGRES_DB=examvan_test -p 5432:5432 postgres:16-alpine

# 2) Start server native :5001 (R2 wajib — isi R2_* & EXAMVAN_SECRET dari .env)
cd webui
PORT=5001 APP_ENV=development \
DATABASE_URL=postgresql://examvan:examvan@localhost:5432/examvan_test?sslmode=disable \
  go run ./cmd/server

# 3) Terminal lain — jalankan test (server :5001 harus hidup)
cd webui
DATABASE_URL=postgresql://examvan:examvan@localhost:5432/examvan_test?sslmode=disable \
  go run test_concurrent_quota.go
```

**Menjalankan versi stack produksi** (kredensial diambil dari `webui/.env`; arahkan `DATABASE_URL` ke container DB di jaringan Docker, mis. IP `172.18.0.x`):

```bash
cd webui
DATABASE_URL=postgresql://examvan:<DB_PASSWORD>@<ip-db-container>:5432/examvan \
EXAMVAN_ADMIN_USER=<admin> EXAMVAN_ADMIN_PASS=<pass> \
  go run test_concurrent_quota_prod.go
```

> ⚠️ **Catatan:** kedua skrip menulis langsung ke database yang dituju (membuat/menghapus user & ujian uji) dan meng-upload PDF palsu ke R2. Untuk versi dev gunakan database **sekali pakai**; untuk versi produksi pastikan Anda bersedia menerima data uji sementara (keduanya membersihkan sendiri setelah selesai). Jangan arahkan `DATABASE_URL` ke database produksi sungguhan.

> **Lanjutan:** ringkasan hasil verifikasi & cara menjalankan juga ada di [upgrade_arsitektur.md → Lampiran — Hasil Verifikasi E2E Kuota "Ujian Serentak"](upgrade_arsitektur.md#lampiran--hasil-verifikasi-e2e-kuota-ujian-serentak).

---

## Informasi Akses Default Admin Panel

Akses halaman admin melalui domain publik (Cloudflare Tunnel): **`https://<domain>/admin/login`**

* **Username:** `superadmin` (atau sesuai `EXAMVAN_ADMIN_USER`)
* **Password:** Sesuai `EXAMVAN_ADMIN_PASS` di `.env` (atau cek log jika dikosongkan)

> Demi keamanan, segera ubah password akun administrator utama setelah berhasil masuk untuk pertama kali.

---

## Sinkronisasi Waktu (Timezone)

Semua waktu di dalam sistem EXAMVAN disimpan dan diproses dalam **UTC+0** untuk menjamin konsistensi di seluruh perangkat.

```
SERVER (Docker, TZ=UTC)
  Database: CURRENT_TIMESTAMP → UTC
  Go:       time.Now().UTC()
  API:      Semua timestamp dalam format ISO 8601 Z
        ↓ JSON (UTC)
  ┌─────────────────────────────────────┐
  │ ADMIN PANEL (Browser)               │
  │ → JS localizeDates() → timezone lokal│
  ├─────────────────────────────────────┤
  │ ANDROID (Device Siswa)              │
  │ → Timestamp UTC, display sesuai TZ  │
  │ → /api/time untuk verifikasi jam    │
  └─────────────────────────────────────┘
```

Endpoint verifikasi waktu server:
```bash
curl https://<domain>/api/time
```

---

## Index Database (Kolom FK yang Sering Di-Query)

Audit menyeluruh kolom **FOREIGN KEY** di `webui/internal/database/schema.sql`: setiap kolom FK yang **sering di-query** memiliki index pendukung. Semua index dibuat dengan `CREATE INDEX IF NOT EXISTS` — idempotent, aman dijalankan ulang setiap boot (skema diterapkan otomatis saat server start).

| Tabel | Kolom FK | Referensi | Index pendukung | Dipakai oleh |
|---|---|---|---|---|
| `exams` | `created_by` | `admin_users(id)` | `idx_exams_created_by` | Scoping kepemilikan exam (guru/operator), stats dashboard, list submissions, tombstone |
| `exams` | `delegated_to` | `admin_users(id)` | `idx_exams_delegated_to` | Scoping kepemilikan (delegasi pengawas) |
| `admin_users` | `created_by` | `admin_users(id)` | `idx_admin_users_created_by` | Kuota bucket `"personal"` per-operator (`loadOperatorAccountQuota`) & migrasi sub-akun saat klaim sekolah (`UpdateInstansi`) |
| `admin_users` | `instansi_id` | `instansi(id)` | `idx_admin_users_instansi_id` | Migrasi `UpdateInstansi` (rename/pindah seluruh user se-instansi) |
| `exam_pengawas` | `exam_id` | `exams(id)` | `idx_exam_pengawas_exam_id` (+ `UNIQUE(exam_id, user_id)`) | Daftar pengawas per exam, cek kepemilikan |
| `exam_pengawas` | `user_id` | `admin_users(id)` | `idx_exam_pengawas_user_id` | Daftar exam per pengawas, cek kepemilikan |
| `submissions` | `exam_id` | `exams(id)` | `idx_submissions_exam_id` + `idx_submissions_exam_mac` | List hasil per exam, dedup perangkat/nomor ujian |
| `student_access_logs` | `exam_id` | `exams(id)` | `idx_student_access_logs_exam_id` + `idx_access_logs_exam_time` | Log kehadiran per exam, riwayat perangkat |
| `student_access_logs` | `submission_id` | `submissions(id)` | — (sengaja tanpa index) | Kolom **hanya ditulis** (`ON DELETE SET NULL`); tidak pernah dipakai sebagai filter/join |
| `exam_approvals` | `exam_id` | `exams(id)` | `UNIQUE(exam_id, mac_address)` (kolom pertama) | Status persetujuan perangkat per exam |
| `exams` (bukan FK) | `end_time` | — | parsial `idx_exams_end_time ON exams(end_time) WHERE end_time IS NOT NULL` | DELETE cleanup approval basi (`PurgeStaleExamApprovals`) — scan ujian berakhir tetap terindeks saat data membesar |
| `vouchers` | `created_by` | `admin_users(id)` | `idx_vouchers_created_by` | JOIN daftar voucher → username pembuat |
| `voucher_redemptions` | `voucher_id` | `vouchers(id)` | `idx_voucher_redemptions_voucher_id` | Cek `used_count` saat redeem, riwayat pemakaian per voucher, backfill legacy |
| `voucher_redemptions` | `user_id` | `admin_users(id)` | `idx_voucher_redemptions_user_id` (+ parsial unik `idx_voucher_redemptions_one_active WHERE is_active`) | Daftar paket per akun, guard entitlement, job expiry/cascade |

> **Catatan desain:** `student_access_logs.submission_id` sengaja **tidak** diberi index — kolom itu hanya diisi saat submit dan di-null-kan oleh `ON DELETE SET NULL`; tidak ada query yang memfilter/join dengannya, jadi index hanya menambah biaya write pada tabel yang paling sering di-INSERT. Index parsial `idx_voucher_redemptions_one_active` (unik, `WHERE is_active`) menegakkan "maks satu paket aktif per user"; index penuh `idx_voucher_redemptions_user_id` melengkapinya untuk query yang tidak memfilter `is_active`.

Daftar index tambahan untuk tuning skala besar (access log, submissions, exam aktif) ada di [upgrade_arsitektur.md → Priority #3: PostgreSQL Tuning + Index](upgrade_arsitektur.md#priority-3-postgresql-tuning--index).

---

## Struktur Direktori

```
EXAMVAN/
├── android/                    # Aplikasi Android (Kotlin)
│   ├── app/src/main/java/com/examvan/app/
│   │   ├── api/                # API client, WebSocket
│   │   ├── helper/             # AnswerSheetBuilder, PdfRenderer, Submission, Security
│   │   ├── model/              # Data models
│   │   └── receiver/           # Device Admin Receiver (kiosk)
│   └── app/src/main/res/       # Layouts, drawables, values
├── webui/                      # Backend Go + Admin Panel
│   ├── cmd/server/             # Entry point (main.go)
│   ├── internal/
│   │   ├── config/             # Konfigurasi aplikasi
│   │   ├── database/           # Koneksi PostgreSQL
│   │   ├── handlers/           # Route handlers (admin, api, public)
│   │   ├── helpers/            # Utility (scoring, sanitization)
│   │   ├── middleware/          # Auth, CSRF, rate-limit, timeout, version check
│   │   ├── models/             # Data models (exam, user, submission, dll)
│   │   ├── queue/              # Submission queue
│   │   ├── redis/              # Redis client
│   │   └── websocket/          # WebSocket hub
│   ├── templates/              # HTML templates
│   │   ├── admin/              # Admin panel (dashboard, users, pengawas, submissions)
│   │   └── public/             # Halaman publik (hasil, download, register, index)
│   ├── static/                 # CSS, JS, favicon
│   ├── nginx/                  # Nginx config
│   ├── docker/                 # Docker support files
│   ├── docker-compose.yml      # Multi-container orchestration
│   ├── Dockerfile              # Multi-stage Go build
│   ├── .env.example            # Template environment variables
│   ├── seed.sql                # Seed data (opsional)
│   ├── go.mod                  # Go module dependencies
│   └── go.sum                  # Go dependency checksums
│   └── app/build/outputs/apk/  # Output build APK (student/kiosk) — di-upload ke R2 via System Apps
├── CLAUDE.md                   # Project instructions
├── README.md                   # Dokumentasi ini
└── package.json                # Tailwind CSS build scripts
```

---

## Perbaikan Ujian Serentak — Submit Async, Jadwal, & Perangkat Bersama (9 Agustus 2026)

Tiga perbaikan menyusul pemeriksaan alur ujian. Kode + tes integrasi ada di `webui/`; ringkasan temuan ada di [webui/BUG_REVIEW.md](webui/BUG_REVIEW.md) (bagian "Perbaikan tambahan — alur ujian (9 Agustus 2026)").

### 1. Submit async kini punya jalur kembali ke skor siswa (Fix #2)

**Masalah:** Saat Redis aktif, `POST /api/exams/:exam_id/submit` men-*enqueue* job dan membalas `status:"queued"` dengan `job_id` — tetapi tidak ada endpoint untuk menanyakan hasilnya. Worker menulis key hasil ke Redis (`examvan:submissions:result:<job_id>`, TTL 5 menit) dan `retryOrFail` (maks 3×) bisa gagal diam-diam, sementara siswa mengira jawabannya sudah terkumpul.

**Solusi (endpoint polling):** endpoint publik baru **`GET /api/exams/:exam_id/result`** (`webui/internal/handlers/api/exams.go` → `ExamResult`, terdaftar di `cmd/server/main.go:515`). Klien memanggil dengan:

```
GET /api/exams/:exam_id/result?job_id=<dari submit>
# atau, bila key Redis sudah kedaluwarsa, sertakan identitas perangkat:
GET /api/exams/:exam_id/result?mac_address=DEVICE:...&identity_data=<json utk identitas>
```

Urutan pencarian hasil: **key Redis job (authoritative)** → bila key sudah TTL 5 menit, **fallback ke database** via `models.GetLatestSubmissionByIdentity` (cocokkan `mac_address` + `identity_data` pada submit) → kalau belum ada, balas `pending`. Bentuk respons menyamai bentuk submit sinkron sehingga klien menangani kedua jalur sama:

```json
{"success":true,  "status":"done",    "score":87.5, "message":"Jawaban berhasil disimpan"}
{"success":true,  "status":"pending", "score":null, "message":"Jawaban masih diproses"}
{"success":false, "status":"failed",  "message":"<alasan>"}
```

Tes: `TestExamResultReturnsScoreFromDB`, `TestExamResultPending` (`webui/internal/handlers/api/exams_test.go`).

### 2. `end_time` kini ditegakkan server-side dengan grace 60 detik (Fix #3)

**Masalah:** `start_time`/`end_time` hanya dihitung oleh aplikasi Android; server hanya memeriksa `IsActive() && ExamStartedAt != nil`, jadi ujian yang waktunya sudah lewat masih bisa dibuka/disubmit dari luar aplikasi.

**Solusi:** helper `examScheduleEnded(exam, now)` (`webui/internal/handlers/api/exams.go:902`) mengembalikan `true` bila `now > end_time + 60s` (konstanta grace `scheduleGraceEnd` untuk submit yang datang tepat di tenggat lewat koneksi lambat). Diterapkan **konsisten** di empat jalur:

| Jalur | Penolakan |
|-------|-----------|
| `GET /api/exams/token/:token` (buka soal) | `403 "Waktu ujian telah berakhir"` |
| `GET /api/exams/:exam_id/pdf` (download PDF) | `403 "Waktu ujian telah berakhir"` |
| `POST /api/exams/:exam_id/submit` (kumpulkan) | `403 "Waktu ujian telah berakhir"` |
| `POST /api/exams/:exam_id/access-log` (heartbeat) | `403 "Waktu ujian telah berakhir"` |

`end_time` sekarang juga dikembalikan dalam payload `ExamByToken`. `CompleteExam` **sengaja tidak di-gate** — klien memanggilnya tepat setelah submit di batas waktu untuk membersihkan heartbeat offline.

Tes: `TestExamScheduleEnded` (unit — end tak diset, future, dalam grace 60s, lewat 5 menit, lewat 2 hari), `TestExamByTokenRejectsPastEndTime` (403), `TestExamByTokenAllowsInsideWindow` (200).

### 3. Dua siswa berbagi satu perangkat tidak lagi tergabung satu baris (Fix #4)

**Masalah:** Baris submission **dide-dup murni per `mac_address`** (identitas device). Dua siswa yang memakai satu perangkat secara bergantian akan disatukan ke satu baris → kehadiran/in-progress salah hitung dan hasil ujian salah.

**Solusi:** dedup kini di-scope dengan `exam_number` saat tidak kosong — di `upsertSubmissionRow` (`webui/internal/queue/submission_queue.go:475`) dan `CreateSubmission` (`webui/internal/models/submission.go:405`). Perangkat bersama + 2 nomor ujian → **dua baris terpisah**; nomor ujian kosong → perilaku lama (satu baris per perangkat) dipertahankan.

Tes: `TestUpsertSubmissionRowTwoStudentsShareDevice` (`webui/internal/queue/submission_queue_test.go`).

---

## Hardening Anti-Spam Auto-Approve & Antrean Persetujuan (10 Agustus 2026)

Audit keamanan `POST /api/exams/request-approval` (gerbang persetujuan perangkat) menemukan endpoint publik **tanpa token** yang bisa di-spam: siapa pun yang menebak `exam_id` dapat mengisi antrean `exam_approvals` (pending) dan — saat auto-approve aktif — menyetujui perangkat sendiri **tanpa batas** (plus satu baris `submissions` per perangkat), membengkakkan database dan halaman pengawasan. Empat lapis mitigasi diterapkan:

1. **Token wajib** — `request-approval` kini menolak **401** tanpa token exam yang valid (`examtoken.Matches`, `webui/internal/handlers/api/exams.go`). Perangkat yang *sudah punya* baris approval tetap ditoleransi dengan token basi (toleransi rotasi token dinamis — sama seperti `submit`/`access-log`), sehingga token yang berputar di tengah menunggu tidak membuat siswa macet. Klien Android (`ApiClient.kt`, v2.5.0) dan desktop (`api.py`) mengirim `token` pada setiap poll.
2. **Cap perangkat per exam** — saat auto-approve aktif dan jumlah perangkat berstatus `approved` mencapai batas, request berikutnya jatuh ke antrean `pending` (bukan di-approve otomatis). Batas diatur lewat setting SaaS **`max_approvals_per_exam`** (default `500`; `0` = tak terbatas; `webui/internal/models/settings.go`) — **dapat diubah dari UI** oleh SuperAdmin di **Users → panel "SaaS & SMTP Email Settings" → bagian Cloudflare Turnstile (Anti-Bot) → "Maks Perangkat Disetujui per Ujian"** (GET/POST `/admin/api/saas-settings`; nilai negatif di-clamp ke 0 = tak terbatas). Perangkat yang sudah approved **tidak pernah** diturunkan oleh cap.
3. **Rate limit per-exam global (Redis)** — bucket `ratelimit:reqapp-exam:<exam_id>` (6000/menit) di atas limiter per-IP middleware, menahan banjir terdistribusi yang memutar IP terhadap satu ujian.
4. **Pagination antrean persetujuan** — `GET /admin/api/pengawas/exams/:exam_id/approvals` menerima `page`/`limit` (default 100, maks 500) dan mengembalikan `total`; UI detail pengawasan (`pengawas_detail.html`) memuat 200 perangkat terlama dan menampilkan catatan saat antrean lebih besar dari yang ditampilkan.

Versi minimum klien dinaikkan ke **2.5.0** (`requiredAndroidVersion` + `SettingAndroidVersion` server, APK `versionCode 34`, desktop `APP_VERSION`) — rilis APK baru **wajib** agar halaman menunggu persetujuan tetap berfungsi (klien lama tidak mengirim `token` dan akan ditolak 401). **Pada instalasi lama**, naikkan juga `android_version` di **SaaS Settings → Android Version** (langkah rilis yang sama seperti biasa) agar klien usang mendapat 426 force-update yang jelas, bukan 401 di layar menunggu.

Tes: `TestRequestApprovalRequiresToken`, `TestRequestApprovalRejectsWrongToken`, `TestRequestApprovalCapLimitsAutoApprovedDevices`, `TestRequestApprovalKnownDeviceToleratesStaleToken` (`webui/internal/handlers/api/request_approval_test.go`); suite spam `TestRequestApprovalSpamMassDevicesBeyondCap`, `TestRequestApprovalSpamResetDoesNotBypassCap`, `TestRequestApprovalCapZeroDisablesBrake`, `TestRequestApprovalRejectedDeviceFreesCapSlot`, `TestRequestApprovalSpamWithoutTokenCreatesNoRows`, plus unit test Redis `TestCheckRateLimitFloodBrake` (miniredis) & wiring `TestRequestApprovalWorksWithRedisPresent` (`webui/internal/handlers/api/request_approval_spam_test.go`); `TestGetPendingApprovalsPaginated` (`webui/internal/handlers/admin/auto_approve_test.go`).

### Jejak Audit Toggle Auto-Approve (siapa menyalakan/mematikan kapan)

Toggle **auto-approve** (server-side) kini meninggalkan jejak audit append-only di tabel **`admin_audit_logs`** — jawaban atas pertanyaan akuntabilitas *"siapa yang menyalakan auto-approve, dan kapan?"*:

- **Setiap toggle berhasil** (`POST /admin/api/pengawas/exams/:exam_id/auto-approve` yang lolos otorisasi exam-scoped) menulis **satu baris**: `user_id` + snapshot `username` (denormalisasi), `action` (`auto_approve_enable` / `auto_approve_disable`), `exam_id`, `detail`, dan `created_at` (`models.CreateAdminAuditLog`, dipanggil dari `SetAutoApprove` di `webui/internal/handlers/admin/pengawas.go`). Caller yang ditolak **403 tidak pernah** menulis baris — percobaan terlarang tidak bisa memalsukan jejak.
- **Keputusan per perangkat ikut diaudit:** `POST /admin/api/pengawas/exams/:exam_id/approvals/:mac_address` (`SetApprovalStatus` — Izinkan/Tolak) kini juga menulis baris dengan `action` `approval_approved` / `approval_rejected` dan `detail` berisi snapshot identitas perangkat (`MAC (nama siswa)`). Baris hanya ditulis saat `UPDATE` benar-benar mengubah baris (`RowsAffected > 0`) — keputusan ke perangkat yang tidak dikenal **tidak** memalsukan jejak — dan tetap best-effort: kegagalan menulis audit tidak membatalkan keputusan.
- **Append-only:** baris tidak pernah di-update/dihapus oleh kode aplikasi. `user_id`/`exam_id` memakai `ON DELETE SET NULL` (jejak tetap terbaca walau aktor/ujian dihapus), sementara snapshot `username`/`detail` menjaga keterbacaan setelah rename akun. Index `(exam_id, created_at DESC)` mendukung query "toggle terakhir per ujian".
- **Hint di UI pengawasan:** `GET /admin/api/pengawas/exams/:exam_id/auto-approve` kini mengembalikan `last_changed_by`, `last_changed_at` (RFC3339), dan `last_action` dari baris audit terakhir — halaman detail pengawasan (`pengawas_detail.html`) menampilkannya sebagai chip **"Diaktifkan/Dimatikan oleh <user> · <waktu>"** di samping toggle (`loadAAStatus`/`updateAAHint`, di-refresh otomatis setelah setiap toggle).
- **Panel riwayat lengkap per ujian:** tombol **"Riwayat Audit"** di toolbar antrean persetujuan membuka modal berisi **seluruh** jejak `admin_audit_logs` ujian (terbaru di atas) — `GET /admin/api/pengawas/exams/:exam_id/audit-logs?limit=` (default 100, maks 500, `admin.GetExamAuditLogs` + `models.ListExamAuditLogs`, otorisasi exam-scoped sama dengan endpoint lain, payload dibatasi anti-DoS). Setiap baris menampilkan aksi (dengan warna: hijau = diaktifkan, merah = dimatikan), aktor, dan waktu (`showAuditLog` di `pengawas_detail.html`).

Tes: `TestAutoApproveToggleWritesAuditLog` (2 baris berurutan: enable lalu disable, atribusi username benar), `TestAutoApproveGetReturnsLastChanged` (field `last_changed_*` muncul setelah toggle, dihilangkan bila belum ada jejak), `TestAutoApproveUnauthorizedWritesNoAuditLog` (403 → 0 baris), `TestExamAuditLogsFullHistory` (history lengkap terbaru-di-atas dengan 2 aktor berbeda), `TestExamAuditLogsEmptyTrail` (tanpa jejak → array kosong, bukan error), `TestExamAuditLogsUnauthorized` (pengawas tak bertugas → 403), `TestApprovalDecisionWritesAuditLog` (Izinkan lalu Tolak → 2 baris berurutan + snapshot MAC/siswa), `TestApprovalDecisionUnknownDeviceWritesNoAuditLog` (perangkat tak dikenal → 0 baris), `TestApprovalDecisionUnauthorizedWritesNoAuditLog` (403 → 0 baris), + guard markup UI (`templates_autoapprove_test.go`) dan guard rute (`routes_pengawas_autoapprove_test.go`) — semuanya di `webui/internal/handlers/admin/` & `webui/cmd/server/`.

### Pembersihan Orphan `exam_approvals` Saat Ujian Dihapus

Baris `exam_approvals` adalah data anak dari ujian (gerbang izin per perangkat) — menghapus ujian harus menghapus baris persetujuannya, apa pun statusnya (`pending`/`approved`/`rejected` — keputusan itu mati bersama ujiannya). Tiga jalur penghapusan kini membersihkannya secara **eksplisit** (di samping `ON DELETE CASCADE` di schema, sebagai defense-in-depth untuk database hasil migrasi pra-FK): `DeleteExam` (transaksi), `BulkDeleteExams` (`DELETE ... WHERE exam_id = ANY($1)` + `exam_pengawas`), dan `DeleteUser` (untuk semua ujian milik user yang dihapus, termasuk cascade operator). Tes: `TestDeleteExamRemovesApprovals`, `TestBulkDeleteExamsRemovesApprovals`, `TestDeleteUserRemovesExamApprovals` (`webui/internal/models/exam_delete_test.go`).

### Job Pembersih Baris Approval Basi (Pending/Approved)

Antrean `exam_approvals` kini dibersihkan otomatis oleh **`StartApprovalCleanupJob`** (`webui/internal/handlers/admin/approval_cleanup_job.go`, dijalankan dari `main.go` bersama job expiry; satu pass saat start lalu setiap interval terkonfigurasi). Tujuannya: membatasi antrean yang bisa dibanjiri spam, dan membebaskan slot cap perangkat (`max_approvals_per_exam` menghitung `status = 'approved'`) yang tersisa setelah ujian selesai — sehingga ujian yang dipakai ulang mulai dari papan bersih.

**Interval & TTL dapat dikonfigurasi SuperAdmin dari panel SaaS Settings** (Users → SaaS & SMTP Email Settings → bagian *Pembersihan Otomatis Antrean Persetujuan*; berlaku tanpa restart server — job membaca ulang nilainya setiap siklus):

| Setting (key `saas_settings`) | Default | Arti |
|---|---|---|
| `approval_cleanup_interval_minutes` | `15` | Seberapa sering pass pembersihan berjalan (minimum 1 menit; 0/negatif di-clamp) |
| `approval_cleanup_ended_grace_hours` | `1` | Toleransi setelah `end_time` ujian lewat sebelum barisnya dibersihkan (`0` = segera) |
| `approval_cleanup_inactive_ttl_hours` | `24` | Umur baris pending/approved pada ujian nonaktif sebelum dihapus (`0` = hapus semua) |

Field disimpan sebagai pointer (payload UI lama tanpa field tidak mereset nilai terkonfigurasi), dan `0` untuk grace/TTL dipertahankan sebagai pilihan disengaja — hanya interval yang di-clamp ke minimum 1.

`models.PurgeStaleExamApprovals` (`webui/internal/models/approval_cleanup.go`) menerapkan **dua aturan konservatif** — keduanya **tidak pernah** menyentuh baris `rejected` (keputusan eksplisit pengawas) dan **tidak pernah** menyentuh `submissions`/access logs (rekam siswa yang tahan lama di balik halaman monitoring & hasil):

| Aturan | Dihapus | Dijaga |
|---|---|---|
| **Ujian sudah berakhir** — `end_time` lewat > 1 jam (grace `approvalEndedGrace`; setelah jadwal berakhir tidak ada perangkat yang bisa join/poll/submit lagi) | `pending` (permintaan mati yang tak akan diputus) + `approved` (disetujui tapi tak pernah mengumpulkan — pegang slot cap) | Semua baris pada ujian yang masih **live** (aktif + dimulai + `end_time` belum lewat) — perangkat yang menunggu/berjalan tidak boleh terdampar |
| **Ujian nonaktif** (`status = 'inactive'`, dihentikan/di-tombstone) dan baris berusia > 24 jam (TTL `approvalInactiveTTL` — memberi guru waktu sehari untuk restart) | `pending` + `approved` tua | Baris yang lebih muda dari TTL (guru masih mungkin restart) + `rejected` segala usia |

Log job mencatat jumlah yang dibersihkan per kategori (`approval-cleanup job: purged N ...`). Tes: `TestApprovalCleanupEndedExam`, `TestApprovalCleanupKeepsLiveExamRows`, `TestApprovalCleanupInactiveExamTTL`, `TestApprovalCleanupPreservesSubmissions` (`webui/internal/handlers/admin/approval_cleanup_test.go`).

---

## API Endpoints

### Public API (Token-based)
| Method | Endpoint | Keterangan |
|--------|----------|------------|
| GET | `/api/health` | Health check |
| GET | `/api/time` | Waktu server (UTC) |
| GET | `/api/exams` | Daftar ujian aktif sekolah (`?instansi=<kode>` wajib; tanpa kode → list kosong) |
| POST | `/api/exams/request-approval` | Minta persetujuan perangkat siswa (tanpa auth) |
| GET | `/api/exams/token/:token` | Ambil data ujian berdasarkan token |
| GET | `/api/exams/:exam_id/pdf` | Redirect ke soal PDF (signed URL R2) |
| POST | `/api/exams/:exam_id/submit` | Submit jawaban ujian (async via Redis → `200 status:"queued"` + `job_id`, atau sync `200` + `score`) |
| GET | `/api/exams/:exam_id/result` | Poll hasil submit async — `?job_id=...` atau `&mac_address=...&identity_data=...` → `done`/`pending`/`failed` |
| POST | `/api/exams/:exam_id/access-log` | Log kehadiran siswa (login/logout/heartbeat) |
| POST | `/api/exams/:exam_id/complete` | Tandai ujian selesai di perangkat (hapus heartbeat) |

### Admin API (Session-based + CSRF)
| Method | Endpoint | Akses | Keterangan |
|--------|----------|-------|------------|
| POST | `/admin/login` | Publik (pre-auth) | Login admin |
| GET | `/admin/api/dashboard` | Semua login | Data dashboard |
| CRUD | `/admin/api/exams` | Semua login | Kelola ujian (scoped per pemilik/instansi) |
| CRUD | `/admin/api/users` | SuperAdmin & Operator | Kelola pengguna |
| GET | `/admin/api/submissions` | Semua login | Data submissions (scoped per kepemilikan) |
| POST | `/admin/api/vouchers/redeem` | Semua login (billing-exempt) | Klaim kode voucher — **403 untuk akun sub** (dibuat operator) |
| POST | `/admin/api/vouchers/activate` | Semua login (billing-exempt) | Aktivasi paket voucher yang sudah diklaim — **403 untuk akun sub** |
| GET | `/admin/api/vouchers/mine` | Semua login (billing-exempt) | Daftar paket yang sudah diklaim (halaman Paket & Voucher — daftarnya disembunyikan untuk akun sub, bukan via 403) |
| POST | `/admin/api/vouchers` | SuperAdmin | Buat kode voucher single |
| POST | `/admin/api/vouchers/batch` | SuperAdmin | Buat kode voucher batch |
| POST | `/admin/api/vouchers/:id/toggle` | SuperAdmin | Aktifkan/nonaktifkan voucher |
| POST | `/admin/api/vouchers/:id/delete` | SuperAdmin | Hapus voucher |
| GET | `/admin/api/vouchers` | SuperAdmin | Daftar & kelola voucher (halaman Kelola Voucher) |
| GET | `/admin/api/vouchers/:id/redemptions` | SuperAdmin | Riwayat pemakaian sebuah voucher |

> **Akses endpoint voucher:** `redeem` & `activate` menolak **HTTP 403** untuk akun sub (`operator_created = true`); `mine` tetap bisa dibaca akun sub, tetapi halaman Paket & Voucher **menyembunyikan daftar paketnya** di UI (bukan via 403). Ketiga endpoint bersifat **billing-exempt** — tetap bisa dipanggil oleh akun yang masa aktifnya habis (semua endpoint admin lain di-gate `FeatureLockRequired`; hanya baris billing-exempt ini yang bisa diakses akun terkunci). Endpoint manajemen voucher (buat/batch/toggle/delete/daftar/riwayat) **SuperAdmin-only**. **Tidak ada versi publik** dari seluruh endpoint voucher (di API publik `/api/*` tidak ada satupun) — dikunci oleh test `TestNoPublicVoucherRoutes`.

### Public Pages
| Endpoint | Keterangan |
|----------|------------|
| `/` | Halaman utama |
| `/download` | Download aplikasi |
| `/hasil` | Hasil ujian (public) |
| `/register` | Registrasi akun guru |

---

## License

ISC
