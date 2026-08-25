/**
 * ══════════════════════════════════════════════════════════════════════════
 * Suite UI/UX BATCH 14 — PUBLIK (agen batch14-publik)
 * ══════════════════════════════════════════════════════════════════════════
 *
 * Referensi: review_uiux_webui.md bagian 5.11 RE-REVIEW RONDE 8. Cakupan
 * temuan milik agen ini:
 *
 *   T25 — Baris hasil publik dirender interaktif via JS:
 *         tr.setAttribute('tabindex','0'); tr.setAttribute('role','button')
 *         (hasil.html:614-615) — tetapi browser umumnya TIDAK menggambar
 *         outline fokus untuk <tr>, dan hasil.css hanya punya :hover.
 *         Pengguna keyboard tidak bisa melihat baris mana yang difokuskan
 *         (WCAG 2.4.7). Kontrak: rule :focus-visible khusus baris role-
 *         button dengan outline token primary-light.
 *
 *   S82 — Blind-spot guard token: register_confirm.html & reset_password.html
 *         penuh hex/rgba literal (7 hex + 19 rgba; 2 hex + 8 rgba) sementara
 *         register.html sudah 0/0 — karena FILES guard batch8-publik hanya
 *         memuat download/shared/register. Kontrak: kedua file masuk daftar
 *         guard B8 DAN literal-literalnya bermigrasi ke token.
 *
 *   S83 — hasil.html literal intra-blok tak konsisten: satu baris sudah
 *         rgba(var(--rgb-warning),…) tetapi tetangganya masih rgba(16,185,…),
 *         rgba(239,68,68,…)×2, rgba(148,163,184,…)×2; state disabled masih
 *         #ef4444 (kontradiksi S70 Batch 13) dan #f8fafc duplikat nilai
 *         --color-text. Kontrak: semua migrasi + file masuk guard B8.
 *
 *   S84 — Ukuran APK dirender inline `document.write()` di download.html
 *         (:538,:556,:698,:777) — deprecated, blocking, dan dengan JS mati
 *         pengguna membaca "Ukuran:  MB" telanjang. Kontrak: 0 document.write
 *         di seluruh templates/public/**; ukuran dihitung server-side.
 *
 *   R91 — Panel evaluasi hasil menyisipkan statusText & display-time mentah
 *         ke innerHTML (hasil.html:749,:803,:807) padahal seluruh jalur lain
 *         lewat escapeHtml — kontrak defense-in-depth file itu sendiri
 *         (:1006) bocor di dua titik ini.
 *
 *   R92 — Dead attribute data-name (nama siswa / PII) ditempel ke setiap
 *         <tr> hasil tanpa ada pembaca dataset.name lagi (pencarian
 *         server-side sejak Batch 10).
 *
 *   R93 — Paritas pre-submit OTP: tombol submit reset_password tidak pernah
 *         di-disabled saat OTP belum lengkap (register_confirm sudah benar)
 *         → OTP kosong lolos ke POST = round-trip server sia-sia.
 *
 *   R94 — Copy error Turnstile merujuk posisi layar ("verifikasi keamanan
 *         di atas") di register/forgot/reset — rapuh terhadap fold dan
 *         bermakna kosong bagi screen reader.
 *
 * Kepemilikan file agen ini: templates/public/**, static/css/hasil.css,
 *   static/js/uiux-batch8-publik.test.mjs (perluasan FILES).
 *
 * Run with:  node --test static/js/uiux-batch14-publik.test.mjs   (from webui/)
 */

import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { sliceBlock } from './uiux-batch15-guard-util.mjs';

const HERE = path.dirname(fileURLToPath(import.meta.url));
const WEBUI_ROOT = path.join(HERE, '..', '..');
const PUBLIC = path.join(WEBUI_ROOT, 'templates', 'public');
const read = (...p) => fs.readFileSync(path.join(...p), 'utf8');

const HASIL_HTML = read(PUBLIC, 'hasil.html');
const HASIL_CSS = read(WEBUI_ROOT, 'static', 'css', 'hasil.css');
const REG_CONFIRM = read(PUBLIC, 'register_confirm.html');
const RESET_PW = read(PUBLIC, 'reset_password.html');
const B8_GUARD = read(WEBUI_ROOT, 'static', 'js', 'uiux-batch8-publik.test.mjs');

// ════════════════════════════════════════════════════════════════════════
// T25 — fokus keyboard terlihat pada baris hasil
// ════════════════════════════════════════════════════════════════════════

test('T25 (statik): baris hasil memang dirender sebagai kontrol custom tabindex+role', () => {
    // Penjaga asumsi kontrak: jika render berubah (misal jadi <button> baris),
    // test ini wajib direvisi bersama assertion focus di bawah.
    assert.match(HASIL_HTML, /setAttribute\('tabindex',\s*'0'\)/,
        "tr.setAttribute('tabindex','0') ada");
    assert.match(HASIL_HTML, /setAttribute\('role',\s*'button'\)/,
        "tr.setAttribute('role','button') ada");
});

test('T25 (statik): hasil.css punya indikator :focus-visible untuk baris role-button (outline token)', () => {
    const rules = [...HASIL_CSS.matchAll(/[^{}]*results-table[^{}]*tr[^{}]*\{[^}]*\}/g)].map((m) => m[0]);
    const focusRule = rules.find((r) => r.includes(':focus-visible') || r.includes(':focus'));
    assert.ok(focusRule,
        'tidak ada rule fokus untuk .results-table tbody tr — browser tidak menggambar outline default <tr>; tambahkan tr[role="button"]:focus-visible { outline: … var(--color-primary-light) }');
    assert.match(focusRule, /outline/,
        'indikator fokus harus berupa outline (bukan hanya background hover)');
    assert.match(focusRule, /var\(--color-primary-light\)/,
        'warna outline pakai token primary-light (#a5b4fc, kontras cukup di dark bg)');
});

// ════════════════════════════════════════════════════════════════════════
// S82 — register_confirm & reset_password masuk guard + migrasi literal
// ════════════════════════════════════════════════════════════════════════

test('S82 (statik): guard batch8-publik mencantumkan register_confirm & reset_password di FILES', () => {
    for (const name of ['register_confirm.html', 'reset_password.html']) {
        assert.ok(B8_GUARD.includes(name),
            `${name} belum masuk FILES uiux-batch8-publik.test.mjs — dua halaman OTP tumbuh literal tanpa alarm`);
    }
});

for (const [label, src] of [['register_confirm.html', REG_CONFIRM], ['reset_password.html', RESET_PW]]) {
    test(`S82 (guard): ${label} bebas hex/rgba literal brand (migrasi token)`, () => {
        const forbidden = [
            /#a5b4fc/i,           // → var(--color-primary-light)
            /#4ade80/i,           // → var(--color-success-light)
            /#34d399/i,           // → var(--color-success-light)
            /rgba\(99,\s*102,\s*241/,  // triplet info → rgba(var(--rgb-info), α)
            /rgba\(255,\s*255,\s*255/, // putih → rgba(var(--rgb-white), α)
            /rgba\(34,\s*197,\s*94/,   // triplet success → rgba(var(--rgb-success), α)
        ];
        for (const re of forbidden) {
            const n = (src.match(new RegExp(re.source, 'gi')) || []).length;
            assert.equal(n, 0,
                `${n}× ${re} tersisa di ${label} — migrasi ke token theme.css (pola register.html yang sudah 0/0)`);
        }
    });
}

// ════════════════════════════════════════════════════════════════════════
// S83 — hasil.html konsisten token (termasuk state disabled/error)
// ════════════════════════════════════════════════════════════════════════

test('S83 (guard): hasil.html bebas literal intra-blok yang inkonsisten', () => {
    const forbidden = [
        [/ef4444/i, '#ef4444 → var(--color-danger-bright) (kontradiksi langsung dengan S70 Batch 13)'],
        [/f8fafc/i, '#f8fafc → var(--color-text) (duplikat nilai token teks)'],
        [/rgba\(16,\s*185,\s*129/, 'rgba(16,185,129,α) → rgba(var(--rgb-success), α)'],
        [/rgba\(239,\s*68,\s*68/, 'rgba(239,68,68,α) → rgba(var(--rgb-danger), α)'],
        [/rgba\(148,\s*163,\s*184/, 'rgba(148,163,184,α) → rgba(var(--rgb-white/black), α) atau token muted'],
    ];
    for (const [re, msg] of forbidden) {
        const n = (HASIL_HTML.match(new RegExp(re.source, 'gi')) || []).length;
        assert.equal(n, 0, `${n}× ${msg}`);
    }
});

test('S83 (statik): guard batch8-publik mencantumkan hasil.html di FILES', () => {
    assert.ok(B8_GUARD.includes('hasil.html'),
        'hasil.html belum masuk FILES guard B8 — satu-satunya halaman hasil publik harus ikut terjaga');
});

// ════════════════════════════════════════════════════════════════════════
// S84 — 0 document.write di seluruh template publik
// ════════════════════════════════════════════════════════════════════════

test('S84 (guard folder-wide): templates/public/** bebas document.write()', () => {
    const files = fs.readdirSync(PUBLIC).filter((f) => f.endsWith('.html'));
    const hits = [];
    for (const f of files) {
        const src = read(PUBLIC, f);
        if (/document\.write/.test(src)) hits.push(f);
    }
    assert.deepEqual(hits, [],
        `${hits.join(', ')} masih merender konten via document.write (deprecated, blocking, blank saat JS mati) — hitung ukuran MB di handler Go (helper formatSizeMB / printf "%.2f") lalu cetak via template`);
});

// ════════════════════════════════════════════════════════════════════════
// R91 — escape seragam 100% di panel evaluasi hasil
// ════════════════════════════════════════════════════════════════════════

test('R91 (statik): statusText & display-time panel evaluasi lewat escapeHtml', () => {
    assert.doesNotMatch(HASIL_HTML, /\$\{statusText\}/,
        '${statusText} mentah bypass pola escape file ini — bungkus ${escapeHtml(String(statusText))}');
    assert.match(HASIL_HTML, /\$\{escapeHtml\(String\(statusText\)\)\}/,
        'kontrak: statusText ter-escape eksplisit (defense-in-depth, komentar prinsip file :1006)');
    // Kartu identitas detail (Batch 15/T29+S104): guard lama memotong dengan
    // marker 'detail-identity-items' (plural) yang 0 HIT di hasil.html —
    // indexOf=-1 → slice(-1) → asersi no-op total. Kini: anchor NYATA kelas
    // singular .detail-identity-item lewat util sliceBlock yang THROW bila
    // marker absen, PLUS asersi positif versi ter-escape wajib ada.
    const detailBlock = sliceBlock(HASIL_HTML, 'detail-identity-item', 'kartu identitas detail hasil');
    assert.ok(detailBlock.length > 200,
        `blok kartu identitas = ${detailBlock.length} char — harus blok render nyata`);
    assert.doesNotMatch(detailBlock, /\$\{startTimeStr\}|\$\{endTimeStr\}/,
        '${startTimeStr}/${endTimeStr} mentah di kartu identitas — bungkus escapeHtml(String(...))');
    assert.match(detailBlock, /\$\{escapeHtml\(String\(startTimeStr\)\)\}/,
        'kontrak positif: Waktu Mulai ter-escape eksplisit — tanpa ini guard bisa vakum tanpa suara');
    assert.match(detailBlock, /\$\{escapeHtml\(String\(endTimeStr\)\)\}/,
        'kontrak positif: Waktu Kumpul ter-escape eksplisit');
});

// ════════════════════════════════════════════════════════════════════════
// R92 — dead attribute data-name (PII di DOM) dihapus
// ════════════════════════════════════════════════════════════════════════

test('R92 (statik): tidak ada lagi setAttribute data-name pada render tabel hasil', () => {
    const n = (HASIL_HTML.match(/setAttribute\(\s*['"]data-name['"]/g) || []).length;
    assert.equal(n, 0,
        `${n}× data-name menempel nama siswa (PII) ke DOM tanpa pembaca dataset.name (pencarian server-side sejak Batch 10) — hapus`);
});

// ════════════════════════════════════════════════════════════════════════
// R93 — gating OTP pre-submit reset_password (paritas register_confirm)
// ════════════════════════════════════════════════════════════════════════

test('R93 (statik): reset_password men-disable submit sampai OTP lengkap (paritas register)', () => {
    assert.match(RESET_PW, /(resetSubmitBtn|submitBtn)\.disabled\s*=/,
        'submit reset_password harus di-disable programatik seperti verifyBtn register_confirm (:252)');
    assert.match(RESET_PW, /\.length\s*<\s*6/,
        'gating berbasis panjang OTP (< 6 digit) wajib ada — salin pola syncHidden/gating register_confirm');
});

test('R93 (kontrak paritas): register_confirm tetap mempertahankan gating-nya', () => {
    // Penjaga arah: perbaikan R93 tidak boleh dilakukan dengan MENURUNKAN
    // register_confirm ke perilaku longgar reset_password.
    assert.match(REG_CONFIRM, /verifyBtn[\s\S]{0,200}disabled|disabled[\s\S]{0,120}verifyBtn/,
        'gating verifyBtn register_confirm adalah referensi pola — jangan hilang');
});

// ════════════════════════════════════════════════════════════════════════
// R94 — copy error Turnstile tanpa referensi posisi layar
// ════════════════════════════════════════════════════════════════════════

test('R94 (guard folder-wide): publik bebas frasa posisional "verifikasi keamanan di atas"', () => {
    const files = fs.readdirSync(PUBLIC).filter((f) => f.endsWith('.html'));
    const hits = [];
    for (const f of files) {
        if (/verifikasi\s+keamanan\s+(yang\s+)?di\s+atas/i.test(read(PUBLIC, f))) hits.push(f);
    }
    assert.deepEqual(hits, [],
        `${hits.join(', ')}: "di atas" rapuh terhadap fold & kosong bagi screen reader — ganti "Harap selesaikan verifikasi keamanan (Cloudflare Turnstile) terlebih dahulu." + pindahkan fokus ke widget saat error`);
});
