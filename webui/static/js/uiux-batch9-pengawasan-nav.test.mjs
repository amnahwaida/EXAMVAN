/* Contract tests Batch 9 UI/UX — halaman pengawasan + partials/nav.
 * Referensi temuan: review_uiux_webui.md §5.6 RE-REVIEW RONDE 3
 * (ID: T14, S38, S41-sisi-pengawas_detail, R29-sisi-nav, R31-sisa).
 *
 *   - T14 : #toastContainer dipindah ke partials/nav.html (satu sumber untuk
 *           semua halaman admin); duplikat per-halaman dihapus dari
 *           pengawas_detail.html & submissions.html (dashboard/settings
 *           diserahkan agen lain). login.html tidak memuat partial ini.
 *   - S38 : ambang warna nilai submissions.html disamakan dengan semantik
 *           halaman publik hasil.html: ≥70 sukses / 40–69 warning / <40 danger.
 *   - S41 : copyToken lokal di pengawas_detail.html dihapus — pemanggil menuju
 *           copyCode() guarded versi admin-core.js (fallback execCommand +
 *           toast gagal). String navigator.clipboard tak boleh ada lagi di
 *           inline template milik batch ini.
 *   - R29 : onclick inline tersisa di partials/nav.html dihapus (hamburger
 *           dilayani initMenuToggle() admin-core.js; "Ubah Password" via
 *           data-action terdaftar di blok script nav; overlay onboarding
 *           tanpa stopPropagation — tidak ada jalur yang menutupnya).
 *   - R31 : feedback harian — toast persetujuan/penolakan menyertakan nama
 *           siswa; jadwal pengawas.html diformat formatDateTimeID.
 *
 * Gaya: fs-read statik + vm.runInNewContext untuk perilaku JS inline
 * (pola uiux-batch6-pengawasan.test.mjs / uiux-batch9-settings.test.mjs).
 *
 * Run: node --test webui/static/js/uiux-batch9-pengawasan-nav.test.mjs
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

const NAV = 'templates/admin/partials/nav.html';
const PENGAWAS = 'templates/admin/pengawas.html';
const DETAIL = 'templates/admin/pengawas_detail.html';
const SUBMISSIONS = 'templates/admin/submissions.html';
const LOGIN = 'templates/admin/login.html';

const TOAST_MARKUP =
    '<div class="toast-container" id="toastContainer" aria-live="polite" aria-atomic="true"></div>';

// ===========================================================================
// T14 — satu sumber #toastContainer di partials/nav.html
// ===========================================================================

test('T14/static: nav.html memiliki tepat SATU #toastContainer dengan aria-live="polite"', () => {
    const html = read(NAV);
    const n = (html.match(/id="toastContainer"/g) || []).length;
    assert.equal(n, 1, `nav.html harus tepat satu #toastContainer (dapat ${n})`);
    assert.ok(html.includes(TOAST_MARKUP),
        'markup container identik dengan versi per-halaman lama (class/aria utuh)');
});

test('T14/static: duplikat #toastContainer dihapus dari template milik batch ini', () => {
    for (const rel of [PENGAWAS, DETAIL, SUBMISSIONS]) {
        const html = read(rel);
        assert.doesNotMatch(html, /id="toastContainer"/,
            `${rel} tidak boleh lagi menduplikasi #toastContainer — disediakan nav.html`);
    }
});

test('T14/static: semua halaman admin yang memuat admin-core.js memuat partials/nav.html (container selalu tersedia)', () => {
    // nav.html dirender oleh setiap halaman admin kecuali login — pastikan
    // kontrak itu sehingga showToast tidak pernah no-op lagi (T14/S23).
    const pages = ['dashboard.html', 'settings.html', 'pengawas.html', 'pengawas_detail.html', 'submissions.html'];
    assert.match(read(NAV), /toastContainer/, 'sanity: nav membawa container');
    for (const p of pages) {
        const html = read(`templates/admin/${p}`);
        assert.match(html, /admin\/partials\/nav\.html/, `${p} memuat partials/nav.html`);
        assert.match(html, /admin-core\.js/, `${p} memuat admin-core.js (pemakai showToast)`);
    }
});

test('T14/static: login.html tidak memuat partials/nav.html & tetap bebas toastContainer', () => {
    const html = read(LOGIN);
    assert.doesNotMatch(html, /partials\/nav\.html/, 'login tidak memuat nav admin');
    assert.doesNotMatch(html, /id="toastContainer"/, 'login tak terdampak T14');
});

// ===========================================================================
// S38 — ambang warna nilai submissions ↔ halaman publik (70/40)
// ===========================================================================

test('S38/static: submissions.html pakai ambang 70 (sukses) / 40–69 (warning) / <40 (danger) + komentar penanda', () => {
    const html = read(SUBMISSIONS);
    assert.ok(html.includes('{{if ge $scorePct 70.0}}'),
        'ambang sukses wajib ≥70 (menyamakan chip "Lulus" halaman publik)');
    assert.ok(html.includes('{{else if ge $scorePct 40.0}}'),
        'ambang warning wajib ≥40 (rentang 40–69 kuning, sama dengan publik)');
    assert.doesNotMatch(html, /ge \$scorePct 80\.0/,
        'ambang lama 80 tidak boleh tersisa (kontradiksi dengan publik)');
    assert.doesNotMatch(html, /ge \$scorePct 60\.0/,
        'ambang warning lama 60 tidak boleh tersisa');
    // Komentar penanda ambang agar tidak drift lagi — harus menyebut 70 dan
    // konteks kontrak publik.
    const marker = html.match(/\{\{\/\*[^*]*[Aa]mbang[^*]*\*\/\}\}/);
    assert.ok(marker, 'komentar penanda ambang ada di dekat blok warna nilai');
    assert.match(marker[0], /70/, 'penanda menyebut ambang 70');
    assert.match(marker[0], /publik|hasil\.html|Lulus/i,
        'penanda merujuk semantik halaman publik (chip Lulus mulai 70)');
});

// ===========================================================================
// S41-sisi-pengawas_detail — hapus copyToken lokal, panggil copyCode guarded
// ===========================================================================

test('S41/static: inline template milik batch bebas navigator.clipboard & definisi copyToken lokal', () => {
    for (const rel of [DETAIL, PENGAWAS, SUBMISSIONS]) {
        const html = read(rel);
        assert.doesNotMatch(html, /navigator\.clipboard/,
            `${rel}: navigator.clipboard tidak boleh ada di inline template (copyCode core yang guarded)`);
    }
    const html = read(DETAIL);
    assert.doesNotMatch(html, /function\s+copyToken\s*\(/,
        'definisi lokal copyToken dihapus (dulu menimpa versi hardening S29)');
    assert.match(html, /data-action="copy-active-token"/,
        'pemicu salin token tetap ada via delegasi data-action');
});

test('S41/vm: handler copy-active-token meneruskan token ke copyCode global (fallback aman tanpa copyToken)', () => {
    const html = read(DETAIL);
    // Blok registrasi Batch 7 (R28) milik pengawas_detail.html.
    const regBlock = html.match(/\/\/ FOLLOW-UP[\s\S]*?\(function\(\)\s*\{[\s\S]*?\n\}\)\(\);/);
    assert.ok(regBlock, 'blok registrasi Actions pengawas_detail bisa diekstrak');

    const registered = {};
    let copied = null;
    let tokenText = 'TOK9X2Q';
    const sandbox = {
        Actions: { register(n, fn) { registered[n] = fn; } },
        document: { getElementById(id) { return id === 'pdActiveToken' ? { textContent: tokenText } : null; } },
        copyCode(t) { copied = t; },
        parseInt,
    };
    sandbox.globalThis = sandbox;
    vm.runInNewContext(regBlock[0], sandbox);

    assert.equal(typeof registered['copy-active-token'], 'function',
        'copy-active-token tetap terdaftar');
    registered['copy-active-token']();
    assert.equal(copied, 'TOK9X2Q', 'token diteruskan ke copyCode (guarded core)');

    // Guard em-dash/placeholder: elemen "—" tidak boleh disalin.
    copied = null;
    tokenText = '—';
    registered['copy-active-token']();
    assert.equal(copied, null, 'placeholder "—" tidak disalin');
});

// ===========================================================================
// R29-sisi-nav — onclick inline di partials/nav.html habis
// ===========================================================================

test('R29/static: partials/nav.html bebas atribut onclick', () => {
    const html = read(NAV);
    const n = (html.match(/\sonclick=/gi) || []).length;
    assert.equal(n, 0, `nav.html masih punya ${n} onclick — migrasikan ke mekanisme CSP-safe`);
});

test('R29/static: hamburger kehilangan onclick tapi a11y & wiring core dipertahankan', () => {
    const btn = read(NAV).match(/<button[^>]*id="menuToggleBtn"[^>]*>/);
    assert.ok(btn, '#menuToggleBtn ada');
    assert.doesNotMatch(btn[0], /onclick=/, 'hamburger tanpa onclick inline');
    assert.match(btn[0], /aria-haspopup="true"/);
    assert.match(btn[0], /aria-expanded="false"/);
    // Kontrak yang DIKONSUMSI dari agen lain (admin-core): initMenuToggle
    // memasang toggle+stopPropagation+sinkronisasi aria-expanded.
    const core = read('static/js/admin-core.js');
    assert.match(core, /getElementById\('menuToggleBtn'\)/,
        'admin-core.initMenuToggle tetap menjadi satu-satunya pengikat hamburger');
    assert.match(core, /menuToggle\.onclick[\s\S]{0,200}stopPropagation/,
        'wiring core mempertahankan stopPropagation + toggle dropdown');
});

test('R29/static: "Ubah Password" memakai data-action yang diregistrasi di blok script nav.html sendiri', () => {
    const html = read(NAV);
    const btn = html.match(/<button[^>]*>[^<]*<svg[^>]*hi-key[^>]*>[\s\S]*?<\/button>/);
    const pwBtn = btn || html.match(/openChangePasswordModal/);
    assert.ok(pwBtn, 'tombol Ubah Password ada');
    assert.doesNotMatch(html, /onclick="openChangePasswordModal\(\)"/,
        'onclick inline openChangePasswordModal dihapus');
    assert.match(html, /data-action="open-change-password-modal"/,
        'tombol memakai delegasi data-action');
    assert.match(html, /Actions\.register\(\s*'open-change-password-modal'/,
        'registrasi kanonik hidup di blok script nav.html (core dimuat duluan di halaman)');
});

test('R29/static: overlay onboarding tanpa stopPropagation & tanpa jalur dismiss backdrop', () => {
    const html = read(NAV);
    const overlay = html.match(/<div[^>]*id="instansiOnboardingModal"[^>]*>/);
    assert.ok(overlay, '#instansiOnboardingModal ada');
    assert.doesNotMatch(overlay[0], /onclick=/, 'overlay tanpa onclick stopPropagation');
    const card = html.match(/<div[^>]*class="modal-card"[^>]*style="max-width: 480px[^"]*"[^>]*>/);
    if (card) assert.doesNotMatch(card[0], /onclick=/, 'kartu modal tanpa onclick stopPropagation');
    // Modal WAJIB diisi — tidak boleh bisa ditutup lewat klik backdrop.
    assert.doesNotMatch(html, /instansiOnboardingModal[\s\S]{0,400}?data-action="modal-dismiss"/,
        'onboarding wajib tidak punya dismiss backdrop');
});

test('R29/vm: handler open-change-password-modal memanggil fungsi global openChangePasswordModal', () => {
    const html = read(NAV);
    const m = html.match(/\(function\s*\(\)\s*\{[\s\S]*?Actions\.register\(\s*'open-change-password-modal'[\s\S]*?\}\)\(\);/);
    assert.ok(m, 'blok registrasi nav.html bisa diekstrak');

    const registered = {};
    let opened = 0;
    const sandbox = {
        Actions: { register(n, fn) { registered[n] = fn; }, has: (n) => Object.prototype.hasOwnProperty.call(registered, n) },
        document: { addEventListener() {} },
        setTimeout,
        openChangePasswordModal() { opened += 1; },
    };
    sandbox.globalThis = sandbox;
    vm.runInNewContext(m[0], sandbox);
    assert.equal(typeof registered['open-change-password-modal'], 'function',
        'handler terdaftar walau Actions baru tersedia belakangan');
    registered['open-change-password-modal']();
    assert.equal(opened, 1, 'klik tombol meneruskan ke modal Ubah Password');
});

// ===========================================================================
// R31-sisa — feedback harian pengawasan
// ===========================================================================

test('R31/static: toast persetujuan/penolakan menyertakan nama siswa (student_name antrean)', () => {
    const html = read(DETAIL);
    assert.match(html, /a\.student_name/,
        'baris antrean membawa student_name (data mentah tetap dipakai)');
    // Helper pencarian nama per-mac harus ada dan dipakai oleh setApproval.
    assert.match(html, /function\s+\w*[Nn]ame[Bb]y[Mm]ac\s*\(|function\s+findApprovalStudentName\s*\(/,
        'helper pencarian nama siswa berdasarkan mac_address ada');
    const src = html.slice(html.indexOf('function setApproval'));
    const successBranch = src.slice(0, src.indexOf('} else {'));
    const toastCall = successBranch.match(/showToast\([^;]*\);/);
    assert.ok(toastCall, 'toast cabang sukses setApproval bisa diekstrak');
    assert.match(toastCall[0], /nameByMac\(|studentName|sName/,
        'pesan toast memakai nama siswa hasil lookup mac_address');
    assert.doesNotMatch(toastCall[0], /'error'/, 'kontrak R24 (info utk tolak) tetap utuh');
    assert.match(toastCall[0], /'info'/, 'kontrak R24 (info utk tolak) tetap utuh');
});

test('R31/vm: setApproval sukses → toast menyebut nama siswa ("Perangkat {nama} diizinkan/ditolak")', () => {
    const html = read(DETAIL);
    // Fungsi helper lookup nama + fungsi setApproval diekstrak terpisah agar
    // tahan refactor di sekitarnya (pola extract-function batch6).
    const helperM = html.match(/function\s+findApprovalStudentName\s*\([\s\S]*?\n\}/);
    assert.ok(helperM, 'fungsi lookup nama bisa diekstrak dari pengawas_detail.html');
    const setM = html.match(/function\s+setApproval\s*\([\s\S]*?\n\}/);
    assert.ok(setM, 'fungsi setApproval bisa diekstrak dari pengawas_detail.html');

    const toasts = [];
    const sandbox = {
        EXAM_ID: 1,
        approvalActionBusy: false,
        apiFetch() {
            return Promise.resolve({ json: () => Promise.resolve({ success: true }) });
        },
        encodeURIComponent,
        showToast(msg) { toasts.push(msg); },
        loadApprovals() {},
        loadDetail() {},
        console,
    };
    sandbox.globalThis = sandbox;
    vm.createContext(sandbox);
    // Cache antrean + flag .finally + helper + setApproval berbagi SATU context.
    vm.runInContext(`
        var approvalRerunPending = false;
        var approvalRowsCache = [
            { mac_address: 'AA:BB:CC:DD:01', student_name: 'Budi' },
            { mac_address: 'AA:BB:CC:DD:02', student_name: '' }
        ];
    `, sandbox);
    vm.runInContext(helperM[0], sandbox);
    vm.runInContext(setM[0], sandbox);

    // Promise internal vm context tidak selalu menyatu dengan await host —
    // poll sampai toast masuk (pola aman lintas-context).
    async function until(cond, timeoutMs = 1000) {
        const start = Date.now();
        while (!cond() && Date.now() - start < timeoutMs) {
            await new Promise((r) => setTimeout(r, 5));
        }
    }

    // NB: setApproval asli fire-and-forget (tidak mengembalikan promise);
    // hasil diamati lewat stub showToast dengan polling.
    return (async () => {
        sandbox.setApproval('AA:BB:CC:DD:01', 'approved', true);
        await until(() => toasts.length > 0);
        assert.deepEqual(toasts, ['Perangkat Budi diizinkan'],
            'persetujuan menyertakan nama siswa dari baris antrean');

        toasts.length = 0;
        sandbox.setApproval('AA:BB:CC:DD:01', 'rejected', true);
        await until(() => toasts.length > 0);
        assert.deepEqual(toasts, ['Perangkat Budi ditolak'],
            'penolakan juga menyertakan nama siswa');

        // Nama kosong/anonim → fallback pesan generik (tanpa "undefined").
        toasts.length = 0;
        sandbox.setApproval('AA:BB:CC:DD:02', 'rejected', true);
        await until(() => toasts.length > 0);
        assert.deepEqual(toasts, ['Perangkat ditolak'],
            'tanpa nama siswa jatuh ke pesan generik, bukan "undefined"');
    })();
});

test('R31/static: jadwal pengawas.html diformat formatDateTimeID (bukan UTC mentah), null-safe ke "—"', () => {
    const html = read(PENGAWAS);
    assert.match(html, /formatDateTimeID\(ex\.start_time\)/,
        'start_time diformat via satu-pintu core (R28/R31)');
    assert.match(html, /formatDateTimeID\(ex\.end_time\)/,
        'end_time diformat via satu-pintu core');
    assert.match(html, /\?\s*\(?\s*formatDateTimeID\(ex\.start_time\)[\s\S]{0,120}:\s*'—'/,
        'jadwal null tetap jatuh ke "—"');
    assert.doesNotMatch(html, /ex\.start_time\s*\+\s*'\s+-\s+'/,
        'konkatenasi UTC mentah lama tidak boleh tersisa');
});
