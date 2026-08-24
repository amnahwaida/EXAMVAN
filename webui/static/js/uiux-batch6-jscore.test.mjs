/* Contract + behavior tests untuk Batch 6 (ronde 2 — bagian JS core milik
 * agen batch-6-jscore: admin.js, admin-core.js, settings-vouchers.js).
 * Referensi temuan: review_uiux_webui.md bagian 5.5 RE-REVIEW RONDE 2
 * (ID: S28, S23, S29-followup, R25, R28-part).
 *
 * Run with:  node --test static/js/uiux-batch6-jscore.test.mjs   (from webui/)
 *
 * Latar belakang & dampak bisnis:
 *   - S28: admin.js memuat dua strategi pencarian sekaligus. Versi client-side
 *     (filterExamRows/searchExams/clearSearch/searchExamsWithStatus) MATI karena
 *     dashboard.html mendefinisikan ulang versi URL-navigasi setelahnya, tapi
 *     listener Enter-nya tetap hidup → Enter di kolom cari menyembunyikan baris
 *     sesaat lalu reload penuh (flicker dobel-alur). Ditambah ±200 baris dead
 *     code lain yang membebani pemeliharaan & cache klien sekolah.
 *   - S23: tidak ada satu pun cek 401 di JS admin. Saat sesi habis di halaman
 *     monitoring yang dibiarkan terbuka, poll tiap 5 detik terus menampilkan
 *     error generik tanpa arahan login ulang — guru mengira "server down".
 *   - S29-followup: settings-vouchers.js masih mendefinisikan ulang copyCode
 *     versi TANPA guard navigator.clipboard, menimpa versi guarded dari
 *     admin-core.js → klik salin kode voucher gagal senyap di HTTP LAN.
 *   - R25: 26 fungsi open/close modal boilerplate; API Modal.open/Modal.close
 *     terpusat di core agar perilaku dasar modal satu pintu.
 *   - R28-part: format tanggal tak satu pintu; helper formatDateTimeID()
 *     menjadi satu-satunya formatter `YYYY-MM-DD HH:MM`.
 *
 * Pola sama dengan uiux-batch1.test.mjs + uiux-batch4-jscore.test.mjs:
 * kontrak statik (fs read) + perilaku via vm.runInNewContext mengeksekusi JS
 * ASLI yang dikirim ke browser dengan stub DOM minimal.
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
const VOUCHERS_SRC = fs.readFileSync(path.join(__dirname, 'settings-vouchers.js'), 'utf8');
const read = (rel) => fs.readFileSync(path.join(WEBUI_ROOT, rel), 'utf8');
const DASHBOARD = read('templates/admin/dashboard.html');

/** Ekstrak sumber deklarasi `function name(...) {...}` dengan penghitungan kurawal. */
function extractFunction(src, name) {
    const start = src.indexOf('function ' + name + '(');
    if (start === -1) return null;
    const open = src.indexOf('{', start);
    let depth = 0;
    for (let j = open; j < src.length; j++) {
        if (src[j] === '{') depth++;
        else if (src[j] === '}') {
            depth--;
            if (depth === 0) return src.slice(start, j + 1);
        }
    }
    return null;
}

// --- harness perilaku --------------------------------------------------------

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
        _spies: { focus: 0, select: 0, reset: 0 }
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
    node.querySelector = () => null;
    node.querySelectorAll = () => [];
    node.closest = () => null;
    node.focus = () => { node._spies.focus++; };
    node.select = () => { node._spies.select++; };
    node.reset = () => { node._spies.reset++; };
    node.getBoundingClientRect = () => ({ bottom: 0, left: 0 });
    return node;
}

// Muat admin-core.js (+admin.js /+settings-vouchers.js sesuai kebutuhan, urutan
// sama seperti urutan <script> di halaman) ke satu sandbox vm.
function loadScripts({ withAdminJs = false, withVouchers = false } = {}) {
    const allNodes = [];
    const register = (n) => { allNodes.push(n); return n; };
    const docListeners = {};
    const winListeners = {};
    const timers = []; // {fn, ms}
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
    // showToast membaca container.children & firstElementChild
    Object.defineProperty(toastContainer, 'firstElementChild', {
        get() { return toastContainer.children[0] || null; }
    });

    const win = {
        fetch: async () => ({ ok: true, status: 200, text: () => Promise.resolve('{}') }),
        location: { origin: 'http://192.168.1.10:8080', pathname: '/admin/dashboard', search: '?page=2', href: '' },
        addEventListener(type, fn) { (winListeners[type] = winListeners[type] || []).push(fn); },
        removeEventListener() {},
        dispatchEvent(ev) {
            (winListeners[ev.type] || []).slice().forEach((fn) => fn(ev));
            return true;
        },
        __settingsReady: {}
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
        URLSearchParams,
        console,
        setTimeout(fn, ms) { timers.push({ fn, ms }); return timers.length; },
        clearTimeout() {},
        setInterval() { return 0; },
        clearInterval() {},
        location: win.location
    };
    sandbox.globalThis = sandbox;
    vm.createContext(sandbox);

    vm.runInContext(ADMIN_CORE_SRC, sandbox, { filename: 'admin-core.js' });
    if (withAdminJs) vm.runInContext(ADMIN_JS_SRC, sandbox, { filename: 'admin.js' });
    if (withVouchers) vm.runInContext(VOUCHERS_SRC, sandbox, { filename: 'settings-vouchers.js' });

    return {
        sandbox,
        doc: docMock,
        docListeners,
        winListeners,
        timers,
        makeNode: (tag) => register(fakeNode(tag)),
        fireWindowEvent: (type, ev) => { ev.type = type; (winListeners[type] || []).forEach((fn) => fn(ev)); }
    };
}

// ---------------------------------------------------------------------------
// S28 — pencarian ganda & dead code admin.js/admin-core.js
// ---------------------------------------------------------------------------

test('S28 (statik): versi client-side filterExamRows/searchExams/clearSearch/searchExamsWithStatus hilang dari admin.js', () => {
    for (const name of ['filterExamRows', 'searchExams', 'clearSearch', 'searchExamsWithStatus']) {
        assert.equal(extractFunction(ADMIN_JS_SRC, name), null,
            `${name} versi client-side harus dihapus — dashboard.html memuat versi URL-navigasi sendiri`);
    }
    assert.ok(!ADMIN_JS_SRC.includes('debounceSearch'),
        'const debounceSearch hanya melayani filterExamRows yang mati');
    assert.ok(!ADMIN_JS_SRC.includes('filterExamRows'),
        'tidak boleh ada sisa referensi filterExamRows (termasuk listener Enter DOMContentLoaded)');
    assert.ok(!/DOMContentLoaded[\s\S]{0,400}filterExamRows/.test(ADMIN_JS_SRC),
        'listener Enter yang memicu flicker dobel-alur harus hilang');
});

test('S28 (positif): dashboard.html tetap punya versi URL-navigasi sendiri (strategi server-side utuh)', () => {
    assert.match(DASHBOARD, /function searchExamsWithStatus\(\)/,
        'dashboard.html inline script adalah versi HIDUP — jangan sampai ikut terhapus');
    assert.match(DASHBOARD, /function clearSearch\(\)/);
});

test('S28 (statik): kandidat dead-code tanpa pemanggil dihapus dari admin.js', () => {
    for (const name of ['openManageUsersModal', 'closeManageUsersModal', 'resetNewUserFormDefaults',
        'copyAllTokens', 'copyResultsLink', 'regenerateToken']) {
        assert.equal(extractFunction(ADMIN_JS_SRC, name), null,
            `${name} terverifikasi nol-pemanggil di templates/ + static/js/ → hapus`);
    }
    assert.ok(!ADMIN_JS_SRC.includes('manageUsersModal'),
        'branch manageUsersModal di listener overlay-click ikut hilang');
});

test('S28 (statik): initPasswordStrengthMeter + shortcut "?" dihapus dari admin-core.js', () => {
    assert.equal(extractFunction(ADMIN_CORE_SRC, 'initPasswordStrengthMeter'), null,
        'initPasswordStrengthMeter nol-pemanggil → hapus');
    assert.equal(extractFunction(ADMIN_CORE_SRC, 'toggleShortcuts'), null,
        '#shortcutsHint tak eksis di template mana pun → toggleShortcuts hanya preventDefault');
    assert.ok(!ADMIN_CORE_SRC.includes("e.key === '?'"),
        'binding tombol "?" dihapus');
});

test('S28 (positif): fungsi yang MASIH dirujuk template tetap ada', () => {
    // dashboard.html:902 onclick="copyAIPrompt()"
    assert.ok(extractFunction(ADMIN_JS_SRC, 'copyAIPrompt'), 'copyAIPrompt dipakai onclick dashboard');
    // copyToken dipakai onclick baris token (dashboard/submissions)
    assert.ok(extractFunction(ADMIN_JS_SRC, 'copyToken'), 'copyToken tetap ada');
    // closeEditUserModal/closeDetailModal dipanggil alur modal lain
    assert.ok(extractFunction(ADMIN_JS_SRC, 'closeEditUserModal'));
    assert.ok(extractFunction(ADMIN_JS_SRC, 'closeDetailModal'));
});

test('S28 (perilaku): shortcut "/" dan Ctrl+U tetap hidup; "?" tidak lagi preventDefault', () => {
    const env = loadScripts({ withAdminJs: true });
    const search = env.makeNode('input');
    search.id = 'searchExam';
    env.doc.body.appendChild(search);
    const examName = env.makeNode('input');
    examName.id = 'examName';
    env.doc.body.appendChild(examName);

    env.sandbox.initKeyboardShortcuts();

    const slash = { type: 'keydown', key: '/', ctrlKey: false, metaKey: false, shiftKey: false, target: search, preventDefault() { this.defaultPrevented = true; } };
    env.doc.dispatchEvent(slash);
    assert.equal(slash.defaultPrevented, true, "'/' tetap preventDefault");
    assert.equal(search._spies.focus, 1, "'/' tetap fokus ke #searchExam");

    const ctrlU = { type: 'keydown', key: 'u', ctrlKey: true, target: examName, preventDefault() { this.defaultPrevented = true; } };
    env.doc.dispatchEvent(ctrlU);
    assert.equal(ctrlU.defaultPrevented, true, 'Ctrl+U tetap hidup');
    assert.equal(examName._spies.focus, 1);

    const qmark = { type: 'keydown', key: '?', shiftKey: true, target: env.doc.body, preventDefault() { this.defaultPrevented = true; } };
    env.doc.dispatchEvent(qmark);
    assert.equal(qmark.defaultPrevented, false, '"?" tidak lagi dibajak (elemen hints tak eksis)');
    assert.equal(env.sandbox.toggleShortcuts, undefined, 'toggleShortcuts tidak diekspos lagi');
    assert.equal(env.sandbox.initPasswordStrengthMeter, undefined);
});

// ---------------------------------------------------------------------------
// S23 — penanganan sesi kedaluwarsa (401) global
// ---------------------------------------------------------------------------

test('S23 (statik): apiFetch mendeteksi 401 + event auth:expired + flag global terekspos', () => {
    assert.match(ADMIN_CORE_SRC, /status === 401/, 'apiFetch wajib memeriksa resp.status === 401');
    assert.match(ADMIN_CORE_SRC, /auth:expired/, 'event window bernama auth:expired');
    assert.match(ADMIN_CORE_SRC, /__examvanAuthExpired/,
        'flag window.__examvanAuthExposed untuk polling (kontrak lintas-agen: nama flag __examvanAuthExpired)');
    assert.match(ADMIN_CORE_SRC, /\/admin\/login\?next=/, 'redirect ke login dengan next=');
});

test('S23 (perilaku): fetch 401 memicu auth:expired TEPAT SATU kali walau apiFetch dipanggil ulang', async () => {
    const env = loadScripts();
    let fired = 0;
    env.sandbox.window.addEventListener('auth:expired', () => { fired++; });

    const unauthorized = {
        ok: false, status: 401, statusText: 'Unauthorized',
        url: 'http://192.168.1.10:8080/admin/api/pengawas/state',
        headers: {}, text: () => Promise.resolve('{"success":false,"message":"unauthorized"}')
    };
    env.sandbox.window.fetch = async () => unauthorized;

    await env.sandbox.apiFetch('/admin/api/pengawas/state');
    await env.sandbox.apiFetch('/admin/api/pengawas/state');
    await env.sandbox.apiFetch('/admin/api/pengawas/state');
    await env.sandbox.apiFetch('/admin/api/pengawas/state');

    assert.equal(fired, 1, 'guard once: event hanya ditembakkan pada 401 PERTAMA');
    assert.equal(env.sandbox.window.__examvanAuthExpired, true, 'flag global menyala untuk polling');
});

test('S23 (perilaku): listener global me-toast pesan spesifik lalu redirect /admin/login?next=<path+search>', async () => {
    const env = loadScripts();
    const toasts = [];
    env.sandbox.showToast = (msg, type) => toasts.push({ msg, type });

    const unauthorized = {
        ok: false, status: 401, statusText: 'Unauthorized',
        url: 'http://x/admin/api/stats', headers: {},
        text: () => Promise.resolve('{"success":false}')
    };
    env.sandbox.window.fetch = async () => unauthorized;

    await env.sandbox.apiFetch('/admin/api/stats');

    // Toast spesifik dikirim oleh listener auth:expired
    assert.deepEqual(toasts, [{ msg: 'Sesi berakhir. Silakan login kembali.', type: 'error' }]);

    // Redirect ditunda dengan delay singkat — timer terekam di harness
    assert.equal(env.timers.length >= 1, true, 'redirect dijadwalkan dengan delay singkat');
    const redirectTimer = env.timers.find((t) => String(t.fn).includes('login'));
    assert.ok(redirectTimer, 'timer redirect berisi navigasi ke /admin/login');

    // Jalankan timer manual → location.href berisi next=path+search
    redirectTimer.fn();
    assert.equal(env.sandbox.location.href, '/admin/login?next=%2Fadmin%2Fdashboard%3Fpage%3D2',
        'redirect mempertahankan pathname+search sebagai next');
    assert.ok(redirectTimer.ms <= 5000, 'delay singkat (≤5 detik)');
});

test('S23 (perilaku): error NON-401 tidak memicu auth:expired maupun redirect', async () => {
    const env = loadScripts();
    const serverError = {
        ok: false, status: 500, statusText: 'ISE', url: 'http://x/a',
        headers: {}, text: () => Promise.resolve('{"success":false,"message":"boom"}')
    };
    env.sandbox.window.fetch = async () => serverError;

    const res = await env.sandbox.apiFetch('/admin/api/x');
    const data = await res.json();

    assert.equal(data.success, false, 'jalur error existing tetap bekerja');
    assert.notEqual(env.sandbox.window.__examvanAuthExpired, true, 'flag auth expired tidak menyala');
    assert.equal(env.timers.length, 0, 'tidak ada redirect terjadwal');
});

// ---------------------------------------------------------------------------
// S29-followup — settings-vouchers.js berhenti menimpa copyCode guarded
// ---------------------------------------------------------------------------

test('S29-followup (statik): settings-vouchers.js tidak lagi mendefinisikan copyCode sendiri', () => {
    assert.equal(/(^|\n)\s*function copyCode\s*\(/.test(VOUCHERS_SRC), false,
        'override copyCode tanpa guard navigator.clipboard dihapus — pakai versi guarded admin-core.js');
    assert.ok(/copyCode\(\s*target\.closest/.test(VOUCHERS_SRC),
        'pemanggil lokal tetap memakai copyCode (kini versi guarded dari core)');
});

test('S29-followup (perilaku): setelah load core+voucher, copyCode tetap versi guarded (aman tanpa navigator.clipboard)', () => {
    const env = loadScripts({ withVouchers: true });
    env.sandbox.navigator.clipboard = undefined;
    const execCalls = [];
    env.sandbox.document.execCommand = (cmd) => { execCalls.push(cmd); return true; };
    env.sandbox.showToast = () => {};

    assert.doesNotThrow(() => env.sandbox.copyCode('VOUCH-123'),
        'copyCode hasil akhir HARUS versi guarded — fallback textarea/execCommand, bukan TypeError');

    const ta = env.doc.body.children.concat().reverse().find((n) => n.tagName === 'TEXTAREA');
    assert.ok(ta, 'fallback membuat textarea sementara');
    assert.equal(ta.value, 'VOUCH-123');
    assert.deepEqual(execCalls, ['copy']);
});

// ---------------------------------------------------------------------------
// R25 — API Modal terpusat (Modal.open / Modal.close)
// ---------------------------------------------------------------------------

test('R25 (statik): admin-core.js mengekspor Modal.open/Modal.close', () => {
    assert.match(ADMIN_CORE_SRC, /(var|const|let)?\s*Modal\s*=\s*\{/, 'objek Modal didefinisikan di core');
});

test('R25 (perilaku): Modal.open/close mengubah style.display overlay dengan benar', () => {
    const env = loadScripts();
    const overlay = env.makeNode('div');
    overlay.id = 'someModal';
    overlay.style.display = 'none';
    env.doc.body.appendChild(overlay);

    assert.equal(typeof env.sandbox.Modal.open, 'function');
    assert.equal(typeof env.sandbox.Modal.close, 'function');

    assert.equal(env.sandbox.Modal.open('someModal'), true, 'Modal.open by id');
    assert.equal(overlay.style.display, 'flex', 'pola existing: display flex saat dibuka');
    assert.equal(env.sandbox.Modal.close(overlay), true, 'Modal.close by element');
    assert.equal(overlay.style.display, 'none');

    assert.equal(env.sandbox.Modal.open('tidakAda'), false, 'elemen tak eksis → false, tidak throw');
});

test('R25 (perilaku refactor): openChangePasswordModal/closeChangePasswordModal mendelegasikan ke Modal & tetap toggle elemen yang sama', () => {
    const env = loadScripts({ withAdminJs: true });
    const modal = env.makeNode('div');
    modal.id = 'changePasswordModal';
    env.doc.body.appendChild(modal);
    const form = env.makeNode('form');
    form.id = 'changePasswordForm';
    env.doc.body.appendChild(form);

    env.sandbox.openChangePasswordModal();
    assert.equal(modal.style.display, 'flex', 'path open tetap membuka #changePasswordModal');
    assert.equal(form._spies.reset, 1, 'perilaku reset form dipertahankan');

    env.sandbox.closeChangePasswordModal();
    assert.equal(modal.style.display, 'none', 'path close tetap menutup #changePasswordModal');

    // Delegasi tipis: body fungsi memanggil Modal.*
    assert.match(extractFunction(ADMIN_JS_SRC, 'openChangePasswordModal'), /Modal\.open/);
    assert.match(extractFunction(ADMIN_JS_SRC, 'closeChangePasswordModal'), /Modal\.close/);
});

test('R25 (perilaku refactor): closeEditTokenModal tetap menutup #editTokenModal + reset form', () => {
    const env = loadScripts({ withAdminJs: true });
    const modal = env.makeNode('div');
    modal.id = 'editTokenModal';
    env.doc.body.appendChild(modal);
    const form = env.makeNode('form');
    form.id = 'editTokenForm';
    env.doc.body.appendChild(form);

    env.sandbox.closeEditTokenModal();
    assert.equal(modal.style.display, 'none');
    assert.equal(form._spies.reset, 1, 'reset form edit token dipertahankan');
});

// Batch 12 (T22): test delegasi closeConfirmActionModal DIHAPUS — fungsi
// ad-hoc voucher (beserta modal arwahnya) dihapus total; konfirmasi voucher
// kini memakai showConfirm core. Kontrak R25 tetap berlaku untuk delegasi
// lain di bawah.

test('R25 (statik): fungsi open/close ad-hoc lain di admin.js menjadi delegasi Modal.*', () => {
    for (const name of ['openQuestionsModal', 'closeQuestionsModal', 'openEditTokenModal',
        'closeDetailModal', 'openDelegateExamModal', 'closeDelegateExamModal']) {
        const body = extractFunction(ADMIN_JS_SRC, name);
        assert.ok(body, `${name} tetap ada`);
        assert.match(body, /Modal\.(open|close)/, `${name} wajib mendelegasikan ke API Modal`);
    }
});

// ---------------------------------------------------------------------------
// R28-part — satu pintu format tanggal (formatDateTimeID)
// ---------------------------------------------------------------------------

test('R28 (statik): formatDateTimeID ada di admin-core.js; pemakaian internal pindah padanya', () => {
    assert.ok(extractFunction(ADMIN_CORE_SRC, 'formatDateTimeID'), 'helper formatDateTimeID didefinisikan di core');
    assert.ok(!ADMIN_JS_SRC.includes('localizeUTC'),
        'admin.js tidak lagi memanggil localizeUTC secara langsung — pakai formatDateTimeID');
});

test('R28 (perilaku): output stabil YYYY-MM-DD HH:MM dan identik dengan formatter lama', () => {
    const env = loadScripts();
    const f = env.sandbox.formatDateTimeID;
    const localize = env.sandbox.localizeUTC;

    assert.equal(typeof f, 'function', 'formatDateTimeID terekspos');
    const samples = [
        '2026-08-23T09:05:00Z',
        '2026-01-02 14:30:00',
        '2026-12-31T23:59Z',
        ''
    ];
    for (const s of samples) {
        assert.equal(f(s), localize(s), `output identik formatter manual lama untuk ${JSON.stringify(s)}`);
    }

    const out = f('2026-08-23T09:05:00Z');
    assert.match(out, /^\d{4}-\d{2}-\d{2} \d{2}:\d{2}$/, 'format konsisten YYYY-MM-DD HH:MM');
    assert.equal(out.length, 16, 'panjang stabil (zero-padded)');

    assert.equal(f(''), '—', 'input kosong → em-dash (perilaku lama)');
    assert.equal(f('bukan-tanggal'), 'bukan-tanggal', 'input invalid dikembalikan apa adanya (perilaku lama)');
});
