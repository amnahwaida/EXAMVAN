# Migrasi EXAMVAN: Go → C++ (uWebSockets)

> Folder ini berisi seluruh dokumen perencanaan migrasi backend dari Go/Gin ke C++20/uWebSockets.
> **Status: PERENCANAAN — belum ada keputusan eksekusi.** Gerbang Go/No-Go ada di Fase 0.
>
> **KEPUTUSAN TERKUNCI (pemilik proyek):**
> 1. Implementasi C++ berada di **REPOSITORI BARU yang terpisah** dari EXAMVAN ini — bukan sidecar menyertai `webui-server`.
> 2. Target akhir: stack baru **menggantikan penuh** stack Go (tidak ada koeksistensi permanen).
> 3. Konsekuensi wajib dibaca: [03-peta-modul §6](03-peta-modul-dan-kontrak-api.md#6-client-non-browser--aturan-data-kritis-untuk-repo-baru) (client Android/desktop tak bisa dipaksa update → kontrak API/WS beku) dan [05 §1](05-rollback-dan-cutover.md#1-model-transisi-dua-deployment) (dua deployment independen, bukan dua service dalam satu compose).

## Daftar Dokumen

| Dokumen | Isi | Kapan dibaca |
|---|---|---|
| [00-ringkasan-eksekutif.md](00-ringkasan-eksekutif.md) | Keputusan inti, 3 opsi arsitektur, rekomendasi, syarat Go/No-Go | Pembuat keputusan, dulu dan paling penting |
| [01-perencanaan-migrasi.md](01-perencanaan-migrasi.md) | Rencana utama: target arsitektur, pemetaan teknologi, fase F0–F8 dengan gerbang & estimasi | Tim pelaksana |
| [02-analisis-risiko.md](02-analisis-risiko.md) | Register risiko + mitigasi (memori-safety, gap ekosistem, regressi) | Tim pelaksana + reviewer |
| [03-peta-modul-dan-kontrak-api.md](03-peta-modul-dan-kontrak-api.md) | Inventaris endpoint/WebSocket/job/sesi yang wajib dipertahankan identik | Tim pelaksana |
| [04-strategi-pengujian.md](04-strategi-pengujian.md) | Nasib 1056 test UI guard, characterization test Go, harness paritas, load test bukti performa | QA + tim pelaksana |
| [05-rollback-dan-cutover.md](05-rollback-dan-cutover.md) | Dual-run, shadow traffic, kriteria cutover, prosedur mundur | Ops + pembuat keputusan |

## Aturan Emas Membaca Dokumen Ini

1. **Tidak ada fase yang boleh dilewati.** Setiap fase punya gerbang keluar yang harus lulus sebelum fase berikutnya.
2. **Fase 0 boleh menghasilkan keputusan TIDAK JADI** — dan itu dianggap hasil yang baik, bukan kegagalan.
3. Frontend (templates + vanilla JS + 1056 test guard) **tidak ikut migrasi** dan adalah aset regresi terbesar kita.
4. Semua angka estimasi usaha adalah untuk **1 developer berpengalaman C++**; kalikan sesuai komposisi tim.
5. **METODOLOGI UTAMA: TEST-CODE FIRST** — identik dengan 20 batch UI/UX: setiap perilaku baru/porting ditulis sebagai TEST terlebih dahulu, diverifikasi MERAH pada kontrak yang tepat (bukan merah karena harness rusak), baru implementasi sampai HIJAU. Tidak ada pengecualian untuk C++. Detail di dokumen 04 §3.
