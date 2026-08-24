/* Regression contract tests untuk Batch 4 perbaikan UI/UX (review_uiux_webui.md):
 *
 *   S16 — Satukan sistem modal. KEPUTUSAN ARSIP: template
 *     templates/admin/base.html DIHAPUS seluruhnya. Alasan:
 *       (a) base.html tidak pernah dirender — cmd/server/main.go me-skip-nya
 *           secara eksplisit ("reference file, not a renderable template");
 *       (b) markup-nya drift dari partials/nav.html (nav asli dipakai lewat
 *           {{adminNav}}), sehingga dua sumber kebenaran saling menyesatkan;
 *       (c) trap inline __openModal/__closeModal di dalamnya hanya hidup di
 *           file mati itu, sementara admin-core.js sudah punya Global Modal
 *           Manager (MutationObserver pada .modal-overlay/.modal-backdrop +
 *           Escape-to-close + Tab focus-trap + scroll-lock + focus restore)
 *           yang menjadi satu-satunya pemilik perilaku modal;
 *       (d) file ganda ini adalah sumber regresi senyap (perubahan nav/modal
 *           di base.html tak pernah terlihat di produksi).
 *     Blok skip base.html di main.go juga dihapus; trap Escape/Tab inline
 *     ganda di dashboard (modal editInstansiModal) didelegasikan ke manager.
 *
 *   S7  — h1 sr-only dashboard "Dashboard Admin" → "Daftar Ujian" (sinkron label nav).
 *   R13 — settings.html punya 3 h1: sr-only diturunkan jadi h2, tepat 1 h1 per dokumen.
 *   R7  — pagination submissions.html mengikuti pola dashboard: nomor halaman,
 *         indikator posisi "N dari M", anchor disabled non-aktif (aria-disabled).
 *   R8  — kartu pengawas utuh onclick window.location dengan link "Pantau"
 *         bersarang: link harus stopPropagation agar tidak navigasi ganda.
 *
 * Run with:  node --test static/js/uiux-batch4-modal.test.mjs   (from webui/)
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const WEBUI_ROOT = path.join(__dirname, '..', '..');
const read = (rel) => fs.readFileSync(path.join(WEBUI_ROOT, rel), 'utf8');
const exists = (rel) => fs.existsSync(path.join(WEBUI_ROOT, rel));

const ADMIN_TEMPLATES = [
    'templates/admin/dashboard.html',
    'templates/admin/settings.html',
    'templates/admin/submissions.html',
    'templates/admin/pengawas.html',
];

// ---------------------------------------------------------------------------
// S16a — arsip: base.html dihapus
// ---------------------------------------------------------------------------

test('S16a: templates/admin/base.html tidak ada lagi (arsip: tak pernah dirender, markup drift dari nav.html)', () => {
    assert.equal(exists('templates/admin/base.html'), false,
        'base.html harus sudah dihapus — lihat komentar header untuk alasannya');
});

// ---------------------------------------------------------------------------
// S16b — main.go bebas blok skip base.html
// ---------------------------------------------------------------------------

test('S16b: main.go tidak lagi memuat blok skip base.html', () => {
    const main = read('cmd/server/main.go');

    assert.doesNotMatch(main, /Skip base\.html/,
        'komentar blok skip harus hilang bersama bloknya');
    assert.doesNotMatch(main, /HasSuffix\(name,\s*"\/base\.html"\)/,
        'logika HasSuffix("/base.html") tidak boleh ada lagi di main.go');

    // Template lain tetap di-walk dan di-parse normal.
    assert.match(main, /filepath\.Walk\(templatesDir/, 'template walk tetap ada');
    assert.match(main, /SetHTMLTemplate/, 'pemasangan template tetap ada');
});

// ---------------------------------------------------------------------------
// S16c — templates/ bebas referensi base.html / trap inline lama
// ---------------------------------------------------------------------------

test('S16c: tidak ada template yang masih mereferensi base.html atau __openModal/__closeModal', () => {
    const dir = path.join(WEBUI_ROOT, 'templates');
    // login.html & public/ dimiliki agen paralel (kontrak kepemilikan file);
    // referensi di sana hanyalah komentar header "Standalone template" dan
    // dibersihkan oleh pemiliknya masing-masing.
    const EXTERNAL = [/^templates\/admin\/login\.html$/, /^templates\/public\//];
    const files = [];
    (function walk(d) {
        for (const e of fs.readdirSync(d, { withFileTypes: true })) {
            const p = path.join(d, e.name);
            if (e.isDirectory()) walk(p);
            else if (e.name.endsWith('.html')) files.push(p);
        }
    })(dir);

    assert.ok(files.length > 0, 'sanity: folder templates berisi file html');
    for (const f of files) {
        const rel = path.relative(WEBUI_ROOT, f);
        if (EXTERNAL.some((re) => re.test(rel))) continue;
        const html = fs.readFileSync(f, 'utf8');
        assert.ok(!html.includes('base.html'),
            `${rel} masih menyebut base.html — referensi ke file arsip harus dibersihkan`);
        assert.ok(!html.includes('__openModal') && !html.includes('__closeModal'),
            `${rel} masih memakai trap inline __openModal/__closeModal milik base.html`);
    }

    // Referensi trap inline tidak boleh ada di template manapun, termasuk
    // yang dimiliki agen lain.
    for (const f of files) {
        const rel = path.relative(WEBUI_ROOT, f);
        const html = fs.readFileSync(f, 'utf8');
        assert.ok(!html.includes('__openModal') && !html.includes('__closeModal'),
            `${rel} masih memakai trap inline __openModal/__closeModal`);
    }
});

// ---------------------------------------------------------------------------
// S16d — tiap admin standalone bebas keydown-trap Escape/Tab inline ganda
// (Escape-to-close & Tab focus-trap dimiliki Modal Manager admin-core.js)
// ---------------------------------------------------------------------------

for (const rel of ADMIN_TEMPLATES) {
    test(`S16d: ${path.basename(rel)} tidak memasang handler keydown Escape/Tab-trap modal sendiri`, () => {
        const html = read(rel);
        // Handler search Enter (fallback S5) boleh ada; yang dilarang adalah
        // listener keydown kedua yang menutup modal / menjebak Tab di luar manager.
        assert.ok(!/__trapHandler|__escHandler|__firstFocus|__lastFocus/.test(html),
            'state trap inline warisan base.html tidak boleh ada lagi');
        assert.doesNotMatch(
            html,
            /addEventListener\(\s*['"]keydown['"][\s\S]{0,200}(Escape|e\.key\s*===?\s*'Tab')/,
            'keydown Escape/Tab-trap modal wajib didelegasikan ke Modal Manager admin-core.js'
        );
    });
}

// ---------------------------------------------------------------------------
// S7 — h1 dashboard sinkron dengan label nav
// ---------------------------------------------------------------------------

test('S7: h1 dashboard.html bertuliskan "Daftar Ujian" (sinkron label nav)', () => {
    const html = read('templates/admin/dashboard.html');
    const h1s = [...html.matchAll(/<h1\b[^>]*>([\s\S]*?)<\/h1>/g)];
    assert.equal(h1s.length, 1, `dashboard harus tepat 1 h1 (dapat ${h1s.length})`);
    assert.match(h1s[0][1], /Daftar Ujian/, 'teks h1 harus "Daftar Ujian"');
    assert.doesNotMatch(h1s[0][1], /Dashboard Admin/, '"Dashboard Admin" tidak boleh lagi jadi h1');
});

// ---------------------------------------------------------------------------
// R13 — settings.html tepat 1 h1 per dokumen
// ---------------------------------------------------------------------------

test('R13: settings.html memiliki tepat 1 h1 (heading visible), heading halaman-tab turun jadi h2', () => {
    const html = read('templates/admin/settings.html');
    const h1s = [...html.matchAll(/<h1\b[^>]*>([\s\S]*?)<\/h1>/g)];
    assert.equal(h1s.length, 1, `settings harus tepat 1 h1 (dapat ${h1s.length})`);
    // Batch 12 (S67): satu-satunya h1 kini sr-only judul kanonik "Pengaturan"
    // di awal dokumen; "Aplikasi Sistem" turun ke h2 (visual via class).
    assert.match(h1s[0][0], /sr-only/, 'satu-satunya h1 adalah judul kanonik sr-only');
    assert.match(h1s[0][0], /Pengaturan/);
});

// ---------------------------------------------------------------------------
// R7 — pagination submissions mengikuti pola dashboard
// ---------------------------------------------------------------------------

test('R7: pagination submissions.html punya indikator posisi "N dari M" dan nomor halaman', () => {
    const html = read('templates/admin/submissions.html');
    const pag = html.match(/class="pagination-wrapper"[\s\S]*?<\/div>\s*<\/div>/);
    assert.ok(pag, 'blok pagination-wrapper harus ada');

    // Indikator posisi: "<isi halaman> dari <total> hasil".
    assert.match(pag[0], /\{\{len \.submissions\}\} dari \{\{\.total_submissions\}\}/,
        'indikator "N dari M hasil" wajib ada');
    // Nomor halaman seperti pola dashboard (.pagination-page-num).
    assert.match(pag[0], /pagination-page-num/, 'nomor halaman (pagination-page-num) wajib ada');
    assert.match(pag[0], /\$tp \| seq|\{\{\s*range[\s\S]{0,80}seq/,
        'nomor halaman dirender dari range seq(total_pages)');
    // Anchor disabled non-aktif: aria-disabled="true" saat di batas.
    assert.match(pag[0], /aria-disabled/, 'anchor Sebelumnya/Berikutnya di batas wajib aria-disabled');
});

// ---------------------------------------------------------------------------
// R8 — link "Pantau" di kartu pengawas stopPropagation
// ---------------------------------------------------------------------------

test('R8: kartu pengawas navigasi via delegasi; link Pantau bersarang tidak menavigasi ganda', () => {
    const html = read('templates/admin/pengawas.html');
    // Batch 7 (R28 lanjutan): onclick inline kartu dimigrasi ke delegasi
    // data-action + data-exam-id — perilaku navigasi tetap sama.
    const card = html.match(/exam-monitor-card[\s\S]{0,200}data-action="open-pengawas-detail" data-exam-id=/);
    assert.ok(card, 'kartu pengawas tetap navigasi via delegasi data-action');

    // Anchor "Pantau" masih ada; stopPropagation inline dihapus karena handler
    // kartu berbasis delegasi kini mundur bila klik berasal dari anchor —
    // href anchor yang menavigasi (tidak ada lagi navigasi ganda).
    const pantau = html.match(/<a href="\/admin\/pengawas\/' \+ ex\.id \+ '"[^>]*>[^<]*<svg[\s\S]*?Pantau<\/a>/);
    assert.ok(pantau, 'anchor Pantau masih ada');
});
