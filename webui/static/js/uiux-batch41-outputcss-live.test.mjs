/**
 * ══════════════════════════════════════════════════════════════════════════
 * Batch 41 — output.css: hasil PENYARINGAN rule mati (review 14 Nov 2026)
 * ══════════════════════════════════════════════════════════════════════════
 *
 * output.css (artefak build tailwind v4.3.1, dimuat admin head.html + login +
 * hasil + reset_password + 4 halaman auth) di-saring rule-level: hanya rule
 * yang SELURUH komponen kelasnya mati di census penuh (templates + JS,
 * word-match + atom compound + famili konkat toast-…/flash-…) yang dibuang —
 * 43 rules, 102KB → 87KB. Rule campuran (≥1 komponen hidup) dipertahankan
 * utuh agar cascade identik; verifikasi computed-style dif semua elemen
 * dilakukan saat penyaringan (4 halaman auth: identik).
 *
 * Run with: node --test static/js/uiux-batch41-outputcss-live.test.mjs
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const HERE = path.dirname(fileURLToPath(import.meta.url));
const WEBUI_ROOT = path.join(HERE, '..', '..');
const OUT = fs.readFileSync(path.join(WEBUI_ROOT, 'static', 'css', 'tailwind', 'output.css'), 'utf8');

// Kelas kunci yang dikonsumsi halaman-halaman yang memuat output.css — wajib
// tetap terdefinisi setelah penyaringan.
const MUST_EXIST = [
    // auth publik
    'btn-login', 'login-card', 'login-container', 'login-orb', 'login-orb-1',
    'login-orb-2', 'login-form', 'login-footer', 'login-logo', 'login-page',
    'login-subtitle', 'password-wrapper', 'password-toggle-btn', 'pw-icon',
    'required', 'divider-subtle', 'flash-messages', 'flash-msg',
    'form-group', 'form-hint-subtle',
    // admin (output.css dimuat admin/partials/head.html)
    'btn-sm', 'sr-only', 'modal-close', 'toast-container', 'toast-msg',
    'toast-icon', 'toast-body', 'toast-close', 'toast-exit', 'toast-success',
    'toast-error', 'loading', 'pagination-page-num', 'page-current',
    'search-clear-btn', 'badge-https',
];

test('B41: kelas kunci admin & auth tetap terdefinisi', () => {
    for (const cls of MUST_EXIST) {
        assert.match(OUT, new RegExp('\\.' + cls.replace(/-/g, '\\-') + '(?![\\w-])'),
            `.${cls} wajib terdefinisi (dikonsumsi halaman yang memuat output.css)`);
    }
});

test('B41: 43 rule mati murni tidak kembali (census penuh templates+JS)', () => {
    // Semua kelas berikut census word-match + atom = 0 pemakaian di seluruh
    // templates/JS. Selector campuran yang mengandung komponen hidup
    // (.method-selector .btn-sm dsb.) sengaja dipertahankan — konservatif.
    const deadSelectors = [
        'absolute', 'fixed', 'sticky', 'gap-2', 'rounded-2xl',
        'border-slate-800', 'bg-slate-900', 'p-6', 'text-indigo-300',
        'text-white', 'shadow-2xl', 'backdrop-blur-2xl', 'btn-weight',
        'btn-regen', 'token-group', 'badge-info', 'badge-http', 'icon-svg-lg',
        'file-icon', 'dropdown-brand-subtitle', 'detail-answers-grid',
        'select-filter', 'desktop-only-item', 'toggle-user-name',
        'topbar-badge', 'topbar-user',
    ];
    for (const dead of deadSelectors) {
        assert.doesNotMatch(OUT, new RegExp('\\.' + dead.replace(/-/g, '\\-') + '(?![\\w-])'),
            `.${dead} census 0 pemakaian — tidak boleh kembali (hidupkan dari git bila dipakai)`);
    }
});

test('B41: rule global elemen wajib dipertahankan (body, option, disabled, focus-visible)', () => {
    assert.match(OUT, /^body\s*\{/m, 'body reset');
    assert.match(OUT, /option:hover|option:focus/, 'option styling');
    assert.match(OUT, /input:disabled|select:disabled|textarea:disabled/, 'disabled form controls');
    assert.match(OUT, /\*:focus-visible/, 'focus ring global');
});

test('B41: kelima @keyframes wajib ada (dirujuk rule hidup / JS)', () => {
    for (const kf of ['toastOut', 'loginFadeIn', 'loginSpinner', 'toastIn', 'orbFloat']) {
        assert.match(OUT, new RegExp('@keyframes ' + kf + '\\b'), '@keyframes ' + kf);
    }
});

test('B41: @layer properties + tailwind-theme wajib ada (custom properties konsumen kelas hidup)', () => {
    assert.match(OUT, /@layer properties/, 'layer properties (registrasi @property)');
    assert.match(OUT, /@layer tailwind-theme/, 'layer theme (custom properties)');
    assert.match(OUT, /--spacing\s*:/, 'var(--spacing) yang dikonsumsi kelas hidup');
});

test('B41: ukuran file pasca-saring ~87KB (build utuh 102KB tidak boleh kembali)', () => {
    const size = fs.statSync(path.join(WEBUI_ROOT, 'static', 'css', 'tailwind', 'output.css')).size;
    assert.ok(size > 80 * 1024 && size < 95 * 1024,
        `output.css ${size} bytes — di luar rentang subset yang dikunci (80-95KB)`);
});
