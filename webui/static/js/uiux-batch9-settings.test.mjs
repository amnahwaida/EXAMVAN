/* Contract + behavior tests untuk Batch 9 — halaman Pengaturan (milik agen
 * batch-9-settings). Referensi: review_uiux_webui.md bagian 5.6 RE-REVIEW
 * RONDE 3, temuan S37, S39, R29 (sisi vouchers), R32, plus kontrak lintas-
 * agen #toastContainer (T14-lanjutan).
 *
 * Run with:  node --test static/js/uiux-batch9-settings.test.mjs   (from webui/)
 *
 * Latar belakang & dampak bisnis:
 *   - S37: selama unggahan aplikasi berjalan, Escape/klik-overlay dari Global
 *     Modal Manager (admin-core.js, MILIK AGEN LAIN — tidak boleh diedit)
 *     tetap menutup modal via forceClose meski closeUploadModal menolak.
 *     Unggahan lanjut tanpa indikator dan bisa gagal diam-diam bila tab
 *     ditutup. Fix dalam batas kepemilikan: migrasi #uploadModal dari kelas
 *     arwah .modal-backdrop ke .modal-overlay standar, listener CAPTURE pada
 *     modal yang menahan Escape/klik-overlay sebelum handler manager, serta
 *     penguncian sementara style.display/classList selama flag
 *     __uploadInProgress aktif + pill progres "Mengunggah...".
 *   - S39: 8 kartu Pengaturan Umum tersimpan terpisah tanpa indikator mana
 *     yang belum disimpan dan tanpa guard navigasi. Kini tiap kartu punya
 *     titik indikator + label tombol bertanda "•", dan beforeunload aktif
 *     bila ada kartu kotor. Handler simpan ada di admin.js (agen lain), maka
 *     pembersihan dirty dilakukan lewat observasi toast sukses setelah klik
 *     simpan (mekanisme paling sederhana yang dapat diuji tanpa menyentuh
 *     admin.js) — follow-up: pindahkan pembersihan ke saveSaasSection bila
 *     kepemilikan berubah.
 *   - R29-sisi-vouchers: dua onclick inline tersisa di string HTML render-JS
 *     settings-vouchers.js (:16 retry daftar, :158 paginasi) bermigrasi ke
 *     data-action + registrasi modul sendiri (pola Batch 8).
 *   - R32: sweep label form settings.html tanpa asosiasi programatik.
 *   - Kontrak lintas-agen: #toastContainer DIHAPUS dari settings.html —
 *     partials/nav.html menjadi satu-satunya sumber container toast.
 *
 * Kontrak antar-agen yang DIKONSUMSI (di-stub di harness):
 *   - admin-core.js: Actions.register + delegasi dokumen [data-action],
 *     showToast (container #toastContainer dari nav.html).
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
const SYSTEM_APPS_SRC = read('static/js/settings-system-apps.js');
const GENERAL_SRC = read('static/js/settings-general.js');
const VOUCHERS_SRC = read('static/js/settings-vouchers.js');

// ===========================================================================
// S37 — Escape/backdrop tidak menembus guard "unggahan masih berlangsung"
// ===========================================================================

test('S37 (statik): #uploadModal memakai .modal-overlay standar — kelas arwah .modal-backdrop hilang', () => {
    const tag = /<div[^>]*id="uploadModal"[^>]*>/.exec(SETTINGS);
    assert.ok(tag, '#uploadModal harus ada');
    assert.match(tag[0], /class="modal-overlay"/, 'uploadModal wajib pakai kelas .modal-overlay standar');
    assert.doesNotMatch(tag[0], /modal-backdrop/, 'kelas arwah .modal-backdrop tidak boleh dipakai lagi');
    assert.doesNotMatch(SETTINGS, /class="[^"]*modal-backdrop/,
        'tidak boleh ada elemen lain yang masih memakai .modal-backdrop');
    // Kontrak lama Batch 7 tetap dijaga: semantik dialog + penunjuk close.
    assert.match(tag[0], /role="dialog"/);
    assert.match(tag[0], /data-action="modal-dismiss"/);
    assert.match(tag[0], /data-modal-close="closeUploadModal"/);
});

test('S37 (statik): guard unggahan + pill progres hidup di settings-system-apps.js & CSS-nya di-inline settings.html', () => {
    for (const fn of ['wireUploadCloseGuard', 'lockUploadOverlay', 'unlockUploadOverlay',
        'showUploadProgressPill', 'hideUploadProgressPill']) {
        assert.match(SYSTEM_APPS_SRC, new RegExp('function\\s+' + fn + '\\b'),
            `${fn} harus didefinisikan di settings-system-apps.js`);
    }
    // Listener capture pada modal (berjalan SEBELUM handler manager di dokumen).
    assert.match(SYSTEM_APPS_SRC, /addEventListener\(\s*'(?:keydown|click)'\s*,[\s\S]*?,\s*true\s*\)/,
        'guard wajib memakai listener fase capture');
    // CSS milik halaman untuk pill + indikator dirty (S39) — token saja.
    assert.match(SETTINGS, /\.upload-progress-pill\s*\{/, 'CSS pill progres harus di-inline di settings.html');
    assert.match(SETTINGS, /\.saas-dirty-dot\s*\{/, 'CSS titik dirty kartu harus di-inline di settings.html');
});

/** Fake node DOM minimal dengan registry listener per tipe. */
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
        // Paritas DOM nyata: men-set className juga memperbarui classList.
        set className(v) {
            _className = String(v);
            _className.split(/\s+/).filter(Boolean).forEach((c) => node.classList._set.add(c));
        },
        innerHTML: '',
        textContent: '',
        disabled: false,
        offsetWidth: 0,
        _listeners: {},
        _spies: { prevented: 0, stopped: 0 },
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
    node.dispatch = function (ev) {
        ev.preventDefault = ev.preventDefault || (() => { ev._prevented = true; });
        ev.stopPropagation = ev.stopPropagation || (() => { ev._stopped = true; });
        (node._listeners[ev.type] || []).slice().forEach((fn) => fn(ev));
        return ev;
    };
    node.appendChild = function (c) { c.parentNode = node; node.children.push(c); return c; };
    node.removeChild = function (c) {
        const i = node.children.indexOf(c);
        if (i !== -1) node.children.splice(i, 1);
    };
    node.querySelector = function (sel) {
        if (sel === '.saas-collapse-title' && node._titleEl) return node._titleEl;
        return node.children.find((c) => c.className && String(c.className).includes(sel.replace('.', ''))) || null;
    };
    node.closest = function (sel) {
        if (sel !== '[data-action]') return null;
        let cur = node;
        while (cur) {
            if (cur.attrs && cur.attrs['data-action']) return cur;
            cur = cur.parentNode;
        }
        return null;
    };
    return node;
}

function loadSystemAppsSandbox(byId) {
    const toasts = [];
    const created = [];
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
    win.showToast = (msg, tone) => toasts.push({ msg, tone });

    const docMock = {
        readyState: 'complete',
        body: fakeEl('body'),
        head: fakeEl('head'),
        getElementById: (id) => byId[id] || created.find((n) => n.attrs.id === id) || null,
        createElement: (tag) => { const n = fakeEl(tag); created.push(n); return n; },
        createTextNode: (text) => ({ nodeType: 3, textContent: String(text) }),
        querySelector: () => null,
        querySelectorAll: () => [],
        addEventListener() {},
        write() {}
    };

    const sandbox = {
        window: win,
        document: docMock,
        URLSearchParams,
        console,
        setTimeout: (fn) => 0,
        clearTimeout() {},
        get showToast() { return win.showToast; },
        get apiFetch() { return win.apiFetch; },
        get showConfirm() { return win.showConfirm; }
    };
    sandbox.globalThis = sandbox;
    vm.createContext(sandbox);
    vm.runInContext(SYSTEM_APPS_SRC, sandbox, { filename: 'settings-system-apps.js' });
    return { sandbox, win, toasts, body: docMock.body };
}

test('S37 (perilaku): saat unggah berjalan, Escape & klik-overlay DITAHAN oleh capture guard + toast', () => {
    const modal = fakeEl('div', { id: 'uploadModal' });
    const env = loadSystemAppsSandbox({ uploadModal: modal });

    env.sandbox.wireUploadCloseGuard();

    // Flag aktif → Escape harus dicegah SEBELUM sampai ke handler manager.
    env.win.__uploadInProgress = true;

    const esc = env.sandbox.document && {};
    const escapeEv = { type: 'keydown', key: 'Escape', _prevented: false, _stopped: false,
        preventDefault() { this._prevented = true; }, stopPropagation() { this._stopped = true; } };
    modal.dispatch(escapeEv);
    assert.ok(escapeEv._prevented, 'Escape wajib di-preventDefault saat unggah berjalan');
    assert.ok(escapeEv._stopped, 'Escape wajib di-stopPropagation agar handler manager (dokumen) tidak jalan');
    assert.ok(env.toasts.some((t) => t.tone === 'error' && /Unggahan masih berlangsung/.test(t.msg)),
        'wajib muncul toast "Unggahan masih berlangsung"');
    assert.equal(modal.style.display !== 'none', true, 'modal tidak boleh disembunyikan oleh Escape');
    assert.ok(modal.classList.contains('show'), 'state show modal tetap utuh');

    // Klik langsung backdrop (target === modal) juga ditahan.
    const clickEv = { type: 'click', target: modal, _prevented: false, _stopped: false,
        preventDefault() { this._prevented = true; }, stopPropagation() { this._stopped = true; } };
    modal.dispatch(clickEv);
    assert.ok(clickEv._prevented && clickEv._stopped, 'klik overlay wajib ditahan saat unggah berjalan');
    assert.equal(modal.style.display !== 'none', true);

    // Klik DI DALAM kartu modal (target ≠ modal) tidak boleh diblokir.
    const inner = fakeEl('button');
    inner.parentNode = modal;
    const innerEv = { type: 'click', target: inner, _prevented: false, _stopped: false,
        preventDefault() { this._prevented = true; }, stopPropagation() { this._stopped = true; } };
    modal.dispatch(innerEv);
    assert.ok(!innerEv._stopped, 'klik konten modal tidak boleh ikut tertahan');
});

test('S37 (perilaku): lock overlay menahan forceClose (classList.remove + display=none) selama unggah, lepas setelahnya', () => {
    const modal = fakeEl('div', { id: 'uploadModal' });
    const env = loadSystemAppsSandbox({ uploadModal: modal });
    modal.style.display = 'flex';

    env.win.__uploadInProgress = true;
    env.sandbox.lockUploadOverlay(modal);
    assert.equal(modal.getAttribute('aria-busy'), 'true', 'modal wajib aria-busy selama unggahan');

    // Simulasi forceClose admin-core: klik sintetis lalu hapus .show / display.
    modal.dispatch({ type: 'click', target: modal });
    modal.classList.remove('show');
    assert.ok(modal.classList.contains('show'),
        'forceClose menghapus class show — harus ditahan selama __uploadInProgress');
    modal.style.display = 'none';
    assert.notEqual(modal.style.display, 'none',
        'forceClose menyembunyikan via style.display — harus ditahan selama __uploadInProgress');

    // Unggahan selesai → kunci dilepas, penutupan normal berjalan lagi.
    env.win.__uploadInProgress = false;
    env.sandbox.unlockUploadOverlay(modal);
    assert.equal(modal.getAttribute('aria-busy'), null, 'aria-busy dilepas setelah unggahan');
    modal.classList.remove('show');
    assert.ok(!modal.classList.contains('show'), 'setelah unlock, close normal bekerja');
    modal.style.display = 'none';
    assert.equal(modal.style.display, 'none');
});

test('S37 (perilaku): pill progres "Mengunggah..." muncul saat upload & sembunyi setelahnya', () => {
    const env = loadSystemAppsSandbox({});
    const pill = env.sandbox.showUploadProgressPill();
    assert.ok(pill, 'pill harus dibuat');
    assert.equal(pill.id, 'uploadProgressPill');
    assert.equal(pill.getAttribute('role'), 'status', 'pill wajib role=status (live region pasif)');
    assert.ok(/Mengunggah/.test(pill.textContent || '') || /Mengunggah/.test(pill.innerHTML || ''),
        'pill memuat teks "Mengunggah..."');
    assert.equal(pill.style.display, 'flex');
    assert.ok(env.body.children.includes(pill), 'pill dirender di document.body');

    env.sandbox.hideUploadProgressPill();
    assert.equal(pill.style.display, 'none', 'pill disembunyikan setelah unggahan selesai');
});

// ===========================================================================
// S39 — dirty tracking per kartu Pengaturan Umum (8 section)
// ===========================================================================

const EXPECTED_CARDS = [
    ['smtp-save', 'saas-card-smtp', 'saveSmtpSettingsBtn'],
    ['turnstile-save', 'saas-card-turnstile', 'saveTurnstileSettingsBtn'],
    ['cleanup-save', 'saas-card-cleanup', 'saveCleanupSettingsBtn'],
    ['default-pkg-save', 'saas-card-default-pkg', 'saveDefaultPkgSettingsBtn'],
    ['versions-save', 'saas-card-versions', 'saveVersionsSettingsBtn'],
    ['footer-save', 'saas-card-footer', 'saveFooterSettingsBtn'],
    ['seo-save', 'saas-card-seo', 'saveSeoSettingsBtn'],
    ['monetization-save', 'saas-card-monetization', 'saveMonetizationSettingsBtn']
];

test('S39 (statik): settings-general.js mendefinisikan pemetaan 8 kartu simpan + guard beforeunload', () => {
    assert.match(GENERAL_SRC, /SAAS_SAVE_CARDS\s*=/, 'pemetaan kartu harus eksplisit di settings-general.js');
    for (const [action, cardId, btnId] of EXPECTED_CARDS) {
        const row = new RegExp(`\\{[^}]*'${action}'[^}]*\\}`).exec(GENERAL_SRC);
        assert.ok(row, `kartu aksi ${action} harus terdaftar`);
        assert.match(row[0], new RegExp(cardId.replace(/-/g, '\\-')), `${row[0]} memuat ${cardId}`);
        assert.match(row[0], new RegExp(btnId.replace(/-/g, '\\-')), `${row[0]} memuat ${btnId}`);
        // Markup kartu & tombolnya benar-benar ada di halaman.
        assert.match(SETTINGS, new RegExp(`data-collapse-id="${cardId.replace('saas-card-', '')}"`),
            `kartu ${cardId} ada di settings.html`);
        assert.match(SETTINGS, new RegExp(`data-action="${action}"`), `tombol ${action} ada di settings.html`);
    }
    assert.match(GENERAL_SRC, /beforeunload/, 'harus memasang guard beforeunload');
    // Batch 10 (S47): pembersihan dirty pindah ke jalur sukses saveSaasSection
    // (admin.js memanggil clearSaasCardDirtyByCardId(cardId)) — observer toast
    // satu-slot DIHAPUS karena bisa membersihkan kartu yang salah.
    assert.doesNotMatch(GENERAL_SRC, /MutationObserver|handleSaasToastForDirty|SAAS_PENDING_SAVE/,
        'observasi toast satu-slot dihapus (kontrak S47 Batch 10)');
});

function loadGeneralSandbox() {
    const winListeners = {};
    const docListeners = {};
    const byId = {};
    const created = [];
    const win = { __settingsReady: {}, location: { hash: '' }, localStorageKeys: {} };
    win.window = win;
    try { Object.defineProperty(win, 'localStorage', { value: { getItem: () => null, setItem() {} } }); } catch (_) { win.localStorage = { getItem: () => null, setItem() {} }; }
    win.Actions = { register() {}, has: () => true };
    win.addEventListener = (type, fn) => { (winListeners[type] = winListeners[type] || []).push(fn); };
    win.removeEventListener = () => {};

    function MutationObserverMock(cb) { this.cb = cb; }
    MutationObserverMock.prototype.observe = function () { MutationObserverMock.observed = this; };
    MutationObserverMock.prototype.disconnect = function () {};

    const docMock = {
        readyState: 'complete',
        body: fakeEl('body'),
        head: fakeEl('head'),
        getElementById: (id) => byId[id] || null,
        createElement: (tag) => { const n = fakeEl(tag); created.push(n); return n; },
        createTextNode: (text) => ({ nodeType: 3, textContent: String(text) }),
        querySelector: () => null,
        querySelectorAll: () => [],
        addEventListener: (type, fn) => { (docListeners[type] = docListeners[type] || []).push(fn); },
        removeEventListener() {}
    };

    const sandbox = {
        window: win, document: docMock, MutationObserver: MutationObserverMock,
        localStorage: win.localStorage, console,
        setTimeout: (fn) => 0, clearTimeout() {}
    };
    sandbox.globalThis = sandbox;
    vm.createContext(sandbox);
    vm.runInContext(GENERAL_SRC, sandbox, { filename: 'settings-general.js' });
    return { sandbox, win, byId, created, winListeners, docListeners };
}

function buildCardEnv(env, [action, cardId, btnId]) {
    const card = fakeEl('section', { id: cardId });
    const title = fakeEl('h3'); title.className = 'saas-collapse-title';
    card._titleEl = title;
    const btn = fakeEl('button', { id: btnId, 'data-action': action });
    btn.innerHTML = '<svg></svg> <span class="saas-save-text">Simpan</span>';
    // Paritas markup nyata: label tombol dibungkus span.saas-save-text.
    const label = fakeEl('span'); label.className = 'saas-save-text'; label.textContent = 'Simpan';
    btn.appendChild(label);
    env.byId[cardId] = card;
    env.byId[btnId] = btn;
    return { card, title, btn, label };
}

test('S39 (perilaku): input/change menandai kartu kotor — titik header + label tombol "•"', () => {
    const env = loadGeneralSandbox();
    const [smtp, seo] = [EXPECTED_CARDS[0], EXPECTED_CARDS[6]];
    const A = buildCardEnv(env, smtp);
    const B = buildCardEnv(env, seo);

    env.sandbox.wireSaasDirtyTracking();
    assert.equal(env.sandbox.anySaasDirty(), false, 'awal: tidak ada kartu kotor');

    // User mengetik di kartu SMTP → hanya kartu itu yang kotor.
    const ev = { target: A.btn, _prevented: false, preventDefault() {}, stopPropagation() {} };
    (A.card._listeners.input || []).forEach((fn) => fn(ev));
    assert.equal(env.sandbox.anySaasDirty(), true);
    assert.equal(env.sandbox.saasDirtyCount(), 1, 'hanya satu kartu kotor');
    assert.ok(A.title.children.some((c) => String(c.className).includes('saas-dirty-dot')),
        'titik indikator muncul di header kartu');
    assert.ok(/•/.test(A.label.textContent), 'label tombol simpan bertanda •');
    assert.equal(B.label.textContent.includes('•'), false, 'kartu lain tidak ikut kotor');

    // Batch 10 (S47): simpan sukses → saveSaasSection (admin.js) memanggil
    // pembersih langsung dengan cardId kartu yang disimpan (tanpa observasi
    // toast — mekanisme lama bisa membersihkan kartu yang salah).
    env.sandbox.clearSaasCardDirtyByCardId(A.card.id);
    assert.equal(env.sandbox.anySaasDirty(), false, 'simpan sukses membersihkan kartu yang disimpan');
    assert.equal(A.title.children.some((c) => String(c.className).includes('saas-dirty-dot')), false,
        'titik indikator hilang setelah tersimpan');
    assert.equal(/•/.test(A.label.textContent), false, 'tanda • hilang setelah tersimpan');

    // Pembersihan HANYA menyentuh kartu yang disimpan: kartu B tetap kotor.
    (B.card._listeners.input || []).forEach((fn) => fn(ev));
    assert.equal(env.sandbox.anySaasDirty(), true, 'pra-kondisi: kartu B kotor');
    env.sandbox.clearSaasCardDirtyByCardId('saas-card-smtp');
    assert.equal(env.sandbox.anySaasDirty(), true, 'membersihkan kartu lain TIDAK menyentuh kartu B');
    assert.equal(/•/.test(B.label.textContent), true, 'indikator kartu B utuh');
});

test('S39 (perilaku): beforeunload guard terpasang dan mencegah navigasi hanya bila ada kartu kotor', () => {
    const env = loadGeneralSandbox();
    const card = buildCardEnv(env, EXPECTED_CARDS[3]);

    env.sandbox.wireSaasDirtyTracking();
    const handlers = env.winListeners.beforeunload || [];
    assert.equal(handlers.length, 1, 'listener beforeunload terpasang sekali');
    env.sandbox.wireSaasDirtyTracking(); // idempoten
    assert.equal((env.winListeners.beforeunload || []).length, 1);

    // Bersih → boleh pergi tanpa dialog.
    let ev = { _prevented: false, preventDefault() { this._prevented = true; } };
    handlers.forEach((fn) => fn(ev));
    assert.equal(ev._prevented, false);

    // Kotor → konfirmasi native browser.
    (card.card._listeners.change || []).forEach((fn) => fn({ target: card.btn, preventDefault() {}, stopPropagation() {} }));
    ev = { _prevented: false, returnValue: undefined, preventDefault() { this._prevented = true; } };
    handlers.forEach((fn) => fn(ev));
    assert.equal(ev._prevented, true, 'beforeunload harus preventDefault bila ada kartu kotor');
    assert.ok(ev.returnValue !== undefined, 'returnValue diset untuk dialog native Chrome');
});

// ===========================================================================
// R29 (sisi vouchers) — onclick inline di settings-vouchers.js habis
// ===========================================================================

test('R29 (statik): settings-vouchers.js bebas onclick inline; retry & paginasi memakai data-action terdaftar', () => {
    const n = (VOUCHERS_SRC.match(/onclick=/g) || []).length;
    assert.equal(n, 0, `masih ada ${n} onclick inline di settings-vouchers.js`);

    assert.match(VOUCHERS_SRC, /data-action="voucher-retry-load"/,
        'tombol Coba Lagi daftar voucher wajib data-action');
    assert.match(VOUCHERS_SRC, /data-action="voucher-page"/,
        'tombol paginasi voucher wajib data-action');
    for (const name of ['voucher-retry-load', 'voucher-page']) {
        assert.match(VOUCHERS_SRC, new RegExp(`Actions\\.register\\(\\s*'${name}'`),
            `${name} wajib didaftarkan di modul yang sama (pola Batch 8)`);
        assert.match(VOUCHERS_SRC, new RegExp(`data-page="`), 'halaman tujuan dibawa data-page');
    }
    // Normalisasi angka seperti kontrak B8-3.
    const reg = VOUCHERS_SRC.match(/Actions\.register\(\s*'voucher-page'[\s\S]*?\}\);/);
    assert.match(reg[0], /parseInt\([\s\S]*?,\s*10\)/, 'handler paginasi menormalisasi data-page dengan parseInt(x, 10)');
});

test('R29 (perilaku): handler voucher-retry-load/voucher-page memuat halaman dari data-page', () => {
    const registered = {};
    const calls = [];
    const win = { Actions: { register(n, fn) { registered[n] = fn; }, has: () => true } };
    const sandbox = {
        window: win, console,
        loadVouchers: (p) => calls.push(p)
    };
    sandbox.globalThis = sandbox;
    vm.createContext(sandbox);
    // Eksekusi hanya blok registrasi Actions milik file ini.
    const snippet = VOUCHERS_SRC.match(/if \(window\.Actions && typeof window\.Actions\.register === 'function'\) \{[\s\S]*?\n\}/);
    assert.ok(snippet, 'blok registrasi Actions ditemukan');
    vm.runInContext(snippet[0], sandbox, { filename: 'settings-vouchers.js#registrations' });

    assert.equal(typeof registered['voucher-page'], 'function', 'voucher-page terdaftar');
    assert.equal(typeof registered['voucher-retry-load'], 'function', 'voucher-retry-load terdaftar');

    const el = fakeEl('button', { 'data-action': 'voucher-page', 'data-page': '4' });
    registered['voucher-page'](el);
    assert.deepEqual(calls, [4], 'paginasi meneruskan angka halaman hasil parseInt');
    el.attrs['data-page'] = 'bukan-angka';
    registered['voucher-retry-load'](el);
    assert.deepEqual(calls, [4, 1], 'nilai rusak jatuh ke halaman 1 (retry dari awal yang aman)');
});

// ===========================================================================
// R32 — label form settings.html punya asosiasi programatik
// ===========================================================================

const LABEL_RE = /<label\b([^>]*)>([\s\S]*?)<\/label>/g;

function auditLabels(html) {
    const problems = [];
    let associated = 0;
    let total = 0;
    let m;
    while ((m = LABEL_RE.exec(html))) {
        total += 1;
        const attrs = m[1];
        const inner = m[2];
        const forMatch = /\bfor="([^"]+)"/.exec(attrs);
        if (forMatch) {
            if (new RegExp(`id="${forMatch[1]}"`).test(html)) associated += 1;
            else problems.push(`label for="${forMatch[1]}" tapi id tidak ditemukan`);
            continue;
        }
        // Wrapping eksplisit: kontrol di dalam label.
        if (/<(?:input|select|textarea)\b/.test(inner)) { associated += 1; continue; }
        problems.push(`label tanpa for & tanpa wrapping: "<label${attrs}>${inner.trim().slice(0, 60)}…"`);
    }
    return { total, associated, problems };
}

test('R32: SETIAP label settings.html terasosiasi (for→id ada, atau wrapping eksplisit)', () => {
    const { total, associated, problems } = auditLabels(SETTINGS);
    assert.deepEqual(problems, [],
        `masih ada ${problems.length} label tanpa asosiasi programatik:\n${problems.join('\n')}`);
    // Baseline pasangan (ronde 3: 41 dari 81) tidak boleh turun.
    assert.ok(total >= 80, `total label = ${total}, tidak boleh menyusut drastis (baseline ±81)`);
    assert.ok(associated >= 41, `pasangan terasosiasi = ${associated}, baseline 41`);
    assert.ok(associated > 60, `pasangan terasosiasi = ${associated}, hasil sweep R32 harus melompat signifikan`);
});

// ===========================================================================
// Kontrak lintas-agen — satu sumber #toastContainer (nav.html)
// ===========================================================================

test('KONTRAK: #toastContainer DIHAPUS dari settings.html (nav.html satu-satunya sumber)', () => {
    assert.doesNotMatch(SETTINGS, /id="toastContainer"/,
        'container toast duplikat di settings.html harus dihapus — nav.html yang menyediakan');
});
