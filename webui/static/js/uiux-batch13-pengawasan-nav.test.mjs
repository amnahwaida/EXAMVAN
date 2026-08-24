/**
 * Suite UI/UX Batch 13 — Pengawasan & Nav (agen batch-13-pengawasan-nav)
 *
 * Cakupan temuan:
 *   R78 — inline `padding:` pada string render-JS .pd-action-btn menimpa
 *         stylesheet fix R76; padding dihapus dari render antrean & peserta.
 *   R81 — chevron afordansi baris pengawas.html #4f46e5 (2.60–2.91:1,
 *         < ambang non-teks 3:1) → var(--color-primary-light) (#a5b4fc).
 *   R84 — skip-link login admin masih inline-style arwah → class .skip-link
 *         theme.css saja.
 *   R85 — input username login admin tanpa triplet autocapitalize/
 *         autocorrect/spellcheck (pola wajib R36, register.html:261).
 *
 * Kepemilikan file agen ini:
 *   admin/partials/nav.html, head.html, login.html, pengawas.html,
 *   pengawas_detail.html, submissions.html
 */

import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';

const HERE = dirname(fileURLToPath(import.meta.url));
const ROOT = join(HERE, '..', '..', 'templates');
const CSS = join(HERE, '..', 'css');

const read = (...p) => readFileSync(join(...p), 'utf8');

const DETAIL = () => read(ROOT, 'admin', 'pengawas_detail.html');
const PENGAWAS = () => read(ROOT, 'admin', 'pengawas.html');
const LOGIN = () => read(ROOT, 'admin', 'login.html');
const THEME = () => read(CSS, 'theme.css');

// ---------------------------------------------------------------------------
// R78 — string render-JS .pd-action-btn bebas inline padding
// ---------------------------------------------------------------------------

/** Ekstrak atribut style dari setiap tag <button class="pd-action-btn …">. */
function pdActionBtnStyles(html) {
    const styles = [];
    const re = /<button class="pd-action-btn[^"]*"[^>]*?style="([^"]*)"/g;
    let m;
    while ((m = re.exec(html)) !== null) styles.push(m[1]);
    return styles;
}

test('R78 (statik): ketiga tombol .pd-action-btn render-JS di pengawas_detail.html ditemukan', () => {
    // 2 di render antrean persetujuan + 1 di tabel peserta (Tolak)
    const tags = DETAIL().match(/<button class="pd-action-btn/g) || [];
    assert.ok(tags.length >= 3,
        `minimal 3 tombol pd-action-btn render-JS (dapat ${tags.length})`);
});

test('R78 (statik): style inline .pd-action-btn render-JS bebas deklarasi padding:', () => {
    const styles = pdActionBtnStyles(DETAIL());
    assert.ok(styles.length >= 3, 'tag dengan atribut style tetap ada');
    for (const s of styles) {
        assert.doesNotMatch(s, /(^|[;\s])padding\s*:/i,
            `inline padding pada pd-action-btn menimpa stylesheet fix R76 — hapus dari string render (style kini: "${s}")`);
    }
});

// ---------------------------------------------------------------------------
// R81 — chevron afordansi baris pakai token primary-light, bukan #4f46e5
// ---------------------------------------------------------------------------

test('R81 (statik): pengawas.html bebas #4f46e5 sebagai warna svg/teks — chevron pakai var(--color-primary-light)', () => {
    const html = PENGAWAS();
    // ambil setiap pemakaian <use href="#hi-chevron-right"> (bukan definisi symbol)
    const uses = [...html.matchAll(/<use href="#hi-chevron-right"/g)];
    assert.ok(uses.length >= 1, 'svg chevron afordansi baris ada');
    let sawToken = false;
    for (const u of uses) {
        const ctx = html.slice(Math.max(0, u.index - 220), u.index);
        assert.doesNotMatch(ctx, /#4f46e5/i,
            '#4f46e5 (2.60–2.91:1) gagal ambang non-teks 3:1');
        if (/var\(--color-primary-light\)/.test(ctx)) sawToken = true;
    }
    assert.ok(sawToken,
        'chevron harus memakai var(--color-primary-light) (#a5b4fc, 8.19+:1)');
    // guard umum: tak ada lagi color:#4f46e5 pada elemen svg/teks di file ini
    assert.doesNotMatch(html, /color:\s*#4f46e5/i,
        '#4f46e5 tidak boleh dipakai sebagai color= (endpoint gradien background di file lain bukan cakupan ini)');
});

// ---------------------------------------------------------------------------
// R84 — skip-link login admin tanpa inline style (class theme.css cukup)
// ---------------------------------------------------------------------------

test('R84 (statik): anchor skip-link login.html tanpa atribut style inline', () => {
    const html = LOGIN();
    const anchor = html.match(/<a href="#main-content" class="skip-link"[^>]*>/);
    assert.ok(anchor, 'login.html wajib punya <a href="#main-content" class="skip-link">');
    assert.doesNotMatch(anchor[0], /\sstyle=/,
        'skip-link arwah inline (position:absolute;left:-9999px;…) menimpa theme.css — hapus seluruh style');
});

test('R84 (statik): base styling .skip-link tersedia di theme.css dengan reveal-on-focus', () => {
    const css = THEME();
    const base = css.match(/\.skip-link\s*\{[^}]*\}/);
    assert.ok(base, 'theme.css wajib punya rule .skip-link dasar');
    assert.match(css, /\.skip-link:focus/, 'reveal-on-focus .skip-link harus ada');
});

// ---------------------------------------------------------------------------
// R85 — input username login: triplet anti-kapital-otomatis mobile
// ---------------------------------------------------------------------------

test('R85 (statik): input username login.html memuat autocapitalize="none" autocorrect="off" spellcheck="false"', () => {
    const html = LOGIN();
    const input = html.match(/<input[^>]*id="username"[^>]*>/);
    assert.ok(input, 'input username ada');
    for (const attr of ['autocapitalize="none"', 'autocorrect="off"', 'spellcheck="false"']) {
        assert.ok(input[0].includes(attr),
            `${attr} wajib ada pada username login (pola register.html:261 / R36)`);
    }
});
