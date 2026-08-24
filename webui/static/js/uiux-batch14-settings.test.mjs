/**
 * ══════════════════════════════════════════════════════════════════════════
 * Suite UI/UX BATCH 14 — SETTINGS (agen batch14-settings)
 * ══════════════════════════════════════════════════════════════════════════
 *
 * Referensi: review_uiux_webui.md bagian 5.11 RE-REVIEW RONDE 8. Cakupan
 * temuan milik agen ini:
 *
 *   S77 — renderVouchersError (settings-vouchers.js:17-18) & renderError
 *         voucher-audit (:8-9) menyisipkan `msg` dari res.message API mentah
 *         ke innerHTML — konvensi anti-XSS yang dipakukan S72 Batch 13 bocor
 *         di jalur error file yang sama.
 *
 *   S78 — loadVouchers / loadAuditLogs / loadUsersList / loadMyPackages
 *         fetch tanpa sequence-token: respons halaman lama yang lambat bisa
 *         mendarat TERAKHIR dan menimpa tabel+paginasi dengan data yang tak
 *         sesuai state UI. Kontrak: token permintaan monoton per daftar —
 *         pola statsRefreshInFlight (admin-core.js:745).
 *
 *   S79 — Tiga switch inti Pengaturan Umum (emailEnabledInput,
 *         turnstileEnabledInput, seoIndexInput) tanpa nama aksesibel: label
 *         span adalah SAUDARA checkbox, bukan ter-asosiasi → screen reader
 *         mendapat "checkbox, dicentang" anonim. Kontrak: aria-labelledby ke
 *         id yang eksis, ATAU input di dalam <label> ber-teks (pola kartu
 *         Monetisasi :1889).
 *
 *   S84(s) — settings.html:2140 merender ukuran aplikasi via inline
 *         document.write() (jalur admin dari temuan S84; jalur publiknya
 *         di suite batch14-publik).
 *
 *   R96(a) — Touch target paginasi vouchers/voucher-audit ≈24px tinggi dengan
 *         gap 6px — mis-tap mudah mendarat di nomor salah di tablet guru.
 *         Kontrak: tombol paginasi min-height ≥40px dan "(Lihat User)" ≥44px.
 *
 *   R97 — 'Generating...' satu-satunya string UI EN tampil-ke-user tersisa
 *         di modul settings (vs 'Menyimpan...' :231).
 *
 *   R98 — Dead code & wiring ganda: window.toggleUsersCollapse redundan;
 *         #btnOpenUploadApp di-wire DUA kali (addEventListener langsung +
 *         delegasi document) sehingga openUploadModalSafe() jalan dobel per
 *         klik — kontradiksi kontrak "satu handler per aksi" (R28); kondisi
 *         Go {{ if and (not $locked) … }} terduplikasi nested identik.
 *
 * Kepemilikan file agen ini: static/js/settings-vouchers.js,
 *   static/js/settings-voucher-audit.js, static/js/settings-users.js,
 *   templates/admin/settings.html, static/js/admin.js (loader users),
 *   static/js/settings-billing.js (loader paket).
 *
 * Run with:  node --test static/js/uiux-batch14-settings.test.mjs   (from webui/)
 */

import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const HERE = path.dirname(fileURLToPath(import.meta.url));
const WEBUI_ROOT = path.join(HERE, '..', '..');
const read = (...p) => fs.readFileSync(path.join(WEBUI_ROOT, ...p), 'utf8');

const VOUCHERS = read('static', 'js', 'settings-vouchers.js');
const AUDIT = read('static', 'js', 'settings-voucher-audit.js');
const USERS = read('static', 'js', 'settings-users.js');
const BILLING = read('static', 'js', 'settings-billing.js');
const ADMIN_JS = read('static', 'js', 'admin.js');
const SETTINGS = read('templates', 'admin', 'settings.html');

/** Ambil badan fungsi top-level `function NAME(...)` sampai `\nfunction ` berikutnya. */
function functionBody(src, name) {
    const start = src.indexOf(`function ${name}(`);
    assert.ok(start !== -1, `function ${name} ditemukan`);
    const end = src.indexOf('\nfunction ', start + 1);
    return src.slice(start, end === -1 ? undefined : end);
}

// ════════════════════════════════════════════════════════════════════════
// S77 — pesan error server ter-escape di kedua jalur renderError
// ════════════════════════════════════════════════════════════════════════

test('S77 (statik): renderVouchersError meng-escape msg sebelum innerHTML', () => {
    const body = functionBody(VOUCHERS, 'renderVouchersError');
    assert.match(body, /\$\{escapeHtml\(msg\)\}/,
        '${msg} dari res.message API disisipkan mentah — bungkus escapeHtml seperti viewRedemptions (:397)');
});

test('S77 (statik): renderAuditError voucher-audit meng-escape msg', () => {
    const body = functionBody(AUDIT, 'renderAuditError');
    assert.match(body, /escapeHtml\(msg\)/,
        'konkatenasi string + msg mentah melanggar konvensi sweep XSS S72 — bungkus escapeHtml(msg)');
});

// ════════════════════════════════════════════════════════════════════════
// S78 — anti race respons basi pada empat loader daftar
// ════════════════════════════════════════════════════════════════════════

for (const [file, src, fn] of [
    ['settings-vouchers.js', VOUCHERS, 'loadVouchers'],
    ['settings-voucher-audit.js', AUDIT, 'loadAuditLogs'],
    ['admin.js', ADMIN_JS, 'loadUsersList'],
    ['settings-billing.js', BILLING, 'loadMyPackages'],
]) {
    test(`S78 (statik): ${fn} (${file}) punya sequence-token anti respons basi`, () => {
        const body = functionBody(src, fn);
        const hasIncrement = /\+\+\s*[\w$]*[Ss]eq[\w$]*/.test(body);
        const hasCompare = /[\w$]*[Ss]eq[\w$]*\s*!==/.test(body);
        assert.ok(hasIncrement && hasCompare,
            `${fn} fetch tanpa guard urutan — klik paginasi cepat dapat menimpa tabel dengan data halaman lama. Pola: const seq = ++requestSeq; … apiFetch(…).then((res) => { if (seq !== requestSeq) return; …render… })`);
    });
}

// ════════════════════════════════════════════════════════════════════════
// S79 — nama aksesibel untuk tiga switch Pengaturan Umum
// ════════════════════════════════════════════════════════════════════════

for (const [id, judul] of [
    ['emailEnabledInput', 'Verifikasi OTP Email'],
    ['turnstileEnabledInput', 'Verifikasi Turnstile'],
    ['seoIndexInput', 'Izinkan Google Index'],
]) {
    test(`S79 (statik): switch ${id} (${judul}) punya nama aksesibel`, () => {
        const idx = SETTINGS.indexOf(`id="${id}"`);
        assert.ok(idx !== -1, `checkbox ${id} ada di settings.html`);
        // Konteks blok switch-container tempat checkbox berada.
        const blockStart = SETTINGS.lastIndexOf('switch-container', idx);
        const ctx = SETTINGS.slice(Math.max(0, blockStart - 50), idx + 500);
        const labelled = ctx.match(/aria-labelledby="([^"]+)"/);
        if (labelled) {
            // aria-labelledby WAJIB menunjuk id yang benar-benar eksis di halaman.
            assert.ok(SETTINGS.includes(`id="${labelled[1]}"`),
                `aria-labelledby="${labelled[1]}" menunjuk id yang tidak eksis`);
            return;
        }
        // Alternatif sah (pola kartu Monetisasi :1889): input berada DI DALAM
        // <label> yang memuat teks TERLIHAT — bukan <label class="switch">
        // kosong yang hanya membungkus slider.
        const labelStart = ctx.lastIndexOf('<label', ctx.indexOf(`id="${id}"`));
        const labelEnd = ctx.indexOf('</label>', labelStart);
        assert.ok(labelStart !== -1 && labelEnd !== -1,
            `${judul}: checkbox tidak terkurung <label> — tambahkan id pada teks label + aria-labelledby, atau pindahkan input ke dalam <label> ber-teks (pola kartu Monetisasi :1889)`);
        const labelText = ctx.slice(labelStart, labelEnd).replace(/<[^>]+>/g, '').replace(/\s+/g, ' ').trim();
        assert.ok(labelText.length >= 4,
            `<label class="switch"> tanpa teks TIDAK menghasilkan nama aksesibel ("${judul}" ada di span saudara, di luar label) — screen reader mendapat "checkbox, dicentang" anonim; gunakan aria-labelledby ke teks label atau pindahkan teks ke dalam <label>`);
    });
}

// ════════════════════════════════════════════════════════════════════════
// S84 (sisi admin) — settings.html bebas document.write
// ════════════════════════════════════════════════════════════════════════

test('S84-s (guard): settings.html bebas document.write()', () => {
    const n = (SETTINGS.match(/document\.write/g) || []).length;
    assert.equal(n, 0,
        `${n}× document.write (ukuran aplikasi sistem :2140) — hitung MB via template Go (printf "%.2f") atau helper; render-JS system-apps sudah benar via textContent`);
});

// ════════════════════════════════════════════════════════════════════════
// R96 (sisi admin) — touch target paginasi & aksi baris vouchers
// ════════════════════════════════════════════════════════════════════════

function minHeightFromStyle(styleStr) {
    const m = styleStr.match(/min-height:\s*(\d+(?:\.\d+)?)px/i);
    return m ? Number(m[1]) : null;
}

test('R96-a (statik): tombol paginasi vouchers min-height ≥40px', () => {
    const body = functionBody(VOUCHERS, 'renderPagination') || VOUCHERS;
    const btnStyle = VOUCHERS.match(/data-action="voucher-page"[^>]*style="([^"]*)"/)
        || VOUCHERS.match(/pagination-btn[^>]*style="([^"]*)"/);
    assert.ok(btnStyle, 'string render tombol paginasi vouchers ada');
    const mh = minHeightFromStyle(btnStyle[1]);
    assert.ok(mh !== null && mh >= 40,
        `min-height paginasi vouchers ${mh ?? '(tidak ada)'}px — target nyaman 44px (min. 40px); gap antar nomor juga jangan 6px`);
});

test('R96-a (statik): tombol paginasi voucher-audit min-height ≥40px', () => {
    const btnStyle = AUDIT.match(/pagination-btn[^>]*style="([^"]*)"/)
        || AUDIT.match(/data-action="audit-page"[^>]*style="([^"]*)"/);
    assert.ok(btnStyle, 'string render tombol paginasi audit ada');
    const mh = minHeightFromStyle(btnStyle[1]);
    assert.ok(mh !== null && mh >= 40,
        `min-height paginasi audit ${mh ?? '(tidak ada)'}px — naikkan (padding 4px 10px ≈24px tinggi saat ini)`);
});

test('R96-a (statik): link "(Lihat User)" baris voucher min-height ≥44px', () => {
    // style attribute berada SEBELUM teks label pada markup button.
    const m = VOUCHERS.match(/<button[^>]*style="([^"]*)"[^>]*>\s*\(Lihat User\)/)
        || VOUCHERS.match(/voucher-user-link[^>]*style="([^"]*)"/);
    assert.ok(m, 'link lihat-user pada baris voucher ada');
    const mh = minHeightFromStyle(m[1]);
    assert.ok(mh !== null && mh >= 44,
        `min-height "Lihat User" ${mh ?? '(tidak ada)'}px — aksi sekunder baris tetap wajib 44px di breakpoint sentuh`);
});

// ════════════════════════════════════════════════════════════════════════
// R97 — string UI EN terakhir di modul settings
// ════════════════════════════════════════════════════════════════════════

test('R97 (statik): "Generating..." → "Membuat voucher..." (paritas bahasa UI)', () => {
    assert.doesNotMatch(VOUCHERS, /'Generating\.\.\.'/,
        'satu-satunya string UI EN yang tampil ke user tersisa di tombol batch — selaraskan dengan \'Menyimpan...\' (:231)');
    assert.match(VOUCHERS, /Membuat voucher\.\.\./,
        'copy pengganti Bahasa Indonesia terpasang');
});

// ════════════════════════════════════════════════════════════════════════
// R98 — dead code & wiring ganda dirapikan
// ════════════════════════════════════════════════════════════════════════

test('R98 (statik): assignment window.toggleUsersCollapse yang redundan dihapus', () => {
    assert.doesNotMatch(USERS, /window\.toggleUsersCollapse\s*=/,
        'function declaration top-level sudah global; yang dipakai Actions adalah toggleAllUsersCollapse — assignment ini arwah');
});

test('R98 (statik): #btnOpenUploadApp hanya punya SATU jalur wiring (delegasi)', () => {
    // Markup menyumbang 1 kemunculan (id tombol). Jalur wiring ganda lama
    // (addEventListener langsung + closest delegation) menambah 2 lagi.
    const occurrences = (SETTINGS.match(/btnOpenUploadApp/g) || []).length;
    assert.ok(occurrences <= 2,
        `${occurrences} kemunculan btnOpenUploadApp — tiap klik menjalankan openUploadModalSafe() DUA kali (kontradiksi kontrak satu-handler-per-aksi R28); sisakan markup + delegasi saja`);
    assert.doesNotMatch(
        SETTINGS,
        /getElementById\(['"]#?btnOpenUploadApp['"]\)\s*\.\s*addEventListener|querySelector\(['"]#btnOpenUploadApp['"]\)\s*\.\s*addEventListener/,
        'listener langsung pada tombol unggah harus dihapus — delegasi document (closest) satu-satunya jalur',
    );
});

test('R98 (statik): kondisi Go {{ if and (not $locked) … }} tidak terduplikasi nested identik', () => {
    const line = '{{ if and (not $locked) (or $isSuper $isOp) }}';
    const n = SETTINGS.split(line).length - 1;
    assert.equal(n, 1,
        `${n}× kondisi if identik berturut-turut (:779-780) — duplikasi nested membuat blok ditutup dua kali (:1053 & :2164) dan rawan drift isi`);
});
