/* Regression contract tests untuk Batch 3 perbaikan UI/UX (bagian halaman
 * unduh publik): temuan S11, S12, S14 di review_uiux_webui.md root repo.
 *
 * Run with:  node --test static/js/uiux-batch3-download.test.mjs   (from webui/)
 *
 * Semua test adalah kontrak statik atas file template ASLI
 * (templates/public/download.html), ditulis SEBELUM implementasi (red → green):
 *   - S11: panduan siswa awam dipisah total dari instruksi IT (kiosk/ADB/
 *     factory reset) menjadi dua bagian collapsible terpisah + peringatan.
 *   - S12: langkah "sumber tidak dikenal" didetailkan umum + per-merk
 *     (Samsung/Xiaomi/vivo/Oppo) dan asal-usul alamat server dijelaskan.
 *   - S14: tab platform accessible (ARIA tabs pattern) + deep-linkable via
 *     location.hash (#android/#windows/#linux).
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const WEBUI_ROOT = path.join(__dirname, '..', '..');
const read = (rel) => fs.readFileSync(path.join(WEBUI_ROOT, rel), 'utf8');

const html = () => read('templates/public/download.html');

// ---------------------------------------------------------------------------
// S14 — Tab platform: ARIA tabs pattern + hash routing
// ---------------------------------------------------------------------------

test('S14a: kontainer tab & tombol tab memakai role="tablist"/"tab" + aria-selected + aria-controls', () => {
    const doc = html();

    assert.match(doc, /class="tabs-container"[^>]*role="tablist"/,
        '.tabs-container wajib punya role="tablist"');
    assert.match(doc, /role="tablist"[^>]*aria-label=/,
        'tablist wajib punya aria-label deskriptif');

    for (const p of ['android', 'windows', 'linux']) {
        const btn = doc.match(new RegExp(`<button[^>]*id="tab-${p}"[^>]*>`));
        assert.ok(btn, `tombol #tab-${p} harus ada`);
        assert.match(btn[0], /role="tab"/, `#tab-${p} wajib role="tab"`);
        assert.match(btn[0], /aria-selected=/, `#tab-${p} wajib punya atribut aria-selected`);
        assert.match(btn[0], new RegExp(`aria-controls="section-${p}"`),
            `#tab-${p} wajib aria-controls ke panelnya`);
    }

    // Hanya satu tab boleh aria-selected="true" di markup awal.
    const selTrue = (doc.match(/aria-selected="true"/g) || []).length;
    assert.equal(selTrue, 1, `tepat satu tab aria-selected="true" (dapat ${selTrue})`);
});

test('S14b: panel platform punya role="tabpanel" + aria-labelledby ke tombol tab-nya', () => {
    const doc = html();
    for (const p of ['android', 'windows', 'linux']) {
        const panel = doc.match(new RegExp(`<div[^>]*id="section-${p}"[^>]*>`));
        assert.ok(panel, `panel #section-${p} harus ada`);
        assert.match(panel[0], /role="tabpanel"/, `#section-${p} wajib role="tabpanel"`);
        assert.match(panel[0], new RegExp(`aria-labelledby="tab-${p}"`),
            `#section-${p} wajib aria-labelledby ke #tab-${p}`);
    }
});

test('S14c: JS membaca location.hash saat load dengan whitelist platform & fallback default', () => {
    const doc = html();

    // Hash dibaca saat DOMContentLoaded (deep-link masuk) melalui helper
    // resolusi, dan juga saat event hashchange (navigasi back/forward).
    assert.match(doc, /function resolvePlatformFromHash\(\)[\s\S]{0,300}?location\.hash/,
        'helper resolusi wajib membaca location.hash');
    assert.match(doc, /DOMContentLoaded[\s\S]{0,400}?resolvePlatformFromHash\(\)/,
        'handler DOMContentLoaded wajib memanggil resolusi hash');
    assert.match(doc, /hashchange[\s\S]{0,300}?resolvePlatformFromHash\(\)/,
        'event hashchange juga harus mengikuti perubahan hash');

    // Whitelist eksplisit ketiga platform agar hash arbitrer tidak dipercaya.
    assert.match(doc, /\[\s*['"]android['"]\s*,\s*['"]windows['"]\s*,\s*['"]linux['"]\s*\]/,
        'daftar platform valid (android/windows/linux) harus eksplisit di JS');

    // Fallback default saat hash kosong/tidak valid.
    assert.match(doc, /return\s+VALID_PLATFORMS\.includes\(h\)\s*\?\s*h\s*:\s*DEFAULT_PLATFORM/,
        'resolusi hash wajib jatuh ke platform default bila tidak valid');
    assert.match(doc, /switchTab\(\s*resolvePlatformFromHash\(\)/,
        'switchTab wajib dipanggil dengan tab hasil resolusi hash/fallback');
});

test('S14d: switchTab menulis hash (deep-linkable) & menyinkronkan state ARIA', () => {
    const doc = html();

    assert.match(doc, /history\.replaceState\(\s*null\s*,\s*''\s*,\s*'#'\s*\+\s*platform\s*\)|location\.hash\s*=\s*'#'\s*\+\s*platform/,
        'switchTab wajib memperbarui URL hash (#platform)');

    assert.match(doc, /setAttribute\(\s*['"]aria-selected['"]\s*,/,
        'switchTab wajib set aria-selected dinamis pada tombol tab');

    assert.match(doc, /\.tabIndex\s*=/,
        'roving tabindex (tabIndex aktif=0, lainnya=-1) wajib disetel oleh switchTab');
});

test('S14e: navigasi keyboard (arrow keys) antar tab tersedia', () => {
    const doc = html();
    assert.match(doc, /addEventListener\(\s*['"]keydown['"][\s\S]{0,500}ArrowRight/,
        'tablist wajib menangani keydown panah (minimal ArrowRight)');
    assert.match(doc, /ArrowLeft|Home|End/,
        'navigasi panah kiri/Home/End turut ditangani');
});

// ---------------------------------------------------------------------------
// S11 — Pemisahan panduan siswa vs admin IT (kiosk/ADB/factory reset)
// ---------------------------------------------------------------------------

/** Blok panduan siswa = antara marker id siswa & id admin; blok admin = sisanya. */
function guideBlocks() {
    const doc = html();
    const siswaIdx = doc.indexOf('id="panduan-siswa-android"');
    const adminIdx = doc.indexOf('id="panduan-admin-it-android"');
    assert.ok(siswaIdx >= 0, 'marker id="panduan-siswa-android" harus ada');
    assert.ok(adminIdx >= 0, 'marker id="panduan-admin-it-android" harus ada');
    assert.ok(adminIdx > siswaIdx, 'blok siswa harus muncul sebelum blok admin IT');
    return { doc, siswa: doc.slice(siswaIdx, adminIdx), admin: doc.slice(adminIdx) };
}

test('S11a: panduan Android dipisah dua section collapsible "Untuk Siswa" vs "Untuk Admin IT Sekolah"', () => {
    const { doc } = guideBlocks();

    assert.match(doc, /Untuk Siswa/, 'label audiens siswa harus eksplisit');
    assert.match(doc, /Untuk Admin IT Sekolah/, 'label audiens admin IT harus eksplisit');

    // Kedua section collapsible terpisah (<details><summary>…).
    for (const id of ['panduan-siswa-android', 'panduan-admin-it-android']) {
        const el = doc.match(new RegExp(`id="${id}"[\\s\\S]{0,80}`));
        assert.ok(el, `#${id} harus ada`);
        const openTag = doc.slice(Math.max(0, doc.indexOf(id) - 200), doc.indexOf(id));
        assert.match(openTag, /<details\b/, `${id} harus berada di dalam elemen <details> (collapsible)`);
    }
    assert.match(doc, /<summary[^>]*>[^<]*Untuk Siswa/, 'summary collapsible siswa');
    assert.match(doc, /<summary[^>]*>[^<]*Untuk Admin IT Sekolah/, 'summary collapsible admin IT');
});

test('S11b: instruksi teknis ADB/factory-reset HANYA di blok Admin IT, bukan blok siswa', () => {
    const { siswa, admin } = guideBlocks();

    assert.doesNotMatch(siswa, /adb shell|set-device-owner/i,
        'perintah ADB tidak boleh tampil di panduan siswa');
    assert.doesNotMatch(siswa, /factory\s*reset|Device Owner/i,
        'istilah factory reset/Device Owner tidak boleh tampil di panduan siswa');
    assert.doesNotMatch(siswa, /USB Debugging/i,
        'instruksi USB Debugging tidak boleh tampil di panduan siswa');

    // Konten teknis lama tetap dipertahankan — kini di blok admin IT.
    assert.match(admin, /adb shell dpm set-device-owner/, 'perintah ADB lengkap tetap ada di blok admin');
    assert.match(admin, /[Ff]actory [Rr]eset|[Rr]eset [Pp]abrik/, 'langkah factory reset tetap ada di blok admin');
    assert.match(admin, /USB Debugging/, 'detail USB Debugging tetap ada di blok admin');
    assert.match(admin, /Kiosk/i, 'metode kiosk tetap ada di blok admin');
});

test('S11c: blok admin IT memuat peringatan hapus data & hanya untuk perangkat sekolah terkelola', () => {
    const { admin } = guideBlocks();

    assert.match(admin, /menghapus|terhapus/i, 'peringatan penghapusan data harus ada');
    assert.match(admin, /seluruh data/i, 'peringatan harus spesifik "seluruh data"');
    assert.match(admin, /dikelola/i, 'harus ada batasan "perangkat yang dikelola"');
    assert.match(admin, /jangan gunakan|tidak untuk|JANGAN/i,
        'harus ada larangan eksplisit memakai metode ini di perangkat pribadi');
});

// ---------------------------------------------------------------------------
// S12 — Detail langkah "sumber tidak dikenal" + asal-usul alamat server
// ---------------------------------------------------------------------------

test('S12a: langkah sumber tidak dikenal menjelaskan jalur menu BERBEDA per merk dalam <details>', () => {
    const { siswa } = guideBlocks();

    assert.match(siswa, /Sumber Tidak Dikenal|sumber tidak dikenal|Tidak Dikenal/i,
        'langkah izinkan instalasi dari sumber tidak dikenal harus ada di panduan siswa');
    assert.match(siswa, /berbeda/i, 'harus ada catatan bahwa jalur menu berbeda antar merk');

    for (const merk of ['Samsung', 'Xiaomi', 'vivo', 'Oppo']) {
        assert.match(siswa, new RegExp(merk), `panduan per-merk ${merk} harus ada`);
    }

    // Setiap merk dibungkus elemen <details> sendiri (accordion kecil).
    for (const merk of ['Samsung', 'Xiaomi', 'vivo', 'Oppo']) {
        const idx = siswa.indexOf(merk);
        const before = siswa.slice(Math.max(0, idx - 300), idx);
        assert.match(before, /<details\b/, `${merk} harus berada di dalam <details>`);
    }
});

test('S12b: asal alamat server & token dijelaskan + contoh format alamat', () => {
    const { siswa } = guideBlocks();

    assert.match(siswa, /diberikan (oleh )?(pengawas|panitia)/i,
        'alamat server/token harus dijelaskan berasal dari pengawas/panitia ujian');

    assert.match(siswa, /http:\/\/192\.168\.1\.10:8080/,
        'contoh format alamat server (http://192.168.1.10:8080) harus ada');
});
