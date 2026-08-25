/* Batch 13 — UI/UX halaman publik (review_uiux_webui.md §5.10 RE-REVIEW
 * RONDE 7, sisi publik). ID temuan: S74, S75, R79, R80, R86, R82-sisi-publik,
 * + kontrak lintas-agen token indigo-400.
 *
 * Run with:  node --test static/js/uiux-batch13-publik.test.mjs   (from webui/)
 *
 * Metode mengikuti uiux-batch12-publik.test.mjs: kontrak statik fs-read +
 * eksekusi perilaku via vm.runInNewContext dengan stub DOM minimal.
 *
 * Dampak bisnis yang dilindungi:
 *   KONTRAK — token --color-primary-bright: #818cf8 di theme.css :root sebagai
 *         satu sumber; seluruh pemakaian #818cf8 publik bermigrasi ke token.
 *   S74 — pencarian tanpa hasil tidak lagi meninggalkan paginasi & statistik
 *         basi bertentangan dengan kartu "Tidak ditemukan" (pola konsisten
 *         renderScoresTable/renderStats yang menyembunyikan).
 *   S75 — BAR strength meter memakai token yang sama dengan labelnya
 *         (good→--color-success, strong→gradient success/accent-cyan);
 *         blok style strength meter kedua halaman bebas hex.
 *   R79 — FOLDER-WIDE: setiap template publik menaruh SEMUA link stylesheet
 *         eksternal SEBELUM blok <style> inline pertama (urutan cascade jelas).
 *   R80 — tidak ada lompatan heading h1→h3 di hasil.html; panel Kunci Jawaban
 *         Resmi naik ke h2.
 *   R86 — feedback kirim-ulang OTP adalah live region (role="status",
 *         aria-live="polite").
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
const REGISTER = () => read('templates/public/register.html');
const RESET_PASSWORD = () => read('templates/public/reset_password.html');
const FORGOT = () => read('templates/public/forgot_password.html');
const REGISTER_CONFIRM = () => read('templates/public/register_confirm.html');
const CEK_HASIL = () => read('templates/public/cek_hasil.html');
const INDEX = () => read('templates/public/index.html');
const SHARED = () => read('templates/public/shared.html');
const THEME = () => read('static/css/theme.css');

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

/** Ganti ekspresi template Go pada state halaman hasil dengan literal JS.
 *  `searchQuery` dijadikan `var` agar test bisa mensimulasikan pencarian
 *  aktif dari luar konteks vm (binding let skrip tidak terlihat host). */
function stripGoTemplates(src) {
    return src
        .replace(/\{\{\.token\}\}/g, 'TOKEN01')
        .replace(/\{\{if \.is_logged_in\}\}true\{\{else\}\}false\{\{end\}\}/g, 'false')
        .replace(/\{\{if \.is_disabled\}\}true\{\{else\}\}false\{\{end\}\}/g, 'false')
        .replace(/\{\{if \.show_answers\}\}true\{\{else\}\}false\{\{end\}\}/g, 'true')
        .replace(/\{\{if \.error\}\}true\{\{else\}\}false\{\{end\}\}/g, 'false')
        .replace(/let searchQuery = '';/, "var searchQuery = '';");
}

// ===========================================================================
// KONTRAK lintas-agen — token --color-primary-bright (#818cf8) di theme.css
// ===========================================================================

test('KONTRAK: theme.css :root mendefinisikan --color-primary-bright: #818cf8 dekat --color-primary-light', () => {
    const css = THEME();
    const m = css.match(/--color-primary-bright:\s*#818cf8\s*;/);
    assert.ok(m, '--color-primary-bright: #818cf8 wajib didefinisikan di :root theme.css '
        + '(satu sumber untuk teks/chip indigo terang di dark surface; agen lain '
        + 'mensubstitusi via var())');
    const idxBright = css.indexOf('--color-primary-bright:');
    const idxLight = css.indexOf('--color-primary-light:');
    assert.ok(idxLight !== -1 && Math.abs(idxBright - idxLight) < 400,
        'token harus berdekatan dengan --color-primary-light (grup semantik primary)');
});

test('KONTRAK: definisi token membawa komentar penjelas (indigo terang dark surface, kontras ≥6.1:1)', () => {
    const css = THEME();
    assert.match(css, /--color-primary-bright:\s*#818cf8\s*;/,
        'definisi token wajib ada sebelum komentar bisa dinilai');
    const idx = css.indexOf('--color-primary-bright:');
    assert.ok(idx !== -1);
    const before = css.slice(Math.max(0, idx - 600), idx);
    assert.match(before, /indigo/i, 'komentar harus menyebut intensi indigo terang');
    assert.match(before, /6\.1|kontras/i, 'komentar harus menyebut kontras ≥6.1:1 di dark surface');
});

// ===========================================================================
// S68 lanjutan sisi publik — migrasi #818cf8 → var(--color-primary-bright)
// ===========================================================================

test('S68 lanjutan: seluruh templates/public bebas #818cf8 literal — pakai var(--color-primary-bright)', () => {
    let uses = 0;
    for (const [name, tpl] of PUBLIC_TEMPLATES) {
        const hits = (tpl().match(/#818cf8/gi) || []).length;
        assert.equal(hits, 0,
            `${name}: ${hits} pemakaian #818cf8 literal tersisa — substitusi var(--color-primary-bright)`);
        uses += (tpl().match(/var\(--color-primary-bright\)/g) || []).length;
    }
    assert.ok(uses >= 8, `minimal 8 pemakaian var(--color-primary-bright) hasil migrasi `
        + `(download ×3, shared ×2, register_confirm ×2, register ×1), dapat ${uses}`);
});

// ===========================================================================
// S74 — pencarian 0 hasil meninggalkan paginasi & statistik basi tampil
// ===========================================================================

/** Sandbox DOM minimal untuk mengeksekusi script inline hasil.html. */
function makeHasilSandbox({ apiFetch }) {
    const els = new Map();
    const mkEl = (id) => ({
        id,
        style: {},
        dataset: {},
        tabIndex: 0,
        disabled: false,
        innerHTML: '',
        textContent: '',
        classList: {
            _s: new Set(),
            toggle(c) { this._s.has(c) ? this._s.delete(c) : this._s.add(c); },
            add(c) { this._s.add(c); },
            remove(c) { this._s.delete(c); },
            contains(c) { return this._s.has(c); },
        },
        setAttribute() {}, getAttribute: () => null, removeAttribute() {},
        appendChild() {}, addEventListener() {}, focus() {},
        querySelector: () => null, querySelectorAll: () => [], remove() {},
        scrollIntoView() {},
    });
    const byId = (id) => {
        if (!els.has(id)) els.set(id, mkEl(id));
        return els.get(id);
    };
    const domListeners = {};
    const sandbox = {
        console: { error() {}, warn() {}, log() {} },
        location: { hash: '', reload() {} },
        history: { replaceState() {} },
        window: { addEventListener() {} },
        document: {
            getElementById: byId,
            activeElement: null,
            querySelector: () => mkEl('q'),
            querySelectorAll: () => [],
            createElement: (tag) => mkEl(tag),
            addEventListener(type, fn) { (domListeners[type] || (domListeners[type] = [])).push(fn); },
        },
        filterResults() {},
        switchTab() {},
        goToPage() {},
        showApiErrorToast() {},
        escapeHtml: (s) => String(s == null ? '' : s),
        URLSearchParams,
        apiFetch,
    };
    const ctx = vm.createContext(sandbox);
    vm.runInContext(stripGoTemplates(lastInlineScript(HASIL())), ctx);
    return { sandbox, els, byId, domListeners };
}

const OK_PAGE = {
    ok: true,
    json: () => Promise.resolve({
        success: true,
        submissions: [
            { id: 1, student_name: 'Budi', score: 80, max_score: 100, start_time: null, created_at: null, answers: {} },
            { id: 2, student_name: 'Ani', score: 90, max_score: 100, start_time: null, created_at: null, answers: {} },
        ],
        questions: [], identity_fields: [],
        stats: { count: 57, average: 75.5, max: 90, min: 40 },
        pagination: { page: 1, total: 57, total_pages: 3 },
    }),
};
const OK_EMPTY = {
    ok: true,
    json: () => Promise.resolve({
        success: true,
        submissions: [], questions: [], identity_fields: [],
        stats: { count: 0, average: 0, max: 0, min: 0 },
        pagination: { page: 1, total: 0, total_pages: 1 },
    }),
};

test('S74 (perilaku): hasil.html — pencarian tanpa hasil MENYEMBUNYIKAN paginasi & statistik basi', async () => {
    let mode = 'page';
    const sb = makeHasilSandbox({
        apiFetch: () => Promise.resolve(mode === 'empty' ? OK_EMPTY : OK_PAGE),
    });

    await sb.sandbox.loadResults(); // muat sukses dengan data → paginasi & stats tampil
    assert.equal(sb.byId('paginationWrapper').style.display, 'flex',
        'pra-kondisi: paginasi tampil pada muat berdata');
    assert.equal(sb.byId('statsRow').style.display, 'grid',
        'pra-kondisi: statistik tampil pada muat berdata');

    // Simulasikan pencarian berikutnya dengan 0 hasil (searchQuery aktif).
    sb.sandbox.searchQuery = 'zzz-tak-ada';
    mode = 'empty';
    await sb.sandbox.loadResults();

    assert.equal(sb.byId('noSearchResults').style.display, 'block',
        'kartu "Tidak ditemukan" tampil (perilaku lama)');
    assert.equal(sb.byId('paginationWrapper').style.display, 'none',
        'S74: paginasi "1–20 dari 57" WAJIB disembunyikan — konsisten pola '
        + 'renderScoresTable yang menyembunyikan paginasi saat kosong');
    assert.equal(sb.byId('statsRow').style.display, 'none',
        'S74: statistik basi (avg/max/min dari hasil sebelumnya) wajib ikut '
        + 'disembunyikan agar tidak bertentangan dengan kartu "Tidak ditemukan"');
});

test('S74 (perilaku): hasil.html — muat berdata BERIKUTNYA memulihkan paginasi & statistik', async () => {
    let mode = 'empty';
    const sb = makeHasilSandbox({
        apiFetch: () => Promise.resolve(mode === 'empty' ? OK_EMPTY : OK_PAGE),
    });
    sb.sandbox.searchQuery = 'zzz-tak-ada';
    await sb.sandbox.loadResults(); // 0 hasil → semuanya disembunyikan
    assert.equal(sb.byId('paginationWrapper').style.display, 'none');

    sb.sandbox.searchQuery = '';
    mode = 'page';
    await sb.sandbox.loadResults();
    assert.equal(sb.byId('paginationWrapper').style.display, 'flex',
        'paginasi pulih saat pencarian kini berhasil');
    assert.equal(sb.byId('statsRow').style.display, 'grid',
        'statistik pulih saat data kini ada');
});

test('S74 (statik): cabang total===0 loadResults menyentuh paginationWrapper & statsRow', () => {
    const script = stripGoTemplates(lastInlineScript(HASIL()));
    const branch = script.match(/if\s*\(pagination\.total\s*===\s*0\)\s*\{[\s\S]*?\n\s*\}/);
    assert.ok(branch, 'cabang total===0 di loadResults harus ada');
    assert.match(branch[0], /getElementById\('paginationWrapper'\)[\s\S]*?style\.display\s*=\s*'none'/,
        'cabang total===0 wajib menyembunyikan paginationWrapper');
    assert.match(branch[0], /getElementById\('statsRow'\)[\s\S]*?style\.display\s*=\s*'none'/,
        'cabang total===0 wajib menyembunyikan statsRow');
});

// ===========================================================================
// S75 — BAR strength meter masih hex (bar ≠ warna label)
// ===========================================================================

for (const [label, tpl] of [['register', REGISTER], ['reset_password', RESET_PASSWORD]]) {
    test(`S75: blok style strength meter ${label} bebas hex — bar good/strong pakai token sama dengan label`, () => {
        const html = tpl();
        const m = html.match(/\.pw-strength-bar\s*\{[\s\S]*?\.pw-strength-text\s*\{/);
        assert.ok(m, `blok CSS .pw-strength-bar harus ada di ${label}`);
        const block = m[0];
        assert.doesNotMatch(block, /#[0-9a-fA-F]{3,8}\b/,
            `${label}: blok style strength meter masih memuat hex literal — `
            + 'bar wajib token theme.css yang sama dengan label');
        const good = block.match(/\.pw-bar-fill\.good\s*\{([^}]*)\}/);
        const strong = block.match(/\.pw-bar-fill\.strong\s*\{([^}]*)\}/);
        assert.ok(good && strong, `${label}: rule pw-bar-fill good/strong harus ada`);
        assert.match(good[1], /background:\s*var\(--color-success\)/,
            `${label}: .good (#22c55e) wajib var(--color-success)`);
        assert.match(strong[1], /linear-gradient\(90deg,\s*var\(--color-success\),\s*var\(--color-accent-cyan\)\)/,
            `${label}: .strong wajib gradient token sama dengan label `
            + '(var(--color-success), var(--color-accent-cyan))');
    });
}

// ===========================================================================
// R79 — FOLDER-WIDE: link stylesheet sebelum blok <style> inline pertama
// ===========================================================================

for (const [name, tpl] of PUBLIC_TEMPLATES) {
    test(`R79 (folder-wide): ${name} — indeks link stylesheet pertama < indeks <style> pertama`, () => {
        const html = tpl();
        const firstStyle = html.search(/<style[\s>]/);
        if (firstStyle === -1) {
            assert.ok(true, `${name} tak punya blok <style> — tidak berlaku`);
            return;
        }
        // Diperbarui Batch 15 (R108): download.html tidak lagi me-link
        // public-mobile/desktop.css sendiri — head-nya sepenuhnya berasal dari
        // partial public_head (shared.html), jadi kontrak urutan link-vs-style
        // diverifikasi di partial tersebut, bukan di halaman.
        if (/{{\s*template\s+"public_head"/.test(html)) {
            const sh = SHARED();
            const shLink = sh.search(/<link[^>]*rel="stylesheet"/);
            const shStyle = sh.search(/<style[\s>]/);
            assert.ok(shLink !== -1 && shLink < shStyle,
                'partial public_head (shared.html) harus me-link stylesheet sebelum <style> pertama');
            return;
        }
        const firstLink = html.search(/<link[^>]*rel="stylesheet"/);
        assert.ok(firstLink !== -1, `${name} punya blok <style> tapi tak punya link stylesheet`);
        assert.ok(firstLink < firstStyle,
            `${name}: link stylesheet pertama (${firstLink}) HARUS berada sebelum `
            + `blok <style> inline pertama (${firstStyle}) — pindahkan semua link `
            + 'eksternal ke atas (urutan antar-link tetap)');
        // Tidak boleh ada link stylesheet tersisa SETELAH <style> pertama.
        const offenders = [...html.matchAll(/<link[^>]*rel="stylesheet"[^>]*>/g)]
            .filter((mm) => mm.index > firstStyle);
        assert.deepEqual(offenders.map((mm) => mm[0]), [],
            `${name}: masih ada link stylesheet SETELAH <style> inline pertama`);
    });
}

// ===========================================================================
// R80 — urutan heading hasil.html (tidak ada lompatan h1→h3)
// ===========================================================================

test('R80: hasil.html — panel Kunci Jawaban Resmi adalah h2 (bukan h3)', () => {
    const html = HASIL();
    const m = html.match(/<h[23][^>]*>\s*(?:<svg[\s\S]*?<\/svg>)?\s*Kunci Jawaban Resmi/s);
    assert.ok(m, 'heading "Kunci Jawaban Resmi" harus ada di hasil.html');
    assert.match(m[0], /^<h2/, '"Kunci Jawaban Resmi" wajib h2 — dulu h3 melompat dari h1 examTitle');
});

test('R80: hasil.html — urutan heading tanpa lompatan level (tak ada h3 sebelum h2 mana pun)', () => {
    const html = HASIL();
    const levels = [...html.matchAll(/<h([1-6])[\s>]/g)].map((mm) => Number(mm[1]));
    assert.ok(levels.includes(1), 'hasil.html harus punya h1');
    let seenH2 = false;
    for (const lvl of levels) {
        if (lvl <= 2) seenH2 = true;
        assert.ok(lvl === 1 || lvl === 2 || seenH2,
            `lompatan heading terdeteksi: h${lvl} muncul sebelum heading h2 mana pun `
            + `(urutan aktual: h${levels.join(',h')})`);
    }
});

// ===========================================================================
// R86 — resend OTP tanpa live region
// ===========================================================================

test('R86: register_confirm.html — #resendMsg adalah live region (role="status" + aria-live="polite")', () => {
    const html = REGISTER_CONFIRM();
    const m = html.match(/<span[^>]*id="resendMsg"[^>]*>/);
    assert.ok(m, '#resendMsg harus ada di register_confirm.html');
    assert.match(m[0], /\brole="status"/,
        'R86: #resendMsg wajib role="status" — feedback kirim-ulang OTP ditulis '
        + 'dinamis via textContent sehingga butuh pengumuman screen reader');
    assert.match(m[0], /\baria-live="polite"/,
        'R86: #resendMsg wajib aria-live="polite"');
});
