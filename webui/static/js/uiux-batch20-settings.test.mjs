/**
 * ══════════════════════════════════════════════════════════════════════════
 * Suite UI/UX BATCH 20 — ARSITEKTUR MODAL SETTINGS & INPUT PARITY
 * (S118, R148, R149 — dieksekusi koordinator)
 * ══════════════════════════════════════════════════════════════════════════
 *
 * Referensi: review_uiux_webui.md bagian 5.17 RE-REVIEW RONDE 14.
 *
 *   S118 — MODAL SETTINGS DI LUAR MODAL MANAGER GLOBAL: lima modal modul
 *         vouchers/billing/system-apps dibuka langsung via
 *         `el.style.display = 'flex'` sehingga tidak mendapat focus trap Tab,
 *         Escape-to-close, restore fokus ke pemicu, dan scroll-lock dari
 *         Modal Manager (admin-core.js). Kontrak:
 *           - open*Modal() memakai Modal.open(...), BUKAN assignment display;
 *           - close*Modal() memakai Modal.close(...);
 *           - redemptionsModal (read-only) ikut dimigrasi.
 *
 *   R148 — MODAL FORM DITUTUP TANPA DIRTY-GUARD: closeSingleModal/
 *         closeBatchModal menghapus seluruh isian tanpa konfirmasi (kelas S2).
 *         Kontrak:
 *           - helper `modalHasUserInput(modalId)` mendeteksi isian user pada
 *             input/textarea non-hidden yang tidak kosong;
 *           - close*(tanpa force) + form berisi → showConfirm konfirmasi;
 *             hanya setelah OK modal ditutup;
 *           - close*(true) = force (dipakai jalur SUKSES submit, :269/:316)
 *             menutup tanpa bertanya.
 *
 *   R149 — SMTP PORT TANPA CONSTRAINT: input type="text" bebas untuk nilai
 *         numerik 1–65535. Kontrak markup: type="number" min="1"
 *         max="65535" inputmode="numeric".
 */
import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import vm from 'node:vm';

const ROOT = new URL('.', import.meta.url).pathname.replace(/\/static\/js\/$/, '');
const read = (p) => readFileSync(join(ROOT, p), 'utf8');

const VOUCHERS_JS = read('static/js/settings-vouchers.js');
const APPS_JS = read('static/js/settings-system-apps.js');
const SETTINGS_HTML = read('templates/admin/settings.html');

// ═══════════════ S118 — arsitektur modal ═══════════════

test('S118a: open/close modal vouchers memakai Modal Manager, bukan display manual', () => {
    // Pasangan open/close wajib lewat Modal.open/close (id string).
    assert.match(VOUCHERS_JS, /function openSingleModal\(\)\s*\{\s*Modal\.open\('singleModal'\);?\s*\}/);
    assert.match(VOUCHERS_JS, /function openBatchModal\(\)\s*\{\s*Modal\.open\('batchModal'\);?\s*\}/);
    // Tidak ada lagi assignment display manual pada kelima modal settings.
    for (const id of ['singleModal', 'batchModal', 'redemptionsModal']) {
        assert.doesNotMatch(VOUCHERS_JS,
            new RegExp(`getElementById\\('${id}'\\)\\.style\\.display\\s*=\\s*'flex'`),
            `${id} masih dibuka via display manual (di luar Modal Manager)`);
    }
});

test('S118b: upload modal system-apps memakai Modal Manager', () => {
    assert.match(APPS_JS, /Modal\.open\(modal\)|Modal\.open\('uploadModal'\)/,
        'openUploadModal wajib Modal.open');
    // Guard unggah berlangsung tetap dipertahankan (canCloseUpload).
    assert.match(APPS_JS, /canCloseUpload/, 'guard canCloseUpload jangan hilang saat migrasi');
});

test('R148a: helper modalHasUserInput eksis & mengecualikan hidden/disabled/button', () => {
    const start = VOUCHERS_JS.indexOf('function modalHasUserInput(');
    assert.ok(start > -1, 'helper modalHasUserInput eksis');
    const block = VOUCHERS_JS.slice(start, start + 700);
    assert.match(block, /input:\[?type="?hidden|input:not\(\[type=.hidden/, 'mengecualikan type=hidden');
    assert.match(block, /disabled/, 'mengecualikan field disabled');
});

test('R148b (vm): tutup dengan isian -> showConfirm; OK baru benar-benar menutup', async () => {
    const sb = buildVoucherSandbox({ singleValue: 'EV-XYZ', confirmOk: true });
    sb.closeSingleModal();
    await new Promise((r) => setTimeout(r, 0));
    assert.ok(sb.__confirmed(), 'showConfirm tampil saat form berisi');
    assert.equal(sb.__closed.single, true, 'setelah OK, modal ditutup');
});

test('R148c (vm): tutup tanpa isian -> langsung tutup tanpa konfirmasi', async () => {
    const sb = buildVoucherSandbox({ singleValue: '', confirmOk: false });
    sb.closeSingleModal();
    await new Promise((r) => setTimeout(r, 0));
    assert.equal(sb.__confirmed(), false, 'form kosong tidak perlu konfirmasi');
    assert.equal(sb.__closed.single, true);
});

test('R148d (vm): jalur sukses (force=true) menutup tanpa konfirmasi walau form berisi', async () => {
    const sb = buildVoucherSandbox({ singleValue: 'EV-XYZ', confirmOk: false });
    sb.closeSingleModal(true);
    await new Promise((r) => setTimeout(r, 0));
    assert.equal(sb.__confirmed(), false, 'force=true tidak bertanya');
    assert.equal(sb.__closed.single, true);
});

// Harness vm untuk R148: ekstraksi fungsi asli + stub document/Modal/showConfirm.
function buildVoucherSandbox(opts) {
    const closed = { single: false };
    let confirms = 0;
    const singleEl = {
        style: {},
        querySelectorAll(sel) {
            if (!sel.includes('input') && !sel.includes('textarea')) return [];
            return [{ value: opts.singleValue, disabled: false, type: 'text' }];
        },
    };
    const sandbox = {
        document: { getElementById(id) { return id === 'singleModal' ? singleEl : null; } },
        Modal: {
            open() {}, 
            close(id) { if (id === 'singleModal') closed.single = true; },
        },
        showConfirm() { confirms++; return Promise.resolve(opts.confirmOk); },
        __confirmShown: () => confirms > 0,
        __closed: closed,
    };
    sandbox.__confirmShownGetter = () => confirms > 0;
    // Fungsi pembaca jumlah konfirmasi (getter object tidak aman untuk
    // vm.createContext - contextify menolak properti accessor).
    sandbox.__confirmed = () => confirms > 0;
    // Fungsi asli mengakses document/Modal/showConfirm sebagai global konteks -
    // properti sandbox sudah cukup (tidak ada pemakaian window di fungsinya).
    vm.createContext(sandbox);
    const names = ['modalHasUserInput', 'closeSingleModal'];
    for (const n of names) {
        const re = new RegExp('function ' + n + '\\([\\s\\S]*?\\n}');
        const fn = VOUCHERS_JS.match(re);
        assert.ok(fn, 'fungsi ' + n + ' eksis di settings-vouchers.js');
        vm.runInContext(fn[0], sandbox, { filename: n });
    }
    return sandbox;
}

// ═══════════════ R149 — parity input numerik ═══════════════

test('R149: smtpPortInput type=number dengan batas 1-65535 + inputmode', () => {
    const tag = SETTINGS_HTML.match(/<input[^>]*id="smtpPortInput"[^>]*>/);
    assert.ok(tag, 'smtpPortInput eksis');
    assert.match(tag[0], /type="number"/, 'type="number"');
    assert.match(tag[0], /min="1"/, 'min="1"');
    assert.match(tag[0], /max="65535"/, 'max="65535"');
    assert.match(tag[0], /inputmode="numeric"/, 'inputmode="numeric"');
});
