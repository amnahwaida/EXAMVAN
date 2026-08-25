/**
 * ══════════════════════════════════════════════════════════════════════════
 * Suite UI/UX BATCH 15 — PENGAWASAN (agen batch15-pengawasan)
 * ══════════════════════════════════════════════════════════════════════════
 *
 * Referensi: review_uiux_webui.md bagian 5.12 RE-REVIEW RONDE 9 (basis
 * f0ab8d7, pasca Batch 14). Cakupan temuan milik agen ini:
 *
 *   S89 — Kolom Durasi submissions mentok "—": normalisasi ISO hanya
 *         dilakukan untuk data-start, sedangkan data-end membawa format
 *         default time.Time Go (nama zona "WIB" dsb.) yang ditolak parser
 *         Date V8 → NaN → durasi kosong permanen.
 *         Kontrak: SATU helper normalisasi dipakai untuk start DAN end;
 *         pasangan string ala Go menghasilkan teks durasi benar.
 *         Kalibrasi lokasi: blok durasi kini di submissions.html:425–455
 *         (review menyebut 431–436; data-end sumber di :312).
 *
 *   S94 — Silent refresh monitoring (12 dtk) rebuild tbody submissions tiap
 *         tick tanpa syarat: fokus keyboard/tap lenyap, SR membaca ulang
 *         tabel penuh. Kontrak perilaku (vm):
 *           (a) payload identik dengan render sebelumnya → tbody.innerHTML
 *               TIDAK ditulis ulang (skip-render bila snapshot sama);
 *           (b) bila berubah, elemen yang sedang fokus (diidentifikasi via
 *               atribut stabil data-action/data-mac/data-submission-id)
 *               DIPULIHAKAN fokusnya setelah render.
 *
 *   S106 — Toggle auto-approve (input opacity:0;width:0;height:0) tanpa
 *         indikator fokus terlihat; outline global menggambar pada kotak
 *         berukuran nol. Kontrak statik: rule fokus eksis dan menyasar
 *         SLIDER (saudara input), bukan kotak 0×0.
 *
 *   R111 — Angka progres 0% memakai literal #6b7280 (abu paling redup di
 *         latar gelap). Kontrak: literal hilang, token var(--color-text-muted)
 *         dipakai pada ternary barColor.
 *
 *   R112 — announceQueueCount hanya dipanggil di jalur sukses; cabang gerbang
 *         tertutup, gagal-muat, dan catch tidak mengumumkan apa pun lewat
 *         live region. Kontrak vm: SEMUA cabang hasil polling antrean
 *         menghasilkan pengumuman di #queueLiveRegion.
 *
 *   R113 — Snapshot submissions di-JSON.stringify ke atribut DOM tiap tick.
 *         Kontrak: tidak ada lagi penulisan atribut data-subs; pembaca
 *         (showAccessLog) membaca variabel modul dan tetap berfungsi (vm).
 *
 *   R114 — Dead CSS ±115 baris + simbol sprite tak terpakai di dua halaman
 *         pengawasan. Kontrak: selector/simbol mati terhapus + guard dua
 *         arah (semua pemakaian use href resolve ke defs lokal/global;
 *         semua simbol lokal dipakai di file sendiri). Kalibrasi: baris
 *         .log-entry.logout::before di pengawas.html DIPERTAHANKAN karena
 *         dijaga uiux-batch12-pengawasan-nav.test.mjs (guard antar-batch).
 *
 *   R115 — Kartu ujian role="button" membungkus link "Pantau" (interactive
 *         nested). Kontrak: aria-label eksplisit pada kartu + klik link
 *         Pantau TIDAK memicu aksi toggle kartu.
 *
 *   R116 — Label dwibahasa "Nilai / Score" → "Nilai"; magic number per_page
 *         =20 terduplikasi → konstanta tunggal SUBS_PER_PAGE.
 *
 *   R99  — Tombol "Detail" submissions ~28px vs Hapus 44px di baris yang
 *         sama. Kontrak statik: kedua tombol min-height ≥44px.
 *
 * Kepemilikan file agen ini: templates/admin/pengawas_detail.html,
 *   templates/admin/pengawas.html, templates/admin/submissions.html,
 *   static/js/pengawas-detail.js (bila logika terkait memanggil),
 *   static/js/uiux-batch15-pengawasan.test.mjs.
 *
 * Metode harness: statik fs-read + vm.runInNewContext atas ekstraksi
 * inline-script / fungsi template (brace matching), pola header
 * uiux-batch14-core.test.mjs.
 *
 * Run with:  node --test static/js/uiux-batch15-pengawasan.test.mjs   (from webui/)
 */

import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';

const HERE = dirname(fileURLToPath(import.meta.url));
const WEBUI_ROOT = join(HERE, '..', '..');
const read = (...p) => readFileSync(join(WEBUI_ROOT, ...p), 'utf8');

const DETAIL = read('templates', 'admin', 'pengawas_detail.html');
const PENGAWAS = read('templates', 'admin', 'pengawas.html');
const SUBMISSIONS = read('templates', 'admin', 'submissions.html');
const GLOBAL_SPRITE = read('templates', 'admin', 'partials', 'svg-symbols.html');

// ---------------------------------------------------------------------------
// Helpers ekstraksi
// ---------------------------------------------------------------------------

/** Ekstrak satu fungsi bernama utuh dari source (brace matching). */
function extractFunction(src, name) {
    const idx = src.indexOf('function ' + name + '(');
    if (idx === -1) return null;
    const start = src.indexOf('{', idx);
    let depth = 0;
    for (let j = start; j < src.length; j++) {
        if (src[j] === '{') depth++;
        else if (src[j] === '}') {
            depth--;
            if (depth === 0) return src.slice(idx, j + 1);
        }
    }
    return null;
}

/** Semua inline script (tanpa atribut src) dari sebuah HTML. */
function inlineScripts(html) {
    const out = [];
    const re = /<script\b[^>]*>([\s\S]*?)<\/script>/g;
    let m;
    while ((m = re.exec(html)) !== null) {
        if (/src=/.test(m.input.slice(m.index, m.index + 120))) continue;
        out.push(m[1]);
    }
    return out;
}

// ===========================================================================
// S89 — Durasi submissions: normalisasi satu pintu untuk start & end
// ===========================================================================

test('S89 (statik): end dinormalisasi dengan helper yang sama seperti start', () => {
    const script = inlineScripts(SUBMISSIONS).join('\n');
    const fn = extractFunction(script, 'computeDurationText') || '';
    const norm = extractFunction(script, 'normalizeGoTimeStr');
    assert.ok(norm, 'helper normalisasi normalizeGoTimeStr eksis di inline script submissions');
    assert.match(fn, /normalizeGoTimeStr\(startStr\)/, 'start lewat helper normalisasi');
    assert.match(fn, /normalizeGoTimeStr\(endStr\)/,
        'end WAJIB lewat helper normalisasi yang sama — sekarang mentah new Date(endStr)');
});

test('S89 (vm): format default Go (zona WIB) di-parse menjadi durasi benar', () => {
    const script = inlineScripts(SUBMISSIONS).join('\n');
    const norm = extractFunction(script, 'normalizeGoTimeStr');
    const dur = extractFunction(script, 'computeDurationText');
    assert.ok(norm && dur, 'kedua fungsi durasi eksis');
    const sb = vm.createContext({});
    vm.runInContext(norm + '\n' + dur, sb, { filename: 's89-duration' });

    // Format default time.Time Go: "2026-08-25 10:00:00.123456789 +0700 WIB m=+…"
    const iso = sb.normalizeGoTimeStr('2026-08-25 10:00:00.123456789 +0700 WIB m=+0.123456789');
    assert.equal(iso, '2026-08-25T10:00:00.123456789+07:00',
        'zona nama (WIB) dibuang, offset numerik dipertahankan, spasi→T');
    assert.equal(sb.normalizeGoTimeStr('2026-08-25T02:00:00Z'), '2026-08-25T02:00:00Z',
        'RFC3339 Z lolos utuh');
    assert.equal(sb.normalizeGoTimeStr('2026-08-25T02:00:00+07:00'), '2026-08-25T02:00:00+07:00',
        'RFC3339 offset lolos utuh');

    // Pasangan ala Go (start RFC3339, end format default Go) → durasi benar.
    assert.equal(
        sb.computeDurationText('2026-08-25T02:00:00Z', '2026-08-25 09:30:05.1 +0700 WIB m=+0'),
        '30 menit 5 detik',
        'end zona WIB (+07:00) vs start UTC → 30 menit 5 detik');
    assert.equal(
        sb.computeDurationText('2026-08-25 07:00:00 +0700 WIB m=+0', '2026-08-25 07:00:30 +0700 WIB m=+0'),
        '30 detik');
    assert.equal(sb.computeDurationText('2026-08-25T02:00:00Z', '2026-08-25T01:00:00Z'), '0 detik',
        'end < start diklem ke 0');
});

// ===========================================================================
// S94 — Silent refresh submissions: skip bila identik + pulihkan fokus
// ===========================================================================

/** Sandbox loadDetail: DOM minimal + penghitung tulisan innerHTML tbody. */
function buildLoadDetailSandbox(payloadFactory, opts = {}) {
    let writes = 0;
    const tbody = { _v: '<tr><td>Memuat data...</td></tr>', textContent: 'Memuat data...' };
    Object.defineProperty(tbody, 'innerHTML', {
        set(v) { writes++; this._v = String(v); },
        get() { return this._v; },
    });
    const restoredBtn = {
        attrs: { 'data-action': 'show-access-log', 'data-submission-id': '42' },
        focused: false,
        focus() { this.focused = true; },
    };
    const activeBtn = {
        attrs: { 'data-action': 'show-access-log', 'data-submission-id': '42' },
        parentNode: null,
        getAttribute(n) { return this.attrs[n] !== undefined ? this.attrs[n] : null; },
    };
    tbody.contains = (el) => el === activeBtn || el === restoredBtn;
    tbody.querySelector = () => restoredBtn;

    const sandbox = {
        document: {
            hidden: false,
            activeElement: opts.focusActive ? activeBtn : null,
            getElementById(id) {
                if (id === 'submissionBody') return tbody;
                if (id === 'pengawasSearch') return { value: '' };
                if (id === 'statusFilter') return { value: '' };
                if (id === 'subPagination') return { style: {}, innerHTML: '', display: '' };
                return null;
            },
        },
        SUB_PAGE: 1,
        EXAM_ID: 7,
        detailLoading: false,
        detailRerunPending: false,
        apiFetch: () => Promise.resolve({ json: () => Promise.resolve(payloadFactory()) }),
        showToast() {},
        markUpdated() {},
        localizeUTC: () => '2026-08-25 09:00',
        escapeHtml: (s) => String(s == null ? '' : s),
        esc: (s) => String(s == null ? '' : s),
        __restoredBtn: restoredBtn,
        __activeBtn: activeBtn,
        __writes: () => writes,
        __tbodyHTML: () => tbody.innerHTML,
    };
    sandbox.globalThis = sandbox;
    vm.createContext(sandbox);

    // State modul + fungsi-fungsi jalur render submissions.
    const decl = `
        var SUBS_PER_PAGE = 20;
        var subsLastHtml = null;
        var subsSnapshot = [];
    `;
    const names = ['getSubsFocusKey', 'restoreSubsFocus', 'renderSubPagination', 'loadDetail'];
    const parts = names.map((n) => {
        const fn = extractFunction(DETAIL, n);
        assert.ok(fn, 'fungsi ' + n + ' eksis di pengawas_detail.html');
        return fn;
    });
    vm.runInContext(decl + '\n' + parts.join('\n'), sandbox, { filename: 's94-loaddetail' });
    return sandbox;
}

const SUBS_PAYLOAD_A = () => ({
    success: true, page: 1, total_pages: 1, total: 1,
    submissions: [{ id: '42', mac_address: 'AA:BB', submitted: false, start_time: '2026-08-25T02:00:00Z', attempt_count: 1 }],
});
const SUBS_PAYLOAD_B = () => ({
    success: true, page: 1, total_pages: 1, total: 2,
    submissions: [
        { id: '42', mac_address: 'AA:BB', submitted: false, start_time: '2026-08-25T02:00:00Z', attempt_count: 2 },
        { id: '43', mac_address: 'CC:DD', submitted: true, start_time: '2026-08-25T03:00:00Z', attempt_count: 1 },
    ],
});

test('S94 (vm): tick dengan payload IDENTIK tidak menulis ulang tbody', async () => {
    const sb = buildLoadDetailSandbox(SUBS_PAYLOAD_A);
    await sb.loadDetail(1, true);
    await new Promise((r) => setTimeout(r, 0));
    const afterFirst = sb.__writes();
    assert.ok(afterFirst >= 1, 'render pertama tetap menulis');

    await sb.loadDetail(1, true); // tick polling ke-12 dtk, payload sama
    await new Promise((r) => setTimeout(r, 0));
    assert.equal(sb.__writes(), afterFirst,
        'snapshot serial identik → tbody.innerHTML TIDAK ditulis ulang (fokus/tap selamat)');
});

test('S94 (vm): payload BERUBAH → render ulang dan fokus DIPULIHAKAN', async () => {
    let useB = false;
    const sb = buildLoadDetailSandbox(() => (useB ? SUBS_PAYLOAD_B() : SUBS_PAYLOAD_A()), { focusActive: true });
    await sb.loadDetail(1, true);
    await new Promise((r) => setTimeout(r, 0));
    useB = true;
    await sb.loadDetail(1, true); // payload berubah, elemen fokus ikut hilang dari DOM baru
    await new Promise((r) => setTimeout(r, 0));

    assert.ok(sb.__writes() >= 2, 'payload berubah wajib render ulang');
    assert.ok(sb.__restoredBtn.focused,
        'elemen padanan (data-action + data-submission-id stabil) difokuskan kembali setelah render');
});

// ===========================================================================
// S106 — Toggle auto-approve: indikator fokus pada slider, bukan kotak 0×0
// ===========================================================================

test('S106 (statik): rule fokus #autoAcceptToggle:focus-valid menyasar slider', () => {
    const rule = DETAIL.match(/#autoAcceptToggle:focus-visible\s*\+\s*\.pd-toggle-slider\s*\{[^}]*\}/);
    assert.ok(rule, 'rule fokus eksis di blok style pengawas_detail.html');
    assert.match(rule[0], /outline:\s*2px solid var\(--color-primary-light\)/,
        'outline memakai token warna, bukan literal');
    assert.match(rule[0], /outline-offset:\s*2px/, 'outline-offset terlihat jelas di sisi slider');
    // Slider adalah elemen visual 44×24 — bukan input 0×0 yang mustahil dilihat.
    assert.match(DETAIL, /\.pd-toggle-slider\s*\{[^}]*position:\s*absolute/,
        'target rule adalah slider yang tampak');
});

// ===========================================================================
// R111 — Angka progres 0% tanpa literal abu redup
// ===========================================================================

test('R111 (statik): #6b7280 hilang dari pengawas.html — token muted dipakai', () => {
    assert.doesNotMatch(PENGAWAS, /#6b7280/i, 'literal #6b7280 harus terhapus');
    const line = PENGAWAS.match(/var barColor[^\n]*/);
    assert.ok(line, 'ternary barColor eksis');
    assert.match(line[0], /var\(--color-text-muted\)/,
        'state 0% memakai var(--color-text-muted)');
});

// ===========================================================================
// R112 — Semua cabang hasil polling antrean mengumumkan lewat live region
// ===========================================================================

function buildApprovalsSandbox(responseFactory, opt = {}) {
    const tbody = { innerHTML: '', textContent: opt.tbodyText || 'Memuat...' };
    const live = { dataset: {}, textContent: '' };
    const toasts = [];
    const sandbox = {
        document: {
            hidden: false,
            getElementById: (id) => {
                if (id === 'approvalBody') return tbody;
                if (id === 'approvalSection') return { id };
                if (id === 'approvalOverflowNote') return { style: {}, textContent: '' };
                if (id === 'queueLiveRegion') return live;
                return null;
            },
        },
        EXAM_ID: 7,
        approvalLoading: false,
        approvalRerunPending: false,
        approvalLastSerialized: null,
        approvalRowsLive: false,
        approvalRowsCache: [],
        approvalActionBusy: false,
        approvalsErrorToasted: false,
        PengawasDetailQueue: { serializeApprovals: () => 'x' },
        apiFetch: () => Promise.resolve({ json: () => Promise.resolve(responseFactory()) }),
        showToast: (msg, type) => { toasts.push([String(msg), type]); },
        markUpdated() {},
        console: { error() {} },
        localizeUTC: () => '2026-08-25 09:00',
        escapeHtml: (s) => String(s == null ? '' : s),
        jsEscape: (s) => String(s == null ? '' : s),
        __live: live,
        __toasts: toasts,
    };
    sandbox.globalThis = sandbox;
    vm.createContext(sandbox);

    const names = ['renderApprovalNotice', 'announceQueueCount', 'buildApprovalRowHTML', 'applyApprovalRowOps', 'loadApprovals'];
    const parts = names.map((n) => {
        const fn = extractFunction(DETAIL, n);
        assert.ok(fn, 'fungsi ' + n + ' eksis di pengawas_detail.html');
        return fn;
    });
    vm.runInContext(parts.join('\n'), sandbox, { filename: 'r112-approvals' });
    return sandbox;
}

test('R112 (vm): gerbang TERTUTUP mengumumkan lewat live region', async () => {
    const sb = buildApprovalsSandbox(() => ({ success: false, message: 'Ujian tidak aktif' }));
    sb.loadApprovals();
    await new Promise((r) => setTimeout(r, 0));
    assert.doesNotMatch(sb.__live.textContent, /^\s*$/,
        'cabang ujian ditutup wajib meninggalkan pengumuman di #queueLiveRegion');
    assert.match(sb.__live.textContent, /ditutup|tidak aktif/i);
});

test('R112 (vm): gagal muat antrean mengumumkan lewat live region', async () => {
    const sb = buildApprovalsSandbox(() => ({ success: false, message: 'boom' }));
    sb.loadApprovals();
    await new Promise((r) => setTimeout(r, 0));
    assert.match(sb.__live.textContent, /gagal/i,
        'cabang gagal muat wajib mengumumkan, bukan senyap');
});

test('R112 (vm): catch jaringan mengumumkan lewat live region', async () => {
    const sb = buildApprovalsSandbox(null);
    sb.apiFetch = () => Promise.reject(new Error('offline'));
    sb.loadApprovals();
    await new Promise((r) => setTimeout(r, 0));
    assert.match(sb.__live.textContent, /gagal|mencoba lagi/i,
        'jalur catch wajib mengumumkan kegagalan jaringan');
});

test('R112 (vm): jalur sukses (kosong & berisi) tetap mengumumkan jumlah', async () => {
    const sbEmpty = buildApprovalsSandbox(() => ({ success: true, data: [], total: 0 }));
    sbEmpty.loadApprovals();
    await new Promise((r) => setTimeout(r, 0));
    assert.match(sbEmpty.__live.textContent, /Tidak ada permintaan izin/,
        'antrean kosong tetap diumumkan (perilaku S85 dipertahankan)');

    const sbData = buildApprovalsSandbox(() => ({
        success: true, total: 1,
        data: [{ mac_address: 'AA:BB', student_name: 'Budi', created_at: '2026-08-25T02:00:00Z' }],
    }));
    sbData.loadApprovals();
    await new Promise((r) => setTimeout(r, 0));
    assert.match(sbData.__live.textContent, /1 permintaan izin menunggu/,
        'antrean berisi tetap diumumkan');
});

// ===========================================================================
// R113 — Snapshot besar pindah ke variabel modul, bukan atribut DOM
// ===========================================================================

test('R113 (statik): tidak ada lagi JSON.stringify(subs) ke setAttribute data-subs', () => {
    assert.doesNotMatch(DETAIL, /setAttribute\(\s*'data-subs'/,
        'penulisan atribut data-subs tiap tick wajib dihapus');
    assert.doesNotMatch(DETAIL, /getAttribute\(\s*'data-subs'/,
        'pembacaan atribut data-subs wajib diganti variabel modul');
    assert.match(DETAIL, /var subsSnapshot\s*=/, 'variabel modul subsSnapshot eksis');
});

test('R113 (vm): showAccessLog bekerja dari snapshot modul', () => {
    const body = { innerHTML: '' };
    const modal = { style: {} };
    const sb = vm.createContext({
        document: {
            getElementById(id) {
                if (id === 'accessLogModal') return modal;
                if (id === 'accessLogBody') return body;
                return null;
            },
        },
        subsSnapshot: [{ id: '42', mac_address: 'AA:BB', student_name: 'Budi', student_class: 'X-1', exam_number: '007', submitted: true, access_logs: [], submission_history: [] }],
        localizeUTC: () => '2026-08-25 09:00',
        esc: (s) => String(s == null ? '' : s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;'),
    });
    const fn = extractFunction(DETAIL, 'showAccessLog');
    assert.ok(fn, 'showAccessLog eksis');
    vm.runInContext(fn, sb, { filename: 'r113-accesslog' });

    sb.showAccessLog('42');
    assert.match(body.innerHTML, /Budi/, 'data siswa dibaca dari snapshot modul');
    assert.match(body.innerHTML, /AA:BB/, 'MAC dibaca dari snapshot modul');
});

// ===========================================================================
// R114 — Dead CSS + simbol sprite tak terpakai dihapus (guard dua arah)
// ===========================================================================

test('R114 (statik): blok CSS mati pengawas.html terhapus', () => {
    // Asersi menyasar DEFINISI rule (selector + {), bukan sebutan dalam komentar.
    assert.doesNotMatch(PENGAWAS, /\.btn-start-pengawas\s*[,{]/, '.btn-start-pengawas tanpa pemakai — hapus');
    assert.doesNotMatch(PENGAWAS, /\.student-table\s*[,{]/, '.student-table tanpa pemakai — hapus');
    assert.doesNotMatch(PENGAWAS, /\.student-status\s*[,{]/, '.student-status tanpa pemakai di halaman ini');
    assert.doesNotMatch(PENGAWAS, /\.log-modal-header-info\s*\{/, '.log-modal-header-info milik halaman lain');
    assert.doesNotMatch(PENGAWAS, /\.log-device-row\s*\{/, '.log-device-row tanpa pemakai di halaman ini');
    assert.doesNotMatch(PENGAWAS, /\.access-log-timeline\s*[,{]/, '.access-log-timeline tanpa pemakai di halaman ini');
    assert.doesNotMatch(PENGAWAS, /\.log-identity\s[,{]/, '.log-identity tanpa pemakai di halaman ini');
    assert.doesNotMatch(PENGAWAS, /\.no-logs\s*\{/, '.no-logs tanpa pemakai di halaman ini');
    // Guard antar-batch: baris ini dijaga uiux-batch12-pengawasan-nav.test.mjs.
    assert.match(PENGAWAS, /\.log-entry\.logout::before\s*\{[^}]*var\(--color-danger-bright\)/,
        '.log-entry.logout::before dipertahankan (kontrak batch12)');
});

test('R114 (statik): blok CSS mati pengawas_detail.html terhapus', () => {
    assert.doesNotMatch(DETAIL, /\.pd-interval-(row|input|label|save)\b/,
        '.pd-interval-* tanpa pemakai di mana pun — hapus');
    assert.doesNotMatch(DETAIL, /\.pd-quick-actions/,
        '.pd-quick-actions (def + rule media query) tanpa elemen pemakai — hapus');
});

test('R114 (dua arah): sprite pengawas.html — simbol lokal semua terpakai, use href semua resolve', () => {
    const defs = new Set([...PENGAWAS.matchAll(/<symbol id="(hi-[a-z-]+)"/g)].map((m) => m[1]));
    const globalDefs = new Set([...GLOBAL_SPRITE.matchAll(/<symbol id="(hi-[a-z-]+)"/g)].map((m) => m[1]));
    const uses = [...PENGAWAS.matchAll(/<use href="#(hi-[a-z-]+)"/g)].map((m) => m[1]);

    for (const u of uses) {
        assert.ok(defs.has(u) || globalDefs.has(u), 'pemakaian #' + u + ' harus resolve ke definisi');
    }
    for (const d of defs) {
        assert.ok(uses.includes(d), 'simbol lokal #' + d + ' wajib dipakai di file ini (simbol mati = hapus)');
    }
});

test('R114 (dua arah): sprite pengawas_detail.html — simbol lokal semua terpakai, use href semua resolve', () => {
    const defs = new Set([...DETAIL.matchAll(/<symbol id="(hi-[a-z-]+)"/g)].map((m) => m[1]));
    const globalDefs = new Set([...GLOBAL_SPRITE.matchAll(/<symbol id="(hi-[a-z-]+)"/g)].map((m) => m[1]));
    const uses = [...DETAIL.matchAll(/<use href="#(hi-[a-z-]+)"/g)].map((m) => m[1]);

    for (const u of uses) {
        assert.ok(defs.has(u) || globalDefs.has(u), 'pemakaian #' + u + ' harus resolve ke definisi');
    }
    for (const d of defs) {
        assert.ok(uses.includes(d), 'simbol lokal #' + d + ' wajib dipakai di file ini (simbol mati = hapus)');
    }
});

// ===========================================================================
// R115 — Link "Pantau" tidak memicu aksi toggle kartu role="button"
// ===========================================================================

test('R115 (statik): kartu monitor membawa aria-label eksplisit', () => {
    const card = PENGAWAS.match(/<div class="exam-monitor-card"[^>]*>/);
    assert.ok(card, 'markup kartu monitor eksis');
    assert.match(card[0], /aria-label="/, 'kartu wajib aria-label eksplisit (bukan teks gabungan ambigu)');
    assert.match(card[0], /role="button"/, 'role button dipertahankan (mitigasi minimal)');
});

test('R115 (vm): klik link Pantau di-stop di fase capture — aksi kartu tidak terpicu', () => {
    // (a) handler delegasi open-pengawas-detail mundur bila klik dari <a>.
    const regIdx = PENGAWAS.indexOf("Actions.register('open-pengawas-detail'");
    assert.ok(regIdx !== -1, 'handler open-pengawas-detail terdaftar');
    const fnStart = PENGAWAS.indexOf('function', regIdx);
    let depth = 0, end = -1;
    for (let j = PENGAWAS.indexOf('{', fnStart); j < PENGAWAS.length; j++) {
        if (PENGAWAS[j] === '{') depth++;
        else if (PENGAWAS[j] === '}') { depth--; if (depth === 0) { end = j; break; } }
    }
    const handlerSrc = PENGAWAS.slice(regIdx, end + 1).replace(/^[\s\S]*?function/, 'function');
    const sb = vm.createContext({ window: { location: '' } });
    vm.runInContext('var openPengawasDetailFn = ' + handlerSrc, sb, { filename: 'r115-handler' });
    const anchor = { tagName: 'a', parentNode: null };
    const cardEl = { getAttribute: () => '9' };
    sb.openPengawasDetailFn(cardEl, { target: anchor });
    assert.equal(sb.window.location, '', 'klik dari anchor TIDAK menavigasi via aksi kartu');

    // (b) listener capture menghentikan propagasi sebelum delegasi dokumen.
    const capMatch = PENGAWAS.match(/addEventListener\('click',\s*function\s*\([^)]*\)\s*\{[\s\S]*?student-list-toggle[\s\S]*?\},\s*true\)/);
    assert.ok(capMatch, 'listener capture untuk link Pantau eksis (flag capture true)');
    assert.match(capMatch[0], /stopPropagation/, 'propagasi dihentikan agar delegasi kartu tidak jalan');
});

// ===========================================================================
// R116 — Label dwibahasa + duplikasi magic number per_page
// ===========================================================================

test('R116 (statik): label "Nilai / Score" menjadi "Nilai"', () => {
    assert.doesNotMatch(DETAIL, /Nilai \/ Score/, 'frasa dwibahasa harus hilang');
    assert.match(DETAIL, />Nilai</, 'label bahasa Indonesia tunggal dipakai');
});

test('R116 (statik): konstanta tunggal SUBS_PER_PAGE untuk kedua pemakaian', () => {
    assert.match(DETAIL, /var SUBS_PER_PAGE\s*=\s*20;/, 'konstanta SUBS_PER_PAGE dideklarasikan');
    assert.doesNotMatch(DETAIL, /per_page=20/, 'magic number di URL harus lewat konstanta');
    const urlLine = DETAIL.match(/var url = '\/admin\/api\/pengawas\/exams\/' \+ EXAM_ID[^\n]*/);
    assert.ok(urlLine && /SUBS_PER_PAGE/.test(urlLine[0]), 'pemakaian URL merujuk konstanta');
    const renderFn = extractFunction(DETAIL, 'renderSubPagination') || '';
    assert.match(renderFn, /SUBS_PER_PAGE/, 'paginasi merujuk konstanta yang sama');
    assert.doesNotMatch(renderFn, /perPage\s*=\s*20\s*;/, 'duplikat lokal perPage = 20 harus hilang');
    assert.match(DETAIL, /\(SUB_PAGE\s*-\s*1\)\s*\*\s*\(\(typeof SUBS_PER_PAGE/,
        'nomor baris juga lewat konstanta (guard typeof untuk ekstraksi suite perilaku)');
});

test('R116 (vm): kedua pemakaian merujuk SATU sumber nilai', () => {
    const renderFn = extractFunction(DETAIL, 'renderSubPagination');

    // Tanpa deklarasi modul (kondisi sandbox batch11) → fallback 20 tetap hidup.
    const pagBare = { style: {}, innerHTML: '' };
    const sbBare = vm.createContext({ document: { getElementById: () => pagBare }, Math });
    vm.runInContext(renderFn, sbBare, { filename: 'r116-bare' });
    sbBare.renderSubPagination({ page: 2, total_pages: 5, total: 100 });
    assert.match(pagBare.innerHTML, /Menampilkan 21(?:&ndash;|–)40 dari 100 perangkat/,
        'fallback 20 saat konstanta tidak terpasang');

    // Dengan konstanta terpasang → seluruh pemakaian mengikuti satu sumber.
    const pagConst = { style: {}, innerHTML: '' };
    const sbConst = vm.createContext({ document: { getElementById: () => pagConst }, Math });
    vm.runInContext('var SUBS_PER_PAGE = 50;\n' + renderFn, sbConst, { filename: 'r116-const' });
    sbConst.renderSubPagination({ page: 2, total_pages: 2, total: 100 });
    assert.match(pagConst.innerHTML, /Menampilkan 51(?:&ndash;|–)100 dari 100 perangkat/,
        'perubahan konstanta memengaruhi perhitungan paginasi');
});

// ===========================================================================
// R99 — Tombol Detail submissions setara sentuh dengan Hapus (44px)
// ===========================================================================

test('R99 (statik): kedua tombol aksi submissions min-height ≥44px', () => {
    const detailBtn = SUBMISSIONS.match(/<button class="btn-sm btn-toggle" data-action="show-submission-detail"[^>]*>/);
    assert.ok(detailBtn, 'tombol Detail eksis');
    const mh = detailBtn[0].match(/min-height:\s*(\d+)px/);
    assert.ok(mh && parseInt(mh[1], 10) >= 44, 'tombol Detail min-height ≥44px (dapat: ' + (mh && mh[1]) + ')');

    const delBtn = SUBMISSIONS.match(/<button class="btn-sm btn-delete" data-action="delete-submission"[^>]*>/);
    assert.ok(delBtn && /min-height:\s*44px/.test(delBtn[0]), 'tombol Hapus tetap 44px (acuan)');
});
