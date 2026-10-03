/* Suite Batch 40 — tombol "Ekspor Excel" per-ujian di halaman Rekapitulasi.
 *
 * Run with:  node --test static/js/uiux-batch40-export-submission.test.mjs   (from webui/)
 *
 * Latar belakang & dampak bisnis:
 *   Bug server-side: gerbang export ?exam_id=<N> dulu memakai
 *   checkExamOwnership (perbandingan instansi byte-exact) sementara halaman
 *   menampilkan ujian itu lewat UserCanAccessExam (instansi_id kanonik +
 *   fallback nama case-insensitive). Operator se-instansi sah bisa MEMILIH
 *   ujian di dropdown lalu kena 403 "Akses ditolak" saat mengunduh.
 *
 *   Perbaikan server sudah diuji di Go. Suite ini mengunci sisi UI-nya:
 *   tombol harus mengirim `exam_id` yang dipilih (bukan selalu ekspor-semua),
 *   menampilkan error server apa adanya (bukan JSON mentah), dan memulihkan
 *   tombol di semua jalur — supaya regresi tidak kembali lewat template/JS.
 *
 * Pola sama dengan suite lain: kontrak statik (fs-read) + perilaku via
 * vm.runInNewContext mengeksekusi fungsi ASLI dari admin.js dengan stub DOM.
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

const ADMIN_JS_SRC = read('static/js/admin.js');
const SUBMISSIONS_HTML = read('templates/admin/submissions.html');

/** Ekstrak sumber deklarasi `function name(...) {...}` dengan penghitungan kurawal. */
function extractFunction(src, name) {
    const start = src.indexOf('function ' + name + '(');
    if (start === -1) return null;
    const open = src.indexOf('{', start);
    let depth = 0;
    for (let j = open; j < src.length; j++) {
        if (src[j] === '{') depth++;
        else if (src[j] === '}') {
            depth--;
            if (depth === 0) return src.slice(start, j + 1);
        }
    }
    return null;
}

const flush = () => new Promise((r) => setTimeout(r, 20));
const EXPORT_FN = extractFunction(ADMIN_JS_SRC, 'exportSubmissions');

// ---------------------------------------------------------------------------
// Kontrak statik: template + fungsi
// ---------------------------------------------------------------------------

test('statik: tombol ekspor & dropdown filter terpasang dengan kontrak yang benar', () => {
    assert.ok(SUBMISSIONS_HTML.includes('data-action="export-submissions"'),
        'tombol "Ekspor Excel" memakai data-action (delegasi CSP-safe)');
    assert.ok(SUBMISSIONS_HTML.includes('id="filterExam"'),
        'dropdown filter ujian punya id #filterExam');
    assert.match(SUBMISSIONS_HTML, /Ekspor Excel/, 'label tombol Bahasa Indonesia');
});

test('statik: exportSubmissions mengirim exam_id dari #filterExam hanya bila dipilih', () => {
    assert.ok(EXPORT_FN, 'exportSubmissions ada di admin.js');
    assert.match(EXPORT_FN, /getElementById\('filterExam'\)/, 'membaca dropdown #filterExam');
    assert.match(EXPORT_FN, /if\s*\(examId\)\s*\{[\s\S]*?exam_id=/, 'exam_id dilampirkan hanya saat ada pilihan');
    assert.match(EXPORT_FN, /tz_offset=/, 'offset zona waktu browser ikut dikirim');
});

// ---------------------------------------------------------------------------
// Perilaku via vm dengan stub DOM
// ---------------------------------------------------------------------------

function makeExportSandbox(opts = {}) {
    let anchorClicks = 0;
    const anchor = {
        href: '', download: '', style: {},
        click() { anchorClicks++; },
        remove() {}
    };
    const btn = {
        disabled: false,
        innerHTML: '<svg></svg> Ekspor Excel',
        attrs: {},
        setAttribute(k, v) { this.attrs[k] = String(v); },
        getAttribute(k) { return Object.prototype.hasOwnProperty.call(this.attrs, k) ? this.attrs[k] : null; },
        removeAttribute(k) { delete this.attrs[k]; }
    };
    const sandbox = {
        document: {
            getElementById(id) {
                if (id === 'filterExam') return { value: opts.examId == null ? '' : String(opts.examId) };
                if (id === 'exportBtn') return opts.withBtn === false ? null : btn;
                return null;
            },
            querySelector() { return opts.fallbackBtn ? btn : null; },
            createElement(tag) { return tag === 'a' ? anchor : {}; },
            body: { appendChild() {}, removeChild() {} }
        },
        apiFetchCalls: [],
        toasts: [],
        getAnchor: () => anchor,
        getAnchorClicks: () => anchorClicks,
        getBtn: () => btn,
        Date,
        Promise,
        Object,
        Error,
        decodeURIComponent,
        URL: { createObjectURL: () => 'blob:x', revokeObjectURL() {} }
    };
    sandbox.showToast = (msg, type) => { sandbox.toasts.push({ msg, type }); };
    sandbox.apiFetch = (url) => {
        sandbox.apiFetchCalls.push(url);
        return opts.api(url);
    };
    return sandbox;
}

test('perilaku: ujian spesifik → URL memuat &exam_id=<N> dan file terunduh sekali', async () => {
    const sandbox = makeExportSandbox({
        examId: 7,
        api: () => Promise.resolve({
            ok: true,
            blob: () => Promise.resolve({ size: 42 }),
            headers: { get: () => 'attachment; filename="Hasil_Ujian_Matematika.xlsx"' }
        })
    });
    vm.createContext(sandbox);
    vm.runInContext(EXPORT_FN, sandbox, { filename: 'admin.js#b40-specific' });

    sandbox.exportSubmissions();
    await flush();
    await flush();

    assert.deepEqual(sandbox.apiFetchCalls, [
        '/admin/api/submissions/export?tz_offset=' + new Date().getTimezoneOffset() + '&exam_id=7'
    ], 'exam_id yang dipilih diteruskan ke server');
    assert.equal(sandbox.getAnchorClicks(), 1, 'unduhan tepat sekali');
    assert.equal(sandbox.getAnchor().download, 'Hasil_Ujian_Matematika.xlsx',
        'nama file dari Content-Disposition server');
    assert.equal(sandbox.getBtn().disabled, false, 'tombol pulih aktif');
});

test('perilaku: "Semua Ujian" (tanpa pilihan) → URL TANPA exam_id', async () => {
    const sandbox = makeExportSandbox({
        examId: '',
        api: () => Promise.resolve({
            ok: true,
            blob: () => Promise.resolve({}),
            headers: { get: () => null }
        })
    });
    vm.createContext(sandbox);
    vm.runInContext(EXPORT_FN, sandbox, { filename: 'admin.js#b40-all' });

    sandbox.exportSubmissions();
    await flush();
    await flush();

    assert.deepEqual(sandbox.apiFetchCalls, [
        '/admin/api/submissions/export?tz_offset=' + new Date().getTimezoneOffset()
    ]);
    assert.doesNotMatch(sandbox.apiFetchCalls[0], /exam_id=/, 'ekspor-semua tidak menyertakan exam_id');
});

test('perilaku: 403 server (cross-tenant) → toast pesan server, tanpa unduhan, tombol pulih', async () => {
    const sandbox = makeExportSandbox({
        examId: 999,
        api: () => Promise.resolve({
            ok: false,
            status: 403,
            json: () => Promise.resolve({ success: false, message: 'Akses ditolak' })
        })
    });
    vm.createContext(sandbox);
    vm.runInContext(EXPORT_FN, sandbox, { filename: 'admin.js#b40-403' });

    sandbox.exportSubmissions();
    await flush();
    await flush();

    assert.equal(sandbox.getAnchorClicks(), 0, 'tidak ada unduhan saat gagal');
    assert.equal(sandbox.toasts.length, 1, 'satu toast error');
    assert.equal(sandbox.toasts[0].type, 'error');
    assert.equal(sandbox.toasts[0].msg, 'Akses ditolak', 'pesan server ditampilkan apa adanya');
    assert.equal(sandbox.getBtn().disabled, false, 'tombol pulih setelah 403');
});
