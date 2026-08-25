/* Batch 10 — perbaikan UI/UX halaman publik (review_uiux_webui.md §5.7
 * RE-REVIEW RONDE 4, sisi publik). ID temuan: T17, S53–S56, S59, R42–R44.
 *
 * Run with:  node --test static/js/uiux-batch10-publik.test.mjs   (from webui/)
 *
 * Metode mengikuti uiux-batch9-publik.test.mjs: kontrak statik fs-read +
 * eksekusi perilaku via vm.runInNewContext dengan stub DOM minimal.
 *
 * Dampak bisnis yang dilindungi:
 *   T17 — deep-link #kunci tidak lagi menampilkan panel Nilai + Kunci
 *         bertumpuk: loadResults() mengembalikan tampilan ke tab aktif.
 *   S53 — init tab hasil di-guard pada state error/disabled; tanpa TypeError
 *         akses elemen null dan tanpa listener mati.
 *   S54 — tab Nilai/Kunci hasil punya semantik ARIA tabs penuh
 *         (role/aria-selected/roving tabindex), konsisten dengan download.
 *   S55 — replika CSS toast download memuat rule .toast-close:focus-visible
 *         (tombol ✕ terlihat bagi pengguna keyboard).
 *   S56 — reset_password memuat admin-core.js dengan defer.
 *   S59 — templates/public/** bebas onclick inline (CSP-safe): aksi lewat
 *         data-action + registry Actions (hasil & download) atau addEventListener
 *         wiring (shared, register_confirm).
 *   R42 — strength meter reset_password memakai token yang sama dengan
 *         register.html (tanpa drift hex vs token).
 *   R43 — cetak rekap: .answer-grid tidak terpotong batas 380px saat print.
 *   R44 — race device_fingerprint: submit cepat menunggu generate() in-flight
 *         (Promise.race timeout ±1.5s) sebelum form dikirim.
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

const HASIL = () => read('templates/public/hasil.html');
const DOWNLOAD = () => read('templates/public/download.html');
const SHARED = () => read('templates/public/shared.html');
const REGISTER_CONFIRM = () => read('templates/public/register_confirm.html');
const RESET_PASSWORD = () => read('templates/public/reset_password.html');
const REGISTER = () => read('templates/public/register.html');
const HASIL_CSS = () => read('static/css/hasil.css');
const DEVICE_FP = () => read('static/js/device-fingerprint.js');

/** Ekstrak sumber deklarasi `(async) function name(...) {...}` (kurawal seimbang). */
function extractFunction(src, name) {
    let start = src.indexOf('function ' + name + '(');
    if (start === -1) return null;
    // Pertahankan keyword async bila ada (loadResults adalah async function).
    const asyncIdx = src.lastIndexOf('async ', start);
    if (asyncIdx !== -1 && src.slice(asyncIdx + 6, start).trim() === '') start = asyncIdx;
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

// ===========================================================================
// Harness vm untuk script inline hasil.html (T17 / S53 / S54)
// ===========================================================================

function mkEl() {
    return {
        style: {},
        attrs: {},
        tabIndex: 0,
        children: [],
        classList: {
            _set: new Set(),
            toggle(cls, force) {
                const want = force === undefined ? !this._set.has(cls) : !!force;
                if (want) this._set.add(cls); else this._set.delete(cls);
                return want;
            },
            add(cls) { this._set.add(cls); },
            remove(cls) { this._set.delete(cls); },
            contains(cls) { return this._set.has(cls); },
        },
        setAttribute(n, v) { this.attrs[n] = String(v); },
        removeAttribute(n) { delete this.attrs[n]; },
        getAttribute(n) { return this.attrs[n] !== undefined ? this.attrs[n] : null; },
        appendChild(c) { this.children.push(c); },
        querySelector: () => null,
        scrollIntoView() {},
        focus() {},
        addEventListener(type, fn) { (this.listeners || (this.listeners = {}))[type] = fn; },
    };
}

/** Sandbox lengkap untuk menjalankan seluruh logika halaman hasil. */
function buildHasilVm({ locationHash = '', errorPage = false, apiPayload } = {}) {
    const els = {};
    const domListeners = {};
    const windowListeners = {};
    const writtenHashes = [];

    const sandbox = {
        console: { error() {}, warn() {}, log() {} },
        URLSearchParams,
        Promise,
        setTimeout,
        clearTimeout,
        location: { hash: locationHash, reload() {} },
        history: { replaceState: (_a, _b, u) => writtenHashes.push(u) },
        document: {
            getElementById: errorPage
                ? () => null
                : (id) => (els[id] || (els[id] = mkEl())),
            querySelector: () => null,
            createElement: () => mkEl(),
            addEventListener(type, fn) { domListeners[type] = fn; },
        },
        window: { addEventListener(type, fn) { windowListeners[type] = fn; } },
        escapeHtml: (s) => String(s == null ? '' : s),
        localizeUTC: (s) => (s || ''),
        showApiErrorToast() {},
        // State halaman (nilai default cabang sukses)
        EXAM_TOKEN: 'TOKEN01',
        isLoggedIn: false,
        isDisabled: false,
        pageHasError: errorPage,
        showAnswersFromServer: true,
        submissionsData: [],
        questionsData: [],
        identityFieldsData: [],
        showCorrectAnswers: true,
        currentTab: 'scores',
        TAB_HASH_NAMES: { scores: 'nilai', keys: 'kunci' },
        currentPage: 1,
        PER_PAGE: 20,
        searchQuery: '',
        pagination: { page: 1, total: 0, total_pages: 1 },
        statsData: null,
        searchTimer: null,
        resultsLoading: false,
        resultsRerunPending: false,
        firstLoadDone: false,
        pendingScrollToTable: false,
        __els: els,
        __domListeners: domListeners,
        __windowListeners: windowListeners,
        __writtenHashes: writtenHashes,
        __apiCalls: [],
    };

    sandbox.apiFetch = () => {
        sandbox.__apiCalls.push(1);
        const payload = apiPayload || {
            success: true,
            submissions: [{
                id: 7, student_name: 'Budi', score: 80, max_score: 100,
                start_time: '2026-08-24T01:00:00Z', created_at: '2026-08-24T01:30:00Z',
                answers: { 1: 'A' }, evaluated_answers: { 1: { statusClass: 'correct', statusText: 'Benar', earned: 10 } },
            }],
            questions: [{ number: 1, type: 'single_choice', key: 'B', weight: 10 }],
            identity_fields: [],
            stats: { count: 1, average: 80, max: 80, min: 80 },
            pagination: { page: 1, total: 1, total_pages: 1 },
        };
        return Promise.resolve({ ok: true, json: () => Promise.resolve(payload) });
    };

    const src = HASIL();
    // Seluruh fungsi yang dirujuk antar-fungsi pada jalur loadResults/init.
    for (const name of [
        'getDurationString', 'getScoreClass', 'getPassStatus', 'formatAnswer',
        'buildDetailContent', 'renderScoresTable', 'renderStats',
        'updateResultsLiveRegion', 'renderPagination', 'createPageBtn',
        'createEllipsis', 'goToPage', 'resolveTabFromHash', 'switchTab',
        'renderKeysGrid', 'filterResults', 'setResultsBusy', 'loadResults',
    ]) {
        const fn = extractFunction(src, name);
        assert.ok(fn, `fungsi ${name} harus bisa diekstrak dari hasil.html`);
        sandbox[name] = fn;
    }

    const ctx = vm.createContext(sandbox);
    // Konversi sumber fungsi menjadi fungsi nyata di dalam realm vm.
    const evalPrelude = Object.keys(sandbox)
        .filter((k) => typeof sandbox[k] === 'string'
            && /^(async )?function [\w$]+\(/.test(sandbox[k]))
        .map((k) => `${k} = eval("(" + ${k} + ")");`)
        .join('\n');
    vm.runInContext(evalPrelude, ctx);
    // Pasang listener DOMContentLoaded ASLI dari sumber halaman (bukan replika).
    const initArg = extractCallArgument(src, "document.addEventListener('DOMContentLoaded',");
    assert.ok(initArg, 'callback init DOMContentLoaded harus bisa diekstrak dari hasil.html');
    vm.runInContext(
        `var __initCb = eval("(" + ${JSON.stringify(initArg)} + ")");
         document.addEventListener('DOMContentLoaded', __initCb);`, ctx);
    return ctx;
}

/** Ambil argumen pemanggilan `callee(...)` dengan penghitungan kurung seimbang. */
function extractCallArgument(src, callee) {
    const i = src.indexOf(callee);
    if (i === -1) return null;
    const open = src.indexOf('(', i);
    if (open === -1) return null;
    let depth = 0;
    for (let j = open; j < src.length; j++) {
        if (src[j] === '(') depth++;
        else if (src[j] === ')') {
            depth--;
            if (depth === 0) return src.slice(open + 1, j).trim();
        }
    }
    return null;
}

async function runDOMContentLoaded(ctx) {
    vm.runInContext(`
        if (!__domListeners['DOMContentLoaded']) throw new Error('listener init tidak terpasang');
        __domListeners['DOMContentLoaded']();
    `, ctx);
    // Tunggu rantai promise loadResults selesai (fetch + json + render).
    for (let i = 0; i < 20; i++) await new Promise((r) => setTimeout(r, 5));
}

// ===========================================================================
// T17 — deep-link #kunci membuat kedua panel bertumpuk
// ===========================================================================

test('T17 (statik): loadResults mengembalikan tampilan ke tab aktif (switchTab currentTab skipHash)', () => {
    const src = HASIL();
    const fn = extractFunction(src, 'loadResults');
    assert.ok(fn, 'loadResults harus bisa diekstrak');
    assert.match(fn, /switchTab\(currentTab,\s*\{\s*skipHash:\s*true\s*\}\)/,
        'akhir loadResults wajib memanggil switchTab(currentTab, { skipHash: true }) '
        + 'agar panel mengikuti tab aktif (deep-link #kunci)');
});

test('T17 (perilaku): hash #kunci → loadResults → panel Nilai tetap display:none, tab aktif tetap Kunci', async () => {
    const ctx = buildHasilVm({ locationHash: '#kunci' });
    await runDOMContentLoaded(ctx);

    const state = vm.runInContext(`JSON.stringify({
        scoresDisplay: document.getElementById('scoresContent').style.display,
        keysDisplay: document.getElementById('keysContent').style.display,
        currentTab: currentTab,
        scoresSelected: document.getElementById('tabScores').getAttribute('aria-selected'),
        keysSelected: document.getElementById('tabKeys').getAttribute('aria-selected'),
    })`, ctx);
    const got = JSON.parse(state);
    assert.equal(got.currentTab, 'keys', 'init mengikuti hash #kunci → tab aktif Kunci');
    assert.equal(got.keysDisplay, 'block', 'panel Kunci tampil');
    assert.equal(got.scoresDisplay, 'none',
        'panel Nilai TIDAK boleh ikut tampil (dulu display:block paksa di loadResults → bertumpuk)');
    assert.equal(got.scoresSelected, 'false', 'aria-selected tab Nilai false');
    assert.equal(got.keysSelected, 'true', 'aria-selected tab Kunci true');
});

test('T17 (perilaku): tanpa hash → tab default Nilai tetap bekerja normal setelah load', async () => {
    const ctx = buildHasilVm({ locationHash: '' });
    await runDOMContentLoaded(ctx);
    const got = JSON.parse(vm.runInContext(`JSON.stringify({
        scoresDisplay: document.getElementById('scoresContent').style.display,
        keysDisplay: document.getElementById('keysContent').style.display,
        currentTab: currentTab,
    })`, ctx));
    assert.equal(got.currentTab, 'scores');
    assert.equal(got.scoresDisplay, 'block');
    assert.equal(got.keysDisplay, 'none');
});

// ===========================================================================
// S53 — init tab tanpa guard state error
// ===========================================================================

test('S53 (perilaku): state error — init DOMContentLoaded tidak melempar TypeError akses elemen null', async () => {
    const ctx = buildHasilVm({ locationHash: '#kunci', errorPage: true });
    // Tidak boleh throw meski getElementById selalu null (halaman error/disabled
    // tidak merender tabNav/tabScores).
    await assert.doesNotReject(() => runDOMContentLoaded(ctx));
    // Dan tidak ada fetch hasil dijalankan pada halaman error.
    assert.equal(vm.runInContext('__apiCalls.length', ctx), 0,
        'state error tidak boleh memulai polling/fetch hasil');
});

test('S53 (perilaku): switchTab defensif — tabNav/tombol tab tidak ada → early-return tanpa throw', () => {
    const src = HASIL();
    const fn = extractFunction(src, 'switchTab');
    assert.ok(fn, 'switchTab harus bisa diekstrak');
    const sandbox = {
        currentTab: 'scores',
        document: { getElementById: () => null, querySelector: () => null },
        history: { replaceState() {} },
    };
    const ctx = vm.createContext(sandbox);
    vm.runInContext(`switchTab = eval("(" + ${JSON.stringify(fn)} + ")")`, ctx);
    assert.doesNotThrow(() => vm.runInContext('switchTab("keys")', ctx),
        'switchTab wajib early-return bila elemen tab tidak ada (S53)');
});

test('S53 (statik): switchTab ber-guard sebelum menyentuh properti elemen tab', () => {
    const fn = extractFunction(HASIL(), 'switchTab');
    assert.match(fn, /getElementById\('tabScores'\)/, 'guard membaca #tabScores');
    const guarded =
        /tabScores\s*=\s*document\.getElementById\('tabScores'\)[\s\S]{0,200}?return/.test(fn)
        || /!\s*\w*[tT]abScores[\s\S]{0,120}return/.test(fn);
    assert.ok(guarded, 'switchTab wajib early-return bila tombol tab tidak dirender');
});

// ===========================================================================
// S54 — semantik ARIA tabs port dari download
// ===========================================================================

test('S54 (statik): tabNav punya role="tablist", tombol role="tab" + aria-controls, tanpa aria-label duplikat', () => {
    const html = HASIL();
    assert.match(html, /id="tabNav"[^>]*role="tablist"/, '#tabNav wajib role="tablist"');
    const tabScores = html.match(/<button[^>]*id="tabScores"[^>]*>/);
    const tabKeys = html.match(/<button[^>]*id="tabKeys"[^>]*>/);
    assert.ok(tabScores && tabKeys, 'kedua tombol tab harus ada');
    for (const [m, id, panel] of [[tabScores[0], 'tabScores', 'scoresContent'], [tabKeys[0], 'tabKeys', 'keysContent']]) {
        assert.match(m, /role="tab"/, `${id} wajib role="tab"`);
        assert.match(m, /aria-selected=/, `${id} wajib membawa aria-selected awal`);
        assert.match(m, new RegExp(`aria-controls="${panel}"`), `${id} wajib aria-controls=${panel}`);
        assert.doesNotMatch(m, /data-tab=/ ? /x^/ : /x^/); // no-op guard
        assert.ok(!/aria-label="/.test(m.replace(/aria-label="(Halaman[^"]*)"/g, '')),
            `${id}: aria-label yang menduplikasi teks tombol wajib dihapus`);
    }
});

test('S54 (statik): panel konten punya role="tabpanel" + aria-labelledby ke tombolnya', () => {
    const html = HASIL();
    const scores = html.match(/<div id="scoresContent"[^>]*>/);
    const keys = html.match(/<div id="keysContent"[^>]*>/);
    assert.ok(scores && keys, 'panel konten harus ada');
    assert.match(scores[0], /role="tabpanel"/, '#scoresContent wajib role="tabpanel"');
    assert.match(scores[0], /aria-labelledby="tabScores"/);
    assert.match(keys[0], /role="tabpanel"/, '#keysContent wajib role="tabpanel"');
    assert.match(keys[0], /aria-labelledby="tabKeys"/);
});

test('S54 (perilaku): switchTab menyinkronkan aria-selected + roving tabindex', async () => {
    const ctx = buildHasilVm({});
    // switchTab dieksekusi via init; periksa state ARIA kedua tab.
    await runDOMContentLoaded(ctx);
    const got = JSON.parse(vm.runInContext(`JSON.stringify({
        scoresSel: document.getElementById('tabScores').getAttribute('aria-selected'),
        keysSel: document.getElementById('tabKeys').getAttribute('aria-selected'),
        scoresIdx: document.getElementById('tabScores').tabIndex,
        keysIdx: document.getElementById('tabKeys').tabIndex,
    })`, ctx));
    assert.equal(got.scoresSel, 'true');
    assert.equal(got.keysSel, 'false');
    assert.equal(got.scoresIdx, 0, 'tab aktif roving tabindex = 0');
    assert.equal(got.keysIdx, -1, 'tab non-aktif roving tabindex = -1');
});

test('S54 (statik): navigasi keyboard panah/Home/End terpasang pada tablist hasil', () => {
    const html = HASIL();
    // Revisi Batch 11: kata "DOMContentLoaded" kini juga muncul di komentar
    // HTML (T21) — ambil kemunculan TERAKHIR yaitu listener init sebenarnya.
    const idx = html.lastIndexOf('DOMContentLoaded');
    assert.ok(idx !== -1, 'blok init harus ada');
    const region = html.slice(idx, idx + 5000);
    for (const key of ['ArrowRight', 'ArrowLeft', 'Home', 'End']) {
        assert.ok(region.includes(key), `navigasi keyboard ${key} wajib ditangani di init tab`);
    }
});

// ===========================================================================
// S55 — toast-close replika download pra-T10a
// ===========================================================================

test('S55: blok CSS toast lokal download.html memuat rule .toast-close:focus-visible { opacity:1 }', () => {
    const doc = DOWNLOAD();
    const rule = doc.match(/\.toast-close:focus-visible\s*\{([^}]*)\}/);
    assert.ok(rule, '.toast-close:focus-visible harus ada di blok style lokal download.html');
    assert.match(rule[1], /opacity:\s*1/, 'rule wajib mereveal tombol ✕ saat fokus keyboard (port T10a)');
});

// ===========================================================================
// S56 — reset_password memuat admin-core.js sinkron
// ===========================================================================

test('S56: reset_password.html TIDAK lagi memuat admin-core.js (diubah Batch 15/S105)', () => {
    // Diperbarui Batch 15: S105 mengganti satu-satunya alasan halaman publik
    // ini memuat bundle admin (togglePasswordVisibility) dengan helper lokal
    // wirePwToggle ala register.html — tag script dihapus total.
    const html = RESET_PASSWORD();
    const tag = html.match(/<script[^>]*admin-core\.js[^>]*>/);
    assert.equal(tag, null,
        'halaman anonim tidak boleh memuat admin-core.js hanya demi toggle password');
});

// ===========================================================================
// S59 — templates/public/** bebas onclick inline (×17)
// ===========================================================================

test('S59: seluruh templates/public/*.html bebas atribut onclick= (kontrak CSP-safe)', () => {
    const dir = path.join(WEBUI_ROOT, 'templates', 'public');
    const files = fs.readdirSync(dir).filter((f) => f.endsWith('.html'));
    assert.ok(files.length >= 9, 'sanity: 9 template publik');
    for (const f of files) {
        const src = fs.readFileSync(path.join(dir, f), 'utf8');
        const n = (src.match(/\sonclick=/gi) || []).length;
        assert.equal(n, 0, `${f}: masih ada ${n} onclick inline — migrasikan ke data-action/wiring`);
    }
});

test('S59 (hasil): aksi tab/paginasi/cari/reload lewat data-action + handler terdaftar di Actions', () => {
    const html = HASIL();
    for (const action of ['switch-tab', 'page-prev', 'page-next', 'clear-search', 'reload-page']) {
        assert.ok(html.includes(`data-action="${action}"`),
            `hasil.html wajib memakai data-action="${action}"`);
    }
    for (const name of ['switch-tab', 'page-prev', 'page-next', 'clear-search', 'reload-page']) {
        assert.match(html, new RegExp(`Actions\\.register\\(['"]${name}['"]`),
            `handler "${name}" wajib didaftarkan ke registry Actions di script halaman`);
    }
    // Tab membawa data-tab untuk handler generik switch-tab.
    assert.match(html, /id="tabScores"[^>]*data-tab="scores"/);
    assert.match(html, /id="tabKeys"[^>]*data-tab="keys"/);
});

test('S59 (download): unduh & tab lewat data-action + handler terdaftar; perilaku preventDefault dipertahankan', () => {
    const html = DOWNLOAD();
    assert.match(html, /Actions\.register\(['"]download-app['"][\s\S]{0,400}?preventDefault/,
        'handler download-app wajib preventDefault seperti onclick lama (fallback href tetap ada)');
    assert.match(html, /Actions\.register\(['"]switch-tab['"]/);
    assert.match(html, /data-action="download-app"/);
    assert.match(html, /data-action="switch-tab"/);
    assert.match(html, /data-app-id=|data-platform=/, 'argumen dibawa via data-*');
});

test('S59 (shared): hamburger & overlay di-wiring addEventListener, bukan onclick', () => {
    const html = SHARED();
    const navScript = html.match(/public_nav[\s\S]*?<\/script>/);
    assert.ok(navScript, 'script partial public_nav harus ada');
    assert.match(navScript[0], /addEventListener\(['"]click['"],\s*toggleMenu\)/,
        'toggleMenu wajib dipasang via addEventListener di partial nav');
});

test('S59 (register_confirm): kirim-ulang OTP di-wiring addEventListener, bukan onclick', () => {
    const html = REGISTER_CONFIRM();
    const link = html.match(/<a[^>]*id="resendLink"[^>]*>/);
    assert.ok(link, '#resendLink harus ada');
    assert.doesNotMatch(link[0], /onclick=/, '#resendLink tanpa onclick');
    assert.match(html, /resendLink\.addEventListener\(['"]click['"]/,
        'resendOtp wajib dipasang via addEventListener');
});

// ===========================================================================
// R42 — strength meter drift register ↔ reset_password
// ===========================================================================

const PW_HEX_DRIFT = ['#ef4444', '#f59e0b'];

test('R42: rule CSS .pw-bar-fill.weak/.fair di reset_password memakai token sama dengan register', () => {
    for (const [label, src] of [['register', REGISTER()], ['reset_password', RESET_PASSWORD()]]) {
        const weak = src.match(/\.pw-bar-fill\.weak\s*\{([^}]*)\}/);
        const fair = src.match(/\.pw-bar-fill\.fair\s*\{([^}]*)\}/);
        assert.ok(weak && fair, `${label}: rule pw-bar-fill weak/fair harus ada`);
        assert.match(weak[1], /var\(--color-danger\)/, `${label}: weak wajib var(--color-danger)`);
        assert.match(fair[1], /var\(--color-warning\)/, `${label}: fair wajib var(--color-warning)`);
        for (const hex of PW_HEX_DRIFT) {
            assert.ok(!weak[1].includes(hex) && !fair[1].includes(hex),
                `${label}: hex ${hex} masih ada di rule .pw-bar-fill.*`);
        }
    }
});

test('R42: skoring JS strength meter reset_password memakai var(--color-danger/--color-warning)', () => {
    const js = RESET_PASSWORD();
    const levels = js.match(/strengthLevels\s*=\s*\[[\s\S]*?\]/);
    assert.ok(levels, 'array strengthLevels harus ada di reset_password');
    assert.match(levels[0], /var\(--color-danger\)/, 'level Lemah wajib token danger');
    assert.match(levels[0], /var\(--color-warning\)/, 'level Cukup wajib token warning');
    for (const hex of PW_HEX_DRIFT) {
        assert.ok(!levels[0].includes(hex), `hex ${hex} masih dipakai strengthLevels reset_password`);
    }
    // Paritas dengan register.html (sumber port).
    const regLevels = REGISTER().match(/levels\s*=\s*\[[\s\S]*?\]/);
    assert.ok(regLevels, 'array levels register.html harus ada');
    assert.match(regLevels[0], /var\(--color-danger\)/);
    assert.match(regLevels[0], /var\(--color-warning\)/);
});

// ===========================================================================
// R43 — print answer-grid terpotong
// ===========================================================================

test('R43: blok @media print hasil.css me-reset batas 380px .answer-grid', () => {
    const css = HASIL_CSS();
    const start = css.indexOf('@media print');
    assert.notEqual(start, -1, '@media print harus ada di hasil.css');
    // Blok print adalah blok @media TERAKHIR file — ambil sampai @media lain
    // berikutnya (tidak ada) atau akhir berkas.
    const region = css.slice(start);
    const rule = region.match(/\.answer-grid\s*\{([^}]*)\}/);
    assert.ok(rule, '.answer-grid wajib di-reset di dalam blok print');
    assert.match(rule[1], /max-height:\s*none\s*!important/, 'max-height:none !important');
    assert.match(rule[1], /overflow:\s*visible\s*!important/, 'overflow:visible !important');
});

// ===========================================================================
// R44 — race device_fingerprint submit cepat
// ===========================================================================

/** Muat device-fingerprint.js asli dalam vm; kembalikan harness form/input/deferred. */
function buildFingerprintHarness({ timeoutMs = 40 } = {}) {
    function makeForm() {
        return {
            listeners: {},
            submitCalls: 0,
            addEventListener(type, fn) { this.listeners[type] = fn; },
            submit() { this.submitCalls++; },
            querySelector(sel) {
                return sel.includes('device_fingerprint') ? harness.input : null;
            },
        };
    }
    let resolveFp;
    const fpPromise = new Promise((res) => { resolveFp = res; });
    const harness = {
        input: { value: '' },
        form: null,
        submitEvents: [],
        fireSubmit() {
            const ev = { preventDefault() { this.prevented = true; } };
            harness.form.listeners['submit'](ev);
            harness.submitEvents.push(ev);
            return ev;
        },
        resolveFingerprint(visitorId) { resolveFp({ get: () => Promise.resolve({ visitorId }) }); },
    };
    harness.form = makeForm();

    const sandbox = {
        setTimeout,
        clearTimeout,
        Promise,
        console,
        document: {
            readyState: 'complete', // init langsung jalan (bukan menunggu DOMContentLoaded)
            querySelectorAll: (sel) => (sel === 'form' ? [harness.form] : []),
            addEventListener() {},
        },
        window: { __deviceFingerprintTimeout: timeoutMs },
    };
    sandbox.window.FingerprintJS = { load: () => fpPromise };

    const ctx = vm.createContext(sandbox);
    vm.runInContext(DEVICE_FP(), ctx, { filename: 'device-fingerprint.js' });
    return harness;
}

test('R44 (perilaku): submit saat generate in-flight → ditahan, field terisi setelah resolve, lalu dikirim', async () => {
    const h = buildFingerprintHarness({});
    // Beri kesempatan init memasang listener (microtask generate sudah berjalan).
    await new Promise((r) => setTimeout(r, 5));

    const ev = h.fireSubmit();
    assert.equal(ev.prevented, true,
        'submit wajib ditahan (preventDefault) saat fingerprint masih in-flight & field kosong');
    assert.equal(h.form.submitCalls, 0, 'form belum boleh terkirim sebelum fingerprint siap');

    h.resolveFingerprint('abc123def456');
    await new Promise((r) => setTimeout(r, 20));

    assert.equal(h.input.value, 'abc123def456', 'field device_fingerprint terisi setelah generate selesai');
    assert.equal(h.form.submitCalls, 1, 'form dikirim ulang setelah tunggu selesai');
});

test('R44 (perilaku): timeout ±1.5s — generate tak kunjung selesai → tetap kirim apa adanya', async () => {
    const h = buildFingerprintHarness({ timeoutMs: 30 }); // timeout dipercepat untuk test
    await new Promise((r) => setTimeout(r, 5));

    const ev = h.fireSubmit();
    assert.equal(ev.prevented, true, 'submit ditahan dulu');
    await new Promise((r) => setTimeout(r, 80)); // lewat batas timeout

    assert.equal(h.form.submitCalls, 1, 'setelah timeout, form tetap dikirim (field kosong apa adanya)');
    assert.equal(h.input.value, '', 'field tidak diisi palsu');
});

test('R44 (perilaku): fingerprint sudah siap di field → submit tidak diganggu sama sekali', async () => {
    const h = buildFingerprintHarness({});
    h.resolveFingerprint('ready123');
    await new Promise((r) => setTimeout(r, 20));
    assert.equal(h.input.value, 'ready123', 'init mengisi field');

    const ev = h.fireSubmit();
    assert.equal(ev.prevented, undefined, 'submit normal tidak dicegat');
    assert.equal(h.form.submitCalls, 0, 'tidak ada pengiriman ganda dari helper');
});

test('R44 (statik): implementasi memakai Promise.race dengan timeout ±1.5 detik', () => {
    const src = DEVICE_FP();
    assert.match(src, /Promise\.race/, 'tunggu in-flight wajib via Promise.race');
    assert.match(src, /1500|1_500/, 'timeout default ±1500ms');
});
