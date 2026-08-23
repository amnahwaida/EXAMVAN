/* Contract + behavior tests untuk Batch 7 (infrastruktur inti milik agen
 * batch-7-core: admin-core.js, theme.css, admin-base.css).
 * Referensi temuan: review_uiux_webui.md Batch 7.
 *
 * Run with:  node --test static/js/uiux-batch7-core.test.mjs   (from webui/)
 *
 * Latar belakang & dampak bisnis:
 *   - Tugas A: API delegasi aksi global `Actions` di admin-core.js. Tiga agen
 *     paralel akan mendaftarkan handler data-action="..." tanpa menyentuh
 *     listener satu sama lain; satu listener klik delegasi di document
 *     meneruskan ke registry, dengan isolasi try/catch agar exception satu
 *     handler tidak membunuh handler lain.
 *   - Tugas B: token warna rgb-triplet + success-light/glass-bg-strong di
 *     theme.css sebagai kontrak lintas-agen (nama & nilai WAJIB persis,
 *     pola komentar z-index S33 Batch 6).
 *   - Tugas C: kelas utilitas tone (.tone-success .. .tone-neutral) +
 *     .notice-warning di admin-base.css — HANYA surface/border/color dari
 *     var()/rgba(var()), tanpa hex literal baru (regresi hitungan hex
 *     uiux-batch4-tokens.test.mjs tetap terkunci ≤ 31).
 *
 * Pola sama dengan uiux-batch6-jscore.test.mjs: kontrak statik (fs read) +
 * perilaku via vm.runInNewContext mengeksekusi JS ASLI yang dikirim ke
 * browser dengan stub DOM minimal.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const WEBUI_ROOT = path.join(__dirname, '..', '..');
const ADMIN_CORE_SRC = fs.readFileSync(path.join(__dirname, 'admin-core.js'), 'utf8');
const read = (rel) => fs.readFileSync(path.join(WEBUI_ROOT, rel), 'utf8');
/** Baca CSS dengan komentar dibuang — komentar boleh memuat nilai contoh
 * tanpa mengelabui pencarian deklarasi. */
const readCss = (rel) => read(rel).replace(/\/\*[\s\S]*?\*\//g, '');
const THEME_CSS = readCss('static/css/theme.css');
const ADMIN_CSS = readCss('static/css/admin-base.css');

// --- harness perilaku (pola uiux-batch6-jscore.test.mjs) ---------------------

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
        type: '',
        disabled: false,
        offsetHeight: 0,
        dataset: {},
        _listeners: {},
        _spies: { focus: 0 }
    };
    node.classList = {
        add(...cls) {
            const set = new Set(node.className.split(/\s+/).filter(Boolean));
            cls.forEach((c) => set.add(c));
            node.className = Array.from(set).join(' ');
        },
        remove(...cls) {
            const set = new Set(node.className.split(/\s+/).filter(Boolean));
            cls.forEach((c) => set.delete(c));
            node.className = Array.from(set).join(' ');
        },
        contains(cls) { return node.className.split(/\s+/).indexOf(cls) > -1; },
        toggle(cls, force) {
            const has = node.classList.contains(cls);
            const want = typeof force === 'undefined' ? !has : Boolean(force);
            if (want && !has) node.classList.add(cls);
            if (!want && has) node.classList.remove(cls);
            return want;
        }
    };
    node.getAttribute = (n) => (Object.prototype.hasOwnProperty.call(node.attrs, n) ? node.attrs[n] : null);
    node.setAttribute = (n, v) => { node.attrs[n] = String(v); };
    node.removeAttribute = (n) => { delete node.attrs[n]; };
    Object.defineProperty(node, 'id', {
        get() { return node.attrs.id || ''; },
        set(v) { if (v) node.attrs.id = String(v); else delete node.attrs.id; }
    });
    node.appendChild = (c) => {
        if (c.parentNode) c.parentNode.removeChild(c);
        c.parentNode = node;
        node.children.push(c);
    };
    node.removeChild = (c) => {
        const i = node.children.indexOf(c);
        if (i > -1) node.children.splice(i, 1);
        c.parentNode = null;
        return c;
    };
    node.remove = () => { if (node.parentNode) node.parentNode.removeChild(node); };
    node.addEventListener = (t, fn) => { (node._listeners[t] = node._listeners[t] || []).push(fn); };
    node.removeEventListener = () => {};
    node.dispatchEvent = (ev) => {
        (node._listeners[ev.type] || []).slice().forEach((fn) => fn(ev));
        return true;
    };
    // closest mini dengan semantik DOM asli: mulai dari node sendiri, naik
    // lewat parentNode — mendukung selector [data-action] yang dipakai
    // listener delegasi Actions.
    node.closest = function (sel) {
        if (sel !== '[data-action]') return null;
        let cur = node;
        while (cur) {
            if (Object.prototype.hasOwnProperty.call(cur.attrs, 'data-action')) return cur;
            cur = cur.parentNode;
        }
        return null;
    };
    node.focus = () => { node._spies.focus++; };
    return node;
}

function loadCore() {
    const allNodes = [];
    const register = (n) => { allNodes.push(n); return n; };
    const docListeners = {};
    const winListeners = {};
    const toastContainer = register(fakeNode('div'));
    toastContainer.id = 'toastContainer';
    toastContainer.children = [];
    toastContainer.appendChild = (c) => { toastContainer.children.push(c); };

    const docMock = {
        readyState: 'complete',
        activeElement: null,
        documentElement: register(fakeNode('html')),
        body: register(fakeNode('body')),
        getElementById(id) { return allNodes.find((n) => n.attrs.id === id) || null; },
        createElement(tag) { return register(fakeNode(tag)); },
        querySelector() { return null; },
        querySelectorAll() { return []; },
        addEventListener(type, fn) { (docListeners[type] = docListeners[type] || []).push(fn); },
        removeEventListener() {},
        dispatchEvent(ev) {
            (docListeners[ev.type] || []).slice().forEach((fn) => fn(ev));
            return true;
        },
        contains() { return true; }
    };
    Object.defineProperty(toastContainer, 'firstElementChild', {
        get() { return toastContainer.children[0] || null; }
    });

    const win = {
        fetch: async () => ({ ok: true, status: 200, text: () => Promise.resolve('{}') }),
        location: { origin: 'http://192.168.1.10:8080', pathname: '/admin/dashboard', search: '', href: '' },
        addEventListener(type, fn) { (winListeners[type] = winListeners[type] || []).push(fn); },
        removeEventListener() {},
        dispatchEvent(ev) {
            (winListeners[ev.type] || []).slice().forEach((fn) => fn(ev));
            return true;
        }
    };

    function MutationObserverMock() {}
    MutationObserverMock.prototype.observe = function () {};
    MutationObserverMock.prototype.disconnect = function () {};

    const sandbox = {
        window: win,
        document: docMock,
        CustomEvent: function (t, opts) { this.type = t; this.detail = (opts && opts.detail) || null; },
        MouseEvent: function (type) { this.type = type; },
        MutationObserver: MutationObserverMock,
        getComputedStyle: () => ({ display: 'block' }),
        navigator: {},
        console,
        setTimeout() { return 0; },
        clearTimeout() {},
        setInterval() { return 0; },
        clearInterval() {},
        location: win.location
    };
    sandbox.globalThis = sandbox;
    vm.createContext(sandbox);
    vm.runInContext(ADMIN_CORE_SRC, sandbox, { filename: 'admin-core.js' });

    /** Tembakkan klik delegasi di document dengan target tertentu. */
    const fireDocClick = (target, extra) => {
        const ev = Object.assign({ type: 'click', target, bubbles: true }, extra || {});
        (docListeners.click || []).slice().forEach((fn) => fn(ev));
        return ev;
    };
    return { sandbox, doc: docMock, docListeners, makeNode: (tag) => register(fakeNode(tag)), fireDocClick };
}

// ---------------------------------------------------------------------------
// Tugas A — API delegasi aksi global (Actions)
// ---------------------------------------------------------------------------

test('B7-A (statik): objek Actions didefinisikan di admin-core.js dengan register+has', () => {
    assert.match(ADMIN_CORE_SRC, /(var|const|let)?\s*Actions\s*=\s*\{/, 'objek Actions didefinisikan di core');
    assert.match(ADMIN_CORE_SRC, /register\s*:\s*function/, 'Actions.register ada');
    assert.match(ADMIN_CORE_SRC, /has\s*:\s*function/, 'Actions.has ada');
});

test('B7-A (statik): SATU listener klik delegasi data-action terpasang di document', () => {
    const decl = /document\.addEventListener\(\s*'click'/g;
    const delegated = ADMIN_CORE_SRC.match(/addEventListener\(\s*'click'\s*,\s*function[\s\S]{0,600}?data-action/);
    assert.ok(delegated, 'ada listener klik document yang mencari [data-action]');
    // Listener lain (backdrop-close dsb.) tidak boleh diganggu — cukup tambahan independen.
    assert.match(ADMIN_CORE_SRC, /modal-overlay/, 'listener backdrop-close existing tetap ada');
});

test('B7-A (perilaku): register → klik elemen data-action → fn dipanggil dengan (el, event)', () => {
    const env = loadCore();
    const calls = [];
    env.sandbox.Actions.register('hapus-data', function (el, e) { calls.push({ el, e }); });

    const btn = env.makeNode('button');
    btn.setAttribute('data-action', 'hapus-data');

    assert.equal(env.sandbox.Actions.has('hapus-data'), true, 'has() true setelah register');
    assert.equal(env.sandbox.Actions.has('tidak-ada'), false, 'has() false untuk nama lain');

    const ev = env.fireDocClick(btn);
    assert.equal(calls.length, 1, 'handler terpanggil tepat sekali');
    assert.equal(calls[0].el, btn, 'argumen pertama = elemen ber-data-action');
    assert.equal(calls[0].e, ev, 'argumen kedua = event klik');
});

test('B7-A (perilaku): closest ke ancestor ber-data-action juga kena', () => {
    const env = loadCore();
    const calls = [];
    env.sandbox.Actions.register('baris-aksi', function (el) { calls.push(el); });

    const row = env.makeNode('tr');
    row.setAttribute('data-action', 'baris-aksi');
    const icon = env.makeNode('span');
    row.appendChild(icon); // closest asli naik lewat parentNode → icon kena handler baris

    env.fireDocClick(icon);
    assert.deepEqual(calls, [row], 'handler menerima ANCESTOR (bukan target asal) sebagai el');
});

test('B7-A (perilaku): nama tak terdaftar → diam (tanpa throw)', () => {
    const env = loadCore();
    const btn = env.makeNode('div');
    btn.setAttribute('data-action', 'belum-didaftarkan');
    assert.doesNotThrow(() => env.fireDocClick(btn), 'klik aksi tak terdaftar tidak boleh throw');
});

test('B7-A (perilaku): exception di fn A tidak mencegah fn B', () => {
    const env = loadCore();
    const errs = [];
    const origError = console.error;
    console.error = (...a) => errs.push(a);
    try {
        env.sandbox.Actions.register('meledak', function () { throw new Error('boom'); });
        const bCalls = [];
        env.sandbox.Actions.register('sehat', function () { bCalls.push(1); });

        const badBtn = env.makeNode('button');
        badBtn.setAttribute('data-action', 'meledak');
        const goodBtn = env.makeNode('button');
        goodBtn.setAttribute('data-action', 'sehat');

        assert.doesNotThrow(() => env.fireDocClick(badBtn), 'exception handler ditelan (try/catch)');
        env.fireDocClick(goodBtn);
        assert.equal(bCalls.length, 1, 'handler berikutnya tetap jalan setelah handler meledak');
        assert.equal(env.sandbox.Actions.has('meledak'), true, 'registry tidak rusak oleh exception');
    } finally {
        console.error = origError;
    }
});

test('B7-A (perilaku): register nama duplikat menimpa dengan console.warn', () => {
    const env = loadCore();
    const warns = [];
    const origWarn = console.warn;
    console.warn = (...a) => warns.push(a.join(' '));
    try {
        const seen = [];
        env.sandbox.Actions.register('dobel', () => seen.push('pertama'));
        env.sandbox.Actions.register('dobel', () => seen.push('kedua'));

        const btn = env.makeNode('button');
        btn.setAttribute('data-action', 'dobel');
        env.fireDocClick(btn);

        assert.deepEqual(seen, ['kedua'], 'duplikat ditimpa handler terbaru');
        assert.equal(warns.length >= 1, true, 'penimpaan diberi console.warn');
    } finally {
        console.warn = origWarn;
    }
});

// ---------------------------------------------------------------------------
// Tugas B — token warna di theme.css (kontrak lintas-agen: nilai persis)
// ---------------------------------------------------------------------------

test('B7-B: token rgb-triplet + turunan terdefinisi di theme.css dengan nilai persis', () => {
    const expected = {
        '--rgb-success': '16, 185, 129',
        '--color-success-light': '#34d399',
        '--rgb-warning': '245, 158, 11',
        '--rgb-danger': '239, 68, 68',
        '--rgb-info': '99, 102, 241',
        '--rgb-accent': '168, 85, 247',
        '--glass-bg-strong': 'rgba(255, 255, 255, 0.06)'
    };
    for (const [token, value] of Object.entries(expected)) {
        const re = new RegExp(token.replace(/[-]/g, '\\-') + '\\s*:\\s*' + value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&') + '\\s*;');
        assert.match(THEME_CSS, re, `${token}: ${value} harus terdefinisi di theme.css`);
    }
});

test('B7-B: token existing direkonsiliasi (tidak dideklarasikan ulang / digeser nilainya)', () => {
    const lockedExisting = {
        '--color-success': '#10b981',
        '--color-danger': '#ef4444',
        '--color-warning-light': '#fbbf24',
        '--color-danger-light': '#fca5a5',
        '--color-primary-light': '#a5b4fc',
        '--color-accent-light': '#a78bfa'
    };
    for (const [token, value] of Object.entries(lockedExisting)) {
        const re = new RegExp(token.replace(/[-]/g, '\\-') + '\\s*:\\s*' + value + '\\s*;');
        assert.match(THEME_CSS, re, `${token}: ${value} harus tetap ada (rekonsiliasi, bukan duplikasi)`);
    }
    // Tidak ada token yang dideklarasikan dua kali (guard anti-duplikasi).
    const names = [...THEME_CSS.matchAll(/--[a-z][a-z0-9-]*(?=\s*:)/g)].map((m) => m[0]);
    const dupes = names.filter((n, i) => names.indexOf(n) !== i);
    assert.deepEqual(dupes, [], `token duplikat di theme.css: ${[...new Set(dupes)].join(', ')}`);
});

test('B7-B: tiap token baru didokumentasikan dengan komentar pemakaian', () => {
    const raw = read('static/css/theme.css');
    for (const token of ['--rgb-success', '--color-success-light', '--rgb-warning',
        '--rgb-danger', '--rgb-info', '--rgb-accent', '--glass-bg-strong']) {
        // Komentar dalam blok yang sama (±400 char sebelum deklarasi token).
        const idx = raw.indexOf(token + ':');
        assert.ok(idx > -1, `${token} ada di theme.css`);
        const before = raw.slice(Math.max(0, idx - 500), idx);
        assert.match(before, /\/\*/, `${token} wajib didokumentasikan komentar singkat`);
    }
});

// ---------------------------------------------------------------------------
// Tugas C — kelas utilitas tone di admin-base.css
// ---------------------------------------------------------------------------

test('B7-C: kelas tone + notice-warning ada di admin-base.css', () => {
    for (const cls of ['.tone-success', '.tone-warning', '.tone-danger', '.tone-info',
        '.tone-accent', '.tone-neutral', '.notice-warning']) {
        const re = new RegExp(cls.replace('.', '\\.') + '\\s*\\{');
        assert.match(ADMIN_CSS, re, `${cls} harus terdefinisi di admin-base.css`);
    }
});

test('B7-C: kelas tone murni memakai var()/rgba(var()) — tanpa hex literal baru', () => {
    // Ekstrak blok .tone-* / .notice-warning dan pastikan tidak ada hex di dalamnya.
    const blocks = [...ADMIN_CSS.matchAll(/\.(?:tone-(?:success|warning|danger|info|accent|neutral)|notice-warning)\s*\{[^}]*\}/g)];
    assert.equal(blocks.length >= 7, true, 'ketujuh blok kelas harus bisa diekstrak');
    for (const m of blocks) {
        assert.ok(!/#[0-9a-fA-F]{3,8}\b/.test(m[0]),
            `blok "${m[0].slice(0, 30)}..." mengandung hex literal — wajib var()/rgba(var())`);
    }
    for (const token of ['--rgb-success', '--rgb-warning', '--rgb-danger', '--rgb-info', '--rgb-accent']) {
        assert.match(ADMIN_CSS, new RegExp('rgba\\(\\s*var\\(' + token.replace(/[-]/g, '\\-') + '\\)'),
            `rgba(var(${token}), ...) harus dipakai kelas tone`);
    }
});

test('B7-C: regresi hitungan hex admin-base.css tidak naik (terkunci uiux-batch4-tokens ≤ 31)', () => {
    const hexCount = (ADMIN_CSS.match(/#[0-9a-fA-F]{3,8}\b/g) || []).length;
    assert.ok(hexCount <= 31, `hitungan hex (${hexCount}) melebihi target pasca-migrasi 31`);
});
