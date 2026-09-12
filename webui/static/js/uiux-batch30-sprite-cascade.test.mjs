/* Contract tests untuk Batch 30 — dedup sprite ikon & kaskade .main
 * (review_ui_halaman_web_2026-09-12.md, M26/M27).
 *
 *   M26 — hasil.html membawa sprite lokal 19 <symbol>; 14 di antaranya punya
 *         kembaran konten-identik dengan sprite admin (13 duplikat ID + 1
 *         rename), 2 dead (hi-eye, hi-paper-airplane: 0 referensi). Sprite
 *         lokal dihapus; halaman memakai partial admin svg-symbols.html
 *         (extended dengan 3 simbol unik hasil) — satu sumber kebenaran.
 *   M27 — public-desktop.css:86 menimpa .main milik hasil.css (1180px +
 *         padding tanpa clearing nav, load-order menang di specificity sama)
 *         padahal .main dipakai HANYA oleh hasil.html — rule dihapus;
 *         hasil.css tetap satu-satunya pemilik layout .main.
 *
 * Run with: node --test static/js/uiux-batch30-sprite-cascade.test.mjs (from webui/)
 */

import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const HERE = path.dirname(fileURLToPath(import.meta.url));
const WEBUI_ROOT = path.join(HERE, '..', '..');
const read = (...p) => fs.readFileSync(path.join(...p), 'utf8');

const HASIL_HTML = read(WEBUI_ROOT, 'templates', 'public', 'hasil.html');
const ADMIN_SPRITE = read(WEBUI_ROOT, 'templates', 'admin', 'partials', 'svg-symbols.html');
const PUBLIC_DESKTOP_CSS = read(WEBUI_ROOT, 'static', 'css', 'public-desktop.css');
const HASIL_CSS = read(WEBUI_ROOT, 'static', 'css', 'hasil.css');

// ---------------------------------------------------------------------------
// M26 — satu sprite: partial admin, bukan salinan lokal
// ---------------------------------------------------------------------------

test('B30-1 (M26): hasil.html tidak lagi mendefinisikan <symbol> lokal — pakai partial admin', () => {
    const localSymbols = [...HASIL_HTML.matchAll(/<symbol id=/g)];
    assert.deepEqual(localSymbols.map((m) => m[0]), [],
        'sprite lokal 19 simbol masih di hasil.html — perbaikan ikon harus dikerjakan ' +
        'dua kali (drift visual admin vs publik); pakai partial admin svg-symbols.html');
    assert.match(HASIL_HTML, /\{\{\s*template\s+"admin\/partials\/svg-symbols\.html"/,
        'hasil.html wajib memuat partial sprite admin');
});

test('B30-2 (M26): setiap #hi-* yang dirujuk hasil.html terdefinisi di partial admin', () => {
    const defined = new Set(
        [...ADMIN_SPRITE.matchAll(/<symbol id="(hi-[a-z-]+)"/g)].map((m) => m[1]));
    assert.ok(defined.size >= 34, 'sprite admin memuat simbol lengkap (prasyarat)');

    const used = new Set(
        [...HASIL_HTML.matchAll(/href="#(hi-[a-z-]+)"/g)].map((m) => m[1]));
    assert.ok(used.size >= 15, 'hasil.html masih memakai sprite (prasyarat kontrak)');

    const missing = [...used].filter((id) => !defined.has(id));
    assert.deepEqual(missing, [],
        'referensi ikon tanpa definisi = ikon mati di halaman hasil: ' + missing.join(', '));

    // Rename wajib: varian hasil memakai nama admin (magnifying-glass→search,
    // arrow-path→refresh) agar tidak ada dua nama untuk bentuk yang sama.
    assert.ok(!used.has('hi-magnifying-glass'),
        'gunakan #hi-search (nama admin) — hi-magnifying-glass adalah rename murni');
    assert.ok(!used.has('hi-arrow-path'),
        'gunakan #hi-refresh (nama admin) — hi-arrow-path adalah varian Heroicons lain');
});

// ---------------------------------------------------------------------------
// M27 — public-desktop.css tidak menimpa .main milik hasil.css
// ---------------------------------------------------------------------------

test('B30-3 (M27): public-desktop.css tanpa rule .main — hasil.css satu-satunya pemilik', () => {
    assert.doesNotMatch(PUBLIC_DESKTOP_CSS, /(^|[,{\s])\.main\s*[,{]/m,
        '.main dipakai HANYA hasil.html; override 1180px di public-desktop.css ' +
        'menimpa layout halaman (padding-top clearing nav hilang) karena load-order');
    // Pemilik sah tetap utuh:
    const mainBlock = HASIL_CSS.match(/\.main\s*\{[^}]*\}/);
    assert.ok(mainBlock, '.main masih terdefinisi di hasil.css');
    assert.match(mainBlock[0], /max-width:\s*1100px/, 'hasil.css tetap mengatur max-width .main');
    assert.match(mainBlock[0], /padding-top:\s*calc\(var\(--public-nav-height\)/,
        'hasil.css tetap mengatur padding-top clearing nav fixed');
});
