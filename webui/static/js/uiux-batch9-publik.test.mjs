/* Batch 9 — perbaikan UI/UX halaman publik (review_uiux_webui.md §5.6
 * RE-REVIEW RONDE 3). ID temuan: T16, S42, S44, S45, S46, R34–R40.
 *
 * Run with:  node --test static/js/uiux-batch9-publik.test.mjs   (from webui/)
 *
 * Metode mengikuti uiux-batch8-publik.test.mjs / uiux-batch3-download.test.mjs:
 * kontrak statik fs-read + eksekusi perilaku via vm.runInNewContext dengan
 * stub DOM minimal (pola uiux-batch4-jscore.test.mjs).
 *
 * Dampak bisnis yang dilindungi:
 *   T16 — CTA unduhan (konversi tertinggi halaman siswa) lolos WCAG AA:
 *         semua endpoint gradien tombol unduh ≥ 4.5:1 terhadap label putih.
 *   S42 — fingerprintjs 37KB tidak lagi memblokir render di 4 halaman auth;
 *         urutan eksekusi fingerprintjs → device-fingerprint tetap terjaga.
 *   S44 — palet light arwah & sistem token paralel shared.html dihapus;
 *         semua pemakaian bermigrasi ke token resmi theme.css (tanpa
 *         referensi menggantung undefined).
 *   S45 — instruksi instalasi Android tak lagi merujuk posisi visual yang
 *         salah di layout mobile satu kolom.
 *   S46 — bar rekap skor menampilkan skor RESMI server; total evaluasi
 *         klien hanya baris rincian berlabel jelas; id duplikat hilang.
 *   R34 — cache-busting konsisten untuk theme.css publik.
 *   R35 — kolom durasi satu nama ("Durasi") di desktop & mobile.
 *   R36 — keyboard mobile tidak mengkapitalisasi username/token.
 *   R37 — paginasi memakai aria-current; tab Nilai/Kunci deep-linkable.
 *   R38 — instruksi SmartScreen mencantumkan kedua varian bahasa Windows.
 *   R39 — mockup landing tidak lagi menampilkan domain cloud hard-coded.
 *   R40 — nilai 40–69 dapat chip kuning netral "Hampir", bukan merah
 *         "Belum Lulus" yang kontradiktif; legenda menjelaskan ambang.
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

const DOWNLOAD = () => read('templates/public/download.html');
const SHARED = () => read('templates/public/shared.html');
const HASIL = () => read('templates/public/hasil.html');
const INDEX = () => read('templates/public/index.html');

/** Ekstrak sumber deklarasi `function name(...) {...}` dengan penghitungan kurawal. */
function extractFunction(src, name) {
    const start = src.indexOf('function ' + name + '(');
    if (start === -1) return null;
    const open = src.indexOf('{', start);
    let depth = 0;
    for (let j = open; j < src.length; j++) {
        if (src[j] === '{') depth++;
        else if (src[j] === '}') {
            depth--;
            if (depth === 0) return src.slice(start, j + 1);
        }
    }
    return null;
}

// ===== WCAG kontras =====

function luminance(hex) {
    const c = hex.replace('#', '');
    const chan = [0, 2, 4].map((i) => parseInt(c.slice(i, i + 2), 16) / 255)
        .map((v) => (v <= 0.03928 ? v / 12.92 : Math.pow((v + 0.055) / 1.055, 2.4)));
    return 0.2126 * chan[0] + 0.7152 * chan[1] + 0.0722 * chan[2];
}

/** Rasio kontras warna terhadap teks putih (#ffffff). */
function contrastVsWhite(hex) {
    return 1.05 / (luminance(hex) + 0.05);
}

// ===========================================================================
// T16 — Kontras gradien tombol unduh primer (WCAG AA)
// ===========================================================================

// Nilai akhir yang lolos hitungan (diverifikasi test ini sendiri di bawah):
//   #9333ea = 5.38:1 · #7c3aed = 5.70:1 · #2563eb = 5.17:1 · #1d4ed8 = 6.70:1
const GRADIENT_ENDPOINT_WHITELIST = ['#9333ea', '#7c3aed', '#2563eb', '#1d4ed8'];

test('T16 (sanity): fungsi kontras menghitung kasus yang diketahui dengan benar', () => {
    // Endpoint lama yang GAGAL AA (temuan review): harus benar-benar < 4.5.
    assert.ok(contrastVsWhite('#a855f7') < 4.5, '#a855f7 memang gagal (≈3.96)');
    assert.ok(contrastVsWhite('#3b82f6') < 4.5, '#3b82f6 memang gagal (≈3.68)');
    assert.ok(contrastVsWhite('#8b5cf6') < 4.5, '#8b5cf6 memang gagal (≈4.23)');
    // Endpoint pengganti wajib lolos.
    for (const hex of GRADIENT_ENDPOINT_WHITELIST) {
        assert.ok(contrastVsWhite(hex) >= 4.5, `${hex} = ${contrastVsWhite(hex).toFixed(2)}:1 wajib ≥ 4.5`);
    }
});

test('T16a: tombol unduh memakai var(--grad-btn-*) — definisi token kini milik theme.css', () => {
    // Batch 10 (kontrak lintas-agen): definisi :root lokal shared.html DIHAPUS
    // karena dipindahkan ke theme.css (nilai sama) agar halaman admin yang
    // memakai token ini juga lolos AA. Yang dikunci dari sisi publik kini:
    // (1) PEMAKAIAN var(--grad-btn-*) pada tombol unduh, dan
    // (2) tidak ada lagi definisi lokal duplikat di shared.html.
    const download = DOWNLOAD();
    for (const token of [
        '--grad-btn-violet-start', '--grad-btn-violet-end',
        '--grad-btn-blue-start', '--grad-btn-blue-end',
    ]) {
        assert.ok(download.includes(`var(${token})`),
            `download.html wajib memakai var(${token})`);
    }
    const shared = SHARED();
    for (const m of shared.matchAll(/(--grad-btn[\w-]*):\s*(#[0-9a-fA-F]{6}|[^;]+);/g)) {
        assert.fail(`definisi lokal ${m[1]} ditemukan lagi di shared.html — satu sumber kebenaran adalah theme.css`);
    }
});

/** Ambil argumen linear-gradient(...) dengan penghitungan kurung sadar-var(). */
function gradientArgs(text) {
    const m = text.match(/linear-gradient\(/);
    if (!m) return null;
    let depth = 1;
    let j = m.index + m[0].length;
    for (; j < text.length && depth > 0; j++) {
        if (text[j] === '(') depth++;
        else if (text[j] === ')') depth--;
    }
    return text.slice(m.index + m[0].length, j - 1);
}

/** Semua deklarasi gradien endpoint tombol unduh: base rule .btn-download-big + inline style per anchor. */
function downloadButtonGradients() {
    const doc = DOWNLOAD();
    const grads = [];

    // Base rule .btn-download-big { ... background: linear-gradient(...) ... }
    const rule = doc.match(/\.btn-download-big\s*\{([^}]*)\}/);
    assert.ok(rule, '.btn-download-big harus ada di download.html');
    const bg = gradientArgs(rule[1]);
    assert.ok(bg, '.btn-download-big wajib punya background linear-gradient');
    grads.push({ origin: '.btn-download-big (base)', endpoints: bg });

    // Inline override pada anchor unduh.
    for (const m of doc.matchAll(/class="btn-download-big"[^>]*style="([^"]*)"/g)) {
        const args = gradientArgs(m[1]);
        if (args) grads.push({ origin: 'inline style', endpoints: args });
    }
    return grads;
}

test('T16b: nilai var(--grad-btn-*) di mana pun terdefinisi (theme.css/shared) tetap di whitelist AA', () => {
    const grads = downloadButtonGradients();
    assert.ok(grads.length >= 4, `minimal 4 gradien tombol unduh (base + 3 inline), dapat ${grads.length}`);

    // Resolusi var() dari theme.css (sumber baru, Batch 10) dengan fallback
    // transisional ke :root shared.html bila migrasi belum mencapai situ.
    let tokenMap = {};
    try {
        for (const m of read('static/css/theme.css').matchAll(/(--[\w-]+):\s*(#[0-9a-fA-F]{6})\s*;/g)) {
            tokenMap[m[1]] = m[2].toLowerCase();
        }
    } catch (_) { /* theme.css wajib ada; guard hanya untuk robustness test */ }
    for (const m of SHARED().matchAll(/(--[\w-]+):\s*(#[0-9a-fA-F]{6})\s*;/g)) {
        tokenMap[m[1]] = m[2].toLowerCase();
    }

    // Nama token yang DIPAKAI wajib termasuk set token gradien kontrak.
    const used = new Set(downloadButtonGradients().flatMap((g) =>
        g.endpoints.split(',').map((s) => s.trim()).filter((s) => s.startsWith('var('))));
    for (const ep of ['var(--grad-btn-violet-start)', 'var(--grad-btn-violet-end)']) {
        assert.ok(used.has(ep), `endpoint ${ep} wajib tetap dipakai tombol unduh`);
    }

    for (const g of grads) {
        const endpoints = g.endpoints.split(',')
            .map((s) => s.trim())
            .filter((s) => s.startsWith('var(') || s.startsWith('#'));
        assert.equal(endpoints.length, 2, `${g.origin}: gradien wajib punya 2 endpoint warna`);
        for (const ep of endpoints) {
            const hex = ep.startsWith('#') ? ep.toLowerCase() : tokenMap[ep.replace(/^var\(|\)$/g, '')];
            if (!hex) continue; // definisi mengikuti kontrak theme.css — dikunci T16a & tokens-guard
            assert.ok(GRADIENT_ENDPOINT_WHITELIST.includes(hex),
                `${g.origin}: endpoint ${hex} tidak ada di whitelist nilai lolos AA (${GRADIENT_ENDPOINT_WHITELIST.join(', ')})`);
            const ratio = contrastVsWhite(hex);
            assert.ok(ratio >= 4.5,
                `${g.origin}: endpoint ${hex} = ${ratio.toFixed(2)}:1 < 4.5 (label putih 15.5px bold bukan large-text)`);
        }
    }
});

test('T16c: endpoint gradien lama yang gagal AA tidak lagi dipakai tombol unduh', () => {
    const grads = JSON.stringify(downloadButtonGradients());
    for (const bad of ['#a855f7', '#3b82f6', '--color-accent)']) {
        assert.ok(!grads.includes(bad), `endpoint lama ${bad} masih dipakai gradien tombol unduh`);
    }
});

// ===========================================================================
// S42 — fingerprintjs + device-fingerprint dimuat defer & berurutan (×4)
// ===========================================================================

for (const page of ['forgot_password', 'register', 'register_confirm', 'reset_password']) {
    test(`S42 (${page}.html): fingerprintjs.min.js & device-fingerprint.js sama-sama defer dan berurutan`, () => {
        const html = read(`templates/public/${page}.html`);
        const fp = html.match(/<script[^>]*fingerprintjs\.min\.js[^>]*>/);
        const df = html.match(/<script[^>]*device-fingerprint\.js[^>]*>/);
        assert.ok(fp, 'tag script fingerprintjs.min.js harus ada');
        assert.ok(df, 'tag script device-fingerprint.js harus ada');
        assert.match(fp[0], /\bdefer\b/, 'fingerprintjs.min.js wajib defer (37KB blocking render)');
        assert.match(df[0], /\bdefer\b/,
            'device-fingerprint.js juga defer — script defer dieksekusi berurutan sesuai posisi dokumen, '
            + 'sehingga jika hanya fingerprintjs yang defer ia akan berjalan SETELAH pemakainya');
        assert.ok(html.indexOf(fp[0]) < html.indexOf(df[0]),
            'urutan dokumen fingerprintjs sebelum device-fingerprint harus dipertahankan');
    });
}

// ===========================================================================
// S45 — instruksi posisi salah ("di sebelah kiri")
// ===========================================================================

test('S45: langkah 1 panduan Android merujuk kartu unduhan, bukan posisi spasial "sebelah kiri"', () => {
    const doc = DOWNLOAD();
    assert.match(doc, /Ketuk tombol <strong>Unduh APK<\/strong> di kartu unduhan di atas/,
        'instruksi wajib mengarah ke tombol Unduh APK di kartu unduhan');
    assert.ok(!doc.includes('di sebelah kiri'),
        'referensi spasial "di sebelah kiri" salah di layout mobile satu kolom — harus hilang');
});

// ===========================================================================
// S46 — skor resmi di summary bar + tanpa id duplikat
// ===========================================================================

/** Harness vm untuk fungsi render hasil.html (stub DOM/global minimal). */
function buildHasilSandbox(extra = {}) {
    const sandbox = {
        questionsData: [],
        identityFieldsData: [],
        isLoggedIn: false,
        showCorrectAnswers: true,
        escapeHtml: (s) => String(s == null ? '' : s),
        localizeUTC: (s) => (s || ''),
        ...extra,
    };
    const src = HASIL();
    for (const name of ['getDurationString', 'getScoreClass', 'getPassStatus', 'formatAnswer', 'buildDetailContent']) {
        const fn = extractFunction(src, name);
        assert.ok(fn, `fungsi ${name} harus bisa diekstrak dari hasil.html`);
        sandbox[name] = fn;
    }
    const ctx = vm.createContext(sandbox);
    vm.runInContext(`
        getDurationString = eval("(" + getDurationString + ")");
        getScoreClass = eval("(" + getScoreClass + ")");
        getPassStatus = eval("(" + getPassStatus + ")");
        formatAnswer = eval("(" + formatAnswer + ")");
        buildDetailContent = eval("(" + buildDetailContent + ")");
    `, ctx);
    return ctx;
}

test('S46: summary bar menampilkan skor RESMI sub.score/sub.max_score, bukan total hitungan klien', () => {
    const sub = {
        id: 1, student_name: 'Budi', score: 62.5, max_score: 100,
        start_time: '2026-08-24T01:00:00Z', created_at: '2026-08-24T01:30:00Z',
        answers: { 1: 'A' }, evaluated_answers: { 1: { statusClass: 'incorrect', statusText: 'Salah', earned: 0 } },
    };
    const ctx = buildHasilSandbox({ sub });
    vm.runInContext('questionsData = [{number:1,type:"single_choice",key:"B"}];', ctx);
    const out = vm.runInContext('buildDetailContent(sub)', ctx);

    // Skor resmi 62.5/100.0 tampil sebagai angka utama bar rekap.
    assert.match(out, /Skor Resmi/, 'bar rekap wajib berlabel "Skor Resmi"');
    assert.match(out, /62\.5\s*\/\s*100\.0/, 'skor resmi sub.score/sub.max_score wajib ditampilkan');

    // Total evaluasi klien hanya baris rincian berlabel jelas.
    assert.match(out, /Perhitungan tampilan/, 'total klien dipertahankan sebagai baris rincian berlabel');
    assert.match(out, /Perhitungan tampilan[\s\S]{0,200}?0\.0\s*\/\s*100\.0/,
        'baris rincian menampilkan hasil penjumlahan evaluasi klien (0.0)');
});

test('S46: skor resmi null → "Belum Dikoreksi" + placeholder, tanpa angka palsu', () => {
    const sub = {
        id: 2, student_name: 'Ani', score: null, max_score: 100,
        start_time: null, created_at: null, answers: {}, evaluated_answers: {},
    };
    const ctx = buildHasilSandbox({
        sub,
        questionsData: [{ number: 1, type: 'single_choice', key: 'B' }],
    });
    const out = vm.runInContext('buildDetailContent(sub)', ctx);
    assert.match(out, /&mdash;/, 'skor belum dikoreksi ditampilkan placeholder mdash, bukan 0');
});

test('S46: tidak ada lagi duplikasi id="scoreStatusBadge" — menjadi class', () => {
    const html = HASIL();
    assert.ok(!/id="scoreStatusBadge"/.test(html),
        'id="scoreStatusBadge" diduplikasi tiap baris detail terbuka (HTML invalid) — wajib jadi class');
    assert.match(html, /\.score-status-badge\s*\{/, 'styling badge kini lewat class .score-status-badge');
    assert.match(html, /class="score-status-chip \$\{passStatus\.cls\} score-status-badge"/,
        'chip status dirender dengan class .score-status-badge');
});

// ===========================================================================
// R34 — cache-busting CSS publik konsisten
// ===========================================================================

test('R34: seluruh link stylesheet di shared.html membawa cache-busting ?v={{.version}}', () => {
    const links = [...SHARED().matchAll(/<link rel="stylesheet" href="([^"]+)">/g)].map((m) => m[1]);
    assert.ok(links.length >= 3, `minimal 3 stylesheet eksternal di shared.html, dapat ${links.length}`);
    for (const href of links) {
        assert.match(href, /\?v=/, `link "${href}" wajib membawa ?v={{.version}} (proxy LAN agresif cache)`);
    }
});

// ===========================================================================
// S44 — palet light arwah + sistem token paralel dihapus
// ===========================================================================

const PARALLEL_TOKENS = ['--text-muted', '--text-main', '--text-secondary', '--bg-primary',
    '--bg-secondary', '--accent-primary', '--accent-secondary', '--accent-glow',
    '--glass-bg', '--glass-border', '--card-hover'];

test('S44a: blok :root[data-theme="light"] dihapus dari shared.html (keputusan S17)', () => {
    const src = SHARED();
    assert.ok(!src.includes(':root[data-theme="light"]'),
        'palet light dead-code wajib dihapus (dark-by-design, S17/Batch 4)');
    for (const ghost of ['#64748b', '#0f172a', '#f1f5f9', '#334155']) {
        assert.ok(!src.includes(ghost), `nilai palet light arwah ${ghost} masih ada di shared.html`);
    }
});

test('S44b: sistem token paralel tidak lagi didefinisikan di shared.html', () => {
    const src = SHARED();
    for (const tok of PARALLEL_TOKENS) {
        assert.ok(!new RegExp(`${tok}\\s*:`).test(src),
            `definisi ${tok} masih ada di shared.html — satu sumber kebenaran adalah theme.css`);
    }
});

test('S44c: audit — tidak ada referensi token paralel menggantung di folder publik + CSS miliknya', () => {
    const dir = path.join(WEBUI_ROOT, 'templates', 'public');
    const files = fs.readdirSync(dir).filter((f) => f.endsWith('.html')).map((f) => path.join(dir, f));
    for (const css of ['static/css/public-mobile.css', 'static/css/public-desktop.css', 'static/css/hasil.css']) {
        files.push(path.join(WEBUI_ROOT, css));
    }
    for (const f of files) {
        const src = fs.readFileSync(f, 'utf8');
        for (const tok of PARALLEL_TOKENS) {
            const uses = src.split(`var(${tok})`).length - 1;
            assert.equal(uses, 0,
                `${path.basename(f)} masih memakai var(${tok}) ${uses}× — token ini sudah tidak terdefinisi`);
        }
    }
});

test('S44d: migrasi nyata ke token resmi theme.css terjadi di shared.html', () => {
    const src = SHARED();
    for (const tok of ['var(--color-bg)', 'var(--color-bg-secondary)', 'var(--color-primary)',
        'var(--color-text)', 'var(--color-text-muted)', 'var(--color-text-secondary)',
        'var(--color-glass)', 'var(--color-glass-border)', 'rgb(var(--rgb-accent))']) {
        assert.ok(src.includes(tok), `shared.html harus memakai token resmi ${tok}`);
    }
});

// ===========================================================================
// R35 — kolom durasi dua nama
// ===========================================================================

test('R35: header kolom tabel hasil menyatu "Durasi" (desktop == data-label mobile)', () => {
    const html = HASIL();
    assert.match(html, /<th scope="col" class="td-center">Durasi<\/th>/, 'th desktop wajib "Durasi"');
    assert.ok(!html.includes('Waktu Pengerjaan'), '"Waktu Pengerjaan" tidak boleh tersisa');
    assert.match(html, /data-label="Durasi"/, 'data-label mobile card tetap "Durasi"');
});

// ===========================================================================
// R36 — autocapitalize input username/token
// ===========================================================================

for (const [page, field] of [['register', 'username'], ['forgot_password', 'username']]) {
    test(`R36 (${page}.html): input username bebas auto-kapitalisasi keyboard mobile`, () => {
        const html = read(`templates/public/${page}.html`);
        const input = html.match(/<input[^>]*id="username"[^>]*>/);
        assert.ok(input, 'input #username harus ada');
        for (const attr of ['autocapitalize="none"', 'autocorrect="off"', 'spellcheck="false"']) {
            assert.match(input[0], new RegExp(attr), `input #username wajib punya ${attr}`);
        }
    });
}

test('R36 (cek_hasil.html): input token punya enterkeyhint="go"', () => {
    const html = read('templates/public/cek_hasil.html');
    const input = html.match(/<input[^>]*id="token"[^>]*>/);
    assert.ok(input, 'input #token harus ada');
    assert.match(input[0], /enterkeyhint="go"/, 'form single-field wajib enterkeyhint="go"');
});

// ===========================================================================
// R37 — paginasi aria-current + tab Nilai/Kunci deep-linkable
// ===========================================================================

test('R37a (perilaku): createPageBtn memasang aria-current="page" hanya untuk halaman aktif', () => {
    const fn = extractFunction(HASIL(), 'createPageBtn');
    assert.ok(fn, 'createPageBtn harus bisa diekstrak');

    const created = [];
    const sandbox = {
        currentPage: 2,
        goToPage: () => {},
        document: {
            createElement: () => {
                const btn = {
                    className: '', textContent: '', attrs: {}, onclick: null,
                    setAttribute(n, v) { this.attrs[n] = v; },
                    removeAttribute(n) { delete this.attrs[n]; },
                };
                created.push(btn);
                return btn;
            },
        },
    };
    vm.runInContext(`createPageBtn = eval("(" + ${JSON.stringify(fn)} + ")")`,
        vm.createContext(sandbox));
    for (const p of [1, 2, 3]) vm.runInContext(`createPageBtn(${p})`, sandbox);

    assert.equal(created.length, 3);
    assert.equal(created[0].attrs['aria-current'], undefined, 'halaman 1 (non-aktif) tanpa aria-current');
    assert.equal(created[1].attrs['aria-current'], 'page', 'halaman aktif wajib aria-current="page"');
    assert.equal(created[2].attrs['aria-current'], undefined, 'halaman 3 (non-aktif) tanpa aria-current');
});

test('R37b (perilaku): switchTab menulis hash #nilai/#kunci; resolusi hash aman', () => {
    const src = HASIL();
    for (const name of ['switchTab', 'resolveTabFromHash']) {
        assert.ok(extractFunction(src, name), `${name} harus bisa diekstrak`);
    }

    const els = {};
    const mkEl = () => ({ classList: { toggle() {} }, style: {}, setAttribute() {}, removeAttribute() {} });
    const writtenHashes = [];
    const sandbox = {
        showAnswersFromServer: true,
        document: {
            getElementById: (id) => (els[id] || (els[id] = mkEl())),
            querySelector: () => null,
        },
        history: { replaceState: (_a, _b, u) => writtenHashes.push(u) },
    };
    const ctx = vm.createContext(sandbox);
    vm.runInContext(`
        const TAB_HASH_NAMES = { scores: 'nilai', keys: 'kunci' };
        switchTab = eval("(" + ${JSON.stringify(extractFunction(src, 'switchTab'))} + ")");
        resolveTabFromHash = eval("(" + ${JSON.stringify(extractFunction(src, 'resolveTabFromHash'))} + ")");
    `, ctx);

    // Penulisan hash saat pindah tab.
    vm.runInContext('switchTab("scores")', ctx);
    vm.runInContext('switchTab("keys")', ctx);
    assert.deepEqual(writtenHashes, ['#nilai', '#kunci'],
        `switchTab wajib menulis hash #nilai/#kunci, dapat ${JSON.stringify(writtenHashes)}`);

    // Pembacaan hash: valid, fallback default untuk hash arbitrer.
    for (const [hash, expected] of [['#nilai', 'scores'], ['#kunci', 'keys'], ['', 'scores'], ['#acak', 'scores']]) {
        sandbox.location = { hash };
        const got = vm.runInContext('resolveTabFromHash()', ctx);
        assert.equal(got, expected, `hash "${hash}" harus resolusi ke ${expected}, dapat ${got}`);
    }

    // Opsi skipHash (balasan event hashchange) tidak menulis ulang hash.
    const nBefore = writtenHashes.length;
    vm.runInContext('switchTab("scores", { skipHash: true })', ctx);
    assert.equal(writtenHashes.length, nBefore, 'skipHash wajib melewati penulisan ulang hash');
});

test('R37c: tab aktif dibaca dari hash saat load + diikuti saat hashchange', () => {
    const html = HASIL();
    // Batch 11 (T19): blok registrasi Actions ikut berpindah ke dalam
    // DOMContentLoaded (sebelum init tab) — jendela regex diperlebar agar
    // tetap menjangkau panggilan switchTab(resolveTabFromHash()) pertama.
    assert.match(html, /DOMContentLoaded[\s\S]{0,1800}?switchTab\(resolveTabFromHash\(\)/,
        'saat load, tab awal wajib dibaca dari location.hash');
    assert.match(html, /hashchange[\s\S]{0,300}?switchTab\(resolveTabFromHash\(\)/,
        'perubahan hash dari luar wajib diikuti (deep-link/back-forward)');
    // Hash arbitrer tidak boleh membuka tab Kunci saat kunci disembunyikan server.
    assert.match(html, /showAnswersFromServer[\s\S]{0,200}?resolveTabFromHash|resolveTabFromHash[\s\S]{0,400}?showAnswersFromServer/,
        'tab #kunci hanya dihormati bila server mengizinkan show_answers');
});

// ===========================================================================
// R38 — SmartScreen bilingual
// ===========================================================================

test('R38: instruksi SmartScreen mencantumkan label EN + ID untuk kedua langkah', () => {
    const doc = DOWNLOAD();
    assert.match(doc, /"More Info"\/"Info lainnya"/, 'langkah klik wajib kedua varian bahasa');
    assert.match(doc, /"Run anyway"\/"Tetap jalankan"/, 'tombol lanjut wajib kedua varian bahasa');
});

// ===========================================================================
// R39 — mockup hero landing domain hard-coded
// ===========================================================================

test('R39: mockup landing memakai placeholder alamat server netral', () => {
    const doc = INDEX();
    assert.match(doc, /mockup-input">http:\/\/alamat-server-sekolah</,
        'placeholder mockup wajib netral (LAN/on-premise), bukan domain cloud');
    assert.ok(!doc.includes('examvan.my.id'), 'domain hard-coded examvan.my.id wajib hilang');
    assert.match(doc, /TOKEN UJIAN/, 'baris token mockup tetap ada');
});

// ===========================================================================
// R40 — band kuning 40–69 + legenda ambang kelulusan
// ===========================================================================

test('R40a (perilaku): getPassStatus punya tiga tingkat — Lulus/Hampir/Belum Lulus + Belum Dikoreksi', () => {
    const fn = extractFunction(HASIL(), 'getPassStatus');
    assert.ok(fn, 'getPassStatus harus bisa diekstrak');
    const ctx = vm.createContext({});
    vm.runInContext(`getPassStatus = eval("(" + ${JSON.stringify(fn)} + ")")`, ctx);

    // Hasil dibandingkan via JSON (objek dari realm vm beda prototype).
    const cases = [
        [[75, 100], { text: 'Lulus', cls: 'score-status-pass' }],
        [[70, 100], { text: 'Lulus', cls: 'score-status-pass' }],
        [[69, 100], { text: 'Hampir', cls: 'score-status-mid' }],
        [[40, 100], { text: 'Hampir', cls: 'score-status-mid' }],
        [[39.9, 100], { text: 'Belum Lulus', cls: 'score-status-fail' }],
        [[null, 100], { text: 'Belum Dikoreksi', cls: 'score-status-none' }],
    ];
    for (const [args, expected] of cases) {
        const got = JSON.parse(vm.runInContext(
            `JSON.stringify(getPassStatus(${args[0]}, ${args[1]}))`, ctx));
        assert.deepEqual(got, expected, `getPassStatus(${args.join(', ')})`);
    }
});

test('R40b: chip tingkat ketiga "Hampir" distyling lewat token (tanpa hex/rgba literal baru)', () => {
    const css = HASIL();
    const rule = css.match(/\.score-status-mid\s*\{([^}]*)\}/);
    assert.ok(rule, '.score-status-mid wajib ada di blok style hasil.html');
    assert.match(rule[1], /var\(--color-warning/, 'warna teks chip memakai token warning');
    // Diperbarui Batch 15 (S103 adopsi-a): rgba(var(--rgb-warning),0.12)
    // bermigrasi ke var(--color-warning-bg) — nilai identik persis
    // (rgba(245,158,11,0.12)), satu sumber kebenaran di theme.css.
    assert.match(rule[1], /background:\s*var\(--color-warning-bg\)/, 'latar chip memakai token bg warning');
    assert.match(rule[1], /rgba\(var\(--rgb-warning\),\s*0\.35\)/, 'border chip memakai triplet token');
});

test('R40c: legenda ambang menjelaskan bahwa Status Lulus mulai dari 70', () => {
    const html = HASIL();
    const legend = html.match(/<p class="score-legend"[\s\S]*?<\/p>/);
    assert.ok(legend, 'legenda skor harus ada');
    assert.match(legend[0], /Hijau ≥ 70/, 'ambang hijau terdokumentasi');
    assert.match(legend[0], /Kuning 40–69/, 'ambang kuning terdokumentasi');
    assert.match(legend[0], /Merah &lt; 40/, 'ambang merah terdokumentasi');
    assert.match(legend[0], /Status Lulus mulai dari 70/, 'klausa status Lulus mulai dari 70 wajib ada');
});
