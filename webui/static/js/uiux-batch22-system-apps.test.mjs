/**
 * ══════════════════════════════════════════════════════════════════════════
 * Suite UI/UX BATCH 22 — SYSTEM-APPS HISTORY AUTO-LOAD
 * ══════════════════════════════════════════════════════════════════════════
 *
 * Latar belakang
 * ─────────────
 * Laporan: "halaman settings#system-apps ketika riwayat aplikasi yang
 * berhasil di upload baru nampak ketika kita klik unggah aplikasi button"
 *
 * Akar masalah (review mendalam 29 Aug 2026):
 *   - Grid riwayat `.apps-grid` di `section-system-apps` dirender server
 *     via `{{ range .apps }}` (SettingsPage → GetAllSystemApps). Namun
 *     tab system-apps dimuat lazy (`loadSectionScript('system-apps')`) dan
 *     `__settingsReady['system-apps']` sebelumnya HANYA menampilkan toast
 *     `?uploaded=1`, TIDAK memanggil `loadApps()` untuk refresh via API
 *     `/admin/api/system-apps`. Akibatnya history tampak kosong/stale
 *     sampai interaksi berikutnya yang kebetulan memicu `loadApps()` —
 *     yakni setelah `submitUpload` sukses (upload pertama) atau setelah
 *     klik "Unggah Aplikasi Baru" yang memuat modul (side-effect).
 *   - Aktivasi tab berikutnya (`activate('system-apps')` saat hashchange/
 *     klik tab) tidak memanggil refresh sama sekali karena `inited[key]`
 *     sudah true → `__settingsReady` tidak dipanggil lagi. Grid tetap
 *     stale jika ada unggahan dari sesi lain / setelah reload.
 *
 * Dampak bisnis
 * ─────────────
 *   SuperAdmin tidak melihat daftar distribusi APK/EXE yang sebenarnya
 *   setelah membuka tab — harus menebak dengan membuka modal unggah.
 *   Risiko: unggahan ganda (duplikat versi) karena riwayat tampak kosong,
 *   padahal sudah ada.
 *
 * Kontrak batch ini (test-first, guard statik + vm ringan)
 * ───────────────────────────────────────────────────────────
 *   S22-1 — `settings-system-apps.js` → `__settingsReady['system-apps']`
 *           WAJIB memanggil `loadApps()` (refresh in-place via API) setiap
 *           kali tab siap, selain toast `?uploaded=1`. Tanpa ini history
 *           hanya mengandalkan render server yang bisa stale.
 *   S22-2 — `settings-system-apps.js` mengekspos `loadApps` secara global
 *           (`window.loadSystemApps` atau `window.loadApps`) agar `activate`
 *           di `settings.html` bisa memicu refresh pada aktivasi berikutnya
 *           tanpa menunggu reload modul.
 *   S22-3 — `templates/admin/settings.html` → `activate()` untuk
 *           `key === 'system-apps'` WAJIB memanggil refresh (`loadApps`/
 *           `__settingsReady['system-apps']`) BAIK saat `inited` pertama
 *           maupun saat tab diaktifkan kembali (hashchange/klik). Tanpa ini
 *           riwayat tidak pernah refresh setelah unggahan di tab lain.
 *   S22-4 — `loadApps` tetap memakai guard `appLoadSeq` (S92) dan
 *           `renderAppsGrid` textContent-aman (anti-XSS) — tidak regresi.
 *
 * Kepemilikan file:
 *   static/js/settings-system-apps.js,
 *   templates/admin/settings.html,
 *   static/js/uiux-batch22-system-apps.test.mjs (BARU)
 *
 * Metode: guard statik fs-read + vm ringan untuk __settingsReady.
 * Run with: node --test static/js/uiux-batch22-system-apps.test.mjs (from webui/)
 */

import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';

const HERE = path.dirname(fileURLToPath(import.meta.url));
const WEBUI_ROOT = path.join(HERE, '..', '..');
const read = (...p) => fs.readFileSync(path.join(...p), 'utf8');

const APPS_JS = read(WEBUI_ROOT, 'static', 'js', 'settings-system-apps.js');
const SETTINGS_HTML = read(WEBUI_ROOT, 'templates', 'admin', 'settings.html');

function stripGo(src) {
  return src.replace(/\{\{[\s\S]*?\}\}/g, '');
}

// ── S22-1: __settingsReady['system-apps'] wajib memanggil loadApps ──────
test('S22-1 (statik): __settingsReady[system-apps] memanggil loadApps (refresh riwayat)', () => {
  const m = APPS_JS.match(/window\.__settingsReady\[.system-apps.\]\s*=\s*function\s*\(\)\s*\{([\s\S]*?)^\};/m);
  assert.ok(m, "__settingsReady['system-apps'] tidak ditemukan di settings-system-apps.js");
  const body = m[1];
  assert.match(body, /loadApps\s*\(\)/, "__settingsReady['system-apps'] wajib memanggil loadApps() agar riwayat tampil tanpa klik Unggah");
});

// ── S22-2: loadApps diekspos global ─────────────────────────────────────
test('S22-2 (statik): loadApps diekspos global untuk dipanggil activate', () => {
  const hasGlobal = /window\.loadSystemApps\s*=\s*loadApps|window\.loadApps\s*=\s*loadApps|window\.__settingsReady/.test(APPS_JS) && /loadApps/.test(APPS_JS);
  const explicit = APPS_JS.includes('window.loadSystemApps') || APPS_JS.includes('window.loadApps');
  assert.ok(explicit, "settings-system-apps.js harus mengekspos loadApps global (window.loadSystemApps = loadApps atau window.loadApps) agar activate bisa refresh di kunjungan berikutnya");
});

// ── S22-3: activate('system-apps') memicu refresh tiap aktivasi ──────────
test('S22-3 (statik): activate di settings.html memicu refresh system-apps tiap aktivasi', () => {
  const html = SETTINGS_HTML;
  const activateStart = html.indexOf('function activate(');
  assert.notEqual(activateStart, -1, 'function activate tidak ditemukan');
  const activateSlice = html.slice(activateStart, activateStart + 5000);
  const hasSystemAppsBranch = /system-apps/.test(activateSlice) && /loadApps|__settingsReady\[.system-apps.\]|loadSystemApps/.test(activateSlice);
  assert.ok(hasSystemAppsBranch, "activate() harus punya cabang khusus system-apps yang memanggil loadApps/__settingsReady tiap tab aktif (bukan hanya inited pertama)");
  const hasElseRefresh = activateSlice.includes('__settingsReady') || activateSlice.includes('loadSystemApps') || activateSlice.includes('loadApps');
  assert.ok(hasElseRefresh, "activate else-branch (inited true) harus tetap refresh system-apps");
});

// ── S22-4: guard appLoadSeq & render aman ─────────────────────────────────
test('S22-4 (statik): loadApps tetap pakai guard appLoadSeq (S92) dan render textContent', () => {
  assert.match(APPS_JS, /var\s+appLoadSeq\s*=\s*0/, 'guard appLoadSeq harus ada');
  assert.match(APPS_JS, /const\s+seq\s*=\s*\+\+appLoadSeq/, 'seq increment harus ada');
  assert.match(APPS_JS, /if\s*\(seq\s*!==\s*appLoadSeq\)\s*return/, 'early return stale response harus ada');
  assert.match(APPS_JS, /title\.textContent\s*=\s*app\.Name/, 'render harus pakai textContent (anti-XSS) bukan innerHTML untuk nama');
});

// ── S22 vm: __settingsReady benar-benar memanggil loadApps ────────────────
test('S22 vm: __settingsReady system-apps memanggil loadApps', () => {
  let loadCalled = 0;
  const sandbox = {
    window: { location: { search: '' }, history: { replaceState() {} } },
    document: { querySelector() { return null; } },
    URLSearchParams,
    showToast() {},
    apiFetch: async () => ({ ok: true, json: async () => ({ success: true, apps: [] }) }),
    loadApps() { loadCalled++; },
  };
  sandbox.window.__settingsReady = {};
  const src = APPS_JS.match(/window\.__settingsReady\[.system-apps.\]\s*=\s*function[\s\S]*?^};/m);
  assert.ok(src, 'snippet __settingsReady tidak ditemukan');
  let code = src[0].replace('window.__settingsReady', 'window.__settingsReady');
  code = code.replace(/loadApps\(\)/g, 'loadApps()');
  const ctx = vm.createContext(sandbox);
  vm.runInContext(code, ctx);
  assert.ok(typeof sandbox.window.__settingsReady['system-apps'] === 'function');
  sandbox.window.__settingsReady['system-apps']();
  assert.equal(loadCalled, 1, '__settingsReady harus memanggil loadApps sekali');
});
