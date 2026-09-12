/* Contract tests untuk Batch 36 — behavioral LOW tab Settings
 * (review_ui_halaman_web_2026-09-12.md — L51, L52, L53, L54, L55, L57, L58, L74).
 *
 *   L51 — header tabel voucher kini sortable, paritas tab Users: th.sortable +
 *         data-sort + data-action + indikator + aria-sort dinamis; server
 *         membatasi kolom lewat whitelist (bukan raw ORDER BY).
 *   L52 — wiring manual tbody voucher (listener sendiri + flag dataset)
 *         diganti registrasi registry Actions (delegasi tunggal admin-core).
 *   L53 — modal redeem billing dibuka lewat API Modal terpusat.
 *   L54 — guard S108 (klik [data-action] tidak melipat kartu) disalin ke
 *         handler collapse Tab Umum.
 *   L55 — dua input pencarian settings di-guard sebelum deref .value.
 *   L57 — fallback max_concurrent_exams tidak lagi menulis "1" diam-diam;
 *         data tak tersedia ditampilkan "—".
 *   L58 — rotasi ikon accordion/toggle-all lewat kelas CSS, bukan
 *         style.transform inline dari JS.
 *   L74 — empat idiom bind-once dataset-flag diganti helper wireOnce core.
 *
 * Run with: node --test static/js/uiux-batch36-settings-low.test.mjs (from webui/)
 */

import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const HERE = path.dirname(fileURLToPath(import.meta.url));
const WEBUI_ROOT = path.join(HERE, '..', '..');
const read = (...p) => fs.readFileSync(path.join(...p), 'utf8');

const SETTINGS = read(WEBUI_ROOT, 'templates', 'admin', 'settings.html');
const VOUCHERS = read(WEBUI_ROOT, 'static', 'js', 'settings-vouchers.js');
const BILLING = read(WEBUI_ROOT, 'static', 'js', 'settings-billing.js');
const GENERAL = read(WEBUI_ROOT, 'static', 'js', 'settings-general.js');
const USERS = read(WEBUI_ROOT, 'static', 'js', 'settings-users.js');
const AUDIT = read(WEBUI_ROOT, 'static', 'js', 'settings-voucher-audit.js');
const SYSAPPS = read(WEBUI_ROOT, 'static', 'js', 'settings-system-apps.js');
const ADMIN_JS = read(WEBUI_ROOT, 'static', 'js', 'admin.js');
const ADMIN_CORE = read(WEBUI_ROOT, 'static', 'js', 'admin-core.js');
const VOUCHER_MODEL = read(WEBUI_ROOT, 'internal', 'models', 'voucher.go');
const VOUCHER_HANDLER = read(WEBUI_ROOT, 'internal', 'handlers', 'admin', 'vouchers.go');

// ---------------------------------------------------------------------------
// L51 — sortable voucher
// ---------------------------------------------------------------------------

test('L51a: th tabel voucher memakai pola sortable users (class+data-sort+data-action+indikator)', () => {
    const ths = SETTINGS.match(/<th[^>]*data-action="voucher-toggle-sort"[^>]*>/g) || [];
    assert.equal(ths.length, 5, '5 kolom voucher sortable (kode/paket/penggunaan/kadaluarsa/status)');
    for (const tag of ths) {
        assert.match(tag, /class="sortable"/);
        assert.match(tag, /data-sort="/);
        assert.match(tag, /tabindex="0"/, 'header sortable tetap fokusable');
    }
    // Indikator arah ada di dalam tiap th (span child).
    for (const tag of ths) {
        const end = SETTINGS.indexOf('</th>', SETTINGS.indexOf(tag));
        assert.match(SETTINGS.slice(SETTINGS.indexOf(tag), end), /sort-indicator/,
            'indikator arah tersedia di th: ' + tag.slice(0, 60));
    }
});

test('L51b: server membatasi kolom sort lewat whitelist, bukan interpolasi mentah', () => {
    assert.match(VOUCHER_MODEL, /var voucherSortExprs = map\[string\]string\{/,
        'whitelist voucherSortExprs terdefinisi di models');
    assert.match(VOUCHER_MODEL, /listVouchersOrderBy\(opts ListVouchersOpts\)/);
    assert.match(VOUCHER_MODEL, /ORDER BY %s/, 'ORDER BY lewat placeholder (whitelist)');
    assert.doesNotMatch(VOUCHER_MODEL, /ORDER BY v\." \+/, 'tidak ada konkatenasi raw');
    assert.match(VOUCHER_HANDLER, /SortBy:\s*sortBy/, 'handler meneruskan sort_by');
    assert.match(VOUCHER_HANDLER, /SortDir:\s*sortDir/, 'handler meneruskan sort_dir');
});

test('L51c: frontend mengirim sort_by & menyinkronkan aria-sort header', () => {
    assert.match(VOUCHERS, /sort_by=/, 'query sort_by dikirim ke API');
    assert.match(VOUCHERS, /function toggleVoucherSort\(/, 'siklus 3-klik tersedia');
    assert.match(VOUCHERS, /function refreshVoucherSortHeaders\(/);
    assert.match(VOUCHERS, /setAttribute\('aria-sort'/, 'aria-sort dinamis (paritas users)');
    // Guard lintas-modul: handler keydown generic hanya melayani users,
    // voucher punya handler sendiri. Cari blok yang MEMANGGIL toggleUsersSort
    // lalu pastikan ber-guard data-action users.
    const callIdx = ADMIN_JS.indexOf('toggleUsersSort(th.getAttribute');
    assert.ok(callIdx !== -1, 'pemanggilan toggleUsersSort dari keydown ada');
    const blockStart = ADMIN_JS.lastIndexOf("document.addEventListener('keydown'", callIdx);
    const generic = ADMIN_JS.slice(blockStart, callIdx);
    assert.match(generic, /users-toggle-sort/,
        'generic keydown di-guard agar tidak menembak th voucher');
});

// ---------------------------------------------------------------------------
// L52 — registry Actions untuk aksi baris voucher
// ---------------------------------------------------------------------------

test('L52: wiring manual tbody dihapus; aksi baris terdaftar di registry Actions', () => {
    assert.doesNotMatch(VOUCHERS, /rowActionsWired/,
        'flag dataset.rowActionsWired (idiom lama) dihapus');
    const body = VOUCHERS.slice(VOUCHERS.indexOf('function wireVoucherRowActions'),
        VOUCHERS.indexOf('function wireVoucherRowActions') + 400);
    assert.doesNotMatch(body, /tbody\.addEventListener\('click'/,
        'tidak ada lagi listener klik tbody manual');
    for (const action of ['copy', 'redemptions', 'toggle', 'delete']) {
        assert.match(VOUCHERS, new RegExp(`Actions\\.register\\(\\s*'${action}'`),
            `handler '${action}' terdaftar di registry`);
    }
});

// ---------------------------------------------------------------------------
// L53 — Modal.open untuk redeem
// ---------------------------------------------------------------------------

test('L53: modal redeem billing lewat API Modal terpusat', () => {
    assert.match(BILLING, /Modal\.open\('confirmRedeemModal'\)/);
    assert.match(BILLING, /Modal\.close\('confirmRedeemModal'\)/);
    assert.doesNotMatch(BILLING, /confirmRedeemModal'\)\.style\.display/,
        'manipulasi style.display manual dihapus');
});

// ---------------------------------------------------------------------------
// L54 — guard data-action pada collapse Tab Umum
// ---------------------------------------------------------------------------

test('L54: handler collapse Tab Umum punya guard [data-action] (paritas S108 users)', () => {
    const wire = GENERAL.slice(GENERAL.indexOf('function wireCollapseBlock'),
        GENERAL.indexOf('function setupGeneralCollapse'));
    assert.match(wire, /e\.target\.closest\('?\[data-action\]'\)/,
        'klik aksi di dalam head tidak boleh ikut melipat kartu');
});

// ---------------------------------------------------------------------------
// L55 — null-guard input pencarian
// ---------------------------------------------------------------------------

test('L55: dua handler pencarian settings di-guard sebelum deref .value', () => {
    assert.match(VOUCHERS, /const searchEl = document\.getElementById\('searchVoucher'\);\s*\n\s*const search = searchEl \? searchEl\.value\.trim\(\) : '';/);
    assert.match(AUDIT, /const searchEl = document\.getElementById\('auditSearchInput'\);\s*\n\s*const search = searchEl \? searchEl\.value\.trim\(\) : '';/);
});

// ---------------------------------------------------------------------------
// L57 — fallback tidak lagi menulis "1" diam-diam
// ---------------------------------------------------------------------------

test('L57: max_concurrent_exams tanpa nilai tampil "—", bukan placeholder 1', () => {
    assert.doesNotMatch(BILLING, /p\.max_concurrent_exams \|\| p\.max_exams \|\| 1/,
        'fallback terselubung ke 1 dihapus');
    assert.match(BILLING, /Number\.isFinite\(p\.max_concurrent_exams\)/,
        'hanya nilai terdefinisi yang dirender');
    assert.match(BILLING, / : '—'/, 'data tidak tersedia ditampilkan —');
});

// ---------------------------------------------------------------------------
// L58 — rotasi ikon via kelas CSS
// ---------------------------------------------------------------------------

test('L58: rotasi ikon toggle-all lewat kelas CSS, transform terpusat', () => {
    assert.doesNotMatch(GENERAL, /icon\.style\.transform/,
        'style.transform inline general dihapus');
    assert.doesNotMatch(USERS, /icon\.style\.transform/,
        'style.transform inline users dihapus');
    assert.match(GENERAL, /icon\.classList\.toggle\('collapsed'/);
    assert.match(USERS, /icon\.classList\.toggle\('collapsed'/);
    assert.match(read(WEBUI_ROOT, 'static', 'css', 'admin-base.css'),
        /#toggleAllGeneralIcon\.collapsed\s*\{[^}]*transform: rotate\(180deg\)/,
        'nilai transform terpusat di admin-base.css');
});

// ---------------------------------------------------------------------------
// L74 — wireOnce menggantikan dataset-flag
// ---------------------------------------------------------------------------

test('L74: helper wireOnce ada di core; keempat situs dataset-flag dimigrasi', () => {
    assert.match(ADMIN_CORE, /function wireOnce\(el, key, fn\)/,
        'helper bind-once terpusat di admin-core.js');
    // Tidak ada lagi flag 'xxxWired' yang diset via dataset (komentar
    // dokumentasi dikecualikan lewat pola assignment).
    assert.doesNotMatch(VOUCHERS, /dataset\.(rowActions|retry)Wired\s*=/);
    assert.doesNotMatch(GENERAL, /dataset\.dirtyWired\s*=/);
    assert.doesNotMatch(SYSAPPS, /dataset\.closeGuardWired\s*=/);
    assert.match(GENERAL, /wireOnce\(card, 'saas-dirty'/);
    assert.match(SYSAPPS, /wireOnce\(modal, 'upload-close-guard'/);
    assert.match(VOUCHERS, /wireOnce\(body, 'redemptions-retry'/);
});
