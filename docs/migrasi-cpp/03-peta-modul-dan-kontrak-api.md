# 03 — Peta Modul & Kontrak API (DIBEKUKAN)

> Dokumen ini adalah kontrak perilaku yang wajib identik antara implementasi Go dan C++.
> Status: kerangka — diisi lengkap saat F1 (kontrak freeze) dengan bantuan characterization test.

## 1. Inventaris Komponen

| Komponen | Lokasi Go | Baris | Migrasi (B/C) |
|---|---|---|---|
| WebSocket hub real-time | internal/websocket/hub.go | 594 | B & C: F3 |
| Halaman publik + hasil + download | internal/handlers/public/ | ±15 file | B: tetap Go · C: F4 |
| Dashboard/admin SSR | internal/handlers/admin/dashboard.go dkk | ±20 file | C: F6 |
| CRUD user/voucher/package/settings | handlers admin + models | ±10 model | C: F6 |
| Export XLSX | excelize handler + test | 1 modul | C: F7 |
| Background jobs (expiry, approval cleanup, access-log retention) | *job*.go | 3 job | B: tetap Go · C: F7 |
| Sesi cookie + CSRF + Turnstile | middleware/auth, helpers | ±2 modul | C: F5 |
| R2/S3 signed URL | r2 handler | 1 modul | B & C: libcurl presign |

## 2. Kontrak Endpoint HTTP

Diisi F1 dari router Gin secara otomatis (script ekstraksi) lalu dibekukan. Format per endpoint:

```
METHOD /path
Auth: admin|operator|guru|publik(+sesi)
Request : <query/form/json — contoh>
Response: status, header penting, bentuk body (atau "SSR html")
Paritas : byte-exact | json-schema | html-structural
```

Aturan paritas:
- Endpoint JSON → paritas **json-schema** (field & tipe sama; urutan key bebas).
- Endpoint SSR → paritas **html-structural** (selector & teks kunci sama; whitespace bebas).
- Redirect → status + Location exact.

## 3. Protokol WebSocket (paling kritikal)

Sumber kebenaran sekarang: `internal/websocket/hub.go`. Yang wajib identik di uWS:

1. **Handshake/auth**: validasi cookie sesi saat upgrade; tolak tanpa sesi valid.
2. **Room**: join per `exam_id` (+ role siswa/pengawas); leave bersih saat disconnect.
3. **Tipe pesan** (daftar final dari hub.go): heartbeat/ping-payload, event join/leave pengawas, notifikasi submission baru, countdown/status exam.
4. **Backpressure**: perilaku saat send queue penuh (Go: slow-client policy) — tiru eksak.
5. **Reconnect semantics**: client JS eksisting TIDAK boleh perlu berubah; handshake close code harus sama.

## 4. Kontrak Sesi Cookie (lintas proses)

- Nama cookie, atribut (Path/Domain/Secure/HttpOnly/SameSite) — salin dari gin-contrib/sessions options saat ini.
- Serialisasi value + signature HMAC: algoritma, urutan field, encoding base64 — dibekukan; C++ hanya VERIFIKASI+DECODE (Opsi B), bukan membuat sesi baru.
- Rotasi kunci: dukung 2 kunci (verify current + previous) agar rotasi tidak mem-broadcast logout massal.

## 5. Job Latar Belakang

| Job | Interval | Efek samping | Pemilik saat Opsi B |
|---|---|---|---|
| expiry_job | terjadwal | tombstone paket kedaluwarsa | Go |
| approval_cleanup_job | terjadwal | hapus approval kadaluarsa | Go |
| access_log_retention_job | terjadwal | purge log lama | Go |

Aturan: selama ada dua backend hidup, HANYA satu proses yang menjalankan tiap job (kunci Redis `SETNX job:<nama>` wajib dipakai keduanya).

## 6. Client Non-Browser & Aturan Data (KRITIS untuk repo baru)

### 7.1 Inventaris client

| Client | Ter-deploy di | Endpoint yang dikonsumsi | Bisa dipaksa update? |
|---|---|---|---|
| Browser siswa/pengawas/admin | — (SSR+JS dari server) | semua | ya, otomatis |
| **Android APK** (`android/`) | perangkat siswa | join ujian, exam viewer, queued submit, heartbeat WS | ❌ tidak serentak |
| **Desktop kiosk** (`desktop/`) | lab sekolah | kemungkinan jalur ujian kiosk | ❌ tidak serentak |

**Konsekuensi:** versi APK/kiosk lama tetap beredar berminggu-minggu setelah cutover. Migrasi backend WAJIB mempertahankan:
- bentuk & field respons endpoint yang dikonsumsi Android/desktop,
- status code dan pesan error yang diparse client (lihat test `android/app/src/androidTest` — JoinFlow, QueuedSubmitProcessDeathRecovery, ExamViewerErrorState),
- protokol WebSocket handshake/close-code persis.

Perubahan API apa pun = rilis APK baru + masa transisi dukung dua versi → DI LUAR scope migrasi ini.

### 7.2 Aturan data selama transisi

1. **Skema PostgreSQL DILARANG berubah** — tidak ada migration tooling formal saat ini (skema bootstrap di `webui/internal/database/schema.sql`); repo baru membaca schema.sql sebagai referensi, bukan membuat skema sendiri.
2. Repo baru TIDAK membuat volume DB produksi sendiri — terhubung ke instance produksi yang sama.
3. Redis: nama channel/key antrean submission dibekukan; selama uji coba gunakan prefix terpisah (dok. 05 §1 poin 3).
4. R2 bucket & pola signed URL identik (libcurl presign).

## 7. Yang TIDAK Berubah (dilarang disentuh migrasi)

- Skema PostgreSQL & migrasi SQL
- Semua template frontend + aset statis + 1056 test node guard
- Konfigurasi nginx/cloudflared/R2 bucket
- Alur review UI/UX (`review_uiux_webui.md`)
