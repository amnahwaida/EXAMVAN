/* Regression contract tests untuk Batch 3 perbaikan UI/UX — halaman hasil ujian.
 * Referensi temuan: review_uiux_webui.md (ID: S8 — font mikro < 12px,
 * S9 — scroll-trap .answer-grid di mobile).
 *
 * Run with:  node --test static/js/uiux-batch3-hasil-css.test.mjs   (from webui/)
 *
 * Kontrak statik atas static/css/hasil.css (file asli yang dibaca server),
 * ditulis SEBELUM implementasi (red → green). Gaya parsing per-rule mengikuti
 * helper cssRule di uiux-batch2.test.mjs, diperluas agar sadar blok @media.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const WEBUI_ROOT = path.join(__dirname, '..', '..');
const CSS_FILE = 'static/css/hasil.css';
const read = (rel) => fs.readFileSync(path.join(WEBUI_ROOT, rel), 'utf8');

// ---------------------------------------------------------------------------
// Helper parse CSS per-rule (gaya uiux-batch2.test.mjs)
// ---------------------------------------------------------------------------

/** Isi blok `selector { ... }` PERTAMA yang cocok dari sebuah CSS. */
function cssRule(css, selectorRegex) {
    const m = css.match(new RegExp(selectorRegex + '\\s*\\{([^}]*)\\}'));
    return m ? m[1] : null;
}

const MEDIA_RE = /@media[^{]*\{(?:[^{}]*|\{[^{}]*\})*\}/g;

/** Semua blok @media (nesting 1 level — cukup untuk struktur hasil.css). */
function mediaBlocks(css) {
    return [...css.matchAll(MEDIA_RE)].map((m) => ({
        query: m[0].slice(m[0].indexOf('@'), m[0].indexOf('{')).replace(/\s+/g, ' ').trim(),
        body: m[0].slice(m[0].indexOf('{') + 1, -1)
    }));
}

/** Rule di dalam SEMUA blok @media yang query-nya cocok. */
function rulesInMedia(css, queryRegex, selectorRegex) {
    const out = [];
    for (const b of mediaBlocks(css)) {
        if (!new RegExp(queryRegex).test(b.query)) continue;
        for (const m of b.body.matchAll(new RegExp('(' + selectorRegex + ')\\s*\\{([^}]*)\\}', 'g'))) {
            out.push({ selector: m[1].replace(/\s+/g, ' ').trim(), decls: m[2] });
        }
    }
    return out;
}

/** Kumpulkan {media, selector, decls} dari scope top-level + semua blok @media. */
function collectRules(css) {
    const scopes = [{ media: '(top-level)', body: css.replace(MEDIA_RE, '') }];
    for (const b of mediaBlocks(css)) scopes.push({ media: b.query, body: b.body });
    const rules = [];
    for (const s of scopes) {
        for (const m of s.body.matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
            const selector = m[1].replace(/\/\*[\s\S]*?\*\//g, ' ').replace(/\s+/g, ' ').trim();
            if (!selector || selector.startsWith('@')) continue;
            rules.push({ media: s.media, selector, decls: m[2] });
        }
    }
    return rules;
}

/** Konversi nilai font-size ke px (root 16px). null bila unit tak bisa diparse. */
function fontSizePx(value) {
    const m = value.trim().match(/^([0-9]*\.?[0-9]+)(rem|px|em)$/);
    if (!m) return null;
    const n = parseFloat(m[1]);
    return m[2] === 'px' ? n : n * 16; // rem/em dihitung relatif root 16px
}

/** Alpha komponen terakhir rgba(...) atau null. */
function rgbaAlpha(value) {
    const m = value.match(/rgba\(\s*\d+\s*,\s*\d+\s*,\s*\d+\s*,\s*([0-9.]+)\s*\)/);
    return m ? parseFloat(m[1]) : null;
}

/** Padding horizontal dari deklarasi padding 1-4 nilai ("2px 8px" → 8). */
function paddingH(body) {
    const m = body.match(/(?:^|[;\s])padding\s*:\s*([^;}]+)/);
    if (!m) return null;
    const parts = m[1].trim().split(/\s+/).filter((p) => /\d+px$/.test(p)).map((p) => parseFloat(p));
    if (!parts.length) return null;
    if (parts.length === 1) return parts[0];
    if (parts.length === 2) return parts[1];
    return parts.length === 3 ? parts[2] : parts[1] + parts[3]; // 3 nilai → left; 4 nilai → left+right
}

// ---------------------------------------------------------------------------
// S8 — Whitelist pengecualian font mikro (< 0.75rem / 12px).
//
// HANYA untuk teks/ikon MIKRO yang MURNI DEKORATIF (bukan informatif), sesuai
// aturan review S8. Setiap entri WAJIB membawa `reason` satu kalimat.
//
// SAAT INI KOSONG: audit seluruh hasil.css menemukan bahwa semua deklarasi
// < 0.75rem adalah teks INFORMATIF (badge identitas halaman, header kolom
// tabel, label statistik, bobot soal, nomor & status jawaban, info footer,
// pseudo-label "Jawaban:"/"Kunci:"), sehingga SEMUANYA dinaikkan ke ≥ 0.75rem
// — termasuk .expand-icon yang ukuran rendernya memang dikunci inline (14px,
// aria-hidden) oleh template, sehingga pengecualian pun tidak diperlukan.
// ---------------------------------------------------------------------------
const FONT_SIZE_WHITELIST = [
    // Bentuk entri (contoh):
    // { selector: '.contoh-dekoratif', value: '0.65rem', reason: 'hiasan murni, tidak membawa informasi' },
];

// ---------------------------------------------------------------------------
// S8 — Batas bawah 12px untuk seluruh teks informatif di hasil.css
// ---------------------------------------------------------------------------

test('S8: tidak ada deklarasi font-size < 12px (0.75rem) di hasil.css, kecuali whitelist dekoratif', () => {
    const css = read(CSS_FILE);
    const rules = collectRules(css);

    const violations = [];
    let total = 0;
    for (const r of rules) {
        for (const dm of r.decls.matchAll(/font-size\s*:\s*([^;}]+)/gi)) {
            total += 1;
            const raw = dm[1].trim();
            const where = `${r.selector}${r.media !== '(top-level)' ? `  [@${r.media}]` : ''}`;
            const px = fontSizePx(raw);
            if (px === null) {
                violations.push(`${where}: "${raw}" tidak bisa diparse — pakai rem/px numerik agar bisa diaudit`);
                continue;
            }
            if (px >= 12) continue;
            const exc = FONT_SIZE_WHITELIST.find((w) => w.selector === r.selector && w.value === raw);
            if (exc) continue;
            violations.push(`${where}: ${raw} = ${px.toFixed(1)}px < 12px (S8: batas bawah teks informatif)`);
        }
    }

    // Sanity parser: hasil.css memiliki ±55 deklarasi font-size; kalau tiba-tiba
    // jauh lebih sedikit, berarti parser/pattern rusak dan test ini kosong merah.
    assert.ok(total >= 40, `parser hanya mendeteksi ${total} deklarasi font-size — audit tidak sah`);
    assert.deepEqual(violations, [], `font mikro tersisa (naikkan ke ≥ 0.75rem, kompensasi lewat padding/gap):\n${violations.join('\n')}`);
});

test('S8: .header-badge (anchor :760 ≈8.8px) — desktop & mobile ≥ 0.75rem, kompensasi via padding', () => {
    const css = read(CSS_FILE);

    const base = cssRule(css, '\\.header-badge');
    assert.ok(base !== null, 'rule .header-badge harus ada');
    const baseFs = fontSizePx(base.match(/font-size\s*:\s*([^;}]+)/)[1]);
    assert.ok(baseFs >= 12, `.header-badge desktop: ${baseFs}px < 12px`);
    const basePad = paddingH(base);
    assert.ok(basePad !== null && basePad <= 8, `.header-badge desktop: padding horizontal ${basePad}px harus ≤ 8px (kompensasi kenaikan font)`);

    const mobile = rulesInMedia(css, 'max-width:\\s*768px', '\\.header-badge');
    assert.ok(mobile.length > 0, 'override .header-badge di @media max-width:768px harus ada');
    for (const r of mobile) {
        const fs = fontSizePx(r.decls.match(/font-size\s*:\s*([^;}]+)/)[1]);
        assert.ok(fs >= 12, `.header-badge mobile: ${fs}px < 12px — kecilkan padding, BUKAN font`);
        const pad = paddingH(r.decls);
        assert.ok(pad !== null && pad <= 6, `.header-badge mobile: padding horizontal ${pad}px harus ≤ 6px`);
    }
});

test('S8: anchor review lain (.stat-mini-label, .key-card-weight, .detail-header-labels, thead th, footer, q-num/q-status mobile, pseudo-label) masih ada & ≥ 0.75rem', () => {
    const css = read(CSS_FILE);
    const anchors = [
        { label: '.stat-mini-label (base :521)', get: () => ruleTop(css, '\\.stat-mini-label') },
        { label: '.key-card-weight (:557)', get: () => ruleTop(css, '\\.key-card-weight') },
        { label: '.detail-header-labels (:484)', get: () => ruleTop(css, '\\.detail-header-labels') },
        { label: '.results-table thead th (:293)', get: () => ruleTop(css, '\\.results-table thead th') },
        { label: '.footer (:736)', get: () => ruleTop(css, '\\.footer') },
        { label: '.pagination-info mobile (:714)', get: () => ruleMedia('\\.pagination-info') },
        { label: '.stat-mini-label mobile (:764)', get: () => ruleMedia('\\.stat-mini-label') },
        { label: '.q-num mobile (:861)', get: () => ruleMedia('\\.q-num') },
        { label: '.q-status mobile (:862)', get: () => ruleMedia('\\.q-status') },
        { label: '.q-student-ans::before mobile (:896)', get: () => ruleMedia('\\.q-student-ans::before') },
        { label: '.q-correct-ans::before mobile (:902)', get: () => ruleMedia('\\.q-correct-ans::before') }
    ];

    function ruleTop(c, sel) {
        const stripped = c.replace(MEDIA_RE, '');
        return cssRule(stripped, sel);
    }
    function ruleMedia(sel) {
        const hits = rulesInMedia(css, 'max-width:\\s*768px', sel);
        return hits.length ? hits[0].decls : null;
    }

    for (const a of anchors) {
        const body = a.get();
        assert.ok(body !== null, `rule ${a.label} harus tetap ada (jangan dihapus demi lolos test)`);
        const dm = body.match(/font-size\s*:\s*([^;}]+)/);
        assert.ok(dm, `rule ${a.label} wajib punya deklarasi font-size eksplisit`);
        const px = fontSizePx(dm[1]);
        assert.ok(px !== null && px >= 12, `${a.label}: ${dm[1].trim()} = ${px === null ? '?' : px.toFixed(1)}px < 12px`);
    }
});

// ---------------------------------------------------------------------------
// S9 — Scroll-trap .answer-grid
// ---------------------------------------------------------------------------

test('S9: desktop — .answer-grid tetap terbatas (max-height + overflow-y auto) sebagai panel ringkas', () => {
    const css = read(CSS_FILE);
    const stripped = css.replace(MEDIA_RE, '');
    const body = cssRule(stripped, '\\.answer-grid');
    assert.ok(body !== null, 'rule .answer-grid top-level harus ada');

    const mh = body.match(/max-height\s*:\s*(\d+)px/);
    assert.ok(mh && parseInt(mh[1], 10) >= 200, `.answer-grid desktop wajib punya max-height px masuk akal (dapat ${mh ? mh[1] + 'px' : 'tidak ada'})`);
    assert.match(body, /overflow-y\s*:\s*auto/, '.answer-grid desktop tetap overflow-y:auto');
});

test('S9: desktop — indikator ada-lanjutan jelas: scrollbar selalu tampak (track + thumb kontras, Firefox scrollbar-color)', () => {
    const css = read(CSS_FILE);
    const stripped = css.replace(MEDIA_RE, '');

    const base = cssRule(stripped, '\\.answer-grid');
    assert.ok(base !== null, 'rule .answer-grid top-level harus ada');
    const sc = base.match(/scrollbar-color\s*:\s*([^;}]+)/);
    assert.ok(sc, '.answer-grid wajib mendeklarasikan scrollbar-color (Firefox)');
    const scThumb = sc[1].split(/\s+/)[0];
    const scA = rgbaAlpha(scThumb);
    assert.ok(scA !== null && scA >= 0.25, `scrollbar-color thumb alpha ${scA} harus ≥ 0.25 agar terlihat jelas`);

    const track = cssRule(stripped, '\\.answer-grid::-webkit-scrollbar');
    assert.ok(track !== null, 'rule .answer-grid::-webkit-scrollbar harus ada');
    const w = track.match(/width\s*:\s*(\d+)px/);
    assert.ok(w && parseInt(w[1], 10) >= 8, `lebar scrollbar webkit ${w ? w[1] : '?'}px harus ≥ 8px (dulu 6px nyaris tak terlihat)`);
    const trackBg = track.match(/background\s*:\s*([^;}]+)/);
    assert.ok(trackBg && /rgba?\(/.test(trackBg[1]) && !/transparent/i.test(trackBg[1]), 'track scrollbar webkit harus berwarna, bukan transparan');

    const thumb = cssRule(stripped, '\\.answer-grid::-webkit-scrollbar-thumb');
    assert.ok(thumb !== null, 'rule .answer-grid::-webkit-scrollbar-thumb harus ada');
    const ta = rgbaAlpha(thumb);
    assert.ok(ta !== null && ta >= 0.25, `thumb scrollbar webkit alpha ${ta} harus ≥ 0.25 (dulu 0.1 — nyaris tak terlihat)`);
});

test('S9: mobile (@media max-width:768px) — .answer-grid melepas scroll-trap (max-height none/auto, overflow bebas)', () => {
    const css = read(CSS_FILE);
    const hits = rulesInMedia(css, 'max-width:\\s*768px', '\\.answer-grid')
        .filter((r) => /max-height/.test(r.decls));
    assert.ok(hits.length > 0, 'harus ada override .answer-grid di dalam @media max-width:768px yang menyentuh max-height');

    for (const r of hits) {
        assert.match(r.decls, /max-height\s*:\s*(none|auto)/, 'di mobile max-height dilepas (none/auto) — konten mengembang penuh, halaman yang scroll');
        assert.match(r.decls, /overflow(-y)?\s*:\s*(visible|unset|auto)/, 'di mobile overflow-y internal dilepas — bukan lagi scroll area tersendiri');
    }
});
