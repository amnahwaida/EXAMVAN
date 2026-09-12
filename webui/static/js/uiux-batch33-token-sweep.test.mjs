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
