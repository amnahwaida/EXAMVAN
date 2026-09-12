/* Contract tests untuk Batch 35 — behavioral LOW halaman admin
 * (review_ui_halaman_web_2026-09-12.md — L42, L43, L44, L45, L71, L76).
 *
 *   L42 — komentar nav.html soal target skip-link "akan menyusul" sudah basi;
 *         seluruh halaman admin kini konsisten memakai #mainContent.
 *   L43 — kurung tutup liar sisa refactor di pengawas_detail.html dihapus.
 *   L44 — alasan disabled tombol kontrol pengawasan kini dirender terlihat
 *         (span #pd-control-reason) + dirujuk aria-describedby, bukan
 *         hanya atribut title hover-only.
 *   L45 — toggle "Terima Otomatis" punya label teks terlihat + wiring
 *         label[for] → input (bukan hanya title/aria-label).
 *   L71 — admin-base.css tidak lagi bergantung token --spacing Tailwind
 *         tanpa fallback.
 *   L76 — tiga modal pengawas_detail dipindah ke akhir <body> sehingga
 *         urutan heading halaman monoton (h1 sebelum heading modal).
 *
 * Run with: node --test static/js/uiux-batch35-admin-low.test.mjs (from webui/)
 */

import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const HERE = path.dirname(fileURLToPath(import.meta.url));
const WEBUI_ROOT = path.join(HERE, '..', '..');
const read = (...p) => fs.readFileSync(path.join(...p), 'utf8');

const NAV = read(WEBUI_ROOT, 'templates', 'admin', 'partials', 'nav.html');
const DETAIL = read(WEBUI_ROOT, 'templates', 'admin', 'pengawas_detail.html');
const ADMIN_BASE = read(WEBUI_ROOT, 'static', 'css', 'admin-base.css');

// ---------------------------------------------------------------------------
// L42 — komentar skip-link nav.html
// ---------------------------------------------------------------------------

test('L42: nav.html tidak lagi mengklaim target skip-link "akan menyusul"', () => {
    assert.doesNotMatch(NAV, /akan\s*\n?\s*menyusul/,
        'komentar basi dihapus — seluruh halaman sudah konsisten');
    assert.match(NAV, /Target #mainContent\s*\n?\s*wajib ada pada <main>/,
        'komentar kini menyatakan kontrak aktual');
});

// ---------------------------------------------------------------------------
// L43 — kurung tutup liar
// ---------------------------------------------------------------------------

test('L43: kurung tutup liar sisa refactor dihapus dari pengawas_detail.html', () => {
    assert.doesNotMatch(DETAIL, /transition: width 1s linear;\n\}\n\}\n/,
        'pola `}\\n}` ganda setelah blok .pd-progress-fill dihapus');
    // CSS dalam <style> tetap seimbang kurawalnya:
    const styleBlock = DETAIL.slice(DETAIL.indexOf('<style>'), DETAIL.indexOf('</style>'));
    const open = (styleBlock.match(/\{/g) || []).length;
    const close = (styleBlock.match(/\}/g) || []).length;
    assert.equal(open, close, 'kurung kurawal <style> seimbang');
});

// ---------------------------------------------------------------------------
// L44 — alasan disabled terlihat + aria-describedby
// ---------------------------------------------------------------------------

test('L44: tombol kontrol disabled merujuk penjelas yang selalu dirender', () => {
    // Kedua tombol (start/stop) memakai aria-describedby, bukan title hover-only:
    assert.equal((DETAIL.match(/disabled aria-describedby="pd-control-reason"/g) || []).length, 2,
        'start & stop sama-sama memakai aria-describedby');
    assert.doesNotMatch(DETAIL, /disabled title="Hanya pembuat/,
        'alasan tidak lagi hanya dikomunikasikan via title');
    // Elemen penjelas dirender tanpa syarat (di luar {{if}}):
    assert.match(DETAIL, /<span id="pd-control-reason"[^>]*>[^<]*Hanya pembuat\/guru[^<]*<\/span>/,
        'span alasan selalu ada di DOM');
});

// ---------------------------------------------------------------------------
// L45 — label toggle terlihat
// ---------------------------------------------------------------------------

test('L45: toggle "Terima Otomatis" punya label teks terlihat + label[for]', () => {
    assert.match(DETAIL, /<label for="autoAcceptToggle"[^>]*class="pd-toggle-switch"/,
        'label terikat eksplisit ke input via for');
    assert.match(DETAIL, /<span class="pd-toggle-label">Terima Otomatis<\/span>/,
        'nama fungsi toggle terlihat oleh pengguna sighted non-hover');
    assert.match(DETAIL, /\.pd-toggle-label\s*\{[^}]*\}/,
        'kelas label terdefinisi di CSS halaman');
});

// ---------------------------------------------------------------------------
// L71 — fallback token --spacing
// ---------------------------------------------------------------------------

test('L71: admin-base.css memakai fallback var(--spacing, 0.25rem)', () => {
    assert.match(ADMIN_BASE, /var\(--spacing,\s*0\.25rem\)/,
        'ketergantungan silang ke output.css diputus dengan fallback');
    assert.doesNotMatch(ADMIN_BASE, /var\(--spacing\)[^,]/,
        'tidak ada lagi pemakaian --spacing tanpa fallback');
});

// ---------------------------------------------------------------------------
// L76 — urutan heading / posisi modal
// ---------------------------------------------------------------------------

test('L76: modal pengawas_detail dirender setelah konten utama (akhir body)', () => {
    const h1 = DETAIL.indexOf('<h1 class="pd-exam-title">');
    const mainEnd = DETAIL.indexOf('</main>');
    const firstModal = DETAIL.indexOf('id="confirmApprovalModal"');
    assert.ok(h1 > -1 && mainEnd > -1 && firstModal > -1, 'anchor lengkap');
    assert.ok(firstModal > mainEnd,
        'modal pindah ke akhir <body> (pola settings.html) — bukan awal <main>');
    assert.ok(h1 < mainEnd, 'h1 judul halaman tetap di dalam <main>');
    // Ketiga modal utuh setelah dipindah:
    for (const id of ['confirmApprovalModal', 'accessLogModal', 'auditLogModal']) {
        assert.match(DETAIL, new RegExp(`id="${id}"`),
            `${id} tetap terdefinisi`);
    }
});
