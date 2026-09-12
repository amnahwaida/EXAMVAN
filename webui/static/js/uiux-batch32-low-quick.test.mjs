/* Contract tests untuk Batch 32 — LOW cepat (L67/L68/L59/L56).
 *
 *   L67 — pola role="alert" inkonsisten: cek_hasil menaruhnya di WRAPPER
 *         (.flash-messages), mayoritas halaman lain menaruhnya di elemen
 *         pesan. Distandardkan: role di elemen pesan tunggal.
 *   L68 — checkMatch reset_password hanya memantau field konfirmasi: bila
 *         pengguna mengisi konfirmasi dulu lalu mengubah password utama,
 *         pesan "Password tidak cocok" tidak diperbarui (register/R22
 *         memantau KEDUA field).
 *   L59 — input pencarian siswa hasil.html type="text" — mobile keyboard
 *         tanpa tombol Search, tanpa clear native; wajib type="search".
 *   L56 — @keyframes spin terduplikasi di settings.html (blok asli :530 dan
 *         blok merged :534) — definisi kedua menimpa diam-diam; hapus blok
 *         asli, pertahankan yang bersekutu dengan konteks merged.
 *
 * Run with: node --test static/js/uiux-batch32-low-quick.test.mjs (from webui/)
 */

import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const HERE = path.dirname(fileURLToPath(import.meta.url));
const WEBUI_ROOT = path.join(HERE, '..', '..');
const read = (...p) => fs.readFileSync(path.join(...p), 'utf8');

const CEK_HASIL = read(WEBUI_ROOT, 'templates', 'public', 'cek_hasil.html');
const RESET_PW = read(WEBUI_ROOT, 'templates', 'public', 'reset_password.html');
const HASIL = read(WEBUI_ROOT, 'templates', 'public', 'hasil.html');
const SETTINGS = read(WEBUI_ROOT, 'templates', 'admin', 'settings.html');

test('B32-1 (L67): cek_hasil role="alert" di elemen pesan, bukan wrapper', () => {
    const block = CEK_HASIL.match(/<div class="flash-messages"[^>]*>\s*<div class="flash-msg[^>]*>/);
    assert.ok(block, 'blok flash error cek_hasil ditemukan');
    assert.doesNotMatch(block[0], /class="flash-messages"[^>]*role="alert"/,
        'role="alert" di wrapper — pindahkan ke elemen pesan (konsistensi mayoritas)');
    assert.match(block[0], /class="flash-msg flash-error"[^>]*role="alert"/,
        'elemen pesan wajib membawa role="alert"');
});

test('B32-2 (L68): checkMatch reset_password terpasang di KEDUA field password', () => {
    const body = RESET_PW.match(/function checkMatch\(\)[\s\S]{0,700}/);
    assert.ok(body, 'checkMatch ditemukan');
    // Listener confirm tetap ada…
    assert.match(body[0], /confirmField\.addEventListener\('input', checkMatch\)/,
        'listener field konfirmasi tetap ada');
    // …dan listener password utama kini juga terpasang.
    assert.match(body[0], /addEventListener\('input',\s*checkMatch\)/g,
        undefined);
    const listeners = [...body[0].matchAll(/addEventListener\('input',\s*checkMatch\)/g)].length;
    assert.ok(listeners >= 2,
        `checkMatch hanya dipasang di ${listeners} field — wajib 2 (password + confirm), ` +
        'kalau tidak perubahan password utama tidak memicu validasi ulang');
});

test('B32-3 (L59): pencarian siswa hasil.html memakai type="search"', () => {
    const input = HASIL.match(/<input[^>]*id="searchInput"[^>]*>/);
    assert.ok(input, 'input pencarian hasil ditemukan');
    assert.match(input[0], /type="search"/,
        'type="text" → mobile keyboard tanpa tombol Search & tanpa clear native');
});

test('B32-4 (L56): @keyframes spin hanya satu definisi di settings.html', () => {
    const defs = [...SETTINGS.matchAll(/@keyframes spin/g)];
    assert.equal(defs.length, 1,
        `@keyframes spin terdefinisi ${defs.length} kali — definisi kedua menimpa diam-diam ` +
        '(hapus blok asli, pertahankan yang bersekutu konteks merged)');
});
