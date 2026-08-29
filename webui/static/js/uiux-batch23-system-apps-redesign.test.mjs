/**
 * ══════════════════════════════════════════════════════════════════════════
 * Suite UI/UX BATCH 23 — SYSTEM-APPS REDESIGN (berantakan → bersih)
 * ══════════════════════════════════════════════════════════════════════════
 *
 * Latar belakang
 * ─────────────
 * Laporan: "kenapa malah semakin berantakan" setelah 3 patch berturut
 * (history auto-load Batch 22, ID case-mismatch 22.1, header tunggal 21).
 * Review mendalam 30 Aug 2026 menemukan:
 *   - HTML `section-system-apps` memakai `premium-card`, `platform-icon`,
 *     `btn-premium`, `header-icon-wrap`, `alert-premium`, dll. dengan
 *     ±40 inline `style="..."` per kartu + CSS `settings.html:478-730`
 *     (±250 baris premium) duplikat `admin-base.css` (glass-card, btn-*).
 *   - JS `renderAppsGrid` membangun kartu via `el('div', 'styleText')`
 *     dengan string style panjang — sulit maintain, tidak responsif konsisten,
 *     dan case-mismatch `app.Platform` vs `platform` baru diperbaiki.
 *   - Modal upload masih `modal-glass` custom, bukan `glass-card` standar.
 *   - History auto-load sudah benar (Batch 22) tapi desain visual tetap
 *     “premium” berat, tidak selaras 4 tab settings lain (users/billing/
 *     vouchers/general) yang pakai `glass-card` + `btn-primary` minimal.
 *
 * Tujuan redesign
 * ─────────────
 *   Satu sumber kebenaran: `glass-card` + `admin-base.css` (tanpa premium).
 *   Kartu aplikasi = `glass-card` ringkas: icon 40px, nama, badge versi,
 *   meta ukuran/tanggal, aksi Unduh/Hapus pakai `btn` standar. Header
 *   seksion pakai pola `users` (judul + deskripsi + btn-primary). Modal
 *   pakai `modal-card glass-card` standar. JS render tanpa inline style
 *   panjang — class-based.
 *
 * Kontrak batch ini (test-first)
 * ───────────────────────────────
 *   R23-1 — `settings.html#section-system-apps` bebas kelas premium
 *           (`premium-card`, `premium-header-title`, `header-icon-wrap`,
 *           `btn-premium`, `alert-premium`, `platform-icon` custom) dan
 *           bebas inline `style` berat (>15 kemunculan `style="` di section).
 *   R23-2 — Kartu dirender via `glass-card` + class utilitas, bukan
 *           `premium-card` + `styleText` panjang. JS `renderAppsGrid` tidak
 *           memakai `el('div','styleText')` dengan string premium.
 *   R23-3 — Modal upload pakai `modal-card` standar (bukan `modal-glass`
 *           custom) dan tombol `btn-primary`/`btn-secondary`.
 *   R23-4 — History tetap auto-load (Batch 22) dan ID normalisasi tetap
 *           (Batch 22.1) — tidak regresi.
 *
 * Kepemilikan: templates/admin/settings.html, static/js/settings-system-apps.js
 * Run: node --test static/js/uiux-batch23-system-apps-redesign.test.mjs
 */

import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const HERE = path.dirname(fileURLToPath(import.meta.url));
const WEBUI_ROOT = path.join(HERE, '..', '..');
const read = (...p) => fs.readFileSync(path.join(...p), 'utf8');

const SETTINGS_HTML = read(WEBUI_ROOT, 'templates', 'admin', 'settings.html');
const APPS_JS = read(WEBUI_ROOT, 'static', 'js', 'settings-system-apps.js');

function sectionSystemApps() {
  const start = SETTINGS_HTML.indexOf('id="section-system-apps"');
  assert.notEqual(start, -1, 'section-system-apps tidak ditemukan');
  const end = SETTINGS_HTML.indexOf('</section>', start + 5000);
  const slice = SETTINGS_HTML.slice(start, end === -1 ? start + 20000 : end + 10);
  return slice;
}

test('R23-1 (statik): section-system-apps bebas kelas premium & inline style berlebihan', () => {
  const sec = sectionSystemApps();
  const banned = ['premium-card', 'premium-header-title', 'header-icon-wrap', 'btn-premium', 'alert-premium', 'platform-icon platform-'];
  for (const cls of banned) {
    assert.doesNotMatch(sec, new RegExp(cls), `section masih pakai ${cls} — ganti glass-card standar`);
  }
  const inlineCount = (sec.match(/style="/g) || []).length;
  assert.ok(inlineCount <= 15, `inline style di section ${inlineCount} >15 — ekstrak ke class`);
});

test('R23-2 (statik): renderAppsGrid pakai glass-card, bukan premium-card + styleText panjang', () => {
  assert.doesNotMatch(APPS_JS, /premium-card/, 'JS masih pakai premium-card — ganti glass-card');
  assert.doesNotMatch(APPS_JS, /platform-icon platform-.*\+.*p/, 'JS masih pakai platform-icon premium — ganti icon standar');
  const longStyle = APPS_JS.match(/el\('div',\s*'[a-z-].*?:.*?:.*?:/g) || [];
  assert.ok(longStyle.length <= 2, `el('div','styleText') panjang ${longStyle.length} >2 — pakai class`);
  assert.match(APPS_JS, /glass-card/, 'JS harus pakai glass-card');
});

test('R23-3 (statik): modal upload pakai modal-card glass-card standar', () => {
  const sec = sectionSystemApps();
  assert.doesNotMatch(sec, /modal-glass/, 'modal masih pakai modal-glass custom — pakai modal-card glass-card');
  assert.match(sec, /modal-card/, 'modal harus pakai modal-card');
  assert.match(sec, /btn-primary|btn-secondary/, 'tombol modal harus pakai btn standar');
});

test('R23-4 (statik): history auto-load & ID normalisasi tetap (tidak regresi Batch 22)', () => {
  assert.match(APPS_JS, /window\.__settingsReady\[.system-apps.\]/, '__settingsReady harus ada');
  assert.match(APPS_JS, /loadApps\(\)/, 'loadApps harus dipanggil');
  assert.match(APPS_JS, /window\.loadSystemApps/, 'expose global harus ada');
  assert.match(APPS_JS, /app\.platform\s*\|\|\s*app\.Platform/, 'normalisasi platform harus ada (ID fix)');
  assert.match(APPS_JS, /cid.*app\.id.*app\.ID/, 'normalisasi ID harus ada');
});
