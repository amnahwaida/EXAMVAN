/**
 * ══════════════════════════════════════════════════════════════════════════
 * Suite UI/UX BATCH 19 — HIGIENE ASET STATIS & META HEAD (S117, R146, R147;
 * dieksekusi koordinator)
 * ══════════════════════════════════════════════════════════════════════════
 *
 * Referensi: review_uiux_webui.md bagian 5.16 RE-REVIEW RONDE 13.
 *
 *   S117 — FILE MATI + DEFINISI GANDA TOAST CONTAINER:
 *
 *     (a) `static/css/tailwind/admin-tailwind.css` (67KB) TIDAK PERNAH
 *         dimuat oleh halaman mana pun (grep repo-wide: nol referensi dari
 *         template/Go/CSS/JS sejak di-commit) namun tetap:
 *           - bisa diunduh publik via /static/css/tailwind/admin-tailwind.css,
 *           - membebani kebijakan guard (baseline f87171 Batch 17, pengecualian
 *             z-index) untuk file yang tak berdampak runtime.
 *         Kontrak: file TERHAPUS dari folder statis. Bila suatu saat varian
 *         build ini dibutuhkan lagi, hidupkan lewat pipeline build — bukan
 *         sebagai artefak manual di dalam web root publik.
 *
 *     (b) `.toast-container` terdefinisi DUA kali lintas-file dengan z-index
 *         BERBEDA: `tailwind/output.css` (:~1106, z-index: 9999 — file ini
 *         DIMUAT produksi) dan `admin-base.css` (:755, var(--z-toast)=10002).
 *         Saat ini aman HANYA karena urutan muat kebetulan benar (output.css
 *         sebelum admin-base.css) — persis pola rapuh R117 pada komponen
 *         layer tertinggi. Kontrak: blok `.toast-container` di output.css
 *         TIDAK lagi membawa properti z-index (posisi/layout tetap di sana);
 *         lapisan toast dikunci SATU tempat di admin-base.css via
 *         var(--z-toast).
 *
 *   R146 — TARGET="_BLANK" TANPA rel="noopener": anchor unduh aplikasi
 *         (settings.html) dan Lihat PDF (dashboard.html) membuka tab baru
 *         tanpa isolasi window.opener. Keduanya same-origin sehingga risiko
 *         tab-nabbing minim, tapi noopener adalah konvensi defensif standar.
 *         Kontrak: SETIAP anchor ber-target="_blank" membawa rel yang
 *         mengandung "noopener".
 *
 *   R147 — REGISTER_CONFIRM TANPA NOINDEX: halaman OTP registrasi memiliki
 *         URL `/register/confirm?username=…` — jika tautannya bocor ke
 *         halaman terindeks, pola URL + nama akun dapat muncul di hasil
 *         pencarian. Saudaranya (reset_password.html) sudah benar memakai
 *         `noindex, nofollow`. Kontrak: register_confirm membawa meta robots
 *         `noindex, nofollow` (paritas reset_password).
 */
import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, existsSync } from 'node:fs';
import { join } from 'node:path';

const ROOT = new URL('.', import.meta.url).pathname.replace(/\/static\/js\/$/, '');
const read = (p) => readFileSync(join(ROOT, p), 'utf8');

test('S117a: file mati admin-tailwind.css sudah dihapus dari folder statis publik', () => {
    const p = join(ROOT, 'static/css/tailwind/admin-tailwind.css');
    assert.equal(existsSync(p), false,
        'admin-tailwind.css tidak pernah dimuat siapa pun (nol referensi) - ' +
        'artefak manual di web root publik wajib dihapus, bukan dipelihara');
});

test('S117b: .toast-container di output.css tidak lagi membawa z-index (lapisan dikunci di admin-base)', () => {
    const out = read('static/css/tailwind/output.css');
    const start = out.indexOf('.toast-container {');
    assert.ok(start > -1, '.toast-container tetap ada di output.css (posisi/layout)');
    const end = out.indexOf('}', start);
    const rule = out.slice(start, end);
    assert.doesNotMatch(rule, /z-index/,
        'z-index toast TIDAK boleh didefinisikan dua tempat - kunci satu tempat di admin-base via var(--z-toast)');
    // Posisi/layout tetap dipertahankan agar tampilan tak berubah:
    assert.match(rule, /position:\s*fixed/, 'posisi fixed tetap');
    assert.match(rule, /bottom:/, 'anchor bawah tetap');
    // Kontrak sisi admin-base (sumber tunggal lapisan):
    const base = read('static/css/admin-base.css');
    const bs = base.indexOf('.toast-container {');
    const be = base.indexOf('}', bs);
    assert.match(base.slice(bs, be), /z-index:\s*var\(--z-toast\)/,
        'admin-base tetap menjadi SATU tempat definisi lapisan toast');
});

test('R146: seluruh target="_blank" membawa rel noopener', () => {
    for (const f of ['templates/admin/settings.html', 'templates/admin/dashboard.html']) {
        const src = read(f);
        const blanks = src.match(/<a [^>]*target="_blank"[^>]*>/g) || [];
        assert.ok(blanks.length >= 1, `${f}: prasyarat minimal satu anchor _blank`);
        for (const tag of blanks) {
            assert.match(tag, /rel="[^"]*noopener[^"]*"/,
                `${f}: ${tag.slice(0, 60)}… wajib rel="noopener"`);
        }
    }
});

test('R147: register_confirm membawa meta robots noindex,nofollow (paritas reset_password)', () => {
    const src = read('templates/public/register_confirm.html');
    assert.match(src, /<meta\s+name="robots"\s+content="noindex,\s*nofollow">/,
        'halaman OTP ber-URL ?username= wajib noindex,nofollow (jangan terindeks mesin pencari)');
});
