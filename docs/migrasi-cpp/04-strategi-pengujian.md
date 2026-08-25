# 04 — Strategi Pengujian & Bukti Performa

## 1. Nasib Aset Test Eksisting

| Aset | Jumlah | Nasib saat migrasi |
|---|---|---|
| Node guard tests (statik + vm behavioral, templates/JS/CSS) | ±1056 test | **TETAP, tidak berubah** — mereka menguji file frontend yang tidak disentuh. Ini jaring pengaman terbesar migrasi. |
| Go handler/model tests | ±27k baris (termasuk test DB via pool nyata) | Sumber untuk **characterization test**: perilaku tercatat → diport ke test C++ / harness parity per endpoint yang dimigrasi |
| miniredis (test redis) | modul test | Ganti: redis sungguhan di CI service container |

## 2. Tiga Lapis Pengujian Baru

### Lapis 1 — Unit C++ (GoogleTest)
- Per fungsi/porter: parser cookie sesi, decoder payload WS, formatter tanggal, presign R2.
- Sanitizer build wajib: setiap commit dibangun & dites dengan ASan+UBSan.

### Lapis 2 — Paritas Go ↔ C++ (kunci utama)
Harness parity (Go test atau script):
1. Jalankan request yang sama ke backend Go dan backend C++ (dual-run lokal).
2. Bandingkan menurut kelas paritas dokumen 03 (byte-exact / json-schema / html-structural).
3. Semua selisih = temuan; nol selisih = gerbang lulus.

Wajib diparity-kan minimal: login/logout, satu endpoint CRUD tiap entitas, hasil ujian, cek hasil, download signed URL, semua tipe pesan WebSocket.

### Lapis 3 — Load & Soak (bukti performa)
- Alat: k6 atau wrk + skenario realistis (gelombang join, heartbeat tiap N detik, submit deadline serentak).
- Metrik wajib dicatat per iterasi: RSS proses, p50/p95/p99 latency, jumlah koneksi aktif, drop rate.
- Target = angka F0 (misal: 10k koneksi WS ≤ X MB; submit p99 ≤ Y ms).
- Soak 24 jam tanpa kebocoran memori (grafik RSS datar).

## 3. METODOLOGI INTI: TEST-CODE FIRST (non-negotiable)

Seluruh migrasi memakai disiplin yang sama dengan 20 batch UI/UX:

1. **Kontrak dulu, kode kemudian.** Setiap fase/endpoint/perilaku ditulis sebagai test SEBELUM implementasi, diverifikasi MERAH pada asersi yang tepat (bukan merah karena syntax error/harness rusak), baru implementasi sampai HIJAU. Bukti merah & hijau dilampirkan di PR.
2. **Sumber kebenaran = output Go yang sedang berjalan.** Golden response/fixture dibuat dari Go dulu; C++ ditulis untuk membuat test itu hijau — bukan sebaliknya.
3. **Merah palsu = bug harness.** Test yang merah karena kesalahan setup wajib diperbaiki sebelum dipakai sebagai bukti.
4. Suite node 1056 + suite lama wajib hijau penuh sebelum merge; `go build`/`go vet` tetap jalan selama Go masih hidup.
5. Setiap bug C++ wajib didahului test pembuktinya (regresi permanen).

### 3b. Penerapan per fase (ringkas)

| Fase | Kontrak test-first |
|---|---|
| F0 | Harness load-test & definisi metrik ditulis + direview SEBELUM pengukuran (agar angka tak bisa dimanipulasi) |
| F1 | Characterization test = fixture golden dari output Go |
| F2 | Test healthcheck/pool MERAH → implementasi skeleton |
| F3 | Parity test tiap tipe pesan WS dari capture Go |
| F4–F6 | Golden response per endpoint → handler C++ sampai hijau (satu PR per endpoint) |
| F7 | Fixture XLSX hasil Go sebagai ekspektasi test |
| F8 | Dashboard diff-parity = test berjalan permanen |

## 4. Aturan Tambahan

1. Suite node 1056 + suite baru wajib hijau penuh sebelum merge (`go build`/`go vet` tetap jalan selama Go masih hidup).
2. Setiap perbaikan bug C++ wajib didahului test yang membuktikan bug tersebut (regresi permanen).

## 4. Smoke Visual Manual

Setelah cutover staging: checklist manual 15 menit per rilis — login admin, buka settings tiap tab, buat voucher single+batch, redeem, upload app, monitor ujian (WS), download PDF/APK, export XLSX, cek toast/fokus keyboard. Daftar hidup di dokumen 05.
