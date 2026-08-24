/* Contract tests untuk Batch 12 — area settings/vouchers + guard (milik
 * koordinator; agen batch-12-settings-guard terputus, dikerjakan langsung).
 * Referensi: review_uiux_webui.md bagian 5.9 RE-REVIEW RONDE 6 —
 * T22, S67, R70, R72.
 *
 * Run with: node --test static/js/uiux-batch12-settings-guard.test.mjs (from webui/)
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const WEBUI_ROOT = path.join(__dirname, '..', '..');
const read = (rel) => fs.readFileSync(path.join(WEBUI_ROOT, rel), 'utf8');

const SETTINGS = read('templates/admin/settings.html');
const VOUCHERS = read('static/js/settings-vouchers.js');
const THEME = read('static/css/theme.css');
const GUARD7 = read('static/js/uiux-batch7-tokens.test.mjs');

// ===========================================================================
// T22 — sistem konfirmasi ketiga dihapus; showConfirm core satu-satunya
// ===========================================================================

test('T22 (statik): showConfirmModal & modal arwah confirmActionModal DIHAPUS total', () => {
    assert.doesNotMatch(VOUCHERS, /showConfirmModal/,
        'sistem konfirmasi ketiga dihapus — pakai showConfirm core (focus-trap G5)');
    assert.doesNotMatch(SETTINGS, /confirmActionModal/,
        'markup modal arwah confirmActionModal dihapus dari settings.html');
    // Batch 13 (R77): dead code ekor T22 habis JUGA dari settings-vouchers.js
    // (blok listener btnConfirmActionSubmit + pendingConfirmCallback yang
    // menunjuk elemen/markup yang sudah tidak ada).
    assert.doesNotMatch(VOUCHERS, /confirmActionModal|pendingConfirmCallback|btnConfirmActionSubmit/,
        'identifier modal arwah lama tidak boleh kembali ke settings-vouchers.js');
});

test('T22 (statik): kedua pemanggil bermigrasi ke showConfirm dengan label aksi eksplisit', () => {
    for (const fn of ['toggleVoucher', 'deleteVoucher']) {
        const start = VOUCHERS.indexOf(`function ${fn}(`);
        assert.ok(start !== -1, `${fn} ada`);
        const body = VOUCHERS.slice(start, VOUCHERS.indexOf('\nfunction ', start + 1));
        assert.match(body, /showConfirm\(/, `${fn} memakai showConfirm core`);
        assert.doesNotMatch(body, /apiFetch\//,
            'POST hanya setelah konfirmasi — apiFetch berada dalam callback .then(ok)');
    }
    // Label eksplisit (bukan default "Ya, Hapus" untuk toggle non-destruktif).
    assert.match(VOUCHERS, /'Ya, Aktifkan Voucher'|'Aktifkan Voucher'/, 'label toggle eksplisit');
    assert.match(VOUCHERS, /'Hapus Voucher'/, 'label hapus eksplisit');
});

test('T22 (statik): endpoint gradien terlarang nol di settings-vouchers.js', () => {
    assert.doesNotMatch(VOUCHERS, /#a855f7|#6366f1/,
        'whitelist larangan T18 kini mencakup JS render-path juga');
});

// ===========================================================================
// S67 — outline heading settings
// ===========================================================================

test('S67 (statik): settings punya tepat satu h1 sr-only di awal dan tak ada h1 tengah dokumen', () => {
    const h1s = [...SETTINGS.matchAll(/<h1[^>]*>[\s\S]*?<\/h1>/g)];
    assert.equal(h1s.length, 1, `tepat satu h1 (ditemukan ${h1s.length})`);
    assert.match(h1s[0][0], /sr-only/, 'h1 tunggal ber-class sr-only');
    assert.match(h1s[0][0], /Pengaturan/, 'judul halaman kanonik "Pengaturan"');
    // Posisi h1 sebelum section konten pertama (awal dokumen konten), bukan di tengah.
    const firstSection = SETTINGS.indexOf('<section class');
    assert.ok(h1s[0].index < firstSection, 'h1 berada sebelum seksi konten pertama');
    assert.doesNotMatch(SETTINGS, /<h1 class="premium-header-title">/,
        '"Aplikasi Sistem" turun ke h2');
});

// ===========================================================================
// R72 — asterisk required memakai token
// ===========================================================================

test('R72 (statik): #f43f5e diganti var(--color-danger-light)', () => {
    assert.doesNotMatch(SETTINGS, /#f43f5e/);
    assert.match(SETTINGS, /color:\s*var\(--color-danger-light\)[^<]*>\*<\/span>/);
});

// ===========================================================================
// S68-kontrak — token --color-danger-bright tersedia (didefinisikan agen publik)
// ===========================================================================

test('S68 (statik): token --color-danger-bright terdefinisi di theme.css', () => {
    assert.match(THEME, /--color-danger-bright:\s*#f87171\s*;/);
});

// ===========================================================================
// R70 — plafon folder-wide dikunci ke angka aktual (bukan longgar ratusan)
// ===========================================================================

test('R70 (statik): plafon folder-wide guard diperketat ke baseline aktual', () => {
    // Hitung ulang angka aktual untuk verifikasi independen.
    let hex = 0, rgba = 0;
    const dir = path.join(WEBUI_ROOT, 'templates');
    const walk = (d) => fs.readdirSync(d, { withFileTypes: true }).forEach((e) => {
        const p = path.join(d, e.name);
        if (e.isDirectory()) walk(p);
        else if (e.name.endsWith('.html')) {
            const src = fs.readFileSync(p, 'utf8');
            hex += (src.match(/#[0-9a-fA-F]{3,8}\b/g) || []).length;
            rgba += (src.match(/rgba\(\s*[0-9]/g) || []).length;
        }
    });
    walk(dir);

    const caps = GUARD7.match(/hex[^]*?<=\s*(\d+)[^]*?rgba[^]*?<=\s*(\d+)/) ||
                 GUARD7.match(/(\d+)[^]*?hex[\s\S]{0,200}?(\d+)[^]*?rgba/);
    // Guard wajib menyertakan angka plafon yang dekat aktual (toleransi ≤15%).
    assert.ok(caps, 'plafon folder-wide tertulis eksplisit di guard');
    const capHex = parseInt(caps[1], 10), capRgba = parseInt(caps[2], 10);
    assert.ok(capHex <= hex * 1.15, `cap hex (${capHex}) wajib ≤ ~aktual ${Math.ceil(hex * 1.15)}`);
    assert.ok(capRgba <= Math.max(rgba * 1.15, rgba + 10), `cap rgba (${capRgba}) wajib dekat aktual ${rgba}`);
});
