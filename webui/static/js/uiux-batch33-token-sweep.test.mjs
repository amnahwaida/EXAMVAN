/* Contract tests untuk Batch 33 — token sweep presisi (review L47, L48, L49, L50).
 *
 *   L47 — dashboard.html memakai hex preset panel mentah (#F43F5E, #06B6D4,
 *         #64748B, data-color #F59E0B) & ikon #fbbf24 — di luar sistem token.
 *         Kontrak baru: data-color membawa token triplet (--rgb-rose-500 dll.);
 *         hex diturunkan hanya di SATU titik (resolvePanelHex, admin.js) via
 *         getComputedStyle — perubahan palet ikut ke preset.
 *   L48 — pengawas.html: #60a5fa (ikon, barColor, badge Statis) dan #c084fc
 *         (badge Dinamis) tidak melalui token.
 *   L49 — pengawas_detail.html: #60a5fa (titik log heartbeat), #6ee7b7
 *         (detail log login) literal.
 *   L50 — admin.js: 8 literal rgba() yang punya pasangan triplet di theme.css.
 *
 * Run with: node --test static/js/uiux-batch33-token-sweep.test.mjs (from webui/)
 */

import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const HERE = path.dirname(fileURLToPath(import.meta.url));
const WEBUI_ROOT = path.join(HERE, '..', '..');
const read = (...p) => fs.readFileSync(path.join(...p), 'utf8');

const DASHBOARD = read(WEBUI_ROOT, 'templates', 'admin', 'dashboard.html');
const PENGAWAS = read(WEBUI_ROOT, 'templates', 'admin', 'pengawas.html');
const DETAIL = read(WEBUI_ROOT, 'templates', 'admin', 'pengawas_detail.html');
const ADMIN_JS = read(WEBUI_ROOT, 'static', 'js', 'admin.js');
const THEME = read(WEBUI_ROOT, 'static', 'css', 'theme.css');

// ---------------------------------------------------------------------------
// Prasyarat — triplet baru terdefinisi (kontrak substitusi nilai-persis)
// ---------------------------------------------------------------------------

test('L47-L50: theme.css mendefinisikan triplet/pasangan warna baru (nilai persis)', () => {
    const expected = {
        '--rgb-gray': '107, 114, 128',
        '--color-gray-light': '#9ca3af',
        '--rgb-blue-400': '96, 165, 250',
        '--rgb-rose-500': '244, 63, 94',
        '--rgb-cyan-500': '6, 182, 212',
        '--rgb-slate-500': '100, 116, 139',
        '--rgb-warning-dim': '251, 191, 36',
    };
    for (const [name, value] of Object.entries(expected)) {
        const re = new RegExp(`${name.replace(/-/g, '\\-')}:\\s*${value.replace(/,/g, '\\,').replace(/\s/g, '\\s*')}\\s*;`.replace(/\\,\s\*/g, '\\,\\s*'));
        assert.match(THEME, re, `token ${name}: ${value} wajib eksis di theme.css (source of truth)`);
    }
});

// ---------------------------------------------------------------------------
// L47 — dashboard preset warna panel & ikon warning
// ---------------------------------------------------------------------------

test('L47a: preset panel dashboard — enam data-color, tanpa hex mati untuk preset bertoken', () => {
    const presets = [...DASHBOARD.matchAll(/data-action="panel-color-set"[^>]*data-color="([^"]+)"/g)]
        .map((m) => m[1]);
    assert.equal(presets.length, 6, 'enam preset warna panel (prasyarat)');
    for (const c of presets) {
        assert.match(c, /^(#[0-9A-F]{6}|var\(--[\w-]+\))$/, `data-color="${c}" harus hex resmi atau token`);
    }
    // Indigo & Emerald ke token warna semantik lama tetap hex (nilai awal input);
    // empat preset lain wajib token triplet (lihat L47b).
});

test('L47b: preset Rose/Cyan/Slate/Amber membawa token (bukan hex mati)', () => {
    const presets = Object.fromEntries(
        [...DASHBOARD.matchAll(/data-action="panel-color-set"[^>]*data-color="([^"]+)"[^>]*>([A-Za-z]+)</g)]
            .map((m) => [m[2], m[1]]));
    assert.equal(presets['Rose'], 'var(--rgb-rose-500)');
    assert.equal(presets['Amber'], 'var(--rgb-warning-dim)');
    assert.equal(presets['Cyan'], 'var(--rgb-cyan-500)');
    assert.equal(presets['Slate'], 'var(--rgb-slate-500)');
    for (const bad of ['#F43F5E', '#06B6D4', '#64748B', '#F59E0B']) {
        assert.doesNotMatch(DASHBOARD, new RegExp(`data-color="${bad}"`),
            `hex mati ${bad} tidak boleh kembali di data-color`);
    }
});

test('L47c: resolvePanelHex — satu titik konversi hex dari token, getComputedStyle', () => {
    assert.match(ADMIN_JS, /function resolvePanelHex\(/,
        'konversi hex wajib terpusat di resolvePanelHex (L47: satu titik derivasi)');
    assert.match(ADMIN_JS, /getComputedStyle\(document\.documentElement\)\.getPropertyValue/,
        'konversi membaca triplet via getComputedStyle');
    assert.doesNotMatch(DASHBOARD, /data-color="#F43F5E"|data-color="#06B6D4"/,
        'preset tidak lagi membawa hex mati');
    // Sisa pemakai hex literal dashboard yang sah: nilai awal input (bukan data set).
    const initialHex = DASHBOARD.match(/id="examPanelColor"[^>]*value="([^"]+)"/);
    assert.ok(initialHex && /^#[0-9A-F]{6}$/.test(initialHex[1]),
        'input color tetap berangkat dari hex awal (batas platform input[type=color])');
});

// ---------------------------------------------------------------------------
// L48 — pengawas.html
// ---------------------------------------------------------------------------

test('L48: pengawas.html bebas #60a5fa/#c084fc literal — token rgb-blue-400 & accent-light', () => {
    assert.doesNotMatch(PENGAWAS, /#60a5fa/, 'ikon & badge biru wajib rgb(var(--rgb-blue-400))');
    assert.doesNotMatch(PENGAWAS, /#c084fc/, 'badge Dinamis wajib var(--color-accent-light)');
    assert.match(PENGAWAS, /color:\s*rgb\(var\(--rgb-blue-400\)\)/);
    assert.match(PENGAWAS, /color:var\(--color-accent-light\)/);
    // Tint badge mengikuti token triplet yang sama (border & background):
    assert.match(PENGAWAS, /background:rgba\(var\(--rgb-blue-400\),0\.1\)/);
    assert.match(PENGAWAS, /border:1px solid rgba\(var\(--rgb-blue-400\),0\.2\)/);
});

// ---------------------------------------------------------------------------
// L49 — pengawas_detail.html
// ---------------------------------------------------------------------------

test('L49: pengawas_detail.html bebas #60a5fa & #6ee7b7 literal', () => {
    assert.doesNotMatch(DETAIL, /#60a5fa/, 'titik log heartbeat wajib rgb(var(--rgb-blue-400))');
    assert.doesNotMatch(DETAIL, /#6ee7b7/, 'detail log login wajib var(--color-success-light)');
    assert.match(DETAIL, /\.log-entry\.heartbeat::before\s*\{\s*background:\s*rgb\(var\(--rgb-blue-400\)\)/);
    assert.match(DETAIL, /color:var\(--color-success-light\);\s*">Jawaban telah dikirimkan/);
});

// ---------------------------------------------------------------------------
// L61 — medali peringkat hasil.css
// ---------------------------------------------------------------------------

test('L61: warna medali hasil.css lewat token --color-medal-*', () => {
    const HASIL_CSS = read(WEBUI_ROOT, 'static', 'css', 'hasil.css');
    assert.match(HASIL_CSS, /\.rank-num\.rank-2\s*\{\s*color:\s*var\(--color-medal-silver\)/);
    assert.match(HASIL_CSS, /\.rank-num\.rank-3\s*\{\s*color:\s*var\(--color-medal-bronze\)/);
    assert.match(THEME, /--color-medal-silver:\s*#e2e8f0;/);
    assert.match(THEME, /--color-medal-bronze:\s*#d97706;/);
    assert.doesNotMatch(HASIL_CSS, /#e2e8f0|#d97706/, 'hex medali tidak boleh kembali');
});

// ---------------------------------------------------------------------------
// L62 — literal warna download.html
// ---------------------------------------------------------------------------

test('L62: trio ikon platform konsisten + literal ungu/biru download masuk token', () => {
    const DOWNLOAD = read(WEBUI_ROOT, 'templates', 'public', 'download.html');
    // Trio platform: android=success, windows=blue-500, linux=warning — semua token.
    assert.match(DOWNLOAD, /\.icon-windows\s*\{[^}]*rgba\(var\(--rgb-blue-500\),\s*0\.15\)/);
    assert.match(DOWNLOAD, /\.icon-windows\s*\{[^}]*color:\s*var\(--color-blue-500\)/);
    assert.match(DOWNLOAD, /\.icon-android\s*\{[^}]*rgba\(var\(--rgb-success\)/);
    assert.match(DOWNLOAD, /\.icon-linux\s*\{[^}]*rgba\(var\(--rgb-warning\)/);
    // Badge versi baru: gradien penuh token, bukan setengah hex.
    assert.match(DOWNLOAD, /linear-gradient\(135deg,\s*var\(--color-success\),\s*var\(--color-success-deep\)\)/);
    assert.match(DOWNLOAD, /color:\s*var\(--color-text-on-success\)/);
    // Flavor-box ungu & teks indigo muda.
    assert.match(DOWNLOAD, /rgba\(var\(--rgb-violet-500\),0\.35\)/);
    assert.match(DOWNLOAD, /color:var\(--color-indigo-300\)/);
    // Duplikat tertulis rgba(129,140,248,…) --color-primary-bright tak boleh kembali.
    assert.doesNotMatch(DOWNLOAD, /rgba\(\s*129\s*,\s*140\s*,\s*248/);
    assert.match(DOWNLOAD, /rgba\(var\(--rgb-primary-bright\),0\.2\)/);
    // Token pendamping terdefinisi di theme.css.
    for (const tok of ['--rgb-blue-500: 59, 130, 246', '--color-blue-500: #3b82f6',
                       '--color-success-deep: #059669', '--color-text-on-success: #04120c',
                       '--rgb-primary-bright: 129, 140, 248', '--rgb-violet-500: 139, 92, 246',
                       '--color-indigo-300: #c4b5fd']) {
        assert.ok(THEME.includes(tok), `theme.css wajib mendefinisikan ${tok.split(':')[0]}`);
    }
});

// ---------------------------------------------------------------------------
// L70 — census putih nol di templates + CSS inti publik/admin
// ---------------------------------------------------------------------------

test('L70: nol literal putih di templates/ & CSS inti (output.css dikecualikan — artefak build)', () => {
    const walk = (dir, acc = []) => {
        for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
            const p = path.join(dir, e.name);
            if (e.isDirectory()) walk(p, acc);
            else if (e.name.endsWith('.html')) acc.push(p);
        }
        return acc;
    };
    const WHITE_RE = /rgba\(\s*255\s*,\s*255\s*,\s*255|#fff(?:fff)?\b/;
    const offenders = walk(path.join(WEBUI_ROOT, 'templates'))
        .filter((f) => WHITE_RE.test(read(f).replace(/&#\d+;|&#x[0-9a-fA-F]+;/g, '')))
        .map((f) => path.relative(WEBUI_ROOT, f));
    assert.deepEqual(offenders, [], 'template masih memakai literal putih: ' + offenders.join(', '));
    for (const css of ['hasil.css', 'admin-base.css', 'public-mobile.css', 'public-desktop.css', 'public-toast.css']) {
        const src = read(WEBUI_ROOT, 'static', 'css', css);
        assert.doesNotMatch(src, WHITE_RE, `${css} masih memakai literal putih`);
    }
    // Token triplet putih tetap source of truth.
    assert.match(THEME, /--rgb-white:\s*255,\s*255,\s*255;/);
});

// ---------------------------------------------------------------------------
// L50 — admin.js rgba literal
// ---------------------------------------------------------------------------

test('L50: admin.js — 8 rgba literal tombol status diganti rgba(var(--rgb-*), α)', () => {
    for (const lit of ['rgba(16, 185, 129', 'rgba(239, 68, 68', 'rgba(251, 191, 36',
                       'rgba(107, 114, 128']) {
        assert.doesNotMatch(ADMIN_JS, new RegExp(lit.replace(/\s/g, '\\s*').replace(/\(/g, '\\(')),
            `${lit}, …) masih literal — wajib rgba(var(--rgb-*), α)`);
    }
    assert.match(ADMIN_JS, /rgba\(var\(--rgb-success\),\s*0\.15\)/);
    assert.match(ADMIN_JS, /rgba\(var\(--rgb-danger\),\s*0\.3\)/);
    assert.match(ADMIN_JS, /rgba\(var\(--rgb-warning-dim\),\s*0\.15\)/);
    assert.match(ADMIN_JS, /rgba\(var\(--rgb-gray\),\s*0\.3\)/);
});
