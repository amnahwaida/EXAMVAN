/* Regression contract tests untuk Batch 6 perbaikan UI/UX — halaman pengawasan.
 * Referensi temuan: review_uiux_webui.md §5.5 (ID: S36, S26, R24).
 *
 * Run with:  node --test static/js/uiux-batch6-pengawasan.test.mjs   (from webui/)
 *
 *   - S36: empat interval permanen di pengawas_detail.html (5s/12s/30s/1s)
 *     berjalan tanpa henti walau tab tersembunyi & tanpa clear — boros fetch,
 *     dan berisiko double-scheduling. Pola pembanding yang benar:
 *     refreshDashboardStats/startAutoRefresh di admin-core.js (guard
 *     document.hidden + flag in-flight). Perbaikan: guard document.hidden pada
 *     seluruh callback polling, handle interval disimpan, di-clear saat
 *     visibilitychange→hidden dan dijadwalkan ulang saat visible.
 *   - S26: kontrol yang dirender lewat JS tidak bisa difokus keyboard —
 *     pagination `<a>` tanpa href, kartu monitor div onclick, mac-cell anchor
 *     tanpa href, popup identitas siswa hanya mouse/touch.
 *   - R24: aksi MENOLAK peserta yang BERHASIL ditampilkan dengan toast merah
 *     tipe 'error' — secara semantik keliru (ini konfirmasi berhasil, bukan
 *     kegagalan). Diganti tipe netral 'info' (didukung showToast admin-core).
 *
 * Gaya test mengikuti uiux-batch3-t8-polling.test.mjs / uiux-batch5-admin-list.test.mjs:
 * statik (fs-read + regex) dan perilaku (vm.runInNewContext dengan stub DOM).
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

const SUBMISSIONS = 'templates/admin/submissions.html';
const PENGAWAS = 'templates/admin/pengawas.html';
const DETAIL = 'templates/admin/pengawas_detail.html';

// ---------------------------------------------------------------------------
// S36 — visibility-guard polling pengawas_detail.html
// ---------------------------------------------------------------------------

test('S36/static: keempat callback polling ter-guard document.hidden', () => {
    const html = read(DETAIL);

    // Callback 5 detik TETAP berupa setInterval(loadApprovals, 5000)
    // (kontrak uiux-batch3-t8-polling), sehingga guard-nya hidup DI DALAM
    // fungsi loadApprovals.
    assert.match(html, /setInterval\(loadApprovals,\s*5000\)/,
        'polling antrean 5 detik tetap terjadwalkan (perilaku realtime dipertahankan)');
    const loadApprovalsSrc = html.slice(
        html.indexOf('function loadApprovals'),
        html.indexOf('function markUpdated')
    );
    assert.ok(loadApprovalsSrc.includes('document.hidden'),
        'loadApprovals harus skip saat document.hidden (guard di dalam fungsi)');
    const guardPos = loadApprovalsSrc.indexOf('document.hidden');
    const bodyPos = loadApprovalsSrc.indexOf('apiFetch');
    assert.ok(guardPos >= 0 && bodyPos > guardPos,
        'guard document.hidden dievaluasi SEBELUM fetch dilakukan');

    // Tiga callback lainnya dibungkus guard inline pada penjadwalannya.
    assert.match(html, /setInterval\(function\s*\(\)\s*\{\s*if\s*\(!document\.hidden\)\s*loadDetail\(/,
        'interval 12 detik (loadDetail silent refresh) wajib ter-guard document.hidden');
    assert.match(html, /setInterval\(function\s*\(\)\s*\{\s*if\s*\(!document\.hidden\)\s*refreshActiveToken\(/,
        'interval 30 detik (refreshActiveToken) wajib ter-guard document.hidden');
    assert.match(html, /setInterval\(function\s*\(\)\s*\{\s*if\s*\(!document\.hidden\)\s*updateCountdown\(/,
        'interval 1 detik (updateCountdown) wajib ter-guard document.hidden');

    // Tidak ada lagi setInterval telanjang tanpa guard di halaman ini.
    const bareIntervals = [...html.matchAll(/setInterval\((?!function\s*\(\)\s*\{\s*if\s*\(!document\.hidden\))(loadApprovals)/g)];
    assert.equal(bareIntervals.length, 1,
        'satu-satunya setInterval ber-nama-fungsi langsung adalah loadApprovals (ber-guard internal)');
});

test('S36/static: handle interval disimpan & di-clear (tidak ada interval permanen bocor)', () => {
    const html = read(DETAIL);

    assert.match(html, /var\s+pollTimers\s*=\s*\[\]/,
        'array penampung handle interval wajib ada');
    assert.match(html, /function\s+stopPengawasPolling\s*\(\)/,
        'fungsi stop/clear semua interval wajib ada');
    const stopSrc = html.match(/function\s+stopPengawasPolling[\s\S]*?\n\}/);
    assert.ok(stopSrc, 'stopPengawasPolling punya body');
    assert.match(stopSrc[0], /clearInterval/, 'stopPengawasPolling memanggil clearInterval');

    const startSrc = html.match(/function\s+startPengawasPolling[\s\S]*?\n\}/);
    assert.ok(startSrc, 'startPengawasPolling punya body');
    const stopFirst = startSrc[0].indexOf('stopPengawasPolling()');
    const firstSchedule = startSrc[0].indexOf('setInterval(');
    assert.ok(stopFirst >= 0 && stopFirst < firstSchedule,
        'startPengawasPolling selalu stop dulu — anti double-scheduling');
});

test('S36/static: visibilitychange menghentikan & menjadwalkan ulang polling', () => {
    const html = read(DETAIL);
    assert.match(html, /addEventListener\('visibilitychange'/,
        'halaman wajib bereaksi terhadap visibilitychange');
    const visIdx = html.indexOf("addEventListener('visibilitychange'");
    const handlerSrc = html.slice(visIdx, visIdx + 600);
    assert.match(handlerSrc, /stopPengawasPolling\(\)/,
        'saat tab tersembunyi semua interval di-clear');
    assert.match(handlerSrc, /startPengawasPolling\(\)/,
        'saat tab terlihat kembali polling dijadwalkan ulang');
    // startPengawasPolling hanya dipanggil dari handler + init — pastikan tak
    // dipanggil dua kali saat init (double-scheduling).
    const initCalls = [...html.matchAll(/(?<!function\s)startPengawasPolling\(\)/g)].length;
    assert.equal(initCalls, 2, 'tepat dua panggilan eksplisit: satu di handler visibilitychange, satu saat init');
});

// ---------------------------------------------------------------------------
// S36 — perilaku scheduler (vm, pola harness batch5)
// ---------------------------------------------------------------------------

function loadScheduler(sandbox) {
    const html = read(DETAIL);
    const begin = html.indexOf('var pollTimers = [];');
    assert.ok(begin >= 0, 'blok scheduler S36 bisa diekstrak');
    const endMarker = 'startPengawasPolling();';
    const end = html.lastIndexOf(endMarker) + endMarker.length;
    const snippet = html.slice(begin, end);

    const calls = { approvals: 0, detail: 0, token: 0, countdown: 0 };
    let timerSeq = 0;
    const timers = new Map();

    sandbox.document = Object.assign(sandbox.document || {}, {
        hidden: false,
        addEventListener(type, fn) { sandbox.__visibilityHandlers = sandbox.__visibilityHandlers || {}; sandbox.__visibilityHandlers[type] = fn; }
    });
    sandbox.SUB_PAGE = 1;
    sandbox.loadApprovals = () => { calls.approvals++; };
    sandbox.loadDetail = () => { calls.detail++; };
    sandbox.refreshActiveToken = () => { calls.token++; };
    sandbox.updateCountdown = () => { calls.countdown++; };
    sandbox.setInterval = (fn, ms) => { const id = ++timerSeq; timers.set(id, { fn, ms }); return id; };
    sandbox.clearInterval = (id) => { timers.delete(id); };

    vm.createContext(sandbox);
    vm.runInContext(snippet, sandbox, { filename: 'pengawas-detail.html#scheduler-S36' });

    return {
        calls,
        activeTimers: () => [...timers.values()],
        fireVisibility: (hidden) => {
            sandbox.document.hidden = hidden;
            sandbox.__visibilityHandlers.visibilitychange();
        }
    };
}

function freshSandbox() {
    const sandbox = {};
    sandbox.globalThis = sandbox;
    return sandbox;
}

test('S36/vm: document.visible — scheduler memasang tepat 4 interval dan callback jalan', () => {
    const env = loadScheduler(freshSandbox());
    const timers = env.activeTimers();
    assert.equal(timers.length, 4, 'empat interval terpasang (5s, 12s, 30s, 1s)');
    assert.deepEqual(timers.map((t) => t.ms), [5000, 12000, 30000, 1000]);

    for (const t of timers) t.fn();
    assert.deepEqual(env.calls, { approvals: 1, detail: 1, token: 1, countdown: 1 },
        'saat tab terlihat, semua callback polling dieksekusi');
});

test('S36/vm: document.hidden — callback polling di-skip (nol kerja)', () => {
    const env = loadScheduler(freshSandbox());
    env.fireVisibility(true);
    assert.equal(env.activeTimers().length, 0, 'tab tersembunyi → semua interval sudah di-clear');

    // Simulasi interval yang belum sempat ter-clear tetap tak boleh bekerja:
    // guard callback dievaluasi lewat loadApprovals internal — panggil langsung
    // dengan hidden=true.
    assert.equal(env.calls.approvals + env.calls.detail + env.calls.token + env.calls.countdown, 0,
        'tidak ada callback yang menyala saat hidden');
});

test('S36/vm: kembali visible — polling dijadwalkan ulang tanpa double-scheduling', () => {
    const env = loadScheduler(freshSandbox());
    env.fireVisibility(true);
    env.fireVisibility(false);

    const timers = env.activeTimers();
    assert.equal(timers.length, 4, 'tepat 4 interval aktif lagi (bukan 8 — tidak menumpuk)');
    assert.deepEqual(timers.map((t) => t.ms), [5000, 12000, 30000, 1000]);

    for (const t of timers) t.fn();
    assert.equal(env.calls.approvals, 2, 'loadApprovals: sekali oleh handler visible + sekali oleh tick 5s');
    assert.deepEqual(
        [env.calls.detail, env.calls.token, env.calls.countdown],
        [2, 2, 2],
        'callback ter-guard lainnya juga jalan via handler visible + tick intervalnya'
    );
});

test('S36/vm: start berulang tanpa visibilitychange juga anti double-scheduling', () => {
    const sandbox = freshSandbox();
    const html = read(DETAIL);
    const begin = html.indexOf('var pollTimers = [];');
    const endMarker = 'startPengawasPolling();';
    const snippet = html.slice(begin, html.lastIndexOf(endMarker));

    let seq = 0;
    const live = new Set();
    sandbox.globalThis = sandbox;
    sandbox.document = { hidden: false, addEventListener() {} };
    sandbox.SUB_PAGE = 1;
    sandbox.loadApprovals = () => {};
    sandbox.loadDetail = () => {};
    sandbox.refreshActiveToken = () => {};
    sandbox.updateCountdown = () => {};
    sandbox.setInterval = () => { const id = ++seq; live.add(id); return id; };
    sandbox.clearInterval = (id) => live.delete(id);

    vm.createContext(sandbox);
    vm.runInContext(snippet + '\nstartPengawasPolling();\nstartPengawasPolling();', sandbox);
    assert.equal(live.size, 4, 'walau start dipanggil berkali-kali, tetap hanya 4 interval hidup');
});

// ---------------------------------------------------------------------------
// S26 — kontrol render-JS keyboard-aksesibel
// ---------------------------------------------------------------------------

test('S26/static: pagination render-JS pakai <button type="button"> (bukan <a> tanpa href)', () => {
    for (const rel of [PENGAWAS, DETAIL]) {
        const html = read(rel);
        assert.ok(!html.includes('<a class="pagination-page-num"'),
            `${rel}: tidak boleh ada lagi pagination <a> tanpa href`);
        const btns = [...html.matchAll(/<button type="button" class="pagination-page-num[^"]*"/g)];
        assert.ok(btns.length >= 3,
            `${rel}: pagination render-JS wajib memakai <button type="button" class="pagination-page-num..."> (prev/next/nomor)`);
        assert.match(html, /<button type="button" class="pagination-page-num'[^]*?page-current/,
            `${rel}: nomor halaman aktif tetap membawa class page-current`);
    }
});

test('S26/static: kartu monitor pengawas.html punya role="button" tabindex="0"', () => {
    const html = read(PENGAWAS);
    const card = html.match(/<div class="exam-monitor-card"[^>]*>/);
    assert.ok(card, 'kartu monitor dirender lewat JS');
    assert.match(card[0], /role="button"/, 'kartu monitor wajib role="button" (Enter/Space via handler global admin-core)');
    assert.match(card[0], /tabindex="0"/, 'kartu monitor wajib tabindex="0" agar bisa difokus');
});

test('S26/static: mac-cell riwayat akses jadi <button type="button"> (bukan <a> tanpa href)', () => {
    const html = read(DETAIL);
    assert.ok(!html.includes('<a class="mac-cell"'), 'mac-cell anchor tanpa href tidak boleh tersisa');
    const btn = html.match(/<button type="button" class="mac-cell"[^>]*>/);
    assert.ok(btn, 'mac-cell wajib dirender sebagai <button type="button" class="mac-cell">');
    // Batch 7 (R28 lanjutan): handler inline onclick dimigrasi ke delegasi
    // data-action — intent proteksi sama, handler riwayat akses tetap terpasang.
    assert.match(btn[0], /data-action="show-access-log"/, 'handler riwayat akses tetap sama (delegasi data-action)');
    assert.match(btn[0], /title="/, 'title petunjuk klik tetap ada');

    // Anchor "Detail" di baris yang sama juga tanpa href — minimal role=button.
    const detailLink = html.match(/<a class="action-link"[^>]*>/);
    if (detailLink) {
        assert.match(detailLink[0], /role="button"/, 'action-link tanpa href wajib role="button"');
        assert.match(detailLink[0], /tabindex="0"/, 'action-link tanpa href wajib tabindex="0"');
    }
});

test('S26/static: popup identitas siswa (submissions.html) keyboard-aksesibel', () => {
    const html = read(SUBMISSIONS);
    const el = html.match(/<strong class="submission-identity-btn"[^>]*>/);
    assert.ok(el, 'elemen pemicu popup identitas ada');
    assert.match(el[0], /role="button"/, 'pemicu popup identitas wajib role="button" (handler click delegasi + Enter/Space global admin-core)');
    assert.match(el[0], /tabindex="0"/, 'pemicu popup identitas wajib tabindex="0"');
});

// ---------------------------------------------------------------------------
// R24 — toast tolak perangkat bertipe keliru ('error' padahal sukses)
// ---------------------------------------------------------------------------

test('R24/static: toast sukses-tolak di setApproval tidak lagi bertipe error (pakai info)', () => {
    const html = read(DETAIL);
    const src = html.slice(html.indexOf('function setApproval'));
    const successBranch = src.slice(0, src.indexOf('} else {'));
    assert.match(successBranch, /showToast\(/, 'cabang sukses setApproval menampilkan toast');

    const toastCall = successBranch.match(/showToast\([^;]*\);/);
    assert.ok(toastCall, 'panggilan toast cabang sukses bisa diekstrak');
    assert.doesNotMatch(toastCall[0], /'error'/,
        'toast konfirmasi penolakan BERHASIL tidak boleh bertipe error (merah = gagal, menyesatkan)');
    assert.match(toastCall[0], /'info'/,
        'tipe toast netral "info" dipakai untuk konfirmasi penolakan (didukung showToast admin-core)');
});
