/* Contract tests untuk Batch 26 — sink/escape admin.js
 * (review_ui_halaman_web_2026-09-12.md, M22/M23/M25 + gap test R146).
 *
 *   M23 — anchor target="_blank" di admin.js TANPA rel="noopener"
 *         (reverse tabnabbing: halaman hasil bisa memanipulasi window.opener
 *         tab admin) + interpolasi token/examId mentah ke atribut href —
 *         satu-satunya pelanggar di repo. Test R146 lama (batch19:79-89)
 *         hanya memindai settings.html + dashboard.html, static/js luput.
 *   M22 — p.id di-interpolasi mentah ke innerHTML (admin.js:1122, :3064)
 *         padahal username/role di baris yang sama sudah di-escape.
 *   M25 — ekspor XML menyisipkan nilai tanpa escaping → soal berisi
 *         "Benar & Salah" menghasilkan XML invalid yang gagal diimpor balik
 *         (round-trip ekspor-impor rusak).
 *
 * Run with: node --test static/js/uiux-batch26-admin-sinks.test.mjs (from webui/)
 */

import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const HERE = path.dirname(fileURLToPath(import.meta.url));
const WEBUI_ROOT = path.join(HERE, '..', '..');
const read = (...p) => fs.readFileSync(path.join(...p), 'utf8');

const ADMIN_JS = read(WEBUI_ROOT, 'static', 'js', 'admin.js');

// ---------------------------------------------------------------------------
// M23 — target="_blank" di seluruh static/js wajib rel noopener
// ---------------------------------------------------------------------------

test('B26-1 (M23): semua anchor target="_blank" di static/js membawa rel noopener', () => {
    const jsDir = path.join(WEBUI_ROOT, 'static', 'js');
    const offenders = [];
    for (const e of fs.readdirSync(jsDir)) {
        if (!e.endsWith('.js')) continue;
        const src = read(jsDir, e);
        for (const tag of src.match(/<a [^>]*target="\\?"?_blank[^>]*>/g) || []) {
            if (!/rel="[^"]*noopener[^"]*"/.test(tag)) offenders.push(`${e}: ${tag.slice(0, 70)}…`);
        }
    }
    assert.deepEqual(offenders, [],
        'anchor _blank tanpa noopener = reverse tabnabbing (window.opener tab admin):\n' + offenders.join('\n'));
});

test('B26-2 (M23): anchor hasil siswa di admin.js meng-escape token & membawa noopener', () => {
    const anchor = ADMIN_JS.match(/<a href="\/hasil\/[^"\s]*"[^>]*>/);
    assert.ok(anchor, 'anchor /hasil harus ada (prasyarat kontrak)');
    assert.match(anchor[0], /rel="[^"]*noopener/,
        'anchor /hasil wajib rel noopener (reverse tabnabbing)');
    assert.match(anchor[0], /href="\/hasil\/\$\{escapeHtml\(token\)\}"/,
        'token wajib dibungkus escapeHtml sebelum interpolasi ke atribut href');
});

// ---------------------------------------------------------------------------
// M22 — p.id wajib di-escape sebelum masuk innerHTML
// ---------------------------------------------------------------------------

test('B26-3 (M22): p.id tidak pernah diinterpolasi mentah ke innerHTML', () => {
    const raw = [...ADMIN_JS.matchAll(/value="'\s*\+\s*p\.id\s*\+\s*'/g)];
    assert.deepEqual(raw, [],
        'p.id mentah di innerHTML = lubang siap-pakai; wajib escapeHtml(String(p.id))');
});

// ---------------------------------------------------------------------------
// M25 — ekspor XML wajib lewat xmlEscape
// ---------------------------------------------------------------------------

test('B26-4 (M25): admin.js mendefinisikan xmlEscape dan memakainya di ekspor XML', () => {
    assert.match(ADMIN_JS, /function xmlEscape\(/,
        'xmlEscape() wajib ada di admin.js');
    const xmlBlock = ADMIN_JS.slice(
        ADMIN_JS.indexOf('function exportXMLQuestions'),
        ADMIN_JS.indexOf('function exportXMLQuestions') + 4000);
    for (const field of ['partial_scoring', 'choices.join', '<key>${']) {
        const line = xmlBlock.split('\\n').find((l) => l.includes(field));
        assert.ok(line && /xmlEscape\(/.test(line),
            `field ${field} wajib dibungkus xmlEscape (round-trip import soal)`);
    }
});
