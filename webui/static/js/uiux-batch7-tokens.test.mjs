/* Guard folder-wide Batch 7 — fase 2 design token (lanjutan S15).
 *
 * Latar belakang & dampak bisnis:
 *   Re-review ronde 2 mencatat medan fase 2: template memuat ±290 hex + ±430
 *   rgba inline dan JS ±49 hex. Batch 7 menurunkan angka itu lewat token
 *   (--rgb-*), kelas tone (.tone-*), dan .notice-warning. Test ini MENGUNCI
 *   baseline hasil migrasi per folder/file: angka tidak boleh NAIK lagi.
 *   Sejak S43 (re-review ronde 3), rgba literal diukur dengan regex
 *   digit-pembuka /rgba\(\s*[0-9]/ — pemakaian token rgba(var(--rgb-*), α)
 *   TIDAK dihitung sebagai literal (regex lama /rgba\(/ menghitungnya keliru,
 *   membuat dev bisa menambah ±295 literal baru tanpa test merah).
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
// S43: hanya rgba dengan DIGIT pembuka yang dihitung sebagai literal sungguhan.
// `rgba(var(--rgb-white), 0.1)` adalah PEMAKAIAN token — tidak boleh dihitung.
// Batch 15 (R105): flag /i — bentuk KAPITAL "RGBA(255,…)" tidak boleh lolos
// counter cukup dengan mengubah case.
const RGBA_RE = /rgba\(\s*[0-9]/gi;

/** Hitung kemunculan pola di satu file, baris komentar HTML tidak dikecualikan
 *  (baseline dikunci apa adanya — konsistensi lebih penting daripada presisi). */
function countIn(file, re) {
    const src = fs.readFileSync(file, 'utf8');
    return (src.match(re) || []).length;
}

function countFolder(re) {
    return listFiles(TEMPLATES).reduce((sum, f) => sum + countIn(f, re), 0);
}

// Self-test regex S43: pemakaian token rgba(var(--rgb-*), α) BUKAN literal.
test('S43 (self-test): regex rgba literal tidak menghitung rgba(var( sebagai literal', () => {
    assert.equal(('rgba(var(--rgb-white), 0.1)'.match(RGBA_RE) || []).length, 0,
        'rgba(var(...) harus dianggap pemakaian token, bukan literal');
    assert.equal(('rgba(var(--rgb-black),var(--alpha-bg))'.match(RGBA_RE) || []).length, 0);
    assert.equal(('rgba(255,255,255,0.1)'.match(RGBA_RE) || []).length, 1,
        'rgba digit pembuka adalah literal sungguhan dan harus terhitung');
    assert.equal((' rgba( 16, 185, 129, 0.2)'.match(RGBA_RE) || []).length, 1,
        'spasi setelah rgba( tetap terdeteksi sebagai literal');
});

// Baseline terkunci pasca-Batch 7 (4 agen paralel: core/dashboard/pengawasan/
// settings); rgba dikunci ulang pasca-S43 dengan regex literal-digit — angka
// lama (520) menghitung ±295 pemakaian token rgba(var(...)) secara keliru.
// Angka = hasil ukur langsung; jangan dinaikkan tanpa alasan terdokumentasi.
test('S15 fase 2 (guard): total hex literal di seluruh templates/ tidak naik dari baseline Batch 7', () => {
    const total = countFolder(HEX_RE);
    // Batch 12 (R70): plafon diperketat ke baseline aktual; Batch 13 (S68-
    // lanjutan/R82) menurunkannya lagi (migrasi #818cf8/#f43f5e) -> 130.
    // Batch 17: dikunci aktual (99) pasca migrasi token R125-sisa/R137.
    assert.ok(total <= 99,
        `total hex templates/ = ${total}, baseline terkunci ≤ 99 — pakai var(--token) untuk warna baru`);
});

test('S15 fase 2/S43 (guard): total rgba LITERAL (digit pembuka) di seluruh templates/ tidak naik', () => {
    const total = countFolder(RGBA_RE);
    // Batch 12 (R70): baseline aktual 140 (pasca migrasi S58/Batch 11-12).
    // Batch 17: dikunci aktual (89) pasca migrasi muted R125-sisa.
    assert.ok(total <= 89,
        `total rgba literal templates/ = ${total}, baseline terkunci ≤ 89 — pakai rgba(var(--rgb-*), α) / --glass-bg-strong`);
});

// Plafon per-file rgba literal (hasil ukur S43, regex digit-pembuka).
// Batch 15 (S95, kontrak ronde 5 "plafon = aktual"): seluruh entri DIKUNCI
// ULANG ke hasil ukur kondisi sumber sekarang (pasca migrasi S80–S83 agen
// lain) dan asersi diubah assert.ok(≤) → assert.equal. Lama → baru:
//   dashboard 32→32 · register_confirm 19→0 · pengawas 11→3 ·
//   pengawas_detail 11→11 · download 11→11 · hasil 10→0 · nav 9→8 ·
//   reset_password 8→0 · shared 4→3.
const RGBA_BASELINE_PER_FILE = {
    // Batch 13 (S71): entri 'admin/settings.html' DIHAPUS — baseline terduplikasi
    // antar-suite. Plafon rgba settings.html (≤28, aktual) kini DIJAGA SATU
    // TEMPAT: uiux-batch11-settings-guard.test.mjs (S64).
    'admin/dashboard.html': 29, /* Batch 16/R125: muted → token rgb-triplet */
    'public/register_confirm.html': 0,
    'admin/pengawas.html': 3,
    'admin/pengawas_detail.html': 7 /* Batch 17/R125-sisa */,
    'public/download.html': 11,
    'public/hasil.html': 0,
    'admin/partials/nav.html': 8,
    'public/reset_password.html': 0,
    'public/shared.html': 3,
};

for (const [rel, cap] of Object.entries(RGBA_BASELINE_PER_FILE)) {
    test(`S43/S95 (guard): rgba literal di templates/${rel} == baseline aktual`, () => {
        const n = countIn(path.join(TEMPLATES, rel), RGBA_RE);
        assert.equal(n, cap,
            `rgba literal ${rel} = ${n}, baseline terkunci tepat ${cap} — pakai rgba(var(--rgb-*), α); ` +
            'turunkan baseline ini setiap migrasi mengurangi literal (jangan naikkan)');
    });
}

test('S15 fase 2 (guard): hex di admin.js tidak naik dari baseline Batch 7', () => {
    const src = fs.readFileSync(path.join(__dirname, 'admin.js'), 'utf8');
    const n = (src.match(HEX_RE) || []).length;
    assert.ok(n <= 8, `hex admin.js = ${n}, baseline ≤ 8`);
});

// Plafon CSS inti (Batch 10 / S58): admin-base.css dimigrasi substitusi-persis
// rgba literal → rgba(var(--rgb-info|danger|success|warning), α) sehingga
// hitungan literal turun 60 → 17 (43 literal hilang). Plafon DIKENALI di angka
// BARU ini — fitur berikutnya wajib memakai token; sisa 17 literal memang
// belum punya pasangan triplet (slate/amber-400/red-400/slate-400/gray-500/
// rgb latar 10,10,x). Detail guard nilai-token ada di
// uiux-batch10-tokens-guard.test.mjs.
const ADMIN_BASE_CSS = path.join(__dirname, '..', 'css', 'admin-base.css');
test('B10/S58 (guard): rgba literal di css/admin-base.css tidak naik dari baseline pasca-migrasi', () => {
    const src = fs.readFileSync(ADMIN_BASE_CSS, 'utf8').replace(/\/\*[\s\S]*?\*\//g, '');
    const n = (src.match(RGBA_RE) || []).length;
    assert.ok(n <= 17,
        `rgba literal admin-base.css = ${n}, baseline pasca-S58 ≤ 17 — pakai rgba(var(--rgb-*), α)`);
});
