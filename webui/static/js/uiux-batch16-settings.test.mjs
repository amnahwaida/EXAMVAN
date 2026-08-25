/**
 * ══════════════════════════════════════════════════════════════════════════
 * Suite UI/UX BATCH 16 — SETTINGS (agen batch16-settings)
 * ══════════════════════════════════════════════════════════════════════════
 *
 * Referensi: review_uiux_webui.md bagian 5.13 RE-REVIEW RONDE 10 (basis
 * 616132a, pasca Batch 15) + gerbang "5 item tertinggal Batch 15" (S92,
 * R102, R103, R104, R110 — tercatat [x] padahal luput dari pembagian tugas
 * agen Batch 15, status dikoreksi menjadi [ ] TERTINGGAL).
 *
 * Cakupan temuan milik agen ini:
 *
 *   S92 — viewRedemptions melakukan fetch tanpa sequence-token (kelas race
 *         S78 yang kelima): klik "(Lihat User)" voucher A lalu cepat ke
 *         voucher B membuat isi modal milik respons yang mendarat
 *         TERAKHIR — bukan milik tombol terakhir. loadApps/loadPackages
 *         juga fetch-tanpa-token (dampak rendah, tapi pola yang sama).
 *         Kontrak: token monoton redemptionSeq + guard "seq !== token →
 *         return" di THEN dan CATCH viewRedemptions; loadApps memakai
 *         appLoadSeq dan loadPackages memakai packageLoadSeq dengan guard
 *         di jalur sukses maupun gagal. Perilaku vm membuktikan respons
 *         basi redemptions diabaikan.
 *
 *   R110 — Paginasi Daftar Voucher tidak menandai halaman aktif maupun
 *         memberi nama aksesibel pada tombol halaman (paritas dengan
 *         settings-voucher-audit.js renderAuditPagination yang benar).
 *         Kontrak: setiap tombol paginasi vouchers punya aria-label
 *         "Halaman N" dan tepat satu tombol (halaman aktif) membawa
 *         aria-current="page".
 *
 *   R102 — Paritas bahasa UI: sisa label EN "Generate Batch" /
 *         "Batch Generate" dan typo "Kesini". Kontrak: settings-vouchers.js
 *         dan settings.html BEBAS frasa tersebut — tombol/judul massal
 *         memakai "Buat Massal" ("Buat Massal Voucher"); toast gagal juga
 *         berbahasa Indonesia; drop-area unggahan aplikasi memakai ejaan
 *         benar "Pilih atau Seret File Ke Sini" di template DAN modul JS.
 *
 *   R103 — Header tabel vouchers/riwayat/packages tanpa scope="col":
 *         pembaca layar tidak bisa mengasosiasikan sel dengan kolomnya
 *         (acuan benar: tabel Kelola User). Kontrak: SEMUA th di ketiga
 *         tabel itu membawa scope="col", dan tiap tabel punya caption
 *         sr-only sebagai nama aksesibel.
 *
 *   R104 — durationText diinterpolasi mentah ke markup: durasi kustom
 *         berasal dari input admin bebas sehingga nilai ber-tag HTML
 *         menyusup tanpa escape. Kontrak: assignment durationText dibungkus
 *         escapeHtml(String(...)) dan tidak ada lagi interpolasi mentah
 *         v.duration_type; perilaku vm membuktikan payload ber-tag aman.
 *
 *   S107 — Branch window.loadSectionScript di openUploadModalSafe SELALU
 *         mati (fungsi tak pernah diekspor): fallback memuat modul tanpa
 *         cache-buster "?v=" dan tanpa menandai __settingsLoaded, sehingga
 *         deploy menyajikan modul basi dan aktivasi tab berikutnya bisa
 *         memuat ulang modul (listener ganda). Kontrak: dispatcher TIDAK
 *         lagi mereferensikan loadSectionScript; jalur tunggal fallback
 *         memakai src ".../settings-system-apps.js?v={{.version}}" DAN
 *         menandai window.__settingsLoaded['system-apps'] = true sebelum
 *         script ditambahkan ke head (diverifikasi perilaku vm).
 *
 *   S108 — Klik tombol "Muat Ulang" di head kartu Daftar User ikut
 *         melipat kartu: handler toggle head tidak memeriksa target,
 *         stopPropagation delegasi Actions datang terlambat (fase bubble).
 *         Kontrak: guard awal toggle "e.target.closest('[data-action]')"
 *         — perilaku vm: klik elemen ber-data-action TIDAK men-toggle;
 *         klik area lain head tetap men-toggle.
 *
 *   R129 — Emoji ⚙️ pada opsi/judul grup kustom voucher (noise screen
 *         reader) dan h3#uploadModalTitle berisi div ikon (content model
 *         invalid — div tak boleh jadi anak h3). Kontrak: settings.html
 *         bebas karakter ⚙; segmen h3 uploadModalTitle bebas "<div".
 *
 * Kepemilikan file agen ini: templates/admin/settings.html,
 *   static/js/settings-vouchers.js, static/js/settings-packages.js,
 *   static/js/settings-system-apps.js, dan suite ini sendiri.
 *   (S108 menyentuh perilaku lewat static/js/settings-users.js sesuai
 *   penugasan koordinator Batch 16.)
 *
 * Catatan kalibrasi:
 *   - Baris 'Generate Batch' (:309/:315/:320) ada di settings-vouchers.js,
 *     BUKAN settings-packages.js seperti tertulis di lembar penugasan —
 *     kepemilikan tetap sah karena kedua file milik agen ini.
 *   - uiux-batch10-settings.test.mjs milik agen lain masih mengunci string
 *     typo lama "Pilih atau Seret File Kesini" (test R48); setelah koreksi
 *     R102 asersi tsb perlu dikalibrasi pemiliknya ke "…Ke Sini".
 *
 * Run with:  node --test static/js/uiux-batch16-settings.test.mjs   (from webui/)
 */

import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';

const HERE = path.dirname(fileURLToPath(import.meta.url));
const WEBUI_ROOT = path.join(HERE, '..', '..');
const read = (...p) => fs.readFileSync(path.join(WEBUI_ROOT, ...p), 'utf8');

const SETTINGS_HTML = read('templates', 'admin', 'settings.html');
const VOUCHERS_JS = read('static', 'js', 'settings-vouchers.js');
const PACKAGES_JS = read('static', 'js', 'settings-packages.js');
const SYSTEM_APPS_JS = read('static', 'js', 'settings-system-apps.js');
const USERS_JS = read('static', 'js', 'settings-users.js');

/** Ambil badan fungsi top-level (`function`/`async function`) NAME sampai deklarasi berikutnya. */
function functionBody(src, name) {
    const start = src.indexOf(`function ${name}(`);
    assert.ok(start !== -1, `function ${name} ditemukan`);
    const end = src.indexOf('\nfunction ', start + 1);
    return src.slice(start, end === -1 ? undefined : end);
}

/** Elemen DOM palsu generik untuk sandbox vm. */
function fakeEl() {
    const el = {
        innerHTML: '',
        textContent: '',
        style: { display: '', color: '' },
        attrs: {},
        listeners: {},
        dataset: {},
        setAttribute(k, v) { el.attrs[k] = String(v); },
        getAttribute(k) { return k in el.attrs ? el.attrs[k] : null; },
        removeAttribute(k) { delete el.attrs[k]; },
        addEventListener(t, f) { (el.listeners[t] = el.listeners[t] || []).push(f); },
        classList: {
            add() {}, remove() {}, toggle() {}, contains() { return false; },
        },
        appendChild() {},
    };
    return el;
}

/** Sandbox vm yang sudah cukup untuk mengeksekusi utuh settings-vouchers.js. */
function buildVouchersSandbox() {
    const byId = {};
    const sandbox = {
        document: {
            getElementById(id) { if (!byId[id]) byId[id] = fakeEl(); return byId[id]; },
            querySelector() { return null; },
            querySelectorAll() { return []; },
            addEventListener() {},
        },
        window: { __settingsReady: {}, __voucherSubtabsWired: false, Actions: null },
        escapeHtml: (s) => String(s).replace(/[&<>"']/g, (c) => (
            { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]
        )),
        formatDateTimeID: () => '2026-01-01 10:00',
        showToast() {}, showConfirm: () => Promise.resolve(false), copyCode() {},
        Modal: { open() {}, close() {} },
        console, setTimeout, clearTimeout, parseInt, Number, isNaN, Math, Date, Set, Array, Promise,
        URLSearchParams: class { get() { return null; } },
    };
    sandbox.window.escapeHtml = sandbox.escapeHtml;
    return { sandbox, byId };
}

// ════════════════════════════════════════════════════════════════════════
// S92 — viewRedemptions / loadApps / loadPackages tanpa seq-token
// ════════════════════════════════════════════════════════════════════════

test('S92 (statik): viewRedemptions punya token monoton redemptionSeq + guard then & catch', () => {
    const body = functionBody(VOUCHERS_JS, 'viewRedemptions');
    assert.match(body, /redemptionSeq/, 'viewRedemptions wajib memakai token redemptionSeq');
    const guards = (body.match(/seq\s*!==\s*redemptionSeq/g) || []).length;
    assert.ok(guards >= 2,
        `guard "seq !== redemptionSeq" wajib ada di THEN dan CATCH (dapat ${guards})`);
});

test('S92 (statik): loadApps (appLoadSeq) & loadPackages (packageLoadSeq) ber-token di sukses + gagal', () => {
    const apps = functionBody(SYSTEM_APPS_JS, 'loadApps');
    assert.match(apps, /appLoadSeq/, 'loadApps wajib memakai token appLoadSeq');
    assert.ok((apps.match(/seq\s*!==\s*appLoadSeq/g) || []).length >= 2,
        'guard appLoadSeq wajib di jalur sukses DAN catch');

    const pkgs = functionBody(PACKAGES_JS, 'loadPackages');
    assert.match(pkgs, /packageLoadSeq/, 'loadPackages wajib memakai token packageLoadSeq');
    assert.ok((pkgs.match(/seq\s*!==\s*packageLoadSeq/g) || []).length >= 2,
        'guard packageLoadSeq wajib di THEN dan CATCH');
});

test('S92 (perilaku vm): respons redemptions basi TIDAK menimpa modal permintaan lebih baru', async () => {
    const { sandbox, byId } = buildVouchersSandbox();
    let resolveA;
    const slowA = new Promise((res) => { resolveA = res; });
    const fastB = Promise.resolve({
        json: () => Promise.resolve({
            success: true,
            redemptions: [{ username: 'userB', redeemed_at: '2026-01-02T09:00:00Z' }],
        }),
    });
    const responders = [() => slowA, () => fastB];
    sandbox.apiFetch = () => responders.shift()();

    // Muat modul utuh di vm (definisi saja; registrasi Actions dilewati).
    vm.runInNewContext(VOUCHERS_JS + '\nthis.viewRedemptions = viewRedemptions; this.renderPagination = renderPagination; this.renderVouchersTable = renderVouchersTable;',
        sandbox, { filename: 'settings-vouchers.js' });

    // Klik (Lihat User) voucher A (lambat), lalu voucher B (cepat).
    sandbox.viewRedemptions(1, 'AAA');
    sandbox.viewRedemptions(2, 'BBB');
    await new Promise((r) => setTimeout(r, 0));
    const bodyEl = byId.redemptionsBody;
    assert.ok(bodyEl.innerHTML.includes('userB'), 'modal menampilkan hasil permintaan TERAKHIR (B)');

    // Respons lambat A mendarat TERAKHIR — wajib diabaikan oleh guard seq.
    resolveA({
        json: () => Promise.resolve({
            success: true,
            redemptions: [{ username: 'userA', redeemed_at: '2026-01-01T10:00:00Z' }],
        }),
    });
    await new Promise((r) => setTimeout(r, 0));
    assert.ok(!bodyEl.innerHTML.includes('userA'),
        'respons basi (permintaan lama) tidak boleh menimpa isi modal');
    assert.ok(bodyEl.innerHTML.includes('userB'), 'isi modal tetap milik permintaan B');
});

// ════════════════════════════════════════════════════════════════════════
// R110 — paginasi vouchers tanpa aria-current/aria-label
// ════════════════════════════════════════════════════════════════════════

test('R110 (statik): renderPagination vouchers menandai aria-label + aria-current ala audit', () => {
    const body = functionBody(VOUCHERS_JS, 'renderPagination');
    assert.match(body, /aria-label="Halaman/,
        'tiap tombol halaman wajib punya aria-label "Halaman N" (paritas audit :92)');
    assert.match(body, /aria-current="page"/,
        'halaman aktif wajib ditandai aria-current="page"');
});

test('R110 (perilaku vm): tepat satu tombol aktif, semua tombol bernama "Halaman N"', async () => {
    const { sandbox, byId } = buildVouchersSandbox();
    sandbox.apiFetch = () => Promise.resolve({ json: () => Promise.resolve({ success: true }) });
    vm.runInNewContext(VOUCHERS_JS + '\nthis.renderPagination = renderPagination;',
        sandbox, { filename: 'settings-vouchers.js' });

    sandbox.renderPagination({ page: 2, total_pages: 3, total: 30 });
    const html = byId.paginationContainer.innerHTML;
    const current = (html.match(/aria-current="page"/g) || []).length;
    assert.equal(current, 1, `tepat satu tombol halaman aktif (dapat ${current})`);
    assert.match(html, /aria-label="Halaman 2"[^>]*aria-current="page"/,
        'tombol aktif adalah halaman 2 dan tetap punya aria-label');
    const buttons = html.match(/<button[^>]*>/g) || [];
    assert.ok(buttons.length >= 3, `tiga halaman dirender (dapat ${buttons.length})`);
    for (const b of buttons) {
        assert.match(b, /data-action="voucher-page"/, 'tombol tetap via delegasi data-action');
        assert.match(b, /aria-label="Halaman \d+"/, 'setiap tombol halaman punya nama aksesibel');
    }
});

// ════════════════════════════════════════════════════════════════════════
// R102 — paritas bahasa: Generate Batch → Buat Massal; typo "Kesini"
// ════════════════════════════════════════════════════════════════════════

test('R102 (statik): settings-vouchers.js bebas "Generate Batch"; restore tombol "Buat Massal"', () => {
    assert.doesNotMatch(VOUCHERS_JS, /Generate Batch/i,
        'label EN arwah di restore btn.textContent (:309/:320) wajib jadi "Buat Massal"');
    assert.doesNotMatch(VOUCHERS_JS, /generate batch/i,
        'toast gagal (:315) juga wajib berbahasa Indonesia');
    const count = (VOUCHERS_JS.match(/Buat Massal/g) || []).length;
    assert.ok(count >= 2, `restore tombol submit batch dua jalur (then+catch) = ${count}, minimal 2`);
});

test('R102 (statik): settings.html bebas "Generate Batch"/"Batch Generate"; memakai "Buat Massal"', () => {
    assert.doesNotMatch(SETTINGS_HTML, /Generate Batch/,
        'judul modal (:1455) & tombol submit (:1558) wajib "Buat Massal Voucher"/"Buat Massal"');
    assert.doesNotMatch(SETTINGS_HTML, /Batch Generate/,
        'toolbar voucher (:1233) wajib "Buat Massal"');
    assert.match(SETTINGS_HTML, />Buat Massal<\/button>/,
        'tombol toolbar/submit memakai teks "Buat Massal"');
    assert.match(SETTINGS_HTML, /Buat Massal Voucher/,
        'judul modal batch memakai "Buat Massal Voucher"');
});

test('R102 (statik): typo "Kesini" diperbaiki jadi "Ke Sini" di settings.html & settings-system-apps.js', () => {
    assert.doesNotMatch(SETTINGS_HTML, /Kesini/,
        'drop-area unggahan (:2037) wajib "Pilih atau Seret File Ke Sini"');
    assert.doesNotMatch(SYSTEM_APPS_JS, /Kesini/,
        'reset label (:158) & updateFileName (:270) wajib ejaan benar');
    assert.match(SETTINGS_HTML, /File Ke Sini/);
    assert.ok((SYSTEM_APPS_JS.match(/File Ke Sini/g) || []).length >= 2,
        'kedua titik reset label di JS memakai "File Ke Sini"');
});

// ════════════════════════════════════════════════════════════════════════
// R103 — th tanpa scope="col" + caption sr-only (vouchers/riwayat/packages)
// ════════════════════════════════════════════════════════════════════════

function tableBlock(tbodyId) {
    const start = SETTINGS_HTML.indexOf(`id="${tbodyId}"`);
    assert.ok(start !== -1, `tbody ${tbodyId} ditemukan`);
    const tableStart = SETTINGS_HTML.lastIndexOf('<table', start);
    const tableEnd = SETTINGS_HTML.indexOf('</table>', start);
    return SETTINGS_HTML.slice(tableStart, tableEnd);
}

for (const [tbodyId, nama] of [
    ['vouchersTableBody', 'Daftar Voucher'],
    ['auditLogsBody', 'Riwayat Klaim'],
    ['packagesTableBody', 'Paket Pendaftaran'],
]) {
    test(`R103 (statik): semua th tabel ${nama} punya scope="col"`, () => {
        const block = tableBlock(tbodyId);
        const ths = block.match(/<th[\s>][^>]*>/g) || [];
        assert.ok(ths.length >= 4, `${nama}: header kolom ditemukan (dapat ${ths.length})`);
        for (const th of ths) {
            assert.match(th, /scope="col"/, `th tanpa scope="col": ${th.trim().slice(0, 80)}`);
        }
    });

    test(`R103 (statik): tabel ${nama} punya caption sr-only`, () => {
        const block = tableBlock(tbodyId);
        assert.match(block, /<caption[^>]*class="[^"]*sr-only[^"]*"/,
            'caption sr-only sebagai nama aksesibel tabel');
    });
}

// ════════════════════════════════════════════════════════════════════════
// R104 — durationText interpolasi mentah
// ════════════════════════════════════════════════════════════════════════

test('R104 (statik): durationText dibungkus escapeHtml(String(...)); tak ada interpolasi mentah', () => {
    const body = functionBody(VOUCHERS_JS, 'renderVouchersTable');
    assert.match(body, /durationText\s*=\s*escapeHtml\(String\(v\.duration_type\)\)/,
        'assignment awal durationText wajib escapeHtml(String(...))');
    assert.doesNotMatch(body, /\$\{v\.duration_type\}/,
        'cabang durasi kustom (:93) wajib lewat escapeHtml(String(...)), bukan mentah');
});

test('R104 (perilaku vm): durasi kustom ber-tag HTML dirender aman (ter-escape)', async () => {
    const { sandbox } = buildVouchersSandbox();
    sandbox.apiFetch = () => Promise.resolve({ json: () => Promise.resolve({ success: true }) });
    vm.runInNewContext(VOUCHERS_JS + '\nthis.renderVouchersTable = renderVouchersTable;',
        sandbox, { filename: 'settings-vouchers.js' });

    sandbox.renderVouchersTable([{
        code: 'V1',
        package: 'Paket <b>A</b>',
        duration_type: '"><img src=x onerror=alert(1)>',
        used_count: 0,
        max_usage: 5,
        is_active: true,
        expires_at: null,
        notes: '',
    }]);
    const tbody = sandbox.document.getElementById('vouchersTableBody');
    assert.ok(!tbody.innerHTML.includes('<img'),
        'payload ber-tag pada duration_type tidak boleh lolos mentah ke markup');
});

// ════════════════════════════════════════════════════════════════════════
// S107 — branch mati window.loadSectionScript di openUploadModalSafe
// ════════════════════════════════════════════════════════════════════════

function dispatcherBlock() {
    const anchor = SETTINGS_HTML.indexOf('var pending');
    assert.ok(anchor !== -1, 'blok dispatcher openUploadModalSafe ditemukan');
    const start = SETTINGS_HTML.lastIndexOf('(function', anchor);
    const end = SETTINGS_HTML.indexOf('})();', anchor);
    return SETTINGS_HTML.slice(start, end + 5);
}

test('S107 (statik): dispatcher tidak lagi mereferensikan loadSectionScript (branch mati dihapus)', () => {
    assert.doesNotMatch(dispatcherBlock(), /loadSectionScript/,
        'typeof window.loadSectionScript selalu false — branch mati wajib dihapus');
});

test('S107 (perilaku vm): fallback memuat modul dengan cache-buster ?v= dan menandai __settingsLoaded', () => {
    let createdScript = null;
    const sandbox = {
        window: {},
        document: {
            createElement() { return {}; },
            head: { appendChild(el) { createdScript = el; } },
            addEventListener() {},
        },
    };
    vm.runInNewContext(dispatcherBlock(), sandbox, { filename: 'settings-dispatcher.js' });

    assert.equal(typeof sandbox.window.openUploadModalSafe, 'function',
        'dispatcher tetap terekspos di window');
    sandbox.window.openUploadModalSafe();
    assert.ok(createdScript, 'fallback membuat elemen script');
    assert.match(createdScript.src,
        /\/static\/js\/settings-system-apps\.js\?v=\{\{\.version\}\}$/,
        'src wajib membawa cache-buster ?v={{.version}}');
    // Dikalibrasi Batch 17/S114: flag TIDAK lagi ditandai sebelum muat —
    // hanya setelah onload sukses; onerror mengembalikan flag + toast.
    assert.notEqual(sandbox.window.__settingsLoaded && sandbox.window.__settingsLoaded['system-apps'],
        true, 'flag TIDAK boleh true sebelum script sukses dimuat (S114: gagal muat = tab mati senyap)');
    if (typeof createdScript.onload === 'function') createdScript.onload();
    assert.equal(sandbox.window.__settingsLoaded && sandbox.window.__settingsLoaded['system-apps'],
        true, 'onload sukses wajib menandai __settingsLoaded[\'system-apps\'] = true');
    // S114: onerror mengembalikan flag ke false + toast gagal-muat.
    sandbox.window.__settingsLoaded['system-apps'] = false;
    let errorToast = null;
    sandbox.window.showToast = (m, t) => { errorToast = { m, t }; };
    if (typeof createdScript.onerror === 'function') createdScript.onerror();
    assert.equal(sandbox.window.__settingsLoaded['system-apps'], false,
        'onerror wajib mengembalikan flag ke false agar modul bisa dimuat ulang');
    assert.ok(errorToast && /Gagal memuat/i.test(errorToast.m || ''),
        'onerror wajib memberi toast gagal-muat (bukan mati senyap)');
});

// ════════════════════════════════════════════════════════════════════════
// S108 — klik "Muat Ulang" ikut melipat kartu Daftar User
// ════════════════════════════════════════════════════════════════════════

test('S108 (statik): toggle head users memeriksa e.target.closest("[data-action]")', () => {
    assert.match(USERS_JS, /e\.target\.closest\('\[data-action\]'\)/,
        'guard awal toggle wajib memeriksa target klik sebelum melipat kartu');
});

test('S108 (perilaku vm): klik tombol refresh TIDAK men-toggle; klik area lain head tetap toggle', () => {
    const sandbox = {
        window: { __settingsReady: {}, Actions: null },
        localStorage: { getItem: () => null, setItem() {} },
        document: {
            getElementById() { return null; },
            querySelectorAll() { return []; },
            addEventListener() {},
        },
        __adminHasRole: () => false,
        fmtStorageSize: (v) => String(v),
    };
    vm.runInNewContext(USERS_JS + '\nthis.wireUsersCollapseBlock = wireUsersCollapseBlock;',
        sandbox, { filename: 'settings-users.js' });

    const listeners = {};
    const bodyEl = { style: { display: '' } };
    const block = {
        dataset: {},
        querySelector(sel) {
            if (sel === '.saas-collapse-head') {
                return {
                    attrs: {}, listeners: {},
                    setAttribute(k, v) { this.attrs[k] = v; },
                    getAttribute(k) { return this.attrs[k] || null; },
                    addEventListener(t, f) { (listeners[t] = f); },
                    classList: { add() {}, toggle() {} },
                    closest(sel2) {
                        // Head berada di dalam blok .saas-collapse miliknya.
                        return sel2 === '.saas-collapse' ? block : null;
                    },
                };
            }
            // Body tunggal per blok — objek yang sama setiap kali diminta.
            return bodyEl;
        },
    };
    sandbox.wireUsersCollapseBlock(block);
    const onClick = listeners.click;
    assert.equal(typeof onClick, 'function', 'head ter-wire listener click');

    const headLike = block.querySelector('.saas-collapse-head');
    const refreshBtn = { closest: (sel) => (sel === '[data-action]' ? {} : null) };

    // Klik tombol Muat Ulang (elemen ber-data-action di dalam head):
    onClick.call(headLike, { type: 'click', target: refreshBtn });
    assert.equal(bodyEl.style.display, '',
        'klik elemen ber-data-action (Muat Ulang) TIDAK boleh melipat kartu');

    // Klik area lain head: tetap men-toggle.
    onClick.call(headLike, { type: 'click', target: { closest: () => null } });
    assert.equal(bodyEl.style.display, 'none', 'klik biasa di head tetap melipat kartu');
});

// ════════════════════════════════════════════════════════════════════════
// R129 — emoji ⚙️ opsi voucher + h3>div uploadModalTitle
// ════════════════════════════════════════════════════════════════════════

test('R129 (statik): settings.html bebas emoji ⚙️ pada opsi/judul grup kustom voucher', () => {
    assert.ok(!SETTINGS_HTML.includes('⚙'),
        'emoji gear pada opsi "Kustom" & judul grup entitlement wajib dihapus (teks polos)');
});

test('R129 (statik): h3#uploadModalTitle tidak lagi berisi elemen <div> (content model valid)', () => {
    const start = SETTINGS_HTML.indexOf('id="uploadModalTitle"');
    assert.ok(start !== -1, 'uploadModalTitle ditemukan');
    const end = SETTINGS_HTML.indexOf('</h3>', start);
    const segment = SETTINGS_HTML.slice(start, end);
    assert.doesNotMatch(segment, /<div\b/,
        'ikon pindah keluar dari h3 ATAU diganti elemen phrasing (span)');
});
