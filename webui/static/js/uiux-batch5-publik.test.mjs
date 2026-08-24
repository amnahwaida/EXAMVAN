/* Regression contract tests untuk Batch 5 perbaikan UI/UX (halaman publik).
 * Referensi temuan: review_uiux_webui.md §5.5 RE-REVIEW RONDE 2
 * (ID: T12, R17, R20, R22, R18-publik, R23, R19, S30-dedup).
 *
 * Run with:  node --test static/js/uiux-batch5-publik.test.mjs   (from webui/)
 *
 * Semua test adalah kontrak statik: membaca file template/CSS/handler ASLI dan
 * memastikan properti kunci perbaikan tidak pernah regresi.
 *
 * Dampak bisnis yang dilindungi:
 *   T12  — siswa/guru tidak lagi melihat judul internal ("Database Tidak
 *          Tersedia") sebagai <h1> raksasa plus chip "Peserta: 0" yang
 *          menyesatkan saat halaman hasil error.
 *   R17  — screen reader pengunjung publik tahu halaman mana yang sedang
 *          aktif di navigasi (aria-current="page").
 *   R20  — cetak halaman hasil tidak lagi membawa elemen pencarian/paginasi;
 *          selector print cocok dengan kelas nyata di markup.
 *   R22  — calon guru yang reset password mendapat umpan balik kekuatan
 *          password yang sama seperti saat mendaftar.
 *   R18  — pesan captcha Turnstile diumumkan screen reader & bersih sebelum
 *          percobaan submit berikutnya.
 *   R23  — input cari nama siswa punya nama aksesibel (tanpa bergantung
 *          placeholder yang hilang saat mengetik).
 *   R19  — outline heading landing tidak melompat h1 → h3.
 *   S30  — markup skip-link tidak lagi disalin inline 9× (satu sumber).
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const WEBUI_ROOT = path.join(__dirname, '..', '..');
const read = (rel) => fs.readFileSync(path.join(WEBUI_ROOT, rel), 'utf8');

// ---------------------------------------------------------------------------
// T12 — Hero halaman hasil tidak dirender saat error/disabled
// ---------------------------------------------------------------------------

test('T12: handler hasil.go tidak memasangkan pesan internal ke field exam_name', () => {
    const go = read('internal/handlers/public/hasil.go');
    assert.ok(!go.includes('"exam_name": "Database Tidak Tersedia"'),
        'pesan internal DB jangan dikirim sebagai exam_name (dulu menjadi h1 raksasa)');
    assert.ok(!go.includes('"exam_name": "Error"'),
        '"Error" jangan dikirim sebagai exam_name');
    // State error mengirim flag bersih + exam_name netral/kosong.
    assert.match(go, /"error_state":\s*true/, 'state error wajib membawa flag error_state');
});

test('T12: hero exam-hero hanya dirender pada cabang sukses; cabang error/disabled memakai judul netral', () => {
    const html = read('templates/public/hasil.html');

    const idxMain = html.indexOf('id="main-content"');
    assert.ok(idxMain >= 0, '#main-content harus ada');

    const iIf = html.indexOf('{{if .is_disabled}}', idxMain);
    assert.ok(iIf >= 0, 'cabang {{if .is_disabled}} harus ada di dalam main');

    const iElseErr = html.indexOf('{{else if .error}}', iIf);
    assert.ok(iElseErr >= 0, 'harus ada cabang {{else if .error}}');

    const iElseSuccess = html.indexOf('{{else}}', iElseErr);
    assert.ok(iElseSuccess >= 0, 'harus ada cabang sukses {{else}}');

    const disabledBranch = html.slice(iIf, iElseErr);
    const errorBranch = html.slice(iElseErr, iElseSuccess);
    const successBranch = html.slice(iElseSuccess);

    assert.ok(!disabledBranch.includes('id="examTitle"'),
        'hero dinamis (examTitle/token/Peserta) tidak boleh tampil saat dinonaktifkan');
    assert.ok(!errorBranch.includes('id="examTitle"'),
        'hero dinamis (examTitle/token/Peserta) tidak boleh tampil saat error');
    assert.match(disabledBranch, /<h1[^>]*>Hasil Ujian<\/h1>/,
        'state dinonaktifkan wajib menampilkan judul netral "Hasil Ujian"');
    assert.match(errorBranch, /<h1[^>]*>Hasil Ujian<\/h1>/,
        'state error wajib menampilkan judul netral "Hasil Ujian"');
    assert.ok(successBranch.includes('id="examTitle"'),
        'judul ujian (examTitle) hanya untuk state sukses');
});

test('T12: polling hasil tidak dijalankan pada halaman error (elemen hero tak ada lagi)', () => {
    const html = read('templates/public/hasil.html');
    assert.match(html, /pageHasError/,
        'init JS wajib mengecek flag halaman error agar loadResults tidak menyentuh elemen yang sudah tidak ada');
    // Batch 10 (S53/S54): blok guard init kini juga memasang listener
    // hashchange + navigasi keyboard sebelum loadResults — cukup pastikan
    // loadResults() berada DI DALAM blok guard yang sama.
    const init = html.match(/if\s*\(\s*!isDisabled[\s\S]{0,80}?pageHasError[\s\S]{0,200}?\{[\s\S]{0,800}?switchTab\(resolveTabFromHash\(\)[\s\S]{0,2200}?loadResults\(\);/);
    assert.ok(init, 'panggilan loadResults() tetap dijalankan dari dalam guard state sukses (!isDisabled && !pageHasError)');
});

// ---------------------------------------------------------------------------
// R17 — Nav publik menandai link aktif dengan aria-current="page"
// ---------------------------------------------------------------------------

test('R17: script penanda aktif nav publik juga menyetel/membersihkan aria-current', () => {
    const html = read('templates/public/shared.html');
    const foot = html.slice(html.indexOf('{{ define "public_foot" }}'));
    assert.match(foot, /setAttribute\(['"]aria-current['"],\s*['"]page['"]\)/,
        'link aktif wajib dapat aria-current="page" (screen reader tidak bisa merasakan class .active)');
    assert.match(foot, /removeAttribute\(['"]aria-current['"]\)/,
        'link lain wajib dibersihkan dari aria-current');
});

// ---------------------------------------------------------------------------
// R20 — Print CSS memakai kelas nyata (.search-section/.pagination-wrapper)
// ---------------------------------------------------------------------------

test('R20: blok @media print hasil.css menyembunyikan elemen yang benar-benar ada', () => {
    const css = read('static/css/hasil.css');
    // Blok print berakhir tepat sebelum komentar blok @media berikutnya.
    const iPrint = css.indexOf('@media print');
    assert.ok(iPrint >= 0, 'blok @media print harus ada di hasil.css');
    const block = css.slice(iPrint, css.indexOf('/* Touch targets', iPrint));

    assert.ok(block.includes('.search-section'), 'selector print harus .search-section (bukan .search-card yang tidak eksis)');
    assert.ok(block.includes('.pagination-wrapper'), 'selector print harus .pagination-wrapper');
    assert.doesNotMatch(block, /\.search-card/, '.search-card tidak eksis di markup');
    assert.doesNotMatch(block, /\.pagination(?![\w-])/, '.pagination telanjang tidak eksis (yang ada .pagination-wrapper)');
    assert.ok(block.includes('.header-badge'), 'badge header ikut disembunyikan saat cetak');

    // Validasi programatik: setiap selector pada daftar print-hide harus
    // cocok dengan class/id yang benar-benar ada di hasil.html.
    const hideRule = block.match(/([^{}]+)\{\s*display:\s*none\s*!important/);
    assert.ok(hideRule, 'ada rule print-hide display:none !important');
    const selectors = hideRule[1].split(',').map((s) => s.trim()).filter(Boolean);
    const html = read('templates/public/hasil.html');
    // .skip-link dsb. dapat dirender lewat partial public_skip_link di
    // shared.html — jadi validasi terhadap gabungan markup + partial.
    const haystack = html + read('templates/public/shared.html');
    assert.ok(selectors.length >= 5, `daftar print-hide wajar (>=5 selector), dapat ${selectors.length}`);
    for (const sel of selectors) {
        assert.match(sel, /^[.#][\w-]+$/, `selector "${sel}" harus satu class/id sederhana`);
        assert.ok(haystack.includes(sel.slice(1)), `kelas/id "${sel}" harus eksis di hasil.html (atau partial-nya)`);
    }
});

// ---------------------------------------------------------------------------
// R22 — reset_password.html memiliki strength meter seperti register.html
// ---------------------------------------------------------------------------

test('R22: reset_password punya strength meter + skoring live yang di-wire ke input password', () => {
    const reset = read('templates/public/reset_password.html');
    const register = read('templates/public/register.html');

    // Elemen meter konsisten dengan register
    for (const frag of ['class="pw-strength-bar"', 'id="pwBarFill"', 'id="pwStrengthText"']) {
        assert.ok(reset.includes(frag), `markup meter "${frag}" harus ada (konsisten dengan register.html)`);
        assert.ok(register.includes(frag), `sanity: register.html memang memuat ${frag}`);
    }

    // Fungsi skor diekstrak (inline, pola halaman publik standalone) dan dipanggil
    assert.match(reset, /function\s+scorePassword\(/,
        'harus ada fungsi skoring password (ekstraksi dari register)');
    const wiring = reset.match(/pwInput\.addEventListener\('input'[\s\S]{0,600}?\}\);/);
    assert.ok(wiring, 'handler input #password harus terdaftar');
    assert.match(wiring[0], /scorePassword\(|strengthLevels\[/,
        'handler input harus memanggil fungsi skor / level kekuatan');
    assert.match(reset, /getElementById\(['"]password['"]\)/, 'meter di-wire ke input #password');
});

// ---------------------------------------------------------------------------
// R18-publik — #turnstileError role="alert" + pesan dibersihkan ulang
// ---------------------------------------------------------------------------

for (const page of ['register', 'forgot_password', 'reset_password']) {
    test(`R18: ${page}.html — turnstileError ber-role alert & dibersihkan sebelum attempt berikutnya`, () => {
        const html = read(`templates/public/${page}.html`);
        assert.match(html, /<div id="turnstileError"[^>]*role="alert"/,
            'pesan captcha harus role="alert" agar diumumkan screen reader');
        assert.match(html, /getElementById\('turnstileError'\);\s*if\s*\(\s*terr\s*\)\s*\{\s*terr\.style\.display\s*=\s*'none';\s*terr\.textContent\s*=\s*''/,
            'handler submit wajib membersihkan pesan lama di awal attempt berikutnya');
    });
}

// ---------------------------------------------------------------------------
// R23 — #searchInput punya aria-label
// ---------------------------------------------------------------------------

test('R23: searchInput halaman hasil punya aria-label "Cari nama siswa"', () => {
    const html = read('templates/public/hasil.html');
    const m = html.match(/<input type="text" class="search-input" id="searchInput"([^>]*)>/s);
    assert.ok(m, '#searchInput harus ada');
    assert.match(m[1], /aria-label="Cari nama siswa"/,
        'placeholder saja tidak cukup — placeholder hilang saat user mengetik');
});

// ---------------------------------------------------------------------------
// R19 — mockup landing bukan heading h3 (lompatan h1 → h3)
// ---------------------------------------------------------------------------

test('R19: index.html tidak memuat h3.mockup-title (menjadi p dengan visual sama)', () => {
    const html = read('templates/public/index.html');
    assert.ok(!html.includes('<h3 class="mockup-title">'),
        'heading h3 setelah h1 melanggar urutan outline');
    assert.match(html, /<p class="mockup-title">/,
        'visual dipertahankan via <p class="mockup-title"> (sudah ditata oleh class)');
});

// ---------------------------------------------------------------------------
// S30-dedup — skip-link satu sumber, tanpa 9 salinan inline style
// ---------------------------------------------------------------------------

const PUBLIC_DIR = path.join(WEBUI_ROOT, 'templates', 'public');

test('S30: inline style skip-link lama (position:absolute;left:-9999px) tinggal <= 1 di folder public', () => {
    let hits = 0;
    for (const f of fs.readdirSync(PUBLIC_DIR)) {
        if (!f.endsWith('.html')) continue;
        const src = fs.readFileSync(path.join(PUBLIC_DIR, f), 'utf8');
        hits += src.split('position:absolute;left:-9999px').length - 1;
    }
    assert.ok(hits <= 1,
        `inline style lama masih muncul ${hits}× (dulu 9×) — bom drift; pakai class .skip-link`);
});

test('S30: partial tunggal public_skip_link ada dan dipakai semua halaman publik', () => {
    const shared = read('templates/public/shared.html');
    assert.match(shared, /\{\{ define "public_skip_link" \}\}/,
        'shared.html wajib mendefinisikan partial skip-link sekali');
    assert.match(shared, /\{\{ template "public_skip_link" \. \}\}/,
        'public_head memakai partial tersebut');

    for (const page of ['cek_hasil', 'forgot_password', 'hasil', 'register', 'register_confirm', 'reset_password']) {
        const html = read(`templates/public/${page}.html`);
        const usesPartial = html.includes('{{ template "public_skip_link" . }}');
        const hasClassLink = html.includes('class="skip-link"');
        assert.ok(usesPartial || hasClassLink,
            `${page}.html masih harus punya mekanisme skip-link (partial ATAU class .skip-link)`);
        assert.ok(html.includes('id="main-content"') || html.includes('#main-content'),
            `${page}.html: target anchor #main-content harus eksis`);
    }
});
