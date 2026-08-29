/**
 * ══════════════════════════════════════════════════════════════════════════
 * Suite UI/UX BATCH 18 — HEADER PUBLIK TUNGGAL
 * ══════════════════════════════════════════════════════════════════════════
 *
 * Latar belakang
 * ─────────────
 * Review mendalam (29 Aug 2026) menemukan header halaman publik terpecah
 * menjadi 3 implementasi yang tidak sinkron:
 *
 *   A. Landing & Unduh  → memakai `public_head` (shared.html) yang memancarkan
 *      <nav> lengkap + inline <style> nav (210-299) + glow-blob + public_nav.
 *   B. Auth (cek_hasil, register, register_confirm, forgot_password,
 *      reset_password, login) → standalone <!DOCTYPE> + `public_auth_nav`
 *      (alias `public_nav`) + `public_skip_link`; styling HANYA dari
 *      public-mobile/desktop.css (tanpa inline head).
 *   C. Hasil (/hasil/:token) → standalone + header CUSTOM
 *      `<header class="header">` (hasil.html:104) + `hasil.css:61` baru —
 *      tidak memakai komponen nav publik sama sekali.
 *
 * Dampak bisnis / UX yang dilindungi batch ini
 * ─────────────────────────────────────────────
 *   H1 — Inkonsistensi visual: Grup A pakai nav fixed 72px blur 12px, Grup C
 *        pakai header relative blur 20px tinggi berbeda; logo/badge, warna
 *        border, dan spacing tidak identik. Pengguna pindah halaman merasa
 *        ganti aplikasi.
 *   H2 — Navigasi hilang di /hasil: tidak ada link Unduh/Cek Hasil/Daftar/
 *        Masuk, tidak ada hamburger/drawer di mobile — jalur navigasi mati
 *        total di hasil (temuan utama laporan).
 *   H3 — Duplikasi sumber kebenaran: style nav ada di inline shared.html DAN
 *        di public-mobile/desktop.css; auth pages hanya dapat yang luar,
 *        landing dapat keduanya → ukuran logo (38px vs 36px vs 40px), padding
 *        nav-container, dan background sedikit berbeda.
 *   H4 — Aksesibilitas & responsif terpecah: drawer (visibility:hidden,
 *        aria-expanded, Escape→fokus kembali) hanya hidup di komponen
 *        public_nav; header hasil custom tidak punya overlay/hamburger/JS.
 *
 * Kontrak batch ini (test-first)
 * ───────────────────────────────
 *   H1 — SEMUA template publik (index, download, cek_hasil, register,
 *        register_confirm, forgot_password, reset_password, hasil) WAJIB
 *        memanggil `{{ template "public_nav" . }}` atau
 *        `{{ template "public_auth_nav" . }}` — satu sumber navbar.
 *        Pengecualian: `shared.html` sendiri adalah tempat definisi, bukan
 *        konsumen, jadi tidak dihitung.
 *   H2 — `templates/public/hasil.html` TIDAK BOLEH lagi mengandung
 *        `<header class="header">` legacy. Harus memakai public_nav, serta
 *        memuat `public_fonts` + `public_skip_link` + CSS publik standar
 *        (theme.css + public-mobile/desktop.css) seperti halaman auth lain.
 *   H3 — `static/css/hasil.css` TIDAK BOLEH mendefinisikan header legacy
 *        yang bersaing (` .header`, `.header-left`, `.header-title`,
 *        `.header-badge`, `.header-logo`, `.header-divider` ). Token nav
 *        hidup di public-mobile/desktop.css; hasil.css hanya untuk konten
 *        tabel/detail/stats. Header legacy dihapus/dideprekasi.
 *   H4 — Komponen tunggal `public_nav` (shared.html) memuat tautan kanonik:
 *        `href="/download"` (Unduh Aplikasi), `href="/hasil"` (Cek Hasil),
 *        `href="/register"` (Daftar), `href="/login"`/`/admin/dashboard`
 *        (Masuk/Dashboard), serta elemen drawer `nav-hamburger`, `nav-overlay`,
 *        `nav-links` + logo E — kelengkapan ini diwarisi otomatis semua halaman.
 *   H5 — Kompensasi fixed-nav: setelah hasil memakai nav fixed, `.main`
 *        hasil wajib diberi offset `padding-top: calc(var(--public-nav-height)`
 *        agar konten tidak tertutup nav (paritas `.login-page` di
 *        public-mobile.css:144). Nilai dihitung dari token tinggi nav, bukan
 *        magic number.
 *
 * Kepemilikan file agen ini:
 *   templates/public/{shared,hasil,cek_hasil,register,register_confirm,
 *     forgot_password,reset_password,index,download}.html,
 *   templates/admin/login.html,
 *   static/css/{hasil,public-mobile,public-desktop,theme}.css,
 *   static/js/uiux-batch18-publik-header.test.mjs (BARU)
 *
 * Metode: guard statik fs-read (pola batch11/15/16) — tanpa eksekusi vm berat,
 * kecuali smoke kecil untuk nav (H4). Tidak menulis pola glob dua-bintang di
 * komentar blok ini.
 *
 * Run with:  node --test static/js/uiux-batch18-publik-header.test.mjs (from webui/)
 */

import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const HERE = path.dirname(fileURLToPath(import.meta.url));
const WEBUI_ROOT = path.join(HERE, '..', '..');
const PUBLIC = path.join(WEBUI_ROOT, 'templates', 'public');
const read = (...p) => fs.readFileSync(path.join(...p), 'utf8');

const SHARED = read(PUBLIC, 'shared.html');
const HASIL_HTML = read(PUBLIC, 'hasil.html');
const HASIL_CSS = read(WEBUI_ROOT, 'static', 'css', 'hasil.css');
const MOBILE_CSS = read(WEBUI_ROOT, 'static', 'css', 'public-mobile.css');
const DESKTOP_CSS = read(WEBUI_ROOT, 'static', 'css', 'public-desktop.css');

// Template publik konsumen (shared.html adalah definisi, tidak dihitung H1)
const PUBLIC_CONSUMERS = [
  ['index.html', read(PUBLIC, 'index.html')],
  ['download.html', read(PUBLIC, 'download.html')],
  ['cek_hasil.html', read(PUBLIC, 'cek_hasil.html')],
  ['register.html', read(PUBLIC, 'register.html')],
  ['register_confirm.html', read(PUBLIC, 'register_confirm.html')],
  ['forgot_password.html', read(PUBLIC, 'forgot_password.html')],
  ['reset_password.html', read(PUBLIC, 'reset_password.html')],
  ['hasil.html', HASIL_HTML],
];
const LOGIN_HTML = read(WEBUI_ROOT, 'templates', 'admin', 'login.html');

// ──────────────────────────────────────────────────────────────────────────
// H1 — satu sumber navbar
// ──────────────────────────────────────────────────────────────────────────

test('H1 (statik): SEMUA template publik konsumen memakai public_nav / public_auth_nav', () => {
  const direct = /\{\{\s*template\s+"public_(?:auth_)?nav"/;
  const viaHead = /\{\{\s*template\s+"public_head"/;
  for (const [name, html] of PUBLIC_CONSUMERS) {
    const ok = direct.test(html) || viaHead.test(html);
    assert.ok(ok, `${name} harus memanggil {{ template "public_nav" . }} / public_auth_nav atau public_head (yang memancarkan nav) — satu komponen header`);
  }
  assert.match(LOGIN_HTML, direct, 'admin/login.html juga memakai public_auth_nav (konsistensi auth)');
});

test('H1 (statik): SEMUA template publik konsumen memakai public_fonts + public_skip_link', () => {
  const viaHead = /\{\{\s*template\s+"public_head"/;
  for (const [name, html] of PUBLIC_CONSUMERS) {
    if (viaHead.test(html)) {
      assert.match(SHARED, /\{\{\s*template\s+"public_fonts"/, 'shared:public_head harus memancarkan public_fonts');
      assert.match(SHARED, /\{\{\s*template\s+"public_skip_link"/, 'shared:public_head harus memancarkan public_skip_link');
      continue;
    }
    assert.match(html, /\{\{\s*template\s+"public_fonts"/, `${name} harus memanggil public_fonts (Outfit + Plus Jakarta Sans)`);
    assert.match(html, /\{\{\s*template\s+"public_skip_link"/, `${name} harus memanggil public_skip_link (WCAG 2.4.1)`);
  }
});

test('H1 (statik): SEMUA template publik konsumen memuat CSS publik standar', () => {
  const viaHead = /\{\{\s*template\s+"public_head"/;
  for (const [name, html] of PUBLIC_CONSUMERS) {
    if (viaHead.test(html)) {
      assert.match(SHARED, /\/static\/css\/theme\.css/, 'shared:public_head harus memuat theme.css');
      assert.match(SHARED, /\/static\/css\/public-mobile\.css/, 'shared:public_head harus memuat public-mobile.css');
      assert.match(SHARED, /\/static\/css\/public-desktop\.css/, 'shared:public_head harus memuat public-desktop.css');
      continue;
    }
    assert.match(html, /\/static\/css\/theme\.css/, `${name} harus memuat theme.css`);
    assert.match(html, /\/static\/css\/public-mobile\.css/, `${name} harus memuat public-mobile.css (layer drawer + compensasi nav)`);
    assert.match(html, /\/static\/css\/public-desktop\.css/, `${name} harus memuat public-desktop.css`);
  }
});

// ──────────────────────────────────────────────────────────────────────────
// H2 — hasil.html lepas dari header custom
// ──────────────────────────────────────────────────────────────────────────

test('H2 (statik): hasil.html bebas <header class="header"> legacy — pakai public_nav', () => {
  assert.doesNotMatch(HASIL_HTML, /<header[^>]*class="[^"]*\bheader\b[^"]*"/,
    'hasil.html masih mengandung <header class="header"> custom — ganti dengan {{ template "public_nav" . }}');
  assert.doesNotMatch(HASIL_HTML, /class="header-left"/, 'sisa markup header-left custom harus bersih');
  assert.doesNotMatch(HASIL_HTML, /class="header-badge"/, 'sisa markup header-badge custom harus bersih');
  assert.match(HASIL_HTML, /\{\{\s*template\s+"public_(?:auth_)?nav"/,
    'hasil.html wajib memanggil public_nav / public_auth_nav seperti halaman publik lain');
});

test('H2 (statik): hasil.html tidak menduplikasi definisi header legacy inline', () => {
  const styleBlocks = [...HASIL_HTML.matchAll(/<style[^>]*>([\s\S]*?)<\/style>/g)].map(m => m[1]).join('\n');
  assert.doesNotMatch(styleBlocks, /\.header\s*\{/, 'blok <style> hasil.html tidak boleh mendefinisikan .header lagi');
});

// ──────────────────────────────────────────────────────────────────────────
// H3 — hasil.css bersih dari header legacy
// ──────────────────────────────────────────────────────────────────────────

test('H3 (statik): hasil.css bebas selector header legacy yang bersaing dengan nav publik', () => {
  const printIdx = HASIL_CSS.indexOf('@media print');
  const layoutCSS = printIdx === -1 ? HASIL_CSS : HASIL_CSS.slice(0, printIdx);
  const banned = ['.header', '.header-left', '.header-divider', '.header-logo', '.header-badge'];
  for (const sel of banned) {
    const re = new RegExp(sel.replace('.', '\\.') + '\\s*\\{');
    assert.doesNotMatch(layoutCSS, re,
      `hasil.css masih mendefinisikan ${sel} { di luar @media print — header publik tunggal hidup di public-mobile/desktop.css`);
  }
  assert.doesNotMatch(layoutCSS, /\.header-title\s*\{\s*[^}]*position:/,
    'hasil.css tidak boleh mendefinisikan header-title layout (print reset diizinkan)');
  assert.doesNotMatch(layoutCSS, /\.back-nav\s*\{[^}]*position:\s*relative/,
    'hasil.css tidak boleh menimpa .back-nav sebagai header legacy');
});

// ──────────────────────────────────────────────────────────────────────────
// H4 — kelengkapan komponen tunggal public_nav
// ──────────────────────────────────────────────────────────────────────────

test('H4 (statik): public_nav (shared.html) memuat tautan & elemen drawer kanonik', () => {
  const navDef = (() => {
    const start = SHARED.indexOf('{{ define "public_nav" }}');
    assert.notEqual(start, -1, 'definisi public_nav tidak ditemukan');
    const end = SHARED.indexOf('{{ end }}', start + 30);
    return SHARED.slice(start, end);
  })();
  assert.match(navDef, /href="\/download"/, 'public_nav harus punya link Unduh Aplikasi');
  assert.match(navDef, /href="\/hasil"/, 'public_nav harus punya link Cek Hasil Ujian');
  assert.match(navDef, /href="\/register"/, 'public_nav harus punya link Daftar');
  assert.match(navDef, /href="\/login"/, 'public_nav harus punya link Masuk');
  assert.match(navDef, /class="logo-container"/, 'public_nav harus punya logo-container');
  assert.match(navDef, /class="logo-badge"/, 'public_nav harus punya logo-badge (E)');
  assert.match(navDef, /id="navHamburger"/, 'public_nav harus punya #navHamburger (drawer mobile)');
  assert.match(navDef, /id="navLinks"/, 'public_nav harus punya #navLinks');
  assert.match(navDef, /id="navOverlay"/, 'public_nav harus punya #navOverlay');
  assert.match(navDef, /data-nav-toggle/, 'pemicu hamburger+overlay memakai data-nav-toggle (S59)');
});

test('H4 (statik): CSS publik mendefinisikan nav fixed + drawer (public-mobile/desktop)', () => {
  assert.match(MOBILE_CSS, /body\s*>\s*nav\s*\{[^}]*position:\s*fixed/, 'public-mobile.css: body > nav harus position:fixed');
  assert.match(MOBILE_CSS, /\.nav-links\s*\{[^}]*position:\s*fixed[^}]*right:\s*-100%/, 'drawer mobile .nav-links harus off-canvas right:-100%');
  assert.match(MOBILE_CSS, /\.nav-links\.open\s*\{[^}]*right:\s*0/, 'state .open harus right:0');
  assert.match(DESKTOP_CSS, /\.nav-links\s*\{[^}]*visibility:\s*visible/, 'desktop mereset visibility:visible (S97)');
});

// ──────────────────────────────────────────────────────────────────────────
// H5 — kompensasi fixed-nav untuk halaman hasil
// ──────────────────────────────────────────────────────────────────────────

test('H5 (statik): hasil.css memberi offset main terhadap nav fixed (bukan magic number lepas)', () => {
  assert.match(HASIL_CSS, /\.main\s*\{[^}]*padding-top:\s*calc\(var\(--public-nav-height\)/,
    '.main hasil harus punya padding-top: calc(var(--public-nav-height) + ...) agar tidak tertutup nav fixed (paritas .login-page)');
});

// ──────────────────────────────────────────────────────────────────────────
// H6 — anti-duplikasi: nav tunggal di external, inline shared.html dibersihkan
// ──────────────────────────────────────────────────────────────────────────

test('H6 (statik): public_head inline <style> bebas duplikat nav layout — nav hidup di public-mobile/desktop.css', () => {
  const headStart = SHARED.indexOf('{{ define "public_head" }}');
  assert.notEqual(headStart, -1, 'definisi public_head tidak ditemukan');
  const headEnd = SHARED.indexOf('{{ define "public_foot" }}', headStart);
  const headSlice = SHARED.slice(headStart, headEnd === -1 ? undefined : headEnd);
  const styleBlocks = [...headSlice.matchAll(/<style[^>]*>([\s\S]*?)<\/style>/g)].map(m => m[1]).join('\n');
  const bannedNav = [
    /(^|\n)\s*nav\s*\{[^}]*position:\s*fixed/,
    /\.nav-container\s*\{[^}]*max-width:\s*1200px[^}]*padding:\s*20px 24px/,
    /\.logo-badge\s*\{[^}]*width:\s*38px/,
    /\.logo-text\s*\{[^}]*font-size:\s*24px/,
    /\.nav-links\s*\{[^}]*gap:\s*32px/,
    /\.nav-link\s*\{[^}]*color:\s*var\(--color-text-secondary\)/,
    /\.btn-nav-login\s*\{[^}]*padding:\s*10px 22px/,
  ];
  for (const re of bannedNav) {
    assert.doesNotMatch(styleBlocks, re,
      `public_head inline masih menduplikasi nav layout (${re}) — hapus, nav tunggal di public-mobile/desktop.css agar landing=auth pixel-identik`);
  }
  assert.match(styleBlocks, /\.hero\s*\{/, 'landing hero styles tetap ada (hanya nav yang dihapus)');
  assert.match(MOBILE_CSS, /body\s*>\s*nav\s*\{/, 'external public-mobile.css tetap punya body > nav');
});
