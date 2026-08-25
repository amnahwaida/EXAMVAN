/* Batch 8 — lanjutan fase 2 design token (S15): migrasi hex/rgba template publik.
 *
 * Latar belakang & dampak bisnis:
 *   Re-review ronde 2 mencatat medan fase 2 design token: template publik
 *   (download/shared/register) masih memuat puluhan hex & rgba literal di
 *   blok <style> inline maupun atribut style — padahal seluruh halaman publik
 *   memuat theme.css sehingga token CSS custom properties RESOLVED. Literal
 *   liar membuat tema sulit diubah dan menaikkan risiko inkonsistensi visual
 *   antarhalaman (pola temuan S15/T9). Batch 8 menambahkan token triplet
 *   hitam/putih (--rgb-black/--rgb-white, kontrak lintas-agen) lalu memigrasi
 *   nilai-nilai yang punya padanan token PERSIS via substitusi nilai-persis
 *   (NOL perubahan visual). Test ini MENGUNCI hasil migrasi:
 *     1. Kontrak token hitam/putih eksis di theme.css.
 *     2. Tidak ada lagi literal rgba hitam/putih di ketiga template.
 *     3. Tidak ada lagi literal rgba triplet warna brand (--rgb-info/
 *        success/warning/danger/accent) di ketiga template.
 *     4. Hex yang punya padanan token semantik tak lagi ditulis literal,
 *        KECUALI whitelist eksplisit (definisi token lokal & meta tag —
 *        konteks non-CSS yang memang harus literal).
 *
 * Run with:  node --test static/js/uiux-batch8-publik.test.mjs   (from webui/)
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const PUBLIC = path.join(__dirname, '..', '..', 'templates', 'public');
const THEME = path.join(__dirname, '..', '..', 'static', 'css', 'theme.css');

// S82/S83 (ronde 8): register_confirm, reset_password & hasil.html ikut
// terjaga — sebelumnya blind-spot (tumbuh literal tanpa alarm karena tidak
// masuk FILES). Baseline = angka aktual pasca migrasi token ronde 8
// (hasil: 6 hitungan seluruhnya false-positive entity HTML &#8226;).
// Batch 15 (R118): cek_hasil/index/forgot_password ikut masuk daftar
// (informative; aktual hex/rgba = 0 hari ini) supaya literal PERTAMA di
// halaman mana pun langsung memerah.
const FILES = ['download.html', 'shared.html', 'register.html',
    'register_confirm.html', 'reset_password.html', 'hasil.html',
    'cek_hasil.html', 'index.html', 'forgot_password.html']
    .map((f) => path.join(PUBLIC, f));
const read = (f) => fs.readFileSync(f, 'utf8');

// ===== 1. Kontrak lintas-agen: triplet hitam/putih di theme.css =============

test('B8 (kontrak): theme.css mendefinisikan --rgb-black & --rgb-white sesuai kontrak', () => {
    const src = read(THEME);
    assert.match(src, /--rgb-black:\s*0,\s*0,\s*0;/, '--rgb-black: 0, 0, 0 wajib eksis di theme.css');
    assert.match(src, /--rgb-white:\s*255,\s*255,\s*255;/, '--rgb-white: 255, 255, 255 wajib eksis di theme.css');
});

// ===== 2. rgba hitam/putih → rgba(var(--rgb-black|white), α) ================

const RGBA_BLACK_RE = /rgba\(\s*0\s*,\s*0\s*,\s*0/g;
const RGBA_WHITE_RE = /rgba\(\s*255\s*,\s*255\s*,\s*255/g;

for (const f of FILES) {
    const name = path.basename(f);
    test(`B8 (${name}): tidak ada lagi literal rgba(0,0,0,α) — pakai rgba(var(--rgb-black), α)`, () => {
        const hits = read(f).match(RGBA_BLACK_RE) || [];
        assert.equal(hits.length, 0,
            `${name}: ${hits.length} literal rgba hitam tersisa — migrasi ke rgba(var(--rgb-black), α)`);
    });
    test(`B8 (${name}): tidak ada lagi literal rgba(255,255,255,α) — pakai rgba(var(--rgb-white), α)`, () => {
        const hits = read(f).match(RGBA_WHITE_RE) || [];
        assert.equal(hits.length, 0,
            `${name}: ${hits.length} literal rgba putih tersisa — migrasi ke rgba(var(--rgb-white), α)`);
    });
}

// ===== 3. rgba triplet warna brand → rgba(var(--rgb-*), α) ==================
// Nilai triplet WAJIB identik dengan theme.css (kontrak Batch 7): info=99,102,241
// success=16,185,129 warning=245,158,11 danger=239,68,68 accent=168,85,247.

const TRIPLET_MAP = [
    ['99, 102, 241', 'var(--rgb-info)'],
    ['16, 185, 129', 'var(--rgb-success)'],
    ['245, 158, 11', 'var(--rgb-warning)'],
    ['239, 68, 68', 'var(--rgb-danger)'],
    ['168, 85, 247', 'var(--rgb-accent)'],
];

for (const f of FILES) {
    const name = path.basename(f);
    for (const [triplet] of TRIPLET_MAP) {
        const re = new RegExp(`rgba\\(\\s*${triplet.split(', ').join('\\s*,\\s*')}`, 'g');
        test(`B8 (${name}): rgba(${triplet}, α) literal sudah dimigrasi ke triplet token`, () => {
            const hits = read(f).match(re) || [];
            assert.equal(hits.length, 0,
                `${name}: ${hits.length} literal rgba(${triplet}) tersisa — pakai rgba(var(--rgb-*), α)`);
        });
    }
}

test('B8 (sanity): pemakaian rgba(var(--rgb-*) di template publik nyata terjadi', () => {
    let uses = 0;
    for (const f of FILES) uses += (read(f).match(/rgba\(var\(--rgb-/g) || []).length;
    assert.ok(uses >= 30, `pemakaian rgba(var(--rgb-*) = ${uses}, wajib >= 30 (bukti migrasi berjalan)`);
});

// ===== 4. Hex bersubstitusi → token semantik (whitelist eksplisit) ==========

// Pasangan hex → token semantik theme.css yang nilainya PERSIS sama.
const HEX_TOKEN_MAP = [
    ['#a5b4fc', '--color-primary-light'],
    ['#c7d2fe', '--color-primary-soft'],
    ['#8b5cf6', '--color-accent'],
    ['#a78bfa', '--color-accent-light'],
    ['#34d399', '--color-success-light'],
    ['#10b981', '--color-success'],
    ['#f59e0b', '--color-warning'],
    ['#fbbf24', '--color-warning-light'],
    ['#ef4444', '--color-danger'],
    ['#fca5a5', '--color-danger-light'],
    ['#6366f1', '--color-primary'],
    ['#111827', '--color-bg-secondary'],
];

// Whitelist baris yang SENGJAHA tetap literal (komentar alasan wajib di sumber):
//   - shared.html blok :root lokal (definisi token TIDAK boleh self-referential):
//     --bg-secondary: #111827, --accent-primary: #6366f1 (2×), --accent-secondary: #8b5cf6
//   - shared.html <meta name="theme-color" content="#09090e"> (atribut HTML,
//     bukan CSS — var() tidak resolve di meta content).
const WHITELIST = [
    { file: 'shared.html', reason: 'definisi token lokal :root / meta theme-color' },
];

function stripWhitelisted(name, src) {
    if (!WHITELIST.some((w) => w.file === name)) return src;
    return src
        .replace(/--bg-secondary:\s*#111827/g, '')
        .replace(/--accent-primary:\s*#6366f1/g, '')
        .replace(/--accent-secondary:\s*#8b5cf6/g, '')
        .replace(/<meta\s+name="theme-color"[^>]*>/g, '');
}

for (const f of FILES) {
    const name = path.basename(f);
    for (const [hex, token] of HEX_TOKEN_MAP) {
        test(`B8 (${name}): hex ${hex} diganti var(${token}) (kecuali whitelist)`, () => {
            const src = stripWhitelisted(name, read(f));
            const re = new RegExp(hex.replace('#', '#') + '\\b', 'gi');
            const hits = src.match(re) || [];
            assert.equal(hits.length, 0,
                `${name}: ${hits.length} literal ${hex} tersisa — pakai var(${token})`);
        });
    }
}

// ===== 5. Baseline terkunci pasca-migrasi (tidak boleh naik lagi) ===========

const HEX_RE = /#[0-9a-fA-F]{3,8}\b/g;
// Batch 15 (S95, kontrak ronde 5 "plafon = aktual"): seluruh plafon DIKUNCI
// ULANG ke hasil ukur kondisi sumber sekarang (migrasi S80–S83 agen lain
// menurunkan banyak literal) dan asersi assert.ok(≤) → assert.equal. Lama → baru:
//   download 20→7 · shared 25→14 · register 12→0 · register_confirm 0→0 ·
//   reset_password 0→0 · hasil 6→6 · cek_hasil/index/forgot_password baru = 0.
const BASELINE_HEX = {
    'download.html': 7,
    'shared.html': 14,
    'register.html': 1, /* +theme-color (R130) */
    'register_confirm.html': 1, /* +theme-color (R130) */
    'reset_password.html': 1, /* +theme-color (R130) */
    'hasil.html': 6,
    'cek_hasil.html': 1, /* +theme-color (R130) */
    'index.html': 0,
    'forgot_password.html': 1, /* +theme-color (R130) */
};

for (const f of FILES) {
    const name = path.basename(f);
    test(`B8/S95 (guard): jumlah hex literal ${name} == baseline aktual`, () => {
        const n = (stripWhitelisted(name, read(f)).match(HEX_RE) || []).length;
        assert.equal(n, BASELINE_HEX[name],
            `${name}: hex = ${n}, baseline terkunci tepat ${BASELINE_HEX[name]} — pakai var(--token); ` +
            'turunkan baseline setiap migrasi mengurangi literal (jangan naikkan)');
    });
}
