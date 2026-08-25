/**
 * ══════════════════════════════════════════════════════════════════════════
 * Suite UI/UX BATCH 16 — ADMIN CORE (agen koordinator, menggantikan agen
 * batch16-admincore yang gagal dieksekusi dua kali)
 * ══════════════════════════════════════════════════════════════════════════
 *
 * Referensi: review_uiux_webui.md bagian 5.13 RE-REVIEW RONDE 10.
 *
 * Kontrak yang ditegakkan suite ini:
 *
 *   S109 — RACE MODAL "Atur User" YANG KEEMPAT: openEditUserModal melakukan
 *         fetch detail user TANPA token generasi DAN tanpa .catch — respons
 *         user A yang lambat menimpa field modal user B (salah konteks
 *         editing!), dan gagal jaringan membuat modal tak terbuka tanpa toast
 *         (unhandled rejection). Kontrak: variabel `editUserModalSeq`
 *         eksis; guard `if (seq !== editUserModalSeq) return;` berada di
 *         cabang then; rantai diakhiri .catch yang menampilkan toast.
 *         Pola acuan: delegateModalSeq (:2920–2992).
 *
 *   R119 — LISTENER MENUMPUK: pasangan addEventListener('change',
 *         syncEditLimitFields) semula dipasang DI DALAM .then setiap kali
 *         modal dibuka — N kali buka = N listener (modal di-cache, elemen
 *         sama). Kontrak: pemasangan itu muncul TEPAT SATU KALI di seluruh
 *         admin.js dan berada dalam createEditUserModal (dipasang sekali
 *         saat modal dibuat).
 *
 *   R120 — PARITAS PAGINASI DASHBOARD: nomor halaman aktif belum membawa
 *         aria-current="page" (submissions.html sudah benar sejak lama).
 *         Kontrak: anchor .pagination-page-num memuat kondisional
 *         {{if eq $pagenum $p}}aria-current="page"{{end}}.
 *
 *   R121 — TARGET SENTUH BARIS UJIAN: .btn-sm-compact hanya padding 5px 8px
 *         + ikon 15px ≈ 31×25px — jauh di bawah konvensi sentuh 44px proyek
 *         (preseden S10/R96/R99/R106). Kontrak: aturan mobile eksplisit
 *         memberi min-height/min-width ≥44px untuk tombol aksi baris.
 *
 *   R122 — loadSaasSettings TANPA .catch: gagal jaringan = form Pengaturan
 *         kosong tanpa pesan apa pun (unhandled rejection). Kontrak: fungsi
 *         loadSaasSettings memiliki cabang .catch yang menampilkan toast,
 *         dan non-success punya cabang else berpesan.
 *
 *   R123 — KONTRAK ESCAPE BOLONG 1 TITIK: p.role di builder dropdown
 *         pengawas masuk innerHTML mentah padahal username di sebelahnya
 *         ter-escape. Kontrak: escapeHtml(p.role || 'Pengawas') dan tidak
 *         ada lagi interpolasi mentah `(p.role ||`.
 *
 *   R132 — LANJUTAN S90 (parsial): dropdown pengawas kini keyboard-operable,
 *         tapi (a) tutup via klik-luar tidak me-reset aria-expanded → screen
 *         reader tetap membaca "expanded=true"; (b) tak ada cabang Escape;
 *         (c) fokus tidak dikembalikan ke pemicu saat ditutup lewat keyboard
 *         (konvensi sudah ada di menu topbar). Kontrak: listener klik-luar
 *         meng-set aria-expanded="false"; keydown header menangani 'Escape'
 *         dengan menutup dropdown dan mem-fokus ulang header.
 *
 *   R125(bagian admin) — KEMBARAN WARNA MUTED: bentuk rgba(107,114,128,…)/
 *         #9ca3af (keluarga gray Tailwind = #6b7280 yang sudah dibasmi
 *         R111) masih tersisa di dashboard (.pd-action-muted) & admin-base
 *         (.student-status.not-started). Kontrak: kedua file bebas literal
 *         itu; warna memakai var(--color-text-muted)/rgba(var(--rgb-
 *         text-muted),α) — triplet baru didefinisikan theme.css sebagai
 *         source-of-truth #a0aec0.
 *
 * Cara kalibrasi bila test MEMERAH setelah edit sah:
 *   - Menambah pemakaian syncEditLimitFields baru? Test R119 sengaja ketat
 *     (==1); jika memang butuh pemasangan kedua di lokasi lain, ubah pola
 *     menjadi delegasi, bukan menaikkan angka.
 */
import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const ROOT = new URL('.', import.meta.url).pathname.replace(/\/static\/js\/$/, '');
const ADMIN_JS = readFileSync(join(ROOT, 'static/js/admin.js'), 'utf8');
const DASH_HTML = readFileSync(join(ROOT, 'templates/admin/dashboard.html'), 'utf8');
const ADMIN_BASE_CSS = readFileSync(join(ROOT, 'static/css/admin-base.css'), 'utf8');

import { join } from 'node:path';

test('S109: openEditUserModal ber-token generasi (then+catch ter-guard)', () => {
    assert.match(ADMIN_JS, /var editUserModalSeq\s*=\s*0/,
        'deklarasi editUserModalSeq eksis (polanya: delegateModalSeq)');
    // Ambil blok fungsi openEditUserModal … hingga penutupnya (batas kasar:
    // hingga function berikutnya) untuk asersi lokal.
    const start = ADMIN_JS.indexOf('function openEditUserModal(');
    assert.ok(start > -1, 'fungsi openEditUserModal eksis');
    const nextFn = ADMIN_JS.indexOf('\nfunction ', start + 10);
    const block = ADMIN_JS.slice(start, nextFn > -1 ? nextFn : start + 6000);

    assert.match(block, /var seq = \+\+editUserModalSeq/,
        'token generasi dinaikkan per panggilan');
    assert.match(block, /if \(seq !== editUserModalSeq\) return;/,
        'guard respons basi di cabang then');
    assert.match(block, /\.catch\s*\(/,
        '.catch wajib ada — gagal jaringan tak boleh unhandled rejection');
    assert.match(block, /showToast\(/,
        '.catch menampilkan toast, bukan gagal senyap');
});

test('R119: pemasangan listener change syncEditLimitFields tepat satu kali (di createEditUserModal)', () => {
    // Kontraknya SATU LOKASI wiring (pasangan eguru+epengawas = 2 panggilan
    // dalam satu blok), bukan hitungan mentah string.
    const openStart = ADMIN_JS.indexOf('function openEditUserModal(');
    const openNext = ADMIN_JS.indexOf('\nfunction ', openStart + 10);
    const openBlock = ADMIN_JS.slice(openStart, openNext > -1 ? openNext : openStart + 6000);
    assert.doesNotMatch(openBlock, /addEventListener\('change',\s*syncEditLimitFields\)/,
        'openEditUserModal tidak boleh memasang listener lagi (.then berjalan tiap buka modal = menumpuk)');

    const idx = ADMIN_JS.indexOf("addEventListener('change', syncEditLimitFields)");
    const fnStart = ADMIN_JS.lastIndexOf('function createEditUserModal', idx);
    assert.ok(fnStart > -1 && fnStart < idx,
        'pemasangan berada di dalam createEditUserModal (dipasang sekali saat modal dibuat)');
});

test('R120: nomor halaman aktif paginasi dashboard membawa aria-current="page"', () => {
    assert.match(DASH_HTML,
        /class="pagination-page-num \{\{if eq \$pagenum \$p\}\}page-current\{\{end\}\}"[^>]*aria-current="page"|aria-current="page"[^>]*>\{\{\$pagenum\}\}/,
        'paritas dengan submissions.html:343 — atribut kondisional pada anchor nomor halaman');
    // Bentuk persis yang diharapkan (urutan atribut bebas, intinya kondisional ada):
    assert.match(DASH_HTML, /\{\{if eq \$pagenum \$p\}\}aria-current="page"\{\{end\}\}/);
});

test('R121: tombol aksi baris ujian punya target sentuh ≥44px di mobile', () => {
    const rule = /\.btn-sm-compact[^{]*\{[^}]*min-height:\s*(4[4-9]|[5-9]\d)px/s;
    const dashMobileBlock = DASH_HTML.slice(DASH_HTML.indexOf('@media'));
    const ok = rule.test(ADMIN_BASE_CSS.replace(/\/\*[\s\S]*?\*\//g, '')) ||
               rule.test(dashMobileBlock);
    assert.ok(ok,
        'aturan min-height ≥44px untuk .btn-sm-compact (di media query mobile dashboard.html atau admin-base.css)');
});

test('R122: loadSaasSettings punya .catch dan cabang else non-success', () => {
    const start = ADMIN_JS.indexOf('function loadSaasSettings(');
    const nextFn = ADMIN_JS.indexOf('\nfunction ', start + 10);
    const block = ADMIN_JS.slice(start, nextFn > -1 ? nextFn : start + 4000);
    assert.match(block, /\.catch\s*\(/, '.catch wajib — form kosong senyap itu bug ronde 10');
    assert.match(block, /showToast\(/, '.catch menampilkan toast');
    assert.doesNotMatch(block, /\}\s*\n\s*\}\);\s*$/,
        'rantai fetch tidak boleh berakhir tanpa catch');
});

test('R123: p.role ter-escape di builder dropdown pengawas', () => {
    assert.match(ADMIN_JS, /escapeHtml\(p\.role \|\| 'Pengawas'\)/,
        'bungkus escapeHtml sesuai kontrak builder');
    assert.doesNotMatch(ADMIN_JS, /['"]\s*\+\s*\(p\.role \|\| 'Pengawas'\)\s*\+\s*['"]/,
        'interpolasi mentah p.role tidak boleh tersisa');
});

test('R132: klik-luar reset aria-expanded; Escape menutup + fokus kembali ke header', () => {
    // (a) Blok listener klik-luar (_pengawasDropdownListener) wajib me-reset state SR.
    const coStart = ADMIN_JS.indexOf('window._pengawasDropdownListener');
    assert.ok(coStart > -1, 'listener klik-luar eksis');
    const coBlock = ADMIN_JS.slice(coStart, coStart + 700);
    assert.match(coBlock, /hd\.setAttribute\('aria-expanded',\s*'false'\)/,
        'tutup via klik-luar wajib me-reset aria-expanded (S108-ronde-10)');
    // (b) Keydown header menangani Escape + focus kembali.
    const kdStart = ADMIN_JS.indexOf("header.addEventListener('keydown'");
    assert.ok(kdStart > -1, 'keydown header eksis');
    const kdBlock = ADMIN_JS.slice(kdStart, kdStart + 900);
    assert.match(kdBlock, /e\.key === 'Escape'/, 'cabang Escape eksis di keydown header');
    assert.match(kdBlock, /\.focus\(\)/, 'fokus dikembalikan ke header saat ditutup via Escape');
});

test('R125(admin): dashboard.html & admin-base.css bebas kembaran muted rgba(107,114,128)/#9ca3af', () => {
    for (const [nama, src] of [['dashboard.html', DASH_HTML], ['admin-base.css', ADMIN_BASE_CSS]]) {
        assert.doesNotMatch(src, /107,\s*114,\s*128/,
            `${nama}: bentuk rgb #6b7280 harus bermigrasi ke token`);
        assert.doesNotMatch(src, /#9ca3af/i,
            `${nama}: #9ca3af harus bermigrasi ke var(--color-text-muted)`);
    }
    // Triplet source-of-truth didefinisikan theme.css (pola triplet rgb lain).
    const THEME = readFileSync(join(ROOT, 'static/css/theme.css'), 'utf8');
    assert.match(THEME, /--rgb-text-muted:\s*160,\s*174,\s*192/,
        'triplet --rgb-text-muted (#a0aec0) didefinisikan theme.css sebagai source-of-truth');
});
