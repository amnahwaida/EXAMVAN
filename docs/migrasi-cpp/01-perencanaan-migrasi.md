# 01 — Perencanaan Migrasi (Rencana Utama)

> Prasyarat baca: 00-ringkasan-eksekutif.md. Dokumen ini berlaku untuk Opsi B (hybrid) **dan** Opsi C (full); perbedaannya hanya cakupan fase F4–F7.

## 1. Target Arsitektur

```
[REPO BARU: C++/uWS — compose lengkap sendiri]
webui-cpp ── PostgreSQL (produksi, sama)
           ├─ Redis (sama)
           └─ R2 via libcurl presign (sama)

nginx (host) ── upstream switch per-grup endpoint:
                 lama = webui-server Go (EXAMVAN, dibekukan)
                 baru = webui-cpp
```

Prinsip:
- **Satu sumber kebenaran data**: PostgreSQL + Redis tetap milik bersama; tidak ada duplikasi state di C++.
- **Penggantian penuh, bukan koeksistensi**: WebSocket hub pindah UTUH ke C++ (F3) — tidak ada dua hub melayani grup klien yang sama; jalur lama ditutup saat grupnya di-switch.
- **Sesi/auth**: C++ memvalidasi cookie sesi yang sama (format HMAC dibekukan, lihat dokumen 03 §4) agar sesi user dan client Android/desktop tidak terputus saat switch.
- **Repo baru, frontend dibawa**: template + statis + 1056 test guard disalin ke repo baru sebagai fondasi (mereka tidak berubah perilaku).

## 2. Pemetaan Teknologi

| Komponen Go saat ini | Pengganti C++ | Catatan risiko |
|---|---|---|
| Gin (routing/middleware) | uWS `App` routing + middleware manual | Pola handler berbeda; tulis helper sendiri |
| gorilla/websocket (hub.go 594 baris) | uWS WebSocket native (per-socket userdata, loop event) | **Inti manfaat performa**; port logika room/heartbeat eksak |
| pgx/v5 | libpq (C) dibungkus wrapper async / SOCI | API paling beda; wajib prepared statement + pool sendiri |
| go-redis v9 | redis-plus-plus (hiredis) | Matang; API mirip |
| gin-contrib/sessions (cookie HMAC) | Custom signed-cookie (HMAC-SHA256, OpenSSL) | Format serialisasi dibekukan (dok. 03 §4) agar sesi lintas proses valid |
| excelize (export XLSX) | OpenXLSX, atau fallback CSV+zip | Fungsi minor; jangan biarkan menggembungkan scope |
| Turnstile siteverify POST | libcurl / uWS HTTP client | Sederhana |
| Background jobs (expiry/cleanup/retention) | std::jthread + timer loop, atau tetap di Go selama Opsi B | Opsi B: job TETAP di Go |
| html/template (SSR) | Opsi B: tidak tersentuh · Opsi C: inja atau prerender | Opsi C = perubahan frontend terbesar |

## 3. Standar Teknis Wajib (C++)

- **C++20**, CMake ≥3.28, compiler GCC ≥13 / Clang ≥17.
- Build: `-Wall -Wextra -Wpedantic -Werror`, sanitizer build (`ASan+UBSan`) wajib hijau di CI setiap commit.
- Package: vcpkg atau Conan (pilih satu, tulis di CI).
- Format: clang-format config di repo; PR tanpa format ditolak CI.
- Dilarang: raw `new/delete`; gunakan `std::unique_ptr/shared_ptr`, `std::string_view` dengan aturan lifetime tertulis, `.at()` untuk akses berbatas.
- Setiap modul wajib punya test unit (GoogleTest/Catch2) + coverage target ≥70% jalur baru.

## 3b. Docker, Compose & Repositori Baru

**Keputusan:** implementasi C++ di **repositori baru**; stack baru menggantikan penuh `webui-server` Go. Compose tetap dipakai, tapi modelnya berubah dari "menambah service" menjadi "dua deployment independen":

### 3b.1 Repo baru = proyek compose lengkap sendiri

Repo C++ memiliki `docker-compose.yml` sendiri:

| Service | Image | Catatan |
|---|---|---|
| `webui-cpp` | multi-stage build (cmake/gcc13 + vcpkg → distroless) | target sanitizer terpisah untuk CI |
| `db` | postgres:16-alpine | identik tuning eksisting (max_connections=150, shared_buffers) |
| `redis` | redis:7-alpine + AOF everysec | identik eksisting |
| (opsional) `nginx` | upstream ke `webui-cpp` | atau nginx host eksisting yang diarahkan |

Standar eksisting dipertahankan: healthcheck `depends_on: service_healthy`, `mem_limit`/`cpus` ketat (bukti target RSS), override dev, `.env` paritas (`DATABASE_URL`, `REDIS_URL`, R2).

### 3b.2 Data: SATU sumber kebenaran PostgreSQL

Skema hidup di database produksi (`schema.sql` Go hanya bootstrap awal — tidak ada tooling migration formal). Aturan transisi:

1. **Dilarang mengubah skema** selama masa transisi (client Android/desktop lama + jalur rollback bergantung padanya).
2. Repo baru TIDAK membuat volume DB sendiri untuk produksi — ia terhubung ke **instance PostgreSQL produksi yang sama**.
3. Untuk staging/dev repo baru: dump schema.sql + data anonim sebagai seed.

### 3b.3 Model transisi: dua deployment independen

```
[repo LAMA: EXAMVAN]                    [repo BARU: c++]
compose: db+redis+webui-server          compose: webui-cpp (+db/redis utk dev)
        ▲                                        ▲
        └──────────── nginx ─────────────────────┘
                 (upstream switch per-grup)
```

- Keduanya hidup bersamaan selama transisi, terhubung ke PostgreSQL/Redis/R2 yang SAMA.
- Redis: hati-hati antrean submission — kunci nama channel/prefix agar tidak saling konsumsi selama uji coba (detail dok. 05).
- Rollback = arahkan nginx kembali ke deployment lama (tetap ter-deploy, dibekukan kecuali patch keamanan).

## 4. Fase & Gerbang

> **Semua fase memakai metodologi TEST-FIRST** (dokumen 04 §3): kontrak test ditulis & diverifikasi MERAH pada asersi yang tepat sebelum implementasi; sumber ekspektasi = perilaku/output Go yang sedang berjalan.

| Fase | Isi | Gerbang keluar (wajib lulus semua) | Estimasi* |
|---|---|---|---|
| **F0** | Profiling & baseline: RSS/koneksi WS, p99 latency endpoint panas, throughput submit deadline | Angka target tertulis; keputusan G0 (B/C/tidak jadi) | 3–5 hari |
| **F1** | Bekukan kontrak (dok. 03): daftar endpoint, protokol WS, format sesi; characterization test Go untuk endpoint yang akan disentuh | Dokumen kontrak disetujui; *characterization test = fixture golden dari output Go, ditulis sebagai kontrak MERAH yang menunggu implementasi C++* | 1 minggu |
| **F2** | Skeleton C++: repo, CMake, CI (build+sanitizer+test), Docker image, healthcheck, koneksi PG/Redis pool | *Test healthcheck/pool MERAH dulu* → implementasi; CI hijau + deploy staging | 1–2 minggu |
| **F3** | Port WebSocket hub (Opsi B mulai di sini; Opsi C lanjut F4–F7): auth cookie, join room exam, heartbeat, pub/sub Redis → fan-out | *Parity test tiap tipe pesan WS ditulis dari capture Go dulu* → implementasi; load test: RSS & latency sesuai target F0 | 2–3 minggu |
| **F4** *(C saja)* | Endpoint read-only HTTP (halaman hasil, cek hasil, download metadata) | *Golden response Go menjadi fixture test sebelum handler C++ ditulis* → hijau per endpoint | 3–4 minggu |
| **F5** *(C saja)* | Sesi + CSRF + Turnstile + login/logout | *Parity test login (sukses & gagal) ditulis dari perilaku Go dulu*; Turnstile verify OK | 2 minggu |
| **F6** *(C saja)* | Write paths admin (CRUD user/voucher/exam/packages/settings) + upload R2 | *Satu PR per endpoint: characterization test MERAH → implementasi HIJAU*; review keamanan | 4–6 minggu |
| **F7** *(C saja)* | Export XLSX + background jobs | *Fixture XLSX hasil Go = ekspektasi test*; job punya test waktu-jalur | 2 minggu |
| **F8** | Dual-run + shadow traffic → cutover bertahap (dok. 05) | *Dashboard diff-parity = test berjalan permanen*; kriteria cutover lulus 1 minggu berturut | 2–3 minggu |

\* Estimasi 1 dev senior C++ full-time; tambahkan buffer ×1,5 untuk realita.

## 5. Urutan Kerja yang Disarankan

1. F0 dulu — keputusan paling murah dan paling informatif.
2. Jika G0 = lanjut: kerjakan **Opsi B sampai selesai F3 + F8 (dual-run WS)**, evaluasi dampak nyata di produksi.
3. Keputusan masuk F4 (full HTTP) hanya dengan bukti pasca-Opsi-B.

## 6. Anti-Scope (larangan selama migrasi)

- Dilarang mengubah skema DB, format API, atau perilaku UI dalam commit migrasi.
- Dilarang "sekalian refactor" kode Go yang tidak disentuh.
- Dilarang menulis framework sendiri di atas uWS.
- Perubahan UI/UX lewat alur `review_uiux_webui.md` seperti biasa — bukan bagian migrasi.
