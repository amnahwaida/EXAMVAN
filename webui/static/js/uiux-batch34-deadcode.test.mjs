/* Contract tests untuk Batch 34 — dead code (review L72, L73, L75).
 *
 *   L72 — token theme.css tanpa konsumen. KOREKSI census review: dari 3 token
 *         yang dilaporkan mati, 2 ternyata DIPAKAI produksi oleh
 *         tailwind/output.css (artefak build yang di-load head.html/login —
 *         var(--color-surface-hover) :667, var(--radius-xl) :591/dst.),
 *         jadi keduanya DIADOPTI (dipertahankan + didokumentasikan).
 *         Hanya --color-primary-dark yang benar-benar mati → dihapus.
 *   L73 — ±13 kelas mati admin-base.css → dihapus + selector print-rule
 *         dibersihkan. Efek kaskade: token --z-bottom-bar & --z-hint kehilangan
 *         konsumen terakhirnya (kelas .mobile-bottom-bar & .shortcuts-hint)
 *         dan ikut dihapus dari theme.css.
 *   L75 — 5 simbol sprite tanpa referensi runtime → dihapus (sprite 45 → 40).
 *
 * Run with: node --test static/js/uiux-batch34-deadcode.test.mjs (from webui/)
 */

import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const HERE = path.dirname(fileURLToPath(import.meta.url));
const WEBUI_ROOT = path.join(HERE, '..', '..');
const read = (...p) => fs.readFileSync(path.join(...p), 'utf8');

const THEME = read(WEBUI_ROOT, 'static', 'css', 'theme.css');
const ADMIN_BASE = read(WEBUI_ROOT, 'static', 'css', 'admin-base.css');
const SPRITE = read(WEBUI_ROOT, 'templates', 'admin', 'partials', 'svg-symbols.html');

// ---------------------------------------------------------------------------
// L72 — token mati vs token ter-adopt
// ---------------------------------------------------------------------------

test('L72a: --color-primary-dark (mati sesungguhnya) dihapus dari theme.css', () => {
    assert.doesNotMatch(THEME, /--color-primary-dark\s*:/,
        'token tanpa konsumen dihapus — hidupkan kembali dari git history bila dibutuhkan');
    // Zero konsumen di seluruh sumber (bukan hanya tema):
    const walk = (dir, acc = []) => {
        for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
            const p = path.join(dir, e.name);
            if (e.isDirectory()) walk(p, acc);
            else acc.push(p);
        }
        return acc;
    };
    const sources = [
        ...walk(path.join(WEBUI_ROOT, 'templates')),
        ...walk(path.join(WEBUI_ROOT, 'static')).filter((f) => !f.includes('tailwind') && !f.includes('test')),
    ];
    const users = sources.filter((f) => read(f).includes('var(--color-primary-dark'));
    assert.deepEqual(users, [], 'tidak boleh ada konsumen token yang didefinisikan ulang');
});

test('L72b: --color-surface-hover & --radius-xl DIADOPTI — dipakai output.css (produksi)', () => {
    // KOREKSI census L72: keduanya TIDAK mati. output.css (artefak build yang
    // dimuat head.html + login.html) memakai keduanya. Migrasi keduanya ke
    // output.css build di masa depan boleh — hapus token HANYA setelah itu.
    assert.match(THEME, /--color-surface-hover\s*:/, 'dipertahankan (konsumen output.css :667)');
    assert.match(THEME, /--radius-xl\s*:/, 'dipertahankan (konsumen output.css :591 dst.)');
    const out = read(WEBUI_ROOT, 'static', 'css', 'tailwind', 'output.css');
    assert.match(out, /var\(--color-surface-hover\)/, 'bukti konsumsi produksi');
    assert.match(out, /var\(--radius-xl\)/, 'bukti konsumsi produksi');
});

// ---------------------------------------------------------------------------
// L73 — kelas mati admin-base.css
// ---------------------------------------------------------------------------

test('L73: kelas mati admin-base.css dihapus (census: 0 pemakaian templates/JS)', () => {
    for (const cls of ['skeleton-inline', 'shortcuts-hint', 'sticky-toolbar',
                       'circular-progress', 'mobile-bottom-bar', 'clickable-row',
                       'tone-danger', 'tone-accent', 'shortcut-row', 'shortcut-key',
                       'bottom-bar-count', 'bottom-bar-actions']) {
        assert.doesNotMatch(ADMIN_BASE, new RegExp(`\\.${cls.replace(/-/g, '\\-')}\\b`),
            `.${cls} seharusnya sudah dihapus (census 0 pemakaian)`);
    }
    // Keluarga tone yang DIPAKAI runtime tetap utuh:
    for (const cls of ['tone-success', 'tone-warning', 'tone-info', 'tone-neutral']) {
        assert.match(ADMIN_BASE, new RegExp(`\\.${cls}\\s*\\{`), `.${cls} tetap terdefinisi`);
    }
    // Print-rule tidak lagi menyebut kelas yang dihapus:
    const printBlock = ADMIN_BASE.slice(ADMIN_BASE.indexOf('@media print'));
    assert.doesNotMatch(printBlock, /mobile-bottom-bar|shortcuts-hint/,
        'selector print wajib dibersihkan dari kelas mati');
});

test('L73-kaskade: token z tanpa konsumen turut dihapus dari theme.css', () => {
    assert.doesNotMatch(THEME, /--z-bottom-bar\s*:/, 'konsumen terakhir (.mobile-bottom-bar) dihapus');
    assert.doesNotMatch(THEME, /--z-hint\s*:/, 'konsumen terakhir (.shortcuts-hint) dihapus');
    // Token z yang masih hidup tetap ada:
    for (const tok of ['--z-skip-link', '--z-dropdown', '--z-modal-overlay',
                       '--z-onboarding', '--z-topbar-floating', '--z-toast']) {
        assert.match(THEME, new RegExp(`${tok.replace(/-/g, '\\-')}\\s*:`), `${tok} tetap terdefinisi`);
    }
});

// ---------------------------------------------------------------------------
// L75 — simbol sprite mati
// ---------------------------------------------------------------------------

test('L75: 5 simbol sprite tanpa referensi runtime dihapus (45 → 40)', () => {
    for (const id of ['hi-copy', 'hi-arrow-up', 'hi-banknotes', 'hi-x-circle', 'hi-check-circle']) {
        assert.doesNotMatch(SPRITE, new RegExp(`id="${id}"`),
            `#${id} census 0 referensi — dihapus; kembalikan dari git bila dipakai lagi`);
    }
    const defined = [...SPRITE.matchAll(/<symbol id="(hi-[a-z-]+)"/g)].map((m) => m[1]);
    assert.equal(defined.length, 40, 'sprite kini 40 simbol');
    // Setiap simbol tersisa ter-referensi (nol mati baru) — census runtime:
    const walk = (dir, acc = []) => {
        for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
            const p = path.join(dir, e.name);
            if (e.isDirectory()) walk(p, acc);
            else if (/\.(html|js)$/.test(e.name)) acc.push(p);
        }
        return acc;
    };
    const sources = walk(path.join(WEBUI_ROOT, 'templates'))
        .concat(walk(path.join(WEBUI_ROOT, 'static', 'js')).filter((f) => !f.includes('test')));
    const referenced = new Set();
    for (const f of sources) {
        for (const m of read(f).matchAll(/#(hi-[a-z-]+)/g)) referenced.add(m[1]);
    }
    const dead = defined.filter((id) => !referenced.has(id));
    assert.deepEqual(dead, [], 'simbol tanpa referensi runtime muncul kembali: ' + dead.join(', '));
});
