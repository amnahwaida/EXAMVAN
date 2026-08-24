/**
 * ══════════════════════════════════════════════════════════════════════════
 * Suite UI/UX BATCH 14 — CORE (agen batch14-core)
 * ══════════════════════════════════════════════════════════════════════════
 *
 * Referensi: review_uiux_webui.md bagian 5.11 RE-REVIEW RONDE 8 (basis
 * b37f715, pasca Batch 13). Cakupan temuan milik agen ini:
 *
 *   T24 — Anchor `role="button"` TANPA href MATI untuk keyboard.
 *         Handler keydown delegasi di admin-core.js mengecualikan tag `A`
 *         secara membabi-buta ("native <a> already handle this"), padahal
 *         asumsi itu hanya benar untuk <a href>. Link "Detail" riwayat akses
 *         di pengawas_detail.html dirender sebagai <a role="button"
 *         tabindex="0"> tanpa href → bisa difokus tapi tidak bisa
 *         diaktifkan Enter/Space (WCAG 2.1.1).
 *         Kontrak perbaikan (salah satu / keduanya):
 *           (a) core hanya mengecualikan <a> yang PUNYA href;
 *           (b) render berubah menjadi <button type="button">.
 *
 *   S76 — showConfirm rentan CROSS-WIRING saat dua dialog bertumpuk:
 *         listener tombol dipasang via document.getElementById (ID statis!)
 *         sehingga dialog kedua ter-wire ke tombol dialog PERTAMA; satu klik
 *         OK me-resolve dua promise → aksi destruktif bisa terkirim ganda.
 *         Selain itu argumen confirmLabel/cancelLabel disisipkan mentah ke
 *         innerHTML padahal message sudah di-escape (kontrak escape
 *         setengah-jadi).
 *         Kontrak perbaikan:
 *           (a) listener memakai overlay.querySelector('#confirmOkBtn' /
 *               '#confirmCancelBtn') — referensi lokal per-dialog;
 *           (b) kedua label lewat escapeHtml seperti message/detailText.
 *
 *   S85 — aria-live="polite" pada stempel #lastUpdatedLabel membuat screen
 *         reader mengumumkan "Diperbarui HH:MM:SS" SETIAP tick polling
 *         (5 dtk antrean / 12 dtk peserta) — jam menenggelamkan pengumuman
 *         penting (perubahan antrean izin). Kontrak perbaikan:
 *           (a) lastUpdatedLabel jadi visual-only (bebas aria-live);
 *           (b) ada region live TERPISAH yang hanya mengumumkan PERUBAHAN
 *               jumlah antrean (id/markernya menyebut queue/antrean).
 *
 *   S86 — Byte NUL (0x00) literal tertanam di string join fingerprint
 *         pengawas-detail.js sejak Batch 3 — file terdeteksi BINER oleh
 *         `file`/grep/diff/proxy; charset serving rapuh. Kontrak: source
 *         bebas 0x00 sepenuhnya (pemisah diganti karakter teks biasa).
 *
 * Kepemilikan file agen ini: static/js/admin-core.js,
 *   static/js/pengawas-detail.js, templates/admin/pengawas_detail.html.
 *
 * Run with:  node --test static/js/uiux-batch14-core.test.mjs   (from webui/)
 */

import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';

const HERE = dirname(fileURLToPath(import.meta.url));
const WEBUI_ROOT = join(HERE, '..', '..');
const read = (...p) => readFileSync(join(WEBUI_ROOT, ...p), 'utf8');

const CORE = read('static', 'js', 'admin-core.js');
const DETAIL_HTML = read('templates', 'admin', 'pengawas_detail.html');

// escapeHtml ASLI dari core — dipakai test vm untuk meniru perilaku render.
function extractCoreFunction(name) {
    const re = new RegExp(`function ${name}\\([\\s\\S]*?\\n\\}`);
    const m = CORE.match(re);
    assert.ok(m, `function ${name}() ditemukan di admin-core.js`);
    return m[0];
}

// ════════════════════════════════════════════════════════════════════════
// T24 — anchor role-button tanpa href harus tetap bisa diaktifkan keyboard
// ════════════════════════════════════════════════════════════════════════

/** Ambil satu-satunya handler keydown delegasi global (yang menyaring Enter/Space). */
function extractGlobalKeydownHandler() {
    // Anchor pada komentar penanda (lebih stabil daripada regex lintas-handler:
    // ada beberapa document.addEventListener('keydown') di core).
    const marker = CORE.indexOf('Keyboard accessibility');
    assert.ok(marker !== -1, 'komentar penanda handler keyboard ada');
    const start = CORE.indexOf("document.addEventListener('keydown'", marker);
    assert.ok(start !== -1, 'handler keydown delegasi ada setelah penanda');
    const end = CORE.indexOf('\n});', start);
    assert.ok(end !== -1, 'badan handler tertutup');
    const src = CORE.slice(start, end + 4);
    assert.ok(src.includes("'Enter'"), 'handler yang terekstrak menyaring Enter');
    return src;
}

test('T24 (statik): core tidak lagi mengecualikan tag A secara membabi-buta dari aktivasi keyboard', () => {
    const src = extractGlobalKeydownHandler();
    // Kontrak: setiap pengecualian <a> wajib dikondisikan punya href.
    // Bentuk yang DILARANG: eksklusi tanpa syarat href, mis.
    //   if (tag === 'BUTTON' || tag === 'A' || ...) return;
    const lines = src.split('\n').filter((l) => l.includes("tag === 'A'"));
    assert.ok(lines.length >= 1, "penanganan khusus tag A ada di handler keydown");
    for (const line of lines) {
        assert.match(line, /hasAttribute\(\s*['"]href['"]\s*\)/,
            `eksklusi <a> tanpa syarat href membuat anchor role-button tanpa href mati untuk keyboard — kondisikan dengan el.hasAttribute('href'): ${line.trim()}`);
    }
});

test('T24 (statik): link Detail riwayat akses pengawas_detail tidak lagi <a> tanpa href ber-role button', () => {
    // Cari semua string render-JS ber-class action-link dengan role="button".
    const anchors = [...DETAIL_HTML.matchAll(/<a\b[^>]*role="button"[^>]*>/g)];
    for (const [tag] of anchors) {
        assert.match(tag, /href=/,
            `anchor ber-role="button" tanpa href tidak akan bisa diaktifkan Enter/Space (WCAG 2.1.1): "${tag.slice(0, 90)}…" — ubah ke <button type="button"> atau tambahkan handler core (lihat test statik pertama T24)`);
    }
});

test('T24 (vm): anchor role-button TANPA href diaktifkan Enter; anchor BER-href tetap dilewati core', () => {
    let registered = null;
    const sandbox = {
        document: {
            addEventListener(type, fn) {
                if (type === 'keydown') registered = fn;
            },
        },
    };
    sandbox.globalThis = sandbox;
    vm.createContext(sandbox);
    vm.runInContext(extractGlobalKeydownHandler(), sandbox, { filename: 'admin-core.js#keydown' });
    assert.equal(typeof registered, 'function', 'handler keydown teregistrasi');

    function fakeEl(tagName, { href = false } = {}) {
        const state = { clicked: 0, prevented: 0 };
        return {
            tagName,
            clicked: () => state.clicked,
            prevented: () => state.prevented,
            getAttribute: (name) => (name === 'role' ? 'button' : null),
            hasAttribute: (name) => (name === 'href' ? href : name === 'onkeydown' ? false : false),
            preventDefault() { state.prevented += 1; },
            click() { state.clicked += 1; },
        };
    }

    // 1) Anchor TANPA href (kasus link "Detail" pengawas_detail) — WAJIB aktif.
    const deadAnchor = fakeEl('A', { href: false });
    registered({ key: 'Enter', target: deadAnchor, preventDefault: deadAnchor.preventDefault });
    assert.equal(deadAnchor.clicked(), 1,
        'anchor role-button tanpa href harus di-click() oleh handler global saat Enter');
    assert.equal(deadAnchor.prevented(), 1, 'Enter pada kontrol custom harus preventDefault (hindari scroll)');

    // Space juga aktif untuk kasus yang sama.
    registered({ key: ' ', target: deadAnchor, preventDefault: deadAnchor.preventDefault });
    assert.equal(deadAnchor.clicked(), 2, 'Space juga mengaktifkan anchor role-button tanpa href');

    // 2) Anchor BER-href — perilaku native, core TIDAK boleh ikut campur.
    const nativeLink = fakeEl('A', { href: true });
    registered({ key: 'Enter', target: nativeLink, preventDefault: nativeLink.preventDefault });
    assert.equal(nativeLink.clicked(), 0, 'anchor ber-href punya navigasi native — tidak boleh di-double-activate');
});

// ════════════════════════════════════════════════════════════════════════
// S76 — showConfirm: wiring per-overlay + label tombol ter-escape
// ════════════════════════════════════════════════════════════════════════

test('S76 (statik): showConfirm memasang listener tombol via overlay.querySelector, bukan document.getElementById', () => {
    const fn = extractCoreFunction('showConfirm');
    assert.doesNotMatch(fn, /document\.getElementById\('confirm(?:Ok|Cancel)Btn'\)/,
        'getElementById ID-statis melintasi dialog bertumpuk: klik OK me-resolve SEMUA promise terbuka (aksi destruktif terkirim ganda) — pakai overlay.querySelector');
    assert.match(fn, /overlay\.querySelector\('#confirmOkBtn'\)/,
        'listener OK wajib dari referensi overlay lokal');
    assert.match(fn, /overlay\.querySelector\('#confirmCancelBtn'\)/,
        'listener Cancel wajib dari referensi overlay lokal');
});

test('S76 (statik): label konfirmasi/batal ikut di-escape (kontrak escape utuh)', () => {
    const fn = extractCoreFunction('showConfirm');
    assert.match(fn, /\$\{escapeHtml\(confirmLabel\)\}/,
        'confirmLabel disisipkan mentah ke innerHTML — bungkus escapeHtml seperti message');
    assert.match(fn, /\$\{escapeHtml\(cancelLabel\)\}/,
        'cancelLabel juga wajib escapeHtml');
});

test('S76 (vm): dua dialog bertumpuk saling independen; label ber-tag HTML dirender aman', async () => {
    // Mock DOM minimal yang MENOLAK getElementById (mengembalikan null):
    // implementasi lama akan langsung TypeError saat memasang listener —
    // persis kegagalan cross-wiring yang dicegah kontrak ini.
    function makeButtonMock() {
        const listeners = {};
        return {
            _fire(type) { listeners[type]?.(); },
            addEventListener(type, fn) { listeners[type] = fn; },
            focus() {},
        };
    }

    function makeShowConfirmScenario() {
        let cardHtml = '';
        const okBtn = makeButtonMock();
        const cancelBtn = makeButtonMock();
        let keydownHandler = null;
        const overlay = {
            className: '',
            style: {},
            removed: false,
            setAttribute() {},
            appendChild() {},
            remove() { this.removed = true; },
            addEventListener(type, fn) { if (type === 'keydown') keydownHandler = fn; },
            removeEventListener() {},
            querySelectorAll(sel) {
                return sel.includes('button') ? [cancelBtn, okBtn] : [];
            },
            querySelector(sel) {
                if (sel === '#confirmOkBtn') return okBtn;
                if (sel === '#confirmCancelBtn') return cancelBtn;
                return null;
            },
        };
        const card = {
            className: '',
            style: {},
            set innerHTML(v) { cardHtml = v; },
            get innerHTML() { return cardHtml; },
        };
        let created = 0;
        const doc = {
            createElement() {
                created += 1;
                return created === 1 ? overlay : card;
            },
            body: { appendChild() {} },
            getElementById: () => null, // ← sengaja: jalur lama wajib gagal di sini
            activeElement: null,
        };
        return { overlay, okBtn, cancelBtn, get html() { return cardHtml; }, doc };
    }

    const escFnSrc = extractCoreFunction('escapeHtml');
    const showConfirmSrc = extractCoreFunction('showConfirm');

    async function runDialog(label) {
        const sc = makeShowConfirmScenario();
        const sandbox = {
            document: sc.doc,
            console,
            setTimeout, // fokus awal dialog (50 ms) — jangan biarkan timer nyata menggantung test
            clearTimeout,
            escapeHtml: undefined,
        };
        sandbox.globalThis = sandbox;
        vm.createContext(sandbox);
        sandbox.escapeHtml = vm.runInContext('(' + escFnSrc + ')', sandbox);
        const promise = vm.runInContext('(' + showConfirmSrc + ')', sandbox)(
            'Hapus voucher X?', '', label, 'Batal');
        await new Promise((r) => setImmediate(r));
        return { promise, ...sc };
    }

    // Dialog A dan B hidup BERSAMAAN (simulasi Enter ganda sebelum focus timer).
    const a = await runDialog('<b>Ya</b>, Hapus');
    const b = await runDialog('Ya, Matikan');

    // 1) Label ter-escape di markup dialog A.
    assert.ok(a.html.includes('&lt;b&gt;Ya&lt;/b&gt;, Hapus'),
        'label dengan markup harus dirender sebagai teks ter-escape, bukan HTML hidup');

    // 2) Setiap dialog ter-wire ke tombol MILIKNYA sendiri.
    a.okBtn._fire('click');
    const resultA = await a.promise;
    assert.equal(resultA, true, 'klik OK dialog A me-resolve promise A');
    assert.equal(a.overlay.removed, true, 'dialog A tertutup setelah OK');

    // Dialog B belum tersentuh — bukti tidak ada cross-wiring.
    assert.equal(b.overlay.removed, false, 'dialog B TIDAK boleh ikut tertutup saat dialog A di-OK');

    b.cancelBtn._fire('click');
    assert.equal(await b.promise, false, 'klik Cancel dialog B me-resolve promise B dengan false');
});

// ════════════════════════════════════════════════════════════════════════
// S85 — stempel polling visual-only; live region khusus perubahan antrean
// ════════════════════════════════════════════════════════════════════════

test('S85 (statik): #lastUpdatedLabel bebas aria-live (visual-only)', () => {
    const tag = DETAIL_HTML.match(/<span[^>]*id="lastUpdatedLabel"[^>]*>/);
    assert.ok(tag, 'stempel kesegaran #lastUpdatedLabel ada');
    assert.doesNotMatch(tag[0], /aria-live/,
        'aria-live pada jam polling membuat SR mengumumkan "Diperbarui HH:MM:SS" tiap 5–12 detik — jam bukan informasi yang layak diumumkan');
});

test('S85 (statik): ada region live TERPISAH untuk perubahan jumlah antrean izin', () => {
    // Region baru wajib: elemen ber-role="status"/aria-live yang id-nya
    // menandai tujuan antrean (queue/antrean/approval) — bukan stempel jam.
    const regions = [...DETAIL_HTML.matchAll(/<[^>]+(?:role="status"|aria-live="polite")[^>]*>/g)]
        .map((m) => m[0])
        .filter((t) => !t.includes('toast')); // toast container bukan bagian kontrak ini
    const dedicated = regions.find((t) => /queue|antrean|approval/i.test(t));
    assert.ok(dedicated,
        `butuh region live khusus perubahan antrean (id mengandung queue/antrean/approval); region terdeteksi: ${regions.length === 0 ? '(tidak ada)' : regions.map((r) => r.slice(0, 80)).join(' | ')}`);
});

// ════════════════════════════════════════════════════════════════════════
// S86 — source pengawas-detail.js bebas byte NUL (0x00)
// ════════════════════════════════════════════════════════════════════════

test('S86 (biner): pengawas-detail.js tidak mengandung byte NUL 0x00 di mana pun', () => {
    const buf = readFileSync(join(WEBUI_ROOT, 'static', 'js', 'pengawas-detail.js'));
    const nulCount = buf.reduce((n, b) => n + (b === 0x00 ? 1 : 0), 0);
    assert.equal(nulCount, 0,
        `${nulCount} byte NUL tertanam (pemisah fingerprint approvalFingerprint salah ketik) — file terdeteksi biner oleh grep/diff/linter/proxy; ganti pemisah dengan karakter teks (' '|' |')`);
});
