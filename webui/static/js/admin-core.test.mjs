/* Unit tests for the apiFetch interceptor + the global 'api:error' listener in
 * admin-core.js.
 *
 * Run with:  node --test static/js/admin-core.test.mjs   (from webui/)
 *
 * admin-core.js is browser-oriented, so the REAL shipped file is executed
 * inside a Node vm context with a minimal DOM/globals mock (document,
 * window.fetch, CustomEvent, MutationObserver, timers). This keeps the tests
 * honest: they exercise the actual source, not a copy.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const ADMIN_CORE_SRC = fs.readFileSync(path.join(__dirname, 'admin-core.js'), 'utf8');

// --- test harness -----------------------------------------------------------

function makeResponse({ ok, status, body = '', url = 'https://examvan.test/admin/api/x' }) {
    return {
        ok,
        status,
        statusText: ok ? 'OK' : 'Internal Server Error',
        url,
        headers: {},
        text: () => Promise.resolve(body)
    };
}

class FakeCustomEvent {
    constructor(type, opts = {}) {
        this.type = type;
        this.detail = (opts && opts.detail) || null;
    }
}

function fakeElement() {
    return {
        className: '',
        classList: { add() {}, remove() {}, contains() { return false; } },
        setAttribute() {},
        addEventListener() {},
        getAttribute() { return null; },
        innerHTML: '',
        textContent: '',
        querySelector() { return null; },
        querySelectorAll() { return []; },
        appendChild() {},
        remove() {},
        style: {},
        offsetHeight: 0
    };
}

// Loads the REAL admin-core.js inside a sandboxed vm context and returns
// handles for testing. fetchImpl is the stub for window.fetch.
//
// NOTE: only top-level FUNCTION declarations (apiFetch, showApiErrorToast,
// apiErrorMessage, ...) become reachable properties on `sandbox` — top-level
// const/let bindings (API_ERROR_MESSAGES, TOAST_ICONS, ...) stay inside the
// vm script scope and are NOT reachable from the test.
function loadAdminCore(fetchImpl) {
    const dispatched = [];          // every event sent through document.dispatchEvent
    const listeners = new Map();    // document.addEventListener registry
    const durations = [];           // setTimeout durations captured (toast display times)
    const toastContainer = { children: [], firstElementChild: null, appendChild(el) { this.children.push(el); } };

    const documentMock = {
        readyState: 'complete',
        addEventListener(type, fn) {
            if (!listeners.has(type)) listeners.set(type, []);
            listeners.get(type).push(fn);
        },
        dispatchEvent(ev) {
            dispatched.push(ev);
            (listeners.get(ev.type) || []).slice().forEach((fn) => fn(ev));
            return true;
        },
        // getElementById returns null (avoids triggering the top-level
        // refreshDashboardStats call on the real page); querySelector is
        // null-SAFE (returns a no-op element) so a future top-level
        // document.querySelector(...).addEventListener(...) in admin-core.js
        // can't crash the whole suite during vm.runInContext.
        getElementById(id) { return id === 'toastContainer' ? toastContainer : null; },
        querySelector(sel) {
            return sel === 'meta[name="csrf-token"]' ? { getAttribute: () => 'test-csrf-token' } : fakeElement();
        },
        querySelectorAll() { return []; },
        createElement() { return fakeElement(); },
        documentElement: {},
        body: { classList: { add() {}, remove() {}, contains() { return false; } } },
        contains() { return true; }
    };

    function MutationObserverMock(cb) { this.cb = cb; }
    MutationObserverMock.prototype.observe = function () {};
    MutationObserverMock.prototype.disconnect = function () {};
    function MouseEventMock(type) { this.type = type; }

    const sandbox = {
        window: { fetch: fetchImpl, CustomEvent: FakeCustomEvent },
        document: documentMock,
        CustomEvent: FakeCustomEvent,
        MutationObserver: MutationObserverMock,
        getComputedStyle: () => ({ display: 'block' }),
        MouseEvent: MouseEventMock,
        navigator: {},
        console,
        setTimeout: (fn, ms) => { durations.push(ms); return 0; },
        clearTimeout() {},
        setInterval() { return 0; },
        clearInterval() {},
        location: { href: '' }
    };
    sandbox.globalThis = sandbox;
    vm.createContext(sandbox);
    vm.runInContext(ADMIN_CORE_SRC, sandbox, { filename: 'admin-core.js' });

    return { sandbox, dispatched, durations, toastContainer };
}

// --- interceptor: normalization --------------------------------------------

test('2xx responses pass through untouched (no interception)', async () => {
    const okResp = makeResponse({ ok: true, status: 200, body: '{"success":true,"data":{}}' });
    const env = loadAdminCore(async () => okResp);

    const out = await env.sandbox.apiFetch('/admin/api/exams');
    assert.equal(out, okResp, 'callers must receive the real Response for 2xx');
    assert.equal(env.dispatched.length, 0, 'no api:error event for a 2xx response');
});

test('non-2xx JSON is normalized and dispatched with error_code', async () => {
    const body = JSON.stringify({ success: false, error_code: 'R2_NOT_CONFIGURED', message: 'Cloudflare R2 tidak dikonfigurasi.' });
    const env = loadAdminCore(async () => makeResponse({ ok: false, status: 500, body }));

    const out = await env.sandbox.apiFetch('/admin/api/upload', { method: 'POST' });
    const data = await out.json();

    assert.equal(out.ok, false);
    assert.equal(out.status, 500);
    assert.equal(data.success, false);
    assert.equal(data.error_code, 'R2_NOT_CONFIGURED');
    assert.equal(data.message, 'Cloudflare R2 tidak dikonfigurasi.');
    assert.equal(await out.text(), body, 'wrapper keeps .text() passthrough');

    const ev = env.dispatched.find((e) => e.type === 'api:error');
    assert.ok(ev, 'api:error event must be dispatched');
    assert.equal(ev.detail.error_code, 'R2_NOT_CONFIGURED');
    assert.equal(ev.detail.status, 500);
    assert.equal(ev.detail.suppressed, false);
});

// --- global listener: auto-toast -------------------------------------------

test('R2_NOT_CONFIGURED is auto-toasted with warning style + 12s duration', async () => {
    const body = JSON.stringify({ success: false, error_code: 'R2_NOT_CONFIGURED', message: 'Cloudflare R2 tidak dikonfigurasi.' });
    const env = loadAdminCore(async () => makeResponse({ ok: false, status: 500, body }));

    await env.sandbox.apiFetch('/admin/api/upload', { method: 'POST' });

    const toast = env.toastContainer.children[0];
    assert.ok(toast, 'global listener must auto-toast an unhandled R2 error');
    assert.ok(toast.className.includes('toast-warning'));
    assert.ok(toast.className.includes('toast-r2-config'));
    assert.ok(toast.innerHTML.includes('Cloudflare R2 belum dikonfigurasi'), 'uses the friendly mapped message');
    assert.ok(env.durations.includes(12000), 'config toasts get the extended 12s duration');
});

test('suppressApiErrorToast suppresses the auto-toast but still normalizes', async () => {
    const body = JSON.stringify({ success: false, error_code: 'R2_NOT_CONFIGURED', message: 'Cloudflare R2 tidak dikonfigurasi.' });
    const env = loadAdminCore(async () => makeResponse({ ok: false, status: 500, body }));

    const out = await env.sandbox.apiFetch('/admin/api/system-apps/5/delete', { method: 'POST', suppressApiErrorToast: true });
    const data = await out.json();

    assert.equal(data.error_code, 'R2_NOT_CONFIGURED', 'data is still normalized for the caller');
    const ev = env.dispatched.find((e) => e.type === 'api:error');
    assert.equal(ev.detail.suppressed, true, 'event carries the suppressed flag');
    assert.equal(env.toastContainer.children.length, 0, 'no auto-toast when the call site handles it');
});

test('non-R2 error codes are NOT auto-toasted (caller handles them)', async () => {
    const body = JSON.stringify({ success: false, error_code: 'SOMETHING_ELSE', message: 'boo' });
    const env = loadAdminCore(async () => makeResponse({ ok: false, status: 400, body }));

    const data = await (await env.sandbox.apiFetch('/admin/api/vouchers')).json();

    assert.equal(data.success, false);
    assert.equal(data.error_code, 'SOMETHING_ELSE');
    assert.equal(env.toastContainer.children.length, 0, 'listener only toasts mapped R2 codes');
});

// --- non-JSON / empty bodies ------------------------------------------------

test('non-JSON and empty error bodies resolve to {success:false}', async () => {
    for (const body of ['<html><body>Internal Server Error</body></html>', '']) {
        const env = loadAdminCore(async () => makeResponse({ ok: false, status: 500, body }));
        const data = await (await env.sandbox.apiFetch('/admin/api/exams/1/delete')).json();

        // Spread into a test-realm object: vm-realm objects have a different
        // prototype, which would make deepStrictEqual complain about identity.
        assert.deepEqual({ ...data }, { success: false }, `body=${JSON.stringify(body)} normalizes to {success:false}`);
        const ev = env.dispatched.find((e) => e.type === 'api:error');
        assert.equal(ev.detail.error_code, null);
        assert.equal(ev.detail.message, null);
        assert.equal(env.toastContainer.children.length, 0, 'no auto-toast without a mapped error_code');
    }
});

// --- CSRF -------------------------------------------------------------------

test('CSRF token is injected for mutating methods only', async () => {
    let lastOpts = null;
    const env = loadAdminCore(async (url, opts) => { lastOpts = opts; return makeResponse({ ok: true, status: 200 }); });

    await env.sandbox.apiFetch('/admin/api/exams/1/toggle', { method: 'POST' });
    assert.equal(lastOpts.headers['X-CSRF-Token'], 'test-csrf-token');

    await env.sandbox.apiFetch('/admin/api/exams');
    assert.equal(lastOpts.headers, undefined, 'GET must not get the CSRF header');
});

// --- message resolution helpers ---------------------------------------------

test('apiErrorMessage prefers mapped code, then server message, then fallback', () => {
    const env = loadAdminCore(async () => makeResponse({ ok: true, status: 200 }));
    const apiErrorMessage = env.sandbox.apiErrorMessage;

    assert.equal(
        apiErrorMessage({ error_code: 'R2_NOT_CONFIGURED', message: 'pesan server' }, 'fallback'),
        'Cloudflare R2 belum dikonfigurasi di server. Hubungi administrator untuk mengisi kredensial R2.',
        'mapped error_code wins over the server message'
    );
    assert.equal(
        apiErrorMessage({ error_code: 'UNKNOWN_CODE', message: 'pesan server' }, 'fallback'),
        'pesan server',
        'unmapped code falls back to the server message'
    );
    assert.equal(apiErrorMessage(null, 'fallback'), 'fallback');
    assert.equal(apiErrorMessage({ message: '' }, 'fallback'), 'fallback');
});

