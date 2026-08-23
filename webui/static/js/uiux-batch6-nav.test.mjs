/* Contract tests statik untuk Batch 6 (bagian nav): landmark <header> pada
 * topbar admin — item R19 lanjutan di review_uiux_webui.md.
 *
 * Latar: topbar admin dirender oleh partial templates/admin/partials/nav.html
 * tanpa elemen landmark semantic; hasilnya 0 <header> di hampir semua halaman
 * admin (kecuali hasil.html). Perbaikan: bungkus blok topbar dengan <header>,
 * TANPA mengubah markup/class/logika template di dalamnya.
 *
 * Gaya: fs-read statik (pola uiux-batch3-settings-nav.test.mjs).
 * Run:   node --test webui/static/js/uiux-batch6-nav.test.mjs
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const WEBUI_ROOT = path.join(__dirname, '..', '..');
const read = (rel) => fs.readFileSync(path.join(WEBUI_ROOT, rel), 'utf8');

const navHtml = () => read('templates/admin/partials/nav.html');

// ---------------------------------------------------------------------------
// Landmark <header> membungkus topbar
// ---------------------------------------------------------------------------

test('R19a: nav.html membungkus topbar dengan elemen <header>', () => {
    const html = navHtml();

    const openIdx = html.indexOf('<header');
    const closeIdx = html.indexOf('</header>');
    const topbarIdx = html.indexOf('<nav class="topbar"');

    assert.ok(openIdx !== -1, 'harus ada tag pembuka <header ...> di nav.html');
    assert.ok(closeIdx !== -1, 'harus ada tag penutup </header> di nav.html');
    assert.ok(topbarIdx !== -1, 'elemen <nav class="topbar"> tetap ada');

    assert.ok(
        openIdx < topbarIdx && topbarIdx < closeIdx,
        '<nav class="topbar"> harus berada DI DALAM pasangan <header>...</header>'
    );
});

test('R19b: landmark tunggal — hanya SATU <header> di nav.html', () => {
    const html = navHtml();
    const opens = [...html.matchAll(/<header[\s>]/g)].length;
    const closes = [...html.matchAll(/<\/header>/g)].length;

    assert.equal(opens, 1, `harus tepat satu tag pembuka <header> (dapat ${opens})`);
    assert.equal(closes, 1, `harus tepat satu tag penutup </header> (dapat ${closes})`);
});

test('R19c: <header> tidak membawa class/atribut styling — styling tetap milik .topbar', () => {
    // Kontrak penting agar posisi fixed/sticky & tampilan tak berubah:
    // seluruh styling (.topbar: sticky, grid/flex, backdrop-filter) terikat
    // pada class .topbar di elemen DALAM; wrapper <header> harus polos.
    const m = navHtml().match(/<header([^>]*)>/);
    assert.ok(m, 'tag <header> ada');
    assert.doesNotMatch(m[1], /class=/, '<header> tidak boleh diberi class baru');
    assert.doesNotMatch(m[1], /style=/, '<header> tidak boleh diberi inline style');
});

// ---------------------------------------------------------------------------
// Regresi: atribut a11y existing di nav.html tetap utuh
// ---------------------------------------------------------------------------

test('R19d-regresi: aria-current="page" masih ada dan terikat kondisi $activePage/$isSettings', () => {
    const html = navHtml();
    const lines = html.split('\n').filter((l) => l.includes('aria-current="page"'));
    assert.ok(lines.length >= 8, `aria-current harus ada di >=8 baris (4 link topbar + 4 dropdown) — dapat ${lines.length}`);
    for (const line of lines) {
        assert.match(line, /\{\{if (eq \$activePage|\$isSettings)/,
            `setiap aria-current harus terikat kondisi Go — baris: ${line.trim().slice(0, 100)}`);
    }
});

test('R19e-regresi: hamburger #menuToggleBtn tetap punya aria-label + aria-expanded="false"', () => {
    const btn = navHtml().match(/<button[^>]*id="menuToggleBtn"[^>]*>/);
    assert.ok(btn, 'tombol #menuToggleBtn ada');
    assert.match(btn[0], /aria-label="Menu akun &amp; pengaturan"|aria-label="Menu akun & pengaturan"/,
        'aria-label hamburger tetap ada');
    assert.match(btn[0], /aria-haspopup="true"/, 'aria-haspopup hamburger tetap ada');
    assert.match(btn[0], /aria-expanded="false"/, 'state awal aria-expanded tetap "false"');
});

test('R19f-regresi: skip-link WCAG tetap jadi elemen pertama sebelum <header>', () => {
    const html = navHtml();
    const skipIdx = html.indexOf('class="skip-link"');
    const headerIdx = html.indexOf('<header');
    assert.ok(skipIdx !== -1, 'skip-link tetap ada');
    assert.ok(headerIdx !== -1, '<header> ada');
    assert.ok(skipIdx < headerIdx, 'skip-link harus berada SEBELUM <header> (urutan fokus keyboard)');
});
