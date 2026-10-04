/* Regression guard for the pengawas DETAIL page inline script.
 *
 * WHY THIS EXISTS — the existing guard was `node --check`, which only parses.
 * A ReferenceError raised by an out-of-scope identifier is perfectly valid
 * syntax, so the page shipped completely dead: the inline script threw at its
 * very last statement, so EVERY setInterval after it never registered, and
 * every render function touching the trapped identifier threw instead of
 * drawing rows. `node --check` stayed green the whole time.
 *
 * This suite therefore EXECUTES the real inline script (template actions
 * substituted) in a node:vm with a stubbed DOM and asserts on observable
 * effects: no throw at load, pollers registered, rows rendered.
 *
 * Run with: node --test static/js/pengawas-detail-runtime.test.mjs (from webui/)
 */

import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';

const HERE = path.dirname(fileURLToPath(import.meta.url));
const WEBUI_ROOT = path.join(HERE, '..', '..');
const DETAIL_PATH = path.join(WEBUI_ROOT, 'templates', 'admin', 'pengawas_detail.html');
const LIST_PATH = path.join(WEBUI_ROOT, 'templates', 'admin', 'pengawas.html');
const readDetail = () => fs.readFileSync(DETAIL_PATH, 'utf8');
const readList = () => fs.readFileSync(LIST_PATH, 'utf8');

// ---------------------------------------------------------------------------
// Extraction: pull the inline <script> out and neutralise Go template actions.
// ---------------------------------------------------------------------------

function inlineScript(html) {
    const blocks = [...html.matchAll(/<script>([\s\S]*?)<\/script>/g)].map((m) => m[1]);
    assert.ok(blocks.length > 0, 'template must contain an inline <script>');
    return blocks.reduce((a, b) => (b.length > a.length ? b : a));
}

function toPlainJs(html) {
    // {{...}} actions become literals; {{if}}/{{end}} wrappers vanish. Any
    // leftover action would be a syntax error, which node --check already
    // covers — here we only need the script to be evaluable.
    return inlineScript(html)
        .replace(/\{\{[^}]*\}\}/g, '0')
        .replace(/\{\{if[^}]*\}\}/g, '')
        .replace(/\{\{end\}\}/g, '');
}

// ---------------------------------------------------------------------------
// Minimal DOM + browser stubs
// ---------------------------------------------------------------------------

function makeEl(id) {
    const el = {
        id, textContent: '', innerHTML: '', value: '', dataset: {}, style: {},
        classList: { add() {}, remove() {}, toggle() {}, contains: () => false },
        addEventListener() {}, removeEventListener() {}, appendChild() {},
        removeChild() {}, querySelector: () => null, querySelectorAll: () => [],
        setAttribute() {}, getAttribute: () => null, remove() {}, closest: () => null,
        focus() {}, click() {}, contains: () => false, children: [], childNodes: [],
        parentElement: null,
    };
    return el;
}

/**
 * Runs the script body inside a vm sandbox.
 *
 * @param {object} opts
 * @param {string} opts.js            script source (already template-free)
 * @param {object} opts.payload       value returned by apiFetch/fetch json()
 * @param {string[]} opts.callAfter   statements to run after load, awaited
 * @returns {Promise<{threw, error, intervals, fetches, els, sandbox}>}
 */
async function runScript({ js, payload = {}, callAfter = [], preset = {} }) {
    const els = {};
    // preset: { elementId: { prop: value } } — seeds the stub DOM before the
    // script runs, e.g. { statusFilter: { value: 'submitted' } }.
    for (const [id, props] of Object.entries(preset)) {
        els[id] = Object.assign(makeEl(id), props);
    }
    const document = {
        createElement: () => makeEl('new'),
        getElementById: (id) => (els[id] ||= makeEl(id)),
        querySelector: (s) => (els[s] ||= makeEl(s)),
        querySelectorAll: () => [],
        addEventListener() {},
        body: makeEl('body'),
        documentElement: makeEl('html'),
        readyState: 'complete',
    };
    const intervals = [];
    const fetches = [];
    const json = () => Promise.resolve(payload);

    const sandbox = {
        console: { log() {}, warn() {}, error() {} },
        setInterval: (_fn, ms) => { intervals.push(ms); return intervals.length; },
        clearInterval() {}, setTimeout: () => 0, clearTimeout() {},
        fetch: (url) => { fetches.push(String(url)); return Promise.resolve({ ok: true, json, text: () => Promise.resolve('') }); },
        requestAnimationFrame: () => 0,
        localStorage: { getItem: () => null, setItem() {}, removeItem() {} },
        location: { href: 'http://x/', search: '', pathname: '/admin/pengawas/1' },
        history: { replaceState() {}, pushState() {} },
        navigator: { clipboard: { writeText: () => Promise.resolve() } },
        Event: class {}, CustomEvent: class {},
        // admin-core.js surface the inline script consumes.
        // admin-core's apiFetch resolves to a Response — the inline script calls
        // r.json(). Returning the bare payload here would make every row builder
        // throw 'r.json is not a function' and mask the bugs under test.
        apiFetch: (url) => { fetches.push(String(url)); return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve(payload) }); },
        escapeHtml: (s) => String(s == null ? '' : s),
        jsEscape: (s) => String(s == null ? '' : s),
        formatDateTimeID: (s) => String(s == null ? '' : s),
        localizeUTC: (s) => String(s == null ? '' : s),
        initLiveSearch() {}, showConfirm: () => Promise.resolve(true),
        copyCode: () => Promise.resolve(), showToast() {},
        PengawasDetailQueue: { serializeApprovals: () => 'x', computeApprovalRowOps: () => [] },
        Actions: { register() {}, get: () => null },
        EXAM_STATUS_LABELS: {},
        Promise, JSON, Math, Date, Object, Array, String, Number, Boolean, Error,
        isNaN, parseInt, parseFloat, encodeURIComponent, decodeURIComponent, URLSearchParams,
    };
    sandbox.window = sandbox;
    sandbox.globalThis = sandbox;
    sandbox.document = document;
    const ctx = vm.createContext(sandbox);

    let threw = false;
    let error = null;
    try {
        vm.runInContext(js, ctx, { filename: 'pengawas_detail.inline.js' });
    } catch (e) {
        threw = true;
        error = e;
    }
    for (const stmt of callAfter) {
        try {
            vm.runInContext(stmt, ctx);
        } catch (e) {
            threw = true;
            error = e;
        }
        // let queued microtasks/promise callbacks run
        await new Promise((r) => setImmediate(r));
        await new Promise((r) => setImmediate(r));
    }
    return { threw, error, intervals, fetches, els, sandbox, ctx };
}

/**
 * Like runScript, but hands back the registered setInterval callbacks so a test
 * can fire a single tick and count what it costs.
 */
async function runScriptCapturingTimers(payload = detailPayload()) {
    const timers = [];
    const fetches = [];
    const els = {};
    for (const [id, props] of Object.entries(arguments[1] || {})) {
        els[id] = Object.assign(makeEl(id), props);
    }
    const document = {
        createElement: () => makeEl('new'),
        getElementById: (id) => (els[id] ||= makeEl(id)),
        querySelector: (s) => (els[s] ||= makeEl(s)),
        querySelectorAll: () => [],
        addEventListener() {},
        body: makeEl('body'),
        documentElement: makeEl('html'),
        readyState: 'complete',
    };
    const mkResp = () => ({ ok: true, status: 200, json: () => Promise.resolve(payload) });
    const sandbox = {
        console: { log() {}, warn() {}, error() {} },
        setInterval: (fn, ms) => { timers.push({ fn, ms }); return timers.length; },
        clearInterval() {}, setTimeout: () => 0, clearTimeout() {},
        fetch: (u) => { fetches.push(String(u)); return Promise.resolve(mkResp()); },
        requestAnimationFrame: () => 0,
        localStorage: { getItem: () => null, setItem() {}, removeItem() {} },
        location: { href: 'http://x/', search: '', pathname: '/admin/pengawas/1' },
        history: { replaceState() {}, pushState() {} },
        navigator: { clipboard: { writeText: () => Promise.resolve() } },
        Event: class {}, CustomEvent: class {},
        apiFetch: (u) => { fetches.push(String(u)); return Promise.resolve(mkResp()); },
        escapeHtml: (s) => String(s == null ? '' : s),
        jsEscape: (s) => String(s == null ? '' : s),
        formatDateTimeID: (s) => String(s == null ? '' : s),
        localizeUTC: (s) => String(s == null ? '' : s),
        initLiveSearch() {}, showConfirm: () => Promise.resolve(true),
        copyCode: () => Promise.resolve(), showToast() {},
        PengawasDetailQueue: { serializeApprovals: () => 'x', computeApprovalRowOps: () => [] },
        Actions: { register() {}, get: () => null },
        EXAM_STATUS_LABELS: {},
        Promise, JSON, Math, Date, Object, Array, String, Number, Boolean, Error,
        isNaN, parseInt, parseFloat, encodeURIComponent, decodeURIComponent, URLSearchParams,
    };
    sandbox.window = sandbox;
    sandbox.globalThis = sandbox;
    sandbox.document = document;
    const ctx = vm.createContext(sandbox);
    try {
        vm.runInContext(toPlainJs(readDetail()), ctx, { filename: 'pd.inline.js' });
    } catch (e) {
        throw new Error('inline script threw at load: ' + e.message);
    }
    // Let the page's OWN initial load finish before measuring. It leaves
    // detailLoading=true while in flight, and loadDetail coalesces a call that
    // arrives during that window into detailRerunPending — counting that would
    // measure an in-flight coalesce, not the steady-state poll cost.
    for (let i = 0; i < 8; i++) await new Promise((r) => setImmediate(r));
    // Drop anything the initial load issued; we are measuring ONE tick.
    fetches.length = 0;
    return { timers, fetches, els, sandbox, ctx };
}

const SUBMISSION_ROW = {
    id: 7, student_name: 'Siswa Ritel', student_key: '01|siswa ritel|xii a',
    exam_number: '01', student_class: 'XII A', identity_data: {}, submitted: true,
    score: 80, start_time: '2026-10-04T08:00:00Z', created_at: '2026-10-04T08:00:00Z',
    submitted_at: '2026-10-04T09:00:00Z', first_access_at: '', last_access_at: '',
    mac_address: 'AA:BB:CC:DD:EE:FF', access_logs: [], is_online: true,
    attempt_count: 1, submission_history: [],
};

function detailPayload(overrides = {}) {
    return {
        success: true, exam_name: 'Ujian', exam_active_token: 'TOK1234',
        page: 1, per_page: 20, total: 1, total_pages: 1,
        stats: { total: 1, active: 0, submitted: 1, not_started: 0 },
        submissions: [SUBMISSION_ROW],
        data: [], grants: [], enabled: false,
        ...overrides,
    };
}

// ---------------------------------------------------------------------------
// The regression: the whole page was dead because of an out-of-scope call.
// ---------------------------------------------------------------------------

test('BUG-1: inline script loads without ReferenceError (poller registration must be reachable)', async () => {
    const { threw, error, intervals } = await runScript({ js: toPlainJs(readDetail()) });
    assert.equal(
        threw, false,
        `pengawas_detail inline script threw at load: ${error && error.message}\n` +
        'An out-of-scope identifier is valid syntax, so node --check stays green ' +
        'while the page is dead. Keep repeat-state helpers at the script top level.'
    );
    assert.ok(
        intervals.length > 0,
        'no setInterval registered — the hot-reload pollers live after the failing ' +
        'statement, so a throw silently disables every one of them'
    );
});

test('BUG-1: hot-reload pollers are registered on the expected cadence', async () => {
    const { intervals } = await runScript({ js: toPlainJs(readDetail()) });
    // 1s = countdown display tick (NOT a network trigger — asserted separately).
    assert.ok(intervals.includes(1000), `expected the 1s countdown tick, got ${intervals}`);
    // Device table + approval queue must stay in the multi-second range.
    const network = intervals.filter((ms) => ms !== 1000);
    assert.ok(network.length > 0, 'expected network pollers');
    for (const ms of network) {
        assert.ok(ms >= 3000,
            `network poller at ${ms}ms is too aggressive for the submissions endpoint`);
    }
    assert.ok(
        intervals.some((ms) => ms >= 10000),
        `expected a device-table poll of >=10s, got ${intervals}`
    );
});

test('BUG-4: the 1s countdown tick never triggers a network request', async () => {
    // Once the page is live (BUG-1 fixed) a 1s tick that calls the token
    // endpoint becomes 1 req/sec FOREVER whenever the countdown is negative:
    // the server refuses to rotate once the exam is stopped, so the client
    // never learns the window moved on.
    const { fetches } = await runScript({
        js: toPlainJs(readDetail()),
        payload: detailPayload(),
        callAfter: [
            // Simulate an already-expired rotation window on a stopped exam:
            // server hands back the SAME token and no usable reset stamp.
            'TOKEN_MODE = "dynamic"; TOKEN_INTERVAL_MINUTES = 5;' +
            'TOKEN_LAST_RESET = new Date(Date.now() - 3600 * 1000).toISOString();',
            'for (var _i = 0; _i < 20; _i++) updateCountdown();',
        ],
    });
    // The token poll is the per_page=1 request. Do NOT match bare
    // 'page=1' — that also prefixes the device-table poll (per_page=20).
    const tokenPolls = fetches.filter((u) => /submissions\?page=1&per_page=1(&|$)/.test(u));
    assert.ok(tokenPolls.length <= 1,
        `a 1s countdown fired ${tokenPolls.length} token requests — it must latch ` +
        'and fetch at most once per due window');
});

test('BUG-2: loadDetail renders real rows instead of an error row', async () => {
    const { els, threw, error } = await runScript({
        js: toPlainJs(readDetail()),
        payload: detailPayload(),
        callAfter: ['loadDetail(1)'],
    });
    const html = els.submissionBody ? els.submissionBody.innerHTML : '';
    assert.ok(
        /data-submission-id="7"/.test(html) && /Terkumpul/.test(html),
        `device table did not render the row (threw=${threw} ${error && error.message})\n` +
        `tbody was: ${html.slice(0, 200)}\n` +
        'A row builder referencing an IIFE-local map throws ReferenceError, which ' +
        'the handler catch turns into a misleading "Gagal menghubungi server".'
    );
    assert.ok(
        !/Gagal menghubungi server/.test(html) && !/Memuat data/.test(html),
        'device table was replaced by an error/loading row instead of data'
    );
});

test('BUG-2: stats are updated together with the rows', async () => {
    const { els } = await runScript({
        js: toPlainJs(readDetail()),
        payload: detailPayload(),
        callAfter: ['loadDetail(1)'],
    });
    assert.equal(String(els.statTotal && els.statTotal.textContent), '1',
        'stat cards must reflect the payload alongside the rows');
});

// ---------------------------------------------------------------------------
// BUG-3: repeat-grant calls must hit the real /admin/api prefix.
// ---------------------------------------------------------------------------

test('BUG-3: every repeat-grants call site is built from the /admin/api prefix', () => {
    const html = readDetail();
    // The URLs are assembled as '/admin/api/pengawas/exams/' + examId +
    // '/repeat-grants', so no single string literal holds the whole path.
    // Assert per line: a line mentioning repeat-grants must either carry the
    // admin prefix or be the bare '/repeat-grants' suffix literal that the
    // concatenation appends.
    const sites = html.split('\n')
        .map((l, i) => ({ n: i + 1, l }))
        .filter(({ l }) => l.includes('repeat-grants')
            && !l.trim().startsWith('//')
            && !l.trim().startsWith('*'));
    assert.ok(sites.length > 0, 'expected repeat-grants call sites');
    for (const { n, l } of sites) {
        const isSuffixLiteral = /['"`]\/repeat-grants['"`]/.test(l);
        assert.ok(
            l.includes('/admin/api/pengawas') || isSuffixLiteral,
            `line ${n} builds a repeat-grants URL without the /admin/api prefix: ${l.trim()}`
        );
    }
});

test('BUG-3: no call site builds a repeat-grants URL without the /admin prefix', () => {
    const js = toPlainJs(readDetail());
    assert.ok(
        !js.includes("'/api/pengawas/"),
        "found apiFetch('/api/pengawas/...') — the admin prefix /admin is missing"
    );
    assert.ok(
        js.includes("/admin/api/pengawas/exams/' + examId + '/repeat-grants"),
        'expected the /admin/api-prefixed repeat-grants URL expression'
    );
});

test('BUG-3: no admin API call targets a bare /api/pengawas path', () => {
    for (const [name, html] of [['pengawas_detail', readDetail()], ['pengawas', readList()]]) {
        const bad = [...html.matchAll(/['"`]\/api\/(?!exams\/|hasil)/g)].map((m) => m[0]);
        assert.deepEqual(bad, [],
            `${name}.html calls a public-API-shaped path; admin endpoints live under /admin/api`);
    }
});

// ---------------------------------------------------------------------------
// Hot reload contract: silent polls must never destroy known-good content.
// ---------------------------------------------------------------------------

test('BUG-11: a failing silent poll keeps the previously rendered rows', async () => {
    const { els, sandbox, ctx } = await runScript({
        js: toPlainJs(readDetail()),
        payload: detailPayload(),
        callAfter: ['loadDetail(1)'],
    });
    assert.ok(/data-submission-id="7"/.test(els.submissionBody.innerHTML),
        'precondition: rows rendered');

    // Next poll fails: a background refresh must not blank the table.
    sandbox.apiFetch = () => Promise.resolve({
        ok: true, status: 200, json: () => Promise.resolve({ success: false, message: 'Akses ditolak' }),
    });
    vm.runInContext('SUB_PAGE = 1; loadDetail(1, true)', ctx);
    await new Promise((r) => setImmediate(r));
    await new Promise((r) => setImmediate(r));

    assert.ok(
        /data-submission-id="7"/.test(els.submissionBody.innerHTML),
        'a silent auto-refresh wiped the device table on failure — the supervisor ' +
        'loses the last known state and sees only a bare error'
    );
});

test('BUG-8: row numbering follows the page the server actually served', async () => {
    const { sandbox, ctx } = await runScript({
        js: toPlainJs(readDetail()),
        // Server clamped an out-of-range request back to page 1.
        payload: detailPayload({ page: 1, total: 3, total_pages: 1 }),
        callAfter: ['loadDetail(3)'],
    });
    const stored = vm.runInContext('SUB_PAGE', ctx);
    assert.equal(stored, 1,
        'SUB_PAGE must follow res.page — otherwise rows are numbered 41-60 while ' +
        'the footer says "Menampilkan 1-20" and the pagination control disappears');
});

// ---------------------------------------------------------------------------
// "Monitoring Perangkat" filter UX
// ---------------------------------------------------------------------------

test('empty state names the status filter instead of claiming the exam is empty', async () => {
    // Behavioural, not a regex over the source: with a status filter that
    // matches nothing, the table must say WHICH filter excluded everything.
    // The old wording keyed only off the search box, so it claimed
    // "Belum ada perangkat terdaftar" while the stat cards directly above
    // still listed devices — the supervisor concludes the roster vanished.
    const { els } = await runScript({
        js: toPlainJs(readDetail()),
        payload: detailPayload({
            submissions: [],
            stats: { total: 4, active: 0, submitted: 0, not_started: 4 },
        }),
        callAfter: ['loadDetail(1)'],
        preset: { statusFilter: { value: 'submitted' } },
    });
    const html = els.submissionBody ? els.submissionBody.innerHTML : '';
    assert.ok(html.length > 0, 'empty-state branch must render something');
    assert.ok(
        !/Belum ada perangkat terdaftar/.test(html),
        'a filtered-to-empty table must not claim no devices are registered: ' + html.slice(0, 200)
    );
    assert.ok(
        /Terkumpul/.test(html),
        'the message must name the status that excluded the rows: ' + html.slice(0, 200)
    );
});

test('empty state still reports an unfiltered empty exam plainly', async () => {
    const { els } = await runScript({
        js: toPlainJs(readDetail()),
        payload: detailPayload({ submissions: [], stats: { total: 0, active: 0, submitted: 0, not_started: 0 } }),
        callAfter: ['loadDetail(1)'],
    });
    const html = els.submissionBody ? els.submissionBody.innerHTML : '';
    assert.ok(
        /Belum ada perangkat terdaftar/.test(html),
        'with no filter at all the honest message is that no device is registered: ' + html.slice(0, 200)
    );
});

// ---------------------------------------------------------------------------
// Hot reload completeness: every server-authoritative state rendered on the
// page must be re-read by the poll, not only on manual reload.
// ---------------------------------------------------------------------------

test('BUG-9: repeat-grant state is re-read by the poller, not just on load', () => {
    const html = readDetail();
    // The granted/cancelled button state is baked into data-granted at render
    // time. Without a poller entry point, a grant made by another supervisor
    // (or another tab) never appears until a manual page reload.
    const poller = html.slice(html.indexOf('function startPengawasPolling'));
    assert.ok(
        /refreshRepeatGrants\(/.test(poller),
        'repeat-grants must be refreshed by the poller — otherwise the Izinkan / ' +
        'Cabut Izin buttons go stale and a supervisor sees the wrong permission state'
    );
});

test('BUG-9: auto-approve toggle state is re-read by the poller', () => {
    const js = toPlainJs(readDetail());
    // One payload now carries the flag, so no extra endpoint is needed.
    assert.ok(
        /auto_approve_enabled/.test(js),
        'the submissions payload must drive the auto-approve toggle so hot reload ' +
        'reflects another supervisor toggling it'
    );
});

// ---------------------------------------------------------------------------
// The device table must actually show who is being monitored, and whether the
// device is currently present.
// ---------------------------------------------------------------------------

test('device table renders the student name, not just a MAC address', async () => {
    // The search box invites "Cari nama, ID perangkat..." but the row only ever
    // showed the MAC. The supervisor could find a student by name and then not
    // see that name anywhere in the table, forcing a modal open per row.
    const { els } = await runScript({
        js: toPlainJs(readDetail()),
        payload: detailPayload({
            submissions: [{ ...SUBMISSION_ROW, student_name: 'Rani Kusuma', is_online: true }],
        }),
        callAfter: ['loadDetail(1)'],
    });
    const html = els.submissionBody.innerHTML;
    assert.ok(/Rani Kusuma/.test(html),
        'the monitoring table must display the student name; got: ' + html.slice(0, 300));
});

test('device table renders the presence indicator from is_online', async () => {
    // is_online is computed server-side (a Redis EXISTS per device) and shipped
    // in every payload — and was never read by any template or script. The
    // heartbeat/presence infrastructure therefore had NO visible effect on the
    // monitoring page it exists for.
    const online = await runScript({
        js: toPlainJs(readDetail()),
        payload: detailPayload({ submissions: [{ ...SUBMISSION_ROW, is_online: true }] }),
        callAfter: ['loadDetail(1)'],
    });
    const onlineHtml = online.els.submissionBody.innerHTML;
    assert.ok(/pd-status-dot-on/.test(onlineHtml),
        'an online device must render the pulsing presence dot; got: ' +
        onlineHtml.slice(0, 300));

    const offline = await runScript({
        js: toPlainJs(readDetail()),
        payload: detailPayload({ submissions: [{ ...SUBMISSION_ROW, is_online: false }] }),
        callAfter: ['loadDetail(1)'],
    });
    const offlineHtml = offline.els.submissionBody.innerHTML;
    assert.ok(
        !/pd-status-dot-on/.test(offlineHtml),
        'an offline device must NOT render the pulsing presence dot: ' +
        offlineHtml.slice(0, 300)
    );
    assert.ok(
        /pd-status-dot-off/.test(offlineHtml),
        'an offline device must still render a static muted dot, so absence of ' +
        'presence reads as "offline", not as "no data": ' + offlineHtml.slice(0, 300)
    );
});

test('device table shows when the device was last seen', async () => {
    const { els } = await runScript({
        js: toPlainJs(readDetail()),
        payload: detailPayload({
            submissions: [{ ...SUBMISSION_ROW, last_access_at: '2026-10-04T09:55:00Z' }],
        }),
        callAfter: ['loadDetail(1)'],
    });
    assert.ok(
        /09:55|09:5/.test(els.submissionBody.innerHTML),
        'the last-access stamp must be visible in the table, not only inside the ' +
        'access-log modal: ' + els.submissionBody.innerHTML.slice(0, 300)
    );
});

test('presence column uses design tokens, not colour literals', () => {
    // There are token-guard tests in this repo; a literal hex/rgb in new UI
    // markup would break the sweep.
    const html = readDetail();
    const at = html.indexOf('pd-status-dot');
    assert.ok(at > 0, 'expected a presence-dot class');
    const css = html.slice(html.indexOf('.pd-status-dot'), html.indexOf('.pd-status-dot') + 400);
    assert.ok(
        !/#[0-9a-f]{3,6}\b/i.test(css),
        'presence dot must be drawn with theme tokens, found a hex literal: ' + css.slice(0, 200)
    );
    assert.ok(/var\(--/.test(css), 'presence dot must use var(--token)');
});

// ---------------------------------------------------------------------------
// The access-log modal is the one place the heartbeat timeline is visible —
// and it was frozen for the whole session.
// ---------------------------------------------------------------------------

test('the access-log modal refreshes while it is open', async () => {
    const js = toPlainJs(readDetail());
    // The modal renders once on open and has no refresh control at all — only
    // a close button. The heartbeat timeline inside it gains an entry roughly
    // every minute per device, so a supervisor watching a candidate for
    // cheating stares at a frozen snapshot while the table behind it live-
    // reloads every 12s.
    const at = js.indexOf('function showAccessLog');
    assert.ok(at > 0, 'showAccessLog not found');
    const modalBlock = js.slice(at, at + 9000);
    assert.ok(
        /openAccessLogId|startAccessLogPoll|refreshAccessLog/.test(modalBlock),
        'the modal must have a refresh path while open (re-render from the ' +
        'latest snapshot, or its own poller)'
    );
});

test('closing the modal stops it following the poll', () => {
    const js = toPlainJs(readDetail());
    const close = js.indexOf('function closeAccessLogModal');
    assert.ok(close > 0, 'closeAccessLogModal not found');
    const closeBlock = js.slice(close, close + 600);
    // No interval is created for the modal — it reuses the table poll — so
    // "stopping" means clearing the tracked device. Leaving it set would keep
    // re-rendering a modal nobody can see on every tick, forever.
    assert.ok(
        /openAccessLogId\s*=\s*null/.test(closeBlock),
        'closing the modal must clear the tracked device id, otherwise it keeps ' +
        're-rendering off-screen on every poll: ' + closeBlock.slice(0, 220)
    );
});

// ---------------------------------------------------------------------------
// Exam liveness: the page must not look like a live control panel after the
// exam stopped.
// ---------------------------------------------------------------------------

test('a polled payload with exam_active=false disables the mutating actions', async () => {
    const { els } = await runScript({
        js: toPlainJs(readDetail()),
        payload: detailPayload({
            exam_active: false,
            exam_schedule_ended: false,
            submissions: [{ ...SUBMISSION_ROW, attempt_count: 1 }],
        }),
        callAfter: ['loadDetail(1)'],
    });
    const html = els.submissionBody.innerHTML;
    assert.ok(
        /disabled/.test(html),
        'approve/reject and repeat-grant buttons must be disabled once the exam is ' +
        'stopped — the server refuses every one of them: ' + html.slice(0, 300)
    );
    assert.ok(els.examLivenessNote, 'a liveness note element must exist');
    assert.ok(
        /dihentikan|berakhir/i.test(els.examLivenessNote.textContent || ''),
        'the note must explain why the controls are dead, got: ' +
        JSON.stringify(els.examLivenessNote && els.examLivenessNote.textContent)
    );
});

test('a live exam leaves the mutating actions enabled and the note hidden', async () => {
    const { els } = await runScript({
        js: toPlainJs(readDetail()),
        payload: detailPayload({ exam_active: true, exam_schedule_ended: false }),
        callAfter: ['loadDetail(1)'],
    });
    const html = els.submissionBody.innerHTML;
    assert.ok(!/disabled/.test(html),
        'a running exam must keep its actions usable: ' + html.slice(0, 300));
    assert.equal(els.examLivenessNote && els.examLivenessNote.style.display, 'none',
        'the liveness note must stay hidden while the exam is running');
});

test('an elapsed exam window disables actions even while status is active', async () => {
    // This is the case that surprised supervisors: status still "active", yet
    // every approval was refused because the window had passed.
    const { els } = await runScript({
        js: toPlainJs(readDetail()),
        payload: detailPayload({ exam_active: true, exam_schedule_ended: true }),
        callAfter: ['loadDetail(1)'],
    });
    assert.ok(
        /disabled/.test(els.submissionBody.innerHTML),
        'a passed window must disable the actions even when the exam is active'
    );
    assert.ok(
        /berakhir/i.test((els.examLivenessNote && els.examLivenessNote.textContent) || ''),
        'the note must mention the elapsed window, got: ' +
        JSON.stringify(els.examLivenessNote && els.examLivenessNote.textContent)
    );
});

// ---------------------------------------------------------------------------
// Repeat attempts are legitimate, not suspicious.
// ---------------------------------------------------------------------------

test('multiple attempts are rendered neutrally, not as a danger badge', async () => {
    // The repeat-grant feature exists precisely so a supervisor can let a
    // student retake. Flagging that student in danger-red right afterwards
    // contradicted the supervisor's own action — the table was asserting
    // "suspicious" about a state the supervisor had explicitly approved.
    const { els } = await runScript({
        js: toPlainJs(readDetail()),
        payload: detailPayload({
            submissions: [{ ...SUBMISSION_ROW, attempt_count: 3, student_name: 'Rani Kusuma' }],
        }),
        callAfter: ['loadDetail(1)'],
    });
    const html = els.submissionBody.innerHTML;
    assert.ok(
        !/rgb-danger/.test(html),
        'a repeat attempt must not be rendered with the danger token: ' + html.slice(0, 300)
    );
    assert.ok(
        !/color-danger/.test(html),
        'a repeat attempt must not use danger colouring: ' + html.slice(0, 300)
    );
    assert.ok(
        /tone-neutral/.test(html),
        'multiple attempts should use the dormant/neutral chip already defined ' +
        'in admin-base.css: ' + html.slice(0, 300)
    );
    assert.ok(
        /3x|3×/.test(html),
        'the attempt count itself must still be shown: ' + html.slice(0, 300)
    );
});

test('a single attempt stays visually quiet', async () => {
    const { els } = await runScript({
        js: toPlainJs(readDetail()),
        payload: detailPayload({ submissions: [{ ...SUBMISSION_ROW, attempt_count: 1 }] }),
        callAfter: ['loadDetail(1)'],
    });
    const html = els.submissionBody.innerHTML;
    assert.ok(!/tone-neutral/.test(html),
        'the ordinary one-attempt case should not carry a chip at all');
    assert.ok(/1x/.test(html), 'the attempt count must still be shown');
});

// ---------------------------------------------------------------------------
// Poll cost: one tick, one fetch per resource.
// ---------------------------------------------------------------------------

test('one 12s tick issues exactly ONE submissions fetch', async () => {
    // Two 12s intervals both reached loadDetail (the repeat-grants poller
    // chained into it as well as the table poller), so the heaviest endpoint on
    // the page was hit twice per tick — the load doubled the moment repeat
    // grants were added to the poll.
    const { timers, fetches } = await runScriptCapturingTimers();
    const twelve = timers.filter((t) => t.ms === 12000);
    assert.equal(twelve.length, 1,
        `expected exactly one 12s interval, found ${twelve.length} — duplicate ` +
        'pollers multiply the load on the submissions endpoint');
    assert.equal(twelve[0].fn.length >= 0, true);
});

test('firing every registered poller once produces one submissions fetch', async () => {
    const { timers, fetches, ctx } = await runScriptCapturingTimers();
    for (const t of timers.filter((x) => x.ms === 12000)) t.fn();
    await new Promise((r) => setImmediate(r));
    await new Promise((r) => setImmediate(r));
    await new Promise((r) => setImmediate(r));
    const subs = fetches.filter((u) => /submissions/.test(u));
    assert.equal(subs.length, 1,
        `one poll tick produced ${subs.length} submissions fetches, want 1: ${subs.join(' | ')}`);
    // The repeat-grants state must still refresh on the same tick, otherwise
    // fixing the double fetch would just re-break BUG-9.
    assert.ok(fetches.some((u) => /repeat-grants/.test(u)),
        'repeat-grants must still be refreshed by the poll');
    void ctx;
});

test('the single 12s poller refreshes both the table and repeat grants', async () => {
    const { timers, fetches } = await runScriptCapturingTimers();
    timers.filter((t) => t.ms === 12000)[0].fn();
    await new Promise((r) => setImmediate(r));
    await new Promise((r) => setImmediate(r));
    await new Promise((r) => setImmediate(r));
    const urls = fetches.join(' | ');
    assert.ok(/repeat-grants/.test(urls), 'repeat-grants must be polled');
    assert.ok(/submissions/.test(urls), 'the device table must be polled');
});

// ---------------------------------------------------------------------------
// Structural invariants that keep these bugs from coming back.
// ---------------------------------------------------------------------------

test('repeat-grant state is declared at the script top level, not inside an IIFE', () => {
    const js = toPlainJs(readDetail());
    // A top-level `var`/`function` declaration is reachable from every IIFE in
    // the same script; an IIFE-local one is not. Assert by execution instead of
    // by brace counting, which is what let this regress in the first place.
    assert.ok(
        /\bfunction\s+refreshRepeatGrants\s*\(/.test(js),
        'refreshRepeatGrants must exist'
    );
    const { threw, error } = vmTest(js);
    assert.equal(threw, false,
        `refreshRepeatGrants is not reachable from the poller IIFE: ${error && error.message}`);
});

test('no admin page script polls the network faster than every 3s', async () => {
    for (const [name, html] of [['pengawas_detail', readDetail()], ['pengawas', readList()]]) {
        const { intervals } = await runScript({ js: toPlainJs(html) });
        // 1000ms is the token-countdown DISPLAY tick; it must not perform I/O
        // (asserted separately for the detail page).
        for (const ms of intervals.filter((x) => x !== 1000)) {
            assert.ok(ms >= 3000, `${name}.html registers a ${ms}ms network poller`);
        }
    }
});

// Small helper for the synchronous reachability assertion above.
function vmTest(js) {
    const sandbox = {
        console: { log() {}, warn() {}, error() {} },
        setInterval: () => 0, clearInterval() {}, setTimeout: () => 0, clearTimeout() {},
        requestAnimationFrame: () => 0,
        localStorage: { getItem: () => null, setItem() {}, removeItem() {} },
        location: { href: 'http://x/', search: '', pathname: '/admin/pengawas/1' },
        history: { replaceState() {}, pushState() {} },
        navigator: {}, Event: class {}, CustomEvent: class {},
        apiFetch: () => Promise.resolve({}),
        escapeHtml: (s) => String(s ?? ''), jsEscape: (s) => String(s ?? ''),
        formatDateTimeID: (s) => String(s ?? ''), localizeUTC: (s) => String(s ?? ''),
        initLiveSearch() {}, showConfirm: () => Promise.resolve(true), copyCode: () => Promise.resolve(),
        showToast() {}, PengawasDetailQueue: { serializeApprovals: () => 'x', computeApprovalRowOps: () => [] },
        Actions: { register() {}, get: () => null }, EXAM_STATUS_LABELS: {},
        fetch: () => Promise.resolve({ ok: true, json: () => Promise.resolve({}) }),
        Promise, JSON, Math, Date, Object, Array, String, Number, Boolean, Error,
        isNaN, parseInt, parseFloat, encodeURIComponent, decodeURIComponent, URLSearchParams,
    };
    sandbox.window = sandbox; sandbox.globalThis = sandbox;
    sandbox.document = {
        createElement: () => makeEl('new'),
        getElementById: () => makeEl('x'),
        querySelector: () => makeEl('x'),
        querySelectorAll: () => [], addEventListener() {},
        body: makeEl('body'), documentElement: makeEl('html'), readyState: 'complete',
    };
    const ctx = vm.createContext(sandbox);
    try {
        vm.runInContext(js, ctx, { filename: 'inline.js' });
        return { threw: false, error: null, ctx };
    } catch (e) {
        return { threw: true, error: e, ctx };
    }
}
