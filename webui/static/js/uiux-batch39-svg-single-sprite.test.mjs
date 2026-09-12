/* Contract tests untuk Batch 39 — 5.4#1: satu sumber sprite SVG.
 *
 *   5.4#1 (review_ui_halaman_web_2026-09-12.md, sensus lama "67 inline") —
 *   koreksi sensus: sebagian besar adalah blok sprite LOKAL per halaman
 *   (dashboard 30, pengawas 7, pengawas_detail 11, submissions 19 = 67
 *   definisi <symbol> duplikat), bukan 67 <svg> inline. Perbaikan:
 *   (a) keempat blok sprite lokal dihapus — partials/svg-symbols.html
 *       (dimuat via nav.html) satu-satunya sumber definisi simbol;
 *   (b) ikon stop pengawasan yang benar-benar inline ×3 (markup + 2 jalur
 *       restore JS) diangkat jadi simbol bersama hi-stop;
 *   (c) 3 glyph dashboard yang tersisa adalah ikon kustom one-off
 *       (bukan kembaran simbol) — dipertahankan.
 *
 * Run with: node --test static/js/uiux-batch39-svg-single-sprite.test.mjs (from webui/)
 */

import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const HERE = path.dirname(fileURLToPath(import.meta.url));
const WEBUI_ROOT = path.join(HERE, '..', '..');
const read = (...p) => fs.readFileSync(path.join(...p), 'utf8');

const SPRITE = read(WEBUI_ROOT, 'templates', 'admin', 'partials', 'svg-symbols.html');
const PAGES = ['dashboard', 'pengawas', 'pengawas_detail', 'submissions']
    .map((n) => read(WEBUI_ROOT, 'templates', 'admin', `${n}.html`));

test('5.4#1a: svg-symbols.html satu-satunya file yang mendefinisikan <symbol>', () => {
    const walk = (dir, acc = []) => {
        for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
            const p = path.join(dir, e.name);
            if (e.isDirectory()) walk(p, acc);
            else if (e.name.endsWith('.html')) acc.push(p);
        }
        return acc;
    };
    const offenders = walk(path.join(WEBUI_ROOT, 'templates'))
        .filter((f) => !f.endsWith('svg-symbols.html'))
        .filter((f) => read(f).includes('<symbol'));
    assert.deepEqual(offenders, [],
        'definisi <symbol> muncul lagi di luar partial sprite — perbaikan ikon akan dikerjakan dua kali (drift visual)');
});

test('5.4#1a: keempat halaman eks-sprite memuat nav.html (sumber sprite via partial)', () => {
    for (const [i, name] of ['dashboard', 'pengawas', 'pengawas_detail', 'submissions'].entries()) {
        assert.match(PAGES[i], /\{\{\s*template\s+"admin\/partials\/nav\.html"/,
            `${name}.html tidak memuat nav.html — <use> di halaman itu akan mati`);
    }
});

test('5.4#1a: semua <use href="#..."> di keempat halaman resolve ke simbol sprite', () => {
    const defined = new Set([...SPRITE.matchAll(/<symbol id="(hi-[a-z-]+)"/g)].map((m) => m[1]));
    for (const [i, name] of ['dashboard', 'pengawas', 'pengawas_detail', 'submissions'].entries()) {
        const uses = [...PAGES[i].matchAll(/<use href="#(hi-[a-z-]+)"/g)].map((m) => m[1]);
        assert.ok(uses.length >= 10, `${name}.html masih memakai sprite (prasyarat kontrak)`);
        const missing = [...new Set(uses)].filter((u) => !defined.has(u));
        assert.deepEqual(missing, [], `${name}.html merujuk simbol yang tak terdefinisi: ${missing.join(', ')}`);
    }
});

test('5.4#1b: simbol hi-stop ada di sprite; ikon stop inline rect duplikat hilang', () => {
    assert.match(SPRITE, /<symbol id="hi-stop"[^>]*><rect x="6" y="6" width="12" height="12" rx="2"\/><\/symbol>/,
        'simbol hi-stop wajib ada di sprite bersama');
    const detail = read(WEBUI_ROOT, 'templates', 'admin', 'pengawas_detail.html');
    assert.equal(
        (detail.match(/<rect x="6" y="6" width="12" height="12" rx="2"\/>/g) || []).length, 0,
        'ikon stop masih inline (markup/JS) — pakai <use href="#hi-stop"> agar konsisten satu sumber');
    assert.match(detail, /<use href="#hi-stop"\/>/, 'pengawas_detail memakai hi-stop via <use>');
});
