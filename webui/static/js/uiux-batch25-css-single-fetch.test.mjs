/* Contract tests untuk Batch 25 — double-fetch theme.css (review_web_flow_dan_dead_code.md,
 * Bagian 5.4 #2 / 6.5 #14).
 *
 * theme.css di-fetch DUA kali per halaman lewat dua URL berbeda untuk file
 * yang sama → cache browser tidak bisa dedup, theme.css (26KB+ font stack)
 * diunduh & diparse dua kali:
 *   (a) 5 halaman admin via partial head.html — link theme.css?v={{.version}}
 *       (head.html:8) PLUS admin-base.css yang @import url('theme.css')
 *       TANPA versi (admin-base.css:5);
 *   (b) hasil.html — link theme.css?v= (:23) PLUS hasil.css @import
 *       url('theme.css') (:1).
 * Keduanya satu-satunya @import di seluruh static/css. Semua konsumen sudah
 * me-link theme.css ter-versi secara langsung, jadi @import murni duplikasi.
 *
 * Kontrak:
 *   A25-1 — TIDAK ADA @import theme.css di static/css (mencegah regreasi
 *           lewat file baru); setiap konsumen wajib me-link langsung.
 *   A25-2 — Wiring konsumen: head.html me-link theme.css?v= SEBELUM
 *           admin-base.css?v=; hasil.html me-link theme.css?v= SEBELUM
 *           hasil.css?v= (sumber token selalu ikut ter-versi).
 *
 * Run with: node --test static/js/uiux-batch25-css-single-fetch.test.mjs (from webui/)
 */

import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const HERE = path.dirname(fileURLToPath(import.meta.url));
const WEBUI_ROOT = path.join(HERE, '..', '..');
const read = (...p) => fs.readFileSync(path.join(...p), 'utf8');

test('A25-1: tidak ada @import theme.css di static/css — konsumen link langsung (?v=)', () => {
    const cssDir = path.join(WEBUI_ROOT, 'static', 'css');
    const offenders = [];
    const walk = (dir) => {
        for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
            const p = path.join(dir, e.name);
            if (e.isDirectory()) walk(p);
            else if (e.name.endsWith('.css') && /@import\s+url\(\s*['"]?theme\.css/.test(read(p))) {
                offenders.push(path.relative(WEBUI_ROOT, p));
            }
        }
    };
    walk(cssDir);
    assert.deepEqual(offenders, [],
        '@import theme.css menduplikasi fetch file yang sudah di-link ter-versi ' +
        '(double-fetch, cache tidak bisa dedup): ' + offenders.join(', '));
});

test('A25-2: wiring konsumen — theme.css?v= di-link sebelum stylesheet dependennya', () => {
    const head = read(WEBUI_ROOT, 'templates', 'admin', 'partials', 'head.html');
    const hasil = read(WEBUI_ROOT, 'templates', 'public', 'hasil.html');

    for (const [name, src, dep] of [
        ['head.html', head, 'admin-base.css'],
        ['hasil.html', hasil, 'hasil.css'],
    ]) {
        const links = [...src.matchAll(/<link rel="stylesheet" href="([^"]+)">/g)].map((m) => m[1]);
        const themeIdx = links.findIndex((h) => /theme\.css\?v=/.test(h));
        const depIdx = links.findIndex((h) => h.includes(dep));
        assert.ok(themeIdx !== -1, `${name} wajib me-link theme.css?v= langsung`);
        assert.ok(depIdx !== -1, `${name} wajib me-link ${dep}`);
        assert.ok(themeIdx < depIdx,
            `${name}: theme.css?v= wajib sebelum ${dep} (sumber token ikut ter-versi)`);
    }
});
