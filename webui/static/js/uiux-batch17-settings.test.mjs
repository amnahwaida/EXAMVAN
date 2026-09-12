/**
 * ══════════════════════════════════════════════════════════════════════════
 * Suite UI/UX BATCH 17 — SETTINGS (agen batch17-settings)
 * ══════════════════════════════════════════════════════════════════════════
 *
 * Referensi: review_uiux_webui.md bagian 5.14 RE-REVIEW RONDE 11 (basis
 * 80e95bb, pasca Batch 16). Cakupan temuan milik agen ini:
 *
 *   S114 — Fallback loader system-apps menandai __settingsLoaded sebelum
 *         script sukses dimuat dan tanpa s.onerror. Satu gagal muat (LAN
 *         flaky/deploy) → flag selamanya true → aktivasi tab berikutnya
 *         skip memuat → tab Aplikasi Sistem mati senyap sampai reload.
 *         Kontrak: (a) penandaan window.__settingsLoaded['system-apps']
 *             = true dipindah ke DALAM s.onload (hanya setelah sukses),
 *             lalu tryOpen() dipanggil;
 *         (b) ada s.onerror yang mengembalikan flag ke false DAN memunculkan
 *             toast 'Gagal memuat modul Pengaturan' bergaya 'error';
 *         (c) perilaku vm: onload sukses → flag true + modal terbuka;
 *             simulasi error → flag tetap/kembali false + toast muncul.
 *
 *   S115 — Badge salin kode voucher keyboard-dead: dirender sebagai
 *         <div class="voucher-code-badge" data-action="copy"> tanpa
 *         role/tabindex/keydown — satu-satunya cara menyalin kode voucher
 *         adalah mouse (WCAG 2.1.1).
 *         Kontrak: elemen adalah <button type="button"> ber-data-action
 *             copy sehingga Enter/Space native mengaktifkan klik; delegasi
 *             click wireVoucherRowActions tetap menangkapnya; penyesuaian
 *             CSS .voucher-code-badge di settings.html TANPA literal baru.
 *
 *   R140 — Registrasi yatim window.__settingsReady['packages'] =
 *         loadPackages (key section 'packages' tak pernah eksis pasca
 *         redesign 5-tab; risiko dobel-init bila kelak dihidupkan).
 *         Kontrak: string registrasi hilang dari settings-packages.js;
 *         jalur init hidup window.initPackages tetap ada.
 *
 *   R141 — activatePackage tanpa penahan klik-ganda: tombol tetap aktif
 *         selama fetch + jeda 1,2 dtk pra-reload → POST aktivasi bisa dobel.
 *         Kontrak: handler delegasi billing-package-activate men-disable
 *             tombol pemanggil di awal dan meng-guard klik lanjutan;
 *             cabang error/catch memulihkan disabled; jalur sukses biarkan
 *             disabled hingga reload.
 *
 *   R142 — Paritas label toolbar lipat + sisa campuran bahasa:
 *         (a) updateToggleAllLabel (general) mengikuti pola users —
 *             label.textContent murni 'Buka Semua'/'Lipat Semua', jumlah
 *             terlipat hanya di btn.title;
 *         (b) 'Buat Voucher Single' → 'Buat Voucher' (toolbar + judul modal);
 *         (c) label catatan kedua form seragam 'Catatan / Nama Campaign'
 *             ('Campaign Name' tidak boleh tersisa).
 *
 * Kepemilikan file agen ini: templates/admin/settings.html,
 *   static/js/settings-vouchers.js, static/js/settings-packages.js,
 *   static/js/settings-billing.js, static/js/settings-general.js,
 *   dan suite ini sendiri.
 *
 * Cara kalibrasi ulang bila test ini MEMERAH setelah edit sah:
 *   - S114: pesan toast ('Gagal memuat modul Pengaturan') boleh berubah
 *     asal onerror tetap me-reset flag + memanggil showToast gaya 'error';
 *     penandaan loaded wajib tetap DI DALAM onload.
 *   - S115: jika markup badge berubah lagi, jangan turunkan kembali ke
 *     elemen non-fokusable — pertahankan <button> atau paritas
 *     role/tabindex/keydown.
 *   - R141: nama aksi delegasi boleh berubah; prinsip disable-awal +
 *     pulih-di-error + guard dobel-klik wajib bertahan.
 *   - R142: konsolidasi istilah lain boleh ditambah, tapi 'Campaign Name'
 *     dan '(n/m terlipat)' di label.textContent tidak boleh hidup lagi.
 *
 * Run with:  node --test static/js/uiux-batch17-settings.test.mjs   (from webui/)
 */

import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';

const HERE = path.dirname(fileURLToPath(import.meta.url));
const WEBUI_ROOT = path.join(HERE, '..', '..');
const read = (...p) => fs.readFileSync(path.join(WEBUI_ROOT, ...p), 'utf8');

const SETTINGS_HTML = read('templates', 'admin', 'settings.html');
const VOUCHERS_JS = read('static', 'js', 'settings-vouchers.js');
const PACKAGES_JS = read('static', 'js', 'settings-packages.js');
const BILLING_JS = read('static', 'js', 'settings-billing.js');
const GENERAL_JS = read('static', 'js', 'settings-general.js');

/** Ambil badan fungsi top-level `function NAME(...)` sampai deklarasi top-level berikutnya. */
function functionBody(src, name) {
    const start = src.indexOf(`function ${name}(`);
    assert.ok(start !== -1, `function ${name} ditemukan`);
    const markers = ['\nfunction ', '\nwindow.']
        .map((m) => src.indexOf(m, start + 1))
        .filter((i) => i !== -1);
    const end = markers.length ? Math.min(...markers) : -1;
    return src.slice(start, end === -1 ? undefined : end);
}

/** Blok loader loadSectionScript di settings.html (pola batch16). */
function dispatcherBlock() {
    const anchor = SETTINGS_HTML.indexOf('function loadSectionScript');
    assert.ok(anchor !== -1, 'blok loadSectionScript ditemukan');
    let depth = 0;
    let i = SETTINGS_HTML.indexOf('{', anchor);
    for (; i < SETTINGS_HTML.length; i++) {
        if (SETTINGS_HTML[i] === '{') depth += 1;
        else if (SETTINGS_HTML[i] === '}') {
            depth -= 1;
            if (depth === 0) break;
        }
    }
    return SETTINGS_HTML.slice(anchor, i + 1);
}

// ════════════════════════════════════════════════════════════════════════
// S114 — fallback loader system-apps: flag loaded hanya SETELAH sukses
// ════════════════════════════════════════════════════════════════════════

test('S114 (statik): loadSectionScript punya s.onerror + flag di-revert + toast error', () => {
    const block = dispatcherBlock();
    assert.match(block, /\.onerror\s*=/,
        'loader tanpa s.onerror — satu gagal muat membuat tab mati senyap');
    assert.match(block, /window\.__settingsLoaded\[key\]\s*=\s*false/,
        'flag wajib di-revert ke false saat onerror agar retry dimungkinkan');
    assert.match(block, /showToast\(\s*['"]Gagal memuat modul Pengaturan['"]\s*,\s*['"]error['"]\s*\)/,
        's.onerror wajib menampilkan toast gagal-muat gaya error');
});

test('S114 (perilaku vm): onload sukses → flag true + cb jalan; onerror → flag false + toast error', () => {
    // ── Jalur sukses ──
    let successScript = null;
    let appended = 0;
    const okSandbox = {
        // Halaman menginisialisasi window.__settingsLoaded = {} sebelum
        // loadSectionScript dipakai — sandbox meniru itu.
        window: { __settingsLoaded: {} },
        document: {
            createElement() { return {}; },
            head: { appendChild(el) { successScript = el; appended += 1; } },
            addEventListener() {},
        },
    };
    vm.runInNewContext(dispatcherBlock(), okSandbox, { filename: 'settings-loader-ok.js' });
    let cbCalled = 0;
    okSandbox.loadSectionScript('system-apps', () => { cbCalled += 1; });
    assert.ok(successScript, 'jalur sukses: elemen script dibuat');
    assert.equal(typeof successScript.onload, 'function', 'onload adalah callback');
    // Dedupe: panggilan ulang saat flag masih true TIDAK membuat script kedua.
    okSandbox.loadSectionScript('system-apps', () => {});
    assert.equal(appended, 1, 'panggilan ulang sebelum sukses wajib dedupe (tanpa appendChild kedua)');
    // Modul "selesai dimuat": onload menyala → flag true + cb dieksekusi.
    successScript.onload();
    assert.equal(okSandbox.window.__settingsLoaded['system-apps'], true,
        'setelah onload sukses, flag system-apps wajib true');
    assert.equal(cbCalled, 1, 'cb wajib dipanggil setelah script sukses dimuat');

    // ── Jalur gagal muat ──
    let failedScript = null;
    const failSandbox = {
        window: { __settingsLoaded: {} },
        document: {
            createElement() { return {}; },
            head: { appendChild(el) { failedScript = el; } },
            addEventListener() {},
        },
    };
    vm.runInNewContext(dispatcherBlock(), failSandbox, { filename: 'settings-loader-fail.js' });
    const toasts = [];
    failSandbox.showToast = (msg, style) => toasts.push({ msg, style });
    let failCb = 0;
    failSandbox.loadSectionScript('system-apps', () => { failCb += 1; });
    assert.equal(typeof failedScript.onerror, 'function', 'onerror adalah callback');
    failedScript.onerror();
    assert.equal(failSandbox.window.__settingsLoaded['system-apps'], false,
        'setelah gagal muat, flag system-apps wajib false agar retry dimungkinkan');
    assert.ok(toasts.some((t) => t.msg === 'Gagal memuat modul Pengaturan' && t.style === 'error'),
        'gagal muat wajib memunculkan toast "Gagal memuat modul Pengaturan" gaya error');
    assert.equal(failCb, 1, 'cb tetap dipanggil pada gagal muat agar UI tidak menggantung');
});

// ════════════════════════════════════════════════════════════════════════
// S115 — badge salin kode voucher hidup untuk keyboard (<button native)
// ════════════════════════════════════════════════════════════════════════

test('S115 (statik): badge salin voucher dirender sebagai button type=button ber-data-action copy', () => {
    assert.doesNotMatch(VOUCHERS_JS, /<div[^>]*class="voucher-code-badge"/,
        'badge salin masih <div> klik-saja — mati untuk keyboard (WCAG 2.1.1)');
    assert.match(VOUCHERS_JS,
        /<button[^>]*type="button"[^>]*class="voucher-code-badge"[^>]*data-action="copy"/,
        'badge wajib <button type="button"> ber-data-action copy agar Enter/Space native aktif');
    // Penutup pembuka harus konsisten </button>, bukan </div> arwah.
    const openIdx = VOUCHERS_JS.search(/<button[^>]*class="voucher-code-badge"/);
    const closeIdx = VOUCHERS_JS.indexOf('</button>', openIdx);
    assert.ok(closeIdx !== -1, 'badge voucher-code-badge ditutup </button>');
});

test('S115 (statik): delegasi click voucher tetap menangkap data-action copy + CSS badge disetel ulang untuk tombol', () => {
    // L52 (review_ui_halaman_web_2026-09-12.md): delegasi manual tbody
    // diganti registry Actions — handler 'copy' terdaftar di modul ini dan
    // melayani klik via delegasi tunggal admin-core.js.
    const body = functionBody(VOUCHERS_JS, 'wireVoucherRowActions');
    assert.match(body, /wireRedemptionsRetry\(\)/,
        'wiring tersisa hanya retry redemptions (idempoten via wireOnce)');
    const regCopy = VOUCHERS_JS.match(/Actions\.register\(\s*'copy'[^;]*\);/);
    assert.ok(regCopy, 'handler registry copy terdaftar');
    assert.match(regCopy[0], /copyCode\(/, 'cabang salin kode wajib tetap ada');
    // Reset default <button> pada CSS badge — tanpa literal warna baru.
    const cssIdx = SETTINGS_HTML.indexOf('.voucher-code-badge {');
    assert.ok(cssIdx !== -1, 'rule .voucher-code-badge ada di settings.html');
    const ruleEnd = SETTINGS_HTML.indexOf('}', cssIdx);
    const rule = SETTINGS_HTML.slice(cssIdx, ruleEnd);
    assert.match(rule, /appearance:\s*none/,
        'tambahkan appearance:none pada .voucher-code-badge agar tampilan default browser tombol tak merusak badge');
});

// ════════════════════════════════════════════════════════════════════════
// R140 — registrasi yatim __settingsReady['packages']
// ════════════════════════════════════════════════════════════════════════

test('R140 (statik): registrasi yatim __settingsReady["packages"] dihapus, jalur initPackages hidup', () => {
    assert.doesNotMatch(PACKAGES_JS, /__settingsReady\[\s*['"]packages['"]\s*\]/,
        'registrasi key section "packages" yatim (tak pernah eksis pasca redesign 5-tab) — hapus');
    assert.match(PACKAGES_JS, /window\.initPackages\s*=\s*loadPackages/,
        'jalur init hidup window.initPackages (dipanggil settings-general.js) wajib dipertahankan');
});

// ════════════════════════════════════════════════════════════════════════
// R141 — activatePackage tanpa penahan klik-ganda
// ════════════════════════════════════════════════════════════════════════

test('R141 (statik): handler aktivasi pakai disable-awal, guard dobel-klik, pulih di error/catch', () => {
    const actBody = functionBody(BILLING_JS, 'activatePackage');
    assert.match(actBody, /\.disabled\s*=\s*false/,
        'cabang error activatePackage wajib memulihkan disabled tombol pemanggil');
    const regMatch = BILLING_JS.match(/Actions\.register\(\s*'billing-package-activate'[\s\S]*?\}\);/);
    assert.ok(regMatch, 'registrasi billing-package-activate ditemukan');
    assert.match(regMatch[0], /disabled/,
        'handler delegasi wajib men-disable/men-guard tombol pemanggil di awal (penahan klik-ganda)');
});

test('R141 (perilaku vm): klik pertama menjalankan POST, klik kedua ter-guard karena tombol disabled', async () => {
    const regMatch = BILLING_JS.match(/Actions\.register\(\s*'billing-package-activate'[\s\S]*?\}\);/);
    assert.ok(regMatch, 'registrasi billing-package-activate ditemukan');

    const posts = [];
    const apiFetch = (url, opts) => {
        posts.push({ url, method: opts && opts.method });
        return Promise.resolve({ json: () => Promise.resolve({ success: true, message: 'ok' }) });
    };
    const toasts = [];
    const registry = {};
    const sandbox = {
        window: {},
        Actions: { register(name, fn) { registry[name] = fn; } },
        apiFetch,
        FormData: class { append() {} },
        showToast: (msg, style) => toasts.push({ msg, style }),
        setTimeout: () => 0,
        console,
    };
    sandbox.globalThis = sandbox;
    vm.createContext(sandbox);
    vm.runInContext(functionBody(BILLING_JS, 'activatePackage'), sandbox, { filename: 'settings-billing.js#fn' });
    vm.runInContext(regMatch[0], sandbox, { filename: 'settings-billing.js#reg' });

    const btn = { disabled: false, getAttribute(k) { return k === 'data-redemption-id' ? '42' : null; } };
    await registry['billing-package-activate'](btn);
    assert.equal(posts.length, 1, 'klik pertama wajib menjalankan tepat satu POST');
    assert.equal(posts[0].method, 'POST');
    assert.equal(posts[0].url, '/admin/api/vouchers/activate');
    assert.equal(btn.disabled, true,
        'selama fetch + jeda pra-reload tombol wajib tetap disabled (biarkan sampai reload sukses)');

    await registry['billing-package-activate'](btn);
    assert.equal(posts.length, 1,
        'klik kedua saat tombol disabled wajib ter-guard — POST aktivasi tidak boleh dobel');
});

test('R141 (perilaku vm): kegagalan server/jaringan memulihkan disabled tombol', async () => {
    const regMatch = BILLING_JS.match(/Actions\.register\(\s*'billing-package-activate'[\s\S]*?\}\);/);

    const registry = {};
    const sandbox = {
        window: { __settingsReady: {} },
        Actions: { register(name, fn) { registry[name] = fn; } },
        apiFetch: () => Promise.reject(new Error('network down')),
        FormData: class { append() {} },
        showToast: () => {},
        setTimeout: () => 0,
        console: { error: () => {} },
    };
    sandbox.globalThis = sandbox;
    vm.createContext(sandbox);
    vm.runInContext(functionBody(BILLING_JS, 'activatePackage'), sandbox, { filename: 'settings-billing.js#fn' });
    vm.runInContext(regMatch[0], sandbox, { filename: 'settings-billing.js#reg' });

    const btn = { disabled: false, getAttribute(k) { return k === 'data-redemption-id' ? '7' : null; } };
    sandbox.loadMyPackages = () => {};
    await registry['billing-package-activate'](btn);
    assert.equal(btn.disabled, false,
        'cabang catch (jaringan gagal) wajib memulihkan tombol agar admin bisa mencoba lagi');
});

// ════════════════════════════════════════════════════════════════════════
// R142 — paritas label toolbar lipat + sisa bahasa
// ════════════════════════════════════════════════════════════════════════

test('R142a (statik): updateToggleAllLabel general murni Buka/Lipat Semua, jumlah hanya di title (pola users)', () => {
    const body = functionBody(GENERAL_JS, 'updateToggleAllLabel');
    assert.match(body, /label\.textContent\s*=\s*expand\s*\?\s*'Buka Semua'\s*:\s*'Lipat Semua';/,
        'label.textContent wajib murni "Buka Semua"/"Lipat Semua" (pola users) — jumlah pindah ke title');
    assert.doesNotMatch(body, /label\.textContent[^\n]*\+/,
        'label.textContent masih dirangkai jumlah "(n/m terlipat)" — teks tombol membingungkan saat 0 terlipat');
    assert.match(body, /btn\.title[^\n]*terlipat/,
        'jumlah terlipat wajib dipindah ke btn.title');
});

test('R142b (statik): istilah "Buat Voucher Single" dikonsolidasikan jadi "Buat Voucher"', () => {
    assert.equal((SETTINGS_HTML.match(/Buat Voucher Single/g) || []).length, 0,
        '"Buat Voucher Single" masih tersisa di settings.html (toolbar + judul modal)');
    assert.ok((SETTINGS_HTML.match(/Buat Voucher\b/g) || []).length >= 2,
        'dua titik (toolbar :1236 + judul modal :1350) wajib memakai "Buat Voucher"');
});

test('R142c (statik): label catatan kedua form voucher seragam "Catatan / Nama Campaign"', () => {
    assert.equal((SETTINGS_HTML.match(/Campaign Name/g) || []).length, 0,
        '"Campaign Name" masih tersisa — seragamkan ke "Nama Campaign"');
    assert.equal((SETTINGS_HTML.match(/Catatan \/ Nama Campaign/g) || []).length, 2,
        'kedua form (single :1442 + batch :1556) wajib berlabel "Catatan / Nama Campaign"');
});
