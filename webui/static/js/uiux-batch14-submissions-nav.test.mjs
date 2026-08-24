/**
 * ══════════════════════════════════════════════════════════════════════════
 * Suite UI/UX BATCH 14 — SUBMISSIONS & NAV (agen batch14-submissions-nav)
 * ══════════════════════════════════════════════════════════════════════════
 *
 * Referensi: review_uiux_webui.md bagian 5.11 RE-REVIEW RONDE 8. Cakupan
 * temuan milik agen ini:
 *
 *   S87 — Label mobile `data-label` tidak sinkron dengan `th` desktop:
 *         submissions.html th "Nama"/"ID Perangkat" vs data-label
 *         "Siswa"/"Perangkat"; dashboard.html th "Nama Ujian" vs data-label
 *         "Ujian". Di ≤768px tabel berubah jadi kartu berlabel yang tak cocok
 *         dengan nama kolom versi desktop. Lebih berbahaya: CSS layout mobile
 *         MENGGANTUNG pada label tak sinkron (submissions.html:56
 *         td[data-label="Siswa"]) — mengubah label tanpa sadar merusak kartu.
 *         Kontrak: (a) set data-label == set teks th per tabel; (b) setiap
 *         selector td[data-label="X"] di <style> halaman punya pasangan
 *         atribut di markup.
 *
 *   R89 — Inline-style arwah pada elemen .sr-only: utility .sr-only sudah
 *         terdefinisi (tailwind output.css:61, clip-pattern ekuivalen), tetapi
 *         enam lokasi masih menempel style inline — dua gaya berbeda untuk
 *         tujuan sama membuktikan class-nya sendiri tidak dipercaya (pola
 *         perbaikan sama dengan R84 skip-link).
 *         Kontrak: folder-wide templates/** — elemen ber-class sr-only bebas
 *         atribut style.
 *
 * Kepemilikan file agen ini: templates/admin/submissions.html,
 *   templates/admin/dashboard.html, seluruh templates/** (sweep sr-only).
 *
 * Run with:  node --test static/js/uiux-batch14-submissions-nav.test.mjs   (from webui/)
 */

import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const HERE = path.dirname(fileURLToPath(import.meta.url));
const WEBUI_ROOT = path.join(HERE, '..', '..');
const TEMPLATES = path.join(WEBUI_ROOT, 'templates');
const read = (...p) => fs.readFileSync(path.join(...p), 'utf8');

const SUBMISSIONS = read(TEMPLATES, 'admin', 'submissions.html');
const DASHBOARD = read(TEMPLATES, 'admin', 'dashboard.html');

function walk(dir, acc = []) {
    for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
        const full = path.join(dir, entry.name);
        if (entry.isDirectory()) walk(full, acc);
        else if (entry.name.endsWith('.html')) acc.push(full);
    }
    return acc;
}

/** Normalisasi teks kolom: buang tag, entity umum, spasi ganda, kapitalisasi. */
const norm = (s) =>
    s.replace(/<[^>]+>/g, '')
        .replace(/&amp;/g, '&').replace(/&nbsp;/g, ' ')
        .replace(/\s+/g, ' ')
        .trim()
        .toLowerCase();

// ════════════════════════════════════════════════════════════════════════
// S87 — paritas th ↔ data-label per tabel + selector CSS tercakup markup
// ════════════════════════════════════════════════════════════════════════

/**
 * Kembalikan daftar tabel { thSet, labelSet } dari satu dokumen HTML.
 * Asumsi struktur repo: satu <table> utama data per halaman; test ini bekerja
 * pada SEMUA tabel agar aman terhadap penambahan tabel baru.
 */
function analyzeTables(html) {
    return [...html.matchAll(/<table\b[\s\S]*?<\/table>/g)].map((m) => {
        const tableHtml = m[0];
        const thSet = new Set(
            [...tableHtml.matchAll(/<th\b[^>]*>([\s\S]*?)<\/th>/g)]
                .map((t) => norm(t[1]))
                .filter(Boolean),
        );
        const labelSet = new Set(
            [...tableHtml.matchAll(/data-label="([^"]+)"/g)].map((l) => norm(l[1])),
        );
        return { tableHtml, thSet, labelSet };
    });
}

for (const [name, html] of [['submissions.html', SUBMISSIONS], ['dashboard.html', DASHBOARD]]) {
    test(`S87 (statik): ${name} — setiap data-label td punya pasangan th desktop (label kartu mobile == nama kolom)`, () => {
        const tables = analyzeTables(html);
        assert.ok(tables.some((t) => t.thSet.size > 0 && t.labelSet.size > 0),
            `${name} punya tabel data dengan th dan data-label (penjaga asumsi)`);
        for (let i = 0; i < tables.length; i++) {
            const { thSet, labelSet } = tables[i];
            if (labelSet.size === 0) continue; // tabel tanpa mode kartu mobile
            for (const label of labelSet) {
                assert.ok(thSet.has(label),
                    `tabel #${i + 1} ${name}: data-label "${label}" tidak ada padanan th — kartu mobile berlabel yang tak cocok dengan kolom desktop; samakan string kedua sisi`);
            }
        }
    });

    test(`S87 (statik): ${name} — setiap th data-table punya cabang mobile atau sebaliknya tidak dipakai selector CSS yatim`, () => {
        // Selector CSS dalam <style> halaman wajib punya pasangan atribut di markup.
        const styleBlocks = [...html.matchAll(/<style[\s\S]*?<\/style>/g)].map((m) => m[0]).join('\n');
        const selectors = [...styleBlocks.matchAll(/td\[data-label="([^"]+)"\]/g)].map((m) => m[1]);
        const markupLabels = [...html.matchAll(/data-label="([^"]+)"/g)].map((m) => norm(m[1]));
        for (const sel of selectors) {
            assert.ok(markupLabels.includes(norm(sel)),
                `selector td[data-label="${sel}"] yatim — label di markup sudah/selalu berbeda; layout kartu mobile ini MATI tanpa terlihat. Perbarui selector bersama penggantian label (S87)`);
        }
    });
}

// ════════════════════════════════════════════════════════════════════════
// R89 — elemen .sr-only bebas inline style (folder-wide)
// ════════════════════════════════════════════════════════════════════════

test('R89 (guard folder-wide): elemen ber-class sr-only tidak memakai atribut style inline', () => {
    const files = walk(TEMPLATES);
    const violations = [];
    for (const f of files) {
        const src = read(f);
        // Tag pembuka dengan class sr-only DAN atribut style pada tag yang sama.
        const re = /<[^>]*class="[^"]*\bsr-only\b[^"]*"[^>]*>/g;
        let m;
        while ((m = re.exec(src)) !== null) {
            if (/\sstyle=/.test(m[0])) {
                violations.push(`${path.relative(TEMPLATES, f)}: ${m[0].slice(0, 100)}…`);
            }
        }
    }
    assert.equal(violations.length, 0,
        `${violations.length} elemen sr-only masih bawa inline-style arwah (dua gaya clip vs left:-9999px) — utility .sr-only output.css sudah ekuivalen; hapus semua style:\n${violations.join('\n')}`);
});

test('R89 (statik): utility .sr-only benar-benar tersedia di CSS build (penjaga kontrak)', () => {
    // Jika suatu saat utility dihapus dari build, guard R89 di atas menjadi
    // jebakan: elemen kehilangan styling visually-hidden sepenuhnya.
    const cssPath = path.join(WEBUI_ROOT, 'static', 'css', 'tailwind', 'output.css');
    const css = fs.readFileSync(cssPath, 'utf8');
    assert.match(css, /\.sr-only\s*\{[^}]*position:\s*absolute/,
        '.sr-only harus tetap terdefinisi sebagai visually-hidden utility');
});
