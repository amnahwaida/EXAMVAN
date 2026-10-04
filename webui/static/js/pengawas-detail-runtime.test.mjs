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
    // innerHTML → textContent: the real DOM derives the flattened text from the
    // parsed markup. Without that, any assertion reading textContent after the
    // code sets innerHTML sees '' and the stub silently hides the code's output.
    const el = {
        id, textContent: '', value: '', dataset: {}, style: {},
        classList: { add() {}, remove() {}, toggle() {}, contains: () => false },
        addEventListener() {}, removeEventListener() {}, appendChild() {},
        removeChild() {}, querySelector: () => null, querySelectorAll: () => [],
        setAttribute() {}, getAttribute: () => null, remove() {}, closest: () => null,
        focus() {}, click() {}, contains: () => false, children: [], childNodes: [],
        parentElement: null,
    };
    let html = '';
    Object.defineProperty(el, 'innerHTML', {
        get() { return html; },
        set(v) {
            html = String(v == null ? '' : v);
            el.textContent = html
                .replace(/<[^>]*>/g, ' ')
                .replace(/&middot;|&ndash;|&raquo;|&laquo;|&lsaquo;|&rsaquo;|&quot;/g, ' ')
                .replace(/\s+/g, ' ')
                .trim();
        },
    });
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

/**
 * Like runScript, but captures setTimeout callbacks so a test can advance
 * time. Needed for the request-watchdog contract: the bug only shows once a
 * request has been in flight longer than the timeout.
 */
async function runScriptWithTimeout({ js, payload = {}, hang = false, preset = {} }) {
    const timeouts = [];
    const fetches = [];
    const els = {};
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
    const json = () => Promise.resolve(payload);
    const sandbox = {
        console: { log() {}, warn() {}, error() {} },
        setInterval: () => 0, clearInterval() {},
        setTimeout: (fn, ms) => { timeouts.push({ fn, ms }); return timeouts.length; },
        clearTimeout() {},
        fetch: (u) => { fetches.push(String(u)); return Promise.resolve({ ok: true, json }); },
        requestAnimationFrame: () => 0,
        localStorage: { getItem: () => null, setItem() {}, removeItem() {} },
        location: { href: 'http://x/', search: '', pathname: '/admin/pengawas/1' },
        history: { replaceState() {}, pushState() {} },
        navigator: { clipboard: { writeText: () => Promise.resolve() } },
        Event: class {}, CustomEvent: class {},
        apiFetch: (u) => {
            fetches.push(String(u));
            if (hang) return new Promise(() => {}); // never settles
            return Promise.resolve({ ok: true, status: 200, json });
        },
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
    vm.runInContext(js, ctx, { filename: 'pd.inline.js' });
    for (let i = 0; i < 8; i++) await new Promise((r) => setImmediate(r));
    return { timeouts, fetches, els, sandbox, ctx };
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

test('poller cadence: approvals + countdown only, no device-table network poller', async () => {
    const { intervals } = await runScript({ js: toPlainJs(readDetail()) });
    // 1s = countdown DISPLAY tick (no I/O — asserted separately).
    assert.ok(intervals.includes(1000), `expected the 1s countdown tick, got ${intervals}`);
    // The approval queue keeps its 5s poll: it is the widget that genuinely
    // waits on the supervisor and it hits a cheap endpoint.
    assert.ok(intervals.includes(5000), `expected the 5s approval poll, got ${intervals}`);
    // No slow network poller any more: hot reload on the device table was
    // removed on request because 10 supervisors meant 50 requests/minute
    // against the heaviest endpoint on the page.
    for (const ms of intervals.filter((x) => x !== 1000 && x !== 5000)) {
        assert.fail(`unexpected extra poller at ${ms}ms — ${intervals}`);
    }
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

test('BUG-9: repeat-grant state is re-read by the manual Muat Ulang, not only on load', () => {
    const js = toPlainJs(readDetail());
    // Auto-poll is gone, so the toolbar refresh is the supervisor's only path to
    // fresh data. It must therefore also re-read the repeat-grant map —
    // otherwise a grant made by another supervisor stays stale until a full
    // page reload, and the Izinkan/Cabut Izin buttons assert something false.
    const at = js.indexOf("Actions.register('load-detail'");
    assert.ok(at > 0, 'load-detail action not found');
    const block = js.slice(at, at + 700);
    assert.ok(
        /refreshRepeatGrants/.test(block),
        'the Muat Ulang action must refresh repeat grants: ' + block.slice(0, 260)
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
// The device table got wedged on "Memuat data..." forever, and must not be
// polled automatically any more.
// ---------------------------------------------------------------------------

test('a hung request cannot wedge the table on "Memuat data..." forever', async () => {
    // Reproduced: one request that never settles left detailLoading=true, so
    // EVERY later call (filter change, refresh, and even the 12s poll) just set
    // detailRerunPending and returned. The table was stuck on "Memuat data..."
    // with no way out but a full page reload.
    const { els, timeouts, ctx } = await runScriptWithTimeout({
        js: toPlainJs(readDetail()),
        payload: detailPayload(),
        hang: true,
        preset: { statusFilter: { value: 'in_progress' } },
    });
    vm.runInContext('loadDetail(1)', ctx);
    for (let i = 0; i < 8; i++) await new Promise((r) => setImmediate(r));
    assert.ok(/Memuat data/.test(els.submissionBody.innerHTML),
        'precondition: the non-silent load painted the loading row');

    // Fire the watchdog timers the page armed.
    for (const t of timeouts) t.fn();
    for (let i = 0; i < 8; i++) await new Promise((r) => setImmediate(r));

    assert.ok(
        !/Memuat data/.test(els.submissionBody.innerHTML),
        'after the timeout the stuck row must offer a way forward instead of ' +
        'spinning forever: ' + els.submissionBody.innerHTML.slice(0, 220)
    );
    assert.ok(
        /data-action="load-detail"/.test(els.submissionBody.innerHTML),
        'the stuck row must carry a retry control: ' + els.submissionBody.innerHTML.slice(0, 220)
    );
    assert.equal(vm.runInContext('detailLoading', ctx), false,
        'the in-flight guard must be released, or the next filter change is ' +
        'coalesced away exactly as before');
});

test('the stuck-row retry uses the existing Muat Ulang action', () => {
    const js = toPlainJs(readDetail());
    // Reuse the toolbar's registered action rather than inventing a second one,
    // so the retry and the toolbar button cannot drift apart.
    assert.ok(
        /data-action="load-detail"/.test(js),
        'the retry must reuse data-action="load-detail"'
    );
});

test('the device table is NOT polled automatically', async () => {
    // Hot reload on this table was removed on request: 10 supervisors on the
    // detail page meant 50 requests/minute against the heaviest endpoint,
    // ~6 queries each. The approval queue keeps its 5s poll — it is the widget
    // that genuinely waits on the supervisor and hits a cheap endpoint.
    //
    // The countdown is put into its normal (not-yet-due) state first: the
    // template placeholders flatten to a truthy-but-ancient timestamp under the
    // harness, which would make the countdown legitimately fire a token refresh
    // and mask what this test is about.
    const { timers, fetches, ctx } = await runScriptCapturingTimers();
    vm.runInContext(
        "TOKEN_MODE = 'dynamic'; TOKEN_INTERVAL_MINUTES = 5;" +
        "TOKEN_LAST_RESET = new Date().toISOString();", ctx);
    fetches.length = 0;

    for (const t of timers) t.fn();
    for (let i = 0; i < 10; i++) await new Promise((r) => setImmediate(r));

    // per_page=20 is the device-table page query (SUBS_PER_PAGE); per_page=1 is
    // the separate token refresh, which is latched and only fires when the
    // rotation window is actually due.
    const tablePolls = fetches.filter((u) => /submissions\?page=\d+&per_page=20/.test(u));
    assert.equal(tablePolls.length, 0,
        `firing every poller once produced ${tablePolls.length} device-table fetches — ` +
        'the table must not auto-poll: ' + tablePolls.join(' | '));
    assert.ok(
        fetches.some((u) => /approvals/.test(u)),
        'the approval queue must keep polling — that queue is what waits on the supervisor'
    );
    assert.ok(
        !timers.some((t) => t.ms >= 10000),
        `no slow network poller may remain, got ${timers.map((t) => t.ms)}`
    );
});

test('the device table shows the class so same-named students are distinct', async () => {
    // Two candidates can share a name across classes. The table showed only the
    // name, so they were indistinguishable — while the search DOES match class,
    // which makes it worse: you search "XII A", get results, and see nothing
    // that confirms the filter applied.
    const { els } = await runScript({
        js: toPlainJs(readDetail()),
        payload: detailPayload({
            submissions: [{
                ...SUBMISSION_ROW,
                student_name: 'Budi Santoso',
                student_class: 'XII A',
                exam_number: '07',
            }],
        }),
        callAfter: ['loadDetail(1)'],
    });
    const html = els.submissionBody.innerHTML;
    assert.ok(
        /XII A/.test(html),
        'the class must be visible in the row — otherwise two "Budi Santoso" ' +
        'from different classes are indistinguishable: ' + html.slice(0, 300)
    );
});

test('an active filter is announced and can be cleared in one click', async () => {
    // No indicator existed at all. The stat cards are deliberately filter-
    // independent (a stable exam-level header), so with a filter active the page
    // showed "Total Perangkat 12 / Terkumpul 3" above a table holding one row —
    // with nothing saying a filter was narrowing the view.
    const { els } = await runScript({
        js: toPlainJs(readDetail()),
        payload: detailPayload({
            submissions: [{ ...SUBMISSION_ROW, student_class: 'XII A' }],
            stats: { total: 12, active: 0, submitted: 3, not_started: 9 },
        }),
        callAfter: ['loadDetail(1)'],
        preset: {
            statusFilter: { value: 'not_started' },
            pengawasSearch: { value: 'budi' },
        },
    });
    const chip = els.activeFilterChip;
    assert.ok(chip, 'an active-filter chip element must exist');
    assert.equal(chip.style.display, 'block',
        'the chip must be visible whenever a filter is narrowing the view');
    assert.ok(/not_started|Terkumpul|Status/i.test(chip.textContent || ''),
        'the chip must name the active filter, got: ' + JSON.stringify(chip.textContent));
    assert.ok(
        /budi/.test(chip.textContent || ''),
        'the chip must show the active search text, got: ' + JSON.stringify(chip.textContent)
    );
    assert.ok(
        /data-action="clear-active-filters"/.test(chip.innerHTML),
        'the chip must offer a one-click way back to the full list: ' + chip.innerHTML
    );
});

test('no filter chip when nothing is filtered', async () => {
    const { els } = await runScript({
        js: toPlainJs(readDetail()),
        payload: detailPayload({ submissions: [{ ...SUBMISSION_ROW, student_class: 'XII A' }] }),
        callAfter: ['loadDetail(1)'],
        preset: { statusFilter: { value: '' }, pengawasSearch: { value: '' } },
    });
    assert.equal(els.activeFilterChip && els.activeFilterChip.style.display, 'none',
        'the chip must stay hidden when no filter is applied');
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

// ---------------------------------------------------------------------------
// The device table got wedged on "Memuat data..." forever, and must not be
// polled automatically any more.
// ---------------------------------------------------------------------------
