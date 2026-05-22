# 📄 EXAMVAN — Sistem Ujian Digital Berbasis PDF

> Distribusi & pelaksanaan ujian digital untuk lingkungan jaringan lokal (LAN/Intranet)

## 🎯 Tentang

EXAMVAN adalah platform ujian digital ringan yang dirancang untuk sekolah dan kampus. Sistem ini mendistribusikan soal ujian dalam format PDF melalui jaringan lokal, dengan perlindungan anti-screenshot dan anti-copy.

## 🏗️ Arsitektur

| Komponen | Teknologi | Fungsi |
|----------|-----------|--------|
| **Backend API** | Python Flask + SQLite | REST API + file serving |
| **Admin Panel** | HTML/CSS/JS (dark theme) | Upload & kelola ujian |
| **Android App** | Kotlin + PdfRenderer + OkHttp | Viewer ujian untuk siswa |

## 🚀 Quick Start

### 1. Menjalankan Server

```bash
cd server
pip install -r requirements.txt
python3 app.py
```

Server akan berjalan di:
- **Local:** http://127.0.0.1:5000
- **LAN:** http://<IP-LAN>:5000

### 2. Login Admin Panel

Buka browser, akses `http://<IP-SERVER>:5000/admin/login`

| | |
|---|---|
| **Username** | `admin` |
| **Password** | `examvan2026` |

### 3. Deploy Android App

1. Buka folder `android/` di Android Studio
2. Build APK (`Build > Build Bundle(s)/APK(s) > Build APK(s)`)
3. Distribusi APK ke device siswa via QR Code atau flashdisk

## 📡 API Endpoints

| Endpoint | Method | Fungsi |
|----------|--------|--------|
| `/api/health` | GET | Health check server |
| `/api/exams` | GET | Daftar ujian aktif |
| `/api/exams/{id}/pdf` | GET | Stream file PDF |

## 🔒 Fitur Keamanan

- **FLAG_SECURE** — Memblokir screenshot & screen recording
- **PdfRenderer → Bitmap** — Tidak ada layer teks (anti-copy)
- **Clipboard clearing** — Clipboard dibersihkan saat masuk viewer
- **Cache private** — File PDF hanya di `cacheDir` aplikasi
- **No-store headers** — Cache-Control: no-store pada response PDF

## 📱 Fitur Aplikasi Android

1. **Konfigurasi Server** — Input URL, checkbox "Ingat URL"
2. **Daftar Ujian** — RecyclerView + pull-to-refresh
3. **Viewer PDF** — Download progress + navigasi halaman
4. **Mode Offline-Ready** — Cache reuse jika file sudah diunduh
5. **Slow Network Support** — Timeout 60s, progress bar, retry

## 🌐 Kompatibilitas Jaringan

- Mendukung HTTP cleartext untuk LAN (`192.168.x.x`, `10.x.x.x`)
- `network_security_config.xml` mengizinkan IP privat
- Tidak memerlukan domain publik atau SSL

## 📁 Struktur Project

```
EXAMVAN/
├── server/                     # Backend
│   ├── app.py                 # Flask application
│   ├── requirements.txt       # Python dependencies
│   ├── storage/               # PDF file storage
│   ├── templates/             # HTML templates
│   │   ├── login.html
│   │   └── dashboard.html
│   └── static/                # CSS & JS
│       ├── css/admin.css
│       └── js/admin.js
│
├── android/                    # Android App
│   ├── app/
│   │   ├── build.gradle.kts
│   │   ├── proguard-rules.pro
│   │   └── src/main/
│   │       ├── AndroidManifest.xml
│   │       ├── java/com/examvan/app/
│   │       │   ├── ServerConfigActivity.kt
│   │       │   ├── ExamListActivity.kt
│   │       │   ├── ExamViewerActivity.kt
│   │       │   ├── model/Exam.kt
│   │       │   ├── api/ApiClient.kt
│   │       │   └── adapter/ExamAdapter.kt
│   │       └── res/
│   │           ├── layout/ (4 layouts)
│   │           ├── values/ (colors, strings, themes)
│   │           └── xml/network_security_config.xml
│   ├── build.gradle.kts
│   ├── settings.gradle.kts
│   └── gradle.properties
│
└── prd.md                      # Product Requirements
```

## ⚙️ Production Deployment

Untuk deployment produksi di server LAN:

```bash
cd server
gunicorn -w 4 -b 0.0.0.0:5000 app:app
```

## 📋 Versi

- **PRD:** v1.1.0
- **Server:** v1.1.0
- **Android:** v1.1.0 (minSdk 21, targetSdk 34)
