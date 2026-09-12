/* Contract tests untuk Batch 29 — aksesibilitas state & delegasi aksi
 * (review_ui_halaman_web_2026-09-12.md, M17/M18/M19/M20).
 *
 *   M17 — packagesTableBody satu-satunya region data settings.html tanpa
 *         aria-live (pembanding: usersTableBody :836, myPackagesList :977,
 *         vouchersTableBody :1073, auditLogsBody :1132 semua aria-live="polite").
 *   M18 — tooltip settings.html hanya merespons :hover; pengguna keyboard yang
 *         mem-fokus elemen ber-.tooltip tidak pernah melihat tooltip.
 *   M19 — 5 halaman auth merender public_auth_nav SEBELUM public_skip_link —
 *         urutan terbalik: tab pertama mendarat di nav, bukan lompat ke konten.
 *   M20 — baris skor & tombol pagination hasil.html dipasang via properti
 *         .onclick per elemen (puluhan handler per render), melanggar pola S59
 *         yang halaman ini sendiri dokumentasikan (:180-183); delegasi ke
 *         registry Actions (data-action) + tetap simpan keydown Enter/Space.
 *
 * Run with: node --test static/js/uiux-batch29-a11y-state.test.mjs (from webui/)
 */

import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const HERE = path.dirname(fileURLToPath(import.meta.url));
const WEBUI_ROOT = path.join(HERE, '..', '..');
const read = (...p) => fs.readFileSync(path.join(...p), 'utf8');

const SETTINGS_HTML = read(WEBUI_ROOT, 'templates', 'admin', 'settings.html');
const HASIL_HTML = read(WEBUI_ROOT, 'templates', 'public', 'hasil.html');
const AUTH_PAGES = ['cek_hasil', 'forgot_password', 'register', 'register_confirm', 'reset_password']
    .map((n) => ({ name: n, src: read(WEBUI_ROOT, 'templates', 'public', `${n}.html`) }));

// ---------------------------------------------------------------------------
// M17 — packagesTableBody wajib aria-live
// ---------------------------------------------------------------------------

test('B29-1 (M17): packagesTableBody membawa aria-live="polite" seperti 4 region data lain', () => {
    const tag = SETTINGS_HTML.match(/<tbody id="packagesTableBody"[^>]*>/);
    assert.ok(tag, 'tbody packages ditemukan (prasyarat kontrak)');
    assert.match(tag[0], /aria-live="polite"/,
        'packagesTableBody satu-satunya region data tanpa aria-live — isi tabel (termasuk ' +
        'pesan gagal-muat) tidak pernah diumumkan ke pembaca layar');
});

// ---------------------------------------------------------------------------
// M18 — tooltip merespons keyboard (focus-within), bukan hover saja
// ---------------------------------------------------------------------------

test('B29-2 (M18): tooltip kustom dihapus — satu mekanisme native title (L28)', () => {
    // L28 (review_web_flow_dan_dead_code.md): dua mekanisme tooltip pada satu
    // halaman disatukan — 5 tooltip kustom .tooltip[data-tooltip] dimigrasi
    // ke native title (±70 pemakai lain). Kontrak kini: TIDAK ada lagi
    // tooltip kustom di settings.html.
    assert.doesNotMatch(SETTINGS_HTML, /class="tooltip"/,
        'tooltip kustom tersisa — migrasi ke native title belum tuntas');
    assert.doesNotMatch(SETTINGS_HTML, /\.tooltip\s*\{/,
        'CSS komponen tooltip kustom harus ikut dihapus (dead code)');
    // Informasi yang tadinya hanya di tooltip kini juga di title:
    assert.match(SETTINGS_HTML, /title="Maksimal total ujian/,
        'tooltip kuota tetap tersedia via native title');
});

// ---------------------------------------------------------------------------
// M19 — skip link sebelum nav di 5 halaman auth
// ---------------------------------------------------------------------------

test('B29-3 (M19): public_skip_link dirender SEBELUM public_auth_nav di 5 halaman auth', () => {
    const offenders = AUTH_PAGES
        .filter(({ src }) => src.indexOf('template "public_skip_link"') > src.indexOf('template "public_auth_nav"'))
        .map(({ name }) => name);
    assert.deepEqual(offenders, [],
        'skip link muncul SETELAH nav — Tab pertama mendarat di nav, bukan lompat ke konten: ' + offenders.join(', '));
});

// ---------------------------------------------------------------------------
// M20 — baris skor & pagination hasil.html via registry Actions
// ---------------------------------------------------------------------------

test('B29-4 (M20): hasil.html tanpa properti .onclick — delegasi via data-action + registry', () => {
    const onclicks = [...HASIL_HTML.matchAll(/\.onclick\s*=/g)];
    assert.deepEqual(onclicks.map((m) => m[0]), [],
        'handler per-elemen via properti .onclick = puluhan listener per render & di luar ' +
        'registry Actions (pola S59 yang halaman ini tulis sendiri di komentar :180-183)');

    // Baris skor: data-action + data-sub-id, keydown Enter/Space TETAP ada (a11y).
    const rowStart = HASIL_HTML.indexOf("const tr = document.createElement('tr');");
    assert.notEqual(rowStart, -1, 'blok pembuatan baris skor ditemukan');
    const rowBlock = HASIL_HTML.slice(rowStart, rowStart + 900);
    assert.match(rowBlock, /setAttribute\('data-action', 'toggle-score-detail'\)/,
        'baris skor wajib membawa data-action untuk delegasi registry');
    assert.match(rowBlock, /data-sub-id/,
        'id submission dibawa via atribut data, bukan closure .onclick');
    assert.match(rowBlock, /e\.key === 'Enter' \|\| e\.key === ' '/,
        'keydown Enter/Space per baris wajib dipertahankan (keyboard path)');

    // Registry: handler baris + pagination terdaftar.
    assert.match(HASIL_HTML, /Actions\.register\('toggle-score-detail'/,
        'handler toggle-score-detail wajib terdaftar di registry Actions');
    assert.match(HASIL_HTML, /Actions\.register\('page-goto'/,
        'handler pagination angka wajib terdaftar di registry Actions');
});
