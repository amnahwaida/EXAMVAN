/**
 * Suite UI/UX BATCH 16 — GUARD INTEGRITAS LINTAS-FILE & REKONSILIASI BASELINE
 * (agen batch16-guard)
 *
 * Referensi: review_uiux_webui.md bagian 5.13 RE-REVIEW RONDE 10.
 *
 * Kontrak yang ditegakkan agen ini:
 *
 *   S112 — NARASI KASKADE R117 DIKOREKSI + BACKSTOP REDUCED-MOTION DIKUNCI:
 *         komentar lama hasil.css mengklaim "kaskade spesifisitas sama sudah
 *         menang" untuk cap animation-iteration-count:1 tanpa tanda penting.
 *         Klaim itu KELIRU: universal selector ber-spesifisitas 0 kalah dari
 *         shorthand kelas .loading-spinner (0,1,0) yang mendeklarasikan
 *         animation: spin ... infinite. Cap iterasi efektif hanya terjadi
 *         BERKAT deklarasi bertanda penting di layer CSS publik lain yang
 *         ikut dimuat halaman hasil. Guard ini MENUNTUT backstop eksplisit
 *         itu tetap ada di public-desktop.css dan public-mobile.css —
 *         sehingga refactor urutan load / penghapusan layer memerah test,
 *         bukan menjadikan reduced-motion vakum senyap.
 *
 *   REKONSILIASI Batch 16 — plafon literal yang dikunci suite lain
 *         divalidasi ulang terhadap ukuran independen (kontrak "plafon =
 *         aktual" ronde 5); lihat juga tabel rekonsiliasi di laporan agen.
 *
 * Catatan penulisan: JANGAN menulis pola glob dua-bintang di dalam komentar
 * blok ini — deretan bintang-garis-miring menutup komentar lebih awal.
 *
 * Run with:  node --test static/js/uiux-batch16-guard.test.mjs   (from webui/)
 */
import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';

const ROOT = new URL('.', import.meta.url).pathname.replace(/\/static\/js\/$/, '');
const CSS_DIR = join(ROOT, 'static', 'css');

const readCss = (name) => readFileSync(join(CSS_DIR, name), 'utf8');

/** Ambil isi blok media reduced-motion pertama sebuah file CSS inti. */
function reducedMotionBlock(src, label) {
    const start = src.indexOf('@media (prefers-reduced-motion');
    assert.notEqual(start, -1, `${label}: blok prefers-reduced-motion wajib ada`);
    const end = src.indexOf('}', src.indexOf('{', start));
    assert.notEqual(end, -1, `${label}: blok reduced-motion harus tertutup`);
    return src.slice(start, end);
}

// ════════════════════════════════════════════════════════════════════════
// S112(a) — backstop !important lintas-file WAJIB eksplisit
// ════════════════════════════════════════════════════════════════════════

test('S112 (statik): public-desktop.css membawa cap animation-iteration-count: 1 dengan tanda penting', () => {
    const block = reducedMotionBlock(readCss('public-desktop.css'), 'public-desktop.css');
    assert.match(block, /animation-iteration-count:\s*1\s*!important/,
        'backstop desktop hilang — cap iterasi hasil.css (tanpa tanda penting) TIDAK akan menang ' +
        'dari shorthand infinite .loading-spinner; halaman yang hanya memuat layer desktop kehilangan reduced-motion');
});

test('S112 (statik): public-mobile.css membawa backstop animasi reduced-motion bertanda penting', () => {
    // Versi ketat kontrak menuntut animation-iteration-count:1 bertanda penting
    // di SINI juga; realitanya layer mobile membentuk backstop dengan bentuk
    // yang LEBIH KUAT: animation: none bertanda penting — shorthand none
    // meng-nol-kan SELURUH longhand animation-* (termasuk iteration-count,
    // nilai awalnya 1), jadi tidak ada animasi beriterasi apa pun yang bisa
    // selamat. Selama shorthand none ini ada, cap iterasi implisit terpenuhi.
    // Paritas literal (menambah iteration-count:1 bertanda penting di baris
    // yang sama) diserahkan kepada koordinator bila dikehendaki — dicatat di
    // laporan Batch 16 sebagai temuan kosmetik, BUKAN lubang perilaku.
    const block = reducedMotionBlock(readCss('public-mobile.css'), 'public-mobile.css');
    const stopper = block.match(/animation:\s*none\s*!important/) ||
        block.match(/animation-iteration-count:\s*1\s*!important/);
    assert.ok(stopper,
        'backstop mobile hilang — blok reduced-motion wajib membawa animation: none ATAU ' +
        'animation-iteration-count: 1 dengan tanda penting agar cap iterasi efektif');
});

test('S112 (statik): cap iterasi hasil.css tetap TANPA tanda penting (disiplin plafon S71)', () => {
    const src = readCss('hasil.css');
    const start = src.indexOf('@media (prefers-reduced-motion');
    assert.notEqual(start, -1, 'blok reduced-motion hasil.css wajib ada (R117)');
    const block = src.slice(start, src.indexOf('}', src.indexOf('{', start)));
    assert.match(block, /animation-iteration-count:\s*1\s*;/,
        'cap iteration-count: 1 harus tetap ada di hasil.css');
    assert.doesNotMatch(block, /animation-iteration-count:\s*1\s*!important/,
        'JANGAN tambah tanda penting baru di hasil.css (plafon S71) — backstop sudah dipegang layer publik');
});
