/**
 * ══════════════════════════════════════════════════════════════════════════
 * Suite UI/UX BATCH 14 — TOKENS & GUARD (agen batch14-tokens-guard)
 * ══════════════════════════════════════════════════════════════════════════
 *
 * Referensi: review_uiux_webui.md bagian 5.11 RE-REVIEW RONDE 8. Tema besar
 * ronde ini: kontrak token selama ini ditegakkan PER-DAFTAR-FILE milik
 * masing-masing agen batch, sehingga literal yang identik lolos di file yang
 * tidak masuk daftar; beberapa guard juga memakai regex FIRST-MATCH-ONLY
 * yang memberi rasa aman palsu. Agen ini menaikkan kontrak menjadi
 * FOLDER-WIDE dan menutup lubang plafon:
 *
 *   S80 — Kontrak `#f87171 → var(--color-danger-bright)` (Batch 12) bocor:
 *         5 literal tersisa di dashboard.html (:62,:326,:538) & settings.html
 *         (:437,:644) karena guard hanya menguji enam template milik agen
 *         batch12. Kontrak baru: templates/** + JS statis = 0 #f87171.
 *
 *   S81 — Klaim R82 "#818cf8 habis" ternyata PARSIAL: sisa di
 *         pengawas_detail.html:1349, settings.html:1972, admin.js:1081;
 *         guard lama hanya memvalidasi gradien PERTAMA settings.html.
 *         Kontrak baru: folder-wide 0 #818cf8 (definisi token theme.css
 *         tetap boleh — konteks non-CSS template wajib var()).
 *
 *   R88 — Literal z-index vs sistem token --z-*: popup pengawas dashboard
 *         (z-index:100), dropdown render-JS admin.js (z-index:100), dan
 *         duplikat manual --z-toast (dashboard :741). Kontrak: tiga lokasi
 *         itu memakai var(--z-*), dan TIDAK BOLEH ada lagi literal
 *         z-index ≥1000 (kelas berbahaya yang melompati toast/onboarding)
 *         di templates/admin/** + admin.js.
 *
 *   R90 — Cap !important S64/S71 tidak mencakup theme.css (ada 1 aktual).
 *         Kontrak: CAPS batch11-settings-guard ditambah 'css/theme.css': 1.
 *
 *   S88 — Baseline JS batch9-tokens-guard berlubang: settings-system-apps.js
 *         punya rgba=2 TANPA entri baseline; users/general/packages/
 *         admin-core juga tanpa entri (aktual 0). Kontrak: semua modul JS
 *         statis punya entri — penambahan warna hardcoded apa pun memerah
 *         test.
 *
 *   R95 — Cap hex settings-vouchers.js longgar (aktual 12 vs plafon 22,
 *         headroom +83%) — inkonsisten dengan kontrak "plafon dikunci
 *         angka aktual" Batch 13. Kontrak: cap == angka terukur hari ini
 *         (test menghitung independen lalu membandingkan dengan guard).
 *
 * Run with:  node --test static/js/uiux-batch14-tokens-guard.test.mjs   (from webui/)
 */

import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const HERE = path.dirname(fileURLToPath(import.meta.url));
const WEBUI_ROOT = path.join(HERE, '..', '..');

function walk(dir, ext, acc = []) {
    for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
        const full = path.join(dir, entry.name);
        if (entry.isDirectory()) walk(full, ext, acc);
        else if (entry.name.endsWith(ext)) acc.push(full);
    }
    return acc;
}

// Seluruh template HTML repo + seluruh JS statis non-test (scope folder-wide).
const HTML_FILES = walk(path.join(WEBUI_ROOT, 'templates'), '.html');
const JS_FILES = [
    ...walk(path.join(WEBUI_ROOT, 'static', 'js'), '.js'),
    ...walk(path.join(WEBUI_ROOT, 'static', 'js'), '.mjs'),
].filter((f) => !path.basename(f).startsWith('uiux-batch')); // guard suite sendiri dikecualikan

const readAll = (files) =>
    files.map((f) => ({ file: path.relative(WEBUI_ROOT, f), src: fs.readFileSync(f, 'utf8') }));

const ALL_HTML = readAll(HTML_FILES);
const ALL_JS = readAll(JS_FILES);

/** Kumpulkan kemunculan regex lintas file; kembalikan daftar "file:n: cuplikan". */
function findMatches(files, re) {
    const out = [];
    for (const { file, src } of files) {
        let m;
        const g = new RegExp(re.source, re.flags.includes('g') ? re.flags : re.flags + 'g');
        while ((m = g.exec(src)) !== null) {
            out.push(`${file}: ${m[0]}`);
            if (m.index === g.lastIndex) g.lastIndex += 1; // anti zero-length loop
        }
    }
    return out;
}

// ════════════════════════════════════════════════════════════════════════
// S80 — #f87171 habis FOLDER-WIDE (templates/** + JS statis)
// ════════════════════════════════════════════════════════════════════════

test('S80 (guard folder-wide): templates/** bebas literal #f87171', () => {
    const hits = findMatches(ALL_HTML, /#f87171/i);
    assert.equal(hits.length, 0,
        `${hits.length} literal #f87171 tersisa (kontras gagal-AA sebagai teks kecil) — migrasi var(--color-danger-bright): ${hits.slice(0, 6).join(' · ')}`);
});

test('S80 (guard folder-wide): JS statis bebas literal #f87171', () => {
    const hits = findMatches(ALL_JS, /#f87171/i);
    assert.equal(hits.length, 0,
        `literal #f87171 di render-JS/JS statis juga wajib token: ${hits.slice(0, 6).join(' · ')}`);
});

test('S80 (statik): token --color-danger-bright tetap terdefinisi theme.css', () => {
    const theme = fs.readFileSync(path.join(WEBUI_ROOT, 'static', 'css', 'theme.css'), 'utf8');
    assert.match(theme, /--color-danger-bright:\s*#f87171\s*;/);
});

// ════════════════════════════════════════════════════════════════════════
// R126 — cakupan folder-wide diperluas ke static/css/**: guard S80 lama
//        hanya berjalan di templates+JS sehingga literal #f87171 di folder
//        CSS lolos diam-diam (temuan ronde 10: 4 hit). Pemetaan cakupan:
//
//          - static/css/*.css (inti, ditulis tangan)      → WAJIB 0
//            (definisi token di theme.css di-whitelist — sumber kebenaran).
//          - static/css/tailwind/admin-tailwind.css       → komponen kustom,
//            MASUK cakupan dengan BASELINE EKSPLISIT (lihat test di bawah);
//            target jangka panjang 0.
//          - static/css/tailwind/output.css               → DIKECUALIKAN.
//            Alasan: artefak build Tailwind (generated), bukan sumber
//            tangan-pertama — mengedit/menghitungnya sia-sia dan akan
//            berganti isi tiap regenerasi. Pengecualian ini EKSPLISIT agar
//            tidak terbaca sebagai lubang yang terlupakan.
// ════════════════════════════════════════════════════════════════════════

const CSS_FILES = walk(path.join(WEBUI_ROOT, 'static', 'css'), '.css')
    .filter((f) => !f.endsWith(path.join('tailwind', 'output.css'))); // generated — lihat catatan R126
const ALL_CSS = readAll(CSS_FILES);
// Zona css inti = tanpa theme.css (definisi token diuji terpisah di bawah)
// dan tanpa admin-tailwind.css (punya baseline eksplisit sendiri).
// Isi komentar blok di-strip sebelum dihitung (preseden batch7 pada guard
// admin-base) — dokumentasi kalibrasi boleh MENYEBUT hex tanpa dihitung
// sebagai pemakaian.
const CORE_CSS = ALL_CSS.filter((e) =>
    !e.file.endsWith(path.join('css', 'theme.css')) &&
    !e.file.endsWith(path.join('tailwind', 'admin-tailwind.css')))
    .map((e) => ({ file: e.file, src: e.src.replace(/\/\*[\s\S]*?\*\//g, '') }));

test('R126 (guard folder-wide): css inti non-generated bebas literal #f87171', () => {
    const hits = findMatches(CORE_CSS, /#f87171/i);
    assert.equal(hits.length, 0,
        `literal #f87171 di css inti wajib var(--color-danger-bright): ${hits.slice(0, 6).join(' · ')}`);
});

test('R126 (statik): satu-satunya #f87171 di theme.css adalah DEFINISI token itu sendiri', () => {
    const theme = ALL_CSS.find((e) => e.file.endsWith(path.join('css', 'theme.css')));
    const noComments = theme.src.replace(/\/\*[\s\S]*?\*\//g, '');
    const n = (noComments.match(/#f87171/gi) || []).length;
    assert.equal(n, 1, 'theme.css wajib tepat satu #f87171 non-komentar (definisi --color-danger-bright)');
    assert.match(noComments, /--color-danger-bright:\s*#f87171\s*;/);
});

test('R126 (guard): admin-tailwind.css TIDAK ADA lagi (dihapus Batch 19 - file mati tanpa pemuat)', () => {
    // Riwayat: file ini tak pernah dimuat halaman mana pun (nol referensi),
    // namun selama ini membebani guard (baseline f87171, pengecualian walk).
    // Batch 16 memigrasi kedua literalnya; Batch 19 menghapus filenya
    // sepenuhnya (S117) - kontrak kini memastikan ia TIDAK kembali.
    const at = ALL_CSS.find((e) => e.file.endsWith(path.join('tailwind', 'admin-tailwind.css')));
    assert.equal(at, undefined,
        'admin-tailwind.css wajib tetap terhapus - artefak manual di web root publik dilarang');
});

// ════════════════════════════════════════════════════════════════════════
// S81 — #818cf8 habis FOLDER-WIDE (koreksi klaim R82 yang parsial)
// ════════════════════════════════════════════════════════════════════════

test('S81 (guard folder-wide): templates/** bebas literal #818cf8 — pakai var(--color-primary-bright)', () => {
    const hits = findMatches(ALL_HTML, /#818cf8/i);
    assert.equal(hits.length, 0,
        `klaim R82 "habis ×11" belum tuntas — ${hits.length} literal tersisa: ${hits.slice(0, 6).join(' · ')}`);
});

test('S81 (guard folder-wide): JS statis bebas literal #818cf8', () => {
    const hits = findMatches(ALL_JS, /#818cf8/i);
    assert.equal(hits.length, 0,
        `sisa literal di JS (mis. accent-color dropdown admin.js): ${hits.slice(0, 6).join(' · ')}`);
});

test('S81 (kontrak): guard batch13-settings-guard kini memakai hitungan GLOBAL untuk #818cf8', () => {
    const oldGuard = fs.readFileSync(
        path.join(WEBUI_ROOT, 'static', 'js', 'uiux-batch13-settings-guard.test.mjs'), 'utf8');
    // Assertion lama FIRST-MATCH-ONLY (`SETTINGS.match(/linear-gradient…/)`)
    // hanya memvalidasi gradien pertama — sisa literal di lokasi lain lolos.
    // Kontrak: harus ada asersi hitungan global yang menuntut 0.
    assert.match(oldGuard, /SETTINGS\.match\(\/#818cf8\/gi/,
        'guard R82 wajib menghitung SEMUA kemunculan #818cf8 di settings.html (bukan hanya gradien pertama)');
});

// ════════════════════════════════════════════════════════════════════════
// R88 — z-index literal → var(--z-*) di lokasi temuan + larangan ≥4 digit
// ════════════════════════════════════════════════════════════════════════

const ADMIN_HTML = ALL_HTML.filter((e) => e.file.startsWith(`templates${path.sep}admin`));
const ADMIN_JS = ALL_JS.filter((e) => /admin\.js$/.test(e.file));

test('R88 (statik): rule .pengawas-popup dashboard memakai token z-index, bukan literal 100', () => {
    const dash = ADMIN_HTML.find((e) => e.file.endsWith('dashboard.html'));
    const rule = dash.src.match(/\.pengawas-popup\s*\{[^}]*\}/);
    assert.ok(rule, 'rule .pengawas-popup ada di dashboard.html');
    assert.match(rule[0], /z-index:\s*var\(--z-[\w-]+\)/,
        'popup pengawas (literal 100) dua tingkat magnitudo di bawah token resmi — pakai var(--z-dropdown)');
});

test('R88 (statik): dropdown render-JS admin.js memakai var(--z-dropdown)', () => {
    const src = ADMIN_JS.map((e) => e.src).join('\n');
    assert.doesNotMatch(src, /z-index:\s*100(?![0-9])/, 
        'dropdown pengawas render-JS masih z-index:100 literal — pakai var(--z-dropdown)');
    assert.match(src, /z-index:\s*var\(--z-dropdown\)/,
        'dropdown wajib menyatakan token z secara eksplisit');
});

test('R88 (statik): upload-progress-pill settings.html tidak lagi menduplikasi nilai --z-toast secara manual (10002)', () => {
    // Koreksi atribusi temuan ronde 8: literal 10002 ada di settings.html:741
    // (upload-progress-pill), bukan dashboard — nilai identik dengan --z-toast.
    const settings = fs.readFileSync(path.join(WEBUI_ROOT, 'templates', 'admin', 'settings.html'), 'utf8');
    assert.doesNotMatch(settings, /z-index:\s*10002/,
        '10002 = duplikat manual nilai --z-toast; pakai var(--z-toast) agar perubahan token tidak lolos');
});

test('R88 (guard): templates/admin/** + admin.js bebas literal z-index ≥1000 (kelas berbahaya)', () => {
    // Batch 15 (R105): flag /i — "Z-INDEX: 999" kapital tidak boleh lolos
    // guard kelas berbahaya hanya dengan mengubah case.
    const hits = [
        ...findMatches(ADMIN_HTML, /z-index:\s*[1-9]\d{3,}/i),
        ...findMatches(ADMIN_JS, /z-index:\s*[1-9]\d{3,}/i),
    ];
    assert.equal(hits.length, 0,
        `literal z-index tinggi dapat melompati toast/onboarding tanpa sadar — pakai token --z-*: ${hits.join(' · ')}`);
});

// ════════════════════════════════════════════════════════════════════════
// R90 — CAPS !important mencakup theme.css (= aktual 1)
// ════════════════════════════════════════════════════════════════════════

test('R90 (statik): batch11-settings-guard CAPS memuat entri css/theme.css', () => {
    const guard = fs.readFileSync(
        path.join(WEBUI_ROOT, 'static', 'js', 'uiux-batch11-settings-guard.test.mjs'), 'utf8');
    assert.match(guard, /'css\/theme\.css'\s*:\s*\d+/,
        'theme.css adalah satu-satunya CSS inti tanpa plafon !important — tambahkan ke CAPS agar tak jadi lubang baru');
});

test('R90 (statik): angka aktual !important theme.css = 1 (skip-link) — cap harus sama dengannya', () => {
    const theme = fs.readFileSync(path.join(WEBUI_ROOT, 'static', 'css', 'theme.css'), 'utf8');
    const actual = theme.split('\n').filter((l) => l.includes('!important')).length;
    const guard = fs.readFileSync(
        path.join(WEBUI_ROOT, 'static', 'js', 'uiux-batch11-settings-guard.test.mjs'), 'utf8');
    const declared = Number(guard.match(/'css\/theme\.css'\s*:\s*(\d+)/)?.[1] ?? NaN);
    assert.equal(actual, declared,
        `!important theme.css aktual ${actual}, cap terdeklarasi ${declared} — kontrak plafon = angka terukur`);
});

// ════════════════════════════════════════════════════════════════════════
// S88 — baseline batch9-tokens-guard mencakup SEMUA modul JS statis
// ════════════════════════════════════════════════════════════════════════

const B9_GUARD_PATH = path.join(WEBUI_ROOT, 'static', 'js', 'uiux-batch9-tokens-guard.test.mjs');

test('S88 (statik): settings-system-apps.js punya entri baseline rgba (aktual 2)', () => {
    const guard = fs.readFileSync(B9_GUARD_PATH, 'utf8');
    assert.match(guard, /'settings-system-apps\.js'\s*:\s*\{[^}]*rgba:\s*\d+/,
        'modul settings yang sedang aktif dikembangkan justru tanpa radar — rgba literalnya (empty-state render) bisa naik tanpa alarm');
});

test('S88 (statik): users/general/packages/admin-core punya entri baseline eksplisit (=0)', () => {
    const guard = fs.readFileSync(B9_GUARD_PATH, 'utf8');
    for (const f of ['settings-users.js', 'settings-general.js', 'settings-packages.js', 'admin-core.js']) {
        assert.match(guard, new RegExp(`'${f.replace('.', '\\.')}'\\s*:\\s*\\{`),
            `${f} tanpa entri BASELINES — tambahkan (nilai aktual saat ini 0) supaya penambahan warna hardcoded pertama langsung memerah test`);
    }
});

test('S88 (statik): baseline system-apps rgba == angka terukur aktual', () => {
    const guard = fs.readFileSync(B9_GUARD_PATH, 'utf8');
    const declared = Number(guard.match(/'settings-system-apps\.js'\s*:\s*\{[^}]*rgba:\s*(\d+)/)?.[1] ?? NaN);
    const src = fs.readFileSync(path.join(WEBUI_ROOT, 'static', 'js', 'settings-system-apps.js'), 'utf8');
    const actual = (src.match(/rgba\(\s*[0-9]/g) || []).length;
    assert.equal(actual, declared,
        `rgba system-apps aktual ${actual}, baseline ${declared} — kontrak plafon = angka terukur`);
});

// ════════════════════════════════════════════════════════════════════════
// R95 — cap hex settings-vouchers.js dikunci ke angka aktual terukur
// ════════════════════════════════════════════════════════════════════════

test('R95 (statik): cap hex vouchers == angka terukur aktual (tanpa headroom diam-diam)', () => {
    const guard = fs.readFileSync(B9_GUARD_PATH, 'utf8');
    const declared = Number(guard.match(/'settings-vouchers\.js'\s*:\s*\{[^}]*hex:\s*(\d+)/)?.[1] ?? NaN);
    const src = fs.readFileSync(path.join(WEBUI_ROOT, 'static', 'js', 'settings-vouchers.js'), 'utf8');
    const actual = (src.match(/#[0-9a-fA-F]{3,8}\b/g) || []).length;
    assert.ok(Number.isFinite(declared), 'entri hex settings-vouchers.js ada di BASELINES');
    assert.equal(actual, declared,
        `hex vouchers aktual ${actual}, cap ${declared} — migrasikan sisa literal (#fca5a5×3 → danger-light, dst.) lalu kunci cap ke angka baru (kontrak "dikunci aktual" Batch 13)`);
});
