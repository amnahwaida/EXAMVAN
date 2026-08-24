/* Contract + behavior tests untuk Batch 8 — konsolidasi registrasi Actions
 * (milik agen batch-8-actions). Referensi temuan: review_uiux_webui.md,
 * follow-up migrasi onclick → delegasi data-action Batch 7 (R28-lanjutan).
 *
 * Run with:  node --test static/js/uiux-batch8-actions.test.mjs   (from webui/)
 *
 * Latar belakang & dampak bisnis:
 *   - B8-1: Batch 7 meninggalkan WRAPPER tipis Actions.register di inline
 *     script settings.html & submissions.html padahal fungsinya hidup di
 *     modul lain (admin.js / settings-vouchers.js). Registrasi yang tidak
 *     tinggal di modul definisinya membuat jejak kode terpecah: pengembang
 *      yang mengubah perilaku tombol mencari handler di halaman, padahal
 *     hanya ada penerus argumen. Semua registrasi dipindahkan ke modul
 *     pemilik fungsinya sehingga SATU tempat mendefinisikan + mendaftarkan.
 *   - B8-2: `modal-dismiss` didaftarkan DUA KALI (admin.js dan settings.html).
 *     Pendaftaran kedua MENIMPA yang pertama di registry (console.warn per
 *     kontrak Actions.register) — boros eksekusi saat load dan rawan saling
 *     menimpa bila salah satu berubah. Satu registrasi kanonik dipindah ke
 *     admin-core.js sehingga tersedia otomatis di SEMUA halaman.
 *   - B8-3: handler hasil delegasi masih meneruskan string mentah untuk id
 *     numerik (data-exam-id / data-submission-id) — inkonsisten dengan pola
 *     yang benar (exam-toggle-status) dan berisiko bug perbandingan tipe /
 *     interpolasi URL. Dinormalisasi parseInt(x, 10).
 *   - B8-4: shim defensif `window.Actions = {...}` di settings.html tidak
 *     diperlukan lagi — admin-core.js selalu dimuat lebih dulu oleh partial
 *     head/nav. Shim duplikat menyembunyikan regresi kontrak core.
 *
 * Pola sama dengan uiux-batch7-core.test.mjs: kontrak statik (fs read) +
 * perilaku via vm.runInContext mengeksekusi JS ASLI dengan stub DOM minimal.
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

const ADMIN_CORE_SRC = read('static/js/admin-core.js');
const ADMIN_JS_SRC = read('static/js/admin.js');
const SETTINGS_HTML = read('templates/admin/settings.html');
const SUBMISSIONS_HTML = read('templates/admin/submissions.html');
const VOUCHERS_JS_SRC = read('static/js/settings-vouchers.js');

/** Nama-nama yang didaftarkan lewat Actions.register('nama', ...) pada sebuah sumber. */
function registeredNames(src) {
    return new Set(
        [...src.matchAll(/Actions\.register\(\s*['"]([a-z0-9-]+)['"]/g)].map((m) => m[1])
    );
}

/** Ekstrak statement registrasi tertentu agar bisa dieksekusi vm secara terisolasi. */
function extractRegistrations(src, names, label) {
    return names.map((n) => {
        const re = new RegExp("Actions\\.register\\(\\s*['\"]" + n + "['\"][\\s\\S]*?\\}\\);");
        const m = src.match(re);
        assert.ok(m, `[${label}] registrasi '${n}' harus ditemukan di sumber`);
        return m[0];
    }).join('\n');
}

// ---------------------------------------------------------------------------
// B8-1 — registrasi wrapper pindah ke modul pemiliknya
// ---------------------------------------------------------------------------

test('B8-1 (statik): wrapper ke admin.js didaftarkan di admin.js, bukan lagi di settings.html/submissions.html', () => {
    const adminOwned = [
        // Pengaturan (settings.html)
        'smtp-test', 'smtp-save', 'turnstile-save', 'cleanup-save',
        'default-pkg-save', 'versions-save', 'footer-save', 'seo-save',
        'monetization-save',
        // Batch 9 (R33): password-modal-close dihapus — tombol ✕ modal Ubah
        // Password settings memakai aksi generik modal-close + data-modal-close
        // (paritas dengan dashboard.html; satu mekanisme, tanpa drift).
        // Kelola User (fungsinya di admin.js: loadUsersList dll.)
        'users-refresh-list', 'users-clear-search', 'users-search', 'users-toggle-sort',
        // Submissions (submissions.html)
        'show-submission-detail', 'delete-submission', 'close-detail-modal', 'export-submissions'
    ];
    const inAdminJs = registeredNames(ADMIN_JS_SRC);
    const inSettings = registeredNames(SETTINGS_HTML);
    const inSubmissions = registeredNames(SUBMISSIONS_HTML);

    for (const name of adminOwned) {
        assert.ok(inAdminJs.has(name),
            `Actions.register('${name}') harus ada di admin.js (modul pemilik fungsinya)`);
        assert.ok(!inSettings.has(name), `'${name}' tidak boleh lagi diregister di settings.html`);
        assert.ok(!inSubmissions.has(name), `'${name}' tidak boleh lagi diregister di submissions.html`);
    }
});

test('B8-1 (statik): fungsi pemilik aksi pindahan memang terdefinisi di admin.js', () => {
    const owners = [
        'testSmtpConnection', 'saveSmtpSettings', 'saveTurnstileSettings',
        'saveCleanupSettings', 'saveDefaultPkgSettings', 'saveVersionsSettings',
        'saveFooterSettings', 'saveSeoSettings', 'saveMonetizationSettings',
        'closeChangePasswordModal', 'loadUsersList', 'clearUsersSearch',
        'toggleUsersSort', 'getCurrentUsersPage', 'showSubmissionDetail',
        'deleteSubmission', 'closeDetailModal', 'exportSubmissions'
    ];
    for (const fn of owners) {
        assert.match(ADMIN_JS_SRC, new RegExp('function\\s+' + fn + '\\b'),
            `${fn} harus hidup di admin.js`);
    }
});

test('B8-1 (statik): wrapper voucher + confirm-action-close didaftarkan di settings-vouchers.js', () => {
    const voucherOwned = [
        'voucher-open-batch', 'voucher-open-single', 'voucher-close-batch',
        'voucher-close-single', 'voucher-close-redemptions', 'voucher-search',
        'voucher-search-clear'
        // Batch 12 (T22): confirm-action-close DIHAPUS — modal arwah voucher
        // diganti showConfirm core; tidak ada lagi registrasinya.
    ];
    const inVouchers = registeredNames(VOUCHERS_JS_SRC);
    assert.ok(!inVouchers.has('confirm-action-close'),
        "registrasi arwah 'confirm-action-close' tidak boleh kembali");
    for (const name of voucherOwned) {
        assert.ok(inVouchers.has(name),
            `Actions.register('${name}') harus ada di settings-vouchers.js`);
        assert.ok(!registeredNames(SETTINGS_HTML).has(name),
            `'${name}' tidak boleh lagi diregister di settings.html`);
    }
    // Fungsi target memang didefinisikan di modul yang sama.
    for (const fn of ['openBatchModal', 'openSingleModal', 'closeBatchModal',
        'closeSingleModal', 'closeRedemptionsModal', 'loadVouchers',
        'clearVoucherSearch']) {
        assert.match(VOUCHERS_JS_SRC, new RegExp('function\\s+' + fn + '\\b'),
            `${fn} harus hidup di settings-vouchers.js`);
    }
});

test('B8-1 (statik): voucher-subtab (fungsi inline settings.html) TETAP diregister di settings.html', () => {
    assert.ok(registeredNames(SETTINGS_HTML).has('voucher-subtab'),
        'aksi yang fungsinya hidup di inline script halaman tetap diregister di sana');
});

test('B8-1 (statik): komentar FOLLOW-UP wrapper habis — tidak ada penunjuk usang tersisa', () => {
    assert.doesNotMatch(SETTINGS_HTML, /FOLLOW-UP \(agen pemilik\)/,
        'penanda follow-up di settings.html harus dihapus setelah registrasi dipindah');
    assert.doesNotMatch(SUBMISSIONS_HTML, /FOLLOW-UP/,
        'penanda follow-up di submissions.html harus dihapus setelah registrasi dipindah');
});

test('B8-1 (statik): submissions.html bebas registrasi, sisakan komentar penunjuk ke admin.js', () => {
    assert.equal(registeredNames(SUBMISSIONS_HTML).size, 0,
        'submissions.html tidak boleh lagi memuat Actions.register apa pun');
    assert.match(SUBMISSIONS_HTML, /admin-core\.js/, 'submissions.html wajib memuat admin-core.js');
    assert.match(SUBMISSIONS_HTML, /admin\.js/, 'komentar penunjuk bahwa registrasi kini di admin.js harus ada');
});

// ---------------------------------------------------------------------------
// B8-2 — satu registrasi kanonik modal-dismiss di admin-core.js
// ---------------------------------------------------------------------------

test('B8-2 (statik): modal-dismiss didaftarkan TEPAT SEKALI dan hanya di admin-core.js', () => {
    const sources = [
        ['admin-core.js', ADMIN_CORE_SRC],
        ['admin.js', ADMIN_JS_SRC],
        ['settings.html', SETTINGS_HTML]
    ];
    let total = 0;
    const places = [];
    for (const [name, src] of sources) {
        const n = registeredNames(src).has('modal-dismiss') ? 1 : 0;
        total += n;
        if (n) places.push(name);
    }
    assert.equal(total, 1, `modal-dismiss harus didaftarkan tepat sekali, kini di: ${places.join(', ') || 'tidak ada'}`);
    assert.ok(registeredNames(ADMIN_CORE_SRC).has('modal-dismiss'),
        'registrasi kanonik modal-dismiss harus hidup di admin-core.js (tersedia di semua halaman)');
    // Semantik superset: resolver close-fn via window lalu fallback globalThis.
    const snippet = ADMIN_CORE_SRC.match(/Actions\.register\(\s*['"]modal-dismiss['"][\s\S]*?\}\);/);
    assert.ok(snippet, 'blok registrasi modal-dismiss bisa diekstrak dari admin-core.js');
    assert.match(snippet[0], /globalThis/, 'harus memakai fallback globalThis (semantik superset)');
});

// ---------------------------------------------------------------------------
// B8-3 — normalisasi parseInt untuk id numerik di handler admin.js
// ---------------------------------------------------------------------------

test('B8-3 (statik): handler id numerik memakai parseInt(attr, 10)', () => {
    const cases = [
        ['token-edit-open', 'data-exam-id'],
        ['questions-open', 'data-exam-id'],
        ['exam-delete', 'data-exam-id'],
        ['edit-exam-open', 'data-exam-id'],
        ['delegate-exam-open', 'data-exam-id'],
        ['show-submission-detail', 'data-submission-id'],
        ['delete-submission', 'data-submission-id']
    ];
    for (const [action, attr] of cases) {
        const re = new RegExp("Actions\\.register\\(\\s*'" + action + "'[\\s\\S]*?\\}\\);");
        const m = ADMIN_JS_SRC.match(re);
        assert.ok(m, `registrasi '${action}' ada di admin.js`);
        assert.match(m[0], new RegExp('parseInt\\(\\s*[a-zA-Z]+\\.getAttribute\\(\\\'' + attr + "\\'\\)\\s*,\\s*10\\s*\\)"),
            `'${action}' wajib menormalisasi ${attr} dengan parseInt(..., 10)`);
    }
});

test('B8-3 (statik): argumen yang sengaja string TIDAK ikut di-parse (token, nama, warna)', () => {
    for (const [action, attr] of [
        ['token-copy', 'data-token'],
        ['panel-color-set', 'data-color']
    ]) {
        const re = new RegExp("Actions\\.register\\(\\s*'" + action + "'[\\s\\S]*?\\}\\);");
        const m = ADMIN_JS_SRC.match(re);
        assert.ok(m, `registrasi '${action}' ada di admin.js`);
        assert.match(m[0], new RegExp('getAttribute\\(\\\'' + attr + "\\'\\)"),
            `'${action}' tetap membaca ${attr}`);
        assert.doesNotMatch(m[0], /parseInt/,
            `'${action}' tidak boleh mengubah nilai string menjadi angka`);
    }
});

// ---------------------------------------------------------------------------
// B8-4 — shim defensif Actions dihapus dari settings.html
// ---------------------------------------------------------------------------

test('B8-4 (statik): shim defensif window.Actions DIHAPUS dari settings.html', () => {
    assert.doesNotMatch(SETTINGS_HTML, /window\.Actions\s*=\s*\{/,
        'shim "window.Actions = {...}" tidak boleh ada lagi — kontrak core pasti');
    assert.doesNotMatch(SETTINGS_HTML, /registry\[el\.getAttribute\('data-action'\)\]/,
        'listener delegasi tiruan milik shim juga harus hilang (disediakan admin-core.js)');
});

// ---------------------------------------------------------------------------
// Harness perilaku (pola uiux-batch7-core.test.mjs)
// ---------------------------------------------------------------------------

function fakeNode(tag) {
    const node = {
        tagName: String(tag || '').toUpperCase(),
        children: [],
        parentNode: null,
        attrs: {},
        className: '',
        style: {},
        dataset: {},
        _spies: {}
    };
    node.classList = {
        add() {}, remove() {}, contains() { return false; }, toggle() { return false; }
    };
    node.getAttribute = (n) => (Object.prototype.hasOwnProperty.call(node.attrs, n) ? node.attrs[n] : null);
    node.setAttribute = (n, v) => { node.attrs[n] = String(v); };
    Object.defineProperty(node, 'id', {
        get() { return node.attrs.id || ''; },
        set(v) { if (v) node.attrs.id = String(v); else delete node.attrs.id; }
    });
    node.closest = function (sel) {
        if (sel !== '[data-action]') return null;
        let cur = node;
        while (cur) {
            if (Object.prototype.hasOwnProperty.call(cur.attrs, 'data-action')) return cur;
            cur = cur.parentNode;
        }
        return null;
    };
    return node;
}

function loadCore() {
    const allNodes = [];
    const register = (n) => { allNodes.push(n); return n; };
    const docListeners = {};
    const docMock = {
        readyState: 'complete',
        documentElement: register(fakeNode('html')),
        body: register(fakeNode('body')),
        getElementById() { return null; },
        createElement(tag) { return register(fakeNode(tag)); },
        querySelector() { return null; },
        querySelectorAll() { return []; },
        addEventListener(type, fn) { (docListeners[type] = docListeners[type] || []).push(fn); },
        removeEventListener() {},
        dispatchEvent() { return true; }
    };
    const win = {
        location: { origin: 'http://x', pathname: '/admin/dashboard', search: '', href: '' },
        addEventListener() {},
        removeEventListener() {}
    };
    function MutationObserverMock() {}
    MutationObserverMock.prototype.observe = function () {};
    MutationObserverMock.prototype.disconnect = function () {};

    const sandbox = {
        window: win,
        document: docMock,
        CustomEvent: function (t) { this.type = t; },
        MouseEvent: function (t) { this.type = t; },
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

    const fireDocClick = (target, extra) => {
        const ev = Object.assign({ type: 'click', target, bubbles: true }, extra || {});
        (docListeners.click || []).slice().forEach((fn) => fn(ev));
        return ev;
    };
    /** Jalankan potongan JS tambahan dalam sandbox yang SAMA (Actions nyata). */
    const runSnippet = (code, filename) =>
        vm.runInContext(code, sandbox, { filename: filename || 'snippet.js' });
    const makeNode = (tag, attrs) => {
        const n = register(fakeNode(tag));
        Object.entries(attrs || {}).forEach(([k, v]) => n.setAttribute(k, v));
        return n;
    };
    return { sandbox, win, docListeners, makeNode, fireDocClick, runSnippet };
}

// ---------------------------------------------------------------------------
// Perilaku — B8-2: modal-dismiss kanonik dari core
// ---------------------------------------------------------------------------

test('B8-2 (perilaku): modal-dismiss dari core — klik kartu tidak menutup, backdrop menutup via window', () => {
    const env = loadCore();
    let closed = 0;
    env.win.closeSingleModal = () => { closed += 1; };

    const overlay = env.makeNode('div', {
        'data-action': 'modal-dismiss',
        'data-modal-close': 'closeSingleModal'
    });
    const card = env.makeNode('div');
    card.parentNode = overlay;

    env.fireDocClick(card);
    assert.equal(closed, 0, 'klik konten modal tidak boleh menutup (guard target===el)');
    env.fireDocClick(overlay);
    assert.equal(closed, 1, 'klik langsung backdrop memanggil window.closeSingleModal');
});

test('B8-2 (perilaku): fallback globalThis dipakai bila fungsi hanya ada di global sandbox', () => {
    const env = loadCore();
    let closed = 0;
    // Sengaja TIDAK di window — hanya di globalThis sandbox (identitas browser
    // sama; di sandbox vm keduanya terpisah sehingga jalur fallback teruji).
    env.sandbox.closeOtherModal = () => { closed += 1; };

    const overlay = env.makeNode('div', {
        'data-action': 'modal-dismiss',
        'data-modal-close': 'closeOtherModal'
    });
    env.fireDocClick(overlay);
    assert.equal(closed, 1, 'resolver harus jatuh ke globalThis bila window[nama] bukan fungsi');
});

// ---------------------------------------------------------------------------
// Perilaku — B8-1/B8-3: registrasi pindahan dieksekusi dari modul aslinya
// ---------------------------------------------------------------------------

test('B8-3 (perilaku): show-submission-detail/delete-submission menerima ANGKA hasil parseInt', () => {
    const env = loadCore();
    const calls = { detail: [], del: [] };
    env.sandbox.showSubmissionDetail = (id) => calls.detail.push(id);
    env.sandbox.deleteSubmission = (id) => calls.del.push(id);

    env.runSnippet(extractRegistrations(ADMIN_JS_SRC,
        ['show-submission-detail', 'delete-submission'], 'admin.js'), 'admin.js#b8');

    env.fireDocClick(env.makeNode('button', {
        'data-action': 'show-submission-detail', 'data-submission-id': '42'
    }));
    env.fireDocClick(env.makeNode('button', {
        'data-action': 'delete-submission', 'data-submission-id': '42'
    }));

    assert.deepEqual(calls.detail, [42], 'id harus sampai sebagai number, bukan string "42"');
    assert.deepEqual(calls.del, [42]);
});

test('B8-1 (perilaku): registrasi voucher di settings-vouchers.js hidup lewat delegasi core', () => {
    const env = loadCore();
    const calls = [];
    env.sandbox.loadVouchers = (p) => calls.push(['loadVouchers', p]);
    env.sandbox.clearVoucherSearch = () => calls.push(['clearVoucherSearch']);
    env.sandbox.openBatchModal = () => calls.push(['openBatchModal']);
    env.sandbox.openSingleModal = () => calls.push(['openSingleModal']);
    env.sandbox.closeBatchModal = () => calls.push(['closeBatchModal']);
    env.sandbox.closeSingleModal = () => calls.push(['closeSingleModal']);
    env.sandbox.closeRedemptionsModal = () => calls.push(['closeRedemptionsModal']);

    env.runSnippet(extractRegistrations(VOUCHERS_JS_SRC, [
        'voucher-open-batch', 'voucher-open-single', 'voucher-close-batch',
        'voucher-close-single', 'voucher-close-redemptions', 'voucher-search',
        'voucher-search-clear'
    ], 'settings-vouchers.js'), 'settings-vouchers.js#b8');

    env.fireDocClick(env.makeNode('button', { 'data-action': 'voucher-search' }));
    env.fireDocClick(env.makeNode('button', { 'data-action': 'voucher-close-single' }));
    env.fireDocClick(env.makeNode('button', { 'data-action': 'voucher-open-batch' }));

    assert.deepEqual(calls, [
        ['loadVouchers', 1],
        ['closeSingleModal'],
        ['openBatchModal']
    ], 'delegasi harus meneruskan ke fungsi modul voucher dengan argumen benar');
});

test('B8-1 (perilaku): users-refresh-list mempertahankan stopPropagation + halaman aktif', () => {
    const env = loadCore();
    const pages = [];
    let stopProp = 0;
    env.sandbox.getCurrentUsersPage = () => 7;
    env.sandbox.loadUsersList = (p) => pages.push(p);

    env.runSnippet(extractRegistrations(ADMIN_JS_SRC,
        ['users-refresh-list'], 'admin.js'), 'admin.js#b8-users');

    const btn = env.makeNode('button', { 'data-action': 'users-refresh-list' });
    env.fireDocClick(btn, { stopPropagation() { stopProp += 1; } });

    assert.deepEqual(pages, [7], 'refresh memuat ulang halaman AKTIF (getCurrentUsersPage), bukan halaman 1');
    assert.equal(stopProp, 1, 'stopPropagation harus dipertahankan (paritas onclick lama)');
});
