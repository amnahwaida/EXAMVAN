# 📄 EXAMVAN — Sistem Ujian Digital Berbasis PDF (LAN-Optimized & Secure)

> Platform distribusi & pelaksanaan ujian digital aman untuk infrastruktur jaringan lokal (LAN/Intranet) sekolah dan kampus dengan perlindungan anti-cheat berlapis di sisi Android & iOS.

---

## 🎯 Tujuan & Manfaat Project
EXAMVAN diciptakan khusus untuk memenuhi kebutuhan instansi pendidikan dalam menyelenggarakan ujian secara mandiri tanpa ketergantungan pada koneksi internet publik.

- **Kemandirian Jaringan:** Server berjalan 100% secara offline di LAN (Local Area Network) sekolah, mengurangi biaya bandwidth internet dan mencegah kegagalan koneksi massal.
- **Keamanan Tingkat Tinggi (Anti-Cheat):** Mengamankan berkas soal PDF dari penyebaran liar dan membatasi gerak-gerik siswa agar tidak dapat mencari jawaban di aplikasi lain.
- **Kemudahan Pengelolaan:** Memungkinkan guru untuk mengelola soal mereka sendiri secara terpisah, sementara Administrator memegang kontrol pengawasan penuh.

---

## 🚀 Fitur Utama

### 1. Panel Admin & Manajemen Guru (Multi-User)
* **Role Management:** Mendukung akun Administrator dan Guru.
* **Hak Akses Eksklusif:** Akun guru hanya dapat melihat, membuat, mengubah, dan menghapus ujian yang dibuatnya sendiri. Administrator memiliki otorisasi penuh untuk mengawasi seluruh ujian dari semua guru.
* **Ubah Password Mandiri:** Setiap pengguna dapat memperbarui kata sandinya kapan saja melalui UI modal yang aman.

### 2. Lembar Jawaban Digital & Koreksi Otomatis
* **Mendukung 4 Tipe Soal:**
  1. *Pilihan Ganda Tunggal (Single Choice)*
  2. *Pilihan Ganda Kompleks (Multiple Choice)*
  3. *Benar / Salah (True/False)*
  4. *Menjodohkan (Matching)*
* **Pengaturan Bobot & Penilaian Parsial:** Bobot nilai per soal dapat disesuaikan. Pilihan ganda kompleks mendukung opsi **Penilaian Parsial (Partial Scoring)** yang dinamis.
* **Rekalkulasi Nilai Otomatis:** Apabila guru mengubah bobot soal atau mengaktifkan/menonaktifkan opsi penilaian parsial *setelah* ujian disubmit oleh siswa, sistem secara otomatis menghitung ulang (*recalculate*) nilai siswa secara instan tanpa perlu submit ulang.

### 3. Keamanan Klien Seluler (Android & iOS)
* **Lock Task Mode (Screen Pinning):** Mengunci layar perangkat agar siswa tidak dapat menekan tombol Home, Recent Apps, atau membuka panel notifikasi.
* **Anti-Screenshot & Recording:** Layar aplikasi otomatis menjadi hitam (*black screen*) jika siswa mencoba menangkap layar (*capture*) atau merekam layar (menggunakan `FLAG_SECURE` pada Android dan `SecureView UITextField` pada iOS).
* **Anti-Copy Text:** PDF dirender sebagai gambar raster dinamis tanpa lapisan teks, sehingga teks soal tidak dapat disalin.
* **Clipboard Cleanser:** Clipboard/papan klip otomatis dikosongkan saat memasuki ruang ujian untuk mencegah metode *copy-paste* jawaban.
* **Anti-Switching App (Auto-Submit):** Jika siswa berhasil meminimalkan aplikasi, menekan tombol keluar, atau membuka aplikasi lain secara paksa, sistem secara otomatis mengumpulkan lembar jawaban saat itu juga (*auto-submit*) dan mengeluarkan siswa dari ruang ujian.

---

## 📥 Download Aplikasi Siswa

Untuk memulai ujian pada perangkat siswa, silakan unduh aplikasinya melalui tautan berikut:

### 🤖 Perangkat Android
* **[Download EXAMVAN Android APK (v1.2.0)](./app-debug.apk)** *(Gunakan tautan ini untuk mengunduh berkas APK secara langsung ke penyimpanan lokal server Anda untuk dibagikan kepada siswa).*

### 🍎 Perangkat iOS
* Kode sumber Swift native siap pakai tersedia di folder `/ios/ExamVan`. Anda dapat langsung membukanya menggunakan Xcode di macOS untuk melakukan build dan install ke perangkat iPad/iPhone siswa tanpa memerlukan dependensi pihak ketiga (*zero external dependencies*).

---

## 🛠️ Panduan Build Aplikasi Klien (Siswa)

Berikut adalah panduan detail mengenai cara melakukan kompilasi (*build*) aplikasi Android dan iOS dari kode sumber yang tersedia:

### A. Kompilasi Klien Android (.APK)
Kompilasi dapat dilakukan di sistem operasi **Windows, macOS, maupun Linux**.

#### 1. Persyaratan Sistem (*Requirements*):
- **JDK 17 (Java Development Kit):** Pastikan variabel lingkungan `JAVA_HOME` mengarah ke JDK 17.
- **Android SDK:** Terpasang versi SDK 34 (Android 14) untuk target kompilasi.
- **Android Gradle Plugin (AGP):** Versi 8.x ke atas.

#### 2. Cara Build dengan Command Line (CLI):
1. Buka terminal/command prompt di direktori `./android`.
2. Jalankan perintah kompilasi berikut:
   ```bash
   ./gradlew assembleDebug
   ```
3. Berkas APK hasil kompilasi akan tersimpan di:
   `android/app/build/outputs/apk/debug/app-debug.apk`

#### 3. Cara Build dengan Android Studio (GUI):
1. Buka software **Android Studio**.
2. Pilih **Open an Existing Project** dan arahkan ke folder `./android`.
3. Tunggu proses singkronisasi Gradle selesai.
4. Klik menu **Build > Build Bundle(s) / APK(s) > Build APK(s)**.
5. Setelah selesai, klik **Locate** di sudut kanan bawah untuk mengambil berkas `.apk`.

---

### B. Kompilasi Klien iOS (.IPA / Aplikasi Perangkat)
Kompilasi **wajib menggunakan komputer macOS (MacBook/iMac)** karena membutuhkan Xcode compiler.

#### 1. Persyaratan Sistem (*Requirements*):
- **Perangkat Keras:** Komputer Mac (Intel / Apple Silicon M-Series).
- **Sistem Operasi:** macOS Big Sur (11.0) atau versi di atasnya.
- **Xcode IDE:** Versi 13.0 atau lebih baru (mendukung Swift 5.0+).
- **Akun Apple Developer:** Akun gratis (*Free Apple ID*) cukup untuk penginstalan langsung ke perangkat pengujian lokal via kabel (tahan hingga 7 hari). Akun berbayar (*Paid Developer Program*) diperlukan untuk distribusi TestFlight atau App Store.

#### 2. Cara Build & Install ke Device menggunakan Xcode:
1. Pindahkan atau salin folder `./ios/ExamVan` ke komputer Mac Anda.
2. Klik ganda berkas **`ExamVan.xcodeproj`** untuk membukanya di Xcode.
3. Hubungkan perangkat iPhone atau iPad siswa ke komputer Mac menggunakan kabel USB.
4. Di bilah menu atas Xcode, pilih target perangkat fisik Anda (misalnya: *My iPad*).
5. Masuk ke tab **Signing & Capabilities** pada setelan proyek:
   - Centang **Automatically manage signing**.
   - Pilih *Team* Anda (masukkan Apple ID Anda jika belum terdaftar).
   - Ubah *Bundle Identifier* jika terjadi bentrok keunikan ID (misalnya: `com.examvan.sekolahanda`).
6. Tekan tombol **Run (ikon Segitiga / CMD+R)** untuk mengompilasi dan memasang aplikasi langsung ke perangkat siswa.
7. *Catatan untuk siswa:* Jika muncul peringatan *"Untrusted Developer"* pada perangkat iOS pertama kali, buka **Settings > General > VPN & Device Management**, lalu pilih profil Apple ID Anda dan klik **Trust**.

---

## 📡 API Endpoints

Semua komunikasi data antara aplikasi siswa dan server web dikirimkan melalui JSON API berikut:

| Endpoint | Method | Parameter / Payload | Fungsi |
| :--- | :---: | :--- | :--- |
| `/api/health` | GET | - | Memverifikasi apakah server menyala dan merespon dalam LAN. Mengembalikan `server_time_utc`. |
| `/api/time` | GET | - | Mengembalikan waktu UTC server (`utc`, `unix`, `timezone`) untuk sinkronisasi jam perangkat siswa. |
| `/api/exams` | GET | - | Mengambil daftar seluruh ujian yang sedang aktif. |
| `/api/exams/token/<token>` | GET | `token` (6 Karakter) | Mengambil konfigurasi soal ujian spesifik berdasarkan token unik. |
| `/api/exams/<id>/pdf` | GET | `id` (ID Ujian) | Mengunduh file PDF soal ujian ke penyimpanan lokal aplikasi siswa. |
| `/api/exams/<id>/submit` | POST | JSON Payload Siswa & Jawaban | Mengirimkan lembar jawaban siswa ke server untuk dinilai. |

---

## ⚙️ Panduan Deployment Server

Pilih salah satu metode deployment di bawah ini untuk dijalankan di PC server sekolah/kampus Anda.

### A. Deployment Dengan Docker (Sangat Direkomendasikan)
Metode ini paling mudah dan aman karena semua dependensi Python sudah terisolasi di dalam container.

1. **Prasyarat:** Pastikan Docker dan Docker Compose telah terpasang di komputer server.
2. **Konfigurasi Lingkungan (Online/Offline Mode):**
   Salin file `.env.example` menjadi `.env` jika belum ada:
   ```bash
   cp .env.example .env
   ```
   Buka file `.env` dan atur variabel berikut sesuai kebutuhan:
   * **Mode Offline (LAN Only):** Biarkan variabel `TUNNEL_TOKEN` kosong. Layanan tunnel akan otomatis berjalan idle tanpa mengonsumsi resource.
   * **Mode Online (Internet Access via Cloudflare):** Masukkan token tunnel Anda dari Cloudflare Zero Trust pada variabel `TUNNEL_TOKEN`.
   * *Catatan:* Di dashboard Cloudflare Zero Trust, konfigurasi rute tunnel (Public Hostname) harus diarahkan ke target URL internal Docker: `http://examvan-server:5000`.

3. **Jalankan Layanan:**
   Buka terminal di direktori utama project (`EXAMVAN/`) lalu ketik:
   ```bash
   docker compose up -d --build
   ```
4. **Persistensi Data:**
   Database SQLite (`examvan.db`) dan seluruh file PDF ujian (`storage/`) akan otomatis disimpan secara persisten di folder `./server/` pada komputer host Anda.
5. **Log Aktivitas:**
   Untuk melihat log aktivitas server secara real-time:
   ```bash
   docker compose logs -f
   ```
   Untuk melihat log status koneksi Cloudflare Tunnel:
   ```bash
   docker compose logs -f cloudflare-tunnel
   ```

---

### B. Deployment Tanpa Docker
Jika Anda ingin menjalankannya langsung menggunakan Python lokal pada sistem operasi host.

1. **Instalasi Dependensi:**
   Masuk ke folder server dan pasang pustaka yang diperlukan:
   ```bash
   cd server
   pip install -r requirements.txt
   ```
2. **Inisialisasi Database:**
   Sistem akan secara otomatis membuat berkas database `examvan.db` dan membuat pengguna admin default saat pertama kali dijalankan.
3. **Jalankan Mode Produksi (Gunicorn):**
   Gunakan Gunicorn untuk menangani trafik multi-client yang stabil:
   ```bash
   gunicorn -w 4 -b 0.0.0.0:5000 app:app
   ```
4. **Jalankan Mode Development (Opsional):**
   Jika ingin melakukan debugging secara lokal:
   ```bash
   python app.py
   ```

---

## 🔑 Informasi Akses Default Admin Panel
Buka browser Anda dan akses halaman admin di: **`http://<IP_SERVER_SEKOLAH>:5000/admin/login`**

* **Username:** `admin`
* **Password:** `examvan2026`

> [!IMPORTANT]
> Demi keamanan, segera ubah password akun administrator utama Anda sesaat setelah berhasil masuk ke halaman dashboard untuk pertama kali.

---

## 🕐 Sinkronisasi Waktu (Timezone)

Semua waktu di dalam sistem EXAMVAN disimpan dan diproses dalam **UTC+0** untuk menjamin konsistensi di seluruh perangkat, terlepas dari zona waktu lokal masing-masing.

### Arsitektur Sinkronisasi Waktu

```
┌──────────────────────────────────────────────────────────────────┐
│ SERVER (Docker Container, TZ=UTC)                                │
│ ┌──────────────────────────────────────────────────────────────┐ │
│ │ SQLite DB: CURRENT_TIMESTAMP → UTC                          │ │
│ │ Python:    datetime.now(timezone.utc)                        │ │
│ │ API:       Semua response timestamp dalam format ISO 8601 Z │ │
│ └──────────────────────────────────────────────────────────────┘ │
│                              ↓ JSON (UTC)                        │
├──────────────────────────────────────────────────────────────────┤
│ ADMIN PANEL (Browser)                                            │
│ → JavaScript localizeDates() mengkonversi UTC → timezone browser │
├──────────────────────────────────────────────────────────────────┤
│ ANDROID / iOS (Device Siswa)                                     │
│ → Menerima timestamp UTC, ditampilkan sesuai timezone device     │
│ → Endpoint /api/time tersedia untuk verifikasi jam server        │
└──────────────────────────────────────────────────────────────────┘
```

### Endpoint Sinkronisasi Waktu

Gunakan endpoint `/api/time` untuk mendapatkan waktu server yang otoritatif:
```bash
curl http://<IP_SERVER>:5000/api/time
```
Contoh response:
```json
{
  "utc": "2026-05-22T17:27:14Z",
  "unix": 1779470834,
  "timezone": "UTC"
}
```

> [!NOTE]
> Docker container berjalan dengan environment `TZ=UTC`. Semua field `created_at` dari database SQLite otomatis tersimpan dalam UTC. Konversi ke zona waktu lokal dilakukan sepenuhnya di sisi klien (browser admin / device siswa).
