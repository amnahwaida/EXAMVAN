/* Guard Batch 11 — sisi settings (T21, S62, S63, S64).
 *
 * Latar belakang & dampak bisnis:
 *   Re-review ronde 5 (bagian 5.8 review_uiux_webui.md) menemukan:
 *   - T21: guard CSP "0 handler inline" selama ini hanya memindai `onclick=`
 *     sehingga handler non-onclick lolos radar — settings.html sendiri
 *     menyimpan ±20 onsubmit/onchange/onkeyup/oninput/onkeydown. Kontrak CSP
 *     jadi memberi rasa aman palsu: semua handler itu tetap butuh
 *     unsafe-inline. Batch 11 memindahkan SEMUANYA ke addEventListener yang
 *     di-wire dari blok script halaman (fungsi pemiliknya tidak disentuh —
 *     tetap hidup di admin.js / modul settings-*.js masing-masing).
 *   - S62: render-JS di settings-billing.js & settings-voucher-audit.js masih
 *     menginterpolasi onclick= ke atribut (id/halaman mentah) — migrasi ke
 *     data-action + Actions.register (pola Batch 8).
 *   - S63: #fff ×66 di settings.html adalah kelompok ad-hoc terbesar sisa —
 *     migrasi kontekstual ke token semantik (teks putih di surface gelap →
 *     var(--color-text); knob/konteks on-primary → var(--color-text-on-primary)).
 *   - S64: plafon guard basi — settings rgba cap 110 vs aktual 28 (longgar 82)
 *     dan count `!important` per CSS file tak dikunci test mana pun.
 *
 * Run with:  node --test static/js/uiux-batch11-settings-guard.test.mjs   (from webui/)
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const read = (rel) => fs.readFileSync(path.join(__dirname, '..', rel), 'utf8');

const SETTINGS = read('../templates/admin/settings.html');
const BILLING = read('js/settings-billing.js');
const AUDIT = read('js/settings-voucher-audit.js');

// Handler inline apa pun (bukan cuma onclick): onsubmit/onchange/onkeyup/
// oninput/onkeydown/onload/... — pola T21. Batch 15 (R105): flag /i —
// " ONCLICK=" kapital tidak boleh lolos guard CSP hanya dengan mengubah case.
const INLINE_HANDLER_RE = /\son[a-z]+=/gi;

// ---------------------------------------------------------------------------
// T21 — settings.html bebas SEMUA handler inline
// ---------------------------------------------------------------------------

test('T21: settings.html bebas atribut handler inline apa pun (=== 0)', () => {
    const n = (SETTINGS.match(INLINE_HANDLER_RE) || []).length;
    assert.equal(n, 0,
        `${n} handler inline tersisa di settings.html — pindah ke addEventListener ` +
        'di blok script halaman (kontrak CSP 0 handler inline kini nyata)');
});

test('T21: form submit di-wire via addEventListener (createUser, voucher single/batch, ubah password)', () => {
    // Wiring ada di blok script halaman (helper wire()); fungsi pemilik TIDAK dipindah.
    assert.match(SETTINGS, /wire\('newUserForm',\s*'submit'[\s\S]{0,120}createUser/,
        'form Tambah User harus memasang listener submit yang meneruskan event ke createUser');
    assert.match(SETTINGS, /wire\('changePasswordForm',\s*'submit'/,
        'form Ubah Password harus memasang listener submit');
    assert.match(SETTINGS, /wire\('formSingleVoucher',\s*'submit',\s*lazy\('submitSingleVoucher'\)\)/,
        'form voucher single harus memasang listener submit (modul lazy)');
    assert.match(SETTINGS, /wire\('formBatchVoucher',\s*'submit',\s*lazy\('submitBatchVoucher'\)\)/,
        'form voucher batch harus memasang listener submit (modul lazy)');
});

test('T21: pencarian & filter di-wire via addEventListener (users, voucher, audit)', () => {
    // Pencarian users: Enter langsung muat + live-search input.
    assert.match(SETTINGS, /wire\('userSearchInput',\s*'keydown'[\s\S]{0,160}__usersSearchTimer/,
        'Enter pada pencarian user harus lewat listener keydown (clear timer + muat halaman 1)');
    assert.match(SETTINGS, /wire\('userSearchInput',\s*'input',\s*function\(\)\s*\{\s*onUsersSearchInput\(this\);?\s*\}\s*\)/,
        'live-search users harus lewat listener input → onUsersSearchInput');
    // Filter/select users + preset paket.
    for (const [id, fn] of [['userRoleFilter', 'loadUsersList'], ['userPerPage', 'loadUsersList'],
                            ['userSortMobile', 'onUsersSortMobileChange'], ['packageSelect', 'applyPackagePreset']]) {
        const re = new RegExp("wire\\('" + id + "',\\s*'change'[\\s\\S]{0,160}" + fn);
        assert.match(SETTINGS, re, `select ${id} harus memasang listener change → ${fn}`);
    }
    // Voucher & audit.
    assert.match(SETTINGS, /wire\('voucherCodeInput',\s*'keyup'[\s\S]{0,80}redeemVoucher/,
        'klaim voucher via Enter harus lewat listener keyup');
    assert.match(SETTINGS, /wire\('searchVoucher',\s*'input',[\s\S]{0,80}toggleVoucherSearchClear/,
        'pencarian voucher harus lewat listener input → toggleVoucherSearchClear');
    assert.match(SETTINGS, /wire\('searchVoucher',\s*'keyup'[\s\S]{0,80}loadVouchers/,
        'Enter pada pencarian voucher harus lewat listener keyup → loadVouchers(1)');
    assert.match(SETTINGS, /wire\('auditSearchInput',\s*'keyup'[\s\S]{0,80}loadAuditLogs/,
        'pencarian riwayat audit via Enter harus lewat listener keyup → loadAuditLogs(1)');
});

test('T21: toggle & field dinamis di-wire via addEventListener (email, turnstile, nama file upload)', () => {
    for (const [id, fn] of [['emailEnabledInput', 'toggleEmailFields'],
                            ['turnstileEnabledInput', 'toggleTurnstileFields']]) {
        assert.match(SETTINGS,
            new RegExp("wire\\('" + id + "',\\s*'change'[\\s\\S]{0,100}" + fn),
            `checkbox ${id} harus memasang listener change → ${fn}`);
    }
    assert.match(SETTINGS, /wire\('appFile',\s*'change'[\s\S]{0,80}updateFileName/,
        'input file APK harus memasang listener change → updateFileName');
});

// ---------------------------------------------------------------------------
// S62 — onclick render-JS di billing & voucher-audit → data-action (pola B8)
// ---------------------------------------------------------------------------

test('S62: settings-billing.js & settings-voucher-audit.js bebas onclick= (=== 0)', () => {
    for (const [name, src] of [['settings-billing.js', BILLING], ['settings-voucher-audit.js', AUDIT]]) {
        const n = (src.match(/\sonclick=/g) || []).length;
        assert.equal(n, 0, `${n} onclick= tersisa di ${name} — pakai data-action + Actions.register`);
    }
});

test('S62: aksi render-JS terdaftar di modul pemiliknya (pola Batch 8)', () => {
    // Billing: aktifkan paket klaiman + retry daftar paket.
    assert.match(BILLING, /data-action="billing-package-activate"/,
        'tombol Aktifkan paket harus membawa data-action (tanpa interpolasi onclick)');
    assert.match(BILLING, /data-redemption-id=/,
        'id redemption harus lewat data-* (bukan interpolasi ke atribut event)');
    assert.match(BILLING, /Actions\.register\(\s*'billing-package-activate'[\s\S]{0,200}parseInt\(/,
        'registrasi aktifkan-paket wajib parseInt id (normalisasi tipe Batch 8)');
    assert.match(BILLING, /data-action="billing-packages-retry"/);
    assert.match(BILLING, /Actions\.register\(\s*'billing-packages-retry'/);
    // Audit: retry + paginasi.
    assert.match(AUDIT, /data-action="audit-retry"/);
    assert.match(AUDIT, /Actions\.register\(\s*'audit-retry'[\s\S]{0,200}parseInt\(/);
    assert.match(AUDIT, /data-action="audit-page"/);
    assert.match(AUDIT, /Actions\.register\(\s*'audit-page'[\s\S]{0,200}parseInt\(/);
});

// ---------------------------------------------------------------------------
// S63 — #fff dimigrasi ke token semantik
// ---------------------------------------------------------------------------

const HEX_RE = /#[0-9a-fA-F]{3,8}\b/g;

test('S63: settings.html bebas hex putih #fff/#ffffff (=== 0)', () => {
    const n = (SETTINGS.match(/#fff\b|#ffffff\b/gi) || []).length;
    assert.equal(n, 0,
        `${n} hex putih tersisa — teks di surface gelap → var(--color-text), ` +
        'konteks on-primary → var(--color-text-on-primary)');
});

test('S63: settings.html benar-benar memakai token pengganti (sanity > 0)', () => {
    assert.ok((SETTINGS.match(/var\(--color-text-on-primary\)/g) || []).length > 0,
        'token --color-text-on-primary harus terpakai (knob switch/konteks on-primary)');
});

// ---------------------------------------------------------------------------
// S64 — plafon ulang settings + plafon !important CSS inti
// ---------------------------------------------------------------------------

// Regex digit-pembuka (pola S43): rgba(var(--rgb-*), α) bukan literal.
// Batch 15 (R105): flag /i — "RGBA(255,…" kapital wajib tetap terhitung.
const RGBA_RE = /rgba\(\s*[0-9]/gi;

test('S64 (guard): rgba literal di settings.html tidak naik dari baseline pasca-Batch 11', () => {
    const n = (SETTINGS.match(RGBA_RE) || []).length;
    assert.ok(n <= 28,
        `rgba literal settings.html = ${n}, plafon baru ≤ 28 (cap lama 110 terlalu longgar — bisa nambah 82 literal tanpa alarm)`);
});

test('S64/R61+R61-lanjutan (guard): count !important per CSS inti tidak naik dari baseline hari ini', () => {
    // Batch 13 (S71): cap dikunci ulang ke ANGKA AKTUAL terukur 24 Agustus 2026
    // (admin-base 47, hasil 65, public-desktop 21, public-mobile 48 — hitungan
    // BARIS ber-!important, pola audit review) — cap lama menyisakan slack
    // total 55 sehingga guard melindungi tempat yang salah.
    const CAPS = {
        'css/admin-base.css': 47,
        'css/hasil.css': 65,
        'css/public-desktop.css': 21,
        'css/public-mobile.css': 48,
        // R90 (ronde 8): theme.css ikut di-cap — satu-satunya CSS inti tanpa
        // plafon sebelumnya (aktual 1: skip-link). Lubang yang sama yang
        // dikritik S71 tidak boleh terbuka lagi.
        'css/theme.css': 1,
    };
    const countImportantLines = (src) => src.split('\n').filter((l) => l.includes('!important')).length;
    for (const [file, cap] of Object.entries(CAPS)) {
        const n = countImportantLines(read(file));
        assert.ok(n <= cap,
            `!important ${file} = ${n} baris, plafon ≤ ${cap} (baseline terukur 24 Agustus 2026 — ` +
            'jangan tambah important baru; selesaikan specificity secara wajar)');
    }
});

// ---------------------------------------------------------------------------
// Kontrak lintas-agen — folder-wide bebas handler inline
// ---------------------------------------------------------------------------

test('KONTRAK lintas-agen (T21): SELURUH templates/**/*.html bebas handler inline', () => {
    const templatesDir = path.join(__dirname, '..', '..', 'templates');
    const files = fs.readdirSync(templatesDir, { recursive: true })
        .map((f) => path.join(templatesDir, f))
        .filter((f) => f.endsWith('.html'));
    const offenders = [];
    for (const f of files) {
        const src = fs.readFileSync(f, 'utf8');
        const n = (src.match(INLINE_HANDLER_RE) || []).length;
        if (n > 0) offenders.push(path.relative(process.cwd(), f) + ' ×' + n);
    }
    assert.deepEqual(offenders, [],
        'semua agen berkontrak mencapai 0 handler inline — sisa: ' + offenders.join(', '));
});
