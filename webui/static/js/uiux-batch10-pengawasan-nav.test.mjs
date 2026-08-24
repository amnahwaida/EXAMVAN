/* Contract + behavior tests untuk Batch 10 — area pengawasan & nav (milik
 * koordinator; agen batch-10-pengawasan-nav terputus, dikerjakan langsung).
 * Referensi: review_uiux_webui.md bagian 5.7 RE-REVIEW RONDE 4 —
 * T18(nav), R52, S48, S50, S52, R45, R46, R49, R53, S49, R50.
 *
 * Run with: node --test static/js/uiux-batch10-pengawasan-nav.test.mjs (from webui/)
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
const PENGAWAS = read('templates/admin/pengawas.html');
const DETAIL = read('templates/admin/pengawas_detail.html');
const SUBMISSIONS = read('templates/admin/submissions.html');
const SUBMISSIONS_GO = read('internal/handlers/admin/submissions.go');
const CORE = read('static/js/admin-core.js');

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
// T18 (sisi nav) — gradien submit instansi lolos AA via token
// ===========================================================================

test('T18 (statik): nav.html tak lagi memakai endpoint gradien gagal-AA (#a855f7/#6366f1)', () => {
    assert.doesNotMatch(NAV, /#a855f7|#6366f1/,
        'endpoint lama dilarang — pakai var(--grad-btn-violet-start/end)');
    assert.match(NAV, /var\(--grad-btn-violet-start\)[\s\S]{0,40}var\(--grad-btn-violet-end\)/,
        'tombol submit instansi memakai token gradien AA dari theme.css');
});

test('T18 (statik): token --grad-btn-* terdefinisi di theme.css dengan nilai terkunci', () => {
    const theme = read('static/css/theme.css');
    assert.match(theme, /--grad-btn-violet-start:\s*#9333ea\s*;/);
    assert.match(theme, /--grad-btn-violet-end:\s*#7c3aed\s*;/);
    assert.match(theme, /--grad-btn-blue-start:\s*#2563eb\s*;/);
    assert.match(theme, /--grad-btn-blue-end:\s*#1d4ed8\s*;/);
});

// ===========================================================================
// R52 — ekor arwah nav.html: z-index token & skip-link class
// ===========================================================================

test('R52 (statik): onboarding modal memakai var(--z-onboarding) (di bawah toast); skip-link tanpa inline style', () => {
    assert.match(read('static/css/theme.css'), /--z-onboarding:\s*10001\s*;/,
        '--z-onboarding harus 10001 (< toast 10002) sesuai kontrak R52');
    assert.doesNotMatch(NAV, /z-index:\s*99999/, 'literal 99999 dilarang di nav.html');
    assert.match(NAV, /z-index:\s*var\(--z-onboarding\)/);
    const skip = NAV.match(/<a href="#mainContent"[^>]*>/);
    assert.ok(skip, 'skip-link nav ada');
    assert.doesNotMatch(skip[0], /style=/, 'skip-link memakai class .skip-link, bukan inline style');
    assert.match(skip[0], /class="[^"]*skip-link/);
});

// ===========================================================================
// S48 — pencarian peserta live-search, bukan Enter-only
// ===========================================================================

test('S48 (statik): pengawas_detail bebas onkeyup/onchange inline; initLiveSearch di-wire ke input peserta', () => {
    assert.doesNotMatch(DETAIL, /onkeyup=/, 'Enter-only search dihapus');
    assert.doesNotMatch(DETAIL, /onchange=/, 'select filter onchange inline dimigrasi');
    const coreInit = extractFunction(CORE, 'initLiveSearch');
    assert.ok(coreInit, 'initLiveSearch tersedia di core');
});

test('S48 (perilaku): initLiveSearch ter-wire ke #pengawasSearch dengan callback loadDetail', async () => {
    // Eksekusi blok wiring halaman dalam sandbox dan pastikan initLiveSearch
    // dipanggil dengan elemen input yang benar.
    const calls = [];
    const sandbox = {
        window: {},
        document: {
            getElementById: (id) => (id === 'pengawasSearch' ? { id, _live: true } : null),
            addEventListener() {}
        },
        initLiveSearch: (inputEl, cb) => calls.push({ inputEl, cb })
    };
    sandbox.window.initLiveSearch = sandbox.initLiveSearch;
    sandbox.globalThis = sandbox;
    const marker = 'Batch 10 (S48):';
    const at = DETAIL.indexOf(marker);
    assert.ok(at !== -1, 'blok komentar penunjuk S48 ada di pengawas_detail.html');
    const start = DETAIL.lastIndexOf('//', at);
    const endAnchor = "statusFilterEl.addEventListener('change', function() { loadDetail(1); });";
    const end = DETAIL.indexOf(endAnchor, start);
    assert.ok(end !== -1, 'wiring select filter ada di blok yang sama');
    const block = DETAIL.slice(start, end + endAnchor.length).replace(/^\s*\/\/.*$/gm, '');
    vm.createContext(sandbox);
    vm.runInContext(block, sandbox);
    assert.equal(calls.length, 1, 'initLiveSearch dipanggil tepat sekali saat init');
    assert.equal(calls[0].inputEl.id, 'pengawasSearch');
});

// ===========================================================================
// S50 — modal Izinkan/Tolak menyebut identitas siswa
// ===========================================================================

test('S50 (perilaku): showConfirmApprovalModal menampilkan nama siswa + MAC di teks konfirmasi', () => {
    const fnSrc =
        "var escapeHtml = function(s){ return String(s == null ? '' : s).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/\"/g,'&quot;'); };\n" +
        "var findApprovalStudentName = function(mac){ return mac==='AA:BB' ? 'Budi' : null; };\n" +
        extractFunction(DETAIL, 'findApprovalStudentName') + '\n' +
        extractFunction(DETAIL, 'formatApprovalStudentLabel') + '\n' +
        extractFunction(DETAIL, 'showConfirmApprovalModal');
    const byId = {};
    const el = (props) => Object.assign({ style: {}, className: '', textContent: '', innerHTML: '' }, props);
    const title = el(), text = el(), btn = el();
    byId['confirmApprovalTitle'] = title;
    byId['confirmApprovalText'] = text;
    byId['confirmApprovalBtn'] = btn;
    const modal = { classList: { add() {}, remove() {} }, style: {} };
    byId['confirmApprovalModal'] = modal;
    const doc = { getElementById: (id) => byId[id] || null };
    const sandbox = { document: doc };
    vm.createContext(sandbox);
    vm.runInContext(fnSrc, sandbox, { filename: 's50' });

    sandbox.showConfirmApprovalModal('AA:BB', 'approved');
    assert.match(text.innerHTML, /<strong>Budi<\/strong>/, 'nama siswa tampil di teks konfirmasi');
    assert.match(text.innerHTML, /AA:BB/, 'MAC ikut ditampilkan');
    sandbox.showConfirmApprovalModal('CC:DD', 'rejected');
    assert.doesNotMatch(text.innerHTML, /<strong><\/strong>/, 'fallback anonim tidak menghasilkan strong kosong');
    assert.match(text.innerHTML, /menolak/i);
});

// ===========================================================================
// S52 — toggle auto-approve wajib konfirmasi saat meng-AKTIFKAN
// ===========================================================================

test('S52 (statik): handler change auto-approve memakai showConfirm untuk state aktif', () => {
    assert.match(DETAIL, /showConfirm\(/, 'showConfirm dipakai di pengawas_detail');
    const aaBlock = DETAIL.match(/aaToggle\.addEventListener\('change'[\s\S]{0,900}?\}\);/);
    assert.ok(aaBlock, 'handler change auto-approve ada');
    assert.match(aaBlock[0], /showConfirm\(/, 'aktivasi auto-approve lewat dialog konfirmasi');
});

// ===========================================================================
// R45 — toast error fetch bukan exception mentah
// ===========================================================================

test('R45 (statik): tidak ada penyambungan objek err mentah ke showToast', () => {
    assert.doesNotMatch(DETAIL, /showToast\([^)]*\+\s*err\b/,
        '"Gagal menghubungi server: " + err menghasilkan [object TypeError]');
    const count = (DETAIL.match(/Gagal menghubungi server\. Periksa koneksi\./g) || []).length;
    assert.ok(count >= 2, `pesan statis ramah dipakai di jalur mulai/hentikan pengawasan (ditemukan ${count})`);
});

// ===========================================================================
// R53 — sisa terakhir #64748b di templates/ dihapus
// ===========================================================================

test('R53 (statik): #64748b nol di seluruh templates/', () => {
    for (const [rel, src] of [['templates/admin/pengawas_detail.html', DETAIL], ['templates/admin/partials/nav.html', NAV], ['templates/admin/pengawas.html', PENGAWAS]]) {
        assert.doesNotMatch(src, /#64748b/i, `${rel} bebas #64748b`);
    }
});

// ===========================================================================
// R46 — badge tombstoned: escape + format waktu satu pintu
// ===========================================================================

test('R46 (statik): title tombstoned di-escape dan timestamp diformat formatDateTimeID', () => {
    const m = PENGAWAS.match(/var tombTitle = [\s\S]{0,400}?statusHtml = [^\n]*;/);
    assert.ok(m, 'pembentukan tombTitle + statusHtml ada');
    assert.match(m[0], /formatDateTimeID\(ex\.tombstoned_at\)/, 'timestamp UTC mentah dilarang');
    assert.match(m[0], /escapeHtml\(jsEscape\(tombTitle\)\)/, 'string atribut wajib lolos dua layer escaping');
});

// ===========================================================================
// R49 — error state daftar pengawasan punya aksi pemulihan
// ===========================================================================

test('R49 (statik): kedua state error daftar pengawasan menyediakan tombol Coba Lagi', () => {
    const fails = [...PENGAWAS.matchAll(/Gagal memuat data|Gagal menghubungi server/g)];
    assert.ok(fails.length >= 2, 'dua state error ada');
    for (const f of fails) {
        const ctx = PENGAWAS.slice(Math.max(0, f.index - 60), f.index + 260);
        assert.match(ctx, /Coba Lagi/, `state error "${f[0]}" harus punya tombol pemulihan`);
    }
});

// ===========================================================================
// S49 — kartu info ujian submissions pakai WIB; kelas mati .utc-date dihapus
// ===========================================================================

test('S49 (statik): submissions.go mengonversi waktu ke WIB; .utc-date hilang dari template', () => {
    assert.doesNotMatch(SUBMISSIONS, /utc-date/);
    assert.match(SUBMISSIONS_GO, /Asia\/Jakarta|FixedZone\("WIB"/,
        'konversi zona WIB harus eksplisit di handler');
    // Setiap Format layout kartu info wajib lewat konversi .In(jakartaLoc).
    const fmts = [...SUBMISSIONS_GO.matchAll(/^[^\n]*\.Format\("2006-01-02 15:04"\)/gm)];
    assert.ok(fmts.length >= 2, 'helper format WIB ada');
    for (const f of fmts) {
        assert.match(f[0], /In\(jakartaLoc\)\.Format/,
            'Format tanpa konversi zona dilarang: ' + f[0].trim());
    }
});

// ===========================================================================
// R50 — sisa EN "Refresh" di pengawas_detail
// ===========================================================================

test('R50 (statik): pengawas_detail bebas title "Refresh"', () => {
    assert.doesNotMatch(DETAIL, /title="Refresh"/, 'gunakan "Muat Ulang"');
});
