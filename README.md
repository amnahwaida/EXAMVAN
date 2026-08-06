# EXAMVAN — Sistem Ujian Digital Berbasis PDF (Secure — LAN & Cloud)

> Platform distribusi & pelaksanaan ujian digital aman untuk infrastruktur jaringan lokal (LAN/Intranet) sekolah dan kampus dengan perlindungan anti-cheat berlapis di sisi Android.

---

## Tujuan & Manfaat Project

EXAMVAN diciptakan khusus untuk memenuhi kebutuhan instansi pendidikan dengan arsitektur *hybrid* yang sangat efisien untuk perangkat server minim resource (seperti STB).

- **Penyimpanan PDF Fleksibel (Cloudflare R2 opsional):** File soal (PDF) dapat di-offload ke Cloudflare R2 (Edge CDN), sehingga server lokal/STB tidak menyimpan file berat dan bandwidth egress terselamatkan — ideal untuk skala ribuan siswa secara simultan. R2 bersifat **opsional**: jika `R2_ACCESS_KEY_ID`, `R2_SECRET_ACCESS_KEY`, dan `R2_ENDPOINT` terisi lengkap, PDF di-upload ke R2 dan di-serve via signed URL; jika tidak, server otomatis menyimpan & menyajikan PDF dari penyimpanan lokal (volume `webui_storage`). Mode R2 memerlukan koneksi internet.
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
* **[Download EXAMVAN Android APK](./EXAMVAN.apk)** *(Siap langsung dipasang di HP siswa).*
* **Alternative Build:** Anda juga dapat menggunakan berkas **[examvan-debug.apk](./examvan-debug.apk)**, **[examvan-release.apk](./examvan-release.apk)**, **[examvan-student-debug.apk](./examvan-student-debug.apk)**, atau **[examvan-kiosk-debug.apk](./examvan-kiosk-debug.apk)** yang terletak di folder root.
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
| `R2_ACCESS_KEY_ID` | Access Key ID Cloudflare R2 | Tidak |
| `R2_SECRET_ACCESS_KEY` | Secret Access Key Cloudflare R2 | Tidak |
| `R2_BUCKET` | Nama bucket R2 (contoh: examvan-pdfs) | Tidak |
| `R2_ENDPOINT` | Endpoint R2 Cloudflare Anda | Tidak |
| `EXAMVAN_CORS_ORIGINS` | Origin yang diizinkan (kosongkan untuk allow all) | Tidak |

> **Catatan R2:** Keempat variabel `R2_*` harus diisi **sekaligus** untuk mengaktifkan mode R2 (PDF via Cloudflare edge). Jika semuanya dikosongkan, PDF disimpan di penyimpanan lokal server.

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

#### 4. Persistensi Data
- **Database:** Volume Docker `postgres_data` untuk PostgreSQL.
- **File PDF:** Volume Docker `webui_storage` untuk file ujian (saat mode lokal). Jika R2 aktif, PDF tersimpan di bucket Cloudflare R2.

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

## Informasi Akses Default Admin Panel

Akses halaman admin di: **`http://<IP_SERVER>:80/admin/login`** (Docker) atau **`http://<IP_SERVER>:5000/admin/login`** (tanpa Docker)

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
curl http://<IP_SERVER>:5000/api/time
```

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
├── *.apk                       # Pre-built Android APKs
├── CLAUDE.md                   # Project instructions
├── README.md                   # Dokumentasi ini
└── package.json                # Tailwind CSS build scripts
```

---

## API Endpoints

### Public API (Token-based)
| Method | Endpoint | Keterangan |
|--------|----------|------------|
| GET | `/api/health` | Health check |
| GET | `/api/time` | Waktu server (UTC) |
| GET | `/api/exams/token/<token>` | Ambil data ujian berdasarkan token |
| POST | `/api/exams/token/<token>/submit` | Submit jawaban ujian |

### Admin API (Session-based + CSRF)
| Method | Endpoint | Keterangan |
|--------|----------|------------|
| POST | `/admin/login` | Login admin |
| GET | `/admin/api/dashboard` | Data dashboard |
| CRUD | `/admin/api/exams` | Kelola ujian |
| CRUD | `/admin/api/users` | Kelola pengguna |
| GET | `/admin/api/submissions` | Data submissions |

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
