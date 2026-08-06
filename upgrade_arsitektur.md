# Upgrade Arsitektur EXAMVAN — Prioritas #1: Cloudflare R2

> **Dokumen ini fokus 100% ke koneksi Cloudflare R2, karena ini adalah satu-satunya upgrade yang dampaknya paling besar: PDF pindah ke Cloudflare, server beban file hilang, bandwidth 100mbps cukup untuk 50.000 siswa.**

> **Status saat ini (Agustus 2026):** R2 kini bersifat **opsional** — server berjalan normal tanpa R2 dengan fallback penyimpanan lokal (PDF di-serve dari disk/volume `webui_storage`). Jika keempat variabel `R2_ACCESS_KEY_ID`, `R2_SECRET_ACCESS_KEY`, `R2_BUCKET`, dan `R2_ENDPOINT` di `.env` terisi lengkap, PDF otomatis di-upload ke R2 dan di-serve via signed URL. Panduan setup di bawah ini tetap berlaku sebagai langkah mengaktifkan mode R2.
>
> Upgrade lain (RAM, CPU, scaling) sifatnya opsional — nanti kalau sudah mentok.

---

## Daftar Isi

1. [Kenapa R2 Prioritas #1](#1-kenapa-r2-prioritas-1)
2. [Arsitektur Sebelum vs Sesudah](#2-arsitektur-sebelum-vs-sesudah)
3. [Prasyarat](#3-prasyarat)
4. [Step 1 — Setup Cloudflare R2 Bucket](#4-step-1--setup-cloudflare-r2-bucket)
5. [Step 2 — Buat API Token R2](#5-step-2--buat-api-token-r2)
6. [Step 3 — Tambah Package AWS SDK di Go](#6-step-3--tambah-package-aws-sdk-di-go)
7. [Step 4 — Tambah Konfigurasi R2 di Config Go](#7-step-4--tambah-konfigurasi-r2-di-config-go)
8. [Step 5 — Buat R2 Client Handler Baru](#8-step-5--buat-r2-client-handler-baru)
9. [Step 6 — Init R2 Client di main.go](#9-step-6--init-r2-client-di-maingo)
10. [Step 7 — Upload PDF ke R2 (Admin Upload)](#10-step-7--upload-pdf-ke-r2-admin-upload)
11. [Step 8 — Serve PDF via R2 Signed URL (Download)](#11-step-8--serve-pdf-via-r2-signed-url-download)
12. [Step 9 — Fallback: Nginx X-Accel (kalo R2 mati)](#12-step-9--fallback-nginx-x-accel-kalo-r2-mati)
13. [Step 10 — Testing R2](#13-step-10--testing-r2)
14. [Capacity Planner Lengkap](#14-capacity-planner-lengkap)

---

## 1. Kenapa R2 Prioritas #1

### Masalah tanpa R2

| Masalah | Dampak |
|---------|--------|
| **Bandwidth VPS dimakan PDF** | 1 siswa download 5MB = 5MB. 2.000 siswa = 10GB. Lemot. Mahal. |
| **CPU dipake baca file** | Go pake `c.File()` → blocking goroutine, CPU terbuang buat I/O. |
| **100mbps jadi bottleneck** | 100mbps = 12.5MB/detik. 1 siswa download = sisa bandwidth tinggal sedikit. |
| **Server down = PDF hilang** | Semua siswa gak bisa download soal. Ujian batal. |
| **Bandwidth AWS bayar** | 100GB free/bulan habits cepet kalau serve PDF. |

### Keuntungan R2

| Keuntungan | Detail |
|------------|--------|
| **Bandwidth gratis** | R2 egress GRATIS. Gak kena biaya transfer. |
| **Zero CPU server** | Server cuma balikin HTTP 302 redirect. Gak baca file. |
| **100mbps cukup** | File lewat Cloudflare edge, bukan server. |
| **Availability global** | Cloudflare ada di 300+ lokasi. Download cepet dari mana aja. |
| **Free tier cukup** | 10GB storage, jutaan request/bln — gratis. |

---

## 2. Arsitektur Sebelum vs Sesudah

### Sebelum — PDF dari server

```
Android ──► VPS (validasi token) ──► Go baca file ──► kirim PDF
                                    ↑
                               CPU + Bandwidth
                               habis buat file
```

### Sesudah — PDF dari Cloudflare R2

```
Android ──► VPS (validasi token) ──► HTTP 302 (redirect)
                                     │
                                     ▼
                Cloudflare R2 ──► download PDF langsung
                (zero CPU VPS, bandwidth gratis)
```

---

## 3. Prasyarat

| Prasyarat | Keterangan |
|-----------|------------|
| **Akun Cloudflare** | Gratis. Daftar di https://dash.cloudflare.com/sign-up |
| **Go project** | Udah punya `go.mod` di `webui/` |
| **Nginx** | Untuk fallback (kalo R2 mati) |

---

## 4. Step 1 — Setup Cloudflare R2 Bucket

1. Login ke [Cloudflare Dashboard](https://dash.cloudflare.com/)
2. Klik **R2** di menu kiri

![R2 menu](https://developers.cloudflare.com/r2/static/documentation/logos/r2-logo.png)

3. Klik **Create Bucket**

![Create bucket](https://i.imgur.com/placeholder.png)

4. Isi nama bucket: `examvan-pdfs` (atau nama lain yang kamu ingat)

```
Bucket name: examvan-pdfs
```

5. Location: **Automatic** (biarkan default)
6. Klik **Create**

> **Catatan:** Nama bucket harus **unik secara global** karena semua bucket Cloudflare R2 pakai namespace yang sama. Kalau `examvan-pdfs` udah dipake, tambah angka: `examvan-pdfs-1`.

---

## 5. Step 2 — Buat API Token R2

1. Di halaman R2 Overview, klik **Manage R2 API Tokens**

![API tokens](https://i.imgur.com/placeholder.png)

2. Klik **Create API Token**

3. Pilih permission:

```
Permission: Admin Read & Write
```

4. Isi nama token: `examvan-server`
5. Klik **Create**

6. **SETELAH BUAT, LANGSUNG SALIN 3 NILAI INI:**

```
Access Key ID:     examvan-server    (atau string panjang)
Secret Access Key: abc123...         (string acak panjang)
Endpoint:          https://abc123.r2.cloudflarestorage.com
```

> ⚠️ **PENTING:** `Secret Access Key` hanya muncul SEKALI. Kalau tidak disalin, harus bikin token baru.

7. Simpan di file `.env` di root proyek:

```bash
# .env — simpan di /home/vannyezha/project/sekolah/EXAMVAN/.env
R2_ACCESS_KEY_ID=examvan-server
R2_SECRET_ACCESS_KEY=abc123def456...
R2_BUCKET=examvan-pdfs
R2_ENDPOINT=https://abc123.r2.cloudflarestorage.com
```

---

## 6. Step 3 — Tambah Package AWS SDK di Go

R2 kompatibel dengan S3 API. Kita pake AWS SDK Go v2.

```bash
cd /home/vannyezha/project/sekolah/EXAMVAN/webui

go get github.com/aws/aws-sdk-go-v2/config
go get github.com/aws/aws-sdk-go-v2/service/s3
go get github.com/aws/aws-sdk-go-v2/credentials
```

Cek `go.mod` udah masuk:

```bash
grep aws go.mod
```

Output contoh:
```
github.com/aws/aws-sdk-go-v2 v1.30.0
github.com/aws/aws-sdk-go-v2/config v1.27.0
github.com/aws/aws-sdk-go-v2/credentials v1.17.0
github.com/aws/aws-sdk-go-v2/service/s3 v1.60.0
```

---

## 7. Step 4 — Tambah Konfigurasi R2 di Config Go

**File: `webui/internal/config/config.go`**

Cari struct `Config` — tambah field R2:

```go
type Config struct {
    // === existing fields ===
    ServerPort  int    `envconfig:"PORT" default:"5000"`
    DatabaseURL string `envconfig:"DATABASE_URL"`
    RedisURL    string `envconfig:"REDIS_URL"`
    SecretKey   string `envconfig:"EXAMVAN_SECRET"`
    
    // === TAMBAH INI: Cloudflare R2 ===
    R2AccessKey     string `envconfig:"R2_ACCESS_KEY_ID"`
    R2SecretKey     string `envconfig:"R2_SECRET_ACCESS_KEY"`
    R2Bucket        string `envconfig:"R2_BUCKET" default:"examvan-pdfs"`
    R2Endpoint      string `envconfig:"R2_ENDPOINT"`
}
```

Cari fungsi `Load()` — pastikan R2 fields ikut di-load (biasanya `envconfig` otomatis):

```go
func Load() *Config {
    cfg := &Config{
        // default values
        R2Bucket: "examvan-pdfs",
    }
    // envconfig otomatis baca dari env
    if err := envconfig.Process("", cfg); err != nil {
        log.Fatalf("config: %v", err)
    }
    return cfg
}
```

---

## 8. Step 5 — Buat R2 Client Handler Baru

**Buat file baru: `webui/internal/handlers/r2/r2.go`**

```go
package r2

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// Client wraps S3-compatible R2 client.
type Client struct {
	S3      *s3.Client
	Bucket  string
	enabled bool
}

// NewClient creates R2 client. Returns nil if credentials missing.
func NewClient(accessKey, secretKey, endpoint, bucket string) *Client {
	if accessKey == "" || secretKey == "" || endpoint == "" {
		log.Println("r2: credentials missing — R2 disabled, PDF will be served locally")
		return nil
	}

	cfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion("auto"),
		config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(accessKey, secretKey, ""),
		),
	)
	if err != nil {
		log.Printf("r2: init config failed: %v — R2 disabled", err)
		return nil
	}

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = true // R2 requires path-style
	})

	log.Println("r2: connected — PDF will be served via Cloudflare")
	return &Client{
		S3:      client,
		Bucket:  bucket,
		enabled: true,
	}
}

// Enabled returns true if R2 is configured and ready.
func (c *Client) Enabled() bool {
	return c != nil && c.enabled
}

// Upload reads file from io.Reader and uploads to R2.
func (c *Client) Upload(ctx context.Context, key string, reader io.Reader) error {
	data, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("r2 read: %w", err)
	}

	_, err = c.S3.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(c.Bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(data),
		ContentType: aws.String("application/pdf"),
	})
	if err != nil {
		return fmt.Errorf("r2 upload: %w", err)
	}
	log.Printf("r2: uploaded %s (%d bytes)", key, len(data))
	return nil
}

// SignedURL generates temporary download URL valid for ttl duration.
func (c *Client) SignedURL(ctx context.Context, key string, ttl time.Duration) (string, error) {
	presign := s3.NewPresignClient(c.S3)

	req, err := presign.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.Bucket),
		Key:    aws.String(key),
	}, func(po *s3.PresignOptions) {
		po.Expires = ttl
	})
	if err != nil {
		return "", fmt.Errorf("r2 presign: %w", err)
	}

	return req.URL, nil
}

// Delete removes file from R2.
func (c *Client) Delete(ctx context.Context, key string) error {
	_, err := c.S3.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(c.Bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("r2 delete: %w", err)
	}
	return nil
}
```

---

## 9. Step 6 — Init R2 Client di main.go

**File: `webui/cmd/server/main.go`**

Cari bagian setelah init Redis, tambah:

```go
package main

import (
	// ... existing imports ...
	r2handler "github.com/examvan/webui/internal/handlers/r2"
)

func main() {
	// === existing code: load config, connect DB, connect Redis ===

	// === TAMBAH: Init R2 client ===
	var r2Client *r2handler.Client
	if cfg.R2AccessKey != "" && cfg.R2SecretKey != "" && cfg.R2Endpoint != "" {
		r2Client = r2handler.NewClient(
			cfg.R2AccessKey,
			cfg.R2SecretKey,
			cfg.R2Endpoint,
			cfg.R2Bucket,
		)
		if r2Client.Enabled() {
			log.Println("R2: Cloudflare R2 ready — PDF upload/download via R2")
		}
	} else {
		log.Println("R2: not configured — PDF will be served from local storage")
	}
	// ================================

	// === TAMBAH: Inject R2 client ke Gin context ===
	if r2Client != nil && r2Client.Enabled() {
		r.Use(func(c *gin.Context) {
			c.Set("r2", r2Client)
			c.Next()
		})
	}
	// ==============================================

	// === existing code: register routes, start server ===
}
```

---

## 10. Step 7 — Upload PDF ke R2 (Admin Upload)

**File: `webui/internal/handlers/admin/exams.go`**

Cari handler upload exam (biasanya `UploadExam` atau nama serupa). Tambah kode R2 setelah file diterima:

```go
func UploadExam() gin.HandlerFunc {
	return func(c *gin.Context) {
		// === existing code: ambil file dari form ===
		file, header, err := c.Request.FormFile("file")
		if err != nil {
			errorResponse(c, http.StatusBadRequest, "File tidak ditemukan")
			return
		}
		defer file.Close()

		filename := header.Filename

		// === existing code: validasi file ===
		// ... validasi ukuran, type ...

		// === existing code: simpen dulu ke temp ===
		// ... atau langsung ke storage ...

		// === TAMBAH: Upload ke R2 ===
		if r2c, exists := c.Get("r2"); exists {
			client := r2c.(*r2handler.Client)
			r2Key := fmt.Sprintf("pdfs/%s", filename)

			// Reset reader ke awal
			if seeker, ok := file.(io.Seeker); ok {
				seeker.Seek(0, io.SeekStart)
			}

			if err := client.Upload(c.Request.Context(), r2Key, file); err != nil {
				log.Printf("admin: R2 upload error: %v — saving locally", err)
				// fallback: simpan local seperti biasa
			} else {
				// Simpan r2_key di database, bukan file_path local
				exam.FilePath = r2Key
				log.Printf("admin: PDF uploaded to R2: %s", r2Key)
			}
		}
		// ========================================

		// === existing code: simpan ke database ===
	}
}
```

> **Catatan:** Kalau `questions_json` udah ada di exam, R2 key bisa disimpan di field `file_path` yang sudah ada. Ganti isinya dari `storage/exam.pdf` jadi `pdfs/exam.pdf`.

---

## 11. Step 8 — Serve PDF via R2 Signed URL (Download)

**File: `webui/internal/handlers/api/exams.go`**

Cari handler `ExamPDF` (fungsi serve PDF ke Android). Ubah jadi:

```go
func ExamPDF() gin.HandlerFunc {
	return func(c *gin.Context) {
		examID, err := strconv.Atoi(c.Param("exam_id"))
		if err != nil {
			errorResponse(c, http.StatusBadRequest, "ID ujian tidak valid")
			return
		}

		token := c.GetHeader("X-Exam-Token")
		if token == "" {
			errorResponse(c, http.StatusUnauthorized, "Token tidak disertakan")
			return
		}

		pool := getPool(c)
		ctx := c.Request.Context()

		// Verifikasi exam
		exam, err := models.GetExamByID(ctx, pool, examID)
		if err != nil {
			errorResponse(c, http.StatusNotFound, "Ujian tidak ditemukan")
			return
		}
		if exam.Token != token {
			errorResponse(c, http.StatusNotFound, "Token tidak valid")
			return
		}

		// ==========================================================
		// PRIORITAS 1: Serve PDF via Cloudflare R2 signed URL
		// ==========================================================
		if r2c, exists := c.Get("r2"); exists {
			client := r2c.(*r2handler.Client)
			if client.Enabled() {
				signedURL, err := client.SignedURL(ctx, exam.FilePath, 1*time.Hour)
				if err == nil {
					// Redirect siswa ke Cloudflare — server gak sentuh file
					c.Redirect(http.StatusFound, signedURL)
					return
				}
				log.Printf("api: R2 signed URL error: %v — fallback to local", err)
			}
		}

		// ==========================================================
		// PRIORITAS 2: Fallback — serve via Nginx X-Accel
		// ==========================================================
		storageDir := getStoragePath(c)
		pdfPath, err := safeStoragePath(storageDir, exam.FilePath)
		if err != nil {
			errorResponse(c, http.StatusBadRequest, "Path tidak valid")
			return
		}

		c.Header("Content-Type", "application/pdf")
		c.Header("X-Accel-Redirect", "/internal/pdf/"+exam.FilePath)
		c.Status(http.StatusOK)
	}
}
```

### Cara kerja redirect:

```
1. Android request PDF ke VPS (dengan token)
2. VPS validasi token → valid
3. VPS generate R2 signed URL (berlaku 1 jam)
4. VPS balikin HTTP 302 (redirect) ke URL itu
5. Android otomatis ikutin redirect
6. Download file langsung dari Cloudflare edge terdekat
7. VPS gak pernah megang file PDF sama sekali
```

**Signed URL expired 1 jam.** Siswa gak bisa sebar link ke orang lain setelah 1 jam.

---

## 12. Step 9 — Fallback: Nginx X-Accel (kalo R2 mati)

Ini penting buat jaga-jaga kalau Cloudflare R2 down atau maintenance.

**File: `webui/nginx/nginx.conf`**

Tambah lokasi baru sebelum `location /`:

```nginx
events {
    worker_connections 2048;
    use epoll;
    multi_accept on;
}

http {
    include       /etc/nginx/mime.types;
    default_type  application/octet-stream;
    sendfile      on;
    tcp_nopush    on;

    access_log /dev/stdout;
    error_log /dev/stderr;

    client_max_body_size 100M;

    upstream backend {
        server 127.0.0.1:5000;
        # Kalau pake STB atau server lain:
        # server 10.0.0.3:5000;
    }

    server {
        listen 80 default_server;
        server_name _;

        # === TAMBAH: Internal redirect untuk PDF (fallback kalo R2 mati) ===
        location /internal/pdf/ {
            internal;                          # cuma bisa diakses via redirect dari backend
            alias /opt/examvan/storage/;       # sesuaikan dengan STORAGE_PATH
        }

        # Health check
        location = /api/health {
            access_log off;
            proxy_pass http://backend;
        }
        location = /healthz {
            access_log off;
            add_header Content-Type text/plain;
            return 200 "ok";
        }

        # WebSocket
        location /ws/ {
            proxy_pass http://backend;
            proxy_http_version 1.1;
            proxy_set_header Upgrade $http_upgrade;
            proxy_set_header Connection "upgrade";
            proxy_read_timeout 86400s;
            proxy_send_timeout 86400s;
            proxy_buffering off;
        }

        # Static files
        location /static/ {
            proxy_pass http://backend;
            expires 365d;
            add_header Cache-Control "public, immutable";
        }

        # All other requests
        location / {
            proxy_pass http://backend;
            proxy_http_version 1.1;
            proxy_set_header Host $host;
            proxy_set_header X-Real-IP $remote_addr;
            proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
            proxy_set_header X-Forwarded-Proto $scheme;
            proxy_read_timeout 120s;
            proxy_send_timeout 120s;
        }
    }
}
```

**Cara kerja fallback:**

```
R2 HIDUP:
  Android ──► VPS (validasi token) ──► HTTP 302 ke Cloudflare R2

R2 MATI:
  Android ──► VPS (validasi token) ──► signedURL error
                                       └──► Nginx X-Accel Redirect
                                            └──► Nginx serve PDF dari disk
                                            (Go gak baca file, Nginx yang handle)
```

---

## 13. Step 10 — Testing R2

### Test upload via curl

```bash
# Upload file PDF ke R2 via API (simulasi admin)
R2_KEY="pdfs/test-upload.pdf"

curl -X POST https://YOUR-VPS-IP/admin/upload \
  -F "file=@/path/to/test.pdf"
```

### Test download signed URL

```bash
# Request PDF via API (simulasi Android)
curl -v http://YOUR-VPS-IP/api/exams/1/pdf \
  -H "X-Exam-Token: TOKEN_EXAM"

# Response harusnya:
# HTTP/1.1 302 Found
# Location: https://examvan-pdfs.r2.cloudflarestorage.com/...
```

### Test fallback — matiin akses R2

Simulasi R2 mati dengan set env kosong:

```bash
# Set R2_SECRET_ACCESS_KEY kosong — server akan fallback ke local
R2_SECRET_ACCESS_KEY="" ./examvan-server

# Atau restart dengan env kosong
```

### Monitoring R2 di Cloudflare Dashboard

1. Login Cloudflare → R2 → `examvan-pdfs`
2. Lihat: **Objects** (daftar file PDF)
3. Lihat: **Usage** (storage, class A/B operations, bandwidth)

---

## 14. Capacity Planner Lengkap

### Kapasitas per tier server (semua dengan R2)

Tabel ini asumsi **R2 sudah aktif** dan **bandwidth 100mbps cukup untuk API**.

| VPS | RAM | Core | PG max_conn | Target siswa | Bottleneck |
|-----|-----|------|-------------|-------------|------------|
| t3.micro ($0) | 1GB | 1 vCPU | 8 | ~500 | RAM |
| t3.small ($15/bln) | 2GB | 2 vCPU | 20 | ~2.000 | RAM |
| 2C/4GB ($20-30/bln) | 4GB | 2 | 50 | ~5.000 | RAM |
| 2C/8GB ($30-40) | 8GB | 2 | 100 | ~7.000 | CPU |
| 4C/8GB ($40-60) | 8GB | 4 | 150 | ~12.000 | ✅ seimbang |
| 8C/16GB ($80-120) | 16GB | 8 | 300 | ~25.000 | ✅ seimbang |
| **12C/32GB ($160+)** | **32GB** | **12** | **400** | **~50.000** | **✅ seimbang** |

### Dengan vs Tanpa R2 (12C/32GB, 100mbps)

| Skenario | Tanpa R2 | Dengan R2 |
|----------|---------|-----------|
| 1 siswa download PDF 5MB | ✅ 0,4 detik | ✅ redirect 1ms |
| 1.000 siswa download bareng | ❌ 6,5 menit — timeout massal | ✅ redirect 1ms per siswa — langsung ke Cloudflare |
| 50.000 siswa download bareng | ❌ 5,5 jam — impossible | ✅ redirect 50.000 × 1ms = 50 detik |
| Bandwidth VPS terpakai | 250GB | ~500KB (redirect) |
| Total siswa bisa ditampung | ~100 (bandwidth mati) | **~50.000** |

### Efek R2 di semua tier

| Tier | Sebelum R2 | Sesudah R2 | Peningkatan |
|------|-----------|-----------|-------------|
| t3.micro 1C/1GB | ~100 | **~500** | 5× |
| t3.small 2C/2GB | ~500 | **~2.000** | 4× |
| 4C/8GB | ~3.000 | **~12.000** | 4× |
| 12C/32GB | ~10.000 | **~50.000** | 5× |

---

## 15. Next Step — Setelah R2 Aktif

R2 udah jalan. Server gak serve PDF. Bottleneck pindah ke CPU/RAM. Sekarang kerjakan urut:

### Priority #1: Heartbeat Redis-only (kurangi write DB 90%)

**Masalah:** Tiap siswa kirim heartbeat tiap 1-2 menit → INSERT ke `student_access_logs`.

| Siswa | INSERT/jam | Per hari |
|-------|-----------|---------|
| 2.000 | 60.000 | 1.440.000 |
| 12.000 | 360.000 | 8.640.000 |

Tabel `student_access_logs` membesar 8 juta baris/hari. Query admin jadi lambat, DB write penuh.

**Solusi:** Heartbeat cuma simpan di Redis. Flush ke DB tiap 30 detik pake goroutine background.

#### Step 1 — Ubah handler AccessLog

File: `webui/internal/handlers/api/exams.go` — fungsi `AccessLog()`:

Cari bagian INSERT access_log ke database:

```go
// HAPUS atau COMMENT blok ini:
if _, err := models.CreateAccessLog(ctx, pool, accessLog); err != nil {
    log.Printf("access-log insert error: %v", err)
    errorResponse(c, http.StatusInternalServerError, "Gagal menyimpan log")
    return
}
```

Ganti jadi **cuma Redis SET**:

```go
func AccessLog() gin.HandlerFunc {
    return func(c *gin.Context) {
        // ... existing validasi code (token, exam, identity) ...

        // TAMBAH: Cuma simpan di Redis, jangan INSERT DB
        setStudentHeartbeat(rdb, examID, macAddress, map[string]interface{}{
            "student_name":  studentName,
            "exam_number":   examNumber,
            "student_class": studentClass,
            "device_info":   deviceInfo,
            "ip_address":    ipAddress,
            "event":         event,
            "last_seen":     time.Now().UTC().Format(time.RFC3339),
        })

        c.JSON(http.StatusOK, gin.H{"success": true, "message": "Access logged"})
    }
}
```

#### Step 2 — Buat background flusher di main.go

File: `webui/cmd/server/main.go` — tambah fungsi baru:

```go
// startHeartbeatFlusher periodically flushes heartbeat data from Redis to PostgreSQL.
// Menjalankan di background, tidak memblokir request API.
func startHeartbeatFlusher(rdb *redis.Client, pool *pgxpool.Pool) {
    if rdb == nil || pool == nil {
        log.Println("heartbeat-flusher: skipped (requires Redis + PostgreSQL)")
        return
    }

    go func() {
        ticker := time.NewTicker(30 * time.Second)
        defer ticker.Stop()

        for range ticker.C {
            flushHeartbeats(context.Background(), rdb, pool)
        }
    }()
    log.Println("heartbeat-flusher: started (flush every 30s)")
}

func flushHeartbeats(ctx context.Context, rdb *redis.Client, pool *pgxpool.Pool) {
    // SCAN semua heartbeat keys
    keys, err := rdb.Keys(ctx, "heartbeat:*").Result()
    if err != nil {
        log.Printf("heartbeat-flusher: scan error: %v", err)
        return
    }
    if len(keys) == 0 {
        return
    }

    // Baca semua data
    type heartbeatData struct {
        macAddress  string
        examID      int
        studentName string
        examNumber  string
        studentClass string
        deviceInfo  string
        ipAddress   string
        event       string
        lastSeen    string
    }

    var batch []heartbeatData
    for _, key := range keys {
        data, err := rdb.Get(ctx, key).Bytes()
        if err != nil {
            continue
        }
        // Parse key: heartbeat:<examID>:<macAddress>
        parts := strings.Split(key, ":")
        if len(parts) < 3 {
            continue
        }
        examID, _ := strconv.Atoi(parts[1])
        macAddress := parts[2]

        var payload struct {
            StudentName  string `json:"student_name"`
            ExamNumber   string `json:"exam_number"`
            StudentClass string `json:"student_class"`
            DeviceInfo   string `json:"device_info"`
            IPAddress    string `json:"ip_address"`
            Event        string `json:"event"`
            LastSeen     string `json:"last_seen"`
        }
        json.Unmarshal(data, &payload)

        batch = append(batch, heartbeatData{
            macAddress:   macAddress,
            examID:       examID,
            studentName:  payload.StudentName,
            examNumber:   payload.ExamNumber,
            studentClass: payload.StudentClass,
            deviceInfo:   payload.DeviceInfo,
            ipAddress:    payload.IPAddress,
            event:        payload.Event,
            lastSeen:     payload.LastSeen,
        })
    }

    if len(batch) == 0 {
        return
    }

    // Batch INSERT ke student_access_logs
    tx, err := pool.Begin(ctx)
    if err != nil {
        log.Printf("heartbeat-flusher: tx begin error: %v", err)
        return
    }
    defer tx.Rollback(ctx)

    for _, hb := range batch {
        _, err := tx.Exec(ctx,
            `INSERT INTO student_access_logs
             (exam_id, student_identifier, student_name, exam_number, student_class, event, ip_address, device_info, created_at)
             VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
            hb.examID, hb.macAddress, hb.studentName, hb.examNumber, hb.studentClass,
            hb.event, hb.ipAddress, hb.deviceInfo, hb.lastSeen,
        )
        if err != nil {
            log.Printf("heartbeat-flusher: insert error: %v", err)
        }
    }

    if err := tx.Commit(ctx); err != nil {
        log.Printf("heartbeat-flusher: tx commit error: %v", err)
    }
}
```

Panggil di `main()` setelah worker:

```go
// Di main.go, setelah queue worker
startHeartbeatFlusher(rdb, pool)
```

#### Step 3 — Hapus atau komentari CREATE Access Log di model

File: `webui/internal/models/access_log.go` — fungsi `CreateAccessLog()` gak dipanggil lagi. Bisa dibiarin aja atau dihapus nanti.

#### Hasil Heartbeat Redis-only

| Metrik | Sebelum | Sesudah |
|--------|---------|---------|
| INSERT ke DB tiap heartbeat | ✅ 30-60×/jam/siswa | ❌ 0× |
| DB write load 2.000 siswa | 60.000 INSERT/jam | 2 batch INSERT/menit |
| DB write load 12.000 siswa | 360.000 INSERT/jam | 2 batch INSERT/menit |
| Data admin lihat di dashboard | real-time | real-time (dari Redis) |
| Riwayat akses di DB | real-time | max 30 detik delay |

---

### Priority #2: Queue Worker Pool

**Masalah:** 1 worker goroutine, 1 job/iterasi. Kalau ribuan siswa submit bareng, antrian numpuk.

**Solusi:** N worker goroutine parallel process + batch INSERT.

File: `webui/internal/queue/submission_queue.go`

#### Ubah StartWorker jadi WorkerPool

```go
// Config queue — via env variable
type QueueConfig struct {
    WorkerCount int
    BatchSize   int
}

func DefaultQueueConfig() QueueConfig {
    return QueueConfig{
        WorkerCount: 4,    // 4 goroutine parallel
        BatchSize:   50,   // 50 submission per batch INSERT
    }
}

// WorkerPool manages multiple workers + batch inserter.
type WorkerPool struct {
    workers   []*worker
    batchChan chan SubmissionResult
    quit      chan struct{}
    wg        sync.WaitGroup
}

type SubmissionResult struct {
    Job    SubmissionJob
    Score  *float64
    Error  error
}

// StartWorkerPool launches N worker goroutines and a batch inserter.
func StartWorkerPool(rdb *goredis.Client, pool *pgxpool.Pool, cfg QueueConfig) *WorkerPool {
    wp := &WorkerPool{
        batchChan: make(chan SubmissionResult, cfg.BatchSize*2),
        quit:      make(chan struct{}),
    }

    // N worker — baca job dari Redis, scoring, kirim hasil ke batchChan
    for i := 0; i < cfg.WorkerCount; i++ {
        wp.wg.Add(1)
        go wp.runWorker(i, rdb, pool, cfg)
    }

    // 1 batch inserter — kumpulin hasil, flush tiap BatchSize atau 500ms
    wp.wg.Add(1)
    go wp.runBatchInserter(pool, cfg)

    log.Printf("queue: worker pool started (%d workers, batch=%d)", cfg.WorkerCount, cfg.BatchSize)
    return wp
}

// Stop gracefully shuts down all workers.
func (wp *WorkerPool) Stop() {
    close(wp.quit)
    wp.wg.Wait()
    log.Println("queue: worker pool stopped")
}

func (wp *WorkerPool) runWorker(id int, rdb *goredis.Client, pool *pgxpool.Pool, cfg QueueConfig) {
    defer wp.wg.Done()

    for {
        select {
        case <-wp.quit:
            return
        default:
        }

        // BRPOP dengan timeout
        ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
        result, err := rdb.BRPop(ctx, 5*time.Second, QueueKey).Result()
        cancel()

        if err != nil {
            if err == goredis.Nil || errors.Is(err, context.DeadlineExceeded) {
                continue
            }
            log.Printf("queue worker-%d: BRPOP error: %v", id, err)
            time.Sleep(time.Second)
            continue
        }

        if len(result) < 2 {
            continue
        }

        var job SubmissionJob
        if err := json.Unmarshal([]byte(result[1]), &job); err != nil {
            log.Printf("queue worker-%d: unmarshal error: %v", id, err)
            continue
        }

        // Proses scoring
        score, err := wp.processSubmission(context.Background(), pool, &job)
        wp.batchChan <- SubmissionResult{Job: job, Score: score, Error: err}
    }
}

func (wp *WorkerPool) runBatchInserter(pool *pgxpool.Pool, cfg QueueConfig) {
    defer wp.wg.Done()

    ticker := time.NewTicker(500 * time.Millisecond)
    defer ticker.Stop()

    var buf []SubmissionResult

    flush := func() {
        if len(buf) == 0 {
            return
        }
        wp.flushBatch(context.Background(), pool, buf)
        buf = buf[:0]
    }

    for {
        select {
        case <-wp.quit:
            flush()
            return
        case result := <-wp.batchChan:
            buf = append(buf, result)
            if len(buf) >= cfg.BatchSize {
                flush()
            }
        case <-ticker.C:
            flush()
        }
    }
}

func (wp *WorkerPool) flushBatch(ctx context.Context, pool *pgxpool.Pool, results []SubmissionResult) {
    tx, err := pool.Begin(ctx)
    if err != nil {
        log.Printf("queue batch: tx begin error: %v", err)
        return
    }
    defer tx.Rollback(ctx)

    for _, r := range results {
        if r.Error != nil {
            continue
        }
        answersJSON, _ := json.Marshal(r.Job.Answers)
        _, err := tx.Exec(ctx,
            `INSERT INTO submissions (exam_id, student_name, exam_number, student_class, answers_json, score, start_time, mac_address)
             VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, ''), $8)`,
            r.Job.ExamID, r.Job.StudentName, r.Job.ExamNumber, r.Job.StudentClass,
            string(answersJSON), r.Score, r.Job.StartTime, r.Job.MACAddress,
        )
        if err != nil {
            log.Printf("queue batch: insert error: %v", err)
        }
    }

    if err := tx.Commit(ctx); err != nil {
        log.Printf("queue batch: tx commit error: %v", err)
    }
}

// processSubmission does the scoring — ambil soal dari DB, hitung skor.
func (wp *WorkerPool) processSubmission(ctx context.Context, pool *pgxpool.Pool, job *SubmissionJob) (*float64, error) {
    var questionsJSON *string
    err := pool.QueryRow(ctx,
        `SELECT questions_json FROM exams WHERE id = $1`, job.ExamID).Scan(&questionsJSON)
    if err != nil {
        return nil, fmt.Errorf("fetch exam: %w", err)
    }

    questions, err := models.ParseQuestionsJSON(questionsJSON)
    if err != nil || len(questions) == 0 {
        return nil, nil
    }

    score := models.CalculateSubmissionScore(job.Answers, questions)
    return score, nil
}
```

#### Ubah config jadi env variable

File: `webui/internal/config/config.go`

```go
type Config struct {
    // ... existing fields ...
    QueueWorkers   int    `envconfig:"QUEUE_WORKERS" default:"4"`
    QueueBatchSize int    `envconfig:"QUEUE_BATCH_SIZE" default:"50"`
}
```

#### Panggil WorkerPool di main.go gantikan StartWorker

```go
// Ganti:
// worker := queue.StartWorker(rdb, pool)

// Jadi:
queueCfg := queue.DefaultQueueConfig()
queueCfg.WorkerCount = cfg.QueueWorkers
queueCfg.BatchSize = cfg.QueueBatchSize
worker := queue.StartWorkerPool(rdb, pool, queueCfg)
```

#### Env:

```bash
# .env
QUEUE_WORKERS=4
QUEUE_BATCH_SIZE=50
```

#### Hasil Queue Worker Pool

| Metrik | Sebelum (1 worker) | Sesudah (N worker) |
|--------|-------------------|-------------------|
| Submit throughput | 5-10/detik | 40-200/detik (tergantung N) |
| INSERT mode | 1 per job | batch 50 rows per 500ms |
| CPU usage | ~5% | ~30-50% (pake core yang ada) |

---

### Priority #3: PostgreSQL Tuning + Index

#### Index tambahan

File: `webui/internal/database/schema.sql`

```sql
-- Composite index — access_log per exam + waktu (untuk sorting)
CREATE INDEX IF NOT EXISTS idx_access_logs_exam_time
    ON student_access_logs(exam_id, created_at DESC);

-- Index — submissions lookup by exam + mac_address (untuk cek duplikat)
CREATE INDEX IF NOT EXISTS idx_submissions_exam_mac
    ON submissions(exam_id, mac_address);

-- Partial index — hanya exam yang aktif
CREATE INDEX IF NOT EXISTS idx_exams_active
    ON exams(id) WHERE status = 'active';
```

#### Autovacuum agresif untuk tabel write-heavy

```sql
-- student_access_logs dan submissions sering INSERT — vacuum agresif
ALTER TABLE submissions SET (autovacuum_vacuum_scale_factor = 0.01);
ALTER TABLE student_access_logs SET (autovacuum_vacuum_scale_factor = 0.01);
```

---

### Priority #4: Load Test

#### Install k6

```bash
# Di VPS atau laptop
sudo apt install k6
```

#### Test file

File: `loadtest.js`

```javascript
import http from 'k6/http';
import { check, sleep } from 'k6';

export const options = {
    stages: [
        { duration: '30s', target: 500 },   // Naik ke 500 VUs
        { duration: '1m', target: 2000 },    // Naik ke 2000 VUs
        { duration: '30s', target: 0 },      // Turun
    ],
    thresholds: {
        http_req_duration: ['p(95)<2000'],  // 95% request < 2 detik
        http_req_failed: ['rate<0.01'],     // Error < 1%
    },
};

export default function () {
    const payload = JSON.stringify({
        identity_data: { student_name: "Test", exam_number: "123", student_class: "A" },
        answers: { "1": "A", "2": "B", "3": "C", "4": "D", "5": "A" },
        mac_address: "AA:BB:CC:DD:EE:FF",
    });

    const res = http.post(
        `http://YOUR-VPS-IP/api/exams/1/submit`,
        payload,
        {
            headers: {
                'Content-Type': 'application/json',
                'X-App-Version': '99.99.99',
            },
        }
    );

    check(res, {
        'status is 200': (r) => r.status === 200,
        'response < 2s': (r) => r.timings.duration < 2000,
    });
}
```

#### Jalankan

```bash
# Test 2000 siswa submit bareng
k6 run --vus 2000 --duration 60s loadtest.js
```

#### Monitoring

```bash
# Terminal 1 — resource server
htop

# Terminal 2 — koneksi DB
watch -n 2 "psql -U examvan -c \"SELECT count(*) FROM pg_stat_activity WHERE state = 'active';\""

# Terminal 3 — queue stats
watch -n 2 'curl -s http://localhost:5000/admin/api/queue/status | jq .'
```

---

### Priority #5: Database Partisi (untuk 10.000+ siswa)

Kalau `submissions` udah > 1 juta baris, perlu partisi.

File: `webui/internal/database/schema.sql`

```sql
-- Buat tabel submissions baru dengan partition by hash
CREATE TABLE IF NOT EXISTS submissions_new (
    id            SERIAL,
    exam_id       INTEGER NOT NULL,
    student_name  TEXT DEFAULT '',
    exam_number   TEXT DEFAULT '',
    student_class TEXT DEFAULT '',
    answers_json  TEXT,
    score         DOUBLE PRECISION,
    start_time    TEXT,
    mac_address   TEXT DEFAULT '',
    created_at    TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id, exam_id)
) PARTITION BY HASH (exam_id);

-- 8 partisi
CREATE TABLE submissions_p0 PARTITION OF submissions_new FOR VALUES WITH (MODULUS 8, REMAINDER 0);
CREATE TABLE submissions_p1 PARTITION OF submissions_new FOR VALUES WITH (MODULUS 8, REMAINDER 1);
CREATE TABLE submissions_p2 PARTITION OF submissions_new FOR VALUES WITH (MODULUS 8, REMAINDER 2);
CREATE TABLE submissions_p3 PARTITION OF submissions_new FOR VALUES WITH (MODULUS 8, REMAINDER 3);
CREATE TABLE submissions_p4 PARTITION OF submissions_new FOR VALUES WITH (MODULUS 8, REMAINDER 4);
CREATE TABLE submissions_p5 PARTITION OF submissions_new FOR VALUES WITH (MODULUS 8, REMAINDER 5);
CREATE TABLE submissions_p6 PARTITION OF submissions_new FOR VALUES WITH (MODULUS 8, REMAINDER 6);
CREATE TABLE submissions_p7 PARTITION OF submissions_new FOR VALUES WITH (MODULUS 8, REMAINDER 7);
```

> **Catatan:** Partisi butuh migrasi data dari tabel lama. Kerjakan di maintenance window. Sebelum 1 juta baris, gak perlu partisi dulu — index cukup.

---

### Checklist Lengkap Urutan Pekerjaan

```
□ Fase 0 — R2 ✅ (udah jalan)
□ Fase 1 — Heartbeat Redis-only (30 menit)
□ Fase 2 — Queue Worker Pool + Batch INSERT (1-2 jam)
□ Fase 3 — PostgreSQL Index + Autovacuum (10 menit)
□ Fase 4 — Load Test k6 (30 menit)
□ Fase 5 — Partisi tabel (nanti, kalau > 1jt baris)
```

---

## Ringkasan Perubahan

### File baru: 2

| File | Baris | Isi |
|------|-------|-----|
| `webui/internal/handlers/r2/r2.go` | ~90 | R2 client: upload, signed URL, delete |
| `loadtest.js` | ~40 | Load test script k6 |

### File yang dimodifikasi: 7

| File | Perubahan | Untuk |
|------|-----------|-------|
| `webui/internal/config/config.go` | Tambah R2 + Queue config | Config |
| `webui/cmd/server/main.go` | Init R2, heartbeat flusher, worker pool | Bootstrap |
| `webui/internal/handlers/admin/exams.go` | Upload ke R2 | Admin |
| `webui/internal/handlers/api/exams.go` | Download R2 signedURL + heartbeat Redis-only | API |
| `webui/internal/queue/submission_queue.go` | Worker pool + batch INSERT | Queue |
| `webui/internal/database/schema.sql` | Index + autovacuum | DB |
| `webui/nginx/nginx.conf` | X-Accel fallback | Nginx |

### Yang TIDAK berubah

- ✅ Semua model (exam, submission, user)
- ✅ Semua handler admin (dashboard, users, pengawas, submissions list)
- ✅ Semua template HTML
- ✅ Semua middleware (auth, CSRF, rate limit)
- ✅ WebSocket hub
- ✅ Session management

---

## Timeline Estimasi

| Fase | Waktu | Yang kerja |
|------|-------|-----------|
| R2 (udah) | — | — |
| Heartbeat Redis-only | 30 menit | 2 file diubah |
| Queue worker pool | 1-2 jam | 2 file diubah |
| Index + tuning | 10 menit | 1 file + SQL |
| Load test | 30 menit | Jalankan script |

**Total: ~3 jam** untuk upgrade lengkap dari R2 sampai load test.

---

---

## Lampiran — Status Implementasi per 2 Juli 2026

### Ceklist Real

| # | Saran | Status | Detail |
|---|-------|--------|--------|
| **R2 Cloudflare** | ✅ SUDAH | Upload, signed URL, delete, fallback X-Accel — lengkap |
| **Nginx X-Accel** | ✅ SUDAH | `location /internal/pdf/` di nginx.conf |
| **Heartbeat skip INSERT** | ✅ SUDAH | `AccessLog()` sudah filter: event `heartbeat` gak INSERT ke DB. Login/logout tetap INSERT. |
| **Queue worker pool (8 goroutine)** | ✅ SUDAH | Naik dari **1 → 8 goroutine**. Parallel processing. |
| **Heartbeat flusher background** | ✅ SUDAH | Goroutine `startHeartbeatFlusher` jalan tiap 30 detik. Pop 100 data dari Redis list, INSERT batch pake transaksi PG. |
| **PG MaxConns tuning** | ✅ SUDAH | `MaxConns=100`, `MinConns=20`. Configurable via env `DATABASE_MAX_CONNS`. |
| **3 index baru** | ✅ SUDAH | `idx_access_logs_exam_time(exam_id, created_at DESC)`, `idx_submissions_exam_mac(exam_id, mac_address)`, `idx_exams_active(id) WHERE status='active'` — ada di schema.sql. |
| **Batch INSERT submissions** | ✅ SUDAH | 8 worker memproses lembar jawaban secara paralel, lalu di-flush ke DB dalam batch berisi 50 submissions per transaksi (atau timeout 500ms). |
| **Autovacuum tuning** | ✅ SUDAH | `ALTER TABLE` autovacuum scale factor = 0.01 sudah diterapkan di schema.sql. |
| **Load test** | ❌ **BELUM** | Belum pernah di-load test. Kapasitas real tidak diketahui. |

### Ringkasan

Dari 10 item:
- **9 sudah** — R2, Nginx, heartbeat skip INSERT, worker 8 goroutine, heartbeat flusher, PG MaxConns, 3 index, autovacuum tuning, batch INSERT submissions
- **1 belum** — load test

### Prioritas berikutnya (hanya 1)

1. **Load test pakai k6** — untuk tau batas real server. **(~30 menit)**

---

*Dokumen lengkap EXAMVAN — dari R2 sampai scaling horizontal. Dibuat 2 Juli 2026.*
