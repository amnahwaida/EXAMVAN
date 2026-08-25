/**
 * ══════════════════════════════════════════════════════════════════════════
 * Suite UI/UX BATCH 16 — PUBLIK (agen batch16-publik)
 * ══════════════════════════════════════════════════════════════════════════
 *
 * Referensi: review_uiux_webui.md bagian 5.13 RE-REVIEW RONDE 10. Cakupan
 * temuan milik agen ini:
 *
 *   T30  — Regresi S98 (Batch 15): autocomplete="one-time-code" menempel pada
 *          input type="hidden" #otp_code (register_confirm.html,
 *          reset_password.html) — mesin autofill OTP OS melewatkan input
 *          yang tak dirender/tak dapat fokus sehingga autofill iOS/Android
 *          MATI TOTAL. Kontrak: atribut pindah ke kotak digit PERTAMA yang
 *          visible (#otp-digit-1), maxlength="1" kotak pertama DIHAPUS agar
 *          isian utuh OS tidak terpotong (distribusi multi-karakter sudah
 *          ada), dan input hidden tidak lagi membawa atribut itu.
 *          Kontrak vm: ketik/tempel '123456' pada digit-1 terdistribusi ke 6
 *          kotak, syncHidden mengisi gabungan, tombol verifikasi enabled.
 *
 *   T31  — Drawer nav mati di perangkat sentuh >=1101px + link off-canvas
 *          tetap tabbable (gap cakupan S97): reset visibility:visible layer
 *          desktop bersifat tanpa-syarat dan burger disembunyikan di semua
 *          perangkat >=1101px. Keputusan: GABUNGAN opsi A+B. Opsi B
 *          (shared.html): html.touch-device .nav-links:not(.open)
 *          visibility:hidden dengan pola delay-transisi identik S97 —
 *          spesifisitas (0,3,1) mengalahkan reset layer mana pun. Opsi A
 *          (public-mobile.css): penyembunyian burger digerdarkan dengan
 *          media query gabungan (min-width:1101px) and (hover:hover) and
 *          (pointer:fine), sehingga di touch-device lebar burger tetap
 *          tersedia dan drawer tetap menjadi jalur navigasi.
 *
 *   R130 — meta theme-color absen di 5 halaman auth publik (register,
 *          register_confirm, reset_password, forgot_password, cek_hasil);
 *          hanya shared.html yang punya. Kontrak statik x5: nilai sama
 *          dengan shared.html (#09090e == --color-bg theme.css).
 *
 *   R131 — Guard "{{ if .android_app }}" membungkus loop rilis Android
 *          tambahan (download.html) sehingga bila app resmi kosong tapi
 *          ada system_apps Android lain, entri tak dirender. Kontrak
 *          statik: guard menjadi .system_apps + komentar penjelas +
 *          pembanding ID nil-safe (and/or Go template short-circuit).
 *          Catatan penulisan: nama field template ditulis tanpa kurung
 *          kurawal ganda dalam komentar agar parser Go tidak mengevaluasi.
 *
 *   R127 — Print: gradien -webkit-text-fill-color:transparent di luar
 *          cakupan S96 (shared.html .logo-text/.hero-title/.section-title/
 *          .stat-val; download.html .hero-title; public-mobile.css
 *          .logo-text) belum di-reset di @media print — teks tercetak
 *          kosong. Kontrak: blok print minimal per file me-reset
 *          -webkit-text-fill-color (+ background:none), TANPA !important.
 *
 *   R125 (bagian publik) — .nav-link public-mobile.css masih memakai
 *          warna muted literal #9ca3af (keluarga gray Tailwind). Kontrak:
 *          migrasi ke var(--color-text-muted) token eksisting theme.css;
 *          nol literal baru.
 *
 * Kepemilikan file agen ini: templates/public/{register_confirm,
 *   reset_password,register,forgot_password,cek_hasil,shared,download,
 *   index,hasil}.html, static/css/{public-mobile,public-desktop}.css,
 *   static/js/uiux-batch16-publik.test.mjs (BARU).
 *
 * Metode: guard statik fs-read + eksekusi perilaku via vm.runInNewContext
 * dengan stub DOM minimal (pola uiux-batch15-publik). Catatan penulisan:
 * jangan menulis pola glob dua-bintang di dalam komentar blok ini.
 *
 * Run with:  node --test static/js/uiux-batch16-publik.test.mjs   (from webui/)
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
const REGISTER = read(PUBLIC, 'register.html');
const FORGOT_PW = read(PUBLIC, 'forgot_password.html');
const CEK_HASIL = read(PUBLIC, 'cek_hasil.html');
const DOWNLOAD = read(PUBLIC, 'download.html');
const MOBILE_CSS = read(WEBUI_ROOT, 'static', 'css', 'public-mobile.css');

// ── Util ───────────────────────────────────────────────────────────────────

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
        fetch: () => Promise.resolve({ ok: true, json: async () => ({ success: true }) }),
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
// T30 — one-time-code pindah ke kotak digit visible pertama (regresi S98)
// ════════════════════════════════════════════════════════════════════════

for (const [name, html] of [['register_confirm.html', REG_CONFIRM], ['reset_password.html', RESET_PW]]) {
    test(`T30 (statik): ${name} — tepat SATU input VISIBLE bawa one-time-code (digit-1)`, () => {
        const occ = html.match(/<input[^>]*autocomplete="one-time-code"[^>]*>/g) || [];
        assert.equal(occ.length, 1, 'tepat satu input yang membawa autocomplete="one-time-code"');
        // Harus kotak digit pertama yang visible — bukan input hidden.
        assert.match(occ[0], /class="otp-digit"/, 'pembawa one-time-code harus kotak digit visible');
        assert.match(occ[0], /id="otp-digit-1"/, 'kotak penerima autofill OS adalah digit PERTAMA (fokus awal)');
        assert.doesNotMatch(occ[0], /type="hidden"/, 'autofill OS melewatkan input hidden — atribut di situ mematikan autofill');
    });

    test(`T30 (statik): ${name} — input hidden #otp_code bebas one-time-code`, () => {
        const hidden = html.match(/<input[^>]*id="otp_code"[^>]*>/g) || [];
        assert.ok(hidden.length === 1, 'input gabungan #otp_code wajib ada (logika server tak berubah)');
        assert.doesNotMatch(hidden[0], /autocomplete/,
            'hidden composite tidak boleh membawa autocomplete apa pun (regresi T30)');
    });

    test(`T30 (statik): ${name} — kotak digit pertama tanpa maxlength="1"`, () => {
        const first = html.match(/<input[^>]*id="otp-digit-1"[^>]*>/);
        assert.ok(first, 'kotak digit pertama hilang');
        assert.doesNotMatch(first[0], /maxlength="1"/,
            'maxlength=1 memotong isian utuh OS sebelum distribusi multi-karakter jalan');
    });

    test(`T30 (vm): ${name} — ketik '123456' di digit-1 terdistribusi + syncHidden + submit enabled`, () => {
        const extra = name.startsWith('register')
            ? { verifyBtn: makeEl('verifyBtn'), otpForm: makeEl('otpForm') }
            : {
                resetSubmitBtn: makeEl('resetSubmitBtn'),
                otpForm: makeEl('otpForm'),
                pwMatchError: makeEl('pwMatchError'),
                password: makeEl('password'),
                password_confirm: makeEl('password_confirm'),
                togglePassword: makeEl('togglePassword'),
                togglePasswordConfirm: makeEl('togglePasswordConfirm'),
                pwStrengthBar: makeEl('pwStrengthBar'),
                pwBarFill: makeEl('pwBarFill'),
                pwStrengthText: makeEl('pwStrengthText'),
            };
        const env = makeOtpEnv(extra);
        runPageScript(html, 'otp-digit', env);
        fireReady(env.docListeners);
        const submitId = name.startsWith('register') ? 'verifyBtn' : 'resetSubmitBtn';
        const handler = env.digits[0].listeners.input?.[0];
        assert.ok(handler, 'listener input kotak digit pertama tidak terpasang');
        // Simulasi autofill OS: string utuh mendarat di satu kotak.
        env.digits[0].value = '123456';
        handler.call(env.digits[0], {});
        assert.deepEqual(env.digits.map((d) => d.value), ['1', '2', '3', '4', '5', '6'],
            'kode utuh harus didistribusikan antar kotak');
        assert.equal(env.elements.otp_code.value, '123456', 'hidden gabungan tersinkron');
        assert.equal(env.elements[submitId].disabled, false, 'tombol verifikasi/simpan harus enabled setelah kode lengkap');
    });
}

test('T30 (statik): keputusan autocomplete level-form didokumentasikan di kedua halaman', () => {
    // Keputusan: register_confirm MELEPAS autocomplete="off" level form
    // (aman — form hanya berisi OTP + field tersembunyi); reset_password
    // MEMPERTAHANKANNYA (melindungi field password dari credential manager).
    assert.match(REG_CONFIRM, /\bT30\b/, 'penanda keputusan T30 wajib dikomentari');
    assert.match(RESET_PW, /\bT30\b/, 'penanda keputusan T30 wajib dikomentari');
    assert.match(REG_CONFIRM, /MELEPAS|DILEPAS/, 'keputusan pelepasan autocomplete form wajib dikomentari');
    assert.match(RESET_PW, /MEMPERTAHAN|DIPERTAHAN/, 'keputusan mempertahankan autocomplete form wajib dikomentari');
});

// ════════════════════════════════════════════════════════════════════════
// T31 — drawer nav hidup lagi + keluar tab-order di touch-device lebar
// ════════════════════════════════════════════════════════════════════════

test('T31 (statik, opsi B): touch-device drawer tertutup visibility:hidden dengan delay-transisi ala S97', () => {
    // Rule inline shared.html (blok html.touch-device) — spesifisitas (0,3,1)
    // mengalahkan reset visibility:visible layer desktop/mobile (0,1,0).
    const rule = SHARED.match(/html\.touch-device \.nav-links:not\(\.open\)\s*\{[^}]*\}/);
    assert.ok(rule, 'rule html.touch-device .nav-links:not(.open) hilang dari shared.html');
    assert.match(rule[0], /visibility:\s*hidden/,
        'drawer tertutup di touch-device (termasuk >=1101px) wajib keluar tab-order (WCAG 2.4.3)');
    assert.match(rule[0], /visibility[^;}]*delay|,\s*visibility\s+0s/,
        'visibility ditunda (delay transisi) agar slide-out tetap halus — pola S97');
    const openRule = SHARED.match(/html\.touch-device \.nav-links\.open\s*\{[^}]*\}/);
    assert.ok(openRule, 'rule open touch-device hilang');
    assert.match(openRule[0], /visibility:\s*visible/, 'state .open tetap visible (tanpa delay)');
});

test('T31 (statik, opsi A): sembunyi burger >=1101px digerdarkan (hover:hover)+(pointer:fine)', () => {
    // Semua display:none untuk .nav-hamburger di konteks min-width:1101px
    // harus berada di dalam media query gabungan pointer-halus, sehingga
    // touch-device lebar (iPad landscape dsb.) tetap punya hamburger.
    const mqRe = /@media([^{]+)\{/g;
    let m;
    const offenders = [];
    while ((m = mqRe.exec(MOBILE_CSS))) {
        const cond = m[1];
        if (!/min-width:\s*1101px/.test(cond)) continue;
        const body = extractBlock(MOBILE_CSS.slice(m.index), `@media${cond}`);
        if (/\.nav-hamburger[^{}]*\{[^}]*display:\s*none/.test(body) &&
            !(/hover:\s*hover/.test(cond) && /pointer:\s*fine/.test(cond))) {
            offenders.push(cond.trim());
        }
    }
    assert.deepEqual(offenders, [],
        'burger masih disembunyikan untuk SEMUA perangkat >=1101px — gerdarkan dengan (hover:hover) and (pointer:fine)');
    const gated = MOBILE_CSS.match(/@media[^{]*min-width:\s*1101px[^{]*hover:\s*hover[^{]*pointer:\s*fine[^{]*\{/);
    assert.ok(gated, 'media query gabungan pointer-halus untuk burger tidak ditemukan');
});

// ════════════════════════════════════════════════════════════════════════
// R130 — meta theme-color di 5 halaman auth publik
// ════════════════════════════════════════════════════════════════════════

for (const [name, html] of [
    ['register.html', REGISTER],
    ['register_confirm.html', REG_CONFIRM],
    ['reset_password.html', RESET_PW],
    ['forgot_password.html', FORGOT_PW],
    ['cek_hasil.html', CEK_HASIL],
]) {
    test(`R130 (statik): ${name} membawa meta theme-color #09090e dekat viewport`, () => {
        const meta = html.match(/<meta[^>]*name="theme-color"[^>]*>/);
        assert.ok(meta, 'meta theme-color absen — address-bar mobile tak sinkron brand');
        assert.match(meta[0], /content="#09090e"/,
            'nilai wajib #09090e == --color-bg theme.css (paritas shared.html/S34)');
        const vp = html.indexOf('name="viewport"');
        assert.ok(vp !== -1 && html.indexOf(meta[0]) > vp - 200 && html.indexOf(meta[0]) < vp + 400,
            'meta theme-color ditaruh dekat viewport meta di head');
    });
}

// ════════════════════════════════════════════════════════════════════════
// R131 — loop rilis Android tambahan tidak lagi bergantung app resmi
// ════════════════════════════════════════════════════════════════════════

test('R131 (statik): download.html guard loop rilis tambahan = system_apps + komentar + nil-safe', () => {
    // Guard lama membungkus loop dengan kondisi app resmi — bila BestAndroidApp
    // kosong tapi stok system_apps android ada, kartu "Belum ada installer"
    // tampil padahal rilis tersedia (dead-branch).
    const deadGuard = DOWNLOAD.match(/\{\{\s*if \.android_app\s*\}\}\s*\{\{\s*range \.system_apps/);
    assert.ok(!deadGuard, 'loop system_apps masih dibungkus guard .android_app (dead-branch R131)');
    const guard = DOWNLOAD.match(/\{\{\s*if \.system_apps\s*\}\}[\s\S]{0,200}\{\{\s*range \.system_apps/);
    assert.ok(guard, 'guard loop rilis tambahan harus {{ if .system_apps }}');
    // Nil-safe: pembanding ID terhadap app resmi dilindungi (not $.android_app)
    // karena Go template and/or short-circuit (go 1.18+) — render tak pecah
    // saat app resmi kosong.
    assert.match(DOWNLOAD, /or \(not \$\.android_app\) \(ne \.ID \$\.android_app\.ID\)/,
        'pembanding ID wajib nil-safe terhadap app resmi kosong');
    // Komentar penjelas perilaku baru wajib menyebut R131.
    const region = DOWNLOAD.slice(Math.max(0, DOWNLOAD.indexOf('{{ if .system_apps }}') - 600),
        DOWNLOAD.indexOf('{{ if .system_apps }}'));
    assert.match(region, /R131/, 'komentar penjelas perilaku baru (sebut R131) wajib ada di atas guard');
});

// ════════════════════════════════════════════════════════════════════════
// R127 — print: reset -webkit-text-fill-color gradien di luar hasil.css
// ════════════════════════════════════════════════════════════════════════

function printResetContract(label, src, selectors) {
    const idx = src.indexOf('@media print');
    assert.ok(idx !== -1, `${label}: blok @media print tidak ditemukan`);
    const block = extractBlock(src.slice(idx), '@media print');
    // Rule boleh berupa selector-group (.a, .b { ... }) — cari rule yang
    // selector-nya MEMUAT selector target.
    const rules = [...block.matchAll(/([^{}]+)\{([^{}]*)\}/g)]
        .map((m) => ({ sel: m[1], body: m[2] }))
        .filter((r) => !r.sel.includes('@'));
    for (const sel of selectors) {
        const rule = rules.find((r) => r.sel.split(',').some((s) => s.trim() === sel));
        assert.ok(rule, `${label}: selector ${sel} belum di-reset di blok print`);
        assert.match(rule.body, /-webkit-text-fill-color:\s*(initial|auto)/,
            `${label}: ${sel} gradien tercetak kosong tanpa reset text-fill`);
        assert.match(rule.body, /background:\s*none/, `${label}: ${sel} latar gradien ikut dinetralkan`);
    }
    assert.doesNotMatch(block, /!important/, `${label}: blok print tanpa !important dulu`);
}

test('R127 (statik): shared.html print me-reset 4 selector gradien', () => {
    printResetContract('shared.html', SHARED, ['.logo-text', '.hero-title', '.section-title', '.stat-val']);
});

test('R127 (statik): download.html print me-reset hero-title', () => {
    printResetContract('download.html', DOWNLOAD, ['.hero-title']);
});

test('R127 (statik): public-mobile.css print me-reset logo-text', () => {
    printResetContract('public-mobile.css', MOBILE_CSS, ['.logo-text']);
});

// ════════════════════════════════════════════════════════════════════════
// R125 (bagian publik) — .nav-link migrasi warna muted ke token
// ════════════════════════════════════════════════════════════════════════

test('R125 (statik): .nav-link public-mobile.css pakai var(--color-text-muted), tanpa #9ca3af', () => {
    const rule = MOBILE_CSS.match(/\.nav-link\s*\{[^}]*\}/);
    assert.ok(rule, 'rule .nav-link hilang dari public-mobile.css');
    assert.match(rule[0], /color:\s*var\(--color-text-muted\)/,
        'warna muted literal gray-Tailwind migrasi ke token eksisting theme.css');
    assert.doesNotMatch(MOBILE_CSS, /#9ca3af/i, 'literal #9ca3af harus lenyap dari public-mobile.css');
});
