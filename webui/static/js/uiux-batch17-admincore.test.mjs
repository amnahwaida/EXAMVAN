/**
 * ══════════════════════════════════════════════════════════════════════════
 * Suite UI/UX BATCH 17 — ADMIN CORE (S113, R133–R139; dieksekusi koordinator)
 * ══════════════════════════════════════════════════════════════════════════
 *
 * Referensi: review_uiux_webui.md bagian 5.14 RE-REVIEW RONDE 11.
 *
 *   S113 — ESCAPE DI DALAM DROPDOWN PENGAWAS MENUTUP SELURUH MODAL DELEGASI:
 *         saat dropdown dibuka fokus pindah ke search box (:1161), sehingga
 *         Escape tidak pernah menyentuh handler header (R132) dan jatuh ke
 *         Modal Manager → forceClose menutup modal Delegasi Ujian; form yang
 *         sudah diisi hilang. Kontrak: elemen #pengawasDropdown punya handler
 *         keydown Escape sendiri yang (a) stopPropagation, (b) menutup
 *         dropdown, (c) me-reset aria-expanded header, (d) mem-fokuskan
 *         kembali header.
 *
 *   R133 — CATCH openEditUserModal TANPA GUARD SEQ: toast error basi dari
 *         permintaan A bisa muncul setelah modal B sukses terisi. Kontrak:
 *         cabang .catch ber-guard `seq !== editUserModalSeq`.
 *
 *   R134 — submitEditUser MENYIMPANG DARI KONTRAK GUARD DOBEL-KIRIM S27:
 *         pola acuan (`submitEditToken`, `saveQuestionsConfig`) membuka
 *         guard dengan `if (!btn || btn.disabled) return;`. Kontrak: baris
 *         guard itu eksis di submitEditUser.
 *
 *   R135 — ANCHOR PAGINASI aria-disabled MASIH BER-HREF HIDUP: pointer-events
 *         hanya memblokir mouse — keyboard Enter tetap menavigasi ke page 0 /
 *         melebihi total (flicker + scroll reset), dan SR membaca kontradiksi
 *         "disabled" pada link aktif. Kontrak: dashboard.html & submissions.html
 *         me-render href SECARA KONDISIONAL (omit saat disabled), bukan href
 *         permanen + class disabled.
 *
 *   R136 — LISTENER GLOBAL GANDA identity-popup: listener kedua tanpa syarat
 *         menghapus .show untuk klik APA PUN termasuk klik DI DALAM popup —
 *         melumpuhkan guard `closest('.identity-popup')` milik listener
 *         pertama (seleksi teks ikut menutup popup). Kontrak: hanya SATU
 *         listener penutup yang tersisa dan ia ber-guard closest.
 *
 *   R137 — LITERAL WARNA KARTU KUOTA DASHBOARD: #c084fc & #38bdf8 bypass
 *         design token. Kontrak: dashboard.html bebas kedua literal;
 *         warna memakai var(--color-accent-light)/var(--color-info-light).
 *
 *   R138 — TOMBOL .btn-more TANPA STATE POPUP: menu baris ujian adalah popup
 *         sesungguhnya tapi tak punya aria-haspopup/aria-expanded, dan ketiga
 *         jalur tutup (toggle, klik-luar, Escape) tidak memulihkan fokus ke
 *         tombol pemicu — kelas cacat yang sama sudah dibereskan untuk
 *         dropdown pengawas (S90+R132) dan topbar. Kontrak: markup membawa
 *         aria-haspopup+aria-expanded awal; toggle menyinkronkan aria-expanded
 *         pada buka & tutup; Escape memulihkan fokus ke tombol pemilik menu
 *         (via __btnWrap).
 *
 *   R139 — PAGINASI JS DAFTAR USER TANPA aria-current: tombol halaman aktif
 *         justru di-disabled sehingga tak fokusable dan SR tak bisa
 *         mengumumkan posisi. Kontrak: saat isCurrent tombol mendapat
 *         aria-current="page" (+ aria-label posisi), dan TIDAK di-disabled.
 */
import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';

const ROOT = new URL('.', import.meta.url).pathname.replace(/\/static\/js\/$/, '');
const ADMIN_JS = readFileSync(join(ROOT, 'static/js/admin.js'), 'utf8');
const DASH_HTML = readFileSync(join(ROOT, 'templates/admin/dashboard.html'), 'utf8');
const SUBS_HTML = readFileSync(join(ROOT, 'templates/admin/submissions.html'), 'utf8');

test('S113: #pengawasDropdown punya handler Escape sendiri (stopPropagation + tutup + fokus header)', () => {
    // Handler dipasang pada elemen dropdown (bukan hanya header) karena fokus
    // default saat terbuka ada di search box.
    const wiringIdx = ADMIN_JS.indexOf("dd.addEventListener('keydown'");
    assert.ok(wiringIdx > -1, "handler keydown pada elemen dd (#pengawasDropdown) eksis");
    const block = ADMIN_JS.slice(wiringIdx, wiringIdx + 900);
    assert.match(block, /ev\.key !== 'Escape'/, 'menangani Escape (ev: handler di dropdown)');
    assert.match(block, /stopPropagation\(\)/, 'Escape di dalam dropdown TIDAK diteruskan ke Modal Manager');
    assert.match(block, /aria-expanded/, 'me-reset aria-expanded header');
    assert.match(block, /\.focus\(\)/, 'fokus kembali ke header');
});

test('R133: catch openEditUserModal ber-guard seq', () => {
    const fnStart = ADMIN_JS.indexOf('function openEditUserModal(');
    const fnEnd = ADMIN_JS.indexOf('\nfunction ', fnStart + 10);
    const block = ADMIN_JS.slice(fnStart, fnEnd > -1 ? fnEnd : fnStart + 6000);
    // Ambil khusus blok .catch (bukan guard cabang then yang sudah ada).
    const catchIdx = block.indexOf('.catch(function () {');
    assert.ok(catchIdx > -1, '.catch eksis');
    const catchBlock = block.slice(catchIdx, catchIdx + 400);
    assert.match(catchBlock, /if \(seq !== editUserModalSeq\) return;/,
        'toast basi dari permintaan lama tidak boleh muncul setelah modal baru terisi');
});

test('R134: submitEditUser memakai guard dobel-kirim ala S27', () => {
    const start = ADMIN_JS.indexOf('function submitEditUser(');
    const nextFn = ADMIN_JS.indexOf('\nfunction ', start + 10);
    const block = ADMIN_JS.slice(start, nextFn > -1 ? nextFn : start + 3000);
    assert.match(block, /if \(!btn \|\| btn\.disabled\) return;/,
        'guard persis seperti submitEditToken/saveQuestionsConfig');
});

test('R135: paginasi disabled tanpa href hidup (dashboard + submissions)', () => {
    for (const [nama, src] of [['dashboard.html', DASH_HTML], ['submissions.html', SUBS_HTML]]) {
        // Href Sebelumnya/berikutnya harus kondisional — tidak boleh href permanen
        // berdampingan dengan aria-disabled.
        assert.match(src, /\{\{if gt \.page 1\}\}href=/,
            `${nama}: href "Sebelumnya" kondisional`);
        assert.match(src, /\{\{if lt \.page \.total_pages\}\}href=/,
            `${nama}: href "Berikutnya" kondisional`);
        // Href permanen pada state disabled tidak boleh ada — href hanya sah
        // bila dibungkus kondisi {{if gt/lt …}} (omit saat disabled).
        assert.doesNotMatch(src, /<a href="\?page=\{\{sub \.page 1\}\}/,
            `${nama}: href Sebelumnya masih permanen (harus {{if gt .page 1}})`);
        assert.doesNotMatch(src, /<a href="\?page=\{\{add \.page 1\}\}/,
            `${nama}: href Berikutnya masih permanen (harus {{if lt .page .total_pages}})`);
    }
});

test('R136: hanya satu listener penutup identity-popup, dan ber-guard closest', () => {
    const hits = ADMIN_JS.split("querySelectorAll('.identity-popup.show')").length - 1;
    assert.ok(hits <= 2, `penutup identity-popup muncul ${hits}× — maksimal satu listener ber-guard`);
    assert.doesNotMatch(ADMIN_JS, /Global click: close identity popup/,
        'listener dokumen tanpa syarat (pembunuh guard) sudah dihapus');
    assert.match(ADMIN_JS, /!e\.target\.closest\('\.identity-popup'\)/,
        'listener tersisa tetap ber-guard klik-dalam-popup');
});

test('R137: dashboard.html bebas literal #c084fc & #38bdf8', () => {
    assert.doesNotMatch(DASH_HTML, /#c084fc/i, 'migrasi ke var(--color-accent-light)');
    assert.doesNotMatch(DASH_HTML, /#38bdf8/i, 'migrasi ke varian info yang setara');
    assert.match(DASH_HTML, /var\(--color-accent-light\)/, 'pengganti token aksen dipakai');
});

test('R138: .btn-more punya state popup; toggle sinkron aria-expanded; Escape pulihkan fokus', () => {
    // (a) Markup awal
    const btn = DASH_HTML.match(/<button[^>]*btn-more[^>]*>/);
    assert.ok(btn, '.btn-more eksis di dashboard.html');
    assert.match(btn[0], /aria-haspopup="true"/, 'aria-haspopup="true"');
    assert.match(btn[0], /aria-expanded="false"/, 'aria-expanded awal false');
    // (b) Toggle menyinkronkan state pada buka & tutup
    const fnStart = ADMIN_JS.indexOf('function toggleRowDropdown(');
    const block = ADMIN_JS.slice(fnStart, fnStart + 4000);
    assert.match(block, /setAttribute\('aria-expanded',\s*dropdown\.classList\.contains\('show'\)\s*\?\s*'true'\s*:\s*'false'\)/,
        'buka/tutup → expanded=true/false (ternary sinkron)');
    // (c) Escape memulihkan fokus ke tombol pemilik menu
    const escStart = ADMIN_JS.indexOf("// Close dropdowns with Escape key");
    const escBlock = ADMIN_JS.slice(escStart, escStart + 700);
    assert.match(escBlock, /__btnWrap/, 'Escape menelusuri pemilik menu via __btnWrap');
    assert.match(escBlock, /\.focus\(\)/, 'Escape mem-fokuskan ulang tombol pemicu');
});

test('R139: paginasi JS users — halaman aktif aria-current="page" dan tidak di-disabled', () => {
    const start = ADMIN_JS.indexOf('function renderUsersPagination(');
    const nextFn = ADMIN_JS.indexOf('\nfunction ', start + 10);
    const block = ADMIN_JS.slice(start, nextFn > -1 ? nextFn : start + 2600);
    assert.match(block, /isCurrent[\s\S]{0,300}aria-current',\s*'page'/,
        'halaman aktif membawa aria-current="page"');
    const curBlock = block.match(/if \(isCurrent\) \{[\s\S]*?\}/);
    assert.ok(curBlock, 'cabang isCurrent eksis');
    assert.doesNotMatch(curBlock[0], /\.disabled = true/,
        'halaman aktif tidak boleh di-disabled (tak fokusable = SR tak bisa umumkan posisi)');
});
