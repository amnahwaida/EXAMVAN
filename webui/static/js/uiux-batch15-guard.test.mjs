/**
 * ══════════════════════════════════════════════════════════════════════════
 * Suite UI/UX BATCH 15 — GUARD TEMPLATE GO & INTEGRITAS TEST (agen batch15-guard)
 * ══════════════════════════════════════════════════════════════════════════
 *
 * Referensi: review_uiux_webui.md bagian 5.12 RE-REVIEW RONDE 9.
 *
 * LATAR BELAKANG (kenapa suite ini ADA — baca sebelum mengubah):
 *
 * Ronde 9 menemukan kerusakan produksi paling serius sepanjang proyek:
 * template admin/settings.html GAGAL DI-PARSE GO sejak Batch 12 (komit
 * 4fc2ab8) karena satu `{{ if … }}` tanpa `{{ end }}` pasangan. Halaman
 * Pengaturan mati total untuk semua role pada setiap build pasca-Batch 12,
 * dan tidak ada yang sadar selama TIGA siklus batch (13 & 14) karena:
 *
 *   1. Seluruh suite guard UI/UX adalah JavaScript statik yang membaca
 *      file sebagai TEKS — tidak satu pun mengeksekusi parser Go, padahal
 *      parser Go-lah satu-satunya otoritas keabsahan template.
 *   2. Test Go `internal/handlers/admin` (yang memang mem-parse template)
 *      tidak dijalankan oleh siapa pun saat eksekusi batch — hanya
 *      `node --test` dan `go build`/`go vet` yang diverifikasi, padahal
 *      template yang gagal parse tetap lolos build (template diparse saat
 *      runtime, bukan compile time).
 *
 * Kontrak-kontrak yang ditegakkan agen ini:
 *
 *   S93 — KESEIMBANGAN TEMPLATE: untuk SETIAP file .html di folder templates (rekursif),
 *         jumlah tag pembuka blok (`{{ if`, `{{ range`, `{{ with`,
 *         `{{ block`, `{{ define`, termasuk varian trim-markers
 *         `{{-`/`-}}`) HARUS sama persis dengan jumlah `{{ end }}`.
 *         Ini adalah terjemahan statik dari apa yang parser Go validasikan;
 *         cukup cepat untuk dijalankan tiap kali suite node berjalan, dan
 *         akan MEMERAH sejak detik pertama jika kelas kesalahan Batch 12
 *         terulang di template mana pun.
 *         ⚠️ Guard ini PELENGKAP, bukan pengganti `go test ./...` — test Go
 *         tetap WAJIB dijalankan (dan catat hasilnya) setiap batch, karena
 *         hanya ia yang memvalidasi SEMANTIK (mis. kondisi `and` vs `or`
 *         pada T27 yang secara sintaks sempurna tapi salah arti).
 *
 *   T26/T27 — VERIFIKASI STRUKTUR SETTINGS.HTML: file yang sama yang
 *         dulu patah kini dikunci strukturnya: (a) keseimbangan global
 *         (tercakup S93), (b) kondisi role-gate section Kelola User harus
 *         `(or $isSuper $isOp)` — BUKAN argumen datar `and $isSuper $isOp`
 *         yang tidak pernah true karena NormalizeSessionRole selalu
 *         menghasilkan tepat satu role, dan (c) tidak boleh ada lagi dua
 *         if identik berturut-turut (pola wrapper ganda yang memicu insiden).
 *
 * Ronde eksekusi Batch 15 (item milik agen guard, §5.12):
 *
 *   T29/S104 — GUARD ANTI-VAKUM: pola `html.slice(html.indexOf(marker))`
 *         tanpa cek keberadaan marker adalah no-op diam-diam saat marker
 *         drift (preseden T29: marker 'detail-identity-items' plural = 0 hit
 *         di hasil.html → indexOf=-1 → slice(-1) = 1 karakter → asersi R91
 *         Batch 14 mengamati string kosong). Kontrak: util sliceBlock()
 *         (uiux-batch15-guard-util.mjs) yang THROW bila marker absen;
 *         guard batch14-publik wajib memakainya dengan anchor NYATA
 *         (.detail-identity-item) plus asersi POSITIF escapeHtml.
 *
 *   R105 — REGEX COUNTER CASE-INSENSITIVE: counter guard yang case-sensitive
 *         bisa dilewati cukup dengan menulis kapital (`RGBA(255,…`,
 *         `Z-INDEX: 999`, ` ONCLICK=`). Kontrak: RGBA_RE batch7-tokens &
 *         batch11-settings-guard, INLINE_HANDLER_RE batch11-settings-guard,
 *         dan regex z-index batch14-tokens-guard wajib membawa flag /i —
 *         diverifikasi terhadap string sintetis KAPITAL.
 *
 *   S99 — CAKUPAN BOLONG DITUTUP: (a) BASELINES batch9-tokens-guard wajib
 *         punya entri pengawas-detail.js & device-fingerprint.js (aktual
 *         0/0); (b) plafon hex+rgba untuk static/css/public-desktop.css &
 *         public-mobile.css dikunci angka aktual di suite ini — target
 *         jangka panjang MENURUN saat literal bermigrasi ke token.
 *
 *   S95 — PLAFON = AKTUAL (kontrak ronde 5): seluruh entri
 *         RGBA_BASELINE_PER_FILE batch7-tokens & BASELINE_HEX batch8-publik
 *         divalidasi == hasil ukur independen di sini. Migrasi agen lain
 *         menurunkan literal; plafon yang tidak ikut turun menyisakan slack
 *         hantu ±39 titik yang membolehkan erosi token diam-diam.
 *
 *   R118 — FILES guard batch8-publik mencakup cek_hasil.html, index.html,
 *         forgot_password.html (informative; aktual 0/0 hari ini) supaya
 *         pertumbuhan literal pertama langsung memerah.
 *
 * Cara kalibrasi ulang bila test ini MEMERAH setelah edit sah:
 *   - Jika Anda MENAMBAH blok `{{ if }}` baru, tambahkan `{{ end }}`-nya —
 *     test ini justru sedang melindungi Anda.
 *   - Jika Anda mengubah semantik role-gate, baca dulu T27 di dokumen review:
 *     operator yang benar adalah `or` (user punya SATU role).
 */
import test from 'node:test';
import assert from 'node:assert/strict';
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join, relative } from 'node:path';
import { sliceBlock } from './uiux-batch15-guard-util.mjs';

const ROOT = new URL('.', import.meta.url).pathname.replace(/\/static\/js\/$/, '');
const TPL_DIR = join(ROOT, 'templates');
const JS_DIR = join(ROOT, 'static', 'js');
const CSS_DIR = join(ROOT, 'static', 'css');

/** Baca file suite guard lain (untuk kontrak meta atas guard sendiri). */
const readSuite = (name) => readFileSync(join(JS_DIR, name), 'utf8');
const B7_TOKENS = readSuite('uiux-batch7-tokens.test.mjs');
const B8_PUBLIK = readSuite('uiux-batch8-publik.test.mjs');
const B9_TOKENS_GUARD = readSuite('uiux-batch9-tokens-guard.test.mjs');
const B11_SETTINGS = readSuite('uiux-batch11-settings-guard.test.mjs');
const B14_TOKENS_GUARD = readSuite('uiux-batch14-tokens-guard.test.mjs');
const B14_PUBLIK = readSuite('uiux-batch14-publik.test.mjs');

// Counter regex yang diukur ulang agen ini (HARUS identik polanya dengan
// suite pemiliknya — S43: hanya rgba digit-pembuka yang dihitung literal).
const RGBA_RE = /rgba\(\s*[0-9]/gi;
const HEX_RE = /#[0-9a-fA-F]{3,8}\b/g;

/** Kumpulkan seluruh file .html rekursif di bawah templates/. */
function listHtml(dir, acc = []) {
    for (const name of readdirSync(dir)) {
        const full = join(dir, name);
        if (statSync(full).isDirectory()) listHtml(full, acc);
        else if (name.endsWith('.html')) acc.push(full);
    }
    return acc;
}

// Regex opener mencakup trim markers `{{- if` dan `{{if`; `else` sengaja
// TIDAK dihitung (tidak membuka blok baru). `end` juga dicocokkan trim-aware.
const OPENER_RE = /\{\{-?\s*(?:if|range|with|block|define)\b/g;
const END_RE = /\{\{-?\s*end\b/g;

test('S93 (statik): SELURUH template Go seimbang — jumlah {{if/range/with/block/define}} == jumlah {{end}}', () => {
    const files = listHtml(TPL_DIR);
    assert.ok(files.length > 10, 'sanity: folder templates ditemukan (' + files.length + ' file)');

    const rusak = [];
    for (const f of files) {
        const src = readFileSync(f, 'utf8');
        const open = (src.match(OPENER_RE) || []).length;
        const end = (src.match(END_RE) || []).length;
        if (open !== end) {
            rusak.push(`${relative(ROOT, f)}: ${open} pembuka vs ${end} end (selisih ${open - end})`);
        }
    }
    assert.deepEqual(rusak, [],
        'Template tak seimbang = GAGAL PARSE GO saat runtime (preseden T26: settings.html mati total ' +
        'sejak Batch 12 tanpa terdeteksi 3 rilis). Perbaiki pasangan if/end sebelum merge.');
});

test('T27 (statik): kondisi role-gate section Kelola User memakai (or $isSuper $isOp) — bukan and datar', () => {
    const src = readFileSync(join(TPL_DIR, 'admin', 'settings.html'), 'utf8');

    // (a) Operator OR wajib ada — user selalu punya TEPAT SATU role, sehingga
    //     `and $isSuper $isOp` tidak pernah true (regresi Batch 14).
    assert.match(src, /\{\{[-~]?\s*if\s+and\s+\(not \$locked\)\s+\(or \$isSuper \$isOp\)\s*\}\}/,
        'section Kelola User harus dibungkus {{ if and (not $locked) (or $isSuper $isOp) }}');
    // (b) Bentuk salah yang menyebabkan Kelola User lenyap tidak boleh kembali.
    assert.doesNotMatch(src, /if\s+and\s+\(not \$locked\)\s+\$isSuper\s+\$isOp(?!\s*\))/,
        'JANGAN pakai `and $locked $isSuper $isOp` datar — tak pernah true (lihat T27)');
    // (c) Anti-regresi wrapper ganda: tidak boleh ada dua if identik berturut-turut.
    const duplikat = /\{\{[^}]*\$locked[^}]*\}\}\s*\n\s*\{\{[^}]*\$locked[^}]*\}\}/;
    assert.doesNotMatch(src, duplikat,
        'Dua if ber-$locked berturut-turut = pola wrapper ganda penyebab insiden T26/T27');
});

// ════════════════════════════════════════════════════════════════════════
// S104 — util sliceBlock: throw bila marker absen (anti-guard-vakum)
// ════════════════════════════════════════════════════════════════════════

test('S104 (unit): sliceBlock mengembalikan blok dari marker yang eksis', () => {
    const html = '<p>depan</p><div class="detail-identity-item">isi blok</div>';
    const block = sliceBlock(html, 'detail-identity-item');
    assert.ok(block.startsWith('detail-identity-item'), 'blok mulai dari marker');
    assert.ok(block.includes('isi blok'), 'blok memuat sisa sumber');
});

test('S104 (unit): sliceBlock THROW bila marker absen — guard vakum harus MERAH, bukan no-op', () => {
    // Ini persis lubang T29: marker 0 hit dulu menghasilkan slice(-1) = 1
    // karakter dan seluruh asersi di atasnya mengamati string kosong.
    assert.throws(() => sliceBlock('<div>tanpa anchor</div>', 'detail-identity-items'),
        /TIDAK ditemukan/,
        'marker absen wajib melempar error dengan pesan jelas (bukan string kosong diam-diam)');
});

// ════════════════════════════════════════════════════════════════════════
// T29 — guard R91 batch14-publik vakum: marker salah + tanpa asersi positif
// ════════════════════════════════════════════════════════════════════════

const HASIL_HTML = readFileSync(join(TPL_DIR, 'public', 'hasil.html'), 'utf8');

test('T29 (demonstrasi slack): marker lama "detail-identity-items" (plural) 0 hit di hasil.html', () => {
    // BUKTI akar masalah: guard R91 Batch 14 memotong dengan marker ini,
    // padahal render-JS hanya mendefinisikan kelas SINGULAR
    // `detail-identity-item`. indexOf = -1 → slice(-1) → asersi no-op.
    const hits = HASIL_HTML.split('detail-identity-items').length - 1;
    assert.equal(hits, 0,
        'marker "detail-identity-items" memang tidak pernah ada di hasil.html — bukti guard lama VAKUM');
});

test('T29 (kontrak): guard batch14-publik memakai sliceBlock + anchor .detail-identity-item yang nyata', () => {
    assert.match(B14_PUBLIK, /sliceBlock\(/,
        'guard blok di batch14-publik wajib lewat util sliceBlock (throw anti-vakum), bukan slice(indexOf()) raw');
    assert.match(B14_PUBLIK, /sliceBlock\(HASIL_HTML,\s*'detail-identity-item'/,
        'anchor wajib kelas singular .detail-identity-item yang benar-benar ada di render-JS hasil.html');
    assert.doesNotMatch(B14_PUBLIK, /HASIL_HTML\.slice\(HASIL_HTML\.indexOf\(/,
        'pola slice(indexOf()) raw dilarang di suite ini — satu rename template cukup mematikan banyak asersi sekaligus (S104)');
});

test('T29 (positif): waktu mulai/kumpul kartu identitas ter-escape EKSPLISIT di hasil.html', () => {
    // Asersi POSITIF yang hilang dari guard lama: bukan cuma "tidak ada versi
    // mentah", tapi versi ter-escape WAJIB ada — sehingga marker/anchor yang
    // drift tidak bisa lagi menyelundupkan guard kosong.
    assert.match(HASIL_HTML, /\$\{escapeHtml\(String\(startTimeStr\)\)\}/,
        '${escapeHtml(String(startTimeStr))} wajib ada di kartu identitas detail (Waktu Mulai)');
    assert.match(HASIL_HTML, /\$\{escapeHtml\(String\(endTimeStr\)\)\}/,
        '${escapeHtml(String(endTimeStr))} wajib ada di kartu identitas detail (Waktu Kumpul)');

    // Guard ulang atas blok nyata: sliceBlock THROW (bukan no-op) bila anchor drift.
    const detailBlock = sliceBlock(HASIL_HTML, 'detail-identity-item', 'kartu identitas detail');
    assert.ok(detailBlock.length > 200,
        `blok kartu identitas = ${detailBlock.length} char — harus blok nyata, bukan trivial`);
    assert.doesNotMatch(detailBlock, /\$\{startTimeStr\}|\$\{endTimeStr\}/,
        'interpolasi mentah ${startTimeStr}/${endTimeStr} dilarang di kartu identitas — wajib escapeHtml(String(...))');
});

// ════════════════════════════════════════════════════════════════════════
// R105 — regex counter guard case-insensitive (RGBA / INLINE-HANDLER / Z-INDEX)
// ════════════════════════════════════════════════════════════════════════

/** Ekstrak literal regex `const NAME = /body/flags;` dari sumber suite. */
function extractRegexDecl(src, name) {
    const m = src.match(new RegExp('const\\s+' + name + '\\s*=\\s*\\/(.+?)\\/([a-z]*);'));
    assert.ok(m, `deklarasi const ${name} harus ditemukan di suite pemiliknya`);
    return { body: m[1], flags: m[2] };
}

test('R105 (unit): RGBA_RE batch7-tokens & batch11-settings-guard case-insensitive', () => {
    for (const [label, src] of [['batch7-tokens', B7_TOKENS], ['batch11-settings-guard', B11_SETTINGS]]) {
        const { body, flags } = extractRegexDecl(src, 'RGBA_RE');
        assert.ok(flags.includes('i'),
            `RGBA_RE ${label} wajib flag /i — tanpa itu "RGBA(255,0,0,0.5)" kapital lolos counter`);
        const re = new RegExp(body, flags);
        assert.equal(('RGBA(255,0,0,0.5)'.match(re) || []).length, 1,
            `RGBA_RE ${label} harus menghitung bentuk KAPITAL sebagai literal`);
        assert.equal(('rgba(255,255,255,0.1)'.match(re) || []).length, 1,
            `perilaku lowercase RGBA_RE ${label} tidak boleh berubah`);
    }
});

test('R105 (unit): INLINE_HANDLER_RE batch11-settings-guard case-insensitive', () => {
    const { body, flags } = extractRegexDecl(B11_SETTINGS, 'INLINE_HANDLER_RE');
    assert.ok(flags.includes('i'),
        'INLINE_HANDLER_RE wajib flag /i — " ONCLICK=" kapital lolos guard CSP 0 handler inline');
    const re = new RegExp(body, flags);
    assert.equal(('<DIV ONCLICK="steal()">x</DIV>'.match(re) || []).length, 1,
        'handler inline KAPITAL harus terhitung');
    assert.equal(('<a href="#" onclick="f()">y</a>'.match(re) || []).length, 1,
        'perilaku lowercase INLINE_HANDLER_RE tidak boleh berubah');
});

test('R105 (unit): regex z-index batch14-tokens-guard case-insensitive', () => {
    const decls = [...B14_TOKENS_GUARD.matchAll(/\/(z-index:\\s\*\[1-9\]\\d\{3,\})\/([a-z]*)/g)];
    assert.ok(decls.length >= 2, 'dua pemakaian regex z-index (HTML + JS) harus ada di suite tokens-guard');
    for (const [, body, flags] of decls) {
        assert.ok(flags.includes('i'),
            `regex z-index wajib flag /i — "Z-INDEX: 999" kapital lolos guard kelas berbahaya (flags: "${flags}")`);
        const re = new RegExp(body, flags);
        assert.equal(('Z-INDEX: 9999'.match(re) || []).length, 1,
            'z-index KAPITAL ≥1000 harus terdeteksi');
        assert.equal(('z-index: 1000'.match(re) || []).length, 1,
            'perilaku lowercase regex z-index tidak boleh berubah');
    }
});

// ════════════════════════════════════════════════════════════════════════
// S99(a) — BASELINES batch9-tokens-guard bolong: pengawas-detail.js &
//          device-fingerprint.js tanpa entri
// ════════════════════════════════════════════════════════════════════════

test('S99a (statik): pengawas-detail.js & device-fingerprint.js punya entri BASELINES batch9 == aktual', () => {
    for (const f of ['pengawas-detail.js', 'device-fingerprint.js']) {
        assert.match(B9_TOKENS_GUARD,
            new RegExp(`'${f.replace('.', '\\.')}'\\s*:\\s*\\{\\s*rgba:\\s*\\d+,\\s*hex:\\s*\\d+`),
            `${f} tanpa entri BASELINES — modul di luar daftar berkembang tanpa alarm (polanya sama dengan S88)`);
        // Kontrak plafon = aktual: nilai terdeklarasi HARUS sama dengan ukuran langsung.
        const declaredRgba = Number(B9_TOKENS_GUARD.match(
            new RegExp(`'${f.replace('.', '\\.')}'\\s*:\\s*\\{\\s*rgba:\\s*(\\d+)`))?.[1] ?? NaN);
        const declaredHex = Number(B9_TOKENS_GUARD.match(
            new RegExp(`'${f.replace('.', '\\.')}'\\s*:\\s*\\{[^}]*hex:\\s*(\\d+)`))?.[1] ?? NaN);
        const src = readFileSync(join(JS_DIR, f), 'utf8');
        const actualRgba = (src.match(/rgba\(\s*[0-9]/gi) || []).length;
        const actualHex = (src.match(/#[0-9a-fA-F]{3,8}\b/g) || []).length;
        assert.equal(actualRgba, declaredRgba,
            `rgba ${f} aktual ${actualRgba}, baseline ${declaredRgba} — kontrak plafon = angka terukur`);
        assert.equal(actualHex, declaredHex,
            `hex ${f} aktual ${actualHex}, baseline ${declaredHex}`);
    }
});

// ════════════════════════════════════════════════════════════════════════
// S99(b) — plafon hex+rgba CSS layer publik (di luar radar batch7 yang hanya
//          mengawasi admin-base.css). Dikunci AKTUAL hasil ukur hari ini;
//          TARGET JANGKA PANJANG MENURUN saat literal bermigrasi ke token —
//          turunkan angka ini (jangan naikkan) setiap kali migrasi berjalan.
// ════════════════════════════════════════════════════════════════════════

const PUBLIC_CSS_BASELINES = {
    // hex 3 (duplikat --color-bg-secondary :13,:76–79), rgba 5 — hasil ukur Batch 15.
    'public-desktop.css': { hex: 3, rgba: 5 },
    // hex 10 (#111827 dsb. :36,:78), rgba 2 — hasil ukur Batch 15.
    'public-mobile.css': { hex: 9, rgba: 2 }, /* Batch 16/R125: #9ca3af → var(--color-text-muted) */
};

for (const [file, caps] of Object.entries(PUBLIC_CSS_BASELINES)) {
    test(`S99b (guard): plafon literal hex+rgba css/${file} dikunci angka aktual`, () => {
        const src = readFileSync(join(CSS_DIR, file), 'utf8');
        const hex = (src.match(HEX_RE) || []).length;
        const rgba = (src.match(RGBA_RE) || []).length;
        assert.equal(hex, caps.hex,
            `hex ${file} = ${hex}, plafon terkunci ${caps.hex} — pakai var(--token); target jangka panjang menurun saat migrasi`);
        assert.equal(rgba, caps.rgba,
            `rgba ${file} = ${rgba}, plafon terkunci ${caps.rgba} — pakai rgba(var(--rgb-*), α); target jangka panjang menurun saat migrasi`);
    });
}

// ════════════════════════════════════════════════════════════════════════
// S95 — plafon = aktual (kontrak ronde 5): validasi independen seluruh
//       entri RGBA_BASELINE_PER_FILE batch7 & BASELINE_HEX batch8.
//       Migrasi agen lain (S80–S83 dst.) menurunkan literal; plafon yang
//       tidak ikut turun = slack hantu yang membolehkan erosi baru.
// ════════════════════════════════════════════════════════════════════════

/** Ambil isi blok `const NAME = { … };` dari sumber suite. */
function extractObjectBlock(src, name) {
    const start = src.indexOf(`const ${name} = {`);
    assert.notEqual(start, -1, `const ${name} harus ada`);
    const end = src.indexOf('};', start);
    assert.notEqual(end, -1, `blok ${name} harus tertutup };`);
    return src.slice(start, end);
}

test('S95 (statik): SEMUA entri RGBA_BASELINE_PER_FILE batch7-tokens == hasil ukur aktual', () => {
    const block = extractObjectBlock(B7_TOKENS, 'RGBA_BASELINE_PER_FILE');
    const entries = [...block.matchAll(/'([^']+)':\s*(\d+)/g)];
    assert.ok(entries.length >= 5, `minimal 5 entri baseline terbaca (dapat ${entries.length})`);
    for (const [, rel, capStr] of entries) {
        const declared = Number(capStr);
        const src = readFileSync(join(TPL_DIR, rel), 'utf8');
        const actual = (src.match(/rgba\(\s*[0-9]/gi) || []).length;
        assert.equal(actual, declared,
            `rgba literal templates/${rel}: aktual ${actual} vs plafon terdeklarasi ${declared} — ` +
            'kontrak ronde 5 "plafon = aktual": TURUNKAN plafon saat migrasi mengurangi literal, ' +
            'JANGAN biarkan slack hantu membuka pintu erosi token');
    }
});

/** Replikasi stripWhitelisted batch8 (shared.html: definisi token lokal & meta theme-color). */
const stripB8Shared = (src) => src
    .replace(/--bg-secondary:\s*#111827/g, '')
    .replace(/--accent-primary:\s*#6366f1/g, '')
    .replace(/--accent-secondary:\s*#8b5cf6/g, '')
    .replace(/<meta\s+name="theme-color"[^>]*>/g, '');

test('S95 (statik): SEMUA entri BASELINE_HEX batch8-publik == hasil ukur aktual', () => {
    const block = extractObjectBlock(B8_PUBLIK, 'BASELINE_HEX');
    const entries = [...block.matchAll(/'([^']+)':\s*(\d+)/g)];
    assert.ok(entries.length >= 6, `minimal 6 entri baseline terbaca (dapat ${entries.length})`);
    for (const [, name, capStr] of entries) {
        const declared = Number(capStr);
        let src = readFileSync(join(TPL_DIR, 'public', name), 'utf8');
        if (name === 'shared.html') src = stripB8Shared(src);
        const actual = (src.match(HEX_RE) || []).length;
        assert.equal(actual, declared,
            `hex templates/public/${name}: aktual ${actual} vs plafon terdeklarasi ${declared} — ` +
            'kontrak ronde 5 "plafon = aktual": turunkan plafon saat migrasi, jangan simpan slack hantu');
    }
});

// ════════════════════════════════════════════════════════════════════════
// R118 — FILES guard batch8 mencakup cek_hasil/index/forgot_password
// ════════════════════════════════════════════════════════════════════════

test('R118 (statik): FILES guard batch8-publik mencakup cek_hasil/index/forgot_password', () => {
    for (const f of ['cek_hasil.html', 'index.html', 'forgot_password.html']) {
        assert.ok(B8_PUBLIK.includes(f),
            `${f} belum masuk FILES uiux-batch8-publik.test.mjs — halaman publik di luar radar tumbuh literal tanpa alarm`);
    }
});
