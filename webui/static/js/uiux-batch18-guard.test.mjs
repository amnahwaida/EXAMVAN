/**
 * ══════════════════════════════════════════════════════════════════════════
 * Suite UI/UX BATCH 18 — Z-INDEX TOKEN FOLDER-WIDE & POLISH A11Y NAV
 * (S116, R144, R145 — dieksekusi koordinator)
 * ══════════════════════════════════════════════════════════════════════════
 *
 * Referensi: review_uiux_webui.md bagian 5.15 RE-REVIEW RONDE 12.
 *
 *   S116 — GUARD "LARANGAN z-index ≥1000" TERNYATA TIDAK PERNAH FOLDER-WIDE:
 *         klaim ronde 8 hanya berisi tiga asersi lokasi spesifik. Fakta di
 *         HEAD: admin-base.css memuat 7 literal legacy ≥9997 (shortcuts-hint,
 *         mobile-bottom-bar, modal-overlay/backdrop 10000, toast-container
 *         10001, topbar floating 10002 ×3), download.html & register.html
 *         masing-masing satu 9999, dan folder generated tailwind beberapa
 *         lagi. Kontrak suite ini:
 *
 *           (a) TOKEN SOURCE-OF-TRUTH: theme.css mendefinisikan empat token
 *               baru yang melengkapi tangga stacking legacy -
 *                 --z-bottom-bar     : 9997  (mobile bottom bar)
 *                 --z-hint           : 9998  (keyboard shortcuts hint)
 *                 --z-modal-overlay  : 10000 (dialog overlay/backdrop)
 *                 --z-topbar-floating: 10002 (topbar toggle/close touch-device)
 *               (empat token lama tetap: --z-skip-link/--z-dropdown/
 *                --z-onboarding/--z-toast.)
 *
 *           (b) MIGRASI: seluruh literal ≥1000 di admin-base.css, download.html,
 *               register.html WAJIB memakai var(--z-*). Pemetaan semantik:
 *               shortcuts-hint→--z-hint; bottom-bar→--z-bottom-bar;
 *               modal-overlay/backdrop→--z-modal-overlay; toast-container→
 *               --z-toast (NAIK 10001→10002 sesuai intent terdokumentasi
 *               "toasts one step above dialogs" + membuat --z-onboarding
 *               benar-benar di bawah toast - memperbaiki latent tie 10001=10001);
 *               topbar floating→--z-topbar-floating; banner unduh/error bar
 *               publik→--z-dropdown.
 *
 *           (c) GUARD FOLDER-WIDE: templates/** + static/css inti (KECUALI
 *               static/css/tailwind/ yang generated - pengecualian
 *               didokumentasikan) + static/js/*.js aplikasi TIDAK BOLEH memuat
 *               literal z-index ≥1000. Kelas ini adalah kelas S80/S81/R88
 *               generasi ketiga: jangan biarkan komponen baru menyisip layer
 *               yang melompati toast tanpa terdeteksi.
 *
 *   R144 — JALUR TUTUP row-dropdown TIDAK ME-RESET aria-expanded: R138 hanya
 *         menyinkronkan pada toggle & Escape. Dua jalur lainnya - klik-luar
 *         dan "Close all other dropdowns" dalam toggleRowDropdown - masih
 *         membiarkan tombol pemilik menu membaca expanded=true. Kontrak:
 *         kedua jalur menelusuri __btnWrap lalu reset aria-expanded='false'.
 *
 *   R145 — TOGGLEMENU (JALUR KLIK OVERLAY) TIDAK MENGEMBALIKAN FOKUS:
 *         closeMenu (Escape) punya wasOpen + burger.focus(), tapi overlay
 *         ter-wire ke toggleMenu yang menutup drawer TANPA memulihkan fokus -
 *         dua jalur tutup, dua perilaku fokus. Kontrak: toggleMenu mendeteksi
 *         wasOpen dan mem-fokuskan burger saat transisi open->closed.
 */
import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, readdirSync, statSync } from 'node:fs';
import { join, relative, sep } from 'node:path';

const ROOT = new URL('.', import.meta.url).pathname.replace(/\/static\/js\/$/, '');
const read = (p) => readFileSync(join(ROOT, p), 'utf8');

const ADMIN_BASE_CSS = read('static/css/admin-base.css');
const THEME_CSS = read('static/css/theme.css');
const DOWNLOAD_HTML = read('templates/public/download.html');
const REGISTER_HTML = read('templates/public/register.html');
const ADMIN_JS = read('static/js/admin.js');
const SHARED_HTML = read('templates/public/shared.html');

// Literal z-index >=1000 (4 digit ke atas). Pemakaian var() tidak cocok pola.
const Z_LITERAL_RE = /z-index:\s*['"]?([0-9]{4,})/g;

test('S116a: theme.css mendefinisikan empat token tangga stacking baru', () => {
    for (const tok of ['--z-bottom-bar:\\s*9997', '--z-hint:\\s*9998',
                       '--z-modal-overlay:\\s*10000', '--z-topbar-floating:\\s*10002']) {
        assert.match(THEME_CSS, new RegExp(tok),
            `token ${tok.split(':')[0].trim()} wajib eksis sebagai source-of-truth`);
    }
});

test('S116b: admin-base.css bebas literal z-index >=1000 (semua via var(--z-*))', () => {
    const hits = [...ADMIN_BASE_CSS.matchAll(Z_LITERAL_RE)].map((m) => m[1]);
    assert.deepEqual(hits, [],
        '7 literal legacy (9997/9998/10000/10001/10002) wajib bermigrasi ke var(--z-*)');
    // Pemakaian token positif sebagai bukti migrasi:
    assert.match(ADMIN_BASE_CSS, /z-index:\s*var\(--z-bottom-bar\)/);
    assert.match(ADMIN_BASE_CSS, /z-index:\s*var\(--z-hint\)/);
    assert.match(ADMIN_BASE_CSS, /z-index:\s*var\(--z-modal-overlay\)/);
    assert.match(ADMIN_BASE_CSS, /z-index:\s*var\(--z-toast\)/,
        '.toast-container naik ke --z-toast (10002) sesuai intent "di atas dialog/onboarding"');
    assert.match(ADMIN_BASE_CSS, /z-index:\s*var\(--z-topbar-floating\)/);
});

test('S116c: banner unduh/error publik memakai var(--z-dropdown), bukan 9999 literal', () => {
    for (const [nama, src] of [['download.html', DOWNLOAD_HTML], ['register.html', REGISTER_HTML]]) {
        assert.doesNotMatch(src, Z_LITERAL_RE,
            `${nama}: literal z-index >=1000 harus bermigrasi ke var(--z-dropdown)`);
        assert.match(src, /z-index:\s*var\(--z-dropdown\)/,
            `${nama}: pemakai token positif`);
    }
});

test('S116d (guard folder-wide): tidak ada literal z-index >=1000 di luar folder generated', () => {
    // Folder static/css/tailwind/ adalah artefak build - pengecualian
    // terdokumentasi (preseden R126).
    function walk(dir, acc = []) {
        for (const f of readdirSync(dir)) {
            const full = join(dir, f);
            if (statSync(full).isDirectory()) walk(full, acc);
            else acc.push(full);
        }
        return acc;
    }
    const targets = [
        ...walk(join(ROOT, 'templates')).filter((f) => f.endsWith('.html')),
        ...walk(join(ROOT, 'static/css')).filter(
            (f) => f.endsWith('.css') && !f.split(sep).includes('tailwind')),
        ...walk(join(ROOT, 'static/js')).filter(
            (f) => f.endsWith('.js') && !f.includes('test') && !f.includes('.min.')),
    ];
    const pelanggaran = [];
    for (const f of targets) {
        const src = readFileSync(f, 'utf8').replace(/\/\*[\s\S]*?\*\//g, '');
        for (const m of src.matchAll(Z_LITERAL_RE)) {
            pelanggaran.push(`${relative(ROOT, f)}: ${m[0]}`);
        }
    }
    assert.deepEqual(pelanggaran, [],
        'literal z-index >=1000 di luar sistem token = kelas S80/S81/R88 - pakai var(--z-*)');
});

test('R144a: klik-luar row-dropdown me-reset aria-expanded pemilik menu', () => {
    const start = ADMIN_JS.indexOf('// Close dropdowns when clicking anywhere outside');
    assert.ok(start > -1, 'listener klik-luar eksis');
    const block = ADMIN_JS.slice(start, start + 900);
    assert.match(block, /__btnWrap/, 'menelusuri pemilik menu via __btnWrap');
    assert.match(block, /setAttribute\('aria-expanded',\s*'false'\)/,
        'klik-luar wajib reset aria-expanded (pola R132)');
});

test('R144b: "Close all other dropdowns" juga reset aria-expanded miliknya', () => {
    const fnStart = ADMIN_JS.indexOf('function toggleRowDropdown(');
    const block = ADMIN_JS.slice(fnStart, fnStart + 1400);
    assert.match(block, /Close all other dropdowns/, 'prasyarat: blok close-others eksis');
    // Di dalam loop close-others wajib ada reset state tombol pemiliknya.
    const loopIdx = block.indexOf('Close all other dropdowns');
    const loopBlock = block.slice(loopIdx, loopIdx + 500);
    assert.match(loopBlock, /aria-expanded/, 'close-others wajib reset aria-expanded');
});

test('R145: toggleMenu mengembalikan fokus ke burger saat transisi open->closed', () => {
    const start = SHARED_HTML.indexOf('function toggleMenu()');
    const block = SHARED_HTML.slice(start, start + 900);
    assert.match(block, /wasOpen|classList\.contains\('open'\)/,
        'state sebelum toggle tercatat');
    assert.match(block, /burger\.focus\(\)|focus\(\)/,
        'fokus dikembalikan ke burger saat drawer ditutup lewat overlay/hamburger');
});
