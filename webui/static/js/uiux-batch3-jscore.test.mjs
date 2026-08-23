/* Contract + behavior tests untuk Batch 3 (bagian JS core).
 * Referensi temuan: review_uiux_webui.md di root repo (ID: S19, T10b, S20,
 * S18, S5).
 *
 * Run with:  node --test static/js/uiux-batch3-jscore.test.mjs   (from webui/)
 *
 * Pola sama dengan admin-core.test.mjs: file admin-core.js ASLI yang dikirim
 * ke browser dieksekusi di Node vm dengan mock DOM/globals. Mock di sini
 * sedikit lebih lengkap (parent/child DOM, classList berbasis className,
 * clock manual) karena helper S19/S5 memang manipulasi DOM + timer:
 *   - S19: setFieldError / clearFieldError / clearFieldErrors
 *   - S20: bajakan Ctrl+F dihapus (statis + perilaku)
 *   - S18: initMenuToggle mensinkronkan aria-expanded
 *   - S5 : initLiveSearch debounce + Enter langsung
 *   - T10b: settings-system-apps.js memakai validasi field-level (statis)
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

// --- test harness -----------------------------------------------------------

// Elemen DOM palsu: cukup untuk kebutuhan admin-core.js (classList berbasis
// string className, atribut, parent/child, listener, nextSibling).
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
        files: null,
        offsetHeight: 0,
        _listeners: {},
        _spies: { focus: 0, select: 0, scrollIntoView: 0 }
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
        contains(cls) {
            return node.className.split(/\s+/).indexOf(cls) > -1;
        },
        toggle(cls, force) {
            const has = node.classList.contains(cls);
            const want = typeof force === 'undefined' ? !has : Boolean(force);
            if (want && !has) node.classList.add(cls);
            if (!want && has) node.classList.remove(cls);
            return want;
        }
    };
    node.getAttribute = (name) =>
        Object.prototype.hasOwnProperty.call(node.attrs, name) ? node.attrs[name] : null;
    node.setAttribute = (name, val) => { node.attrs[name] = String(val); };
    node.removeAttribute = (name) => { delete node.attrs[name]; };
    Object.defineProperty(node, 'id', {
        get() { return node.attrs.id || ''; },
        set(v) {
            if (v === undefined || v === null || v === '') delete node.attrs.id;
            else node.attrs.id = String(v);
        }
    });
    Object.defineProperty(node, 'nextSibling', {
        get() {
            if (!node.parentNode) return null;
            const sibs = node.parentNode.children;
            const i = sibs.indexOf(node);
            return i > -1 && i < sibs.length - 1 ? sibs[i + 1] : null;
        }
    });
    node.appendChild = (c) => {
        if (c.parentNode) c.parentNode.removeChild(c);
        c.parentNode = node;
        node.children.push(c);
    };
    node.insertBefore = (c, ref) => {
        const i = ref ? node.children.indexOf(ref) : -1;
        if (i === -1) { node.appendChild(c); return; }
        if (c.parentNode) c.parentNode.removeChild(c);
        node.children.splice(i, 0, c);
        c.parentNode = node;
    };
    node.removeChild = (c) => {
        const i = node.children.indexOf(c);
        if (i > -1) node.children.splice(i, 1);
        c.parentNode = null;
        return c;
    };
    node.remove = () => { if (node.parentNode) node.parentNode.removeChild(node); };
    node.contains = (other) => {
        let cur = other;
        while (cur) { if (cur === node) return true; cur = cur.parentNode; }
        return false;
    };
    node.closest = () => null;
    node.addEventListener = (t, fn) => { (node._listeners[t] = node._listeners[t] || []).push(fn); };
    node.removeEventListener = (t, fn) => {
        const a = node._listeners[t] || [];
        const i = a.indexOf(fn);
        if (i > -1) a.splice(i, 1);
    };
    node.dispatchEvent = (ev) => {
        // Elemen nyata memicu properti onclick lewat dispatch juga.
        if (ev.type === 'click' && typeof node.onclick === 'function') node.onclick(ev);
        (node._listeners[ev.type] || []).slice().forEach((fn) => fn(ev));
        return true;
    };
    node.querySelector = () => null;
    // Selector sederhana 'tag, tag, ...' — cukup untuk clearFieldErrors.
    node.querySelectorAll = (sel) => {
        const parts = String(sel).split(',').map((s) => s.trim().toUpperCase()).filter(Boolean);
        const out = [];
        (function walk(n) {
            n.children.forEach((c) => {
                if (parts.indexOf(c.tagName) > -1) out.push(c);
                walk(c);
            });
        })(node);
        return out;
    };
    node.focus = () => { node._spies.focus += 1; };
    node.select = () => { node._spies.select += 1; };
    node.scrollIntoView = () => { node._spies.scrollIntoView += 1; };
    return node;
}

// Memuat admin-core.js ASLI di vm dengan mock DOM + clock manual.
// setTimeout TIDAK otomatis menjalankan callback — gunakan flushTimers().
function loadAdminCore() {
    const allNodes = [];
    const register = (n) => { allNodes.push(n); return n; };
    const docListeners = {};
    const clock = { seq: 0, pending: new Map(), durations: [] };

    const documentMock = {
        readyState: 'complete',
        activeElement: null,
        documentElement: register(fakeNode('html')),
        body: register(fakeNode('body')),
        getElementById(id) {
            const inDocument = (n) => {
                let cur = n;
                while (cur) {
                    if (cur === documentMock.body || cur === documentMock.documentElement) return true;
                    cur = cur.parentNode;
                }
                return false;
            };
            return allNodes.find((n) => n.attrs && n.attrs.id === id && inDocument(n)) || null;
        },
        createElement(tag) { return register(fakeNode(tag)); },
        // Hanya meta csrf yang dikenali; selektor lain null (modal manager
        // melihat dokumen tanpa overlay — sama seperti harness admin-core).
        querySelector(sel) {
            return sel === 'meta[name="csrf-token"]' ? { getAttribute: () => 'test-csrf-token' } : null;
        },
        querySelectorAll() { return []; },
        addEventListener(type, fn) { (docListeners[type] = docListeners[type] || []).push(fn); },
        removeEventListener(type, fn) {
            const a = docListeners[type] || [];
            const i = a.indexOf(fn);
            if (i > -1) a.splice(i, 1);
        },
        dispatchEvent(ev) {
            (docListeners[ev.type] || []).slice().forEach((fn) => fn(ev));
            return true;
        },
        contains() { return true; }
    };

    function MutationObserverMock() {}
    MutationObserverMock.prototype.observe = function () {};
    MutationObserverMock.prototype.disconnect = function () {};

    const sandbox = {
        window: { fetch: async () => ({ ok: true, status: 200, text: () => Promise.resolve('') }), CustomEvent: function (t) { this.type = t; } },
        document: documentMock,
        CustomEvent: function (t) { this.type = t; },
        MutationObserver: MutationObserverMock,
        MouseEvent: function (type) { this.type = type; },
        getComputedStyle: () => ({ display: 'block' }),
        navigator: {},
        console,
        // Clock manual: durasi dicatat, callback antre sampai flushTimers().
        setTimeout(fn, ms) {
            const id = ++clock.seq;
            clock.pending.set(id, fn);
            clock.durations.push(ms);
            return id;
        },
        clearTimeout(id) { clock.pending.delete(id); },
        setInterval() { return 0; },
        clearInterval() {},
        location: { href: '' }
    };
    sandbox.globalThis = sandbox;
    vm.createContext(sandbox);
    vm.runInContext(ADMIN_CORE_SRC, sandbox, { filename: 'admin-core.js' });

    return {
        sandbox,
        doc: documentMock,
        docListeners,
        clock,
        makeNode: (tag) => register(fakeNode(tag)),
        flushTimers() {
            let guard = 0;
            while (clock.pending.size && guard++ < 1000) {
                const [id, fn] = clock.pending.entries().next().value;
                clock.pending.delete(id);
                fn();
            }
        }
    };
}

function keyEvent(overrides) {
    return Object.assign({
        type: 'keydown',
        key: '',
        ctrlKey: false,
        metaKey: false,
        shiftKey: false,
        defaultPrevented: false,
        target: null,
        preventDefault() { this.defaultPrevented = true; }
    }, overrides);
}

// ---------------------------------------------------------------------------
// S19 — helper validasi field
// ---------------------------------------------------------------------------

test('S19: setFieldError inject <p role=alert> tepat setelah input + aria-invalid + input-error + aria-describedby', () => {
    const env = loadAdminCore();
    const host = env.makeNode('div');
    env.doc.body.appendChild(host);
    const input = env.makeNode('input');
    input.id = 'examName';
    host.appendChild(input);

    env.sandbox.setFieldError(input, 'Nama ujian wajib diisi');

    const errEl = env.doc.getElementById('examName-error');
    assert.ok(errEl, 'elemen error dengan id {input.id}-error harus ada');
    assert.equal(errEl.tagName, 'P');
    assert.equal(errEl.attrs.role, 'alert', 'error text wajib role="alert"');
    assert.equal(errEl.className, 'field-error-text');
    assert.equal(errEl.textContent, 'Nama ujian wajib diisi');
    assert.equal(host.children[1], errEl, 'error <p> harus tepat setelah input');
    assert.equal(input.attrs['aria-invalid'], 'true');
    assert.ok(input.className.includes('input-error'), 'input harus diberi class input-error');
    assert.equal(input.attrs['aria-describedby'], 'examName-error');
});

test('S19: pemanggilan ulang setFieldError UPDATE pesan, bukan membuat duplikat', () => {
    const env = loadAdminCore();
    const host = env.makeNode('div');
    env.doc.body.appendChild(host);
    const input = env.makeNode('input');
    input.id = 'appToken';
    host.appendChild(input);

    env.sandbox.setFieldError(input, 'Token wajib diisi');
    const first = env.doc.getElementById('appToken-error');
    env.sandbox.setFieldError(input, 'Token harus 8 karakter A-Z0-9');

    assert.equal(host.children.length, 2, 'tidak boleh menumpuk <p> error');
    const now = env.doc.getElementById('appToken-error');
    assert.equal(now, first, 'elemen error yang sama dipakai ulang');
    assert.equal(now.textContent, 'Token harus 8 karakter A-Z0-9', 'pesan ter-update');
});

test('S19: aria-describedby pre-existing dijaga (append, bukan timpa)', () => {
    const env = loadAdminCore();
    const input = env.makeNode('input');
    input.id = 'newPassword';
    env.doc.body.appendChild(input);
    input.setAttribute('aria-describedby', 'pw-hint');

    env.sandbox.setFieldError(input, 'Password terlalu pendek');
    assert.equal(input.attrs['aria-describedby'], 'pw-hint newPassword-error',
        'referensi hint lama tetap ada, id error di-append');

    env.sandbox.clearFieldError(input);
    assert.equal(input.attrs['aria-describedby'], 'pw-hint',
        'clear hanya melepas id error, hint lama tetap terhubung');
});

test('S19: clearFieldError memulihkan semua state error', () => {
    const env = loadAdminCore();
    const host = env.makeNode('div');
    env.doc.body.appendChild(host);
    const input = env.makeNode('input');
    input.id = 'appFile';
    host.appendChild(input);

    env.sandbox.setFieldError(input, 'File aplikasi wajib dipilih');
    assert.ok(env.doc.getElementById('appFile-error'));

    env.sandbox.clearFieldError(input);

    assert.ok(!input.className.includes('input-error'), 'class input-error dihapus');
    assert.equal(input.attrs['aria-invalid'], undefined, 'aria-invalid dihapus');
    assert.equal(input.attrs['aria-describedby'], undefined, 'aria-describedby kosong dilepas');
    const errEl = env.doc.getElementById('appFile-error');
    assert.equal(errEl, null, '<p> error dihapus dari dokumen');
    assert.equal(host.children.length, 1, 'host kembali hanya berisi input');
});

test('S19: input tanpa id diberi id unik lalu error terhubung', () => {
    const env = loadAdminCore();
    const input = env.makeNode('input');
    env.doc.body.appendChild(input);

    env.sandbox.setFieldError(input, 'Wajib diisi');
    assert.ok(input.id, 'input tanpa id harus diberi id');
    const errEl = env.doc.getElementById(input.id + '-error');
    assert.ok(errEl, 'error element merujuk id yang digenerate');
    assert.equal(input.attrs['aria-describedby'], input.id + '-error');
});

test('S19: clearFieldErrors(container) membersihkan semua field dalam container', () => {
    const env = loadAdminCore();
    const form = env.makeNode('form');
    env.doc.body.appendChild(form);
    const ids = ['fName', 'fVersion', 'fFile'];
    ids.forEach((id) => {
        const f = env.makeNode('input');
        f.id = id;
        form.appendChild(f);
        env.sandbox.setFieldError(f, 'err ' + id);
    });

    env.sandbox.clearFieldErrors(form);

    ids.forEach((id) => {
        const f = env.doc.getElementById(id);
        assert.ok(!f.className.includes('input-error'), id + ' bersih dari class error');
        assert.equal(f.attrs['aria-invalid'], undefined, id + ' bersih dari aria-invalid');
        assert.equal(env.doc.getElementById(id + '-error'), null, id + ' kehilangan <p> error');
    });
});

// ---------------------------------------------------------------------------
// S20 — bajakan Ctrl+F / Cmd+F dihapus
// ---------------------------------------------------------------------------

test('S20: sumber admin-core.js tidak lagi memuat case \'f\' di shortcut Ctrl', () => {
    assert.equal(ADMIN_CORE_SRC.includes("case 'f':"), false,
        'binding Ctrl+F yang membatalkan find-in-browser harus dihapus');
});

test('S20: Ctrl+F tidak lagi preventDefault; shortcut / dan Ctrl+U tetap hidup', () => {
    const env = loadAdminCore();
    const search = env.makeNode('input');
    search.id = 'searchExam';
    env.doc.body.appendChild(search);
    const examName = env.makeNode('input');
    examName.id = 'examName';
    env.doc.body.appendChild(examName);

    env.sandbox.initKeyboardShortcuts();
    const evF = keyEvent({ key: 'f', ctrlKey: true, target: search });
    env.doc.dispatchEvent(evF);
    assert.equal(search._spies.focus, 0, 'Ctrl+F tidak boleh memfokuskan search lagi');
    assert.equal(evF.defaultPrevented, false, 'find-in-browser bawaan browser tidak dibajak');

    const evSlash = keyEvent({ key: '/', target: search });
    env.doc.dispatchEvent(evSlash);
    assert.equal(evSlash.defaultPrevented, true, "shortcut '/' tetap memfokuskan search");
    assert.equal(search._spies.focus, 1, "shortcut '/' tetap fokus ke #searchExam");

    const evU = keyEvent({ key: 'u', ctrlKey: true, target: examName });
    env.doc.dispatchEvent(evU);
    assert.equal(evU.defaultPrevented, true, 'Ctrl+U tetap dibajak (perilaku lama dipertahankan)');
    assert.equal(examName._spies.focus, 1, 'Ctrl+U tetap fokus ke #examName');
    assert.equal(examName._spies.scrollIntoView, 1);
});

// ---------------------------------------------------------------------------
// S18 — aria-expanded hamburger
// ---------------------------------------------------------------------------

test('S18: initMenuToggle mensinkronkan aria-expanded saat toggle & close-by-outside-click', () => {
    const env = loadAdminCore();
    const btn = env.makeNode('button');
    btn.id = 'menuToggleBtn';
    const dd = env.makeNode('div');
    dd.id = 'menuDropdownContent';
    env.doc.body.appendChild(btn);
    env.doc.body.appendChild(dd);

    env.sandbox.initMenuToggle();
    assert.equal(btn.getAttribute('aria-expanded'), null, 'belum ada state sebelum interaksi pertama tidak wajib; yang penting setelah toggle');

    btn.dispatchEvent({ type: 'click', stopPropagation() {} });
    assert.ok(dd.classList.contains('show'), 'menu terbuka');
    assert.equal(btn.getAttribute('aria-expanded'), 'true', 'aria-expanded=true saat menu terbuka');

    btn.dispatchEvent({ type: 'click', stopPropagation() {} });
    assert.ok(!dd.classList.contains('show'), 'menu tertutup');
    assert.equal(btn.getAttribute('aria-expanded'), 'false', 'aria-expanded=false saat menu tertutup');

    // Buka lagi, lalu klik di luar (outside-click) harus menutup + sinkron.
    btn.dispatchEvent({ type: 'click', stopPropagation() {} });
    assert.equal(btn.getAttribute('aria-expanded'), 'true');
    const outside = env.makeNode('div');
    env.doc.dispatchEvent({ type: 'click', target: outside });
    assert.ok(!dd.classList.contains('show'), 'outside-click menutup menu');
    assert.equal(btn.getAttribute('aria-expanded'), 'false', 'aria-expanded=false setelah close-by-outside-click');
});

// ---------------------------------------------------------------------------
// S5 — initLiveSearch
// ---------------------------------------------------------------------------

test('S5: dua keystroke cepat hanya memicu SATU callback (debounce)', () => {
    const env = loadAdminCore();
    const input = env.makeNode('input');
    env.doc.body.appendChild(input);
    let calls = 0;
    const handle = env.sandbox.initLiveSearch(input, () => { calls += 1; }, 50);
    assert.ok(handle, 'initLiveSearch mengembalikan handle teardown');

    input.dispatchEvent({ type: 'input' });
    input.dispatchEvent({ type: 'input' });
    input.dispatchEvent({ type: 'keyup', key: 'a' });
    assert.equal(calls, 0, 'belum ada pemanggilan sebelum delay lewat');

    env.flushTimers();
    assert.equal(calls, 1, 'keystroke beruntun = satu callback setelah delay');
});

test('S5: Enter memanggil callback LANGSUNG dan membatalkan timer pending (tidak dobel)', () => {
    const env = loadAdminCore();
    const input = env.makeNode('input');
    env.doc.body.appendChild(input);
    let calls = 0;
    env.sandbox.initLiveSearch(input, () => { calls += 1; }, 300);

    // Keystroke biasa meninggalkan timer pending...
    input.dispatchEvent({ type: 'input' });
    input.dispatchEvent({ type: 'keyup', key: 'x' });
    assert.ok(env.clock.pending.size > 0, 'debounce pending terjadwal');

    // ...lalu Enter: callback sinkron, pending dibatalkan.
    input.dispatchEvent({ type: 'keydown', key: 'Enter' });
    assert.equal(calls, 1, 'Enter memanggil callback langsung');
    assert.equal(env.clock.pending.size, 0, 'timer debounce yang pending dibatalkan');

    // Flush penuh: tidak ada callback susulan.
    env.flushTimers();
    assert.equal(calls, 1, 'tidak ada pemanggilan kedua dari timer lama');

    // keyup Enter tidak boleh menjadwalkan debounce lagi.
    input.dispatchEvent({ type: 'keyup', key: 'Enter' });
    env.flushTimers();
    assert.equal(calls, 1, 'keyup Enter tidak memicu callback kedua');
});

test('S5: delay default 300ms dan teardown melepas listener', () => {
    const env = loadAdminCore();
    const input = env.makeNode('input');
    env.doc.body.appendChild(input);
    let calls = 0;
    const handle = env.sandbox.initLiveSearch(input, () => { calls += 1; });
    assert.ok(handle, 'handle tetap dikembalikan tanpa delay eksplisit');

    input.dispatchEvent({ type: 'input' });
    assert.ok(env.clock.durations.includes(300), 'delay default 300ms');

    handle.destroy();
    assert.equal((input._listeners.input || []).length, 0, 'listener input dilepas');
    assert.equal((input._listeners.keydown || []).length, 0, 'listener keydown dilepas');
    assert.equal((input._listeners.keyup || []).length, 0, 'listener keyup dilepas');

    input.dispatchEvent({ type: 'input' });
    env.flushTimers();
    assert.equal(calls, 0, 'setelah destroy tidak ada callback lagi');
});

// ---------------------------------------------------------------------------
// T10b — settings-system-apps.js memakai validasi field-level (kontrak statik)
// ---------------------------------------------------------------------------

test('T10b: settings-system-apps.js memanggil setFieldError + listener blur/input live', () => {
    const js = read('static/js/settings-system-apps.js');
    assert.match(js, /setFieldError\(/, 'validasi field-level harus memakai helper setFieldError');
    assert.match(js, /clearFieldError/, 'error harus bisa dibersihkan saat field diperbaiki');
    assert.match(js, /addEventListener\('blur'/, 'validasi saat blur');
    assert.match(js, /addEventListener\('input'/, 'pembersihan live saat mengetik');
    assert.match(js, /typeof setFieldError === 'function'/, 'defensif bila admin-core belum termuat');
    assert.match(js, /validateUploadForm\(\)/, 'submitUpload harus menjalankan validasi form');
});
