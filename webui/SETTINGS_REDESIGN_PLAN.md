# Rencana Implementasi: Redesign Tab Pengaturan (5 Tab + Dropdown Mobile)

> **Status:** ✅ SELESAI DIIMPLEMENTASI (commit `bb953be`-lanjutan). Dokumen ini
> awalnya adalah kontrak kerja untuk jika pengerjaan terputus; semua langkah di
> bawah sudah selesai dan diverifikasi (build, vet, test DB, browser desktop &
> mobile). Tetap dipertahankan sebagai referensi desain.

---

## 1. Tujuan

Menyelesaikan dua masalah UX yang disepakati:

1. **Tab bar mobile harus scroll kanan-kiri** — 6 tab berlabel panjang tidak muat
   di viewport ponsel (`overflow-x: auto` memaksa scroll ~535px dari total 897px
   di layar 390px). Solusi: ganti tab bar dengan **dropdown "Pilih Bagian"** di
   bawah breakpoint mobile.
2. **Tab Kelola User terlalu penuh** — mencampur 3 domain berbeda (SaaS & SMTP
   Email Settings, Tambah User Manual, Daftar User & Guru). Solusi: pindahkan
   SaaS & SMTP (plus Pengaturan Paket) ke tab baru **"Pengaturan Umum"**, dan
   gabungkan Kelola Voucher + Riwayat Klaim Voucher menjadi satu tab **"Voucher"**
   dengan subtab.

Hasil akhir: **6 tab → 5 tab**, tiap tab berfokus satu domain, mobile bebas
scroll horizontal.

---

## 2. Desain Target

### 2.1 Struktur 5 tab (SuperAdmin)

| # | Tab (label) | Section id | Hash | Isi |
|---|---|---|---|---|
| 1 | Kelola User | `users` | `#users` | Tambah User Manual + Daftar User & Guru (SaaS & SMTP **dipindah keluar**) |
| 2 | Paket & Voucher | `billing` | `#billing` | Claim/renewal pemilik akun (tetap, tidak berubah) |
| 3 | Voucher | `vouchers` | `#vouchers` | Subtab: **Daftar Voucher** + **Riwayat Klaim** (ex-tab `voucher-audit` digabung ke sini) |
| 4 | Pengaturan Umum | `general` | `#general` | **BARU** — SaaS & SMTP Email Settings (ex-Kelola User) + Pengaturan Paket (ex-tab `packages`) |
| 5 | Aplikasi Sistem | `system-apps` | `#system-apps` | Tetap |

Role lain (tidak berubah dari sekarang):
- **Operator:** 2 tab — Kelola User, Paket & Voucher.
- **Guru/Pengawas:** 1 tab — Paket & Voucher.
- **Feature-locked:** 1 tab — Paket & Voucher (hanya section `billing` dirender).

### 2.2 Backward-compat hash & URL (WAJIB dipertahankan)

Semua bookmark/link lama harus tetap bekerja. `SettingsRedirect` di `main.go`
tetap mengarah ke hash lama; JS tab yang menerjemahkan hash → section:

| Hash lama (dari redirect) | Tab yang terbuka | Catatan |
|---|---|---|
| `#users` | Kelola User | tetap |
| `#billing` | Paket & Voucher | tetap |
| `#vouchers` | Voucher (subtab Daftar) | section sama |
| `#voucher-audit` | Voucher (**subtab Riwayat**) | alias → `vouchers` + buka subtab Riwayat |
| `#packages` | Pengaturan Umum (scroll ke kartu Pengaturan Paket) | alias → `general` + buka subtab/kartu Paket |
| `#system-apps` | Aplikasi Sistem | tetap |
| `#general` | Pengaturan Umum | hash baru |

Implementasi: di JS tab, fungsi `resolveSection(hash)` memetakan alias di atas.
Hash yang ditulis ke address bar saat klik tab tetap hash "kanonik" tab
(`#users`, `#billing`, `#vouchers`, `#general`, `#system-apps`).

### 2.3 Dropdown mobile

- **Desktop (≥1100px):** tab bar horizontal seperti sekarang (`.settings-tabs`).
- **Mobile (<1100px):** tab bar disembunyikan (`display:none`), muncul
  `<select id="settingsSectionSelect">` (atau dropdown custom) berisi opsi tab
  yang dirender untuk role saat ini.
- **Sinkronisasi:** saat `activate(key)` dipanggil → `select.value = key`;
  saat `select` berubah → `activate(select.value)` + `history.replaceState`
  hash. Dropdown dirender di partial `settings-tabs.html` dengan gate role yang
  sama persis dengan tab bar (agar opsi yang tampil = tab yang ada).
- Catatan CSS: breakpoint yang dipakai sekarang `max-width: 1100px`
  (`admin-base.css`) — pakai konstanta yang sama.

---

## 3. Perubahan Per File

### 3.1 Template

- **`webui/templates/admin/settings.html`**
  - Pindahkan blok kartu **"SaaS & SMTP Email Settings"** (baris ±761–994,
    termasuk `saasSettingsForm` dan `window.__settingsReady.users` terkait)
    dari `section-users` ke section baru `section-general`.
  - Pindahkan blok **"Pengaturan Paket"** (ex-`section-packages`, baris
    ±1725–1776) ke `section-general`.
  - Ubah `section-voucher-audit` (baris ±1670) menjadi **subtab di dalam
    `section-vouchers`** — dua panel yang di-toggle: "Daftar Voucher" dan
    "Riwayat Klaim Voucher".
  - Update inline JS tab (baris ±1932): tambah `resolveSection(hash)` untuk
    alias `voucher-audit`/`packages`; tambah sinkronisasi dropdown mobile;
    tambah logika subtab voucher (toggle panel + tombol subtab).
  - Tambah `select#settingsSectionSelect` (atau markup dropdown) — atau
    render di partial (lihat 3.2).

- **`webui/templates/admin/partials/settings-tabs.html`**
  - Tambah dropdown mobile dengan gate role & `feature_locked` yang sama
    persis dengan tab bar (baris 26–37).
  - Tab bar desktop: 5 tab (hapus `voucher-audit` & `packages` dari baris tab;
    label "Kelola Voucher" → "Voucher").
  - Perhatikan: baris 26–31 saat ini berisi beberapa duplikat tab `billing`
    (artifact render role) — rapikan saat mengerjakan.

### 3.2 JavaScript

- **`webui/static/js/settings-vouchers.js`** — tambah init subtab Riwayat
  (pindahkan logika render riwayat dari `settings-voucher-audit.js`).
- **`webui/static/js/settings-voucher-audit.js`** — menjadi modul subtab
  Riwayat (dipanggil saat subtab Riwayat aktif, bukan sebagai section sendiri).
- **`webui/static/js/settings-users.js`** — pastikan init SaaS (jika ada) pindah
  ke modul `settings-general.js`; file baru **`settings-general.js`** untuk
  init SaaS & SMTP + Pengaturan Paket (ambil dari `settings-users.js` +
  `settings-packages.js`).
- **`webui/static/js/settings-packages.js`** — dipanggil saat kartu Pengaturan
  Paket di tab general aktif (bisa tetap file terpisah, di-load dari
  `settings-general.js` atau via key `general`).
- Tab shell di `settings.html` (inline) — `loadSectionScript` tetap memuat
  `/static/js/settings-<key>.js`; untuk tab `general` muat
  `settings-general.js`, untuk tab `vouchers` muat `settings-vouchers.js`
  (yang kini berisi subtab Daftar + Riwayat).

### 3.3 Handler (kemungkinan tanpa perubahan)

- `SettingsPage` (`webui/internal/handlers/admin/settings.go`) — tidak perlu
  berubah selama data yang dikirim (billing/users/feature_locked/role) tetap.
  Jika section `general` butuh data paket, pastikan data yang sama dengan
  `loadBillingPageData`/paket sudah dikirim.
- `SettingsRedirect` + route di `main.go` — **tidak berubah** (URL lama tetap
  redirect ke hash lama; JS yang menerjemahkan).

### 3.4 CSS

- **`webui/static/css/admin-base.css`** (atau style inline di settings.html):
  - `@media (max-width:1100px)`: `.settings-tabs { display:none }`,
    `#settingsSectionSelect { display:block }` (dan sebaliknya di desktop).
  - Style subtab voucher (tombol toggle Daftar/Riwayat) + kartu active state.

---

## 4. Aturan yang TIDAK BOLEH Berubah (jebakan)

1. **Feature-lock:** akun expired HANYA boleh melihat section `billing`
   (tab Paket & Voucher). Partial `settings-tabs.html` dan dropdown mobile
   harus mempertahankan gate `feature_locked` — akun terkunci tidak boleh
   melihat tab lain, termasuk di dropdown.
2. **Role gate:** `users` = super+operator; `vouchers`/`general`/`system-apps`
   = superadmin; `billing` = semua. Dropdown mobile harus merender opsi yang
   sama persis dengan tab bar.
3. **Hash lama tetap berfungsi:** `#voucher-audit` dan `#packages` harus tetap
   membuka konten yang benar (via alias `resolveSection`). Redirect 302 di
   `main.go` TIDAK diubah.
4. **Perilaku API tidak tersentuh:** semua endpoint `/admin/api/*` tetap —
   redesign ini murni template + JS.
5. **JS lazy-load:** pola `window.__settingsReady[key]` / `__settingsLoaded`
   dipertahankan; jangan mengubah kontrak init section.

---

## 5. Langkah Implementasi (checklist)

Urutan dikerjakan dari yang paling independen ke paling berisiko. Tandai `[x]`
setelah selesai.

- [x] **L1 — Pindahkan SaaS & SMTP ke tab `general` (template):**
  buat `section-general` baru di `settings.html`, pindahkan kartu SaaS & SMTP
  (form `saasSettingsForm` + field-fieldnya) dari `section-users`, gate
  `{{if eq .admin_role "superadmin"}}` dipertahankan.
- [x] **L2 — Pindahkan Pengaturan Paket ke tab `general`:**
  pindahkan kartu Pengaturan Paket (ex-`section-packages`) ke `section-general`.
- [x] **L3 — Gabungkan Riwayat Klaim ke tab `vouchers` (subtab):**
  `section-voucher-audit` menjadi panel subtab di dalam `section-vouchers`;
  tombol toggle "Daftar Voucher" / "Riwayat Klaim"; JS subtab.
- [x] **L4 — Update tab bar (partial) jadi 5 tab:**
  hapus tab `voucher-audit` & `packages`, tambah tab `general`, label
  "Kelola Voucher" → "Voucher"; rapikan duplikat tab `billing`.
- [x] **L5 — JS shell: `resolveSection` + subtab + dropdown sync:**
  inline script di `settings.html`: alias hash, logika subtab voucher, dan
  sinkronisasi `select#settingsSectionSelect` ↔ `activate()`.
- [x] **L6 — Dropdown mobile (partial + CSS):**
  tambah `select#settingsSectionSelect` di `settings-tabs.html` dengan gate
  role/feature_locked sama; CSS media query ≤1100px.
- [x] **L7 — Pisah/atur modul JS per tab:**
  `settings-general.js` (SaaS + paket), `settings-vouchers.js` (daftar +
  riwayat), `settings-voucher-audit.js` → subtab riwayat,
  `settings-packages.js` → dipanggil dari general.
- [x] **L8 — Update test:**
  `settings_page_test.go` (markup section baru, default section),
  `voucher_audit_template_test.go` (baca `settings-vouchers.js` / subtab),
  `saas_settings_test.go` (marker SaaS kini di `section-general`),
  `templates_js_syntax_test.go` (file JS baru ikut dicek), dll. yang
  menyebut `section-users` untuk SaaS.
- [x] **L9 — Verifikasi:**
  `go build ./...`, `go vet ./...`, test dengan DB
  (`TEST_DATABASE_URL=... TEST_DB_RESET=1 go test ./internal/handlers/admin/`),
  lalu verifikasi browser: desktop (5 tab), mobile (dropdown, tanpa scroll),
  hash lama (`#voucher-audit`, `#packages`), role operator/guru/feature-locked.

---

## 6. Test yang Terkena Dampak (referensi cepat)

- `webui/internal/handlers/admin/settings_page_test.go` — markup tab/section,
  default section, feature-lock.
- `webui/internal/handlers/admin/voucher_audit_template_test.go` — baca JS
  riwayat (sekarang `settings-voucher-audit.js`; setelah redesign bisa
  `settings-vouchers.js`).
- `webui/internal/handlers/admin/saas_settings_test.go` — marker SaaS
  (`saasSettingsForm`, `emailEnabledInput`, dst.) sekarang di
  `section-general`.
- `webui/internal/handlers/admin/templates_js_syntax_test.go` — pastikan file
  JS baru (`settings-general.js`) ikut ter-parse.
- `webui/internal/handlers/admin/users_page_test.go` / `billing_page_test.go` —
  verifikasi marker yang mereka cari masih ada di `settings.html`.

---

## 7. Verifikasi Manual (browser)

1. **Desktop (1440px), superadmin:** 5 tab tampil; pindah antar tab tanpa
   reload; hash berubah (`#users`, `#billing`, `#vouchers`, `#general`,
   `#system-apps`).
2. **Mobile (390px):** tab bar tidak tampil; dropdown "Pilih Bagian" tampil;
   pindah tab via dropdown; **tidak ada scroll horizontal**.
3. **Hash lama:** buka `/admin/settings#voucher-audit` → tab Voucher dengan
   subtab Riwayat aktif; `/admin/settings#packages` → tab Pengaturan Umum
   dengan kartu Pengaturan Paket terlihat; redirect 302 `/admin/vouchers/audit`
   dan `/admin/packages` tetap bekerja.
4. **Role:** operator → 2 tab (users, billing); guru → 1 tab (billing);
   feature-locked → 1 tab (billing), tidak ada tab lain di dropdown.
5. **Konten:** SaaS & SMTP + Pengaturan Paket hanya di tab Pengaturan Umum
   (superadmin); Kelola User berisi Tambah + Daftar saja.

---

## 8. Rollback

Perubahan terbatas di `webui/templates/admin/settings.html`,
`webui/templates/admin/partials/settings-tabs.html`, file
`webui/static/js/settings-*.js`, dan CSS — tidak menyentuh handler/route/API.
Revert `git checkout -- <file>` pada file tersebut sudah cukup; redirect lama
dan behavior server tidak terpengaruh.
