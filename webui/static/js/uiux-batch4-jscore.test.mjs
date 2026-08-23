/* Contract + behavior tests untuk Batch 4 (review_uiux_webui.md).
 * Referensi temuan: R2 (helper skeleton MATI), R6 (deleteExam reload).
 *
 * Run with:  node --test static/js/uiux-batch4-jscore.test.mjs   (from webui/)
 *
 * KEPUTUSAN R2 (FINAL): showSkeleton/showDashboardSkeletons DIHAPUS, bukan
 * diaktifkan. Kedua helper tidak PERNAH dipanggil mana pun sehingga hanya
 * jadi dead code; aktivasi skeleton loading ditunda karena butuh desain
 * loading state per halaman (dashboard & pengawas blank-flash saat render
 * awal tetap ada tapi bukan scope batch ini). Bila kelak ingin diaktifkan,
 * pulihkan implementasinya dari git history (commit penghapusan R2).
 * CSS .skeleton* di admin-base.css sengaja TIDAK disentuh (bukan milik
 * batch ini dan masih dipakai elemen lain yang memuat class tersebut).
 *
 * Pola sama dengan uiux-batch1.test.mjs / uiux-batch3-t8-polling.test.mjs:
 * fungsi diekstrak dari file ASLI yang dikirim ke browser dan dijalankan di
 * Node vm dengan stub DOM minimal; harness loadAdminCore untuk regresi fungsi
 * publik mengikuti pola admin-core.test.mjs.
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

const adminJs = () => read('static/js/admin.js');

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

function flush() { return new Promise((r) => setTimeout(r, 20)); }

// ---------------------------------------------------------------------------
// R2 — helper skeleton mati dihapus (keputusan: HAPUS, bukan aktifkan)
// ---------------------------------------------------------------------------

test('R2: admin-core.js tidak lagi memuat deklarasi showSkeleton/showDashboardSkeletons', () => {
    assert.equal(extractFunction(ADMIN_CORE_SRC, 'showSkeleton'), null,
        'showSkeleton harus sudah dihapus (dead code — lihat header untuk keputusan)');
    assert.equal(extractFunction(ADMIN_CORE_SRC, 'showDashboardSkeletons'), null,
        'showDashboardSkeletons harus sudah dihapus');
});

test('R2: tidak ada pemanggil/deklarasi showSkeleton/showDashboardSkeletons di seluruh static/js/', () => {
    const dir = path.join(__dirname);
    // Pola memerlukan tanda kurung agar komentar dokumentasi keputusan R2
    // (yang menyebut nama helper tanpa tanda kurung) tidak dihitung pelanggaran.
    const pattern = /(showSkeleton|showDashboardSkeletons)\s*\(/;
    const offenders = fs.readdirSync(dir)
        .filter((f) => f.endsWith('.js'))
        .filter((f) => pattern.test(fs.readFileSync(path.join(dir, f), 'utf8')));
    assert.deepEqual(offenders, [],
        'tidak boleh ada pemanggilan/definisi helper skeleton di file JS manapun: ' + offenders.join(', '));
});

test('R2: fallback skeleton-reload di refreshDashboardStats juga hilang (tak mungkin terpicu lagi)', () => {
    // Dengan helper skeleton dihapus, tidak ada yang bisa merender
    // .skeleton-card sehingga cabang fallback location.reload() menjadi
    // dead code yang mustahil terpicu — ikut dibersihkan.
    const fn = extractFunction(ADMIN_CORE_SRC, 'refreshDashboardStats') || ADMIN_CORE_SRC;
    assert.ok(!fn.includes('.skeleton-card'),
        'querySelector .skeleton-card (fallback reload) tidak boleh tersisa');
});

// ---------------------------------------------------------------------------
// R6 — deleteExam tanpa location.reload(): hapus row langsung + update counter
// ---------------------------------------------------------------------------

test('R6 statik: deleteExam tidak memanggil location.reload() dan menghapus row dari DOM', () => {
    const fn = extractFunction(adminJs(), 'deleteExam');
    assert.ok(fn, 'fungsi deleteExam harus ada di admin.js');
    assert.ok(!fn.includes('location.reload'),
        'location.reload() harus hilang dari jalur hapus ujian (posisi scroll & pagination tidak boleh hilang)');
    assert.match(fn, /getElementById\(`exam-row-\$\{examId\}`\)/,
        'row ujian dilokasi lewat id exam-row-{examId}');
    assert.match(fn, /\.remove\(\)/, 'row dihapus langsung dari DOM (.remove())');
});

test('R6 statik: deleteExam tetap konfirmasi konsekuensi dulu lalu toast sukses & update counter', () => {
    const fn = extractFunction(adminJs(), 'deleteExam');
    const idxConfirm = fn.indexOf('showConfirm');
    const idxFetch = fn.indexOf('apiFetch');
    assert.ok(idxConfirm !== -1 && idxFetch !== -1 && idxConfirm < idxFetch,
        'showConfirm (konsekuensi hapus permanen) tetap dipanggil SEBELUM apiFetch');
    assert.match(fn, /File PDF juga akan dihapus permanen/,
        'pesan konsekuensi hapus permanen dipertahankan');
    assert.match(fn, /showToast\([^)]*'success'/, 'toast sukses existing tetap ada');
    assert.match(fn, /refreshDashboardStats/,
        'counter kartu statistik dashboard diperbarui (tiruan jalur refreshDashboardStats)');
});

test('R6 perilaku: sukses hapus → row di-remove, TANPA reload, toast sukses, counter di-refresh', async () => {
    const fn = extractFunction(adminJs(), 'deleteExam');
    let removed = false;
    const row = { remove() { removed = true; }, style: {} };
    const toasts = [];
    let statsRefreshed = 0;
    const sandbox = {
        document: {
            getElementById(id) {
                if (id === 'exam-row-42') return row;
                return null; // #statsGrid dsb tidak ada — refreshDashboardStats adalah stub spy
            }
        },
        showConfirm(msg, detail) {
            assert.match(msg, /Hapus ujian "UTS Fisika"\?/);
            assert.match(detail, /File PDF juga akan dihapus permanen/);
            return Promise.resolve(true);
        },
        apiFetch(url, opts) {
            assert.equal(url, '/admin/api/exams/42/delete');
            assert.equal(opts.method, 'POST');
            return Promise.resolve({ json: () => Promise.resolve({ success: true, message: 'Ujian dihapus' }) });
        },
        showToast(msg, kind) { toasts.push({ msg, kind }); },
        refreshDashboardStats() { statsRefreshed += 1; }
    };
    vm.createContext(sandbox);
    vm.runInContext(fn, sandbox, { filename: 'admin.js#deleteExam' });
    sandbox.deleteExam(42, 'UTS Fisika');
    await flush();

    assert.equal(removed, true, 'row dihapus langsung dari DOM');
    assert.ok(!sandbox.location || !sandbox.location.reloaded,
        'TIDAK boleh ada location.reload setelah hapus sukses');
    assert.ok(toasts.some((t) => t.kind === 'success' && /Ujian dihapus/.test(t.msg)),
        'toast sukses tampil dengan pesan server');
    assert.equal(statsRefreshed, 1, 'counter kartu statistik di-refresh sekali');
});

test('R6 perilaku: gagal hapus → row TETAP ada, toast error, tanpa refresh counter', async () => {
    const fn = extractFunction(adminJs(), 'deleteExam');
    let removed = false;
    const row = { remove() { removed = true; }, style: {} };
    const toasts = [];
    let statsRefreshed = 0;
    const sandbox = {
        document: { getElementById: (id) => (id === 'exam-row-42' ? row : null) },
        showConfirm() { return Promise.resolve(true); },
        apiFetch() {
            return Promise.resolve({
                json: () => Promise.resolve({ success: false, message: 'R2 tidak dikonfigurasi' })
            });
        },
        showApiErrorToast(res, fallback) { toasts.push({ msg: res.message || fallback, kind: 'error' }); },
        showToast(msg, kind) { toasts.push({ msg, kind }); },
        refreshDashboardStats() { statsRefreshed += 1; }
    };
    vm.createContext(sandbox);
    vm.runInContext(fn, sandbox, { filename: 'admin.js#deleteExam-fail' });
    sandbox.deleteExam(42, 'UTS Fisika');
    await flush();

    assert.equal(removed, false, 'gagal hapus → row tidak disentuh');
    assert.ok(toasts.some((t) => t.kind === 'error'), 'toast error via showApiErrorToast');
    assert.equal(statsRefreshed, 0, 'counter tidak direfresh saat gagal');
});

// ---------------------------------------------------------------------------
// Regresi — fungsi publik admin-core.js tetap eksis pasca penghapusan R2
// ---------------------------------------------------------------------------

// Harness ringan mengikuti pola admin-core.test.mjs: file ASLI admin-core.js
// dieksekusi di Node vm dengan mock DOM/globals minimum.
function loadAdminCore() {
    const listeners = new Map();
    function fakeElement() {
        return {
            className: '',
            classList: { add() {}, remove() {}, contains() { return false; }, toggle() {} },
            setAttribute() {},
            getAttribute() { return null; },
            addEventListener() {},
            removeEventListener() {},
            innerHTML: '',
            textContent: '',
            querySelector() { return null; },
            querySelectorAll() { return []; },
            appendChild() {},
            insertBefore() {},
            removeChild() {},
            remove() {},
            contains() { return false; },
            closest() { return null; },
            focus() {},
            select() {},
            scrollIntoView() {},
            style: {},
            offsetHeight: 0
        };
    }
    const documentMock = {
        readyState: 'complete',
        activeElement: null,
        documentElement: fakeElement(),
        body: fakeElement(),
        addEventListener(type, fn) {
            if (!listeners.has(type)) listeners.set(type, []);
            listeners.get(type).push(fn);
        },
        removeEventListener() {},
        dispatchEvent(ev) {
            (listeners.get(ev.type) || []).slice().forEach((fn) => fn(ev));
            return true;
        },
        getElementById() { return null; },
        querySelector(sel) {
            return sel === 'meta[name="csrf-token"]' ? { getAttribute: () => 'test-csrf-token' } : fakeElement();
        },
        querySelectorAll() { return []; },
        createElement() { return fakeElement(); },
        contains() { return true; }
    };
    function MutationObserverMock() {}
    MutationObserverMock.prototype.observe = function () {};
    MutationObserverMock.prototype.disconnect = function () {};

    const sandbox = {
        window: { fetch: async () => ({ ok: true, status: 200, text: () => Promise.resolve('') }) },
        document: documentMock,
        CustomEvent: function (t) { this.type = t; },
        MutationObserver: MutationObserverMock,
        MouseEvent: function (type) { this.type = type; },
        getComputedStyle: () => ({ display: 'block' }),
        navigator: {},
        console,
        setTimeout: () => 0,
        clearTimeout() {},
        setInterval() { return 0; },
        clearInterval() {},
        location: { href: '' }
    };
    sandbox.globalThis = sandbox;
    vm.createContext(sandbox);
    vm.runInContext(ADMIN_CORE_SRC, sandbox, { filename: 'admin-core.js' });
    return sandbox;
}

test('Regresi: fungsi publik admin-core.js tetap eksis & helper skeleton benar-benar hilang dari runtime', () => {
    const sandbox = loadAdminCore();
    for (const name of ['escapeHtml', 'jsEscape', 'apiFetch', 'showToast', 'showConfirm',
        'initLiveSearch', 'initMenuToggle', 'setFieldError']) {
        assert.equal(typeof sandbox[name], 'function',
            `fungsi publik ${name} harus tetap ada di runtime admin-core.js`);
    }
    assert.equal(typeof sandbox.showSkeleton, 'undefined',
        'showSkeleton tidak boleh terdefinisi lagi di runtime');
    assert.equal(typeof sandbox.showDashboardSkeletons, 'undefined',
        'showDashboardSkeletons tidak boleh terdefinisi lagi di runtime');
});
