/**
 * ══════════════════════════════════════════════════════════════════════════
 * Suite UI/UX BATCH 17 — PENGAWASAN (gerbang item tertinggal Batch 16 +
 * ekor ronde 11; dieksekusi koordinator)
 * ══════════════════════════════════════════════════════════════════════════
 *
 * Referensi: review_uiux_webui.md bagian 5.14 RE-REVIEW RONDE 11.
 * Latar proses: agen batch16-pengawasan tidak pernah diluncurkan sehingga
 * empat item tercatat [x] tanpa eksekusi — suite ini adalah kontrak
 * penuntasannya, plus satu ekor escape (R143).
 *
 *   S110 — PAGINASI MONITORING IKUT REBUILD TIAP TICK: jalur skip-if-identical
 *         S94 (`subsLastHtml`) hanya melindungi tbody; `renderSubPagination`
 *         tetap menulis `pagEl.innerHTML` tiap tick 12 detik walau payload
 *         identik — fokus keyboard di tombol halaman lenyap berkala.
 *         Kontrak: fungsi menyimpan fingerprint HTML paginasi terakhir dan
 *         MELEWATI penulisan bila identik (pola subsLastHtml), sehingga DOM
 *         paginasi stabil antar tick senyap.
 *
 *   S111 — RESTORE FOKUS TANPA FALLBACK: `restoreSubsFocus` hanya memfokuskan
 *         bila target ditemukan; baris yang dicabut/disetujui/menghilang dari
 *         filter membuat fokus jatuh ke <body> tanpa kabar. Kontrak: cabang
 *         fallback eksplisit — tbody dibuat fokusable (tabIndex=-1) dan
 *         menerima fokus, plus pesan singkat ke #queueLiveRegion agar screen
 *         reader tahu konteksnya pindah.
 *
 *   R124 — KONTRAK ESCAPE data-mac GANDA: baris tabel memakai
 *         `escapeHtml(jsEscape(a.mac_address))` (:1321) sedangkan tombol
 *         Izinkan/Tolak & pembanding findApprovalRowByMac memakai nilai
 *         polos — MAC mengandung `\`/`'` membuat pencocokan remove/update
 *         in-place meleset → baris duplikat. Kontrak: SATU kontrak
 *         `escapeHtml(a.mac_address)` di seluruh atribut data-mac; jsEscape
 *         tidak lagi dipakai pada atribut tersebut.
 *
 *   R125(sisa) — KEMBARAN WARNA MUTED DI PENGAWAS_DETAIL: :201 (stat-icon
 *         inline) dan blok .pd-action-muted (:576–580) masih literal
 *         rgba(107,114,128,…)/#9ca3af — sisa migrasi Batch 16 yang baru
 *         menjangkau dashboard/admin-base/public-mobile. Kontrak: file bebas
 *         literal keluarga itu; warna memakai var(--color-text-muted)/
 *         rgba(var(--rgb-text-muted),α).
 *
 *   R143 — INTERPOLASI MENTAH NILAI SERVER: `s.attempt_count` masuk atribut
 *         title dan teks (:1606), `s.id` masuk data-submission-id (:1615)
 *         tanpa escapeHtml(String(...)) — meski bernilai numerik, kontrak
 *         escape-everything-from-server wajib paritas lintas builder.
 *         Kontrak: kedua titik membungkus escapeHtml(String(...)).
 *
 * Cara kalibrasi bila test MEMERAH setelah edit sah: lihat ID di dokumen
 * review; jangan turunkan asersi tanpa alasan terdokumentasi.
 */
import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const ROOT = new URL('.', import.meta.url).pathname.replace(/\/static\/js\/$/, '');
const HTML = readFileSync(join(ROOT, 'templates/admin/pengawas_detail.html'), 'utf8');

import { join } from 'node:path';

function fnBlock(name) {
    const start = HTML.indexOf('function ' + name + '(');
    assert.ok(start > -1, 'fungsi ' + name + ' eksis');
    // Batas kasar: hingga 'function ' berikutnya di kolom 0 atau akhir file.
    let next = HTML.indexOf('\nfunction ', start + 10);
    if (next === -1) next = Math.min(HTML.length, start + 12000);
    return HTML.slice(start, next);
}

test('S110: renderSubPagination skip penulisan bila HTML identik (fingerprint)', () => {
    const block = fnBlock('renderSubPagination');
    assert.match(block, /__lastHtml/,
        'variabel fingerprint paginasi eksis');
    assert.match(block, /if\s*\([^)]*===\s*[^)]*\)\s*\{\s*(\/\/[^\n]*\n\s*)?return;/,
        'cabang skip-return bila fingerprint sama');
    // Penulisan innerHTML hanya boleh terjadi SETELAH cek fingerprint.
    const writeIdx = block.indexOf("pagEl.innerHTML = html;");
    const checkIdx = block.search(/__lastHtml/);
    assert.ok(writeIdx > -1 && checkIdx > -1 && checkIdx < writeIdx,
        'cek fingerprint terjadi sebelum penulisan innerHTML');
});

test('S111: restoreSubsFocus punya fallback fokus tbody + announce live region', () => {
    const block = fnBlock('restoreSubsFocus');
    assert.match(block, /if\s*\(target\s*&&\s*target\.focus\)\s*\{\s*target\.focus\(\);/,
        'jalur sukses tetap memfokuskan target');
    assert.match(block, /else|!\s*target/, 'ada cabang else/fallback saat target hilang');
    assert.match(block, /tabIndex/, 'fallback membuat tbody fokusable (tabIndex=-1)');
    assert.match(block, /queueLiveRegion|announce/i,
        'fallback mengumumkan perpindahan konteks ke live region');
});

test('R124: atribut data-mac memakai SATU kontrak escapeHtml polos (jsEscape dihapus dari atribut)', () => {
    assert.doesNotMatch(HTML, /data-mac="' \+ escapeHtml\(jsEscape\(a\.mac_address\)\)/,
        'jsEscape pada atribut data-mac membuat pembanding findApprovalRowByMac meleset');
    const hits = HTML.split(`escapeHtml(a.mac_address)`).length - 1;
    assert.ok(hits >= 3,
        `atribut data-mac konsisten escapeHtml polos (ditemukan ${hits}, minimal 3: baris + Izinkan + Tolak)`);
});

test('R125(sisa): pengawas_detail.html bebas kembaran muted rgba(107,114,128)/#9ca3af', () => {
    assert.doesNotMatch(HTML, /107,\s*114,\s*128/,
        'bentuk rgb #6b7280 bermigrasi rgba(var(--rgb-text-muted),α)');
    assert.doesNotMatch(HTML, /#9ca3af/i,
        '#9ca3af bermigrasi var(--color-text-muted)');
    // Stat-icon inline :201 kini memakai token:
    assert.match(HTML, /pd-stat-icon" style="background:rgba\(var\(--rgb-text-muted\),0\.12\);color:var\(--color-text-muted\);"/,
        'stat-icon inline memakai triplet token');
});

test('R143: attempt_count & s.id ter-escape di builder submissions', () => {
    const loadBlock = HTML.slice(HTML.indexOf('function loadDetail('));
    assert.match(loadBlock, /title="' \+ escapeHtml\(String\(s\.attempt_count\)\) \+/,
        'attempt_count di atribut title wajib escapeHtml(String(...))');
    assert.match(loadBlock, /\+ escapeHtml\(String\(s\.attempt_count\)\) \+ 'x<\/span>'/,
        'attempt_count di teks wajib escapeHtml(String(...))');
    assert.match(loadBlock, /data-submission-id="' \+ escapeHtml\(String\(s\.id\)\) \+ '/,
        's.id di data-submission-id wajib escapeHtml(String(...))');
});
