/**
 * ══════════════════════════════════════════════════════════════════════════
 * Batch 42 — public-desktop.css & public-mobile.css: subset hidup
 * ══════════════════════════════════════════════════════════════════════════
 *
 * Referensi: review penyederhanakan 14 Nov 2026 (ronde 2, terkonfirmasi).
 * 22 kelas warisan landing-page v1 (billing/pricing/comparison/features/
 * adv/hero-badge/download-btn/results-header/save-badge/hasil-container/
 * filter-bar/page-header/stats-section/result-container) census word-match
 * + atom + classList/konkat = 0 pemakaian di seluruh templates/JS — rule-nya
 * dibuang (23 rules, -2.5KB). Verifikasi computed-style dif seluruh elemen
 * dilakukan saat penyaringan untuk halaman-halaman pemuat CSS ini
 * (index, download, login, register, register_confirm, forgot_password,
 * cek_hasil, reset_password, hasil) — hasil: IDENTIK.
 *
 * Run with: node --test static/js/uiux-batch42-publiccss-live.test.mjs
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const HERE = path.dirname(fileURLToPath(import.meta.url));
const WEBUI_ROOT = path.join(HERE, '..', '..');
const read = (...p) => fs.readFileSync(path.join(WEBUI_ROOT, ...p), 'utf8');
const DESKTOP = read('static', 'css', 'public-desktop.css');
const MOBILE = read('static', 'css', 'public-mobile.css');

const DEAD_CLASSES = [
    'pricing-card', 'pricing-grid', 'billing-toggle', 'billing-toggle-wrapper',
    'comparison-table', 'comparison-table-wrapper', 'popular', 'adv-card',
    'adv-grid', 'benefits-grid', 'features-grid', 'features-section',
    'download-btn', 'hero-badge', 'results-header', 'hasil-container',
    'filter-bar', 'page-header', 'stats-section', 'save-badge',
    'result-container', 'toggle-btn',
];

test('B42: 22 kelas warisan landing v1 tidak kembali', () => {
    for (const cls of DEAD_CLASSES) {
        const re = new RegExp('\\.' + cls.replace(/-/g, '\\-') + '(?![\\w-])');
        assert.doesNotMatch(DESKTOP, re, `.${cls} census 0 pemakaian (desktop)`);
        assert.doesNotMatch(MOBILE, re, `.${cls} census 0 pemakaian (mobile)`);
    }
});

test('B42: kelas hidup halaman pemuat tetap terdefinisi', () => {
    // landing (index) + auth — score-badge/student-name milik hasil.css
    // (file lain), jadi tidak termasuk kontrak dua file ini.
    for (const cls of ['hero', 'hero-title', 'btn-primary', 'version-badge',
                       'login-page', 'login-card', 'login-orb', 'flash-msg']) {
        const re = new RegExp('\\.' + cls.replace(/-/g, '\\-') + '(?![\\w-])');
        assert.ok(re.test(DESKTOP) || re.test(MOBILE),
            `.${cls} wajib ada di public-desktop.css atau public-mobile.css`);
    }
});

test('B42: ukuran kedua file pasca-saring', () => {
    const d = fs.statSync(path.join(WEBUI_ROOT, 'static', 'css', 'public-desktop.css')).size;
    const m = fs.statSync(path.join(WEBUI_ROOT, 'static', 'css', 'public-mobile.css')).size;
    assert.ok(d > 4 * 1024 && d < 6 * 1024, `public-desktop.css ${d}B di luar rentang 4-6KB`);
    assert.ok(m > 9 * 1024 && m < 12.5 * 1024, `public-mobile.css ${m}B di luar rentang 9-12.5KB`);
});
