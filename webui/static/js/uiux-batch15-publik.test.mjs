/**
 * ══════════════════════════════════════════════════════════════════════════
 * Suite UI/UX BATCH 15 — PUBLIK (agen batch15-publik)
 * ══════════════════════════════════════════════════════════════════════════
 *
 * Referensi: review_uiux_webui.md bagian 5.12 RE-REVIEW RONDE 9. Cakupan
 * temuan milik agen ini:
 *
 *   S97  — Drawer nav mobile (.nav-links public-mobile.css:74-81) tertutup
 *          hanya dengan right:-100% — link di dalamnya tetap menerima fokus
 *          keyboard padahal tak terlihat (WCAG 2.4.3), dan closeMenu()
 *          (shared.html:43-63) tidak mengembalikan fokus ke hamburger.
 *          Kontrak: state tertutup visibility:hidden (+ transisi delay agar
 *          animasi halus), .open visibility:visible, reset desktop tetap
 *          visible, dan closeMenu mem-fokus #navHamburger hanya bila drawer
 *          memang sedang terbuka (Escape saat tertutup tak boleh curi fokus).
 *
 *   S98  — autocomplete="one-time-code" menempel pada kotak maxlength="1"
 *          (register_confirm.html:243, reset_password.html:132) sehingga
 *          autofill OTP iOS/Android terpotong jadi 1 digit. Kontrak: kotak
 *          digit bebas one-time-code; atribut pindah ke SATU input gabungan
 *          (#otp_code); input multi-karakter didistribusikan antar kotak.
 *
 *   S105 — reset_password.html:181 memuat admin-core.js penuh demi
 *          togglePasswordVisibility (:185-187). Kontrak: helper lokal
 *          wirePwToggle ala register.html:344-356, tag admin-core.js dihapus,
 *          kedua toggle tetap ter-wire dan berfungsi (vm).
 *
 *   R107 — Resend OTP sukses (register_confirm.html:398-406) tidak
 *          membersihkan digit lama; server merotasikan kode
 *          (cmd/server/auth_recovery.go:131). Kontrak vm: sukses resend
 *          mengosongkan 6 digit, sinkron hidden, fokus ke digit-1.
 *
 *   R109 — TTL OTP 15 menit (cmd/server/auth_recovery.go:23) tak pernah
 *          dikomunikasikan UI. Kontrak statik: frasa "berlaku 15 menit" di
 *          helper register_confirm & reset_password + pesan resend.
 *
 *   R108 — download.html:5-6 memuat ulang public-mobile/desktop.css yang
 *          sudah dipancarkan public_head (shared.html:117-119). Kontrak
 *          statik: download.html bebas kedua link itu.
 *
 *   S96  — Print hasil: judul gradien memakai -webkit-text-fill-color:
 *          transparent (hasil.css:102,:156) tapi blok @media print (:975)
 *          hanya me-reset color — teks gradien tercetak kosong. Kontrak:
 *          blok print me-reset properti vendor itu (-webkit-text-fill-color:
 *          initial + background: none) untuk selector gradien yang sama
 *          (.header-title, .exam-hero h1).
 *
 *   S101 — Kontras chip skor borderline di garis AA 4.5:1. Kalibrasi
 *          koordinator: .score-low base (#ef4444 di tint rgba(239,68,68,.15)
 *          atas glass gelap) sekitar 4.45:1, .score-status-fail sekitar
 *          4.59:1 — sensitif terhadap backdrop blur; varian aman:
 *          danger-bright sekitar 6.25:1, success-light sekitar 10.8:1.
 *          Kontrak: WARNA TEKS saja migrasi ke varian terang (.score-low /
 *          .score-status-fail ke --color-danger-bright; .score-high /
 *          .score-status-pass ke --color-success-light), tint latar tetap.
 *
 *   S103 — Token mati theme.css (--color-success-bg :27, --color-danger-bg
 *          :35, --color-warning-bg :38, --shadow-card :79, --shadow-lg :80).
 *          Keputusan adopt-or-delete: (a) nilai triplet bg identik dengan
 *          rgba manual chip hasil.html (rgba(var(--rgb-*),0.12)) -> chip
 *          bermigrasi ke var(--color-*-bg); border alpha 0.35 beda -> tetap
 *          rgba(var(--rgb-*),0.35), didokumentasikan. (b) --shadow-card
 *          diadopsi ke .glass-card hasil.css; --shadow-lg menggantikan
 *          duplikat literal identik mockup-wrapper shared.html.
 *          Kontrak: tiap token dari daftar itu punya pemakaian >= 1 ATAU
 *          definisinya dihapus dari theme.css.
 *
 *   R106 — Touch target halaman hasil (hasil.css:1001-1006) 38-42px di
 *          bawah standar repo 44px (admin-base.css:514-522). Kontrak: blok
 *          @media max-width:768px memuat min-height/min-width/size >= 44px
 *          untuk search-clear-btn, back-nav, tab-btn, pagination-btn.
 *
 *   R117 — Blok reduced-motion hasil.css (:933) tidak meng-cap
 *          animation-iteration-count. Kontrak: animation-iteration-count: 1
 *          TANPA !important (blok media datang belakangan dalam file ->
 *          menang kaskade spesifisitas sama; tidak ada deklarasi
 *          iteration-count kompetitor lain di file ini).
 *
 * Kepemilikan file agen ini: templates/public/{register_confirm,
 *   reset_password,register,shared,download,hasil,cek_hasil,
 *   forgot_password,index}.html, static/css/{hasil,theme,public-desktop,
 *   public-mobile}.css, static/js/uiux-batch15-publik.test.mjs (BARU).
 *
 * Metode: guard statik fs-read + eksekusi perilaku via vm.runInNewContext
 * dengan stub DOM minimal (pola uiux-batch10-publik). Catatan penulisan:
 * jangan menulis pola glob dua-bintang di dalam komentar blok ini.
 *
 * Run with:  node --test static/js/uiux-batch15-publik.test.mjs   (from webui/)
 */

import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';

const HERE = path.dirname(fileURLToPath(import.meta.url));
const WEBUI_ROOT = path.join(HERE, '..', '..');
const PUBLIC = path.join(WEBUI_ROOT, 'templates', 'public');
const read = (...p) => fs.readFileSync(path.join(...p), 'utf8');

const SHARED = read(PUBLIC, 'shared.html');
const REG_CONFIRM = read(PUBLIC, 'register_confirm.html');
const RESET_PW = read(PUBLIC, 'reset_password.html');
const DOWNLOAD = read(PUBLIC, 'download.html');
const HASIL_HTML = read(PUBLIC, 'hasil.html');
const MOBILE_CSS = read(WEBUI_ROOT, 'static', 'css', 'public-mobile.css');
const DESKTOP_CSS = read(WEBUI_ROOT, 'static', 'css', 'public-desktop.css');
const HASIL_CSS = read(WEBUI_ROOT, 'static', 'css', 'hasil.css');
const THEME_CSS = read(WEBUI_ROOT, 'static', 'css', 'theme.css');

// ── Util ───────────────────────────────────────────────────────────────────

// S104-friendly: ambil blok ber-kurung seimbang mulai dari marker; throw bila
// marker tidak ditemukan sehingga guard tidak bisa menjadi vakum diam-diam.
function extractBlock(css, marker, label = marker) {
    const start = css.indexOf(marker);
    assert.notEqual(start, -1, `marker "${label}" tidak ditemukan — guard wajib direvisi bersama perubahan CSS`);
    let depth = 0, end = css.length;
    for (let i = css.indexOf('{', start); i < css.length; i++) {
        if (css[i] === '{') depth++;
        else if (css[i] === '}') { depth--; if (depth === 0) { end = i + 1; break; } }
    }
    return css.slice(start, end);
}

function inlineScripts(html) {
    const out = [];
    const re = /<script(?![^>]*\bsrc=)[^>]*>([\s\S]*?)<\/script>/g;
    let m;
    while ((m = re.exec(html))) out.push(m[1]);
    return out;
}

// Buang konstruksi template Go agar skrip inline valid dieksekusi vm.
function stripGo(src) {
    return src
        .replace(/\{\{if[\s\S]*?\}\}([\s\S]*?)\{\{else\}\}[\s\S]*?\{\{end\}\}/g, '$1')
        .replace(/\{\{[\s\S]*?\}\}/g, '');
}

let activeEl = null;

function makeClassList() {
    const set = new Set();
    return {
        add: (c) => set.add(c),
        remove: (c) => set.delete(c),
        contains: (c) => set.has(c),
        toggle(c) {
            if (set.has(c)) { set.delete(c); return false; }
            set.add(c); return true;
        },
    };
}

function makeEl(id = '') {
    const el = {
        id, value: '', innerHTML: '', textContent: '', disabled: false,
        type: '', style: {}, focused: false, listeners: {}, attributes: {},
    };
    el.classList = makeClassList();
    el.addEventListener = (t, fn) => { (el.listeners[t] ||= []).push(fn); };
    el.setAttribute = (k, v) => { el.attributes[k] = String(v); };
    el.removeAttribute = (k) => { delete el.attributes[k]; };
    el.getAttribute = (k) => (k in el.attributes ? el.attributes[k] : null);
    el.focus = () => { el.focused = true; activeEl = el; };
    el.select = () => {};
    return el;
}

function makeDoc(elements, lists = {}) {
    const docListeners = {};
    const document = {
        getElementById: (id) => elements[id] || null,
        querySelector: () => elements.csrf || null,
        querySelectorAll: (sel) => lists[sel] || [],
        addEventListener: (t, fn) => { docListeners[t] = fn; },
        body: { style: {} },
        get activeElement() { return activeEl; },
    };
    return { docListeners, document };
}

function runPageScript(html, needle, env) {
    const src = inlineScripts(html).map(stripGo).find((s) => s.includes(needle));
    assert.ok(src, `skrip inline berisi "${needle}" tidak ditemukan`);
    const ctx = {
        window: {},
        document: env.document,
        sessionStorage: { getItem: () => null, setItem: () => {} },
        setInterval: () => 1,
        clearInterval: () => {},
        fetch: () => Promise.resolve({ ok: true, json: async () => ({ success: true, message: 'Kode baru terkirim.' }) }),
    };
    ctx.globalThis = ctx;
    vm.runInNewContext(src, ctx, { filename: 'template-inline.vm' });
    return ctx;
}

function fireReady(docListeners) {
    const cb = docListeners['DOMContentLoaded'];
    assert.ok(cb, 'handler DOMContentLoaded tidak terpasang');
    cb();
}

async function flush() {
    await new Promise((r) => setTimeout(r, 0));
    await new Promise((r) => setTimeout(r, 0));
}

function makeOtpEnv(extraElements = {}) {
    const digits = Array.from({ length: 6 }, (_, i) => makeEl(`otp-digit-${i + 1}`));
    const elements = {
        otp_code: makeEl('otp_code'),
        csrf: makeEl('csrf'),
        ...Object.fromEntries(digits.map((d) => [d.id, d])),
        ...extraElements,
    };
    const doc = makeDoc(elements, { '.otp-digit': digits });
    return { digits, elements, ...doc };
}

// ════════════════════════════════════════════════════════════════════════
// S97 — drawer nav mobile keluar tab-order saat tertutup + fokus kembali
// ════════════════════════════════════════════════════════════════════════

test('S97 (statik): drawer tertutup visibility:hidden dengan delay transisi (bukan cuma right:-100%)', () => {
    const rules = [...MOBILE_CSS.matchAll(/([^{}]+)\{([^{}]*)\}/g)]
        .map((m) => ({ sel: m[1].trim().split('\n').pop().trim(), body: m[2] }));
    const closed = rules.find((r) => r.sel === '.nav-links' && /right:\s*-100%/.test(r.body));
    assert.ok(closed, 'rule drawer mobile .nav-links (right:-100%) hilang — kontrak direvisi?');
    assert.match(closed.body, /visibility:\s*hidden/,
        'drawer tertutup masih menerima fokus keyboard (WCAG 2.4.3) — tambah visibility:hidden');
    assert.match(closed.body, /visibility[^;}]*delay|,\s*visibility\s+0s/,
        'visibility harus ditunda (transisi delay) agar slide-out tetap halus');
});

test('S97 (statik): state .open visibility:visible tanpa delay agar slide-in langsung tampak', () => {
    const openRule = MOBILE_CSS.match(/\.nav-links\.open\s*\{[^}]*\}/);
    assert.ok(openRule, '.nav-links.open hilang dari public-mobile.css');
    assert.match(openRule[0], /visibility:\s*visible/);
});

test('S97 (statik): reset desktop (min-width:1101px) menegaskan visibility:visible', () => {
    const desk = DESKTOP_CSS.match(/\.nav-links\s*\{[^}]*\}/);
    assert.ok(desk, 'rule .nav-links desktop hilang');
    assert.match(desk[0], /visibility:\s*visible/,
        'tanpa reset ini nav desktop ikut hidden oleh rule dasar mobile');
});

function navEnv() {
    const links = makeEl('navLinks');
    const overlay = makeEl('navOverlay');
    const burger = makeEl('navHamburger');
    const doc = makeDoc(
        { navLinks: links, navOverlay: overlay, navHamburger: burger },
        { '[data-nav-toggle]': [overlay, burger] },
    );
    return { links, overlay, burger, ...doc };
}

test('S97 (vm): closeMenu mengembalikan fokus ke #navHamburger setelah drawer terbuka', () => {
    activeEl = null;
    const env = navEnv();
    const ctx = runPageScript(SHARED, 'closeMenu', env);
    assert.equal(typeof ctx.toggleMenu, 'function');
    ctx.toggleMenu();
    assert.ok(env.links.classList.contains('open'), 'drawer harus terbuka setelah toggleMenu');
    ctx.closeMenu();
    assert.equal(activeEl, env.burger, 'fokus harus pindah ke tombol hamburger setelah tutup');
    assert.equal(env.burger.attributes['aria-expanded'], 'false');
});

test('S97 (vm): Escape/closeMenu saat drawer TIDAK terbuka tidak mencuri fokus', () => {
    activeEl = null;
    const other = makeEl('otherControl');
    activeEl = other;
    const env = navEnv();
    const ctx = runPageScript(SHARED, 'closeMenu', env);
    ctx.closeMenu();
    assert.equal(activeEl, other, 'closeMenu pada drawer tertutup tidak boleh memindahkan fokus');
});

// ════════════════════════════════════════════════════════════════════════
// S98 — one-time-code pindah ke input gabungan + distribusi multi-karakter
// ════════════════════════════════════════════════════════════════════════

for (const [name, html] of [['register_confirm.html', REG_CONFIRM], ['reset_password.html', RESET_PW]]) {
    test(`S98 (statik, dikalibrasi T30/Batch 16): ${name} — one-time-code tepat satu di kotak digit PERTAMA visible; hidden bersih`, () => {
        const digitInputs = html.match(/<input[^>]*class="otp-digit"[^>]*>/g) || [];
        assert.ok(digitInputs.length === 6, `harus ada 6 kotak digit di ${name}`);
        for (const [i, inp] of digitInputs.entries()) {
            if (i === 0) {
                // T30: mesin autofill OS melewatkan field non-visible — lokasi
                // yang benar adalah kotak pertama yang VISIBLE & fokusable,
                // tanpa maxlength pemotong isian utuh.
                assert.match(inp, /one-time-code/,
                    'kotak digit pertama wajib membawa autocomplete="one-time-code" (T30)');
                assert.doesNotMatch(inp, /maxlength="1"/,
                    'kotak digit pertama tanpa maxlength pemotong (T30)');
            } else {
                assert.doesNotMatch(inp, /one-time-code/,
                    'hanya kotak digit pertama yang membawa one-time-code');
            }
        }
        const occ = html.match(/<input[^>]*autocomplete="one-time-code"[^>]*>/g) || [];
        assert.equal(occ.length, 1, 'tepat SATU input yang membawa autocomplete="one-time-code"');
        assert.doesNotMatch(occ[0], /type="hidden"/,
            'input hidden TIDAK boleh membawa one-time-code — autofill OS melewatinya (T30)');
    });

    test(`S98 (vm): ${name} — tempel/input 6 digit sekaligus terdistribusi antar kotak`, () => {
        const extra = name.startsWith('register')
            ? {}
            : {
                resetSubmitBtn: makeEl('resetSubmitBtn'),
                otpForm: makeEl('resetForm'),
                pwMatchError: makeEl('pwMatchError'),
                password: makeEl('password'),
                password_confirm: makeEl('password_confirm'),
                togglePassword: makeEl('togglePassword'),
                togglePasswordConfirm: makeEl('togglePasswordConfirm'),
                pwStrengthBar: makeEl('pwStrengthBar'),
                pwBarFill: makeEl('pwBarFill'),
                pwStrengthText: makeEl('pwStrengthText'),
            };
        const env = makeOtpEnv({
            ...extra,
            ...(name.startsWith('register') ? { verifyBtn: makeEl('verifyBtn'), otpForm: makeEl('otpForm') } : {}),
        });
        runPageScript(name.startsWith('register') ? REG_CONFIRM : RESET_PW, 'otp-digit', env);
        fireReady(env.docListeners);
        const handler = env.digits[0].listeners.input?.[0];
        assert.ok(handler, 'listener input kotak digit pertama tidak terpasang');
        env.digits[0].value = '123456';
        handler.call(env.digits[0], {});
        assert.deepEqual(env.digits.map((d) => d.value), ['1', '2', '3', '4', '5', '6'],
            'kode utuh harus didistribusikan antar kotak (autofill OS menyalin 6 karakter ke satu kotak)');
        assert.equal(env.elements.otp_code.value, '123456', 'hidden gabungan tersinkron');
    });
}

// ════════════════════════════════════════════════════════════════════════
// S105 — reset_password lepas dari admin-core.js
// ════════════════════════════════════════════════════════════════════════

test('S105 (statik): reset_password.html bebas referensi admin-core.js & punya wirePwToggle lokal', () => {
    assert.doesNotMatch(RESET_PW, /admin-core\.js/,
        'halaman anonim tidak boleh memuat bundle admin hanya untuk toggle password');
    assert.match(RESET_PW, /function\s+wirePwToggle\s*\(/, 'helper lokal wirePwToggle ala register.html wajib ada');
    assert.match(RESET_PW, /wirePwToggle\(\s*'password'\s*,\s*'togglePassword'\s*\)/);
    assert.match(RESET_PW, /wirePwToggle\(\s*'password_confirm'\s*,\s*'togglePasswordConfirm'\s*\)/);
    assert.doesNotMatch(RESET_PW, /togglePasswordVisibility/,
        'referensi helper admin-core harus hilang total (kalau tidak, toggle mati senyap)');
});

test('S105 (vm): kedua toggle password reset_password tetap ter-wire dan berfungsi', () => {
    const env = makeOtpEnv({
        resetSubmitBtn: makeEl('resetSubmitBtn'),
        otpForm: makeEl('resetForm'),
        pwMatchError: makeEl('pwMatchError'),
        password: makeEl('password'),
        password_confirm: makeEl('password_confirm'),
        togglePassword: makeEl('togglePassword'),
        togglePasswordConfirm: makeEl('togglePasswordConfirm'),
        pwStrengthBar: makeEl('pwStrengthBar'),
        pwBarFill: makeEl('pwBarFill'),
        pwStrengthText: makeEl('pwStrengthText'),
    });
    runPageScript(RESET_PW, 'wirePwToggle', env);
    fireReady(env.docListeners);
    for (const [pwId, btnId] of [['password', 'togglePassword'], ['password_confirm', 'togglePasswordConfirm']]) {
        const pw = env.elements[pwId];
        const btn = env.elements[btnId];
        pw.type = 'password';
        const click = btn.listeners.click?.[0];
        assert.ok(click, `toggle ${btnId} tidak ter-wire`);
        click();
        assert.equal(pw.type, 'text', `${pwId} harus tampil sebagai teks setelah klik`);
        assert.equal(btn.attributes['aria-label'], 'Sembunyikan password', 'aria-label harus berganti');
        click();
        assert.equal(pw.type, 'password');
        assert.equal(btn.attributes['aria-label'], 'Tampilkan password');
    }
});

// ════════════════════════════════════════════════════════════════════════
// R107 — resend OTP sukses membersihkan digit lama
// ════════════════════════════════════════════════════════════════════════

test('R107 (vm): resend sukses membersihkan digit + fokus digit-1 (register_confirm)', async () => {
    activeEl = null;
    const resendLink = makeEl('resendLink');
    const resendTimer = makeEl('resendTimer');
    const resendMsg = makeEl('resendMsg');
    const env = makeOtpEnv({
        verifyBtn: makeEl('verifyBtn'),
        otpForm: makeEl('otpForm'),
        resendLink, resendTimer, resendMsg,
    });
    const ctx = runPageScript(REG_CONFIRM, 'resendOtp', env);
    fireReady(env.docListeners);
    env.digits.forEach((d, i) => { d.value = String((i + 7) % 10); d.classList.add('filled'); });
    assert.ok(ctx.window.resendOtp, 'window.resendOtp harus terekspose');
    ctx.window.resendOtp({ preventDefault() {} });
    await flush();
    assert.deepEqual(env.digits.map((d) => d.value), ['', '', '', '', '', ''],
        'digit lama wajib dikosongkan saat kode baru terkirim (server merotasikan kode)');
    assert.equal(env.elements.otp_code.value, '', 'hidden gabungan ikut kosong');
    assert.ok(env.digits.every((d) => !d.classList.contains('filled')), 'kelas .filled turun');
    assert.equal(activeEl, env.digits[0], 'fokus pindah ke digit-1 untuk mengetik kode baru');
    assert.equal(env.elements.verifyBtn.disabled, true, 'submit kembali ter-gating sampai kode lengkap');
});

// ════════════════════════════════════════════════════════════════════════
// R109 — masa berlaku OTP 15 menit dikomunikasikan UI
// ════════════════════════════════════════════════════════════════════════

test('R109 (statik): helper text register_confirm menyebut "berlaku 15 menit"', () => {
    assert.match(REG_CONFIRM, /berlaku 15 menit/i,
        'helper OTP register_confirm wajib menyebut masa berlaku 15 menit (TTL auth_recovery.go:23)');
});

test('R109 (statik): helper text reset_password menyebut "berlaku 15 menit"', () => {
    assert.match(RESET_PW, /berlaku 15 menit/i);
});

test('R109 (statik): jalur resend register_confirm juga menyampaikan masa berlaku', () => {
    const script = inlineScripts(REG_CONFIRM).map(stripGo).find((s) => s.includes('resendOtp'));
    assert.ok(script, 'skrip resend tidak ditemukan');
    assert.match(script, /berlaku 15 menit/i,
        'pesan resend sukses wajib mengulang informasi "berlaku 15 menit"');
});

// ════════════════════════════════════════════════════════════════════════
// R108 — download.html berhenti memuat ulang CSS publik
// ════════════════════════════════════════════════════════════════════════

test('R108 (statik): download.html tidak lagi me-link public-mobile/public-desktop.css', () => {
    assert.doesNotMatch(DOWNLOAD, /<link[^>]*href="\/static\/css\/public-mobile\.css/,
        'link dobel per kunjungan — public_head (shared.html) sudah memancarkan keduanya');
    assert.doesNotMatch(DOWNLOAD, /<link[^>]*href="\/static\/css\/public-desktop\.css/);
});

// ════════════════════════════════════════════════════════════════════════
// S96 — reset -webkit-text-fill-color di blok @media print
// ════════════════════════════════════════════════════════════════════════

test('S96 (statik): blok print me-reset text-fill gradien judul (.header-title, .exam-hero h1)', () => {
    const printBlock = extractBlock(HASIL_CSS, '@media print');
    const resetRule = printBlock.match(/[^{}]*\.header-title[^{}]*\{[^}]*\}/);
    assert.ok(resetRule, 'tidak ada rule reset untuk judul ber-gradien di blok print');
    assert.match(resetRule[0], /\.exam-hero h1/, 'kedua selector gradien (:102,:156) wajib di-reset bersama');
    assert.match(resetRule[0], /-webkit-text-fill-color:\s*(initial|auto)/,
        '-webkit-text-fill-color: transparent lolos ke media cetak — teks tercetak kosong');
    assert.match(resetRule[0], /background:\s*none/,
        'latar gradien juga harus dinetralkan saat cetak');
});

// ════════════════════════════════════════════════════════════════════════
// S101 — kontras teks badge/chip skor ke varian terang
// ════════════════════════════════════════════════════════════════════════

test('S101 (statik): .score-low & .score-high pakai varian terang untuk TEKS (tint tetap)', () => {
    const low = HASIL_CSS.match(/\.score-low\s*\{[^}]*\}/);
    const high = HASIL_CSS.match(/\.score-high\s*\{[^}]*\}/);
    assert.ok(low && high);
    // Kalibrasi koordinator: base #ef4444 di tint sendiri ≈ 4.45:1 (< 4.5 AA);
    // --color-danger-bright ≈ 6.25:1.
    assert.match(low[0], /color:\s*var\(--color-danger-bright\)/,
        'teks .score-low borderline AA — migrasi ke --color-danger-bright (sekitar 6.25:1)');
    assert.match(low[0], /background:\s*rgba\(var\(--rgb-danger\),\s*0\.15\)|background:\s*rgba\(239,\s*68,\s*68,\s*0\.15\)/,
        'tint latar dipertahankan (yang dimigrasi hanya warna teks)');
    assert.match(high[0], /color:\s*var\(--color-success-light\)/,
        'teks .score-high migrasi ke --color-success-light (sekitar 10.8:1)');
});

test('S101 (statik): chip status kelulusan hasil.html ikut ke varian terang', () => {
    const pass = HASIL_HTML.match(/\.score-status-pass\s*\{[^}]*\}/);
    const fail = HASIL_HTML.match(/\.score-status-fail\s*\{[^}]*\}/);
    assert.ok(pass && fail);
    assert.match(pass[0], /color:\s*var\(--color-success-light\)/,
        '.score-status-pass base token hijau menyentuh garis AA — pakai success-light');
    assert.match(fail[0], /color:\s*var\(--color-danger-bright\)/,
        '.score-status-fail sekitar 4.59:1 (menyentuh garis) — pakai danger-bright');
});

// ════════════════════════════════════════════════════════════════════════
// S103 — adopt-or-delete token mati theme.css
// ════════════════════════════════════════════════════════════════════════

function allSources() {
    const files = [];
    const walk = (dir) => {
        for (const f of fs.readdirSync(dir)) {
            const p = path.join(dir, f);
            if (fs.statSync(p).isDirectory()) walk(p);
            else if (/\.(css|html|js|mjs)$/.test(f) && !f.endsWith('.min.js')) files.push(p);
        }
    };
    walk(path.join(WEBUI_ROOT, 'static'));
    walk(PUBLIC);
    return files.filter((p) => !p.endsWith('uiux-batch15-publik.test.mjs'));
}

test('S103 (kontrak): tiap token daftar punya pemakaian >= 1 atau definisinya dihapus', () => {
    const tokens = ['--color-success-bg', '--color-danger-bg', '--color-warning-bg', '--shadow-card', '--shadow-lg'];
    const sources = allSources().filter((p) => path.basename(p) !== 'theme.css').map((p) => read(p));
    for (const tok of tokens) {
        const defined = new RegExp(`\\${tok}\\s*:`, 'm').test(THEME_CSS);
        const used = sources.some((src) => src.includes(`var(${tok})`));
        if (defined) {
            assert.ok(used, `token ${tok} didefinisikan theme.css tapi mati (0 pemakai) — adopsi atau hapus definisinya`);
        }
        assert.ok(defined || used, `token ${tok} lenyap tanpa jejak — revisi kontrak?`);
    }
});

test('S103 (adopsi-a): chip hasil.html memakai var(--color-*-bg) — nilai identik rgba manual', () => {
    // --color-success-bg = rgba(16,185,129,0.12) == rgba(var(--rgb-success),0.12)
    const pass = HASIL_HTML.match(/\.score-status-pass\s*\{[^}]*\}/);
    const fail = HASIL_HTML.match(/\.score-status-fail\s*\{[^}]*\}/);
    const mid = HASIL_HTML.match(/\.score-status-mid\s*\{[^}]*\}/);
    assert.ok(pass && fail && mid);
    assert.match(pass[0], /background:\s*var\(--color-success-bg\)/);
    assert.match(mid[0], /background:\s*var\(--color-warning-bg\)/);
    assert.match(fail[0], /background:\s*var\(--color-danger-bg\)/);
});

test('S103 (adopsi-b): --shadow-card hidup di .glass-card hasil.css; --shadow-lg menggantikan literal identik shared.html', () => {
    const glass = extractBlock(HASIL_CSS, '.glass-card', '.glass-card (hasil.css)');
    assert.match(glass, /box-shadow:\s*var\(--shadow-card\)/,
        'elevasi glass card adalah pemakaian alami --shadow-card');
    assert.match(SHARED, /box-shadow:\s*var\(--shadow-lg\)/,
        'mockup-wrapper shared.html menduplikasi nilai --shadow-lg persis — pakai token');
});

// ════════════════════════════════════════════════════════════════════════
// R106 — touch target halaman hasil 44px (standar admin-base.css:514-522)
// ════════════════════════════════════════════════════════════════════════

test('R106 (statik): target sentuh mobile hasil.css minimal 44px', () => {
    let block = null;
    const re = /@media \(max-width:\s*768px\)\s*\{/g;
    let m;
    while ((m = re.exec(HASIL_CSS))) {
        const candidate = extractBlock(HASIL_CSS.slice(m.index), '@media (max-width: 768px)');
        if (candidate.includes('.search-clear-btn')) { block = candidate; break; }
    }
    assert.ok(block, 'blok touch-target (search-clear-btn) tidak ditemukan');
    const pxOf = (sel, prop) => {
        const rule = block.match(new RegExp(`${sel}\\s*\\{[^}]*\\}`));
        assert.ok(rule, `${sel} hilang dari blok touch-target`);
        const num = rule[0].match(new RegExp(`${prop}:\\s*(\\d+)px`));
        assert.ok(num, `${sel} tidak mendeklarasikan ${prop}`);
        return Number(num[1]);
    };
    assert.ok(pxOf('.search-clear-btn', 'width') >= 44 && pxOf('.search-clear-btn', 'height') >= 44,
        'search-clear-btn 38px < standar repo 44px');
    assert.ok(pxOf('.back-nav', 'min-height') >= 44);
    assert.ok(pxOf('.tab-btn', 'min-height') >= 44);
    assert.ok(pxOf('.pagination-btn', 'min-height') >= 44 && pxOf('.pagination-btn', 'min-width') >= 44);
});

// ════════════════════════════════════════════════════════════════════════
// R117 — reduced-motion meng-cap animation-iteration-count
// ════════════════════════════════════════════════════════════════════════

test('R117 (statik): blok prefers-reduced-motion hasil.css memuat animation-iteration-count: 1', () => {
    const block = extractBlock(HASIL_CSS, '@media (prefers-reduced-motion');
    assert.match(block, /animation-iteration-count:\s*1\b/,
        'animasi infinite (mis. pulse) tetap berjalan terus walau durasi dipangkas — cap iteration-count: 1');
});
