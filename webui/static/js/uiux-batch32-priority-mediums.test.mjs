/* Contract tests untuk Batch 32 — prioritas MEDIUM tersisa review
 * review_ui_halaman_web_2026-09-12.md (temuan M19, M21-lanjutan, L64, L69).
 *
 *   M19 — hasil.html me-render public_nav SEBELUM public_skip_link —
 *         terlewat oleh ab64bd9 (yang membetulkan 5 halaman auth). Tab
 *         pertama keyboard mendarat di nav, bukan lompat ke konten.
 *   M21 — sisa: host toast hardcode hasil.html:73 + skin toast lokal
 *         download.html:397-486 + komentar shared.html:899 tak akurat.
 *         Kontrak baru: SATU host (partial public_toast_host) + SATU skin
 *         publik (static/css/public-toast.css via public_toast_skin /
 *         public_head). Salinan skin admin di tailwind/output.css tidak
 *         disentuh (hasil build; halaman admin memuat keduanya, nilai
 *         akhir identik).
 *   L64 — noscript download.html hanya membuka .reveal; .platform-section
 *         tetap display:none → tanpa JS konten inti halaman (installer
 *         Android/Windows/Linux) tak terlihat.
 *   L69 — pasangan token z theme.css berbagi nilai: --z-toast ==
 *         --z-topbar-floating (10002) dan --z-hint == --z-skip-link (9998).
 *         Uji ketegasan lapisan aktif ada di batch18 (L69); di sini cukup
 *         sanity lintas-berkas untuk skin publik.
 *
 * Run with: node --test static/js/uiux-batch32-priority-mediums.test.mjs (from webui/)
 */

import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const HERE = path.dirname(fileURLToPath(import.meta.url));
const WEBUI_ROOT = path.join(HERE, '..', '..');
const read = (...p) => fs.readFileSync(path.join(...p), 'utf8');

const HASIL = read(WEBUI_ROOT, 'templates', 'public', 'hasil.html');
const DOWNLOAD = read(WEBUI_ROOT, 'templates', 'public', 'download.html');
const SHARED = read(WEBUI_ROOT, 'templates', 'public', 'shared.html');
const SKIN_CSS = read(WEBUI_ROOT, 'static', 'css', 'public-toast.css');
const OUTPUT_CSS = read(WEBUI_ROOT, 'static', 'css', 'tailwind', 'output.css');
const ADMIN_BASE_CSS = read(WEBUI_ROOT, 'static', 'css', 'admin-base.css');

// ---------------------------------------------------------------------------
// M19 — skip link sebelum nav di hasil.html
// ---------------------------------------------------------------------------

test('B32-1 (M19): hasil.html me-render public_skip_link SEBELUM public_nav', () => {
    const skip = HASIL.indexOf('template "public_skip_link"');
    const nav = HASIL.indexOf('template "public_nav"');
    assert.ok(skip !== -1 && nav !== -1, 'kedua partial dipanggil (prasyarat)');
    assert.ok(skip < nav,
        'skip link muncul SETELAH nav — Tab pertama mendarat di nav, bukan lompat ' +
        'ke konten (terlewat oleh ab64bd9 yang membetulkan 5 halaman auth)');
    // Paritas halaman auth (pola cek_hasil :25-27): skip-link adalah elemen
    // non-interaktif pertama di body.
    const bodyIdx = HASIL.indexOf('<body');
    assert.ok(bodyIdx !== -1 && skip > bodyIdx, 'skip-link dirender di dalam body');
    const between = HASIL.slice(bodyIdx, skip);
    assert.doesNotMatch(between, /<a\s/, 'tidak ada <a lain sebelum skip-link di body');
});

// ---------------------------------------------------------------------------
// M21 — satu host + satu skin toast publik
// ---------------------------------------------------------------------------

test('B32-2 (M21): shared.html menyediakan partial public_toast_skin (link skin bersama)', () => {
    const def = SHARED.match(/\{\{ define "public_toast_skin" \}\}([\s\S]*?)\{\{ end \}\}/);
    assert.ok(def, 'define "public_toast_skin" harus ada di shared.html');
    assert.match(def[1], /\/static\/css\/public-toast\.css\?v=\{\{\.version\}\}/,
        'skin partial me-link public-toast.css berversi');
});

test('B32-3 (M21): skin bersama public-toast.css lengkap (host, varian, keyframes, S10/S55)', () => {
    for (const cls of ['.toast-container', '.toast {', '.toast-body', '.toast-icon',
                       '.toast-msg', '.toast-close', '.toast-close:focus-visible',
                       '.toast-close::before',
                       '.toast-success', '.toast-error', '.toast-warning', '.toast-info',
                       '.toast-r2-config', '.toast-exit']) {
        assert.ok(SKIN_CSS.includes(cls.replace(' {', '')), `skin wajib memuat ${cls}`);
    }
    assert.match(SKIN_CSS, /@keyframes toastIn/, 'animasi masuk');
    assert.match(SKIN_CSS, /@keyframes toastOut/, 'animasi keluar');
    assert.match(SKIN_CSS, /z-index:\s*var\(--z-toast\)/,
        'lapisan dikunci via var(--z-toast) — bukan literal (L69)');
    assert.doesNotMatch(SKIN_CSS, /rgba\(\s*\d/,
        'skin publik wajib token: rgba(var(--rgb-*), α) — nol literal triplet');
    // Port S55/S10: fokus keyboard + hit-area 44px terjaga.
    assert.match(SKIN_CSS, /min-width:\s*44px[\s\S]{0,80}min-height:\s*44px/,
        'hit-area ✕ ≥44px di ponsel (S10)');
    assert.match(SKIN_CSS, /@media \(prefers-reduced-motion: reduce\)/,
        'reduced-motion toast (paritas output.css)');
});

test('B32-4 (M21): skin dipakai SEMUA halaman publik — public_head atau public_toast_skin, tidak dobel', () => {
    // public_head memancarkan link skin; halaman non-head memanggil partial skin.
    const headOk = /public-toast\.css\?v=/.test(
        SHARED.match(/\{\{ define "public_head" \}\}([\s\S]*?)\{\{ end \}\}/)?.[1] || '');
    assert.ok(headOk, 'public_head harus memuat link public-toast.css (index/download/hasil)');

    const authPages = ['cek_hasil', 'forgot_password', 'register', 'register_confirm', 'reset_password']
        .map((n) => ({ name: n, src: read(WEBUI_ROOT, 'templates', 'public', `${n}.html`) }));
    for (const { name, src } of authPages) {
        const direct = (src.match(/public-toast\.css\?v=/g) || []).length;
        const viaPartial = src.includes('template "public_toast_skin"');
        assert.ok(direct === 1 || viaPartial,
            `${name}: skin toast harus dimuat (link langsung atau partial public_toast_skin)`);
        assert.ok(direct + viaPartial === 1,
            `${name}: skin jangan dobel (link langsung + partial sekaligus)`);
    }
});

test('B32-5 (M21): hasil.html & download.html tanpa host/skin hardcode — partial bersama', () => {
    for (const [name, src] of [['hasil.html', HASIL], ['download.html', DOWNLOAD]]) {
        assert.match(src, /\{\{\s*template\s+"public_toast_host"\s+\.\s*\}\}/,
            `${name}: host toast dari partial public_toast_host`);
        assert.doesNotMatch(src, /<div class="toast-container"/,
            `${name}: host hardcode dihapus`);
    }
    assert.doesNotMatch(DOWNLOAD, /\.toast\s*\{|\.toast-container\s*\{/,
        'download.html: blok skin toast lokal (dulu :397-486) dihapus');
    // download memakai public_head — skin link datang dari head (memanggil
    // public_toast_skin lagi = CSS dobel, kontra R108 single-fetch).
    assert.equal((DOWNLOAD.match(/public-toast\.css/g) || []).length, 0,
        'download.html tidak me-link skin langsung — lewat public_head');
});

test('B32-6 (M21): komentar shared.html akurat — skin bukan lagi klaim output.css', () => {
    const m = SHARED.match(/public_toast_host —[\s\S]*?\*\//);
    assert.ok(m, 'komentar partial public_toast_host ada');
    assert.match(m[0], /public-toast\.css/, 'komentar menyebut skin bersama public-toast.css');
    assert.doesNotMatch(m[0], /sudah dimuat semua halaman publik/,
        'klaim lama "output.css sudah dimuat semua halaman publik" salah — download tak memuatnya');
});

// ---------------------------------------------------------------------------
// L64 — noscript membuka .platform-section
// ---------------------------------------------------------------------------

test('B32-7 (L64): noscript download.html membuka .reveal DAN .platform-section', () => {
    const blocks = [...DOWNLOAD.matchAll(/<noscript><style>([\s\S]*?)<\/style><\/noscript>/g)]
        .map((m) => m[1]);
    assert.ok(blocks.length >= 1, 'noscript style ada (prasyarat)');
    const joined = blocks.join('\n');
    assert.match(joined, /\.reveal[^}]*opacity:\s*1/, '.reveal tetap dibuka');
    assert.match(joined, /\.platform-section\s*\{[^}]*display:\s*block/,
        '.platform-section harus dibuka noscript — tanpa JS konten installer ' +
        '(Android/Windows/Linux) tidak terlihat sama sekali');
    // Default CSS tetap menyembunyikan section (animasi JS); noscript yang membuka.
    assert.match(DOWNLOAD, /\.platform-section\s*\{[^}]*display:\s*none/,
        'default .platform-section tetap display:none (dibuka JS/noscript)');
});

// ---------------------------------------------------------------------------
// L69 — skin publik mengikuti skala z token
// ---------------------------------------------------------------------------

test('B32-8 (L69): skin toast publik & admin memakai var(--z-toast) — tanpa duplikasi definisi nilai', () => {
    assert.match(SKIN_CSS, /z-index:\s*var\(--z-toast\)/);
    // admin-base tetap satu-satunya tempat yang mengunci var(--z-toast) utk admin,
    // skin publik jangan menimpa dengan nilai lain (spesifisitas sama, urutan file bebas).
    const skinZ = SKIN_CSS.match(/z-index:\s*([^;]+);/)?.[1]?.trim();
    const baseZ = (() => {
        const i = ADMIN_BASE_CSS.indexOf('.toast-container {');
        return ADMIN_BASE_CSS.slice(i, ADMIN_BASE_CSS.indexOf('}', i))
            .match(/z-index:\s*([^;]+);/)?.[1]?.trim();
    })();
    assert.equal(skinZ, baseZ,
        'nilai lapisan toast publik & admin wajib identik (sama-sama var(--z-toast))');
    // output.css tidak menyentuh z-index toast (kontrak S117b tetap).
    const oi = OUTPUT_CSS.indexOf('.toast-container {');
    const orule = OUTPUT_CSS.slice(oi, OUTPUT_CSS.indexOf('}', oi));
    assert.doesNotMatch(orule, /z-index/, 'output.css tetap bebas z-index toast (S117b)');
});
