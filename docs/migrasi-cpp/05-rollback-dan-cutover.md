# 05 — Dual-Run, Cutover & Rollback

## 1. Model Transisi: Dua Deployment Independen

**Keputusan repositori terpisah** mengubah model: bukan dua service dalam satu compose, melainkan **dua proyek compose lengkap** yang hidup bersamaan di server yang sama (atau berdekatan), dengan nginx sebagai penentu:

```
[DEPLOYMENT LAMA: EXAMVAN]              [DEPLOYMENT BARU: repo C++]
compose: db + redis + webui-server      compose: webui-cpp (+db/redis dev)
PostgreSQL & Redis PRODUKSI ◄──────────────── SAMA (satu sumber kebenaran)
R2 (PDF/APK signed URL) ◄─────────────────── SAMA
        ▲                                          ▲
        └──────────── nginx ──────────────────────┘
              upstream switch per-grup endpoint
```

Aturan main:
1. **Data tunggal**: kedua stack terhubung ke PostgreSQL/Redis/R2 produksi yang sama — tidak ada duplikasi data.
2. **Deployment lama dibekukan**: hanya patch keamanan kritis; tidak ada perubahan perilaku selama transisi (agar parity tetap bermakna).
3. **Redis antrean submission**: saat uji coba, service baru memakai prefix channel/key terpisah ATAU diuji read-only sampai cutover grupnya — jangan biarkan dua proses mengkonsumsi antrean yang sama sebelum siap.
4. **Client Android/desktop kiosk** menunjuk ke satu domain yang sama → mereka otomatis pindah ke backend baru saat nginx berpindah. Karena itu paritas API/WS (dok. 03) wajib sempurna SEBELUM switch grup yang menyentuh endpoint mobile.

## 2. Kriteria Cutover Per Grup Endpoint

Semua wajib hijau selama 7 hari berturut-turut SEBELUM grup dipindah permanen:

1. Paritas: nol selisih pada harness parity (dok. 04 Lapis 2).
2. Performa: p99 ≤ target F0; RSS sesuai angka; tidak ada restart OOM.
3. Error rate: < baseline Go + 0,1%.
4. Soak 24 jam bersih di staging dengan trafik produksi yang direkam ulang.

## 3. Urutan Cutover Disarankan (Opsi C)

1. `/ws/*` (setelah F3) — dampak performa terbesar, permukaan terkecil.
2. Halaman publik read-only (hasil, cek hasil).
3. Login/logout + halaman admin SSR.
4. CRUD write paths admin.
5. Export XLSX + matikan job Go (pindah ke C++ — pastikan kunci Redis job aktif satu proses).

## 4. Prosedur Rollback

| Kondisi | Aksi | Waktu |
|---|---|---|
| Error rate naik / latency melonjak setelah cutover grup | Balik upstream nginx grup itu ke deployment lama | < 1 menit |
| Bug data (tulis salah format dsb.) | Balik upstream + audit baris yang ditulis C++ sejak cutover | < 1 hari |
| Keraguan besar apa pun | Full rollback ke stack lama; deployment C++ tetap hidup untuk investigasi | Secepat deploy |

Syarat rollback aman:
- **State tunggal**: PostgreSQL/Redis/R2 tetap sumber kebenaran — kedua backend stateless terhadap file/disk lokal.
- **Skema tidak berubah selama transisi** (aturan dok. 03 §7): penulisan data oleh C++ identik Go sehingga bolak-balik tidak meninggalkan jejak yang membuat stack lama tersandung.
- **Cookie sesi**: format dibekukan (dok. 03 §4) — sesi user tetap valid di kedua arah.
- Deployment lama WAJIB tetap ter-deploy & ter-update patch keamanan sampai masa pengamatan berakhir.

## 5. Setelah Cutover Penuh (decommission)

1. Matikan traffic Go ≥ 2 minggu dengan tetap ter-deploy (jaga kemampuan balik cepat).
2. Arsipkan repo/tag Go (`v-go-final`), jangan hapus — riwayat 20 batch UI/UX & test mengacu padanya.
3. Pindahkan ownership job & dokumentasi operasional (runbook) ke service C++.
4. Hapus route Go dari nginx.

## 6. Checklist Hari-Cutover (per grup)

- [ ] Flag nginx siap dibalik dalam 1 menit (rehearsal dilakukan)
- [ ] Dashboard metrik (RSS/p99/error) terbuka & diawasi
- [ ] Smoke visual manual 15 menit lulus (dok. 04 §4)
- [ ] Cadangan DB terbaru ada & restore-tested
- [ ] Jendela waktu: di luar jam ujian; pengumuman ke pengawas bila grup = /ws/*
