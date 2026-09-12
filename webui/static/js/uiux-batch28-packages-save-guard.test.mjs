/* Contract tests untuk Batch 28 — state error Packages (review_ui_halaman_web_2026-09-12.md, M16).
 *
 *   M16 — catch kegagalan loadPackages hanya menulis pesan error ke tabel;
 *         tombol Simpan tetap aktif → savePackages membaca state internal
 *         kosong dan mengirim POST {"packages":[]} → server menolak 400
 *         ("Tidak ada paket yang dikirim", packages.go:79-82) tanpa operator
 *         sadar payload-nya array kosong.
 *
 *   Kontrak:
 *     B28-1 (statik) catch loadPackages men-disable btnSavePackages;
 *                    load sukses men-enable kembali (recovery path).
 *     B28-2 (statik) savePackages menolak state kosong SEBELUM tombol di-disable
 *                    / apiFetch dipanggil (guard di atas fetch).
 *     B28-3 (vm)     load gagal → tombol disabled; klik save (bila apapun
 *                    lolos) TIDAK memanggil apiFetch; toast error memandu.
 *     B28-4 (vm)     save saat PACKAGES kosong → TANPA POST, TANPA disable
 *                    tombol, toast error "belum termuat".
 *
 * Run with: node --test static/js/uiux-batch28-packages-save-guard.test.mjs (from webui/)
 */

import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';

const HERE = path.dirname(fileURLToPath(import.meta.url));
const WEBUI_ROOT = path.join(HERE, '..', '..');
const read = (...p) => fs.readFileSync(path.join(...p), 'utf8');

const PACKAGES_JS = read(WEBUI_ROOT, 'static', 'js', 'settings-packages.js');

/** Badan fungsi top-level `function NAME(` di file ber-indent 1 spasi. */
function functionBody(src, name) {
    const start = src.indexOf(`function ${name}(`);
    assert.ok(start !== -1, `function ${name} ditemukan`);
    const markers = ['\n\n function', '\n\n window.', '\n\n //', '\n\n if (']
        .map((m) => src.indexOf(m, start + 1))
        .filter((i) => i !== -1);
    const end = markers.length ? Math.min(...markers) : src.length;
    return src.slice(start, end);
}

// ---------------------------------------------------------------------------
// Statik — guard ada di sumber
// ---------------------------------------------------------------------------

test('B28-1 (M16, statik): catch loadPackages men-disable Simpan; load sukses men-enable kembali', () => {
    const body = functionBody(PACKAGES_JS, 'loadPackages');
    const catchIdx = body.lastIndexOf('.catch(');
    assert.ok(catchIdx !== -1, 'blok .catch loadPackages ditemukan');
    const catchBody = body.slice(catchIdx);
    assert.match(catchBody, /btnSavePackages/,
        'catch gagal-muat tidak menyentuh btnSavePackages — Simpan tetap aktif di atas state kosong (M16)');
    assert.match(catchBody, /\.disabled\s*=\s*true/,
        'catch wajib men-disable tombol Simpan');
    // Recovery: sukses (then) men-enable kembali agar retry setelah gagal bisa menyimpan.
    const thenPart = body.slice(0, catchIdx);
    assert.match(thenPart, /\.disabled\s*=\s*false/,
        'load sukses wajib men-enable kembali tombol Simpan (recovery setelah gagal-muat)');
});

test('B28-2 (M16, statik): savePackages menolak state kosong sebelum menyentuh apiFetch', () => {
    const body = functionBody(PACKAGES_JS, 'savePackages');
    const guardIdx = body.search(/PACKAGES\.length/);
    const fetchIdx = body.indexOf('apiFetch(');
    assert.ok(guardIdx !== -1, 'savePackages wajib punya guard PACKAGES.length');
    assert.ok(fetchIdx !== -1, 'savePackages memanggil apiFetch (prasyarat)');
    assert.ok(guardIdx < fetchIdx,
        'guard state-kosong harus dievaluasi SEBELUM apiFetch — bukan sesudah payload dibangun');
    const guardBlock = body.slice(guardIdx, guardIdx + 220);
    assert.match(guardBlock, /return/,
        'guard wajib return (menghentikan save), bukan hanya toast');
    assert.match(guardBlock, /showToast/,
        'guard wajib memandu operator lewat toast, bukan 400 dari server');
});

// ---------------------------------------------------------------------------
// Perilaku (vm) — seluruh IIFE dijalankan di sandbox
// ---------------------------------------------------------------------------

function makeSandbox() {
    const calls = { fetch: 0, toasts: [] };
    const btn = { disabled: false, textContent: 'Simpan Perubahan' };
    const tbody = { innerHTML: '' };
    const doc = {
        getElementById(id) {
            if (id === 'btnSavePackages') return btn;
            if (id === 'packagesTableBody') return tbody;
            return null;
        },
        querySelectorAll() { return []; },
    };
    const sandbox = {
        document: doc,
        window: {},
        console,
        showToast(msg, type) { calls.toasts.push(`${msg}|${type}`); },
        apiFetch() {
            calls.fetch += 1;
            return Promise.reject(new Error('boom')); // default: server gagal
        },
        escapeHtml: (s) => String(s),
    };
    sandbox.globalThis = sandbox;
    vm.createContext(sandbox);
    return { sandbox, calls, btn, tbody };
}

test('B28-3 (M16, vm): load gagal → Simpan ter-disable; save terhalang TANPA POST', async () => {
    const { sandbox, calls, btn } = makeSandbox();
    vm.runInContext(PACKAGES_JS, sandbox, { filename: 'settings-packages.js' });

    assert.equal(btn.disabled, false, 'prasyarat: tombol aktif sebelum load');
    sandbox.window.initPackages();
    await new Promise((r) => setTimeout(r, 5));
    assert.equal(btn.disabled, true, 'M16: gagal muat harus men-disable tombol Simpan');
    assert.match(calls.toasts.join('\n'), /Gagal memuat|gagal/i,
        'pesan gagal-muat tetap ditampilkan ke operator');

    // Jalur apapun yang memanggil savePackages di state gagal TIDAK boleh POST
    // (baseline = jumlah fetch setelah load, karena load sendiri memakai 1).
    const baselineFetch = calls.fetch;
    sandbox.window.savePackages();
    await new Promise((r) => setTimeout(r, 5));
    assert.equal(calls.fetch, baselineFetch, 'TIDAK boleh ada POST {"packages":[]} saat state kosong');
});

test('B28-4 (M16, vm): save saat state kosong → toast panduan, tanpa POST, tanpa disable', async () => {
    const { sandbox, calls, btn } = makeSandbox();
    vm.runInContext(PACKAGES_JS, sandbox, { filename: 'settings-packages.js' });

    // Load belum pernah sukses (PACKAGES kosong), tapi tombol belum ter-disable
    // (mis. urutan event tidak biasa) — save tetap wajib menolak state kosong.
    sandbox.window.savePackages();
    await new Promise((r) => setTimeout(r, 5));
    assert.equal(calls.fetch, 0, 'state kosong tidak boleh dikirim ke server');
    assert.equal(btn.disabled, false, 'guard state-kosong keluar SEBELUM tombol di-disable');
    assert.match(calls.toasts.join('\n'), /belum termuat|muat ulang/i,
        'toast wajib memandu operator memuat ulang, bukan error server 400');
});
