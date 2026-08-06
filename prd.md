# 📄 PRD: EXAMVAN (MVP)

> **NOTE:** This PRD was written for v1.1.0 (MVP). The current version is 2.2.0
> with many additional features. See README.md for current documentation.
>
> ⚠️ **PERUBAHAN ARAH (Agustus 2026):** Dukungan LAN/Intranet telah **DIHAPUS**.
> Produk kini **cloud-only**: client terhubung via HTTPS/domain publik (Cloudflare Tunnel),
> cleartext HTTP & IP privat tidak lagi diizinkan. Bagian-bagian LAN di bawah ini
> hanya dokumentasi historis MVP dan **tidak berlaku lagi**.

**Versi:** 1.1.0 (Revisi Intranet & Slow-Network)  
**Status:** READY FOR DEVELOPMENT  
**Tanggal:** 22 Mei 2026  
**Platform:** Android Native (Kotlin) + REST API + Web Admin  
**Sifat:** MVP (Minimum Viable Product)

---

## 1. 🎯 Ringkasan Eksekutif & Visi Produk
**EXAMVAN** adalah aplikasi Android ringan untuk distribusi & pelaksanaan ujian digital berbasis PDF. Aplikasi ini dirancang untuk akses **cloud** (HTTPS via domain publik), mendukung koneksi internet lambat, dan mencegah penyalahgunaan konten (screenshot & copy-paste). Pengelolaan soal terpusat melalui panel admin web, sehingga guru/admin cukup upload PDF → siswa langsung melihat daftar ujian.

**Visi (sekarang):** Memberikan akses ujian yang stabil dan aman melalui cloud, dengan footprint teknis seminimal mungkin dan pengalaman siswa yang tetap lancar meski bandwidth terbatas.

---

## 2. 👥 Target Pengguna & Persona
| Persona | Kebutuhan Utama | Interaksi dengan Sistem |
|---------|----------------|------------------------|
| **Siswa** | Akses soal cepat, navigasi mudah, tidak hang di jaringan lambat, tidak perlu login | Setting URL server (1x) → Pilih ujian → Tunggu loading → Kerjakan soal |
| **Admin/Guru** | Upload PDF cepat, kelola status ujian, pantau ketersediaan | Login web → Upload → Set status aktif/nonaktif |
| **IT/Network Admin** | Deploy di LAN, kontrol traffic, maintenance ringan | Hosting server lokal → Atur IP/DHCP → Distribusi APK via QR/Flashdisk |

---

## 3. 📊 Tujuan MVP & Metrik Keberhasilan
| Tujuan | Metrik KPI (MVP) |
|--------|------------------|
| Kompatibel Intranet | Berjalan normal di `http://192.168.x.x` atau `http://server.local` tanpa blokir Android |
| Support Slow Internet | Loading PDF ≤ 15 detik di 3G/1 Mbps, progress jelas, retry tersedia, tidak force-close |
| Anti Screenshot/Copy | `FLAG_SECURE` aktif, teks tidak selectable, clipboard terblokir |
| Konfigurasi Persisten | Checkbox "Ingat URL Server" menyimpan preferensi terakhir di `SharedPreferences` |
| Ringan & Stabil | APK ≤ 2.5 MB, Memory ≤ 90 MB, Crash rate < 1%, PDF load success ≥ 95% |

---

## 4. ⚙️ Lingkup Fungsional (Functional Requirements)

### 4.1 Aplikasi Android
| Fitur | Deskripsi Teknis |
|-------|------------------|
| **Layar Konfigurasi Server** | Input URL base server, validasi format (`http://`/`https://`, IP lokal, domain). `CheckBox` **"Simpan URL ini untuk selanjutnya"** (default: ✅). Jika dicentang → simpan ke `SharedPreferences`. Jika tidak → hanya gunakan untuk sesi ini. |
| **Layar Daftar Ujian** | `GET /api/exams`, parsing JSON, `RecyclerView` dengan indikator status & waktu update. Auto-retry jika network error. |
| **Layar Viewer PDF** | Download ke temp file → progress bar (0-100%) → rename ke `cacheDir` → render via `PdfRenderer` ke `Bitmap`. Navigasi Prev/Next, indikator halaman. |
| **Manajemen Cache & Slow Network** | Timeout disesuaikan: `connect 15s`, `read 60s`. File disimpan sementara selama download, hanya dipindahkan ke cache jika 100% berhasil. Jika gagal, file temp dihapus otomatis. |
| **Mode Aman (Secure Mode)** | `FLAG_SECURE` aktif sejak `onCreate`. Clipboard dibersihkan. Tidak ada menu share/copy/print di toolbar. |

### 4.2 Backend & REST API
| Endpoint | Method | Fungsi | Response |
|----------|--------|--------|----------|
| `/api/exams` | `GET` | Ambil daftar ujian aktif | `{"success":true,"data":[{"id":1,"name":"UTS Matematika","status":"active","size_mb":3.2}]}` |
| `/api/exams/{id}/pdf` | `GET` | Stream file PDF ujian | `Content-Type: application/pdf`, `Cache-Control: no-store`, stream byte |
| `/api/health` | `GET` | Cek ketersediaan server | `{"status":"ok","version":"2.2","certificate_fingerprint":"sha256/..."}` |

### 4.3 Panel Admin Web (MVP)
| Fitur | Deskripsi |
|-------|-----------|
| **Autentikasi** | Login sederhana (session-based) |
| **Upload Ujian** | Form: Nama Ujian, File PDF, Status. Validasi MIME `application/pdf`, **max 5 MB** (rekomendasi untuk jaringan lambat). Kompresi otomatis opsional. |
| **Manajemen Daftar** | Tabel daftar ujian, toggle status aktif/nonaktif, hapus file + DB |
| **Info Jaringan** | Tampilkan IP server lokal, status HTTP/HTTPS, log akses terakhir |

---

## 5. 🛡️ Lingkup Non-Fungsional (Non-Functional Requirements)
| Kategori | Spesifikasi |
|----------|-------------|
| **Performa** | APK tanpa library berat. `PdfRenderer` native. Timeout network: connect 15s, read 60s. Progress UI wajib saat download. |
| **Kompatibilitas Jaringan** | Mendukung `http://` (cleartext) untuk LAN. Tidak memblokir IP privat (`192.168.x.x`, `10.x.x.x`, `172.16.x.x`, `127.0.0.1`). |
| **Keamanan** | `FLAG_SECURE` di semua Activity. Render ke `Bitmap` memutus layer teks. Cache private. Tidak ada credential hardcode. |
| **Ketersediaan** | Stateless API. File PDF di luar web root. Fallback error UI jelas. Retry mechanism built-in. |
| **Pemeliharaan** | Struktur kode modular. Versioning API. Dokumentasi endpoint terbuka. Backup storage harian. |

---

## 6. 🏗️ Arsitektur Sistem & Alur Data (Fokus LAN)
```
[Android App]
   │
   ├─📱 Screen 1: Server Config → Validasi URL → [☑ Simpan] → SharedPreferences / Memory
   │
   ├─📱 Screen 2: Exam List → GET /api/exams → Parse JSON → RecyclerView
   │
   └─📱 Screen 3: Exam Viewer → GET /api/exams/{id}/pdf → Download Progress → Cache → PdfRenderer → Bitmap
         ↑
[REST API (Local Server)] ←→ [Database] ←→ [Admin Panel Web]
         ↓
   [Storage: /var/www/exam/storage/*.pdf]
   [Network: LAN / Intranet (192.168.x.x / 10.x.x.x)]
```
**Catatan Arsitektur LAN:**
- Server tidak perlu domain publik. Cukup IP lokal atau hostname `.local` (mDNS).
- Android 9+ secara default memblokir HTTP. App secara eksplisit mengizinkan cleartext via `AndroidManifest.xml` & `network_security_config.xml`.
- PDF tidak di-embed. Diambil on-demand. Jika jaringan putus saat download, user bisa retry tanpa kehilangan state sebelumnya.

---

## 7. 📜 Spesifikasi Kontrak API (MVP)

### 7.1 Format Request/Response
- **Content-Type:** `application/json` (kecuali PDF endpoint)
- **Auth:** Tidak diperlukan di MVP (akses terbuka via jaringan sekolah/intranet).
- **Error Handling:**
  ```json
  {"success": false, "error": "exam_not_found", "message": "Ujian tidak tersedia atau sudah berakhir"}
  ```

### 7.2 Header Respons PDF
```http
HTTP/1.1 200 OK
Content-Type: application/pdf
Content-Disposition: inline; filename="exam.pdf"
Cache-Control: no-store, no-cache, must-revalidate
X-Content-Type-Options: nosniff
Content-Length: <bytes>
Accept-Ranges: bytes
```
> `Accept-Ranges: bytes` memungkinkan client melakukan resume jika terputus (diimplementasikan via logika download chunked di v2).

---

## 8. 🖼️ Alur UI/UX & Deskripsi Layar

| Layar | Komponen Utama | Interaksi |
|-------|----------------|-----------|
| **ServerConfig** | EditText URL, CheckBox "Ingat URL Server Ini", Button Simpan & Lanjut | Validasi realtime. Jika checkbox ✅ → simpan ke `SharedPreferences`. Jika ❌ → simpan di memory hanya untuk sesi ini. |
| **ExamList** | RecyclerView, EmptyState, LastSyncTime, PullRefresh | Tap item → Intent ke Viewer. Jika network error → tombol "Coba Lagi". |
| **ExamViewer** | ProgressBar (0-100%), ImageView (PDF), Button Prev/Next, Page Counter, Cancel/Retry | Progress jelas saat download. Jika lambat > 30s → tampilkan estimasi & opsi cancel. Tidak ada menu share/copy. |

**Prinsip Desain:**
- Minimalis, kontras tinggi, ukuran teks ≥ 14sp.
- Indikator loading & progress wajib ada.
- Error state dengan tombol retry & pesan jaringan.
- Tidak ada animasi berat.

---

## 9. 🔒 Strategi Keamanan & Anti-Curang
| Ancaman | Mitigasi Teknis | Batasan Realistis |
|---------|-----------------|-------------------|
| Screenshot/Screen Record | `FLAG_SECURE` di semua Activity | Tidak memblokir device rooted/custom ROM/camera eksternal |
| Copy-Paste Teks | `PdfRenderer` → render ke `Bitmap`. Tidak ada layer teks | Aman di 99% device stock |
| Ekstraksi PDF | Hanya di `cacheDir` (private). Dihapus saat app clear | Dapat di-backup via ADB jika device diunlock |
| Intercept LAN | HTTPS opsional. HTTP diizinkan untuk intranet | Admin jaringan bertanggung jawab atas isolasi VLAN/WiFi |
| Distribusi Ujian | Status `active` dikontrol server. App hanya render jika valid | Tidak ada lock device/proctoring di MVP |

---

## 10. ⚠️ Batasan & Manajemen Risiko
| Risiko | Dampak | Mitigasi |
|--------|--------|----------|
| Jaringan LAN Lambat/Penuh | PDF lama load, timeout | Timeout 60s, progress bar, max PDF 5MB, retry UI |
| Perangkat Rooted/Emulator | `FLAG_SECURE` bypass | Kebijakan manual, audit, Play Integrity v2 |
| URL Server Berubah | Siswa tidak bisa akses | Checkbox "Ingat URL" + tombol "Ganti Server" di menu |
| HTTP Cleartext Diblokir Android 9+ | App crash saat request | `android:usesCleartextTraffic="true"` + `network_security_config.xml` khusus IP privat |
| PDF > 10MB | OOM di device RAM 2GB | Validasi admin panel ≤ 5MB, kompresi server-side |

---

## 11. 📅 Rencana Pengembangan & Milestone
| Fase | Durasi | Deliverable |
|------|--------|-------------|
| **Phase 1: Core Android** | 5 hari | 3 Activity, ViewBinding, `FLAG_SECURE`, `PdfRenderer`, `OkHttp`, Config screen + checkbox |
| **Phase 2: Backend + Admin** | 4 hari | REST API (list + stream), Panel upload, DB schema, Static file serving, LAN config |
| **Phase 3: Integrasi & Test** | 3 hari | End-to-end testing, cache cleanup, slow-network simulation, error handling |
| **Phase 4: Deploy & Handover** | 2 hari | Panduan install intranet, dokumentasi API, script backup, APK signed release |

**Total Estimasi:** ~2 minggu kerja penuh

---

## 12. 🔧 Implementasi Teknis Khusus (3 Permintaan Anda)

### ✅ 1. Berjalan di Server 1 Jaringan (Intranet/LAN)
**AndroidManifest.xml**
```xml
<application
    android:usesCleartextTraffic="true"
    ... >
    <!-- ... -->
</application>
```
> [!WARNING]
> Snippet di bawah menggunakan tag `<domain>` yang tidak valid untuk IP range (mendukung hostname saja). Implementasi aktual ada di `android/app/src/main/res/xml/network_security_config.xml` yang sudah menggunakan tag `<ip-range>` yang benar (API 24+).

**res/xml/network_security_config.xml** (Wajib untuk Android 9+)
```xml
<network-security-config>
    <domain-config cleartextTrafficPermitted="true">
        <domain includeSubdomains="true">192.168.0.0/16</domain>
        <domain includeSubdomains="true">10.0.0.0/8</domain>
        <domain includeSubdomains="true">172.16.0.0/12</domain>
        <domain includeSubdomains="true">localhost</domain>
        <domain includeSubdomains="true">127.0.0.1</domain>
    </domain-config>
</network-security-config>
```
Deklarasikan di `AndroidManifest.xml`: `android:networkSecurityConfig="@xml/network_security_config"`

### ✅ 2. Checkbox "Simpan Link API Server Terakhir"
**Layout (`activity_server_config.xml`)**
```xml
<CheckBox
    android:id="@+id/cbRememberUrl"
    android:text="Ingat URL Server ini untuk selanjutnya"
    android:checked="true"
    android:layout_marginTop="8dp" />
```
**Kotlin Logic**
```kotlin
val prefs = getSharedPreferences("app_config", MODE_PRIVATE)
val rememberUrl = prefs.getBoolean("remember_url", true) // default ✅

// Saat tombol Simpan diklik
if (binding.cbRememberUrl.isChecked) {
    prefs.edit()
        .putString("server_url", url)
        .putBoolean("remember_url", true)
        .apply()
} else {
    // Hanya simpan di memory (tidak persist)
    prefs.edit().putBoolean("remember_url", false).apply()
    // URL disimpan di Intent extras atau Singleton untuk sesi ini
}
```
Saat app dibuka kembali: cek `remember_url`. Jika `false`, selalu tampilkan layar config. Jika `true`, auto-load URL terakhir.

### ✅ 3. Siswa dengan Internet Lambat Tetap Bisa Mengakses
| Optimasi | Implementasi |
|----------|--------------|
| **Timeout Longgar** | `connectTimeout(15, TimeUnit.SECONDS)`, `readTimeout(60, TimeUnit.SECONDS)` |
| **Progress UI** | `ProgressBar` horizontal + `%` text. Update via `OkHttp` interceptor atau `CountingSink` |
| **Atomic Save** | Download ke `temp_exam_{id}.pdf`. Hanya rename ke `exam_{id}.pdf` jika 100% selesai. Jika gagal/timeout → hapus temp file |
| **Cache Reuse** | Jika file sudah di `cacheDir`, langsung load tanpa request ulang. Hemat bandwidth |
| **Rekomendasi Admin** | Validasi upload ≤ 5MB. Gunakan kompresi PDF (`ghostscript` atau `qpdf`) sebelum upload |
| **Retry Mechanism** | Tombol "Coba Lagi" muncul jika network error/timeout. Tidak reset state UI |

---

## 13. 🧪 Strategi Pengujian & QA
| Jenis Tes | Skenario Utama | Kriteria Lolos |
|-----------|----------------|----------------|
| **Intranet/LAN** | Server di `http://192.168.1.100`, app konek via WiFi sekolah | App fetch & load PDF tanpa error HTTP cleartext |
| **Slow Network** | Simulasi 3G/1 Mbps via `adb shell cmd network`, download PDF 4MB | Progress bar berjalan, tidak hang, retry tersedia, sukses dalam ≤ 60s |
| **Checkbox URL** | Uncheck → simpan → restart app → check if config screen muncul | Sesuai preferensi checkbox. Tidak save jika ❌ |
| **Keamanan** | Screenshot, long-press, clipboard, ADB pull | Gambar hitam/blur, cache tidak accessible |
| **Backend** | Concurrent request, upload >5MB, delete active exam | API return error jelas, storage konsisten, DB tidak corrupt |

---

## 14. 📖 Glosarium & Lampiran
| Istilah | Definisi |
|---------|----------|
| `FLAG_SECURE` | Flag Android yang memblokir screenshot & screen recording di level window |
| `PdfRenderer` | API native Android (API 21+) untuk render halaman PDF ke bitmap |
| `cacheDir` | Direktori private aplikasi yang dihapus otomatis saat storage penuh |
| `Cleartext` | Traffic HTTP tidak terenkripsi. Diperbolehkan untuk intranet tertutup |
| `Atomic Save` | Teknik menyimpan file sementara lalu rename hanya jika download sukses |

**Lampiran Teknis:**
- `AndroidManifest.xml` snippet siap pakai
- `network_security_config.xml` untuk IP privat
- Contoh `OkHttp` downloader dengan progress & atomic save
- Skema DB: `exams(id, name, file_path, size_bytes, status, created_at)`
- Panduan setup server LAN (Nginx + PHP/Python)

---
✅ **PRD v1.1.0 ini sudah mencakup:**  
🔹 Kompatibilitas penuh dengan jaringan lokal/intranet  
🔹 Checkbox eksplisit untuk menyimpan URL server terakhir  
🔹 Optimasi khusus untuk koneksi internet lambat (progress, timeout, atomic cache, retry)  

