/* Regression contract tests untuk Batch 3 perbaikan UI/UX (bagian
 * settings-nav): temuan S3, S4, S5, S18 di review_uiux_webui.md root repo.
 *
 * Run with:  node --test static/js/uiux-batch3-settings-nav.test.mjs   (from webui/)
 *
 * Dua jenis test:
 *   1. Kontrak statik — membaca file template/JS ASLI dan memastikan properti
 *      kunci perbaikan tidak pernah regresi (mis. tidak ada lagi interpolasi
 *      kode voucher mentah ke atribut onclick).
 *   2. Perilaku — mengeksekusi admin-core.js + settings-vouchers.js ASLI dalam
 *      Node vm (pola sama dengan admin-core.test.mjs / uiux-batch1.test.mjs)
 *      untuk membuktikan kode voucher berbahaya tidak merusak markup.
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

// ---------------------------------------------------------------------------
// S3 — escaping onclick voucher (settings-vouchers.js)
// ---------------------------------------------------------------------------

test('S3a: settings-vouchers.js tidak lagi memasang inline onclick untuk aksi baris voucher', () => {
    const js = read('static/js/settings-vouchers.js');

    // Handler-handler yang membawa kode voucher TIDAK BOLEH lagi dipasang
    // lewat atribut onclick (termasuk tombol "Coba Lagi" di modal
    // redemptions) karena interpolasi mentah ${v.code} / ${code} memutus
    // atribut saat kode berisi kutip/backslash sekaligus membuka injeksi.
    assert.doesNotMatch(
        js,
        /onclick="[^"]*\b(copyCode|viewRedemptions|toggleVoucher|deleteVoucher)\(/,
        'interpolasi ${v.code}/${code} ke onclick harus diganti mekanisme yang aman'
    );
});

test('S3b: settings-vouchers.js memakai data-* + event delegation & teks tampil ter-escape', () => {
    const js = read('static/js/settings-vouchers.js');

    assert.match(js, /data-voucher-code/, 'kode voucher harus dibawa lewat data-* attribute');
    assert.match(js, /data-action=/, 'aksi baris (copy/toggle/delete/redemptions) ditandai data-action');
    assert.match(js, /addEventListener\(\s*'click'/, 'event delegation (listener klik pada kontainer tabel/modal)');
    assert.match(js, /closest\(\s*['"]\[data-action\]['"]/ , 'delegasi harus resolve target via closest([data-action])');
    assert.match(js, /escapeHtml\(v\.code\)/, 'teks kode yang tampil di sel tabel wajib di-escape');
    // Batch 13 (T23): pesan konfirmasi PLAIN TEXT — showConfirm core meng-escape
    // seluruh argumen message (admin-core.js:512), sehingga kode cukup
    // diinterpolasi langsung; markup manual justru tampil sebagai tag literal.
    const delAt = js.indexOf('menghapus kode voucher');
    const toggleAt = js.indexOf("' kode voucher ' + code");
    assert.ok(toggleAt !== -1 && delAt !== -1, 'kedua pesan konfirmasi ada');
    for (const at of [toggleAt, delAt]) {
        const msg = js.slice(at, at + 400);
        assert.match(msg, /' \+ code \+ '/, 'kode voucher diinterpolasi (di-escape oleh core)');
        assert.doesNotMatch(msg, /<strong|escapeHtml\(code\)/, 'tanpa markup manual di pesan konfirmasi');
    }
});

test('S3c (perilaku): kode voucher `"\' ><img src=x>\\` tidak merusak markup tabel', () => {
    // Muat admin-core.js (escapeHtml asli) lalu settings-vouchers.js dalam vm.
    const coreSrc = read('static/js/admin-core.js');
    const vouchersSrc = read('static/js/settings-vouchers.js');

    const tbodyEl = {
        _html: '',
        setAttribute() {},
        get innerHTML() { return this._html; },
        set innerHTML(v) { this._html = v; },
        addEventListener() {},
        __rowActionsWired: false
    };
    const documentMock = {
        readyState: 'complete',
        addEventListener() {},
        dispatchEvent() { return true; },
        getElementById(id) {
            if (id === 'vouchersTableBody') return tbodyEl;
            if (id === 'searchVoucher') return { value: '' };
            return null;
        },
        querySelector() { return null; },
        querySelectorAll() { return []; },
        createElement() { return { style: {}, classList: { add() {}, remove() {}, contains() { return false; } }, appendChild() {}, setAttribute() {}, removeChild() {} }; },
        documentElement: {},
        body: { classList: { add() {}, remove() {}, contains() { return false; } } },
        contains() { return true; }
    };
    const sandbox = {
        window: { __settingsReady: {} },
        document: documentMock,
        CustomEvent: class FakeCustomEvent { constructor(t, o) { this.type = t; this.detail = (o && o.detail) || null; } },
        MutationObserver: function () { this.observe = () => {}; this.disconnect = () => {}; },
        getComputedStyle: () => ({ display: 'block' }),
        MouseEvent: function () {},
        navigator: {},
        console: { debug() {}, log() {}, warn() {}, error() {}, info() {} },
        setTimeout: () => 0,
        clearTimeout() {},
        setInterval: () => 0,
        clearInterval() {},
        location: { href: '' }
    };
    sandbox.globalThis = sandbox;
    vm.createContext(sandbox);
    vm.runInContext(coreSrc, sandbox, { filename: 'admin-core.js' });
    vm.runInContext(vouchersSrc, sandbox, { filename: 'settings-vouchers.js' });

    const RAW_CODE = '"\' ><img src=x>\\';
    sandbox.renderVouchersTable([{
        id: 7,
        code: RAW_CODE,
        package: 'bulanan',
        duration_type: 'bulanan',
        used_count: 2,
        max_usage: 10,
        is_active: true,
        expires_at: null,
        notes: null
    }]);

    const html = tbodyEl.innerHTML;
    assert.ok(html.length > 0, 'tabel voucher harus terender');

    // 1. Tidak boleh ada byte mentah dari kode berbahaya di markup.
    assert.ok(!html.includes('<img'), 'tag <img> mentah tidak boleh lolos ke markup');
    assert.ok(!html.includes(RAW_CODE), 'kode mentah (kutip/backslash) tidak boleh muncul apa adanya');

    // 2. Teks tampil tetap kode itu sendiri, hanya ter-escape (entitas).
    const expectedEscaped = sandbox.escapeHtml(RAW_CODE);
    assert.ok(html.includes(expectedEscaped), 'teks tampil tetap kode voucher dalam bentuk ter-escape');

    // 3. Data attribute membawa nilai penuh: round-trip decode == kode asli.
    const m = html.match(/data-voucher-code="([^"]*)"/);
    assert.ok(m, 'setiap baris membawa data-voucher-code');
    const decoded = m[1]
        .replace(/&lt;/g, '<')
        .replace(/&gt;/g, '>')
        .replace(/&quot;/g, '"')
        .replace(/&#39;/g, "'")
        .replace(/&amp;/g, '&');
    assert.equal(decoded, RAW_CODE, 'nilai data-voucher-code setelah di-decode harus persis kode asli');
});

// ---------------------------------------------------------------------------
// S4 — sweep EN→ID di settings.html (string user-facing saja)
// ---------------------------------------------------------------------------

test('S4: heading/placeholder EN di settings.html sudah diterjemahkan', () => {
    const html = read('templates/admin/settings.html');

    // String baru wajib ada (teks visible).
    assert.match(html, /Kustomisasi Footer/, 'heading kartu footer memakai bahasa Indonesia');
    assert.match(html, /© 2026 Tim EXAMVAN\. Hak cipta dilindungi\./, 'placeholder footer contoh ID');
    assert.match(html, /Pengaturan Email SaaS &amp; SMTP/, 'heading kartu email memakai bahasa Indonesia');

    // String lama tidak boleh lagi tampil sebagai TEKS visible (komentar HTML
    // masih boleh menyebut istilah lama).
    assert.doesNotMatch(html, />[^<]*Customizing Footer/, '"Customizing Footer" tidak boleh jadi teks visible');
    assert.ok(
        !html.includes('placeholder="© 2026 EXAMVAN Team. All rights reserved."'),
        'placeholder footer EN harus diganti'
    );
    assert.doesNotMatch(html, />[^<]*SaaS &amp; SMTP Email Settings/, '"SaaS & SMTP Email Settings" tidak boleh jadi teks visible');
});

// ---------------------------------------------------------------------------
// S5 — live-search dihalaman Pengawasan memakai helper bersama
// ---------------------------------------------------------------------------

test('S5: pengawas.html memakai initLiveSearch (debounce) tanpa onkeyup Enter-only', () => {
    const html = read('templates/admin/pengawas.html');

    assert.doesNotMatch(
        html,
        /onkeyup=[^>]*loadPengawasExams/,
        'pola onkeyup Enter-only harus dihapus dari input pencarian pengawas'
    );
    assert.match(
        html,
        /typeof\s+initLiveSearch\s*===?\s*['"]function['"]/,
        'pemanggilan initLiveSearch harus defensif (typeof check)'
    );
    assert.match(
        html,
        /initLiveSearch\(\s*document\.getElementById\('pengawasSearch'\)\s*,\s*function\(\) \{\s*loadPengawasExams\(1\)/,
        'input pengawasSearch harus dihubungkan ke initLiveSearch dengan callback loadPengawasExams(1) (S60: reset halaman)'
    );
    assert.match(
        html,
        /addEventListener\(\s*['"]keydown['"][\s\S]{0,200}Enter/,
        'fallback sederhana (keydown Enter) wajib ada bila helper belum tersedia'
    );
    // Tombol "Cari" existing tetap dipertahankan.
    assert.match(html, /toolbar-btn-search[^>]*>[^<]*<svg[\s\S]*?<\/svg> Cari<\/button>/, 'tombol Cari tetap ada');
});

// Cross-check kontrak dengan agen paralel: admin-core.js AKAN mendefinisikan
// global initLiveSearch(inputEl, callback, delayMs = 300). Selama file itu
// belum mendarat (dikerjakan agen lain), test ini dilewati — bukan gagal.
test('S5-cross: admin-core.js mendefinisikan function initLiveSearch (kontrak antar-agen)', (t) => {
    const core = read('static/js/admin-core.js');
    if (!/function\s+initLiveSearch\s*\(/.test(core)) {
        t.skip('initLiveSearch belum ada di admin-core.js — dikerjakan agen paralel; dilewati sementara');
        return;
    }
    assert.match(core, /function\s+initLiveSearch\s*\(\s*inputEl\s*,\s*callback\s*(?:,\s*delayMs\s*=\s*300\s*)?\)/,
        'signature kontrak: initLiveSearch(inputEl, callback, delayMs = 300)');
});

// ---------------------------------------------------------------------------
// S18 — aria-current di nav + aria-expanded awal pada hamburger
// ---------------------------------------------------------------------------

test('S18: nav.html menandai link aktif dengan aria-current="page" terikat kondisi', () => {
    const html = read('templates/admin/partials/nav.html');

    // Setiap kemunculan aria-current harus berada DI DALAM kondisi Go yang
    // merujuk $activePage / $isSettings (baris yang sama), sehingga nilainya
    // eksklusif: hanya satu link aktif per daftar pada request mana pun.
    const lines = html.split('\n').filter((l) => l.includes('aria-current="page"'));
    assert.ok(lines.length >= 8, `aria-current harus ada di 4 link topbar + 4 dropdown-item (dapat ${lines.length})`);
    for (const line of lines) {
        assert.match(line, /\{\{if (eq \$activePage|\$isSettings)/,
            `aria-current harus terikat kondisi $activePage/$isSettings — baris: ${line.trim().slice(0, 120)}`);
    }

    // Kelas "active" dan aria-current="page" dipasang lewat DUA kondisi yang
    // teksnya identik (backreference \1) pada link yang sama — aria-current
    // ditulis sebagai atribut terpisah di luar nilai class karena menaruh
    // kutip di dalam class="..." merusak parsing Go html/template
    // (`"\"" in attribute name`). Struktur ini sudah diverifikasi render-nya.
    const navLinkPairs = [...html.matchAll(
        /class="nav-link \{\{if ([^{}]+)\}\}active\{\{end\}\}" ?\{\{if \1\}\}aria-current="page"\{\{end\}\}/g)];
    assert.equal(navLinkPairs.length, 4, `4 link topbar harus berpasangan kondisi active & aria-current identik (dapat ${navLinkPairs.length})`);
    const ddPairs = [...html.matchAll(
        /class="dropdown-item \{\{if ([^{}]+)\}\}active\{\{end\}\}" ?\{\{if \1\}\}aria-current="page"\{\{end\}\}/g)];
    assert.equal(ddPairs.length, 4, `4 dropdown-item harus berpasangan kondisi active & aria-current identik (dapat ${ddPairs.length})`);

    // Kondisi antar-link dalam satu daftar harus unik (eksklusif): hanya satu
    // yang bernilai true per request.
    for (const [label, pairs] of [['topbar', navLinkPairs], ['dropdown', ddPairs]]) {
        const conds = pairs.map((m) => m[1]);
        assert.equal(new Set(conds).size, conds.length,
            `kondisi aria-current daftar ${label} harus saling eksklusif: ${conds.join(' | ')}`);
        assert.ok(conds.some((c) => c.includes('$isSettings')), `link Pengaturan di ${label} memakai $isSettings`);
        assert.equal(conds.filter((c) => c.includes('$isSettings')).length, 1,
            `$isSettings hanya boleh mengikat SATU link per daftar ${label}`);
    }

    // Hamburger: state awal aria-expanded eksplisit (sinkronisasi JS oleh
    // initMenuToggle milik agen lain).
    const btn = html.match(/<button[^>]*id="menuToggleBtn"[^>]*>/);
    assert.ok(btn, 'tombol #menuToggleBtn ada');
    assert.match(btn[0], /aria-expanded="false"/, 'hamburger wajib punya aria-expanded="false" awal');
    assert.match(btn[0], /aria-label=/, 'aria-label hamburger tetap ada');
});
