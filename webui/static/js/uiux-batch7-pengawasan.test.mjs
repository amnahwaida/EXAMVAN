/* Contract + behavior tests untuk Batch 7 — halaman pengawasan
 * (milik agen batch-7-pengawasan).
 * Referensi temuan: review_uiux_webui.md — R28 lanjutan (migrasi onclick →
 * delegasi data-action) + S15 lanjutan (fase 2 design token).
 *
 * Run with:  node --test static/js/uiux-batch7-pengawasan.test.mjs   (from webui/)
 *
 * Latar belakang & dampak bisnis:
 *   - R28 (lanjutan): 41 handler onclick tersebar di tiga halaman pengawasan
 *     (pengawas_detail.html ±27, pengawas.html ±9, submissions.html ±5)
 *     masih memasang behavior lewat string inline — sulit di-audit, tumpang-
 *     tindih dengan listener global, dan tidak bisa di-fuzz CSP ketat.
 *     Migrasi ke atribut data-action + Actions.register() (admin-core.js)
 *     membuat semua aksi satu pintu dan terdaftar eksplisit.
 *   - S15 (lanjutan): chip/notice berulang masih hard-code hex/rgba literal
 *     (#34d399, rgba(99,102,241,x), dst.) padahal token --rgb-* / tone sudah
 *     tersedia di theme.css/admin-base.css → perubahan tema tidak merambat.
 *
 * Fungsi yang HIDUP di file milik agen lain (admin.js / pengawas-detail.js)
 * TIDAK dipindah — halaman hanya mendaftarkan wrapper tipis via
 * Actions.register yang meneruskan data-* ke fungsi aslinya (dicatat
 * sebagai follow-up untuk pemilik file).
 *
 * Pola sama dengan uiux-batch6-jscore.test.mjs: kontrak statik (fs read) +
 * perilaku via vm.runInNewContext mengeksekusi potongan script inline ASLI.
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

const SUBMISSIONS = read('templates/admin/submissions.html');
const PENGAWAS = read('templates/admin/pengawas.html');
const DETAIL = read('templates/admin/pengawas_detail.html');
// Batch 8: registrasi aksi submissions pindah ke modul pemilik fungsinya
// (admin.js) — tetap dihitung valid untuk pemetaan dua arah.
const ADMIN_JS_SRC = read('static/js/admin.js');

/**
 * Blok registrasi halaman. Penangkap menyertakan prefix komentar `//` agar
 * potongan yang dieksekusi vm.runInContext() tetap JS valid — penanda
 * "===== Batch 7 (R28) ..." memang hidup di dalam baris komentar.
 */
const REG_BLOCK_RE = /\/\/\s*===== Batch 7 \(R28\): delegasi data-action[\s\S]*?\/\/\s*===== end Batch 7 \(R28\) =====/;

/** Kumpulkan nama data-action="..." dari sumber template (markup statis + string render-JS). */
function collectDataActions(src) {
    const names = new Set();
    for (const m of src.matchAll(/data-action="([a-z0-9-]+)"/g)) names.add(m[1]);
    return [...names].sort();
}

/** Kumpulkan nama yang didaftarkan lewat Actions.register('nama', ...) di blok delegasi halaman. */
function collectRegistered(src) {
    const block = src.match(REG_BLOCK_RE);
    if (!block) return null;
    const names = new Set();
    for (const m of block[0].matchAll(/Actions\.register\('([a-z0-9-]+)'/g)) names.add(m[1]);
    return [...names].sort();
}

// ---------------------------------------------------------------------------
// R28 — onclick === 0 di ketiga halaman (guard keras)
// ---------------------------------------------------------------------------

test('R28/static: tidak ada lagi atribut onclick di pengawas_detail.html, pengawas.html, submissions.html', () => {
    for (const [name, html] of [['pengawas_detail.html', DETAIL], ['pengawas.html', PENGAWAS], ['submissions.html', SUBMISSIONS]]) {
        const n = (html.match(/\sonclick=/gi) || []).length;
        assert.equal(n, 0, `${name}: sisa ${n} onclick — semua aksi wajib migrasi ke data-action`);
    }
});

// ---------------------------------------------------------------------------
// R28 — mapping data-action ↔ Actions.register (per halaman, dua arah)
// ---------------------------------------------------------------------------

for (const [name, html] of [['pengawas_detail.html', DETAIL], ['pengawas.html', PENGAWAS], ['submissions.html', SUBMISSIONS]]) {
    test(`R28/static (${name}): setiap data-action punya handler Actions.register (inline halaman ∪ modul pemilik)`, () => {
        const used = collectDataActions(html);
        assert.ok(used.length > 0, `${name}: minimal satu data-action harus ada`);
        // Batch 8: submissions.html tidak lagi memuat blok registrasi —
        // handler-nya didaftarkan di admin.js (modul pemilik fungsinya),
        // dan modal-dismiss kanonik di admin-core.js. Keduanya valid.
        const registered = new Set(collectRegistered(html) || []);
        if (name === 'submissions.html') {
            for (const m of ADMIN_JS_SRC.matchAll(/Actions\.register\(\s*['"]([a-z0-9-]+)['"]/g)) {
                registered.add(m[1]);
            }
            registered.add('modal-dismiss'); // kanonik di admin-core.js (Batch 8)
        }
        assert.ok(registered.size > 0, `${name}: tidak ada sumber registrasi yang ditemukan`);
        for (const action of used) {
            assert.ok(registered.has(action),
                `${name}: data-action="${action}" dipakai di markup tapi tidak didaftarkan via Actions.register`);
        }
    });

    test(`R28/static (${name}): setiap Actions.register halaman dipakai minimal sekali di markup (tidak ada handler yatim)`, () => {
        const used = collectDataActions(html);
        // Batch 8: pemeriksaan yatim hanya untuk registrasi yang TINGGAL di
        // halaman; registrasi pindahan ke modul pemilik punya markup halaman
        // lain sehingga justru wajar "yatim" dari sudut pandang file ini.
        const registered = collectRegistered(html) || [];
        for (const action of registered) {
            assert.ok(used.includes(action),
                `${name}: Actions.register('${action}') terdaftar tapi tidak ada elemen pemakainya`);
        }
    });
}

test('R28/static: tombol Izinkan/Tolak antrean izin membawa data-mac & data-status (bukan interpolasi ke onclick)', () => {
    // buildApprovalRowHTML render-JS: mac harus lewat atribut, bukan disisipkan
    // ke ekspresi JS dalam onclick (pola aman setelah S3 voucher).
    assert.match(DETAIL, /data-action="set-approval" data-mac=/);
    assert.match(DETAIL, /data-status="approved"/);
    assert.match(DETAIL, /data-status="rejected"/);
    assert.doesNotMatch(DETAIL, /setApproval\\'\s*\+/,
        'interpolasi mac ke onclick setApproval tidak boleh tersisa');
});

test('R28/static: pagination render-JS memakai data-page (tetap <button type=button> hasil Batch 6)', () => {
    for (const [name, html] of [['pengawas_detail.html', DETAIL], ['pengawas.html', PENGAWAS]]) {
        assert.match(html, /class="pagination-page-num[^"]*" data-action="[a-z-]+" data-page=/,
            `${name}: tombol pagination wajib membawa data-page untuk delegasi`);
    }
});

// ---------------------------------------------------------------------------
// S15 — guard baseline design token (hard-code, fase 2)
// ---------------------------------------------------------------------------

const countRe = (src, re) => (src.match(re) || []).length;

test('S15/static (pengawas_detail.html): ≥40% hex & ≥30% rgba bermigrasi ke token', () => {
    const hexTotal = 59;   // baseline sebelum Batch 7 (diukur, hard-code)
    const rgbaTotal = 69;
    const hexLeft = countRe(DETAIL, /#[0-9a-fA-F]{3,8}\b/g);
    const rgbaLeft = countRe(DETAIL, /rgba\(\s*\d/g); // rgba( angka → literal, BUKAN var(--rgb-*)
    assert.ok(hexLeft <= hexTotal - Math.ceil(hexTotal * 0.4),
        `pengawas_detail: sisa hex ${hexLeft} melewati batas ${hexTotal - Math.ceil(hexTotal * 0.4)} (target ≥40% termigrasi dari ${hexTotal})`);
    assert.ok(rgbaLeft <= rgbaTotal - Math.ceil(rgbaTotal * 0.3),
        `pengawas_detail: sisa rgba literal ${rgbaLeft} melewati batas (target ≥30% termigrasi dari ${rgbaTotal})`);

    // Token kontrak benar-benar dipakai (positif), bukan sekadar menghapus literal.
    assert.match(DETAIL, /var\(--color-success-light\)/);
    assert.match(DETAIL, /var\(--color-warning-light\)/);
    assert.match(DETAIL, /var\(--color-danger-light\)/);
    assert.match(DETAIL, /rgba\(var\(--rgb-info\)/);
    assert.match(DETAIL, /rgba\(var\(--rgb-warning\)/);
    assert.match(DETAIL, /rgba\(var\(--rgb-success\)/);
    assert.match(DETAIL, /rgba\(var\(--rgb-danger\)/);
    assert.match(DETAIL, /var\(--glass-bg-strong\)/);
});

test('S15/static (pengawas.html): ≥35% hex & ≥25% rgba bermigrasi ke token', () => {
    const hexTotal = 24;
    const rgbaTotal = 30;
    const hexLeft = countRe(PENGAWAS, /#[0-9a-fA-F]{3,8}\b/g);
    const rgbaLeft = countRe(PENGAWAS, /rgba\(\s*\d/g);
    assert.ok(hexLeft <= hexTotal - Math.ceil(hexTotal * 0.35),
        `pengawas: sisa hex ${hexLeft} melewati batas (target ≥35% termigrasi dari ${hexTotal})`);
    assert.ok(rgbaLeft <= rgbaTotal - Math.ceil(rgbaTotal * 0.25),
        `pengawas: sisa rgba literal ${rgbaLeft} melewati batas (target ≥25% termigrasi dari ${rgbaTotal})`);

    assert.match(PENGAWAS, /var\(--color-success-light\)/);
    assert.match(PENGAWAS, /var\(--color-primary-light\)/);
    assert.match(PENGAWAS, /rgba\(var\(--rgb-success\)/);
    assert.match(PENGAWAS, /rgba\(var\(--rgb-warning\)/);
    assert.match(PENGAWAS, /rgba\(var\(--rgb-info\)/);
    assert.match(PENGAWAS, /rgba\(var\(--rgb-accent\)/);
    assert.match(PENGAWAS, /var\(--glass-bg-strong\)/);
});

test('S15/static (submissions.html): badge/token indigo & surface kaca memakai token', () => {
    assert.match(SUBMISSIONS, /rgba\(var\(--rgb-info\)/);
    assert.match(SUBMISSIONS, /var\(--glass-bg-strong\)/);
});

// ---------------------------------------------------------------------------
// Perilaku — vm harness (potongan script inline ASLI + stub DOM minimal)
// ---------------------------------------------------------------------------

/** Node DOM palsu secukupnya: atribut, classList tipis, closest untuk delegasi. */
function fakeEl(tag, attrs, parent) {
    const node = {
        tagName: String(tag || 'div').toUpperCase(),
        attrs: Object.assign({}, attrs),
        parentNode: parent || null,
        className: '',
        innerHTML: '',
        textContent: '',
        style: {},
        disabled: false
    };
    node.getAttribute = (n) => (Object.prototype.hasOwnProperty.call(node.attrs, n) ? node.attrs[n] : null);
    node.setAttribute = (n, v) => { node.attrs[n] = String(v); };
    node.hasAttribute = (n) => Object.prototype.hasOwnProperty.call(node.attrs, n);
    node.classList = {
        add(...cls) { node.className = (node.className + ' ' + cls.join(' ')).trim(); },
        remove() {},
        contains(c) { return node.className.split(/\s+/).indexOf(c) > -1; }
    };
    node.closest = function (sel) {
        let cur = node;
        while (cur) {
            if (sel === '[data-action]' && cur.attrs && cur.attrs['data-action']) return cur;
            cur = cur.parentNode;
        }
        return null;
    };
    return node;
}

/** Replika listener delegasi admin-core.js: closest('[data-action]') → registry lookup → fn(el, ev). */
function makeDispatcher(actions) {
    return function dispatch(el, ev) {
        ev = ev || { type: 'click', target: el };
        const target = ev.target && typeof ev.target.closest === 'function' ? ev.target.closest('[data-action]') : null;
        if (!target) return;
        const name = target.getAttribute('data-action');
        if (!name || !actions.has(name)) return;
        actions._registry[name](target, ev);
    };
}

/** Stub Actions sesuai kontrak admin-core.js (register/has/_registry). */
function stubActions() {
    return {
        _registry: {},
        register(name, fn) { this._registry[name] = fn; },
        has(name) { return Object.prototype.hasOwnProperty.call(this._registry, name); }
    };
}

/** Ekstrak deklarasi `function name(...) {...}` dari sumber template (hitung kurawal). */
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

/** Muat fungsi2 + blok registrasi Batch 7 pengawas_detail.html ke sandbox. */
function loadDetailPageActions(sandbox) {
    sandbox.Actions = stubActions();
    sandbox.escapeHtml = (s) => String(s)
        .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;').replace(/'/g, '&#39;');
    sandbox.jsEscape = (s) => String(s)
        .replace(/\\/g, '\\\\').replace(/'/g, "\\'")
        .replace(/\n/g, '\\n').replace(/\r/g, '\\r');
    // Batch 10 (S50): global halaman yang dibaca findApprovalStudentName.
    sandbox.approvalRowsCache = [];
    sandbox.console = console;
    // Batch 11 (R57): definisi lokal localizeUTC dihapus dari pengawas_detail
    // (pemakaian jatuh ke alias admin-core.js) — stub setara untuk sandbox.
    sandbox.localizeUTC = (s) => (s || '');

    const parts = [];
    for (const fname of ['esc', 'buildApprovalRowHTML', 'showConfirmApprovalModal', 'closeConfirmApprovalModal',
        // Batch 10 (S50): helper label identitas dipanggil dari dalam
        // showConfirmApprovalModal — ikut dimuat agar sandbox lengkap.
        'findApprovalStudentName', 'formatApprovalStudentLabel']) {
        const fn = extractFunction(DETAIL, fname);
        assert.ok(fn, `fungsi ${fname} harus tetap ada di pengawas_detail.html`);
        parts.push(fn);
    }
    const regBlock = DETAIL.match(REG_BLOCK_RE);
    assert.ok(regBlock, 'blok registrasi Batch 7 (R28) pengawas_detail.html ditemukan');
    parts.push(regBlock[0]);

    sandbox.globalThis = sandbox;
    vm.createContext(sandbox);
    vm.runInContext(parts.join('\n'), sandbox, { filename: 'pengawas_detail.html#batch7' });
    return sandbox;
}

function approvalRowButtonEl(mac, status) {
    // Render baris nyata lewat fungsi template lalu ambil tag tombolnya —
    // memastikan atribut data-* yang diuji persis seperti yang dikirim browser.
    const sandbox = loadDetailPageActions({});
    const rowHtml = sandbox.buildApprovalRowHTML({
        mac_address: mac,
        student_name: 'Budi',
        created_at: null
    }, 0);
    // Pilih tombol yang datanya cocok dengan `status` yang diminta (baris
    // selalu merender dua tombol: Izinkan lalu Tolak).
    const btnTag = [...rowHtml.matchAll(/<button[^>]*data-action="set-approval"[^>]*>/g)]
        .map((m) => m[0])
        .find((tag) => tag.indexOf('data-status="' + status + '"') > -1);
    assert.ok(btnTag, 'tombol set-approval dirender di baris antrean');
    const attr = (n) => {
        const m = btnTag.match(new RegExp(n + '="([^"]*)"'));
        return m ? m[1] : null;
    };
    return fakeEl('button', {
        'data-action': attr('data-action'),
        'data-mac': attr('data-mac'),
        'data-status': attr('data-status'),
        class: attr('class') || ''
    });
}

test('R28/vm: klik Izinkan (delegasi data-action=set-approval) memicu modal konfirmasi dengan mac benar', () => {
    const modal = fakeEl('div', { id: 'confirmApprovalModal' });
    const title = fakeEl('h3', { id: 'confirmApprovalTitle' });
    const text = fakeEl('p', { id: 'confirmApprovalText' });
    const btn = fakeEl('button', { id: 'confirmApprovalBtn' });
    const sb = loadDetailPageActions({
        document: { getElementById: (id) => ({ confirmApprovalModal: modal, confirmApprovalTitle: title, confirmApprovalText: text, confirmApprovalBtn: btn }[id] || null) }
    });

    const el = approvalRowButtonEl('AA:BB:CC:01', 'approved');
    makeDispatcher(sb.Actions)(el, { type: 'click', target: el });

    assert.equal(modal.style.display, 'flex', 'modal konfirmasi tampil (bukan langsung POST)');
    assert.match(title.innerHTML, /Izinkan Siswa/);
    assert.equal(btn.textContent, 'Ya, Izinkan');
    assert.equal(btn.className, 'btn-modal-approve');

    // Konfirmasi pada modal meneruskan mac+status ke setApproval (skip-confirm).
    let called = null;
    sb.setApproval = (mac, status, skip) => { called = { mac, status, skip }; };
    btn.onclick();
    assert.deepEqual(called, { mac: 'AA:BB:CC:01', status: 'approved', skip: true },
        'konfirmasi meneruskan mac dari data-* tanpa interpolasi ulang');
});

test('R28/vm: klik Tolak memicu modal penolakan; defer-render approvalActionBusy tetap dijaga setApproval', () => {
    const modal = fakeEl('div', { id: 'confirmApprovalModal' });
    const title = fakeEl('h3', { id: 'confirmApprovalTitle' });
    const text = fakeEl('p', { id: 'confirmApprovalText' });
    const btn = fakeEl('button', { id: 'confirmApprovalBtn' });
    const sb = loadDetailPageActions({
        document: { getElementById: (id) => ({ confirmApprovalModal: modal, confirmApprovalTitle: title, confirmApprovalText: text, confirmApprovalBtn: btn }[id] || null) }
    });

    const el = approvalRowButtonEl('AA:BB:CC:02', 'rejected');
    makeDispatcher(sb.Actions)(el, { type: 'click', target: el });

    assert.equal(modal.style.display, 'flex');
    assert.match(title.innerHTML, /Tolak Siswa/);
    assert.equal(btn.textContent, 'Ya, Tolak');

    // Guard T8 (defer-render saat in-flight) tidak boleh hilang akibat migrasi.
    const setApprovalSrc = extractFunction(DETAIL, 'setApproval');
    assert.match(setApprovalSrc, /approvalActionBusy\s*=\s*true/);
    assert.match(setApprovalSrc, /approvalActionBusy\s*=\s*false/);

    // Modal overlay juga bermigrasi: close via delegasi tetap mengenal event-guard.
    // (Overlay asli membawa data-action="close-confirm-approval-modal" di markup.)
    modal.attrs['data-action'] = 'close-confirm-approval-modal';
    const overlayEv = { target: modal };
    makeDispatcher(sb.Actions)(fakeEl('button', { 'data-action': 'close-confirm-approval-modal' }, modal), overlayEv);
    assert.equal(modal.style.display, 'none', 'klik backdrop (target = overlay) menutup modal');
});

test('R28/vm: aksi pagination delegasi memanggil loadDetail dengan nomor halaman dari data-page', () => {
    const pages = [];
    const sb = loadDetailPageActions({ loadDetail: (p) => pages.push(p) });

    const el = fakeEl('button', { 'data-action': 'load-detail-page', 'data-page': '3' });
    makeDispatcher(sb.Actions)(el, { type: 'click', target: el });
    assert.deepEqual(pages, [3], 'loadDetail dipanggil dengan halaman hasil parse data-page');

    const first = fakeEl('button', { 'data-action': 'load-detail-page', 'data-page': '1' });
    makeDispatcher(sb.Actions)(first, { type: 'click', target: first });
    assert.deepEqual(pages, [3, 1]);
});

test('R28/vm: aksi submissions terdaftar di admin.js meneruskan data-submission-id (angka) ke showSubmissionDetail/deleteSubmission', () => {
    // Batch 8: blok registrasi pindah dari inline submissions.html ke
    // admin.js — eksekusi potongan registrasi ASLI dari modul pemiliknya.
    const names = ['show-submission-detail', 'delete-submission', 'close-detail-modal', 'export-submissions'];
    const snippets = names.map((n) => {
        const m = ADMIN_JS_SRC.match(new RegExp("Actions\\.register\\(\\s*'" + n + "'[\\s\\S]*?\\}\\);"));
        assert.ok(m, `registrasi '${n}' harus ada di admin.js`);
        return m[0];
    }).join('\n');

    const calls = { detail: [], del: [], close: 0, export: 0 };
    const sandbox = {
        Actions: stubActions(),
        console,
        showSubmissionDetail: (id) => calls.detail.push(id),
        deleteSubmission: (id) => calls.del.push(id),
        closeDetailModal: () => { calls.close++; },
        exportSubmissions: () => { calls.export++; }
    };
    sandbox.globalThis = sandbox;
    vm.createContext(sandbox);
    vm.runInContext(snippets, sandbox, { filename: 'admin.js#submissions' });

    const dispatch = makeDispatcher(sandbox.Actions);
    dispatch(fakeEl('button', { 'data-action': 'show-submission-detail', 'data-submission-id': '42' }));
    dispatch(fakeEl('button', { 'data-action': 'delete-submission', 'data-submission-id': '42' }));
    dispatch(fakeEl('button', { 'data-action': 'close-detail-modal' }));
    dispatch(fakeEl('button', { 'data-action': 'export-submissions' }));

    // Batch 8 normalisasi: id numerik lewat parseInt(..., 10), bukan string.
    assert.deepEqual(calls.detail, [42], 'showSubmissionDetail menerima id angka dari data-*');
    assert.deepEqual(calls.del, [42]);
    assert.equal(calls.close, 1);
    assert.equal(calls.export, 1);
});

test('R28/vm: kartu monitor pengawas.html — navigasi via delegasi, klik link "Pantau" tidak menavigasi ganda', () => {
    const regBlock = PENGAWAS.match(REG_BLOCK_RE);
    assert.ok(regBlock, 'blok registrasi Batch 7 (R28) pengawas.html ditemukan');

    const sandbox = {
        Actions: stubActions(),
        console,
        window: { location: '' },
        loadPengawasExams: () => {}
    };
    sandbox.globalThis = sandbox;
    vm.createContext(sandbox);
    vm.runInContext(regBlock[0], sandbox, { filename: 'pengawas.html#batch7' });

    const dispatch = makeDispatcher(sandbox.Actions);
    const anchor = fakeEl('a', { href: '/admin/pengawas/7' });
    const card = fakeEl('div', { 'data-action': 'open-pengawas-detail', 'data-exam-id': '7' });

    // Klik di area kartu → navigasi ke detail ujian.
    dispatch(card, { type: 'click', target: card });
    assert.equal(sandbox.window.location, '/admin/pengawas/7',
        'kartu membawa data-exam-id dan navigasi tetap sama seperti onclick lama');

    // Klik pada anchor "Pantau" bersarang → handler kartu mundur (href anchor yang navigasi).
    sandbox.window.location = '';
    card.attrs['data-action'] = 'open-pengawas-detail';
    dispatch(card, { type: 'click', target: anchor });
    assert.equal(sandbox.window.location, '', 'klik link Pantau tidak memicu navigasi ganda dari kartu');

    // Pagination daftar ujian.
    const pages = [];
    sandbox.loadPengawasExams = (p) => pages.push(p);
    dispatch(fakeEl('button', { 'data-action': 'load-pengawas-page', 'data-page': '2' }));
    assert.deepEqual(pages, [2]);
});
