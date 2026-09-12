/* Contract tests untuk Batch 31 — satu implementasi toast (M21).
 *
 *   M21 — tiga implementasi toast hidup bersamaan:
 *     (a) showToast tersentral admin-core.js (dipakai admin + hasil + download),
 *     (b) toast ad-hoc register.html (:462-471) — DOM dibangun manual dengan
 *         cssText inline, tanpa de-dup, tanpa limit, tanpa aksesibilitas skin,
 *     (c) skin CSS lokal download.html (disengaja — halaman tidak memuat
 *         output.css; M21 menyentuh implementasi JS, bukan skin).
 *   Kontrak: auth pages menyediakan host #toastContainer via partial bersama
 *   + memuat admin-core.js; register.html mendelegasikan ke showToast.
 *
 * Run with: node --test static/js/uiux-batch31-toast-unify.test.mjs (from webui/)
 */

import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const HERE = path.dirname(fileURLToPath(import.meta.url));
const WEBUI_ROOT = path.join(HERE, '..', '..');
const read = (...p) => fs.readFileSync(path.join(...p), 'utf8');

const SHARED = read(WEBUI_ROOT, 'templates', 'public', 'shared.html');
const REGISTER = read(WEBUI_ROOT, 'templates', 'public', 'register.html');
const AUTH_PAGES = ['cek_hasil', 'forgot_password', 'register', 'register_confirm', 'reset_password']
    .map((n) => ({ name: n, src: read(WEBUI_ROOT, 'templates', 'public', `${n}.html`) }));

// M21 scope note: reset_password SENGAJA dikecualikan — kontrak S56/S105
// (batch10/batch15) melarang halaman anonim itu memuat admin-core.js, dan
// halaman tersebut tidak memunculkan toast sama sekali. Empat halaman lain
// yang memunculkan toast wajib memakai implementasi tersentral.
const TOAST_PAGES = ['cek_hasil', 'forgot_password', 'register', 'register_confirm'];
const RESET_PW = read(WEBUI_ROOT, 'templates', 'public', 'reset_password.html');

test('B31-1 (M21): shared.html menyediakan partial public_toast_host (container tunggal)', () => {
    const def = SHARED.match(/\{\{ define "public_toast_host" \}\}([\s\S]*?)\{\{ end \}\}/);
    assert.ok(def, 'define "public_toast_host" harus ada di shared.html');
    assert.match(def[1], /id="toastContainer"/, 'host harus berisi #toastContainer');
    assert.match(def[1], /class="toast-container"/, 'class skin output.css');
    assert.match(def[1], /aria-live="polite"/, 'region live untuk pembaca layar');
});

test('B31-2 (M21): halaman auth pemakai toast memuat host toast + admin-core.js (showToast tersentral)', () => {
    const pages = TOAST_PAGES.map((n) => ({ name: n, src: read(WEBUI_ROOT, 'templates', 'public', `${n}.html`) }));
    const offenders = pages.filter(({ src }) =>
        !src.includes('template "public_toast_host"') || !src.includes('admin-core.js'));
    assert.deepEqual(offenders.map((o) => o.name), [],
        'halaman auth tanpa host toast / admin-core: ' + offenders.map((o) => o.name).join(', '));
    // reset_password tetap bebas admin-core (kontrak S56/S105) — kecualian
    // eksplisit, bukan lupa.
    assert.doesNotMatch(RESET_PW, /admin-core\.js/,
        'reset_password tetap anonim (S56/S105) — jangan tambahkan admin-core kembali');
});

test('B31-3 (M21): register.html tanpa toast ad-hoc — delegasi ke showToast', () => {
    assert.match(REGISTER, /showToast\(\s*['"]Beberapa karakter tidak diizinkan/,
        'pesan charset wajib lewat showToast tersentral (de-dup, limit, skin, a11y)');
    // Blok showUsernameCharsetToast tidak lagi membangun DOM sendiri.
    const start = REGISTER.indexOf('function showUsernameCharsetToast');
    assert.notEqual(start, -1, 'wrapper showUsernameCharsetToast dipertahankan (titik panggil tidak berubah)');
    const body = REGISTER.slice(start, start + 400);
    assert.doesNotMatch(body, /document\.createElement/,
        'toast tidak lagi dibangun manual (cssText ad-hoc) — panggil showToast');
    assert.doesNotMatch(body, /cssText = 'position:fixed/,
        'cssText posisi-fixed manual adalah skin kedua — dihapus');
    assert.doesNotMatch(REGISTER, /toast\.style\.cssText = 'position:fixed/,
        'sisa implementasi ad-hoc masih ada');
});
