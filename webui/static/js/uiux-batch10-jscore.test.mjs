/* Suite Batch 10 — jscore (milik agen batch-10-jscore).
 * Referensi temuan: review_uiux_webui.md bagian "5.7 RE-REVIEW RONDE 4"
 * (ID: T18 sisi dashboard, S47, S51, R47, R50 sisi dashboard; R49 milik
 * agen pengawasan-nav — tidak di sini).
 *
 * Run with:  node --test static/js/uiux-batch10-jscore.test.mjs   (from webui/)
 *
 * Latar belakang & dampak bisnis:
 *   - T18: tombol submit edit instansi dashboard memakai endpoint gradien lama
 *     #a855f7/#6366f1 + teks putih = 3.96/4.47:1 (< 4.5 WCAG AA). Migrasi ke
 *     token --grad-btn-violet-start/end (kontrak lintas-agen: token
 *     DIDEFINISIKAN di theme.css :root oleh agen tokens-guard — file ini hanya
 *     memakai var(), TIDAK mendefinisikan ulang).
 *   - S47: pembersihan dirty kartu Pengaturan Umum pindah dari observer toast
 *     satu-slot SAAS_PENDING_SAVE (bisa membersihkan kartu yang salah) ke
 *     jalur sukses saveSaasSection di admin.js — identitas kartu (cardId)
 *     diteruskan pemanggil dan clearSaasCardDirtyByCardId dipanggil HANYA di
 *     cabang sukses.
 *   - S51: ±11 handler inline tersisa di render-path users/modal dinamis
 *     admin.js migrasi ke data-action + Actions.register (pola Batch 8);
 *     argumen lewat data-user-id/data-name/data-page, escaping escapeHtml
 *     tetap untuk konteks atribut HTML (jsEscape tak lagi diperlukan).
 *   - R47: body error non-JSON (proxy 502 HTML) membuat resp.json() reject
 *     SyntaxError mentah ke toast — dibungkus catch → fallback pesan generik.
 *   - R50: "Export XML" → "Ekspor XML" (konsistensi Bahasa Indonesia).
 *
 * Kontrak antar-agen yang DIKONSUMSI:
 *   - admin-core.js: Actions.register + delegasi dokumen [data-action]
 *     (fn(el, ev)), showToast, escapeHtml.
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

const ADMIN_JS_SRC = read('static/js/admin.js');
const GENERAL_SRC = read('static/js/settings-general.js');
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

/** Ekstrak statement Actions.register('name', ...) dari sebuah sumber. */
function extractRegistration(src, name, label) {
    const re = new RegExp("Actions\\.register\\(\\s*['\"]" + name + "['\"][\\s\\S]*?\\}\\);");
    const m = src.match(re);
    assert.ok(m, `[${label}] registrasi '${name}' harus ditemukan di sumber`);
    return m[0];
}

const flush = () => new Promise((r) => setTimeout(r, 20));

// ===========================================================================
// T18 — gradien submit edit instansi gagal AA → token --grad-btn-violet-*
// ===========================================================================

test('T18 (statik): dashboard.html bebas endpoint gradien terlarang #a855f7/#6366f1 sebagai background', () => {
    // Kontrak: endpoint dilarang sebagai BACKGROUND/gradien tombol. Value
    // color-picker panel ujian (#6366F1 sebagai pilihan warna user) tidak
    // termasuk — itu bukan permukaan teks putih.
    assert.doesNotMatch(DASHBOARD, /linear-gradient[^;"']*(?:a855f7|6366f1)/i,
        'gradien dengan endpoint #a855f7/#6366f1 dilarang (3.96/4.47:1 < 4.5 AA)');
    assert.doesNotMatch(DASHBOARD, /background(?:-image)?\s*:[^;"']*(?:a855f7|6366f1)/i,
        'endpoint #a855f7/#6366f1 dilarang sebagai nilai background');
});

test('T18 (statik): submit edit instansi memakai token var(--grad-btn-violet-start/end) tanpa definisi lokal', () => {
    const btn = /<button[^>]*id="btnSaveEditInstansi"[^>]*>/.exec(DASHBOARD);
    assert.ok(btn, 'tombol btnSaveEditInstansi harus ada');
    assert.match(btn[0], /var\(--grad-btn-violet-start\)/,
        'gradien wajib via token --grad-btn-violet-start (didefinisikan theme.css oleh agen tokens-guard)');
    assert.match(btn[0], /var\(--grad-btn-violet-end\)/,
        'gradien wajib via token --grad-btn-violet-end');
    // Kontrak lintas-agen: file ini TIDAK mendefinisikan token (hanya memakai).
    assert.doesNotMatch(DASHBOARD, /--grad-btn-violet-start\s*:/,
        'token tidak boleh didefinisikan lokal — satu-satunya sumber di theme.css :root');
});

// ===========================================================================
// R50 — "Export XML" → "Ekspor XML"
// ===========================================================================

test('R50 (statik): dashboard.html memakai label "Ekspor XML", bukan "Export XML"', () => {
    assert.match(DASHBOARD, /Ekspor XML/, 'label ekspor XML harus berbahasa Indonesia');
    assert.doesNotMatch(DASHBOARD, /Export XML/, 'label Inggris "Export XML" tidak boleh tersisa');
});

// ===========================================================================
// S47 — pembersihan dirty kartu pindah ke jalur sukses saveSaasSection
// ===========================================================================

test('S47 (statik): mekanisme SAAS_PENDING_SAVE + observer toast DIHAPUS dari settings-general.js', () => {
    for (const banned of ['SAAS_PENDING_SAVE', 'handleSaasToastForDirty', 'wireSaasSaveToastObserver']) {
        assert.doesNotMatch(GENERAL_SRC, new RegExp(banned),
            `${banned} harus dihapus — pembersihan kini di jalur sukses saveSaasSection`);
    }
    assert.match(GENERAL_SRC, /clearSaasCardDirtyByCardId/,
        'fungsi pembersih per-kartu tetap ada (dipanggil admin.js)');
});

test('S47 (statik): saveSaasSection menerima cardId & membersihkan dirty HANYA di cabang sukses', () => {
    const fn = extractFunction(ADMIN_JS_SRC, 'saveSaasSection');
    assert.ok(fn, 'saveSaasSection ada di admin.js');
    assert.match(fn, /clearSaasCardDirtyByCardId/, 'cabang sukses wajib membersihkan dirty per kartu');

    const idxSuccess = fn.indexOf("res.success");
    const idxClear = fn.indexOf('clearSaasCardDirtyByCardId');
    assert.ok(idxSuccess !== -1 && idxClear > idxSuccess,
        'pembersihan harus berada SETELAH cek res.success (jalur sukses saja)');

    // Kedelapan pemanggil meneruskan identitas kartunya.
    const callers = [
        ['saveSmtpSettings', 'saas-card-smtp'],
        ['saveTurnstileSettings', 'saas-card-turnstile'],
        ['saveCleanupSettings', 'saas-card-cleanup'],
        ['saveDefaultPkgSettings', 'saas-card-default-pkg'],
        ['saveVersionsSettings', 'saas-card-versions'],
        ['saveFooterSettings', 'saas-card-footer'],
        ['saveSeoSettings', 'saas-card-seo'],
        ['saveMonetizationSettings', 'saas-card-monetization']
    ];
    for (const [name, cardId] of callers) {
        const src = extractFunction(ADMIN_JS_SRC, name);
        assert.ok(src, `${name} ada`);
        assert.match(src, new RegExp("['\"]" + cardId + "['\"]"),
            `${name} wajib meneruskan cardId ${cardId} ke saveSaasSection`);
    }
});

// --- perilaku S47 ---

function makeSaasSaveSandbox(opts) {
    const byId = {};
    const sandbox = {
        document: {
            getElementById: (id) => byId[id] || null,
            createElement: () => ({ className: '', setAttribute() {}, remove() {} })
        },
        console
    };
    sandbox.toasts = [];
    sandbox.showToast = (msg, type) => { sandbox.toasts.push({ msg, type }); };
    sandbox.apiFetch = () => Promise.resolve({ json: () => Promise.resolve(opts.response) });
    sandbox.loadSaasSettings = () => {};
    return { sandbox, byId };
}

test('S47 (perilaku): simpan sukses membersihkan dirty KARTU YANG DISIMPAN saja; gagal membiarkannya kotor', async () => {
    // Muat fungsi dirty tracking ASLI dari settings-general.js + saveSaasSection/
    // saveFooterSettings ASLI dari admin.js dalam satu sandbox.
    const env = makeSaasSaveSandbox({
        response: { success: true, message: 'Footer disimpan' }
    });
    const dirtyFns = ['markSaasCardDirty', 'renderSaasDirtyState', 'setSaasHeaderDot',
        'anySaasDirty', 'saasDirtyCount', 'clearSaasCardDirtyByCardId', 'saasCardMeta']
        .map((n) => extractFunction(GENERAL_SRC, n)).join('\n');
    const cardsSrc = GENERAL_SRC.match(/var SAAS_SAVE_CARDS = \[[\s\S]*?\];/)[0] + '\nvar SAAS_DIRTY = {};';
    const saveFns = [extractFunction(ADMIN_JS_SRC, 'saveSaasSection'),
        extractFunction(ADMIN_JS_SRC, 'saveFooterSettings')].join('\n');

    const footerBtn = {
        id: 'saveFooterSettingsBtn', disabled: false,
        innerHTML: '<svg></svg> <span class="saas-save-text">Simpan</span>',
        textContent: '', attrs: {},
        setAttribute(n, v) { this.attrs[n] = v; },
        removeAttribute(n) { delete this.attrs[n]; },
        getAttribute(n) { return Object.prototype.hasOwnProperty.call(this.attrs, n) ? this.attrs[n] : null; },
        querySelector(sel) {
            if (sel !== '.saas-save-text') return null;
            if (!this._label) {
                this._label = { className: 'saas-save-text', textContent: 'Simpan' };
            }
            return this._label;
        }
    };
    const titleEl = { className: 'saas-collapse-title', children: [],
        appendChild(c) { this.children.push(c); },
        removeChild(c) { const i = this.children.indexOf(c); if (i !== -1) this.children.splice(i, 1); } };
    env.byId['saveFooterSettingsBtn'] = footerBtn;
    env.byId['saas-card-footer'] = {
        id: 'saas-card-footer', querySelector: (sel) => (sel === '.saas-collapse-title' ? titleEl : null)
    };
    const field = (v) => ({ value: v });
    env.byId['footerTextInput'] = field('Teks footer');
    env.byId['footerTaglineInput'] = field('Tagline');

    vm.createContext(env.sandbox);
    vm.runInContext(dirtyFns + '\n' + cardsSrc + '\n' + saveFns, env.sandbox,
        { filename: 'batch10-s47' });

    // Kartu Footer ditandai kotor persis seperti listener input/change asli.
    vm.runInContext("markSaasCardDirty('saas-card-footer');", env.sandbox);
    assert.equal(vm.runInContext('anySaasDirty()', env.sandbox), true, 'pra-kondisi: kartu footer kotor');

    vm.runInContext('saveFooterSettings();', env.sandbox);
    await flush();

    assert.equal(vm.runInContext('anySaasDirty()', env.sandbox), false,
        'simpan sukses KARTU INI membersihkan dirty kartu itu (tanpa observer toast)');
    assert.deepEqual(env.sandbox.toasts, [{ msg: 'Footer disimpan', type: 'success' }]);
});

test('S47 (perilaku): simpan GAGAL tidak membersihkan dirty kartu mana pun', async () => {
    const env = makeSaasSaveSandbox({
        response: { success: false, message: 'Gagal menyimpan' }
    });
    const dirtyFns = ['markSaasCardDirty', 'renderSaasDirtyState', 'setSaasHeaderDot',
        'anySaasDirty', 'saasDirtyCount', 'clearSaasCardDirtyByCardId', 'saasCardMeta']
        .map((n) => extractFunction(GENERAL_SRC, n)).join('\n');
    const cardsSrc = GENERAL_SRC.match(/var SAAS_SAVE_CARDS = \[[\s\S]*?\];/)[0] + '\nvar SAAS_DIRTY = {};';
    const saveFns = [extractFunction(ADMIN_JS_SRC, 'saveSaasSection'),
        extractFunction(ADMIN_JS_SRC, 'saveSmtpSettings')].join('\n');

    const smtpBtn = {
        id: 'saveSmtpSettingsBtn', disabled: false,
        innerHTML: '<svg></svg> <span class="saas-save-text">Simpan</span>',
        textContent: '', attrs: {},
        setAttribute(n, v) { this.attrs[n] = v; },
        removeAttribute(n) { delete this.attrs[n]; },
        getAttribute(n) { return Object.prototype.hasOwnProperty.call(this.attrs, n) ? this.attrs[n] : null; },
        querySelector(sel) {
            if (sel !== '.saas-save-text') return null;
            if (!this._label) this._label = { className: 'saas-save-text', textContent: 'Simpan' };
            return this._label;
        }
    };
    const titleEl = { className: 'saas-collapse-title', children: [],
        appendChild(c) { this.children.push(c); },
        removeChild(c) { const i = this.children.indexOf(c); if (i !== -1) this.children.splice(i, 1); } };
    const fld = (v) => ({ value: v });
    env.byId['emailEnabledInput'] = { checked: false };
    env.byId['emailDomainWhitelistInput'] = fld('');
    env.byId['smtpHostInput'] = fld('smtp.sekolah.id');
    env.byId['smtpPortInput'] = fld('587');
    env.byId['smtpUserInput'] = fld('no-reply');
    env.byId['smtpPasswordInput'] = fld('rahasia');
    env.byId['smtpSenderNameInput'] = fld('EXAMVAN');
    env.byId['saveSmtpSettingsBtn'] = smtpBtn;
    env.byId['saas-card-smtp'] = {
        id: 'saas-card-smtp',
        querySelectorAll: () => [],
        querySelector: (sel) => (sel === '.saas-collapse-title' ? titleEl : null)
    };

    vm.createContext(env.sandbox);
    vm.runInContext(dirtyFns + '\n' + cardsSrc + '\n' + saveFns, env.sandbox,
        { filename: 'batch10-s47-fail' });

    vm.runInContext("markSaasCardDirty('saas-card-smtp');", env.sandbox);
    vm.runInContext('saveSmtpSettings();', env.sandbox);
    await flush();

    assert.equal(vm.runInContext('anySaasDirty()', env.sandbox), true,
        'request gagal → titik dirty TETAP (editan belum tentu hilang)');
    assert.equal(env.sandbox.toasts[0].type, 'error');
});

// ===========================================================================
// S51 — migrasi onclick render-path users/modal dinamis ke data-action
// ===========================================================================

const USER_ACTIONS = ['user-verify', 'user-deactivate-package', 'user-edit-open', 'user-delete'];

test('S51 (statik): string render daftar user & modal dinamis bebas onclick= ; aksi lewat data-action', () => {
    const usersList = extractFunction(ADMIN_JS_SRC, 'loadUsersList');
    assert.ok(usersList, 'loadUsersList ada');
    assert.doesNotMatch(usersList, /\sonclick=/, 'string render daftar user bebas onclick inline');
    for (const action of USER_ACTIONS) {
        assert.match(usersList, new RegExp(`data-action="${action}"`),
            `aksi user ${action} wajib data-action`);
    }
    // Argumen lewat data attribute, bukan interpolasi ke JS-atribut.
    assert.match(usersList, /data-user-id="/, 'identitas user dibawa data-user-id');
    assert.match(usersList, /data-name="/, 'nama user dibawa data-name');
    assert.match(usersList, /data-action="users-retry-load"/, 'Coba Lagi daftar user wajib data-action');

    for (const fname of ['setAllWeights', 'createEditUserModal']) {
        const src = extractFunction(ADMIN_JS_SRC, fname);
        assert.ok(src, `${fname} ada`);
        assert.doesNotMatch(src, /\sonclick=/, `${fname} bebas onclick inline`);
        assert.doesNotMatch(src, /\sonchange=/, `${fname} bebas onchange inline`);
    }
    // Escaping tetap konteks HTML (escapeHtml); jsEscape tak lagi dipakai untuk
    // atribut data-* milik tombol aksi baru.
    assert.doesNotMatch(usersList, /data-name="' \+ escapeHtml\(jsEscape\(/,
        'interpolasi username ke atribut data-* cukup escapeHtml (bukan JS-atribut)');
});

test('S51 (statik): seluruh handler baru terdaftar dengan normalisasi parseInt(...,10)', () => {
    for (const action of [...USER_ACTIONS, 'users-retry-load', 'bulk-weight-apply', 'modal-remove']) {
        const reg = extractRegistration(ADMIN_JS_SRC, action, 'admin.js');
        assert.ok(reg.length > 0);
    }
    for (const action of ['user-verify', 'user-deactivate-package', 'user-delete', 'user-edit-open', 'users-retry-load']) {
        const reg = extractRegistration(ADMIN_JS_SRC, action, 'admin.js');
        assert.match(reg, /parseInt\(/, `${action} menormalisasi id/halaman dengan parseInt`);
    }
    const retry = extractRegistration(ADMIN_JS_SRC, 'users-retry-load', 'admin.js');
    assert.match(retry, /data-page/, 'retry membawa halaman asal via data-page');
});

test('S51 (perilaku): handler user-delete/user-edit-open membaca data-user-id & data-name', () => {
    const registered = {};
    const win = { __adminRole: 'superadmin' };
    win.Actions = { register(n, fn) { registered[n] = fn; }, has: () => true };
    win.window = win;
    const calls = [];
    const sandbox = {
        window: win, console,
        Actions: win.Actions,
        deleteUser: (id, name) => calls.push(['deleteUser', id, name]),
        verifyUser: (id, name) => calls.push(['verifyUser', id, name]),
        deactivatePackage: (id, name) => calls.push(['deactivatePackage', id, name]),
        openEditUserModal: (id) => calls.push(['openEditUserModal', id])
    };
    sandbox.globalThis = sandbox;
    vm.createContext(sandbox);
    const regs = USER_ACTIONS.map((a) => extractRegistration(ADMIN_JS_SRC, a, 'admin.js')).join('\n');
    vm.runInContext(regs, sandbox, { filename: 'admin.js#registrations-s51' });

    const el = {
        attrs: { 'data-action': 'user-delete', 'data-user-id': '7', 'data-name': 'Budi & Co' },
        getAttribute(n) { return this.attrs[n] || null; }
    };
    registered['user-delete'](el);
    assert.deepEqual(calls, [['deleteUser', 7, 'Budi & Co']],
        'id dinormalisasi parseInt, nama dibawa utuh dari data-name');

    el.attrs['data-action'] = 'user-verify';
    registered['user-verify'](el);
    el.attrs['data-action'] = 'user-deactivate-package';
    registered['user-deactivate-package'](el);
    el.attrs['data-action'] = 'user-edit-open';
    delete el.attrs['data-name'];
    registered['user-edit-open'](el);
    assert.deepEqual(calls, [
        ['deleteUser', 7, 'Budi & Co'],
        ['verifyUser', 7, 'Budi & Co'],
        ['deactivatePackage', 7, 'Budi & Co'],
        ['openEditUserModal', 7]
    ]);
});

// ===========================================================================
// R47 — body error non-JSON tidak lagi bocor sebagai SyntaxError ke toast
// ===========================================================================

test('R47 (perilaku): resp.json() yang REJECT (body HTML proxy) → toast fallback generik', async () => {
    const container = { children: [], querySelectorAll: () => [] };
    const btn = { disabled: false, innerHTML: '<svg></svg> Ekspor Excel', attrs: {}, style: {},
        setAttribute(n, v) { this.attrs[n] = v; }, removeAttribute(n) { delete this.attrs[n]; },
        getAttribute(n) { return Object.prototype.hasOwnProperty.call(this.attrs, n) ? this.attrs[n] : null; } };
    const anchors = [];
    const sandbox = {
        document: {
            getElementById(id) { return id === 'filterExam' ? { value: '' } : (id === 'exportBtn' ? btn : null); },
            createElement(tag) {
                return { tagName: tag.toUpperCase(), style: {},
                    appendChild() {}, remove() {} , click() {}};
            },
            body: { appendChild(c) { anchors.push(c); }, removeChild() {} },
            querySelector: () => null
        },
        console
    };
    sandbox.showToast = (msg, type) => { sandbox.toasts.push({ msg, type }); };
    sandbox.apiFetch = () => Promise.resolve({
        ok: false,
        status: 502,
        json: () => Promise.reject(new SyntaxError('Unexpected token \'<\' ...'))
    });
    sandbox.toasts = [];
    vm.createContext(sandbox);
    vm.runInContext(extractFunction(ADMIN_JS_SRC, 'exportSubmissions'), sandbox,
        { filename: 'admin.js#r47' });

    sandbox.exportSubmissions();
    await flush();
    await flush();

    assert.deepEqual(sandbox.toasts, [{ msg: 'Gagal mengekspor data', type: 'error' }],
        'SyntaxError mentah TIDAK boleh tampil di toast — fallback pesan generik');
    assert.equal(btn.disabled, false, 'tombol tetap dipulihkan');
});
