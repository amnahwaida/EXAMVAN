/* Regression contract tests untuk Batch 4 perbaikan UI/UX (bagian CSS design
 * token). Referensi temuan: review_uiux_webui.md ID S15 & S17.
 *
 * Run with:  node --test static/js/uiux-batch4-tokens.test.mjs   (from webui/)
 *
 * Keputusan yang dikunci oleh test ini:
 *   - S17 — Palet light (:root[data-theme="light"]) DIHAPUS dari theme.css.
 *     Keputusan final Batch 4: dead code tanpa mekanisme aktivasi; aplikasi
 *     adalah dark-by-design (surfaces hardcoded dark, prefers-color-scheme
 *     menghasilkan UI setengah-light yang rusak). Bila kelak dibutuhkan,
 *     hidupkan kembali via git history. color-scheme: dark TETAP ada.
 *   - S15 fase 1 — Design token ditegakkan secara bertahap di admin-base.css:
 *     HANYA substitusi nilai PERSIS (hex ad-hoc -> var(token), radius literal
 *     yang nilainya sama dengan token -> var(token), box-shadow identik ->
 *     var(token)). Nilai visual tidak boleh berubah sama sekali. rgba surface
 *     putih-transparan ad-hoc sengaja DIBIARKAN untuk fase 2.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const WEBUI_ROOT = path.join(__dirname, '..', '..');
const read = (rel) => fs.readFileSync(path.join(WEBUI_ROOT, rel), 'utf8');
/** Baca CSS dengan komentar dibuang — komentar boleh memuat hex/radius
 * contoh tanpa mengelabui penghitungan deklarasi. */
const readCss = (rel) => read(rel).replace(/\/\*[\s\S]*?\*\//g, '');

const THEME_CSS = readCss('static/css/theme.css');
const THEME_RAW = read('static/css/theme.css');
const ADMIN_CSS = readCss('static/css/admin-base.css');
const REPO_ROOT = path.join(WEBUI_ROOT, '..');

// Baseline review_uiux_webui.md (Batch 4): admin-base.css memuat 40+ hex
// literal dan 0 pemakaian var(--radius-*. Setelah migrasi S15 fase 1,
// 10 kemunculan lima hex target hilang -> hitungan turun signifikan.
const BASELINE_HEX_COUNT = 41;
const EXPECTED_HEX_AFTER_MIGRATION = 31;

// ---------------------------------------------------------------------------
// S17 — palet light dihapus (dead code), dark-by-design dipertahankan
// ---------------------------------------------------------------------------

test('S17: theme.css tidak lagi mendefinisikan :root[data-theme="light"]', () => {
    assert.doesNotMatch(THEME_CSS, /data-theme\s*=\s*"light"/,
        'blok palet light harus sudah dihapus dari theme.css (keputusan Batch 4)');
});

test('S17: theme.css tetap color-scheme: dark (dark-by-design)', () => {
    assert.match(THEME_CSS, /color-scheme:\s*dark/, 'color-scheme: dark wajib dipertahankan');
});

test('S17: komentar penanda keputusan penghapusan palet light ada di theme.css', () => {
    // Penanda dicari pada konten mentah karena memang berupa komentar CSS.
    assert.match(THEME_RAW, /Batch 4/, 'harus ada komentar yang menyebut keputusan Batch 4');
    assert.match(THEME_RAW, /(dead code|git history)/i,
        'komentar harus menjelaskan alasan (dead code) & cara memulihkan (git history)');
});

// ---------------------------------------------------------------------------
// S15 fase 1a — token baru terdefinisi di theme.css dengan nilai benar
// ---------------------------------------------------------------------------

test('S15: token baru terdefinisi di theme.css dengan nilai persis', () => {
    const expected = {
        '--radius-xs': '4px',               // radius paling kecil (focus ring chip, dll.)
        '--color-danger-light': '#fca5a5',   // teks danger di atas surface gelap
        '--color-primary-light': '#a5b4fc',  // sudah ada sejak awal — dikunci agar tak bergeser
        '--color-primary-soft': '#c7d2fe',   // teks primary versi soft
        '--color-text-placeholder': '#94a3b8', // placeholder & badge dorman
        '--color-warning-light': '#fbbf24',  // teks warning terang
    };
    for (const [token, value] of Object.entries(expected)) {
        const re = new RegExp(token.replace(/[-]/g, '\\-') + '\\s*:\\s*' + value.replace('#', '#') + '\\s*;');
        assert.match(THEME_CSS, re, `${token}: ${value} harus terdefinisi di theme.css`);
    }
});

test('S15: token baru tidak menduplikasi semantik token lama', () => {
    // Setiap nama token baru harus unik (tidak dideklarasikan dua kali).
    const names = [...THEME_CSS.matchAll(/--[a-z-]+(?=\s*:)/g)].map((m) => m[0]);
    const dupes = names.filter((n, i) => names.indexOf(n) !== i);
    assert.deepEqual(dupes, [], `token duplikat di theme.css: ${[...new Set(dupes)].join(', ')}`);
});

// ---------------------------------------------------------------------------
// S15 fase 1b — substitusi nilai PERSIS di admin-base.css
// ---------------------------------------------------------------------------

test('S15: lima hex ad-hoc tidak lagi muncul sebagai literal di admin-base.css', () => {
    const banned = ['#fca5a5', '#a5b4fc', '#c7d2fe', '#94a3b8', '#fbbf24'];
    for (const hex of banned) {
        assert.ok(!ADMIN_CSS.includes(hex),
            `${hex} masih literal di admin-base.css — ganti dengan var() token-nya`);
    }
});

test('S15: substitusi tetap memakai token (var token lima warna hadir di admin-base.css)', () => {
    for (const token of ['--color-danger-light', '--color-primary-light',
        '--color-primary-soft', '--color-text-placeholder', '--color-warning-light']) {
        assert.ok(ADMIN_CSS.includes(`var(${token})`),
            `admin-base.css harus memakai var(${token}) hasil migrasi`);
    }
});

test('S15: hitungan hex literal admin-base.css turun signifikan vs baseline review', () => {
    const hexCount = (ADMIN_CSS.match(/#[0-9a-fA-F]{3,8}\b/g) || []).length;
    assert.ok(hexCount < BASELINE_HEX_COUNT,
        `hitungan hex (${hexCount}) harus lebih kecil dari baseline ${BASELINE_HEX_COUNT}`);
    assert.ok(hexCount <= EXPECTED_HEX_AFTER_MIGRATION,
        `hitungan hex (${hexCount}) melebihi target pasca-migrasi ${EXPECTED_HEX_AFTER_MIGRATION} — ada substitusi yang mundur`);
});

test('S15: pemakaian var(--radius-*) di admin-base.css naik dari 0 menjadi ≥ 10', () => {
    const uses = (ADMIN_CSS.match(/var\(--radius-(?:xs|sm|md|lg|xl)\)/g) || []).length;
    assert.ok(uses >= 10, `pemakaian var(--radius-*) hanya ${uses}; migrasi radius belum berjalan`);
});

test('S15: radius literal 4/10/12/16px yang setara token tidak tersisa di admin-base.css', () => {
    // Semua border-radius bernilai tepat 4px/10px/12px/16px wajib sudah
    // jadi var(--radius-xs/sm/md/lg). Nilai lain (3/6/8px, 999px, 50%, 0)
    // memang belum punya token dan dibiarkan di fase 1.
    const leftovers = [...ADMIN_CSS.matchAll(/border-radius\s*:\s*(4|10|12|16)px/g)];
    assert.deepEqual(leftovers.map((m) => m[0]), [],
        `radius literal setara token tersisa: ${leftovers.map((m) => m[0]).join(', ')}`);
});

// ---------------------------------------------------------------------------
// S15 fase 1c — guard no-visual-change (sanity): jumlah deklarasi stabil
// ---------------------------------------------------------------------------

test('S15/no-visual-change: jumlah total deklarasi radius & box-shadow tidak berubah drastis', () => {
    const radiusDecls = (ADMIN_CSS.match(/border-radius\s*:/g) || []).length;
    const shadowDecls = (ADMIN_CSS.match(/box-shadow\s*:/g) || []).length;
    // Baseline pra-migrasi: 27 radius, 6 shadow. Substitusi var() tidak boleh
    // menambah/mengurangi jumlah deklarasi (toleransi ketat ±0; guard anti
    // kehilangan rule saat edit massal).
    assert.equal(radiusDecls, 27, `jumlah deklarasi border-radius berubah (baseline 27, dapat ${radiusDecls})`);
    assert.equal(shadowDecls, 6, `jumlah deklarasi box-shadow berubah (baseline 6, dapat ${shadowDecls})`);
});

// ---------------------------------------------------------------------------
// S15 fase lanjutan — config stylelint disiapkan (belum aktif tanpa npm install)
// ---------------------------------------------------------------------------

test('S15: .stylelintrc.json ada di root repo dengan guard hex/rgba di luar theme.css', () => {
    const cfgPath = path.join(REPO_ROOT, '.stylelintrc.json');
    assert.ok(fs.existsSync(cfgPath), '.stylelintrc.json harus ada di root repo');
    const cfgRaw = fs.readFileSync(cfgPath, 'utf8');
    let cfg;
    assert.doesNotThrow(() => { cfg = JSON.parse(cfgRaw); }, '.stylelintrc.json harus JSON valid');
    const rules = cfg.rules || {};
    assert.ok(rules['declaration-property-value-disallowed-list'],
        'aturan declaration-property-value-disallowed-list wajib ada');
    assert.match(cfgRaw, /theme\.css/i,
        'config harus mendokumentasikan bahwa theme.css adalah satu-satunya pengecualian');
});
