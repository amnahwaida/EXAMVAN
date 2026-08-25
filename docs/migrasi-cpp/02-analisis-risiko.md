# 02 — Analisis Risiko & Mitigasi

> Skor: Probabilitas (P) × Dampak (D), 1–5. Wajib ditinjau ulang di akhir setiap fase.

## Register Risiko

| ID | Risiko | P | D | Skor | Mitigasi |
|---|---|---|---|---|---|
| R-01 | Memory-safety bug C++ (use-after-free, buffer overflow) pada service yang melayani ujian nasional | 4 | 5 | **20** | ASan+UBSan wajib hijau per commit; larangan raw pointer; code review 2 orang; fuzzing endpoint HTTP (libFuzzer) sejak F2 |
| R-02 | Paritas sesi/CSRF tidak identik → seluruh user ter-logout / CSRF bypass | 3 | 5 | 15 | Format cookie dibekukan + test parity lintas proses (F5); dual-run membandingkan accept/reject |
| R-03 | Gap ekosistem (excelize/OpenXLSX fitur beda, formatting XLSX rusak) | 4 | 3 | 12 | F7 paritas file: buka hasil export dengan LibreOffice headless + bandingkan nilai sel; fallback CSV |
| R-04 | Regressi perilaku halus (header, redirect, status code) yang tak tercover test | 4 | 4 | 16 | Characterization test Go (golden response) untuk SEMUA endpoint tersentuh sebelum porting; diff proxy shadow (dok. 05) |
| R-05 | Scope creep "sekalian" menulis ulang frontend | 3 | 4 | 12 | Anti-scope dok. 01 §6; frontend & 1056 guard test dilarang disentuh |
| R-06 | Keahlian C++ tim terbatas → kecepatan & kualitas turun | 3 | 4 | 12 | F2 wajib ada review dari engineer C++ eksternal/senior; standar kode tertulis sebelum F2 keluar |
| R-07 | Bus factor: satu-satunya paham C++ resign | 3 | 4 | 12 | Dokumentasi ADR per keputusan; pairing wajib; Opsi B membatasi permukaan C++ |
| R-08 | Kinerja TIDAK lebih baik dari Go (target salah ukur) | 2 | 4 | 8 | Gerbang F0: tanpa profil, tidak ada migrasi; load test F3 = bukti sebelum cutover |
| R-09 | Dua backend hidup lama → drift konfigurasi/job dobel jalan | 3 | 4 | 12 | Job TETAP milik Go sampai hari cutover final; flag feature untuk pemindahan tanggung jawab satu per satu |
| R-10 | Keamanan: kesalahan implementasi TLS/cookie/crypto manual di C++ | 3 | 5 | 15 | Gunakan OpenSSL primitives baku; JANGAN implementasi kripto sendiri; security review sebelum F5 keluar |

## Risiko yang Sengaja TIDAK Dimitigasi (diterima)

- Build time C++ lebih lambat dari Go — diterima, diimbangi cache CI.
- Stack trace/error message C++ kurang ramah — diterima, diimbangi structured logging sejak F2.

## Pemicu Menghentikan Migrasi (Kill Criteria)

Migrasi WAJIB berhenti & dibongkar jika salah satu terjadi:

1. F3 gagal mencapai target RSS/latency setelah 2 iterasi.
2. Lebih dari 2 regressi produksi berturut-turut yang berakar ke sidecar C++.
3. Tidak ada lagi developer aktif yang memahami kode C++.
4. Bukti F0 ternyata menunjukkan bottleneck asli di PostgreSQL/hardware (bukan aplikasi).
