# 00 — Ringkasan Eksekutif

> **Keputusan yang diminta:** apakah backend EXAMVAN dimigrasi dari Go/Gin ke C++20/uWebSockets.
> **Dokumen lengkap:** lihat README.md folder ini. Dokumen ini ringkasannya + rekomendasi.

## 1. Fakta Kunci (Agustus 2026)

> **Keputusan pemilik proyek:** implementasi C++ di **repositori baru terpisah**, target akhir **penggantian penuh** stack Go. Konsekuensinya: (a) kontrak API/WS wajib beku — client Android APK & desktop kiosk yang sudah ter-deploy tidak bisa dipaksa update serentak; (b) skema PostgreSQL tidak boleh berubah selama transisi; (c) stack lama tetap ter-deploy sebagai jalur rollback sampai parity stabil. Detail: dokumen 03 §7 dan 05 §1.

| Fakta | Nilai | Sumber |
|---|---|---|
| Ukuran backend Go | ±27.000 baris (+27.000 baris test) | `wc -l` repo |
| Stack saat ini | Gin, pgx/PostgreSQL, go-redis, gorilla/websocket (hub 594 baris), excelize, R2/S3, sessions cookie, 3 background job | go.mod |
| Dinding performa terukur | **RAM per koneksi WebSocket** di server t620 (2 core/7,2GB), lalu CPU+PostgreSQL | roadmap_kapasitas.md |
| Target skala bisnis | 1000 ujian × maks 500 perangkat (potensi ratusan ribu koneksi) | roadmap_kapasitas.md |
| Aset regresi frontend | 1056 test node hijau + test Go handler/model | CI repo |
| Prinsip arsitektur eksisting | "Naik lapis hanya dipicu sinyal nyata" (anti-scope) | roadmap_kapasitas.md §8 |

## 2. Analisis Jujur: Apa Sebenarnya Bottleneck-nya?

Roadmap kapasitas sudah mendokumentasikan bahwa dinding sistem berbentuk **infrastruktur**, bukan bahasa:

1. **RAM per koneksi WebSocket** — gorilla/websocket boros memori per koneksi. *Di sinilah C++/uWebSockets benar-benar unggul*: uWS mampu ratusan ribu koneksi WS dengan footprint KB-level per koneksi.
2. **CPU + PostgreSQL** — migrasi ke C++ hampir tidak membantu; waktu dihabiskan di query & disk. Yang membantu adalah indeks, connection pool, dan hardware.

**Kesimpulan teknis:** argumen C++ kuat KHUSUS untuk lapisan WebSocket real-time, lemah untuk keseluruhan aplikasi (SSR halaman, CRUD admin, export XLSX, job).

## 3. Tiga Opsi Arsitektur

| Opsi | Deskripsi | Biaya | Risiko | Dampak RAM/WS |
|---|---|---|---|---|
| **A. Stay Go + naik Lapis 2/3** | Ikuti roadmap kapasitas eksisting (replika multi-proses di mesin kuat → HA) | Terendah | Terendah | Tidak berubah (dibayar hardware) |
| **B. Hybrid sidecar (REKOMENDASI)** | C++/uWS service KEcil khusus WebSocket heartbeat/monitoring push; Go tetap urus semua bisnis. Redis pub/sub sebagai jembatan | Sedang | Sedang-rendah | 10–50× lebih hemat per koneksi |
| **C. Full rewrite C++/uWS** | Seluruh backend ditulis ulang | Sangat tinggi (4–6+ bulan 1 dev senior, gap ekosistem) | Tinggi (memori-safety, regressi, parity sesi/CSRF) | Sama dengan B |

## 4. Rekomendasi

1. **Jalankan Fase 0 dulu (profiling + bukti).** Ukur RSS per 1000 koneksi WS di staging; definisikan angka target (mis. ≤100MB per 10k koneksi).
2. **Default: Opsi B (hybrid sidecar)** — mendapat 90% manfaat performa C++ dengan <20% biaya/risko full rewrite. Arsitekturnya identik dengan Opsi C untuk komponen WS, jadi **tidak menutup jalan ke Opsi C**.
3. **Opsi C hanya jika** setelah Opsi B aktif masih ada bukti bottleneck di jalur non-WS, DAN tim memiliki ≥1 developer C++ production-grade, DAN jendela 4–6 bulan tersedia.
4. Keputusan final = gerbang G0 di dokumen perencanaan (Fase 0).

## 5. Syarat Mutlak Sebelum Menulis Baris C++ Pertama

- [ ] Profil pprof/metrics produksi/staging tersimpan & dibaca (bukan asumsi)
- [ ] Angka target performa tertulis & disetujui (latency p99, RSS/koneksi, throughput submit)
- [ ] Kontrak API & protokol WebSocket dibekukan di dokumen 03
- [ ] CI pipeline baru siap menerima build/test C++
- [ ] Rollback plan (dokumen 05) direview & disetujui
- [ ] Seluruh tim sepakat metodologi **TEST-FIRST**: kontrak test ditulis & diverifikasi MERAH sebelum implementasi (dokumen 04 §3)
