/* Guard folder-wide Batch 7 — fase 2 design token (lanjutan S15).
 *
 * Latar belakang & dampak bisnis:
 *   Re-review ronde 2 mencatat medan fase 2: template memuat ±290 hex + ±430
 *   rgba inline dan JS ±49 hex. Batch 7 menurunkan angka itu lewat token
 *   (--rgb-*), kelas tone (.tone-*), dan .notice-warning. Test ini MENGUNCI
 *   baseline hasil migrasi per folder/file: angka tidak boleh NAIK lagi.
 *   Setiap fitur baru wajib memakai var(--token)/kelas utilitas; bila baseline
 *   memang perlu dinaikkan (mis. halaman baru dengan kebutuhan warna khusus),
 *   naikkan angkanya secara sadar di sini dengan komentar alasannya.
 *
 * Run with:  node --test static/js/uiux-batch7-tokens.test.mjs   (from webui/)
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const TEMPLATES = path.join(__dirname, '..', '..', 'templates');

function listFiles(dir) {
    return fs.readdirSync(dir, { recursive: true })
        .map((f) => path.join(dir, f))
        .filter((f) => f.endsWith('.html'));
}

const HEX_RE = /#[0-9a-fA-F]{3,8}\b/g;
const RGBA_RE = /rgba\(/g;

/** Hitung kemunculan pola di satu file, baris komentar HTML tidak dikecualikan
 *  (baseline dikunci apa adanya — konsistensi lebih penting daripada presisi). */
function countIn(file, re) {
    const src = fs.readFileSync(file, 'utf8');
    return (src.match(re) || []).length;
}

function countFolder(re) {
    return listFiles(TEMPLATES).reduce((sum, f) => sum + countIn(f, re), 0);
}

// Baseline terkunci pasca-Batch 7 (4 agen paralel: core/dashboard/pengawasan/
// settings). Angka = hasil ukur langsung setelah migrasi; jangan dinaikkan
// tanpa alasan terdokumentasi.
test('S15 fase 2 (guard): total hex literal di seluruh templates/ tidak naik dari baseline Batch 7', () => {
    const total = countFolder(HEX_RE);
    assert.ok(total <= 300,
        `total hex templates/ = ${total}, baseline terkunci ≤ 300 — pakai var(--token) untuk warna baru`);
});

test('S15 fase 2 (guard): total rgba( literal di seluruh templates/ tidak naik dari baseline Batch 7', () => {
    const total = countFolder(RGBA_RE);
    assert.ok(total <= 520,
        `total rgba( templates/ = ${total}, baseline terkunci ≤ 520 — pakai rgba(var(--rgb-*), α) / --glass-bg-strong`);
});

test('S15 fase 2 (guard): hex di admin.js tidak naik dari baseline Batch 7', () => {
    const src = fs.readFileSync(path.join(__dirname, 'admin.js'), 'utf8');
    const n = (src.match(HEX_RE) || []).length;
    assert.ok(n <= 8, `hex admin.js = ${n}, baseline ≤ 8`);
});
