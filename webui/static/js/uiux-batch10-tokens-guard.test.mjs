/* Guard Batch 10 — kontrak lintas-agen gradien tombol AA + --z-onboarding
 * di bawah toast + S58 substitusi triplet rgba di admin-base.css.
 *
 * Latar belakang & dampak bisnis:
 *   Re-review ronde 4 (bagian 5.7 review_uiux_webui.md) menemukan:
 *   - T18: definisi lokal token --grad-btn-* di shared.html dipindah ke
 *     theme.css (kontrak lintas-agen) agar halaman ADMIN (nav/dashboard)
 *     yang memakai var(--grad-btn-*) juga mewarisi endpoint gradien yang
 *     lolos WCAG AA terhadap label putih (semua ≥ 4.5:1):
 *       #9333ea = 5.38:1 · #7c3aed = 5.70:1 · #2563eb = 5.17:1 · #1d4ed8 = 6.70:1
 *   - R52: modal onboarding instansi (nav.html) memakai z-index 99999 dan
 *     mengalahkan toast/skip-link — feedback sukses/gagal tertimbun di bawah
 *     modal layar-pertama admin baru. Token --z-onboarding WAJIB bernilai DI
 *     BAWAH --z-toast.
 *   - S58: ±43 rgba literal di admin-base.css adalah pasangan PERSIS triplet
 *     token yang sudah ada (--rgb-info/danger/success/warning). Migrasi
 *     substitusi-persis menurunkan literal 60 → 17 tanpa perubahan visual.
 *
 * Test ini mengunci: nilai token terkunci persis, admin-base.css bebas dari
 * triplet literal yang dimigrasi, plafon sisa literal tidak boleh naik lagi,
 * dan regex penghitung tetap benar (self-test).
 *
 * Run with:  node --test static/js/uiux-batch10-tokens-guard.test.mjs   (from webui/)
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const read = (rel) => fs.readFileSync(path.join(__dirname, '..', rel), 'utf8');
/** CSS dengan komentar dibuang — komentar boleh memuat contoh warna lama
 *  tanpa mengelabui penghitungan deklarasi. */
const stripComments = (src) => src.replace(/\/\*[\s\S]*?\*\//g, '');

const THEME_RAW = read('css/theme.css');
const THEME_CSS = stripComments(THEME_RAW);
const ADMIN_RAW = read('css/admin-base.css');
const ADMIN_CSS = stripComments(ADMIN_RAW);

// Regex literal rgba digit-pembuka (pola S43): pemakaian token
// rgba(var(--rgb-*), α) TIDAK dihitung sebagai literal.
const RGBA_LITERAL_RE = /rgba\(\s*[0-9]/g;

// ---------------------------------------------------------------------------
// Kontrak lintas-agen #1 (T18) — token gradien tombol lolos AA di theme.css
// ---------------------------------------------------------------------------

test('B10 #1: theme.css :root mendefinisikan 4 token --grad-btn-* dengan nilai persis kontrak', () => {
    const expected = {
        '--grad-btn-violet-start': '#9333ea',
        '--grad-btn-violet-end': '#7c3aed',
        '--grad-btn-blue-start': '#2563eb',
        '--grad-btn-blue-end': '#1d4ed8',
    };
    for (const [token, value] of Object.entries(expected)) {
        const re = new RegExp(token.replace(/[-]/g, '\\-') + '\\s*:\\s*' + value + '\\s*;');
        assert.match(THEME_CSS, re,
            `${token}: ${value} harus terdefinisi persis di :root theme.css ` +
            '(kontrak lintas-agen — nilai sama dengan definisi lokal shared.html lama)');
    }
});

test('B10 #1: alasan kontras AA terdokumentasi di komentar dekat definisi --grad-btn-*', () => {
    // Ambil blok komentar yang membahas grad-btn; wajib menyebut ambang AA 4.5:1
    const commentBlocks = [...THEME_RAW.matchAll(/\/\*[\s\S]*?\*\//g)].map((m) => m[0]);
    const doc = commentBlocks.find((c) => /grad-btn/.test(c));
    assert.ok(doc, 'harus ada komentar yang menjelaskan kontrak token --grad-btn-*');
    assert.match(doc, /4\.5/,
        'komentar kontrak harus mencantumkan rasio/ambang kontras AA (≥ 4.5:1)');
});

// ---------------------------------------------------------------------------
// Kontrak lintas-agen #2 (R52) — --z-onboarding DI BAWAH toast
// ---------------------------------------------------------------------------

test('B10 #2: --z-onboarding terdefinisi di skala --z-* dengan nilai DI BAWAH --z-toast', () => {
    const onboarding = THEME_CSS.match(/--z-onboarding\s*:\s*(\d+)\s*;/);
    const toast = THEME_CSS.match(/--z-toast\s*:\s*(\d+)\s*;/);
    assert.ok(onboarding, '--z-onboarding harus terdefinisi di :root theme.css');
    assert.ok(toast, '--z-toast harus terdefinisi di :root theme.css');
    const zOnboarding = parseInt(onboarding[1], 10);
    const zToast = parseInt(toast[1], 10);
    assert.ok(zOnboarding < zToast,
        `--z-onboarding (${zOnboarding}) harus DI BAWAH --z-toast (${zToast}) — ` +
        'toast sukses/gagal wajib tetap terlihat di atas modal onboarding (R52)');
});

test('B10 #2: alasan penempatan --z-onboarding terdokumentasi di komentar skala z', () => {
    const doc = [...THEME_RAW.matchAll(/\/\*[\s\S]*?\*\//g)].map((m) => m[0])
        .find((c) => /--z-onboarding|R52/.test(c));
    assert.ok(doc, 'skala z-index wajib punya komentar yang membahas --z-onboarding');
    assert.match(doc, /(toast|di bawah|R52)/i,
        'komentar harus menjelaskan mengapa onboarding berada di bawah toast');
});

// ---------------------------------------------------------------------------
// S58 sisi CSS — admin-base.css bebas triplet literal yang punya pasangan token
// ---------------------------------------------------------------------------

const MIGRATED_TRIPLETS = [
    ['--rgb-info', '99', '102', '241'],
    ['--rgb-danger', '239', '68', '68'],
    ['--rgb-success', '16', '185', '129'],
    ['--rgb-warning', '245', '158', '11'],
];

for (const [token, r, g, b] of MIGRATED_TRIPLETS) {
    const re = new RegExp(`rgba\\(\\s*${r}\\s*,\\s*${g}\\s*,\\s*${b}\\s*,`, 'g');

    test(`S58/B10: admin-base.css bebas literal rgb(${r},${g},${b},*) — pakai var(${token})`, () => {
        const n = (ADMIN_CSS.match(re) || []).length;
        assert.equal(n, 0,
            `${n} rgba(${r},${g},${b},…) literal tersisa di admin-base.css — ` +
            `substitusi-persis memakai rgba(var(${token}), α)`);
    });

    test(`S58/B10: admin-base.css benar-benar memakai rgba(var(${token}), …) (sanity > 0)`, () => {
        const uses = (ADMIN_CSS.match(
            new RegExp('rgba\\(\\s*var\\(\\s*' + token.replace(/[-]/g, '\\-') + '\\)'), 'g') || []).length;
        assert.ok(uses > 0,
            `rgba(var(${token}), α) tidak dipakai sama sekali — migrasi tidak boleh ` +
            '"lolos" dengan sekadar menghapus pemakaian');
    });
}

// ---------------------------------------------------------------------------
// Plafon pasca-migrasi — hasil ukur langsung, jangan dinaikkan tanpa alasan
// ---------------------------------------------------------------------------

test('S58/B10 (guard): total rgba LITERAL di admin-base.css turun ke plafon baru ≤ 17 dan tidak naik', () => {
    const n = (ADMIN_CSS.match(RGBA_LITERAL_RE) || []).length;
    assert.ok(n <= 17,
        `rgba literal admin-base.css = ${n}, plafon pasca-S58 ≤ 17 ` +
        '(baseline pra-migrasi 60; sisa literal memang belum punya pasangan token) — ' +
        'pakai rgba(var(--rgb-*), α) untuk warna baru');
});

// ---------------------------------------------------------------------------
// Self-test regex — penghitung tetap benar
// ---------------------------------------------------------------------------

test('B10 (self-test): regex literal tidak menghitung rgba(var( sebagai literal', () => {
    assert.equal(('rgba(var(--rgb-info), 0.35)'.match(RGBA_LITERAL_RE) || []).length, 0,
        'rgba(var(...)) adalah pemakaian token, bukan literal');
    assert.equal(('rgba(99,102,241,.5)'.match(/rgba\(\s*99\s*,\s*102\s*,\s*241\s*,/g) || []).length, 1,
        'regex triplet harus menangkap literal dengan spasi/nol-tanda fleksibel');
    assert.equal(('rgba(99, 102, 241, .5)'.match(/rgba\(\s*99\s*,\s*102\s*,\s*241\s*,/g) || []).length, 1);
    assert.equal(('rgba(255,255,255,0.1)'.match(RGBA_LITERAL_RE) || []).length, 1,
        'rgba digit pembuka adalah literal sungguhan dan harus terhitung');
});
