/* Contract tests untuk Batch 23 — M13: guard double-submit di cek_hasil.html
 * (review_web_flow_dan_dead_code.md, Bagian 5.3 M13 / 6.5 #13).
 *
 * M13: tombol submit cek hasil tidak di-disable saat request berjalan,
 * tidak seperti register_confirm (:366-368) dan reset_password (:339) yang
 * punya guard — pola yang sama, satu halaman tertinggal. Dampak: klik ganda
 * / submit ulang GET dengan token yang sama dibuka dua kali.
 *
 * Kontrak yang dikunci (sama dengan dua halaman OTP):
 *   - listener submit pada #cekHasilForm men-disable tombol + state loading;
 *   - guard re-entry: form yang sudah terkirim tidak memicu navigasi kedua
 *     (disabled submit button mencegah submit via Enter/klik berikutnya);
 *   - pulih saat kembali ke halaman via bfcache (pageshow persisted) — GET
 *     bisa gagal (token salah) sehingga pengguna harus bisa mencoba lagi;
 *   - normalisasi token uppercase tetap jalan SEBELUM guard (urutan listener:
 *     normalisasi dulu terpasang, guard menonaktifkan setelahnya).
 *
 * Metode: guard statik fs-read + eksekusi perilaku via vm.runInNewContext
 * (pola uiux-batch15-publik / uiux-batch22-system-apps).
 *
 * Run with: node --test static/js/uiux-batch23-cekhasil-guard.test.mjs (from webui/)
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

const CEK_HASIL = read(WEBUI_ROOT, 'templates', 'public', 'cek_hasil.html');

/** Ambil blok <script> inline TERAKHIR (script logika halaman). */
function lastInlineScript(html) {
    const openers = [...html.matchAll(/<script(?![^>]*\bsrc=)[^>]*>/g)];
    assert.ok(openers.length > 0, 'template harus punya minimal satu script inline');
    const open = html.indexOf('>', openers[openers.length - 1].index) + 1;
    const close = html.indexOf('</script>', open);
    return html.slice(open, close);
}

// ── Statik: pola guard ada di sumber ────────────────────────────────────────

test('M13 (statik): listener submit cek_hasil men-disable tombol + state loading', () => {
    const script = lastInlineScript(CEK_HASIL);
    assert.match(script, /addEventListener\(\s*'submit'/,
        'listener submit wajib ada di inline script cek_hasil.html');
    assert.match(script, /\.disabled\s*=\s*true/,
        'guard double-submit wajib men-disable tombol (pola register_confirm :366-368 / reset_password :339)');
    assert.match(script, /classList\.add\(\s*'loading'\s*\)/,
        'state loading wajib ditandai (pola reset_password)');
});

test('M13 (statik): cek_hasil pulih dari bfcache via pageshow persisted', () => {
    const script = lastInlineScript(CEK_HASIL);
    assert.match(script, /addEventListener\(\s*'pageshow'/,
        'pageshow listener wajib ada: GET bisa gagal (token salah) dan browser kembali via bfcache dengan tombol masih disabled — pengguna harus bisa mencoba lagi');
    assert.match(script, /persisted/,
        'pageshow wajib ber-guard pada event.persisted (restorasi bfcache)');
});

// ── Perilaku: vm ringan atas inline script ─────────────────────────────────

function runCekHasilScript() {
    const script = lastInlineScript(CEK_HASIL);

    const listeners = { form: {}, input: {}, window: {} };
    const makeTarget = (store) => ({
        addEventListener(type, fn) { (store[type] = store[type] || []).push(fn); },
    });	const btn = {
		disabled: false,
		textContent: 'Lihat Hasil',
		classList: {
			set: new Set(),
			add(c) { this.set.add(c); },
			remove(c) { this.set.delete(c); },
			contains(c) { return this.set.has(c); },
		},
	};
	const input = makeTarget(listeners.input);
	input.value = 'ab12cd34';
    const form = makeTarget(listeners.form);	const sandbox = {
		document: {
			addEventListener(type, fn) { (listeners.window[type] = listeners.window[type] || []).push(fn); },
			getElementById(id) {
				if (id === 'cekHasilForm') return form;
				if (id === 'token') return input;
				if (id === 'cekHasilSubmitBtn') return btn;
				return null;
			},
		},
		window: {
			addEventListener(type, fn) { (listeners.window[type] = listeners.window[type] || []).push(fn); },
		},
	};	vm.createContext(sandbox);
	vm.runInContext(script, sandbox);

	// Semua wiring hidup DI DALAM callback DOMContentLoaded — jalankan ia
	// (pola nyata browser: event menyusul parse script).
	const domReady = (listeners.window.DOMContentLoaded || [])[0];
	if (domReady) domReady.call(sandbox.document);

	return { listeners, btn, input };
}

test('M13 (perilaku): submit pertama menormalisasi token lalu men-disable tombol', () => {
    const { listeners, btn, input } = runCekHasilScript();	const submit = (listeners.form.submit || [])[0];
	assert.ok(submit, 'listener submit harus terpasang pada #cekHasilForm');
	const inputListener = (listeners.input.input || [])[0];
	assert.ok(inputListener, 'listener input normalisasi tetap terpasang');

    // Normalisasi input (S-lama) tetap berjalan.
    inputListener.call(input);
    assert.equal(input.value, 'AB12CD34', 'token dinormalisasi ke uppercase');	// Submit: normalisasi ulang + guard aktif.
	input.value = 'ab12cd34';
	submit.call(listeners.form, {});
    assert.equal(input.value, 'AB12CD34', 'submit menormalisasi token sebelum dikirim');
    assert.equal(btn.disabled, true, 'tombol disabled setelah submit (M13)');
    assert.ok(btn.classList.contains('loading'), 'tombol ber-state loading (M13)');
});

test('M13 (perilaku): pageshow persisted memulihkan tombol agar bisa coba lagi', () => {
    const { listeners, btn } = runCekHasilScript();	const submit = (listeners.form.submit || [])[0];
	submit.call(listeners.form, {});
	assert.equal(btn.disabled, true, 'pra-syarat: tombol disabled setelah submit');

    const pageshow = (listeners.window.pageshow || [])[0];
    assert.ok(pageshow, 'listener pageshow harus terpasang di window/document');

    // Kembali dari bfcache → tombol harus aktif lagi.
    pageshow.call(null, { persisted: true });
    assert.equal(btn.disabled, false, 'bfcache restore wajib memulihkan tombol (GET token salah harus bisa diulang)');
    assert.equal(btn.textContent, 'Lihat Hasil', 'label tombol dipulihkan');

    // Navigasi normal (bukan bfcache) tidak mengubah state.
    submit.call(listeners.form, {});
    pageshow.call(null, { persisted: false });
    assert.equal(btn.disabled, true, 'tanpa bfcache, state tidak direset');
});
