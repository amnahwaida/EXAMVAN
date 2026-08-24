/* Contract + behavior tests untuk Batch 11 — area pengawasan & nav
 * (milik agen batch-11-pengawasan-nav).
 * Referensi temuan: review_uiux_webui.md bagian "5.8 RE-REVIEW RONDE 5" —
 * S60, R54, R56, R57, T20 (sisi submissions), T21 (sisi nav), R60 (sisi head).
 *
 * Run with: node --test static/js/uiux-batch11-pengawasan-nav.test.mjs (from webui/)
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

const NAV = read('templates/admin/partials/nav.html');
const HEAD = read('templates/admin/partials/head.html');
const PENGAWAS = read('templates/admin/pengawas.html');
const DETAIL = read('templates/admin/pengawas_detail.html');
const SUBMISSIONS = read('templates/admin/submissions.html');
const CORE = read('static/js/admin-core.js');
const MY_TEMPLATES = [
    ['templates/admin/partials/nav.html', NAV],
    ['templates/admin/partials/head.html', HEAD],
    ['templates/admin/pengawas.html', PENGAWAS],
    ['templates/admin/pengawas_detail.html', DETAIL],
    ['templates/admin/submissions.html', SUBMISSIONS],
];

function extractFunction(src, name) {
    const start = src.indexOf(`function ${name}(`);
    if (start === -1) return null;
    let depth = 0, i = src.indexOf('{', start);
    for (let j = i; j < src.length; j++) {
        if (src[j] === '{') depth++;
        else if (src[j] === '}') { depth--; if (depth === 0) return src.slice(start, j + 1); }
    }
    return null;
}

// ===========================================================================
// S60 — live-search/filter peserta wajib reset ke halaman 1
// ===========================================================================

test('S60 (statik): callback live-search & listener change di pengawas_detail memanggil loadDetail(1)', () => {
    assert.match(DETAIL,
        /initLiveSearch\(document\.getElementById\('pengawasSearch'\), function\(\) \{ loadDetail\(1\); \}\)/,
        'callback initLiveSearch tanpa argumen → halaman tidak reset (hasil kosong palsu)');
    assert.match(DETAIL,
        /statusFilterEl\.addEventListener\('change', function\(\) \{ loadDetail\(1\); \}\)/,
        'listener change statusFilter juga wajib reset ke halaman 1');
});

test('S60 (statik): callback live-search & fallback Enter di pengawas.html memanggil loadPengawasExams(1)', () => {
    assert.match(PENGAWAS,
        /initLiveSearch\(document\.getElementById\('pengawasSearch'\), function\(\) \{ loadPengawasExams\(1\); \}\)/,
        'callback initLiveSearch wajib me-reset PENG_CURRENT_PAGE via argumen 1');
    const fallback = PENGAWAS.match(/addEventListener\('keydown'[\s\S]{0,200}?loadPengawasExams\(1\)/);
    assert.ok(fallback, 'fallback keydown Enter ikut reset ke halaman 1');
});

test('S60 (perilaku): detail — SUB_PAGE=3 lalu callback live-search dipanggil → fetch URL page=1', async () => {
    const urls = [];
    let changeHandler = null;
    const el = (id, extra) => Object.assign({ id }, extra);
    const tbody = el('submissionBody', { innerHTML: '' });
    const searchInput = el('pengawasSearch', { value: 'budi' });
    const statusSel = null; // sengaja null: wiring filter tak ikut dieksekusi di blok ini
    const byId = { submissionBody: tbody, pengawasSearch: searchInput, statusFilter: statusSel };
    const sandbox = {
        SUB_PAGE: 3,
        EXAM_ID: 7,
        encodeURIComponent,
        JSON,
        console,
        document: { getElementById: (id) => (id in byId ? byId[id] : null) },
        apiFetch: (url) => { urls.push(url); return Promise.resolve({ json: () => Promise.resolve({ success: true }) }); },
        showToast() {},
        markUpdated() {},
        renderSubPagination() {},
        escapeHtml: (s) => String(s),
        localizeUTC: (s) => s || '—',
        initLiveSearch(inputEl, cb) { sandbox.__liveCb = cb; },
    };
    sandbox.globalThis = sandbox;
    vm.createContext(sandbox);

    const guardVars = 'var detailLoading = false; var detailRerunPending = false;';
    const fnSrc = extractFunction(DETAIL, 'loadDetail');
    assert.ok(fnSrc, 'loadDetail ada di pengawas_detail.html');

    // Blok wiring live-search (tanpa blok statusFilter yang butuh elemen DOM).
    const marker = "initLiveSearch(document.getElementById('pengawasSearch')";
    const at = DETAIL.indexOf(marker);
    assert.ok(at !== -1, 'blok wiring live-search ada');
    const blockEnd = DETAIL.indexOf('});', at);
    const wiring = DETAIL.slice(DETAIL.lastIndexOf('if (typeof initLiveSearch', at), DETAIL.indexOf('}', blockEnd + 3) + 1);

    vm.runInContext(guardVars + '\n' + fnSrc + '\n' + wiring, sandbox, { filename: 's60-detail' });

    sandbox.__liveCb();
    await new Promise((r) => setTimeout(r, 0));
    assert.ok(urls.length >= 1, 'callback live-search memicu fetch');
    assert.match(urls[urls.length - 1], /[?&]page=1(&|$)/,
        'fetch setelah pencarian dari hal >1 harus page=1, dapat: ' + urls[urls.length - 1]);
    assert.equal(sandbox.SUB_PAGE, 1, 'SUB_PAGE global ter-reset ke 1');

    // Listener change statusFilter (dengan elemen tersedia) → juga page=1.
    byId.statusFilter = el('statusFilter', {
        value: '',
        addEventListener(type, fn) { if (type === 'change') changeHandler = fn; },
    });
    const filterWiring = DETAIL.match(/var statusFilterEl[\s\S]{0,300}?statusFilterEl\.addEventListener\('change'[\s\S]*?\}\);/);
    assert.ok(filterWiring, 'wiring statusFilter ada');
    vm.runInContext(filterWiring[0], sandbox);
    assert.equal(typeof changeHandler, 'function', 'listener change terpasang');
    changeHandler();
    await new Promise((r) => setTimeout(r, 0));
    assert.match(urls[urls.length - 1], /[?&]page=1(&|$)/,
        'perubahan filter juga reset ke page=1');
});

test('S60 (perilaku): daftar ujian — PENG_CURRENT_PAGE=3 lalu callback live-search dipanggil → fetch URL page=1', async () => {
    const urls = [];
    const listEl = { innerHTML: '' };
    const searchInput = { value: 'matematika', dataset: {} };
    const sandbox = {
        PENG_CURRENT_PAGE: 3,
        encodeURIComponent,
        console,
        document: {
            getElementById: (id) => (id === 'pengawasExamList' ? listEl : (id === 'pengawasSearch' ? searchInput : null)),
        },
        apiFetch: (url) => { urls.push(url); return Promise.resolve({ json: () => Promise.resolve({ success: true }) }); },
        setText() {},
        updateStats() {},
        renderPagination() {},
        initLiveSearch(inputEl, cb) { sandbox.__liveCb = cb; },
    };
    sandbox.globalThis = sandbox;
    vm.createContext(sandbox);

    const fnSrc = extractFunction(PENGAWAS, 'loadPengawasExams');
    assert.ok(fnSrc, 'loadPengawasExams ada di pengawas.html');
    const marker = "initLiveSearch(document.getElementById('pengawasSearch')";
    const at = PENGAWAS.indexOf(marker);
    assert.ok(at !== -1, 'blok wiring live-search pengawas.html ada');
    const start = PENGAWAS.lastIndexOf('(function()', at);
    const end = PENGAWAS.indexOf('})();', at);
    const wiring = PENGAWAS.slice(start, end + 5);
    vm.runInContext(fnSrc + '\n' + wiring, sandbox, { filename: 's60-pengawas' });

    sandbox.__liveCb();
    await new Promise((r) => setTimeout(r, 0));
    assert.ok(urls.length >= 1, 'callback live-search memicu fetch');
    assert.match(urls[urls.length - 1], /[?&]page=1(&|$)/,
        'fetch pencarian dari hal >1 harus page=1, dapat: ' + urls[urls.length - 1]);
    assert.equal(sandbox.PENG_CURRENT_PAGE, 1, 'PENG_CURRENT_PAGE ter-reset ke 1');
});

// ===========================================================================
// R54 — konfirmasi auto-approve pakai label eksplisit "Ya, Aktifkan"
// ===========================================================================

test('R54 (statik): showConfirm aktivasi auto-approve memakai label "Ya, Aktifkan"/"Batal", bukan default merah', () => {
    const block = DETAIL.match(/aaToggle\.addEventListener\('change'[\s\S]{0,900}?\}\);/);
    assert.ok(block, 'handler change auto-approve ada');
    assert.match(block[0], /showConfirm\(/, 'aktivasi lewat dialog konfirmasi');
    assert.match(block[0], /'Ya, Aktifkan',\s*'Batal'/,
        'label eksplisit wajib ada — default showConfirm adalah tombol merah "Ya, Hapus"');
    assert.doesNotMatch(block[0], /Ya, Hapus/, 'label default hapus dilarang di jalur aktifkan');
});

// ===========================================================================
// R56 — info paginasi peserta monitoring memakai rentang offset
// ===========================================================================

test('R56 (statik): renderSubPagination menghitung rentang start–end, bukan jumlah baris halaman', () => {
    assert.match(DETAIL, /rangeStart = \(res\.page - 1\) \* perPage \+ 1/, 'rumus start ada');
    assert.match(DETAIL, /rangeEnd = Math\.min\(res\.page \* perPage, res\.total\)/, 'rumus end ada');
    assert.match(DETAIL, /Menampilkan ' \+ rangeStart/,
        'teks info paginasi memakai rentang, bukan res.submissions.length');
    assert.doesNotMatch(DETAIL, /Menampilkan ' \+ res\.submissions\.length/,
        '"Menampilkan 20 dari 57" palsu dilarang');
});

test('R56 (perilaku): halaman 3 dari 57 data → "Menampilkan 41–57 dari 57 perangkat"', () => {
    const pagEl = { style: {}, innerHTML: '' };
    const sandbox = { document: { getElementById: (id) => (id === 'subPagination' ? pagEl : null) }, Math };
    sandbox.globalThis = sandbox;
    vm.createContext(sandbox);
    const fnSrc = extractFunction(DETAIL, 'renderSubPagination');
    assert.ok(fnSrc, 'renderSubPagination ada');
    vm.runInContext(fnSrc, sandbox, { filename: 'r56' });
    sandbox.renderSubPagination({ page: 3, total_pages: 3, total: 57, submissions: new Array(17) });
    assert.match(pagEl.innerHTML,
        /Menampilkan 41(?:&ndash;|–)57 dari 57 perangkat/,
        'info rentang salah, dapat: ' + pagEl.innerHTML);
    // Halaman penuh: page 2, total 100, per_page 20 → 21–40.
    sandbox.renderSubPagination({ page: 2, total_pages: 5, total: 100, submissions: new Array(20) });
    assert.match(pagEl.innerHTML, /Menampilkan 21(?:&ndash;|–)40 dari 100 perangkat/,
        'rentang halaman tengah salah, dapat: ' + pagEl.innerHTML);
});

// ===========================================================================
// R57 — definisi lokal localizeUTC dihapus; jatuh ke alias core
// ===========================================================================

test('R57 (statik): pengawas_detail tanpa function localizeUTC lokal; alias core tetap tersedia', () => {
    assert.doesNotMatch(DETAIL, /function\s+localizeUTC/,
        'definisi lokal menimpa alias core → dua format tanggal dalam satu produk');
    assert.match(CORE, /function localizeUTC\(utcStr\) \{\s*return formatDateTimeID\(utcStr\);/,
        'alias core formatDateTimeID harus jadi satu-satunya sumber localizeUTC');
});

// ===========================================================================
// T20 (sisi submissions) — heading order h2→h4 diperbaiki menjadi h2→h3
// ===========================================================================

test('T20 (statik): submissions.html bebas h4; empat judul info-group adalah h3 setelah h2', () => {
    assert.doesNotMatch(SUBMISSIONS, /<h4[\s>]/, 'h4 setelah h2 = outline screen reader melompat');
    let lastLevel = 0;
    for (const title of ['Statistik Ujian', 'Waktu &amp; Jadwal', 'Pembuat &amp; Guru', 'Pengawas Terdaftar']) {
        const re = new RegExp(`<h3[^>]*>${title}</h3>`);
        const m = SUBMISSIONS.match(re);
        assert.ok(m, `judul "${title}" harus ber-h3`);
        const headings = [...SUBMISSIONS.slice(0, m.index).matchAll(/<h([1-6])[\s>]/g)];
        const prev = headings[headings.length - 1];
        assert.ok(prev && Number(prev[1]) <= 3,
            `heading sebelum "${title}" tidak boleh melompat ke bawah (h${prev ? prev[1] : '?'} → h3)`);
        lastLevel = Number(prev[1]);
    }
    assert.ok(lastLevel <= 3);
    assert.match(SUBMISSIONS, /\.info-group h3\s*\{/, 'selector CSS blok style halaman ikut h3');
    assert.doesNotMatch(SUBMISSIONS, /\.info-group h4\s*\{/, 'selector .info-group h4 mati dilarang');
});

// ===========================================================================
// T21 (sisi nav) — handler inline non-onclick dimigrasi ke addEventListener
// ===========================================================================

test('T21 (statik): nav.html bebas handler inline apa pun; form instansi di-wire via addEventListener', () => {
    assert.doesNotMatch(NAV, /\son[a-z]+=/, 'handler inline (onsubmit/onchange/dll) dilarang di nav.html');
    assert.match(NAV, /getElementById\('formMandatoryInstansi'\)[\s\S]{0,200}addEventListener\('submit', submitMandatoryInstansi\)/,
        'wiring submit instansi wajib addEventListener di blok script nav');
    assert.match(NAV, /function submitMandatoryInstansi\(e\)/, 'handler tetap bernama & terdefinisi');
});

// ===========================================================================
// R60 (sisi head) — suffix cache-busting manual dihapus
// ===========================================================================

test('R60 (statik): head.html tanpa suffix manual di atas {{.version}}', () => {
    assert.match(HEAD, /admin-base\.css\?v=\{\{\.version\}\}"/, 'admin-base.css memakai version murni');
    assert.doesNotMatch(HEAD, /\?v=\{\{\.version\}\}-/, 'suffix "-settings-tabs-1" dsb dilarang');
});

// ===========================================================================
// Asersi umum milik batch ini — kontrak lintas-agen
// ===========================================================================

test('Kontrak: kelima template milik batch ini bebas \\son[a-z]+= dan bebas ?v={{.version}}-', () => {
    for (const [rel, src] of MY_TEMPLATES) {
        assert.doesNotMatch(src, /\son[a-z]+=/, `${rel} wajib 0 inline handler`);
        assert.doesNotMatch(src, /\?v=\{\{\.version\}\}-/, `${rel} wajib bebas suffix cache-busting manual`);
    }
});
