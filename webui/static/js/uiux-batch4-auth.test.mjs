/* Regression contract tests untuk Batch 4 perbaikan UI/UX (bagian auth/publik):
 * temuan S6, R9, R11, R14, S17/R14, S15(R15) di review_uiux_webui.md root repo.
 *
 * Keputusan arsitektur Batch 4:
 *   - S6 (FINAL): unifikasi pola OTP ke pola 6-kotak register_confirm.html
 *     (paste multi-digit, navigasi panah/backspace antar kotak, aria-label per
 *     digit, autocomplete="one-time-code" pada kotak pertama). reset_password
 *     yang tadinya satu input biasa di-port ke pola ini tanpa mengubah
 *     endpoint/logika server (hidden input name="otp_code" tetap dikirim).
 *   - S17/R14: aplikasi resmi DARK-BY-DESIGN sejak Batch 4 (palet light
 *     dihapus dari theme.css), sehingga data-theme="dark" pada widget
 *     cf-turnstile di admin/login.html KONSISTEN dan benar — cukup didokumen-
 *     tasikan lewat komentar markup agar tidak dianggap hard-code salah.
 *
 * Run with:  node --test static/js/uiux-batch4-auth.test.mjs   (from webui/)
 *
 * Semua test adalah kontrak statik: membaca file template ASLI dan memastikan
 * properti kunci perbaikan tidak pernah regresi.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const WEBUI_ROOT = path.join(__dirname, '..', '..');
const read = (rel) => fs.readFileSync(path.join(WEBUI_ROOT, rel), 'utf8');

// ---------------------------------------------------------------------------
// S6 — reset_password.html memakai pola OTP 6-kotak (paritas register_confirm)
// ---------------------------------------------------------------------------

test('S6a: reset_password punya tepat 6 input .otp-digit dengan inputmode numeric + aria-label per digit', () => {
    const html = read('templates/public/reset_password.html');

    const digits = [...html.matchAll(/<input[^>]*class="otp-digit"[^>]*>/g)];
    assert.equal(digits.length, 6, `harus ada 6 kotak digit OTP (dapat ${digits.length})`);
    for (const [i, m] of digits.entries()) {
        assert.match(m[0], /inputmode="numeric"/, `kotak digit ${i + 1} wajib inputmode="numeric"`);
        if (i === 0) {
            // T30 (Batch 16): kotak PERTAMA bebas maxlength — isian utuh OS
            // tidak boleh terpotong; distribusi multi-karakter menyalurkannya.
            assert.doesNotMatch(m[0], /maxlength="1"/, `kotak digit 1 tidak boleh maxlength="1" (T30)`);
        } else {
            assert.match(m[0], /maxlength="1"/, `kotak digit ${i + 1} wajib maxlength="1"`);
        }
        assert.match(m[0], new RegExp(`aria-label="Digit ${i + 1}[^"]*"`),
            `kotak digit ${i + 1} wajib punya aria-label per digit`);
    }
});

test('S6b: autocomplete one-time-code tepat satu di KOTAK DIGIT PERTAMA visible reset_password (diubah Batch 16/T30)', () => {
    // Diperbarui Batch 16: Batch 15 memindah atribut ke input HIDDEN #otp_code,
    // tetapi mesin autofill OS melewatkan field non-visible (regresi T30).
    // Lokasi yang benar: kotak digit pertama yang VISIBLE dan fokusable,
    // tanpa maxlength pemotong; hidden #otp_code wajib bersih.
    const html = read('templates/public/reset_password.html');
    const first = html.match(/<input[^>]*class="otp-digit"[^>]*>/);
    assert.ok(first, 'kotak digit pertama harus ada');
    assert.match(first[0], /autocomplete="one-time-code"/,
        'kotak digit pertama VISIBLE wajib membawa one-time-code (autofill OS)');
    assert.doesNotMatch(first[0], /maxlength="1"/,
        'kotak digit pertama tanpa maxlength pemotong isian utuh OS (T30)');
    // Hitung hanya pada markup nyata — komentar HTML tidak dihitung.
    const markup = html.replace(/<!--[\s\S]*?-->/g, '');
    const occ = markup.match(/autocomplete="one-time-code"/g) || [];
    assert.equal(occ.length, 1, 'tepat satu lokasi membawa one-time-code');
});

test('S6c: nilai gabungan tetap dikirim sebagai hidden input name="otp_code" (logika server tak berubah)', () => {
    const html = read('templates/public/reset_password.html');
    assert.match(html, /<input[^>]*type="hidden"[^>]*id="otp_code"[^>]*name="otp_code"/,
        'harus ada hidden input #otp_code name="otp_code" penerima nilai gabungan');
    // Form action endpoint TIDAK boleh berubah.
    assert.match(html, /action="\/reset-password/, 'endpoint form tidak boleh berubah');
});

test('S6d: reset_password punya handler paste multi-digit (paritas register_confirm)', () => {
    const rp = read('templates/public/reset_password.html');
    const rc = read('templates/public/register_confirm.html');

    for (const [name, html] of [['reset_password', rp], ['register_confirm', rc]]) {
        assert.match(html, /addEventListener\(\s*'paste'/,
            `${name} harus menangani event paste`);
        assert.match(html, /clipboardData/,
            `${name} harus membaca clipboardData saat paste`);
    }
    // Paste multi-digit mengisi SEMUA kotak (loop sampai batas 6 digit).
    assert.match(rp, /Math\.min\(paste\.length,\s*6\)/,
        'reset_password harus loop mengisi hingga 6 digit dari paste');
});

test('S6e: reset_password punya navigasi panah & backspace antar kotak (paritas register_confirm)', () => {
    const rp = read('templates/public/reset_password.html');

    assert.match(rp, /addEventListener\(\s*'keydown'/, 'harus ada keydown handler');
    assert.match(rp, /ArrowLeft[\s\S]{0,200}focus\(\)/, 'panah kiri harus memindah fokus ke kotak sebelumnya');
    assert.match(rp, /ArrowRight[\s\S]{0,200}focus\(\)/, 'panah kanan harus memindah fokus ke kotak berikutnya');
    assert.match(rp, /Backspace[\s\S]{0,300}\w+\[\s*idx\s*-\s*1\s*\]/,
        'backspace pada kotak kosong harus mundur ke kotak sebelumnya');
});

test('S6f: reset_password menyinkronkan nilai gabungan 6 kotak ke hidden otp_code', () => {
    const rp = read('templates/public/reset_password.html');
    assert.match(rp, /querySelectorAll\(\s*['"]\.otp-digit['"]/,
        'JS harus mengambil semua kotak .otp-digit');
    assert.match(rp, /\w+\.value\s*=\s*code\b/,
        'nilai gabungan harus ditulis ke hidden #otp_code');
    // CSS lokal untuk visual kotak OTP agar konsisten dengan register_confirm.
    assert.match(rp, /\.otp-digit\s*\{/, 'harus ada styling .otp-digit lokal');
    assert.match(rp, /\.otp-input-wrapper\s*\{/, 'harus ada wrapper flex .otp-input-wrapper');
});

// ---------------------------------------------------------------------------
// R9 — username register: hint charset + toast sekali saat sanitasi menghapus
// ---------------------------------------------------------------------------

test('R9a: register.html menampilkan aturan charset username pada hint field', () => {
    const html = read('templates/public/register.html');
    const hint = html.match(/<span[^>]*form-hint-subtle[^>]*id="usernameHint"[^>]*>([\s\S]*?)<\/span>/)
        || html.match(/<span[^>]*id="usernameHint"[^>]*>([\s\S]*?)<\/span>/);
    assert.ok(hint, 'harus ada <span id="usernameHint"> di bawah input username');
    // Revisi Batch 11/R65: kini eksplisit "Huruf kecil" (sanitasi JS
    // melakukan toLowerCase diam-diam) — intent R9 tetap: hint menyebut
    // aturan huruf.
    assert.match(hint[1], /[Hh]uruf( kecil)?|[Hh]anya huruf/i, 'hint harus menyebut aturan huruf');
    assert.match(hint[1], /angka/, 'hint harus menyebut angka');
    assert.match(hint[1], /titik|garis bawah|strip|hubung/i, 'hint harus menyebut karakter khusus yang diizinkan');
    assert.match(hint[1], /[Mm]in\.?\s*3/, 'hint harus menyebut panjang minimum 3 karakter');
});

test('R9b: sanitasi username memunculkan toast SEKALI via flag (bukan tiap ketikan)', () => {
    const html = read('templates/public/register.html');

    // Flag "sekali-per-sesi-ketik" eksplisit.
    assert.match(html, /\w*[Ss]anitizeToastShown\w*\s*=\s*false/,
        'harus ada flag boolean awal false untuk toast sekali');
    assert.match(html, /usernameSanitizeToastShown\s*=\s*true/,
        'flag harus di-set true setelah toast pertama');
    // Toast hanya dipicu bila sanitasi BENAR-BENAR menghapus karakter.
    assert.match(html, /cleaned\s*!==?\s*this\.value[\s\S]{0,400}showUsernameCharsetToast\(\)/,
        'toast hanya boleh tampil bila cleaned !== nilai asli (ada karakter terhapus)');
    // Implementasi toast ada.
    assert.match(html, /function showUsernameCharsetToast\(\)/,
        'harus ada fungsi showUsernameCharsetToast()');
    assert.match(html, /role="status"|setAttribute\(\s*['"]role['"],\s*['"]status['"]\s*\)/,
        'toast harus role="status" agar terumumkan screen reader');
});

// ---------------------------------------------------------------------------
// R11 — fingerprintjs.min.js dimuat non-blocking (defer)
// ---------------------------------------------------------------------------

test('R11: login.html memuat fingerprintjs.min.js dengan atribut defer', () => {
    const html = read('templates/admin/login.html');
    const tag = html.match(/<script[^>]*fingerprintjs\.min\.js[^>]*>/);
    assert.ok(tag, 'tag script fingerprintjs.min.js harus ada');
    assert.match(tag[0], /\bdefer\b/, 'script fingerprintjs.min.js wajib defer (jangan blok parser)');
});

// ---------------------------------------------------------------------------
// R14/S17 — Turnstile data-theme dark didokumentasikan (dark-by-design)
// ---------------------------------------------------------------------------

test('R14: komentar keputusan dark-by-design ada dekat cf-turnstile di login.html', () => {
    const html = read('templates/admin/login.html');
    const region = html.match(/<!--[\s\S]{0,600}?-->[\s\S]{0,200}?class="cf-turnstile"/)
        || (() => {
            const i = html.indexOf('cf-turnstile');
            return { 0: html.slice(Math.max(0, i - 700), i), index: Math.max(0, i - 700) };
        })();
    assert.match(region[0], /dark-by-design/,
        'komentar menjelaskan data-theme="dark" (dark-by-design sejak Batch 4/S17) harus dekat markup cf-turnstile');
    assert.match(region[0], /Batch 4|S17/, 'komentar harus menyebut asal keputusan (Batch 4/S17)');
});

// ---------------------------------------------------------------------------
// R15 — kebersihan markup publik
// ---------------------------------------------------------------------------

test('R15a: shared.html tidak punya <script> setelah </html>', () => {
    const html = read('templates/public/shared.html');
    const closeHtml = html.lastIndexOf('</html>');
    assert.ok(closeHtml !== -1, '</html> harus ada');
    const lastScript = html.lastIndexOf('<script');
    assert.ok(lastScript !== -1 && lastScript < closeHtml,
        `tag <script> terakhir (index ${lastScript}) harus berada sebelum </html> (index ${closeHtml})`);
});

test('R15b: shared.html melink favicon tepat satu kali', () => {
    const html = read('templates/public/shared.html');
    const count = [...html.matchAll(/<link[^>]*rel="icon"[^>]*>/g)].length;
    assert.equal(count, 1, `favicon link harus tepat 1 (dapat ${count})`);
});

test('R15c: register_confirm.html punya skip-link seperti halaman publik lain', () => {
    // Batch 6 (tugas 6): anchor literal + salinan lokal <style> .skip-link
    // di register_confirm DIHAPUS — satu-satunya mekanisme kini partial
    // public_skip_link di shared.html, dengan base styling di theme.css.
    // Assertion lama yang mewajibkan anchor literal identik antar dua file
    // disesuaikan ke kontrak partial (perubahan minimal, tujuan R15 sama:
    // skip-link hadir dan seragam).
    const rc = read('templates/public/register_confirm.html');
    assert.match(rc, /\{\{\s*template\s+"public_skip_link"\s+\.\s*\}\}/,
        'register_confirm wajib memakai partial public_skip_link seperti halaman publik lain');

    const shared = read('templates/public/shared.html');
    assert.match(shared, /\{\{\s*define\s+"public_skip_link"\s*\}\}/,
        'shared.html tetap mendefinisikan partial tunggal tersebut');
});
