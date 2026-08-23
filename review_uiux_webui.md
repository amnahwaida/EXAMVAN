# Review UI/UX Web App EXAMVAN

> **Tanggal review:** 23 Agustus 2026 · **Basis kode:** branch `main` @ `111019e` (working tree bersih)
> **Metode:** pembacaan menyeluruh ±26.000 baris template + CSS + JS oleh 3 reviewer paralel (area admin, area publik, lintas-halaman/a11y/design-system) + verifikasi manual temuan kunci.
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

## 2. Temuan SEDANG (S1–S22)

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

## 3. Temuan RENDAH (R1–R16)

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

### P2 — "Remember me" pada login admin belum ada
- **Lokasi:** `login.html:67-91` (form hanya username/password/turnstile; `autocomplete` sudah benar).
- **Isu:** Murni keputusan produk. Untuk perangkat guru pribadi, sesi lebih panjang nyaman; untuk perangkat lab bersama, sesi pendek lebih aman.
- **Opsi:** Checkbox remember-me (extend TTL cookie) + default sesi pendek.

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

### Keputusan produk (butuh diskusi, bukan kode)
- [ ] **P1** visibilitas peringkat & nilai terendah publik
- [ ] **P2** remember-me login admin

---

*Laporan ini dihasilkan dari review statis kode. Rekomendasi perlu diverifikasi ulang di runtime bila menyangkut perilaku dinamis (polling, autofill OTP, kontras di device nyata).*
