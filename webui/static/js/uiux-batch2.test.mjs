/* Regression contract tests untuk Batch 2 perbaikan UI/UX.
 * Referensi temuan: review_uiux_webui.md (ID: T1, T3, T4, T6, T7, T9, T10a, T11).
 *
 * Run with:  node --test static/js/uiux-batch2.test.mjs   (from webui/)
 *
 * Semua test adalah kontrak statik atas file ASLI yang dikirim/dibaca server,
 * ditulis SEBELUM implementasi (red → green). Kontras warna T9 diverifikasi
 * dengan perhitungan rasio WCAG sungguhan, bukan sekadar keberadaan nilai.
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
// Helper kontras WCAG (untuk T9)
// ---------------------------------------------------------------------------

function luminance(hex) {
    const c = hex.replace('#', '');
    const [r, g, b] = [0, 2, 4].map((i) => {
        let v = parseInt(c.slice(i, i + 2), 16) / 255;
        return v <= 0.03928 ? v / 12.92 : Math.pow((v + 0.055) / 1.055, 2.4);
    });
    return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

function contrastRatio(fgHex, bgHex) {
    const l1 = luminance(fgHex);
    const l2 = luminance(bgHex);
    return (Math.max(l1, l2) + 0.05) / (Math.min(l1, l2) + 0.05);
}

// Permukaan gelap TERTERANG yang dipakai kartu/topbar (kasus terburuk bagi
// teks abu-abu). Jika lolos di sini, lolos di permukaan lebih gelap.
const WORST_BG = '#1c1c2e';

// Resolusi var() sederhana ke nilai theme.css (dark default).
const TOKENS = {
    '--color-text': '#f8fafc',
    '--color-text-secondary': '#cbd5e1',
    '--color-text-muted': '#a0aec0'
};

function resolveColor(value) {
    const v = value.trim();
    const varMatch = v.match(/var\((--[a-z-]+)/);
    if (varMatch) return TOKENS[varMatch[1]] || null;
    const hex = v.match(/#[0-9a-fA-F]{6}\b/);
    return hex ? hex[0] : null;
}

/** Ekstrak isi blok `selector { ... }` pertama dari sebuah CSS. */
function cssRule(css, selectorRegex) {
    const m = css.match(new RegExp(selectorRegex + '\\s*\\{([^}]*)\\}'));
    return m ? m[1] : null;
}

// ---------------------------------------------------------------------------
// T1 — Registrasi wajib punya konfirmasi password
// ---------------------------------------------------------------------------

test('T1: register.html punya field "Ulangi Password" + validasi mismatch live', () => {
    const html = read('templates/public/register.html');

    assert.match(html, /id="regPasswordConfirm"/, 'field konfirmasi id=regPasswordConfirm harus ada');
    assert.match(html, /Ulangi Password/, 'label field konfirmasi harus ada');

    // Markup + pembanding JS minimal 3 referensi (input, baca JS, pesan).
    const refs = html.split('regPasswordConfirm').length - 1;
    assert.ok(refs >= 3, `regPasswordConfirm harus dirujuk di markup & JS (ditemukan ${refs})`);

    assert.match(html, /Password tidak cocok/, 'pesan mismatch eksplisit harus ada');
});

// ---------------------------------------------------------------------------
// T6 — Pesan error tidak boleh auto-hilang; wajib role="alert"
// ---------------------------------------------------------------------------

const AUTH_PAGES = [
    'templates/public/register.html',
    'templates/public/register_confirm.html',
    'templates/public/forgot_password.html',
    'templates/public/reset_password.html',
    'templates/admin/login.html'
];

for (const page of AUTH_PAGES) {
    test(`T6: ${path.basename(page)} — error persisten (tanpa setTimeout×error) & role="alert"`, () => {
        const html = read(page);

        assert.match(html, /role="alert"/, 'kontainer pesan error wajib role="alert" agar screen reader mengumumkan');

        // Tidak boleh ada satu baris pun yang memadukan setTimeout dengan
        // penanganan elemen error (pola lama: pesan error lenyap sendiri).
        const badLines = html.split('\n').filter((l) => /setTimeout/i.test(l) && /\berror\b/i.test(l));
        assert.deepEqual(badLines, [], `setTimeout tidak boleh menyentuh elemen error:\n${badLines.join('\n')}`);
    });
}

test('T6: login.html — fade-out flash hanya untuk sukses, bukan seluruh .flash-messages', () => {
    const html = read('templates/admin/login.html');
    const badLines = html.split('\n').filter((l) => /setTimeout|fade|dismiss/i.test(l) && /flash-messages/i.test(l));
    assert.deepEqual(badLines, [], 'selector .flash-messages tidak boleh lagi dipakai untuk auto-hide (mencakup error):\n' + badLines.join('\n'));
});

// ---------------------------------------------------------------------------
// T7 — Font design system dimuat di semua halaman admin
// ---------------------------------------------------------------------------

test('T7: partials/head.html memuat font Plus Jakarta Sans / Outfit', () => {
    const head = read('templates/admin/partials/head.html');
    assert.match(head, /fonts\.googleapis\.com/, 'halaman admin harus memuat Google Fonts (atau self-host)');
    assert.match(head, /Plus\+Jakarta\+Sans|Outfit/, 'keluarga font sesuai token theme.css');
});

// ---------------------------------------------------------------------------
// T9 — Kontras teks fungsional ≥ 4.5:1; #64748b lenyap dari titik kritis
// ---------------------------------------------------------------------------

const CONTRAST_TARGETS = [
    { file: 'static/css/tailwind/output.css', rule: '\\.form-hint', label: '.form-hint (output.css)' },
    { file: 'static/css/tailwind/output.css', rule: '\\.role-chip', label: '.role-chip (output.css)' },
];

for (const t of CONTRAST_TARGETS) {
    test(`T9: ${t.label} — kontras teks ≥ 4.5:1`, () => {
        const css = read(t.file);
        const body = cssRule(css, t.rule);
        assert.ok(body !== null, `rule ${t.rule} harus ada`);

        const colorLine = body.match(/color:\s*([^;]+);/);
        assert.ok(colorLine, `rule ${t.rule} harus punya deklarasi color`);
        const fg = resolveColor(colorLine[1]);
        assert.ok(fg !== null, `warna ${t.rule} harus hex atau var() yang dikenali`);

        const ratio = contrastRatio(fg, WORST_BG);
        assert.ok(ratio >= 4.5, `${t.label}: ${fg} di atas ${WORST_BG} = ${ratio.toFixed(2)}:1 (butuh ≥ 4.5:1)`);
    });
}

test('T9: #64748b tidak lagi dipakai di dashboard.html (dropdown instansi)', () => {
    // base.html sudah diarsipkan/dihapus (Batch 4, temuan S16) — hanya
    // dashboard.html yang tersisa sebagai pemakai warna ini.
    for (const f of ['templates/admin/dashboard.html']) {
        const html = read(f);
        assert.ok(!html.includes('#64748b'), `${f} masih memakai #64748b — ganti ke var(--color-text-muted)`);
    }
});

// ---------------------------------------------------------------------------
// T10a — Tombol ✕ toast: terlihat saat keyboard-focus + hit-area layak
// ---------------------------------------------------------------------------

for (const f of ['static/css/tailwind/output.css']) {
    test(`T10a: ${path.basename(f)} — .toast-close punya :focus-visible & padding ≥ 10px`, () => {
        const css = read(f);

        const focusRule = cssRule(css, '\\.toast-close:focus-visible');
        assert.ok(focusRule !== null, 'rule .toast-close:focus-visible harus ada');
        assert.match(focusRule, /opacity:\s*1/, 'focus-visible wajib mereveal tombol (dulu opacity:0 permanen di desktop)');

        const base = cssRule(css, '\\.toast-close');
        assert.ok(base !== null, 'rule .toast-close harus ada');
        const pad = base.match(/padding:\s*(\d+)px/);
        assert.ok(pad && parseInt(pad[1], 10) >= 10, `.toast-close butuh padding ≥ 10px agar hit-area mendekati 44px (dapat ${pad ? pad[1] : 'tidak ada'}px)`);
    });
}

// ---------------------------------------------------------------------------
// T11 — Hapus permanen di Hasil Ujian: berlabel & ≥ 44px
// ---------------------------------------------------------------------------

test('T11: tombol hapus submissions berlabel teks "Hapus" + target sentuh ≥ 44px', () => {
    const html = read('templates/admin/submissions.html');
    // Batch 7 (R28): tombol memanggil via data-action="delete-submission"
    // (delegasi); handler deleteSubmission hidup di admin.js.
    const idx = html.indexOf('data-action="delete-submission"');
    assert.ok(idx >= 0, 'tombol hapus (data-action="delete-submission") harus tetap ada');
    const btnRegion = html.slice(Math.max(0, idx - 300), idx + 500);

    assert.match(btnRegion, />\s*Hapus\s*</, 'label teks "Hapus" harus terlihat (bukan ikon-only)');
    assert.match(btnRegion, /min-height:\s*44px/, 'min-height:44px wajib');
    assert.match(btnRegion, /min-width:\s*44px/, 'min-width:44px wajib');
    assert.match(btnRegion, /aria-label=/, 'aria-label tetap dipertahankan');
});

// ---------------------------------------------------------------------------
// T4 — Entry point "Cek Hasil Ujian" di nav publik + halaman input token
// ---------------------------------------------------------------------------

test('T4: navbar publik punya link "Cek Hasil Ujian" → /hasil', () => {
    const html = read('templates/public/shared.html');
    assert.match(html, /href="\/hasil"/, 'nav publik harus menautkan /hasil');
    assert.match(html, /Cek Hasil Ujian/, 'label menu harus ada');
});

test('T4: route GET /hasil (tanpa token) terdaftar di main.go', () => {
    const main = read('cmd/server/main.go');
    assert.match(main, /r\.GET\("\/hasil",/, 'route statik /hasil harus terdaftar di samping /hasil/:token');
});

test('T4: handler CekHasilPage — redirect bila token diisi, render form bila kosong', () => {
    const src = fs.readFileSync(path.join(WEBUI_ROOT, 'internal/handlers/public/cek_hasil.go'), 'utf8');
    assert.match(src, /func CekHasilPage\(\)/, 'fungsi CekHasilPage harus ada');
    assert.match(src, /Redirect\(/, 'token terisi → redirect ke /hasil/<token>');
    assert.match(src, /"token"/, 'membaca query param token');
});

test('T4: halaman cek_hasil.html berisi form input token', () => {
    const html = read('templates/public/cek_hasil.html');
    assert.match(html, /action="\/hasil"/, 'form mengarah ke /hasil');
    assert.match(html, /name="token"/, 'input token harus ada');
    assert.match(html, /Cek Hasil Ujian/, 'judul halaman harus ada');
});

// ---------------------------------------------------------------------------
// T3 — Status Lulus/Belum Lulus/Belum Dikoreksi + legenda skor
// ---------------------------------------------------------------------------

test('T3: halaman hasil menampilkan status kelulusan eksplisit + legenda ambang', () => {
    const html = read('templates/public/hasil.html');

    // Batch 9 (S46): badge kini dirender dengan CLASS .score-status-badge —
    // id="scoreStatusBadge" diduplikasi tiap baris detail terbuka (HTML
    // invalid). Intent proteksi sama: badge status tetap ada & diisi dari JS.
    assert.match(html, /\.score-status-badge/, 'styling badge status kelulusan harus ada');
    const refs = html.split('score-status-badge').length - 1;
    assert.ok(refs >= 2, `badge status harus dirender dari JS juga (ditemukan ${refs} referensi)`);

    assert.match(html, /Belum Dikoreksi/, 'state skor belum ada → "Belum Dikoreksi"');
    assert.match(html, /Belum Lulus/, 'state di bawah ambang → "Belum Lulus"');
    assert.match(html, /Lulus/, 'state di atas ambang → "Lulus"');

    const legendIdx = html.indexOf('scoreLegend');
    assert.ok(legendIdx >= 0, 'legenda skor (id=scoreLegend) harus ada');
});
