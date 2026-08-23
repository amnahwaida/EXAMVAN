/* Contract + behavior tests untuk Batch 7 — halaman Dashboard (dashboard.html)
 * dan modul miliknya (admin.js). Referensi: review_uiux_webui.md fase 2 design
 * token (lanjutan S15) + migrasi event-delegation `data-action` (lanjutan R28).
 *
 * Run with:  node --test static/js/uiux-batch7-dashboard.test.mjs   (from webui/)
 *
 * Latar belakang & dampak bisnis:
 *   - R28-lanjutan: dashboard.html masih memakai ±48 handler onclick inline
 *     (termasuk argumen Go-template {{ $exam.ID }} di dalam atribut JS).
 *     Semua aksi bermigrasi ke delegasi `data-action` + registry Actions
 *     (admin-core.js, kontrak antar agen) dengan argumen dibawa lewat data-*;
 *     elemen non-button bekas onclick tetap keyboard-operable.
 *   - S15-lanjutan: ±49 hex & ±54 rgba literal di dashboard.html + 23 hex di
 *     admin.js membuat perubahan tema harus menyentuh puluhan titik. Warna
 *     ber-padanan token dipindah ke var(--rgb-*), var(--color-*),
 *     var(--glass-bg-strong); baseline angka dikunci sebagai guard regresi.
 *
 * Kontrak antar-agen yang DIKONSUMSI (sudah terpasang di admin-core.js):
 *   - global `Actions = { register(name, fn), has(name) }` + satu listener
 *     klik dokumen: closest('[data-action]') → fn(el, event).
 *   - theme.css: triplet --rgb-success/warning/danger/info/accent,
 *     --glass-bg-strong, --color-success-light/warning-light/danger-light/
 *     primary-light/accent-light/text-placeholder/text-on-primary/on-accent.
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

const DASHBOARD = read('templates/admin/dashboard.html');
const ADMIN_JS_SRC = read('static/js/admin.js');
const ADMIN_CORE_SRC = read('static/js/admin-core.js');

// --- baseline guard (S15-lanjutan) -------------------------------------------

const HEX_RE = /#[0-9a-fA-F]{6}\b|#[0-9a-fA-F]{3}\b/g;
const countMatches = (src, re) => (src.match(re) || []).length;

test('GUARD token: dashboard.html hex ≤ 29 & rgba literal ≤ 32 (reduksi ≥40%/≥30% dari 49/54)', () => {
    const hex = countMatches(DASHBOARD, HEX_RE);
    // Yang dihitung hanya rgba() LITERAL ber-komponen numerik; bentuk
    // rgba(var(--rgb-*), α) justru hasil migrasi yang diinginkan.
    const rgba = countMatches(DASHBOARD, /rgba\(\s*\d/gi);
    assert.ok(hex <= 29, `hex dashboard.html = ${hex}, harus ≤ 29 (baseline awal 49, reduksi ≥40%)`);
    assert.ok(rgba <= 32, `rgba literal dashboard.html = ${rgba}, harus ≤ 32 (baseline awal 54, reduksi ≥30%)`);
});

test('GUARD token: admin.js sisa hex literal ≤ 8 (baseline awal 23)', () => {
    const hex = countMatches(ADMIN_JS_SRC, HEX_RE);
    assert.ok(hex <= 8, `hex admin.js = ${hex}, harus ≤ 8 (baseline awal 23)`);
});

// --- R28-lanjutan: tidak ada inline onclick tersisa ---------------------------

test('R28: dashboard.html bebas onclick inline (=== 0)', () => {
    const n = countMatches(DASHBOARD, /onclick=/g);
    assert.equal(n, 0, `masih ada ${n} atribut onclick di dashboard.html`);
});

/** Kumpulkan isi <script> TANPA src (inline) dari sebuah dokumen HTML. */
function inlineScripts(html) {
    const out = [];
    const re = /<script\b([^>]*)>([\s\S]*?)<\/script>/g;
    let m;
    while ((m = re.exec(html))) {
        if (!/\bsrc=/.test(m[1])) out.push(m[2]);
    }
    return out;
}

const INLINE_JS = inlineScripts(DASHBOARD).join('\n');

test('R28: SEMUA data-action di dashboard.html terdaftar (admin-core ∪ admin.js ∪ inline script halaman)', () => {
    const used = new Set(
        [...DASHBOARD.matchAll(/data-action="([a-z0-9-]+)"/g)].map((m) => m[1])
    );
    assert.ok(used.size >= 15, `minimal 15 nama aksi unik dipakai HTML, dapat ${used.size}`);

    // Batch 8: registrasi kanonik modal-dismiss kini hidup di admin-core.js
    // (satu sumber untuk semua halaman), jadi core ikut masuk union sumber.
    const registered = new Set(
        [...(ADMIN_CORE_SRC + '\n' + ADMIN_JS_SRC + '\n' + INLINE_JS).matchAll(/Actions\.register\(\s*['"]([a-z0-9-]+)['"]/g)].map((m) => m[1])
    );
    const missing = [...used].filter((name) => !registered.has(name));
    assert.deepEqual(missing, [],
        `aksi tanpa register di sumber manapun: ${missing.join(', ')}`);
});

test('R28: elemen non-button bekas onclick tetap keyboard-operable', () => {
    // Kartu instansi, kode instansi, badge status, badge pengawas, dan kode
    // token semuanya non-button → wajib role="button" + tabindex="0".
    const cases = [
        [/data-action="instansi-open"/, 'kartu ubah instansi'],
        [/data-action="instansi-code-copy"/, 'kode instansi'],
        [/data-action="pengawas-popup-toggle"/, 'badge pengawas']
    ];
    for (const [re, label] of cases) {
        const tag = DASHBOARD.match(new RegExp(`<[^>]*${re.source}[^>]*>`));
        assert.ok(tag, `${label} bermigrasi ke data-action`);
        assert.match(tag[0], /role="button"/, `${label} wajib role="button"`);
        assert.match(tag[0], /tabindex="0"/, `${label} wajib tabindex="0"`);
    }
    // Badge status: klik & keyboard (Enter/Space) kini lewat satu pintu
    // delegasi — atribut onclick maupun onkeydown inline hilang, tapi
    // role/tabindex/aria-pressed dari S1 tetap utuh.
    for (const cls of ['status-active', 'status-tombstoned', 'status-inactive']) {
        const tag = DASHBOARD.match(new RegExp(`<span class="status-badge ${cls}" id="status-[^>]*>`));
        assert.ok(tag, `badge ${cls} ada`);
        assert.match(tag[0], /data-action="exam-toggle-status"/, `badge ${cls} pakai delegasi`);
        assert.match(tag[0], /data-exam-id/, `badge ${cls} membawa data-exam-id`);
        assert.match(tag[0], /tabindex="0"/, `badge ${cls} tetap fokusable`);
        assert.ok(!/onkeydown=/.test(tag[0]), `badge ${cls}: keyboard parity via delegasi, bukan onkeydown inline`);
    }
});

test('R28: backdrop modal instansi pakai pola modal-dismiss (klik kartu tidak menutup)', () => {
    const tag = DASHBOARD.match(/<div[^>]*id="editInstansiModal"[^>]*>/);
    assert.ok(tag, 'overlay #editInstansiModal ada');
    assert.match(tag[0], /data-action="modal-dismiss"/);
    assert.match(tag[0], /data-modal-close="closeEditInstansiModal"/);
    // Kartu modal tidak lagi memasang stopPropagation inline (onclick dihapus);
    // guard target===el pada handler modal-dismiss yang mencegah tutup prematur.
    const card = DASHBOARD.match(/<div class="modal-card glass-card" style="max-width: 440px;"[^>]*>/);
    assert.ok(card, 'kartu modal instansi ada');
    assert.ok(!/onclick=/.test(card[0]), 'kartu modal bebas onclick inline');
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
        offsetHeight: 0,
        _spies: { removed: 0 },
        title: ''
    };
    node.classList = {
        _set: new Set(),
        add(...c) { c.forEach((x) => node.classList._set.add(x)); },
        remove(...c) { c.forEach((x) => node.classList._set.delete(x)); },
        contains(c) { return node.classList._set.has(c); },
        toggle(c, force) {
            const want = force === undefined ? !node.classList._set.has(c) : !!force;
            want ? node.classList._set.add(c) : node.classList._set.delete(c);
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
    // closest minimal: cukup untuk selector '[data-action]', '[data-action][role="button"]' & '#id'.
    node.closest = (sel) => {
        let cur = node;
        while (cur) {
            if (/^\[data-action\](\[role="button"\])?$/.test(sel) && cur.attrs['data-action'] !== undefined) {
                if (sel.includes('role="button"') && cur.attrs.role !== 'button') { cur = cur.parentNode; continue; }
                return cur;
            }
            const idSel = /^#([\w-]+)$/.exec(sel);
            if (idSel && cur.attrs.id === idSel[1]) return cur;
            cur = cur.parentNode;
        }
        return null;
    };
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
    node.remove = () => { node._spies.removed++; if (node.parentNode) node.parentNode.removeChild(node); };
    node.click = () => {};
    return node;
}

/**
 * Muat admin-core.js + admin.js (urutan <script> halaman) ke satu sandbox vm,
 * lalu sediakan stub apiFetch/showConfirm/showToast level konteks.
 */
function loadPage() {
    const byId = {};
    const docListeners = {};
    const reg = (n) => n;

    const docMock = {
        readyState: 'complete',
        body: reg(fakeNode('body')),
        getElementById(id) { return byId[id] || null; },
        createElement(tag) { return reg(fakeNode(tag)); },
        querySelector() { return null; },
        querySelectorAll() { return []; },
        addEventListener(type, fn) { (docListeners[type] = docListeners[type] || []).push(fn); },
        removeEventListener() {}
    };

    const win = {
        location: { origin: 'http://192.168.1.10:8080', pathname: '/admin/dashboard', search: '', href: '' },
        addEventListener() {},
        removeEventListener() {},
        matchMedia: () => ({ matches: false })
    };
    win.window = win;

    function MutationObserverMock() {}
    MutationObserverMock.prototype.observe = function () {};
    MutationObserverMock.prototype.disconnect = function () {};

    const sandbox = {
        window: win,
        document: docMock,
        CustomEvent: function (t, opts) { this.type = t; this.detail = (opts && opts.detail) || null; },
        MouseEvent: function (type) { this.type = type; },
        MutationObserver: MutationObserverMock,
        navigator: {},
        URLSearchParams,
        console,
        setTimeout(fn) { fn(); return 0; },
        clearTimeout() {},
        setInterval() { return 0; },
        clearInterval() {},
        location: win.location,
        // Global yang di halaman didefinisikan inline script dashboard.
        ADMIN_ID: 1,
        IS_PRIVILEGED: true
    };
    sandbox.globalThis = sandbox;
    vm.createContext(sandbox);

    vm.runInContext(ADMIN_CORE_SRC, sandbox, { filename: 'admin-core.js' });
    vm.runInContext(ADMIN_JS_SRC, sandbox, { filename: 'admin.js' });

    return {
        sandbox,
        docListeners,
        byId,
        makeNode(tag, attrs) {
            const n = reg(fakeNode(tag));
            Object.entries(attrs || {}).forEach(([k, v]) => n.setAttribute(k, v));
            return n;
        },
        /** Simulasikan klik nyata: jalankan semua listener klik dokumen. */
        click(el) {
            const ev = {
                target: el,
                preventDefault() {},
                stopPropagation() {},
                stopImmediatePropagation() {}
            };
            (docListeners.click || []).forEach((fn) => fn(ev));
            return ev;
        }
    };
}

const flush = () => new Promise((r) => setTimeout(r, 20));

// --- skenario perilaku 1: hapus ujian via delegasi → showConfirm dulu ---------

test('PERILAKU: exam-delete via delegasi konfirmasi dulu; setuju → POST tanpa reload; batal → tanpa request', async () => {
    const env = loadPage();
    const confirmCalls = [];
    let confirmResult = true;
    env.sandbox.showConfirm = (...a) => { confirmCalls.push(a); return Promise.resolve(confirmResult); };
    const apiCalls = [];
    env.sandbox.apiFetch = (url, opts) => { apiCalls.push({ url, opts }); return Promise.resolve({ json: () => Promise.resolve({ success: true, message: 'terhapus' }) }); };
    env.sandbox.showToast = () => {};

    const row = env.makeNode('tr', { id: 'exam-row-42' });
    env.byId['exam-row-42'] = row;
    let rowRemoved = 0;
    row.remove = () => { rowRemoved++; };

    const btn = env.makeNode('button', {
        'data-action': 'exam-delete',
        'data-exam-id': '42',
        'data-exam-name': 'UTS "Matematika"'
    });
    const child = env.makeNode('span');
    child.parentNode = btn;

    // Klik pada anak tombol → closest resolve ke tombol.
    env.click(child);
    await flush(); // selesaikan dulu alur setuju dari klik pertama

    assert.equal(confirmCalls.length, 1, 'showConfirm muncul SEBELUM request');
    assert.match(String(confirmCalls[0][0]), /Hapus ujian "UTS "Matematika""\?/,
        'nama ujian dari data-exam-name sampai ke dialog');
    assert.equal(apiCalls.length, 1, 'setuju pada klik pertama → POST terkirim');
    apiCalls.length = 0;
    rowRemoved = 0;

    // Batal → tidak ada POST.
    confirmResult = false;
    confirmCalls.length = 0;
    env.click(btn);
    await flush();
    assert.equal(apiCalls.length, 0, 'showConfirm ditolak → tidak boleh DELETE');

    // Setuju → POST ke endpoint hapus, row dibuang dari DOM tanpa reload.
    confirmResult = true;
    env.click(btn);
    await flush();
    await flush();
    assert.equal(apiCalls.length, 1);
    assert.equal(apiCalls[0].url, '/admin/api/exams/42/delete');
    assert.equal(apiCalls[0].opts.method, 'POST');
    assert.equal(rowRemoved, 1, 'R6: row dihapus in-place, tanpa reload halaman');
});

// --- skenario perilaku 2: toggle status via data-exam-id ----------------------

test('PERILAKU: exam-toggle-status via delegasi membaca data-exam-id & menjalankan alur S1', async () => {
    const env = loadPage();
    const classSet = new Set(['status-badge', 'status-active']);
    const badge = {
        classList: {
            contains: (c) => classSet.has(c),
            remove: (c) => classSet.delete(c),
            add: (c) => classSet.add(c),
            toggle: (c, f) => {
                const want = f === undefined ? !classSet.has(c) : !!f;
                want ? classSet.add(c) : classSet.delete(c);
                return want;
            }
        },
        style: {}, dataset: {}, attrs: { 'aria-pressed': 'true' }, textContent: 'Aktif', title: '',
        setAttribute(k, v) { this.attrs[k] = String(v); },
        getAttribute(k) { return Object.prototype.hasOwnProperty.call(this.attrs, k) ? this.attrs[k] : null; }
    };
    env.byId['status-7'] = badge;
    env.byId['exam-row-7'] = {
        querySelector: () => ({ getAttribute: (a) => (a === 'data-name' ? 'Fisika Dasar' : null), textContent: 'Fisika Dasar' })
    };

    const confirmCalls = [];
    env.sandbox.showConfirm = (...a) => { confirmCalls.push(a); return Promise.resolve(true); };
    const apiCalls = [];
    env.sandbox.apiFetch = (url, opts) => {
        apiCalls.push({ url, opts });
        return Promise.resolve({ json: () => Promise.resolve({ success: true, new_status: 'inactive', message: 'ok' }) });
    };
    env.sandbox.showToast = () => {};

    const span = env.makeNode('span', {
        'data-action': 'exam-toggle-status',
        'data-exam-id': '7',
        role: 'button'
    });
    env.click(span);
    await flush();

    assert.equal(confirmCalls.length, 1, 'S1: toggle lewat dialog konfirmasi');
    assert.match(String(confirmCalls[0][0]), /Nonaktifkan ujian "Fisika Dasar"\?/);
    assert.equal(apiCalls.length, 1, 'setuju → POST terkirim');
    assert.equal(apiCalls[0].url, '/admin/api/exams/7/toggle', 'data-exam-id string dipakai sebagai id numerik');
    assert.equal(badge.textContent, 'Nonaktif', 'badge diperbarui in-place');
});

// --- skenario perilaku 3: modal soal buka/tutup lewat delegasi + Modal.* ------

test('PERILAKU: questions-open & modal-close via delegasi tetap bekerja lewat API Modal', async () => {
    const env = loadPage();

    const modal = env.makeNode('div', { id: 'questionsModal' });
    modal.style.display = 'none';
    env.byId.questionsModal = modal;
    env.byId.modalTitle = env.makeNode('h3', { id: 'modalTitle' });
    env.byId.questionsList = env.makeNode('div', { id: 'questionsList' });
    env.byId.identityFieldsList = env.makeNode('div', { id: 'identityFieldsList' });
    env.byId.studentAccessControls = env.makeNode('div', { id: 'studentAccessControls' });

    env.sandbox.apiFetch = () => Promise.resolve({
        json: () => Promise.resolve({
            success: true,
            security_level: 'medium',
            questions: [],
            identity_fields: []
        })
    });
    env.sandbox.showToast = () => {};

    const openBtn = env.makeNode('button', {
        'data-action': 'questions-open',
        'data-exam-id': '42',
        'data-exam-name': 'Sejarah'
    });
    env.click(openBtn);
    await flush();

    assert.equal(modal.style.display, 'flex', 'aksi delegasi membuka #questionsModal via Modal.open');
    assert.match(env.byId.modalTitle.textContent, /Atur Soal Ujian: Sejarah/);

    // Tutup lewat tombol Batal generik: modal-close + data-modal-close.
    const closeBtn = env.makeNode('button', {
        'data-action': 'modal-close',
        'data-modal-close': 'closeQuestionsModal'
    });
    env.click(closeBtn);
    assert.equal(modal.style.display, 'none', 'modal-close menutup via closeQuestionsModal → Modal.close');
});
