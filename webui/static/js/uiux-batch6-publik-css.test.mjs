/* Regression contract tests untuk Batch 6 perbaikan UI/UX — sisi publik & CSS.
 * Referensi temuan: review_uiux_webui.md §5.5 RE-REVIEW RONDE 2
 * (ID temuan: S34, S35, S32, R21, S33 + tugas register_confirm skip-link).
 *
 * ID temuan & dampak:
 *   S34 — Open Graph & theme-color = 0 di seluruh template: preview share
 *         WhatsApp/social tanpa judul-gambar; address bar mobile tak ikut
 *         warna brand. Kontrak: shared.html (head publik) memuat og:* +
 *         theme-color, dan nilai theme-color VALIDASILINTAS-FILE sama dengan
 *         --color-bg di theme.css (satu sumber warna brand).
 *   S35 — Feedback loading paginasi/pencarian halaman hasil tidak ada +
 *         transisi state tak diumumkan: tabel tampak mati saat jaringan lambat,
 *         screen reader tak tahu hasil baru telah tampil. Kontrak statik:
 *         aria-live region, guard loading (aria-busy + resultsLoading),
 *         dan scroll-after-render (bukan scroll sebelum fetch).
 *   S32 — prefers-reduced-motion bolong: public-desktop.css tadinya 0
 *         kemunculan; halaman yang hanya memuat layer desktop ikut bolong.
 *   R21 — Konflik grid tab platform: desktop repeat(2,1fr) dead code karena
 *         dikalahkan mobile repeat(3) — intent aktual adalah 3 kolom di semua
 *         ukuran; deklarasi ganda untuk selector sama wajib hilang.
 *   S33 — Token z-index: 4 token kontrak lintas-agen didefinisikan di
 *         theme.css (:root); literal z-index besar di CSS milik publik
 *         diganti var(--z-*).
 *   T6  — register_confirm: satu-satunya sisa skip-link inline; kini memakai
 *         partial public_skip_link seperti halaman publik lain, folder public
 *         bersih dari inline style lama position:absolute;left:-9999px.
 *
 * Run with:  node --test static/js/uiux-batch6-publik-css.test.mjs   (from webui/)
 *
 * Semua test adalah kontrak statik fs-read ala uiux-batch1.test.mjs: membaca
 * file template/CSS ASLI dan memastikan properti kunci perbaikan tidak regresi.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const WEBUI_ROOT = path.join(__dirname, '..', '..');
const read = (rel) => fs.readFileSync(path.join(WEBUI_ROOT, rel), 'utf8');

const SHARED = () => read('templates/public/shared.html');
const THEME = () => read('static/css/theme.css');
const DESKTOP = () => read('static/css/public-desktop.css');
const MOBILE = () => read('static/css/public-mobile.css');
const HASIL_CSS = () => read('static/css/hasil.css');
const HASIL_HTML = () => read('templates/public/hasil.html');

// ---------------------------------------------------------------------------
// S34 — Open Graph + theme-color di head publik (shared.html)
// ---------------------------------------------------------------------------

test('S34a: shared.html memuat og:title, og:description, og:type=website, og:image dengan fallback default', () => {
    const head = SHARED();
    const ogTitle = head.match(/<meta[^>]*property="og:title"[^>]*>/);
    const ogDesc = head.match(/<meta[^>]*property="og:description"[^>]*>/);
    const ogType = head.match(/<meta[^>]*property="og:type"[^>]*>/);
    const ogImage = head.match(/<meta[^>]*property="og:image"[^>]*>/);

    assert.ok(ogTitle, 'meta og:title harus ada di head publik');
    assert.ok(ogDesc, 'meta og:description harus ada di head publik');
    assert.ok(ogType && /content="website"/.test(ogType[0]), 'og:type wajib website');
    assert.ok(ogImage, 'meta og:image harus ada');

    // Override-able ala seo_description existing: var .og_* bila di-set handler,
    // fallback aman ke seo_title/seo_description (diisi middleware/template.go).
    assert.match(ogTitle[0], /\.og_title[\s\S]{0,120}\.seo_title/,
        'og:title harus pakai pola {{if .og_title}}…{{else}}…seo_title…{{end}}');
    assert.match(ogDesc[0], /\.og_description[\s\S]{0,120}\.seo_description/,
        'og:description harus pakai pola fallback seo_description');
    assert.match(ogImage[0], /favicon\.png/, 'og:image fallback ke /static/favicon.png');
});

test('S34b: nilai theme-color == nilai --color-bg di theme.css (validasi lintas-file)', () => {
    const bgVar = THEME().match(/--color-bg:\s*(#[0-9a-fA-F]{3,8})\s*;/);
    assert.ok(bgVar, '--color-bg harus terdefinisi di theme.css');
    const meta = SHARED().match(/<meta[^>]*name="theme-color"[^>]*>/);
    assert.ok(meta, 'meta theme-color harus ada di head publik');
    const content = meta[0].match(/content="([^"]*)"/);
    assert.ok(content, 'theme-color wajib punya content');
    assert.equal(content[1].toLowerCase(), bgVar[1].toLowerCase(),
        'theme-color harus sinkron dengan token --color-bg (satu sumber warna brand)');
});

// ---------------------------------------------------------------------------
// S35 — Feedback loading paginasi/pencarian halaman hasil + aria-live
// ---------------------------------------------------------------------------

test('S35a: hasil.html punya region aria-live="polite" untuk pengumuman jumlah baris', () => {
    const html = HASIL_HTML();
    const live = html.match(/<[^>]*id="resultsLiveRegion"[^>]*>/);
    assert.ok(live, 'elemen #resultsLiveRegion harus ada di markup');
    assert.match(live[0], /aria-live="polite"/, 'region wajib aria-live="polite"');
});

test('S35b: loadResults menerapkan feedback loading (aria-busy + class dim) & melepasnya di finally', () => {
    const html = HASIL_HTML();
    assert.match(html, /function\s+setResultsBusy\(/,
        'harus ada helper setResultsBusy() untuk redup area tabel');
    assert.match(html, /setAttribute\(\s*['"]aria-busy['"]/,
        'area tabel wajib ditandai aria-busy="true" saat fetch berjalan');
    // Guard anti-race/dobel-klik yang sudah ada tetap dipertahankan.
    assert.match(html, /let\s+resultsLoading\s*=\s*false/, 'guard resultsLoading tetap ada');
    assert.match(html, /if\s*\(resultsLoading\)\s*\{\s*resultsRerunPending\s*=\s*true;\s*return;\s*\}/,
        'panggilan saat in-flight tetap di-guard (rerun pending)');
    // Busy ON di awal fetch, OFF di finally (sukses maupun gagal).
    const busyOn = html.indexOf('setResultsBusy(true)');
    const finallyIdx = html.indexOf('} finally {');
    assert.ok(busyOn !== -1 && finallyIdx !== -1 && busyOn < finallyIdx,
        'setResultsBusy(true) dipanggil sebelum blok try/finally');
    const finallyEnd = html.indexOf('}', html.indexOf('resultsLoading = false', finallyIdx));
    const finallyBlock = html.slice(finallyIdx, finallyEnd);
    assert.match(finallyBlock, /setResultsBusy\(false\)/,
        'state busy dilepas di finally agar tidak nyangkut saat error');
    // CSS pendukung dim/spinner kecil ada di hasil.css.
    assert.match(HASIL_CSS(), /#resultsSection\.is-loading/,
        'hasil.css wajib punya rule dim untuk #resultsSection.is-loading');
});

test('S35c: scroll pagination terjadi SETELAH data tampil, bukan sebelum fetch', () => {
    const html = HASIL_HTML();

    // Ambil badan fungsi goToPage (sampai penanda fungsi berikutnya).
    const start = html.indexOf('function goToPage');
    const end = html.indexOf('// ===== Switch Tab', start);
    assert.ok(start !== -1 && end > start, 'fungsi goToPage harus eksis');
    const goToPageSrc = html.slice(start, end);
    assert.doesNotMatch(goToPageSrc, /scrollIntoView/,
        'goToPage tidak boleh lagi scroll sebelum fetch (dead-feel saat jaringan lambat)');
    assert.match(goToPageSrc, /pendingScrollToTable\s*=\s*true/,
        'goToPage menandai intent scroll yang dieksekusi setelah render');

    // Eksekusi scroll berada di jalur sukses loadResults, SETELAH konten tampil.
    const marker = html.indexOf("getElementById('scoresContent').style.display = 'block'");
    assert.ok(marker !== -1, 'jalur render sukses harus eksis');
    const afterRender = html.slice(marker, marker + 900);
    assert.match(afterRender, /scrollIntoView/,
        'scrollIntoView harus dipanggil SETELAH scoresContent ditampilkan');
});

test('S35d: live region diupdate "Menampilkan X–Y dari Z peserta" setiap render tabel selesai', () => {
    const html = HASIL_HTML();
    assert.match(html, /function\s+updateResultsLiveRegion\(/,
        'harus ada fungsi updateResultsLiveRegion()');
    assert.match(html, /Menampilkan '\s*\+\s*start/,
        'pesan live region berformat "Menampilkan X–Y dari Z peserta"');
    // Dipanggil di akhir renderScoresTable (setelah seluruh baris dirender).
    const renderStart = html.indexOf('function renderScoresTable');
    const toggleStart = html.indexOf('// ===== Toggle Detail Scores', renderStart);
    const renderFn = html.slice(renderStart, toggleStart);
    assert.match(renderFn, /updateResultsLiveRegion\(\)/,
        'renderScoresTable wajib menutup dengan pembaruan live region');
});

// ---------------------------------------------------------------------------
// S32 — prefers-reduced-motion lengkap di public-desktop.css
// ---------------------------------------------------------------------------

test('S32: public-desktop.css memuat blok @media (prefers-reduced-motion: reduce)', () => {
    const css = DESKTOP();
    const m = css.match(/@media\s*\(prefers-reduced-motion:\s*reduce\)\s*\{[\s\S]*?\n\}/);
    assert.ok(m, 'blok reduced-motion wajib ada (dulu 0 kemunculan — halaman yang '
        + 'hanya memuat layer desktop bolong)');
    assert.match(m[0], /animation-duration:\s*\.01ms\s*!important|animation:\s*none\s*!important/,
        'animasi desktop wajib dinonaktifkan');
    assert.match(m[0], /transition-duration:\s*\.01ms\s*!important|transition:\s*none\s*!important/,
        'transisi desktop wajib dinonaktifkan');
});

// ---------------------------------------------------------------------------
// R21 — Konflik grid tab platform: satu intent, tanpa dua aturan bertabrakan
// ---------------------------------------------------------------------------

test('R21a: public-desktop.css tidak lagi mendeklarasikan grid-template-columns untuk .tabs-container', () => {
    const css = DESKTOP();
    const rule = css.match(/\.tabs-container\s*\{[^}]*\}/);
    assert.ok(rule, '.tabs-container masih ada di public-desktop.css (rule lain valid)');
    assert.doesNotMatch(rule[0], /grid-template-columns/,
        'repeat(2,1fr) desktop dead code (dikalahkan mobile 3 kolom) — hapus, jangan biarkan dua intent');
});

test('R21b: public-mobile.css tetap 3 kolom untuk .tabs-container (tanpa !important pada kolom)', () => {
    const css = MOBILE();
    const rule = css.match(/\.tabs-container\s*\{[^}]*\}/);
    assert.ok(rule, '.tabs-container ada di public-mobile.css');
    assert.match(rule[0], /grid-template-columns:\s*repeat\(3,\s*minmax\(0,\s*1fr\)\)\s*;/,
        'intent aktual: 3 kolom di SEMUA ukuran (Batch 1 R16)');
    // Aman tanpa !important: public-mobile.css dimuat SETELAH style inline
    // download.html dan tak ada rule lain yang memperebutkan properti ini.
    assert.doesNotMatch(rule[0], /grid-template-columns:[^;]*!important/,
        '!important pada grid-template-columns tidak dibutuhkan lagi (cascade sudah menang)');
});

// ---------------------------------------------------------------------------
// S33 — Token z-index kontrak lintas-agen di theme.css
// ---------------------------------------------------------------------------

test('S33a: theme.css :root mendefinisikan 4 token z kontrak dengan nama & nilai persis', () => {
    const css = THEME();
    const expected = {
        '--z-skip-link': '9998',
        '--z-dropdown': '9999',
        '--z-toast': '10002',
        // Batch 10 (R52): diturunkan dari 99999 — modal onboarding harus
        // berada DI BAWAH toast (10002) agar feedback simpan instansi tetap
        // terlihat; urutan final: dropdown < onboarding < toast.
        '--z-onboarding': '10001'
    };
    for (const [name, value] of Object.entries(expected)) {
        const re = new RegExp(`${name.replace('-', '\\-')}:\\s*${value}\\s*;`);
        assert.match(css, re, `token ${name}: ${value} wajib ada di :root theme.css`);
    }
});

test('S33b: tidak ada lagi literal z-index 9999|10002|99999 di theme.css/hasil.css/public-*.css', () => {
    for (const [rel, css] of [
        ['static/css/theme.css', THEME()],
        ['static/css/hasil.css', HASIL_CSS()],
        ['static/css/public-desktop.css', DESKTOP()],
        ['static/css/public-mobile.css', MOBILE()]
    ]) {
        const hits = [...css.matchAll(/z-index:\s*(9999|10002|99999)\b/g)];
        assert.equal(hits.length, 0,
            `${rel} masih memakai literal ${hits.map((h) => h[1]).join(',')} — ganti var(--z-*)`);
    }
    // Substitusi riil: penerimaan var(--z-toast) di tempat 10002 theme.css:81 lama.
    assert.match(THEME(), /z-index:\s*var\(--z-toast\)/, 'nilai 10002 lama diganti var(--z-toast)');
});

// ---------------------------------------------------------------------------
// Tugas 6 — register_confirm menyatu ke partial public_skip_link
// ---------------------------------------------------------------------------

test('T6a: register_confirm memakai partial public_skip_link seperti halaman publik lain', () => {
    const rc = read('templates/public/register_confirm.html');
    assert.match(rc, /\{\{\s*template\s+"public_skip_link"\s+\.\s*\}\}/,
        'register_confirm wajib memakai partial public_skip_link (satu mekanisme untuk semua halaman publik)');
    assert.match(SHARED(), /\{\{\s*define\s+"public_skip_link"\s*\}\}/,
        'partial tetap terdefinisi di shared.html');
});

test('T6b: folder public bersih dari inline style skip-link lama (position:absolute;left:-9999px)', () => {
    const dir = path.join(WEBUI_ROOT, 'templates', 'public');
    const offenders = [];
    for (const f of fs.readdirSync(dir)) {
        if (!f.endsWith('.html')) continue;
        const src = fs.readFileSync(path.join(dir, f), 'utf8');
        if (src.includes('left:-9999px') || src.includes('left: -9999px')) offenders.push(f);
    }
    assert.deepEqual(offenders, [],
        'inline style lama harus nol — base styling .skip-link kini cukup dari theme.css');
});

test('T6c: base styling .skip-link tersedia di theme.css (pengganti style inline partial)', () => {
    const css = THEME();
    const base = css.match(/\.skip-link\s*\{[^}]*\}/);
    assert.ok(base, 'theme.css wajib punya rule .skip-link dasar');
    assert.match(base[0], /position:\s*absolute/, 'off-screen positioning pindah ke theme.css');
    assert.match(base[0], /var\(--z-skip-link\)/, 'layer skip-link memakai token z kontrak');
    assert.match(css, /\.skip-link:focus/, 'reveal saat focus tetap ada');
});
