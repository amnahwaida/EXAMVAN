/* Contract + behavior tests untuk Batch 10 — halaman Pengaturan (milik agen
 * batch-10-settings). Referensi: review_uiux_webui.md bagian 5.7 RE-REVIEW
 * RONDE 4, temuan R48, R50 (sisi settings), S58 (sisi template settings).
 *
 * Run with:  node --test static/js/uiux-batch10-settings.test.mjs   (from webui/)
 *
 * Latar belakang & dampak bisnis:
 *   - R48: closeUploadModal menunda penutupan 300 ms via setTimeout tanpa
 *     menyimpan handle. Tutup → buka ulang cepat (<300 ms) membuat timeout
 *     lama menutup modal yang BARU dibuka — user melihat modal menghilang
 *     sendiri. Kini handle disimpan di modul dan openUploadModal melakukan
 *     clearTimeout sebelum body timeout sempat berjalan.
 *   - R50 (sisi settings): sisa EN "Refresh" ×2 (settings.html:956, :1299)
 *     dimigrasi ke "Muat Ulang" (aria-label ikut diselaraskan).
 *   - S58 (sisi template): migrasi substitusi-nilai-persis mekanis
 *     rgba(255,255,255,α) → rgba(var(--rgb-white), α) dan
 *     rgba(0,0,0,α) → rgba(var(--rgb-black), α) di inline style/blok <style>
 *     settings.html. Token --rgb-white/--rgb-black sudah ada di theme.css
 *     (Batch 8); visual nol perubahan. Jumlah rgba literal digit harus turun
 *     signifikan (baseline ronde 4: 109) dan tidak ada bentuk salah tulis.
 *
 * Kontrak antar-agen yang DIKONSUMSI (di-stub di harness):
 *   - admin-core.js: Actions.register, showToast, showConfirm, apiFetch.
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

const SETTINGS = read('templates/admin/settings.html');
const THEME_CSS = read('static/css/theme.css');
const SYSTEM_APPS_SRC = read('static/js/settings-system-apps.js');

// ===========================================================================
// R48 — race tutup→buka uploadModal (timeout 300 ms menutup modal baru)
// ===========================================================================

test('R48 (statik): handle timeout closeUploadModal disimpan & openUploadModal melakukan clearTimeout', () => {
    const openFn = /function\s+openUploadModal\s*\(\)\s*\{[\s\S]*?\n\}/.exec(SYSTEM_APPS_SRC);
    assert.ok(openFn, 'openUploadModal harus terdefinisi');
    assert.match(openFn[0], /clearTimeout\s*\(/,
        'openUploadModal wajib membatalkan timeout penutupan tertunda');

    const closeFn = /function\s+closeUploadModal\s*\(\)\s*\{[\s\S]*?\n\}/.exec(SYSTEM_APPS_SRC);
    assert.ok(closeFn, 'closeUploadModal harus terdefinisi');
    assert.match(closeFn[0], /=\s*setTimeout\s*\(/,
        'closeUploadModal wajib menyimpan handle hasil setTimeout ke variabel modul');
});

/** Fake node DOM minimal (pola batch9-settings). */
function fakeEl(tag, attrs) {
    let _className = '';
    const node = {
        tagName: String(tag || '').toUpperCase(),
        attrs: Object.assign({}, attrs),
        children: [],
        parentNode: null,
        style: {},
        dataset: {},
        get className() { return _className; },
        set className(v) {
            _className = String(v);
            _className.split(/\s+/).filter(Boolean).forEach((c) => node.classList._set.add(c));
        },
        innerHTML: '',
        innerText: '',
        textContent: '',
        reset() {},
        disabled: false,
        offsetWidth: 0,
        _listeners: {},
        classList: {
            _set: new Set(['show']),
            add(...c) { c.forEach((x) => node.classList._set.add(x)); },
            remove(...c) { c.forEach((x) => node.classList._set.delete(x)); },
            contains(c) { return node.classList._set.has(c); }
        }
    };
    node.getAttribute = (n) => (Object.prototype.hasOwnProperty.call(node.attrs, n) ? node.attrs[n] : null);
    node.setAttribute = (n, v) => { node.attrs[n] = String(v); };
    node.removeAttribute = (n) => { delete node.attrs[n]; };
    Object.defineProperty(node, 'id', {
        get() { return node.attrs.id || ''; },
        set(v) { if (v) node.attrs.id = String(v); }
    });
    node.addEventListener = function (type, fn) {
        (node._listeners[type] = node._listeners[type] || []).push(fn);
    };
    node.appendChild = function (c) { c.parentNode = node; node.children.push(c); return c; };
    return node;
}

function loadSystemAppsSandbox(byId, timers) {
    // Fake timers deterministik: handle numerik, callback ditahan sampai di-flush.
    let nextHandle = 1;
    timers.pending = new Map();
    timers.cleared = [];
    const win = {
        __settingsReady: {},
        __uploadInProgress: false,
        location: { search: '', replaceState() {} },
        addEventListener() {}
    };
    win.window = win;
    win.Actions = { register() {}, has: () => true };
    win.apiFetch = () => Promise.resolve({ json: () => Promise.resolve({ success: true }) });
    win.showConfirm = () => Promise.resolve(true);
    win.showToast = () => {};
    // S118 (Batch 20): open/close uploadModal kini lewat Modal Manager -
    // stub memetakan ke perilaku display asli agar penghitung R48 tetap sah.
    win.Modal = {
        open(el) { el.style.display = 'flex'; },
        close(el) { el.style.display = 'none'; }
    };

    const docMock = {
        readyState: 'complete',
        body: fakeEl('body'),
        head: fakeEl('head'),
        getElementById: (id) => byId[id] || null,
        createElement: (tag) => fakeEl(tag),
        querySelector: () => null,
        querySelectorAll: () => [],
        addEventListener() {},
        write() {}
    };

    const sandbox = {
        window: win,
        document: docMock,
        // S118 (Batch 20): fungsi sistem-apps kini menunjuk Modal global
        // langsung - ekspos alias di level konteks.
        Modal: win.Modal,
        URLSearchParams,
        console,
        setTimeout: (fn) => {
            const h = nextHandle++;
            timers.pending.set(h, fn);
            return h;
        },
        clearTimeout: (h) => {
            timers.cleared.push(h);
            timers.pending.delete(h);
        },
        get showToast() { return win.showToast; },
        get apiFetch() { return win.apiFetch; },
        get showConfirm() { return win.showConfirm; }
    };
    sandbox.globalThis = sandbox;
    vm.createContext(sandbox);
    vm.runInContext(SYSTEM_APPS_SRC, sandbox, { filename: 'settings-system-apps.js' });
    return { sandbox, win, docMock };
}

function buildUploadModalDom() {
    const modal = fakeEl('div', { id: 'uploadModal' });
    return {
        uploadModal: modal,
        uploadError: fakeEl('div', { id: 'uploadError' }),
        uploadAppForm: fakeEl('form', { id: 'uploadAppForm' }),
        'file-name-display': fakeEl('span', { id: 'file-name-display' }),
        uploadProgressContainer: fakeEl('div', { id: 'uploadProgressContainer' }),
        uploadProgressPill: fakeEl('div', { id: 'uploadProgressPill' })
    };
}

test('R48 (perilaku): tutup lalu buka cepat → timeout lama DIBATALKAN, modal tetap tampil', () => {
    const timers = {};
    const byId = buildUploadModalDom();
    const env = loadSystemAppsSandbox(byId, timers);

    // Buka → tutup → jadwal penutupan 300 ms tertahan.
    env.sandbox.openUploadModal();
    assert.equal(byId.uploadModal.style.display, 'flex');
    env.sandbox.closeUploadModal();
    assert.equal(timers.pending.size, 1, 'closeUploadModal menunda penutupan via satu timer');

    // Buka ulang SEBELUM 300 ms berlalu → timer lama wajib diclearTimeout.
    env.sandbox.openUploadModal();
    assert.equal(timers.cleared.length, 1, 'openUploadModal wajib clearTimeout handle lama');
    assert.equal(timers.pending.size, 0, 'tidak boleh ada callback penutupan tertunda tersisa');

    // Flush semua timer yang mungkin masih hidup → modal TETAP tampil.
    timers.pending.forEach((fn) => fn());
    assert.notEqual(byId.uploadModal.style.display, 'none',
        'timeout lama tidak boleh menutup modal yang baru dibuka ulang');
    assert.equal(byId.uploadModal.style.display, 'flex', 'modal tetap tampil untuk user');
});

test('R48 (perilaku): tanpa buka ulang, penutupan tertunda tetap berjalan normal setelah 300 ms', () => {
    const timers = {};
    const byId = buildUploadModalDom();
    const env = loadSystemAppsSandbox(byId, timers);

    env.sandbox.openUploadModal();
    env.sandbox.closeUploadModal();
    assert.equal(timers.cleared.length, 0, 'tanpa buka ulang, tidak ada timer yang dibatalkan');

    timers.pending.forEach((fn) => fn());
    assert.equal(byId.uploadModal.style.display, 'none', 'modal tertutup oleh timer');
    assert.equal(byId['file-name-display'].innerText, 'Pilih atau Seret File Ke Sini',
        'label drop area direset');
    assert.equal(byId.uploadProgressContainer.style.display, 'none');
});

// ===========================================================================
// R50 (sisi settings) — sisa EN "Refresh"
// ===========================================================================

test('R50 (statik): settings.html bebas label EN "Refresh"; memakai "Muat Ulang"', () => {
    assert.doesNotMatch(SETTINGS, />\s*Refresh\b/, 'teks tombol "Refresh" harus jadi "Muat Ulang"');
    assert.doesNotMatch(SETTINGS, /aria-label="[^"]*\bRefresh\b/,
        'aria-label juga wajib berbahasa Indonesia ("Muat Ulang")');

    const muatUlang = (SETTINGS.match(/Muat Ulang/g) || []).length;
    assert.ok(muatUlang >= 2, `label "Muat Ulang" = ${muatUlang}, minimal 2 (tombol daftar user + audit)`);
    // Kedua tombol tetap membawa aksi refresh yang sama.
    assert.match(SETTINGS, /data-action="users-refresh-list"[^>]*aria-label="Muat Ulang/,
        'tombol refresh daftar user memakai aria-label "Muat Ulang"');
    assert.match(SETTINGS, /data-action="audit-refresh"/, 'tombol audit-refresh tetap ada');
});

// ===========================================================================
// S58 (sisi template settings) — migrasi rgba hitam/putih ke token triplet
// ===========================================================================

test('S58 (kontrak): token --rgb-white/--rgb-black tersedia di theme.css (Batch 8)', () => {
    assert.match(THEME_CSS, /--rgb-white:\s*255,\s*255,\s*255\s*;/, 'token --rgb-white wajib ada');
    assert.match(THEME_CSS, /--rgb-black:\s*0,\s*0,\s*0\s*;/, 'token --rgb-black wajib ada');
});

test('S58 (statik): jumlah rgba literal digit settings.html turun signifikan (109 → ≤30)', () => {
    const n = (SETTINGS.match(/rgba\(\s*\d/gi) || []).length;
    assert.ok(n <= 30, `rgba literal digit settings.html = ${n}, harus ≤ 30 (baseline ronde 4: 109)`);
});

test('S58 (statik): bentuk token rgba(var(--rgb-*)) selalu benar — tidak ada salah tulis', () => {
    // Bentuk sah: rgba(var(--rgb-white), α) / rgba(var(--rgb-black), α).
    const valid = SETTINGS.match(/rgba\(var\(--rgb-(?:white|black)\),\s*[0-9.]+\)/g) || [];
    assert.ok(valid.length >= 75, `bentuk valid = ${valid.length}, hasil migrasi mekanis ±81`);
    // Salah tulis yang dilarang: koma hilang sebelum α, kurung/typo varian lain.
    assert.doesNotMatch(SETTINGS, /rgba\(var\(--rgb-(?:white|black)\s*,/, 'koma penutup var() hilang');
    assert.doesNotMatch(SETTINGS, /rgba\(var\(--rgb-white\)[^,]/, 'α tanpa koma pemisah');
    assert.doesNotMatch(SETTINGS, /rgba\(var\(--rgb-black\)[^,]/, 'α tanpa koma pemisah');
    const usages = SETTINGS.match(/rgba\(var\(--rgb-(?:white|black)\b/g) || [];
    assert.equal(usages.length, valid.length,
        'semua pemakaian rgba(var(--rgb-white/black)) harus cocok pola valid');
});
