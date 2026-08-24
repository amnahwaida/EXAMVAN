/* Contract + behavior tests untuk Batch 13 — sisi settings/vouchers.
 * Referensi: review_uiux_webui.md bagian 5.10 RE-REVIEW RONDE 7 —
 * T23, S70, S72, S73 (parsial), R77, S71, R82 (sisi settings).
 *
 * Run with:  node --test static/js/uiux-batch13-settings-guard.test.mjs   (from webui/)
 *
 * Latar belakang & dampak bisnis:
 *   - T23: showConfirm core SELALU meng-escape argumen message
 *     (admin-core.js:512) sehingga pemanggil voucher yang mengirim markup
 *     <strong style=...> membuat dialog menampilkan tag HTML mentah ke user.
 *   - S70: label "Hapus" & badge "Nonaktif" #ef4444 di atas tint merah hanya
 *     3.71:1 (< ambang teks 4.5:1) — migrasi ke --color-danger-light.
 *   - S72: v.package & v.notes dirender tanpa escapeHtml — celah XSS render.
 *   - S73: timestamp voucher pakai jam perangkat + format ad-hoc — disatukan
 *     lewat formatDateTimeID core ("YYYY-MM-DD HH:MM"); perbandingan expired
 *     vs jam klien DITUNDA (butuh API waktu server).
 *   - R77: dead code ekor T22 (modal arwah) dihapus dari settings-vouchers.js.
 *   - S71: rebalance cap !important ke angka aktual + hapus baseline rgba
 *     settings.html yang terduplikasi antar-suite.
 *   - R82: gradien endpoint settings bermigrasi ke --color-primary-bright
 *     (kontrak: token didefinisikan agen lain di theme.css).
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const WEBUI_ROOT = path.join(__dirname, '..', '..');
const read = (rel) => fs.readFileSync(path.join(WEBUI_ROOT, rel), 'utf8');

const VOUCHERS = read('static/js/settings-vouchers.js');
const SETTINGS = read('templates/admin/settings.html');
const THEME = read('static/css/theme.css');
const CORE = read('static/js/admin-core.js');
const GUARD7 = read('static/js/uiux-batch7-tokens.test.mjs');

// ---------------------------------------------------------------------------
// T23 — pesan konfirmasi voucher PLAIN TEXT (escape ditangani showConfirm core)
// ---------------------------------------------------------------------------

test('T23 (statik): pemanggil showConfirm voucher tidak lagi mengirim markup <strong>', () => {
    for (const fn of ['toggleVoucher', 'deleteVoucher']) {
        const start = VOUCHERS.indexOf(`function ${fn}(`);
        assert.ok(start !== -1, `${fn} ada`);
        const body = VOUCHERS.slice(start, VOUCHERS.indexOf('\nfunction ', start + 1));
        assert.doesNotMatch(body, /<strong|<[^>]+style=/,
            `${fn} wajib mengirim plain text — tag HTML tampil mentah karena showConfirm core meng-escape pesan`);
    }
});

test('T23 (vm): dialog hasil showConfirm (meniru escapeHtml core) bebas "&lt;strong"', async () => {
    // escapeHtml ASLI dari admin-core.js — meniru perilaku showConfirm :512.
    const escMatch = CORE.match(/function escapeHtml\(str\) \{[\s\S]*?\n\}/);
    assert.ok(escMatch, 'escapeHtml ada di admin-core.js');

    const win = { __settingsReady: {} };
    win.window = win;
    const captured = [];
    const sandbox = {
        window: win,
        document: { getElementById: () => null },
        console,
        // Stub showConfirm yang MENIRU core: pesan selalu di-escape sebelum
        // dirender ke .confirm-dialog-msg.
        showConfirm(message) {
            captured.push(String(message));
            return Promise.resolve(false);
        },
        apiFetch: () => Promise.resolve({ json: () => Promise.resolve({ success: true }) }),
        showToast: () => {},
        get escapeHtml() {
            return vm.runInContext('(' + escMatch[0] + ')', vm.createContext({}));
        },
        isNaN, parseInt,
    };
    sandbox.globalThis = sandbox;
    vm.createContext(sandbox);
    vm.runInContext(VOUCHERS, sandbox, { filename: 'settings-vouchers.js' });

    const escFn = vm.runInContext('(' + escMatch[0] + ')', vm.createContext({}));
    for (const [fnName, code] of [['toggleVoucher', 'GRAD-2026'], ['deleteVoucher', 'HAPUS"1']]) {
        captured.length = 0;
        await sandbox[fnName](1, code, true);
        await new Promise((r) => setImmediate(r));
        assert.equal(captured.length, 1, `${fnName} memanggil showConfirm`);
        const msg = captured[0];
        assert.doesNotMatch(msg, /<[a-z/][^>]*>/i,
            `${fnName}: pesan mentah wajib plain text (tanpa tag apa pun)`);
        const rendered = escFn(msg); // persis seperti .confirm-dialog-msg di core
        assert.ok(!rendered.includes('&lt;strong'),
            `${fnName}: dialog tidak boleh menampilkan tag ter-escape (&lt;strong)`);
        assert.ok(rendered.includes(escFn(code)),
            `${fnName}: kode voucher tetap sampai ke dialog (ter-escape oleh core)`);
        assert.match(rendered, /Apakah Anda yakin ingin/,
            `${fnName}: copy konfirmasi Bahasa Indonesia dipertahankan`);
    }
});

// ---------------------------------------------------------------------------
// S70 — #ef4444 di atas tint merah 3.71:1 → var(--color-danger-light)
// ---------------------------------------------------------------------------

test('S70 (statik): settings-vouchers.js bebas color:#ef4444 (badge Nonaktif & tombol Hapus)', () => {
    const n = (VOUCHERS.match(/color:\s*#ef4444/gi) || []).length;
    assert.equal(n, 0,
        `${n} color:#ef4444 tersisa — kontras 3.71:1 di atas tint merah; pakai var(--color-danger-light) (7.37:1)`);
});

test('S70 (statik): var(--color-danger-light) benar-benar dipakai (badge + tombol Hapus)', () => {
    assert.ok((VOUCHERS.match(/var\(--color-danger-light\)/g) || []).length >= 2,
        'minimal dua pemakaian token: badge "Nonaktif" dan tombol "Hapus"');
});

// ---------------------------------------------------------------------------
// S72 — v.package & v.notes wajib escapeHtml (sweep render vouchers)
// ---------------------------------------------------------------------------

test('S72 (statik): render tabel meng-escape v.package dan v.notes', () => {
    assert.match(VOUCHERS, /\$\{escapeHtml\(v\.package\)\}/,
        'v.package harus lewat escapeHtml (input server tak terjamin)');
    assert.match(VOUCHERS, /\$\{escapeHtml\(v\.notes \|\| '—'\)\}/,
        'v.notes harus lewat escapeHtml');
});

test('S72 (vm): sweep render — package/notes ber-tag HTML dirender aman', async () => {
    let tableHtml = '';
    const tbodyMock = {
        setAttribute() {},
        set innerHTML(v) { tableHtml = v; },
        get innerHTML() { return tableHtml; },
    };
    const win = { __settingsReady: {} };
    win.window = win;
    const sandbox = {
        window: win,
        document: {
            getElementById: (id) => (id === 'vouchersTableBody' ? tbodyMock : null),
        },
        console,
        showConfirm: () => Promise.resolve(false),
        apiFetch: () => Promise.resolve({ json: () => Promise.resolve({ success: true }) }),
        showToast: () => {},
        isNaN, parseInt,
    };
    sandbox.globalThis = sandbox;
    vm.createContext(sandbox);
    // escapeHtml & formatDateTimeID ASLI dari admin-core.js (kontrak core
    // yang dipakai renderVouchersTable) dieksekusi di sandbox yang sama.
    const escFn2 = vm.runInContext('(' + CORE.match(/function escapeHtml\(str\) \{[\s\S]*?\n\}/)[0] + ')', sandbox);
    const fmtFn = vm.runInContext('(' + CORE.match(/function formatDateTimeID\(dateStr\) \{[\s\S]*?\n\}/)[0] + ')', sandbox);
    sandbox.escapeHtml = escFn2;
    sandbox.formatDateTimeID = fmtFn;
    win.escapeHtml = escFn2;
    win.formatDateTimeID = fmtFn;
    vm.runInContext(VOUCHERS, sandbox, { filename: 'settings-vouchers.js' });

    sandbox.renderVouchersTable([{
        code: 'OK-1',
        package: '<b>Paket Ilahi</b>',
        notes: '<img src=x onerror="alert(1)"> catatan',
        duration_type: 'bulanan',
        used_count: 0,
        max_usage: 5,
        is_active: true,
        expires_at: '',
        id: 9,
    }]);
    assert.ok(!tableHtml.includes('<b>Paket Ilahi</b>') && !tableHtml.includes('<img'),
        'tag pada v.package/v.notes tidak boleh masuk markup mentah');
    assert.ok(tableHtml.includes('&lt;b&gt;Paket Ilahi&lt;/b&gt;'),
        'package tampil sebagai teks ter-escape');
    assert.ok(tableHtml.includes('&lt;img'),
        'notes tampil sebagai teks ter-escape');
});

// ---------------------------------------------------------------------------
// S73 (parsial) — tampilan timestamp satu-pintu formatDateTimeID core
// ---------------------------------------------------------------------------

test('S73 (statik): settings-vouchers.js bebas format tanggal ad-hoc (toLocale*)', () => {
    const n = (VOUCHERS.match(/toLocale(Date|Time)?String/g) || []).length;
    assert.equal(n, 0,
        `${n} pemakaian toLocale* tersisa — pakai formatDateTimeID core (kanonik "YYYY-MM-DD HH:MM")`);
});

test('S73 (statik): kedua titik timestamp memakai formatDateTimeID satu-pintu', () => {
    assert.match(VOUCHERS, /formatDateTimeID\(v\.expires_at\)/,
        'kadaluarsa daftar voucher via formatDateTimeID');
    assert.match(VOUCHERS, /formatDateTimeID\(r\.redeemed_at\)/,
        'waktu redeem modal pengguna via formatDateTimeID');
});

test('S73 (statik): perbandingan expired vs jam klien ditandai DITUNDA + penunjuk API waktu server', () => {
    // Baris isExpired memakai new Date() (jam klien) — kontrak: jangan "benarkan"
    // diam-diam; tunggu API waktu server (pola WIB satu-pintu R57/S69).
    const line = VOUCHERS.split('\n').find((l) => l.includes('new Date(v.expires_at) < new Date()'));
    assert.ok(line, 'baris perbandingan expired masih ada (perbaikan ditunda)');
    const ctx = VOUCHERS.slice(Math.max(0, VOUCHERS.indexOf(line) - 400), VOUCHERS.indexOf(line));
    assert.match(ctx, /DITUNDA[\s\S]*API waktu server|API waktu server[\s\S]*DITUNDA/i,
        'komentar penunjuk eksplisit wajib menyebut DITUNDA + API waktu server');
});

// ---------------------------------------------------------------------------
// R77 — dead code ekor T22 habis dari settings-vouchers.js
// ---------------------------------------------------------------------------

test('R77 (statik): settings-vouchers.js bebas identifier modal arwah lama', () => {
    const n = (VOUCHERS.match(/confirmActionModal|pendingConfirmCallback|btnConfirmActionSubmit/g) || []).length;
    assert.equal(n, 0,
        `${n} identifier dead-code ekor T22 tersisa — blok listener btnConfirmActionSubmit dihapus total`);
});

// ---------------------------------------------------------------------------
// S71 — deduplikasi baseline: entri rgba settings.html hanya di guard Batch 11
// ---------------------------------------------------------------------------

test('S71 (statik): batch7-tokens tidak lagi memegang baseline rgba settings.html (duplikat)', () => {
    assert.doesNotMatch(GUARD7, /'admin\/settings\.html'\s*:/,
        'entri \'admin/settings.html\' dihapus dari batch7-tokens — plafon rgba settings.html ≤28 dijaga uiux-batch11-settings-guard');
    assert.match(GUARD7, /batch11-settings-guard/,
        'komentar penunjuk ke guard batch11-settings-guard harus tertinggal di batch7-tokens');
});

test('S71 (guard): cap !important CSS inti terkunci di angka aktual terukur', () => {
    const AKTUAL = {
        'static/css/admin-base.css': 47,
        'static/css/hasil.css': 65,
        'static/css/public-desktop.css': 21,
        'static/css/public-mobile.css': 48,
    };
    // Verifikasi independen — hitungan BARIS ber-!important (pola audit review).
    for (const [file, aktual] of Object.entries(AKTUAL)) {
        const n = read(file).split('\n').filter((l) => l.includes('!important')).length;
        assert.equal(n, aktual, `!important ${file} = ${n} baris, aktual ${aktual} — guard batch11-settings-guard dikunci ke angka ini`);
    }
});

// ---------------------------------------------------------------------------
// R82 sisi settings — gradien endpoint → var(--color-primary-bright)
// ---------------------------------------------------------------------------

test('R82 (statik): token --color-primary-bright terdefinisi di theme.css (kontrak lintas-agen)', () => {
    assert.match(THEME, /--color-primary-bright:\s*#818cf8\s*;/);
});

test('R82 (statik): gradien endpoint settings.html memakai token, bukan #818cf8 literal', () => {
    // S81 (ronde 8): asersi lama FIRST-MATCH-ONLY (hanya gradien pertama)
    // memberi rasa aman palsu — sisa #818cf8 di lokasi lain lolos tanpa alarm.
    // Diganti hitungan GLOBAL; penegakan folder-wide kini di uiux-batch14-tokens-guard.
    const n = (SETTINGS.match(/#818cf8/gi) || []).length;
    assert.equal(n, 0,
        `${n} literal #818cf8 tersisa di settings.html — wajib var(--color-primary-bright)`);
    const grad = SETTINGS.match(/linear-gradient\(135deg,[^;]+\);/);
    assert.ok(grad, 'gradien endpoint ada');
    assert.match(grad[0], /var\(--color-primary-bright\)/);
});
