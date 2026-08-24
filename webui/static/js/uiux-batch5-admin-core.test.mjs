/* Contract + behavior tests untuk Batch 5 (ronde 2 — bagian admin core).
 * Referensi temuan: review_uiux_webui.md bagian 5.5 RE-REVIEW RONDE 2
 * (ID: S31, S25, S27, S29, S30-admin, R18-login, R23-part, R19-part).
 *
 * Run with:  node --test static/js/uiux-batch5-admin-core.test.mjs   (from webui/)
 *
 * Latar belakang & dampak bisnis:
 *   - S31/S23/R23: tombol ikon & input pencarian tanpa nama aksibel membuat
 *     pengguna screen reader di dashboard/settings menebak-nebak fungsi kontrol
 *     (refresh, bersihkan pencarian, cari user/voucher).
 *   - S25: 12 modal tanpa role="dialog"/aria-modal/aria-labelledby — screen
 *     reader tidak mengumumkan konteks "dialog", guru awam tersesat di form.
 *   - S27: 5 aksi tulis tanpa proteksi double-submit → POST duplikat saat
 *     koneksi lambat (konfigurasi soal dobel, hapus/toggle massal berulang).
 *   - S29: navigator.clipboard = undefined di HTTP LAN (http://192.168.x.x)
 *     → klik "Salin Token" melempar TypeError dan gagal senyap. Sekolah adalah
 *     pengguna utama via LAN HTTP, jadi ini bug nyata, bukan teoretis.
 *   - S30: skip-link hanya ada di login; keyboard user harus Tab melewati
 *     seluruh topbar di 5 halaman admin terpadat.
 *   - R18/R19: live region error Turnstile login admin + hierarki heading
 *     dashboard yang melompat h1→h3.
 *
 * Pola sama dengan uiux-batch3-jscore.test.mjs: sumber JS ASLI yang dikirim ke
 * browser dieksekusi di Node vm dengan mock DOM. Untuk admin.js kita memuat
 * admin-core.js lebih dulu dalam sandbox yang sama (urutan sama seperti di
 * template), lalu menimpa apiFetch/showConfirm dengan spy sesuai kebutuhan.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const WEBUI_ROOT = path.join(__dirname, '..', '..');
const ADMIN_JS_SRC = fs.readFileSync(path.join(__dirname, 'admin.js'), 'utf8');
const ADMIN_CORE_SRC = fs.readFileSync(path.join(__dirname, 'admin-core.js'), 'utf8');
const read = (rel) => fs.readFileSync(path.join(WEBUI_ROOT, rel), 'utf8');

const DASHBOARD = read('templates/admin/dashboard.html');
const SETTINGS = read('templates/admin/settings.html');
const LOGIN = read('templates/admin/login.html');
const NAV = read('templates/admin/partials/nav.html');
const HEAD = read('templates/admin/partials/head.html');

// --- helper kontrak statik ---------------------------------------------------

// Ambil isi fungsi (dari kata kunci function sampai deklarasi function lain
// berikutnya di kolom 0) — cukup untuk assertion pola di dalam body.
function extractFunction(src, name) {
    const startRe = new RegExp(`(^|\\n)(async )?function ${name}\\(`);
    const m = startRe.exec(src);
    if (!m) return null;
    const rest = src.slice(m.index);
    const nextRe = /\n(async )?function [A-Za-z_$][\w$]*\(/g;
    nextRe.lastIndex = 10;
    const nm = nextRe.exec(rest);
    return nm ? rest.slice(0, nm.index) : rest;
}

// --- harness perilaku (admin-core.js + admin.js di satu sandbox) -------------

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
        files: null,
        offsetHeight: 0,
        offsetParent: null,
        dataset: {},
        _listeners: {}
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
    node.dispatchEvent = () => true;
    node.querySelector = () => null;
    node.querySelectorAll = () => [];
    node.focus = () => {};
    node.select = () => {};
    node.reset = () => {};
    node.reportValidity = () => true;
    return node;
}

// Muat admin-core.js lalu admin.js (urutan seperti <script> di template).
// Mengembalikan sandbox + doc mock yang bisa diprogram per-test.
function loadAdminScripts() {
    const allNodes = [];
    const createdTags = [];
    const register = (n) => { allNodes.push(n); return n; };
    const docListeners = {};
    const docMock = {
        readyState: 'complete',
        activeElement: null,
        documentElement: register(fakeNode('html')),
        body: register(fakeNode('body')),
        getElementById(id) {
            return allNodes.find((n) => n.attrs && n.attrs.id === id) || null;
        },
        createElement(tag) {
            createdTags.push(String(tag).toUpperCase());
            return register(fakeNode(tag));
        },
        querySelector() { return null; },
        querySelectorAll(sel) {
            // Hanya selector checkbox bulk yang dikenali (untuk bulk*Exams).
            if (sel === '.exam-checkbox:checked') return docMock._checkedBoxes;
            return [];
        },
        _checkedBoxes: [],
        addEventListener(type, fn) { (docListeners[type] = docListeners[type] || []).push(fn); },
        removeEventListener() {},
        dispatchEvent(ev) {
            (docListeners[ev.type] || []).slice().forEach((fn) => fn(ev));
            return true;
        },
        contains() { return true; }
    };

    const win = {
        fetch: async () => ({ ok: true, status: 200, text: () => Promise.resolve('{}'), json: () => Promise.resolve({ success: true }) }),
        location: { origin: 'http://192.168.1.10:8080', pathname: '/admin/dashboard', search: '', href: '' },
        addEventListener() {},
        removeEventListener() {},
        __adminRole: ''
    };

    const execCommandCalls = [];
    function MutationObserverMock() {}
    MutationObserverMock.prototype.observe = function () {};
    MutationObserverMock.prototype.disconnect = function () {};

    const sandbox = {
        window: win,
        document: docMock,
        CustomEvent: function (t) { this.type = t; },
        MouseEvent: function (type) { this.type = type; },
        MutationObserver: MutationObserverMock,
        getComputedStyle: () => ({ display: 'block' }),
        navigator: {},
        URLSearchParams,
        console,
        setTimeout(fn) { return 0; },
        clearTimeout() {},
        setInterval() { return 0; },
        clearInterval() {},
        location: win.location,
        history: { replaceState() {}, pushState() {} }
    };
    sandbox.globalThis = sandbox;
    vm.createContext(sandbox);

    vm.runInContext(ADMIN_CORE_SRC, sandbox, { filename: 'admin-core.js' });
    vm.runInContext(ADMIN_JS_SRC, sandbox, { filename: 'admin.js' });

    return {
        sandbox,
        doc: docMock,
        makeNode: (tag) => register(fakeNode(tag)),
        createdTags,
        createdNodes: allNodes,
        // Assign ke binding `let` top-level admin.js (mis. activeExamId):
        // properti sandbox TIDAK menimpa lexical binding script vm.
        runInContext: (code) => vm.runInContext(code, sandbox)
    };
}

// ---------------------------------------------------------------------------
// S31 — icon-button refresh & clear-search tanpa nama aksibel
// ---------------------------------------------------------------------------

test('S31: tombol refresh dashboard (ikon saja) punya aria-label "Muat ulang daftar"', () => {
    const btn = /<button class="toolbar-btn toolbar-btn-refresh"[^>]*>/.exec(DASHBOARD);
    assert.ok(btn, 'tombol refresh toolbar harus ada');
    assert.match(btn[0], /aria-label="Muat ulang daftar"/,
        'tombol yang hanya berisi SVG wajib aria-label (bandingkan pengawas.html:62)');
});

test('S31: #searchClearBtn (ikon X) punya aria-label "Bersihkan pencarian"', () => {
    const btn = /<button class="toolbar-search-clear" id="searchClearBtn"[^>]*>/.exec(DASHBOARD);
    assert.ok(btn, '#searchClearBtn harus ada');
    assert.match(btn[0], /aria-label="Bersihkan pencarian"/,
        'ikon X pencarian wajib punya nama aksibel');
});

// ---------------------------------------------------------------------------
// S25 — semantik dialog pada semua modal admin
// ---------------------------------------------------------------------------

const MODALS_DASHBOARD = [
    'changePasswordModal', 'editExamModal', 'editTokenModal',
    'delegateExamModal', 'questionsModal'
];
const MODALS_SETTINGS = [
    'confirmRedeemModal', 'singleModal', 'batchModal',
    // Batch 12 (T22): confirmActionModal DIHAPUS — konfirmasi voucher kini
    // memakai showConfirm core (satu sistem, focus-trap G5).
    'changePasswordModal'
];

function modalTagById(html, id) {
    const re = new RegExp(`<div[^>]*id="${id}"[^>]*>`);
    const m = re.exec(html);
    return m ? m[0] : null;
}

for (const id of MODALS_DASHBOARD) {
    test(`S25: #${id} (dashboard.html) punya role="dialog" + aria-modal + aria-labelledby valid`, () => {
        const tag = modalTagById(DASHBOARD, id);
        assert.ok(tag, `elemen modal #${id} harus ada`);
        assert.match(tag, /role="dialog"/);
        assert.match(tag, /aria-modal="true"/);
        const lm = /aria-labelledby="([^"]+)"/.exec(tag);
        assert.ok(lm, 'wajib aria-labelledby');
        assert.match(DASHBOARD, new RegExp(`id="${lm[1]}"`),
            `heading target "${lm[1]}" harus benar-benar eksis di dashboard.html`);
    });
}

for (const id of MODALS_SETTINGS) {
    test(`S25: #${id} (settings.html) punya role="dialog" + aria-modal + aria-labelledby valid`, () => {
        const tag = modalTagById(SETTINGS, id);
        assert.ok(tag, `elemen modal #${id} harus ada`);
        assert.match(tag, /role="dialog"/);
        assert.match(tag, /aria-modal="true"/);
        const lm = /aria-labelledby="([^"]+)"/.exec(tag);
        assert.ok(lm, 'wajib aria-labelledby');
        assert.match(SETTINGS, new RegExp(`id="${lm[1]}"`),
            `heading target "${lm[1]}" harus benar-benar eksis di settings.html`);
    });
}

test('S25: SEMUA .modal-overlay/modal utama di dashboard+settings ber-role dialog (guard regresi)', () => {
    for (const [name, html] of [['dashboard.html', DASHBOARD], ['settings.html', SETTINGS]]) {
        const overlays = html.match(/<div[^>]*class="(?:modal-overlay|modal-backdrop)[^"]*"[^>]*>/g) || [];
        assert.ok(overlays.length >= 6, `${name}: minimal 6 modal ditemukan`);
        for (const tag of overlays) {
            assert.match(tag, /role="dialog"/, `${name}: overlay tanpa role="dialog" → ${tag}`);
            assert.match(tag, /aria-modal="true"/, `${name}: overlay tanpa aria-modal → ${tag}`);
            assert.match(tag, /aria-labelledby=/, `${name}: overlay tanpa aria-labelledby → ${tag}`);
        }
    }
});

test('S25: SEMUA tombol .modal-close punya aria-label="Tutup" (settings)', () => {
    for (const [name, html] of [['dashboard.html', DASHBOARD], ['settings.html', SETTINGS]]) {
        const closes = html.match(/<button[^>]*class="[^"]*modal-close[^"]*"[^>]*>/g) || [];
        // Batch 12 (T22): confirmActionModal dihapus — minimal settings 6 → 5.
        const min = name === 'dashboard.html' ? 5 : 5;
        // Catatan: close button uploadModal (settings.html:1992) tidak ber-class
        // modal-close (inline styled) tapi sudah punya aria-label="Tutup".
        assert.ok(closes.length >= min, `${name}: minimal ${min} modal-close ditemukan, dapat ${closes.length}`);
        for (const tag of closes) {
            assert.match(tag, /aria-label="Tutup"/, `${name}: close button tanpa aria-label → ${tag}`);
        }
    }
});

// ---------------------------------------------------------------------------
// S27 — proteksi double-submit pada 5 fungsi admin.js
// ---------------------------------------------------------------------------

const GUARD_FNS = [
    'submitChangePassword',
    'submitEditToken',
    'saveQuestionsConfig',
    'bulkDeleteExams',
    'bulkToggleExams'
];

test('S27 (statik): kelima fungsi memuat guard disable + restore di finally', () => {
    for (const name of GUARD_FNS) {
        const body = extractFunction(ADMIN_JS_SRC, name);
        assert.ok(body, `fungsi ${name} harus ada di admin.js`);
        assert.match(body, /\.disabled = true/, `${name}: wajib disable tombol sebelum request`);
        assert.match(body, /finally/, `${name}: restore wajib di blok finally agar error pun pulih`);
        assert.match(body, /\.disabled = false/, `${name}: wajib re-enable tombol setelah selesai`);
    }
});

test('S27 (perilaku): submitChangePassword mendisable tombol saat request & restore setelah selesai', async () => {
    const env = loadAdminScripts();
    const btn = env.makeNode('button');
    btn.innerHTML = '<svg></svg> Simpan Password Baru';

    const form = env.makeNode('form');
    form.querySelector = () => btn;

    const mk = (id, val) => {
        const el = env.makeNode('input');
        el.id = id;
        el.value = val;
        env.doc.body.appendChild(el);
        return el;
    };
    mk('currentPassword', 'lama12345');
    mk('newPassword', 'baru12345');
    mk('confirmNewPassword', 'baru12345');

    let resolveApi;
    let inFlightDisabled = null;
    env.sandbox.apiFetch = () => new Promise((res) => {
        resolveApi = res;
        inFlightDisabled = btn.disabled;
    });
    env.sandbox.showToast = () => {};

    await env.sandbox.submitChangePassword({ target: form, preventDefault() {} });
    assert.equal(inFlightDisabled, true, 'selama request berjalan tombol harus disabled');
    assert.equal(btn.disabled, true);

    resolveApi({ json: () => Promise.resolve({ success: true, message: 'ok' }) });
    await new Promise((r) => setImmediate(r));
    await new Promise((r) => setImmediate(r));

    assert.equal(btn.disabled, false, 'setelah request selesai tombol harus aktif lagi');
    assert.equal(btn.innerHTML, '<svg></svg> Simpan Password Baru', 'label tombol direstore');
});

test('S27 (perilaku): klik ganda saveQuestionsConfig saat in-flight diabaikan (tidak POST dua kali)', async () => {
    const env = loadAdminScripts();
    env.runInContext('activeExamId = 7'); // let-binding admin.js, bukan properti sandbox

    const btnSave = env.makeNode('button');
    btnSave.setAttribute('id', 'btnSaveQuestionsConfig');
    btnSave.innerHTML = 'Simpan Konfigurasi';
    env.doc.body.appendChild(btnSave);

    const secLevel = env.makeNode('select');
    secLevel.id = 'examSecurityLevel';
    secLevel.value = 'medium';
    env.doc.body.appendChild(secLevel);

    let calls = 0;
    let resolveApi;
    env.sandbox.apiFetch = () => {
        calls += 1;
        return new Promise((res) => { resolveApi = res; });
    };
    env.sandbox.showToast = () => {};

    await env.sandbox.saveQuestionsConfig();
    await env.sandbox.saveQuestionsConfig(); // klik ganda saat masih in-flight

    assert.equal(calls, 1, 'panggilan kedua saat tombol disabled tidak boleh memicu POST lagi');

    resolveApi({ json: () => Promise.resolve({ success: true, message: 'ok' }) });
    await new Promise((r) => setImmediate(r));
    await new Promise((r) => setImmediate(r));

    assert.equal(btnSave.disabled, false, 'tombol dipulihkan setelah selesai');
});

test('S27 (perilaku): bulkDeleteExams guard tombol #bulkDeleteBtn + restore di finally (juga saat error)', async () => {
    const env = loadAdminScripts();
    const cb = env.makeNode('input');
    cb.className = 'exam-checkbox';
    cb.setAttribute('checked', 'checked');
    cb.value = '11';
    env.doc._checkedBoxes = [cb];

    const btn = env.makeNode('button');
    btn.setAttribute('id', 'bulkDeleteBtn');
    btn.innerHTML = 'Hapus Terpilih (1)';
    env.doc.body.appendChild(btn);

    let rejectApi;
    env.sandbox.apiFetch = () => new Promise((_res, rej) => { rejectApi = rej; });
    env.sandbox.showToast = () => {};

    const p = env.sandbox.bulkDeleteExams();
    assert.equal(btn.disabled, true, 'tombol bulk delete harus disabled saat request');

    rejectApi(new Error('network'));
    await p.catch(() => {});
    await new Promise((r) => setImmediate(r));

    assert.equal(btn.disabled, false, 'meski request gagal, tombol harus direstore (finally)');
    assert.equal(btn.innerHTML, 'Hapus Terpilih (1)');
});

test('S27 (perilaku): bulkToggleExams restore tombol setelah showConfirm ditolak maupun sukses', async () => {
    const env = loadAdminScripts();
    const mkCb = (status) => {
        const cb = env.makeNode('input');
        cb.className = 'exam-checkbox';
        cb.setAttribute('data-status', status);
        cb.value = String(Math.floor(Math.random() * 100) + 1);
        return cb;
    };
    const btn = env.makeNode('button');
    btn.setAttribute('id', 'bulkToggleBtn');
    btn.innerHTML = 'Nonaktifkan Terpilih (2)';
    env.doc.body.appendChild(btn);

    env.sandbox.showToast = () => {};
    env.sandbox.showConfirm = async () => false;
    let apiCalls = 0;
    env.sandbox.apiFetch = () => { apiCalls += 1; return Promise.resolve({ json: () => Promise.resolve({ success: true }) }); };

    env.doc._checkedBoxes = [mkCb('active'), mkCb('inactive')];
    await env.sandbox.bulkToggleExams();
    assert.equal(apiCalls, 0, 'konfirmasi ditolak → tidak ada request');
    assert.equal(btn.disabled, false, 'tombol tetap bisa dipakai lagi setelah batal');

    env.sandbox.showConfirm = async () => true;
    let resolveApi;
    let duringFlight = null;
    env.sandbox.apiFetch = () => new Promise((res) => {
        resolveApi = res;
        duringFlight = btn.disabled;
        apiCalls += 1;
    });
    const p = env.sandbox.bulkToggleExams();
    await new Promise((r) => setImmediate(r)); // biarkan async fn melewati showConfirm
    assert.equal(duringFlight, true, 'saat request berjalan tombol disabled');
    resolveApi({ json: () => Promise.resolve({ success: true }) });
    await p;
    await new Promise((r) => setImmediate(r));
    assert.equal(btn.disabled, false, 'tombol direstore setelah sukses');
});

// ---------------------------------------------------------------------------
// S29 — guard clipboard: wrapper tipis di atas copyCode (admin-core.js)
// ---------------------------------------------------------------------------

test('S29 (statik): admin.js tidak lagi memanggil navigator.clipboard langsung', () => {
    assert.equal(ADMIN_JS_SRC.includes('navigator.clipboard'), false,
        'semua akses clipboard harus lewat copyCode yang berguard+fallback');
});

// Revisi Batch 6: copyAllTokens & copyResultsLink dihapus dari daftar ini —
// keduanya terverifikasi nol-pemanggil dan dibersihkan oleh S28 (Batch 6).
test('S29 (statik): copyToken/copyAIPrompt menjadi wrapper copyCode', () => {
    for (const name of ['copyToken', 'copyAIPrompt']) {
        const body = extractFunction(ADMIN_JS_SRC, name);
        assert.ok(body, `${name} harus ada`);
        assert.match(body, /copyCode\(/, `${name} wajib mendelegasikan ke copyCode`);
    }
});

test('S29 (perilaku): copyToken TANPA navigator.clipboard (HTTP LAN) tidak throw & pakai fallback execCommand', () => {
    const env = loadAdminScripts();
    env.sandbox.navigator.clipboard = undefined;
    env.sandbox.showToast = () => {};

    const execCalls = [];
    env.sandbox.document.execCommand = (cmd) => { execCalls.push(cmd); return true; };

    assert.doesNotThrow(() => env.sandbox.copyToken('ABC12DEF'),
        'clipboard undefined tidak boleh melempar TypeError');

    // copyCode membuat textarea fallback berisi token lalu menyalin via
    // execCommand("copy") — textarea dibersihkan setelah pakai.
    assert.ok(env.createdTags.includes('TEXTAREA'),
        'fallback harus membuat textarea sementara');
    const ta = env.createdNodes.find((n) => n.tagName === 'TEXTAREA');
    assert.equal(ta && ta.value, 'ABC12DEF', 'textarea fallback berisi token');
    assert.deepEqual(execCalls, ['copy'], 'execCommand("copy") dipanggil pada jalur fallback');
});

test('S29 (perilaku): copyToken DENGAN clipboard API → writeText terpanggil dengan token', async () => {
    const env = loadAdminScripts();
    let written = null;
    env.sandbox.navigator.clipboard = {
        writeText(text) { written = text; return Promise.resolve(); }
    };
    env.sandbox.showToast = () => {};
    env.sandbox.copyToken('XYZ99AAA');
    await new Promise((r) => setImmediate(r));
    assert.equal(written, 'XYZ99AAA', 'writeText dipanggil dengan token');
});

test('S29 (perilaku): copyToken kosong/em-dash tetap aman tanpa menyentuh clipboard', () => {
    const env = loadAdminScripts();
    let touched = false;
    env.sandbox.navigator.clipboard = { writeText() { touched = true; return Promise.resolve(); } };
    env.sandbox.document.execCommand = () => { touched = true; return true; };
    const toasts = [];
    env.sandbox.showToast = (msg, type) => toasts.push(type);

    env.sandbox.copyToken('');
    env.sandbox.copyToken('—');
    assert.equal(touched, false, 'token kosong tidak boleh menyentuh clipboard');
    assert.deepEqual(toasts, ['error', 'error'], 'feedback error tetap diberikan');
});

// ---------------------------------------------------------------------------
// S30-admin — skip-link via partials/nav.html
// ---------------------------------------------------------------------------

test('S30: nav.html memuat skip-link sebagai elemen pertama menuju #mainContent', () => {
    const idx = NAV.indexOf('skip-link');
    assert.ok(idx > -1, 'nav.html wajib memuat a.skip-link');
    const anchor = /<a href="#mainContent" class="skip-link"[^>]*>[^<]*<\/a>/.exec(NAV);
    assert.ok(anchor, 'skip-link harus <a href="#mainContent" class="skip-link">…</a>');
    assert.match(anchor[0], /Langsung ke konten utama/);
    assert.ok(idx < NAV.indexOf('<nav'), 'skip-link harus sebelum <nav> agar jadi tab-stop pertama');
});

test('S30: halaman milik batch ini punya target anchor id="mainContent" pada <main>', () => {
    assert.match(DASHBOARD, /<main[^>]*id="mainContent"/, 'dashboard.html <main> wajib id="mainContent"');
    assert.match(SETTINGS, /<main[^>]*id="mainContent"/, 'settings.html <main> wajib id="mainContent"');
});

test('S30: head.html tidak mendefinisikan CSS skip-link sendiri (CSS tunggal di theme.css)', () => {
    assert.equal(HEAD.includes('.skip-link'), false,
        'head.html hanya memuat stylesheet; rule .skip-link cukup dari theme.css:77-84');
});

test('S30: skip-link login.html tetap ada (tidak teregresi)', () => {
    assert.match(LOGIN, /class="skip-link"/);
});

// ---------------------------------------------------------------------------
// R18-login — live region error Turnstile
// ---------------------------------------------------------------------------

test('R18: #turnstileError di login.html punya role="alert"', () => {
    const div = /<div id="turnstileError"[^>]*>/.exec(LOGIN);
    assert.ok(div, '#turnstileError harus ada');
    assert.match(div[0], /role="alert"/,
        'error captcha dimunculkan dinamis — screen reader wajib diumumkan');
});

// ---------------------------------------------------------------------------
// R23-part — aria-label input pencarian settings
// ---------------------------------------------------------------------------

test('R23: #auditSearchInput aria-label "Cari riwayat voucher"', () => {
    const input = /<input type="text" id="auditSearchInput"[^>]*>/.exec(SETTINGS);
    assert.ok(input, '#auditSearchInput harus ada');
    assert.match(input[0], /aria-label="Cari riwayat voucher"/);
});

test('R23: #userSearchInput aria-label "Cari pengguna"', () => {
    const input = /<input type="text" id="userSearchInput"[^>]*>/.exec(SETTINGS);
    assert.ok(input, '#userSearchInput harus ada');
    assert.match(input[0], /aria-label="Cari pengguna"/);
});

// ---------------------------------------------------------------------------
// R19-part — hierarki heading dashboard
// ---------------------------------------------------------------------------

test('R19: dashboard.html tidak boleh ada h3 sebelum h2 pertama', () => {
    const firstH3 = DASHBOARD.indexOf('<h3');
    const firstH2 = DASHBOARD.indexOf('<h2');
    assert.ok(firstH2 > -1, 'harus ada h2');
    if (firstH3 > -1) {
        assert.ok(firstH3 > firstH2,
            'h1 sr-only → h2 seksi → h3 sub; h3 tidak boleh mendahului h2 pertama');
    }
});

test('R19: seksi-seksi utama dashboard naik ke h2 (Info Server, Tambah Ujian, Daftar Ujian)', () => {
    const headings = [...DASHBOARD.matchAll(/<h([23])[^>]*>([^<]*)/g)]
        .map((m) => ({ level: Number(m[1]), after: m[2] }));
    const h2Texts = DASHBOARD.match(/<h2[^>]*>[\s\S]{0,200}?<\/h2>/g) || [];
    const joined = h2Texts.join('\n');
    for (const expected of ['Info Server', 'Tambah Ujian Baru', 'Daftar Ujian']) {
        assert.match(joined, new RegExp(expected),
            `"${expected}" harus menjadi h2 (sebelumnya h3 tanpa h2 induk)`);
    }
    const h2Count = (DASHBOARD.match(/<h2/g) || []).length;
    assert.ok(h2Count >= 3, `minimal 3 h2 hasil konversi, dapat ${h2Count}`);
});

test('R19: visual heading dipertahankan — mobile rule .upload-section juga mencakup h2', () => {
    // Keputusan teknis: h3 seksi → h2 dengan inline font-size 1.15rem (nilai
    // lama rule global h3 di admin-base.css:66) sehingga visual desktop tak
    // berubah meski tag naik level; rule mobile .upload-section h3 di inline
    // <style> dashboard diperluas mencakup h2.
    const sectionH2 = /<h2[^>]*style="[^"]*font-size:\s*1\.15rem/.test(DASHBOARD);
    assert.ok(sectionH2, 'h2 seksi pakai inline font-size:1.15rem agar visual setara h3 lama');
    assert.match(DASHBOARD, /\.upload-section h[23],\s*\.upload-section h[23]\s*\{/,
        'rule mobile upload-section harus mencakup h2 DAN h3 (kompatibilitas)');
});
