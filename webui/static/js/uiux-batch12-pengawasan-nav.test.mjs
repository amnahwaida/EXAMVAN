/* Contract + behavior tests untuk Batch 12 — area pengawasan & nav
 * (milik agen batch-12-pengawasan-nav).
 * Referensi temuan: review_uiux_webui.md bagian "5.9 RE-REVIEW RONDE 6" —
 * R73, R74, R75, R76, S66 (sisi login), kontrak token --color-danger-bright.
 *
 * Run with: node --test static/js/uiux-batch12-pengawasan-nav.test.mjs (from webui/)
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
const LOGIN = read('templates/admin/login.html');
const PENGAWAS = read('templates/admin/pengawas.html');
const DETAIL = read('templates/admin/pengawas_detail.html');
const SUBMISSIONS = read('templates/admin/submissions.html');
const MY_TEMPLATES = [
    ['templates/admin/partials/nav.html', NAV],
    ['templates/admin/partials/head.html', HEAD],
    ['templates/admin/login.html', LOGIN],
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
// Kontrak lintas-agen — substitusi #f87171 → var(--color-danger-bright)
// (--color-danger-bright didefinisikan agen lain di theme.css)
// ===========================================================================

test('Kontrak token: keenam template milik batch ini bebas hard-code #f87171', () => {
    for (const [rel, src] of MY_TEMPLATES) {
        assert.doesNotMatch(src, /#f87171/i,
            `${rel} masih memakai #f87171 hard-code — wajib var(--color-danger-bright)`);
    }
});

test('Kontrak token: substitusi terpasang di lokasi yang ditetapkan', () => {
    // pengawas_detail ×4 lokasi: .pd-action-danger, .btn-modal-reject:hover,
    // .log-entry.logout::before, .audit-log-entry.disable::before
    const detailHits = [...DETAIL.matchAll(/var\(--color-danger-bright\)/g)].length;
    assert.ok(detailHits >= 4, `pengawas_detail butuh >=4 pemakaian token, dapat ${detailHits}`);
    assert.match(DETAIL, /\.pd-action-danger\s*\{[^}]*color:\s*var\(--color-danger-bright\)/);
    assert.match(DETAIL, /\.btn-modal-reject:hover\s*\{[^}]*background:\s*var\(--color-danger-bright\)/);
    assert.match(DETAIL, /\.log-entry\.logout::before\s*\{[^}]*var\(--color-danger-bright\)[^}]*var\(--color-danger-bright\)/);
    assert.match(DETAIL, /\.audit-log-entry\.disable::before\s*\{[^}]*var\(--color-danger-bright\)[^}]*var\(--color-danger-bright\)/);
    // pengawas ×1: .log-entry.logout::before
    assert.match(PENGAWAS, /\.log-entry\.logout::before\s*\{[^}]*var\(--color-danger-bright\)/);
    // login ×1: inline style #turnstileError
    assert.match(LOGIN, /id="turnstileError"[^>]*color:var\(--color-danger-bright\)/);
});

// ===========================================================================
// S66 (sisi login) — suffix cache-busting manual dihapus (integritas R60)
// ===========================================================================

test('S66 (statik): login.html tanpa suffix manual di atas {{.version}}', () => {
    assert.doesNotMatch(LOGIN, /\?v=\{\{\.version\}\}-/,
        'suffix seperti "-5"/"-3" mem-bypass version murni → cache tak pernah invalidasi');
    assert.match(LOGIN, /public-mobile\.css\?v=\{\{\.version\}\}"/);
    assert.match(LOGIN, /public-desktop\.css\?v=\{\{\.version\}\}"/);
});

test('S66 (statik): seluruh template milik batch ini bebas ?v={{.version}}-', () => {
    for (const [rel, src] of MY_TEMPLATES) {
        assert.doesNotMatch(src, /\?v=\{\{\.version\}\}-/, `${rel} wajib bebas suffix cache-busting manual`);
    }
});

// ===========================================================================
// R73 — info paginasi daftar ujian pengawasan tanpa offset (ekor R56)
// ===========================================================================

test('R73 (statik): renderPagination di pengawas.html menghitung rentang start–end', () => {
    assert.match(PENGAWAS, /perPage = 10;/, 'perPage lokal harus sinkron dengan per_page=10 di loadPengawasExams');
    assert.match(PENGAWAS, /rangeStart = \(res\.page - 1\) \* perPage \+ 1/, 'rumus start ada');
    assert.match(PENGAWAS, /rangeEnd = Math\.min\(res\.page \* perPage, res\.total\)/, 'rumus end ada');
    assert.match(PENGAWAS, /Menampilkan ' \+ rangeStart[\s\S]{0,80}' ujian</,
        'teks info paginasi memakai rentang, satuan "ujian"');
    assert.doesNotMatch(PENGAWAS, /Menampilkan ' \+ res\.exams\.length/,
        '"Menampilkan 10 dari 25" palsu dilarang');
});

test('R73 (perilaku): halaman 3 dari 25 data → "Menampilkan 21–25 dari 25 ujian"', () => {
    const pagEl = { style: {}, innerHTML: '' };
    const sandbox = { document: { getElementById: (id) => (id === 'pengawasPagination' ? pagEl : null) }, Math };
    sandbox.globalThis = sandbox;
    vm.createContext(sandbox);
    const fnSrc = extractFunction(PENGAWAS, 'renderPagination');
    assert.ok(fnSrc, 'renderPagination ada di pengawas.html');
    vm.runInContext(fnSrc, sandbox, { filename: 'r73' });
    // Halaman terakhir parsial: page 3, per_page 10, total 25 → 21–25.
    sandbox.renderPagination({ page: 3, total_pages: 3, total: 25, exams: new Array(5) });
    assert.match(pagEl.innerHTML,
        /Menampilkan 21(?:&ndash;|–)25 dari 25 ujian/,
        'rentang ekor parsial salah, dapat: ' + pagEl.innerHTML);
    // Halaman tengah penuh: page 2, total 100, per_page 10 → 11–20.
    sandbox.renderPagination({ page: 2, total_pages: 10, total: 100, exams: new Array(10) });
    assert.match(pagEl.innerHTML, /Menampilkan 11(?:&ndash;|–)20 dari 100 ujian/,
        'rentang halaman tengah salah, dapat: ' + pagEl.innerHTML);
});

// ===========================================================================
// R74 — empty-state countdown rotasi tidak lagi cabang mati
// ===========================================================================

function runUpdateCountdown(state, initialHtml) {
    const el = { id: 'pdTokenCountdown', innerHTML: initialHtml !== undefined ? initialHtml : '' };
    const sandbox = {
        TOKEN_MODE: state.mode || 'rotating',
        TOKEN_LAST_RESET: state.lastReset,
        TOKEN_INTERVAL_MINUTES: state.interval,
        document: { getElementById: (id) => (id === 'pdTokenCountdown' ? el : null) },
    };
    sandbox.globalThis = sandbox;
    vm.createContext(sandbox);
    const fnSrc = extractFunction(DETAIL, 'updateCountdown');
    assert.ok(fnSrc, 'updateCountdown ada di pengawas_detail.html');
    vm.runInContext(fnSrc, sandbox, { filename: 'r74' });
    sandbox.updateCountdown();
    return el.innerHTML;
}

test('R74 (statik): guard ganda dihapus — hanya !TOKEN_INTERVAL_MINUTES yang return', () => {
    assert.doesNotMatch(DETAIL, /if \(!TOKEN_LAST_RESET \|\| !TOKEN_INTERVAL_MINUTES\) return;/,
        'guard ganda membuat pesan empty-state mustahil tampil (cabang mati)');
    assert.match(DETAIL, /if \(!TOKEN_INTERVAL_MINUTES\) return;/, 'guard interval tetap ada');
    assert.match(DETAIL, /if \(!TOKEN_LAST_RESET( \|\| TOKEN_LAST_RESET === '')?\) \{\s*el\.innerHTML = '<span style="color:var\(--color-text-muted\)/,
        'cabang !TOKEN_LAST_RESET menampilkan pesan empty-state');
});

test('R74 (perilaku): TOKEN_LAST_RESET kosong → pesan "Rotasi otomatis belum aktif"', () => {
    const html = runUpdateCountdown({ mode: 'rotating', lastReset: '', interval: 30 });
    assert.match(html, /Rotasi otomatis belum aktif/,
        'empty-state wajib tampil saat rotasi belum pernah berjalan, dapat: ' + html);
});

test('R74 (perilaku): TOKEN_INTERVAL_MINUTES mati → keluar tanpa menulis DOM', () => {
    const html = runUpdateCountdown({ mode: 'rotating', lastReset: '', interval: 0 }, 'SENTINEL');
    assert.equal(html, 'SENTINEL',
        'interval 0 harus return sebelum menulis apa pun ke elemen countdown');
});

test('R74 (perilaku): mode static tetap mengosongkan elemen', () => {
    const html = runUpdateCountdown({ mode: 'static', lastReset: '2026-01-01T00:00:00Z', interval: 30 }, 'X');
    assert.equal(html, '');
});

// ===========================================================================
// R75 — toast gagal antrean izin tidak spam tiap poll 5 detik
// ===========================================================================

function buildApprovalsSandbox() {
    const tbody = { innerHTML: '', textContent: '' };
    const section = { id: 'approvalSection' };
    const toasts = [];
    const sandbox = {
        document: {
            hidden: false,
            getElementById: (id) => (id === 'approvalBody' ? tbody : (id === 'approvalSection' ? section : null)),
        },
        EXAM_ID: 7,
        apiFetch: () => Promise.resolve({ json: () => Promise.resolve({ success: false, message: 'boom' }) }),
        showToast: (msg, type) => { toasts.push([String(msg), type]); },
        markUpdated() {},
        PengawasDetailQueue: { serializeApprovals: () => '' },
        console: { error() {} },
        __toasts: toasts,
        __tbody: tbody,
        __setResponse: (makeRes) => { sandbox.apiFetch = () => Promise.resolve({ json: () => Promise.resolve(makeRes()) }); },
    };
    sandbox.globalThis = sandbox;
    vm.createContext(sandbox);

    // Deklarasi flag modul + helper notice + fungsi loadApprovals utuh.
    const varsMatch = DETAIL.match(/var approvalLoading = false;[\s\S]*?var approvalsErrorToasted = false;/);
    assert.ok(varsMatch, 'blok deklarasi state antrean ada');
    assert.match(varsMatch[0], /var approvalsErrorToasted = false;/,
        'flag de-dup toast approvalsErrorToasted wajib dideklarasikan di scope modul');
    const noticeFn = extractFunction(DETAIL, 'renderApprovalNotice');
    const loadFn = extractFunction(DETAIL, 'loadApprovals');
    assert.ok(noticeFn && loadFn, 'renderApprovalNotice & loadApprovals ada');
    vm.runInContext(varsMatch[0] + '\n' + noticeFn + '\n' + loadFn, sandbox, { filename: 'r75' });
    return sandbox;
}

test('R75 (perilaku): 3 kegagalan berturut = tepat 1 toast error', async () => {
    const sb = buildApprovalsSandbox();
    for (let i = 0; i < 3; i++) {
        sb.loadApprovals();
        await new Promise((r) => setTimeout(r, 0));
    }
    const errs = sb.__toasts.filter(([, t]) => t === 'error');
    assert.equal(errs.length, 1, 'poll 5s gagal berulang wajib 1 toast (pola once), dapat: '
        + JSON.stringify(sb.__toasts));
    assert.match(errs[0][0], /boom/, 'toast membawa pesan server');
});

test('R75 (perilaku): sukses me-reset flag — kegagalan berikutnya men-toast lagi', async () => {
    const sb = buildApprovalsSandbox();
    sb.loadApprovals(); // gagal #1 → toast
    await new Promise((r) => setTimeout(r, 0));
    assert.equal(sb.__toasts.filter(([, t]) => t === 'error').length, 1);

    sb.__setResponse(() => ({ success: true, data: [], total: 0 }));
    sb.loadApprovals(); // sukses → reset flag
    await new Promise((r) => setTimeout(r, 0));

    sb.__setResponse(() => ({ success: false, message: 'boom-lagi' }));
    sb.loadApprovals(); // gagal #2 setelah sukses → toast boleh lagi
    await new Promise((r) => setTimeout(r, 0));
    const errs = sb.__toasts.filter(([, t]) => t === 'error');
    assert.equal(errs.length, 2, 'setelah sukses, kegagalan baru wajib men-toast kembali');
    assert.match(errs[1][0], /boom-lagi/);
});

test('R75 (statik): jalur sukses me-reset approvalsErrorToasted', () => {
    const fnSrc = extractFunction(DETAIL, 'loadApprovals');
    assert.match(fnSrc, /approvalsErrorToasted = true/, 'toast dikirim bersamaan dengan set flag');
    assert.match(fnSrc, /if \(res && res\.success\) approvalsErrorToasted = false;/,
        'reset flag hanya di jalur sukses respons');
});

// ===========================================================================
// R76 — label Izinkan/Tolak terbaca di ≤480px
// ===========================================================================

test('R76 (statik): .pd-action-btn pada ≤480px minimal 0.75rem — hemat lewat padding', () => {
    const mq = DETAIL.match(/@media \(max-width: 480px\) \{[\s\S]*?\n\}/);
    assert.ok(mq, 'blok media query 480px ada');
    const rule = mq[0].match(/\.pd-action-btn\s*\{[^}]*\}/);
    assert.ok(rule, '.pd-action-btn di dalam media query 480px');
    assert.match(rule[0], /font-size:\s*0\.75rem/, 'font-size 0.65rem (~10,4px) terlalu kecil untuk aksi kritis');
    assert.doesNotMatch(rule[0], /0\.65rem/);
});
