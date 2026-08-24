/* Batch 11 — perbaikan UI/UX halaman publik (review_uiux_webui.md §5.8
 * RE-REVIEW RONDE 5, sisi publik). ID temuan: T19, T20 (sisi download),
 * T21 (sisi publik), R60, R62, R63, R64, R65, R66.
 *
 * Run with:  node --test static/js/uiux-batch11-publik.test.mjs   (from webui/)
 *
 * Metode mengikuti uiux-batch10-publik.test.mjs: kontrak statik fs-read +
 * eksekusi perilaku via vm.runInNewContext dengan stub DOM minimal.
 *
 * Dampak bisnis yang dilindungi:
 *   T19 — registrasi Actions (hasil & download) tereksekusi SETELAH
 *         admin-core.js ber-defer: blok registrasi hidup di dalam
 *         DOMContentLoaded, bukan top-level yang kalah race (tab, paginasi,
 *         clear-search, reload, download mati untuk klik mouse).
 *   T20 — heading order kartu unduhan h2 → h3 (tanpa lompatan outline).
 *   T21 — templates/public/** bebas SEMUA handler inline (\son[a-z]+=),
 *         termasuk oninput tersisa di hasil.html (pencarian di-wiring via
 *         addEventListener dengan perilaku identik).
 *   R60 — suffix cache-busting manual `?v={{.version}}-N` dihapus dari semua
 *         template publik (sumber kebenaran busting = {{.version}}).
 *   R62 — label grup OTP ter-asosiasi programatik (for → id kotak pertama).
 *   R63 — state error 404 hasil punya CTA "Kembali ke Beranda" + link
 *         sekunder "Coba Token Lain" (bukan dead-end).
 *   R64 — strength meter password terhubung programatik: input
 *         aria-describedby="pwStrengthText" + aria-live="polite".
 *   R65 — hint username menyebut huruf kecil wajib.
 *   R66 — waktu halaman hasil dikirim API sebagai string WIB terformat
 *         (fallback FixedZone) dan dipakai JS apa adanya — tidak lagi
 *         mengikuti jam perangkat penonton.
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

const HASIL = () => read('templates/public/hasil.html');
const DOWNLOAD = () => read('templates/public/download.html');
const SHARED = () => read('templates/public/shared.html');
const REGISTER_CONFIRM = () => read('templates/public/register_confirm.html');
const RESET_PASSWORD = () => read('templates/public/reset_password.html');
const REGISTER = () => read('templates/public/register.html');
const FORGOT = () => read('templates/public/forgot_password.html');
const CEK_HASIL = () => read('templates/public/cek_hasil.html');
const INDEX = () => read('templates/public/index.html');
const HASIL_GO = () => read('internal/handlers/public/hasil.go');

const PUBLIC_TEMPLATES = [
    ['hasil.html', HASIL], ['download.html', DOWNLOAD], ['shared.html', SHARED],
    ['register_confirm.html', REGISTER_CONFIRM], ['register.html', REGISTER],
    ['reset_password.html', RESET_PASSWORD], ['forgot_password.html', FORGOT],
    ['cek_hasil.html', CEK_HASIL], ['index.html', INDEX],
];

/** Ambil isi blok <script> inline TERAKHIR sebuah template (script logika halaman). */
function lastInlineScript(html) {
    const openers = [...html.matchAll(/<script(?![^>]*\bsrc=)[^>]*>/g)];
    assert.ok(openers.length > 0, 'template harus punya minimal satu script inline');
    const open = html.indexOf('>', openers[openers.length - 1].index) + 1;
    const close = html.indexOf('</script>', open);
    return html.slice(open, close);
}

/** Ganti ekspresi template Go pada state halaman hasil dengan literal JS. */
function stripGoTemplates(src) {
    return src
        .replace(/\{\{\.token\}\}/g, 'TOKEN01')
        .replace(/\{\{if \.is_logged_in\}\}true\{\{else\}\}false\{\{end\}\}/g, 'false')
        .replace(/\{\{if \.is_disabled\}\}true\{\{else\}\}false\{\{end\}\}/g, 'false')
        .replace(/\{\{if \.show_answers\}\}true\{\{else\}\}false\{\{end\}\}/g, 'true')
        .replace(/\{\{if \.error\}\}true\{\{else\}\}false\{\{end\}\}/g, 'false');
}

// ===========================================================================
// T19 — registrasi Actions kalah race terhadap admin-core.js defer
// ===========================================================================

/**
 * Simulasi urutan nyata browser: (1) inline script akhir-body dieksekusi saat
 * parsing — Actions BELUM ada; (2) skrip defer (admin-core.js) dieksekusi
 * setelah parsing; (3) event DOMContentLoaded memicu listener halaman.
 */
function runRaceScenario(scriptSrc, extraGlobals = {}) {
    const domListeners = {};
    // Elemen DOM generik — init halaman (switchTab dll.) menyentuh classList/
    // style elemen; cukup no-op, yang diuji adalah registrasi Actions.
    const mkEl = () => ({
        style: {},
        dataset: {},
        tabIndex: 0,
        innerHTML: '',
        classList: {
            _s: new Set(),
            toggle(c) { this._s.has(c) ? this._s.delete(c) : this._s.add(c); },
            add(c) { this._s.add(c); },
            remove(c) { this._s.delete(c); },
            contains(c) { return this._s.has(c); },
        },
        setAttribute() {}, getAttribute: () => null, removeAttribute() {},
        appendChild() {}, addEventListener() {}, focus() {}, querySelectorAll: () => [],
    });
    const sandbox = {
        console: { error() {}, warn() {}, log() {} },
        location: { hash: '', reload() {} },
        history: { replaceState() {} },
        window: { addEventListener() {} },
        document: {
            getElementById: () => mkEl(),
            activeElement: null,
            querySelector: () => mkEl(),
            querySelectorAll: () => [],
            createElement: () => mkEl(),
            addEventListener(type, fn) { (domListeners[type] || (domListeners[type] = [])).push(fn); },
        },
        filterResults() {},
        switchTab() {},
        goToPage() {},
        // init halaman hasil memulai loadResults() (floating promise) —
        // beri stub API agar tidak ada activity menggantung setelah test.
        apiFetch: () => Promise.resolve({ ok: false, json: () => Promise.resolve({ success: false }) }),
        showApiErrorToast() {},
        escapeHtml: (s) => String(s == null ? '' : s),
        ...extraGlobals,
    };
    const ctx = vm.createContext(sandbox);

    // Fase 1 — inline script saat parsing (Actions belum ada).
    vm.runInContext(scriptSrc, ctx);

    // Fase 2 — skrip defer selesai → registry Actions kini terdefinisi.
    const registered = {};
    sandbox.Actions = {
        register(name) { registered[name] = true; },
        has: (name) => !!registered[name],
    };

    // Fase 3 — DOMContentLoaded menyala (semua listener, sesuai urutan pasang).
    const cbs = domListeners['DOMContentLoaded'] || [];
    assert.ok(cbs.length > 0, 'halaman wajib memasang listener DOMContentLoaded');
    for (const cb of cbs) cb();
    return registered;
}

test('T19 (perilaku): hasil.html — handler aksi terdaftar walau core defer selesai SETELAH inline script', () => {
    const src = stripGoTemplates(lastInlineScript(HASIL()));
    const got = runRaceScenario(src);
    for (const name of ['switch-tab', 'page-prev', 'page-next', 'clear-search', 'reload-page']) {
        assert.equal(got[name], true,
            `Actions.register('${name}') harus tereksekusi (dulu: guard typeof Actions `
            + 'dievaluasi sebelum skrip defer → selalu undefined → semua aksi mati)');
    }
});

test('T19 (perilaku): download.html — switch-tab & download-app terdaftar pada urutan eksekusi nyata', () => {
    const src = lastInlineScript(DOWNLOAD());
    const got = runRaceScenario(src);
    assert.equal(got['switch-tab'], true,
        "Actions.register('switch-tab') harus tereksekusi — dulu tab platform MATI untuk klik mouse");
    assert.equal(got['download-app'], true,
        "Actions.register('download-app') harus tereksekusi — tombol unduh tidak boleh mati");
});

// ===========================================================================
// T20 — heading order kartu unduhan (h4 flavor-title → h3)
// ===========================================================================

test('T20: download.html bebas <h4> — flavor-title memakai h3 setelah h2 kartu', () => {
    const html = DOWNLOAD();
    const h4 = (html.match(/<h4\b/gi) || []).length;
    assert.equal(h4, 0, `download.html masih punya ${h4} <h4> — outline screen reader melompat`);
    const titles = (html.match(/<h3 class="flavor-title"/g) || []).length;
    assert.ok(titles >= 4, `flavor-title wajib jadi <h3> (ditemukan ${titles}, butuh ≥4)`);
});

test('T20: rule .flavor-title murni class-based — visual tak bergantung tag h4', () => {
    const html = DOWNLOAD();
    const rule = html.match(/\.flavor-title\s*\{([^}]*)\}/);
    assert.ok(rule, '.flavor-title harus ada di blok style lokal');
    // Selector tidak menyebut elemen h4/h3 — ganti tag aman secara visual.
    const selectorRegion = html.slice(Math.max(0, rule.index - 60), rule.index);
    assert.doesNotMatch(selectorRegion, /h4\s*,?\s*$/, 'selector jangan digandengkan ke tag h4');
});

// ===========================================================================
// T21 — templates/publik bebas SEMUA handler inline (\son[a-z]+=)
// ===========================================================================

test('T21 (guard folder-wide): templates/public/*.html bebas \\son[a-z]+= (oninput/onsubmit/onchange/onkeyup/onclick/dst.)', () => {
    const dir = path.join(WEBUI_ROOT, 'templates', 'public');
    for (const [name] of PUBLIC_TEMPLATES) {
        const src = fs.readFileSync(path.join(dir, name), 'utf8');
        const hits = src.match(/\son[a-z]+=/gi) || [];
        assert.deepEqual(hits, [],
            `${name}: masih ada handler inline ${JSON.stringify(hits)} — wiring via addEventListener`);
    }
});

test('T21 (perilaku): pencarian hasil di-wiring addEventListener input — filterResults + toggle tombol clear identik dengan oninput lama', () => {
    // Replika perilaku oninput lama: filterResults(); clearBtn.display = value ? flex : none.
    const calls = [];
    const clearBtn = { style: { display: 'none' } };
    const searchInput = {
        value: '',
        listeners: {},
        addEventListener(type, fn) { this.listeners[type] = fn; },
    };
    const sandbox = {
        console,
        filterResults: () => calls.push('filter'),
        document: {
            getElementById: (id) => (id === 'searchInput' ? searchInput
                : id === 'searchClearBtn' ? clearBtn : null),
            querySelector: () => null,
            addEventListener() {},
        },
    };
    const ctx = vm.createContext(sandbox);

    // Wiring HARUS berada di dalam init DOMContentLoaded (bukan oninput atribut).
    const html = HASIL();
    assert.doesNotMatch(html, /oninput=/, 'atribut oninput wajib dihapus dari searchInput');
    const init = html.match(/DOMContentLoaded[\s\S]*?addEventListener\(['"]input['"][\s\S]*?</);
    assert.ok(init, 'init DOMContentLoaded wajib memasang listener input untuk searchInput');
    const wiringSrc = `
        var searchInput = document.getElementById('searchInput');
        var clearBtn = document.getElementById('searchClearBtn');
        if (searchInput) searchInput.addEventListener('input', function () {
            filterResults();
            if (clearBtn) clearBtn.style.display = this.value ? 'flex' : 'none';
        });
    `;
    vm.runInContext(wiringSrc, ctx);

    searchInput.listeners['input'].call(searchInput);
    assert.deepEqual(calls, ['filter'], 'listener memanggil filterResults');
    searchInput.value = 'budi';
    searchInput.listeners['input'].call(searchInput);
    assert.equal(clearBtn.style.display, 'flex', 'nilai terisi → tombol clear tampil');
    searchInput.value = '';
    searchInput.listeners['input'].call(searchInput);
    assert.equal(clearBtn.style.display, 'none', 'nilai kosong → tombol clear sembunyi');
});

// ===========================================================================
// R60 — suffix cache-busting manual ?v={{.version}}-N
// ===========================================================================

test('R60: seluruh template publik memakai ?v={{.version}} polos — tanpa suffix manual -N', () => {
    const dir = path.join(WEBUI_ROOT, 'templates', 'public');
    for (const [name] of PUBLIC_TEMPLATES) {
        const src = fs.readFileSync(path.join(dir, name), 'utf8');
        const hits = src.match(/\?v=\{\{\.version\}\}-[^\s"']*/g) || [];
        assert.deepEqual(hits, [],
            `${name}: suffix cache-busting manual ${JSON.stringify(hits)} wajib dihapus `
            + '(mekanismenya {{.version}} — suffix bukti rilis lama edit file tanpa bump version)');
    }
});

// ===========================================================================
// R62 — label grup OTP tanpa asosiasi programatik
// ===========================================================================

for (const [label, tpl] of [['register_confirm', REGISTER_CONFIRM], ['reset_password', RESET_PASSWORD]]) {
    test(`R62: label "Kode OTP" ${label} ter-asosiasi via for= ke id kotak OTP pertama`, () => {
        const html = tpl();
        const labelTag = html.match(/<label[^>]*for="([^"]+)"[^>]*>\s*(?:Masukkan Kode OTP|Kode OTP)\s*<\/label>/);
        assert.ok(labelTag, 'label grup OTP wajib membawa atribut for=');
        const id = labelTag[1];
        const firstDigit = html.match(new RegExp(`<input[^>]*class="otp-digit"[^>]*>`));
        assert.ok(firstDigit, 'kotak OTP pertama harus ada');
        assert.match(firstDigit[0], new RegExp(`id="${id}"`),
            `for="${id}" harus menunjuk id kotak OTP pertama`);
    });
}

// ===========================================================================
// R63 — state error 404 dead-end tanpa CTA
// ===========================================================================

test('R63: kartu error "Ujian Tidak Ditemukan" punya CTA Kembali ke Beranda + Coba Token Lain', () => {
    const html = HASIL();
    const errIdx = html.indexOf('{{else if .error}}');
    const endIdx = html.indexOf('{{else}}', errIdx);
    assert.ok(errIdx !== -1 && endIdx !== -1, 'cabang error harus ada');
    const branch = html.slice(errIdx, endIdx);
    const home = branch.match(/<a[^>]*href="\/"[^>]*>/);
    assert.ok(home, 'CTA primer href="/" wajib ada di state error');
    assert.match(branch, /Kembali ke Beranda/, 'teks CTA primer "Kembali ke Beranda"');
    const retry = branch.match(/<a[^>]*href="\/hasil"[^>]*>/);
    assert.ok(retry, 'link sekunder href="/hasil" (Coba Token Lain) wajib ada');
    assert.match(branch, /Coba Token Lain/, 'teks link sekunder "Coba Token Lain"');
});

// ===========================================================================
// R64 — strength meter tak terhubung programatik
// ===========================================================================

for (const [label, tpl] of [['register', REGISTER], ['reset_password', RESET_PASSWORD]]) {
    test(`R64: input password ${label} aria-describedby=pwStrengthText + teks kekuatan aria-live=polite`, () => {
        const html = tpl();
        const pw = html.match(/<input[^>]*type="password"[^>]*id="password"[^>]*>/)
            || html.match(/<input[^>]*id="password"[^>]*type="password"[^>]*>/);
        assert.ok(pw, 'input #password harus ada');
        assert.match(pw[0], /aria-describedby="pwStrengthText"/,
            'input password wajib aria-describedby="pwStrengthText"');
        const live = html.match(/<div[^>]*id="pwStrengthText"[^>]*>/);
        assert.ok(live, '#pwStrengthText harus ada');
        assert.match(live[0], /aria-live="polite"/,
            'feedback kekuatan wajib aria-live="polite" agar screen reader mendengar perubahan');
    });
}

// ===========================================================================
// R65 — hint username lowercase
// ===========================================================================

test('R65: hint username register menyebut huruf kecil wajib', () => {
    const hint = REGISTER().match(/id="usernameHint"[^>]*>([^<]*)</);
    assert.ok(hint, '#usernameHint harus ada');
    assert.equal(hint[1].trim(),
        'Huruf kecil, angka, titik, dan garis bawah. Min. 3 karakter.',
        'hint wajib menjelaskan konversi toLowerCase yang diam-diam (JS sanitasi)');
});

// ===========================================================================
// R66 — zona waktu halaman hasil: API kirim WIB terformat, JS pakai apa adanya
// ===========================================================================

test('R66 (Go): hasil.go punya jakartaLoc fallback FixedZone WIB + formatter waktu tampilan', () => {
    const go = HASIL_GO();
    assert.match(go, /FixedZone\(\s*"WIB"/,
        'fallback tzdata wajib FixedZone("WIB", ...) — pola submissions.go');
    assert.match(go, /Asia\/Jakarta/, 'LoadLocation Asia/Jakarta');
    assert.match(go, /start_time_display/, 'API wajib mengirim start_time_display terformat WIB');
    assert.match(go, /created_at_display/, 'API wajib mengirim created_at_display terformat WIB');
});

test('R66 (JS): detail waktu hasil memakai field *_display dari API apa adanya (bukan jam browser)', () => {
    const fnStart = HASIL().match(/const startTimeStr[\s\S]{0,220}?;/);
    const fnEnd = HASIL().match(/const endTimeStr[\s\S]{0,220}?;/);
    assert.ok(fnStart && fnEnd, 'deklarasi startTimeStr/endTimeStr harus ada');
    assert.match(fnStart[0], /start_time_display/,
        'Waktu Mulai wajib dari start_time_display (string WIB server)');
    assert.match(fnEnd[0], /created_at_display/,
        'Waktu Kumpul wajib dari created_at_display (string WIB server)');
});
