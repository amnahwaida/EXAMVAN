# Review UI/UX Web App EXAMVAN

> **Tanggal review:** 23 Agustus 2026 · **Basis kode awal:** branch `main` @ `111019e` · **Re-review ronde 2:** 23 Agustus 2026 @ `cbc837f` (setelah Batch 1–4 selesai)
> **Metode:** pembacaan menyeluruh ±26.000 baris template + CSS + JS oleh 3 reviewer paralel (area admin, area publik, lintas-halaman/a11y/design-system) + verifikasi manual temuan kunci. Ronde 2 mengulang metode yang sama (3 reviewer paralel) untuk memverifikasi perbaikan dan mencari temuan baru.
> **Ronde 2:** seluruh temuan lama Tinggi/Sedang/Rendah (kecuali yang dicatat masih terbuka) terverifikasi BERES; ditemukan **2 masalah Tinggi, 14 Sedang, dan 12 Rendah baru** — lihat [bagian 5.5](#55-re-review-ronde-2--temuan-baru-pasca-batch-14).
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

## 6. REKAP TRACKING

> Centang `[x]` + cantumkan hash commit saat selesai. Urut sesuai prioritas eksekusi.

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
