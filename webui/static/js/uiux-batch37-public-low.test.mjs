/* Contract tests untuk Batch 37 — behavioral LOW halaman publik
 * (review_ui_halaman_web_2026-09-12.md — L60, L63, L65, L66).
 *
 *   L60 — renderStats (hasil.html) memakai kelas semantik .stat-success dst.
 *         di hasil.css, bukan style inline per-statistik dari JS.
 *   L63 — tiga empty-state download.html memakai kelas .empty-state, bukan
 *         3×~90 karakter styling inline duplikatif.
 *   L65 — blok CSS OTP 6-kotak satu-sumber di public-auth.css
 *         (register_confirm + reset_password tidak lagi menyimpan salinan).
 *   L66 — blok pw-strength satu-sumber di public-auth.css
 *         (register + reset_password tidak lagi menyimpan salinan).
 *
 * Run with: node --test static/js/uiux-batch37-public-low.test.mjs (from webui/)
 */

import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const HERE = path.dirname(fileURLToPath(import.meta.url));
const WEBUI_ROOT = path.join(HERE, '..', '..');
const read = (...p) => fs.readFileSync(path.join(...p), 'utf8');

const HASIL = read(WEBUI_ROOT, 'templates', 'public', 'hasil.html');
const HASIL_CSS = read(WEBUI_ROOT, 'static', 'css', 'hasil.css');
const DOWNLOAD = read(WEBUI_ROOT, 'templates', 'public', 'download.html');
const AUTH_CSS = read(WEBUI_ROOT, 'static', 'css', 'public-auth.css');
const REGISTER = read(WEBUI_ROOT, 'templates', 'public', 'register.html');
const REGISTER_CONFIRM = read(WEBUI_ROOT, 'templates', 'public', 'register_confirm.html');
const RESET_PASSWORD = read(WEBUI_ROOT, 'templates', 'public', 'reset_password.html');

// ---------------------------------------------------------------------------
// L60 — kelas semantik statistik
// ---------------------------------------------------------------------------

test('L60a: hasil.css mendefinisikan kelas semantik .stat-* yang memetakan token', () => {
    for (const [cls, token] of [
        ['stat-success', 'var(--color-success)'],
        ['stat-warning', 'var(--color-warning)'],
        ['stat-danger', 'var(--color-danger)'],
        ['stat-accent', 'var(--color-accent-light)'],
    ]) {
        const rule = HASIL_CSS.match(new RegExp(`\\.${cls}\\s*\\{([^}]*)\\}`));
        assert.ok(rule, `.${cls} terdefinisi di hasil.css`);
        assert.match(rule[1], new RegExp(`color:\\s*${token.replace(/[()*]/g, '\\$&')}`),
            `.${cls} memetakan ${token}`);
    }
});

test('L60b: renderStats memakai kelas semantik, tanpa style inline warna', () => {
    const fn = HASIL.slice(HASIL.indexOf('function renderStats'),
        HASIL.indexOf('function getDurationString'));
    assert.match(fn, /stat-mini-value stat-success/, 'Rata-rata = stat-success');
    assert.match(fn, /stat-mini-value stat-warning/, 'Tertinggi = stat-warning');
    assert.match(fn, /stat-mini-value stat-danger/, 'Terendah = stat-danger');
    assert.match(fn, /stat-mini-value stat-accent/, 'Total Peserta = stat-accent');
    assert.doesNotMatch(fn, /style=\\"?color: var\(--color-/,
        'style inline warna dihapus dari markup renderStats');
});

// ---------------------------------------------------------------------------
// L63 — kelas .empty-state di download.html
// ---------------------------------------------------------------------------

test('L63: tiga empty-state download.html memakai kelas, styling terpusat', () => {
    assert.equal((DOWNLOAD.match(/class="empty-state"/g) || []).length, 3,
        '3 blok empty-state (Android/Windows/Linux) memakai kelas');
    assert.doesNotMatch(DOWNLOAD, /padding: 60px 20px; color: var\(--color-text-muted\)/,
        'styling inline panjang duplikatif dihapus dari markup');
    // Rule kelas terpusat di <style> lokal download.html:
    const rule = DOWNLOAD.match(/\.empty-state\s*\{[^}]*\}/);
    assert.ok(rule, '.empty-state terdefinisi di <style> download.html');
    assert.match(rule[0], /border: 1px dashed rgba\(var\(--rgb-white\),0\.05\)/,
        'nilai gaya identik dengan inline semula');
});

// ---------------------------------------------------------------------------
// L65 — OTP satu-sumber
// ---------------------------------------------------------------------------

test('L65: blok CSS OTP satu-sumber di public-auth.css; salinan lokal dihapus', () => {
    assert.match(AUTH_CSS, /\.otp-input-wrapper\s*\{/);
    assert.match(AUTH_CSS, /\.otp-input-wrapper \.otp-digit\s*\{/);
    assert.match(AUTH_CSS, /\.otp-digit:focus\s*\{/);
    assert.match(AUTH_CSS, /\.otp-input-wrapper \.otp-digit\.filled\s*\{/);
    assert.match(AUTH_CSS, /@media \(max-width: 480px\)[\s\S]*\.otp-input-wrapper \.otp-digit \{ width: 40px;/,
        'rule mobile OTP ikut terpusat');
    // Konsumen me-link, bukan menyalin:
    for (const [label, html] of [['register_confirm', REGISTER_CONFIRM], ['reset_password', RESET_PASSWORD]]) {
        assert.match(html, /public-auth\.css/, `${label} me-link public-auth.css`);
        assert.doesNotMatch(html, /\.otp-digit:focus\s*\{/,
            `${label}: salinan rule OTP lokal dihapus`);
    }
});

// ---------------------------------------------------------------------------
// L66 — pw-strength satu-sumber
// ---------------------------------------------------------------------------

test('L66: blok pw-strength satu-sumber di public-auth.css; salinan lokal dihapus', () => {
    for (const rule of [/\.pw-strength-bar\s*\{/, /\.pw-bar-fill\.weak\s*\{/,
                        /\.pw-bar-fill\.strong\s*\{/, /\.pw-strength-text\s*\{/]) {
        assert.match(AUTH_CSS, rule, `rule ${rule} ada di public-auth.css`);
    }
    for (const [label, html] of [['register', REGISTER], ['reset_password', RESET_PASSWORD]]) {
        assert.match(html, /public-auth\.css/, `${label} me-link public-auth.css`);
        assert.doesNotMatch(html, /\.pw-strength-bar\s*\{/,
            `${label}: salinan pw-strength lokal dihapus`);
    }
});
