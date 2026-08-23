/* Regression contract tests untuk Batch 3 perbaikan UI/UX (bagian CSS & copy).
 * Referensi temuan: review_uiux_webui.md (ID: S10, S19/T10b kontrak CSS, S4 publik).
 *
 * Run with:  node --test static/js/uiux-batch3-a11y-css.test.mjs   (from webui/)
 *
 * Fokus tiga kontrak:
 *   1. S10 — target sentuh kontrol inti ≥44×44px. Ukuran efektif dihitung
 *      dengan asumsi box-sizing:border-box (dideklarasikan global di kedua
 *      stylesheet), ATAU lewat hit-area pseudo-element ber-inset negatif
 *      yang dihitung: dimensi_visual_floor + 2×|inset| ≥ 44.
 *   2. Kontrak CSS helper validasi setFieldError() (.input-error/.field-error-text)
 *      — persis seperti disepakati antar-agen (admin-core.js memakai class ini).
 *   3. S4 (bagian publik) — istilah unduh disatukan menjadi "Unduh Aplikasi";
 *      "Download App" / "Unduh Client Ujian" tidak boleh muncul lagi.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const WEBUI_ROOT = path.join(__dirname, '..', '..');
const read = (rel) => fs.readFileSync(path.join(WEBUI_ROOT, rel), 'utf8');
/** Baca CSS dengan komentar dibuang dulu — komentar boleh memuat ';'/','
 * tanpa mengelabui parser selektor/deklarasi di bawah. */
const readCss = (rel) => read(rel).replace(/\/\*[\s\S]*?\*\//g, '');

// ---------------------------------------------------------------------------
// Helper parse CSS (pola cssRule uiux-batch2.test.mjs, diperluas)
// ---------------------------------------------------------------------------

/** Isi blok rule PERTAMA yang cocok (tanpa konteks media query). */
function cssRule(css, selectorRegex) {
    const m = css.match(new RegExp(selectorRegex + '\\s*\\{([^}]*)\\}'));
    return m ? m[1] : null;
}

/** Ekstrak isi SEMUA blok `@media <kondisi> { ... }` (kurung seimbang). */
function mediaBlocks(css, condRegex) {
    const blocks = [];
    const re = new RegExp('@media[^{]*' + condRegex + '[^{]*\\{', 'g');
    let m;
    while ((m = re.exec(css)) !== null) {
        let depth = 1;
        let i = m.index + m[0].length;
        while (i < css.length && depth > 0) {
            if (css[i] === '{') depth++;
            else if (css[i] === '}') depth--;
            i++;
        }
        blocks.push(css.slice(m.index + m[0].length, i - 1));
    }
    return blocks;
}

/** Gabungan isi semua blok media query mobile (max-width: 768px). */
function mobileCss(css) {
    return mediaBlocks(css, 'max-width:\\s*768px').join('\n');
}

/**
 * Semua rule yang SELEKTOR TERAKHIRNYA cocok persis (mendukung daftar
 * selektor ber-koma). Menghindari false-match ".toast:hover .toast-close"
 * saat yang dicari ".toast-close".
 */
function rulesFor(css, lastSelectorRegex) {
    const out = [];
    const re = /(^|[{};\n])\s*([^{}]*?)\s*\{/g;
    let m;
    while ((m = re.exec(css)) !== null) {
        const last = m[2].split(',').map((s) => s.trim()).pop();
        if (!new RegExp('^' + lastSelectorRegex + '$').test(last)) continue;
        let depth = 1;
        let i = re.lastIndex;
        while (i < css.length && depth > 0) {
            if (css[i] === '{') depth++;
            else if (css[i] === '}') depth--;
            i++;
        }
        out.push({ selector: last, body: css.slice(re.lastIndex, i - 1) });
    }
    return out;
}

function decl(body, prop) {
    const m = body.match(new RegExp('(?:^|;)\\s*' + prop + '\\s*:\\s*([^;]+)', 'i'));
    return m ? m[1].trim() : null;
}

function px(v) {
    if (v == null) return null;
    const m = String(v).match(/(-?\d+(?:\.\d+)?)px/);
    return m ? parseFloat(m[1]) : null;
}

/**
 * Verifikasi kontrak hit-area S10 untuk satu selector ikon.
 * Visual TIDAK boleh berubah (width/height/min-* tetap), dan zona klik
 * diperluas via pseudo-element ber-inset negatif hingga ≥44px per sumbu
 * (box-sizing:border-box → floor = width/height eksplisit atau min-*).
 */
function assertHitArea44(css, selector, expectedVisual) {
    const label = `${selector} (${expectedVisual.label})`;
    const baseRules = rulesFor(css, selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'));
    assert.ok(baseRules.length > 0, `rule dasar ${label} harus ada`);
    const base = baseRules[0].body;

    // 1. Visual dipertahankan — teknik yang dipilih adalah hit-area, bukan
    //    membesarkan tampilan (keputusan didokumentasikan di komentar CSS).
    for (const [prop, want] of Object.entries(expectedVisual.props)) {
        assert.equal(
            px(decl(base, prop)), want,
            `${label}: ${prop} harus tetap ${want}px agar visual desktop padat tidak berubah`)
    }

    // 2. Kontaining block untuk pseudo-element absolut.
    assert.ok(
        /position:\s*relative/.test(base) || /position:\s*absolute/.test(base),
        `${label} harus position:relative/absolute sebagai acuan ::before hit-area`);

    // 3. Hit-area pseudo-element: inset negatif cukup besar.
    const pseudoSel = selector + '::before';
    const pseudoRules = rulesFor(css, pseudoSel.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'));
    assert.ok(pseudoRules.length > 0, `${pseudoSel} (hit-area transparan) harus ada`);
    const pseudo = pseudoRules[0].body;
    assert.equal(decl(pseudo, 'content'), "''", `${pseudoSel} wajib content:''`);
    assert.match(pseudo, /position:\s*absolute/, `${pseudoSel} harus position:absolute`);

    const insets = ['top', 'right', 'bottom', 'left'].map((side) => {
        const v = px(decl(pseudo, side));
        return { side, v };
    });
    const shorthand = px(decl(pseudo, 'inset'));
    for (const { side, v } of insets) {
        const eff = v ?? shorthand;
        assert.ok(eff !== null && eff <= 0, `${pseudoSel}: ${side} harus negatif (dapat ${eff})`);
    }

    // 4. Matematika ukuran efektif: floor visual + 2×|inset| ≥ 44 tiap sumbu.
    const fw = Math.max(px(decl(base, 'width')) ?? 0, px(decl(base, 'min-width')) ?? 0);
    const fh = Math.max(px(decl(base, 'height')) ?? 0, px(decl(base, 'min-height')) ?? 0);
    const exW = px(decl(pseudo, 'left')) ?? shorthand;
    const exH = px(decl(pseudo, 'top')) ?? shorthand;
    const hitW = fw + 2 * Math.abs(exW);
    const hitH = fh + 2 * Math.abs(exH);
    assert.ok(hitW >= 44, `${label}: zona klik horizontal ${fw}+2×${Math.abs(exW)}=${hitW}px < 44px`);
    assert.ok(hitH >= 44, `${label}: zona klik vertikal ${fh}+2×${Math.abs(exH)}=${hitH}px < 44px`);
}

// ---------------------------------------------------------------------------
// S10 — .toast-close (output.css): visual tetap ~24px+padding, klik ≥44px
// ---------------------------------------------------------------------------

const OUTPUT_CSS = readCss('static/css/tailwind/output.css');
const ADMIN_CSS = readCss('static/css/admin-base.css');

test('S10: .toast-close — hit-area ≥44px tanpa mengubah visual, regresi Batch 2 terjaga', () => {
    // Batch 2: padding 10px + reveal :focus-visible — tidak boleh hilang.
    const focus = cssRule(OUTPUT_CSS, '\\.toast-close:focus-visible');
    assert.ok(focus !== null && /opacity:\s*1/.test(focus), '.toast-close:focus-visible (Batch 2) harus mereveal tombol');
    const base = cssRule(OUTPUT_CSS, '\\.toast-close');
    const pad = px(decl(base, 'padding'));
    assert.ok(pad !== null && pad >= 10, `.toast-close padding Batch 2 (≥10px) hilang (dapat ${pad})`);

    // Floor border-box: min-width/min-height 24px (padding sudah termasuk).
    assertHitArea44(OUTPUT_CSS, '.toast-close', {
        label: 'ikon ✕ toast',
        props: { 'min-width': 24, 'min-height': 24 }
    });
});

test('S10: .toast-close di MOBILE — elemen sendiri 44×44px (bukan hanya hit-area)', () => {
    const rule = rulesFor(mobileCss(OUTPUT_CSS), '\\.toast-close')[0];
    assert.ok(rule, 'override mobile .toast-close harus ada di @media max-width:768px');
    const mw = px(decl(rule.body, 'min-width'));
    const mh = px(decl(rule.body, 'min-height'));
    assert.ok(mw >= 44, `.toast-close mobile min-width ${mw}px < 44px`);
    assert.ok(mh >= 44, `.toast-close mobile min-height ${mh}px < 44px`);
});

// ---------------------------------------------------------------------------
// S10 — .search-clear-btn (output.css): visual 28px, zona klik 44×44
// ---------------------------------------------------------------------------

test('S10: .search-clear-btn — hit-area 28+2×8=44px, visual tombol ✕ pencarian tetap 28px', () => {
    assertHitArea44(OUTPUT_CSS, '.search-clear-btn', {
        label: 'tombol bersihkan pencarian',
        props: { width: 28, height: 28 }
    });
});

// ---------------------------------------------------------------------------
// S10 — .modal-close (output.css): kontrol inti kecil di SEMUA breakpoint —
// hit-area 32+2×6=44px tanpa membesarkan chip ✕ modal
// ---------------------------------------------------------------------------

test('S10: .modal-close — hit-area 32+2×6=44px, visual chip ✕ modal tetap 32px', () => {
    assertHitArea44(OUTPUT_CSS, '.modal-close', {
        label: 'tombol tutup modal',
        props: { width: 32, height: 32 }
    });
});

// ---------------------------------------------------------------------------
// S10 — admin-base.css blok mobile: badge toggle & tombol aksi ≥44px
// ---------------------------------------------------------------------------

test('S10: .status-badge[role="button"] di MOBILE min-height ≥44px (toggle status ujian)', () => {
    const rule = rulesFor(mobileCss(ADMIN_CSS), '\\.status-badge\\[role="button"\\]')[0];
    assert.ok(rule, 'rule mobile .status-badge[role="button"] harus ada di admin-base.css');
    const mh = px(decl(rule.body, 'min-height'));
    assert.ok(mh >= 44, `min-height mobile ${mh}px < 44px (dulu 34px — toggle Aktif/Nonaktif rawan salah ketuk)`);
});

test('S10: .btn-icon & .btn-sm di MOBILE ≥44px (dulu di-cap 40px)', () => {
    const mob = mobileCss(ADMIN_CSS);

    const icon = rulesFor(mob, '\\.btn-icon')[0];
    assert.ok(icon, 'rule mobile .btn-icon harus ada');
    assert.ok(px(decl(icon.body, 'min-width')) >= 44, '.btn-icon mobile min-width < 44px');
    assert.ok(px(decl(icon.body, 'min-height')) >= 44, '.btn-icon mobile min-height < 44px');

    const sm = rulesFor(mob, '\\.btn-sm')[0];
    assert.ok(sm, 'rule mobile .btn-sm harus ada');
    assert.ok(px(decl(sm.body, 'min-height')) >= 44, '.btn-sm mobile min-height < 44px');
});

// ---------------------------------------------------------------------------
// Kontrak CSS helper validasi (pendukung S19/T10b) — dipakai setFieldError()
// ---------------------------------------------------------------------------

test('Kontrak: .input-error & .field-error-text terdefinisi persis di admin-base.css', () => {
    const inputErr = cssRule(ADMIN_CSS, '\\.input-error');
    assert.ok(inputErr !== null, '.input-error harus ada di admin-base.css');
    assert.match(inputErr, /border-color:\s*var\(--color-danger\)\s*!important/,
        '.input-error wajib border-color: var(--color-danger) !important (kontrak setFieldError)');

    const fet = cssRule(ADMIN_CSS, '\\.field-error-text');
    assert.ok(fet !== null, '.field-error-text harus ada di admin-base.css');
    // Batch 4 (S15): literal #fca5a5 dimigrasi ke token --color-danger-light
    // (nilai visual identik, didefinisikan di theme.css).
    assert.match(fet, /color:\s*var\(--color-danger-light\)/,
        '.field-error-text color wajib var(--color-danger-light) (= #fca5a5)');
    assert.match(fet, /font-size:\s*12\.5px/, '.field-error-text font-size wajib 12.5px');
    assert.match(fet, /margin-top:\s*6px/, '.field-error-text margin-top wajib 6px');
    assert.match(fet, /margin-bottom:\s*0/, '.field-error-text margin-bottom wajib 0');
    assert.match(fet, /line-height:\s*1\.45/, '.field-error-text line-height wajib 1.45');

    // Penanda silang agar kontrak mudah ditemukan & tidak putus diam-diam.
    // Dicek pada konten MENTAH (komentar tidak di-strip) karena jangkarnya
    // memang komentar CSS di atas rule.
    const raw = read('static/css/admin-base.css');
    const anchor = raw.indexOf('.input-error');
    const around = raw.slice(Math.max(0, anchor - 400), anchor + 400);
    assert.match(around, /setFieldError\(\)/, 'dekat rule harus ada komentar penunjuk ke setFieldError()');
    assert.match(around, /admin-core\.js/, 'komentar harus menyebut admin-core.js sebagai pemakai');
});

// ---------------------------------------------------------------------------
// S4 (bagian landing/nav publik) — istilah unduh disatukan: "Unduh Aplikasi"
// ---------------------------------------------------------------------------

test('S4: nav publik shared.html memakai "Unduh Aplikasi", bukan "Download App"', () => {
    const html = read('templates/public/shared.html');
    assert.ok(!html.includes('Download App'), '"Download App" masih ada di nav publik');
    const idx = html.indexOf('Unduh Aplikasi');
    assert.ok(idx >= 0, 'label "Unduh Aplikasi" harus ada');
    const around = html.slice(Math.max(0, idx - 200), idx);
    assert.match(around, /href="\/download"/, '"Unduh Aplikasi" harus tetap menautkan /download');
});

test('S4: CTA index.html (hero + penutup) memakai "Unduh Aplikasi"', () => {
    const html = read('templates/public/index.html');
    assert.ok(!html.includes('Unduh Client Ujian'), '"Unduh Client Ujian" masih ada — ganti ke "Unduh Aplikasi"');

    const hits = [...html.matchAll(/Unduh Aplikasi/g)];
    assert.ok(hits.length >= 2, `dua CTA (hero & cta-section) harus "Unduh Aplikasi", ditemukan ${hits.length}`);
    for (const h of hits) {
        // 400px: antara <a href> dan label ada SVG inline yang panjang.
        const around = html.slice(Math.max(0, h.index - 400), h.index);
        assert.match(around, /href="\/download"/, 'setiap CTA "Unduh Aplikasi" harus mengarah ke /download');
    }

    // Jangan sampai sweep ini ikut mengubah copy landing lain (S21/S22 bukan batch ini).
    assert.ok(html.includes('Unduh Gratis'), 'copy "Unduh Gratis" di mockup tidak boleh ikut terganti');
});
