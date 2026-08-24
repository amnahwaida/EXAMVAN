/* Suite Batch 11 — jscore (milik agen batch-11-jscore-admin).
 * Referensi temuan: review_uiux_webui.md bagian "5.8 RE-REVIEW RONDE 5"
 * (ID: S61, R55, R58, T21 sisi dashboard).
 *
 * Run with:  node --test static/js/uiux-batch11-jscore.test.mjs   (from webui/)
 *
 * Latar belakang & dampak bisnis:
 *   - S61: popup kuota user masih menampilkan expires_at UTC mentah
 *     ("2026-08-24 17:00:00" padahal user di WIB melihat 24 jam dulu) —
 *     sub-item S49 yang tertinggal. Semua tampilan expires_at wajib lewat
 *     formatDateTimeID() satu-pintu.
 *   - R55: dua kalkulator durasi berlomba di halaman submissions — versi
 *     singkat "Xj Ym" di admin.js menimpa hasil formatter verbose milik
 *     submissions.html beberapa detik setelah render. Versi singkat DIHAPUS.
 *   - R58: countdown kedaluwarsa akun memakai Math.ceil sehingga "hari ini"
 *     mustahil tercapai (sisa 2 jam → ceil = 1 hari). Logika jam: <1 jam →
 *     "± N menit lagi", <24 jam → "Kedaluwarsa hari ini", sisanya floor hari.
 *   - T21 (dashboard): seluruh handler inline non-onclick (onsubmit/onchange/
 *     oninput) dimigrasi ke addEventListener / delegasi change — CSP-safe.
 *
 * Pola sama dengan suite lain: kontrak statik (fs-read) + perilaku via
 * vm.runInNewContext mengeksekusi fungsi ASLI dari sumber.
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

const ADMIN_JS_SRC = read('static/js/admin.js');
const ADMIN_CORE_SRC = read('static/js/admin-core.js');
const DASHBOARD = read('templates/admin/dashboard.html');

/** Ekstrak sumber deklarasi `function name(...) {...}` dengan penghitungan kurawal. */
function extractFunction(src, name) {
    const start = src.indexOf('function ' + name + '(');
    if (start === -1) return null;
    const open = src.indexOf('{', start);
    let depth = 0;
    for (let j = open; j < src.length; j++) {
        if (src[j] === '{') depth++;
        else if (src[j] === '}') {
            depth--;
            if (depth === 0) return src.slice(start, j + 1);
        }
    }
    return null;
}

// ---------------------------------------------------------------------------
// S61 — popup kuota user: expires_at diformat lokal (formatDateTimeID)
// ---------------------------------------------------------------------------

test('S61 (statik): render popup kuota user memformat expires_at via formatDateTimeID', () => {
    assert.match(ADMIN_JS_SRC,
        /const expiresAt = user\.expires_at \? formatDateTimeID\(user\.expires_at\) : '—'/,
        'expiresAt wajib lewat formatDateTimeID satu-pintu');
    assert.doesNotMatch(ADMIN_JS_SRC, /user\.expires_at \|\| '—'/,
        'pola mentah user.expires_at || \'—\' tidak boleh tersisa');
});

test('S61 (statik): tak ada interpolasi mentah user.expires_at ke string render', () => {
    // Pemakaian sah user.expires_at hanyalah: guard boolean, parse Date
    // (.replace(' ', 'T') ...), dan formatDateTimeID(user.expires_at).
    const rawInterpolations =
        ADMIN_JS_SRC.match(/\$\{user\.expires_at\}|'\s*\+\s*user\.expires_at\s*\+\s*'|"\s*\+\s*user\.expires_at\s*\+\s*"/) || [];
    assert.equal(rawInterpolations.length, 0,
        'user.expires_at tidak boleh diinterpolasi mentah ke HTML');
});

test('S61 (perilaku): blok edit user mengonversi UTC ke tanggal/jam LOKAL', () => {
    // Blok edit-user (~admin.js:2090) mem-parse "YYYY-MM-DD HH:MM:SS" sebagai
    // UTC (+Z) lalu mengisi input date/time dari getter lokal — pastikan
    // kontraknya tetap: parse ber-Z, isi via getFullYear/getHours (lokal).
    const editSrc = ADMIN_JS_SRC.slice(
        ADMIN_JS_SRC.indexOf("document.getElementById('editUserExpiry')"),
        ADMIN_JS_SRC.indexOf('syncEditLimitFields();'));
    assert.ok(editSrc.includes("+ 'Z'"), 'parse expires_at wajib treat as UTC (+Z)');
    assert.match(editSrc, /getFullYear\(\)/, 'isi input pakai komponen lokal');
    assert.match(editSrc, /getHours\(\)/, 'isi input jam pakai komponen lokal');
});

test('S61 (kontrak): formatDateTimeID satu-pintu tetap ada di admin-core.js', () => {
    const fn = extractFunction(ADMIN_CORE_SRC, 'formatDateTimeID');
    assert.ok(fn, 'formatDateTimeID harus ada di admin-core.js');
    assert.match(fn, /'Z'/, 'formatter treat input tanpa zona sebagai UTC');
});

// ---------------------------------------------------------------------------
// R55 — kalkulator durasi ganda di submissions: versi singkat "Xj Ym" dihapus
// ---------------------------------------------------------------------------

test('R55 (statik): admin.js bebas kalkulator durasi .duration-cell', () => {
    assert.doesNotMatch(ADMIN_JS_SRC, /duration-cell/,
        'querySelectorAll(\'.duration-cell\') versi singkat wajib dihapus (versi verbose milik submissions.html)');
});

test('R55 (statik): formatter durasi singkat ("Xj Ym") tidak tersisa di admin.js', () => {
    assert.doesNotMatch(ADMIN_JS_SRC, /\+\s*'j'/, 'unit jam singkat "j" hilang');
    assert.doesNotMatch(ADMIN_JS_SRC, /\+\s*'m'/, 'unit menit singkat "m" hilang');
    assert.doesNotMatch(ADMIN_JS_SRC, /parts\.join\(' '\)/, 'pola parts.join versi singkat hilang');
});

// ---------------------------------------------------------------------------
// R58 — countdown kedaluwarsa akun: logika jam, "Kedaluwarsa hari ini" terjangkau
// ---------------------------------------------------------------------------

function runExpiryCountdown(diffMs) {
    const fn = extractFunction(DASHBOARD, 'expiryCountdownHtml');
    assert.ok(fn, 'fungsi expiryCountdownHtml harus ada di dashboard.html');
    const ctx = {};
    vm.runInNewContext(fn + '; this.__out = expiryCountdownHtml;', ctx);
    return ctx.__out(diffMs);
}

const MIN = 60 * 1000;
const HOUR = 60 * MIN;
const DAY = 24 * HOUR;

test('R58 (perilaku): sudah lewat → badge "Kedaluwarsa"', () => {
    const html = runExpiryCountdown(-5 * MIN);
    assert.match(html, /Kedaluwarsa</);
    assert.doesNotMatch(html, /hari lagi/);
});

test('R58 (perilaku): sisa 10 menit → "± N menit lagi" (bukan pembulatan hari)', () => {
    const html = runExpiryCountdown(10 * MIN);
    assert.match(html, /±\s*10 menit lagi/, '10 menit tampil menit, bukan "1 hari lagi"');
    assert.doesNotMatch(html, /hari/);
});

test('R58 (perilaku): sisa 5 jam → "Kedaluwarsa hari ini" (Math.ceil lama membuat ini mustahil)', () => {
    const html = runExpiryCountdown(5 * HOUR);
    assert.match(html, /Kedaluwarsa hari ini/);
    assert.doesNotMatch(html, /hari lagi/);
});

test('R58 (perilaku): sisa 2 hari → floor "2 hari lagi" (ceil lama = 3)', () => {
    const html = runExpiryCountdown(2 * DAY + 3 * HOUR);
    assert.match(html, />2 hari lagi</, 'floor, bukan ceil: 2h3j bukan "3 hari"');
});

test('R58 (statik): Math.ceil penghitung hari hilang; DOMContentLoaded memakai expiryCountdownHtml', () => {
    assert.doesNotMatch(DASHBOARD, /Math\.ceil\(diffMs/,
        'pembulatan ceil penyebab bug R58 wajib hilang');
    assert.match(DASHBOARD, /expiryEl\.innerHTML = expiryCountdownHtml\(diffMs\)/,
        'countdown dirender lewat expiryCountdownHtml');
});

// ---------------------------------------------------------------------------
// T21 (dashboard) — 0 handler inline on*= ; migrasi addEventListener/delegasi
// ---------------------------------------------------------------------------

test('T21 (statik): dashboard.html bebas seluruh handler inline \\son[a-z]+=', () => {
    const hits = DASHBOARD.match(/\son[a-z]+=/g) || [];
    assert.deepEqual(hits, [],
        'dashboard.html wajib 0 inline handler (dapat 13: onchange/onsubmit/oninput)');
});

test('T21 (statik): form submit dimigrasi ke addEventListener', () => {
    for (const formId of ['changePasswordForm', 'editExamForm', 'editTokenForm', 'formEditInstansi']) {
        assert.match(DASHBOARD,
            new RegExp(`getElementById\\('${formId}'\\)[\\s\\S]{0,120}addEventListener\\('submit'`),
            `#${formId} wajib addEventListener('submit')`);
    }
});

test('T21 (statik): kontrol tabel ujian lewat delegasi change', () => {
    assert.match(DASHBOARD, /document\.addEventListener\('change'/,
        'delegasi change tingkat document terpasang');
    for (const marker of ['selectAllExams', 'mobile-select-all-exams', 'exam-checkbox',
        'token-mode-select', 'statusFilter']) {
        assert.match(DASHBOARD, new RegExp(marker),
            `delegasi harus mencakup ${marker}`);
    }
    // Argumen examId token-mode dibawa data attribute, bukan interpolasi JS.
    assert.match(DASHBOARD, /token-mode-select[^>]*data-exam-id=/,
        '.token-mode-select membawa data-exam-id untuk delegasi');
    assert.doesNotMatch(DASHBOARD, /onTokenModeChange\(this,\s*\{\{/,
        'interpolasi Go ke atribut JS inline tidak boleh tersisa');
});

test('T21 (statik): input file & sinkronisasi warna panel lewat listener', () => {
    assert.match(DASHBOARD, /getElementById\('editPdfFile'\)[\s\S]{0,120}addEventListener\('change'/,
        '#editPdfFile change listener');
    assert.match(DASHBOARD, /getElementById\('xmlFileInput'\)[\s\S]{0,120}addEventListener\('change'/,
        '#xmlFileInput change listener');
    assert.match(DASHBOARD, /panelColorEl\.addEventListener\('change'/,
        '#examPanelColor change listener (sinkron hex)');
    assert.match(DASHBOARD, /panelHexEl\.addEventListener\('input'/,
        '#panelColorHex input listener (sinkron color picker)');
});

test('R58 (kontrak lintas-agen): badge pakai warna token CSS, tanpa hex literal baru', () => {
    const fn = extractFunction(DASHBOARD, 'expiryCountdownHtml');
    assert.ok(fn);
    assert.doesNotMatch(fn, /#[0-9a-fA-F]{3,6}\b(?!\d)/,
        'fungsi countdown tidak menambah hex literal — pakai #f87171 via pemanggil lama? TIDAK: semua warna var()');
});
