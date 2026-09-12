# Review UI Halaman Web EXAMVAN — 12 September 2026

Dokumen kelanjutan dari `review_web_flow_dan_dead_code.md` (commit f151a44). Review ini mencakup **seluruh halaman web secara detail** — template admin, template publik, CSS, dan JavaScript statis — serta melaporkan status terbaru seluruh temuan UI dokumen lama.

- **Baseline**: `main` @ `3c34337` (12 Sep 2026, sudah memuat `57260b4` — perbaikan M13). Selama review berjalan, 13 commit fix paralel mendarat di branch yang sama hingga HEAD `90ca7de` (daftar verbatim §8 #9); status per temuan baru ada di §6.
- **Metode**: review-only. Tidak ada file yang diubah oleh review ini.
- **Penomoran**: MEDIUM baru mulai **M16**, LOW baru mulai **L42** (meneruskan penomoran global dokumen lama).

---

## 1. Ringkasan Eksekutif

| Kategori | Jumlah | Rincian |
|---|---|---|
| Temuan baru — HIGH | 0 | — |
| Temuan baru — MEDIUM | 12 | M16–M27 |
| Temuan baru — LOW | 35 | L42–L76 |

**Sorotan temuan baru (MEDIUM):**

1. **Kleen sink/escape belum tuntas di `admin.js`** — M22 (`p.id` di-interpolasi mentah ke `innerHTML`), M23 (`target="_blank"` tanpa `rel="noopener"` + token tak di-escape — satu-satunya pelanggar di repo), M25 (ekspor XML tanpa escaping → round-trip import soal "Benar & Salah" gagal) — **FIX `428568c`**.
2. **Aksesibilitas state & keyboard** — M17 (tabel packages tanpa `aria-live`), M18 (tooltip 100% hover, 0 `:focus-within`), M20 (`tr.onclick` + tabindex duplikat di halaman hasil), M24 (tombol identitas submissions lolos dari filter keyboard `[data-action][role="button"]`) — **FIX `ab64bd9`**; M19 (skip link muncul SETELAH nav — 4 dari 5 halaman auth terfix `ab64bd9`, **MASIH di hasil.html:71-72**).
3. **Duplikasi implementasi** — M21 (3 implementasi toast hidup bersamaan — register terfix `3305d0d`; sisa skin download.html:397-486 + host hardcode hasil.html:73) — **FIX PARSIAL**; M26 (sprite 19 simbol duplikat lokal di hasil.html, 2 di antaranya dead), M27 (kaskade `.main` max-width bertabrakan antara public-desktop.css dan hasil.css) — **FIX `e86ecda`**.
4. **State error yang menyesatkan** — M16 (kegagalan memuat Packages membiarkan tombol Simpan aktif → POST `{"packages":[]}` → server menolak dengan "Tidak ada paket yang dikirim") — **FIX `71c19ec`**.

**Status temuan lama (detail §3):**

| Status | Temuan lama |
|---|---|
| SUDAH DIPERBAIKI | **M9–M11** (token dashboard, via `4033735`), **M15 + 5.4#2** (`@import` theme.css dihapus, via `245b4f7`), **M13** (guard double-submit cek_hasil, via `57260b4`, terverifikasi + test 143 baris), **L8** (via `06c3b43`), **L10+L11** (via `f88ef27`), H1, H2, M1–M8 (8 commit backend 12 Sep 2026 — di luar scope UI) |
| MASIH ADA | M12 (settings 8 blok inline), M14 (copy Inggris index), 5.4#1, 5.4#4, L19, L20, L23, L25, L26, L27, L28 |

Status temuan **baru** (M16–M27 / L42–L76) pasca sesi fix paralel dirinci di §6 (10 MEDIUM fix penuh, 1 parsial, 1 aktif; 4 LOW fix).
| BERUBAH (menyempit) | 5.4#3 (showToast kini tersentral di admin-core.js; sisa skin CSS download + toast lokal register), L21 (sisa teks loading polos tinggal di Packages/Vouchers), L22 (kini `data-page` mentah di voucher-audit:8-9), L24 (settings:1916 sudah `data-action="app-delete"`; sisa inline hanya settings-system-apps.js:109-112) |
| Tak terpulihkan | Teks L12–L18 (7 LOW) dan L29–L41 (13 LOW) tidak dapat direkonstruksi — hitungan dipertahankan sesuai dokumen lama (:318, :375) |

**Koreksi terhadap draf internal (disiplin bukti §5.6):** verifikasi ulang `shared.html:882-895` membuktikan `public_head` memuat `{{ template "public_skip_link" . }}` (:886) **sebelum** `{{ template "public_nav" . }}` (:893) — jadi index, download, hasil, dan semua halaman pemakai `public_head` mewarisi urutan skip-link yang BENAR. Klaim draf awal "index/download tanpa skip link" ditarik; M19 menyempit ke 5 halaman auth saja — dan setelah `ab64bd9` (sesi fix paralel), tersisa **hasil.html saja** (§4.4).

---

## 2. Ruang Lingkup & Metodologi

### 2.1 Cakupan file

**Template admin** (`webui/templates/admin/`): login.html (157 baris), dashboard.html (1221), pengawas.html (412), pengawas_detail.html (2291), settings.html (2235), submissions.html (476).
**Partials admin** (`webui/templates/admin/partials/`): head.html (12), nav.html (210), settings-tabs.html (61), svg-symbols.html (45).
**Template publik** (`webui/templates/public/`): cek_hasil.html (100), forgot_password.html (105), index.html (135), reset_password.html (359), register_confirm.html (469), register.html (491), shared.html (943), download.html (1024), hasil.html (1070).
**JavaScript** (`webui/static/js/` — flat, tanpa subdirektori): admin.js (4430), admin-core.js (1050), settings-vouchers.js (540), settings-system-apps.js (512), settings-general.js (275), settings-billing.js (232), settings-packages.js (157), pengawas-detail.js (147), settings-users.js (147), settings-voucher-audit.js (128).
**CSS** (`webui/static/css/`): admin-base.css (1535), hasil.css (984), public-desktop.css (93), public-mobile.css (219), theme.css (172).

*Catatan snapshot*: cek_hasil (100), shared (943), hasil (1070), dan public-desktop.css (93) diverifikasi ulang pasca-sesi-fix; admin.js/settings.html kemungkinan bergeser beberapa baris dari jumlah pra-fix — metodologi "nomor baris = snapshot saat temuan ditemukan" berlaku (§8 #10).*

### 2.2 Aturan bukti (mengikuti §5.6 dokumen lama)

1. Setiap temuan wajib bukti `file:line` yang dapat direproduksi; klaim yang tidak dapat direproduksi ke sumber **dikecualikan**.
2. Bukti baris terkutip mengalahkan ingatan/draf — dua klaim draf dikoreksi/ditarik oleh bukti segar selama review ini (lihat koreksi M19 dan census `white` 28-match di §8).
3. Sensus dilakukan dengan skrip yang mencakup partials dan `find` rekursif untuk JS (iterasi pertama sempat salah karena glob non-rekursif; hasil final tersaji di sini).
4. Tabel rekap dibangun ulang dari ledger temuan terverifikasi — angka kerangka draf tidak dipercaya; baris tanpa bukti ledger didrop (mis. "forgot_password 0/0/1" tanpa item bukti → dicatat 0/0/0).

### 2.3 Verifikasi git

Review dimulai di `HEAD = 3c34337` ("fix(queue): heartbeat worker aktif di-refresh tiap tick — M8"), parent `57260b4` ("fix(ui): M13 guard cek_hasil"), `bb21933` (M3+M4). Selama review berjalan, 13 commit fix paralel mendarat di branch yang sama hingga `90ca7de` (daftar verbatim §8 #9). Perbaikan Bagian 1–4 (M1–M8) dan H1/H2 telah masuk via 8 commit backend 12 Sep 2026 — di luar scope review UI ini dan hanya dilaporkan sebagai status.

---

## 3. Status Temuan Lama (dokumen f151a44)

### 3.1 Grid status

| ID | Temuan (ringkas) | Status | Bukti terkini |
|---|---|---|---|
| M9 | Dashboard `--accent-light` (nama token lama) | **SUDAH FIX** (`4033735`) | dashboard.html:459 (snapshot pra-fix) → padanan resmi `--color-accent-light` theme.css:23; diganti token resmi oleh commit fix paralel |
| M10 | Dashboard `--glass-border` | **SUDAH FIX** (`4033735`) | dashboard.html:693/:709 (snapshot pra-fix) → theme.css:14; diganti token resmi |
| M11 | Dashboard `--text-primary` | **SUDAH FIX** (`4033735`) | dashboard.html:697/:714 (snapshot pra-fix) → theme.css:67; diganti token resmi |
| M12 | Settings 8 blok `<style>` inline + hex literal | **MASIH ADA** | settings.html :22–:558; `#6ee7b7` ×8 (:1454/:1500/:1532/:1580/:1606/:1633/:1674/:1697), `#93c5fd` :1446, `#1e1b4b` :462; juga :438/:283/:308/:1161/:1173; settings-vouchers.js:75/:80/:82/:106/:110/:121; settings-billing.js:132/:139 |
| M13 | cek_hasil tanpa guard double-submit | **SUDAH FIX** (57260b4) | cek_hasil.html:64-98 guard + test `uiux-batch23-cekhasil-guard.test.mjs` (143 baris) — verbatim §9 |
| M14 | Copy Inggris di index.html | **MASIH ADA** | :11 "Managed Kiosk Mode Device Owner", :48-49 "Low, Medium & Strict", :90 "5 Tipe Soal & Partial Scoring", :114 "Zero-Friction Launch" |
| M15 | `@import` CSS tanpa versi | **SUDAH FIX** (`245b4f7`) | `@import theme.css` dihapus seluruhnya — kini hanya link berversi (kontras pra-fix: head.html:8-10/admin-base.css:5 berversi vs `@import` polos); M15 selesai bersama 5.4#2 |
| 5.4#1 | Duplikasi simbol SVG inline | **MASIH ADA** (sensus baru) | 67 inline / 31 unik / 0 missing-sprite: login 0, dashboard 30, pengawas 7, pengawas_detail 11 (+ sprite lokal 11 simbol :2277-2278), submissions 19, settings 0. Termasuk eye-toggle ×3 (dashboard :589-592/:601-604/:613-615) dan copy-clipboard inline (pengawas_detail:100) |
| 5.4#2 | `@import theme.css` dobel | **SUDAH FIX** (`245b4f7`) | `@import` dihapus — theme.css tidak lagi di-fetch dua kali per halaman (login tetap tidak terdampak) |
| 5.4#3 | Toast tersebar | **BERUBAH** (membaik) | showToast kini tersentral: admin-core.js:134-136 (`getElementById('toastContainer')` :135). Sisa: skin CSS download.html:397-486, toast lokal register.html:461-471, cssText ad-hoc — lihat M21 |
| 5.4#4 | Inkonsistensi guard anti-bind ganda | **MASIH ADA** | 3 idiom berbeda hidup bersamaan — lihat L74 |
| H1/H2 | Versi Android lintas instansi / operator | **SUDAH FIX** | c32756b, 3c4fc7d |
| M1–M8 | Temuan Bagian 1–4 (non-UI) | **SUDAH FIX** | 8 commit backend 12 Sep 2026 (M1 4bd8936, M2 9f65e3a, M3+M4 bb21933, M5 7b74e02, M6 018295a, M7 5071014, M8 3c34337) |
| L19 | Ternary dead settings | **MASIH** | settings.html:2166 |
| L20 | `isExpired` jam perangkat | **MASIH** | settings-vouchers.js:69-73 (komentar S73 :70-72) |
| L21 | Teks loading polos | **BERUBAH** (menyempit) | Sisa: settings-packages.js:118-121 'Menyimpan...', settings-vouchers.js:31 'Memuat data voucher...' + :280-281/:325-326/:444-445 'Memuat...' |
| L22 | Raw `data-page` | **BERUBAH** | settings-voucher-audit.js:8-9 |
| L23 | Error state toast-only System Apps | **MASIH** | settings-system-apps.js:37-40 catch + :51-54 tanpa loading state |
| L24 | Inline onclick System Apps | **BERUBAH** (membaik) | settings.html:1916 kini `data-action="app-delete"`; sisa inline hanya settings-system-apps.js:109-112 |
| L25 | openUploadModalSafe jalur ganda | **MASIH** | settings.html:1841-1882 (:1844/:1879) |
| L26 | Probe `#emailEnabledInput` | **MASIH** | settings-general.js:121 |
| L27 | changePasswordModal tanpa `data-action="modal-dismiss"` | **MASIH** | settings.html:1928 vs :988/:1145/:1254/:1368/:1790 |
| L28 | 5 tooltip kustom Billing | **MASIH** | settings.html:927/:931/:941/:955/:959 (terhubung M18) |
| L12–L18 | 7 LOW | **TEKS TAK TERPULIHKAN** | Hitungan dipertahankan (dokumen lama :318) |
| L29–L41 | 13 LOW | **TEKS TAK TERPULIHKAN** | Hitungan dipertahankan (dokumen lama :375) |

### 3.2 Catatan M13 (satu-satunya temuan UI lama yang berubah status menjadi FIX)

Guard double-submit kini ada di cek_hasil.html:64-98 — pola keluarga yang sama dengan forgot_password:96, register_confirm:366-368, reset_password:339. Verbatim §9.2. Perbaikan disertai test `uiux-batch23-cekhasil-guard.test.mjs` (143 baris) dan dikommit sebagai `57260b4` **sebelum** HEAD `3c34337`.

---

## 4. Temuan Baru per Halaman

Format tiap temuan: lokasi `file:line` → bukti terkutip → dampak → rekomendasi. Temuan lintas-file disajikan di §4.4 dan dihitung pada baris "lintas" di rekap (§6); setiap temuan dihitung pada tepat satu baris rekap.

### 4.1 Template Admin

#### login.html — 0 temuan baru

Halaman paling bersih di antara template admin. Yang sudah baik: `lang="id"` (:12), pencegahan auto-zoom iOS `font-size: 16px` (:19-22), atribut `autocomplete` sesuai, toggle password dengan `aria-label` (:74/:79), guard double-submit (:133), dan error Turnstile dengan `role="alert"`. Login juga tidak terdampak duplikasi `@import` (5.4#2).

#### dashboard.html — 0 MEDIUM / 1 LOW baru

**[L47] Literal hex off-token di ikon dan preset warna panel** — LOW

- **Lokasi**: dashboard.html:68 dan :806-811
- **Bukti**: :68 memakai `#fbbf24` langsung. Blok preset 6 tombol `panel-color-set` (:806-811): `:806` Indigo `var(--color-primary)` + `data-color="#6366F1"`, `:807` Emerald `var(--color-success)` (dua tombol ini sudah token), namun `:808` `style` mentah `#F43F5E`, `:809` Amber `var(--color-warning)` tetapi `data-color="#F59E0B"` mentah, `:810` `#06B6D4`, `:811` `#64748B` — keenam tombol membawa hex mentah di `data-color`, dan 3 tombol juga di `style`. Konteks input: `:801` `<input type="color" value="#6361F1">`, `:802` `panelColorHex` `value="#6361F1"`.
- **Dampak**: nilai warna terpisah dari sistem token theme.css — perubahan tema tidak menjalar; rawan drift dengan `--color-*`.
- **Rekomendasi**: simpan `data-color` sebagai token name atau rgb-triplet (`var(--rgb-primary)` dst.) dan turunkan hex hanya di satu titik (JS konversi ke `input[type=color]`).

Catatan temuan lama yang masih hidup di halaman ini: M9/M10/M11 (5 baris token nama lama), 5.4#1 (30 SVG inline, termasuk eye-toggle ×3 :589-592/:601-604/:613-615), dan :1118 masuk sensus literal white (L70, §4.4).

Yang sudah baik: tabel sortable, penutupan modal via Escape, toggle mata password, kemurnian pola `data-action`, `lang="id"` (:11), h1 pertama via sr-only heading (:247, R19).

#### pengawas.html — 0 MEDIUM / 1 LOW baru

**[L48] Literal hex off-token** — LOW

- **Lokasi**: pengawas.html:42, :213, :240-241
- **Bukti**: `#60a5fa` di :42/:213/:240 plus `rgba(59, 130, 246, 0.1/0.2)` dan `#c084fc` di :241.
- **Dampak**: sama dengan L47 — warna tidak ikut sistem token.
- **Rekomendasi**: ganti ke `var(--color-primary-bright)`/`var(--rgb-primary)` dan token aksen yang setara.

Catatan lama: 5.4#1 (7 SVG inline). Yang sudah baik: baris pengawas `role="button"` + `data-action` (:248), double-guard `aria-label` (:249), badge memakai token, `barColor` satu sumber kebenaran, h1 awal (:16).

#### pengawas_detail.html — 0 MEDIUM / 5 LOW baru

**[L43] Kurung tutup liar** — LOW

- **Lokasi**: pengawas_detail.html:492
- **Bukti**: baris :492 hanya berisi `}` tanpa konteks pembuka — sisa refactor.
- **Dampak**: kosmetik/kebingungan pembaca template; tidak mengubah render (Go mengabaikan teks liar dalam HTML), tapi menandakan edit yang belum tuntas.
- **Rekomendasi**: hapus baris.

**[L44] State disabled tombol kontrol hanya dikomunikasikan via `title`** — LOW

- **Lokasi**: pengawas_detail.html:157 (stop) dan :162 (start)
- **Bukti**: :157 — `{{if not .can_control_settings}}disabled title="Hanya pembuat/guru yang dapat menghentikan pengawasan" style="opacity: 0.65; cursor: not-allowed;"{{end}}`; :162 pola identik untuk start.
- **Dampak**: alasan tombol non-aktif hanya muncul saat hover — tidak terbaca pengguna keyboard/layar sentuh; pembaca layar hanya mendengar "disabled" tanpa penjelasan.
- **Rekomendasi**: tambahkan teks/alasan terlihat (mis. baris kecil di bawah tombol) atau `aria-describedby` ke elemen penjelas yang selalu dirender.

**[L45] Toggle "Terima Otomatis" tanpa label terlihat** — LOW

- **Lokasi**: pengawas_detail.html:217-220
- **Bukti**: :217-220 — `<label class="pd-toggle-switch" title="Terima Otomatis — …"> <input type="checkbox" id="autoAcceptToggle" aria-label="Terima otomatis semua peserta yang meminta izin masuk"> <span class="pd-toggle-slider"></span> </label>`
- **Dampak**: label pendamping hanya `title` + `aria-label` — pengguna sighted non-hover tidak melihat nama fungsi toggle di dekatnya (kontras dengan tombol audit :222 yang punya `title` + `aria-label` + teks tombol).
- **Rekomendasi**: render teks label "Terima Otomatis" di samping slider.

**[L49] Literal hex off-token** — LOW

- **Lokasi**: pengawas_detail.html:909 (`#60a5fa`), :1829 (`#6ee7b7`)
- **Dampak/rekomendasi**: sama dengan L48.

**[L76] Tiga `h3` modal mendahului `h1` halaman** — LOW

- **Lokasi**: pengawas_detail.html:24/:43/:56 (modal h3) vs :83 (`<h1 class="pd-exam-title">`)
- **Bukti**: `confirmApprovalModal` h3 :24-26 (dengan `style="font-size:1.1rem;color:var(--color-primary-light);…"`), `accessLogModal` h3 :43, `auditLogModal` h3 :56 — ketiganya dirender di awal `<main>` sebelum h1 judul halaman di :83.
- **Dampak**: urutan heading melompat h3 → h1 → h3; outline halaman tidak monoton naik (navigasi pembaca layar by-heading membingungkan).
- **Rekomendasi**: pindahkan modal ke akhir body (pola yang dipakai settings.html), atau jadikan judul modal `h2`.

Catatan lama: 5.4#1 (11 SVG inline + sprite lokal 11 simbol :2277-2278), literal white ×5 (:594/:866/:875/:891/:1043 — L70). Yang sudah baik: `esc()` konsisten (:1705), polling tiered visibility-aware 5s/12s/30s + anti-overlap (:2243-2273), `encodeURIComponent`/`parseInt` pada input polling, ikon back `#hi-chevron-left` (:69).

#### submissions.html — 1 MEDIUM / 1 LOW baru

**[M24] Tombol identitas siswa lolos dari filter interaksi keyboard** — MEDIUM

- **Lokasi**: submissions.html:280; admin.js:4425 (filter), :4114 (handler)
- **Bukti**: :280 — `<strong class="submission-identity-btn" style="cursor:pointer;" role="button" tabindex="0" data-identity='{{json .IdentityData}}'>{{.StudentName}}</strong>`. Filter global admin.js:4425 hanya mengikat `[data-action][role="button"]` — elemen ini tidak punya `data-action`, sehingga hanya di-cover handler klik langsung :4114 (`closest('.submission-identity-btn')`), tanpa jalur keyboard.
- **Dampak**: pengguna keyboard dapat memfokuskan tombol (tabindex=0, role=button) tapi Enter/Space tidak membuka popup identitas — interaksi ghost.
- **Catatan aman**: XSS tidak menjadi masalah di sini — `{{json .IdentityData}}` diserialisasi oleh FuncMap `webui/cmd/server/main.go:261` (`"json": func(v interface{}) string { b, _ := json.Marshal(v); return string(b) }`), dan isi popup di-escape (`escapeHtml` admin.js:4133/:4141).
- **Rekomendasi**: tambahkan `data-action="identity-open"` (terdaftar di registry `Actions`) sehingga jalur delegasi keyboard global berlaku; hapus handler klik khusus.

**[L46] Informasi MAC hanya via judul hover** — LOW

- **Lokasi**: submissions.html:282
- **Bukti**: sel MAC menampilkan nilai terpotong dengan keterangan penuh hanya pada atribut judul (hover-only).
- **Dampak**: sama dengan L44 — informasi tidak tersedia bagi pengguna keyboard/pembaca layar.
- **Rekomendasi**: `aria-label` penuh pada sel + `title`, atau tampilan detail di popup identitas.

Yang sudah baik: h1 awal (:134), popup identitas dengan `escapeHtml` + click-outside (:4163-4164), token popup `output.css:2734-2748`.

#### partials/nav.html — 1 LOW baru

**[L42] Komentar basi tentang target skip link** — LOW

- **Lokasi**: nav.html:5-8
- **Bukti**: komentar menyatakan target `#mainContent` dan "halaman lain akan menyusul" — padahal seluruh halaman admin kini memakai `id="main-content"`/konsisten (dokumen lama §5.1).
- **Dampak**: menyesatkan maintainer; komentar mengklaim pekerjaan yang sudah selesai.
- **Rekomendasi**: hapus/perbarui komentar.

Catatan: nav:129/:146/:152 masuk sensus literal white (L70). Yang sudah baik: `header` :16 (R19), delegasi klik tunggal, `#hi-logout` :85.

#### admin lintas (admin.js) — 3 MEDIUM / 1 LOW baru

**[M22] Sink `innerHTML` dengan `p.id` mentah (dua lokasi)** — MEDIUM

- **Lokasi**: admin.js:1122 dan :3064
- **Bukti**: :1122 — `opt.innerHTML = '<input type="checkbox" class="pengawas-checkbox" value="' + p.id + '"' + (isChecked ? ' checked' : '') + ' style="accent-color:var(--color-primary-bright);cursor:pointer;"> '`; :3064 memakai pola identik untuk delegate pengawas.
- **Dampak**: `p.id` di-interpolasi mentah ke `innerHTML`. Saat ini id numerik dari server sehingga tidak eksploitatif, tapi ini adalah lubang siap-pakai bila sumber id berubah menjadi string bebas — dan tidak konsisten dengan `username`/`role` di baris yang sama yang sudah di-escape.
- **Rekomendasi**: `escapeHtml(String(p.id))` atau bangun elemen via `document.createElement`.

**[M23] `target="_blank"` tanpa `rel="noopener"` + interpolasi token mentah** — MEDIUM

- **Lokasi**: admin.js:604 (anchor), :603-610 (interpolasi `token`/`examId`)
- **Bukti**: :604 — `<a href="/hasil/${token}" target="_blank" class="pd-action-btn pd-action-link" title="Buka halaman hasil ujian untuk siswa">`; :603-610 membangun string link tanpa `escapeHtml` pada `token`/`examId`.
- **Dampak**: (1) `reverse tabnaback` — halaman hasil bisa memanipulasi `window.opener` tab admin; (2) token mentah berpotensi memutus atribut bila formatnya berubah. Ini **satu-satunya pelanggar** di repo — dashboard.html:534 dan settings.html:1915 keduanya sudah `noopener`.
- **Gap test**: `uiux-batch19-hygiene.test.mjs:79-89` (R146) hanya memindai `settings.html` + `dashboard.html`, tidak `static/js` — pelanggaran ini tak terdeteksi CI.
- **Rekomendasi**: tambah `rel="noopener noreferrer"`, escape variabel, dan perluas test R146 ke `static/js`.

**[M25] Ekspor XML tanpa escaping — round-trip gagal** — MEDIUM

- **Lokasi**: admin.js:1360-1392 (ekspor) vs :2681 (import parsererror)
- **Bukti**: :1361 `partial_scoring="${q.partial_scoring}"`, :1366 `q.choices.join(', ')`, :1379 `q.key.join(', ')`, :1383 `${k}:${v}`, :1390 `<key>${keyStr}</key>` — semua nilai dimasukkan ke string XML tanpa escape (`&`, `<`, `>`, `"`).
- **Dampak**: soal berisi karakter XML (mis. pilihan jawaban "Benar & Salah") menghasilkan XML invalid → import di :2681 menampilkan `parsererror` → data tidak dapat diimpor kembali. Ekspor-impor tidak round-trip.
- **Rekomendasi**: fungsi `xmlEscape()` untuk atribut dan teks (minimum `& < > " '`).

**[L50] Literal `rgba()` padanan hex di JS** — LOW

- **Lokasi**: admin.js:2873-2874, 2878-2879, 2913-2914, 2918-2919
- **Bukti**: :2873 `btn.style.background = 'rgba(16, 185, 129, 0.15)';` :2874 `'rgba(16, 185, 129, 0.3)'`; :2878/:2879 `rgba(239, 68, 68, …)`; :2913/:2914 `rgba(251, 191, 36, …)`; :2918/:2919 `rgba(107, 114, 128, …)`. Menariknya `color` di baris yang sama SUDAH memakai token (:2875 `var(--color-success-light)`, :2880 `var(--color-danger-light)`) — hanya `background`/`border` yang literal.
- **Dampak**: setengah baris token, setengah literal — drift warna saat tema berubah.
- **Rekomendasi**: `rgba(var(--rgb-success), 0.15)` dst. (rgb-triplet token sudah tersedia di theme.css).

Yang sudah baik di admin.js: 53 `Actions.register` + delegasi klik tunggal (admin-core.js:355), `escapeHtml` dominan di seluruh sink lain, Modal Manager generik (admin-core.js:888-1040).

### 4.2 Temuan Baru — Tab Settings

Halaman `settings.html` (2.235 baris) memuat 6 tab; skrip per-tab ada di `webui/static/js/settings-*.js`. Tab System Apps bersih dari temuan baru. Tiga temuan MEDIUM: dua di tab Paket, satu page-level (tooltip).

**[M18] Tooltip merespons hover saja — keyboard tidak pernah melihatnya** — MEDIUM (page-level)

- **Lokasi**: settings.html:314-318
- **Bukti**: :314-318 — `.tooltip:hover::after, .tooltip:hover::before { opacity: 1; visibility: visible; … }` — satu-satunya trigger adalah `:hover`; string `:focus-within` tidak muncul sama sekali di blok tooltip.
- **Dampak**: elemen ber-`.tooltip` (mis. ikon bantuan di form paket/voucher) fokus via Tab tapi tooltip tak muncul — pengguna keyboard kehilangan penjelasan yang sama sekali tidak tersedia di label. Terhubung dengan L28 lama (ikon `:927/:931/:941/:955/:959` ber-`.tooltip`).
- **Rekomendasi**: tambahkan `.tooltip:focus-within::after, .tooltip:focus-within::before` (dan `:focus-visible` pada host) dengan transisi yang sama.

**[L56] `@keyframes spin` didefinisikan dua kali** — LOW

- **Lokasi**: settings.html:526 dan :531
- **Bukti**: :526 `@keyframes spin { … }` di blok style asli; :531 `@keyframes spin { … }` lagi setelah komentar merged :529 — `/* ===== merged styles: templates/admin/system_apps.html ===== */`.
- **Dampak**: duplikat hasil merge template `system_apps.html` ke dalam `settings.html`; definisi kedua menimpa yang pertama secara diam-diam — dua sumber kebenaran untuk animasi spinner.
- **Rekomendasi**: hapus salah satu (idealnya blok asli, pertahankan yang bersekutu dengan konteks merged), atau angkat ke `admin-base.css` yang sudah punya spinner.

#### Tab Paket — 2 MEDIUM baru

**[M16] Catch kegagalan load membiarkan tombol Simpan aktif — save kirim `{"packages":[]}`** — MEDIUM

- **Lokasi**: settings-packages.js:70-75 (catch), :118-128 (savePackages)
- **Bukti**: :70-75 catch `loadPackages` hanya menulis pesan error ke tabel — `packagesTableBody.innerHTML = '<tr><td colspan="7" style="padding:32px;text-align:center;color:var(--color-danger-light);">Gagal memuat pengaturan paket.</td></tr>'` — **tanpa** men-disable tombol Simpan. savePackages :118-128 kemudian membaca state internal (array kosong karena load gagal) dan `btn.disabled = true; btn.textContent = 'Menyimpan...'` sambil mengirim `POST` body `{"packages":[]}` → server menolak: `webui/internal/handlers/admin/packages.go:79-82` memvalidasi "Tidak ada paket yang dikirim" (400).
- **Dampak**: skenario gagal-then-save: operator kehilangan seluruh konfigurasi paket di layar, menekan Simpan, dan menerima 400 tanpa menyadari bahwa payload yang dikirim adalah array kosong — bukan konfigurasi yang terlihat sebelum gagal.
- **Rekomendasi**: disable tombol Simpan di path catch; atau simpan snapshot terakhir sukses dan blok save bila state internal diketahui basi.

**[M17] `packagesTableBody` satu-satunya tabel tab tanpa `aria-live`** — MEDIUM

- **Lokasi**: settings.html:1740 vs :836, :977, :1073, :1132
- **Bukti**: :1740 — `<tbody id="packagesTableBody">` polos. Pembanding (pola emas dalam file yang sama): :836 `<tbody id="usersTableBody" aria-live="polite">`, :977 `<div id="myPackagesList" aria-live="polite">`, :1073 `<tbody id="vouchersTableBody" aria-live="polite">` ✓, :1132 `<tbody id="auditLogsBody" aria-live="polite">`.
- **Dampak**: pembaca layar tidak diumumkan ketika isi tabel paket diganti hasil fetch (termasuk pesan error :70-75 di atas); 4 dari 5 region data serupa sudah live — inkonsistensi, bukan ketidaksadaran pola.
- **Rekomendasi**: tambahkan `aria-live="polite"` (dan `aria-busy` selama fetch) pada `packagesTableBody`, samakan dengan 4 region lain.

#### Tab Voucher — 3 LOW baru

**[L51] Header tabel voucher tidak sortable — inkonsisten dengan tab Users** — LOW

- **Lokasi**: settings.html:1064-1070 vs :825-832
- **Bukti**: :1064-1070 tujuh `<th scope="col" style="padding:16px 20px;">Kode</th>` … polos (tanpa `sortable`/`data-sort`/`tabindex`). Pembanding tab users :825-832 — `<th scope="col" class="sortable" data-sort="username" …>Username</th>` lengkap dengan indikator.
- **Dampak**: tabel voucher besar (ratusan baris) tidak bisa diurutkan, sementara pola sortable sudah terimplementasi penuh satu tab sebelah.
- **Rekomendasi**: adopsi pola `class="sortable" data-sort="kode"` + ikon arah, plus `aria-sort` dinamis seperti users.

**[L52] `wireVoucherRowActions` memasang listener sendiri — melewati registry `Actions`** — LOW

- **Lokasi**: settings-vouchers.js:135-154
- **Bukti**: :137-138 — `if (!tbody || tbody.dataset.rowActionsWired) return; tbody.dataset.rowActionsWired = '1';` — lalu memasang listener klik tbody sendiri untuk aksi baris voucher; bandingkan konvensi repo S3 :132-134 (elemen aksi diberi `data-action`, delegasi tunggal admin-core.js:355 yang menangkapnya).
- **Dampak**: dua sistem delegasi hidup berdampingan (registry `Actions` 53 registrasi vs wiring manual ini); aksi voucher tidak tercatat di registry — konsistensi arsitektur dan auditabilitas berkurang.
- **Rekomendasi**: migrasikan aksi baris voucher ke `data-action` + `Actions.register`, hapus wiring manual (idiom dataset-flag bisa ikut dihapus).

**[L55] Null-guard hilang pada dua input pencarian Settings** — LOW

- **Lokasi**: settings-vouchers.js:28; settings-voucher-audit.js:18
- **Bukti**: :28 — `const search = document.getElementById('searchVoucher').value.trim();` (guard `if (tbody)` baru muncul di :29, terlambat untuk deref `.value` di :28); :18 — pola sama pada `auditSearchInput` (guard di :19; konvensi S78 :13-14 memangkas input lebih dulu).
- **Dampak**: bila markup diubah/id diganti, TypeError saat handler input pertama menyala — bukan crash saat load, sehingga tidak terdeteksi smoke-test.
- **Rekomendasi**: `const el = document.getElementById(...); if (!el) return;` di awal kedua handler (samakan dengan pola S78).

#### Tab Billing — 2 LOW baru

**[L53] Modal ditampilkan via `style.display='flex'` — melewati Modal Manager** — LOW

- **Lokasi**: settings-billing.js:18
- **Bukti**: :18 — `getElementById('confirmRedeemModal').style.display='flex'` — langsung memanipulasi display, tanpa `Modal.open` (Manager generik admin-core.js:888-1040 yang mengurus focus-trap, Escape, dan overlay).
- **Dampak**: modal redeem tidak mendapat focus-trap/Escape/overlay-close gratis dari Manager; pola berbeda dari modal lain di aplikasi.
- **Rekomendasi**: ganti ke `Modal.open('confirmRedeemModal')`, hapus manipulasi display manual.

**[L57] Fallback `max_concurrent_exams` menulis `1` secara diam-diam** — LOW

- **Lokasi**: settings-billing.js:149
- **Bukti**: :149 — `(p.max_concurrent_exams || p.max_exams || 1)` — bila kedua field kosong/null, nilai render `1`, bukan tanda bahwa data tidak tersedia.
- **Dampak**: operator melihat "1" dan tidak bisa membedakan konfigurasi asli dari placeholder; fallback terselubung.
- **Rekomendasi**: tampilkan `—` bila kedua field kosong, dan hanya jatuh ke nilai numerik bila benar-benar terdefinisi.

#### Tab Umum & Users — 2 LOW baru

**[L54] Guard `data-action` hilang di handler collapse Tab Umum** — LOW

- **Lokasi**: settings-general.js:16-26 vs settings-users.js:44-47
- **Bukti**: settings-users.js:44-47 punya — `if (e && e.target && e.target.closest && e.target.closest('[data-action]')) return;` (komentar S108 :44-46) sebelum memasang listeners collapse :50-51. settings-general.js:16-26 memasang handler collapse serupa **tanpa** guard tersebut.
- **Dampak**: klik elemen `data-action` yang kebetulan berada dalam area collapse bisa memicu toggle collapse bersamaan dengan aksi utamanya — dua efek sekali klik.
- **Rekomendasi**: salin guard S108 ke settings-general.js:16-26 (atau angkat ke helper bersama).

**[L58] Rotasi ikon accordion via `style.transform` inline** — LOW

- **Lokasi**: settings-general.js:129; settings-users.js:102
- **Bukti**: keduanya — `icon.style.transform = expand ? 'rotate(0deg)' : 'rotate(180deg)'` — menulis transform string secara manual.
- **Dampak**: nilai transform di-hardcode di 2 file JS; tidak bisa ditimpa tema/transisi terpusat; pola CSS class (`.collapsed .icon { transform: … }`) sudah tersedia di admin-base.
- **Rekomendasi**: pindahkan rotasi ke kelas CSS (toggle class pada parent), JS cukup `classList.toggle`.

#### Tab System Apps — 0 temuan baru

Status lama tetap berlaku: **L23 MASIH** (`:37-40/:51-54`), **L24 BERUBAH** — `settings.html:1916` kini memakai `data-action`, sisa pelanggar hanya `settings-system-apps.js:109-112`; **L25 MASIH** (`:1841-1882`).

#### Status lama tab-tab lain

- **Tab Umum**: **L19 MASIH** (settings:2166); **L21 BERUBAH** (sisa: packages.js:118-121 `'Menyimpan...'`, vouchers :31/:280-281/:325-326/:444-445; voucher-audit:8-9 kini bersih); **L22 BERUBAH** (voucher-audit:8-9 bersih); **L27 MASIH** (:1928).
- **Tab Voucher**: **L20 MASIH** (vouchers:69-73).
- **Tab Users**: **L26 MASIH** (:121).
- **Tema/tooltip**: **L28 MASIH** (:927/:931/:941/:955/:959 — terhubung M18).
- **M12 MASIH** (settings.html:22-:558; `#6ee7b7` ×8, `#93c5fd` :1446, `#1e1b4b` :462; settings-vouchers.js:75/:80/:82/:106/:110/:121; settings-billing.js:132/:139).

Yang sudah baik di Settings: guard race `voucherLoadSeq` (settings-vouchers.js:25) & `auditLoadSeq` (voucher-audit:13-16), pre-check disk-space sebelum save paket (packages.js:98-116), caption `sr-only` (:1060-1061), state retry `voucher-retry-load` (:18-19) + `billing-packages-retry` (:163-164), tabel users ARIA emas (:836), `aria-live` voucher (:1073).

### 4.3 Temuan Baru — Halaman Publik

Sembilan file di `webui/templates/public/` — delapan halaman utuh plus partial `shared.html`. Dua halaman bersih dari temuan baru: **index** (catatan: :36 masuk sensus L70 — dihitung di baris lintas, §4.4) dan **forgot_password** (0/0/0). **cek_hasil** menyumbang satu LOW (L67); M13 sudah FIXED, lihat §3.2.

#### cek_hasil.html — 1 LOW baru (M13 sudah FIXED)

M13 (guard double-submit) sudah diperbaikan di commit 57260b4 — skrip verbatim kini `:64-98`, lampiran §9.2. Urutan skip-link halaman ini **benar** (:25-27 — `public_skip_link` sebelum `public_auth_nav`), sehingga M19 tidak berlaku di sini; M19 kini hanya menyangkut hasil.html (§4.4). L67 di bawah, yang di-anchor di halaman ini agar terhitung tepat satu kali di rekap.

**[L67] Pola `role="alert"` inkonsisten: wrapper vs elemen** — LOW

- **Lokasi**: cek_hasil.html:39 (wrapper) vs register.html:240/:309/:316, register_confirm.html:218, reset_password.html:171 (elemen)
- **Bukti**: cek_hasil:38-42 — `{{if .error}}` … `<div class="flash-messages" role="alert">` membungkus `<div class="flash-msg flash-error">{{.error}}</div>` … `{{end}}` (role di **wrapper**). register:240 — `role="alert"` langsung di elemen pesan (+SVG :241); :309 (`pwMatchError`)/:316 (`turnstileError`), register_confirm:218 (+SVG :219), reset_password:171 — `<span id="pwMatchError" role="alert" style="display:none;font-size:12px;color:var(--color-danger-bright);margin-top:6px;">Password tidak cocok</span>` (role di **elemen**).
- **Dampak**: dua konvensi bersanding — di halaman wrapper pembaca layar mengumumkan region, di halaman lain elemen; perilaku pengumuman bisa berbeda halus antar halaman bila markup turunan berubah (mis. beberapa pesan dalam satu wrapper hanya diumumkan sekali).
- **Rekomendasi**: standardisasi satu pola (rekomendasi: role di elemen pesan tunggal, sesuai mayoritas), dokumentasikan di konvensi.

#### register_confirm.html — 1 LOW baru

**[L65] Blok CSS OTP terduplikasi penuh dengan reset_password** — LOW

- **Lokasi**: register_confirm.html:82-114 ↔ reset_password.html:23-60
- **Bukti**: ~33 baris CSS OTP 6-box identik di kedua file (kelas `.otp-box`, `.otp-grid`, dsb.) — termasuk komentar yang ikut tersalin dengan **drift** :85 (komentar menyebut halaman lain, bukan halaman ini).
- **Dampak**: perbaikan pada satu salinan (mis. kontras border error) tidak menular ke salinan lain; komentar salah konteks memperparah.
- **Rekomendasi**: angkat ke `public-mobile.css`/kelas bersama (satu sumber), hapus kedua salinan lokal.

#### register.html — 1 LOW baru

**[L66] Blok pw-strength terduplikasi dengan reset_password** — LOW

- **Lokasi**: register.html:49-70 ↔ reset_password.html:64-88
- **Bukti**: ~25 baris CSS meter kekuatan password (`.pw-meter`, `.pw-bar`, dsb.) identik antar kedua file.
- **Dampak**: sama dengan L65 — dua sumber kebenaran untuk elemen yang berperilaku identik; koreksi tema hanya menyentuh satu.
- **Rekomendasi**: satu sumber (shared partial `<style>` atau file CSS publik bersama).

#### reset_password.html — 1 LOW baru

**[L68] `checkMatch` hanya memantau field konfirmasi** — LOW

- **Lokasi**: reset_password.html:275-282
- **Bukti**: :282 — listener input hanya pada `confirmField` (`confirmField.addEventListener('input', checkMatch)`); `checkMatch` :275-282 membandingkan `pwField` vs `confirmField`, tapi perubahan pada `pwField` tidak memicu apa pun.
- **Dampak**: bila pengguna mengisi konfirmasi dulu lalu mengubah password utama, pesan "Password tidak cocok" tidak muncul/diperbarui hingga pengguna menyentuh konfirmasi lagi. (Kontras: register.html:399-416 — R22 — memantau kedua field.)
- **Rekomendasi**: pasang listener pada kedua field (port penuh R22).

#### hasil.html — 2 MEDIUM / 3 LOW baru

**[M20] Baris tabel skor diklik via `tr.onclick` — melanggar pola S59 yang halaman ini sendiri dokumentasikan** — MEDIUM

- **Lokasi**: hasil.html:605-615, :905 (pagination); komentar pola :180-183, tab-nav yang mematuhinya :185
- **Bukti**: :605 — `tr.onclick = () => toggleDetailScores(sub.id);` — handler per baris via properti onclick; :606 — `tr.style.cursor = 'pointer';`; :607-610 — `tr.setAttribute('tabindex', '0')` / `('role', 'button')` / `('aria-expanded', 'false')` / `('aria-label', 'Lihat rincian jawaban ' + sub.student_name)`; :611-615 — keydown Enter/Space. :905 — pagination juga `btn.onclick = () => goToPage(page);` per tombol. Halaman ini sendiri mendokumentasikan pola repo di :180-183 — "S59: klik dilayani registry Actions admin-core.js (tanpa onclick inline)" — dan tab-nav :185 mematuhinya via `data-action="switch-tab"`; :605/:905 melanggar pola yang halamannya sendiri tulis.
- **Dampak**: puluhan/ratusan handler terpasang per render (vs 1 delegasi); aksi di luar registry `Actions` tidak teraudit — ironis karena halaman sendiri mendokumentasikan S59; sisi a11y justru sudah tertangani (`role="button"` + Enter/Space :611-615), ketidaksesuaian murni pada sistem `data-action` repo.
- **Rekomendasi**: delegasi tunggal di container dengan `data-action="toggle-detail"` (registry tersedia — download/hasil sudah memuat admin-core.js).

**[M26] Sprite ikon lokal 19 simbol — 13 duplikat dengan sprite admin, 2 mati lokal** — MEDIUM

- **Lokasi**: hasil.html:74-95 ↔ webui/templates/admin/partials/svg-symbols.html
- **Bukti**: :74 komentar `<!-- Heroicons Sprite -->` + :75 `<svg … style="display:none;" aria-hidden="true">` membuka sprite lokal berisi 19 `<symbol>` (:76-94 — `hi-results` :76, `hi-key` :77, `hi-users` :78, `hi-dashboard` :79, `hi-eye` :80, `hi-lock` :81, `hi-arrow-left` :82, `hi-trending-up` :83, `hi-magnifying-glass` :84, `hi-arrow-path` :85, `hi-chevron-down` :86, `hi-clock` :87, `hi-paper-airplane` :88, `hi-check` :89, `hi-exclamation-triangle` :90, `hi-x` :91, `hi-chevron-left` :92, `hi-chevron-right` :93, `hi-exclamation` :94). Hasil audit: **2 mati lokal** — `hi-eye` :80 dan `hi-paper-airplane` :88 (nol referensi di halaman); **13 ID duplikat** dengan sprite admin — 12 byte-identik, plus `hi-chevron-left` :92 (`<path d="m15.75 19.5-7.5-7.5 7.5-7.5"/>`, bentuk relatif) vs admin :24 (bentuk absolut) — geometri sama beda sintaks; di luar itu `hi-magnifying-glass` :84 byte-identik dengan `hi-search` admin :7 (rename murni), dan `hi-arrow-path` :85 vs `hi-refresh` admin (dua varian resmi Heroicons — dua-duanya sah, tapi kini dua nama untuk dua varian di dua sprite).
- **Dampak**: 14 dari 19 simbol punya kembaran konten-identik (13 duplikat ID + 1 pasangan rename) — setiap perbaikan path/gaya ikon harus dikerjakan di dua sprite; risiko drift visual antar area admin dan publik; 2 simbol mati membebani definisi tanpa konsumen.
- **Rekomendasi**: pakai partial sprite admin (`svg-symbols.html`, sudah dimuat 5 halaman admin) untuk ID yang duplikat, pertahankan simbol yang benar-benar unik untuk hasil, hapus `hi-eye`/`hi-paper-airplane`.

**[L59] Input pencarian siswa `type="text"`, bukan `type="search"`** — LOW

- **Lokasi**: hasil.html:197
- **Bukti**: :197 — `<input type="text" class="search-input" id="searchInput" placeholder="Cari berdasarkan nama siswa..." aria-label="Cari nama siswa">` — bukan `type="search"` (T21 :200-201 sudah menangani tombol clear; `search-clear-btn` punya target 44px R106).
- **Dampak**: tanpa `type="search"`, mobile browser tidak menawarkan tombol `Search` di keyboard virtual, dan tombol clear native tidak muncul di browser desktop.
- **Rekomendasi**: `type="search"` + pastikan `enterkeyhint` konsisten.

**[L60] `renderStats` menulis token warna inline per-statistik** — LOW

- **Lokasi**: hasil.html:542-559
- **Bukti**: :544 `style="color: var(--color-success);"`, :548 `var(--color-warning)`, :552 `var(--color-danger)`, :556 `var(--color-accent-light)"` — markup kartu statistik dibangun via `innerHTML` dengan style inline ber-token. (Konteks: early-return :522-527; textContent dipakai di :533-538 untuk angka.)
- **Dampak**: token sudah dipakai (bukan hex), tapi pola inline menyebar konsumsi token ke JS — ganti nama token tidak terdeteksi grep template; kartu tidak ikut restyle CSS.
- **Rekomendasi**: kelas semantik (`.stat-success` dst.) di hasil.css yang memetakan ke token.

**[L61] Dua literal hex off-token di hasil.css** — LOW

- **Lokasi**: hasil.css:612-613
- **Bukti**: :612 `#e2e8f0` / :613 `#d97706` (medali peringkat) — tidak melalui token `--color-*`.
- **Dampak**: drift tema; medali tidak ikut berubah bila palet disesuaikan.
- **Rekomendasi**: petakan ke token (atau buat `--color-medal-*` bila belum ada padanan).

#### download.html — 3 LOW baru

**[L62] Literal hex/rgba off-token di blok unduhan** — LOW

- **Lokasi**: download.html:161, :206-207, :538-540, :638; deretan kecil :130, :676, :824
- **Bukti**: :161 — `.icon-windows { background: rgba(59, 130, 246, 0.15); color: #3b82f6; border: 1px solid rgba(59, 130, 246, 0.3); }` — padahal tetangganya :160/:162 (`.icon-android`/`.icon-linux`) memakai `var(--rgb-success)`/`var(--rgb-warning)`; :206-207 — `background: linear-gradient(135deg, var(--color-success), #059669);` + `color: #04120c;` (badge versi baru — gradien setengah token, setengah hex); :538-540 — `style="border-color: rgba(139,92,246,0.35); background: rgba(139,92,246,0.06);"` + `style="color:#c4b5fd;"` (flavor-box "APK Rilis Resmi" — ungu `#8b5cf6`/`#c4b5fd` tanpa token); :638 — `<code style="background: rgba(var(--rgb-black),0.4); color: var(--color-primary-bright); … border: 1px solid rgba(129,140,248,0.2);">http://192.168.1.10:8080</code>` — `rgba(129,140,248,…)` adalah bentuk literal dari token `--color-primary-bright: #818cf8` (theme.css:20), ditulis tangan di border. Deretan kecil: :130 `rgba(17,24,39,0.5)`, :676 & :824 `rgba(129,140,248,0.2)`.
- **Dampak**: area download adalah etalase publik — warna literal tidak ikut berubah saat palet token disesuaikan; `rgba(129,140,248,…)` menduplikasi `--color-primary-bright` tanpa jejak grep (mengganti token tidak menemukannya); trio ikon platform tidak konsisten: dua token, satu literal.
- **Rekomendasi**: petakan ke token rgb pendamping (mis. tambah `--rgb-primary-bright`), konsistenkan trio ikon platform, audit ulang :130/:676/:824.

**[L63] Tiga empty-state duplikatif dengan styling inline panjang** — LOW

- **Lokasi**: download.html:584-587, :726-729, :805-808
- **Bukti**: :584-587 — `<div style="text-align: center; padding: 60px 20px; color: var(--color-text-muted); background: rgba(var(--rgb-black),0.15); border-radius: 16px; border: 1px dashed rgba(var(--rgb-white),0.05);">` + `:586 <p style="font-size:15px; font-weight:600;">Belum ada installer Android (APK) yang tersedia di sistem.</p>` — :726-729 dan :805-808 mengulang blok identik (hanya teks berbeda).
- **Dampak**: satu kelas CSS (`.empty-state`) akan menggantikan 3×~90 karakter inline; perubahan gaya menuntut 3 edit.
- **Rekomendasi**: kelas `empty-state` di CSS publik bersama; teks tetap per-templating.

**[L64] `noscript` membuka `.reveal` tapi tidak `.platform-section`** — LOW

- **Lokasi**: download.html:120-123 (CSS), :124-127 (keyframes), :487 (noscript)
- **Bukti**: :120-123 — `.platform-section { display: none; animation: fadeIn 0.5s cubic-bezier(0.16, 1, 0.3, 1) forwards; }` + :124-127 `@keyframes fadeIn` — section platform disembunyikan default dan dibuka oleh JS. noscript :487 hanya merestore `.reveal` — **tidak** membuka `.platform-section`; tanpa JS, konten Android (:522 inline `display:block`), Windows (:689), Linux (:768) tetap tersembunyi.
- **Dampak**: pengguna tanpa JS (dan crawler ringan) melihat halaman download tanpa satu pun installer — konten inti halaman hilang.
- **Rekomendasi**: tambahkan `<noscript><style>.platform-section { display: block; }</style></noscript>` (dan hapus `display:none` default; animasikan via JS class bila perlu).

### 4.4 Temuan Lintas Halaman (Public) — 2 MEDIUM / 7 LOW baru

Temuan bagian ini bersifat lintas-berkas: pola yang muncul di lebih dari satu halaman publik, atau yang berakar pada berkas bersama (theme.css, sprite SVG, partial shared.html) sehingga tidak bisa dilokalkan ke satu halaman.

**[M19] `hasil.html`: urutan `public_nav` sebelum `public_skip_link` — skip-link menjadi elemen non-interaktif pertama kedua** — MEDIUM

- **Lokasi**: hasil.html:71-72
- **Bukti**: :72 `{{ template "public_skip_link" . }}` diletakkan **setelah** :71 `{{ template "public_nav" . }}` (urutan aktual di berkas: nav :71, lalu skip-link :72; baris :73 adalah host toast hardcode — lihat M21). Pembanding halaman auth yang **benar**: cek_hasil.html:25-27 — `{{ template "public_skip_link" . }}` **sebelum** `{{ template "public_auth_nav" . }}`; register.html:202-204, forgot_password.html:30-31, register_confirm, reset_password sama-sama benar (skip-link dulu, nav kemudian). Halaman index/download memakai `public_head` (shared.html:886→:893) yang urutannya benar.
- **Dampak**: skip-link ("Lewati ke konten utama") hanya berguna bila ia adalah elemen non-interaktif pertama di DOM — keyboard user menekan Tab pertama dan langsung melompat ke main content. Diletakkan setelah nav, Tab pertama malah jatuh ke link nav (7+ langkah ekstra keyboard sebelum skip-link bisa diaktifkan, dan urutan tab jadi tidak mencerminkan prioritas aksesibilitas).
- **Anomali**: commit `ab64bd9` (sesi fix paralel) mengklaim "urutan skip-link (M17-M20)" dan membetulkan seluruh halaman auth — namun hasil.html:71-72 **terlewat** dan masih terbalik pada snapshot 2026-09-12 ini. M19 kini scoped ke hasil.html saja (dari 5 halaman saat pertama ditemukan).
- **Rekomendasi**: tukar dua baris tersebut agar `public_skip_link` mendahului `public_nav`, konsisten dengan cek_hasil.html:25-27.

**[M21] Implementasi toast terduplikasi — satu `showToast` sentral, tapi host & skin tersebar** — MEDIUM *(FIX PARSIAL `3305d0d`)*

- **Status**: **register.html TERFIX** (`3305d0d`) — :463 komentar M21 + :466 kini memakai `showToast` sentral; partial bersama `public_toast_host` (shared.html:904-906, host `:905`) kini dikonsumsi cek_hasil.html:27, forgot_password.html:32, register.html:204, register_confirm.html:188; `showToast` sentral di admin-core.js:134-136 (dimuat download.html:2 & hasil.html:68).
- **Sisa aktif (dua halaman + satu komentar)**:
  - (a) **download.html:397-486** — blok `<style>` toast ad-hoc ~90 baris (`.toast-container`, `.toast-item`, varian warna, keyframes) masih ada. **Catatan penting**: komentar download.html:400 menyatakan "halaman unduhan tidak memuat `output.css`" — verifikasi grep membuktikan komentar itu **akurat** (satu-satunya kemunculan "output.css" di download.html adalah komentar itu sendiri). Jadi duplikasi CSS toast di download adalah keputusan sadar (skin toast diambil dari output.css yang tidak dimuat), bukan kelalaian salin-tempel murni.
  - (b) **hasil.html:73** — host toast masih hardcode `<div class="toast-container" id="toastContainer" aria-live="polite" aria-atomic="true"></div>`, padahal hasil.html **memuat** output.css (:24) sehingga tinggal diganti `{{ template "public_toast_host" . }}` — trivial.
  - (c) **shared.html:899** — komentar "skin dari tailwind/output.css (sudah dimuat semua halaman publik)" kini **tidak akurat** untuk download (lihat (a)).
- **Dampak**: dua sumber skin toast → perbaikan gaya toast tidak sampai ke download (atau sebaliknya); host hardcode di hasil menduplikasi partial.
- **Rekomendasi**: (1) hasil.html:73 → partial `public_toast_host`; (2) untuk download, pilih salah satu: muat output.css (berat, membatalkan keputusan eksisting) **atau** pindahkan skin toast kecil (`.toast-*`) ke CSS bersama ringan yang dimuat semua halaman publik; (3) perbarui komentar shared.html:899.

**[M27] `public-desktop.css` menimpa layout `hasil.html` via override load-order** — MEDIUM *(FIX `e86ecda`)*

- **Status**: **SUDAH DIPERBAIKI** — public-desktop.css:86-88 kini berisi komentar: "M27: rule .main DIHAPUS — selektor ini dipakai HANYA oleh hasil.html, yang mendefinisikan layout-nya sendiri di hasil.css (max-width 1100px, padding-top calc(var(--public-nav-height) + 24px) untuk clearing nav fixed). Override di sini menang lewat load-order (public-desktop.css dimuat setelah hasil.css) dan merusak padding clearing nav." hasil.css:72 `max-width: 1100px` kini satu-satunya sumber kebenaran layout hasil. Dipertahankan di dokumen sebagai temuan terverifikasi-fix untuk jejak audit.

**[L69] Tabrakan nilai z-index token theme.css** — LOW

- **Lokasi**: theme.css:125-135
- **Bukti**: :125 `--z-topbar-floating: 10002;` vs :127(±) `--z-toast: 10002;` — dua layer beda fungsi berbagi nilai sama; :129(±) `--z-hint: 9998;` vs :131(±) `--z-skip-link: 9998;`. (Verifikasi nilai persis saat perbaikan; pasangan tabrakan inilah inti temuan.)
- **Dampak**: toast yang muncul saat topbar floating aktif tidak dijamin berada di atasnya (spec: elemen lebih akhir di stacking context menang — kebetulan benar, tapi rapuh); hint vs skip-link sama.
- **Rekomendasi**: pisahkan nilai (toast ≥ topbar+1, skip-link < hint atau sebaliknya sesuai desain), tambahkan komentar skala z di theme.css.

**[L70] 28 literal warna putih bypass token** — LOW

- **Lokasi** (census): download.html ×10, shared.html ×6, pengawas_detail.html ×5, nav.html ×3, hasil.html ×2, index.html ×1, dashboard.html ×1 — total 28 kemunculan literal `#fff`/`#ffffff`/`rgba(255,255,255,…)`.
- **Bukti**: contoh — shared.html (skin kartu publik) memakai `rgba(255,255,255,…)` langsung alih-alih `var(--rgb-white)` yang tersedia di theme.css.
- **Dampak**: perubahan palet (`--rgb-white`) tidak menjangkau 28 titik ini; konsistensi tema retak diam-diam.
- **Rekomendasi**: substitusi mekanis ke `var(--rgb-white)`/`rgba(var(--rgb-white), α)`; census lengkap ada di transkrip review.

**[L71] admin-base.css `calc(var(--spacing) * 2)` bergantung token Tailwind yang tidak dijamin dimuat** — LOW

- **Lokasi**: admin-base.css:995
- **Bukti**: `calc(var(--spacing) * 2)` — `--spacing` didefinisikan di tailwind/output.css:19, yang hanya dimuat halaman yang meng-include-nya; admin-base.css dipakai halaman admin (yang memuat output.css) sehingga selamat hari ini, tapi ketergantungan silang tak terdokumentasi.
- **Dampak**: bila output.css tidak dimuat (halaman admin baru yang lupa), spacing jatuh ke `calc(0 * 2)`-style kegagalan senyap.
- **Rekomendasi**: definisikan `--spacing-admin` di theme.css atau fallback `var(--spacing, 0.25rem)`.

**[L72] Tiga token theme.css tanpa konsumen** — LOW

- **Lokasi**: theme.css:12, :16, :80
- **Bukti**: `--color-surface-hover` (:12), `--color-primary-dark` (:16), `--radius-xl` (:80) — grep seluruh templates/static/css menemukan nol konsumsi.
- **Dampak**: token mati menambah beban kognitif sistem desain; pembaca mengira ada konsumen yang harus ikut di-restyle.
- **Rekomendasi**: hapus, atau adopsi bila memang direncanakan (dokumentasikan).

**[L73] ±13 kelas dead di admin-base.css** — LOW

- **Lokasi**: admin-base.css — antara lain :158 (`.skeleton-inline` dst.), `.shortcuts-hint`, `.sticky-toolbar`, `.circular-progress`, `.mobile-bottom-bar`, :475-481 (`.clickable-row`), :942 (`.tone-danger`), :944 (`.tone-accent`), dsb. (census 13 kelas: 0 kemunculan di templates maupun JS).
- **Dampak**: CSS mati menambah ukuran berkas dan noise audit; beri komentar "deprecated" membingungkan lebih lanjut.
- **Rekomendasi**: hapus dead class; jadikan census bagian pemeriksaan CI bila ingin mencegah kambuh.

**[L74] Empat idiom "bind-once" `dataset` sebagai flag state** — LOW

- **Lokasi**: settings-system-apps.js:183-184 (`closeGuardWired`), settings-general.js:250-251 (`dirtyWired`), settings-vouchers.js:137-138 (`rowActionsWired`), :482-483 (`retryWired`)
- **Bukti**: pola `if (!el.dataset.xxxWired) { el.dataset.xxxWired = '1'; el.addEventListener(…) }` — flag di DOM dipakai sebagai penanda bahwa listener sudah dipasang. Ini idiom delegasi-manual; empat situs menyalin pola yang sama.
- **Dampak**: state wiring tersebar di DOM — sulit di-debug (tidak terlihat di snapshot state), dan duplikasi empat kali menandakan helper `wireOnce(el, key, fn)` layak diekstrak (atau migrasi ke registry `Actions` yang sudah ada).
- **Rekomendasi**: helper tunggal atau delegasi via registry; hapus flag dataset.

**[L75] Simbol sprite SVG tanpa referensi runtime — census ulang 45 simbol** — LOW

- **Lokasi**: partials/svg-symbols.html (45 simbol; baseline `3c34337` = 42, +3 baru dari `e86ecda`: `hi-arrow-left`, `hi-trending-up`, `hi-exclamation-triangle` — ketiganya ter-referensi di hasil.html).
- **Bukti**: census `grep '#hi-*'` atas `webui/templates` + `webui/static/js` menemukan **40 referensi unik** (satu entri `#hi-` adalah artefak pattern parsial, abaikan) → **5 simbol tanpa referensi runtime**: `hi-arrow-up`, `hi-banknotes`, `hi-check-circle`, `hi-copy`, `hi-x-circle`. (Census lama berbasis 42 simbol menghasilkan angka sama 5 dead + catatan 5 simbol hanya dipakai via injeksi JS — `hi-exclamation`, `hi-eye-off`, `hi-information`, `hi-lock-open`, `hi-star` — keduanya konsisten: hanya 5 dead.)
- **Dampak**: 5 definisi `<symbol>` (~1 KB) di-parse setiap halaman tanpa pernah dirender; menambah waktu parse & ukuran partial.
- **Rekomendasi**: hapus 5 simbol dead, atau beri marker komentar sementara bila ada rencana pemakaian.
- **Catatan metodologi**: census lama di review sebelumnya menggunakan kombinasi grep yang gagal senyap (0 match) — angka final di atas berasal dari census ulang dengan normalisasi format (`#`-prefix) yang benar.

## 5. Yang Sudah Bagus

Hal-hal berikut sudah benar dan layak dipertahankan sebagai pola rujukan (bukan daftar wishful — semuanya diverifikasi langsung di berkas):

1. **Sprite SVG via partial tunggal** — semua ikon admin/public di-serve dari `partials/svg-symbols.html` (45 simbol) dengan `<use href="#hi-…">`; tidak ada inline SVG path duplikat per halaman setelah `e86ecda`.
2. **Token warna sebagai sumber kebenaran** — mayoritas warna halaman publik & admin sudah melalui `--color-*`/`--rgb-*` theme.css; temuan L61/L62/L70 adalah sisa, bukan keadaan umum.
3. **Pola stylesheet terkurasi** — `R71` (link eksternal sebelum style inline, urutan antar-link dipertahankan) konsisten di seluruh halaman publik (cek_hasil.html:14, register.html:18, dst.); komentar `R130`/`S34`/`S32` menandai keputusan review lampau secara jelas — jejak dokumentasi dalam kode yang sangat membantu.
4. **`public_toast_host` + `showToast` sentral** — infrastruktur toast bersama sudah ada (shared.html:904-906, admin-core.js:134-136) dan kini dikonsumsi 4 halaman auth; arsitektur target M21 tinggal dijangkau di 2 halaman sisa.
5. **Guard anti double-submit konsisten** — cek_hasil (:81-88), register_confirm (:366-368), reset_password (:339) memakai pola disable+loading yang sama, plus restore `pageshow persisted` (cek_hasil :90-96) — detail bfcache yang jarang diurus.
6. **Urutan skip-link benar di semua halaman auth** — cek_hasil :25-27, register :202-204, forgot :30-31 — template `public_skip_link` sebelum nav; hasil.html adalah satu-satunya penyimpangan (M19).
7. **`role="alert"` konsisten di elemen flash** — setelah `90ca7de`, seluruh flash message publik menaruh `role="alert"` pada elemen flash itu sendiri (bukan pembungkus), pola seragam di 6 halaman auth.
8. **Registry `Actions` untuk interaksi dinamis** — hasil.html (:392 `toggle-score-detail`, :396 `page-goto`) dan submissions (`identity-open`) memakai event-delegation registry alih-alih properti `.onclick` per elemen — skalabel dan mudah diaudit.
9. **Fail-safe muat data** — settings-packages.js setelah `71c19ec`: kegagalan fetch Paket menonaktifkan tombol Simpan + toast eksplisit (M16 fix) — pola yang layak ditiru tab settings lain.
10. **Fokus keyboard pada tooltip** — `.tooltip:focus-within` (settings.html:317-318) setelah `ab64bd9` — komponen hint ikut dapat jalur keyboard, bukan hanya hover.

## 6. Rekap Temuan per Halaman

Kolom **Status** = hasil verifikasi ulang 2026-09-12 terhadap working tree pasca sesi fix paralel (commit di kolom = fix terverifikasi substansi, bukan sekadar klaim pesan commit).

| Halaman / Berkas | MEDIUM | LOW | Status fix |
|---|---|---|---|
| login.html | 0 | 0 | — |
| index.html (beranda) | 0 | 0 | — |
| dashboard.html | 0 | 1 (L47) | L47 MASIH |
| pengawas.html | 0 | 1 (L48) | L48 MASIH |
| pengawas_detail.html | 0 | 5 (L43, L44, L45, L49, L76) | semua MASIH |
| submissions.html | 1 (M24) | 1 (L46) | M24 **FIX `594c675`**; L46 MASIH |
| nav.html (partial) | 0 | 1 (L42) | L42 MASIH |
| admin.js | 3 (M22, M23, M25) | 1 (L50) | M22/M23/M25 **FIX `428568c`**; L50 MASIH |
| settings.html (level halaman) | 1 (M18) | 1 (L56) | M18 **FIX `ab64bd9`**; L56 **FIX `90ca7de`** |
| settings — Tab Paket | 2 (M16, M17) | 0 | M16 **FIX `71c19ec`**; M17 **FIX `ab64bd9`** |
| settings — Tab Voucher | 0 | 3 (L51, L52, L55) | semua MASIH |
| settings — Tab Billing | 0 | 2 (L53, L57) | semua MASIH |
| settings — Tab Umum & Users | 0 | 2 (L54, L58) | semua MASIH |
| settings — Tab System Apps | 0 | 0 | — |
| forgot_password.html | 0 | 0 | — (L67 tercatat di cek_hasil) |
| cek_hasil.html | 0 | 1 (L67) | L67 **FIX `90ca7de`** |
| register.html | 0 | 1 (L66) | L66 MASIH |
| register_confirm.html | 0 | 1 (L65) | L65 MASIH |
| reset_password.html | 0 | 1 (L68) | L68 **FIX `90ca7de`** |
| download.html | 0 | 3 (L62, L63, L64) | semua MASIH |
| hasil.html | 2 (M20, M26) | 3 (L59, L60, L61) | M20 **FIX `ab64bd9`**; M26 **FIX `e86ecda`**; L59 **FIX `90ca7de`**; L60, L61 MASIH |
| Lintas halaman / berkas bersama | 3 (M19, M21, M27) | 7 (L69–L75) | M19 **MASIH** (anomali: terlewat oleh `ab64bd9`); M21 **FIX PARSIAL `3305d0d`** (sisa download:397-486 + hasil:73); M27 **FIX `e86ecda`**; L69–L75 MASIH |
| **Total** | **12** | **35** | MEDIUM: 10 FIX penuh + 1 FIX parsial (M21) + 1 aktif (M19); LOW: 4 FIX + 31 aktif — rincian baris di atas |

**Hitungan status MEDIUM (12):** FIX penuh 10 (M16, M17, M18, M20, M22, M23, M24, M25, M26, M27) + FIX parsial 1 (M21) + masih aktif 1 (M19).
**Hitungan status LOW (35):** FIX 4 (L56, L59, L67, L68) + masih aktif 31.

## 7. Rekomendasi Prioritas

Diurutkan berdasarkan dampak-ke-upayaan; temuan yang sudah FIX tidak diulang di sini (lihat §6).

1. **[M19] Tukar urutan skip-link di hasil.html:71-72** — dua baris, dampak aksesibilitas langsung, satu-satunya MEDIUM aktif. Anomali `ab64bd9` menunjukkan perlu grep-regresi: `grep -L "public_skip_link" templates/public/*.html | xargs grep -B1 public_nav` semacamnya untuk memastikan tidak ada halaman ke-6.
2. **[M21 sisa] Dua langkah kecil + satu keputusan** — (a) hasil.html:73 → `{{ template "public_toast_host" . }}` (trivial, halaman sudah memuat output.css); (b) download.html: pilih antara memuat output.css vs mengekstrak skin toast `.toast-*` ke CSS bersama ringan; (c) koreksi komentar shared.html:899. Ini menuntaskan arsitektur toast satu sumber.
3. **[L69] Pisahkan nilai z-index yang bertabrakan** (theme.css:125-135) — perbaikan satu baris per token; mencegah bug stacking yang sulit direproduksi.
4. **[L64] `noscript` download.html** — tambah `<noscript><style>.platform-section { display: block; }</style></noscript>`; konten inti halaman publik tidak boleh bergantung JS untuk sekadar tampil.
5. **[L75] Hapus 5 simbol sprite dead** (`hi-arrow-up`, `hi-banknotes`, `hi-check-circle`, `hi-copy`, `hi-x-circle`) — census §4.4; ringan, mengurangi parse tiap halaman.
6. **[L70/L61/L62] Substitusi literal warna → token** — 28 white + 2 hex medali + deretan download; kerja mekanis, cocok untuk satu commit bersih "token sweep".
7. **[L73/L72] Dead class admin-base.css & token theme.css tanpa konsumen** — hapus dengan census sebagai bukti; pertahankan census sebagai skrip pemeriksaan bila ingin mencegah kambuh.
8. **[L74] Helper `wireOnce` atau migrasi registry Actions** untuk 4 situs dataset-flag di settings-*.js.
9. **[L42–L58, L60, L63, L65, L66, L76] LOW per-halaman sisanya** — lihat detail §4.1–§4.4; tidak ada yang memerlukan keputusan arsitektur, semuanya perbaikan lokal.
10. **Cross-ref review lama**: #14 (theme.css double-fetch) **FIX `245b4f7`** — selaraskan; #15 (SVG `<use>` lintas-berkas) kini makin relevan karena sprite jadi partial tunggal; #16 → diperluas menjadi "perluas partial `public_toast_host` ke download + ganti host hardcode di hasil" (sama dengan rekomendasi #2 di atas).

## 8. Koreksi Bukti-Driven (perubahan sejak review berjalan)

Review ini ditulis sambil sesi fix paralel berkomitmen ke branch yang sama. Bagian ini mencatat setiap tempat bukti bergerak di bawah kaki review, dan bagaimana dokumen menanganinya — supaya pembaca tahu mana angka snapshot dan mana angka hidup.

1. **M19 menyempit 5 → 1 halaman** — saat ditemukan, 5 halaman publik memasang nav sebelum skip-link. Commit `ab64bd9` (klaim "M17-M20") membetulkan cek_hasil (:25-27), register (:202-204), forgot (:30-31), register_confirm, reset_password — **tapi terlewat hasil.html:71-72**. §4.4 M19 ditulis scoped hasil.html + anomali dicatat; angka §1/§3 tidak dihitung ulang mundur.
2. **Klaim interim "register toast via cssText" usang** — bukti awal M21 menunjukkan register memakai cssText ad-hoc; `3305d0d` menggantinya dengan `showToast` sentral (:463-467). M21 ditulis ulang sebagai FIX PARSIAL dengan sisa terverifikasi (download :397-486, hasil :73).
3. **Partial `public_toast_host` ditemukan** (shared.html:904-906) — review awal mengira host toast harus dibuat baru; ternyata sudah ada dan tinggal dikonsumsi. Rekomendasi M21 disesuaikan.
4. **Census putih (28) & medali (L61) diverifikasi ulang** — angka census white 28 konsisten antar-pass; `#e2e8f0`/`#d97706` di hasil.css:612-613 masih ada pada snapshot final.
5. **`searchVoucher` di :28 settings-vouchers.js** (L52) — verifikasi ulang menunjukkan fungsi tetap di posisi itu; bukti dipertahankan.
6. **register_confirm urutan skip-link benar** — dugaan awal keliru; halaman masuk daftar "benar" sejak awal penulisan §4.3.
7. **`public_auth_nav` adalah wrapper** (shared.html:942: `{{ define "public_auth_nav" . }}{{ template "public_nav" . }}{{ end }}`) — penting untuk bukti M19: memakai wrapper itu tidak otomatis membetulkan urutan; urutan tetap tanggung jawab halaman.
8. **Jumlah baris shared.html ≥943** (dokumen awal menulis 930-an) — angka di §2.1 dikoreksi ke snapshot final.
9. **13 commit fix paralel (3c34337..HEAD)** — `422dc8b` (cleanup file sekali-pakai), `521c196` (±30 fungsi dead code), `4033735` (M9-M11 token dashboard), `245b4f7` (5.4#2 + M15 @import), `06c3b43` (L8), `f88ef27` (L10+L11), `428568c` (M22/M23/M25), `594c675` (M24), `71c19ec` (M16), `ab64bd9` (M17-M20), `e86ecda` (M26+M27), `3305d0d` (M21), `90ca7de` (L56/L59/L67/L68). Sesi fix mengikuti pola test-first (TDD) — pesan commit menyandang ID temuan review ini.
10. **Metodologi "nomor baris = snapshot saat temuan ditemukan"** — working tree terus bergerak selama review; beberapa nomor baris di §4 (mis. renderStats L60 bergerak dari :542-559 area) bisa bergeser ±beberapa baris terhadap HEAD. §9 memuat kutipan verbatim sebagai jangkar.
11. **Sprite 42 → 45 simbol** — `e86ecda` menambah `hi-arrow-left`, `hi-trending-up`, `hi-exclamation-triangle` (semuanya untuk hasil.html). Census L75 dijalankan ulang untuk 45; 5 dead tetap 5.
12. **Salah-kutip 'Menyapan…' dikoreksi** — draf awal §4.2 (M16) mengutip teks tombol `'Menyapan...'`; verifikasi settings-packages.js:138 membuktikan ejaan sebenarnya `'Menyimpan...'`. Kutipan di dokumen ini sudah dikoreksi. Pelajaran metodologi: tidak ada kutipan masuk dokumen tanpa verifikasi berkas.
13. **Mixed-snapshot di §2.1** — beberapa jumlah baris ditulis pasca-fix tertentu sementara yang lain pra-fix (cek_hasil 79→100, shared 930→943, hasil 1065→1070, svg-symbols 45 pasca-`e86ecda`). §2.1 dikoreksi ke snapshot final 2026-09-12; perbedaan dicatat di sini, bukan disembunyikan.

## 9. Lampiran Kutipan Verbatim

Kutipan lengkap untuk temuan yang buktinya berupa blok skrip (nomor baris bisa bergeser; teks ini jangkar identitasnya).

### 9.1 (dipakai oleh M16 — kini FIX `71c19ec`, dipertahankan sebagai baseline saat temuan)

```js
// settings-packages.js :138 (teks tombol saat menyimpan — dikutip untuk
// membuktikan ejaan yang benar setelah koreksi §8 #12)
btn.textContent = 'Menyimpan...';
```

### 9.2 cek_hasil.html:64-98 — guard double-submit + restore bfcache (konteks L67 flash & pola §5 #5)

```html
<script>
// Normalisasi token ke huruf besar sebelum dikirim (server juga melakukan
// hal yang sama; ini menjaga nilai input tetap sama dengan tampilannya).
// M13: guard double-submit seperti register_confirm (:366-368) dan
// reset_password (:339) — tombol di-disable + state loading saat request
// berjalan, agar klik ganda / Enter berulang tidak membuka halaman hasil
// yang sama dua kali. Dipulihkan saat kembali via bfcache (pageshow
// persisted): GET dengan token salah harus bisa dicoba lagi tanpa reload
// penuh.
document.addEventListener('DOMContentLoaded', function() {
  var form = document.getElementById('cekHasilForm');
  var input = document.getElementById('token');
  var btn = document.getElementById('cekHasilSubmitBtn');
  if (form && input) {
    input.addEventListener('input', function() {
      if (this.value !== this.value.toUpperCase()) this.value = this.value.toUpperCase();
    });
    form.addEventListener('submit', function() {
      input.value = input.value.trim().toUpperCase();
      if (btn && !btn.disabled) {
        btn.classList.add('loading');
        btn.disabled = true;
        btn.textContent = 'Memeriksa...';
      }
    });
  }
  window.addEventListener('pageshow', function(e) {
    if (e.persisted && btn) {
      btn.classList.remove('loading');
      btn.disabled = false;
      btn.textContent = 'Lihat Hasil';
    }
  });
});
</script>
```

*— Akhir dokumen. Review UI halaman web EXAMVAN, 2026-09-12: 0 HIGH / 12 MEDIUM / 35 LOW; 10 MEDIUM + 1 parsial + 4 LOW telah diterapkan oleh sesi fix paralel (lihat §6, §8 #9).*
