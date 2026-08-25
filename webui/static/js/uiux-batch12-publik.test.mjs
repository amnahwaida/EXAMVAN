/* Batch 12 — UI/UX halaman publik (review_uiux_webui.md §5.8/5.9 RE-REVIEW
 * RONDE 5, sisi publik). ID temuan: S65, R67, R68, R69, R71, S68 (sisi publik),
 * + kontrak lintas-agen token merah-400.
 *
 * Run with:  node --test static/js/uiux-batch12-publik.test.mjs   (from webui/)
 *
 * Metode mengikuti uiux-batch11-publik.test.mjs: kontrak statik fs-read +
 * eksekusi perilaku via vm.runInNewContext dengan stub DOM minimal.
 *
 * Dampak bisnis yang dilindungi:
 *   KONTRAK — token --color-danger-bright: #f87171 di theme.css :root sebagai
 *         satu sumber; seluruh pemakaian #f87171 publik bermigrasi ke token.
 *   S65 — kartu error "Coba Lagi" TAMPIL pada kegagalan fetch berikutnya:
 *         loadingIndicator disembunyikan permanen setelah muat sukses pertama;
 *         ketiga jalur gagal wajib memulihkan display sebelum menulis
 *         innerHTML, jika tidak state error tak terlihat (test vm: sukses
 *         lalu gagal → container visible + tombol reload-page ada).
 *   R67 — clear-search mengembalikan fokus keyboard ke input pencarian
 *         (dulu: fokus jatuh ke body karena tombolnya di-display:none).
 *   R68 — strength meter good/strong memakai token (--color-success,
 *         --color-accent-cyan); blok levels bebas hex.
 *   R69 — copywriting publik: klaim "(Stable)" yang tak dikendalikan data
 *         server dihapus dari download.html; hero landing selaras value-prop
 *         offline ("Ujian Digital Teraman & Siap Offline").
 *   R71 — semua link stylesheet eksternal berada SEBELUM blok <style> inline
 *         pertama di hasil/download/register/cek_hasil (urutan cascade jelas;
 *         tidak ada link menyelip setelah style lokal).
 *   S68 — templates/public bebas #f87171 literal (→ var(--color-danger-bright))
 *         dan hasil.css bebas rgba(99,102,241,α) literal (→ rgba(var(--rgb-info), α)).
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
const THEME = () => read('static/css/theme.css');
const HASIL_CSS = () => read('static/css/hasil.css');

const PUBLIC_TEMPLATES = [
    ['hasil.html', HASIL], ['download.html', DOWNLOAD], ['shared.html', () => read('templates/public/shared.html')],
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
// KONTRAK lintas-agen — token --color-danger-bright di theme.css
// ===========================================================================

test('KONTRAK: theme.css :root mendefinisikan --color-danger-bright: #f87171 dekat --color-danger-light', () => {
    const css = THEME();
    const m = css.match(/--color-danger-bright:\s*#f87171\s*;/);
    assert.ok(m, '--color-danger-bright: #f87171 wajib didefinisikan di :root theme.css '
        + '(satu sumber untuk teks status merah terang; agen lain mensubstitusi via var())');
    const idxBright = css.indexOf('--color-danger-bright:');
    const idxLight = css.indexOf('--color-danger-light:');
    assert.ok(idxLight !== -1 && Math.abs(idxBright - idxLight) < 400,
        'token harus berdekatan dengan --color-danger-light (grup semantik danger)');
});

test('KONTRAK: definisi token membawa komentar penjelas (#ef4444 lebih terang, beda dari #fca5a5)', () => {
    const css = THEME();
    const idx = css.indexOf('--color-danger-bright:');
    const before = css.slice(Math.max(0, idx - 600), idx);
    assert.match(before, /#ef4444/, 'komentar harus menjelaskan hubungan dengan --color-danger #ef4444');
    assert.match(before, /#fca5a5|--color-danger-light/, 'komentar harus menjelaskan beda dari --color-danger-light #fca5a5');
});

// ===========================================================================
// S65 — kartu error "Coba Lagi" tak terlihat pada kegagalan fetch berikutnya
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
        // loadingIndicator terlihat saat halaman dibuka (CSS default).
        filterResults() {},
        switchTab() {},
        goToPage() {},
        showApiErrorToast() {},
        escapeHtml: (s) => String(s == null ? '' : s),
        URLSearchParams,
        apiFetch,
    };
    byId('loadingIndicator').style.display = 'block';
    const ctx = vm.createContext(sandbox);
    vm.runInContext(stripGoTemplates(lastInlineScript(HASIL())), ctx);
    return { sandbox, els, byId, domListeners };
}

test('S65 (perilaku): hasil.html — sukses lalu gagal (HTTP non-ok) → kartu error + tombol Coba Lagi TERLIHAT', async () => {
    let mode = 'ok-empty';
    const sb = makeHasilSandbox({
        apiFetch: () => {
            if (mode === 'http-fail') {
                return Promise.resolve({ ok: false, status: 500, json: () => Promise.resolve({ success: false, message: 'Server sedang bermasalah.' }) });
            }
            return Promise.resolve({
                ok: true,
                json: () => Promise.resolve({
                    success: true,
                    submissions: [], questions: [], identity_fields: [],
                    stats: null, pagination: { page: 1, total: 0, total_pages: 1 },
                }),
            });
        },
    });

    await sb.sandbox.loadResults(); // muat sukses (0 peserta)
    const indicator = sb.byId('loadingIndicator');
    assert.equal(indicator.style.display, 'none',
        'muat sukses menyembunyikan loadingIndicator (perilaku lama dipertahankan)');

    mode = 'http-fail';
    await sb.sandbox.loadResults(); // gagal berikutnya (paginasi/pencarian/error)
    assert.equal(indicator.style.display, 'block',
        'S65: jalur gagal WAJIB memulihkan display loadingIndicator — dulu tetap none '
        + '(innerHTML ditulis ke elemen display:none → kartu error tak terlihat sama sekali)');
    assert.match(indicator.innerHTML, /data-action="reload-page"/,
        'kartu error harus memuat tombol Coba Lagi (reload-page)');
    assert.match(indicator.innerHTML, /Server sedang bermasalah\./,
        'pesan error dari server ditampilkan');
});

test('S65 (perilaku): hasil.html — cabang !data.success juga memulihkan tampilan kartu error', async () => {
    let mode = 'ok-empty';
    const sb = makeHasilSandbox({
        apiFetch: () => {
            if (mode === 'soft-fail') {
                return Promise.resolve({ ok: true, json: () => Promise.resolve({ success: false, message: 'Ujian tidak ditemukan.' }) });
            }
            return Promise.resolve({
                ok: true,
                json: () => Promise.resolve({
                    success: true,
                    submissions: [], questions: [], identity_fields: [],
                    stats: null, pagination: { page: 1, total: 0, total_pages: 1 },
                }),
            });
        },
    });
    await sb.sandbox.loadResults();
    const indicator = sb.byId('loadingIndicator');
    assert.equal(indicator.style.display, 'none');

    mode = 'soft-fail';
    await sb.sandbox.loadResults();
    assert.equal(indicator.style.display, 'block',
        'cabang !data.success wajib memulihkan display sebelum menulis innerHTML');
    assert.match(indicator.innerHTML, /Ujian tidak ditemukan\./);
});

test('S65 (perilaku): hasil.html — exception jaringan (catch) juga memulihkan tampilan + tombol Coba Lagi', async () => {
    let mode = 'ok-empty';
    const sb = makeHasilSandbox({
        apiFetch: () => {
            if (mode === 'throw') return Promise.reject(new Error('network down'));
            return Promise.resolve({
                ok: true,
                json: () => Promise.resolve({
                    success: true,
                    submissions: [], questions: [], identity_fields: [],
                    stats: null, pagination: { page: 1, total: 0, total_pages: 1 },
                }),
            });
        },
    });
    await sb.sandbox.loadResults();
    const indicator = sb.byId('loadingIndicator');
    assert.equal(indicator.style.display, 'none');

    mode = 'throw';
    await sb.sandbox.loadResults();
    assert.equal(indicator.style.display, 'block',
        'jalur catch wajib memulihkan display loadingIndicator');
    assert.match(indicator.innerHTML, /data-action="reload-page"/,
        'tombol Coba Lagi ada pada jalur exception');
});

test('S65 (statik): ketiga jalur gagal loadResults memulihkan display sebelum menulis innerHTML', () => {
    const script = stripGoTemplates(lastInlineScript(HASIL()));
    // Setiap penugasan innerHTML ke loadingIndicator harus didahului
    // pemulihan .style.display = 'block' pada elemen yang sama.
    const writes = [...script.matchAll(/loadingIndicator'\)\.innerHTML/g)];
    assert.equal(writes.length, 3, 'hasil.html punya tepat 3 jalur penulisan error ke loadingIndicator');
    const restores = (script.match(/loadingIndicator'\)\.style\.display\s*=\s*'block'/g) || []).length;
    assert.ok(restores >= 3,
        `butuh ≥3 pemulihan display='block' (ditemukan ${restores}) — satu per jalur gagal`);
});

// ===========================================================================
// R67 — fokus jatuh ke body saat clear-search
// ===========================================================================

test('R67 (perilaku): clear-search membersihkan input lalu MENGEMBALIKAN FOKUS ke input pencarian', () => {
    const html = HASIL();
    const m = html.match(/Actions\.register\('clear-search',\s*function\s*\(el\)\s*\{[\s\S]*?\}\);/);
    assert.ok(m, 'registrasi handler clear-search harus ada di hasil.html');

    const focused = [];
    const searchInput = { value: 'budi', focus: () => focused.push('input') };
    const clearBtn = { style: { display: 'flex' } };
    const registered = {};
    const sandbox = {
        Actions: { register: (name, fn) => { registered[name] = fn; } },
        filterResults: () => {},
        document: {
            getElementById: (id) => (id === 'searchInput' ? searchInput
                : id === 'searchClearBtn' ? clearBtn : null),
        },
    };
    vm.runInNewContext(m[0], sandbox);
    assert.equal(typeof registered['clear-search'], 'function');
    registered['clear-search'](clearBtn);

    assert.equal(searchInput.value, '', 'nilai pencarian dibersihkan (perilaku lama)');
    assert.equal(clearBtn.style.display, 'none', 'tombol clear disembunyikan (perilaku lama)');
    assert.deepEqual(focused, ['input'],
        'R67: fokus HARUS dikembalikan ke searchInput — tombol clear hilang '
        + '(display:none) sehingga tanpa focus() eksplisit fokus jatuh ke <body>');
});

// ===========================================================================
// R68 — strength meter good/strong hex → token
// ===========================================================================

for (const [label, tpl] of [['register', REGISTER], ['reset_password', RESET_PASSWORD]]) {
    test(`R68: blok strengthLevels ${label} bebas hex — good→var(--color-success), strong→var(--color-accent-cyan)`, () => {
        const html = tpl();
        const m = html.match(/var\s+(?:strengthLevels|levels)\s*=\s*\[[\s\S]*?\]/);
        assert.ok(m, `blok strengthLevels harus ada di ${label}`);
        const block = m[0];
        assert.doesNotMatch(block, /#[0-9a-fA-F]{3,8}\b/,
            `${label}: blok levels masih memuat hex literal — pakai token theme.css`);
        assert.match(block, /var\(--color-success\)/,
            'level "good" (#22c55e) wajib var(--color-success)');
        assert.match(block, /var\(--color-accent-cyan\)/,
            'level "strong" (#06b6d4) wajib var(--color-accent-cyan)');
    });
}

// ===========================================================================
// R69 — copywriting publik
// ===========================================================================

test('R69: download.html tidak lagi mengklaim "(Stable)" hard-coded (klaim tak dikendalikan data server)', () => {
    const html = DOWNLOAD();
    assert.doesNotMatch(html, /\(Stable\)/,
        '"(Stable)" hard-coded dihapus — handler download.go tidak menyediakan data '
        + 'stabilitas rilis, jadi halaman tidak boleh mengklaim apa yang tak dikendalikan data');
    assert.match(html, /v\{\{\.webapp_version\}\}/,
        'badge versi webapp tetap menampilkan versi dari data server');
});

test('R69: hero landing selaras value-prop offline — "Ujian Digital Teraman & Siap Offline"', () => {
    const m = INDEX().match(/<h1 class="hero-title">([^<]*)<\/h1>/);
    assert.ok(m, 'hero title landing harus ada');
    assert.equal(m[1].trim(), 'Ujian Digital Teraman &amp; Siap Offline',
        'hero "Ujian Digital Cloud Teraman" diganti selaras keluarga eksekusi S22 (offline-first)');
});

// ===========================================================================
// R71 — link stylesheet eksternal wajib sebelum blok <style> inline pertama
// ===========================================================================

for (const [label, tpl] of [['hasil', HASIL], ['download', DOWNLOAD], ['register', REGISTER], ['cek_hasil', CEK_HASIL]]) {
    test(`R71: ${label}.html — tidak ada <link rel="stylesheet"> SETELAH blok <style> pertama`, () => {
        const html = tpl();
        const firstStyle = html.search(/<style[\s>]/);
        assert.ok(firstStyle !== -1, `${label}.html harus punya blok <style>`);
        const offenders = [...html.matchAll(/<link rel="stylesheet"[^>]*>/g)]
            .filter((mm) => mm.index > firstStyle);
        assert.deepEqual(offenders.map((mm) => mm[0]), [],
            `${label}.html: link stylesheet menyusup SETELAH <style> inline pertama — `
            + 'pindahkan SEMUA link eksternal sebelum blok <style> (urutan antar-link tetap)');
        // Diperbarui Batch 15 (R108): download.html berhenti me-link ulang
        // public-mobile/desktop.css — keduanya kini hanya dari public_head
        // (shared.html), jadi hitungan link lokal boleh 0 untuk halaman yang
        // mendelegasikan head-nya ke partial.
        if (!/{{\s*template\s+"public_head"/.test(html)) {
            const links = [...html.matchAll(/<link rel="stylesheet"/g)].length;
            assert.ok(links >= 2, `${label}.html masih punya ≥2 link stylesheet (semua dipindah, bukan dihapus)`);
        }
    });
}

// ===========================================================================
// S68 sisi publik — migrasi #f87171 & rgba(99,102,241,α)
// ===========================================================================

test('S68: seluruh templates/public bebas #f87171 literal — pakai var(--color-danger-bright)', () => {
    let total = 0;
    for (const [name, tpl] of PUBLIC_TEMPLATES) {
        const hits = (tpl().match(/#f87171/gi) || []).length;
        total += hits;
        assert.equal(hits, 0,
            `${name}: ${hits} pemakaian #f87171 literal tersisa — substitusi var(--color-danger-bright)`);
    }
    let uses = 0;
    for (const [, tpl] of PUBLIC_TEMPLATES) {
        uses += (tpl().match(/var\(--color-danger-bright\)/g) || []).length;
    }
    assert.ok(uses >= 10, `minimal 10 pemakaian var(--color-danger-bright) hasil migrasi, dapat ${uses}`);
});

test('S68: hasil.css bebas rgba(99,102,241,α) literal — substitusi persis rgba(var(--rgb-info), α)', () => {
    const css = HASIL_CSS();
    const literals = (css.match(/rgba\(\s*99\s*,\s*102\s*,\s*241\s*,/g) || []).length;
    assert.equal(literals, 0,
        `${literals} rgba(99,102,241,…) literal tersisa di hasil.css — pakai rgba(var(--rgb-info), α)`);
    const tokenUses = (css.match(/rgba\(\s*var\(\s*--rgb-info\s*\)\s*,/g) || []).length;
    assert.ok(tokenUses >= 18,
        `substitusi persis harus meninggalkan ≥18 pemakaian rgba(var(--rgb-info), α), dapat ${tokenUses} `
        + '(visual nol perubahan — triplet identik 99,102,241)');
});
