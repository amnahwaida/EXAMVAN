/* Contract + behavior tests untuk Batch 7 — halaman Pengaturan (settings.html)
 * dan modul-modul miliknya. Referensi: review_uiux_webui.md fase 2 design
 * token (lanjutan S15) + migrasi event-delegation `data-action` (lanjutan R28).
 *
 * Run with:  node --test static/js/uiux-batch7-settings.test.mjs   (from webui/)
 *
 * Latar belakang & dampak bisnis:
 *   - R28-lanjutan: settings.html adalah halaman terpadat (57 handler onclick
 *     inline). Inline onclick menggandakan logika di markup, sulit di-CSP,
 *     dan argumen yang diinterpolasi (mis. deleteApp({{ .ID }}, '{{ .Name }}'))
 *     rapuh terhadap nama aplikasi ber-kutip. Semua aksi bermigrasi ke
 *     delegasi `data-action` + registry Actions (admin-core.js, kontrak antar
 *     agen) dengan argumen dibawa lewat data-*.
 *   - S15-lanjutan: ±184 hex & ±191 rgba literal di satu halaman membuat
 *     perubahan tema harus menyentuh puluhan titik. Warna ber-padanan token
 *     dipindah ke var(--rgb-*), var(--color-*), kelas .tone-* / .notice-warning;
 *     baseline angka dikunci sebagai guard regresi.
 *
 * Kontrak antar-agen yang DIKONSUMSI (di-stub di harness bila belum terpasang):
 *   - admin-core.js: global `Actions = { register(name, fn), has(name) }`
 *     + satu listener klik global dokumen: closest('[data-action]') → fn(el, event).
 *   - theme.css/admin-base.css: triplet --rgb-success/warning/danger/info/accent,
 *     --glass-bg-strong, kelas .tone-* dan .notice-warning.
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

// Modul milik agen batch-7-settings. Batch 8: registrasi wrapper kini juga
// valid di modul pemilik fungsinya (admin.js, settings-vouchers.js) dan di
// admin-core.js (kanonik modal-dismiss) — semuanya ikut dalam union sumber.
const OWN_MODULES = [
    'settings-users.js',
    'settings-general.js',
    'settings-packages.js',
    'settings-billing.js',
    'settings-voucher-audit.js',
    'settings-system-apps.js'
].map((f) => ({ file: f, src: read('static/js/' + f) }));

const ADMIN_CORE_SRC = read('static/js/admin-core.js');
const ADMIN_JS_SRC = read('static/js/admin.js');
const VOUCHERS_JS_SRC = read('static/js/settings-vouchers.js');

// --- baseline guard (S15-lanjutan) -------------------------------------------

const HEX_RE = /#[0-9a-fA-F]{6}\b|#[0-9a-fA-F]{3}\b/g;
const countMatches = (src, re) => (src.match(re) || []).length;

test('GUARD token: settings.html hex ≤ 101 & rgba literal ≤ 124 (reduksi ≥45%/≥35% dari 184/191)', () => {
    const hex = countMatches(SETTINGS, HEX_RE);
    // Yang dihitung hanya rgba() LITERAL ber-komponen numerik; bentuk
    // rgba(var(--rgb-*), α) justru hasil migrasi yang diinginkan.
    const rgba = countMatches(SETTINGS, /rgba\(\s*\d/gi);
    assert.ok(hex <= 101, `hex settings.html = ${hex}, harus ≤ 101 (baseline awal 184, reduksi ≥45%)`);
    assert.ok(rgba <= 124, `rgba literal settings.html = ${rgba}, harus ≤ 124 (baseline awal 191, reduksi ≥35%)`);
});

test('GUARD token: total hex keenam modul settings milik batch ini ≤ 4', () => {
    const total = OWN_MODULES.reduce((n, m) => n + countMatches(m.src, HEX_RE), 0);
    assert.ok(total <= 4, `total hex modul settings = ${total}, harus ≤ 4`);
});

// --- R28-lanjutan: tidak ada inline onclick tersisa ---------------------------

test('R28: settings.html bebas onclick inline (=== 0)', () => {
    const n = countMatches(SETTINGS, /onclick=/g);
    assert.equal(n, 0, `masih ada ${n} atribut onclick di settings.html`);
});

/** Kumpulkan isi <script> TANPA src (inline) dari sebuah dokumen HTML. */
function inlineScripts(html) {
    const out = [];
    const re = /<script\b([^>]*)>([\s\S]*?)<\/script>/g;
    let m;
    while ((m = re.exec(html))) {
        if (!/\bsrc=/.test(m[1])) out.push(m[2]);
    }
    return out.join('\n');
}

const INLINE_SCRIPTS = (() => {
    const out = [];
    const re = /<script\b([^>]*)>([\s\S]*?)<\/script>/g;
    let m;
    while ((m = re.exec(SETTINGS))) {
        if (!/\bsrc=/.test(m[1])) out.push(m[2]);
    }
    return out;
})();
// Untuk pemindaian kontrak statik: semua script inline.
const INLINE_JS = INLINE_SCRIPTS.join('\n');
// Untuk eksekusi vm: buang sintaks Go template ({{ .X }}) dari script inline —
// di browser nilai itu sudah ter-render sebelum JS dieksekusi.
const INLINE_JS_EXEC = INLINE_SCRIPTS
    .map((s) => s.replace(/\{\{[^{}]*\}\}/g, '0'))
    .join('\n');

test('R28: SEMUA data-action di settings.html terdaftar (inline script ∪ modul milik batch ini)', () => {
    const used = new Set(
        [...SETTINGS.matchAll(/data-action="([a-z0-9-]+)"/g)].map((m) => m[1])
    );
    assert.ok(used.size > 20, `minimal 20 nama aksi unik dipakai HTML, dapat ${used.size}`);

    // Batch 8: registrasi wrapper pindah ke modul pemilik (admin.js /
    // settings-vouchers.js) dan kanonik modal-dismiss ke admin-core.js —
    // ketiganya dihitung valid selain inline script halaman.
    const sources = INLINE_JS + '\n'
        + OWN_MODULES.map((m) => m.src).join('\n') + '\n'
        + ADMIN_CORE_SRC + '\n' + ADMIN_JS_SRC + '\n' + VOUCHERS_JS_SRC;
    const registered = new Set(
        [...sources.matchAll(/Actions\.register\(\s*['"]([a-z0-9-]+)['"]/g)].map((m) => m[1])
    );
    const missing = [...used].filter((name) => !registered.has(name));
    assert.deepEqual(missing, [],
        `aksi tanpa register di sumber manapun: ${missing.join(', ')}`);
});

test('R28: elemen non-button bekas onclick (header sort) tetap keyboard-operable', () => {
    // 8 kolom sort users kini memakai data-action + tetap punya tabindex="0".
    const ths = SETTINGS.match(/<th[^>]*data-action="users-toggle-sort"[^>]*>/g) || [];
    assert.equal(ths.length, 8, `8 header kolom sort harus bermigrasi, dapat ${ths.length}`);
    for (const tag of ths) {
        assert.match(tag, /tabindex="0"/, 'header sort wajib tetap fokusable: ' + tag);
        assert.match(tag, /data-sort="/, 'argumen sort wajib dibawa data-sort');
    }
});

test('R28: backdrop modal pakai pola modal-dismiss + data-modal-close, role="dialog" utuh (S25)', () => {
    // Overlay yang dulu onclick="if(event.target===this)closeXxx()" kini
    // menandai dirinya dengan modal-dismiss; fungsi penutup disebut via nama.
    for (const [id, closeFn] of [
        ['confirmRedeemModal', 'closeConfirmRedeemModal'],
        ['singleModal', 'closeSingleModal'],
        ['batchModal', 'closeBatchModal'],
        ['redemptionsModal', 'closeRedemptionsModal'],
        // Batch 12 (T22): confirmActionModal DIHAPUS — konfirmasi voucher
        // memakai showConfirm core; entri dari daftar dikeluarkan.
        ['uploadModal', 'closeUploadModal']
    ]) {
        const tag = new RegExp(`<div[^>]*id="${id}"[^>]*>`).exec(SETTINGS);
        assert.ok(tag, `overlay #${id} harus ada`);
        assert.match(tag[0], /data-action="modal-dismiss"/, `#${id} wajib modal-dismiss`);
        assert.match(tag[0], new RegExp(`data-modal-close="${closeFn}"`), `#${id} wajib menunjuk ${closeFn}`);
        // Regresi S25 (batch5): semantik dialog TIDAK boleh diganti role button.
        assert.match(tag[0], /role="dialog"/, `#${id} tetap role="dialog"`);
    }
});

// --- harness perilaku (vm) ----------------------------------------------------

function fakeNode(tag) {
    const node = {
        tagName: String(tag || '').toUpperCase(),
        children: [],
        parentNode: null,
        attrs: {},
        className: '',
        innerHTML: '',
        textContent: '',
        style: {},
        value: '',
        disabled: false,
        dataset: {},
        offsetWidth: 0,
        classList: {
            _set: new Set(),
            add(...c) { c.forEach((x) => node.classList._set.add(x)); },
            remove(...c) { c.forEach((x) => node.classList._set.delete(x)); },
            contains(c) { return node.classList._set.has(c); },
            toggle(c, force) {
                const want = force === undefined ? !node.classList._set.has(c) : !!force;
                want ? node.classList._set.add(c) : node.classList._set.delete(c);
                return want;
            }
        }
    };
    node.getAttribute = (n) => (Object.prototype.hasOwnProperty.call(node.attrs, n) ? node.attrs[n] : null);
    node.setAttribute = (n, v) => { node.attrs[n] = String(v); };
    node.removeAttribute = (n) => { delete node.attrs[n]; };
    Object.defineProperty(node, 'id', {
        get() { return node.attrs.id || ''; },
        set(v) { if (v) node.attrs.id = String(v); else delete node.attrs.id; }
    });
    // closest minimal: cukup untuk selector '#id' dan '[data-action]'.
    node.closest = (sel) => {
        let cur = node;
        while (cur) {
            if (sel === '[data-action]' && cur.attrs['data-action'] !== undefined) return cur;
            const idSel = /^#([\w-]+)$/.exec(sel);
            if (idSel && cur.attrs.id === idSel[1]) return cur;
            cur = cur.parentNode;
        }
        return null;
    };
    node.appendChild = (c) => { c.parentNode = node; node.children.push(c); };
    node.remove = () => {};
    node.addEventListener = () => {};
    node.scrollIntoView = () => {};
    return node;
}

/**
 * Muat halaman settings.html dalam sandbox dengan URUTAN <script> aslinya:
 * admin-core.js dulu (kontrak Actions + listener delegasi dokumen), baru
 * script inline. Batch 8: shim defensif di inline script DIHAPUS — kalau
 * kontrak core rusak, test ini yang harus merah.
 */
function loadPageInlineScripts() {
    const allNodes = [];
    const reg = (n) => { allNodes.push(n); return n; };
    const docListeners = {};
    const byId = {};

    const docMock = {
        readyState: 'complete',
        body: reg(fakeNode('body')),
        // document.write dipakai blok render ukuran aplikasi (Go template);
        // no-op agar harness vm bisa mengeksekusi script inline utuh.
        write() {},
        getElementById(id) { return byId[id] || null; },
        createElement(tag) { return reg(fakeNode(tag)); },
        querySelector() { return null; },
        querySelectorAll() { return []; },
        addEventListener(type, fn) { (docListeners[type] = docListeners[type] || []).push(fn); },
        removeEventListener() {},
        head: reg(fakeNode('head'))
    };

    const win = {
        location: { hash: '', href: '', replace() {} },
        __adminRole: 'superadmin',
        __storageFreeMb: 0,
        addEventListener() {},
        removeEventListener() {}
    };
    win.window = win;

    function MutationObserverMock() {}
    MutationObserverMock.prototype.observe = function () {};
    MutationObserverMock.prototype.disconnect = function () {};

    const sandbox = {
        window: win,
        document: docMock,
        location: win.location,
        history: { replaceState() {}, pushState() {} },
        setTimeout: () => 0,
        clearTimeout() {},
        setInterval: () => 0,
        clearInterval() {},
        localStorage: { getItem: () => null, setItem() {} },
        // Stub API yang dipakai admin-core.js saat load.
        CustomEvent: function (t, opts) { this.type = t; this.detail = (opts && opts.detail) || null; },
        MouseEvent: function (type) { this.type = type; },
        MutationObserver: MutationObserverMock,
        getComputedStyle: () => ({ display: 'block' }),
        navigator: {},
        console
    };
    sandbox.globalThis = sandbox;
    vm.createContext(sandbox);
    vm.runInContext(ADMIN_CORE_SRC, sandbox, { filename: 'admin-core.js' });
    // Di browser `var Actions` top-level menjadi properti window; vm
    // memisahkan keduanya — cerminkan agar identik dengan runtime asli.
    win.Actions = sandbox.Actions;
    vm.runInContext(INLINE_JS_EXEC, sandbox, { filename: 'settings.html#inline' });

    return {
        sandbox,
        win,
        byId,
        makeNode: (tag, attrs) => {
            const n = reg(fakeNode(tag));
            Object.entries(attrs || {}).forEach(([k, v]) => n.setAttribute(k, v));
            return n;
        },
        /** Simulasikan klik nyata: jalankan semua listener klik dokumen. */
        click(el) {
            const ev = { target: el, preventDefault() {}, stopPropagation() {} };
            (docListeners.click || []).forEach((fn) => fn(ev));
            return ev;
        }
    };
}

test('PERILAKU: delegasi klik dokumen (admin-core) menjalankan aksi terdaftar dengan argumen data-*', () => {
    const env = loadPageInlineScripts();

    // Batch 8: shim defensif DIHAPUS — Actions pasti berasal dari
    // admin-core.js yang dimuat lebih dulu (urutan <script> halaman).
    assert.equal(typeof env.sandbox.Actions.register, 'function',
        'Actions harus tersedia dari admin-core.js (bukan shim halaman)');
    assert.equal(typeof env.sandbox.Actions.has, 'function');
    assert.doesNotMatch(INLINE_JS_EXEC, /window\.Actions\s*=\s*\{/,
        'inline script tidak boleh memasang shim registry sendiri lagi');

    // Registrasi users-toggle-sort kini hidup di admin.js (modul pemilik,
    // Batch 8) — eksekusi potongan aslinya di sandbox yang sama.
    const reg = ADMIN_JS_SRC.match(/Actions\.register\(\s*'users-toggle-sort'[\s\S]*?\}\);/);
    assert.ok(reg, 'registrasi users-toggle-sort ada di admin.js');
    vm.runInContext(reg[0], env.sandbox, { filename: 'admin.js#users-toggle-sort' });

    const calls = [];
    env.sandbox.toggleUsersSort = (field) => calls.push(field);

    const th = env.makeNode('th', { 'data-action': 'users-toggle-sort', 'data-sort': 'username' });
    const child = env.makeNode('span');
    child.parentNode = th; // klik pada <span> di dalam th

    env.click(child);
    env.click(th);

    assert.deepEqual(calls, ['username', 'username'],
        'delegasi harus resolve closest([data-action]) lalu memanggil handler dengan data-sort');
});

test('PERILAKU: modal-dismiss kanonik (admin-core) hanya menutup saat klik LANGSUNG pada backdrop', () => {
    const env = loadPageInlineScripts();

    let closed = 0;
    env.win.closeSingleModal = () => { closed += 1; };

    const overlay = env.makeNode('div', {
        id: 'singleModal',
        'data-action': 'modal-dismiss',
        'data-modal-close': 'closeSingleModal'
    });

    // Klik di dalam kartu modal (target ≠ overlay) → jangan menutup.
    const card = env.makeNode('div');
    card.parentNode = overlay;
    env.click(card);
    assert.equal(closed, 0, 'klik konten modal tidak boleh menutup (meniru if(event.target===this))');

    // Klik langsung backdrop → tutup via fungsi yang ditunjuk data-modal-close.
    env.click(overlay);
    assert.equal(closed, 1, 'klik backdrop memanggil window.closeSingleModal');
});

test('PERILAKU: aksi modul system-apps menerima data-id/data-name & lewat showConfirm', async () => {
    // Sandbox terpisah: Actions di-stub (kontrak admin-core), lalu modul ASLI
    // dimuat — register milik modul harus masuk ke registry stub.
    const registered = {};
    const docListeners = {};
    const byId = {};
    const mkEl = () => fakeNode('div');

    const docMock = {
        readyState: 'complete',
        getElementById(id) { return byId[id] || null; },
        createElement: () => mkEl(),
        querySelector: () => null,
        querySelectorAll: () => [],
        addEventListener(type, fn) { (docListeners[type] = docListeners[type] || []).push(fn); },
        head: mkEl()
    };
    const win = { __settingsReady: {}, location: { search: '', replaceState() {} }, addEventListener() {} };
    win.window = win;
    win.Actions = {
        register(name, fn) { registered[name] = fn; },
        has: (n) => typeof registered[n] === 'function'
    };

    const apiCalls = [];
    win.apiFetch = (url) => {
        apiCalls.push(url);
        return Promise.resolve({ json: () => Promise.resolve({ success: true }) });
    };
    const confirmArgs = [];
    let confirmResult = true;
    win.showConfirm = (...a) => { confirmArgs.push(a); return Promise.resolve(confirmResult); };
    win.showToast = () => {};

    const sandbox = { window: win, document: docMock, URLSearchParams, console,
        setTimeout: () => 0, clearTimeout() {},
        // Di browser properti window = global; vm memisahkan keduanya, jadi
        // identifier telanjang (showConfirm/apiFetch) diarahkan ke sandbox.
        get showConfirm() { return win.showConfirm; },
        get apiFetch() { return win.apiFetch; },
        get showToast() { return win.showToast; } };
    sandbox.globalThis = sandbox;
    vm.createContext(sandbox);
    vm.runInContext(OWN_MODULES.find((m) => m.file === 'settings-system-apps.js').src,
        sandbox, { filename: 'settings-system-apps.js' });

    assert.equal(typeof registered['app-delete'], 'function',
        'modul system-apps wajib meregister aksi app-delete sendiri');

    // Delegasi kontrak: fn(el, event).
    const el = mkEl();
    el.setAttribute('data-action', 'app-delete');
    el.setAttribute('data-app-id', '42');
    el.setAttribute('data-app-name', 'Client "CBT" v1');
    registered['app-delete'](el, { target: el });
    await new Promise((r) => setImmediate(r));
    await new Promise((r) => setImmediate(r));

    assert.equal(confirmArgs.length, 1, 'hapus aplikasi wajib konfirmasi dulu');
    assert.ok(String(confirmArgs[0][0]).includes('Client "CBT" v1'),
        'nama dari data-app-name (kutip aman) harus sampai ke showConfirm');
    assert.ok(apiCalls.some((u) => String(u).includes('/admin/api/system-apps/42/delete')),
        'id numerik dari data-app-id harus sampai ke endpoint hapus');

    // Konfirmasi ditolak → tidak ada request.
    confirmArgs.length = 0;
    apiCalls.length = 0;
    confirmResult = false;
    registered['app-delete'](el, { target: el });
    await new Promise((r) => setImmediate(r));
    assert.equal(apiCalls.length, 0, 'showConfirm ditolak → tidak boleh DELETE');
});
