/* Contract tests untuk Batch 24 — M9–M11: tiga token CSS tak terdefinisi di
 * dashboard.html (review_web_flow_dan_dead_code.md, Bagian 5.1 / 6.5 #12).
 *
 * M9  — var(--accent-light)  :459 → varian terdefinisi: --color-accent-light
 * M10 — var(--glass-border)  :693/:709 → --color-glass-border
 * M11 — var(--text-primary)  :697/:714 → --color-text
 *
 * Ketiga token dipakai dashboard tapi TIDAK terdefinisi di mana pun
 * (theme.css / admin-base.css / partials / blok <style> halaman) → deklarasi
 * styling diam-diam tidak berlaku (fallback browser). Dashboard adalah
 * satu-satunya pemakai ketiganya.
 *
 * Metode: audit statik fs-read — ekstrak seluruh pemakaian var(--*) dari
 * dashboard.html dan bandingkan terhadap seluruh definisi --* di theme.css
 * (satu sumber kebenaran token admin; dashboard mendefinisikan 0 token
 * lokal — dikunci oleh audit A24-3).
 *
 * Run with: node --test static/js/uiux-batch24-dashboard-tokens.test.mjs (from webui/)
 */

import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const HERE = path.dirname(fileURLToPath(import.meta.url));
const WEBUI_ROOT = path.join(HERE, '..', '..');
const read = (...p) => fs.readFileSync(path.join(...p), 'utf8');

const DASHBOARD = read(WEBUI_ROOT, 'templates', 'admin', 'dashboard.html');
const THEME_CSS = read(WEBUI_ROOT, 'static', 'css', 'theme.css');

/** Semua nama token yang DIDEFINISIKAN di theme.css (kata --x sebelum ':'). */
function definedTokens(css) {
    const defs = new Set();
    for (const m of css.matchAll(/(--[a-z0-9-]+)\s*:/g)) defs.add(m[1]);
    return defs;
}

/** Semua pemakaian var(--x) dalam sebuah sumber. */
function usedTokens(src) {
    const uses = new Map(); // token -> jumlah pemakaian
    for (const m of src.matchAll(/var\((--[a-z0-9-]+)\)/g)) {
        uses.set(m[1], (uses.get(m[1]) ?? 0) + 1);
    }
    return uses;
}

test('A24-1 (M9-M11): setiap var(--token) di dashboard.html terdefinisi di theme.css', () => {
    const defined = definedTokens(THEME_CSS);
    const used = usedTokens(DASHBOARD);
    const ghosts = [...used.keys()].filter((t) => !defined.has(t));
    assert.deepEqual(ghosts, [],
        `token tak terdefinisi dipakai dashboard (deklarasi diam-diam tidak berlaku): ` +
        ghosts.map((t) => `${t} ×${used.get(t)}`).join(', '));
});

test('A24-2: tiga token arwah M9-M11 tidak lagi muncul di dashboard.html', () => {
    for (const ghost of ['--accent-light', '--glass-border', '--text-primary']) {
        assert.ok(!DASHBOARD.includes(`var(${ghost})`),
            `${ghost} masih dipakai dashboard — wajib migrasi ke varian terdefinisi ` +
            `(--color-accent-light / --color-glass-border / --color-text, theme.css)`);
    }
});

test('A24-3: dashboard.html tidak mendefinisikan token lokal (theme.css = satu sumber)', () => {
    const pageStyle = DASHBOARD.match(/<style[^>]*>([\s\S]*?)<\/style>/);
    assert.ok(pageStyle, 'dashboard.html diharapkan punya satu blok <style> halaman');
    const localDefs = [...pageStyle[1].matchAll(/(--[a-z0-9-]+)\s*:/g)].map((m) => m[1]);
    assert.deepEqual(localDefs, [],
        'dashboard.html wajib tidak mendefinisikan ulang token — sumbernya theme.css');
});
