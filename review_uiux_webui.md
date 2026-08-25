# Review UI/UX Web App EXAMVAN

> **Tanggal review:** 23 Agustus 2026 · **Basis kode awal:** branch `main` @ `111019e` · **Re-review ronde 2:** 23 Agustus 2026 @ `cbc837f` (setelah Batch 1–4 selesai)
> **Metode:** pembacaan menyeluruh ±26.000 baris template + CSS + JS oleh 3 reviewer paralel (area admin, area publik, lintas-halaman/a11y/design-system) + verifikasi manual temuan kunci. Ronde 2 mengulang metode yang sama (3 reviewer paralel) untuk memverifikasi perbaikan dan mencari temuan baru.
> **Ronde 2:** seluruh temuan lama Tinggi/Sedang/Rendah (kecuali yang dicatat masih terbuka) terverifikasi BERES; ditemukan **2 masalah Tinggi, 14 Sedang, dan 12 Rendah baru** — lihat [bagian 5.5](#55-re-review-ronde-2--temuan-baru-pasca-batch-14).
> **Ronde 3 (24 Agustus 2026 @ `1387853`, pasca Batch 8):** migrasi Batch 7–8 terverifikasi bersih di level registry; ditemukan **3 masalah Tinggi, 10 Sedang, dan 13 Rendah baru** — lihat [bagian 5.6](#56-re-review-ronde-3--temuan-baru-pasca-batch-58). Seluruhnya dieksekusi di **Batch 9** (25/26 item — sisa terbuka: R30 ditunda butuh keputusan UX).
> **Ronde 4 (24 Agustus 2026 @ `a1afd9c`, pasca Batch 9):** regresi Batch 9 hampir seluruhnya bersih (1 eksekusi perlu dirapikan → S47); ditemukan **2 masalah Tinggi, 13 Sedang, dan 12 Rendah baru** — lihat [bagian 5.7](#57-re-review-ronde-4--temuan-baru-pasca-batch-9). Seluruhnya dieksekusi di **Batch 10** kecuali S57 ditunda (ekstraksi blok inline besar).
> **Ronde 5 (24 Agustus 2026 @ `2debff6`, pasca Batch 10):** eksekusi Batch 10 terverifikasi asli & terukur, namun ditemukan **3 masalah Tinggi, 5 Sedang, dan 13 Rendah baru** — termasuk regresi fungsional T19 (race defer vs registrasi Actions) dan dua temuan integritas proses (klaim `[x]` yang tidak tuntas) — lihat [bagian 5.8](#58-re-review-ronde-5--temuan-baru-pasca-batch-10). Seluruhnya dieksekusi di **Batch 11**.
> **Ronde 6 (24 Agustus 2026 @ `4c87bc8`, pasca Batch 11):** verifikasi Batch 11 praktis bersih (1 sub-item tertinggal → S66); ditemukan **1 masalah Tinggi, 5 Sedang, dan 10 Rendah baru** — lihat [bagian 5.9](#59-re-review-ronde-6--temuan-baru-pasca-batch-11). Seluruhnya dieksekusi di **Batch 12** (S69 parsial: aria-live tuntas, stempel jam-perangkat ditunda butuh timestamp server).
> **Ronde 7 (24 Agustus 2026 @ `4fc2ab8`, pasca Batch 12):** ditemukan **1 masalah Tinggi (regresi tampilan dialog voucher), 6 Sedang, dan 11 Rendah baru** — lihat [bagian 5.10](#510-re-review-ronde-7--temuan-baru-pasca-batch-12). Eksekusi di **Batch 13** (S73 parsial: format disatukan client-side; perbandingan kedaluwarsa server-side ditunda butuh API).
> **Ronde 8 (24 Agustus 2026 @ `b37f715`, pasca Batch 13):** eksekusi Batch 13 terverifikasi solid (satu klaim dikoreksi: R82 ternyata PARSIAL — sisa literal `#818cf8`); ditemukan **2 masalah Tinggi, 13 Sedang, dan 11 Rendah baru** — lihat [bagian 5.11](#511-re-review-ronde-8--temuan-baru-pasca-batch-13). Tema dominan ronde ini: keyboard-dead custom controls, kontrak token yang ditegakkan per-daftar-file (bukan folder-wide), dan guard regex first-match-only yang memberi rasa aman palsu.
> **Ronde 9 (25 Agustus 2026 @ `f0ab8d7`, pasca Batch 14):** eksekusi Batch 14 mayoritas solid, namun ditemukan **kerusakan produksi kritis yang lolos 3 rilis batch dengan seluruh suite JS hijau**: template Pengaturan gagal di-parse Go sejak Batch 12 (T26) + regresi role-gating operator (T27); total ronde ini **4 masalah Tinggi, 17 Sedang, dan 20 Rendah baru** — lihat [bagian 5.12](#512-re-review-ronde-9--temuan-baru-pasca-batch-14-bahan-batch-15). Tema dominan ronde ini: guard JS yang tidak pernah mengeksekusi parser Go maupun `go test`, assertion yang mengunci teks bukan perilaku (vakum/marker salah), dan kelas race respons basi yang hanya dibasmi pada daftar-loader kontrak. Seluruhnya dieksekusi di **Batch 15** (25 Agustus 2026, test-first via gerbang koordinator + 4 agen paralel; satu-satunya penundaan: S100 checksum SHA-256 butuh keputusan skema DB).
> **Ronde 10 (25 Agustus 2026 @ `616132a`, pasca Batch 15):** eksekusi Batch 15 terverifikasi solid di lima area (966 test node + `go test` penuh hijau), NAMUN audit integritas menemukan **5 item tercatat [x] padahal tidak pernah dieksekusi** (S92, R102–R104, R110 — luput dari pembagian tugas agen); ditemukan **2 masalah Tinggi (regresi autofill OTP, navigasi mati di tablet sentuh ≥1101px), 6 Sedang, dan 14 Rendah baru** — lihat [bagian 5.13](#513-re-review-ronde-10--temuan-baru-pasca-batch-15-bahan-batch-16). Tema dominan ronde ini: ekor polishment dari fix besar (regresi & celah cakupan), kontrak escape/token yang belum seragam, dan dokumentasi teknis berklaim keliru. Seluruhnya dieksekusi di **Batch 16** (25 Agustus 2026, test-first via 4 agen paralel + gerbang koordinator; cakupan admincore diambil alih koordinator setelah agen gagal dua kali).
> **Ronde 11 (25 Agustus 2026 @ `80e95bb`, pasca Batch 16):** kualitas terkonvergensi — ditemukan **0 masalah Tinggi, 3 Sedang, dan 11 Rendah baru**; NAMUN audit integritas lagi-lagi menemukan item tercatat [x] yang tidak/belum tereksekusi (S110, S111, R124 + R125 parsial — agen batch16-pengawasan tak pernah diluncurkan). Lihat [bagian 5.14](#514-re-review-ronde-11--temuan-baru-pasca-batch-16-bahan-batch-17). Tema dominan ronde ini: interaksi antar-fix (Escape × Modal Manager), sisa polish a11y kontrol render-JS, dan konsistensi kontrak escape/token. Seluruhnya dieksekusi di **Batch 17** (25 Agustus 2026, test-first; gerbang pengawasan + admincore oleh koordinator, settings via agen — sekaligus menuntaskan 4 item tertinggal Batch 16).
> **Ronde 12 (25 Agustus 2026 @ `5095c75`, pasca Batch 17):** review berurutan oleh koordinator (tanpa agen). Kode makin stabil - ditemukan **0 masalah Tinggi, 1 Sedang, dan 2 Rendah baru** (volume terkecil sejak ronde pertama); NAMUN satu klaim guard lama terkoreksi: larangan z-index ≥1000 ternyata hanya mengunci tiga lokasi spesifik, bukan folder-wide (admin-base.css memuat 7 literal legacy ≥9998 - S116). Lihat [bagian 5.15](#515-re-review-ronde-12--temuan-baru-pasca-batch-17-bahan-batch-18). Plafon folder-wide kini PERSIS aktual (hex 99/99, rgba 89/89). Seluruhnya dieksekusi di **Batch 18** (25 Agustus 2026, test-first oleh koordinator: 7 test MERAH dulu, termasuk guard folder-wide z-index baru; token tangga stacking --z-bottom-bar/--z-hint/--z-modal-overlay/--z-topbar-floating ditambahkan theme.css; .toast-container naik ke --z-toast memperbaiki latent tie dengan onboarding).
> **Ronde 13 (25 Agustus 2026 @ `6507041`, pasca Batch 18):** review berurutan oleh koordinator. Stabil - ditemukan **0 masalah Tinggi, 1 Sedang, dan 2 Rendah baru**: file mati admin-tailwind.css 67KB + definisi ganda .toast-container lintas-file yang hanya aman karena urutan muat (S117), target-blank tanpa noopener (R146), register_confirm tanpa noindex padahal URL membawa username (R147). Lihat [bagian 5.16](#516-re-review-ronde-13--temuan-baru-pasca-batch-18-bahan-batch-19). Seluruhnya dieksekusi di **Batch 19** (25 Agustus 2026, test-first oleh koordinator: file mati dihapus, z-index toast dikunci satu tempat, noopener ×2, noindex register_confirm).
> **Tujuan:** acuan perbaikan UI/UX tahap selanjutnya. Setiap temuan punya ID unik (`T`=Tinggi, `S`=Sedang, `R`=Rendah, `P`=Keputusan Produk, `G`=Positif) agar mudah dirujuk di commit/issues (mis. `fix(uiux): T2 …`).

---

## Cara memakai & mencrosscheck laporan ini

1. **Setiap temuan punya:** lokasi `file:line`, kutipan bukti kode, uraian masalah, dampak ke pengguna, dan rekomendasi perbaikan konkret.
2. **Nomor baris bisa bergeser** setelah kode diedit. Jika lokasi tidak cocok, cari dengan **teks bukti yang dikutip** (mis. grep `Error message here`) — itu yang stabil.
3. Kolom **Status** pada [Rekap Tracking](#rekap-tracking) di bagian akhir dipakai untuk menandai progres: `[ ]` belum → `[x]` selesai (cantumkan hash commit).
4. Estimasi usaha: **XS** < 15 menit · **S** < 1 jam · **M** beberapa jam · **L** > 1 hari.

---

## 0. Konteks & Batasan Review

| Fakta | Konsekuensi |
|---|---|
| Interface ujian siswa **bukan halaman web** — ia aplikasi Android native (Kotlin: `android/app/src/main/java/com/examvan/app/ExamViewerActivity.kt`). Server tidak punya halaman ujian web. | Seluruh UX saat ujian berlangsung (timer, navigasi soal, auto-save, anti-cheat) **di luar cakupan laporan ini** — perlu review terpisah ke kode Kotlin client. |
| Area web terbagi dua: **admin** (guru/pengawas: dashboard, submissions, pengawasan, settings) dan **publik** (landing, register, forgot/reset password, download APK, hasil ujian). | Temuan dikelompokkan per area, tapi masalah sistemik (design token, modal, bahasa) melintasi keduanya. |
| `webui/templates/admin/base.html` **tidak pernah dirender** — dilewati sebagai "reference file" oleh `cmd/server/main.go:307-309`. Semua halaman admin adalah dokumen standalone yang memakai `partials/head.html` + `partials/nav.html`. | Base.html masih dirawat tapi sudah **drift** dari nav.html (lihat S16/R-drift). Ini sumber regresi senyap; nasibnya harus diputuskan (lihat Rencana Eksekusi tahap 4). |
| App adalah **dark-by-design** (`theme.css`: `color-scheme: dark`); palet light ada tapi sengaja tidak otomatis (dokumentasinya jujur di komentar). | Keputusan sah; masalahnya adalah *dead code* tanpa mekanisme aktivasi (S17). |

**Ringkasan vonis:** fondasi di atas rata-rata aplikasi sekelas (a11y dasar tertanam, higiene XSS rapi, tabel mobile-first, `prefers-reduced-motion` diseriusi). Namun ada **11 masalah Tinggi** yang langsung dirasakan pengguna, plus 3 penyakit sistemik: **bahasa campur EN/ID**, **design token tidak ditegakkan**, dan **tiga sistem modal berjalan paralel**.

---

## 1. Temuan PRIORITAS TINGGI (T1–T11)

> Perbaikan berdampak langsung dan terlihat oleh pengguna; mayoritas usahanya kecil.

---

### T1 — Registrasi tanpa konfirmasi password
- **Prioritas:** 🔴 Tinggi · **Usaha:** S · **Area:** Publik · **Status:** `[ ]`
- **Lokasi:** `webui/templates/public/register.html:276-289`
- **Masalah:** Form registrasi hanya punya SATU field "Password" (plus strength meter). Tidak ada field "Ulangi Password". Bandingkan `reset_password.html:73-82` yang sudah benar: punya "Ulangi Password Baru" + validasi mismatch secara live.
- **Bukti:**
  ```html
  <!-- register.html — hanya ini -->
  <input type="password" id="regPassword" ...>  <!-- + password-strength meter -->
  <!-- tidak ada input konfirmasi sama sekali (grep "ulangi/konfirmasi" = kosong) -->
  ```
- **Dampak:** Guru/staff salah ketik password → akun terkunci sejak hari pertama → harus melewati alur OTP email. Hambatan besar bagi pengguna awam di momen paling penting (adopsi produk).
- **Rekomendasi:** Duplikat pola yang sudah ada di `reset_password.html`: field kedua "Ulangi Password" + indikator mismatch live + blokir submit saat tidak cocok.

---

### T2 — Kotak error PALSU terlihat sejak modal unggah aplikasi dibuka
- **Prioritas:** 🔴 Tinggi · **Usaha:** XS · **Area:** Admin (Settings) · **Status:** `[ ]` ✅ **terverifikasi manual**
- **Lokasi:** `webui/templates/admin/settings.html:2033-2036` (+ logika di `settings-system-apps.js`)
- **Masalah:** `div#uploadError` di-inline-style dengan `display:flex` dan TIDAK punya `display:none`. JS hanya menyembunyikannya di `closeUploadModal()` dan di awal `submitUpload()` — sehingga saat modal pertama kali dibuka, kotak error merah sudah tampil berisi teks placeholder **bahasa Inggris**: `"Error message here"`.
- **Bukti:**
  ```html
  <div id="uploadError" style="margin-bottom:20px; padding:16px 20px; border-radius:12px;
       background:rgba(239,68,68,0.15); ... color:#fca5a5; font-size:14.5px; ...
       display:flex; align-items:flex-start; gap:12px; ...">   <!-- ← tanpa display:none -->
    <svg>...</svg>
    <span id="uploadErrorText" style="line-height:1.5;">Error message here</span>
  </div>
  ```
- **Dampak:** Guru membuka modal unggah dan langsung "disambut" error merah palsu berbahasa campur — tampilan rusak + kesan produk setengah jadi dalam satu layar.
- **Rekomendasi:** Tambahkan `display:none` ke inline style (atau class `.hidden`), kosongkan `#uploadErrorText` saat membuka modal, dan pastikan fungsi open/close yang mengatur visibility.

---

### T3 — Skor hasil ujian tanpa status Lulus/Belum & tanpa KKM
- **Prioritas:** 🔴 Tinggi · **Usaha:** M · **Area:** Publik (Hasil) · **Status:** `[ ]`
- **Lokasi:** `webui/templates/public/hasil.html:791-797` (threshold warna) & `456-458`
- **Masalah:** Warna badge skor murni dari threshold hard-coded (`pct >= 70` hijau, `>= 40` kuning, merah di bawahnya). Grep seluruh template publik: kata **"lulus" dan "KKM" tidak ada sama sekali**, tidak ada legenda warna, dan tidak ada state eksplisit "belum dikoreksi".
- **Dampak:** Siswa melihat angka merah tanpa tahu apakah itu "belum lulus", "belum dikoreksi", atau sekadar styling. Ambang 70 hard-coded juga bisa **bertentangan dengan KKM sekolah** masing-masing.
- **Rekomendasi:**
  1. Tampilkan label status eksplisit: `Lulus` / `Belum Lulus` / `Belum Dikoreksi` (badge teks, bukan cuma warna).
  2. Jadikan ambang lulus **setting** (per ujian atau global admin), bukan konstanta JS.
  3. Tambahkan legenda warna kecil di dekat rekap skor.

---

### T4 — Halaman Hasil tidak punya entry point di navigasi publik
- **Prioritas:** 🔴 Tinggi · **Usaha:** S · **Area:** Publik · **Status:** `[ ]`
- **Lokasi:** `webui/templates/public/shared.html:28-36` (nav), `index.html`, `download.html`
- **Masalah:** Navbar publik hanya berisi "Download App / Daftar / Masuk". Tidak ada link "Lihat Hasil Ujian" di mana pun, dan landing tidak menjelaskan cara siswa mengecek nilai.
- **Dampak:** Fitur hasil ujian (yang justru dibuka untuk siswa) harus diakses dengan **URL token mentah** — siswa praktis tidak akan pernah menemukannya tanpa diberi tahu URL persis.
- **Rekomendasi:** Tambah menu "Cek Hasil Ujian" di navbar publik → halaman kecil berisi input token ujian → redirect ke `/hasil/<token>`. Sekalian jelaskan di landing bahwa nilai dicek via token dari pengawas.

---

### T5 — CTA salah pada state "hasil dinonaktifkan"
- **Prioritas:** 🔴 Tinggi · **Usaha:** XS · **Area:** Publik (Hasil) · **Status:** `[ ]`
- **Lokasi:** `webui/templates/public/hasil.html:114-127`
- **Masalah:** Ketika hasil dinonaktifkan, pesannya menyuruh "hubungi pihak sekolah" — tetapi tombol utama satu-satunya adalah **"Login Webapp Admin"**.
- **Dampak:** Siswa (yang justru menerima pesan ini) dibawa ke halaman login admin yang tidak punya kredensial. Dead end yang mengecoh.
- **Rekomendasi:** Ganti CTA menjadi "Kembali ke Beranda" dan/atau halaman cek hasil (lihat T4); teks kontak sekolah cukup sebagai informasi sekunder.

---

### T6 — Pesan error auto-hilang 5–8 detik, tanpa `role="alert"`/aria-live
- **Prioritas:** 🔴 Tinggi · **Usaha:** S · **Area:** Publik + Admin (Login) · **Status:** `[ ]`
- **Lokasi:**
  - `register.html:403-410`, `register_confirm.html:344-351`, `forgot_password.html:94-101`, `reset_password.html:146-153` (auto-hide 5–8 dtk)
  - `login.html:136-144` — selector `.flash-messages` mencakup `flash-error` (baris 57): SEMUA flash termasuk "username/password salah" di-fade-out otomatis 5 dtk
- **Masalah:** Pesan error hilang sendiri tanpa cara memunculkan kembali (kecuali re-submit form), dan tidak diumumkan ke screen reader (tidak ada `role="alert"` / `aria-live`).
- **Dampak:** Pengguna lambat membaca (atau pengguna screen reader) kehilangan pesan penting seperti "username sudah dipakai" / "password salah" — tampak seperti "tombolnya mati".
- **Rekomendasi:** **Error jangan auto-dismiss** (pesan sukses boleh). Tambahkan `role="alert"` pada kontainer error. Pola toast admin (`showToast` + `aria-live`) sudah benar — samakan perilakunya.

---

### T7 — Font design system tidak pernah dimuat di halaman admin
- **Prioritas:** 🔴 Tinggi · **Usaha:** XS · **Area:** Admin · **Status:** `[ ]`
- **Lokasi:** `webui/templates/admin/partials/head.html:1-9` + `theme.css:47-48`; pembanding `login.html:16` (yang benar)
- **Masalah:** Token `--font-sans: 'Plus Jakarta Sans', 'Inter', ...` dan `--font-display: 'Outfit', ...` dideklarasikan dan dipakai (`body { font-family: var(--font-sans) }` di `admin-base.css:15`; heading di `admin-base.css:58`) — tetapi hanya `login.html` yang memuat `public_fonts` (Google Fonts + preconnect via `shared.html:7-9`). Dashboard/submissions/pengawas/settings **tidak punya `<link>` font sama sekali**.
- **Dampak:** Seluruh panel admin diam-diam fallback ke `system-ui`, sementara login & halaman publik pakai Outfit/Jakarta → inkonsistensi visual antar-halaman, dan token font menjadi menyesatkan.
- **Rekomendasi:** Muat font (partial `public_fonts`) di `partials/head.html`, atau lebih baik: self-host kedua font (subset latin) agar tidak bergantung Google saat jaringan sekolah offline — konsisten dengan skenario LAN.

---

### T8 — Polling antrean pengawas me-render ulang tbody penuh tiap 5 detik
- **Prioritas:** 🔴 Tinggi · **Usaha:** M · **Area:** Admin (Pengawasan) · **Status:** `[ ]`
- **Lokasi:** `webui/templates/admin/pengawas_detail.html:1909` (`setInterval(loadApprovals, 5000)`) & `:1270` (`tbody.innerHTML = html`)
- **Masalah:** Tiap 5 detik SELURUH isi tbody antrean diganti dengan innerHTML baru — tanpa membandingkan data lama/baru.
- **Dampak:** Tap guru pada tombol **Izinkan/Tolak** sering mendarat di DOM yang baru saja diganti (ketukan "hilang", tombol seolah mati), hover/fokus reset. Di HP dengan jaringan lambat, ini terasa seperti aplikasi tidak responsif **persis di momen paling kritis: menerima siswa masuk ujian**.
- **Rekomendasi:**
  1. Skip re-render bila payload identik (hash/versi data).
  2. Update per-baris (bandingkan status per approval id), bukan replace tbody.
  3. Jangan replace DOM saat ada interaksi berlangsung — flag `approvalLoading` sudah ada, manfaatkan untuk menunda render.
  4. Catatan positif: refresh senyap + stempel waktu "Diperbarui HH:MM:SS" (lihat G8) sudah benar — masalahnya murni strategi replace DOM.

---

### T9 — Kontras abu-abu `#64748b` gagal WCAG AA untuk teks kecil fungsional
- **Prioritas:** 🔴 Tinggi · **Usaha:** S · **Area:** Admin · **Status:** `[ ]`
- **Lokasi:** `static/css/tailwind/output.css:401` (placeholder pencarian), `:1994` (`.role-chip` 13px), `:2033` (`.form-hint` 10px); `base.html:28` + duplikat di `dashboard.html:15` (`.dropdown-user-instansi` 11.2px)
- **Masalah:** `#64748b` (relatif luminance ≈ 0.17) di atas permukaan gelap (`#0d0d1e`–`#14141f`) menghasilkan rasio ≈ **3.8–4.2 : 1** — di bawah ambang WCAG AA 4.5:1 untuk teks normal. Dipakai justru untuk teks fungsional kecil: hint form (10px), chip role, nama instansi.
- **Dampak:** Hint form, chip role, dan nama instansi sulit dibaca bagi pengguna mata lemah — dan ini teks penjelas, bukan dekorasi.
- **Rekomendasi:** Naikkan ke minimal ~5:1. `theme.css` **sudah punya token yang tepat**: `--color-text-muted: #a0aec0` (≈6.3:1 di permukaan gelap) — gunakan token itu, hapus nilai `#64748b` yang hard-coded. Sekalian audit semua pasangan warna/teks < 4.5:1.

---

### T10 — Tombol ✕ toast invisible bagi keyboard + validasi upload hanya-lewat-toast
- **Prioritas:** 🔴 Tinggi · **Usaha:** S · **Area:** Admin · **Status:** `[ ]`
- **Lokasi:** `output.css:1160-1176` (`.toast-close`) dan `admin.js:62-83` (validasi upload)
- **Masalah (dua terkait feedback/error):**
  1. `.toast-close { opacity: 0 }`, hanya muncul saat `.toast:hover`; reveal non-hover hanya ada di breakpoint mobile. **Tidak ada rule `:focus-visible`** — pengguna keyboard Tab ke tombol yang tak terlihat (opacity:0 juga menyembunyikan focus ring). Ukuran 24×24px < 44px.
  2. Validasi form upload (nama wajib, file wajib, token 8 char A-Z0-9) baru dieksekusi **saat submit** dan hanya menampilkan `showToast(...,'error')` — toast lenyap dalam 5 detik.
- **Dampak:** Keyboard user mendapat tombol hantu; guru harus submit berulang untuk tahu field mana yang salah, dan pesannya sudah hilang saat ia scroll kembali ke form.
- **Rekomendasi:**
  1. CSS: `.toast-close:focus-visible { opacity: 1; outline: 2px solid var(--color-primary-light); }` + perbesar hit-area ≥ 44px (padding transparan tidak mengubah visual).
  2. Pindahkan validasi upload ke level field (event `blur`/`input`) dengan **pesan inline per field** (lihat juga S19) — toast hanya sebagai pelengkap.

---

### T11 — Hapus permanen = ikon tempat sampah 34px tanpa label (Halaman Hasil Ujian)
- **Prioritas:** 🔴 Tinggi · **Usaha:** XS · **Area:** Admin (Submissions) · **Status:** `[ ]`
- **Lokasi:** `webui/templates/admin/submissions.html:316-317`; pembanding `admin.js:1528-1534`
- **Masalah:** Tombol hapus permanen hasil ujian hanya ikon tempat sampah dengan `min-width:34px; padding:4px` — di bawah standar target sentuh 44px dan rawan salah ketuk. Tim sendiri sudah mengakui pola ini bermasalah: komentar `admin.js:1528-1534` ("title does not appear on tap") dan halaman lain **sudah** diperbaiki — halaman Hasil Ujian tertinggal.
- **Dampak:** Aksi IRREVERSIBLE (hapus permanen hasil ujian siswa) adalah aksi yang PALING tidak boleh salah ketuk — justru di sini tombolnya paling kecil.
- **Rekomendasi:** Samakan dengan pola halaman lain yang sudah benar (teks label atau tombol ≥44px), tetap dengan `showConfirm` yang menjelaskan konsekuensi (sudah ada).

---

## 2. Temuan SEDANG RONDE 1 (S1–S22) ✅ seluruhnya tereksekusi Batch 1–4

### 2a. Keselamatan aksi

### S1 — Toggle status ujian (aktif/nonaktif) tanpa konfirmasi, petunjuknya tooltip-only
- **Usaha:** S · **Area:** Dashboard · **Status:** `[ ]`
- **Lokasi:** `dashboard.html:488-504`
- **Masalah:** `<span class="status-badge status-active" onclick="toggleExam({{$exam.ID}})" title="Klik untuk menonaktifkan ujian">Aktif</span>` — elemen badge berperilaku tombol destruktif-ringan; satu-satunya petunjuk interaksi ada di `title` yang **tidak muncul di sentuhan HP**.
- **Dampak:** Guru dapat menonaktifkan ujian secara tak sengaja saat scroll/zoom daftar di HP; siswa yang hendak login ujian langsung terblokir. Tanpa dialog konfirmasi apa pun.
- **Rekomendasi:** Ubah menjadi komponen switch yang jelas + `showConfirm` sebelum mengubah status; minimal tambah `role="button"`, `aria-pressed`, dan konfirmasi.

### S2 — "Batal" di modal konfigurasi soal membuang semua perubahan tanpa peringatan
- **Usaha:** S · **Area:** Dashboard · **Status:** `[ ]`
- **Lokasi:** `dashboard.html:915-916`; penyimpanan massal di `admin.js:1207` (`saveQuestionsConfig`)
- **Masalah:** Modal berisi banyak input (bobot per tipe soal, level keamanan, jumlah soal per tipe) yang disimpan **massal** — tombol Batal langsung menutup tanpa guard unsaved-changes.
- **Dampak:** Satu klik menghapus pekerjaan mengatur puluhan soal. Padahal `showConfirm` tersedia dan sudah dipakai baik di tempat lain.
- **Rekomendasi:** Track dirty-state; saat Batal/Escape/backdrop dengan perubahan → `showConfirm("Buang perubahan?")`.

### S3 — Kode voucher diinterpolasi mentah ke atribut `onclick` (pecah + celah injeksi)
- **Usaha:** S · **Area:** Settings/Voucher · **Status:** `[ ]`
- **Lokasi:** `settings-vouchers.js:113,130,135`
- **Bukti:** `onclick="copyCode(this, '${v.code}')"` dan `deleteVoucher(${v.id}, '${v.code}')` — tanpa escaping, padahal `admin-core.js` menyediakan `escapeHtml` dan `jsEscape`.
- **Dampak:** Kode voucher yang mengandung karakter kutip/backslash memutus handler salin/hapus; sekaligus celah injeksi atribut (vektor terbatas karena admin-only, tetap harus dibereskan).
- **Rekomendasi:** Escape via `jsEscape`, atau lebih baik: simpan data di attribute/data-* + `addEventListener` (hilangkan inline onclick sepenuhnya).

### 2b. Konsistensi bahasa, istilah & pola

### S4 — Bahasa Inggris–Indonesia bercampur di UI berbahasa Indonesia
- **Usaha:** S · **Area:** Semua · **Status:** `[ ]`
- **Daftar lokasi terkonfirmasi:**
  - `dashboard.html:747-751` — opsi level keamanan: `"Low — Bisa Keluar (Tanpa Submit)"` (dan padanan Medium/High) — guru non-teknis tidak tentu paham "Low/Medium/High"
  - `dashboard.html:897` — tombol `"Set All Bobot"`
  - `dashboard.html:407` — filter status `"Ditombstone"` (istilah internal teknis!)
  - `settings.html:1589` — `"SaaS & SMTP Email Settings"`; `:1805` — `"Customizing Footer"`; `:1813` — placeholder `"© 2026 EXAMVAN Team. All rights reserved."`
  - `settings-system-apps.js:2035` — placeholder `"Error message here"` (juga T2)
  - Publik: `shared.html:29` nav `"Download App"` vs `index.html:16` CTA `"Unduh Client Ujian"` vs `download.html` "Pusat Unduhan" — tiga istilah untuk konsep sama; `hasil.html:124` `"Login Webapp Admin"`
- **Dampak:** Kesan produk setengah jadi; guru non-teknis terhambat istilah teknis; istilah tak seragam menambah beban kognitif.
- **Rekomendasi:** Tetapkan kamus istilah (ID untuk semua UI; EN hanya bila benar-benar standar, mis. PDF/SaaS). Sweep bertahap mulai dari yang user-facing terluas (level keamanan Low/Med/High → "Rendah/Sedang/Tinggi").

### S5 — Dua pola pencarian: Enter-only vs live-search
- **Usaha:** XS · **Area:** Pengawasan vs Settings · **Status:** `[ ]`
- **Lokasi:** `pengawas.html:58-59` (`onkeyup="if(event.key==='Enter') loadPengawasExams()"`) vs `admin.js:1427-1432` (live-search debounced 300ms di Pengaturan)
- **Dampak:** Di halaman Pengawasan pengguna mengetik lalu menunggu hasil yang tak pernah datang tanpa tahu harus tekan Enter.
- **Rekomendasi:** Samakan ke live-search debounce di semua input pencarian (helper bersama di `admin-core.js`).

### S6 — Dua pola input OTP untuk konsep yang sama
- **Usaha:** M · **Area:** Publik · **Status:** `[ ]`
- **Lokasi:** `register_confirm.html:238-243` (6 kotak terpisah; `autocomplete="one-time-code"` hanya di kotak PERTAMA — autofill OTP SMS Android umumnya gagal mengisi 6 kotak) vs `reset_password.html:60-63` (satu input biasa)
- **Dampak:** Pengguna yang baru berhasil di satu alur menghadapi interaksi berbeda di alur lain; autofill SMS rusak di jalur registrasi.
- **Rekomendasi:** Samakan pola. Opsi terbaik untuk autofill Android: SATU input `autocomplete="one-time-code"` `inputmode="numeric"` `maxlength="6"` (visual bisa tetap dibuat 6 segmen via letter-spacing), atau isi-otomatis 6 kotak dari paste (paste 6-digit sudah didukung — G4).

### S7 — Judul halaman tidak sinkron dengan label navigasi
- **Usaha:** XS · **Area:** Admin · **Status:** `[ ]`
- **Lokasi:** `dashboard.html:242` (`<h1 class="sr-only">Dashboard Admin</h1>`) vs `nav.html:18` (menu aktif bernama "Daftar Ujian")
- **Dampak:** Membingungkan orientasi — terutama pengguna screen reader yang membaca judul halaman lalu mendengar menu aktif dengan nama berbeda.
- **Rekomendasi:** Sinkronkan (pilih satu nama kanonik, mis. "Daftar Ujian").

### 2c. Mobile

### S8 — Font mikro di halaman hasil (sampai ≈8,8px)
- **Usaha:** S · **Area:** Publik (Hasil) · **Status:** `[ ]`
- **Lokasi:** `hasil.css:760` (`.header-badge { font-size: 0.55rem }` ≈ 8.8px!), `:519-527` (`.stat-mini-label` 0.65rem ≈ 10.4px), `:556-560` (`.key-card-weight`)
- **Dampak:** Teks pendamping nilai/status praktis tak terbaca di layar 5–6 inci tanpa zoom — dan mayoritas pengguna halaman ini adalah **siswa di HP Android**.
- **Rekomendasi:** Batas bawah 12px (0.75rem) untuk semua teks informatif; badge header cukup diperkecil padding-nya, bukan font-nya. (Admin juga punya 28 deklarasi `10px` — lihat R-audit di bagian rendah.)

### S9 — Scroll bersarang di rincian jawaban (scroll-trap klasik)
- **Usaha:** S · **Area:** Publik (Hasil) · **Status:** `[ ]`
- **Lokasi:** `hasil.css:412-419` — `.answer-grid { max-height: 380px; overflow-y: auto; }` di dalam kartu yang halamannya juga scroll
- **Dampak:** Di sentuhan, siswa mengira daftar jawaban "habis" padahal terpotong pada batas 380px; jumlah soal banyak membuat area aktif makin kecil.
- **Rekomendasi:** Di mobile, hapus `max-height` (biarkan mengembang penuh); di desktop boleh dipertahankan dengan indikator visual ada-lanjutan (fade/scrollbar selalu tampil).

### S10 — Target sentuh < 44px pada kontrol inti
- **Usaha:** S · **Area:** Admin · **Status:** `[ ]`
- **Daftar lokasi:** `.status-badge[role=button]` min-height 34px di mobile (`admin-base.css:901`) · `.toast-close` 24px (`output.css:1160`, juga T10) · `.search-clear-btn` 24×28 (`output.css:401` sekitar) · `.modal-close` 32×32 (`output.css:2183-2184`) · `.btn-sm`/`.btn-icon` di-cap 40px (`admin-base.css:516,520`)
- **Catatan positif:** hamburger & dropdown-item sudah benar 44px (`admin-base.css:498-500`).
- **Rekomendasi:** Audit satu kali semua kontrol klikabel: naikkan ke ≥44×44px (boleh via padding/hit-area transparan tanpa mengubah visual).

### 2d. Halaman unduh (download)

### S11 — Panduan siswa awam bercampur instruksi IT (factory reset + ADB) dalam satu kartu
- **Usaha:** S · **Area:** Publik (Download) · **Status:** `[ ]`
- **Lokasi:** `download.html:569-609` — "Metode 1 Student" dan "Metode 2 Kiosk" (factory reset + perintah terminal `adb shell dpm set-device-owner ...` di `:604`) berdempetan dalam satu kartu tanpa pemisahan kuat.
- **Dampak:** Siswa/orang tua awam melihat instruksi factory-reset dan terminal lebih dulu — menakutkan dan menaikkan beban kognitif.
- **Rekomendasi:** Pisahkan total: section/tab "Untuk Siswa" vs "Untuk Admin IT Sekolah" (collapsible terpisah), dengan peringatan "metode ini untuk perangkat sekolah yang dikelola IT".

### S12 — Instruksi "sumber tidak dikenal" terlalu tipis + alamat server tidak dijelaskan asalnya
- **Usaha:** S · **Area:** Publik (Download) · **Status:** `[ ]`
- **Lokasi:** `download.html:575-583`
- **Masalah:** Hanya satu kalimat 'aktifkan opsi "Izinkan Pemasangan dari Sumber Tidak Dikenal"' tanpa jalur layar aktual (jalurnya BERBEDA antar merk: Samsung/Xiaomi/vivo/Oppo punya menu berbeda). Langkah 2 menyuruh "masukkan alamat server sekolah Anda" tanpa menjelaskan dari mana mendapatkannya.
- **Dampak:** Titik gagal instalasi tertinggi justru yang paling tidak didetailkan — siswa berhenti di tengah alur.
- **Rekomendasi:** (a) Detail langkah umum + catatan per-merk (accordion kecil). (b) Tambahkan: "Alamat server & token diberikan pengawas/panitia ujian" + contoh format.

### S13 — Tombol unduh tanpa state loading/disabled
- **Usaha:** XS · **Area:** Publik (Download) · **Status:** `[ ]`
- **Lokasi:** `download.html:527-530, 545, 655, 734`; probe fetch di `824-851`
- **Masalah:** `onclick` melakukan probe `fetch` dulu tanpa mengubah tampilan tombol.
- **Dampak:** Di jaringan sekolah lambat tombol terasa mati → pengguna tap berkali-kali (multiple request unduhan).
- **Rekomendasi:** Saat probe: `disabled` + spinner/teks "Memeriksa..." ; kembalikan setelah selesai/gagal.

### S14 — Tab platform tidak accessible dan tidak deep-linkable
- **Usaha:** S · **Area:** Publik (Download) · **Status:** `[ ]`
- **Lokasi:** `download.html:483-495` + `789-799`
- **Masalah:** Tombol tab tanpa `role="tablist"/tab/aria-selected`; state hanya via `style.display`; tidak tersimpan di hash/URL.
- **Dampak:** Screen reader tidak tahu ada tab; guru tidak bisa membagikan link langsung "bagian Windows" ke siswa PC (reload selalu kembali ke Android).
- **Rekomendasi:** ARIA tabs pattern + simpan tab aktif di hash (`#windows`) atau query param; baca saat load.

### 2e. Design system & arsitektur front-end

### S15 — Design token tidak ditegakkan (nilai ad-hoc jauh lebih banyak dari token)
- **Usaha:** L · **Area:** Admin CSS · **Status:** `[ ]`
- **Lokasi:** `admin-base.css` — hasil hitung: **40 hex + 93 `rgba()` hard-coded vs hanya 58 `var()`**
- **Detail:**
  - Mayoritas rgba adalah surface putih-transparan ad-hoc (0.02–0.12) padahal token `--color-surface` / `--color-surface-hover` tersedia nyaris tak dipakai.
  - Hex tanpa padanan token: `#fca5a5`, `#a5b4fc`, `#c7d2fe`, `#94a3b8`, `#fbbf24`.
  - Radius: **10 nilai berbeda** (8px ×8, 10px ×5, 12px ×4, 4px ×3, 6px, 3px, 999px…) padahal `--radius-sm/md/lg/xl` ada di `theme.css:37-40` — pemakaian `--radius*` di admin-base.css: **0**.
  - Box-shadow: 5 varian ad-hoc vs token `--shadow-card/sm/lg`.
- **Dampak:** Inkonsistensi visual perlahan meningkat setiap fitur baru; tema sulit diubah (lihat S17).
- **Rekomendasi (bertahap, bukan big-bang):**
  1. Tambah token yang kurang (`--radius-xs`, `--danger-text`/`#fca5a5`, `--primary-text`/`#a5b4fc`, dst.).
  2. Migrasi pelan: setiap kali menyentuh sebuah rule, ganti nilai literal → token.
  3. Pasang stylelint dengan aturan `declaration-property-value-disallowed-list` untuk hex/rgba di luar theme.css.

### S16 — Tiga sistem modal paralel + markup referensi yang drift
- **Usaha:** M · **Area:** Admin · **Status:** `[ ]`
- **Fakta:**
  1. Trap inline `__openModal`/`__closeModal` (`base.html:277-314`, pola sama disalin ke halaman-halaman standalone).
  2. Global Modal Manager berbasis MutationObserver (`admin-core.js:676-830`) — juga trap Tab, Escape, restore focus.
  3. `showConfirm` membuat trap ketiga (`admin-core.js:341-401`).
  - Ketiganya BERTABRAKAN: double-handling Escape/Tab antara handler inline dan manager; tiga tempat untuk bug aksesibilitas yang sama.
  - Drift markup: `base.html:69` memakai class `active-dropdown-item` yang **tidak terdefinisi di CSS mana pun** (CSS hanya mengenal `.dropdown-item.active`, `admin-base.css:533-538`); hamburger di `base.html:55` tanpa `aria-label`/`aria-haspopup` sementara `nav.html:31` punya.
- **Dampak:** Perilaku modal sulit diprediksi; perbaikan a11y harus dilakukan 3×; base.html yang "dirawat" ternyata tak pernah dirender dan sudah salah.
- **Rekomendasi:** Jadikan Global Modal Manager satu-satunya (refactor call site `__openModal` → API manager); putuskan nasib base.html (lihat tahap 4 Rencana Eksekusi).

### S17 — Palet light "mati": 30 baris token tanpa mekanisme toggle
- **Usaha:** XS (putuskan) · **Area:** Theme · **Status:** `[ ]`
- **Lokasi:** `theme.css:56-91`
- **Fakta:** `:root[data-theme="light"]` didefinisikan lengkap, tapi TIDAK ADA satu pun kode/template yang men-set `data-theme` (grep kosong), dan `prefers-color-scheme` tidak dipakai. Komentarnya jujur: disimpan untuk "FUTURE explicit toggle" — sah sebagai dark-by-design, tetapi saat ini dead code yang bisa dianggap aktif.
- **Rekomendasi:** Pilih satu: (a) hapus sampai benar-benar dibutuhkan, atau (b) implementasikan toggle + persistensi. Jangan biarkan menggantung. Catatan terkait: `login.html:86` Turnstile `data-theme="dark"` hard-coded — ikut token jika suatu hari light aktif.

### 2f. Aksesibilitas lain

### S18 — Nav tanpa `aria-current`; hamburger tanpa `aria-expanded`
- **Usaha:** XS · **Area:** Admin · **Status:** `[ ]`
- **Lokasi:** `partials/nav.html:18-25` (active state murni class CSS; `admin-base.css:531-532`) — padahal `settings-tabs.html` sudah benar men-set `aria-current` (`settings.html:2289`). Hamburger `nav.html:31` punya `aria-label` + `aria-haspopup` tapi **tanpa `aria-expanded`**; `initMenuToggle` (`admin-core.js:279-282`) hanya toggle class `show`.
- **Rekomendasi:** Tambah `{{if ...}}aria-current="page"{{end}}` di nav (topbar & dropdown); update `aria-expanded` di toggle handler.

### S19 — Validasi form tidak punya error state per-field
- **Usaha:** M · **Area:** Semua · **Status:** `[ ]`
- **Lokasi:** contoh `base.html:119-154` (modal Ubah Password); grep `aria-invalid` di templates/: **kosong**
- **Masalah:** Semua form hanya mengandalkan validasi native + toast global; tidak ada class `.input-error`, `aria-invalid`, atau `aria-describedby` ke teks error.
- **Dampak:** Ketika toast hilang (dan error memang auto-dismiss — T6), tidak ada penanda field mana yang salah.
- **Rekomendasi:** Helper bersama di `admin-core.js`: `setFieldError(inputEl, msg)` yang men-set border merah + `aria-invalid="true"` + inject `<p role="alert" id="...-error">` + hubungkan `aria-describedby`. Gunakan di modal-modal utama dulu.

### S20 — Shortcut `Ctrl+F` / `Cmd+F` dibajak, tidak didokumentasikan
- **Usaha:** XS · **Area:** Admin · **Status:** `[ ]`
- **Lokasi:** `admin-core.js:476-480` (`case 'f': e.preventDefault(); …`); panel pintasan `base.html:270-275` hanya mencantumkan `/`, `⌘U`, `?`
- **Dampak:** Menimpa find-in-browser bawaan — fitur yang dipakai banyak orang; dan pembajakannya sendiri tak terdaftar di panel bantuan.
- **Rekomendasi:** Hapus binding `Ctrl+F` (shortcut `/` sudah cukup dan wajar), atau jika dipertahankan, wajib masuk daftar pintasan.

### 2g. Landing & halaman publik lain

### S21 — Slot statistik landing diisi kalimat panjang; hierarki hero berisik
- **Usaha:** S · **Area:** Publik (Landing) · **Status:** `[ ]`
- **Lokasi:** `index.html:44-60` (slot `.stat-val` ditata untuk angka besar 48px, diisi "3 Mode Keamanan", "< 3 Detik Deteksi Focus Loss & Submit"); breakpoint mobile 22px di `shared.html:812` — tetap multi-baris patah-patah.
- **Dampak:** Hero jadi ramai; kontennya pun ("deteksi focus loss", anti-cheat) berbahasa admin, padahal halaman juga dibaca siswa.
- **Rekomendasi:** Statistik bernilai bagi sekolah (mis. "Koreksi otomatis < 3 detik", "100% offline-capable", "N sekolah")—angka pendek, satu baris.

### S22 — Jargon teknis di atas fold landing
- **Usaha:** S · **Area:** Publik (Landing) · **Status:** `[ ]`
- **Lokasi:** `index.html:5-11` (badge "EXAMVAN v{{.version}} Rilis Terbaru"), copy utama menyebut "Managed Kiosk Mode Device Owner", `:114` "Zero-Friction Launch"
- **Dampak:** Informasi rilis build tak bermakna bagi pelanggan sekolah; jargon mengaburkan value proposition di detik pertama (terlihat juga di screenshot mobile).
- **Rekomendasi:** Badge versi → cukup di footer/changelog; hero copy fokus manfaat ("Ujian digital anti-cheat, koreksi otomatis, tanpa internet") — detail teknis dipindah ke halaman download/IT (sinergi dengan S11).

---

## 3. Temuan RENDAH RONDE 1 (R1–R16) — mayoritas tereksekusi; sisa terbuka: R4

> Mayoritas quick wins murah — kandidat "sekali PR kebersihan".

| ID | Masalah | Lokasi | Rekomendasi | Usaha | Status |
|----|---------|--------|-------------|-------|--------|
| R1 | `colspan="8"` pada tabel 6 kolom — baris loading & error tampak meleset | `pengawas_detail.html:1347` & `:1409` (nilai benar `colspan="6"` dipakai di `:1362`) | Ganti ke 6 | XS | `[ ]` |
| R2 | Helper skeleton MATI — `showSkeleton`/`showDashboardSkeletons` tak pernah dipanggil; dashboard & pengawas blank-flash saat render awal | `admin-core.js:404-438` (target `#examTableBody` tak ada pemanggil) | Panggil saat initial load, atau hapus | S | `[ ]` |
| R3 | Duplikasi push modul script — `settings-packages.js` di-push 2× → dimuat & dieksekusi dobel tiap buka tab Umum/Paket (risiko handler dobel) | `settings.html:2255-2256` (dua baris `files.push('settings-packages.js')` berturut-turut) | Hapus satu | XS | `[ ]` |
| R4 | Sisa `document.write` untuk ukuran file — rapuh, kosong bila JS gagal | `settings.html:2130` (ukuran app; pembanding aman `settings-system-apps.js:89`) · `download.html:524, 542, 652, 731` (ukuran APK, tanpa fallback noscript) | Render via JS DOM biasa | XS | `[ ]` |
| R5 | Placeholder "menit" dobel — input `placeholder="menit"` + span label "menit" persis di sebelahnya | `dashboard.html:477-478` | Kosongkan placeholder atau isi contoh angka | XS | `[ ]` |
| R6 | Hapus ujian: baris dianimasikan keluar lalu TETAP `location.reload()` — posisi scroll & halaman pagination hilang; animasi sia-sia | `admin.js:172-205` | Remove row dari DOM + update counter (tanpa reload) | S | `[ ]` |
| R7 | Pagination submissions minim: prev/next tanpa posisi "Halaman X dari Y"; anchor prev tetap `href` aktif walau class disabled | `submissions.html:326-335` (pembanding benar: `dashboard.html:548-562` menampilkan nomor halaman + "N dari M") | Samakan pola dashboard | S | `[ ]` |
| R8 | Kartu utuh clickable berisi link bersarang — `onclick="window.location=..."` di kartu yang di dalamnya ada `<a>Pantau</a>`; dua target interaktif bertumpuk | `pengawas.html:304` & `:325` | Jadikan judul kartu link tunggal, atau stopPropagation pada link | S | `[ ]` |
| R9 | Username "disanitasi diam-diam" — karakter ilegal dihapus langsung saat mengetik tanpa pesan aturan (placeholder hanya "min. 3 karakter") | `register.html:394-400` | Tampilkan aturan charset di hint + toast sekali saat sanitasi terjadi | XS | `[ ]` |
| R10 | Bug NaN statistik dashboard: `(stats.total - stats.active) ?? '0'` — `??` tak menangkap NaN hasil `undefined - undefined`. *Koreksi setelah verifikasi: komentar "30s interval" ternyata AKURAT — `admin.js:3876` memanggil `startAutoRefresh(30)`; default 120 hanya fallback* | `admin-core.js:535` | Guard eksplisit cek tipe number sebelum mengurangi | XS | `[x]` ✅ |
| R11 | Script dimuat sinkron tanpa `defer` (posisi akhir body, dampak kecil — tapi `defer` gratis & lebih aman urutan) | `base.html:315-316`; `login.html:148-149` (fingerprintjs.min.js 37KB) | Tambah `defer` | XS | `[ ]` |
| R12 | Skip-link pola rapuh: inline `left:-9999px` + override `!important` di theme.css untuk mereveal (diakui sendiri di komentarnya) | `base.html:33` + `theme.css:101-108` | Pindahkan ke class `.skip-link` biasa | XS | `[ ]` |
| R13 | Tiga `<h1>` dalam satu dokumen settings (dua sr-only + satu visible) — outline heading melompat-lompat | `settings.html:754, 1574, 1965` | Turunkan ke h2 (halaman lain sudah patuh, mis. dashboard 1 h1) | XS | `[ ]` |
| R14 | Turnstile `data-theme="dark"` hard-coded — salah bila light palette suatu hari diaktifkan | `login.html:86` | Ikut token tema | XS | `[ ]` |
| R15 | Kebersihan markup publik: `<script>` setelah `</html>` di partial footer; favicon dilink 2×; `register_confirm.html` TIDAK punya skip-link (halaman publik lain punya) | `shared.html:937-944` (footer), `:85` & `:911` (favicon), `register_confirm.html` | Rapikan; tambah skip-link | XS | `[ ]` |
| R16 | Grid 3 tab platform dipaksa 2 kolom di mobile — tab Linux jatuh sendirian setengah lebar | `public-mobile.css:112-116` (`grid-template-columns: 1fr 1fr`) | `repeat(auto-fit, minmax(110px, 1fr))` atau 3 kolom kecil | XS | `[ ]` |

---

## 4. KEPUTUSAN PRODUK (bukan bug, tapi perlu keputusan eksplisit)

### P1 — Peringkat & nilai terendah PUBLIK untuk siapa pun yang punya URL token
- **Lokasi:** `hasil.html:451` (+ `103-110`): rank 1–3 diberi warna emas/perunggu; rata-rata & nilai terendah publik.
- **Isu:** Siapa pun yang mengetahui/menebak URL token bisa melihat peringkat seluruh siswa termasuk nilai TERENDAH. Secara sosial-emosional berdampak ke siswa (perundungan berbasis nilai adalah risiko nyata di sekolah).
- **Opsi:** (a) peringkat hanya tampil untuk guru/admin, (b) peringkat anonim (nis saja), (c) biarkan — tapi jadilah keputusan sadar. Minimal: nilai terendah/rata-rata kelas hanya untuk guru.
- **✅ KEPUTUSAN (2026-08-23): opsi (c) — biarkan terbuka, secara sadar.** Lihat rekap "Keputusan produk".

### P2 — "Remember me" pada login admin belum ada
- **Lokasi:** `login.html:67-91` (form hanya username/password/turnstile; `autocomplete` sudah benar).
- **Isu:** Murni keputusan produk. Untuk perangkat guru pribadi, sesi lebih panjang nyaman; untuk perangkat lab bersama, sesi pendek lebih aman.
- **Opsi:** Checkbox remember-me (extend TTL cookie) + default sesi pendek.
- **✅ KEPUTUSAN (2026-08-23): tidak dibuat.** Sesi pendek dipertahankan demi keamanan perangkat bersama. Lihat rekap "Keputusan produk".

### P3 — Ambang warna skor 70/40 hard-coded (terkait T3)
- **Lokasi:** `hasil.html:791-797`
- **Isu:** Nilai KKM tiap sekolah berbeda; threshold warna global akan sering "salah" dari perspektif sekolah. Terkait erat dengan T3 — selesaikan bareng.

---

## 5. YANG SUDAH BAGUS — pertahankan & jadikan pola resmi

| ID | Hal | Bukti |
|----|-----|-------|
| G1 | `prefers-reduced-motion` ditangani serius: semua animasi/transition dipatuhkan, shimmer skeleton diganti background statis | `admin-base.css:273-283` |
| G2 | Higiene XSS & error handling rapi: `escapeHtml` konsisten di `showToast`/`showConfirm`, `jsEscape` untuk konteks atribut, `apiFetch` menormalkan error + event `api:error` terpusat (opt-out `suppressApiErrorToast`), guard `__proto__` — plus test suite | `admin-core.js:26-67, 115, 159, 245-251, 341-401`; `admin-core.test.mjs` |
| G3 | Mobile-first sungguhan: semua tabel bertransformasi jadi kartu dengan `data-label`; anti auto-zoom iOS (input 16px) di admin & login | `dashboard.html:78-235`, `submissions.html:13-123`, dst.; `admin-base.css:491-495`; `login.html:29-33` |
| G4 | Alur OTP registrasi matang: paste 6 digit sekaligus, navigasi panah, backspace antar kotak, aria-label per digit, cooldown resend persist di sessionStorage (salah kode tidak mengulang tunggu 60 dtk) | `register_confirm.html:318-327, 359-426` |
| G5 | Dialog konfirmasi custom berkualitas: focus trap, Escape/backdrop, dan pesan SELALU menjelaskan konsekuensi ("File PDF juga akan dihapus permanen"); hapus massal mendaftar nama ujian terpilih | `admin-core.js:341-401`; `admin.js:173-180`; `dashboard.html:963-969` |
| G6 | Halaman hasil tangguh: loading → error dengan "Coba Lagi", guard anti-race fetch pagination, empty-state terpisah ("belum ada peserta" vs "tidak ketemu"), print stylesheet gelap→terang untuk guru cetak rekap | `hasil.html:251-258, 281-284, 331-333`; `hasil.css:909-925` |
| G7 | Show/hide password implementasi benar: `aria-label` di-update dua arah, tombol 40×40px, swap ikon tanpa FOUC | `login.html:110`; `admin-core.js:336` |
| G8 | Higiene realtime di pengawas_detail: refresh senyap + stempel "Diperbarui HH:MM:SS", flag anti-overlap poll (`approvalLoading/approvalRerunPending`, `detailLoading/detailRerunPending`), error polling TIDAK menghapus data yang sudah tampil, antrean panjang dijelaskan jumlah tersembunyinya | `pengawas_detail.html:1321-1327, 1276-1278, 1210-1211` |
| G9 | Fondasi a11y dasar tertanam: skip-link WCAG 2.4.1 (dengan CSS fallback untuk halaman tanpa admin-core.js), `*:focus-visible` global 2px, `lang="id"`, title dinamis, touch-target disiplin (input 48–50px, blok audit khusus di hasil.css) | `theme.css:96-108`; `admin-base.css:95-99, 498-500`; `public-mobile.css:97, 141-142`; `hasil.css:927-933` |

> **Catatan:** G5 + G8 adalah bukti bahwa pola yang benar SUDAH ADA di codebase. Sebagian besar temuan di atas sebenarnya adalah *inkonsistensi terhadap pola terbaik milik sendiri* — perbaikan = menyeragamkan ke pola terbaik, bukan menemukan sesuatu yang baru.

---

## 5.5 RE-REVIEW RONDE 2 — Temuan baru pasca Batch 1–4

> **Tanggal:** 23 Agustus 2026 · **Basis kode:** `cbc837f` (working tree bersih) · **Metode:** 3 reviewer paralel (publik, admin, lintas-halaman), setiap temuan diverifikasi langsung ke file.
> Penomoran ID melanjutkan ronde 1 agar mudah dirujuk di commit (`fix(uiux): T12 …`).

### Status "penyakit sistemik" ronde 1

| Penyakit | Status | Bukti |
|---|---|---|
| Bahasa campur EN/ID | ✅ **Berres** (level UI string) | Grep massal Error/Success/Loading/Save/Cancel/dst.: temuan tersisa hanya komentar/ID/console. Sisa kosmetik → R26 |
| Design token tidak ditegakkan | ⚠️ **Sebagian** | CSS inti rapi + test batch4 ✓ (`admin-base.css` hex non-komentar turun ke 27, dikunci test); tapi template (**290 hex + 430 rgba** inline `style=`) & JS (**±49 hex**: admin.js 21, settings-vouchers.js 14, …) belum tersentuh — medan fase 2 |
| Tiga sistem modal paralel | ✅ **Hampir beres** | Satu pola visual `.modal-overlay/.modal-card`, confirm unified via `showConfirm()`, Global Modal Manager focus-trap ✓; sisa 2 kelas arwah + 26 fungsi open/close boilerplate → R25 |

### Verifikasi temuan ronde 1 yang sudah BERES (spot-check langsung ke kode)

- **T1** field "Ulangi Password" + mismatch live (`register.html:293-305, 419-437`) · **T2** `uploadError` kini `display:none` · **T3** chip Lulus/Belum Lulus/Belum Dikoreksi + legenda · **T4/T5** nav "Cek Hasil Ujian" + CTA benar · **T6** error persisten + `role="alert"` · **T7** font dimuat di partials/head.html · **T8** diff-render polling (`pengawas-detail.js`) · **T9** kontras ≥4.5:1 via `--color-text-muted` · **T10** toast-close focus-visible + validasi inline per-field · **T11** tombol hapus berlabel 44px
- **S1–S20** seluruhnya tereksekusi sesuai rekap Batch 1–4 (S16: base.html dihapus — verifikasi `find` kosong; S17: palet light dihapus; S18: nav admin kini `aria-current`+`aria-expanded`; S19/S3: helper `setFieldError` + escaping voucher)
- Konfirmasi destruktif kini seragam via `showConfirm`/`showConfirmModal`; SMTP/Turnstile secret dimasking server; login punya toggle password + anti-double-submit
- **P1/P2** terdokumentasi sebagai keputusan produk sadar

### Masih TERBUKA dari ronde 1 (diingatkan, bukan temuan baru)

- **R4** — `document.write` ukuran file masih utuh: `download.html:532,550,692,771` (tanpa fallback noscript) dan `settings.html:2130`. Checklist rekap bawah belum dicoret.
- **S21 / S22** — Landing masih memuat badge versi build (`index.html:7`) dan jargon EN ("Low, Medium & Strict" `:49`, "Deteksi Focus Loss" `:57`, "Zero-Friction Launch" `:114`). Belum disentuh batch mana pun.
- **P3** — Ambang lulus jadi setting server masih DITUNDA (butuh skema/API backend).

---

### T12 — Hero halaman hasil tetap dirender saat error: judul internal jadi `<h1>` + chip "Peserta: 0" menyesatkan
- **Prioritas:** 🔴 Tinggi · **Usaha:** S · **Area:** Publik (Hasil) · **Status:** `[ ]`
- **Lokasi:** `webui/templates/public/hasil.html:105-140` · `webui/internal/handlers/public/hasil.go:95-123`
- **Bukti:**
  ```html
  <!-- hasil.html — section hero DI LUAR cabang if/else state -->
  <section class="exam-hero">
    <h1 id="examTitle">{{.exam_name}}</h1>
    ...
    <span>Peserta: <strong id="totalStudents">{{.total_students}}</strong></span>
  ```
  ```go
  // hasil.go — nilai .exam_name untuk state error adalah pesan internal
  c.HTML(http.StatusInternalServerError, ..., gin.H{"exam_name": "Database Tidak Tersedia"})  // :96
  c.HTML(http.StatusInternalServerError, ..., gin.H{"exam_name": "Error"})                    // :118
  ```
- **Dampak:** Saat DB down/error 500, pengguna melihat `h1` raksasa bertuliskan "Database Tidak Tersedia"/"Error" (jargon teknis bagi siswa/guru awam), disertai chip "Token: XXXX" dan "Peserta: 0" — padahal ujian bisa saja punya peserta. Kartu error di bawahnya bilang "Ujian Tidak Ditemukan" — dua pesan kontradiktif dalam satu layar. "Peserta: 0" bisa disimpulkan siswa sebagai "tidak ada yang ikut ujian".
- **Rekomendasi:** Bungkus hero di dalam state sukses saja, atau saat `.error`/`.is_disabled` render judul netral ("Hasil Ujian"). Jangan pernah memasangkan pesan internal ke field `exam_name`; gunakan flag state + pesan user-friendly di kartu error.

### T13 — Halaman Hasil Ujian tidak punya `#toastContainer` — semua feedback toast hilang senyap
- **Prioritas:** 🔴 Tinggi · **Usaha:** XS · **Area:** Admin (Submissions) · **Status:** `[ ]`
- **Lokasi:** `webui/templates/admin/submissions.html` (seluruh file); `admin-core.js:80-81`; `admin.js:3377, 3864`
- **Bukti:**
  - `admin-core.js:80`: `const container = document.getElementById('toastContainer'); if (!container) return;`
  - Container hanya ada di `dashboard.html:921`, `pengawas_detail.html:14`, `settings.html:740` — **tidak ada di submissions.html**.
  - `admin.js:3377`: `showToast(res.message, 'success')` (dalam `deleteSubmission`) dan `admin.js:3864`: `showToast(res.message || 'Gagal memuat detail', 'error')`.
- **Dampak:** Di halaman Hasil Ujian, guru yang menghapus hasil siswa tidak menerima toast sukses maupun gagal. Saat hapus *gagal* (server error), baris tetap tampil tanpa pesan apa pun — guru tidak yakin apakah data terhapus. Error muat detail jawaban juga tak pernah muncul.
- **Rekomendasi:** Tambahkan container toast di `submissions.html` (posisi sama seperti `pengawas_detail.html:14`). Lebih baik: pindahkan container ke `partials/nav.html` agar mustahil terlewat lagi di halaman baru.

---

### S23 — Tanpa penanganan sesi kedaluwarsa (401): halaman polling menampilkan error generik berulang
- **Usaha:** M · **Area:** Admin · **Status:** `[x]` ✅ **Batch 6**
- **Lokasi:** `admin-core.js:26-67` (`apiFetch`); `pengawas_detail.html:2038` (poll 5 dtk), `pengawas.html:247`
- **Masalah:** Tidak ada satu pun cek `resp.status === 401` di JS admin. Saat sesi habis di halaman monitoring yang dibiarkan terbuka, poll tiap 5 detik terus menampilkan toast/baris "Gagal memuat…" tanpa penjelasan bahwa sesinya berakhir, tanpa arahan login ulang.
- **Dampak:** Guru menyalahkan "server down" dan bisa kehilangan momen pengawasan ujian berjalan.
- **Rekomendasi:** Di `apiFetch` deteksi 401 sekali → event `auth:expired` → toast spesifik ("Sesi berakhir, silakan login kembali") → redirect `/admin/login?next=...`; skip semua interval polling setelahnya.

### S24 — Dua label berbeda untuk status yang sama: "Nonaktif Otomatis" vs "Ditombstone"
- **Usaha:** XS · **Area:** Admin · **Status:** `[ ]`
- **Lokasi:** `dashboard.html:492` ("Nonaktif Otomatis") vs `submissions.html:144,163`, `pengawas.html:286`, `pengawas_detail.html:115` ("Ditombstone")
- **Dampak:** Status ujian sama tampil dengan nama berbeda antarhalaman; guru yang belajar dari dashboard bingung mencari baris "Ditombstone" (jargon internal Inggris).
- **Rekomendasi:** Satukan ke satu label Indonesia, mis. "Nonaktif Otomatis" di semua halaman (4 lokasi + string JS). Istilah "tombstone" cukup untuk komentar kode.

### S25 — 12 modal tanpa semantik dialog: tanpa `role="dialog"`/`aria-modal`/`aria-labelledby`
- **Usaha:** S · **Area:** Admin · **Status:** `[ ]`
- **Lokasi (tanpa ARIA dialog):** `dashboard.html:570,622,657,678,728`; `submissions.html:351` (`detailModal`); `settings.html:1161,1315,1424,1538,1550,2158`. Pembanding sudah benar: `dashboard.html:1065`, `settings.html:1989`.
- **Bukti tambahan:** `settings.html:1556` — satu-satunya tombol ✕ modal tanpa `aria-label="Tutup"` (semua lainnya sudah).
- **Dampak:** Modal Manager sudah memberi focus-trap/ESC, tapi screen reader tidak tahu elemen ini dialog — konten form "menempel" di halaman tanpa konteks.
- **Rekomendasi:** Tambahkan atribut ARIA ke 12 modal (id heading sebagian besar sudah ada); lengkapi `aria-label="Tutup"` di `settings.html:1556`.

### S26 — Kontrol hasil render-JS tak bisa dioperasikan keyboard: pagination `<a>` tanpa href, kartu ujian `<div onclick>`
- **Usaha:** M · **Area:** Admin · **Status:** `[x]` ✅ **Batch 6**
- **Lokasi:** `pengawas.html:347-359` (pagination `<a>` tanpa href/tabindex), `:304` (kartu monitor `div onclick`); `pengawas_detail.html:1562-1572` (pola sama), `:1521,1526` (`<a class="mac-cell">` tanpa href); `submissions.html:277` (popup identitas hanya mouse/touch)
- **Dampak:** Pengguna keyboard/screen reader tak bisa pindah halaman daftar pengawasan/peserta, tak bisa membuka riwayat akses perangkat atau popup identitas siswa. Helper global `role="button"` handler (admin-core.js:765-776) tak membantu karena elemen-elemen ini tak punya role.
- **Rekomendasi:** Pakai `<button type="button">` untuk pagination/aksi; tambahkan `role="button" tabindex="0"` pada kartu ujian & tombol identitas (handler Enter/Space global sudah siap).

### S27 — Beberapa aksi tulis tanpa proteksi double-submit
- **Usaha:** S · **Area:** Admin · **Status:** `[ ]`
- **Lokasi:** `admin.js:1477-1510` (`submitChangePassword`), `:2523-2531` (`submitEditToken`), `:1417-1458` (`saveQuestionsConfig`), `:3316-3341` (`bulkDeleteExams`) & `:3803-3843` (`bulkToggleExams`). Pembanding yang benar: `createUser` `admin.js:3618-3625`, `saveSaasSection` `:3400-3428`.
- **Dampak:** Klik ganda/koneksi lambat mengirim request berulang (POST konfigurasi soal duplikat → dobel `location.reload()` beruntun, dsb.) — inkonsisten dengan form yang sudah baik.
- **Rekomendasi:** Pola `btn.disabled = true` + label "Menyimpan..." + restore di `.finally()` pada kelima fungsi.

### S28 — Implementasi pencarian ganda & dead-code besar di admin.js (definisi fungsi saling menimpa)
- **Usaha:** S · **Area:** Admin · **Status:** `[x]` ✅ **Batch 6**
- **Lokasi:** `admin.js:3996-4074` mendefinisikan `filterExamRows/searchExams/clearSearch/searchExamsWithStatus` client-side — namun `dashboard.html:971-984` (inline script, dimuat SETELAH admin.js) mendefinisikan ulang versi URL-navigasi; versi inilah yang menang. Versi admin.js mati, tapi listener Enter-nya (`admin.js:4064-4074`) tetap hidup: Enter di kolom cari menyembunyikan baris sesaat lalu reload penuh (flicker dobel-alur).
- **Dead code terverifikasi tanpa pemanggil:** `openManageUsersModal/closeManageUsersModal` (admin.js:1542-1553), `resetNewUserFormDefaults` (:3694), `copyAllTokens` (:355), `copyResultsLink` (:376), `regenerateToken` (:467), `initPasswordStrengthMeter` (admin-core.js:683), shortcut `?`/`toggleShortcuts` (admin-core.js:423-445 — elemen `#shortcutsHint` tak ada sehingga menekan `?` hanya `preventDefault` tanpa efek), `const CSRF_TOKEN` (settings.html:2216).
- **Dampak:** Dua strategi pencarian hidup berdampingan; ~200 baris mati membebani pemeliharaan & cache klien.
- **Rekomendasi:** Putuskan satu strategi (URL-based lebih tepat untuk tabel server-side paginated); hapus blok mati + listener Enter; hapus shortcut `?` atau render elemen hints-nya.

### S29 — `copyToken`/`copyAIPrompt` tanpa guard `navigator.clipboard` — gagal senyap di LAN HTTP
- **Usaha:** XS · **Area:** Admin · **Status:** `[x]` ✅ Batch 5 (wrapper) + **Batch 6** (follow-up override vouchers)
- **Lokasi:** `admin.js:340` (`navigator.clipboard.writeText(token)` tanpa cek eksistensi; juga `:362, :382, :2791`). Pembanding yang benar: `admin-core.js:214-230` (`copyCode`: guard + fallback `execCommand`).
- **Dampak:** `navigator.clipboard` = `undefined` pada origin non-secure — skenario nyata sekolah via `http://192.168.x.x`. Klik "Salin Token" melempar TypeError, token tak tersalin, tanpa toast apa pun.
- **Rekomendasi:** Jadikan `copyCode` satu-satunya implementasi; fungsi lain menjadi wrapper tipis di atasnya.

### S30 — Skip-link hilang di 5 halaman admin utama & markup terduplikasi inline 9×
- **Usaha:** S · **Area:** Lintas · **Status:** `[ ]`
- **Lokasi:** skip-link hanya ada di `admin/login.html`; dashboard/settings/pengawas/pengawas_detail/submissions tidak punya. Halaman publik lengkap 8/8. Di sisi lain markup skip-link copy-paste identik 9× (login, cek_hasil, forgot/reset password, hasil, register_confirm, register, shared.html:916) dengan inline style panjang — padahal `theme.css:77-84` sudah punya class `.skip-link`.
- **Dampak:** Keyboard user tak bisa lompat nav di halaman admin terpadat; 9 salinan inline = bom drift.
- **Rekomendasi:** Pindahkan skip-link ke `partials/nav.html` (admin) dan head publik; hapus inline style, andalkan CSS theme.

### S31 — Icon-button refresh & clear-search tanpa nama aksesibel
- **Usaha:** XS · **Area:** Admin (Dashboard) · **Status:** `[ ]`
- **Lokasi:** `dashboard.html:415` (tombol refresh, hanya SVG — bandingkan `pengawas.html:62` yang benar: `aria-label="Muat ulang daftar"`); `dashboard.html:412` (`#searchClearBtn` ikon X tanpa aria-label).
- **Rekomendasi:** Tambah `aria-label`; tambahkan assertion di test a11y bahwa `<button>` tanpa teks wajib ber-aria-label.

### S32 — `prefers-reduced-motion` bolong di jalur tertentu; `prefers-contrast` = 0; print style parsial
- **Usaha:** M · **Area:** Lintas · **Status:** `[x]` ✅ **Batch 6** (reduced-motion public-desktop + print style minimal publik)
- **Lokasi:** reduced-motion ada di admin-base.css, hasil.css, public-mobile.css, shared.html, download.html — **tapi 0 di public-desktop.css** dan hanya 2 dari ±20 blok `<style>` inline template. `prefers-contrast`: 0 di seluruh repo. `@media print` hanya 2 (admin-base.css:453, hasil.css:934) — index/register/download/cek_hasil tanpa print style.
- **Rekomendasi:** Tambah reduced-motion ke public-desktop.css + blok umum; print style minimal bersama untuk halaman publik.

### S33 — Z-index acak tanpa token
- **Usaha:** S · **Area:** Lintas · **Status:** `[x]` ✅ **Batch 6**
- **Lokasi:** sebaran `9999` ×11, `10002` (theme.css:81), `99999` (nav.html:82 onboarding modal — mengalahkan semua termasuk toast/skip-link), plus 220/200/160/… tersebar. `admin-base.css:744` sudah mendokumentasikan skala — tapi hanya komentar, bukan token.
- **Rekomendasi:** Definisikan `--z-skip-link/--z-dropdown/--z-sticky/--z-toast/--z-modal` di theme.css, substitusi persis (pola S15 fase 1).

### S34 — Open Graph & `theme-color` = 0 di seluruh template
- **Usaha:** S · **Area:** Publik · **Status:** `[x]` ✅ **Batch 6**
- **Lokasi:** grep og:/theme-color → 0 hasil. Viewport/favicon/meta description sudah ✓ (shared.html:77-86, hasil.html:21).
- **Dampak:** Preview share WhatsApp/social tanpa judul-gambar — padahal `download.html` adalah halaman yang paling layak dibagikan; address bar mobile tak ikut warna brand.
- **Rekomendasi:** Tambah `og:title/description/image` minimal di shared.html (override-able per halaman) + `theme-color: #09090e` di kedua head partial.

### S35 — Feedback loading paginasi/pencarian halaman hasil tidak ada; transisi state tak diumumkan
- **Usaha:** S · **Area:** Publik (Hasil) · **Status:** `[x]` ✅ **Batch 6**
- **Lokasi:** `hasil.html:334` (`loadingIndicator` disembunyikan permanen setelah muat awal, tak pernah dipakai ulang), `:750-753` (`goToPage` scroll SEBELUM data datang, tanpa spinner/disable), `:744-754`.
- **Dampak:** Jaringan lambat → klik halaman 2/ketik pencarian membuat tabel tampak mati beberapa detik (mirip dobel-klik unduhan yang sudah difix di S13); pengguna screen reader tak tahu hasil pencarian telah tampil.
- **Rekomendasi:** Redupkan area tabel + spinner saat `loadResults()` berjalan (guard anti-dobel seperti pola `resultsLoading`); tambahkan region `aria-live="polite"` "Menampilkan X–Y dari Z peserta".

### S36 — Empat interval polling detail pengawas mengabaikan `document.hidden` & tak pernah di-clear
- **Usaha:** S · **Area:** Admin (Pengawasan) · **Status:** `[x]` ✅ **Batch 6**
- **Lokasi:** `pengawas_detail.html:2038-2044`: `setInterval(loadApprovals, 5000)`, `loadDetail(12000)`, `refreshActiveToken(30000)`, `updateCountdown(1000)` — permanen, tanpa clearInterval, tanpa visibility-guard. Pembanding yang benar: `refreshDashboardStats` (admin-core.js:657-668) cek `document.hidden` + flag in-flight.
- **Dampak:** Tab dibiarkan terbuka terus menembak 4 endpoint berkala — beban server (relevan dengan roadmap kapasitas) + boros baterai laptop/tablet pengawas. Countdown habis memicu request refreshActiveToken tiap detik.
- **Rekomendasi:** Bungkus callback dengan cek `document.hidden` (seperti startAutoRefresh), simpan handle, clear pada `visibilitychange`/`pagehide`.

---

### R17 — `aria-current` belum ada di nav publik (perbaikan S18 hanya menyentuh nav admin)
- **Usaha:** XS · **Area:** Publik · **Status:** `[ ]`
- **Lokasi:** `public/shared.html:28-37` & script `public_foot` `:936-942` — penanda aktif hanya class visual `.active`.
- **Rekomendasi:** Set `link.setAttribute('aria-current','page')` untuk link yang cocok.

### R18 — Pesan error Turnstile: tanpa live region & tidak dibersihkan setelah captcha diselesaikan
- **Usaha:** S · **Area:** Publik + Login Admin · **Status:** `[ ]`
- **Lokasi:** `register.html:311` (+handler `:369-374`), `forgot_password.html:60`, `reset_password.html:133`, `login.html:89` — `#turnstileError` dimunculkan dinamis tanpa `role="alert"` dan tak pernah dikosongkan ketika user menyelesaikan captcha.
- **Dampak:** User yang captcha-nya sudah selesai tapi submit gagal karena alasan lain masih melihat instruksi usang "selesaikan verifikasi".
- **Rekomendasi:** `role="alert"` pada div; kosongkan pada callback sukses/token-expired Turnstile atau sebelum tiap attempt submit.

### R19 — Hierarki heading melompat di beberapa halaman + nav admin tanpa `<header>`
- **Usaha:** XS–S · **Area:** Publik + Admin · **Status:** `[x]` ✅ Batch 5 (heading) + **Batch 6** (landmark `<header>` topbar nav.html)
- **Lokasi:** `index.html:33` (`h1`→`h3` sebelum `h2` pertama `:66`); `dashboard.html:242` (h1 sr-only) → `:303,:365,:401,…` sembilan kali h3 tanpa satu pun h2 (sama di `pengawas.html:16→53`, `pengawas_detail.html`); `nav.html` tak dibungkus `<header>` (0 `<header>` di semua halaman kecuali hasil.html). Submissions & settings sudah benar.
- **Rekomendasi:** Naikkan h3 seksi menjadi h2 (CSS visual via class); mockup-title landing → p/h2; bungkus topbar dengan `<header>`.

### R20 — Print CSS halaman hasil memakai selector yang tidak eksis
- **Usaha:** XS · **Area:** Publik (Hasil) · **Status:** `[ ]`
- **Lokasi:** `hasil.css:939` — menyembunyikan `.search-card`/`.pagination`, padahal kelas nyata adalah `.search-section` (hasil.html:173) & `.pagination-wrapper` (:210).
- **Dampak:** Guru yang mencetak rekap nilai mendapat kolom pencarian & tombol halaman di kertas.
- **Rekomendasi:** Ganti selector (pertimbangkan juga `.header-badge`).

### R21 — Konflik grid tab platform: aturan desktop kalah permanen oleh `!important` layer mobile
- **Usaha:** XS · **Area:** Publik (Download) · **Status:** `[x]` ✅ **Batch 6**
- **Lokasi:** `public-mobile.css:112-115` (`repeat(3,...) !important`, topik-level semua lebar) vs `public-desktop.css:46` (`repeat(2,1fr)` tanpa important) — intent desktop tak pernah diterapkan (dead code menyesatkan, sumber regresi senyap).
- **Rekomendasi:** Putuskan satu intent (3 kolom tampaknya aktual): hapus grid-template-columns desktop, atau pindahkan rule mobile ke media query.

### R22 — Reset password tidak punya strength meter (inkonsisten dengan registrasi)
- **Usaha:** XS–S · **Area:** Publik · **Status:** `[ ]`
- **Lokasi:** `reset_password.html:112-118` vs `register.html:288-289, 387-417` (pw-strength-bar + skoring live).
- **Rekomendasi:** Port komponen strength bar register; ekstrak fungsi skor bersama.

### R23 — Input pencarian tanpa label/aria-label di banyak tempat
- **Usaha:** XS · **Area:** Publik + Admin · **Status:** `[ ]`
- **Lokasi:** `hasil.html:176-178` (`#searchInput` placeholder-only), `pengawas.html:58`, `pengawas_detail.html:262`, `settings.html:1281` (`#auditSearchInput`), `settings.html:939` (`#userSearchInput`). Pembanding yang benar: `dashboard.html:410` (label sr-only), `settings.html:1222` (aria-label).
- **Rekomendasi:** Tambah `aria-label="Cari ..."` pada keenam input.

### R24 — Toast tolak perangkat memakai tipe `error` + frasa salin tidak seragam
- **Usaha:** XS · **Area:** Admin (Pengawasan) · **Status:** `[x]` ✅ **Batch 6** (tipe toast; frasa salin distandarkan lewat copyCode Batch 5/6)
- **Lokasi:** `pengawas_detail.html:1425` — menolak peserta (aksi berhasil) ditampilkan toast merah 'error'. Pesan salin beda-beda: `admin-core.js:218` vs `admin.js:341` vs `settings-vouchers.js:19`.
- **Rekomendasi:** Tipe `info`/`success` untuk konfirmasi tolak; standarkan frasa salin lewat satu helper (`copyCode`).

### R25 — Boilerplate modal: 26 fungsi open/close ad-hoc + 2 kelas arwah
- **Usaha:** M · **Area:** Admin · **Status:** `[x]` ✅ **Batch 6**
- **Lokasi:** pola dominan kini seragam (`.modal-overlay` ×17, `.modal-card` ×17), tapi JS masih punya **26 fungsi `open*/close*Modal`** tulisan tangan yang isinya cuma toggle display (admin.js:554,629,1463,1472,…); sisa anomali `.modal-backdrop` ×1 dan `.modal-glass` ×1. `<dialog>`: 0.
- **Rekomendasi:** Ekspor API `Modal.open(el)/Modal.close(el)` dari core, refactor 26 fungsi jadi delegasi; seragamkan 2 kelas arwah.

### R26 — Sisa string EN user-visible
- **Usaha:** XS · **Area:** Semua · **Status:** `[ ]`
- **Lokasi:** `submissions.html:147` tombol "Export Excel"; deskripsi fitur landing "focus loss" (`index.html:82`) — istilah mode keamanan Low/Medium/Strict diputuskan boleh sebagai proper noun (dokumentasikan di style guide).
- **Rekomendasi:** "Export Excel" → "Ekspor Excel".

### R27 — CSS higiene: `!important` massal & ±20 blok `<style>`/`@media` inline dalam template
- **Usaha:** M · **Area:** Lintas · **Status:** `[ ]`
- **Bukti:** `!important`: hasil.css 63, public-mobile.css 47, admin-base.css 47, public-desktop.css 19 (theme.css 1, disengaja). Blok hasil.css:790-796 memakai 7 `!important` beruntun untuk override Tailwind/grid. Breakpoint konsisten ✓ (768/480/1024px). Catatan positif: ±20 blok @media hidup di `<style>` inline template HTML, bukan file CSS.
- **Rekomendasi:** Pindahkan `<style>` inline halaman ke file CSS per-halaman; audit `!important` hasil.css — kebanyakan hilang jika urutan load CSS dirapikan.

### R28 — Global namespace pollution (±50 handler inline onclick) & format tanggal tak satu pintu
- **Usaha:** S · **Area:** Admin · **Status:** `[x]` ✅ **Batch 6** (formatDateTimeID satu pintu; migrasi penuh data-action tetap berkelanjutan)
- **Bukti:** ±dozen `window.x = fn` dipaksa oleh onclick inline (57 di settings.html, 48 di dashboard.html, 27 di pengawas_detail.html) — CSP-unsafe. Tanggal: `admin-core.js:263-268` format manual tanpa `Intl`, sementara `pengawas_detail.html:1592` sudah `toLocaleString('id-ID',...)`. Positif: pola event-delegation `data-action` mulai dipakai (settings-vouchers.js:128).
- **Rekomendasi:** Satu helper `formatDateTimeID()` di core; lanjutkan migrasi delegation `data-action`.

---

## 5.6 RE-REVIEW RONDE 3 — Temuan baru pasca Batch 5–8

> **Tanggal:** 24 Agustus 2026 · **Basis kode:** `1387853` (pasca Batch 8, suite 411/411 hijau) · **Metode:** 3 reviewer paralel (area admin, area publik, lintas-halaman/design-system) + verifikasi manual silang temuan kunci ke kode.
> Penomoran ID melanjutkan ronde sebelumnya (`fix(uiux): T14 …`). Fokus khusus ronde ini: regresi dari migrasi besar Batch 7–8 (delegasi `data-action`, fase 2 token), dan audit terukur sisa design-token/`!important`.

### Status verifikasi cepat (item lama)

| Item | Status ronde 3 | Bukti |
|---|---|---|
| Migrasi Batch 7–8 (`data-action`) | ✅ **Bersih di level registry** | Seluruh `data-action` markup statis maupun render-JS punya handler terdaftar; tanpa dobel-registrasi fungsional; normalisasi `parseInt(...,10)` konsisten; diff-render T8 + visibility-guard S36 utuh. *(Tapi lihat S43/R29 — guard testnya bocor.)* |
| R4 `document.write` | 🔴 **MASIH TERBUKA, utuh 5 lokasi** | `download.html:532,550,692,771` · `settings.html:2130`; tanpa fallback noscript. Test bahkan no-op `write()` karena keberadaannya (`uiux-batch7-settings.test.mjs:226`) — patuh pada bug, bukan fix |
| R26 "Ekspor Excel" | ✅ **BERES** | `submissions.html:151`; test penjaga ada. Sisa EN minor baru: `settings.html:930` ("Refresh daftar user"/"Refresh") |
| R27 `!important` | 🔴 **TERBUKA — angka NAIK sejak ronde 1** | Aktual (terverifikasi): admin-base.css **55** (47) · hasil.css **64** (63) · public-mobile.css **81** (47 — hampir dobel saat Batch 5–6) · public-desktop.css **34** (19). Blok inline belum pindah: settings.html **8 blok `<style>` = 717 baris**; shared.html ±893 baris |
| S28 shortcut `?` | ✅ **BERES** | `admin-core.js:567` komentar penghapusan eksplisit; grep `case '?'` = 0 |
| S21/S22/P3 | Belum disentuh batch mana pun (tetap terbuka) | |

---

### T14 — `pengawas.html` tidak punya `#toastContainer`: pesan "Sesi berakhir" (S23) & error global lenyap senyap di halaman pemantauan
- **Prioritas:** 🔴 Tinggi · **Usaha:** XS · **Area:** Admin (Pengawasan) · **Status:** `[x]` ✅ **Batch 9**
- **Lokasi:** `webui/templates/admin/pengawas.html` (seluruh file); `admin-core.js:135-136`, `:64-66`. Container hanya ada di dashboard.html:923, settings.html:740, submissions.html:132 (fix T13), pengawas_detail.html:14.
- **Bukti:** `const container = document.getElementById('toastContainer'); if (!container) return;` — listener `auth:expired` memanggil `showToast('Sesi berakhir…')` yang jadi no-op di halaman ini.
- **Dampak:** Halaman yang paling lama dibiarkan terbuka pengawas adalah satu-satunya halaman admin tanpa toast: sesi habis → redirect 1,2 dtk **tanpa penjelasan apa pun** (gejala S23 bangkit), semua toast `api:error` ikut lenyap.
- **Rekomendasi:** Pindahkan `<div id="toastContainer">` ke `partials/nav.html` sekali untuk semua halaman (rekomendasi awal T13); tambah assertion test bahwa setiap halaman yang memuat admin-core.js punya container.

### T15 — Generate/Import XML + hapus field identitas bocor dari guard unsaved-changes (S2)
- **Prioritas:** 🔴 Tinggi · **Usaha:** S · **Area:** Admin (Dashboard) · **Status:** `[x]` ✅ **Batch 9**
- **Lokasi:** `admin.js:1161-1187` (quickGenerate), `:2608-2613` (importXML), `:960` (hapus field identitas)
- **Bukti:** Keduanya berujung `renderQuestions(questions)` (:1186, :2612 → `container.innerHTML = ''`) **tanpa** `showConfirm` maupun `markQuestionsConfigDirty()` — bandingkan jalur sejenis yang benar (`admin.js:702,723`). Hapus baris identitas pakai inline handler `onclick="this.closest('.identity-field-row').remove()"`.
- **Dampak:** Guru dengan 30 soal+kunci di editor klik Generate/Import → semuanya tertimpa seketika tanpa dialog; flag dirty tetap `false` sehingga Batal menutup tanpa peringatan — guard unggulan S2 bocor tepat pada aksi paling destruktif di modal.
- **Rekomendasi:** `showConfirm("Ganti semua soal di editor?")` sebelum replace di kedua fungsi; panggil `markQuestionsConfigDirty()` pasca render (termasuk hapus-baris identitas).

### T16 — Kontras gradien tombol unduh primer gagal WCAG AA
- **Prioritas:** 🔴 Tinggi · **Usaha:** S · **Area:** Publik (Download) · **Status:** `[x]` ✅ **Batch 9** *(rasio diverifikasi perhitungan)*
- **Lokasi:** `download.html:535` (`linear-gradient(135deg, var(--color-accent), #7c3aed)`), `:695` (`#3b82f6→#2563eb`), `:774`; base `.btn-download-big` `color:white; font-size:15.5px` (:225-238)
- **Masalah:** Putih di atas endpoint gradien: `#a855f7` = **3.96:1**, `#3b82f6` = **3.68:1**, `--color-accent #8b5cf6` = **4.23:1**. Font 15.5px bold < ambang large-text (≥18.66px bold), jadi ambang 4.5:1 — CTA konversi tertinggi halaman siswa gagal AA.
- **Rekomendasi:** Gelapkan ujung gradien (`#a855f7`→`#9333ea`, dst.) atau naikkan label ≥16px bold; tambahkan asersi kontras endpoint gradien ke test publik.

### S37 — Escape/backdrop menembus guard "unggahan masih berlangsung"; `.modal-backdrop` arwah tersisa
- **Usaha:** S · **Area:** Admin (Settings) · **Status:** `[x]` ✅ **Batch 9**
- **Lokasi:** `settings-system-apps.js:131-137`; `admin-core.js:991-1011` (forceClose menghapus class `show`); `settings.html:1989` + CSS `:655-662`
- **Masalah:** Tombol ✕/Batal menolak menutup selama upload (benar), tapi Escape/klik-overlay tetap menutup modal via fallback core — unggahan lanjut tanpa indikator progres, bisa gagal diam-diam bila tab ditutup. `uploadModal` satu-satunya pemakai kelas arwah `.modal-backdrop` (sisa R25).
- **Rekomendasi:** `closeUploadModal` memberi sinyal penolakan (return boolean / `data-force-locked`) yang dihormati forceClose; atau sembunyikan-ke-background dengan floating progress pill. Migrasi ke `.modal-overlay`.

### S38 — Dua ambang warna nilai berbeda: rekap guru (80/60) vs halaman siswa (70/40 + chip Lulus)
- **Usaha:** XS · **Area:** Admin + Publik · **Status:** `[x]` ✅ **Batch 9**
- **Lokasi:** `submissions.html:287-298` vs `public/hasil.html:899-907`
- **Dampak:** Siswa 75% melihat hijau "Lulus", guru melihat kuning di Rekapitulasi (dan sebaliknya di 65%) — dua layar resmi saling kontradiktif. Perluasan P3 tapi bisa disatukan tanpa backend.
- **Rekomendasi:** Satukan threshold ke satu konstanta bersama (ekspor core / data atribut dari server) sekarang; setting backend menyusul saat P3 dieksekusi.

### S39 — Pengaturan Umum: 8 kartu tersimpan terpisah tanpa indikator dirty maupun guard unload
- **Usaha:** M · **Area:** Admin (Settings) · **Status:** `[x]` ✅ **Batch 9**
- **Lokasi:** `settings.html:1643-1887` (8 tombol "Simpan …"); `beforeunload` hanya untuk editor soal (`admin.js:422-427`)
- **Dampak:** Di halaman form terpanjang produk, tak ada cara tahu kartu mana yang belum disimpan; navigasi/tab-close membuang suntingan tanpa peringatan; toast sukses generik tak menyebut section mana.
- **Rekomendasi:** Track dirty per section (badge titik "belum disimpan" di header kartu + tombol "Simpan •"), guard `beforeunload` bila ada kartu kotor, toast spesifik ("Setelan SMTP disimpan").

### S40 — "Ekspor Excel" tanpa loading state & error handling — JSON error mentah bisa merender
- **Usaha:** S · **Area:** Admin (Submissions) · **Status:** `[x]` ✅ **Batch 9**
- **Lokasi:** `admin.js:3283-3291` (`window.location.href = url` langsung); tombol `submissions.html:151`
- **Dampak:** Pola identik S13 yang sudah difix di publik: dataset besar + jaringan lambat = tombol terasa mati → klik berulang (navigasi ganda); server error merender JSON teknis menggantikan halaman.
- **Rekomendasi:** Fetch via `apiFetch` + blob download dengan spinner/disable (pola S13); tetap di halaman saat gagal + toast.

### S41 — `copyToken` lokal di pengawas_detail MENIMPA versi hardening S29; `copyServerURL` juga di luar pola
- **Usaha:** XS · **Area:** Admin (Pengawasan/Dashboard) · **Status:** `[x]` ✅ **Batch 9**
- **Lokasi:** `pengawas_detail.html:1902-1917` (`navigator.clipboard.writeText(token).then(...)` tanpa catch/fallback, deklarasi dimuat setelah admin.js sehingga menimpa versi S29); `dashboard.html:1046-1063` (duplikasi guard sendiri)
- **Dampak:** Skenario S29 (LAN HTTP / izin clipboard ditolak): klik "Salin Token" di momen paling kritis pembagian token — tidak menyalin apa pun tanpa toast gagal.
- **Rekomendasi:** Hapus kedua definisi lokal; panggil `copyCode()` langsung. Test: string `navigator.clipboard` tak boleh ada di inline template.

### S42 — fingerprintjs 37KB dimuat sinkron di 4 halaman auth publik (regresi parsial R11)
- **Usaha:** XS · **Area:** Publik · **Status:** `[x]` ✅ **Batch 9**
- **Lokasi:** `forgot_password.html:99`, `register.html:483`, `register_confirm.html:424`, `reset_password.html:320`; pembanding benar `admin/login.html:152` (sudah `defer`)
- **Dampak:** Script blocking render di halaman pendaftaran — momen adopsi produk — padahal hanya dipakai untuk field fingerprint tersembunyi.
- **Rekomendasi:** Tambah `defer` di keempat lokasi.

### S43 — Guard test token salah ukur rgba: regex `/rgba\(/g` menghitung pemakaian TOKEN sebagai literal; JS tak ter-guard
- **Usaha:** XS–S · **Area:** Tooling/S15 · **Status:** `[x]` ✅ **Batch 9**
- **Lokasi:** `uiux-batch7-tokens.test.mjs:30` & `:52-56`; `admin.js` **36 rgba literal** (mis. `:1045` style string panjang); `settings-vouchers.js` **22 hex + 9 rgba** (mis. `:66-70`)
- **Fakta terverifikasi:** total `rgba(` templates/ = **520 persis di plafon**, padahal literal digit hanya **225** — 295 hitungan adalah `rgba(var(--rgb-*))` yang justru ingin didorong. Dev bisa menambah ~295 literal baru sebelum test merah; migrasi literal→token tak menurunkan metrik.
- **Rekomendasi:** Regex `/rgba\(\s*[0-9]/g`, kunci ulang baseline aktual per file (settings 110, dashboard 32, dst.), turunkan plafon bertahap; perluas guard ke `static/js/*.js` (kecuali fingerprintjs.min.js).

### S44 — Palet light arwah masih hidup di shared.html + sistem token paralel menyaingi theme.css
- **Usaha:** XS–S · **Area:** Publik · **Status:** `[x]` ✅ **Batch 9**
- **Lokasi:** `shared.html:129-141` (`:root[data-theme="light"] { --text-main:#0f172a; --text-muted:#64748b; ... }`); kontras `theme.css:102` (keputusan S17: palet light DIHAPUS)
- **Dampak:** Duplikat keputusan dead-code S17 tak ikut dihapus — lengkap dengan `#64748b` yang dilarang T9 (3.84:1), dipakai ±10 lokasi via `var(--text-muted)`; blok ini juga mendefinisikan token paralel (`--bg-primary`, `--accent-primary`, …) = dua sumber kebenaran tema.
- **Rekomendasi:** Hapus blok light; migrasikan pemakaian token lokalnya ke token resmi theme.css.

### S45 — Instruksi instalasi merujuk posisi visual yang salah di mobile ("di sebelah kiri")
- **Usaha:** XS · **Area:** Publik (Download) · **Status:** `[x]` ✅ **Batch 9**
- **Lokasi:** `download.html:586` vs `public-mobile.css:99-101` (layout 1 kolom s/d 1100px)
- **Dampak:** Langkah-1 panduan siswa menyebut tombol "di sebelah kiri" padahal di layout satu kolom (mayoritas audiens HP) kartu unduh ada **di atas** — instruksi pertama sudah tidak cocok dengan layar.
- **Rekomendasi:** Hapus referensi spasial ("ketuk tombol **Unduh APK** di kartu di atas") atau anchor link ke tombol unduh.

### S46 — Detail nilai memajangkan total hitungan klien di samping chip status resmi — bisa kontradiktif
- **Usaha:** S · **Area:** Publik (Hasil) · **Status:** `[x]` ✅ **Batch 9**
- **Lokasi:** `hasil.html:653-655` (chip dari `sub.score` resmi) vs `:728-733` (badge `${totalEarned}` hasil penjumlahan evaluasi klien); bonus: `id="scoreStatusBadge"` diduplikasi tiap baris terbuka (HTML invalid)
- **Dampak:** Bila skor server ≠ penjumlahan klien (revisi kunci pasca-ujian — fitur yang diiklankan landing), siswa melihat "62.5/100 [Belum Lulus]" di panel dan badge tabel 70 — dua angka + satu status bertentangan dalam satu layar.
- **Rekomendasi:** Tampilkan `sub.score / sub.max_score` resmi di summary bar (total evaluasi klien cukup sebagai baris rincian); `scoreStatusBadge` jadi class tanpa id.

### R29 — onclick inline tersisa di render-JS & nav.html — guard test hanya memindai template
- **Usaha:** S · **Area:** Admin · **Status:** `[x]` ✅ **Batch 9**
- **Lokasi:** `admin.js:654,680,691,599,602`; `settings-vouchers.js:16,158`; `partials/nav.html:38,73,90` (hamburger, openChangePasswordModal, overlay stopPropagation)
- **Dampak:** Klaim "0 onclick" Batch 7 tidak penuh — partial nav dirender di SEMUA halaman admin; string HTML dalam JS tidak terkunci guard; tetap CSP-unsafe + interpolasi `${index}` ke atribut.
- **Rekomendasi:** Migrasi ke `data-action`; masukkan nav.html & literal `onclick=` di `static/js/*.js` render-path ke guard test.

### R30 — Antrean izin tanpa aksi massal: konfirmasi beruntun per siswa di momen paling sibuk
- **Usaha:** M · **Area:** Admin (Pengawasan) · **Status:** `[ ]` ⏸ **DITUNDA** — butuh keputusan UX alur konfirmasi ("Izinkan semua tampil" vs "jangan tanya lagi 5 menit")
- **Lokasi:** `pengawas_detail.html:1344-1352` (per-baris saja), `:1801-1827` (modal konfirmasi per aksi); grep bulk/approve-all = kosong
- **Dampak:** 30 siswa login serentak = 30× (modal + tap) bagi pengawas — confirm fatigue persis saat waktu paling sempit; alternatif auto-approve all-or-nothing berisiko lupa dimatikan.
- **Rekomendasi:** "Izinkan Semua Terpilih"/"Izinkan semua tampil" dengan `showConfirm` sekali + loop POST; atau opsi "jangan tanya lagi 5 menit".

### R31 — Feedback minor harian: toast tanpa nama siswa, jadwal UTC mentah, reload penuh pasca simpan soal
- **Usaha:** XS–S · **Area:** Admin · **Status:** `[x]` ✅ **Batch 9**
- **Lokasi:** `pengawas_detail.html:1431` (toast persetujuan tanpa `student_name` yang tersedia :1338); `pengawas.html:293` (`start_time + ' - ' + end_time` mentah, melanggar satu-pintu R28); `admin.js:1362` (`setTimeout(location.reload, 500)` pasca simpan konfigurasi soal — pola yang sudah dihapus R6)
- **Rekomendasi:** Sertakan nama di toast; format via `formatDateTimeID`; ganti reload dengan update badge baris (pola R6).

### R32 — ±40 label form settings tanpa asosiasi programatik
- **Usaha:** S · **Area:** Admin · **Status:** `[x]` ✅ **Batch 9**
- **Lokasi:** `settings.html` (81 `<label>`, hanya 41 ber-`for=`; contoh `:1324-1325`); pola serupa OTP group `register_confirm.html:240`, `reset_password.html:129`
- **Dampak:** Klik label tak memfokuskan input; screen reader membaca field tanpa nama (placeholder hilang saat mengetik).
- **Rekomendasi:** Sweep `for=`/`id` (sebagian besar id sudah ada).

### R33 — Duplikasi modal "Ubah Password" dashboard↔settings — sudah drift mekanisme close
- **Usaha:** S · **Area:** Admin · **Status:** `[x]` ✅ **Batch 9**
- **Lokasi:** `dashboard.html:~575-620` vs `settings.html:~2160-2205` (`data-action="modal-close"` vs `"password-modal-close"`)
- **Dampak:** Dua salinan form sensitif yang harus dirawat 2× dan memang telah menyimpang — pola drift base.html lama (S16).
- **Rekomendasi:** Partial Go tunggal yang di-include kedua halaman; satu registrasi close kanonik.

### R34 — Cache-busting CSS publik tidak konsisten; urutan cascade rapuh
- **Usaha:** XS · **Area:** Publik · **Status:** `[x]` ✅ **Batch 9**
- **Lokasi:** `shared.html:933` (theme.css **tanpa** `?v=`) vs `partials/head.html:8` (dengan `?v=`); ±900 baris `<style>` inline shared.html dimuat SEBELUM link eksternal
- **Dampak:** Proxy LAN suka cache agresif — rilis tema berisiko user dapat CSS basi; override equal-specificity bergantung urutan file.
- **Rekomendasi:** Tambah `?v={{.version}}` ke link di shared.html:933-935; pertimbangkan pindah blok inline terbesar ke file (R27).

### R35 — Kolom durasi dua nama: "Waktu Pengerjaan" (desktop) vs "Durasi" (mobile card)
- **Usaha:** XS · **Area:** Publik (Hasil) · **Status:** `[x]` ✅ **Batch 9**
- **Lokasi:** `hasil.html:212` vs `:532` — isinya durasi (`getDurationString` :465-485), bukan waktu.
- **Rekomendasi:** Satukan ke "Durasi".

### R36 — Input username publik tanpa `autocapitalize="none"`
- **Usaha:** XS · **Area:** Publik · **Status:** `[x]` ✅ **Batch 9**
- **Lokasi:** `register.html:260`, `forgot_password.html:55`; pembanding disiplin `cek_hasil.html:48`
- **Dampak:** Keyboard mobile mengkapitalisasi otomatis → forgot-password tidak menemukan akun tanpa pesan yang menjelaskan.
- **Rekomendasi:** `autocapitalize="none" autocorrect="off" spellcheck="false"` (+ `enterkeyhint="go"` di form single-field cek hasil).

### R37 — Paginasi hasil publik tanpa `aria-current`; tab Nilai/Kunci tidak deep-linkable
- **Usaha:** XS–S · **Area:** Publik (Hasil) · **Status:** `[x]` ✅ **Batch 9**
- **Lokasi:** `hasil.html:788-794` (createPageBtn hanya aria-label), `:817-829` (switchTab tak menyentuh hash) — pola yang sama sudah difix di admin (S26) dan tab download (Batch 3).
- **Rekomendasi:** `aria-current="page"`; simpan tab aktif di hash (`#nilai`/`#kunci`).

### R38 — Instruksi SmartScreen hanya mengutip label Windows Inggris
- **Usaha:** XS · **Area:** Publik (Download) · **Status:** `[x]` ✅ **Batch 9**
- **Lokasi:** `download.html:730` — `"More Info"` → `"Run Anyway"` tanpa padanan Windows Indonesia ("Info lainnya" → "Tetap jalankan").
- **Rekomendasi:** Tulis kedua varian bahasa.

### R39 — Mockup hero landing menampilkan domain hard-coded yang kontradiksi panduan instalasi LAN
- **Usaha:** XS · **Area:** Publik (Landing) · **Status:** `[x]` ✅ **Batch 9**
- **Lokasi:** `index.html:35` (`https://examvan.my.id`) vs `download.html:617` (contoh `http://192.168.1.10:8080`)
- **Dampak:** Landing menampilkan citra SaaS cloud sementara alur nyata & value-prop offline memakai alamat LAN; calon pembeli awam bingung alamat mana yang harus diisi aplikasi.
- **Rekomendasi:** Placeholder netral (`http://alamat-server-sekolah` + `TOKEN UJIAN`) atau editable dari data server.

### R40 — Band kuning 40–69 dipasangkan chip merah "Belum Lulus" — dua sinyal warna berlawanan
- **Usaha:** XS · **Area:** Publik (Hasil) · **Status:** `[x]` ✅ **Batch 9**
- **Lokasi:** `hasil.html:890-904` (`score-mid` kuning + `score-status-fail` merah untuk pct<70); legenda `:169` tak menjelaskan hubungannya.
- **Rekomendasi:** Tingkat ketiga chip ("Hampir", netral/kuning untuk 40–69) atau minimal klausa legenda "status Lulus mulai dari 70" (menyatu dengan S38/P3).

### R41 — Sisa ekor kontras T9 pada kombinasi surface tertentu
- **Usaha:** S · **Area:** Semua · **Status:** `[x]` ✅ **Batch 9** *(rasio diverifikasi perhitungan)*
- **Lokasi:** `tailwind/output.css:415` (`#64748b` masih hidup di bundle); `pengawas_detail.html:1626` (ikon 18px `#64748b`); teks merah kecil `#ef4444` di kartu `#1e1e32` = **4.34:1** (di bawah 4.5; aman di bg utama)
- **Rekomendasi:** Teks/chip merah kecil → `--color-danger-light` seperti pola `.tone-danger`; test kontras pasangan fg×{#09090e,#14141f,#0d0d1e,#1e1e32}.

---

## 5.7 RE-REVIEW RONDE 4 — Temuan baru pasca Batch 9

> **Tanggal:** 24 Agustus 2026 · **Basis kode:** `a1afd9c` (pasca Batch 9, suite 507/507 hijau) · **Metode:** 3 reviewer paralel (area admin, area publik, lintas-halaman/design-system) + verifikasi manual silang temuan kunci.
> Fokus khusus ronde ini: regresi Batch 9 (toast terpusat, guard editor, kontras gradien, dirty tracking) dan audit terukur lanjutan. Penomoran ID melanjutkan ronde sebelumnya.

### Status verifikasi cepat

**Regresi Batch 9 (spot-check langsung ke kode):**

| Item | Vonis | Bukti kunci |
|---|---|---|
| T14 toastContainer | ✅ BERES | Container tunggal `partials/nav.html:98`; kelima halaman admin memuat partial; `login.html` tak memuat nav → tak terdampak |
| T15 guard editor soal | ✅ BERES | `replaceEditorQuestions()` dipakai quickGenerate & importXML; jalur lain hanya pemuatan awal dari server (sah) |
| S37 guard upload | ✅ BERES, robust | Capture listener + lock display/classList no-op saat in-flight; pill ter-wiring penuh *(sisa race 300 ms → R48)* |
| S39 dirty tracking | ⚠️ SEBAGIAN → **S47** | Indikator/beforeunload bekerja; pembersihan via observer toast satu-slot bisa salah-bersih |
| S40 export blob | ✅ BERES | Content-Disposition + guard dobel-klik *(celah pesan SyntaxError → R47)* |
| S41/R31/R32/R33 | ✅ BERES | `navigator.clipboard`=0 di template; toast bernama; label-for 102/113; paritas close modal password ✓ |
| R29 onclick render-JS | ⚠️ PARSIAL → **S51** | Target asli bersih, tapi ±11 onclick tersisa di render-path users/modal dinamis |

**Item lama:**

| Item | Status ronde 4 | Bukti |
|---|---|---|
| R4 `document.write` | 🔴 MASIH TERBUKA, 5 lokasi (3 ronde berturut-turut) | `download.html:532,550,692,771` · `settings.html:2156` (shift dari 2130); test patuh-bug masih ada |
| R27 `!important` | ⚠️ Turun signifikan: admin-base 55→**47**, hasil 64→**63**, public-mobile 81→**48** (−40%), public-desktop 34→**21** — tapi **+63 baru** ditemukan di blok `<style>` inline settings.html (di luar cakupan audit per-file CSS) | audit terukur ronde 4 |
| Angka token | settings.html diam (100 hex/109 rgba); shared 27→21 hex; download 16→12 hex; **margin plafon rgba folder tinggal 2** (223 vs ≤225) → S58 | audit terukur |
| Matriks kontras WCAG | Pelanggaran AA aktif yang tersisa hanya endpoint gradien nav/dashboard (**T18**) + ikon `#64748b` ×1 (**R53**); `#ef4444` di `#1e1e32` kini hanya ikon/asterisk (lolos ambang non-teks 3:1) | perhitungan luminance |
| R26 EN sisa | "Refresh" ×4 (`settings.html:956,1299`, `pengawas_detail.html:226,268`) + "Export XML" ×1 (`dashboard.html:905`) → **R50** | grep |
| R30 / S21/S22 / P3 / duplikasi modal password | Tetap terbuka sesuai catatan sebelumnya | — |

---

### T17 — Regresi R37: deep-link `#kunci` membuat KEDUA panel (Nilai + Kunci) tampil bertumpuk
- **Prioritas:** 🔴 Tinggi · **Usaha:** XS · **Area:** Publik (Hasil) · **Status:** `[x]` ✅ **Batch 10**
- **Lokasi:** `hasil.html:400` (interaksi dengan `:328` dan `switchTab` `:862-863`)
- **Bukti:** init mengikuti hash (`switchTab(resolveTabFromHash())`), tetapi `loadResults()` selalu `document.getElementById('scoresContent').style.display = 'block'` tanpa membaca `currentTab`.
- **Dampak:** Membuka `/hasil/<token>#kunci` (pola share yang didorong fitur deep-link Batch 9) → tabel Nilai dan grid Kunci bertumpuk dalam satu kartu, tab aktif menunjukkan "Kunci Jawaban".
- **Rekomendasi:** Di akhir `loadResults()` panggil `switchTab(currentTab, { skipHash: true })` (atau guard `if (currentTab === 'scores')`); test vm: hash `#kunci` → setelah loadResults panel nilai tetap `display:none`.

### T18 — Kontrak T16 tidak tuntas: tombol submit instansi di nav & dashboard masih gradien gagal AA
- **Prioritas:** 🔴 Tinggi · **Usaha:** XS · **Area:** Admin · **Status:** `[x]` ✅ **Batch 10** *(rasio diverifikasi perhitungan)*
- **Lokasi:** `nav.html:152` (modal onboarding instansi — layar pertama admin baru), `dashboard.html:1071` (edit instansi): `linear-gradient(135deg, #a855f7, #6366f1)` + teks putih ~15px bold
- **Masalah:** Putih di `#a855f7` = **3.96:1**, di `#6366f1` = **4.47:1** — keduanya < 4.5:1. Token `--grad-btn-*` yang lolos AA (Batch 9) sudah ada tapi tak dipakai di sini; whitelist larangan endpoint lama hanya mencakup template publik.
- **Rekomendasi:** Ganti kedua gradien ke `var(--grad-btn-violet-start/end)`; perluas whitelist larangan `#a855f7|#6366f1` ke template admin.

### S47 — Pembersihan dirty S39 berbasis observasi toast bisa membersihkan KARTU YANG SALAH
- **Usaha:** S · **Area:** Admin (Settings) · **Status:** `[x]` ✅ **Batch 10**
- **Lokasi:** `settings-general.js:241-252`, `:277-287` — slot global tunggal `SAAS_PENDING_SAVE` ditimpa setiap klik simpan; toast sukses APA PUN membersihkan kartu yang menunggu.
- **Dampak:** Klik "Simpan SMTP" lalu cepat klik "Simpan Footer" → toast pertama membersihkan titik dirty **Footer** yang belum tersimpan; toast sukses ganti-password juga bisa membersihkan kartu yang requestnya gagal — editan tampak "tersimpan" padahal tidak.
- **Rekomendasi:** Pindahkan pembersihan ke jalur sukses `saveSaasSection` (teruskan cardId, panggil `clearSaasCardDirtyByCardId` di cabang success); hapus observer toast.

### S48 — Pencarian peserta di monitoring masih Enter-only (S5 tidak pernah sampai ke sini)
- **Usaha:** XS · **Area:** Admin (Pengawasan) · **Status:** `[x]` ✅ **Batch 10**
- **Lokasi:** `pengawas_detail.html:264-265` (`onkeyup="if(event.key==='Enter') loadDetail()"`); helper `initLiveSearch` core sudah ada dan dipakai `pengawas.html:436`. Select filter `:256` masih `onchange` inline.
- **Dampak:** Pengawas mengetik nama siswa lalu menunggu hasil yang tak pernah datang — gejala persis S5 — di halaman momen paling terburu-buru.
- **Rekomendasi:** Wire `initLiveSearch(input, …loadDetail…)` di init pengawas_detail; migrasi onchange select ke data-action.

### S49 — Kartu info ujian Hasil Ujian menampilkan waktu UTC mentah; `.utc-date` kelas mati
- **Usaha:** XS–S · **Area:** Admin (Submissions) · **Status:** `[x]` ✅ **Batch 10**
- **Lokasi:** `submissions.go:245-251` (Format tanpa `.In(jakarta)`) + `submissions.html:201,206,212`; kanonik server `formatExamTime` (WIB, `main.go:271-275`) justru dipakai badge di halaman yang sama (`:166`). Variannya bocor ke popup kuota user: `admin.js:1674` (`expires_at` mentah).
- **Dampak:** Guru WIB melihat "Upload: 03:11" padahal ujiannya 10:11 — jam salah pada data resmi hasil ujian, di samping badge yang benar.
- **Rekomendasi:** `formatExamTime` untuk ketiga field Go; `formatDateTimeID` untuk expires_at; hapus kelas mati.

### S50 — Modal konfirmasi Izinkan/Tolak tidak menyebut identitas siswa
- **Usaha:** XS · **Area:** Admin (Pengawasan) · **Status:** `[x]` ✅ **Batch 10**
- **Lokasi:** `pengawas_detail.html:1827-1838` — "mengizinkan **perangkat ini**…" padahal nama tersedia via `findApprovalStudentName` (`:1414-1421`, sudah dipakai toast R31).
- **Dampak:** Antrean berisi beberapa perangkat → pengawas harus mengingat posisi baris di balik modal; salah-approve antar-perangkat sulit dibedakan setelahnya.
- **Rekomendasi:** "Izinkan **Budi (AA:BB:CC…)** memulai ujian?" — fallback "(Anonim)" seperti baris antrean.

### S51 — Klaim R29 belum penuh: ±11 onclick tersisa di render-path users/modal dinamis admin.js; guard test per-snippet
- **Usaha:** M · **Area:** Admin · **Status:** `[x]` ✅ **Batch 10**
- **Lokasi:** `admin.js:860,873-874,1686,1696,1702-1703,1758,1766,2341,2352` — termasuk interpolasi username berlapis escaping manual ke atribut onclick (`:1686,:1696,:1703`). Guard Batch 9 hanya mengunci snippet tertentu (`uiux-batch9-jscore.test.mjs:375-393`).
- **Rekomendasi:** Migrasi baris aksi user + modal dinamis ke data-action; tambahkan asersi sweep "string render users-list bebas onclick=".

### S52 — Toggle auto-approve aktif tanpa konfirmasi: satu ketukan mengubah perilaku SERVER
- **Usaha:** XS · **Area:** Admin (Pengawasan) · **Status:** `[x]` ✅ **Batch 10**
- **Lokasi:** `pengawas_detail.html:217-220` (markup), `:2087-2113` (handler POST langsung tanpa showConfirm); penjelasan hanya di `title` (tak muncul di sentuhan).
- **Dampak:** Ketukan tak sengaja saat scroll antrean di HP → semua siswa berikutnya disetujui tanpa pemeriksaan, bertahan walau halaman ditutup. Sisi lain confirm-fatigue R30: nol friksi pada aksi berdampak luas.
- **Rekomendasi:** Saat meng-AKTIFKAN saja: `showConfirm("Aktifkan terima otomatis? …")`; mematikan boleh langsung.

### S53 — Init tab hasil dieksekusi tanpa guard state error: TypeError + listener hashchange mati
- **Usaha:** XS · **Area:** Publik (Hasil) · **Status:** `[x]` ✅ **Batch 10**
- **Lokasi:** `hasil.html:325-339` — `switchTab(resolveTabFromHash())` dijalankan tanpa syarat; elemen `#tabScores` hanya dirender di cabang sukses (`:178-185`) → `TypeError: null` di semua halaman error/disabled; `hashchange` tak pernah terpasang.
- **Rekomendasi:** Bungkus init tab dengan guard `!isDisabled && !pageHasError` atau early-return bila `#tabNav` tak ada.

### S54 — Tab Nilai/Kunci hasil tanpa semantik ARIA tabs — inkonsisten dengan pola terbaik sendiri di download
- **Usaha:** S · **Area:** Publik (Hasil) · **Status:** `[x]` ✅ **Batch 10**
- **Lokasi:** `hasil.html:178-185` (button polos, aria-label menduplikasi teks) vs pembanding benar `download.html:490-503` (role tablist/tab/tabpanel, aria-selected, roving tabindex, panah — pola S14).
- **Rekomendasi:** Port persis pola download (role + aria-selected di switchTab + keydown panah).

### S55 — Replika CSS toast di download adalah salinan pra-T10a: `.toast-close` invisible bagi keyboard
- **Usaha:** XS · **Area:** Publik (Download) · **Status:** `[x]` ✅ **Batch 10** *(diverifikasi: tanpa rule :focus-visible)*
- **Lokasi:** `download.html:426-443` — blok lokal "direplikasi dari output.css" tanpa rule `:focus-visible` (fix T10a Batch 2 tak pernah berlaku karena halaman ini sengaja tak memuat output.css).
- **Dampak:** Keyboard user Tab ke tombol ✕ toast yang opacity:0 — bug T10a hidup lagi di halaman unduhan.
- **Rekomendasi:** Salin rule `:focus-visible` dari `output.css:1183`; lebih tahan-regresi: ekstrak CSS toast ke file bersama.

### S56 — reset_password memuat admin-core.js sinkron — satu-satunya halaman publik yang begitu
- **Usaha:** XS · **Area:** Publik · **Status:** `[x]` ✅ **Batch 10**
- **Lokasi:** `reset_password.html:179` (tanpa defer; pembanding `hasil.html:58` defer). Script besar itu hanya dipakai untuk `togglePasswordVisibility`.
- **Dampak:** Render tertunda di halaman auth yang dibuka user locked-out dari HP — inkonsisten dengan disiplin defer S42.
- **Rekomendasi:** Tambah `defer`; jangka menengah lepaskan dependensi admin-core dari halaman auth publik.

### S57 — Blok inline TERBESAR produk tak pernah masuk radar: pengawas_detail (style 825 + script 1022 baris)
- **Usaha:** L · **Area:** Lintas · **Status:** `[ ]` ⏸ **DITUNDA** — ekstraksi blok inline 1847 baris mematahkan kontrak fs-read statik banyak suite (pola penundaan R33); kerjakan bersama reformasi harness per-file.
- **Fakta:** angka aktual ronde 4 — pengawas_detail **1847 baris gabungan** (1 blok style + 1 blok script inline), shared.html 812, settings.html 731 (9 blok, naik tipis dari 717). Semua bypass mekanisme cache/guard file CSS/JS.
- **Rekomendasi:** Ekstraksi berbasis ROI per-file (pola Batch 8): style → `pengawas-detail.css`, script → modul `pengawas-detail.js` yang memang sudah ada; shared blok utama → `public-base.css`.

### S58 — Migrasi rgba berhenti di hitam/putih: ±86 literal tersisa adalah pasangan PERSIS token triplet yang sudah ada
- **Usaha:** M · **Area:** Lintas · **Status:** `[x]` ✅ **Batch 10**
- **Fakta:** `admin-base.css`: `rgba(99,102,241,…)` ×23 (=--rgb-info), `rgba(239,68,68,…)` ×12 (=--rgb-danger), `rgba(16,185,129,…)` ×6 (=--rgb-success); `settings.html`: white ×43 + black ×38 (token ada sejak Batch 8). Margin plafon rgba folder tinggal **2** — fitur berikutnya hampir pasti membuat test merah tanpa pekerjaan migrasi yang jelas.
- **Rekomendasi:** Dua batch migrasi substitusi-persis mekanis (pola Batch 8); plafon settings bisa turun 109→±28.

### S59 — CSP-safety asimetris: admin 0 onclick (ter-guard), publik masih 17 onclick inline tanpa guard
- **Usaha:** S–M · **Area:** Publik · **Status:** `[x]` ✅ **Batch 10**
- **Lokasi:** `download.html` ×7 (mis. `:535`), `hasil.html` ×7 (`:179,:182,:195,:232,:236` + render-JS `:358,:419`), `shared.html:20,:27`, `register_confirm.html:260`. Registry `Actions` sudah tersedia di halaman yang memuat core (hasil & download).
- **Rekomendasi:** Jadikan "0 inline handler di templates/public/**" kontrak test (pola R29); migrasi bertahap mulai hasil.html.

### R42 — Strength meter password register ↔ reset_password sudah drift (hex vs token)
- **Usaha:** XS–S · **Area:** Publik · **Status:** `[x]` ✅ **Batch 10**
- **Lokasi:** `register.html:59-62,407-413` (`var(--color-danger)`) vs `reset_password.html:73-76,261-267` (`#ef4444` literal) — port R22 belum ikut migrasi token Batch 8; drift pertama telah terjadi.
- **Rekomendasi:** Samakan reset ke token; asersi test bahwa kedua blok identik.

### R43 — Print rekap: baris detail jawaban terbuka terpotong batas 380px
- **Usaha:** XS · **Area:** Publik (Hasil) · **Status:** `[x]` ✅ **Batch 10**
- **Lokasi:** `hasil.css:420-423` (`max-height:380px; overflow-y:auto`) vs blok print `:967-983` (tidak me-reset) — dokumen cetak hanya memuat ±380px pertama jawaban tanpa indikasi lanjutan.
- **Rekomendasi:** Blok print tambah `.answer-grid { max-height:none; overflow:visible; }`.

### R44 — Race device_fingerprint: submit cepat → field kosong tanpa sinyal
- **Usaha:** S · **Area:** Publik (auth) · **Status:** `[x]` ✅ **Batch 10**
- **Lokasi:** `device-fingerprint.js:38-54` + field hidden `register.html:254` — `FingerprintJS.load().then(...)` tetap async ratusan ms meski urutan defer benar; autofill password-manager + Enter bisa mengirim `device_fingerprint=""` → rate limiter diam-diam jatuh ke dimensi IP saja.
- **Rekomendasi:** Saat submit bila field kosong & generate() in-flight: tahan kirim via `Promise.race` timeout ±1,5 dtk.

### R45 — Toast error menyambung exception mentah: "Gagal menghubungi server: [object TypeError]"
- **Usaha:** XS · **Area:** Admin (Pengawasan) · **Status:** `[x]` ✅ **Batch 10**
- **Lokasi:** `pengawas_detail.html:1154`, `:1183` — `'…' + err` pada jalur fetch mulai/hentikan pengawasan.
- **Rekomendasi:** Pesan statis "Periksa koneksi."; detail ke console.error.

### R46 — `tombstoned_at` mentah & tanpa escaping di atribut title badge daftar pengawasan
- **Usaha:** XS · **Area:** Admin (Pengawasan) · **Status:** `[x]` ✅ **Batch 10**
- **Lokasi:** `pengawas.html:288-289` — timestamp UTC mentah (melanggar satu-pintu R31 yang dipakai tiga baris di bawahnya) + interpolasi tanpa escapeHtml ke atribut.
- **Rekomendasi:** `escapeHtml(jsEscape(…))` + `formatDateTimeID`; samakan versi template `submissions.html:166`.

### R47 — Ekspor Excel: body error non-JSON (proxy 502 HTML) → toast berisi SyntaxError
- **Usaha:** XS · **Area:** Admin (Submissions) · **Status:** `[x]` ✅ **Batch 10**
- **Lokasi:** `admin.js:3351-3354` — `resp.json()` reject `SyntaxError: Unexpected token '<'` yang tampil mentah di toast `:3376`.
- **Rekomendasi:** `.catch(() => ({}))` sebelum baca message, atau cek content-type.

### R48 — Race tutup→buka uploadModal: setTimeout 300ms menutup modal yang baru dibuka ulang
- **Usaha:** XS · **Area:** Admin (Settings) · **Status:** `[x]` ✅ **Batch 10**
- **Lokasi:** `settings-system-apps.js:144-152` vs `:126-134` — timeout lama tak dibatalkan saat modal dibuka lagi <300 ms.
- **Rekomendasi:** Simpan handle timeout; openUploadModal melakukan clearTimeout / cek generasi.

### R49 — Error state daftar pengawasan dead-end tanpa aksi pemulihan
- **Usaha:** XS · **Area:** Admin (Pengawasan) · **Status:** `[x]` ✅ **Batch 10**
- **Lokasi:** `pengawas.html:249`, `:340` — "Gagal memuat data" statis tanpa tombol Coba Lagi/auto-retry; pembanding terbaik sendiri: daftar user (`admin.js:1758`) & hasil.html (G6).
- **Rekomendasi:** Tambah button `data-action` "Coba Lagi" pada kedua state error.

### R50 — Ekor R26: "Refresh" ×4 & "Export XML" ×1
- **Usaha:** XS · **Area:** Admin · **Status:** `[x]` ✅ **Batch 10**
- **Lokasi:** `settings.html:956,1299`; `pengawas_detail.html:226,268` (title); `dashboard.html:905` ("Export XML", padahal submissions sudah "Ekspor Excel").
- **Rekomendasi:** "Muat Ulang"/"Segarkan" + "Ekspor XML"; masuk test bahasa.

### R51 — Heading order melompat turun: h2 → h4 di download & submissions
- **Usaha:** XS · **Area:** Publik + Admin · **Status:** `[x]` ✅ **Batch 10**
- **Lokasi:** `download.html:516→525` (skip h3; sama `:769`); `submissions.html:159→182` (juga `:197,:220,:236`). R19 memperbaiki arah naik, bukan turun.
- **Rekomendasi:** Turunkan h4 flavor/section-title jadi h3 (visual via class — pola R19); assertion urutan heading di test.

### R52 — Ekor arwah di nav.html: z-index `99999` onboarding & skip-link inline `left:-9999px`
- **Usaha:** XS · **Area:** Admin · **Status:** `[x]` ✅ **Batch 10**
- **Lokasi:** `nav.html:126` (`z-index:99999` mengalahkan toast/skip-link; dirender global) dan `nav.html:8` (skip-link gaya inline, dua gaya dalam satu partial — sisa S33/R12 yang scopenya hanya CSS inti).
- **Rekomendasi:** Token z (mis. `--z-onboarding`) di bawah toast; skip-link nav pakai class `.skip-link` theme.css.

### R53 — Ikon `#64748b` tersisa satu lokasi (klaim R41 "bersih" tidak penuh)
- **Usaha:** XS · **Area:** Admin (Pengawasan) · **Status:** `[x]` ✅ **Batch 10** *(rasio diverifikasi perhitungan)*
- **Lokasi:** `pengawas_detail.html:1646` — render-JS riwayat perangkat; `#64748b` di kartu = 3.43:1 (ikon 18px, lolos non-teks 3:1, tetapi satu-satunya sisa di seluruh templates/ yang dilarang T9; ikon sejenis di file sama sudah var(--color-text-muted)).
- **Rekomendasi:** Ganti ke token; masukkan `#64748b` ke hex terlarang guard template.

---

## 5.8 RE-REVIEW RONDE 5 — Temuan baru pasca Batch 10

> **Tanggal:** 24 Agustus 2026 · **Basis kode:** `2debff6` (pasca Batch 10, suite 569+/569+ hijau) · **Metode:** 3 reviewer paralel (area admin, area publik, lintas-halaman/design-system) + verifikasi manual silang temuan kunci.
> Fokus khusus ronde ini: verifikasi eksekusi Batch 10 (termasuk integritas klaim `[x]`) dan audit terukur lanjutan. Penomoran ID melanjutkan ronde sebelumnya.

### Status verifikasi cepat

**Regresi/eksekusi Batch 10 (spot-check langsung ke kode):**

| Item | Vonis | Bukti kunci |
|---|---|---|
| T17 deep-link #kunci | ✅ BERES di jalur utama *(fungsionalitas tab kini mati untuk mouse oleh T19)* | `hasil.html:463` menutup dengan switchTab(currentTab); paginasi hidup dalam panel Nilai |
| T18 gradien instansi | ✅ BERES | grep `#a855f7\|#6366f1` templates/admin = 0; guard endpoint terlarang ada |
| S47 dirty-clear | ✅ BERES | **8/8** pemanggil save meneruskan cardId yang cocok; pembersihan hanya cabang success; observer toast dihapus |
| S48 live-search | ⚠️ SEBAGIAN → **S60** | Wiring benar & debounce jalan, TAPI callback tak me-reset halaman: cari dari page >1 → hasil kosong palsu |
| S49 waktu WIB | ⚠️ SEBAGIAN → **S61**, **R59** | submissions.go konsisten & lebih robust; sub-item `expires_at` popup kuota TIDAK dieksekusi (klaim Batch 10 tidak ada di diff — diverifikasi `git show`); fallback tz main.go vs submissions.go tak seragam |
| S50/S52 modal identitas & auto-approve | ✅ BERES *(catatan kecil → R54: tombol default "Ya, Hapus" merah)* | formatApprovalStudentLabel dipakai kedua cabang; showConfirm + revert switch benar |
| S51 onclick users | ✅ BERES | grep onclick= admin.js = 0; render users sepenuhnya data-action |
| S55–R44, R45–R53 | ✅ SELURUHNYA BERES | focus-visible download; defer reset_password; token strength meter; print max-height:none; Promise.race 1500ms; dst. |
| R51 heading order | 🔴 **FALSE POSITIVE → T20** | Semua lokasi yang dikutip masih `<h4>`; tidak ada perubahan h3/h4 di diff `2debff6`; assertion urutan heading juga tidak ada |
| S59 "0 onclick publik" | 🔴 SEBAGIAN → **T21**, **S62** | Guard hanya memindai `onclick=` — **36 handler inline non-onclick** (onsubmit/onkeyup/oninput/onchange) lolos radar; +4 onclick tersisa di settings-billing.js & settings-voucher-audit.js |

**Audit terukur pasca Batch 10:**

| Metrik | Angka | vs ronde 4 |
|---|---|---|
| hex templates/ | **241** | ≈256 → turun |
| rgba literal templates/ | **141** | 223 → turun 37% |
| Margin plafon folder | hex **59**, rgba **84** | rgba pulih dari margin 2 (S58 berhasil) |
| hasil.css `!important` | **65** | 63 → NAIK (+2 dari fix R43 print — lihat R61) |
| Matriks kontras AA aktif | Praktis bersih | Endpoint gradien pasca-T16/T18 lolos semua; watchlist: endpoint lama masih hidup di JS render-path (billing.js) |

Item lama tetap terbuka: R4 document.write (`download.html:537,555,697,776`, `settings.html:2156`), S21/S22, P3, R30, S57, duplikasi modal password.

---

### T19 — Regresi S59: registrasi `Actions` halaman publik kalah race terhadap admin-core.js ber-`defer` — tab, paginasi, unduhan MATI untuk klik mouse
- **Prioritas:** 🔴 Tinggi · **Usaha:** XS–S · **Area:** Publik (Hasil + Download) · **Status:** `[x]` ✅ **Batch 11** *(terverifikasi manual)*
- **Lokasi:** `hasil.html:58` (core defer) + blok registrasi top-level `:369-382`; `download.html:2` + `:842-850`. Pembanding benar: halaman admin memuat core sinkron.
- **Bukti:** Inline script akhir-body dieksekusi SAAT parsing; script `defer` dieksekusi SETELAH parsing selesai. Saat guard `if (typeof Actions !== 'undefined')` dievaluasi, nilainya pasti undefined → seluruh `Actions.register(...)` dilewati permanen tanpa retry.
- **Dampak:** `/hasil/<token>`: klik mouse pada tab Daftar Nilai/Kunci, paginasi prev/next, clear-search, dan "Coba Lagi" tak berfungsi sama sekali (navigasi panah keyboard masih jalan karena terpasang di DOMContentLoaded). `/download`: ketiga tab platform mati untuk klik. Ini regresi fungsional langsung dari migrasi onclick→data-action Batch 10.
- **Rekomendasi:** Pindahkan blok registrasi ke dalam `DOMContentLoaded`; test vm yang mengeksekusi skrip dalam urutan nyata (inline → core defer → DOMContentLoaded) lalu asersi `Actions.has('switch-tab')`.

### T20 — Klaim R51 false-positive: heading order tidak pernah diperbaiki dan assertion tidak pernah ditulis
- **Prioritas:** 🔴 Tinggi · **Usaha:** XS · **Area:** Lintas · **Status:** `[x]` ✅ **Batch 11** *(diverifikasi `git show`)*
- **Lokasi:** `download.html:530,553,774` masih `<h4 class="flavor-title">` setelah `<h2>`; `submissions.html:182,197,220,236` masih empat `<h4>` setelah `<h2>`; grep 38 suite = 0 asersi urutan heading.
- **Dampak:** Item dicatat `[x] ✅ Batch 10` padahal fix tak pernah mendarat — outline screen reader tetap melompat, dan rekap tracking kehilangan kredibilitas.
- **Rekomendasi:** Eksekusi ulang R51 (h4→h3, visual via class) + assertion urutan heading; audit silang sampel acak klaim `[x]` Batch 9–10.

### T21 — Kontrak CSP "0 handler inline" memberi rasa aman palsu: 36 handler non-onclick tersisa
- **Prioritas:** 🔴 Tinggi · **Usaha:** S · **Area:** Lintas · **Status:** `[x]` ✅ **Batch 11**
- **Lokasi/bukti:** grep `\son(click|change|submit|keyup|input)=` templates = **36**: settings.html ×20 (mis. `:795 onsubmit="createUser(event)"`, `:1112/:1248/:1308 onkeyup Enter-only`, `:2191 onsubmit="submitChangePassword(event)"`), dashboard ×13, nav ×1, publik `hasil.html:197` (oninput multi-statement).
- **Dampak:** Semuanya tetap butuh CSP `unsafe-inline`; guard S59 hanya memindai `onclick=` sehingga suite hijau padahal masalahnya utuh — preseden handler inline baru "tak terhitung".
- **Rekomendasi:** Perluas guard ke regex `\son[a-z]+=` di templates/**; migrasi bertahap mulai `hasil.html:197` & form onsubmit (listener submit delegasi).

### S60 — Live-search & filter peserta tidak me-reset halaman: "Pencarian tidak ditemukan" palsu
- **Usaha:** XS · **Area:** Admin (Pengawasan) · **Status:** `[x]` ✅ **Batch 11**
- **Lokasi:** `pengawas_detail.html:2146` (callback tanpa argumen) + `loadDetail` :1503 (`SUB_PAGE` hanya berubah bila argumen diberikan) + server `pengawas.go:380-382` (tanpa clamp ke total_pages). Pola sama `pengawas.html:237`.
- **Dampak:** Di halaman 3, mencari nama siswa → request `page=3` dari hasil terfilter → tabel kosong padahal siswa ada.
- **Rekomendasi:** Wrapper live-search & listener change memanggil `loadDetail(1)` / `loadPengawasExams(1)`.

### S61 — Sub-item S49 tidak tuntas: `expires_at` popup kuota user MASIH UTC mentah
- **Usaha:** XS · **Area:** Admin (Settings/Users) · **Status:** `[x]` ✅ **Batch 11**
- **Lokasi:** `admin.js:1674` (`const expiresAt = user.expires_at || '—'`) dirender `:1721` samping "Terdaftar" yang sudah WIB (`:1675`). Diverifikasi `git show 2debff6`: titik ini tak tersentuh meski diklaim di rekap.
- **Rekomendasi:** `user.expires_at ? formatDateTimeID(user.expires_at) : '—'` (+ cek blok edit ~:2090); asersi render popup bebas interpolasi mentah.

### S62 — onclick render-JS lolos kedua guard di settings-billing.js & settings-voucher-audit.js
- **Usaha:** XS–S · **Area:** Admin · **Status:** `[x]` ✅ **Batch 11**
- **Lokasi:** `settings-billing.js:134,158`; `settings-voucher-audit.js:9,86` (interpolasi id/page mentah ke atribut). Guard S59 hanya pindai template publik; R29 hanya snippet admin.js tertentu.
- **Rekomendasi:** Migrasi ke Actions.register; sweep `settings-*.js bebas \sonclick=` di suite guard.

### S63 — Kelompok ad-hoc terbesar tersisa: `#fff` ×66 di settings.html (saudara rgba-white yang sudah dimigrasi)
- **Usaha:** XS–S · **Area:** Admin · **Status:** `[x]` ✅ **Batch 11**
- **Fakta:** dari 100 hex settings.html, 66 adalah `#fff` — baris yang sama kerap campur `rgba(var(--rgb-black), …)` (contoh `:1112`). Watchlist terkait: endpoint lama `rgba(168,85,247,…)` masih hidup di JS render-path di luar whitelist gradien (billing.js:134).
- **Rekomendasi:** Satu pass `#fff|#ffffff` → token semantik sesuai konteks; perluas whitelist larangan endpoint lama ke static/js/*.js; tambahkan cap hex per-file settings.

### S64 — Plafon guard basi: beberapa file TEPAT di plafon, settings longgar 82
- **Usaha:** XS · **Area:** Tooling · **Status:** `[x]` ✅ **Batch 11**
- **Fakta:** tepat-di-plafon: admin-base.css rgba 17/17, dashboard 32/32, register_confirm 19/19, pengawas/pengawas_detail/download 11/11, hasil 10/10 — fitur berikutnya langsung merah. Sebaliknya settings cap 110 vs aktual 28 (bisa nambah 82 literal tanpa alarm). `!important` CSS tak dikunci test apa pun.
- **Rekomendasi:** Kunci ulang cap per-file = angka aktual; pecah plafon settings; tambahkan count `!important` per-file ke guard.

### R54 — Konfirmasi AKTIFKAN auto-approve memakai tombol default merah "Ya, Hapus"
- **Usaha:** XS · **Area:** Admin (Pengawasan) · **Status:** `[x]` ✅ **Batch 11**
- **Lokasi:** `pengawas_detail.html:2104-2107` — showConfirm satu argumen → confirmLabel default `'Ya, Hapus'` + class `.btn-delete` (admin-core.js:497,:517). Pembanding benar: `regenerateActiveToken` mengirim label eksplisit.
- **Rekomendasi:** `showConfirm(msg, '', 'Ya, Aktifkan', 'Batal')`.

### R55 — Dua kalkulator durasi berlomba di submissions ("2j 5m" vs "2 jam 5 menit")
- **Usaha:** XS · **Area:** Admin (Submissions) · **Status:** `[x]` ✅ **Batch 11**
- **Lokasi:** `admin.js:4061-4085` (singkat, skip diff≤0) vs `submissions.html:421-448` (verbose, clamp 0) — keduanya terdaftar DOMContentLoaded pada target `.duration-cell`; inline menimpa output admin.js sehingga satu blok duplikat-mati-berjalan, cukup perubahan urutan script untuk format berubah.
- **Rekomendasi:** Satu formatter saja (versi verbose); hapus blok lain.

### R56 — Info paginasi peserta monitoring tanpa offset: "Menampilkan 20 dari 57" di semua halaman
- **Usaha:** XS · **Area:** Admin (Pengawasan) · **Status:** `[x]` ✅ **Batch 11**
- **Lokasi:** `pengawas_detail.html:1591`; pembanding benar `admin.js:1792` (rentang start–end).
- **Rekomendasi:** Hitung start/end → "Menampilkan 41–57 dari 57 perangkat".

### R57 — `localizeUTC` lokal pengawas_detail menimpa alias core: dua format tanggal dalam satu produk
- **Usaha:** XS · **Area:** Admin (Pengawasan) · **Status:** `[x]` ✅ **Batch 11**
- **Lokasi:** `pengawas_detail.html:1619-1627` mendefinisikan ulang alias core (:413-416 "jangan tambahkan pemakaian baru") → kolom waktu monitoring "24 Agu 10.11" vs halaman lain "2026-08-24 10:11".
- **Rekomendasi:** Hapus definisi lokal (jatuh ke alias core) atau tambahkan varian resmi di core.

### R58 — Countdown kedaluwarsa akun: "Kedaluwarsa hari ini" nyaris mustahil (Math.ceil)
- **Usaha:** XS · **Area:** Admin (Dashboard) · **Status:** `[x]` ✅ **Batch 11**
- **Lokasi:** `dashboard.html:1032-1036` — sisa 10 menit → ceil = "1 hari lagi"; cabang hari-ini hanya kena bila sisa tepat 0 ms.
- **Rekomendasi:** Floor untuk sisa >0 + ambang jam (<24h → "hari ini"/"± X jam").

### R59 — Fallback zona tak seragam: main.go diam-diam UTC bila tzdata hilang, submissions.go WIB
- **Usaha:** XS · **Area:** Admin · **Status:** `[x]` ✅ **Batch 11**
- **Lokasi:** `main.go:274-276` (error diabaikan, In(nil)=UTC) vs `submissions.go:33-38` (fallback FixedZone benar — pola Batch 10).
- **Dampak:** Di kontainer tanpa tzdata, badge jadwal dashboard/pengawas kembali UTC persis seperti bug S49 — hanya separuh yang ditambal.
- **Rekomendasi:** Ekstrak jakartaLoc+fallback ke helper bersama, atau import `_ "time/tzdata"` di main.

### R60 — Cache-busting manual drift: suffix `-N` tangan di atas `{{.version}}`
- **Usaha:** XS · **Area:** Publik + Admin · **Status:** `[x]` ✅ **Batch 11**
- **Lokasi:** `partials/head.html:11` (`?v={{.version}}-settings-tabs-1`), `hasil.html:25,59,60` (`-2`, `-5`, `-3`), sama di download/register/reset/cek_hasil/forgot.
- **Dampak:** Eksistensi suffix membuktikan rilis lama mengedit file tanpa bump version — mekanisme busting bocor; suffix manual pasti lupa di-bump (skenario proxy LAN R34).
- **Rekomendasi:** Hitung `?v=` dari hash-konten file (middleware murah untuk ±7 file) atau hapus semua suffix.

### R61 — hasil.css `!important` naik 63→65 (arah berlawanan metrik R27) & tak dikunci guard
- **Usaha:** XS · **Area:** Publik · **Status:** `[x]` ✅ **Batch 11**
- **Lokasi:** `git diff a1afd9c..2debff6 -- hasil.css`: fix R43 menambah 2 `!important` print; aktual kini 65 (tertinggi repo). Padahal blok print ada SETELAH rule sumber, specificity setara — important tak perlu.
- **Rekomendasi:** Hapus kedua important; tambahkan count `!important` per-file ke guard (gabung S64).

### R62 — Label grup OTP tanpa asosiasi programatik (ekor R32)
- **Usaha:** XS · **Area:** Publik · **Status:** `[x]` ✅ **Batch 11**
- **Lokasi:** `register_confirm.html:240`, `reset_password.html:129` — label non-wrapping tanpa for; sisa label tanpa-for lain sah (implicit wrapping).
- **Rekomendasi:** `for="otp-1"` atau aria-labelledby ke container group; asersi "label non-wrapping wajib ber-for".

### R63 — State error 404 "Ujian Tidak Ditemukan" dead-end tanpa CTA
- **Usaha:** XS · **Area:** Publik (Hasil) · **Status:** `[x]` ✅ **Batch 11**
- **Lokasi:** `hasil.html:134-138` — kartu tanpa satu pun link/tombol; pembanding state disabled sudah benar punya "Kembali ke Beranda".
- **Rekomendasi:** Duplikat CTA "Kembali ke Beranda" + link "Coba Token Lain" → /hasil.

### R64 — Strength meter tak terhubung programatik: screen reader tak pernah mendengar feedback kekuatan
- **Usaha:** XS–S · **Area:** Publik (auth) · **Status:** `[x]` ✅ **Batch 11**
- **Lokasi:** `register.html:281,288-289` (+reset_password): input tanpa aria-describedby ke pwStrengthText; teks kekuatan tanpa aria-live.
- **Rekomendasi:** `aria-describedby="pwStrengthText"` + `aria-live="polite"`.

### R65 — Hint username tak menyebut huruf kecil wajib; konversi toLowerCase diam-diam
- **Usaha:** XS · **Area:** Publik (Register) · **Status:** `[x]` ✅ **Batch 11**
- **Lokasi:** hint `register.html:263` vs JS `:446` (toLowerCase) — "BudiGuru" tampil "budiguru" tanpa penjelasan (toast R9 hanya untuk karakter dihapus).
- **Rekomendasi:** Hint "Huruf kecil, angka, titik, garis bawah…".

### R66 — Zona waktu halaman hasil publik mengikuti jam perangkat penonton — kontradiksi dengan kartu guru (WIB)
- **Usaha:** XS · **Area:** Publik (Hasil) · **Status:** `[x]` ✅ **Batch 11**
- **Lokasi:** `hasil.html:715-716` (formatDateTimeID zona browser) vs kanonik server formatExamTimeWIB (fix S49) — submission sama bisa tampil beda jam di dua layar resmi (pola S49).
- **Rekomendasi:** Kirim string terformat WIB dari API hasil, atau dokumentasikan perilaku zona-penonton.

---

## 5.9 RE-REVIEW RONDE 6 — Temuan baru pasca Batch 11

> **Tanggal:** 24 Agustus 2026 · **Basis kode:** `4c87bc8` (pasca Batch 11, suite 666/666 hijau) · **Metode:** 3 reviewer paralel (area admin, area publik, lintas-halaman/design-system) + verifikasi manual silang temuan kunci.
> Fokus khusus ronde ini: verifikasi eksekusi Batch 11 dan sisa ekor polish. Penomoran ID melanjutkan ronde sebelumnya.

### Status verifikasi cepat

**Regresi/eksekusi Batch 11 (spot-check langsung ke kode):**

| Item | Vonis | Bukti kunci |
|---|---|---|
| T19 registrasi Actions DOMContentLoaded | ✅ BERES | hasil.html:337-356 & download.html:842-856; core defer tereksekusi sebelum event; fungsi terhoisting |
| T21 handler inline | ✅ BERES | grep `\son[a-z]+=` templates = 0; guard folder-wide rekursif benar; dashboard wiring setara (Enter pencarian via keydown, delegasi change lengkap) |
| S60 reset halaman live-search | ✅ BERES | loadDetail(1)/loadPengawasExams(1) di callback & fallback Enter |
| S49-lanjutan/R66 waktu WIB server | ✅ BERES | hasil.go jakartaLoc+FixedZone; field *_display dikirim, ISO dipertahankan untuk durasi klien — dua layar resmi konsisten |
| S47/S61/R55/R58/R62–R65 | ✅ BERES | dirty-clear 8 kartu; expires_at terformat; formatter durasi tunggal; countdown berbasis jam; label OTP for; CTA 404; aria strength meter; hint lowercase |
| S63 #fff settings | ✅ BERES (0 tersisa) | migrasi kontekstual diverifikasi per-baris |

**Audit terukur pasca Batch 12:** hex templates/**146** (plafon diperketat ≤150), rgba literal **140** (≤145) — margin folder kini sempit & bermakna; `#64748b`=0, `#fff` settings=0.

Item lama tetap terbuka: R4 document.write, R30, P3, S57, duplikasi modal password, S21/S22 (sebagian dieksekusi R69).

---

### T22 — Sistem konfirmasi KETIGA masih hidup di vouchers + gradien gagal-AA dipasang dari JS
- **Prioritas:** 🔴 Tinggi · **Usaha:** S · **Area:** Admin (Settings/Vouchers) · **Status:** `[x]` ✅ **Batch 12**
- **Lokasi (pra-fix):** `settings-vouchers.js:315-332` (showConfirmModal + trap sendiri), pemanggil :356/:391; gradien terlarang `linear-gradient(135deg,#a855f7,#6366f1)` + putih (:326, 3.96/4.47:1); markup `settings.html:1575`.
- **Dampak:** CTA destruktif hapus voucher gagal WCAG AA dan dialognya berbeda dari semua konfirmasi lain — drift sistem modal (S16) bangkit di titik baru.
- **Eksekusi Batch 12:** showConfirmModal + closeConfirmActionModal + modal arwah DIHAPUS; kedua pemanggil memakai showConfirm core dengan label eksplisit ("Ya, Matikan/Aktifkan Voucher", "Hapus Voucher"); registrasi arwah `confirm-action-close` dihapus; warna link voucher → var(--color-accent-light) (kontras naik). Whitelist larangan endpoint kini mencakup JS render-path.

### S65 — Kartu error "Coba Lagi" tak terlihat pada kegagalan fetch pasca-muat-pertama
- **Prioritas:** 🟡 Sedang · **Usaha:** XS · **Area:** Publik (Hasil) · **Status:** `[x]` ✅ **Batch 12**
- **Lokasi (pra-fix):** `hasil.html:447` menyembunyikan loadingIndicator permanen; ketiga jalur gagal menulis innerHTML tanpa memulihkan display → tombol pemulihan mustahil diklik pada gagal paginasi/pencarian berikutnya.
- **Eksekusi Batch 12:** ketiga jalur gagal men-set display='block' sebelum menulis error state; test vm sukses→gagal.

### S66 — Klaim R60 tidak penuh: suffix cache-busting manual lolos di login.html
- **Prioritas:** 🟡 Sedang (integritas proses) · **Usaha:** XS · **Status:** `[x]` ✅ **Batch 12**
- **Lokasi (pra-fix):** `admin/login.html:34-35` (`?v={{.version}}-5` / `-3`) — guard R60 hanya memindai folder publik.
- **Eksekusi Batch 12:** suffix dihapus; asersi anti-suffix diperluas ke template milik cakupan pengawasan-nav.

### S67 — Outline heading settings rusak dua arah: h1 "Aplikasi Sistem" di tengah dokumen
- **Prioritas:** 🟡 Sedang · **Usaha:** XS · **Area:** Admin · **Status:** `[x]` ✅ **Batch 12**
- **Lokasi (pra-fix):** `settings.html:1990` h1 di tengah outline (setelah 23 seksi h2/h3); tak ada judul halaman.
- **Eksekusi Batch 12:** turun ke h2; h1 sr-only kanonik "Pengaturan" ditambahkan di awal main; assertion urutan heading baru.

### S68 — Kelompok ad-hoc terbesar: `#f87171` ×24 + triplet info ×18 di hasil.css
- **Prioritas:** 🟡 Sedang · **Usaha:** S · **Area:** Lintas · **Status:** `[x]` ✅ **Batch 12**
- **Eksekusi Batch 12:** token baru `--color-danger-bright: #f87171` di theme.css; seluruh pemakaian publik ×10 + admin ×14 → var(--color-danger-bright); hasil.css rgba(99,102,241,α) ×18 → rgba(var(--rgb-info), α).
- **Catatan:** dashboard.html ×3 ikut dimigrasi koordinator saat integrasi.

### S69 — Stempel realtime "Diperbarui" jam perangkat pengawas + aria-live off
- **Prioritas:** 🟡 Sedang · **Usaha:** XS–S · **Area:** Admin (Pengawasan) · **Status:** `[x]` ✅ **Batch 12 (PARSIAL)** *(diverifikasi manual)*
- **Fakta:** stempel memakai `new Date()` perangkat (jam PC lab sering meleset) bersanding data WIB server; `aria-live="off"` membuat refresh senyap bagi screen reader.
- **Eksekusi Batch 12:** `aria-live="polite"` terpasang ✅; konversi timestamp-server DITUNDA (API belum menyertakan waktu respons) — komentar kode mendokumentasikan keterbatasan; format HH:MM:SS dipertahankan (detik bermakna untuk polling).

### R67 — Klik ✕ clear-search membuat fokus jatuh ke `<body>`
- **Usaha:** XS · **Area:** Publik (Hasil) · **Status:** `[x]` ✅ **Batch 12**
- **Lokasi (pra-fix):** `hasil.html:349-354`. **Fix:** akhir handler `input.focus()`.

### R68 — Strength meter good/strong masih hex + klaim asersi Batch 10 tidak akurat
- **Usaha:** XS · **Area:** Publik · **Status:** `[x]` ✅ **Batch 12**
- **Lokasi (pra-fix):** register.html:413-414 & reset_password.html:266-267 (`#22c55e`, `#06b6d4`). **Fix:** token --color-success/--color-accent-cyan + asersi blok bebas hex (klaim lama kini benar-benar ada penjaganya).

### R69 — Copywriting publik: badge "(Stable)" hard-coded & hero "Cloud Teraman"
- **Usaha:** XS · **Area:** Publik · **Status:** `[x]` ✅ **Batch 12**
- **Eksekusi Batch 12:** "(Stable)" dihapus (server tak mengirim data stabilitas — diverifikasi download.go); hero → "Ujian Digital Teraman & Siap Offline" (selaras keluarga S21/S22).

### R70 — Margin guard folder-wide melebar lagi + 2 file JS tak ter-guard
- **Usaha:** XS · **Area:** Tooling · **Status:** `[x]` ✅ **Batch 12**
- **Eksekusi Batch 12:** plafon folder-wide hex 300→150, rgba 225→145 (=aktual); baseline settings-system-apps.js & pengawas-detail.js ditambahkan; entri basi settings.html:110 dirapikan.

### R71 — Blok `<style>` inline disisipkan DI ANTARA link eksternal ×4 halaman publik
- **Usaha:** S · **Area:** Publik · **Status:** `[x]` ✅ **Batch 12**
- **Eksekusi Batch 12:** semua link stylesheet dipindah sebelum blok inline pertama (hasil/download/register/cek_hasil); asersi "0 link setelah style pertama" per halaman.

### R72 — Asterisk required `#f43f5e` di kartu form 4.44:1
- **Usaha:** XS · **Area:** Admin (Settings) · **Status:** `[x]` ✅ **Batch 12**
- **Eksekusi Batch 12:** ×4 → var(--color-danger-light) (pola R41/tone-danger).

### R73 — Info paginasi daftar ujian pengawasan tanpa offset (ekor R56)
- **Usaha:** XS · **Area:** Admin (Pengawasan) · **Status:** `[x]` ✅ **Batch 12**
- **Eksekusi Batch 12:** rentang "Menampilkan X–Y dari Z ujian" (test vm page 3/25 → "21–25 dari 25").

### R74 — Empty-state countdown rotasi mati (dead branch)
- **Usaha:** XS · **Area:** Admin (Pengawasan) · **Status:** `[x]` ✅ **Batch 12**
- **Eksekusi Batch 12:** guard ganda dipisah — pesan "Rotasi otomatis belum aktif" kini dapat tampil (test vm 4 kasus).

### R75 — Toast gagal antrean izin spam tiap 5 detik tanpa de-dup
- **Usaha:** XS · **Area:** Admin (Pengawasan) · **Status:** `[x]` ✅ **Batch 12**
- **Eksekusi Batch 12:** flag `approvalsErrorToasted` once-pattern (reset saat sukses) — test vm 3 gagal berturut = 1 toast.

### R76 — Label Izinkan/Tolak menyusut ±10,4px di layar ≤480px
- **Usaha:** XS · **Area:** Admin (Pengawasan) · **Status:** `[x]` ✅ **Batch 12**
- **Eksekusi Batch 12:** font-size 0.75rem, hemat lewat padding (target sentuh tetap aman).

---

## 5.10 RE-REVIEW RONDE 7 — Temuan baru pasca Batch 12

> **Tanggal:** 24 Agustus 2026 · **Basis kode:** `4fc2ab8` (pasca Batch 12, suite 666/666 hijau) · **Metode:** 3 reviewer paralel + verifikasi manual silang temuan kunci.
> Fokus khusus ronde ini: verifikasi eksekusi Batch 12 dan sisa ekor polish. Penomoran ID melanjutkan ronde sebelumnya.

### Status verifikasi cepat

| Item | Vonis | Bukti kunci |
|---|---|---|
| T22 konfirmasi voucher core | ⚠️ Mekanisme benar, **regresi tampilan → T23** | POST hanya setelah ok=true; label eksplisit; modal arwah hilang — TAPI showConfirm meng-escape pesan sedangkan pemanggil mengirim markup |
| T19 lanjutan Actions publik | ✅ BERES | Registry vs pemakaian ter-audit: tak ada aksi tak terdaftar; bebas race |
| S65 error state visible | ✅ BERES | Ketiga jalur (:428/:439/:500) memulihkan display |
| S67 heading settings | ✅ BERES | h1 sr-only "Pengaturan" :777; "Aplikasi Sistem" h2 :1975; assertion ada |
| R73–R76 | ✅ BERES (R76 → catatan R78) | Rentang paginasi, dead branch reachable, toast once-pattern; padding fix R76 mati karena inline style menang |
| S68 token danger-bright | ✅ BERES | #f87171 = 0 di templates/ |
| Guard handler inline | ✅ templates/** tetap 0 `\son[a-z]+=`; onclick render-JS JS = 0 |
| Cache-busting | ✅ 62× `?v={{.version}}`, 0 suffix manual |
| Kontras aktif | ⚠️ tersisa: S70 (vouchers tint), R81 (chevron non-teks), R72-saudara |

**Audit terukur:** hex templates **146**/150 · rgba literal **140**/145 · `!important`: hasil.css 65/cap 66 · public-mobile 48/**cap 81** · admin-base 47/**55** · public-desktop 21/**34** (slack total 55 → S71).

Item lama tetap terbuka: R4, R30, P3, S57, duplikasi modal password & OTP (→R83 asersi drift), S69-parsial.

---

### T23 — Regresi T22: dialog konfirmasi voucher menampilkan TAG HTML mentah
- **Prioritas:** 🔴 Tinggi · **Usaha:** XS · **Area:** Admin (Settings/Vouchers) · **Status:** `[x]` ✅ **Batch 13** *(terverifikasi manual)*
- **Lokasi:** `admin-core.js:512` (`${escapeHtml(message)}`) vs `settings-vouchers.js:329` & `:366` (pesan berisi `<strong style="...">`)
- **Masalah:** showConfirm core SELALU meng-escape pesan; kedua pemanggil voucher Batch 12 mengirim HTML → dialog menampilkan `&lt;strong style=&quot;...&quot;&gt;KODE123&lt;/strong&gt;` apa adanya. Ironisnya aria-label (:500) membuang tag sehingga screen reader mendengar teks bersih sementara pengguna lihat melihat markup kotor. 19 pemanggil lain pakai plain text.
- **Rekomendasi:** Kirim plain-text (`'Apakah Anda yakin ingin menghapus kode voucher ' + code + '?'`) — escape ditangani core.

### S70 — Teks "Hapus" voucher & badge "Nonaktif" di atas tint merah = 3.71:1
- **Usaha:** XS · **Area:** Admin · **Status:** `[x]` ✅ **Batch 13**
- **Lokasi:** `settings-vouchers.js:107` (label fungsional 12px), `:68` (badge 11px) — `color:#ef4444` atas background rgba(239,68,68,0.15).
- **Rekomendasi:** Kedua lokasi → var(--color-danger-light); asersi vouchers bebas `color:#ef4444`.

### S71 — Cap `!important` basi (slack 55) + baseline rgba settings terduplikasi antar-suite
- **Usaha:** XS · **Area:** Tooling · **Status:** `[x]` ✅ **Batch 13**
- **Lokasi:** batch11-settings-guard CAPS: admin-base 55 (aktual 47), hasil 66 (65), public-desktop 34 (21), public-mobile 81 (**48 — slack 33**); batch7-tokens masih punya `'admin/settings.html': 110` padahal guard lain ≤28.
- **Rekomendasi:** Kunci ulang CAPS ke aktual; hapus entri ganda (satu-baseline-satu-metrik).

### S72 — `v.notes` dan `v.package` dirender tanpa escapeHtml di tabel voucher
- **Usaha:** XS · **Area:** Admin · **Status:** `[x]` ✅ **Batch 13**
- **Lokasi:** `settings-vouchers.js:92` (`${v.package}`) & `:101` (`${v.notes || '—'}`) — padahal `code` di file sama di-escape dengan disiplin (komentar S3).
- **Rekomendasi:** escapeHtml keduanya; masukkan ke sweep render vouchers.

### S73 — Seluruh timestamp voucher memakai jam perangkat + format ad-hoc
- **Usaha:** XS–S · **Area:** Admin · **Status:** `[x]` ✅ **Batch 13 (PARSIAL)** — tampilan disatukan via formatDateTimeID ✅; perbandingan expired dari waktu server DITUNDA (butuh API waktu server — komentar penunjuk dipasang di kode).
- **Lokasi:** `settings-vouchers.js:63` (perbandingan expired vs jam klien), `:75` (`toLocaleDateString('id-ID')` zona penonton), `:407` (`toLocaleString('id-ID')` format `"24/8/2026 10.11"` ≠ kanonik).
- **Dampak:** Badge "Kadaluarsa" salah muncul/hilang ±1 hari di PC jam meleset — pelanggaran putusan WIB satu-pintu (pola R57/S69).
- **Rekomendasi:** Minimal seragamkan tampilan via formatDateTimeID; perbandingan expired dari waktu server menyusul (butuh API).

### S74 — Pencarian 0 hasil meninggalkan paginasi & statistik basi tampil
- **Usaha:** XS · **Area:** Publik (Hasil) · **Status:** `[x]` ✅ **Batch 13**
- **Lokasi:** `hasil.html:459-466` — cabang total==0 hanya menampilkan empty-state lalu return; paginasi "1–20 dari 57" & statsRow tetap tampil bertentangan dengan kartu "Tidak ditemukan".
- **Rekomendasi:** Sembunyikan paginationWrapper di cabang tersebut (+ redup statsRow).

### S75 — Ekor R68: BAR strength meter masih hex — warna bar ≠ warna label
- **Usaha:** XS · **Area:** Publik · **Status:** `[x]` ✅ **Batch 13**
- **Lokasi:** `register.html:64-65`, `reset_password.html:75-76` (`.pw-bar-fill.good{#22c55e}`, `.strong{gradient #22c55e,#06b6d4}`) vs label yang sudah token (#10b981/#22d3ee).
- **Rekomendasi:** Migrasi rule CSS ke token sama dengan label; perluas asersi bebas-hex ke blok style.

### R77 — Dead code ekor T22: listener modal arwah + identifier tak terdeklarasi
- **Usaha:** XS · **Area:** Admin · **Status:** `[x]` ✅ **Batch 13**
- **Lokasi:** `settings-vouchers.js:313-321` — blok `btnConfirmActionSubmit`/`pendingConfirmCallback` (elemen sudah dihapus; identifier akan ReferenceError bila dibangunkan). Guard Batch 12 hanya melarang string di settings.html.
- **Rekomendasi:** Hapus blok; perluas asersi ke regex `confirmActionModal|pendingConfirmCallback|btnConfirmActionSubmit` pada settings-vouchers.js.

### R78 — Padding fix R76 MATI: inline style render-JS menimpa stylesheet
- **Usaha:** XS · **Area:** Admin (Pengawasan) · **Status:** `[x]` ✅ **Batch 13**
- **Lokasi:** `pengawas_detail.html:1055` (fix 4px 6px) vs inline `padding:6px 12px` (:1361,:1364) dan `5px 10px` (:1578) pada ketiga `.pd-action-btn` render-JS — klaim "hemat via padding" tak pernah berlaku (font-size tetap efektif).
- **Rekomendasi:** Hapus padding inline dari string render atau class modifier; asersi render antrean bebas padding inline.

### R79 — Fix R71 hanya mencakup 4 halaman; 4 halaman publik lain masih link-setelah-style
- **Usaha:** XS–S · **Area:** Publik · **Status:** `[x]` ✅ **Batch 13**
- **Lokasi:** forgot_password.html (:14 vs :18-19), register_confirm.html (:11 vs :170-171), reset_password.html (:14 vs :85-86), shared.html (:117/:918 vs :930-932).
- **Rekomendasi:** Pindahkan link; ubah asersi R71 menjadi folder-wide.

### R80 — Ekor heading order: lompatan h1→h3 panel Kunci Jawaban hasil
- **Usaha:** XS · **Area:** Publik (Hasil) · **Status:** `[x]` ✅ **Batch 13**
- **Lokasi:** `hasil.html:151` (h1 examTitle) → `:259` (h3 "Kunci Jawaban Resmi") — satu-satunya lompatan tersisa repo-wide; assertion T20 belum mencakup hasil.
- **Rekomendasi:** h2 visual via class; asersi urutan heading untuk hasil.html.

### R81 — Ikon chevron afordansi baris `#4f46e5` = 2.60–2.91:1 (< ambang non-teks 3:1)
- **Usaha:** XS · **Area:** Admin (Pengawasan) · **Status:** `[x]` ✅ **Batch 13**
- **Lokasi:** `pengawas.html:327` — satu-satunya penanda visual baris clickable. Lokasi #4f46e5 lain aman (endpoint gradien background).
- **Rekomendasi:** var(--color-primary-light); guard `#4f46e5`-sebagai-warna-ikon/teks.

### R82 — Kelompok ad-hoc terbesar tersisa: `#818cf8` ×11 tanpa padanan token
- **Usaha:** XS · **Area:** Lintas · **Status:** `[x]` ✅ **Batch 13**
- **Lokasi:** download ×3, shared ×2, register_confirm ×2, register ×1, pengawas_detail ×1 (chip MAC), settings ×1 (gradien). Kluster berikutnya: #6ee7b7 ×10, #60a5fa ×9.
- **Rekomendasi:** Definisikan `--color-primary-bright: #818cf8` (pola danger-bright) + migrasi ×11; cap hex folder turun 150→±139.

### R83 — Duplikasi blok OTP register_confirm ↔ reset_password (saudara kasus modal password)
- **Usaha:** S · **Area:** Publik · **Status:** `[x]` ✅ **Batch 13**
- **Fakta:** markup/CSS/JS OTP identik karakter-per-karakter di dua file; paritas dirawat manual sejak R62/R64.
- **Rekomendasi:** Ekstraksi partial saat reformasi harness (gabung S57); sementara: asersi hash-normalized "kedua blok OTP identik" agar drift terdeteksi.

### R84 — Skip-link login admin masih inline-style arwah + z-index literal
- **Usaha:** XS · **Area:** Admin (Login) · **Status:** `[x]` ✅ **Batch 13**
- **Lokasi:** `admin/login.html:45` — pola persis yang dihapus R52 dari nav.html, lengkap fallback hex `#6366f1`.
- **Rekomendasi:** Class `.skip-link` theme.css saja.

### R85 — Username login admin kapital-otomatis di keyboard mobile (ekor R36)
- **Usaha:** XS · **Area:** Admin (Login) · **Status:** `[x]` ✅ **Batch 13**
- **Lokasi:** `login.html:75` — tanpa triplet autocapitalize/autocorrect/spellcheck (pola wajib R36 sudah dipasang di form publik).
- **Rekomendasi:** Tambahkan triplet + asersi test.

### R86 — Feedback kirim-ulang OTP tanpa live region
- **Usaha:** XS · **Area:** Publik · **Status:** `[x]` ✅ **Batch 13**
- **Lokasi:** `register_confirm.html:258-260` (`#resendMsg` statis) vs penulisan dinamis :399-406.
- **Rekomendasi:** role="status" pada resendMsg.

### R87 — Tiga label berbeda untuk satu tujuan /login di area publik
- **Usaha:** XS · **Area:** Publik · **Status:** `[x]` ✅ **Batch 13**
- **Lokasi:** shared.html:35 ("Masuk"), index.html:19 ("Panel Admin"), index.html:130 ("Masuk ke Panel Admin").
- **Rekomendasi:** Kanonik: "Masuk" di nav, "Masuk ke Panel Admin" hanya CTA landing; dokumentasikan di kamus istilah S4.

---

## 5.11 RE-REVIEW RONDE 8 — Temuan baru pasca Batch 13

> **Tanggal:** 24 Agustus 2026 · **Basis kode:** `b37f715` (pasca Batch 13) · **Metode:** 3 reviewer paralel (area settings/admin-core, area pengawasan+nav+lintas-guard, area publik) + verifikasi manual silang temuan kunci ke kode oleh koordinator.
> Fokus khusus ronde ini: verifikasi eksekusi Batch 13, audit integritas guard test (celah regex/whitelist/plafon), keyboard & fokus management, dan sisa kontrak token yang belum ditegakkan folder-wide. Penomoran ID melanjutkan ronde sebelumnya.

### Status verifikasi cepat

| Item | Vonis | Bukti kunci |
|---|---|---|
| T23 dialog voucher plain text | ✅ BERES | `settings-vouchers.js:326-331` & `:364-368` plain text; guard hijau |
| S70 danger-light vouchers | ✅ BERES | `var(--color-danger-light)` di :72/:113; `#ef4444` = 0 di vouchers |
| S72 escape v.package/v.notes | ✅ BERES | `escapeHtml` di :98/:107 |
| S73 formatDateTimeID voucher | ✅ BERES (parsial terdokumentasi) | :81/:408 + komentar penunjuk API waktu server :63-66 |
| R77 dead code confirm arwah | ✅ BERES | regex `confirmActionModal\|pendingConfirmCallback\|btnConfirmActionSubmit` = 0 repo |
| **R82 primary-bright** | ⚠️ **PARSIAL → S81** | klaim "habis" tidak akurat: sisa `#818cf8` di settings.html:1972, pengawas_detail.html:1349, admin.js:1081; guard hanya cek gradien pertama |
| R78 padding inline pd-action-btn | ✅ BERES | ketiga string render bebas `padding:`; stylesheet yang memegang |
| R81 chevron token | ✅ BERES | `var(--color-primary-light)`; `#4f46e5` = 0 di pengawas.html |
| S71 cap !important aktual | ✅ BERES | admin-base 47 · hasil 65 · public-desktop 21 · public-mobile 48 — identik dengan CAPS test |
| Guard folder-wide handler inline | ✅ templates/** tetap 0 `\son[a-z]+=`; cache-busting utuh |
| S36 visibility-guard polling | ✅ start selalu stop dulu; toggle visibility cepat tanpa handle dobel; registry Actions bersih dua arah |

**Audit terukur:** nol pelanggaran plafon aktual — templates/ hex **129**/132 · rgba **140**/145 · admin.js hex 5/8 rgba 36/36 · dashboard hex 24/28 · pengawas_detail hex 13 · nav 7/9. Lubang justru di *cakupan* guard, bukan angkanya: theme.css tak punya plafon `!important` (R90), settings-system-apps.js tak punya baseline JS (S88), register_confirm/reset_password/hasil.html tak masuk daftar FILES batch8-publik (S82/S83), dan kontrak `#f87171`/`#818cf8` ditegakkan per-daftar-file agen, bukan folder-wide (S80/S81).

Item lama tetap terbuka: R4, R30, P3, S57, S69/S73-parsial (butuh API waktu server), duplikasi modal password & OTP.

---

### T24 — Anchor `role="button"` tanpa href MATI untuk keyboard; core mengecualikan tag A dari aktivasi
- **Prioritas:** 🔴 Tinggi · **Usaha:** XS · **Area:** Pengawasan (a11y) · **Status:** `[ ]`
- **Lokasi:** `pengawas_detail.html:1577` (string render-JS) + `admin-core.js:874-875`
- **Bukti:**
  ```js
  '<a class="action-link" role="button" tabindex="0" data-action="show-access-log">Detail</a>' // tanpa href
  // admin-core.js keydown delegasi:
  if (tag === 'BUTTON' || tag === 'A' ...) return; // "Native <button>/<a> already handle this"
  ```
- **Masalah:** Asumsi core hanya benar untuk `<a href>`; anchor ini tak punya perilaku native, dan handler global Enter/Space melewatinya karena tagnya `A`. Pengguna keyboard bisa FOKUS ke link "Detail" (riwayat akses perangkat) tapi tidak bisa mengaktifkannya sama sekali.
- **Dampak:** Pelanggaran WCAG 2.1.1 pada kontrol inti halaman pengawasan.
- **Rekomendasi:** Ubah ke `<button type="button" class="action-link">`, ATAU perbaiki core: `if (tag === 'A' && el.hasAttribute('href')) return;`.

### T25 — Fokus keyboard tak terlihat pada baris hasil (`tr tabindex=0` tanpa rule focus)
- **Prioritas:** 🔴 Tinggi · **Usaha:** XS · **Area:** Publik (Hasil) · **Status:** `[ ]`
- **Lokasi:** `hasil.html:614-615` (`tr.setAttribute('tabindex','0'); tr.setAttribute('role','button')`) · CSS: hasil.css hanya punya `:hover` (:311), tidak ada rule focus untuk `.results-table tbody tr`
- **Masalah:** Browser umumnya tidak menggambar outline fokus untuk `<tr>`; baris nilai siswa adalah kontrol interaktif utama halaman hasil publik. Pengguna keyboard tidak bisa melihat baris mana yang difokuskan — WCAG 2.4.7.
- **Rekomendasi:** `.results-table tbody tr[role="button"]:focus-visible { outline: 2px solid var(--color-primary-light); outline-offset: -2px; }` di hasil.css.

### S76 — showConfirm rentan cross-wiring saat dialog bertumpuk + label tombol tak di-escape
- **Prioritas:** 🟠 Sedang · **Usaha:** XS · **Area:** Admin core · **Status:** `[ ]`
- **Lokasi:** `admin-core.js:551-552` (& :516-517)
- **Bukti:** `document.getElementById('confirmOkBtn').addEventListener(...)` — ID statis pada overlay yang bisa ditumpuk; `${confirmLabel}`/`${cancelLabel}` disisipkan mentah ke innerHTML padahal `message` sudah di-escape.
- **Masalah:** Dua showConfirm bertumpuk (mis. Enter ganda <50 ms sebelum `firstFocus.focus()` di :527) menghasilkan ID kembar — kedua promise ter-wire ke tombol dialog PERTAMA; satu klik OK me-resolve keduanya `true` → aksi destruktif (hapus voucher/app) bisa terkirim ganda. Overlay kedua juga tak ber-id sehingga tak bisa di-force-close manager (:994-997).
- **Rekomendasi:** `overlay.querySelector('#confirmOkBtn')` (ref lokal), dan `escapeHtml(confirmLabel/cancelLabel)` agar kontrak escape utuh.

### S77 — Pesan error server disisipkan mentah ke innerHTML di renderError vouchers & audit
- **Prioritas:** 🟠 Sedang · **Usaha:** XS · **Area:** Admin · **Status:** `[ ]`
- **Lokasi:** `settings-vouchers.js:17-18`; `settings-voucher-audit.js:8-9`
- **Bukti:** `` tbody.innerHTML = `<tr><td ...>${msg}...` `` — padahal `viewRedemptions` (:397) meng-escape `res.message` di file yang sama.
- **Masalah:** `msg` berasal dari `res.message` API; konvensi anti-XSS yang baru dipakukan S72 Batch 13 bocor di jalur error. Bukan eksploit aktif (server-controlled) tapi celah konsistensi.
- **Rekomendasi:** `escapeHtml(msg)` di kedua renderError + asersi guard blok render*Error memakai escapeHtml.

### S78 — Race respons basi pada loadVouchers/loadAuditLogs/loadUsersList/loadMyPackages
- **Prioritas:** 🟠 Sedang · **Usaha:** S · **Area:** Admin · **Status:** `[ ]`
- **Lokasi:** `settings-vouchers.js:21-46`, `settings-voucher-audit.js:12-36`, `admin.js:1595-1770`
- **Masalah:** Fetch daftar tanpa sequence-token — respons halaman lama yang lambat bisa mendarat terakhir dan menimpa tabel+paginasi dengan data yang tak sesuai state UI (paginasi menyala salah halaman). Pola penangkal sudah ada internal: `statsRefreshInFlight` (admin-core.js:745).
- **Rekomendasi:** Token permintaan monoton per daftar (`const seq = ++mySeq; … if (seq !== mySeq) return;`) sebelum render.

### S79 — Tiga switch inti Pengaturan Umum tanpa nama aksesibel
- **Prioritas:** 🟠 Sedang · **Usaha:** XS · **Area:** Admin (Settings/a11y) · **Status:** `[ ]`
- **Lokasi:** `settings.html:1604-1608` (Verifikasi OTP Email), `:1669-1673` (Turnstile), `:1862-1868` (Izinkan Google Index)
- **Bukti:** `<span class="switch-label">Verifikasi OTP Email</span>` adalah saudara checkbox, bukan ter-asosiasi (tanpa for/id/aria-labelledby). Pembaca layar mendapat "checkbox, dicentang" anonim.
- **Rekomendasi:** Ikuti pola kartu Monetisasi yang sudah benar (:1889, input di dalam label ber-teks) atau tambah `aria-labelledby`.

### S80 — Kontrak `#f87171 → var(--color-danger-bright)` bocor: 5 literal tersisa di dashboard & settings
- **Prioritas:** 🟠 Sedang · **Usaha:** S · **Area:** Lintas/guard-test · **Status:** `[ ]`
- **Lokasi:** `dashboard.html:62, :326, :538` (.pd-action-danger, badge langganan, tombol Hapus Ujian); `settings.html:437, :644` — guard `uiux-batch12-pengawasan-nav.test.mjs` hanya menguji keenam template milik agen batch itu.
- **Masalah:** Migrasi token Batch 12 ditegakkan per-daftar-file, bukan folder-wide — literal tinggal di dua halaman inti yang paling sering dipakai.
- **Rekomendasi:** Naikkan kontrak jadi folder-wide (pola KONTRAK T21: readdirSync recursive templates/** bebas `#f87171`) + migrasi 5 lokasi.

### S81 — Klaim R82 PARSIAL: sisa `#818cf8` ×3 + guard first-match-only memberi rasa aman palsu
- **Prioritas:** 🟠 Sedang · **Usaha:** XS–S · **Area:** Lintas/guard-test · **Status:** `[ ]`
- **Lokasi:** `pengawas_detail.html:1349` (chip MAC antrean), `settings.html:1972` (ikon svg header Aplikasi Sistem), `admin.js:1081` (`accent-color:#818cf8`) · guard `uiux-batch13-settings-guard.test.mjs` memakai `SETTINGS.match(/linear-gradient…/)` — HANYA match pertama.
- **Masalah:** Assertion R82 hanya menyentuh gradien pertama settings.html; pemakaian warna identik di tiga lokasi lain lolos tanpa test merah. Verifikasi klaim "[x] R82 habis ×11" ternyata tidak tuntas.
- **Rekomendasi:** Ganti ketiganya dengan `var(--color-primary-bright)`; ganti assertion dengan hitungan global `(SETTINGS.match(/#818cf8/gi)||[]).length === 0` + sweep folder-wide (gabung guard S80).

### S82 — Blind-spot guard token: register_confirm & reset_password penuh hex/rgba literal
- **Prioritas:** 🟠 Sedang · **Usaha:** S · **Area:** Publik (auth) · **Status:** `[ ]`
- **Lokasi:** `register_confirm.html` = **7 hex** (#a5b4fc×4, #4ade80, #fff, #34d399) + **19 rgba literal**; `reset_password.html` = 2 hex + **8 rgba literal** — sementara register.html sudah 0/0.
- **Masalah:** Guard batch8-publik (FILES + BASELINE) hanya mencakup download/shared/register; dua halaman OTP tidak masuk daftar sehingga aturan token tak pernah ditegakkan di sana. Drift sudah nyata.
- **Rekomendasi:** Masukkan kedua file ke FILES+baseline B8, lalu migrasi: #a5b4fc→primary-light, #4ade80/#34d399→success-light, rgba(99,102,241,α)→rgba(var(--rgb-info),α), dst.

### S83 — hasil.html: literal intra-blok tidak konsisten (satu baris token, tetangganya literal)
- **Prioritas:** 🟠 Sedang · **Usaha:** S · **Area:** Publik (Hasil) · **Status:** `[ ]`
- **Lokasi:** `hasil.html:42-48` (chip CSS), `:117-119` (state disabled), `:137` (state error)
- **Bukti:** :46 memakai `rgba(var(--rgb-warning),0.12)` tapi tetangganya masih `rgba(16,185,129,.12)`, `rgba(239,68,68,.12)`×2, `rgba(148,163,184,…)`×2; state disabled memakai `color:#ef4444` (:118) & `#f8fafc` (:119,:137) sementara state error sebelah sudah `var(--color-danger-bright)` (:136). `#ef4444` kontradiktif dengan S70 Batch 13.
- **Rekomendasi:** Migrasi rgba triplet → `rgba(var(--rgb-*))`; #ef4444→danger-bright; #f8fafc→--color-text; masukkan hasil.html ke guard B8.

### S84 — Ukuran APK/file dirender via inline `document.write()` di dua jalur (publik + admin)
- **Prioritas:** 🟠 Sedang · **Usaha:** XS–S · **Area:** Download + Settings · **Status:** `[ ]`
- **Lokasi:** `download.html:538, :556, :698, :777`; `settings.html:2140`
- **Bukti:** `<script>document.write(({{ .SizeBytes }}/1048576.0).toFixed(2));</script> MB`
- **Masalah:** document.write deprecated & blocking; dengan JS mati pengguna membaca "Ukuran: MB" telanjang (noscript hanya memperbaiki .reveal); jalur render-JS system-apps sudah benar via textContent — dua pola paralel untuk hal yang sama.
- **Rekomendasi:** Hitung MB server-side (helper Go `formatSizeMB` / printf "%.2f") dan cetak langsung via template.

### S85 — aria-live stempel "Diperbarui" diumumkan tiap tick polling (noise screen reader)
- **Prioritas:** 🟠 Sedang · **Usaha:** XS · **Area:** Pengawasan (a11y) · **Status:** `[ ]`
- **Lokasi:** `pengawas_detail.html:266` (`lastUpdatedLabel` aria-live="polite") + `markUpdated()` dipanggil tiap poll sukses (5 dtk antrean / 12 dtk peserta)
- **Masalah:** Screen reader mengumumkan "Diperbarui 10:31:07" berulang tanpa henti — jam menenggelamkan pengumuman penting (perubahan antrean izin). Ironi S69: live region yang dimaksud membantu justru jadi noise.
- **Rekomendasi:** Lepas aria-live dari lastUpdatedLabel (visual-only/aria-hidden); sediakan region live tersembunyi terpisah yang hanya diumumkan saat jumlah antrean BERUBAH ("2 permintaan izin menunggu").

### S86 — Byte NUL (0x00) tertanam di source `pengawas-detail.js`
- **Prioritas:** 🟠 Sedang · **Usaha:** XS · **Area:** Pengawasan/tooling · **Status:** `[ ]`
- **Lokasi:** `pengawas-detail.js:26` — `].join('<NUL>');` byte 00 literal dalam string (ter-commit sejak Batch 3; `file` mengklasifikasi file sebagai "data", bukan teks; grep -c menghitung 148 baris "berisi" NUL)
- **Masalah:** Aset JS mengandung raw NUL → terdeteksi biner; alat (grep/diff/linter/proxy/AV tertentu) dapat menolak atau merusak; charset serving rapuh. Jelas salah ketik pemisah fingerprint.
- **Rekomendasi:** Ganti dengan `' '` atau `'|'`; guard di suite T8 bahwa source bebas `\x00`.

### S87 — Label mobile `data-label` tidak sinkron dengan `th` desktop (+ selector CSS bergantung padanya)
- **Prioritas:** 🟠 Sedang · **Usaha:** S · **Area:** Submissions/Dashboard (mobile) · **Status:** `[ ]`
- **Lokasi:** `submissions.html:268-272` vs `:279/:282` (`th "Nama"` ↔ `data-label="Siswa"`; `th "ID Perangkat"` ↔ `"Perangkat"`); CSS bahkan menggantung pada label tak sinkron (`submissions.html:56` `td[data-label="Siswa"]`). Pola sama `dashboard.html:439` vs `:454`.
- **Masalah:** Kartu mobile berlabel "SISWA"/"PERANGKAT" tak cocok dengan kolom desktop "Nama"/"ID Perangkat"; siapa pun yang menjaga selector mudah merusak layout mobile tanpa sadar.
- **Rekomendasi:** Samakan string th↔data-label + guard statik per tabel (assert set label identik).

### S88 — Guard batch9-tokens-guard berlubang: settings-system-apps.js punya rgba literal TANPA baseline
- **Prioritas:** 🟠 Sedang · **Usaha:** XS · **Area:** Guard-test · **Status:** `[ ]`
- **Lokasi:** `uiux-batch9-tokens-guard.test.mjs:53-58` vs `settings-system-apps.js:51` (rgba=2 aktual, nol entrinya)
- **Masalah:** Kontrak "file JS statis tidak boleh naik" berlubang persis di modul settings yang sedang aktif dikembangkan; users/general/packages/admin-core juga tanpa entri (kini 0/0).
- **Rekomendasi:** Tambah `'settings-system-apps.js': { rgba: 2 }` + entri 0 untuk empat file lain agar setiap penambahan warna hardcoded memerah test.

### R88 — Literal z-index sisa vs sistem token `--z-*`
- **Prioritas:** 🟡 Rendah · **Usaha:** XS · **Area:** Dashboard/jscore · **Status:** `[ ]`
- **Lokasi:** `dashboard.html:19` (`.pengawas-popup{z-index:100}`), `:741` (10002 = duplikat manual --z-toast), `admin.js:1053` (dropdown render z-index:100) — padahal `theme.css:123-126` punya `--z-dropdown/--z-onboarding/--z-toast`.
- **Rekomendasi:** Ganti ke var(--z-*); guard "nol `z-index:\s*\d` literal di templates/ + render-JS admin.js".

### R89 — Inline-style arwah pada elemen `.sr-only`: pola R84 belum diperluas
- **Prioritas:** 🟡 Rendah · **Usaha:** XS · **Area:** Lintas-halaman · **Status:** `[ ]`
- **Lokasi:** `pengawas.html:16` (clip-pattern), `submissions.html:134` (`left:-9999px`), `dashboard.html:244`, `settings.html:777/782/1584`, `submissions.html:143`
- **Masalah:** Dua gaya sr-only berbeda untuk tujuan sama; utility `.sr-only` Tailwind output.css:61 sudah ekuivalen — class-nya sendiri tidak dipercaya.
- **Rekomendasi:** Hapus semua inline style pada elemen ber-class sr-only; guard: elemen `[class*=sr-only]` bebas atribut style.

### R90 — Cap `!important` S64/S71 tidak mencakup theme.css
- **Prioritas:** 🟡 Rendah · **Usaha:** XS · **Area:** Guard-test/css · **Status:** `[ ]`
- **Lokasi:** `uiux-batch11-settings-guard.test.mjs:161-166` — CAPS hanya admin-base/hasil/public-desktop/public-mobile; `theme.css` punya 1 `!important` (:158 skip-link) tanpa plafon.
- **Masalah:** Satu-satunya file CSS inti di luar radar — `!important` baru bisa masuk tanpa alarm, persis lubang yang S71 kritik.
- **Rekomendasi:** Tambah `'css/theme.css': 1` ke CAPS.

### R91 — Interpolasi mentah `statusText` & display-time di panel evaluasi hasil
- **Prioritas:** 🟡 Rendah · **Usaha:** XS · **Area:** Publik (Hasil) · **Status:** `[ ]`
- **Lokasi:** `hasil.html:749` (`${statusText}`), `:803/:807` (`${startTimeStr}`/`${endTimeStr}`) — sementara q.number/nama/jawaban semuanya lewat escapeHtml.
- **Masalah:** Server-controlled (bukan XSS aktif) tapi melanggar prinsip "defense-in-depth" file itu sendiri (:1006); satu-satunya jalur data eksternal yang bypass pola escape.
- **Rekomendasi:** Bungkus escapeHtml(String(...)) agar kontrak seragam 100%.

### R92 — Dead attribute `data-name` sisa filter klien lama (menempel PII ke DOM)
- **Prioritas:** 🟡 Rendah · **Usaha:** XS · **Area:** Publik (Hasil) · **Status:** `[ ]`
- **Lokasi:** `hasil.html:610` — `tr.setAttribute('data-name', sub.student_name.toLowerCase())`; tidak ada pembaca dataset.name lagi (pencarian server-side sejak Batch 10).
- **Rekomendasi:** Hapus baris.

### R93 — Paritas pre-submit OTP: reset_password tanpa gating digit (beda dari register_confirm)
- **Prioritas:** 🟡 Rendah · **Usaha:** XS · **Area:** Publik (Reset password) · **Status:** `[ ]`
- **Lokasi:** `reset_password.html:169` (submit tanpa disabled) vs `register_confirm.html:252` (verifyBtn disabled + syncHidden gating)
- **Masalah:** Submit dengan OTP kosong lolos ke POST → round-trip server + potensi kehilangan isi form, sementara jalur register mencegahnya di klien.
- **Rekomendasi:** Salin gating `submit.disabled = code.length < 6` atau minimal preventDefault + fokus kotak pertama.

### R94 — Copy error Turnstile merujuk posisi layar ("di atas") di 3 template auth
- **Prioritas:** 🟡 Rendah · **Usaha:** XS · **Area:** Publik · **Status:** `[ ]`
- **Lokasi:** `register.html:377`, `forgot_password.html:89`, `reset_password.html:300` — "verifikasi keamanan di atas".
- **Masalah:** Referensi spasial rapuh (widget bisa di bawah fold dari posisi tombol di layar pendek); screen reader membaca frasa tanpa konteks.
- **Rekomendasi:** "Harap selesaikan verifikasi keamanan (Cloudflare Turnstile) terlebih dahulu." + pindahkan fokus ke widget saat error.

### R95 — Cap hex settings-vouchers.js longgar (aktual 12 vs plafon 22, headroom +83%)
- **Prioritas:** 🟡 Rendah · **Usaha:** XS · **Area:** Guard-test · **Status:** `[ ]`
- **Lokasi:** `uiux-batch9-tokens-guard.test.mjs:54` — inkonsisten dengan file lain yang exact-plafon (audit 2/2, billing 1/1). Hex layak migrasi: `#fca5a5`×3 → danger-light, `#94a3b8`×2, dst.
- **Rekomendasi:** Migrasikan sisa 12 hex ke token lalu turunkan cap ke angka baru (kontrak "dikunci aktual" Batch 13).

### R96 — Touch target <44px lintas area: paginasi/aksi baris settings + search-clear/toast-close publik
- **Prioritas:** 🟡 Rendah · **Usaha:** S · **Area:** Admin + Publik · **Status:** `[ ]`
- **Lokasi:** `settings-vouchers.js:167` & `voucher-audit.js:86` (tombol paginasi ≈24px tinggi, gap 6px), `:103` ("Lihat User" 8px 6px), `settings.html:1315` (toolbar 38px); `hasil.css:994/:997` (search-clear 38px, pagination-btn 42px — komentar sendiri bilang "~44px minimum"), `output.css:1825` (password-toggle 40×40), `download.html:431` (toast-close 24×24).
- **Masalah:** Halaman kerja frekuensi tinggi guru di tablet/HP; mis-tap paginasi rapat mudah mendarat di nomor salah. Lolos AA 24px tapi gagal target nyaman 44px.
- **Rekomendasi:** min-height paginasi ±40-44px + gap lebih besar; toast-close minimal 32px + pseudo-element hit-area; naikkan sisanya ke 44px via padding transparan.

### R97 — Sisa bahasa EN: satu string UI + blok komentar besar
- **Prioritas:** 🟡 Rendah · **Usaha:** XS · **Area:** Lintas · **Status:** `[ ]`
- **Lokasi:** UI: `settings-vouchers.js:277` (`'Generating...'` vs 'Menyimpan...' di :231). Komentar EN: `settings-system-apps.js:62-175`, `settings-billing.js:4-85`, `pengawas_detail.html:1305-1521`, `admin-core.js:369-889`.
- **Rekomendasi:** 'Membuat voucher...'; terjemahkan blok komentar saat file disentuh berikutnya (konvensi komentar ID di core/vouchers sudah konsisten).

### R98 — Dead code & wiring ganda kecil di area settings
- **Prioritas:** 🟡 Rendah · **Usaha:** XS · **Area:** Admin · **Status:** `[ ]`
- **Lokasi & bukti:** `settings-users.js:144` (`window.toggleUsersCollapse` redundan — yang dipakai Actions fungsi lain); `settings-vouchers.js:333-345/:371-383` (blok `{…}` no-op dalam .then); `settings.html:779-780` (kondisi Go `{{ if and … }}` diduplikasi nested identik, ditutup :1053 & :2164); **`settings.html:2099-2108` — `#btnOpenUploadApp` di-wire DUA kali** (addEventListener langsung + delegasi document) sehingga tiap klik menjalankan `openUploadModalSafe()` dua kali.
- **Masalah:** Wiring ganda kontradiktif dengan kontrak "satu handler per aksi" (R28) dan rawan dobel efek bila openUploadModal kelak punya side-effect.
- **Rekomendasi:** Hapus assignment redundan + blok braces + if Go duplikat; sisakan SATU jalur wiring tombol unggah (delegasi saja).

---

## 5.12 RE-REVIEW RONDE 9 — Temuan baru pasca Batch 14 (bahan Batch 15)

> **Tanggal:** 25 Agustus 2026 · **Basis kode:** `f0ab8d7` (pasca Batch 14) · **Metode:** 5 reviewer paralel (admin core/dashboard/submissions · settings · publik auth+download · hasil+token+integritas-guard · pengawasan) + verifikasi silang koordinator atas seluruh temuan Tinggi dan sampel Sedang/Rendah langsung ke kode — termasuk arkeologi git keseimbangan template lintas-commit, eksekusi `go test ./internal/handlers/admin/`, dan kalibrasi ulang independen hitungan kontras WCAG.
> Ditemukan **4 masalah Tinggi (termasuk 1 kerusakan produksi), 17 Sedang, dan 20 Rendah baru**. Tema dominan ronde ini: **(1)** guard JS yang tidak pernah mengeksekusi parser Go maupun `go test` — kerusakan template lolos 3 rilis batch dengan seluruh suite hijau; **(2)** assertion yang mengunci *teks* bukan *perilaku* (marker slice salah, string-literal lock) meneruskan pola first-match-only ronde 8; **(3)** kelas race respons basi (S78) yang hanya dibasmi pada empat loader kontrak — modal & fetch lain luput; **(4)** sisa kontrol render-JS yang mati untuk keyboard di area admin.

### ⚠️ TEMUAN PALING KRITIS RONDE INI

**Template `settings.html` GAGAL DI-PARSE GO sejak Batch 12 (`4fc2ab8`)** — halaman Pengaturan mati total untuk semua role pada build apa pun yang memuat Batch 12+. Lolos Batch 13 & Batch 14 karena seluruh suite guard adalah JS statik yang tidak pernah memanggil parser Go, dan test Go settings tidak dijalankan di gerbang mana pun (detail: [T26](#t26--template-settingshtml-gagal-di-parse-go-sejak-batch-12-halaman-pengaturan-mati-total)). Gerbang pertama Batch 15: perbaiki ini, baru item lain.

### Status verifikasi cepat Batch 14 (per area, spot-check langsung ke kode)

| Item | Vonis | Bukti kunci |
|---|---|---|
| T24/T25 | ✅ | core href-aware + button Detail parity utuh; rule fokus `tr[role="button"]` tepat |
| S76/S77 | ✅ | `overlay.querySelector` + label ter-escape; kedua `renderError` lewat `escapeHtml(msg)` |
| S78 | ✅⚠️ | empat loader ber-token; **namun kelasnya belum habis** → S91/S92/S102 (showSubmissionDetail, delegate modal, viewRedemptions) |
| S79–S83 | ✅ | aria-labelledby ×3 id eksis; folder-wide nol literal `#f87171`/`#818cf8`; FILES B8 diperluas |
| S84 | ✅ | 0 `document.write`; catatan minor: tanpa JS kolom ukuran tampil kosong (jejak keluhan awal belum 100% hilang) |
| S85 | ⚠️ | region live benar untuk tick sukses, tapi tak dipanggil di jalur notice/gagal → R112 |
| S86 | ✅ | NUL→'\|'; byte `\x01` tersisa disengaja (pemisah serialisasi kedua) |
| S87 | ✅ | paritas th↔data-label dua tabel + selector CSS ikut |
| S88 | ⚠️ | entri BASELINES bertambah, tapi masih bolong: `pengawas-detail.js` & `device-fingerprint.js` → S103 |
| R88–R90/R92/R93–R95 | ✅ | token z-index, sr-only, CAPS theme.css, syncOtpHidden, frasa Turnstile, danger-light |
| R91 | ⚠️ | kode benar (`:748/:802/:806`) tapi guard penjaganya VAKUM (marker slice salah) → T29 |
| R96 | ⚠️ | paginasi vouchers/audit tuntas 40px; touch target hasil.css masih 38–42px → R111 |
| R97 | ⚠️ | satu string diganti; tetangganya ('Generate Batch', 'Batch Generate', typo 'Kesini') tinggal → R102 |
| R98 | 🔴 GAGAL | wiring unggah satu jalur & hapus `window.toggleUsersCollapse` bersih ✅ — tapi klaim "if Go redundan dihapus" ternyata: kondisi dalam DIUBAH jadi `and $isSuper $isOp` (T27) dan `{{ end }}` yang hilang justru milik if-luar tambahan Batch 12 (T26); 5 test Go merah di HEAD |

**Suite node gabungan tetap hijau (828+ test), tapi 5 test Go `internal/handlers/admin` MERAH di HEAD** — inilah celah proses inti ronde ini (→ S93).

### Audit terukur plafon guard vs aktual (HEAD `f0ab8d7`, hasil ukur reviewer + verifikasi koordinator)

| Plafon | Dideklarasikan | Aktual | Slack hantu |
|---|---|---|---|
| batch7 `RGBA_BASELINE_PER_FILE` register_confirm | 19 | 0 | 19 |
| batch7 idem reset_password / hasil.html | 8 / 10 | 0 / 0 | 18 |
| batch8-publik `BASELINE_HEX` download/shared/register | 20 / 25 / 12 | 7 / ~11–15 / 0 | ±30 |
| batch9 `BASELINES` JS: `pengawas-detail.js`, `device-fingerprint.js` | (tanpa entri) | 0/0 | tak dijaga sama sekali |

Kontrak ronde 5 "plafon = aktual" tidak lagi dipenuhi ±39 titik — migrasi S80–S83 menurunkan aktual tanpa menurunkan plafon. Detail di S95/S99.

### Item lama tetap terbuka (diingatkan, bukan temuan baru)

R4 · R30 (butuh keputusan UX) · P1/P2/P3 (keputusan produk) · S57 (ekstraksi blok inline besar — makin relevan: `pengawas_detail.html` kini 2.260 baris) · S69-parsial & S73-parsial (butuh API waktu server) · duplikasi modal password & OTP di dashboard (dashboard.html:572–621).

---

### T26 — Template settings.html gagal di-parse Go sejak Batch 12: halaman Pengaturan mati total

- **Prioritas:** 🔴 Tinggi (KRITIS) · **Usaha:** XS · **Area:** Settings · **Status:** [ ]
- **Lokasi:** `webui/templates/admin/settings.html:779` (if tanpa penutup)
- **Bukti:** selisih opener vs `{{ end }}`: `4c87bc8`(B11)=0 → `4fc2ab8`(B12)=+1 → `f0ab8d7`(HEAD)=+1. `$ go test ./internal/handlers/admin/ -run TestSettingsPage` → 5 FAIL: `template: admin/settings.html:2479: unexpected EOF`. Baris 779 `{{ if and (not $locked) (or $isSuper $isOp) }}` ditambah Batch 12 sebagai wrapper luar TANPA `{{ end }}` pasangan (inner :780 ditutup :1053).
- **Masalah:** parser Go menolak seluruh file; runtime menerima error parse saat startup/render (dicatat sebagai warning, bukan fail-fast), sehingga `/admin/settings` tidak ter-render.
- **Dampak:** halaman Pengaturan mati untuk semua role di setiap build pasca-Batch 12; lolos 3 rilis karena suite guard semuanya JS statik.
- **Rekomendasi:** hapus wrapper :779 (kembalikan ke satu if seperti pra-Batch 12) — lihat T27 untuk kondisi yang benar; tambahkan guard keseimbangan `{{if}}/{{end}}` per template (bagian dari S105) dan jadikan `go test ./internal/handlers/admin/` gerbang wajib skrip test repo.

### T27 — Regresi role-gating: kondisi dalam diganti `and $isSuper $isOp` — section Kelola User mustahil tampil

- **Prioritas:** 🔴 Tinggi · **Usaha:** XS · **Area:** Settings · **Status:** [ ]
- **Lokasi:** `webui/templates/admin/settings.html:780`
- **Bukti:** diff `b37f715..f0ab8d7`: `-{{ if and (not $locked) (or $isSuper $isOp) }}` → `+{{ if and (not $locked) $isSuper $isOp }}`. `NormalizeSessionRole` hanya menghasilkan tepat satu role ("superadmin" ATAU "operator"), sehingga `and` dua-duanya tidak pernah true.
- **Masalah:** Batch 14 bermaksud menghapus duplikasi nested, tapi yang terjadi: baris dalam diedit mengganti `(or …)` menjadi argumen datar `and` — blok Kelola User (:781–:1053) tak akan dirender untuk siapa pun.
- **Dampak:** operator (dan superadmin) kehilangan seluruh manajemen user setelah parse diperbaiki; regresi fungsional terselubung di balik error parse T26.
- **Rekomendasi:** pulihkan `{{ if and (not $locked) (or $isSuper $isOp) }}` pada :780 sambil menghapus wrapper :779 (T26); verifikasi dengan `TestSettingsPageSectionsRoleGated`.

### T28 — Divider "Sisipkan Soal" menyisipkan DUA soal per klik pasca-reindex (onclick dobel)

- **Prioritas:** 🔴 Tinggi · **Usaha:** XS · **Area:** Dashboard/editor ujian · **Status:** [ ]
- **Lokasi:** `webui/static/js/admin.js:757–760` vs `:699` + registry `:4199`
- **Bukti:** `reindexQuestions` menempel `btn.setAttribute('onclick', \`insertQuestionAt(${dividerCount})\`)` — padahal tombol yang sama sudah punya `data-action="question-insert-at"` (:699) yang tertangani delegasi Actions (:4199). Kedua jalur aktif → satu klik = dua panggilan `insertQuestionAt`.
- **Masalah:** jejak migrasi ke delegasi (ronde awal) meninggalkan atribut onclick arwah yang dihidupkan ulang oleh reindex; setelah add/remove soal (reindex dipanggil :726/:737/:834), klik divider dobel-insert.
- **Dampak:** soal duplikat masuk draft tanpa sadar — korupsi data konten ujian langsung di UI inti.
- **Rekomendasi:** hapus blok :757–760; delegasi saja. Tambah test vm yang men-trigger klik pada tombol hasil reindex dan menghitung panggilan `insertQuestionAt` == 1.

### T29 — Guard vakum: proteksi escape kartu identitas hasil berjalan di string kosong (marker slice salah)

- **Prioritas:** 🔴 Tinggi (guard palsu) · **Usaha:** XS · **Area:** Integritas guard · **Status:** [ ]
- **Lokasi:** `webui/static/js/uiux-batch14-publik.test.mjs:175–179`
- **Bukti:** `HASIL_HTML.slice(HASIL_HTML.indexOf('detail-identity-items'))` — marker `detail-identity-items` = **0 hit** di `hasil.html` (verifikasi grep koordinator), sehingga `indexOf` = −1, `slice(-1)` = 1 karakter terakhir, `assert.ok(detailBlock.length > 0)` lolos trivially, dan asersi anti-`${startTimeStr}` mengamati string kosong.
- **Masalah:** guard yang diklaim melindungi escape waktu kartu identitas ternyata no-op total — pola lanjutan first-match-only ronde 8, kini bentuk ekstremnya: asersi vakum.
- **Dampak:** regesi escape `startTimeStr/endTimeStr` di blok detail lolos tanpa pernah ketahuan; rasa aman palsu bagi eksekutor batch berikutnya.
- **Rekomendasi:** ganti marker ke anchor eksis (mis. id elemen detail identitas aktual), pertahankan `assert.ok(length > threshold)` minimal, dan tambah asersi positif (pola `${escapeHtml(String(startTimeStr))}` harus ada).

---

### S89 — Kolom Durasi submissions mentok "—": data-end tak dinormalisasi, `CreatedAt` default Go ditolak `Date`

- **Prioritas:** 🟠 Sedang · **Usaha:** XS · **Area:** Submissions · **Status:** [ ]
- **Lokasi:** `webui/templates/admin/submissions.html:431–436` (+ sumber data `:312`)
- **Bukti:** start dinormalisasi lengkap (:431–433: spasi→T, tambah Z), tapi end mentah: `var endDt = new Date(endStr);` dengan `data-end="{{.CreatedAt}}"` — format default `time.Time` Go memuat nama zona (mis. `WIB`/`UTC` suffix non-RFC3339) yang ditolak parser V8 → NaN → fungsi balik `return` tanpa isi durasi.
- **Masalah:** normalisasi hanya dilakukan untuk start; kolom Durasi permanen "—" padahal datanya ada.
- **Dampak:** guru tak bisa melihat lamanya pengerjaan (sinyal kecurangan/perilaku hilang) tanpa pesan apa pun.
- **Rekomendasi:** samakan normalisasi untuk end, atau lebih baik: server kirim epoch-ms/data-duration siap pakai; test vm dengan string zona `WIB`.

### S90 — Dropdown multi-select pengawas mati total untuk keyboard (header, chip-remove, searchBox onclick-only)

- **Prioritas:** 🟠 Sedang · **Usaha:** M · **Area:** Dashboard/delegasi · **Status:** [ ]
- **Lokasi:** `webui/static/js/admin.js:1019` (header.onclick), `:1041` (chip × .onclick), `:1067` (searchBox)
- **Bukti:** seluruh kontrol dropdown dibangun sebagai `<div>` dengan handler `.onclick` inline — tanpa `tabindex`, `role`, atau keydown; chip styling juga memuat literal `rgba(168,85,247,0.15)`/`#c084fc` (:1036) yang bypass token.
- **Masalah:** alur delegasi pengawasan — fitur inti admin — tidak dapat dioperasikan tanpa mouse (WCAG 2.1.1).
- **Dampak:** pengguna keyboard/screen reader tak bisa menugaskan pengawas via UI ini sama sekali.
- **Rekomendasi:** header jadi `<button type="button" aria-expanded aria-haspopup="listbox">`, chip remove jadi `<button aria-label="Hapus {nama}">`, searchBox `<input role="combobox">`; migrasi warna chip ke token.

### S91 — showSubmissionDetail tanpa sequence-token (kelas race S78 luput)

- **Prioritas:** 🟠 Sedang · **Usaha:** S · **Area:** Submissions/modal · **Status:** [ ]
- **Lokasi:** `webui/static/js/admin.js:3893–3965` (fetch :3903)
- **Bukti:** fetch detail submission tanpa variabel generasi/guard `then` — respons lambat yang datang belakangan menimpa isi modal dari permintaan terakhir.
- **Masalah:** pola race respons basi yang sama dengan S78, tapi di jalur modal detail yang tak termasuk kontrak Batch 14.
- **Dampak:** modal menampilkan detail submission orang lain (salah konteks) saat klik cepat bergantian.
- **Rekomendasi:** token `submissionDetailSeq` + guard `if (seq !== …) return;` di then/catch — salin pola S78.

### S92 — viewRedemptions tanpa sequence-token (loader vouchers sendiri sudah aman, redemptions luput)

- **Prioritas:** 🟠 Sedang · **Usaha:** S · **Area:** Settings/vouchers · **Status:** [ ]
- **Lokasi:** `webui/static/js/settings-vouchers.js:393–426`
- **Bukti:** `voucherLoadSeq` ada untuk daftar voucher (:25/:38/:48), tapi `viewRedemptions` melakukan fetch tabel redemp tanpa token generasi apa pun.
- **Masalah:** kelas race S78 hanya dibasmi pada empat loader kontrak; loader keenam di modul yang sama luput.
- **Dampak:** tab Riwayat Redemp bisa menampilkan data sesi filter sebelumnya (salah akun/salah filter) pada jaringan LAN lambat.
- **Rekomendasi:** `redemptionSeq` + guard then/catch; sekalian harden `loadApps`/`loadPackages` yang polanya sama.

### S93 — Assertion R98 hanya mengunci teks; tidak ada guard keseimbangan {{if}}/{{end}} maupun go test di gerbang

- **Prioritas:** 🟠 Sedang · **Usaha:** S · **Area:** Integritas guard/proses · **Status:** [ ]
- **Lokasi:** `webui/static/js/uiux-batch14-settings.test.mjs:228–233`
- **Bukti:** test R98 menghitung kemunculan persis string `'{{ if and (not $locked) (or $isSuper $isOp) }}'` — perubahan kondisi menjadi `and $isSuper $isOp` (T27) dan EOF parse (T26) keduanya lolos hijau. Tidak ada test apa pun yang menghitung keseimbangan opener/end per template, dan `go test ./internal/handlers/admin/` tidak dieksekusi skrip test repo.
- **Masalah:** guard mengunci literal bukan perilaku; parser Go — satu-satunya otoritas — tak pernah dijalankan.
- **Dampak:** kerusakan produksi kelas T26 dapat terulang di template lain kapan saja.
- **Rekomendasi:** guard keseimbangan `\{\{-?\s*(if|range|with|block|define)\b` vs `\{\{-?\s*end\b` untuk semua `templates/**/*.html` + wire `go test ./...` (minimal package handlers) ke skrip test; ubah assertion R98 menjadi cek struktur (jumlah if dengan kondisi itu ≤ 1 DAN balance ok).

### S94 — Silent refresh monitoring rebuild tbody submissions tiap 12 detik — fokus & tap hilang

- **Prioritas:** 🟠 Sedang · **Usaha:** M · **Area:** Pengawasan · **Status:** [ ]
- **Lokasi:** `webui/templates/admin/pengawas_detail.html:1585–1606` (innerHTML :1606), trigger polling :2215
- **Bukti:** refresh senyap submissions menulis ulang `tbody.innerHTML = html` tanpa syarat — berbeda dengan tabel antrean izin yang sudah punya mesin diff (`serializeApprovals`/`computeApprovalRowOps`, :1133).
- **Masalah:** baris yang sedang disorot/diklik (Detail, Tolak) digantikan DOM baru tiap tick — fokus keyboard lenyap, tap sedang berlangsung menghilang.
- **Dampak:** pengawas yang hendak menolak perangkat kehilangan kliknya berkala; screen reader membacakan ulang tabel penuh tiap 12 dtk.
- **Rekomendasi:** tiru mesin diff antrean izin (ops per-baris) untuk tabel submissions, atau minimal skip-render bila snapshot serial identik.

### S95 — Baseline per-file token stale pasca-migrasi S80–S83: ±39 slack hantu, kontrak "plafon = aktual" putus

- **Prioritas:** 🟠 Sedang · **Usaha:** S · **Area:** Integritas guard · **Status:** [ ]
- **Lokasi:** `webui/static/js/uiux-batch7-tokens.test.mjs:80–93` + `uiux-batch8-publik.test.mjs` (BASELINE_HEX)
- **Bukti:** `RGBA_BASELINE_PER_FILE`: register_confirm 19→aktual 0, reset_password 8→0, hasil 10→0 (verifikasi koordinator); B8 HEX: download 20→7, shared 25→~11–15, register 12→0. Migrasi Batch 13–14 menurunkan aktual tanpa menurunkan plafon.
- **Masalah:** plafon yang jauh di atas aktual membolehkan ~39 literal baru masuk tanpa alarm — kebalikan tujuan guard.
- **Dampak:** erosi token diam-diam; audit "terukur" jadi tidak berarti.
- **Rekomendasi:** kunci semua baseline ke nilai aktual (assert.equal, bukan ≤), konsisten dengan kontrak R70/koreksi Batch 13.

### S96 — Print stylesheet halaman hasil: judul gradien transparan (-webkit-text-fill-color tak di-reset)

- **Prioritas:** 🟠 Sedang · **Usaha:** XS · **Area:** Hasil/print · **Status:** [ ]
- **Lokasi:** `webui/static/css/hasil.css:102,:156` (fill-color transparent) vs blok print `:975–997`
- **Bukti:** judul & badge memakai gradien `-webkit-text-fill-color: transparent`, blok `@media print` hanya meng-override `color` — properti vendor itu tetap transparent di media cetak (grep: tak ada reset lain).
- **Masalah:** di kertas, teks ber-gradien dirender kosong/transparan.
- **Dampak:** guru yang mencetak rekap hasil mendapat lembar tanpa judul — keluhan klasik "print kosong".
- **Rekomendasi:** di blok print: `-webkit-text-fill-color: initial; background: none;` untuk selector ber-gradien + smoke-test cetak.

### S97 — Drawer nav mobile tetap menerima fokus saat tertutup (WCAG 2.4.3) + fokus tak dikembalikan setelah tutup

- **Prioritas:** 🟠 Sedang · **Usaha:** S · **Area:** Publik/nav · **Status:** [ ]
- **Lokasi:** `webui/static/css/public-mobile.css:74–81` + `templates/public/shared.html:43–63`
- **Bukti:** `.nav-links` ditutup hanya dengan `right:-100%` + transisi — tanpa `visibility:hidden`/`display:none`/inert; `closeMenu()` tidak mengembalikan fokus ke hamburger.
- **Masalah:** Tab dari halaman yang tampak "bersih" melompat ke link drawer di luar layar; urutan fokus tak sesuai visual.
- **Dampak:** disorientasi keyboard/screen reader di SEMUA halaman publik ≤1100px; link terpicu "tak terlihat".
- **Rekomendasi:** tambah `visibility:hidden` saat tertutup (transisi delay) atau `inert`/`aria-hidden` + kembalikan fokus ke tombol hamburger saat menutup.

### S98 — autocomplete="one-time-code" menempel di kotak maxlength="1" — autofill OTP iOS/Android terpotong

- **Prioritas:** 🟠 Sedang · **Usaha:** S · **Area:** Publik/auth · **Status:** [ ]
- **Lokasi:** `templates/public/register_confirm.html:243` + `reset_password.html:132`
- **Bukti:** `<input class="otp-digit" maxlength="1" ... autocomplete="one-time-code">` — OS menyalin kode 6 digit ke input pertama, handler digit memotong ke 1 karakter; distribusi multi-karakter hanya ada di jalur paste.
- **Masalah:** atribut one-time-code hanya sah di SATU input yang menampung kode utuh; pada pola 6 kotak ia aktif membusukkan autofill.
- **Dampak:** pengguna iOS/Android yang mengandalkan autofill SMS/WA mendapat digit tunggal salah — OTP gagal berulang.
- **Rekomendasi:** hapus autocomplete dari kotak digit, letakkan pada hidden input gabungan (pola sinkron R93 sudah ada), atau tangani event input multi-char dengan distribusi antar kotak.

### S99 — Blind-spot guard token: CSS layer publik di luar radar + BASELINES batch9 bolong lagi

- **Prioritas:** 🟠 Sedang · **Usaha:** S · **Area:** Integritas guard · **Status:** [ ]
- **Lokasi:** `static/css/public-desktop.css` (:13,:76–79 literal duplikat --color-bg-secondary), `public-mobile.css:36,:78` (#111827), `uiux-batch9-jscore` BASELINES
- **Bukti:** guard batch7 hanya mengawasi `admin-base.css`; kedua file publik berisi hex/rgba literal bebas; BASELINES batch9 tak punya entri `pengawas-detail.js` & `device-fingerprint.js` (aktual 0/0 — verifikasi koordinator) padahal klaim S88 "semua modul".
- **Masalah:** pola "guard per-daftar-file" (tema ronde 8) berlanjut: area di luar daftar berkembang tanpa pengawasan.
- **Dampak:** drift token di CSS publik & JS pengawasan tak akan pernah terdeteksi.
- **Rekomendasi:** tambahkan public-desktop/mobile.css + kedua JS ke guard dengan baseline aktual (=0), atau naikkan guard ke folder-wide seperti preseden S80/S81.

### S100 — Distribusi aplikasi mengajarkan bypass SmartScreen/Play Protect tanpa checksum SHA-256 tersedia

- **Prioritas:** 🟠 Sedang · **Usaha:** M · **Area:** Download/trust · **Status:** [ ]
- **Lokasi:** `templates/public/download.html:634–636,:735–737` + model `internal/models/system_apps.go:12–21`
- **Bukti:** panduan menginstruksikan "Tetap unduh"/klik-through peringatan OS, tapi skema data aplikasi tidak memiliki field checksum — UI tak bisa menampilkan SHA-256 untuk diverifikasi siswa/IT sekolah.
- **Masalah:** mengajarkan membiasakan mengabaikan peringatan keamanan OS tanpa mekanisme verifikasi alternatif — kontradiksi trust-and-safety.
- **Dampak:** risiko supply-chain (APK ditukar/dicemari di mirror/LAN) tak terdeteksi; praktik keamanan buruk diajarkan ke ribuan siswa.
- **Rekomendasi:** tambah kolom `sha256` (dihitung otomatis saat unggah), tampilkan di kartu unduh + panduan "verifikasi checksum" singkat sebagai pengganti nada bypass.

### S101 — Kontras badge/chip skor borderline di garis AA — migrasi teks ke varian light/bright + guard statik

- **Prioritas:** 🟠 Sedang · **Usaha:** XS · **Area:** Hasil/kontras · **Status:** [ ]
- **Lokasi:** `static/css/hasil.css:337–356` (.score-high/mid/low) + `templates/public/hasil.html:42–47` (chips)
- **Bukti (kalibrasi koordinator):** pasangan terburuk `.score-low` #ef4444 di tint rgba(239,68,68,.15) atas glass gelap ≈ **4.45:1** (di bawah ambang tipis), `.score-status-fail` ≈ 4.59:1 (menyentuh garis); varian terang jelas aman: `--color-danger-bright` ≈ 6.25:1, `success-light` ≈ 10.8:1. Catatan: hitungan awal reviewer (~3.2:1) ternyata terlalu pesimistik terhadap latar efektif — angka koordinator yang dipakai.
- **Masalah:** teks base-token di atas tint warna sendiri duduk tepat di garis AA 4.5:1 — sensitif terhadap backdrop blur; chip 0.66rem ≈ 10.6px bold wajib ambang penuh.
- **Dampak:** keterbacaan skor "Belum Lulus" meragukan di proyektor/LCD redup — justru status paling penting secara pedagogis.
- **Rekomendasi:** migrasikan warna TEKS badge/chip ke varian light/bright (token eksis semua), biarkan tint untuk latar; tambah guard kontras statik untuk pasangan token-teks×tint yang disepakati.

### S102 — openDelegateExamModal fetch tanpa token generasi (modal bersama, kelas race ketiga)

- **Prioritas:** 🟠 Sedang · **Usaha:** XS · **Area:** Dashboard/delegasi · **Status:** [ ]
- **Lokasi:** `webui/static/js/admin.js:2877–2945` (fetch :2891)
- **Bukti:** modal delegasi mengambil data ujian/pengawas tanpa guard generasi; klik cepat dua kartu → isi modal campuran respons basi.
- **Masalah/Dampak:** sama dengan S91 — konteks salah di modal aksi tulis (penugasan), berisiko salah assign.
- **Rekomendasi:** `delegateSeq` + guard then/catch; jadikan pola wajib semua pembuka modal ber-fetch.

### S103 — Token mati di theme.css sementara nilainya diduplikasi manual di template

- **Prioritas:** 🟠 Sedang · **Usaha:** S · **Area:** Design token · **Status:** [ ]
- **Lokasi:** `static/css/theme.css:27,:35,:38` (--color-{success,danger,warning}-bg), `:79–80` (--shadow-card/--shadow-lg); duplikasi: `templates/public/hasil.html:42–48`, `hasil.css` (.tone-info ×5)
- **Bukti:** grep folder-wide: definisi tanpa pemakai (satu-satunya "shadow-lg" adalah utility Tailwind, bukan var) — sementara triplet rgba identik ditulis tangan berulang.
- **Masalah:** arah desain (token latar chip & elevasi) sudah dirumuskan tapi tak diadopsi; nilai manual drift bebas.
- **Dampak:** inkonsistensi visual antarhalaman + beban migrasi menumpuk (keluhan S15/S80 berulang).
- **Rekomendasi:** keputusan tegas adopt-or-delete: migrasikan chips hasil & tone-info ke token bg/shadow, atau hapus token mati dan catat alasannya.

### S104 — Guard R91-vakum bukan kasus tunggal: tak ada test bahwa marker guard benar-benar eksis

- **Prioritas:** 🟠 Sedang · **Usaha:** XS · **Area:** Integritas guard · **Status:** [ ]
- **Lokasi:** pola umum suite `uiux-batch*.test.mjs` (contoh T29; juga slice-by-string lain)
- **Bukti:** beberapa guard membangun "blok" via `HTML.slice(indexOf(marker))` tanpa assert marker ditemukan (>0); jika marker drift karena rename template, guard diam-diam menjadi vakum.
- **Masalah:** helper slice-string rapuh tanpa kontrak; satu rename template cukup mematikan banyak asersi sekaligus.
- **Dampak:** perlindungan regresi menipis tanpa sinyal — persis mekanisme yang menutupi T26–T29.
- **Rekomendasi:** util `sliceBlock(html, marker)` yang throw bila indexOf<0; audit semua pemakaian slice/indexOf di suite dan migrasikan.

### S105 — reset_password memuat admin-core.js penuh hanya untuk toggle password

- **Prioritas:** 🟠 Sedang · **Usaha:** XS · **Area:** Publik/perf · **Status:** [ ]
- **Lokasi:** `templates/public/reset_password.html:181,:185–187`
- **Bukti:** `<script src="/static/js/admin-core.js">` dimuat halaman publik hanya demi `togglePasswordVisibility`; register.html sudah punya pola lokal `wirePwToggle` (:344–356).
- **Masalah/Dampak:** payload + coupling halaman anonim ke bundle admin (permukaan serangan & cache-miss di LAN sekolah).
- **Rekomendasi:** salin helper lokal ala register.html, hapus tag script admin-core.

### S106 — Toggle auto-approve tanpa indikator fokus terlihat (input 0×0, WCAG 2.4.7 pada kontrol keselamatan)

- **Prioritas:** 🟠 Sedang (argumen Tinggi ala T25 — eksekusi awal Batch 15 disarankan) · **Usaha:** XS · **Area:** Pengawasan/a11y · **Status:** [ ]
- **Lokasi:** `webui/templates/admin/pengawas_detail.html:1074–1078` (input opacity:0;width:0;height:0); satu-satunya gaya fokus global `*:focus-visible` di admin-base.css:95 menggambar outline pada kotak berukuran nol
- **Bukti:** verifikasi koordinator — tidak ada rule `#autoAcceptToggle:focus-visible`/`.pd-toggle-slider` di admin-base.css maupun template.
- **Masalah:** satu-satunya kontrol keselamatan halaman monitoring (auto-approve izin perangkat) tidak menunjukkan posisi fokus keyboard sama sekali.
- **Dampak:** operator keyboard tak tahu toggle sedang difokuskan — risiko menyalakan auto-approve tanpa sadar saat menekan Space.
- **Rekomendasi:** `#autoAcceptToggle:focus-visible + .pd-toggle-slider { outline: 2px solid var(--color-primary-light); outline-offset: 2px; }` + test penjaga ala T25.

---

---

### R99 — Tombol "Detail" submissions ~28px, tak sejajar standar sentuh 44px barisnya sendiri

- **Prioritas:** 🟡 Rendah · **Usaha:** XS · **Area:** Submissions · **Status:** [ ]
- **Lokasi:** `templates/admin/submissions.html:319` (vs Hapus :322 = 44px)
- **Masalah/Dampak:** target sentuh terkecil di tabel justru untuk aksi paling sering dibuka.
- **Rekomendasi:** samakan min-height/padding dengan Hapus.

### R100 — Atribut event inline (onfocus/onblur/onsubmit) hidup kembali di generator HTML admin.js

- **Prioritas:** 🟡 Rendah · **Usaha:** XS · **Area:** Dashboard/CSP hygiene · **Status:** [ ]
- **Lokasi:** `admin.js:2177` (onfocus/onblur) + `:2350` (onsubmit)
- **Masalah:** melanggar kontrak delegasi Actions + inline-handler guard (regex kapital pun lolos, lihat R105).
- **Rekomendasi:** ganti data-action + delegasi; hapus atribut on*.

### R101 — Pagination dashboard: boundary hanya class disabled, tanpa aria-disabled (beda pola dengan submissions)

- **Prioritas:** 🟡 Rendah · **Usaha:** XS · **Area:** Dashboard/a11y · **Status:** [ ]
- **Lokasi:** `admin.js:554–561` (bandingkan submissions.html:338,:346 yang benar)
- **Rekomendasi:** tambah `aria-disabled="true"` + skip render, satukan pola.

### R102 — Paritas bahasa R97 parsial: 'Generate Batch'/'Gagal generate batch'/'Batch Generate' + typo "Kesini"

- **Prioritas:** 🟡 Rendah · **Usaha:** XS · **Area:** Settings/bahasa · **Status:** [ ]
- **Lokasi:** `settings-packages.js:309,:315,:320`; `settings.html:1234`; `settings-system-apps.js:158,:270` ("Pilih atau Seret File Kesini")
- **Rekomendasi:** "Buat Massal", "Gagal membuat massal", "Buat Batch"; typo → "Ke Sini".

### R103 — th tanpa scope="col" + tabel tanpa caption (vouchers/riwayat/packages)

- **Prioritas:** 🟡 Rendah · **Usaha:** XS · **Area:** Settings/a11y · **Status:** [ ]
- **Lokasi:** `settings.html:1265–1271,:1325–1328,:1928–1934` (users :1028 sudah benar — jadikan acuan)
- **Rekomendasi:** tambah scope="col" + `<caption class="sr-only">` per tabel.

### R104 — durationText diinterpolasi mentah ke innerHTML (paritas defense-in-depth escape)

- **Prioritas:** 🟡 Rendah · **Usaha:** XS · **Area:** Settings/vouchers · **Status:** [ ]
- **Lokasi:** `settings-vouchers.js:89–93,:105`
- **Rekomendasi:** bungkus `escapeHtml(String(...))` sesuai kontrak file ini sendiri.

### R105 — Regex counter guard case-sensitive: `RGBA(`, `Z-INDEX:`, `ONCLICK=` kapital lolos

- **Prioritas:** 🟡 Rendah · **Usaha:** XS · **Area:** Integritas guard · **Status:** [ ]
- **Lokasi:** `RGBA_RE` batch7:36 / batch11:148 (tanpa flag `i`); `INLINE_HANDLER_RE` batch11:38; z-index regex batch14-tokens-guard:173–174
- **Rekomendasi:** tambah flag `/i` + test negatif ber-kapital.

### R106 — Touch target halaman hasil 38–42px, di bawah standar repo 44px, tanpa test penjaga

- **Prioritas:** 🟡 Rendah · **Usaha:** S · **Area:** Hasil/mobile · **Status:** [ ]
- **Lokasi:** `static/css/hasil.css:1001–1006` (standar repo: admin-base.css:514–522)
- **Rekomendasi:** naikkan min-height 44px + guard min-height di suite hasil.

### R107 — Resend OTP sukses tidak membersihkan digit lama — kode baru bercampur kode basi

- **Prioritas:** 🟡 Rendah · **Usaha:** XS · **Area:** Publik/auth · **Status:** [ ]
- **Lokasi:** `register_confirm.html:398–406` (cabang sukses hanya `startCooldown(60)`); server merotasikan kode (`cmd/server/auth_recovery.go:131`)
- **Rekomendasi:** pada sukses: kosongkan 6 digit, fokus ke digit-1, announce "kode baru terkirim".

### R108 — download.html memuat ulang public-mobile/desktop.css yang sudah dipancarkan public_head

- **Prioritas:** 🟡 Rendah · **Usaha:** XS · **Area:** Download/perf · **Status:** [ ]
- **Lokasi:** `download.html:5–6` vs `shared.html:117–119`
- **Masalah/Dampak:** request CSS dobel per kunjungan halaman unduh (halaman paling ramai siswa); juga melawan catatan urutan cascade R71.
- **Rekomendasi:** hapus dua link; guard statik: larangan link CSS mobile/desktop di luar shared.html.

### R109 — Masa berlaku OTP 15 menit tak pernah dikomunikasikan UI

- **Prioritas:** 🟡 Rendah · **Usaha:** XS · **Area:** Publik/auth · **Status:** [ ]
- **Lokasi:** TTL `cmd/server/auth_recovery.go:23`; copy halaman `register_confirm.html:220–231`, `reset_password.html:121–123`
- **Rekomendasi:** tambah "berlaku 15 menit" pada helper text + pesan resend.

### R110 — Paginasi Daftar Voucher tanpa aria-current/aria-label (audit sudah benar — paritas)

- **Prioritas:** 🟡 Rendah · **Usaha:** XS · **Area:** Settings/a11y · **Status:** [ ]
- **Lokasi:** `settings-vouchers.js:173` (acuan benar: `settings-voucher-audit.js:92`)
- **Rekomendasi:** salin pola aria-label + aria-current="page".

### R111 — Angka progres 0% memakai #6b7280 — warna paling redup justru untuk state paling lama terlihat

- **Prioritas:** 🟡 Rendah · **Usaha:** XS · **Area:** Pengawasan/kontras · **Status:** [ ]
- **Lokasi:** `pengawas.html:279` (def) & :334 (pakai)
- **Bukti:** `pct > 0 ? '#60a5fa' : '#6b7280'` — abu-abu medium di latar gelap < 4.5:1 (kelas T9).
- **Rekomendasi:** ganti `var(--color-text-muted)` (atau secondary) + hapus literal.

### R112 — announceQueueCount tak dipanggil di jalur notice/gagal — live region basi justru saat penting

- **Prioritas:** 🟡 Rendah · **Usaha:** XS · **Area:** Pengawasan/a11y · **Status:** [ ]
- **Lokasi:** `pengawas_detail.html` — hanya :1298/:1310 yang memanggil; jalur gerbang tertutup (:1283–1295) & catch (:1330) tidak
- **Rekomendasi:** `renderApprovalNotice(html, announceText)` — paksa penyertaan pesan live region di semua cabang.

### R113 — Snapshot access_logs+history di-JSON.stringify ke atribut DOM tiap tick polling

- **Prioritas:** 🟡 Rendah · **Usaha:** S · **Area:** Pengawasan/perf · **Status:** [ ]
- **Lokasi:** `pengawas_detail.html:1609–1610` (penulis), :1676–1684 (pembaca); payload backend `internal/handlers/admin/pengawas.go:421–424`
- **Masalah/Dampak:** serialisasi payload terbesar halaman diulang tiap 12 dtk ke atribut — boros CPU/GC di perangkat sekolah low-end.
- **Rekomendasi:** simpan ke variabel modul (WeakMap per kartu), buang atribut.

### R114 — Dead CSS ±115 baris + 9 simbol sprite tak terpakai di dua halaman pengawasan

- **Prioritas:** 🟡 Rendah · **Usaha:** XS · **Area:** Pengawasan/hygiene · **Status:** [ ]
- **Lokasi:** `pengawas.html:95–108,:176–207,:208–223`; `pengawas_detail.html:492–535,:580–585` + `.pd-quick-actions`(:985) tanpa pemakai; simbol :2235–2253
- **Rekomendasi:** hapus bertahap + guard simbol sprite (semua `<use href="#hi-*">` harus eksis dan terpakai).

### R115 — Kartu ujian role="button" membungkus link "Pantau" (interactive nested)

- **Prioritas:** 🟡 Rendah · **Usaha:** S · **Area:** Pengawasan/a11y · **Status:** [ ]
- **Lokasi:** `pengawas.html:314,:335`
- **Rekomendasi:** minimal aria-label eksplisit pada kartu + stopPropagation pada link; idealnya kartu tanpa role button, aksi via tombol internal.

### R116 — Label dwibahasa "Nilai / Score" + magic number per_page=20 terduplikasi

- **Prioritas:** 🟡 Rendah · **Usaha:** XS · **Area:** Pengawasan/konsistensi · **Status:** [ ]
- **Lokasi:** `pengawas_detail.html:1729` (label); `:1555` & `:1634` (per_page=20)
- **Rekomendasi:** "Nilai" saja (bahasa UI ID); konstanta SUBS_PER_PAGE bersama.

### R117 — Blok reduced-motion hasil.css tak meng-cap animation-iteration-count (selamat karena urutan load kebetulan)

- **Prioritas:** 🟡 Rendah · **Usaha:** XS · **Area:** Hasil/a11y · **Status:** [ ]
- **Lokasi:** `static/css/hasil.css:933–935` vs pola lengkap admin-base/public-desktop
- **Rekomendasi:** tambah `animation-iteration-count: 1 !important;` di blok tersebut.

### R118 — Guard FILES batch8 belum mencakup cek_hasil/index/forgot_password (informative, aktual 0/0 hari ini)

- **Prioritas:** 🟡 Rendah · **Usaha:** XS · **Area:** Integritas guard · **Status:** [ ]
- **Lokasi:** FILES guard batch8-publik
- **Rekomendasi:** tambahkan ketiga file dengan baseline aktual 0/0 supaya pertumbuhan literaltak lolos diam-diam.

### Catatan minor tanpa ID

Komentar prinsip "server-side" pada S84 sebenarnya pengisi client-side (perjelas komentar); blok @media hasil.css terfragmentasi 6 titik (rawankan konsolidasi saat file disentuh); komentar vestigial hasil.css:687 menyebut lokasi lama.

---

---

## 5.13 RE-REVIEW RONDE 10 — Temuan baru pasca Batch 15 (bahan Batch 16)

> **Tanggal:** 25 Agustus 2026 · **Basis kode:** `616132a` (pasca Batch 15) · **Metode:** 5 reviewer paralel (settings · pengawasan · publik auth/download · admin core/dashboard/submissions · token/guard-integrity) + verifikasi silang koordinator atas seluruh temuan Tinggi langsung ke kode.
> Ditemukan **2 masalah Tinggi, 6 Sedang, dan 14 Rendah baru**. Tema dominan ronde ini: **(1)** ekor polishment dari perbaikan besar Batch 15 — dua fix ternyata menghasilkan regresi/celah baru (autofill OTP, drawer touch-device) dan satu fix bersifat parsial; **(2)** kontrak escape/token yang belum seragam di titik-titik kecil; **(3)** dokumentasi teknis yang klaimnya keliru (narasi kaskade R117, angka kalibrasi kontras).

### ⚠️ KOREKSI INTEGRITAS PROSES — 5 item Batch 15 tercatat [x] padahal tidak dieksekusi

Audit ronde 10 menemukan **S92, R102, R103, R104, R110 luput dari pembagian tugas agen Batch 15** (tak ada agen pemilik file settings-vouchers/settings-packages/settings.html untuk item itu), namun tetap tercentang `[x]` di Rekap Tracking — persis kelas kesalahan integritas ronde 5. Bukti di HEAD `616132a`: `redemptionSeq`=0 hit; `aria-current` di settings-vouchers.js = 0; string 'Generate Batch'/'Kesini' masih ada; tabel vouchers tanpa `scope="col"`; `durationText` masih interpolasi mentah. **Status telah dikoreksi menjadi [ ] ⚠️ TERTINGGAL** dan masuk gerbang awal Batch 16. Pelajaran proses: checklist eksekusi wajib dipetakan eksplisit ke kepemilikan file agen sebelum penugasan.

### Status verifikasi cepat Batch 15 (lima area, spot-check langsung ke kode)

| Area | Vonis | Catatan |
|---|---|---|
| Settings (T26/T27/S93 + test Go lazy-audit) | ✅ PENUH | 45 pembuka vs 45 end; `go test ./internal/handlers/admin/` penuh `-count=1` OK; blok Kelola User pasca-restorasi diaudit utuh |
| Pengawasan/submissions (S89,S94,S106,R99,R111–R116) | ✅ | Semua benar intinya; dua ekor → S110 (paginasi ikut rebuild) & S111 (restore fokus tanpa fallback); R114 aman (selector hidup tak ada yang hilang) |
| Publik auth/download (S97,S98,S105,R107–R109) | ❌ 1 regresi | **S98 REGRESI → T30**; S97 lulus di ≤1100px tapi bermasalah di kelas perangkat lain → T31; sisanya ✅ |
| Admin core (T28,S90,S91,S102,R100,R101) | ⚠️ 1 parsial | S90 inti hidup tapi 3 celah polish → R132; lainnya ✅ (guard seq then+catch lengkap, on* = 0 folder-wide) |
| Token/guard/hasil (T29,S95,S99,S104,R105,R118,S96,S101,S103,R106,R117) | ✅⚠️ | Guard anti-vakum nyata; baseline == aktual terukur ulang; R117 vonisnya "lulus syarat" tapi narasi kaskadenya keliru → S112 |

**Gate:** `node --test static/js/uiux-batch*.test.mjs` = **966/0** · `go test ./internal/handlers/...` = ok semua · `go build`+`go vet` bersih (diverifikasi reviewer independen).

---

### T30 — Regresi S98: `autocomplete="one-time-code"` pada input hidden mematikan autofill OTP OS total

- **Prioritas:** 🔴 Tinggi · **Usaha:** S · **Area:** Publik/auth · **Status:** [ ]
- **Lokasi:** `templates/public/register_confirm.html:238`, `reset_password.html:131`
- **Bukti:** `<input type="hidden" id="otp_code" name="otp_code" value="" autocomplete="one-time-code">` di dalam form `autocomplete="off"` (:234/:125). Mesin autofill iOS/Android melewatkan field yang tak dirender/tak dapat fokus; dan meski terisi programatik pun, `syncHidden()` (register_confirm :282–292) menimpa `hiddenInput.value` murni dari kotak digit pada event input apa pun — isian OS terhapus diam-diam, tombol tetap disabled.
- **Masalah/Dampak:** perbaikan S98 memindahkan atribut ke tempat yang membuat autofill **tidak pernah terjadi** — lebih buruk dari kondisi pra-S98 (terpotong 1 digit). Siswa iOS/Android yang mengandalkan OTP otomatis kini harus mengetik manual tanpa tahu kenapa saran OTP tak muncul.
- **Rekomendasi:** pindahkan `one-time-code` ke `otp-digit-1` yang visible, hapus `maxlength="1"` minimal di kotak pertama (distribusi multi-karakter S98 sudah siap), pertimbangkan melepas `autocomplete="off"` level form; uji di perangkat nyata.

### T31 — Drawer nav tak terjangkau di perangkat sentuh ≥1101px + link off-canvas tetap tabbable (gap cakupan S97)

- **Prioritas:** 🔴 Tinggi · **Usaha:** M · **Area:** Publik/nav · **Status:** [ ]
- **Lokasi:** `shared.html:777–795` (inline `html.touch-device .nav-links{position:fixed;right:-280px}`), `public-mobile.css:180` (burger `display:none !important` di ≥1101px) vs `:187`/`public-desktop.css:17` (reset `visibility:visible` tanpa syarat)
- **Bukti:** di iPad Air/Pro landscape (±1180–1366px, kelas perangkat sekolah realistis): hamburger hilang, drawer terparkir off-canvas → **seluruh navigasi tak bisa dibuka**; dan karena visibility di-reset tanpa syarat, keenam link off-screen tetap masuk tab-order (fokus jatuh ke elemen terpotong `overflow-x:clip`).
- **Masalah/Dampak:** tujuan S97 ("drawer tertutup keluar tab-order") tidak tercapai di kelas perangkat sentuh lebar; navigasi utama lenyap sama sekali di sana.
- **Rekomendasi:** gerdarkan reset visibility + sembunyi burger dengan `(hover:hover) and (pointer:fine)`, atau tambahkan `html.touch-device .nav-links:not(.open){visibility:hidden}` dengan pola delay-transisi identik S97.

### S107 — Branch `window.loadSectionScript` mati: fallback tanpa cache-buster + modul system-apps berisiko dobel-eksekusi

- **Prioritas:** 🟠 Sedang · **Usaha:** S · **Area:** Settings/system-apps · **Status:** [ ]
- **Lokasi:** `templates/admin/settings.html:2088–2096` (branch mati), `:2335` (definisi privat tak diekspor)
- **Bukti:** `typeof window.loadSectionScript === 'function'` selalu false (grep repo: tak ada assignment `window.loadSectionScript`). Jalur aktif selalu fallback :2093–2095 yang (a) memuat modul **tanpa `?v={{.version}}`** — risiko JS basi pasca-deploy — dan (b) tak menandai `__settingsLoaded['system-apps']`, sehingga aktivasi tab berikutnya memuat modul lagi → listener `wireDragDrop` ganda (tanpa guard dataset).
- **Dampak:** unggahan bisa terproses dobel; deploy produksi menyajikan modul basi dari cache.
- **Rekomendasi:** ekspor loader ke window ATAU hapus branch mati + isi `__settingsLoaded` + tambahkan `?v=` di fallback.

### S108 — Klik "Muat Ulang" ikut melipat kartu Daftar User (toggle head tanpa cek target; stopPropagation datang terlambat)

- **Prioritas:** 🟠 Sedang · **Usaha:** XS · **Area:** Settings/users · **Status:** [ ]
- **Lokasi:** `static/js/settings-users.js:41–47` vs `admin.js:4286` (stopPropagation di ujung bubble) — bug baru-keliahatan karena blok ini baru ter-render lagi pasca-T26
- **Bukti:** handler toggle head tak memeriksa `e.target`; delegasi Actions di `document` (fase bubble) — urutan: tombol → **head (toggle jalan)** → document (stopPropagation terlambat).
- **Dampak:** setiap klik Muat Ulang sekaligus melipat kartu — hasil muat ulang tak terlihat tanpa klik lagi; terkesan seperti gagal muat.
- **Rekomendasi:** guard di awal toggle: `if (e.target.closest('[data-action]')) return;`

### S109 — openEditUserModal tanpa seq-token DAN tanpa `.catch` (kelas race S78 yang keempat)

- **Prioritas:** 🟠 Sedang · **Usaha:** S · **Area:** Dashboard/users · **Status:** [ ]
- **Lokasi:** `static/js/admin.js:2076–2158` (fetch :2080; grep catch di rentang = 0)
- **Bukti/Masalah:** (a) respons user A yang lambat menimpa field modal user B — loader konteks-modal TERAKHIR yang belum ber-token; (b) gagal jaringan = modal tak terbuka tanpa toast (unhandled rejection).
- **Rekomendasi:** salin pola `delegateModalSeq` (:2920–2992) + `.catch` toast.

### S110 — Paginasi submissions ditulis ulang tiap tick meski payload identik (melawan tujuan S94 sendiri)

- **Prioritas:** 🟠 Sedang · **Usaha:** XS · **Area:** Pengawasan · **Status:** [ ]
- **Lokasi:** `templates/admin/pengawas_detail.html:1621,:1627` vs `renderSubPagination` :1645–1678
- **Bukti:** jalur skip-if-identical S94 tetap memanggil `pagEl.innerHTML = html` tiap tick 12 dtk.
- **Dampak:** fokus keyboard di tombol halaman lenyap tiap tick — persis masalah yang S94 mau basmi, pindah lokasi.
- **Rekomendasi:** fingerprint HTML paginasi; skip bila identik (pola `subsLastHtml`).

### S111 — Restore fokus S94 tanpa fallback saat baris hilang dari daftar

- **Prioritas:** 🟠 Sedang · **Usaha:** XS · **Area:** Pengawasan/a11y · **Status:** [ ]
- **Lokasi:** `pengawas_detail.html:1538–1547`
- **Bukti:** `if (target && target.focus) target.focus();` — tanpa cabang else; elemen dicabut/disetujui/filter berubah → fokus jatuh ke `<body>` tanpa pengumuman.
- **Dampak:** keyboard/screen-reader user kehilangan posisi berkala di halaman monitoring yang justru paling sering berubah datanya.
- **Rekomendasi:** fallback `tbody.tabIndex=-1; tbody.focus()` + pesan singkat ke queueLiveRegion.

### S112 — Narasi kaskade R117 keliru; cap iterasi efektif hanya berkat backstop `!important` lintas-file yang tak dikunci guard

- **Prioritas:** 🟠 Sedang · **Usaha:** XS · **Area:** Hasil/a11y/integritas-guard · **Status:** [ ]
- **Lokasi:** `hasil.css:941–948` (klaim komentar :944–945) vs shorthand `.loading-spinner` :600/:626 (`infinite`, spesifisitas 0,1,0 mengalahkan universal 0,0,0); penyelamat nyata: `public-desktop.css:6` + `public-mobile.css:203` (keduanya `!important`)
- **Bukti:** analisis dua reviewer independen sepakat: tanpa layer lain, cap tanpa `!important` di hasil.css KALAH. Guard R117 (`uiux-batch15-publik.test.mjs:562–566`) hanya asersi kehadiran `iteration-count: 1` — tak mengunci backstop layer maupun status `!important`.
- **Dampak:** refactor link CSS suatu hari bisa menjadikan reduced-motion vakum senyap; dokumentasi yang menjustifikasikan keputusan memuat klaim teknis salah (menyesatkan generasi berikutnya).
- **Rekomendasi:** koreksi komentar + guard eksplisit bahwa kedua layer membawa cap `!important` (atau terima +1 `!important` agar self-contained).

---

### R119 — Listener `change` syncEditLimitFields menumpuk tiap kali modal Atur User dibuka

- **Prioritas:** 🟡 Rendah · **Usaha:** XS · **Area:** Dashboard · **Status:** [ ]
- **Lokasi:** `admin.js:2153–2154` (dieksekusi dalam `.then`, modal di-cache :2089–2093)
- **Rekomendasi:** pasang sekali di `createEditUserModal()`.

### R120 — Nomor halaman paginasi dashboard tanpa `aria-current="page"` (ekor paritas R101)

- **Prioritas:** 🟡 Rendah · **Usaha:** XS · **Area:** Dashboard/a11y · **Status:** [ ]
- **Lokasi:** `dashboard.html:559` (bandingkan submissions.html:343 yang benar)
- **Rekomendasi:** tambah kondisional aria-current; satukan pola.

### R121 — Target sentuh tombol aksi baris ujian dashboard ±25px

- **Prioritas:** 🟡 Rendah · **Usaha:** S · **Area:** Dashboard/mobile · **Status:** [ ]
- **Lokasi:** `dashboard.html:469–529` × `.btn-sm-compact` admin-base.css:443–446; media-query mobile hanya menaikkan `.pd-action-btn`
- **Rekomendasi:** mobile `min-height/min-width ≥44px` untuk tombol baris tabel.

### R122 — loadSaasSettings tanpa `.catch`: form Pengaturan kosong senyap saat jaringan gagal

- **Prioritas:** 🟡 Rendah · **Usaha:** XS · **Area:** Settings/general · **Status:** [ ]
- **Lokasi:** `admin.js:3782–3871` (fetch :3783, rantai then saja)
- **Rekomendasi:** `.catch` toast + state retry ala `users-retry-load`.

### R123 — `p.role` mentah di builder dropdown pengawas (kontrak escape bolong 1 titik)

- **Prioritas:** 🟡 Rendah · **Usaha:** XS · **Area:** Dashboard/escape · **Status:** [ ]
- **Lokasi:** `admin.js:1113` (username sebelahnya ter-escape :1112)
- **Rekomendasi:** bungkus `escapeHtml(p.role || 'Pengawas')`.

### R124 — Kontrak escape `data-mac` ganda: `jsEscape` di baris vs mentah di tombol/pembanding

- **Prioritas:** 🟡 Rendah · **Usaha:** XS · **Area:** Pengawasan/escape · **Status:** [ ]
- **Lokasi:** `pengawas_detail.html:1321` (baris: `escapeHtml(jsEscape(...))`) vs :1336/:1339 (tombol: `escapeHtml(...)` mentah) vs lookup :1347–1353
- **Masalah:** MAC mengandung `\`/`'` membuat pembandingan remove/update-in-place meleset → baris duplikat. Edge-case, tapi inkonsistensi kontrak nyata.
- **Rekomendasi:** satu kontrak `escapeHtml(a.mac_address)` di semua atribut data; hapus jsEscape :1321.

### R125 — Kembaran warna muted tersisa 5 lokasi folder-wide pasca R111

- **Prioritas:** 🟡 Rendah · **Usaha:** S · **Area:** Token · **Status:** [ ]
- **Lokasi:** `pengawas_detail.html:201,:577`; `dashboard.html:74`; `admin-base.css:889` (`.student-status.not-started`); `public-mobile.css:49` (`.nav-link`)
- **Bukti:** bentuk rgb `rgba(107,114,128,…)` + `#9ca3af` (= keluarga gray Tailwind) tersebar; hanya pengawas.html yang dibersihkan R111.
- **Rekomendasi:** migrasi ke `var(--color-text-muted)`/`rgba(var(--rgb-neutral),…)` + kunci plafon.

### R126 — Guard literal tidak mencakup `static/css/**`: 4× `#f87171` lolos di folder tailwind

- **Prioritas:** 🟡 Rendah · **Usaha:** S · **Area:** Integritas guard · **Status:** [ ]
- **Lokasi:** walk guard batch14-tokens-guard:67–74 (hanya templates+JS); literal di `static/css/tailwind/output.css:1269,:2486`, `admin-tailwind.css:715,:1424` (yang terakhir komponen kustom `.toast-error`)
- **Rekomendasi:** walk CSS inti non-generated atau dokumentasikan pengecualian tailwind eksplisit.

### R127 — Print: gradien `-webkit-text-fill-color` di luar hasil.css belum di-reset (luar cakupan S96)

- **Prioritas:** 🟡 Rendah · **Usaha:** S · **Area:** Print · **Status:** [ ]
- **Lokasi:** `shared.html:255,:345,:545,:640`; `download.html:53`; `public-mobile.css:46` (logo); `settings.html:483` — grep `@media print` di file tsb = 0
- **Rekomendasi:** blok print ala S96 untuk shared/download, atau dokumentasikan risiko (frekuensi cetak rendah).

### R128 — Komentar kalibrasi kontras memakai nilai token fiktif

- **Prioritas:** 🟡 Rendah · **Usaha:** XS · **Area:** Hasil/dokumentasi · **Status:** [ ]
- **Lokasi:** `hasil.css:340–343` ("success-light ≈ 10.8:1") & :362 ("bright ≈ 6.25:1")
- **Bukti:** ukur presisi reviewer independen: `--color-success-light` aktual = **#34d399** (bukan #6ee7b7) = **8.34:1**; #f87171 = **6.19:1**. Semua PASS AA — vonis tak berubah, tapi angka komentar menyesatkan kalibrasi berikutnya.
- **Rekomendasi:** koreksi angka komentar ke nilai token aktual.

### R129 — Emoji ⚙️ di opsi voucher + `<h3>` berisi `<div>` (content model invalid)

- **Prioritas:** 🟡 Rendah · **Usaha:** XS · **Area:** Settings/markup · **Status:** [ ]
- **Lokasi:** `settings.html` opsi "⚙️ Kustom…" & judul grup kustom; `:2002–2007` h3 uploadModalTitle berisi div ikon
- **Rekomendasi:** teks polos "Kustom"; turunkan div ikon keluar dari h3.

### R130 — `theme-color` absen di seluruh halaman auth publik

- **Prioritas:** 🟡 Rendah · **Usaha:** XS · **Area:** Publik/konsistensi · **Status:** [ ]
- **Lokasi:** register/register_confirm/reset_password/forgot_password/cek_hasil (hanya shared.html:121 yang punya)
- **Rekomendasi:** tambah meta theme-color `#09090e` (pola S34).

### R131 — Guard `{{ if .android_app }}` membungkus loop rilis Android *tambahan* — potensi dead-branch

- **Prioritas:** 🟡 Rendah · **Usaha:** XS · **Area:** Download · **Status:** [ ]
- **Lokasi:** `download.html:548–565`
- **Bukti/Masalah:** bila app resmi belum diunggah tapi ada system_apps Android lain, entri tak dirender (kartu "Belum ada installer" padahal stok ada); tanpa komentar intent.
- **Rekomendasi:** ubah ke `{{ if .system_apps }}` atau dokumentasikan disengaja.

### R132 — Dropdown pengawas lanjutan S90 (parsial): `aria-expanded` stale + tanpa Escape/fokus-kembali

- **Prioritas:** 🟡 Rendah · **Usaha:** S · **Area:** Dashboard/a11y · **Status:** [ ]
- **Lokasi:** `admin.js:1127–1134` (klik-luar tanpa reset aria-expanded), `:1033–1039` (keydown hanya Enter/Space)
- **Dampak:** SR membaca "expanded=true" pada dropdown tertutup; konvensi Escape sudah ada di menu topbar (admin-core.js:449–454).
- **Rekomendasi:** reset aria-expanded di klik-luar; tangani Escape + `hd.focus()` saat menutup.

### Catatan minor tanpa ID

Jalur tutup via klik-overlay drawer tidak mengembalikan fokus ke burger (hanya jalur closeMenu yang iya) — konsistensi kecil menyertai T31; fetch `loadPackages`/`loadApps` single-trigger tanpa token (dampak rendah, reload pasca-save) — cukup dicatat; `HEX_RE` menangkap entitas HTML (`&#8226;`) sebagai false-positive plafon (terdokumentasi di suite).

---

---

## 5.14 RE-REVIEW RONDE 11 — Temuan baru pasca Batch 16 (bahan Batch 17)

> **Tanggal:** 25 Agustus 2026 · **Basis kode:** `80e95bb` (pasca Batch 16) · **Metode:** 2 reviewer paralel (settings · admin-core/dashboard/submissions) + review langsung oleh koordinator untuk 3 area tersisa (token/guard-integrity · pengawasan · publik auth/download) setelah agen paralel berulang kali gagal/cancel.
> Ditemukan **0 masalah Tinggi, 3 Sedang, dan 11 Rendah baru** — volume menurun tajam dibanding dua ronde sebelumnya: kualitas terkonvergensi pasca dua batch eksekusi besar. NAMUN audit integritas kembali menemukan **item tercatat [x] yang tidak/belum tereksekusi** (lihat koreksi di bawah) — akar masalahnya identik dengan ronde 10: agen `batch16-pengawasan` dalam peta rencana tidak pernah diluncurkan.

### ⚠️ KOREKSI INTEGRITAS PROSES (Batch 16)

| Item | Status tercatat | Fakta di HEAD `80e95bb` |
|---|---|---|
| S110 | [x] ✅ | ❌ `renderSubPagination` (:1645) tanpa fingerprint/skip — dipanggil mentah :1591/:1621/:1627 |
| S111 | [x] ✅ | ❌ `restoreSubsFocus` (:1538–1547) tanpa cabang fallback saat target hilang |
| R124 | [x] ✅ | ❌ :1321 masih `escapeHtml(jsEscape(a.mac_address))` vs :1336/:1339 polos |
| R125 | [x] ✅ | ⚠️ PARSIAL — dashboard/admin-base/public-mobile bersih, tapi `pengawas_detail.html:201,:576–580` masih literal rgba(107,114,128,…)/#9ca3af |

Status dikoreksi menjadi `[ ]`/parsial dan masuk gerbang awal Batch 17. Pelajaran proses (dua kali berulang): **checklist eksekusi wajib diverifikasi silang ke diff commit — bukan ke rencana penugasan.**

### Status verifikasi cepat Batch 16

| Area | Vonis | Catatan |
|---|---|---|
| Settings (S92†,R102†,R103†,R104†,R110†,S107,S108,R129) | ✅ PENUH | redemptionSeq/appLoadSeq/packageLoadSeq then+catch lengkap; satu catatan robustness → S114 |
| Publik (T30,T31,R125-mobile,R127,R130,R131) | ✅ PENUH | one-time-code di digit-1 visible tanpa maxlength ✓; drawer gabungan opsi A+B ✓; keputusan form-autocomplete didokumentasikan per halaman ✓ |
| Guard (S112,R126,R128) | ✅ PENUH | narasi kaskade baru akurat; walk CSS jalan; angka kalibrasi terverifikasi ulang |
| Admincore koordinator (S109,R119–R123,R132,R125-dashboard/admin-base) | ⚠️ 1 parsial | semua benar; R132 menyisakan celah interaksi → S113 |

**Gate:** node suite **1023/1023** hijau · `go test ./internal/handlers/... -count=1` OK · `go build`+`go vet` bersih · plafon folder-wide templates hex 106≤107 / rgba 93≤104 (slack hex tinggal 1 — pertimbangkan dikunci aktual di Batch 17).

---

### S113 — Escape di dalam dropdown pengawas menutup SELURUH modal Delegasi (interaksi R132 × Modal Manager)

- **Prioritas:** 🟠 Sedang · **Usaha:** S · **Area:** Dashboard/delegasi · **Status:** [ ]
- **Lokasi:** `admin.js:1161–1162` (fokus pindah ke search box saat dropdown dibuka), `admin.js:1033–1050` (Escape hanya di header), `admin-core.js:1019–1031` (early-return manager tak mengenal `#pengawasDropdown`)
- **Bukti:** buka dropdown → fokus otomatis di kolom cari → tekan Escape (niat: tutup dropdown) → handler header tak terjangkau, Modal manager `forceClose(top)` menutup seluruh modal Delegasi Ujian — **form yang sudah diisi hilang**; dropdown tertinggal `display:block`.
- **Rekomendasi:** handler Escape capture-phase pada `#pengawasDropdown` (tutup dropdown + fokus ke header), atau masukkan dropdown yang sedang tampil ke selector early-return manager.

### S114 — Fallback loader system-apps menandai modul "loaded" sebelum sukses & tanpa `onerror`

- **Prioritas:** 🟠 Sedang · **Usaha:** XS · **Area:** Settings/system-apps · **Status:** [ ]
- **Lokasi:** `templates/admin/settings.html:2100–2105`
- **Bukti:** `window.__settingsLoaded['system-apps'] = true;` dieksekusi saat MULAI memuat script, dan tidak ada `s.onerror`. Satu gagal muat (LAN flaky/deploy) → flag selamanya true → `loadSectionScript` skip, tab Aplikasi Sistem mati senyap sampai reload halaman; tombol "Unggah Aplikasi Baru" klik-nihil.
- **Rekomendasi:** tiru pola `loadSectionScript`: `s.onerror` mengembalikan flag ke false + toast gagal-muat.

### S115 — Badge salin kode voucher keyboard-dead (`<div>` klik-saja untuk aksi inti)

- **Prioritas:** 🟠 Sedang · **Usaha:** XS · **Area:** Settings/vouchers/a11y · **Status:** [ ]
- **Lokasi:** `static/js/settings-vouchers.js:100–103` (+ CSS `.voucher-code-badge`)
- **Bukti:** `<div class="voucher-code-badge" data-action="copy" …>` tanpa role/tabindex/keydown — satu-satunya cara menyalin kode voucher adalah mouse; delegasi hanya `click`.
- **Rekomendasi:** ubah jadi `<button type="button">` (delegasi click tetap jalan); atau role="button"+tabindex+branch keydown.

---

### R133 — Catch `openEditUserModal` tanpa guard seq (asimetri pola S102)

- **Prioritas:** 🟡 Rendah · **Usaha:** XS · **Area:** Dashboard/users · **Status:** [ ]
- **Lokasi:** `admin.js:2176–2180`
- **Masalah/Dampak:** klik cepat A(lambat, gagal) → B(sukses): toast error basi A muncul setelah modal B terisi — menyesatkan. Pola S102 ber-guard di kedua cabang.
- **Rekomendasi:** `if (seq !== editUserModalSeq) return;` di catch.

### R134 — `submitEditUser` menyimpang dari kontrak guard dobel-kirim S27

- **Prioritas:** 🟡 Rendah · **Usaha:** XS · **Area:** Dashboard/users · **Status:** [ ]
- **Lokasi:** `admin.js:2365–2368` (bandingkan pola acuan :2560–2562, :1405–1406)
- **Rekomendasi:** tambah `if (!btn || btn.disabled) return;` agar auditability kontrak terjaga.

### R135 — Anchor paginasi `aria-disabled` tetap ber-`href` hidup (keyboard bisa ke page 0/melebihi)

- **Prioritas:** 🟡 Rendah · **Usaha:** S · **Area:** Dashboard+submissions · **Status:** [ ]
- **Lokasi:** `dashboard.html:557,:564`; `submissions.html:337–338,:345–346`
- **Bukti/Masalah:** `pointer-events:none` hanya blokir mouse; link fokusable + Enter menavigasi ke `?page=0` (flicker + scroll reset); kontradiksi semantik SR ("disabled" tapi aktif).
- **Rekomendasi:** conditionally omit `href` saat disabled (paling bersih).

### R136 — Listener global ganda melumpuhkan guard klik-dalam identity-popup

- **Prioritas:** 🟡 Rendah · **Usaha:** XS · **Area:** Submissions · **Status:** [ ]
- **Lokasi:** `admin.js:4119–4122` (ber-guard `closest('.identity-popup')`) vs `:4126–4129` (dokumen tanpa syarat)
- **Dampak:** seleksi teks identitas di dalam popup ikut menutup popup — niat guard pertama efektif mati.
- **Rekomendasi:** hapus listener kedua atau beri guard sama.

### R137 — Literal warna bypass token di kartu kuota dashboard

- **Prioritas:** 🟡 Rendah · **Usaha:** XS · **Area:** Dashboard/token · **Status:** [ ]
- **Lokasi:** `dashboard.html:312` (`#c084fc`), `:320` (`#38bdf8`)
- **Rekomendasi:** migrasi ke `var(--color-accent-light)` / varian info-light yang setara.

### R138 — Tombol `.btn-more` baris ujian tanpa `aria-expanded`, tutupnya tak memulihkan fokus

- **Prioritas:** 🟡 Rendah · **Usaha:** S · **Area:** Dashboard/a11y · **Status:** [ ]
- **Lokasi:** `dashboard.html:532`; `admin.js:3195–3298` (toggle/close/outside/Escape)
- **Masalah:** kelas cacat yang sama sudah dibereskan untuk dropdown pengawas (S90+R132) dan topbar — menu popup ini belum: SR tak tahu state; setelah Escape fokus jatuh ke body.
- **Rekomendasi:** `aria-haspopup` + sinkron `aria-expanded` di ketiga jalur tutup + fokus kembali ke `.btn-more` pemilik menu (tersimpan via `__btnWrap`).

### R139 — Paginasi JS Daftar User tanpa `aria-current` + tombol aktif malah `disabled`

- **Prioritas:** 🟡 Rendah · **Usaha:** XS · **Area:** Settings/users/a11y · **Status:** [ ]
- **Lokasi:** `admin.js:1861–1865` (`renderUsersPagination`)
- **Masalah:** tombol halaman aktif `disabled` → tak fokusable, SR tak bisa mengumumkan posisi; paritas R120 hanya menjangkau jalur Go-template daftar ujian.
- **Rekomendasi:** `aria-current="page"` saat isCurrent (+ `aria-label="Halaman N, halaman saat ini"`).

### R140 — Registrasi `__settingsReady['packages']` yatim (dead wiring)

- **Prioritas:** 🟡 Rendah · **Usaha:** XS · **Area:** Settings/packages · **Status:** [ ]
- **Lokasi:** `static/js/settings-packages.js:147`
- **Bukti/Masalah:** key section `'packages'` tak pernah eksis pasca redesign 5-tab; risiko dobel-init bila kelak dihidupkan.
- **Rekomendasi:** hapus baris; jalur init yang hidup adalah `initPackages`.

### R141 — `activatePackage` tanpa penahan klik-ganda

- **Prioritas:** 🟡 Rendah · **Usaha:** S · **Area:** Settings/billing · **Status:** [ ]
- **Lokasi:** `static/js/settings-billing.js:168–192` (tombol :139)
- **Masalah/Dampak:** tombol tetap aktif selama fetch + jeda 1,2 dtk pra-reload → POST aktivasi bisa dobel.
- **Rekomendasi:** disable tombol pemanggil di awal handler, pulihkan di cabang error.

### R142 — Paritas label toolbar lipat + sisa campuran bahasa pasca-R102

- **Prioritas:** 🟡 Rendah · **Usaha:** S · **Area:** Settings/bahasa · **Status:** [ ]
- **Lokasi:** `settings-general.js:126` (label + count digabung; pola users :101–103 sudah murni + count ke title); `settings.html:1236/:1350` "Buat Voucher Single"; `:1442` vs `:1556` ("Nama Campaign" vs "Campaign Name")
- **Rekomendasi:** terapkan pola users ke general; konsolidasikan istilah.

### R143 — `attempt_count` & `s.id` interpolasi mentah di builder submissions monitoring

- **Prioritas:** 🟡 Rendah · **Usaha:** XS · **Area:** Pengawasan/escape · **Status:** [ ]
- **Lokasi:** `pengawas_detail.html:1606` (title attr + teks), `:1615` (data-submission-id)
- **Masalah:** nilai numerik dari server masuk innerHTML/atribut tanpa `escapeHtml(String(...))` — risiko aktual rendah, tapi melanggar kontrak escape-everything yang ditegakkan lintas batch (preseden R123).
- **Rekomendasi:** bungkus `escapeHtml(String(...))` demi paritas kontrak.

### Catatan minor tanpa ID

Suite batch16 aman dari kelas marker-vakum (indexOf function-boundary + fallback window — bukan slice-by-string rapuh); `localizeUTC(l.created_at)` disisipkan mentah di builder audit (server-controlled informatif — parity kandidat R143); slack plafon hex templates tinggal 1 (107 vs 106) — kunci aktual di rekonsiliasi berikutnya; `wireCollapseBlock` general (:39–43) belum ber-guard `[data-action]` ala S108 (saat ini aman, seragamkan bila head diberi kontrol).

---

---

## 5.15 RE-REVIEW RONDE 12 — Temuan baru pasca Batch 17 (bahan Batch 18)

> **Tanggal:** 25 Agustus 2026 · **Basis kode:** `5095c75` (pasca Batch 17) · **Metode:** review berurutan oleh koordinator (tanpa agen) — sweep global mekanis + verifikasi kualitas eksekusi Batch 16/17 per area (settings · admin core · pengawasan · publik) + audit meta guard generasi kelima.
> Ditemukan **0 masalah Tinggi, 1 Sedang, dan 2 Rendah baru** — volume terkecil sejak ronde pertama; kode berada pada titik stabil. NAMUN satu klaim guard lama terkoreksi: **"larangan z-index ≥1000" ronde 8 ternyata hanya mengunci tiga lokasi spesifik, bukan larangan folder-wide** — admin-base.css masih memuat 7 literal ≥9998 (→ S116). Verifikasi integritas: plafon folder-wide kini PERSIS aktual (hex 99/99, rgba 89/89), gray twins 0, inline handler 0, gray-area lain bersih.

### Status verifikasi cepat Batch 17

| Area | Vonis | Catatan |
|---|---|---|
| Settings (S114,S115,R140,R141,R142 — agen) | ✅ PENUH | onerror+reset flag+toast ✓; badge jadi `<button>` ✓; dead wiring packages dihapus ✓; activatePackage anti-dobel-klik ✓; label toolbar/istilah konsolidasi ✓ |
| Pengawasan gerbang (S110†,S111†,R124†,R125-sisa†,R143) | ✅ PENUH | fingerprint `pagEl.__lastHtml` + fallback fokus tbody+announce + kontrak data-mac tunggal + token muted + escape attempt_count/s.id |
| Admincore (S113,R133–R139) | ✅ dengan 1 ekor | Escape dropdown delegasi tertahan di dalam dropdown ✓; guard seq catch simetris ✓; **ekor**: jalur tutup klik-luar row-dropdown belum reset aria-expanded → R144 |
| Publik (warisan T30/T31/R127/R130/R131) | ✅ | distribusi multi-karakter OTP solid; drawer gabungan A+B konsisten |
| Token/guard | ✅ | plafon = aktual PERSIS; gray twins 0; inline handler 0; z-index ≥4-digit = temuan baru S116 |

**Gate:** node suite **1047/1047 hijau** · `go test ./internal/handlers/... -count=1` OK semua · `go build`+`go vet` bersih.
**Bukti guard bekerja:** implementasi awal fingerprint S110 (versi `window.*`) memicu ReferenceError di sandbox vm → render ganda — **tertangkap test S94 batch15** (writes 4≠2) sebelum commit; diperbaiki ke `pagEl.__lastHtml`.

---

### S116 — Guard "larangan z-index ≥1000" ternyata tidak pernah folder-wide; admin-base.css memuat 7 literal legacy ≥9998

- **Prioritas:** 🟠 Sedang · **Usaha:** M · **Area:** Integritas guard/token · **Status:** [ ]
- **Lokasi:** `static/css/admin-base.css:187 (:9998), :292 (:9997), :748 (:10000 modal-overlay), :755 (:10001 toast-container), :980,:1004,:1041 (:10002 topbar)`; plus `templates/public/download.html` (:9999) & `templates/public/register.html` (:9999); tailwind generated (exempt terdokumentasi)
- **Bukti:** grep folder-wide = **19 kemunculan** z-index ≥1000; guard batch14-tokens-guard R88 hanya berisi TIGA asersi lokasi spesifik (popup dashboard, dropdown admin.js, pill settings) — klaim header suite "larangan z-index ≥1000 (kelas berbahaya)" tidak pernah ditulis sebagai asersi umum, dan file CSS bahkan tidak di-walk (R126 baru menambah walk untuk #f87171).
- **Masalah/Dampak:** stacking context legacy (modal-overlay 10000 vs toast 10001 vs topbar 10002) hidup di luar sistem `--z-*`; komponen baru bisa menyisip layer yang melompati toast tanpa terdeteksi — persis kelas erosi yang dibereskan untuk warna.
- **Rekomendasi:** (a) petakan ketujuh literal admin-base ke token `--z-*` (nilai semantik sudah ada: modal/toast/topbar/dropdown); (b) migrasikan 9999 download/register ke token banner; (c) tambah guard folder-wide (templates+CSS inti+JS): larangan literal z-index ≥1000 dengan whitelist generated tailwind; (d) kunci ulang plafon.

### R144 — Jalur tutup klik-luar & "tutup semua lainnya" row-dropdown tidak me-reset `aria-expanded` (ekor R138)

- **Prioritas:** 🟡 Rendah · **Usaha:** XS · **Area:** Dashboard/a11y · **Status:** [ ]
- **Lokasi:** `admin.js:3310–3317` (klik-luar hanya `classList.remove('show')`); `admin.js:3205–3208` ("Close all other dropdowns" idem)
- **Bukti/Masalah:** R138 menyinkronkan aria-expanded pada toggle & Escape, tapi dua jalur tutup lainnya membiarkan tombol pemilik menu tetap `aria-expanded="true"` — screen reader membaca menu terbuka padahal tertutup (pola identik R132 yang sudah dibereskan untuk dropdown pengawas).
- **Rekomendasi:** di kedua jalur, telusuri `__btnWrap.querySelector('.btn-more')` → `setAttribute('aria-expanded','false')`.

### R145 — Jalur tutup via klik overlay tidak mengembalikan fokus ke burger (inkonsisten dengan Escape)

- **Prioritas:** 🟡 Rendah · **Usaha:** XS · **Area:** Publik/nav/a11y · **Status:** [ ]
- **Lokasi:** `shared.html:20` (overlay `data-nav-toggle`) + `:43–50` (`toggleMenu` tanpa focus-return) vs `closeMenu` :54–70 (punya wasOpen + `burger.focus()`)
- **Bukti/Masalah:** overlay dan hamburger sama-sama ter-wire ke `toggleMenu` (:51) — menutup drawer lewat klik overlay menyinkronkan aria-expanded tapi tidak mem-fokuskan burger, sedangkan jalur Escape melakukannya. Dua jalur tutup, dua perilaku fokus.
- **Rekomendasi:** ekstraksi logik closeMenu (dengan wasOpen+focus) dan panggil dari kedua jalur, atau tambah focus-return di cabang tutup toggleMenu.

### Catatan minor tanpa ID

Duplicate-id `concurrentInput`/`limitInput`/`newUserExpiry` settings.html (:903–942) adalah pola Go `{{if operator}}{{else}}` — hanya satu cabang render per role, aman runtime (kandidat refactor id-per-role bila suatu saat diaudit validator); `localizeUTC(l.created_at)` tetap disisipkan mentah di builder audit (server-controlled informatif — parity kandidat); `wireCollapseBlock` general (:39–43) belum ber-guard `[data-action]` ala S108 (saat ini aman); slack hex folder-wide tinggal 0 — setiap warna baru WAJIB lewat token.

---

---

## 5.16 RE-REVIEW RONDE 13 — Temuan baru pasca Batch 18 (bahan Batch 19)

> **Tanggal:** 25 Agustus 2026 · **Basis kode:** `6507041` (pasca Batch 18) · **Metode:** review berurutan oleh koordinator (tanpa agen) — verifikasi eksekusi Batch 18 per area, sweep kelas baru (target-blank noopener, meta head parity, z-index runtime chain, ikon-only buttons), dan audit file statis tak-terreferensi.
> Ditemukan **0 masalah Tinggi, 1 Sedang, dan 2 Rendah baru** — untuk kedua kalinya berturut-turut volume di bawah 4: proyek berada pada fase stabilisasi. Verifikasi Batch 18: ✅ PENUH (token tangga stacking terpasang, guard folder-wide jalan, R144/R145 benar); gate **1054/1054** node hijau, go test/build/vet bersih.

### Status verifikasi cepat Batch 18

| Item | Vonis | Bukti |
|---|---|---|
| S116 token+migrasi+guard folder-wide | ✅ | admin-base 0 literal ≥1000 & pemakaian var() positif ×5; download/register → var(--z-dropdown); walk CSS inti non-generated lolos |
| S116 bonus | ✅ | `.toast-container` 10001→var(--z-toast): tie dengan onboarding terpecahkan sesuai intent theme.css |
| R144 dua jalur tutup row-dropdown | ✅ | klik-luar & close-others menelusuri `__btnWrap` → reset aria-expanded |
| R145 toggleMenu focus-return | ✅ | wasOpen + burger.focus() saat transisi open→closed |

---

### S117 — File mati `admin-tailwind.css` (67KB, nol referensi) + definisi ganda `.toast-container` lintas-file yang hanya aman karena urutan muat kebetulan benar

- **Prioritas:** 🟠 Sedang · **Usaha:** S · **Area:** Arsitektur CSS/integritas · **Status:** [ ]
- **Lokasi:** `static/css/tailwind/admin-tailwind.css` (67KB, grep repo-wide: nol referensi dari template/Go/CSS/JS); `.toast-container` terdefinisi DUA kali — `tailwind/output.css:1106` (`z-index: 9999`, dimuat produksi) dan `admin-base.css:755` (`z-index: var(--z-toast)`=10002, dimuat belakangan sehingga menang)
- **Bukti/Masalah:** (a) admin-tailwind.css tidak pernah dimuat siapa pun namun tetap berada di dalam folder statis publik (`/static/css/tailwind/admin-tailwind.css` dapat diunduh siapa saja) dan selama ini membebani kebijakan guard (baseline f87171, pengecualian z-index) untuk file yang tak berdampak runtime; (b) komponen layer-tertinggi aplikasi (toast) memiliki z-index yang efektifnya bergantung penuh pada URUTAN MUAT stylesheet — persis pola rapuh R117; jika urutan link berubah atau rule admin-base dihapus, toast turun ke level dropdown (9999) senyap, dan guard z-index tidak melihatnya karena folder tailwind di-exempt.
- **Rekomendasi:** (a) hapus `admin-tailwind.css` (atau pindah keluar folder statis + dokumentasikan status arsip); (b) hapus `z-index` dari rule `.toast-container` output.css (posisi/layout tetap di sana, lapisan dikunci satu tempat di admin-base via token); (c) pertimbangkan whitelist per-selector alih-alih per-folder bila kelak ada blok kustom di dalam generated CSS.

### R146 — `target="_blank"` tanpa `rel="noopener"` (×2)

- **Prioritas:** 🟡 Rendah · **Usaha:** XS · **Area:** Dashboard/settings/higiene · **Status:** [ ]
- **Lokasi:** `templates/admin/settings.html:2178` (unduh aplikasi), `templates/admin/dashboard.html:534` (lihat PDF)
- **Masalah/Dampak:** keduanya same-origin sehingga risiko tab-nabbing minim, tetapi `noopener` tetap konvensi defensif (mengisolasi `window.opener` + mencegah pembukaan tab memblokir halaman asal di beberapa browser).
- **Rekomendasi:** tambah `rel="noopener"` pada kedua anchor.

### R147 — Halaman konfirmasi registrasi tanpa `noindex` (inkonsisten reset_password; URL membawa username)

- **Prioritas:** 🟡 Rendah · **Usaha:** XS · **Area:** Publik/privasi · **Status:** [ ]
- **Lokasi:** `templates/public/register_confirm.html:3` (<head> tanpa meta robots) vs `reset_password.html:11` (`noindex, nofollow`)
- **Bukti/Masalah:** halaman OTP registrasi memiliki URL `?username={{.username}}` — jika tautannya pernah bocor ke halaman terindeks, pola URL + nama akun dapat muncul di hasil pencarian; saudaranya (reset password) sudah benar noindex. Juga tanpa meta description (parity R147-minor).
- **Rekomendasi:** tambah `<meta name="robots" content="noindex, nofollow">` (+ description singkat) menyamai reset_password.

### Catatan minor tanpa ID

`localizeUTC(l.created_at)` tetap mentah di builder audit (server-controlled informatif); `wireCollapseBlock` general belum ber-guard `[data-action]` ala S108 (saat ini aman); posisi toast (fixed bottom-right) tidak overlap topbar-toggle (top-right) sehingga nilai 10002 kembar Batch 18 aman secara visual; hitungan "button tanpa aria-label" mentah menyesatkan — mayoritas ber-teks visible (nama aksesibel sah); plafon hex templates kini 99=99 & rgba 89=89 — slack NOL, setiap warna baru wajib lewat token (guard akan merah, itu memang tujuannya).

---

---

## 6. REKAP TRACKING

> Centang `[x]` + cantumkan hash commit saat selesai. Urut sesuai prioritas eksekusi.
### Batch 19 — Ronde 13: eksekusi temuan 5.16 ✅ SELESAI (2026-08-25, test-first oleh koordinator; suite gabungan repo **1056/1056 hijau**, `go test ./internal/handlers/...` OK, `go build`+`go vet` OK)

> **Suite:** `uiux-batch19-hygiene.test.mjs` (4 test) — S117a file mati terhapus, S117b z-index toast tunggal (output.css bebas z-index; admin-base var(--z-toast) satu-satunya sumber), R146 noopener ×2, R147 noindex+paritas.
>
> **Kalibrasi ikutan penghapusan file (3 test lama):** (1) batch14 R126 — dari "baseline #f87171=0 pada file" menjadi "file TIDAK ADA lagi" (larangan artefak manual kembali ke web root); (2) batch2 T9 — entri kontras .form-hint admin-tailwind dihapus (output.css tetap terguard); (3) batch2 T10a — loop target tinggal output.css.
>
> **Verifikasi regressi:** seluruh suite node hijau; grep repo-wide nol referensi admin-tailwind tersisa; go test/build/vet bersih.

- [x] **S117** ✅ **Batch 19** — Hapus admin-tailwind.css (file mati); hapus z-index dari .toast-container output.css (lapisan dikunci admin-base); dokumentasi kebijakan whitelist generated
- [x] **R146** ✅ **Batch 19** — rel="noopener" pada 2 anchor target=_blank
- [x] **R147** ✅ **Batch 19** — register_confirm: meta robots noindex,nofollow (+description parity)
### Batch 18 — Ronde 12: eksekusi temuan 5.15 ✅ SELESAI (2026-08-25, test-first oleh koordinator; suite gabungan repo **1054/1054 hijau**, `go test ./internal/handlers/...` OK, `go build`+`go vet` OK)

> **Suite:** `uiux-batch18-guard.test.mjs` (7 test) — S116a token source-of-truth, S116b admin-base bersih literal + pemakaian positif, S116c banner publik → var(--z-dropdown), S116d guard folder-wide z-index ≥1000 (templates/** + CSS inti non-generated + JS aplikasi; tailwind generated exempt terdokumentasi), R144a/b reset aria-expanded dua jalur tutup row-dropdown, R145 toggleMenu focus-return.
>
> **Catatan teknis:** (1) pemetaan semantik S116 menyingkap latent bug stacking: `.toast-container` semula 10001 = TIE dengan `--z-onboarding`; naik ke `var(--z-toast)`=10002 sesuai intent terdokumentasi theme.css ("toasts one step above dialogs", onboarding DI BAWAH toast); (2) empat token baru theme.css: `--z-bottom-bar` 9997, `--z-hint` 9998, `--z-modal-overlay` 10000, `--z-topbar-floating` 10002; (3) folder `static/css/tailwind/` tetap exempt dari guard z-index (artefak build, preseden R126); admin-tailwind.css IKUT diwalk. 

- [x] **S116** ✅ **Batch 18** — Petakan 7 literal z-index admin-base ke --z-*; migrasi 9999 download/register; guard folder-wide larangan >=1000 (whitelist tailwind generated); kunci plafon
- [x] **R144** ✅ **Batch 18** — Reset aria-expanded owner .btn-more di jalur klik-luar & close-others row-dropdown
- [x] **R145** ✅ **Batch 18** — toggleMenu cabang tutup mengembalikan fokus ke burger (konsisten closeMenu)
### Batch 17 — Ronde 11: eksekusi temuan 5.14 ✅ SELESAI (2026-08-25, test-first: gerbang koordinator + agen settings; suite gabungan repo **1047/1047 hijau**, `go test ./internal/handlers/...` OK, `go build`+`go vet` OK)

> **Metode:** pelajaran proses diterapkan ketat — seluruh item dipetakan ke kepemilikan file sebelum penugasan, dan checklist diverifikasi silang ke diff commit.
>
> | Pelaksana | Kepemilikan | Suite | Item |
> |---|---|---|---|
> | batch17-settings (agen) | settings.html + settings-vouchers/packages/billing/general.js | `uiux-batch17-settings.test.mjs` (11 test) | S114, S115, R140, R141, R142 |
> | koordinator | pengawas_detail.html | `uiux-batch17-pengawasan.test.mjs` (5 test) | GERBANG S110†, S111†, R124†, R125-sisa† + R143 |
> | koordinator | admin.js, dashboard.html, submissions.html, theme.css | `uiux-batch17-admincore.test.mjs` (8 test) | S113, R133–R139 |
>
> (†) = item tertinggal Batch 16 yang tuntas di gerbang ini.
>
> **Catatan teknis:** (1) implementasi fingerprint S110 disimpan pada elemen paginasi (`pagEl.__lastHtml`) — versi awal memakai `window.*` yang melempar ReferenceError di sandbox vm dan memicu jalur rerun-pending (terdeteksi test S94 batch15: writes 4≠2); (2) R135 mengubah href jadi kondisional `{{if gt/lt …}}` — asersi R101 batch15-admincore dikalibrasi ke pola baru; (3) R137 menambah token source-of-truth `--color-info-light: #38bdf8` theme.css; (4) rekonsiliasi baseline akhir: folder-wide templates hex 107→99 & rgba 104→89 (=aktual), batch7 pengawas_detail rgba 11→7; (5) kalibrasi kontrak S107 batch16-settings (flag pindah ke onload + onerror reset/toast).

- [x] **Gerbang** ✅ **Batch 17** — S110+S111+R124+R125-sisa (4 item tertinggal/parsial Batch 16) di area pengawasan
- [x] **S113** ✅ **Batch 17** — Handler Escape capture-phase #pengawasDropdown (tutup dropdown + fokus header)
- [x] **S114** ✅ **Batch 17** — s.onerror fallback loader system-apps + reset flag + toast
- [x] **S115** ✅ **Batch 17** — voucher-code-badge jadi <button type="button">
- [x] **R133** ✅ **Batch 17** — Guard seq di catch openEditUserModal
- [x] **R134** ✅ **Batch 17** — Guard if (!btn || btn.disabled) return ala S27
- [x] **R135** ✅ **Batch 17** — Conditional omit href paginasi disabled (dashboard+submissions)
- [x] **R136** ✅ **Batch 17** — Hapus listener identity-popup kedua / beri guard sama
- [x] **R137** ✅ **Batch 17** — #c084fc & #38bdf8 kartu kuota → token accent/info-light
- [x] **R138** ✅ **Batch 17** — .btn-more aria-haspopup+aria-expanded+focus-return 3 jalur tutup
- [x] **R139** ✅ **Batch 17** — aria-current + label posisi halaman paginasi JS users
- [x] **R140** ✅ **Batch 17** — Hapus __settingsReady[packages] yatim
- [x] **R141** ✅ **Batch 17** — Disable tombol activatePackage selama fetch
- [x] **R142** ✅ **Batch 17** — Label toolbar general pola users + konsolidasi istilah voucher
- [x] **R143** ✅ **Batch 17** — escapeHtml(String(attempt_count)) & s.id di builder monitoring
### Batch 16 — Ronde 10: eksekusi temuan 5.13 ✅ SELESAI (2026-08-25, test-first via 4 agen paralel + gerbang koordinator; suite gabungan repo **1023/1023 hijau**, `go test ./internal/handlers/...` OK, `go build`+`go vet` OK)

> **Metode:** pelajaran proses ronde 10 diterapkan — checklist dipetakan eksplisit ke kepemilikan file agen (tak ada item yatim).
>
> | Agen | Kepemilikan | Suite | Item |
> |---|---|---|---|
> | batch16-settings | settings.html, settings-vouchers/packages/system-apps.js | `uiux-batch16-settings.test.mjs` (22 test) | S92†, R102†, R103†, R104†, R110†, S107, S108, R129 |
> | batch16-publik | template publik + CSS publik | `uiux-batch16-publik.test.mjs` (21 test) | T30, T31, R125(mobile), R127, R130, R131 |
> | batch16-guard | suite guard + komentar hasil.css | `uiux-batch16-guard.test.mjs` (3 test) | S112, R126, R128 + rekonsiliasi baseline |
> | koordinator | admin.js, dashboard.html, admin-base.css, theme.css | `uiux-batch16-admincore.test.mjs` (8 test) | S109, R119–R123, R132, R125(dashboard/admin-base) — agen admincore gagal jalan dua kali (laporan kosong/cancel), diambil alih |
>
> **Gerbang kritis:** T30/T31 diverifikasi MERAH pada asersi tepat sebelum implementasi; keputusan T30 terdokumentasi (one-time-code → kotak digit pertama visible tanpa maxlength; form autocomplete="off" DILEPAS di register_confirm, DIPERTAHANKAN di reset_password demi melindungi field password dari credential manager). T31 dieksekusi gabungan opsi A+B (gerdarkan media query burger + visibility:hidden touch-device :not(.open)).
>
> **Rekonsiliasi lintas-suite oleh koordinator:** batch4-auth S6a/S6b & batch15-publik S98 dikalibrasi ke kontrak T30; batch8 BASELINE_HEX auth pages 0→1 (+meta theme-color R130); batch7 dashboard rgba 32→29 & batch9 admin.js rgba 36→33 (migrasi token); batch15-guard public-mobile hex 10→9 (R125); batch10-settings R48 typo terkalibrasi; triplet baru `--rgb-text-muted` theme.css (#a0aec0); admin-tailwind.css #f87171 ×2 → var(--color-danger-bright), BASELINE_ADMIN_TAILWIND_F87171 2→**0**.
>
**KOREKSI pasca-Ronde 11** (sudah DITUNTASKAN di Batch 17): agen batch16-pengawasan dalam rencana TIDAK PERNAH diluncurkan sehingga S110/S111/R124 belum dieksekusi dan R125 baru parsial. Keempatnya menjadi gerbang pertama Batch 17 dan kini hijau.

(†) = item tertinggal Batch 15.

- [x] **Gerbang** ✅ **Batch 16** — S92+R102+R103+R104+R110 (5 item tertinggal Batch 15) + T30 regresi OTP + T31 drawer touch-device
- [x] **T30** ✅ **Batch 16** — one-time-code → kotak digit visible pertama tanpa maxlength=1; uji perangkat
- [x] **T31** ✅ **Batch 16** — Gerdarkan reset visibility/burger dengan (hover:hover)+(pointer:fine) atau visibility:hidden touch-device
- [x] **S107** ✅ **Batch 16** — Ekspor window.loadSectionScript ATAU hapus branch mati + __settingsLoaded + ?v=
- [x] **S108** ✅ **Batch 16** — Guard e.target.closest([data-action]) di toggle head users
- [x] **S109** ✅ **Batch 16** — editUserModalSeq + .catch toast
- [ ] **S110** ⚠️ TERTINGGAL — tidak pernah dieksekusi (agen batch16-pengawasan tak diluncurkan; terverifikasi ronde 11) — Fingerprint HTML paginasi submissions; skip bila identik
- [ ] **S111** ⚠️ TERTINGGAL — tidak pernah dieksekusi (terverifikasi ronde 11) — Fallback fokus tbody + announce saat baris hilang
- [x] **S112** ✅ **Batch 16** — Koreksi komentar hasil.css:941–948 + guard backstop !important layer publik
- [x] **R119** ✅ **Batch 16** — Pasang listener change sekali di createEditUserModal
- [x] **R120** ✅ **Batch 16** — aria-current nomor halaman dashboard
- [x] **R121** ✅ **Batch 16** — min-height/min-width ≥44px tombol baris dashboard di mobile
- [x] **R122** ✅ **Batch 16** — .catch + retry state loadSaasSettings
- [x] **R123** ✅ **Batch 16** — escapeHtml(p.role)
- [ ] **R124** ⚠️ TERTINGGAL — tidak pernah dieksekusi (terverifikasi ronde 11) — Satukan kontrak data-mac: escapeHtml polos semua atribut
- [x] **R125** ⚠️ PARSIAL **Batch 16** — 3/4 lokasi beres; sisa pengawas_detail.html:201,:576–580 → gerbang Batch 17 — Migrasi 5 lokasi rgba(107,114,128,…)/#9ca3af ke token + kunci plafon
- [x] **R126** ✅ **Batch 16** — Walk CSS inti non-generated di guard literal / dokumentasikan pengecualian tailwind
- [x] **R127** ✅ **Batch 16** — Blok print gradien shared/download atau dokumentasi risiko
- [x] **R128** ✅ **Batch 16** — Koreksi angka komentar kontras hasil.css ke nilai token aktual (#34d399=8.34, #f87171=6.19)
- [x] **R129** ✅ **Batch 16** — Hapus emoji opsi voucher; perbaiki h3>div uploadModalTitle
- [x] **R130** ✅ **Batch 16** — meta theme-color ×5 halaman auth
- [x] **R131** ✅ **Batch 16** — Konfirmasi intent guard android_app; ubah ke .system_apps atau beri komentar
- [x] **R132** ✅ **Batch 16** — Reset aria-expanded klik-luar; Escape + focus-return dropdown pengawas

### Batch 15 — Ronde 9: eksekusi temuan 5.12 ✅ SELESAI (2026-08-25, test-first: gerbang koordinator + 4 agen paralel; suite gabungan repo **966/966 hijau**, `go test ./internal/handlers/...` OK, `go build`+`go vet` OK)

> **Gerbang kritis (koordinator):** guard keseimbangan template Go (S93, `uiux-batch15-guard.test.mjs`) ditulis lebih dulu dan diverifikasi MERAH dengan **mereproduksi insiden** (wrapper ganda :779–780 → 0/2 pass) — baru T26/T27 diperbaiki (hapus wrapper, pulihkan `(or $isSuper $isOp)`), guard 2/2 + `TestSettingsPage` 5/5 hijau. Empat pemilik cakupan paralel tanpa tumpang tindih file:
>
> | Agen | Kepemilikan | Suite | Item |
> |---|---|---|---|
> | batch15-guard | file test/guard + 1 test Go | `uiux-batch15-guard.test.mjs` (+util `sliceBlock`) | T29, S93, S95, S99, S104, R105, R118 |
> | batch15-admincore | `admin.js`, `dashboard.html` | `uiux-batch15-admincore.test.mjs` | T28, S90, S91, S102, R100, R101 |
> | batch15-pengawasan | `pengawas*.html`, `submissions.html` | `uiux-batch15-pengawasan.test.mjs` | S89, S94, S106, R99, R111–R116 |
> | batch15-publik | `templates/public/**`, CSS publik+hasil+theme | `uiux-batch15-publik.test.mjs` | S96–S98, S101, S103, S105, R106–R109, R117 |
>
> **Koreksi kalibrasi saat eksekusi:** (1) R101 paginasi dashboard ternyata di **dashboard.html:554/:561**, bukan admin.js (atribusi awal meleset); (2) simbol sprite mati pengawas_detail = **11** (bukan 9 — `hi-download` & `hi-chevron-down` ikut); (3) `.log-entry.logout::before` pengawas.html TIDAK boleh dihapus — dijaga batch12 (dipertahankan + komentar); (4) BASELINES batch9 ternyata di `uiux-batch9-tokens-guard.test.mjs`; (5) meta-test regex z-index menguji kontrak ≥4 digit (`Z-INDEX: 9999`, bukan 999).
> **Bonus proses (pelajaran S93):** `TestVoucherAuditUIMarkupPresent` terbukti sudah GAGAL diam-diam di HEAD pra-Batch 15 (markup utuh; asersi masih meng-grep string pra-lazy-load) — asersi test diperbarui ke arsitektur lazy-load (`lazy('loadAuditLogs')` + fungsi di modul). Seluruh package `internal/handlers/admin` kini hijau penuh.
> **Ditunda:** **S100** (checksum SHA-256 distribusi APK) — butuh kolom skema DB + pipeline unggah: keputusan skema dahulu.
> **KOREKSI pasca-Ronde 10:** item **S92, R102, R103, R104, R110** ternyata luput dari pembagian tugas agen dan TIDAK pernah dieksekusi padahal sempat tercentang — status dikembalikan `[ ]` dan menjadi gerbang awal Batch 16.

- [x] **T26** ✅ **Batch 15** — Template settings.html gagal parse Go sejak Batch 12 (hapus wrapper :779; 5 test Go merah → hijau)
- [x] **T27** ✅ **Batch 15** — Pulihkan (or $isSuper $isOp) pada kondisi dalam :780
- [x] **T28** ✅ **Batch 15** — Hapus setAttribute onclick divider soal admin.js:757–760 (delegasi saja)
- [x] **T29** ✅ **Batch 15** — Guard vakum R91: marker detail-identity-items diganti anchor eksis + asersi positif
- [x] **S89** ✅ **Batch 15** — Normalisasi data-end Durasi submissions (atau server kirim duration)
- [x] **S90** ✅ **Batch 15** — Dropdown pengawas keyboard-operable (button/aria-expanded/chip button)
- [x] **S91** ✅ **Batch 15** — submissionDetailSeq + guard then/catch
- [ ] **S92** ⚠️ TERTINGGAL — ternyata TIDAK pernah dieksekusi (luput penugasan agen; terverifikasi ronde 10: tanpa seq-token) — redemptionSeq (+ harden loadApps/loadPackages)
- [x] **S93** ✅ **Batch 15** — Guard keseimbangan {{if}}/{{end}} per template + go test di gerbang skrip test
- [x] **S94** ✅ **Batch 15** — Diff-based render tabel submissions monitoring (skip bila snapshot identik)
- [x] **S95** ✅ **Batch 15** — Kunci ulang baseline batch7/B8 ke aktual (assert.equal)
- [x] **S96** ✅ **Batch 15** — Reset -webkit-text-fill-color di @media print hasil.css
- [x] **S97** ✅ **Batch 15** — visibility/inert drawer nav tertutup + fokus kembali ke hamburger
- [x] **S98** ✅ **Batch 15** — autocomplete one-time-code pindah dari kotak digit maxlength=1
- [x] **S99** ✅ **Batch 15** — public-desktop/mobile.css + pengawas-detail.js/device-fingerprint.js masuk guard, baseline aktual
- [ ] **S100** ⏳ DITUNDA (butuh keputusan skema DB + pipeline unggah) — Field sha256 system app + tampil di halaman unduh
- [x] **S101** ✅ **Batch 15** — Migrasi teks badge/chip skor ke varian light/bright + guard kontras statik
- [x] **S102** ✅ **Batch 15** — delegateSeq + guard then/catch openDelegateExamModal
- [x] **S103** ✅ **Batch 15** — Adopt-or-delete token mati theme.css (--color-*-bg, --shadow-*)
- [x] **S104** ✅ **Batch 15** — Util sliceBlock throw-bila-marker-tak-ada; audit semua slice guard
- [x] **S105** ✅ **Batch 15** — Ganti admin-core.js reset_password dengan helper lokal wirePwToggle
- [x] **S106** ✅ **Batch 15** — Rule fokus #autoAcceptToggle:focus-visible + .pd-toggle-slider
- [x] **R99** ✅ **Batch 15** — Detail submissions min-height 44px sejajar Hapus
- [x] **R100** ✅ **Batch 15** — Hapus onfocus/onblur/onsubmit inline di generator admin.js
- [x] **R101** ✅ **Batch 15** — aria-disabled pagination dashboard boundary
- [ ] **R102** ⚠️ TERTINGGAL — tidak pernah dieksekusi (terverifikasi ronde 10) — Generate Batch→Buat Massal dkk + typo Kesini
- [ ] **R103** ⚠️ TERTINGGAL — tidak pernah dieksekusi (terverifikasi ronde 10) — scope="col" + caption sr-only ×3 tabel settings
- [ ] **R104** ⚠️ TERTINGGAL — tidak pernah dieksekusi (terverifikasi ronde 10) — escapeHtml(durationText)
- [x] **R105** ✅ **Batch 15** — Flag /i pada regex guard + test negatif kapital
- [x] **R106** ✅ **Batch 15** — Touch target hasil.css → 44px + guard
- [x] **R107** ✅ **Batch 15** — Resend OTP sukses: kosongkan digit + fokus digit-1
- [x] **R108** ✅ **Batch 15** — Hapus link CSS dobel download.html:5–6
- [x] **R109** ✅ **Batch 15** — Copy "berlaku 15 menit" pada OTP
- [ ] **R110** ⚠️ TERTINGGAL — tidak pernah dieksekusi (terverifikasi ronde 10) — aria-current/aria-label paginasi vouchers
- [x] **R111** ✅ **Batch 15** — #6b7280 → var(--color-text-muted) progres 0%
- [x] **R112** ✅ **Batch 15** — announceQueueCount di jalur notice/gagal
- [x] **R113** ✅ **Batch 15** — data-subs atribut → variabel modul
- [x] **R114** ✅ **Batch 15** — Bersihkan dead CSS/sprite pengawasan
- [x] **R115** ✅ **Batch 15** — Kartu ujian nested interactive: aria-label + stopPropagation
- [x] **R116** ✅ **Batch 15** — Label "Nilai / Score"→"Nilai"; SUBS_PER_PAGE konstanta
- [x] **R117** ✅ **Batch 15** — animation-iteration-count cap reduced-motion hasil.css
- [x] **R118** ✅ **Batch 15** — FILES B8 + cek_hasil/index/forgot_password baseline 0

### Batch 14 — Ronde 8: eksekusi temuan 5.11 ✅ SELESAI (2026-08-24, test-first: kontrak ditulis lebih dulu sebagai 5 suite `uiux-batch14-*.test.mjs` — diverifikasi MERAH 54/61 gagal pada kontrak yang tepat, baru implementasi sampai hijau; suite gabungan repo **836/836 hijau**, `go build`+`go vet` OK)

> **Metode:** sama dengan Batch 6–13 — kontrak perilaku ditulis lebih dulu sebagai file
> `uiux-batch14-*.test.mjs` (statik fs-read + perilaku via `vm.runInNewContext`), diverifikasi
> MERAH dulu, baru implementasi sampai HIJAU. Lima pemilik cakupan paralel tanpa tumpang tindih file:
>
> | Agen | Kepemilikan file | Suite | Test |
> |---|---|---|---|
> | batch14-core | `admin-core.js`, `pengawas-detail.js`, `pengawas_detail.html` | `uiux-batch14-core.test.mjs` | 9 |
> | batch14-tokens-guard | guard folder-wide: templates/** + JS statis, CAPS/BASELINES suite lama | `uiux-batch14-tokens-guard.test.mjs` | 16 |
> | batch14-publik | `templates/public/**`, `hasil.css`, FILES guard B8 | `uiux-batch14-publik.test.mjs` | 13 |
> | batch14-settings | `settings.html`, `settings-vouchers/voucher-audit/users/billing.js`, loader users di `admin.js` | `uiux-batch14-settings.test.mjs` | 17 |
> | batch14-submissions-nav | `submissions.html`, `dashboard.html`, sweep sr-only folder-wide | `uiux-batch14-submissions-nav.test.mjs` | 6 |
>
> **Kontrak lintas-agen:** (1) literal `#f87171` & `#818cf8` dinaikkan FOLDER-WIDE (templates/** +
> JS statis) — menutup S80/S81 sekaligus; (2) FILES+baseline batch8-publik diperluas ke
> register_confirm/reset_password/hasil.html (S82/S83) dan CAPS `!important` ditambah
> `css/theme.css`: aktual (R90); (3) BASELINES batch9 ditambah settings-system-apps.js rgba=2 +
> entri eksplisit users/general/packages/admin-core (S88); (4) perbaikan core T24+S76 satu agen
> (sama-sama menyentuh admin-core.js).
>
> **Koreksi kalibrasi saat penulisan test (verifikasi manual):**
> - R88: literal `z-index:10002` ternyata di **settings.html:741** (`upload-progress-pill`),
>   bukan dashboard.html seperti atribusi awal ronde 8 — nilai identik dengan `--z-toast`;
>   dashboard tetap menyumbang `.pengawas-popup{z-index:100}` (:19) dan admin.js dropdown
>   `z-index:100` (:1053). Test dikalibrasi ke lokasi benar.
> - S79: fallback `<label class="switch">` TIDAK sah sebagai nama aksesibel karena label itu
>   tak ber-teks (hanya membungkus slider) — test menuntut aria-labelledby ke id eksis ATAU
>   input di dalam `<label>` ber-teks nyata (pola kartu Monetisasi :1889).
> - S86: hitung byte NUL via Buffer (bukan grep teks) = 1 byte, sejak Batch 3.

**Catatan eksekusi:** (1) cap folder-wide batch7-tokens diturunkan ke aktual baru
(hex 132→107, rgba 145→104) akibat migrasi S80/S81/S83 — kontrak R70 "plafon = aktual"
dipertahankan; (2) pengisi ukuran APK download.html disatukan ke listener DOMContentLoaded
skrip utama (blok skrip terpisah membuat harness T19 salah ekstraksi inline-script terakhir);
(3) if Go `{{ if and (not $locked) … }}` ternyata BERSARANG redundan (bukan sekuensial) —
yang dihapus adalah if-dalam beserta `{{ end }}` pasangannya sehingga users dibungkus sekali;
(4) R97 blok komentar EN besar ditunda (usaha dokumen-lintas-file, bukan UX runtime).

- [x] **T24** ✅ **Batch 14** — core href-aware + Detail-link jadi <button> (styling parity)
- [x] **T25** ✅ **Batch 14** — rule tr[role="button"]:focus-visible outline primary-light
- [x] **S76** ✅ **Batch 14** — overlay.querySelector + escapeHtml(confirmLabel/cancelLabel); test vm dua-dialog independen
- [x] **S77** ✅ **Batch 14** — kedua renderError lewat escapeHtml(msg)
- [x] **S78** ✅ **Batch 14** — voucherLoadSeq/auditLoadSeq/usersListSeq/myPackagesSeq + guard then/catch (test statik 4 loader)
- [x] **S79** ✅ **Batch 14** — id label + aria-labelledby ×3 (emailEnabled/Turnstile/seoIndex)
- [x] **S80** ✅ **Batch 14** — folder-wide templates/**+JS = 0 #f87171
- [x] **S81** ✅ **Batch 14** — folder-wide = 0 #818cf8; guard batch13 R82 kini hitungan global
- [x] **S82** ✅ **Batch 14** — migrasi token penuh (7 hex+19 rgba; 2 hex+8 rgba → 0/0) + FILES B8 diperluas, baseline 0
- [x] **S83** ✅ **Batch 14** — hasil.html nol literal intra-blok (termasuk fallback var(…,#hex)) + FILES B8
- [x] **S84** ✅ **Batch 14** — download.html data-size-bytes + pengisi textContent di skrip utama (T19-safe); settings.html data-app-size-bytes
- [x] **S85** ✅ **Batch 14** — lastUpdatedLabel visual-only + #queueLiveRegion role=status announce perubahan count
- [x] **S86** ✅ **Batch 14** — pemisah fingerprint NUL → '|'; guard Buffer bebas 0x00
- [x] **S87** ✅ **Batch 14** — Siswa→Nama, Perangkat→ID Perangkat (+3 selector CSS ikut), Ujian→Nama Ujian; test paritas th↔label & selector tak yatim
- [x] **S88** ✅ **Batch 14** — BASELINES batch9: system-apps rgba=2 + entri eksplisit users/general/packages/admin-core=0
- [x] **R88** ✅ **Batch 14** — popup dashboard/dropdown admin.js → var(--z-dropdown), pill settings → var(--z-toast); guard larangan ≥1000
- [x] **R89** ✅ **Batch 14** — 6 inline-style arwah dihapus folder-wide + penjaga utility .sr-only ada
- [x] **R90** ✅ **Batch 14** — CAPS batch11 + 'css/theme.css': 1 (=aktual)
- [x] **R91** ✅ **Batch 14** — statusText/startTimeStr/endTimeStr lewat escapeHtml(String(...))
- [x] **R92** ✅ **Batch 14** — baris setAttribute data-name dihapus
- [x] **R93** ✅ **Batch 14** — syncOtpHidden men-disable submit saat code.length<6 + sinkron awal
- [x] **R94** ✅ **Batch 14** — frasa posisional diganti "(Cloudflare Turnstile)" ×3 halaman
- [x] **R95** ✅ **Batch 14** — 3× #fca5a5 → danger-light; cap hex vouchers 22→9 (=aktual)
- [x] **R96** ✅ **Batch 14** — paginasi vouchers/audit min-height:40px padding 8px 14px; Lihat User 44px
- [x] **R97** ✅ **Batch 14** — 'Membuat voucher...' (blok komentar EN menyusul saat file disentuh)
- [x] **R98** ✅ **Batch 14** — if Go redundan + {{ end }} pasangan dihapus; wiring unggah satu jalur; window.toggleUsersCollapse dihapus

### Batch 13 — Ronde 7: eksekusi temuan 5.10 ✅ SELESAI (2026-08-24, test-first via 3 agen paralel (1 terputus → dituntaskan koordinator); suite gabungan repo **705/705 hijau**, `go build`+`go vet` OK)

> Kontrak lintas-agen: (1) token baru `--color-primary-bright: #818cf8` didefinisikan theme.css
> (agen publik) — seluruh pemakaian #818cf8 di template bermigrasi ke var(); (2) plafon folder-wide
> hex dikunci ulang 150→132 oleh agen settings-guard (=aktual pasca migrasi); (3) pesan konfirmasi
> voucher PLAIN TEXT — escape ditangani showConfirm core (kontrak T23).

- [x] **T23** regresi tampilan dialog voucher dibereskan: kedua pemanggil kirim plain text
  (`'... kode voucher ' + code + '? ...'`) — core meng-escape seluruh pesan; tag literal tidak
  lagi tampil; kontrak S3b batch3 direvisi ke intent baru (markup manual justru dilarang).
- [x] **S70** label "Hapus" & badge "Nonaktif" vouchers → var(--color-danger-light) (7.37:1 di
  tint yang sama); asersi vouchers bebas `color:#ef4444`.
- [x] **S71** CAPS `!important` dikunci ke aktual (admin-base 47, hasil 65, public-desktop 21,
  public-mobile 48 — slack 33 ditutup); entri duplikat `'admin/settings.html': 110` dihapus dari
  batch7-tokens (satu-baseline-satu-metrik).
- [x] **S72** `v.package` & `v.notes` di-escapeHtml di render tabel voucher.
- [x] **S73 (PARSIAL)** tampilan timestamp voucher disatukan via formatDateTimeID (:75,:407);
  perbandingan expired dari waktu server DITUNDA (butuh API — komentar penunjuk dipasang).
- [x] **S74** pencarian 0 hasil menyembunyikan paginationWrapper + statsRow (tak ada lagi kontrol
  basi "1–20 dari 57" bersama kartu "Tidak ditemukan"); test vm termasuk pemulihan.
- [x] **S75** BAR strength meter migrasi token (bar = warna label): .good var(--color-success),
  .strong gradient success→accent-cyan; asersi blok style bebas hex ×2 halaman.
- [x] **R77** dead code ekor T22 dihapus (__btnConfirmAction/pendingConfirmCallback); asersi
  diperluas ke regex confirmActionModal|pendingConfirmCallback|btnConfirmActionSubmit pada JS.
- [x] **R78** padding inline pada ketiga string render .pd-action-btn dihapus — fix R76 kini
  benar-benar efektif; asersi render antrean bebas padding inline.
- [x] **R79** urutan CSS diselesaikan folder-wide: forgot_password/register_confirm/reset_password/
  shared.html ikut memindah link sebelum style pertama; asersi R71 ditingkatkan menjadi
  folder-wide (9 template publik).
- [x] **R80** lompatan h1→h3 panel Kunci Jawaban → h2 (visual via style existing) + assertion
  urutan heading untuk hasil.html.
- [x] **R81** chevron afordansi baris pengawasan → var(--color-primary-light) (8.19:1 ≥ ambang
  non-teks 3:1); guard color:#4f46e5 pada pengawas.html.
- [x] **R82** token --color-primary-bright (#818cf8) + migrasi ×11 (download/shared/
  register_confirm/register/settings/pengawas_detail); cap hex folder turun 150→132.
- [x] **R83** duplikasi OTP register↔reset: asersi hash-normalized "kedua blok identik" ditambahkan
  agar drift terdeteksi (ekstraksi partial tetap gabung S57/reformasi harness).
- [x] **R84** skip-link login admin memakai class .skip-link theme.css (inline arwah + z literal
  dihapus).
- [x] **R85** triplet autocapitalize/autocorrect/spellcheck pada username login admin (pola R36).
- [x] **R86** #resendMsg OTP ber-role="status" aria-live="polite".
- [x] **R87** label /login dikanonikkan: nav "Masuk", CTA landing "Masuk ke Panel Admin".

### Batch 12 — Ronde 6: eksekusi temuan 5.9 ✅ SELESAI (2026-08-24, test-first via 3 agen paralel (1 terputus → dituntaskan koordinator); suite gabungan repo **666/666 hijau**, `go build`+`go vet` OK)

> Kontrak lintas-agen: (1) token baru `--color-danger-bright: #f87171` didefinisikan theme.css
> (agen publik) — seluruh pemakaian #f87171 di template bermigrasi ke var(); (2) plafon guard
> folder-wide dikunci ke baseline aktual oleh agen settings-guard; (3) dashboard.html (#f87171 ×3)
> ditangani koordinator saat integrasi.

- [x] **T22** sistem konfirmasi ketiga DIHAPUS dari vouchers: showConfirmModal +
  closeConfirmActionModal + modal arwah confirmActionModal (markup settings.html) dihapus; kedua
  pemanggil (toggle/delete voucher) memakai showConfirm core dengan label eksplisit ("Ya,
  Matikan/Aktifkan Voucher", "Hapus Voucher"); gradien terlarang #a855f7/#6366f1 hilang dari JS —
  link warna aksen → var(--color-accent-light) (kontras naik); registrasi arwah confirm-action-close
  dihapus. Whitelist larangan endpoint kini mencakup JS render-path.
- [x] **S65** ketiga jalur gagal fetch halaman hasil memulihkan display kontainer error sebelum
  menulis innerHTML — tombol "Coba Lagi" kini terlihat pada gagal paginasi/pencarian berikutnya.
- [x] **S66** suffix cache-busting manual di login.html (`-5`/`-3`) dihapus; asersi anti-suffix
  diperluas ke cakupan pengawasan-nav (klaim R60 kini benar-benar penuh).
- [x] **S67** outline heading settings dibereskan: h1 sr-only kanonik "Pengaturan" di awal main;
  "Aplikasi Sistem" turun h2; assertion urutan heading baru.
- [x] **S68** migrasi literal: #f87171 ×24 → var(--color-danger-bright) (publik ×10, admin ×14);
  hasil.css rgba(99,102,241,α) ×18 → rgba(var(--rgb-info), α) — visual nol perubahan.
- [x] **S69 (PARSIAL)** `aria-live="polite"` pada stempel "Diperbarui" ✅; konversi timestamp-server
  DITUNDA (API belum menyertakan waktu respons — didokumentasikan di kode; format HH:MM:SS
  dipertahankan karena detik bermakna untuk polling).
- [x] **R67** clear-search mengembalikan fokus ke input pencarian (tak lagi jatuh ke body).
- [x] **R68** strength meter good/strong → token (--color-success/--color-accent-cyan) register &
  reset_password + asersi blok bebas hex (klaim Batch 10 yang tidak akurat kini benar-benar dijaga).
- [x] **R69** badge "(Stable)" hard-coded dihapus (server tak kirim data stabilitas); hero landing →
  "Ujian Digital Teraman & Siap Offline" (keluarga S21/S22).
- [x] **R70** plafon guard folder-wide hex 300→150, rgba 225→145 (=aktual); baseline JS ditambah
  settings-system-apps.js & pengawas-detail.js; entri basi settings.html dirapikan.
- [x] **R71** semua link stylesheet eksternal dipindah sebelum blok <style> inline pertama
  (hasil/download/register/cek_hasil) + asersi per-halaman.
- [x] **R72** asterisk required #f43f5e ×4 → var(--color-danger-light).
- [x] **R73** info paginasi daftar ujian pengawasan memakai rentang (pola R56).
- [x] **R74** dead branch countdown rotasi diperbaiki — pesan "Rotasi otomatis belum aktif" dapat tampil.
- [x] **R75** toast gagal antrean izin de-dup once-pattern (reset saat sukses) — 3 gagal = 1 toast.
- [x] **R76** label Izinkan/Tolak ≤480px naik 0.75rem (hemat via padding).
- Kontrak test lama direvisi minimal dengan intent proteksi dipertahankan: batch4-modal R13
  (h1 tunggal kini sr-only kanonik), batch5-admin-core S25 (confirmActionModal keluar daftar,
  minimum modal-close 6→5), batch7-settings (entri modal arwah keluar), batch6-jscore (test
  delegasi closeConfirmActionModal dihapus bersama fungsinya), batch8-actions B8-1 (registrasi
  arwah dilarang kembali).

### Batch 11 — Ronde 5: eksekusi temuan 5.8 ✅ SELESAI (2026-08-24, test-first via 4 agen paralel (2 terputus → dikerjakan/dituntaskan koordinator); suite gabungan repo **631/631 hijau**, `go build`+`go vet` OK)

> Kontrak lintas-agen: (1) seluruh `templates/**/*.html` wajib **0 handler inline** (`\son[a-z]+=`)
> — dikunci guard folder-wide baru; (2) plafon token di-rebalance ke angka aktual (pengurangan
> oleh agen lain aman); (3) R59 (fallback tz main.go) dikerjakan koordinator langsung.

- [x] **T19** regresi fungsional T19 dibereskan: blok registrasi `Actions` hasil.html & download.html
  dipindah ke dalam DOMContentLoaded (deferred core terekseksi sebelum event itu) — tab Nilai/Kunci,
  paginasi, clear-search, "Coba Lagi", dan tab platform unduhan hidup kembali untuk klik mouse;
  test vm mengeksekusi skrip dalam urutan nyata (inline → core → DOMContentLoaded) dan mengunci
  `Actions.has('switch-tab'/'download-app')`.
- [x] **T20** eksekusi ulang R51 yang false-positive: heading order download.html (3 flavor-title
  h4→h3) & submissions.html (4 section-title h4→h3, visual via style existing + selector disesuaikan);
  assertion urutan heading ditambahkan di kedua suite.
- [x] **T21** seluruh 36 handler inline non-onclick dihabiskan: settings ×20, dashboard ×13
  (form submit listener, delegasi change select-all/token-mode/statusFilter, wiring file-input &
  color-picker), nav ×1, hasil oninput ×1, + submissions onchange ×1 (ekstra kontrak) —
  **guard folder-wide `\son[a-z]+=` = 0 di templates/** kini hijau**.
- [x] **S60** live-search & filter peserta me-reset halaman: callback → `loadDetail(1)` /
  `loadPengawasExams(1)` — cari dari page >1 tak lagi menghasilkan kosong palsu (test vm page=3).
- [x] **S61** sub-item S49 tuntas: `expires_at` popup kuota user diformat `formatDateTimeID`
  (+ asersi render popup bebas interpolasi mentah).
- [x] **S62** onclick render-JS settings-billing.js (:134 activatePackage, :158 retry) & 
  settings-voucher-audit.js (:9,:86 paginasi) bermigrasi ke data-action + registrasi modul pemilik;
  sweep bebas onclick untuk modul settings ditambahkan.
- [x] **S63** hex `#fff` ×66 settings.html dimigrasi kontekstual ke token semantik
  (--color-text/--color-text-on-primary/rgba(var(--rgb-white))) — visual dijaga per-baris.
- [x] **S64** plafon guard di-rebalance: cap per-file = angka aktual (settings rgba/hex, file-file
  tepat-plafon), dan count `!important` per CSS file dikunci sebagai plafon (pertama kali).
- [x] **R54** konfirmasi auto-approve: label eksplisit `'Ya, Aktifkan'` (bukan default merah
  "Ya, Hapus").
- [x] **R55** kalkulator durasi ganda: blok admin.js ("Xj Ym") dihapus — formatter verbose
  submissions.html satu-satunya sumber.
- [x] **R56** info paginasi peserta monitoring memakai rentang: "Menampilkan 41–57 dari 57
  perangkat" (test vm page 2/3).
- [x] **R57** shadowing `localizeUTC` lokal pengawas_detail dihapus — format waktu seragam satu-pintu.
- [x] **R58** countdown kedaluwarsa akun: <1 jam "± N menit lagi" (danger), <24 jam "Kedaluwarsa
  hari ini", sisanya floor hari — Math.ceil dihapus (test vm 10 menit/5 jam/2 hari/lewat).
- [x] **R59** fallback zona seragam: main.go formatExamTime memakai FixedZone("WIB") bila tzdata
  hilang — selaras submissions.go & hasil.go (dikerjakan koordinator).
- [x] **R60** suffix cache-busting manual `-N` dihapus semua (head.html + template publik) —
  `?v={{.version}}` tunggal.
- [x] **R61** dua `!important` print hasil.css dihapus (urutan file cukup) — count turun 65→63,
  kini terkunci plafon guard S64.
- [x] **R62** label grup OTP ber-asosiasi programatik (for ke kotak pertama) di
  register_confirm & reset_password.
- [x] **R63** state error 404 hasil dapat CTA "Kembali ke Beranda" + "Coba Token Lain".
- [x] **R64** strength meter terhubung programatik: aria-describedby + aria-live="polite"
  (register & reset_password).
- [x] **R65** hint username menyebut huruf kecil wajib.
- [x] **R66** waktu halaman hasil publik dikirim terformat WIB dari server (hasil.go helper
  jakartaLoc fallback FixedZone — pilihan A) — konsisten dengan kartu guru.
- Kontrak test lama direvisi minimal dengan intent proteksi dipertahankan: batch9-publik R37c
  (jendela regex diperlebar atas blok komentar T19), batch10-pengawasan-nav/batch3-settings-nav/
  batch7 (anchor loadDetail(1), localizeUTC keluar daftar ekstraksi).

### Batch 10 — Ronde 4: eksekusi temuan 5.7 ✅ SELESAI (2026-08-24, test-first via 5 agen paralel (2 agen terputus → dikerjakan koordinator); suite gabungan repo **577/577 hijau**, `go build`+`go vet` OK)

> Kontrak lintas-agen: (1) token `--grad-btn-violet/blue-start/end` & `--z-onboarding` dipindah/
> didefinisikan di theme.css sebagai satu sumber (nilai AA terkunci test); (2) pembersihan dirty
> kartu settings kini dari jalur sukses `saveSaasSection(cardId)` — observer toast satu-slot dihapus;
> (3) guard onclick diperluas: templates/public/** wajib 0 inline handler.

- [x] **T17** regresi deep-link `#kunci`: `loadResults()` kini menutup dengan
  `switchTab(currentTab, { skipHash: true })` — panel Nilai tidak lagi bertumpuk dengan Kunci saat
  halaman dibuka via hash; diuji vm.
- [x] **T18** gradien submit instansi gagal-AA dibereskan di nav.html (onboarding) & dashboard
  (edit instansi): `var(--grad-btn-violet-start/end)` dari theme.css (endpoint 5.38/5.70:1);
  whitelist larangan `#a855f7|#6366f1` dikunci test untuk admin + publik.
- [x] **S47** pembersihan dirty kartu settings pindah ke cabang sukses `saveSaasSection`
  (argumen cardId, guard typeof untuk halaman tanpa modul general); `SAAS_PENDING_SAVE` +
  observer toast MutationObserver DIHAPUS dari settings-general.js — tak ada lagi salah-bersih
  lintas kartu/fitur. Kontrak batch9-settings direvisi ke mekanisme baru (intent anti-drift utuh).
- [x] **S48** pencarian peserta monitoring kini live-search (`initLiveSearch`, debounce core);
  select filter lewat listener change — Enter-only & onchange inline dihapus.
- [x] **S49** kartu info ujian Hasil Ujian tampil WIB: helper `formatExamTimeWIB`/
  `formatCreatedTimeWIB` di submissions.go (LoadLocation + fallback FixedZone), selaras
  formatExamTime main.go; `expires_at` popup kuota diformat; kelas mati `.utc-date` dihapus.
- [x] **S50** modal Izinkan/Tolak menyebut identitas: helper `formatApprovalStudentLabel(mac)`
  → "**Budi** (AA:BB…)", fallback "(Anonim)"; nama dari cache antrean yang sama dengan toast R31.
- [x] **S51** sisa ±11 onclick render-path users/modal dinamis admin.js dimigrasi ke data-action;
  asersi sweep "string render users bebas onclick=" ditambahkan (bukan lagi per-snippet).
- [x] **S52** toggle auto-approve wajib `showConfirm` saat meng-AKTIFKAN ("semua perangkat
  berikutnya disetujui tanpa pemeriksaan hingga dimatikan"), batal mengembalikan posisi switch;
  mematikan tetap langsung.
- [x] **S53** init tab hasil dibungkus guard `!isDisabled && !pageHasError` — tanpa TypeError di
  state error/disabled; listener hashchange ikut dalam guard.
- [x] **S54** tab Nilai/Kunci hasil mendapat semantik ARIA tabs lengkap (tablist/tab/tabpanel,
  aria-selected, roving tabindex + panah/Home/End) — port pola download S14.
- [x] **S55** blok CSS toast lokal download mendapat rule `.toast-close:focus-visible` (fix T10a
  kini berlaku juga di halaman unduhan).
- [x] **S56** admin-core.js di reset_password dimuat `defer` — disiplin S42 kini seragam.
- [x] **S58** migrasi rgba literal pasangan-token: settings.html white ×43 + black ×38 →
  rgba(var(--rgb-white/black)) (109→28 literal); admin-base.css triplet brand info/danger/success
  (+primary/accent/warning) → rgba(var(--rgb-*)); plafon guard diturunkan sesuai angka baru.
- [x] **S59** templates/public/** kini **0 onclick inline** (hasil/download via Actions registry;
  shared/register_confirm via addEventListener lokal); guard "0 onclick" diperluas ke folder publik.
- [ ] **S57 DITUNDA** — ekstraksi blok inline pengawas_detail (style 825 + script 1022 baris)
  mematahkan kontrak fs-read statik banyak suite; kerjakan bersama reformasi harness per-file.
- [x] **R42** strength meter reset_password bermigrasi ke token (--color-danger/warning) — paritas
  register↔reset pulih; asersi kedua blok bebas hex.
- [x] **R43** print style hasil me-reset `.answer-grid { max-height:none }` — rekap cetak lengkap.
- [x] **R44** race device_fingerprint: submit dengan field kosong & generate() in-flight ditahan
  via Promise.race timeout ±1,5 dtk lalu lanjut apa adanya — coverage fingerprint naik tanpa
  mengubah UX.
- [x] **R45** toast fetch mulai/hentikan pengawasan → pesan statis "Periksa koneksi." (err ke console).
- [x] **R46** title badge tombstoned lolos escapeHtml(jsEscape()) + timestamp via formatDateTimeID.
- [x] **R47** Ekspor Excel toleran body non-JSON (catch parse → fallback pesan ramah).
- [x] **R48** race tutup→buka uploadModal: handle setTimeout disimpan & dibatalkan openUploadModal.
- [x] **R49** kedua error state daftar pengawasan dapat tombol "Coba Lagi" (data-action reload).
- [x] **R50** sisa EN habis: "Refresh"→"Muat Ulang" (settings ×2, pengawas_detail ×2),
  "Export XML"→"Ekspor XML".
- [x] **R51** heading order h2→h4 diperbaiki (download flavor-title, submissions section-title → h3,
  visual via class — pola R19); assertion urutan heading ditambahkan.
- [x] **R52** nav.html: z-index onboarding 99999 → var(--z-onboarding)=10001 (di bawah toast);
  skip-link memakai class `.skip-link` theme.css (inline style dihapus).
- [x] **R53** `#64748b` terakhir (ikon riwayat perangkat pengawas_detail) → var(--color-text-muted);
  grep templates/ = 0.
- Kontrak test lama direvisi minimal dengan intent proteksi dipertahankan: batch9-settings (mekanisme
  S47 baru), batch6-publik-css (nilai --z-onboarding 10001 + alasan), batch1 S13 (anchor unduh kini
  data-action + delegasi meneruskan elemen), batch5-publik T12 (regex guard init toleran komentar S54),
  batch7-pengawasan (helper S50 ikut dimuat sandbox + global approvalRowsCache).

### Batch 9 — Ronde 3: eksekusi temuan 5.6 ✅ SELESAI (2026-08-24, test-first via 5 agen paralel dengan kepemilikan file terpisah; suite gabungan repo **507/507 hijau**, `go build`+`go vet` OK)

> **Metode:** sama dengan Batch 6–8 — kontrak ditulis lebih dulu sebagai file `uiux-batch9-*.test.mjs`
> (diverifikasi merah → implementasi → hijau). Lima pemilik cakupan paralel tanpa tumpang tindih file:
>
> | Agen | Kepemilikan file | Suite |
> |---|---|---|
> | batch-9-jscore | `admin-core.js`, `admin.js`, `dashboard.html` | `uiux-batch9-jscore.test.mjs` — 21 |
> | batch-9-pengawasan-nav | `partials/nav.html`, `pengawas*.html`, `submissions.html` | `uiux-batch9-pengawasan-nav.test.mjs` — 15 |
> | batch-9-settings | `settings.html` + seluruh `settings-*.js` | `uiux-batch9-settings.test.mjs` — 13 |
> | batch-9-publik | `templates/public/**` (+CSS publik bila perlu) | `uiux-batch9-publik.test.mjs` — 29 |
> | batch-9-tokens-guard | `uiux-batch7-tokens.test.mjs` + guard JS baru | `uiux-batch9-tokens-guard.test.mjs` |
>
> Kontrak lintas-agen yang dipatuhi: (1) `#toastContainer` pindah ke `partials/nav.html` sebagai
> satu-satunya sumber — container per-halaman dihapus dari semua halaman admin; (2) ambang warna
> nilai submissions disamakan ke semantik publik 70/40 tanpa kode bersama baru; (3) `copyCode`
> core menjadi satu-satunya implementasi salin (`navigator.clipboard` = 0 di inline template).

- [x] **T14** container toast kini dari partials/nav.html:98 (aria-live/aria-atomic); duplikat dihapus
  dari dashboard/pengawas_detail/submissions/settings (jadi komentar penunjuk); login.html tak memuat
  partial sehingga tak terdampak. Test menjamin halaman admin tak lagi punya container sendiri.
- [x] **T15** guard penggantian soal: helper `replaceEditorQuestions()` (admin.js) — bila editor berisi
  soal/kotor → `showConfirm("Ganti semua soal di editor? …")` sebelum render; pasca render selalu
  `markQuestionsConfigDirty()`. Berlaku untuk quickGenerate & importXML. Hapus field identitas
  migrasi ke `data-action="identity-field-remove"` + penandaan kotor.
- [x] **T16** token gradien tombol unduh baru di :root shared.html (`--grad-btn-violet-*`,
  `--grad-btn-blue-*`) dengan endpoint lolos AA terverifikasi hitung di test (#9333ea=5.38 ·
  #7c3aed=5.70 · #2563eb=5.17 · #1d4ed8=6.70); endpoint lama dilarang whitelist.
- [x] **S37** uploadModal migrasi `.modal-backdrop` → `.modal-overlay`; guard capture
  `wireUploadCloseGuard` menahan Escape/klik-overlay selama `__uploadInProgress` + pill progres
  "Mengunggah..." (aria-busy) — unggahan tak bisa tertutup diam-diam lagi.
- [x] **S38** ambang warna submissions.html diganti 70/40 (dari 80/60) — konsisten dengan chip
  Lulus/Belum Lulus halaman siswa; komentar kontrak anti-drift.
- [x] **S39** dirty tracking per kartu Pengaturan Umum: listener input/change capture per kartu → titik
  `.saas-dirty-dot` di header + label tombol bertanda "•"; klik simpan mencatat pending card dan
  MutationObserver toast sukses membersihkan dirty; guard `beforeunload` saat ada kartu kotor.
  *(Follow-up: pindahkan pembersihan ke saveSaasSection bila kepemilikan berubah.)*
- [x] **S40** Ekspor Excel via `apiFetch` → blob download (nama dari Content-Disposition), tombol
  disabled + "Mengekspor..." + guard dobel-klik, restore di akhir; gagal → toast, tetap di halaman.
- [x] **S41** definisi lokal `copyToken` (pengawas_detail) & `copyServerURL` (dashboard) dihapus —
  semua salin via `copyCode` guarded core; `navigator.clipboard` nol di inline template.
- [x] **S42** fingerprintjs.min.js + device-fingerprint.js ber-`defer` ×4 halaman auth publik
  (urutan dokumen dipertahankan, diuji).
- [x] **S43** guard token diperbaiki: regex rgba literal `/rgba\(\s*[0-9]/g` (tak menghitung
  `rgba(var(--rgb-*))`), plafon folder-wide turun 520→225 + 10 plafon per-file template baru;
  guard JS baru mengunci rgba/hex admin.js ≤36, vouchers ≤9/≤22, voucher-audit ≤2/≤2, billing ≤8/≤1
  (baseline aktual hari ini; self-test regex).
- [x] **S44** blok `:root[data-theme="light"]` arwah + sistem token paralel shared.html DIHAPUS
  (konsisten keputusan S17); ±40 pemakaian dimigrasi ke token resmi theme.css termasuk
  `--text-muted #64748b` → `var(--color-text-muted)`; audit test memastikan nol referensi menggantung.
- [x] **S45** instruksi instalasi: "ketuk tombol Unduh APK di kartu unduhan di atas" (referensi
  spasial salah dihapus).
- [x] **S46** summary bar detail hasil menampilkan Skor Resmi `sub.score/max_score`; total evaluasi
  sisi klien jadi baris rincian berlabel; `id="scoreStatusBadge"` duplikat → class `.score-status-badge`.
- [x] **R29** onclick/onchange inline habis dari string HTML render-JS admin.js
  (`toggle-public-results`, `question-remove`, `question-insert-at`, select tipe soal, hapus baris
  identitas) DAN settings-vouchers.js (retry + paginasi) — registrasi di modul pemiliknya (pola
  Batch 8); nav.html bebas onclick (hamburger kembali ke `initMenuToggle`, Ubah Password via
  `data-action="open-change-password-modal"` dengan registrasi di blok script nav, overlay
  onboarding tanpa dismiss backdrop).
- [ ] **R30 DITUNDA** — aksi massal antrean izin butuh keputusan UX alur konfirmasi
      ("Izinkan semua tampil" vs "jangan tanya lagi 5 menit"). Belum dieksekusi.
- [x] **R31** toast persetujuan menyertakan nama siswa (`findApprovalStudentName`, fallback generik
  bila anonim); jadwal kartu pengawas diformat via `formatDateTimeID`; reload penuh pasca simpan
  konfigurasi soal diganti tutup-modal + `refreshDashboardStats()` (pola R6).
- [x] **R32** sweep asosiasi label settings: `<label for>` naik 41→73 pasangan programatik.
- [x] **R33** drift mekanisme close modal Ubah Password disatukan: settings memakai aksi generik
  `modal-close` + `data-modal-close="closeChangePasswordModal"` (paritas dashboard); registrasi
  ad-hoc `password-modal-close` dihapus. *(Ekstraksi partial bersama DITUNDA — mematahkan kontrak
  fs-read statik banyak suite; putuskan bersama reformasi harness bila dieksekusi.)*
- [x] **R34** seluruh link stylesheet shared.html ber-cache-busting `?v={{.version}}`.
- [x] **R35** th desktop "Waktu Pengerjaan" → "Durasi" (menyamai data-label mobile).
- [x] **R36** `autocapitalize="none" autocorrect="off" spellcheck="false"` pada input username/token
  register & forgot_password; `enterkeyhint="go"` di cek_hasil.
- [x] **R37** paginasi hasil publik `aria-current="page"`; tab Nilai/Kunci deep-linkable
  (`#nilai`/`#kunci`, dibaca saat load + respons hashchange, guard tab Kunci).
- [x] **R38** SmartScreen bilingual: "More Info"/"Info lainnya" → "Run anyway"/"Tetap jalankan".
- [x] **R39** mockup hero landing → placeholder netral `http://alamat-server-sekolah`.
- [x] **R40** chip status tiga tingkat: ≥70 Lulus / 40–69 **Hampir** (`.score-status-mid`, token
  warning) / <40 Belum Lulus + legenda "Status Lulus mulai dari 70".
- [x] **R41** teks merah kecil di permukaan kartu → `var(--color-danger-light)` (kontras 8.60:1,
  dihitung dalam test) di badge admin.js & ikon pengawas_detail; tanpa hex/rgba literal baru.
- Kontrak test lama direvisi minimal dengan intent proteksi dipertahankan: `uiux-batch8-actions`
  (password-modal-close keluar daftar), `uiux-batch2` T3 (badge jadi class), `uiux-batch5-admin-list`
  T13 (container tersedia via nav, duplikat dilarang).

### Batch 1 — Quick wins (±1 jam total) ✅ SELESAI (2026-08-23, test-first: `webui/static/js/uiux-batch1.test.mjs` — 17/17 hijau)
- [x] **T2** uploadError `display:none` default + hapus teks placeholder EN + reset saat modal dibuka
- [x] **R1** colspan 8→6 (baris loading & catch)
- [x] **R3** dobel push settings-packages.js dihapus
- [x] **R5** placeholder "menit" → contoh angka `30` (label span tetap menyebut satuan)
- [x] **T5** CTA state dinonaktifkan → "Kembali ke Beranda" (`href="/"`, ikon panah kiri)
- [x] **S13** loading state tombol unduh: spinner "Memeriksa..." + `aria-busy` + guard dobel-klik `dataset.loading` + pemulihan via `.finally()`
- [x] **R10** guard NaN kartu nonaktif *(koreksi: bagian "komentar interval" batal — komentar terbukti akurat, dashboard memanggil `startAutoRefresh(30)` di `admin.js:3876`)*
- [x] **R16** tab download mobile 3 kolom sejajar (`repeat(3, minmax(0, 1fr))`)

### Batch 2 — Perbaikan tinggi hari ini ✅ SELESAI (2026-08-23, test-first via 3 agen paralel: `webui/static/js/uiux-batch2.test.mjs` — total 37/37 hijau lintas suite)
- [x] **T1** konfirmasi password registrasi — field "Ulangi Password", validasi mismatch live, blokir submit native
- [x] **T6** error persisten + `role="alert"` di 5 halaman auth; login hanya fade-out pesan sukses
- [x] **T4** nav publik "Cek Hasil Ujian" → halaman `/hasil` baru (form token, redirect ke `/hasil/<token>`) + handler Go `CekHasilPage`
- [x] **T7** font Outfit + Plus Jakarta Sans dimuat di partials/head.html — panel admin tak lagi fallback system-ui
- [x] **T9** `#64748b` → `var(--color-text-muted)` (form-hint, role-chip, placeholder, dropdown instansi); kontras ≥4.5:1 diverifikasi perhitungan WCAG dalam test
- [x] **T3** chip status Lulus/Belum Lulus/Belum Dikoreksi + badge rekap + legenda ambang *(bagian P3 "KKM jadi setting server" DITUNDA — butuh skema/API backend, masuk batch berikutnya)*
- [x] **T11** tombol hapus permanen Hasil Ujian berlabel "Hapus" + target sentuh 44px
- [x] **T10a** `.toast-close`: padding 10px (min-size anti box-sizing no-op) + `:focus-visible { opacity:1 }`

### Batch 3 — Minggu ini ✅ SELESAI (2026-08-23, test-first: `webui/static/js/uiux-batch3-*.test.mjs` — total 119/119 hijau lintas 7 suite)
- [x] **T8** diff-render polling pengawas — modul murni `pengawas-detail.js` (`serializeApprovals` + `computeApprovalRowOps`), skip bila payload identik, update per-baris by mac (`data-mac`, `applyApprovalRowOps`), defer render saat aksi Izinkan/Tolak in-flight (`approvalActionBusy` + rerun), notice selalu invalidate snapshot (22 test: `uiux-batch3-t8-polling.test.mjs`)
- [x] **T10b + S19** validasi inline per-field + helper `setFieldError` (`aria-invalid`, pesan per-field, toast hanya pelengkap)
- [x] **S1** toggle status ujian → switch + konfirmasi `showConfirm`
- [x] **S2** guard unsaved-changes modal soal (Batal/Escape/backdrop → konfirmasi buang perubahan) *(S1–S4, S19: `uiux-batch3-dashboard.test.mjs`)*
- [x] **S3** escaping onclick voucher — data-* + event delegation, tanpa interpolasi mentah
- [x] **S4** sweep bahasa EN→ID (level keamanan Rendah/Sedang/Tinggi, "Set Semua Bobot", filter status, label settings, nav publik seragam)
- [x] **S5** samakan live-search debounce di semua input pencarian admin
- [x] **S8/S9** font mikro halaman hasil ≥ 12px & hapus scroll bersarang `.answer-grid` di mobile *(`uiux-batch3-hasil-css.test.mjs`)*
- [x] **S10** audit touch target ≥44px (status-badge, search-clear, modal-close, btn-sm/btn-icon; toast-close sudah di Batch 2) *(`uiux-batch3-a11y-css.test.mjs`)*
- [x] **S11/S12** panduan dipisah "Untuk Siswa" vs "Untuk Admin IT Sekolah" (collapsible `<details>`) + detail unknown-source per-merk Samsung/Xiaomi/vivo/Oppo + asal-usul alamat server & token dengan contoh format
- [x] **S14** ARIA tabs pattern (tablist/tab/tabpanel, roving tabindex, arrow keys) + deep-link hash (#android/#windows/#linux) + respons `hashchange` *(`uiux-batch3-download.test.mjs`, 10 test)*
- [x] **S18** `aria-current="page"` di nav topbar & dropdown + `aria-expanded` pada hamburger
- [x] **S20** bajakan Ctrl+F dilepas (shortcut `/` tetap); panel pintasan disesuaikan

### Batch 4 — Bersih-bersih struktural ✅ SELESAI (2026-08-23, test-first via 4 agen paralel: `webui/static/js/uiux-batch4-*.test.mjs` — total 163/163 hijau lintas 11 suite)
- [x] **S16** sistem modal disatukan: **keputusan arsitektur — `base.html` DIHAPUS** (tak pernah dirender sejak awal, markup drift dari nav.html, sumber regresi senyap); blok skip di `main.go` ikut dihapus; audit 4 halaman standalone bebas trap Escape/Tab ganda — fokus-trap kini murni Global Modal Manager (`uiux-batch4-modal.test.mjs`, 11 test)
- [x] **S15** fase 1 tegakkan design token: token baru di theme.css (`--radius-xs`, `--color-danger-light`, `--color-primary-soft`, `--color-text-placeholder`, `--color-warning-light`), migrasi substitusi-nilai-persis di admin-base.css (hex literal 41→31, pemakaian `var(--radius*` 0→13, visual nol perubahan) + guard `.stylelintrc.json` (config siap, install stylelint ditunda fase 2 tanpa menambah dependensi) *(`uiux-batch4-tokens.test.mjs`, 12 test)*
- [x] **base.html** — terjawab bersama S16 di atas: dihapus, bukan dijadikan layout
- [x] **S17** palet light: **KEPUTUSAN — hapus** (dead code tanpa mekanisme aktivasi; aplikasi resmi dark-by-design; pulihkan via git history bila kelak dibutuhkan)
- [x] **S6** pola OTP disatukan ke 6-kotak register_confirm (G4): reset_password port penuh — paste multi-digit, panah/backspace, aria-label per digit, hidden `otp_code` tetap sinkron *(`uiux-batch4-auth.test.mjs`, 13 test)*
- [x] **S7** judul dashboard ↔ nav kanonik "Daftar Ujian"
- [x] **R2** helper skeleton mati: **KEPUTUSAN — hapus** (`showSkeleton`/`showDashboardSkeletons` + fallback reload; aktivasi butuh desain loading state per halaman — ditunda) *(`uiux-batch4-jscore.test.mjs`, 8 test)*
- [x] **R6** hapus ujian tanpa `location.reload()` — row di-remove, counter diperbarui via `refreshDashboardStats()`, showConfirm dipertahankan
- [x] **R7** pagination submissions meniru dashboard (nomor halaman ±2 + "N dari M" + `aria-disabled`)
- [x] **R8** link Pantau bersarang di kartu pengawas dapat `stopPropagation`
- [x] **R9** username register: hint charset eksplisit + toast sekali-per-sesi saat sanitasi menghapus karakter
- [x] **R11** fingerprintjs login dimuat `defer`
- [x] **R12** skip-link inline base.html moot — file diarsipkan; skip-link halaman aktif memakai class biasa
- [x] **R13** settings tinggal 1 h1 (2 h1 sr-only diturunkan h2)
- [x] **R14** Turnstile `data-theme="dark"` kini konsisten by-design (dark-only sejak S17) + komentar penjelas
- [x] **R15** script setelah `</html>` dipindah ke body, favicon duplikat disisakan satu, register_confirm dapat skip-link

### Keputusan produk ✅ SELESAI (2026-08-23 — diputuskan oleh pemilik produk, terdokumentasi)
- [x] **P1** visibilitas peringkat & nilai terendah publik → **KEPUTUSAN: C — BIARKAN TERBUKA.**
  Siapa pun yang punya token hasil ujian tetap dapat melihat peringkat 1–3, rata-rata kelas,
  dan nilai terendah. Keputusan ini dibuat secara sadar dengan mempertimbangkan risiko
  perundungan berbasis nilai; mitigasi yang tetap berlaku adalah sifat token itu sendiri
  (URL harus diketahui/diberikan pengawas). Bila kelak kebijakan sekolah berubah, opsi
  A (peringkat hanya guru) atau B (peringkat anonim per NIS) tinggal diambil dari catatan ini.
- [x] **P2** remember-me login admin → **KEPUTUSAN: TIDAK DIBUAT.**
  Sesi login admin dibiarkan pendek seperti sekarang; guru selalu login ulang saat sesi
  habis. Alasan: keamanan perangkat lab bersama lebih diutamakan daripada kenyamanan,
  dan frekuensi login admin dinilai masih wajar.

### Batch 5 — Ronde 2: quick wins a11y & feedback ✅ SELESAI (2026-08-23, test-first via 3 agen paralel dengan kepemilikan file terpisah: `uiux-batch5-admin-core.test.mjs` 35/35 · `uiux-batch5-publik.test.mjs` 13/13 · `uiux-batch5-admin-list.test.mjs` 8/8 — total gabungan seluruh suite repo 219/219 hijau, `go build` OK)
- [x] **T13** container toast ditambahkan ke submissions.html (`#toastContainer` aria-live="polite", markup identik pengawas_detail.html) — feedback hapus hasil ujian kini tampil
- [x] **S24** label status disatukan: "Ditombstone" → "Nonaktif Otomatis" di submissions (filter + badge), pengawas (string JS), pengawas_detail (badge + komentar dibersihkan)
- [x] **S29** `copyToken`/`copyAllTokens`/`copyResultsLink`/`copyAIPrompt` menjadi wrapper tipis di atas `copyCode` (guard clipboard + fallback execCommand + toast); signature onclick tak berubah. *Follow-up: `settings-vouchers.js:6` masih meng-override `copyCode` versi tanpa guard saat tab voucher dibuka — delegasikan kembali ke implementasi core.*
- [x] **R17** nav publik set `aria-current="page"` (+ removeAttribute untuk link non-aktif) di script public_foot
- [x] **R20** print CSS hasil: selector fiktif `.search-card`/`.pagination` → `.search-section`, `.pagination-wrapper`, + `.header-badge`
- [x] **R26** "Export Excel" → "Ekspor Excel"
- [x] **R23** aria-label input pencarian ×6: hasil ("Cari nama siswa"), pengawas ("Cari ujian"), pengawas_detail ("Cari peserta"), settings audit ("Cari riwayat voucher"), settings user ("Cari pengguna")
- [x] **S31** icon-button dashboard diberi nama aksesibel: refresh → `aria-label="Muat ulang daftar"`, clear-search → `aria-label="Bersihkan pencarian"`
- [x] **R18** semua `#turnstileError` (register, forgot_password, reset_password, login admin) ber-role="alert" + pesan dibersihkan di awal attempt submit berikutnya
- [x] **R19** heading order: landing mockup-title h3→p; seksi dashboard & pengawas h3→h2 (visual dipertahankan via font-size inline = rule h3 lama); judul tabel pengawas_detail ikut dirapikan. *(Landmark `<header>` topbar belum — masuk batch berikutnya bersama R27)*
- [x] **T12** state error halaman hasil: handler tidak lagi memasangkan pesan internal ke `exam_name` (kirim `"error_state": true` + nama kosong); hero hanya dirender di cabang sukses, state error render `<h1>` netral "Hasil Ujian"; init JS diguard `pageHasError` agar polling tak jalan di halaman error
- [x] **S25** seluruh modal kini semantik dialog: 5 di dashboard + 6 di settings (+3 id heading baru) + tombol ✕ confirmActionModal dapat `aria-label="Tutup"` + detailModal submissions + 3 modal pengawas_detail (`confirmApprovalModal`, `accessLogModal`, `auditLogModal`) — semua `aria-labelledby` divalidasi eksis oleh test
- [x] **S27** double-submit guard pada 5 fungsi admin.js (`submitChangePassword`, `submitEditToken`, `saveQuestionsConfig` via `#btnSaveQuestionsConfig`, `bulkDeleteExams`/`bulkToggleExams` via tombol toolbar): disable + restore di `.finally()`, terverifikasi vm-test klik ganda = 1 POST
- [x] **S30** skip-link: partials/nav.html memuat skip-link tunggal `#mainContent`; deduplikasi publik via partial baru `public_skip_link` di shared.html (inline style lama 9× → 1×, pengecualian register_confirm karena kontrak test batch4); `id="mainContent"` terpasang di kelima halaman admin
- [x] **R22** reset_password mendapat strength meter penuh (port dari register: markup bar+teks, fungsi skor, wiring input event)

### Batch 6 — Ronde 2: struktural ✅ SELESAI (2026-08-24, test-first via 3 agen paralel + 1 agen lanjutan dengan kepemilikan file terpisah; suite gabungan repo **271/271 hijau**, `go build` OK)

> **Metode batch ini:** setiap item dikerjakan *test-first* — kontrak perilaku ditulis lebih dulu sebagai
> file `uiux-batch6-*.test.mjs` (statik fs-read + perilaku via `vm.runInNewContext` mengeksekusi JS asli
> yang dikirim ke browser, pola sama dengan batch sebelumnya), diverifikasi MERAH dulu, baru implementasi
> sampai HIJAU. Empat agen berjalan paralel dengan kepemilikan file yang tidak tumpang tindih:
>
> | Agen | Kepemilikan file | Suite |
> |---|---|---|
> | batch-6-jscore | `admin-core.js`, `admin.js`, `settings-vouchers.js` | `uiux-batch6-jscore.test.mjs` — 20/20 |
> | batch-6-pengawasan | `pengawas_detail.html`, `pengawas.html`, `submissions.html` | `uiux-batch6-pengawasan.test.mjs` — 12/12 |
> | batch-6-nav | `partials/nav.html` | `uiux-batch6-nav.test.mjs` — 6/6 |
> | (lanjutan publik/CSS, sesi awal) | `shared.html`, `hasil.html`, `hasil.css`, `public-*.css`, `theme.css`, `register_confirm.html` | `uiux-batch6-publik-css.test.mjs` — 14/14 |

- [x] **S28** pencarian ganda & dead code dihapus (~230 baris netto dari admin.js):
  versi client-side MATI (`filterExamRows`/`searchExams`/`clearSearch`/`searchExamsWithStatus`,
  `debounceSearch`, listener Enter DOMContentLoaded penyebab flicker dobel-alur) dihapus total;
  strategi URL-navigasi di dashboard.html tetap satu-satunya. Dead code nol-pemanggil ikut dibersihkan:
  `openManageUsersModal`/`closeManageUsersModal` (+ branch overlay-click), `resetNewUserFormDefaults`,
  `copyAllTokens`, `copyResultsLink`, `regenerateToken`; di admin-core.js: `initPasswordStrengthMeter`,
  `toggleShortcuts` + bajakan tombol `"?"`. Shortcut `/` dan Ctrl+U dipertahankan dan diuji perilakunya.
  *(Kontrak test S29 Batch 5 direvisi seperlunya: dua nama wrapper yang dihapus S28 keluar dari daftar asersi.)*
- [x] **S23** penanganan sesi kedaluwarsa global: `apiFetch` mendeteksi `resp.status === 401` →
  `notifyAuthExpired()` (guard once per halaman) menyalakan flag `window.__examvanAuthExpired`
  (bisa dicek polling untuk skip interval) dan menembakkan event `auth:expired`; listener global
  me-toast "Sesi berakhir. Silakan login kembali." lalu redirect tertunda ke
  `/admin/login?next=<pathname+search>`. Error non-401 tidak tersentuh (diuji). Konstruktor CustomEvent
  di-resolve robust (`window.CustomEvent` → global → null); listener terpasang di window dengan fallback
  document.
- [x] **S36** visibility-guard polling pengawas_detail: 4 interval permanen (5s/12s/30s/1s) diganti
  scheduler `startPengawasPolling()`/`stopPengawasPolling()` — handle disimpan, semua callback di-guard
  `document.hidden`, tab tersembunyi mengosongkan timer & kembali visible me-re-schedule + refresh
  langsung; start selalu stop dulu (anti double-scheduling). Flag anti-overlap & diff-render Batch 3 tak
  berubah. *(Diuji perilaku via vm: visible→4 interval jalan; hidden→skip+clear; kembali visible→tanpa
  penumpukan.)*
- [x] **S26** kontrol render-JS keyboard-aksesibel: pagination render-JS pengawas & pengawas_detail kini
  `<button type="button" class="pagination-page-num">` (+`aria-current`); kartu monitor pengawas,
  `.mac-cell` riwayat perangkat, action-link Detail, dan pemicu popup identitas submissions diberi
  `role="button" tabindex="0"` (Enter/Space lewat handler global admin-core).
- [x] **R24** toast tolak peserta sukses tidak lagi merah `'error'` → `'info'`. Frasa salin distandarkan
  lewat `copyCode` (Batch 5) + follow-up di bawah.
- [x] **R25** API Modal terpusat: core mengekspor `var Modal = { open, close }` (resolve by id-string /
  element, return boolean, aman bila elemen tak eksis). Fungsi boilerplate open/close di admin.js &
  settings-vouchers.js menjadi delegasi tipis sambil mempertahankan side-effect (reset form dsb.) —
  diuji bahwa path open/close masih menyasar elemen yang sama.
- [x] **R28** (parsel tanggal) helper `formatDateTimeID()` di core menjadi satu pintu format
  "YYYY-MM-DD HH:MM" (identik formatter manual lama — diuji kesetaraan output); admin.js tak lagi
  memanggil `localizeUTC` (alias kompatibilitas dipertahankan untuk skrip lama lain). *(Migrasi penuh
  event-delegation `data-action` bersifat berkelanjutan, bukan sekali angkat.)*
- [x] **S34** meta sosial: `og:title/og:description/og:type/og:image` (dengan fallback default) di head
  publik + `theme-color` yang nilainya divalidasi test == `--color-bg` theme.css.
- [x] **S35** feedback loading halaman hasil: region `aria-live="polite"` mengumumkan "Menampilkan X–Y
  dari Z peserta" tiap render tabel; `loadResults` menerapkan `aria-busy` + redup tabel saat fetch dan
  melepasnya di `finally`; scroll paginasi kini SETELAH data tampil (diuji urutannya).
- [x] **S32** reduced-motion lengkap: blok `@media (prefers-reduced-motion: reduce)` ditambahkan ke
  public-desktop.css (sebelumnya 0) + print style minimal halaman publik.
- [x] **S33** token z-index: `--z-*` didefinisikan di theme.css; literal liar `9999|10002|99999` di CSS
  inti publik/hasil diganti token (test lintas-file memastikan tak ada lagi literalnya).
- [x] **R21** konflik grid tab download diselesaikan: intent tunggal 3 kolom mobile tanpa
  `!important`; deklarasi desktop yang mati dihapus.
- [x] **Sisa Batch 5:**
  - landmark `<header>` membungkus topbar di partials/nav.html (tanpa mengubah markup/class/logika
    template; aria-current, aria-expanded hamburger, dan urutan skip-link dipertahankan — snapshot oleh test regresi);
  - follow-up S29: settings-vouchers.js berhenti mendefinisikan ulang `copyCode` tanpa guard — call site
    kini memakai versi guarded core (diuji: tanpa `navigator.clipboard`, salin tetap sukses via fallback
    execCommand);
  - register_confirm skip-link menyatu ke partial `public_skip_link` (kontrak test batch4 direvisi;
    inline style lama 9× → bersih, base styling `.skip-link` di theme.css).

### Batch 7 — Fase 2 design token + migrasi event-delegation ✅ SELESAI (2026-08-24, test-first via 4 agen paralel; suite gabungan repo **322/322 hijau**, `go build` OK)

> **Metode batch ini:** sama dengan Batch 6 — kontrak ditulis lebih dulu sebagai file
> `uiux-batch7-*.test.mjs` (diverifikasi merah → implementasi → hijau). Karena skala besar
> (baseline: ±300 hex & ±520 rgba di template, ±69 hex di JS, 146 onclick inline), pekerjaan
> dipecah ke 4 agen paralel dengan kepemilikan file yang tidak tumpang tindih, dan dua
> **kontrak lintas-agen ditetapkan di depan** agar semua agen bisa menulis test terhadapnya:
>
> | Agen | Cakupan | Suite |
> |---|---|---|
> | batch-7-core | infrastruktur: API `Actions` + token `--rgb-*`/`--glass-bg-strong` + kelas `.tone-*`/`.notice-warning` | `uiux-batch7-core.test.mjs` — 13 |
> | batch-7-dashboard | dashboard.html (48 onclick) + admin.js | `uiux-batch7-dashboard.test.mjs` — 9 |
> | batch-7-pengawasan | pengawas_detail/pengawas/submissions (41 onclick) | `uiux-batch7-pengawasan.test.mjs` — 17 |
> | batch-7-settings | settings.html (57 onclick) + 6 modul settings-*.js | `uiux-batch7-settings.test.mjs` — 9 |

- [x] **Infrastruktur delegasi aksi global (R28 lanjutan):** core kini mengekspor
  `var Actions = { register(name, fn), has(name) }` + SATU listener klik delegasi di document
  (`closest('[data-action]') → fn(el, e)` dalam try/catch per handler; nama tak terdaftar diam).
  Argumen lewat data-* (`data-exam-id`, `data-mac`, `data-submission-id`, dst.) — interpolasi mentah
  ke atribut onclick (pola S3) pun hilang sepenuhnya dari halaman-halaman yang dimigrasi.
- [x] **Migrasi onclick → data-action selesai untuk SELURUH halaman admin:** 146 handler inline
  (settings 57 · dashboard 48 · pengawas_detail 27 · pengawas 9 · submissions 5) kini **0 onclick**
  (dikunci guard test per halaman). Handler didaftarkan di tempat definisinya (inline script halaman /
  module settings-*.js / admin.js); fungsi milik file agen lain dibungkus wrapper tipis
  *(follow-up: pindahkan register wrapper ke modul pemiliknya)*. Perilaku lama utuh: showConfirm,
  guard unsaved-changes (S2), double-submit guard (S27), hapus-tanpa-reload (R6), defer-render polling
  Izinkan/Tolak (T8), visibility-guard (S36). Elemen non-button bekas onclick diberi
  role="button" tabindex="0"; keyboard parity via listener keydown delegasi.
- [x] **Fase 2 design token (S15 lanjutan):** token triplet baru di theme.css —
  `--rgb-success/warning/danger/info/accent`, `--color-success-light`, `--glass-bg-strong`
  (rekonsiliasi anti-duplikat: warning/danger/primary/accent-light existing dipakai ulang) — dan kelas
  utilitas surface di admin-base.css: `.tone-success/warning/danger/info/accent/neutral` +
  `.notice-warning`, murni `var()`/`rgba(var(--rgb-*), α)` tanpa hex literal baru.
- [x] **Reduksi terukur (dikunci guard):**
  | File | Hex | rgba literal |
  |---|---|---|
  | settings.html | 184 → 100 (−46%) | 191 → 110 (−42%) |
  | pengawas_detail.html | 59 → 20 (−66%) | 69 → 11 (−84%) |
  | dashboard.html | 49 → 28 (−43%) | 54 → 32 (−41%) |
  | pengawas.html | 24 → 14 (−42%) | 30 → 11 (−63%) |
  | admin.js (JS) | 23 → 8 | — |
  Sisa literal = nilai unik sekali-pakai/tanpa padanan token (swatch data, canvas/chart) atau butuh
  token hitam/putih triplet yang belum ada. Folder-wide guard baru `uiux-batch7-tokens.test.mjs`
  mengunci total templates/ ≤300 hex & ≤520 rgba serta admin.js ≤8 hex — angka tidak boleh naik lagi
  tanpa keputusan sadar.
- [x] Kontrak test lama yang basi akibat migrasi direvisi minimal dengan intent proteksi dipertahankan
  (batch6-pengawasan mac-cell, batch4-modal R8 kartu monitor, batch2 T11 locator tombol hapus).

### Batch 8 — Lanjutan delegasi aksi + fase 2 token ✅ SELESAI (2026-08-24, test-first via 2 agen paralel + 1 sesi langsung dengan kepemilikan file terpisah; suite gabungan repo **411/411 hijau**, `go build` OK)

> **Metode:** sama dengan Batch 6–7 — kontrak ditulis lebih dulu sebagai file
> `uiux-batch8-*.test.mjs` (diverifikasi merah → implementasi → hijau). Tiga pemilik
> cakupan paralel tanpa tumpang tindih file:
>
> | Agen | Kepemilikan file | Suite |
> |---|---|---|
> | batch-8-actions | `admin-core.js`, `admin.js`, `settings-vouchers.js`, `settings-users.js`, `settings.html`, `submissions.html` | `uiux-batch8-actions.test.mjs` — 15 |
> | batch-8-tokens-css | `theme.css`, `admin-base.css`, `hasil.css`, `public-desktop.css`, `public-mobile.css` | `uiux-batch8-tokens-css.test.mjs` — 23 |
> | (publik, sesi langsung) | `download.html`, `shared.html`, `register.html` | `uiux-batch8-publik.test.mjs` — 62 |

- [x] **Registrasi Actions pindah ke modul pemiliknya** (lanjutan R28/Batch 7):
  wrapper tipis di inline script halaman dihapus dari tempat asalnya dan didaftarkan
  langsung di modul yang MENDEFINISIKAN fungsinya — 10 aksi settings (`smtp-test`,
  `smtp-save`, `turnstile-save`, `cleanup-save`, `default-pkg-save`, `versions-save`,
  `footer-save`, `seo-save`, `monetization-save`, `password-modal-close`) + 4 aksi users +
  4 aksi submissions kini di blok registrasi `admin.js`; 7 aksi voucher +
  `confirm-action-close` kini di `settings-vouchers.js`; inline `submissions.html` tinggal
  komentar penunjuk. Guard `typeof window.x === 'function'` dihapus karena kini satu file.
- [x] **`modal-dismiss` disatukan** (sebelumnya dobel-registrasi admin.js + settings.html yang
  saling MENIMPA di registry): SATU registrasi kanonik di `admin-core.js` tepat setelah objek
  `Actions` — tersedia otomatis di semua halaman; semantik superset (guard klik-overlay
  `ev.target !== el` + resolver close-fn via `window` → fallback `globalThis`).
- [x] **Normalisasi tipe argumen data-*:** handler id numerik di admin.js kini konsisten
  `parseInt(..., 10)` — termasuk `show-submission-detail`/`delete-submission`
  (`data-submission-id` sebelumnya string mentah) plus `token-edit-open`, `questions-open`,
  `exam-delete`, `edit-exam-open`, `delegate-exam-open`. Argumen yang memang string
  (`data-token`, `data-name`, `data-color`) tidak disentuh.
- [x] **Shim defensif `Actions` di settings.html DIHAPUS total** — kontrak core pasti
  (admin-core.js selalu dimuat lebih dulu); harness uji batch7-settings direvisi mengikuti
  urutan `<script>` halaman sungguhan.
- [x] **Token triplet hitam/putih** di theme.css: `--rgb-black: 0, 0, 0` & `--rgb-white:
  255, 255, 255` (kontrak lintas-agen, nama & nilai dikunci test). Migrasi substitusi-
  nilai-persis di CSS inti: **75 literal rgba hitam/putih → rgba(var(--rgb-*), α)** —
  admin-base.css 30 · hasil.css 30 · public-desktop.css 6 · public-mobile.css 6 ·
  theme.css 3 (shadow-card/lg/sm). Pengecualian ber-comment alasan: definisi token :root
  theme.css (tidak boleh self-referential) + scrollbar hasil.css (regex test Batch 3
  menguras alpha literal).
- [x] **Migrasi hex/rgba template publik (download/shared/register):** rgba hitam/putih
  → `var(--rgb-black/white)`; triplet brand → `var(--rgb-info/success/warning/danger/accent)`
  (63+44+12 = 119 pemakaian `rgba(var(--rgb-*))` baru); hex bersubstitusi → token semantik
  persis (#a5b4fc→primary-light, #fbbf24→warning-light, dst.). Hex turun download 37→16,
  shared 32→27, register 16→8; sisa = nilai unik tanpa padanan token (#818cf8, #7c3aed,
  rgba(129,140,248,…)). Whitelist eksplisit dikunci test: definisi token lokal `:root`
  shared.html & `<meta name="theme-color">` (konteks non-CSS wajib literal).
- [x] Kontrak test lama direvisi minimal dengan intent proteksi dipertahankan:
  batch7-dashboard/settings/pengawasan kini menerima registrasi dari modul pemilik +
  admin-core.js (union sumber); asersi "shim harus ada" dibalik menjadi "shim tidak ada";
  test vm wrapper submissions mengharapkan id angka hasil normalisasi.



---

*Catatan ronde 1:* *Laporan ini dihasilkan dari review statis kode. Rekomendasi perlu diverifikasi ulang di runtime bila menyangkut perilaku dinamis (polling, autofill OTP, kontras di device nyata).*
