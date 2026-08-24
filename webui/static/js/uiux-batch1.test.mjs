/* Regression contract tests untuk Batch 1 perbaikan UI/UX.
 * Referensi temuan: review_uiux_webui.md di root repo (ID: T2, T5, R1, R3,
 * R5, S13, R16, R10).
 *
 * Run with:  node --test static/js/uiux-batch1.test.mjs   (from webui/)
 *
 * Dua jenis test:
 *   1. Kontrak statik — membaca file template/CSS/JS ASLI yang dikirim ke
 *      browser dan memastikan properti kunci perbaikan tidak pernah regresi
 *      (mis. #uploadError wajib display:none secara default).
 *   2. Perilaku — mengeksekusi admin-core.js asli dalam Node vm (pola sama
 *      dengan admin-core.test.mjs) untuk bug NaN pada refreshUserInterface.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const WEBUI_ROOT = path.join(__dirname, '..', '..');
const read = (rel) => fs.readFileSync(path.join(WEBUI_ROOT, rel), 'utf8');

// ---------------------------------------------------------------------------
// T2 — Kotak error palsu di modal unggah aplikasi (settings.html)
// ---------------------------------------------------------------------------

test('T2: #uploadError tersembunyi secara default & tanpa placeholder EN', () => {
    const html = read('templates/admin/settings.html');
    const m = html.match(/<div id="uploadError" style="([^"]*)"/);
    assert.ok(m, 'elemen #uploadError dengan inline style harus ada');
    assert.match(m[1], /display:\s*none/i, 'inline style wajib display:none (dulu flex → error palsu tampak saat modal dibuka)');
    assert.doesNotMatch(m[1], /display:\s*flex/i, 'markup awal tidak boleh display:flex');
    assert.ok(!html.includes('Error message here'), 'teks placeholder EN "Error message here" harus dihapus dari markup');

    const js = read('static/js/settings-system-apps.js');
    assert.match(js, /errorDiv\.style\.display\s*=\s*'flex'/, 'jalur reveal error saat upload gagal tetap ada di JS');
});

// ---------------------------------------------------------------------------
// T5 — CTA state "hasil dinonaktifkan" jangan mengarahkan siswa ke login admin
// ---------------------------------------------------------------------------

test('T5: halaman hasil dinonaktifkan menawarkan "Kembali ke Beranda", bukan login admin', () => {
    const html = read('templates/public/hasil.html');
    assert.ok(!html.includes('Login Webapp Admin'), 'CTA login admin tidak boleh ditawarkan ke siswa');

    const idx = html.indexOf('Kembali ke Beranda');
    assert.ok(idx >= 0, 'label CTA "Kembali ke Beranda" harus ada');
    const around = html.slice(Math.max(0, idx - 500), idx);
    assert.match(around, /href="\/"/, 'CTA harus mengarah ke beranda "/"');
});

// ---------------------------------------------------------------------------
// R1 — colspan="8" pada tabel 6 kolom (pengawas_detail.html)
// ---------------------------------------------------------------------------

test('R1: tabel submissions pengawas_detail tidak memakai colspan="8"', () => {
    const html = read('templates/admin/pengawas_detail.html');
    assert.ok(!html.includes('colspan="8"'), 'tabel 6 kolom — baris loading/error memakai colspan="8" sehingga tampak meleset');
});

// ---------------------------------------------------------------------------
// R3 — settings-packages.js didorong dua kali ke daftar modul
// ---------------------------------------------------------------------------

test('R3: settings-packages.js hanya dimuat satu kali', () => {
    const html = read('templates/admin/settings.html');
    const count = html.split("files.push('settings-packages.js')").length - 1;
    assert.equal(count, 1, `push settings-packages.js harus tepat 1x, ditemukan ${count}x (risiko handler dobel)`);
});

// ---------------------------------------------------------------------------
// R5 — placeholder "menit" dobel di dashboard
// ---------------------------------------------------------------------------

test('R5: input interval token tanpa placeholder "menit" (label span sudah menyebut menit)', () => {
    const html = read('templates/admin/dashboard.html');
    assert.ok(!html.includes('placeholder="menit"'), 'placeholder cukup contoh angka; kata "menit" sudah ada di label span di sebelahnya');
});

// ---------------------------------------------------------------------------
// S13 — tombol unduh download.html punya loading state & anti dobel-klik
// ---------------------------------------------------------------------------

test('S13: semua tombol unduh meneruskan elemen tombol + downloadApp punya guard & pemulihan state', () => {
    const html = read('templates/public/download.html');

    // Batch 10 (S59): onclick inline bermigrasi ke data-action — intent
    // kontrak dipertahankan: 4 anchor unduh (APK resmi, flavor tambahan,
    // Windows, Linux) dan handler delegasi tetap meneruskan elemen tombol.
    const anchors = [...html.matchAll(/data-action="download-app"/g)].length;
    assert.equal(anchors, 4, `4 anchor unduh wajib memakai data-action download-app — ditemukan ${anchors}`);
    const delegated = html.match(/downloadApp\([^)]*getAttribute\('data-app-id'\)[^)]*,\s*(?:btn|el)\)/);
    assert.ok(delegated, 'handler delegasi meneruskan elemen tombol (el) ke downloadApp');

    assert.match(html, /function downloadApp\(appId,\s*btn\)/, 'downloadApp menerima elemen tombol');
    assert.match(html, /dataset\.loading/, 'guard re-entry via dataset.loading agar tidak dobel probe');
    assert.match(html, /aria-busy/, 'tombol ditandai aria-busy saat memeriksa');
    assert.match(html, /\.finally\(/, 'state tombol dipulihkan lewat finally (sukses maupun gagal)');

    const cssRule = html.match(/\.btn-download-big\.is-loading\s*\{[^}]*\}/);
    assert.ok(cssRule, 'ada rule .btn-download-big.is-loading');
    assert.match(cssRule[0], /pointer-events:\s*none/, 'saat loading, klik ulang dimatikan');
});

// ---------------------------------------------------------------------------
// R16 — grid tab platform mobile: 3 kolom, bukan 2 (public-mobile.css)
// ---------------------------------------------------------------------------

test('R16: .tabs-container mobile memuat 3 tab sejajar', () => {
    const css = read('static/css/public-mobile.css');
    const rule = css.match(/\.tabs-container\s*\{[^}]*\}/);
    assert.ok(rule, '.tabs-container ada di public-mobile.css');
    assert.match(rule[0], /grid-template-columns:\s*repeat\(3,\s*minmax\(0,\s*1fr\)\)/, '3 tab (Android/Windows/Linux) harus 3 kolom');
    assert.doesNotMatch(rule[0], /1fr\s+1fr/, 'grid 2 kolom membuat tab Linux timpang sendirian');
});

// ---------------------------------------------------------------------------
// R10 — refreshUserInterface tidak menampilkan "NaN" (perilaku, via vm)
// ---------------------------------------------------------------------------

function fakeElement() {
    return {
        className: '',
        classList: { add() {}, remove() {}, contains() { return false; } },
        setAttribute() {},
        addEventListener() {},
        getAttribute() { return null; },
        innerHTML: '',
        textContent: '',
        querySelector() { return null; },
        querySelectorAll() { return []; },
        appendChild() {},
        remove() {},
        style: {},
        offsetHeight: 0
    };
}

// Muat admin-core.js ASLI dalam vm; querySelector/selectAll di-mock supaya
// refreshUserInterface membaca elemen statistik tiruan yang bisa diaudit.
function loadAdminCore() {
    const src = fs.readFileSync(path.join(__dirname, 'admin-core.js'), 'utf8');
    const els = {
        total: { textContent: '' },
        active: { textContent: '' },
        inactive: { textContent: '' },
        storage: { textContent: '' }
    };
    const toastContainer = { children: [], appendChild() {} };
    const documentMock = {
        readyState: 'complete',
        addEventListener() {},
        dispatchEvent() { return true; },
        getElementById(id) { return id === 'toastContainer' ? toastContainer : null; },
        querySelector(sel) {
            if (sel === '.stat-total .stat-value') return els.total;
            if (sel === '.stat-status .stat-value') return els.active;
            if (sel === '.stat-storage .stat-value') return els.storage;
            if (sel === 'meta[name="csrf-token"]') return { getAttribute: () => 'test-csrf-token' };
            return fakeElement();
        },
        querySelectorAll(sel) {
            return sel === '.stat-status .stat-value' ? [els.active, els.inactive] : [];
        },
        createElement() { return fakeElement(); },
        documentElement: {},
        body: { classList: { add() {}, remove() {}, contains() { return false; } } },
        contains() { return true; }
    };
    const sandbox = {
        window: { fetch: async () => ({ ok: true, status: 200, text: () => Promise.resolve('') }), CustomEvent: class FakeCustomEvent { constructor(t, o) { this.type = t; this.detail = (o && o.detail) || null; } } },
        document: documentMock,
        CustomEvent: class FakeCustomEvent2 { constructor(t, o) { this.type = t; this.detail = (o && o.detail) || null; } },
        MutationObserver: function () { this.observe = () => {}; this.disconnect = () => {}; },
        getComputedStyle: () => ({ display: 'block' }),
        MouseEvent: function () {},
        navigator: {},
        console: { debug() {}, log() {}, warn() {}, error() {}, info() {} },
        setTimeout: () => 0,
        clearTimeout() {},
        setInterval: () => 0,
        clearInterval() {},
        location: { href: '' }
    };
    sandbox.globalThis = sandbox;
    vm.createContext(sandbox);
    vm.runInContext(src, sandbox, { filename: 'admin-core.js' });
    return { sandbox, els };
}

// Mock textContent tidak melakukan stringifikasi seperti DOM asli, jadi
// semua perbandingan lewat String().
test('R10: kartu "nonaktif" tidak menampilkan NaN ketika field stats hilang', () => {
    const env = loadAdminCore();
    env.sandbox.refreshUserInterface({});

    assert.equal(String(env.els.total.textContent), '0');
    assert.equal(String(env.els.active.textContent), '0');
    assert.equal(String(env.els.inactive.textContent), '0', 'dulu: (undefined - undefined) ?? "0" merender "NaN"');
    assert.equal(String(env.els.storage.textContent), '0 MB');
});

test('R10b: selisih total−aktif tetap benar ketika data lengkap', () => {
    const env = loadAdminCore();
    env.sandbox.refreshUserInterface({ total: 12, active: 5, storage_mb: 30 });

    assert.equal(String(env.els.total.textContent), '12');
    assert.equal(String(env.els.active.textContent), '5');
    assert.equal(String(env.els.inactive.textContent), '7');
    assert.equal(String(env.els.storage.textContent), '30 MB');
});
