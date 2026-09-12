/* Contract tests untuk Batch 27 — tombol identitas submissions lewat registry
 * Actions (review_ui_halaman_web_2026-09-12.md, M24).
 *
 *   M24 — tombol identitas (role="button" tabindex="0") tidak punya jalur
 *         keyboard: klik di-handle listener tabel, sedangkan filter keyboard
 *         global hanya memicu [data-action]. Tanpa data-action, Enter/Space
 *         pada tombol = interaksi ghost.
 *   Regression guard — sisa body render LAMA di dalam listener tabel
 *         dereference btn yang kini selalu null → setiap klik biasa di tabel
 *         melempar TypeError dan logic tutup-di-luar tidak pernah jalan.
 *   Regression guard — guard tutup-di-luar wajib membiarkan klik pada tombol/
 *         popup (kalau tidak, popup langsung tertutup lagi setelah dibuka).
 *
 * RED pertama dibuktikan via grep (identity-open tidak terdaftar di mana pun)
 * sebelum implementasi; file ini memantapkan kontraknya secara permanen.
 *
 * Run with: node --test static/js/uiux-batch27-identity-action.test.mjs (from webui/)
 */

import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const HERE = path.dirname(fileURLToPath(import.meta.url));
const WEBUI_ROOT = path.join(HERE, '..', '..');
const read = (...p) => fs.readFileSync(path.join(...p), 'utf8');

const ADMIN_JS = read(WEBUI_ROOT, 'static', 'js', 'admin.js');
const SUBMISSIONS_HTML = read(WEBUI_ROOT, 'templates', 'admin', 'submissions.html');

// ---------------------------------------------------------------------------
// M24 — tombol identitas memakai data-action + registry
// ---------------------------------------------------------------------------

test('B27-1 (M24): tombol identitas di submissions.html membawa data-action identity-open', () => {
    const btn = SUBMISSIONS_HTML.match(/<strong class="submission-identity-btn"[^>]*>/);
    assert.ok(btn, 'tombol identitas harus ada (prasyarat kontrak)');
    assert.match(btn[0], /data-action="identity-open"/,
        'wajib data-action agar delegasi global DAN filter keyboard (Enter/Space) berlaku');
});

test('B27-2 (M24): action identity-open terdaftar di registry Actions', () => {
    assert.match(ADMIN_JS, /Actions\.register\('identity-open'/,
        'popup identitas dibuka via registry agar jalur klik & keyboard sama-sama berlaku');
});

// ---------------------------------------------------------------------------
// Regression guard — listener tabel tidak boleh membawa body render lama
// ---------------------------------------------------------------------------

test('B27-3: listener tabel submissions tidak lagi berisi body render popup lama', () => {
    // Isolasi IIFE listener tabel (dari anchor table submissionsTable sampai
    // penutup IIFE-nya). Di dalamnya btn HANYA dipakai untuk early-return;
    // dereference apa pun (getAttribute/getBoundingClientRect/textContent)
    // berarti body render lama masih tertinggal dan akan melempar TypeError
    // karena btn selalu null di path tersebut.
    const start = ADMIN_JS.indexOf("var table = document.getElementById('submissionsTable')");
    assert.notEqual(start, -1, 'listener tabel submissions harus ada (prasyarat kontrak)');
    const end = ADMIN_JS.indexOf('\n})();', start);
    assert.notEqual(end, -1, 'penutup IIFE listener tabel harus ada');
    const tableListener = ADMIN_JS.slice(start, end);
    assert.doesNotMatch(tableListener, /btn\.(getAttribute|getBoundingClientRect|textContent)/,
        'body render lama masih tertinggal di listener tabel: btn selalu null di sana ' +
        'setelah early-return → setiap klik biasa di tabel melempar TypeError; ' +
        'render hanya boleh lewat handler registry');
});

// ---------------------------------------------------------------------------
// Regression guard — guard tutup-di-luar tetap menghormati tombol & popup
// ---------------------------------------------------------------------------

test('B27-4: klik di luar popup tetap menutup, tapi tombol & popup dikecualikan', () => {
    assert.match(ADMIN_JS,
        /closest\('\.identity-popup'\) && !e\.target\.closest\('\.submission-identity-btn'\)/,
        'guard wajib mengecualikan popup DAN tombol identitas, jika tidak popup ' +
        'langsung tertutup kembali saat diklik/dibuka');
});
