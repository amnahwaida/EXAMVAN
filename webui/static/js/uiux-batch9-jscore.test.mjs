/* Suite Batch 9 — jscore (milik agen batch-9-jscore).
 * Referensi temuan: review_uiux_webui.md bagian "5.6 RE-REVIEW RONDE 3"
 * (ID: T15, S40, S41 sisi dashboard, R29 sisi admin.js, R31 parsel reload,
 * R41 parsel kontras).
 *
 * Run with:  node --test static/js/uiux-batch9-jscore.test.mjs   (from webui/)
 *
 * Latar belakang & dampak bisnis:
 *   - T15: Generate & Import XML menimpa SELURUH isi editor soal tanpa
 *     konfirmasi dan tanpa menandai state kotor — guru dengan 30 soal yang
 *     sedang diedit kehilangan semuanya seketika dan Batal menutup tanpa
 *     peringatan (bocoran guard unggulan S2 pada aksi paling destruktif).
 *     Hapus field identitas juga masih memakai onclick inline pada string HTML.
 *   - S40: Ekspor Excel memakai window.location.href langsung — dataset besar
 *     + jaringan lambat membuat tombol terasa mati (klik berulang = request
 *     ganda) dan error server merender JSON mentah menggantikan halaman.
 *   - S41 (dashboard): copyServerURL duplikasi guard clipboard sendiri di
 *     inline template; harus delegasi ke copyCode() core. div #toastContainer
 *     juga DIHAPUS dari dashboard.html (nav.html satu-satunya sumber).
 *   - R29 (admin.js): sisa onclick/onchange inline pada STRING HTML render-JS
 *     (kartu soal, divider sisip, kontrol halaman siswa) migrasi ke
 *     data-action + Actions.register — CSP-safe & terkunci guard.
 *   - R31: simpan konfigurasi soal tidak lagi me-reload halaman penuh —
 *     posisi scroll & pagination dipertahankan (pola R6).
 *   - R41: teks merah kecil literal #f87171/#ef4444 pada permukaan kartu gelap
 *     bermasalah kontrasnya; migrasi ke var(--color-danger-light).
 *
 * Pola sama dengan suite lain: kontrak statik (fs-read) + perilaku via
 * vm.runInNewContext mengeksekusi fungsi ASLI dari admin.js dengan stub DOM.
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

// ---------------------------------------------------------------------------
// T15 — guard penggantian isi editor (Generate/Import XML) + identitas
// ---------------------------------------------------------------------------

test('T15 (statik): replaceEditorQuestions mengkonfirmasi SEBELUM render & menandai kotor SESUDAHNYA', () => {
    const fn = extractFunction(ADMIN_JS_SRC, 'replaceEditorQuestions');
    assert.ok(fn, 'helper replaceEditorQuestions harus ada di admin.js');
    assert.match(fn, /Ganti semua soal di editor\?/, 'pesan konfirmasi penggantian eksplisit');
    assert.match(fn, /Soal yang sedang diedit akan hilang/, 'konsekuensi dijelaskan di dialog');
    assert.match(fn, /showConfirm/, 'guard memakai showConfirm');

    const idxRender = fn.indexOf('renderQuestions');
    const idxMark = fn.indexOf('markQuestionsConfigDirty');
    assert.ok(idxRender !== -1 && idxMark !== -1);
    assert.ok(idxRender < idxMark, 'renderQuestions dipanggil sebelum markQuestionsConfigDirty');
});

test('T15 (statik): quickGenerate & importXML tidak lagi langsung renderQuestions', () => {
    const gen = extractFunction(ADMIN_JS_SRC, 'quickGenerateQuestions');
    assert.ok(gen, 'quickGenerateQuestions ada');
    assert.match(gen, /replaceEditorQuestions/, 'generate lewat guard');
    assert.doesNotMatch(gen, /(?<!Helper)renderQuestions\(/, 'generate tidak boleh memanggil renderQuestions langsung');

    const imp = extractFunction(ADMIN_JS_SRC, 'importXMLQuestions');
    assert.ok(imp, 'importXMLQuestions ada');
    assert.match(imp, /replaceEditorQuestions/, 'import lewat guard');
    // Toast sukses import hanya boleh dipicu SETELAH soal benar-benar dirender
    // (di dalam callback onReplaced), bukan sebelum guard disetujui.
    const idxGuard = imp.indexOf('replaceEditorQuestions');
    const idxToast = imp.indexOf('Berhasil mengimpor');
    assert.ok(idxToast > idxGuard, 'toast sukses berada setelah pemanggilan guard');
});

test('T15 (statik): hapus field identitas tanpa onclick inline — data-action + markQuestionsConfigDirty', () => {
    const row = extractFunction(ADMIN_JS_SRC, 'addIdentityFieldRow');
    assert.ok(row, 'addIdentityFieldRow ada');
    assert.doesNotMatch(row, /onclick=/, 'string HTML baris identitas bebas onclick inline');
    assert.match(row, /data-action="identity-field-remove"/, 'tombol hapus memakai data-action');

    const reg = extractRegistration(ADMIN_JS_SRC, 'identity-field-remove', 'admin.js');
    assert.match(reg, /closest\('\.identity-field-row'\)/, 'handler menghapus baris lewat closest');
    assert.match(reg, /markQuestionsConfigDirty/, 'hapus baris identitas menandai state kotor (S2)');
});

// --- perilaku ---

function makeQuestionSandbox(env = {}) {
    const container = {
        children: [],
        querySelectorAll(sel) {
            if (sel === '.question-editor-card') return env.cards || [];
            return [];
        }
    };
    const sandbox = {
        document: {
            getElementById(id) {
                if (id === 'questionsList') return container;
                return env.byId && env.byId[id] ? env.byId[id] : null;
            }
        },
        questionsConfigDirty: false,
        confirmCalls: [],
        __answer: true,
        rendered: []
    };
    sandbox.showConfirm = (...a) => {
        sandbox.confirmCalls.push(a);
        return Promise.resolve(sandbox.__answer);
    };
    sandbox.renderQuestions = (qs) => { sandbox.rendered.push(qs); };
    sandbox.markQuestionsConfigDirty = function () { this.questionsConfigDirty = true; };
    return { sandbox, container };
}

const GUARD_SCRIPT = [
    'var questionsConfigDirty = false;',
    extractFunction(ADMIN_JS_SRC, 'markQuestionsConfigDirty'),
    extractFunction(ADMIN_JS_SRC, 'resetQuestionsConfigDirty'),
    extractFunction(ADMIN_JS_SRC, 'replaceEditorQuestions'),
    'questionsConfigDirty = false;'
].join('\n');

test('T15 (perilaku): editor kosong → generate langsung render tanpa dialog, flag jadi kotor', async () => {
    const { sandbox } = makeQuestionSandbox({ cards: [] });
    vm.createContext(sandbox);
    vm.runInContext(GUARD_SCRIPT, sandbox, { filename: 'admin.js#t15-empty' });

    sandbox.replaceEditorQuestions([{ number: 1 }]);
    await flush();

    assert.equal(sandbox.confirmCalls.length, 0, 'editor kosong tidak perlu konfirmasi');
    assert.equal(sandbox.rendered.length, 1, 'soal langsung dirender');
    assert.equal(sandbox.questionsConfigDirty, true, 'hasil generate ditandai belum tersimpan');
});

test('T15 (perilaku): editor berisi kartu → konfirmasi dulu; batal = utuh; setuju = diganti + kotor', async () => {
    const { sandbox } = makeQuestionSandbox({ cards: [{}, {}] });
    vm.createContext(sandbox);
    vm.runInContext(GUARD_SCRIPT, sandbox, { filename: 'admin.js#t15-cards' });

    sandbox.__answer = false;
    sandbox.replaceEditorQuestions([{ number: 1 }]);
    await flush();
    assert.equal(sandbox.confirmCalls.length, 1, 'editor berisi soal → dialog muncul');
    assert.equal(sandbox.rendered.length, 0, 'menolak → editor TIDAK ditimpa');
    assert.equal(sandbox.questionsConfigDirty, false);

    sandbox.__answer = true;
    sandbox.replaceEditorQuestions([{ number: 2 }]);
    await flush();
    assert.equal(sandbox.confirmCalls.length, 2);
    assert.equal(sandbox.rendered.length, 1, 'setuju → soal lama diganti');
    assert.equal(sandbox.questionsConfigDirty, true, 'setelah render flag kotor');
});

test('T15 (perilaku): flag dirty saja (tanpa kartu) juga memicu guard', async () => {
    const { sandbox } = makeQuestionSandbox({ cards: [] });
    vm.createContext(sandbox);
    vm.runInContext(GUARD_SCRIPT, sandbox, { filename: 'admin.js#t15-dirty' });

    vm.runInContext('markQuestionsConfigDirty(); questionsConfigDirty = true;', sandbox);
    sandbox.rendered.length = 0;
    sandbox.confirmCalls.length = 0;
    sandbox.__answer = false;

    sandbox.replaceEditorQuestions([]);
    await flush();
    assert.equal(sandbox.confirmCalls.length, 1, 'state kotor → konfirmasi tetap muncul');
    assert.equal(sandbox.rendered.length, 0);
});

test('T15 (perilaku): handler identity-field-remove menghapus baris & menandai kotor', () => {
    const registry = {};
    const sandbox = {
        Actions: { register: (n, f) => { registry[n] = f; }, _registry: registry },
        questionsConfigDirty: false
    };
    vm.createContext(sandbox);
    vm.runInContext('function markQuestionsConfigDirty() { questionsConfigDirty = true; }', sandbox);
    vm.runInContext(extractRegistration(ADMIN_JS_SRC, 'identity-field-remove', 'admin.js'),
        sandbox, { filename: 'admin.js#t15-reg' });

    let removed = 0;
    const row = { remove() { removed++; } };
    const btn = { closest: (sel) => (sel === '.identity-field-row' ? row : null) };
    registry['identity-field-remove'](btn);

    assert.equal(removed, 1, 'baris field dihapus dari DOM');
    assert.equal(sandbox.questionsConfigDirty, true, 'penghapusan menandai konfigurasi kotor');
});

// ---------------------------------------------------------------------------
// S40 — ekspor Excel: loading state, guard dobel-klik, error handling
// ---------------------------------------------------------------------------

test('S40 (statik): exportSubmissions fetch+blob, bukan window.location.href', () => {
    const fn = extractFunction(ADMIN_JS_SRC, 'exportSubmissions');
    assert.ok(fn, 'exportSubmissions ada');
    assert.doesNotMatch(fn, /window\.location\.href\s*=/, 'navigasi langsung harus hilang (JSON error tak merender)');
    assert.match(fn, /apiFetch/, 'fetch lewat apiFetch (error ternormalisasi)');
    assert.match(fn, /blob/, 'unduhan via Blob');
    assert.match(fn, /\.download\s*=/, 'unduhan via anchor a.download');
    assert.match(fn, /Mengekspor/, 'label loading Bahasa Indonesia saat fetch');
    assert.match(fn, /data-exporting/, 'guard dobel-klik via penanda data-exporting');
    assert.match(fn, /exportBtn/, 'tombol dicari by id #exportBtn bila ada');
    assert.match(fn, /data-action="export-submissions"/, 'fallback querySelector data-action (tombol milik agen lain)');
});

// --- perilaku ---

function makeExportSandbox(opts = {}) {
    let anchorClicks = 0;
    const anchor = {
        href: '', download: '', style: {},
        click() { anchorClicks++; },
        remove() {}
    };
    const btn = {
        disabled: false,
        innerHTML: '<svg></svg> Ekspor Excel',
        attrs: {},
        setAttribute(k, v) { this.attrs[k] = String(v); },
        getAttribute(k) { return Object.prototype.hasOwnProperty.call(this.attrs, k) ? this.attrs[k] : null; },
        removeAttribute(k) { delete this.attrs[k]; }
    };
    const appended = [];
    const sandbox = {
        document: {
            getElementById(id) {
                if (id === 'filterExam') return { value: opts.examId == null ? '' : String(opts.examId) };
                if (id === 'exportBtn') return opts.withBtn === false ? null : btn;
                return null;
            },
            querySelector(sel) {
                sandbox.querySelectorSelectors.push(sel);
                return opts.fallbackBtn ? btn : null;
            },
            createElement(tag) { return tag === 'a' ? anchor : {}; },
            body: { appendChild(el) { appended.push(el); }, removeChild() {} }
        },
        querySelectorSelectors: [],
        apiFetchCalls: [],
        toasts: [],
        appended,
        getAnchor: () => anchor,
        getAnchorClicks: () => anchorClicks,
        getBtn: () => btn,
        Date,
        Promise,
        Object,
        Error,
        decodeURIComponent,
        URL: { createObjectURL: () => 'blob:x', revokeObjectURL() {} }
    };
    sandbox.showToast = (msg, type) => { sandbox.toasts.push({ msg, type }); };
    sandbox.apiFetch = (url) => {
        sandbox.apiFetchCalls.push(url);
        return opts.api(url, sandbox.apiFetchCalls.length);
    };
    return sandbox;
}

const EXPORT_FN = extractFunction(ADMIN_JS_SRC, 'exportSubmissions');

test('S40 (perilaku): sukses → unduh sekali via a.download, tombol pulih', async () => {
    const sandbox = makeExportSandbox({
        examId: 3,
        api: () => Promise.resolve({
            ok: true,
            blob: () => Promise.resolve({ size: 42 }),
            headers: { get: () => 'attachment; filename="rekap.xlsx"' }
        })
    });
    vm.createContext(sandbox);
    vm.runInContext(EXPORT_FN, sandbox, { filename: 'admin.js#s40-ok' });

    sandbox.exportSubmissions();
    await flush();
    await flush();

    assert.deepEqual(sandbox.apiFetchCalls, ['/admin/api/submissions/export?tz_offset=' + new Date().getTimezoneOffset() + '&exam_id=3']);
    assert.equal(sandbox.getAnchorClicks(), 1, 'unduh tepat sekali');
    assert.equal(sandbox.getAnchor().download, 'rekap.xlsx', 'nama file dari Content-Disposition');
    const btn = sandbox.getBtn();
    assert.equal(btn.disabled, false, 'tombol aktif kembali');
    assert.equal(btn.innerHTML, '<svg></svg> Ekspor Excel', 'label asli dipulihkan');
    assert.equal(btn.getAttribute('data-exporting'), null, 'penanda exporting dibersihkan');
});

test('S40 (perilaku): dobel-klik cepat hanya memicu SATU request', async () => {
    let resolveApi;
    const sandbox = makeExportSandbox({
        examId: null,
        api: () => new Promise((res) => { resolveApi = res; })
    });
    vm.createContext(sandbox);
    vm.runInContext(EXPORT_FN, sandbox, { filename: 'admin.js#s40-dbl' });

    sandbox.exportSubmissions();
    sandbox.exportSubmissions(); // klik kedua saat in-flight
    assert.equal(sandbox.apiFetchCalls.length, 1, 'request kedua diabaikan selama ekspor berjalan');
    assert.equal(sandbox.getBtn().innerHTML, 'Mengekspor...', 'label loading tampil selama fetch');

    resolveApi({
        ok: true,
        blob: () => Promise.resolve({}),
        headers: { get: () => null }
    });
    await flush();
    await flush();
    assert.equal(sandbox.getAnchorClicks(), 1);
    assert.equal(sandbox.getBtn().disabled, false, 'tombol dipulihkan setelah selesai');
});

test('S40 (perilaku): gagal → tetap di halaman, toast error, tombol pulih', async () => {
    const sandbox = makeExportSandbox({
        examId: 5,
        api: () => Promise.resolve({
            ok: false,
            status: 500,
            json: () => Promise.resolve({ success: false, message: 'Ekspor gagal di server' })
        }),
        withBtn: false,
        fallbackBtn: true
    });
    vm.createContext(sandbox);
    vm.runInContext(EXPORT_FN, sandbox, { filename: 'admin.js#s40-err' });

    sandbox.exportSubmissions();
    await flush();
    await flush();

    assert.equal(sandbox.getAnchorClicks(), 0, 'tanpa navigasi/unduhan saat gagal');
    assert.deepEqual(sandbox.toasts, [{ msg: 'Ekspor gagal di server', type: 'error' }],
        'pesan error server tampil sebagai toast, bukan JSON mentah');
    const btn = sandbox.getBtn();
    assert.equal(btn.disabled, false, 'tombol fallback dipulihkan');
    assert.equal(btn.innerHTML, '<svg></svg> Ekspor Excel', 'label fallback dipulihkan');
    assert.deepEqual(sandbox.querySelectorSelectors, ['[data-action="export-submissions"]'],
        'tombol dicari defensif via data-action bila #exportBtn tak ada');
});

// ---------------------------------------------------------------------------
// R29 — onclick/onchange inline pada string HTML render-JS admin.js
// ---------------------------------------------------------------------------

test('R29 (statik): string HTML kartu soal/divider/kontrol siswa bebas handler inline', () => {
    const card = extractFunction(ADMIN_JS_SRC, 'createNewQuestionCard');
    assert.ok(card, 'createNewQuestionCard ada');
    assert.doesNotMatch(card, /onclick=/, 'kartu soal bebas onclick');
    assert.doesNotMatch(card, /onchange=/, 'select tipe soal bebas onchange');
    assert.match(card, /data-action="question-remove"/, 'tombol hapus soal memakai data-action');

    const divider = extractFunction(ADMIN_JS_SRC, 'createDivider');
    assert.ok(divider, 'createDivider ada');
    assert.doesNotMatch(divider, /onclick=/, 'divider bebas onclick');
    assert.match(divider, /data-action="question-insert-at"/, 'sisip soal memakai data-action');
    assert.match(divider, /data-index=/, 'argumen index lewat data-index (bukan interpolasi ke handler)');

    const controls = extractFunction(ADMIN_JS_SRC, 'renderStudentAccessControls');
    assert.ok(controls, 'renderStudentAccessControls ada');
    assert.doesNotMatch(controls, /onclick=/, 'kontrol halaman siswa bebas onclick');
    assert.match(controls, /data-action="toggle-public-results"/);
    assert.match(controls, /data-action="toggle-show-answers"/);
});

test('R29 (statik): keempat handler terdaftar dengan normalisasi parseInt(...,10)', () => {
    for (const [action, attr] of [
        ['toggle-public-results', 'data-exam-id'],
        ['toggle-show-answers', 'data-exam-id'],
        ['question-insert-at', 'data-index']
    ]) {
        const reg = extractRegistration(ADMIN_JS_SRC, action, 'admin.js');
        assert.match(reg, new RegExp("parseInt\\([^)]*getAttribute\\('" + attr + "'\\)[^)]*,\\s*10\\s*\\)"),
            `${action} wajib menormalisasi ${attr} dengan parseInt(..., 10)`);
    }
    const reg = extractRegistration(ADMIN_JS_SRC, 'question-remove', 'admin.js');
    assert.match(reg, /removeQuestionCard/, 'question-remove meneruskan elemen ke removeQuestionCard');
});

// --- perilaku ---

test('R29 (perilaku): delegasi data-action meneruskan argumen yang benar', () => {
    const registry = {};
    const sandbox = {
        Actions: { register: (n, f) => { registry[n] = f; }, _registry: registry },
        calls: { public: [], answers: [], insert: [], remove: [] }
    };
    sandbox.togglePublicResults = (id) => sandbox.calls.public.push(id);
    sandbox.toggleShowAnswers = (id) => sandbox.calls.answers.push(id);
    sandbox.insertQuestionAt = (i) => sandbox.calls.insert.push(i);
    sandbox.removeQuestionCard = (el) => sandbox.calls.remove.push(el);
    vm.createContext(sandbox);

    const src = [
        extractRegistration(ADMIN_JS_SRC, 'toggle-public-results', 'admin.js'),
        extractRegistration(ADMIN_JS_SRC, 'toggle-show-answers', 'admin.js'),
        extractRegistration(ADMIN_JS_SRC, 'question-insert-at', 'admin.js'),
        extractRegistration(ADMIN_JS_SRC, 'question-remove', 'admin.js')
    ].join('\n');
    vm.runInContext(src, sandbox, { filename: 'admin.js#r29-reg' });

    const mkBtn = (attrs) => ({
        attrs,
        getAttribute(k) { return Object.prototype.hasOwnProperty.call(attrs, k) ? attrs[k] : null; }
    });

    registry['toggle-public-results'](mkBtn({ 'data-exam-id': '9' }));
    registry['toggle-show-answers'](mkBtn({ 'data-exam-id': '12' }));
    registry['question-insert-at'](mkBtn({ 'data-index': '4' }));
    const cardEl = { id: 'card-el' };
    registry['question-remove'](cardEl);

    assert.deepEqual(sandbox.calls.public, [9], 'exam-id sampai sebagai angka');
    assert.deepEqual(sandbox.calls.answers, [12]);
    assert.deepEqual(sandbox.calls.insert, [4], 'index divider sampai sebagai angka');
    assert.deepEqual(sandbox.calls.remove, [cardEl], 'elemen kartu diteruskan utuh');
});

test('R29 (perilaku): perubahan select tipe soal terdelegasi ke onQuestionTypeChange', () => {
    // Listener delegated 'change' pada questionsModal (pengganti onchange inline)
    const iifeStart = ADMIN_JS_SRC.indexOf("(function () {\n    const qModal = document.getElementById('questionsModal')");
    assert.notEqual(iifeStart, -1, 'blok listener modal soal ada');
    const block = ADMIN_JS_SRC.slice(iifeStart, iifeStart + 2200);
    assert.match(block, /q-type-select/, 'listener change memeriksa .q-type-select');
    assert.match(block, /onQuestionTypeChange\(t\)/, 'meneruskan elemen select ke onQuestionTypeChange');
});

// ---------------------------------------------------------------------------
// R31 — simpan konfigurasi soal tanpa reload penuh
// ---------------------------------------------------------------------------

test('R31 (statik): saveQuestionsConfig tanpa location.reload — tutup modal + segarkan statistik', () => {
    const fn = extractFunction(ADMIN_JS_SRC, 'saveQuestionsConfig');
    assert.ok(fn, 'saveQuestionsConfig ada');
    assert.doesNotMatch(fn, /location\.reload/, 'reload penuh harus hilang (posisi scroll & pagination)');
    assert.match(fn, /closeQuestionsModal/, 'modal ditutup pasca simpan sukses');
    assert.match(fn, /typeof refreshDashboardStats === 'function'/, 'refreshDashboardStats dipanggil guard');
});

test('R31 (perilaku): sukses simpan → toast, modal tertutup, stats disegarkan, TANPA reload', async () => {
    const btn = { disabled: false, innerHTML: 'Simpan Konfigurasi' };
    const sandbox = {
        activeExamId: 7,
        questionsConfigDirty: true,
        document: {
            getElementById(id) {
                if (id === 'btnSaveQuestionsConfig') return btn;
                return null; // examSecurityLevel, panelColorHex, jadwal, dsb. opsional
            }
        },
        getQuestionsFromEditor: () => [{ number: 1 }],
        getIdentityFieldsFromEditor: () => [],
        getPengawasIdsFromEditor: () => [],
        apiFetch: () => Promise.resolve({ json: () => Promise.resolve({ success: true, message: 'Konfigurasi tersimpan' }) }),
        toasts: [],
        closed: 0,
        statsRefreshed: 0,
        reloaded: 0
    };
    sandbox.location = { reload() { sandbox.reloaded++; } };
    sandbox.showToast = (msg, type) => { sandbox.toasts.push({ msg, type }); };
    sandbox.closeQuestionsModal = () => { sandbox.closed++; };
    sandbox.refreshDashboardStats = () => { sandbox.statsRefreshed++; };
    vm.createContext(sandbox);

    const script = [
        'var questionsConfigDirty = true;',
        'var activeExamId = 7;',
        extractFunction(ADMIN_JS_SRC, 'markQuestionsConfigDirty'),
        extractFunction(ADMIN_JS_SRC, 'resetQuestionsConfigDirty'),
        extractFunction(ADMIN_JS_SRC, 'saveQuestionsConfig')
    ].join('\n');
    vm.runInContext(script, sandbox, { filename: 'admin.js#r31' });

    sandbox.saveQuestionsConfig();
    await flush();
    await flush();

    assert.equal(sandbox.reloaded, 0, 'TIDAK boleh location.reload');
    assert.equal(sandbox.closed, 1, 'modal konfigurasi ditutup');
    assert.equal(sandbox.statsRefreshed, 1, 'statistik dashboard disegarkan in-place');
    assert.equal(sandbox.questionsConfigDirty, false, 'flag kotor direset setelah tersimpan');
    assert.deepEqual(sandbox.toasts, [{ msg: 'Konfigurasi tersimpan', type: 'success' }]);
    assert.equal(btn.disabled, false, 'tombol simpan dipulihkan');
});

// ---------------------------------------------------------------------------
// S41 (dashboard) — copyServerURL lokal dihapus, delegasi ke copyCode core
// ---------------------------------------------------------------------------

test('S41 (statik): dashboard.html tanpa copyServerURL/navigator.clipboard lokal; server-url-copy → copyCode', () => {
    assert.doesNotMatch(DASHBOARD, /copyServerURL/, 'definisi lokal copyServerURL harus dihapus');
    assert.doesNotMatch(DASHBOARD, /navigator\.clipboard/, 'inline template tidak boleh menyentuh navigator.clipboard langsung');
    const reg = DASHBOARD.match(/Actions\.register\(\s*'server-url-copy'[\s\S]*?\}\);/);
    assert.ok(reg, 'registrasi server-url-copy tetap ada');
    assert.match(reg[0], /copyCode\(el\.getAttribute\('data-url'\)\)/, 'delegasi ke copyCode core (guard + fallback + toast)');
});

test('S41 (statik): div #toastContainer DIHAPUS dari dashboard.html (nav.html satu-satunya sumber)', () => {
    assert.doesNotMatch(DASHBOARD, /id="toastContainer"/, 'container toast tidak boleh diduplikasi di halaman');
});

// ---------------------------------------------------------------------------
// R41 (parsel) — teks merah literal pada permukaan kartu gelap → token
// ---------------------------------------------------------------------------

test('R41 (statik): admin.js bebas hex merah literal (#f87171/#ef4444) di string render', () => {
    for (const hex of ['#f87171', '#ef4444']) {
        const n = (ADMIN_JS_SRC.match(new RegExp(hex, 'gi')) || []).length;
        assert.equal(n, 0, `hex ${hex} harus nol di admin.js — pakai var(--color-danger-light)`);
    }
    // Pemakaian token hadir di titik-titik yang tadinya literal.
    const toggle = extractFunction(ADMIN_JS_SRC, 'togglePublicResults');
    assert.match(toggle, /var\(--color-danger-light\)/, 'badge Nonaktif memakai token danger-light');
});

test('R41 (statik): kontras var(--color-danger-light) #fca5a5 ≥ 4.5:1 di atas kartu #1e1e32', () => {
    const lum = ([r, g, b]) => {
        const lin = [r, g, b].map((v) => {
            const c = v / 255;
            return c <= 0.03928 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4);
        });
        return 0.2126 * lin[0] + 0.7152 * lin[1] + 0.0722 * lin[2];
    };
    const fg = lum([0xfc, 0xa5, 0xa5]);
    const bgCard = lum([0x1e, 0x1e, 0x32]);
    const bgMain = lum([0x09, 0x09, 0x0e]);
    const ratio = (l1, l2) => (Math.max(l1, l2) + 0.05) / (Math.min(l1, l2) + 0.05);
    assert.ok(ratio(fg, bgCard) >= 4.5, `kontras di kartu #1e1e32 = ${ratio(fg, bgCard).toFixed(2)}:1 (< 4.5)`);
    assert.ok(ratio(fg, bgMain) >= 4.5, `kontras di bg utama = ${ratio(fg, bgMain).toFixed(2)}:1 (< 4.5)`);
});
