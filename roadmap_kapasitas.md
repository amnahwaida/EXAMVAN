# Roadmap Kapasitas EXAMVAN — Lapis 2 & Lapis 3

> **Status:** Dokumen perencanaan (bukan panduan implementasi). Mendeskripsikan arah pengembangan arsitektur setelah Lapis 1, pemicunya, desainnya secara deskriptif, dan batas keputusan yang sudah dibuat.
>
> **Prasyarat membaca:** seksi [Kapasitas Satu NAT](README.md#kapasitas-satu-nat-wifi-sekolah--maksimal-60-perangkat) dan [Kapasitas Lapis 1](README.md#kapasitas-lapis-1--plafon-koneksi-nginx-cache-setting-pipa-heartbeat-retensi-log--persistensi-redis-23-agustus-2026) di README.

---

## Daftar Isi

1. [Target & Titik Berangkat](#1-target--titik-berangkat)
2. [Peta Lapisan](#2-peta-lapisan)
3. [Rekap Lapis 1 — Sudah Selesai](#3-rekap-lapis-1--sudah-selesai)
4. [Lapis 2 — Multi-Replika di Satu Mesin Kuat](#4-lapis-2--multi-replika-di-satu-mesin-kuat)
5. [Lapis 3 — High Availability Multi-Host](#5-lapis-3--high-availability-multi-host)
6. [Tangga Kapasitas](#6-tangga-kapasitas)
7. [Kapan Naik Lapis — Kriteria Objektif](#7-kapan-naik-lapis--kriteria-objektif)
8. [Keputusan yang Sudah Dibuat (Anti-Scope)](#8-keputusan-yang-sudah-dibuat-anti-scope)
9. [Urutan Kerja yang Disarankan](#9-urutan-kerja-yang-disarankan)

---

## 1. Target & Titik Berangkat

**Target bisnis jangka panjang:** hingga **1000 ujian aktif bersamaan, masing-masing maksimal 500 perangkat** — potensi agregat ±500.000 perangkat serentak.

**Titik berangkat (Agustus 2026):** satu server HP t620 (2 core AMD Jaguar @1,65 GHz, 7,2 GB RAM, disk ±13 GB) menjalankan seluruh stack: PostgreSQL, Redis, Go server, nginx, cloudflared tunnel.

Review kapasitas menemukan bahwa dinding sistem muncul **berurutan**, bukan sekaligus:

```
(1) plafon koneksi nginx  →  (2) RAM koneksi WebSocket  →  (3) CPU + PostgreSQL
     [dibuang Lapis 1]           [dinding t620 hari ini]        [dinding berikutnya]
                                   lalu gelombang join & submit deadline memakai
                                   sisa kepala ruang yang tersisa
```

Prinsip roadmap ini: **naik lapis hanya ketika dipicu sinyal nyata** (lihat seksi 7), karena setiap lapisan menukar kesederhanaan operasional dengan skala — dan biaya kompleksitas itu harus dibayar hanya kalau dipakai.

## 2. Peta Lapisan

| Lapis | Tema | Status | Bentuk |
|---|---|---|---|
| **0** | Fondasi arsitektur: PDF offload ke Cloudflare R2, submit async via Redis queue, WS siswa receive-only, rate-limit sadar-NAT | ✅ Ada sejak awal | Desain software |
| **1** | Membuang plafon artifisial pada satu instance: koneksi nginx, query setting per-request, drain heartbeat, retensi log, persistensi Redis, limit resource | ✅ **Selesai 23 Agustus 2026** | Konfigurasi + perbaikan lokal |
| **2** | Menjadikan aplikasi **replica-ready**: WS hub via Redis pub/sub, audit state in-process, load balancer upstream, PG terpisah + pgbouncer | 📋 Direncanakan | Arsitektur software |
| **3** | **High availability**: beberapa host fisik/VPS, PG primary+replica atau managed, failover tunnel | 🔭 Visi | Infrastruktur |

Intuisinya: **Lapis 0–1 membuat satu instance sekuat mungkin; Lapis 2 membuat instance bisa digandakan; Lapis 3 membuat penggandaan itu bertahan dari kerusakan mesin.**

## 3. Rekap Lapis 1 — Sudah Selesai

Dirilis dalam commit `351cd2d` (23 Agustus 2026), dokumentasi lengkap di README:

1. Plafon koneksi nginx diangkat (`worker_processes auto`, `worker_connections 10240`, nofile 65535);
2. Cache baca `saas_settings` TTL 30 detik dengan invalidasi tulisan;
3. Heartbeat flusher drain-until-empty (±20.000 baris/menit) + requeue saat transaksi gagal;
4. SELECT `submissions` tidak lagi dijalankan untuk event heartbeat;
5. Job retensi `student_access_logs` (default 90 hari) + index `created_at`;
6. Persistensi Redis AOF `everysec` + volume;
7. Limit resource diselaraskan (db 1G, `max_connections=150`, pool app 60, `gin.Logger` dev-only).

**Efek:** urutan dinding bergeser dari *koneksi nginx* ke *RAM WebSocket → CPU+PostgreSQL*. Pada t620, band realistisnya **±60–150 ujian × 500 perangkat** (optimis, dengan join/submit ter-stagger).

## 4. Lapis 2 — Multi-Replika di Satu Mesin Kuat

### 4.0 Karakter penting: Lapis 2 ≠ banyak mesin

Seluruh Lapis 2 dapat dijalankan pada **satu server modern** (8–16 core, 32–64 GB, NVMe) berupa beberapa kontainer Go + PostgreSQL + Redis. Yang digandakan adalah **proses**, bukan mesin. Estimasi band topologi seperti itu: **±100–250 rb perangkat serentak (≈200–500 ujian)**. Angka analitis — gerbang verifikasinya load test (seksi 9).

Sebaliknya, Lapis 2 **tidak boleh** dijalankan sebagai multi-proses di t620: replika tambahan di box 2-core hanya membagi-bagi CPU yang sama sambil menduplikasi memori — nol tambahan kapasitas, tambah kompleksitas (detail di seksi 8).

```
TOPOLOGI LAPIS 2 (satu mesin kuat):

                    Cloudflare Edge (TLS, CDN, R2 PDF)
                              │ cloudflared (multi-connector)
                        ┌─────▼─────┐
                        │   nginx   │  upstream load-balancing
                        └──┬──┬──┬──┘
                     ┌─────┘  │  └─────┐
                ┌────▼───┐ ┌──▼────┐ ┌─▼──────┐
                │ go #1  │ │ go #2 │ │ go #3  │   stateless
                └──┬──┬──┘ └──┬──┬─┘ └─┬──┬───┘
        WS pub/sub │  │       │  │     │  │
              ┌────▼──▼───────▼──▼─────▼──▼───┐
              │        Redis (pub/sub + queue)│
              └───────────────┬───────────────┘
                        ┌─────▼──────┐
                        │ pgbouncer  │
                        └─────┬──────┘
                        ┌─────▼──────┐
                        │ PostgreSQL │  kontainer/host terpisah
                        └────────────┘
```

### 4.1 Eksternalisasi WebSocket hub ke Redis pub/sub

**Kondisi sekarang** (`webui/internal/websocket/hub.go`): hub in-process — `rooms map[string]map[*Client]bool`, broadcast lewat channel internal, event penting (`exam_terminated`, `student_update`). Klien siswa **receive-only** (token-authed, hanya ping/receive); presence tetap lewat HTTP access-log. Untuk satu instance, desain ini adalah yang paling murah dan benar.

**Masalah saat ada ≥2 replika:** koneksi TCP menempel pada tepat satu replika. Event yang dipicu request ke replika A (mis. admin menghentikan ujian lewat halaman yang dilayani replika B) tidak akan pernah sampai ke socket yang hidup di replika A. Dasbor monitoring juga melihat subset siswa saja. Inilah satu-satunya bloker stateful untuk replikasi horizontal.

**Desain rencana:**

- Setiap event room di-publish ke channel Redis `examvan:ws:room:<exam_id>` (payload sama dengan yang sekarang masuk `broadcast chan`);
- Tiap replika subscribe sekali per room yang punya klien lokal, lalu meneruskan pesan ke socket-nya masing-masing;
- Register/unregister klien tetap lokal per replika (tidak ada shared state klien);
- Pub/sub bersih otomatis: unsubscribe saat room kosong (Redis pub/sub tanpa persistence — pesan hanya untuk klien yang sedang terhubung, sesuai semantik sekarang yang memang best-effort push).

**Konsekuensi yang harus sadar:** latensi broadcast bertambah satu hop Redis (~sub-ms lokal); throughput fan-out dibatasi loopback, bukan network. Di satu mesin modern ini murah; di t620 ini kerugian murni (seksi 8).

**Estimasi:** ±3–5 hari kerja termasuk test dua-hub saling bertukar event (miniredis mendukung pub/sub).

### 4.2 Audit state in-process lain (replica-readiness)

Pemeriksaan menyeluruh state yang tersimpan di proses:

| Komponen | Kondisi kini | Perlu perubahan? |
|---|---|---|
| Session login admin/guru | Cookie store (`cookie.NewStore`) — stateless | ✅ Aman apa adanya |
| Rate-limit middleware | Redis utama; **fallback in-memory** jika Redis nil | ⚠️ Saat multi-replika wajib Redis hidup; fallback in-memory menjadi salah hitung antar replika → dokumentasikan Redis sebagai dependensi keras |
| Throttle handler (submit/approval/result/pdf) | Semua via Redis | ✅ Aman |
| Queue worker submit | `LPUSH`/`BRPOP` — atomik, aman multi-konsumen | ✅ Aman; replika tambahan otomatis menambah tenaga konsumsi |
| Heartbeat flusher | `RPop` atomik; ticker ganda antar replika tidak merusak | ✅ Aman (pekerjaan terbagi siapa duluan pop) |
| Job cleanup (approval, retensi access-log) | Loop timer per proses, `DELETE` idempoten | ⚠️ Aman tapi boros: N replika melakukan DELETE yang sama tiap interval → tambahkan advisory lock PostgreSQL (`pg_try_advisory_lock`) agar satu pemimpin per pass |
| `checkRateLimit` fallback & cache `saas_settings` | In-process | ✅ Cache TTL per-proses valid untuk semua replika (staleness ≤30 dtk tetap terkontrol) |
| Health endpoint LB | `/api/health` sudah ada | ✅ Cukup sebagai probe |

### 4.3 Load balancing & edge

- **nginx upstream block** dengan N target `webui-server:N` + `keepalive` ke upstream (menghemat pembuatan koneksi per request);
- **Tanpa sticky session** — setelah WS hub via pub/sub, tidak ada afinitas yang diperlukan: HTTP bebas ke replika mana pun, dan socket WS memang tertempel satu replika oleh TCP sendiri (broadcast mengejarnya lewat pub/sub);
- **Cloudflared multi-connector:** jalankan ≥2 kontainer/koneksi tunnel dengan token sama; Cloudflare otomatis membagi dan mem-failover trafik. Ini juga langkah pertama menuju Lapis 3 tanpa mengubah aplikasi.

### 4.4 PostgreSQL terpisah + pgbouncer

- Pisahkan kontainer PG dari tier aplikasi (host/kontainer berbeda dengan budget resource sendiri) agar OOM/penyetelan keduanya independen;
- **Perhitungan koneksi berubah saat multi-replika:** total koneksi = Σ pool per replika (mis. 4 replika × 60 = 240 > `max_connections=150`). Dua pilihan: turunkan pool per replika (4 × 30 = 120), atau pasang **pgbouncer mode transaction** sehingga jumlah koneksi nyata ke PG tetap kecil apa pun jumlah replikanya. Untuk workload ini (query pendek, banyak) transaction pooling sangat cocok;
- Tuning lanjutan yang layak dievaluasi saat itu: `work_mem`, checkpoint/WAL untuk gelombang insert heartbeat, partisi `student_access_logs` per bulan bila retensi panjang.

### 4.5 Observability minimum sebelum menggandakan apa pun

Multi-replika tanpa metrik = debugging buta. Prasyarat Lapis 2:

- Metrik antrean Redis (`LLen examvan:submissions:pending`, `examvan:heartbeats:pending`) diekspor/dilog berkala;
- Jumlah klien WS per replika dan total (endpoint internal kecil cukup);
- Alert sederhana pada kriteria seksi 7 (CPU sustained, memori, genangan antrean) — bahkan cron + threshold + webhook pun cukup untuk tahap ini.

## 5. Lapis 3 — High Availability Multi-Host

Visi, belum dirancang final. Dipicu oleh dua hal yang berbeda:

1. **Kapasitas mentok** satu mesin kuat (>±200–300 rb perangkat agregat);
2. **Kebutuhan asuransi** — jendela ujian bersifat immovable (ujian nasional jam 08:00 tidak bisa ditunda), sehingga kerusakan PSU/NIC/disk di hari-H adalah risiko bisnis, bukan teknis. *Dua server sedang lebih baik daripada satu server besar.*

Bentuk kasarnya:

- 2+ host app (identik, image sama, replika Lapis 2) di belakang multi-connector tunnel;
- PostgreSQL: streaming replication + failover otomatis (Patroni dsb.) **atau** managed DB (biaya vs tenaga ops diputuskan saat itu); RPO/RTO ditetapkan eksplisit karena jawaban siswa adalah data uang;
- Redis: tetap single dengan AOF + backup terjadwal dulu; Sentinel baru kalau downtime Redis tak lagi dapat diterima;
- **Latihan restore rutin** (backup yang belum pernah di-restore adalah harapan, bukan backup).

## 6. Tangga Kapasitas

| Tahap | Topologi | Band perangkat serentak* | ≈ Ujian × 500 | Catatan |
|---|---|---|---|---|
| Sekarang (t620 + Lapis 1) | 1 mesin lemah, single instance | ±30–80 rb | ±60–150 | Dinding: RAM WS, CPU+PG |
| Lapis 2 | 1 mesin kuat (16c/64G/NVMe), 3–6 replika | ±100–250 rb | ±200–500 | Dinding: CPU total, gelombang join |
| Lapis 2 + host kedua (app) | app tier 2 mesin, PG dedikated | ±250–400 rb | ±500–800 | Mulai HA |
| Target penuh | 2–4 app host + PG dedikated (+replica) | ±500 rb | **1000** | Lapis 3 untuk redundansi penuh |

\* Band analitis dari review kode Agustus 2026 — **bukan benchmark**. Setiap lompatan tahap wajib melewati gerbang load test (seksi 9).

Faktor di luar server yang tetap menentukan pada skala besar: bandwidth WAN tiap sekolah untuk unduh PDF (sudah dioffload ke R2) dan kapasitas AP WiFi per gedung (desain ≤60 WS/menit/NAT sudah mengantisipasinya).

## 7. Kapan Naik Lapis — Kriteria Objektif

Naik lapis **bukan** keputusan kalender, tapi keputusan data. Alarm yang dipantau selama jam ujian riil:

| Sinyal | Cara memantau | Artinya |
|---|---|---|
| CPU `go-server` + `db` sustained >70% di jam ujian | `docker stats` / cAdvisor | Dinding CPU tercapai → pertimbangkan mesin lebih kuat (masih Lapis 1) atau mulai Lapis 2 |
| Memori `go-server` merangkak mendekati limit 512M | `docker stats` | Jumlah koneksi WS mendekati plafon RAM → mesin lebih besar, atau Lapis 2 bila RAM tak bisa ditambah |
| Log flusher: `flushed N` terus tinggi tiap tick / `LLen heartbeats:pending` tidak kembali ke ~0 | log + `redis-cli LLen` | Pipa heartbeat tak terkejar → perbesar mesin/PG |
| Join lambat / approval pending menumpuk padahal auto-approve ON | keluhan + dasbor monitoring | Gelombang join melampaui kemampuan → stagger jadwal, lalu evaluasi hardware |
| Beban bisnis mendekati ±100 ujian bersamaan di t620 | angka penjualan/operasional | Pemicu bisnis: rencanakan mesin kedua + kerjakan Lapis 2 |

Aturan praktisnya: **dua sinyal infrastruktur muncul rutin pada beban normal = waktunya upgrade hardware; target bisnis 1000 ujian masuk horizon 1–2 kuartal = waktunya Lapis 2**, dalam urutan itu.

## 8. Keputusan yang Sudah Dibuat (Anti-Scope)

Hal-hal yang **secara eksplisit TIDAK dilakukan** agar tidak ada keraguan di kemudian hari:

1. **Tidak ada multi-replika di t620.** Replika membagi CPU yang sama + menduplikasi memori = nol kapasitas tambahan, plus kompleksitas. Lapis 2 hanya masuk akal di mesin yang lebih kuat.
2. **Tidak ada eksternalisasi WS hub selama masih single-instance.** Pub/sub menambah hop per broadcast; di satu proses, channel in-process lebih murah. Kode hub sekarang adalah bentuk optimal untuk topologi sekarang.
3. **Tidak ada pgbouncer di t620.** Pool 60 koneksi tidak punya masalah churn yang diselesaikan pgbouncer.
4. **Tidak menunda item Lapis 1.** Semua perbaikan Lapis 1 bernilai penuh pada topologi mana pun (cache setting, retensi log, persistensi Redis ikut terpakai di Lapis 2–3 apa adanya).
5. **t620 tidak dibuang.** Saat mesin utama bertambah, t620 berguna sebagai host cadangan/monitoring/staging — cocok untuk beban ringan non-uji.

## 9. Urutan Kerja yang Disarankan

Saat pemicu seksi 7 menyala, urutan pengerjaan Lapis 2 yang meminimalkan risiko:

```
1. Observability minimum (seksi 4.5)          ← dasar semua keputusan berikut
2. WS hub → Redis pub/sub + test 2-hub        ← bloker utama; bisa diuji di 1 mesin
   (jalankan SINGLE instance dulu di produksi)
3. Advisory lock job cleanup                  ← kecil, persiapan multi-proses
4. nginx upstream + pgbouncer + PG pisah      ← infrastruktur, tanpa ubah kode
5. Naikkan replika 1→2→4 sambil load test     ← gerbang: k6 (HTTP + skenario WS),
   di staging dengan profil gelombang join      bandingkan hasil vs estimasi seksi 6
6. Baru bicara Lapis 3 (host kedua, PG HA)
```

**Gerbang verifikasi tiap tahap:** load test dengan profil realistis (gelombang join 500 device/menit, heartbeat 1/menit/device, burst submit deadline) — angka di tangga kapasitas baru berubah status dari "estimasi" menjadi "terukur".

---

*Dokumen ini dibuat 23 Agustus 2026 setelah penyelesaian Lapis 1. Perbarui tabel seksi 2 dan 6 setiap kali sebuah tahap selesai atau estimasi terverifikasi oleh load test.*
