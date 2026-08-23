/* Guard Batch 8 — token triplet hitam/putih (--rgb-black & --rgb-white).
 *
 * Latar belakang & dampak bisnis:
 *   Re-review UI/UX S15 fase 2 (Batch 8) menemukan sisa ±70 literal
 *   rgba(0,0,0,x) / rgba(255,255,255,x) yang tersebar di CSS inti
 *   (admin-base, hasil, publik desktop/mobile, theme). Literal seperti ini
 *   menyulitkan penyetelan konsistensi visual: mengubah intensitas overlay,
 *   border kaca, atau shadow global berarti berburu puluhan angka manual di
 *   lima file. Batch 8 menambahkan triplet --rgb-black/--rgb-white di
 *   theme.css (kontrak lintas-agen, dipakai juga agen publik) lalu memigrasi
 *   SEMUA substitusi nilai-persis menjadi rgba(var(--rgb-black), α) /
 *   rgba(var(--rgb-white), α). Migrasi ini netral-visual: tidak ada nilai α
 *   maupun urutan komponen yang berubah.
 *
 * Test ini mengunci tiga hal:
 *   1. Token triplet eksis dengan nilai persis kontrak.
 *   2. Tidak ada lagi literal rgba(0,0,0,...)/rgba(255,255,255,...) di kelima
 *      file milik Batch 8 — KECUALI whitelist eksplisit di bawah (definisi
 *      token :root theme.css memang source-of-truth literal, dan dua baris
 *      scrollbar di hasil.css yang alphas-nya dibaca parser test Batch 3).
 *   3. Sanity: setiap file benar-benar memakai token baru (>0 substitusi),
 *      supaya migrasi tidak bisa "lolos" dengan sekadar menghapus pemakaian.
 *
 * Run with:  node --test static/js/uiux-batch8-tokens-css.test.mjs   (from webui/)
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const cssPath = (f) => path.join(__dirname, '..', 'css', f);
const readCss = (f) => fs.readFileSync(cssPath(f), 'utf8');

const OWN_FILES = [
    'theme.css',
    'admin-base.css',
    'hasil.css',
    'public-desktop.css',
    'public-mobile.css',
];

// Pola literal rgba hitam/putih MURNI (ketiga komponen sama) — spasi fleksibel.
// Hanya bentuk persis ini yang boleh disubstitusi var(--rgb-black)/white;
// rgba campuran (mis. rgba(11, 15, 25, .9)) memang bukan ranah token ini.
const BLACK_RE = /rgba\(\s*0\s*,\s*0\s*,\s*0\s*,/g;
const WHITE_RE = /rgba\(\s*255\s*,\s*255\s*,\s*255\s*,/g;

/** Baris whitelist theme.css: DEFINISI token :root yang nilainya literal
 *  secara desain (source-of-truth warna surface/glass). Substitusi PEMAKAIAN
 *  token ini tetap wajib migrasi; deklarasinya sendiri tidak boleh diubah. */
const THEME_TOKEN_DEF_RE =
    /^\s*--(color-surface|color-surface-hover|color-glass-border|glass-bg-strong)\s*:/;

/** Baris whitelist hasil.css: scrollbar-color & rule ::webkit-scrollbar-thumb.
 *  ALASAN TETAP LITERAL: uiux-batch3-hasil-css.test.mjs menguras alpha thumb
 *  scrollbar dengan regex digit literal (rgba(<digit>,<digit>,<digit>,α));
 *  var() membuat alpha tak terbaca dan test Batch 3 merah. Nilai visual tidak
 *  berubah — hanya bentuknya yang belum bisa dimigrasi sampai parser itu
 *  diperbarui (di luar kepemilikan Batch 8). Pola ketiga mencakup baris
 *  deklarasi background di dalam rule ::-webkit-scrollbar-thumb multi-baris
 *  (alpha 0.3 persis — deklarasi thumb itu sendiri, bukan substitusi lain). */
const HASIL_SCROLLBAR_RE =
    /scrollbar-color\s*:|scrollbar-thumb|^background:\s*rgba\(255,\s*255,\s*255,\s*0\.3\)/;

function stripWhitelisted(file, src) {
    return src.split('\n').filter((line) => {
        if (file === 'theme.css' && THEME_TOKEN_DEF_RE.test(line)) return false;
        if (file === 'hasil.css' && HASIL_SCROLLBAR_RE.test(line)) return false;
        return true;
    }).join('\n');
}

test('Batch 8: token --rgb-black & --rgb-white eksis di theme.css dengan nilai persis kontrak lintas-agen', () => {
    const src = readCss('theme.css');
    assert.match(src, /--rgb-black:\s*0,\s*0,\s*0\s*;/,
        '--rgb-black harus bernilai persis "0, 0, 0" (kontrak lintas-agen)');
    assert.match(src, /--rgb-white:\s*255,\s*255,\s*255\s*;/,
        '--rgb-white harus bernilai persis "255, 255, 255" (kontrak lintas-agen)');
});

for (const f of OWN_FILES) {
    test(`Batch 8: ${f} — tidak ada lagi literal rgba(0,0,0,*)/rgba(255,255,255,*) di luar whitelist`, () => {
        const stripped = stripWhitelisted(f, readCss(f));
        const blacks = (stripped.match(BLACK_RE) || []).length;
        const whites = (stripped.match(WHITE_RE) || []).length;
        assert.equal(blacks + whites, 0,
            `${f} masih punya ${blacks} rgba-hitam + ${whites} rgba-putih literal — ` +
            'substitusi pakai rgba(var(--rgb-black), α) / rgba(var(--rgb-white), α); ' +
            'bila benar-benar tak bisa dimigrasi, daftarkan di whitelist test ini DENGAN alasannya');
    });

    test(`Batch 8: ${f} — token rgb-black/rgb-white benar-benar dipakai (sanity > 0)`, () => {
        const n = (readCss(f).match(/var\(--rgb-(?:black|white)\)/g) || []).length;
        assert.ok(n > 0,
            `${f} tidak memakai var(--rgb-black)/var(--rgb-white) sama sekali (${n}) — ` +
            'migrasi Batch 8 belum terjadi di file ini');
    });
}

test('Batch 8: whitelist theme.css tetap utuh — 4 definisi token :root masih literal (tidak boleh "dimigrasi" jadi self-referential)', () => {
    const defs = readCss('theme.css').split('\n')
        .filter((l) => THEME_TOKEN_DEF_RE.test(l));
    assert.equal(defs.length, 4,
        'whitelist definisi token (:root) harus tepat 4 baris — ' +
        'token source-of-truth memang literal, jangan diubah memakai dirinya sendiri');
});
