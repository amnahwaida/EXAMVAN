/* Regression contract tests untuk Batch 5 (Ronde 2) — halaman daftar admin:
 * submissions.html, pengawas.html, pengawas_detail.html.
 * Referensi temuan: review_uiux_webui.md §5.5 RE-REVIEW RONDE 2
 * (ID: T13, S24, S25-part, R23-part, R19-part, R26 + tindak lanjut skip-link).
 *
 * Run with:  node --test static/js/uiux-batch5-admin-list.test.mjs   (from webui/)
 *
 * Semua test adalah kontrak statik (fs-read + regex) mengikuti pola
 * uiux-batch1.test.mjs. ID temuan & dampak bisnis dijelaskan per seksi:
 *   - T13: tanpa #toastContainer, feedback sukses/gagal hapus hasil ujian
 *     tidak pernah tampil — guru tidak yakin data terhapus atau tidak.
 *   - S24: label status sama ("tombstoned") tampil dengan dua nama beda
 *     antarhalaman — guru bingung mencari baris yang dipelajari dari dashboard.
 *   - S25: modal tanpa role="dialog"/aria-modal/aria-labelledby — screen
 *     reader tidak tahu konten form adalah dialog terpisah dari halaman.
 *   - R23: input pencarian placeholder-only — tak berlabel bagi screen reader.
 *   - R19: hierarki heading melompat h1→h3 tanpa h2 — struktur dokumen
 *     tidak bisa dinavigasi per-seksi oleh screen reader.
 *   - Skip-link nav menunjuk #mainContent — target harus eksis di tiap halaman,
 *     kalau tidak, lompatan keyboard user mendarat di awal halaman (no-op).
 *   - R26: sisa string EN user-visible ("Export Excel") merusak konsistensi UI.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const WEBUI_ROOT = path.join(__dirname, '..', '..');
const read = (rel) => fs.readFileSync(path.join(WEBUI_ROOT, rel), 'utf8');

const SUBMISSIONS = 'templates/admin/submissions.html';
const PENGAWAS = 'templates/admin/pengawas.html';
const DETAIL = 'templates/admin/pengawas_detail.html';

// ---------------------------------------------------------------------------
// T13 — submissions.html wajib punya container toast (pola persis
// pengawas_detail.html:14); tanpa ini showToast di admin.js (hapus hasil
// ujian, gagal muat detail) lenyap senyap.
// ---------------------------------------------------------------------------

test('T13/T14: #toastContainer tersedia untuk submissions.html via partials/nav.html (satu sumber)', () => {
    const markup = '<div class="toast-container" id="toastContainer" aria-live="polite" aria-atomic="true"></div>';
    const submissions = read(SUBMISSIONS);
    const nav = read('templates/admin/partials/nav.html');

    // Batch 9 (T14): container pindah ke partials/nav.html yang dirender di
    // SEMUA halaman admin — intent proteksi T13 (toast selalu tampil)
    // dipertahankan; duplikat per-halaman dilarang.
    assert.ok(nav.includes(markup), 'sanity: partials/nav.html menyediakan markup toastContainer');
    assert.ok(submissions.includes('admin/partials/nav.html'),
        'submissions.html memuat partials/nav.html (pembawa toastContainer)');
    assert.ok(!submissions.includes(markup),
        'submissions.html tidak lagi menduplikasi container toast (satu sumber: nav.html)');
});

// ---------------------------------------------------------------------------
// S24 — label kanonik status tombstoned = "Nonaktif Otomatis" (dipakai
// dashboard.html). Tidak boleh ada lagi teks jargon internal "Ditombstone"
// di ketiga halaman daftar admin.
// ---------------------------------------------------------------------------

test('S24: teks "Ditombstone" hilang dari ketiga halaman, label "Nonaktif Otomatis" hadir di lokasi kunci', () => {
    for (const rel of [SUBMISSIONS, PENGAWAS, DETAIL]) {
        const html = read(rel);
        assert.ok(!html.includes('Ditombstone'), `${rel} tidak boleh lagi memuat label jargon "Ditombstone"`);
    }

    const submissions = read(SUBMISSIONS);
    assert.match(submissions, /— Nonaktif Otomatis\{\{end\}\}/, 'opsi filter ujian submissions harus memakai label kanonik');
    const badgeCount = [...submissions.matchAll(/status-badge status-tombstoned[^>]*>Nonaktif Otomatis<\/span>/g)].length;
    assert.equal(badgeCount, 1, 'badge status exam_info submissions harus "Nonaktif Otomatis"');

    const pengawas = read(PENGAWAS);
    assert.match(pengawas, /status-badge status-tombstoned[^\n]*>Nonaktif Otomatis<\/span>'/, 'string JS badge pengawas.html harus "Nonaktif Otomatis"');

    const detail = read(DETAIL);
    assert.match(detail, /status-badge status-tombstoned[^>]*>Nonaktif Otomatis<\/span>/, 'badge pengawas_detail.html harus "Nonaktif Otomatis"');
});

// ---------------------------------------------------------------------------
// S25 (part) — SEMUA elemen modal utama (.modal-overlay) di ketiga file wajib
// punya semantik dialog lengkap: role="dialog" + aria-modal="true" +
// aria-labelledby yang menunjuk id heading yang benar-benar eksis.
// ---------------------------------------------------------------------------

test('S25: setiap .modal-overlay punya role="dialog", aria-modal, dan aria-labelledby yang valid', () => {
    const overlayRe = /<div\b[^>]*class="modal-overlay"[^>]*>/g;

    for (const rel of [SUBMISSIONS, PENGAWAS, DETAIL]) {
        const html = read(rel);
        const overlays = [...html.matchAll(overlayRe)].map((m) => m[0]);

        // pengawas.html memang tidak memiliki modal — kontrak: tetap nol.
        if (rel === PENGAWAS) {
            assert.equal(overlays.length, 0, 'pengawas.html tidak diharapkan punya modal-overlay');
            continue;
        }
        assert.ok(overlays.length > 0, `${rel} sanity: masih punya modal untuk diaudit`);

        for (const tag of overlays) {
            assert.match(tag, /\brole="dialog"/, `${rel}: modal-overlay wajib role="dialog" → ${tag}`);
            assert.match(tag, /aria-modal="true"/, `${rel}: modal-overlay wajib aria-modal="true" → ${tag}`);
            const m = tag.match(/aria-labelledby="([^"]+)"/);
            assert.ok(m, `${rel}: modal-overlay wajib aria-labelledby → ${tag}`);
            assert.match(html, new RegExp(`id="${m[1]}"`), `${rel}: id heading "${m[1]}" yang dirujuk aria-labelledby harus eksis di halaman`);
        }
    }

    // Lokasi spesifik yang dilaporkan reviewer: detailModal submissions.
    const submissions = read(SUBMISSIONS);
    assert.match(submissions, /<div[^>]*class="modal-overlay"[^>]*id="detailModal"[^>]*role="dialog"/, 'detailModal submissions.html wajib ber-role="dialog"');
});

// ---------------------------------------------------------------------------
// R23 (part) — input pencarian placeholder-only wajib punya aria-label agar
// screen reader mengumumkan tujuan field, bukan sekadar placeholder.
// ---------------------------------------------------------------------------

test('R23: kedua input #pengawasSearch punya aria-label sesuai konteks halaman', () => {
    const pengawasInput = read(PENGAWAS).match(/<input[^>]*id="pengawasSearch"[^>]*>/);
    assert.ok(pengawasInput, '#pengawasSearch ada di pengawas.html');
    assert.match(pengawasInput[0], /aria-label="Cari ujian"/, 'input pencarian daftar ujian wajib aria-label="Cari ujian"');

    const detailInput = read(DETAIL).match(/<input[^>]*id="pengawasSearch"[^>]*>/);
    assert.ok(detailInput, '#pengawasSearch ada di pengawas_detail.html');
    assert.match(detailInput[0], /aria-label="Cari peserta"/, 'input pencarian daftar peserta wajib aria-label="Cari peserta"');
});

// ---------------------------------------------------------------------------
// R19 (part) — hierarki heading: pengawas.html tidak boleh melompat h1→h3;
 // seksi diganti h2 dengan inline font-size agar visual tak berubah.
// ---------------------------------------------------------------------------

test('R19: pengawas.html tidak memuat h3 sebelum h2 pertamanya (visual tetap dijaga inline)', () => {
    const html = read(PENGAWAS);

    const h1 = html.indexOf('<h1');
    const h2 = html.indexOf('<h2');
    const h3 = html.search(/<h3[\s>]/);
    assert.notEqual(h1, -1, 'sanity: h1 halaman tetap ada');
    assert.notEqual(h2, -1, 'harus ada minimal satu h2 (seksi naik dari h3)');
    assert.ok(h1 < h2, 'h1 mendahului h2 pertama');
    assert.ok(h3 === -1 || h3 > h2, 'tidak boleh ada h3 sebelum h2 pertama');

    // Visual preservation: h2 seksi membawa inline font-size seperti gaya lokal h3 lama.
    const h2Tag = html.slice(h2, html.indexOf('>', h2) + 1);
    assert.match(h2Tag, /font-size:\s*1\.1rem/, 'h2 seksi mempertahankan font-size inline 1.1rem milik h3 lama (visual identik)');
});

test('R19b: judul seksi tabel pengawas_detail.html sudah naik ke h2 (bukan lagi h3-seksi)', () => {
    const html = read(DETAIL);
    const titles = [...html.matchAll(/<(h[1-6])[^>]*class="pd-table-title"/g)].map((m) => m[1]);
    assert.equal(titles.length, 2, 'sanity: dua judul seksi tabel (Antrean Peserta & Monitoring Perangkat)');
    for (const t of titles) {
        assert.equal(t, 'h2', `judul seksi .pd-table-title harus h2, ditemukan ${t} — kelas .pd-table-title sudah menahan visual`);
    }
});

// ---------------------------------------------------------------------------
// Tindak lanjut agen lain — skip-link di partials/nav.html menunjuk
// #mainContent; ketiga halaman wajib menyediakan targetnya di <main>.
// ---------------------------------------------------------------------------

test('skip-link: ketiga halaman memiliki <main ... id="mainContent">', () => {
    for (const rel of [SUBMISSIONS, PENGAWAS, DETAIL]) {
        assert.match(read(rel), /<main\b[^>]*class="dashboard"[^>]*id="mainContent"/, `${rel}: <main class="dashboard"> wajib punya id="mainContent" sebagai target skip-link nav`);
    }
});

// ---------------------------------------------------------------------------
// R26 — sisa string EN user-visible pada tombol ekspor.
// ---------------------------------------------------------------------------

test('R26: tombol ekspor submissions memakai "Ekspor Excel", bukan "Export Excel"', () => {
    const html = read(SUBMISSIONS);
    assert.ok(!html.includes('Export Excel'), '"Export Excel" (EN) tidak boleh tersisa di submissions.html');
    assert.match(html, /<\/svg>\s*Ekspor Excel<\/button>/, 'label tombol harus "Ekspor Excel"');
});
