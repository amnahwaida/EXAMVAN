# Review UI/UX Web App EXAMVAN

> **Tanggal review:** 23 Agustus 2026 · **Basis kode awal:** branch `main` @ `111019e` · **Re-review ronde 2:** 23 Agustus 2026 @ `cbc837f` (setelah Batch 1–4 selesai)
> **Metode:** pembacaan menyeluruh ±26.000 baris template + CSS + JS oleh 3 reviewer paralel (area admin, area publik, lintas-halaman/a11y/design-system) + verifikasi manual temuan kunci. Ronde 2 mengulang metode yang sama (3 reviewer paralel) untuk memverifikasi perbaikan dan mencari temuan baru.
> **Ronde 2:** seluruh temuan lama Tinggi/Sedang/Rendah (kecuali yang dicatat masih terbuka) terverifikasi BERES; ditemukan **2 masalah Tinggi, 14 Sedang, dan 12 Rendah baru** — lihat [bagian 5.5](#55-re-review-ronde-2--temuan-baru-pasca-batch-14).
> **Ronde 3 (24 Agustus 2026 @ `1387853`, pasca Batch 8):** migrasi Batch 7–8 terverifikasi bersih di level registry; ditemukan **3 masalah Tinggi, 10 Sedang, dan 13 Rendah baru** — lihat [bagian 5.6](#56-re-review-ronde-3--temuan-baru-pasca-batch-58). Seluruhnya dieksekusi di **Batch 9** (25/26 item — sisa terbuka: R30 ditunda butuh keputusan UX).
> **Ronde 4 (24 Agustus 2026 @ `a1afd9c`, pasca Batch 9):** regresi Batch 9 hampir seluruhnya bersih (1 eksekusi perlu dirapikan → S47); ditemukan **2 masalah Tinggi, 13 Sedang, dan 12 Rendah baru** — lihat [bagian 5.7](#57-re-review-ronde-4--temuan-baru-pasca-batch-9). Seluruhnya dieksekusi di **Batch 10** kecuali S57 ditunda (ekstraksi blok inline besar).
> **Ronde 5 (24 Agustus 2026 @ `2debff6`, pasca Batch 10):** eksekusi Batch 10 terverifikasi asli & terukur, namun ditemukan **3 masalah Tinggi, 5 Sedang, dan 13 Rendah baru** — termasuk regresi fungsional T19 (race defer vs registrasi Actions) dan dua temuan integritas proses (klaim `[x]` yang tidak tuntas) — lihat [bagian 5.8](#58-re-review-ronde-5--temuan-baru-pasca-batch-10). Seluruhnya dieksekusi di **Batch 11**.
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

## 6. REKAP TRACKING

> Centang `[x]` + cantumkan hash commit saat selesai. Urut sesuai prioritas eksekusi.

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
