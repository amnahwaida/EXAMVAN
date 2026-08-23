/* Regression contract tests untuk Batch 3 perbaikan UI/UX — T8.
 * Referensi temuan: review_uiux_webui.md (ID: T8; positif G8 dipertahankan).
 *
 * Run with:  node --test static/js/uiux-batch3-t8-polling.test.mjs   (from webui/)
 *
 * T8: polling antrean izin di pengawas_detail.html tiap 5 detik mengganti
 * SELURUH tbody via innerHTML tanpa membandingkan data — tap Izinkan/Tolak
 * sering mendarat di DOM yang baru diganti. Perbaikan:
 *   (1) skip bila payload identik dengan render sukses terakhir,
 *   (2) tunda render selama aksi Izinkan/Tolak in-flight,
 *   (3) update per-baris by mac_address, replace penuh hanya fallback.
 *
 * Dua jenis test:
 *   1. Perilaku — modul murni webui/static/js/pengawas-detail.js dieksekusi
 *      dalam Node vm (pola sama dengan uiux-batch1.test.mjs atas admin-core.js)
 *      untuk computeApprovalRowOps / serializeApprovals.
 *   2. Kontrak statik — template ASLI memakai modul itu dan membawa guard
 *      skip-if-same + defer-saat-loading + polling 5 detik + higiene G8.
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

// ---------------------------------------------------------------------------
// Muat modul murni pengawas-detail.js dalam vm (browser-global style)
// ---------------------------------------------------------------------------

function loadQueueModule() {
    const src = fs.readFileSync(path.join(__dirname, 'pengawas-detail.js'), 'utf8');
    const sandbox = {
        console: { debug() {}, log() {}, warn() {}, error() {} }
    };
    sandbox.window = sandbox;
    sandbox.globalThis = sandbox;
    vm.createContext(sandbox);
    vm.runInContext(src, sandbox, { filename: 'pengawas-detail.js' });
    const mod = sandbox.PengawasDetailQueue;
    // Nilai kembalian dari vm context berada di realm lain (Array.prototype
    // berbeda), membuat assert.deepEqual menganggapnya "not reference-equal"
    // meski strukturnya sama. Klon ke realm host agar perbandingan murni isi.
    const clone = (v) => (v && typeof v === 'object' ? JSON.parse(JSON.stringify(v)) : v);
    return {
        serializeApprovals: (...a) => mod.serializeApprovals(...a),
        computeApprovalRowOps: (...a) => clone(mod.computeApprovalRowOps(...a))
    };
}

// Item approval meniru bentuk payload server (internal/handlers/admin/pengawas.go):
// { mac_address, student_name, exam_number, student_class, identity_data, created_at, status }.
function item(mac, opts) {
    opts = opts || {};
    return {
        mac_address: mac,
        student_name: opts.name || 'Siswa ' + mac,
        exam_number: opts.examNumber || 'U-' + mac,
        student_class: opts.kelas || 'X-A',
        identity_data: opts.identity || { nis: '1234' },
        created_at: opts.at || '2026-08-23T07:00:00Z',
        status: 'pending'
    };
}

const MOD = loadQueueModule();

// ---------------------------------------------------------------------------
// Perilaku — serializeApprovals (fondasi guard skip-if-same)
// ---------------------------------------------------------------------------

test('T8/serialize: serialisasi deterministik — urutan panggilan sama hasil sama', () => {
    const list = [item('AA:01'), item('AA:02')];
    assert.equal(MOD.serializeApprovals(list), MOD.serializeApprovals(list.slice()));
});

test('T8/serialize: perubahan field yang TIDAK ditampilkan tidak mengubah serialisasi', () => {
    // identity_data / exam_number / student_class / status tidak dirender ke baris;
    // poll yang hanya berubah di sana HARUS dianggap identik (skip total).
    const a = [item('AA:01')];
    const b = [item('AA:01', { identity: { nis: 'berubah' }, examNumber: 'U-lain', kelas: 'X-B' })];
    assert.equal(MOD.serializeApprovals(a), MOD.serializeApprovals(b));
});

test('T8/serialize: perubahan field yang ditampilkan mengubah serialisasi', () => {
    const a = [item('AA:01')];
    assert.notEqual(MOD.serializeApprovals(a), MOD.serializeApprovals([item('AA:01', { name: 'Nama lain' })]));
    assert.notEqual(MOD.serializeApprovals(a), MOD.serializeApprovals([item('AA:01', { at: '2026-08-23T07:05:00Z' })]));
    assert.notEqual(MOD.serializeApprovals(a), MOD.serializeApprovals([item('AA:02')]), 'mac berbeda = isi berbeda');
});

test('T8/serialize: urutan list berpengaruh — nomor urut ("No") bagian dari tampilan', () => {
    const a = [item('AA:01'), item('AA:02')];
    const b = [item('AA:02'), item('AA:01')];
    assert.notEqual(MOD.serializeApprovals(a), MOD.serializeApprovals(b));
});

test('T8/serialize: list kosong punya serialisasi stabil & beda dari list berisi', () => {
    assert.equal(MOD.serializeApprovals([]), MOD.serializeApprovals([]));
    assert.notEqual(MOD.serializeApprovals([]), MOD.serializeApprovals([item('AA:01')]));
});

// ---------------------------------------------------------------------------
// Perilaku — computeApprovalRowOps (diff by mac_address)
// ---------------------------------------------------------------------------

test('T8/diff: payload identik -> type "none", nol operasi', () => {
    const prev = [item('AA:01'), item('AA:02')];
    const res = MOD.computeApprovalRowOps(prev, prev.map((x) => ({ ...x })));
    assert.equal(res.type, 'none');
    assert.ok(!res.ops || res.ops.length === 0, 'tidak boleh ada operasi');
});

test('T8/diff: device baru masuk -> tepat satu operasi add dengan id & index benar', () => {
    const prev = [item('AA:01'), item('AA:03')];
    const next = [item('AA:01'), item('AA:02'), item('AA:03')];
    const res = MOD.computeApprovalRowOps(prev, next);
    assert.equal(res.type, 'ops');
    assert.equal(res.ops.length, 1);
    assert.equal(res.ops[0].op, 'add');
    assert.equal(res.ops[0].id, 'AA:02');
    assert.equal(res.ops[0].index, 1);
    assert.deepEqual(Object.keys(res.ops[0].item).sort(), Object.keys(item('AA:02')).sort(), 'op add membawa item lengkap');
});

test('T8/diff: device disetujui di tempat lain (hilang) -> operasi remove', () => {
    const prev = [item('AA:01'), item('AA:02'), item('AA:03')];
    const next = [item('AA:01'), item('AA:03')];
    const res = MOD.computeApprovalRowOps(prev, next);
    assert.equal(res.type, 'ops');
    assert.deepEqual(res.ops.map((o) => o.op + ':' + o.id), ['remove:AA:02']);
});

test('T8/diff: isi baris berubah (nama siswa) -> update in-place hanya baris itu', () => {
    const prev = [item('AA:01'), item('AA:02', { name: 'Lama' })];
    const next = [item('AA:01'), item('AA:02', { name: 'Baru' })];
    const res = MOD.computeApprovalRowOps(prev, next);
    assert.equal(res.type, 'ops');
    assert.equal(res.ops.length, 1);
    assert.equal(res.ops[0].op, 'update');
    assert.equal(res.ops[0].id, 'AA:02');
    assert.equal(res.ops[0].index, 1);
    assert.equal(res.ops[0].item.student_name, 'Baru');
});

test('T8/diff: reorder set sama -> tetap bisa dipetakan per baris (bukan replace)', () => {
    // Server mengurutkan created_at ASC; reorder jarang tapi mungkin.
    const prev = [item('AA:01'), item('AA:02'), item('AA:03')];
    const next = [item('AA:03'), item('AA:01'), item('AA:02')];
    const res = MOD.computeApprovalRowOps(prev, next);
    assert.equal(res.type, 'ops', 'reorder harus dipetakan sebagai operasi per baris');
    const updatedIdx = res.ops.filter((o) => o.op === 'update').map((o) => o.index).sort();
    assert.deepEqual(updatedIdx, [0, 1, 2], 'ketiga posisi bergeser -> ketiganya update dengan index final');
});

test('T8/diff: kosong <-> berisi dipetakan sebagai add/remove, bukan replace', () => {
    const filled = [item('AA:01'), item('AA:02')];

    const toFilled = MOD.computeApprovalRowOps([], filled);
    assert.equal(toFilled.type, 'ops');
    assert.deepEqual(
        toFilled.ops.filter((o) => o.op === 'add').map((o) => [o.id, o.index]),
        [['AA:01', 0], ['AA:02', 1]]
    );

    const toEmpty = MOD.computeApprovalRowOps(filled, []);
    assert.equal(toEmpty.type, 'ops');
    assert.equal(toEmpty.ops.length, 2);
    assert.ok(toEmpty.ops.every((o) => o.op === 'remove'));
});

test('T8/diff: campuran add+remove+update dalam satu poll', () => {
    const prev = [item('AA:01'), item('AA:02', { name: 'Lama' }), item('AA:03')];
    const next = [item('AA:04'), item('AA:02', { name: 'Baru' }), item('AA:05')];
    const res = MOD.computeApprovalRowOps(prev, next);
    assert.equal(res.type, 'ops');
    const kinds = res.ops.map((o) => o.op).sort().join(',');
    assert.equal(kinds, 'add,add,remove,remove,update', '2 remove (AA:01, AA:03) + 1 update (AA:02) + 2 add (AA:04, AA:05)');
});

test('T8/diff: kontrak urutan operasi — remove dulu, lalu update, lalu add naik by index', () => {
    // Urutan ini yang membuat penerapan DOM sederhana: hapus dulu, sisipkan add
    // secara ascending relatif ke posisi akhir.
    const prev = [item('AA:01'), item('AA:02'), item('AA:03'), item('AA:04')];
    const next = [item('AA:05'), item('AA:02', { name: 'X' }), item('AA:06'), item('AA:07')];
    const res = MOD.computeApprovalRowOps(prev, next);
    assert.equal(res.type, 'ops');

    const rank = { remove: 0, update: 1, add: 2 };
    const ranks = res.ops.map((o) => rank[o.op]);
    assert.deepEqual(ranks, [...ranks].sort((a, b) => a - b), 'semua remove < update < add');

    const addIndexes = res.ops.filter((o) => o.op === 'add').map((o) => o.index);
    assert.deepEqual(addIndexes, [...addIndexes].sort((a, b) => a - b), 'add terurut index menaik');
    for (const op of res.ops) {
        if (op.op !== 'remove') {
            assert.ok(op.index >= 0 && op.index < next.length, 'index add/update mengacu posisi di nextList');
            assert.equal(op.item.mac_address, op.id, 'item konsisten dengan id');
        }
    }
});

test('T8/diff: data tak terpetakan (mac hilang / duplikat) -> fallback replace', () => {
    assert.equal(MOD.computeApprovalRowOps([item('AA:01')], [{ ...item('AA:02'), mac_address: '' }]).type, 'replace');
    assert.equal(MOD.computeApprovalRowOps([item('AA:01'), item('AA:01')], [item('AA:02')]).type, 'replace');
    assert.equal(MOD.computeApprovalRowOps([item('AA:01')], [item('AA:02'), item('AA:02')]).type, 'replace');
});

// ---------------------------------------------------------------------------
// Kontrak statik — template memakai modul + guards + higiene G8
// ---------------------------------------------------------------------------

const html = read('templates/admin/pengawas_detail.html');

test('T8/static: pengawas-detail.js dimuat dengan cache-buster sebelum script inline', () => {
    const tagRe = /<script src="\/static\/js\/pengawas-detail\.js\?v=\{\{\.version\}\}"><\/script>/;
    assert.match(html, tagRe, 'modul harus dimuat ala halaman admin lain (?v={{.version}})');
    const tagIdx = html.search(tagRe);
    const inlineIdx = html.indexOf('function loadApprovals');
    assert.ok(tagIdx >= 0 && inlineIdx > tagIdx, 'modul harus terload SEBELUM script inline pemakainya');
});

test('T8/static: inline script memanggil serializeApprovals + computeApprovalRowOps', () => {
    assert.match(html, /PengawasDetailQueue\.serializeApprovals\(/, 'snapshot payload sukses lewat modul');
    assert.match(html, /PengawasDetailQueue\.computeApprovalRowOps\(/, 'diff per-baris lewat modul');
});

test('T8/static: guard skip-if-same — simpan snapshot render sukses & bandingkan SEBELUM menulis tbody', () => {
    // Variabel snapshot render sukses terakhir.
    assert.match(html, /var\s+approvalLastSerialized\s*=/, 'simpan serialisasi render sukses terakhir');
    assert.match(html, /var\s+approvalRowsLive\s*=/, 'penanda tbody sedang berisi baris data (bukan notice)');

    const cmpIdx = html.search(/approvalLastSerialized\s*===\s*\w+|\w+\s*===\s*approvalLastSerialized/);
    assert.ok(cmpIdx >= 0, 'ada perbandingan payload vs snapshot');
    const firstInnerHtml = html.indexOf('tbody.innerHTML');
    assert.ok(firstInnerHtml >= 0);
    const compareInLoadApprovals = html.indexOf('approvalLastSerialized', html.indexOf('function loadApprovals'));
    assert.ok(compareInLoadApprovals >= 0, 'perbandingan dilakukan di dalam loadApprovals');
    // Guard skip harus dievaluasi sebelum penulisan DOM pertama pada jalur sukses.
    const successBranch = html.slice(html.indexOf('function loadApprovals'));
    const skipGuardPos = successBranch.search(/approvalRowsLive\s*&&/);
    const emptyNoticePos = successBranch.indexOf('Belum ada antrean peserta saat ini');
    assert.ok(skipGuardPos >= 0 && emptyNoticePos > skipGuardPos, 'cek skip-if-same mendahului semua penulisan notice/data');
});

test('T8/static: snapshot divalidasi saat tbody ditimpa notice (error/empty/closed)', () => {
    // Bila error/empty menimpa tbody tanpa invalidate, poll berikutnya dengan
    // payload sama akan salah "skip" dan layar mentok di notice.
    assert.match(html, /approvalLastSerialized\s*=\s*null/, 'notice wajib mengosongkan snapshot');
    assert.match(html, /approvalRowsLive\s*=\s*false/, 'notice menandai tbody tidak sedang berisi data');
});

test('T8/static: defer saat aksi Izinkan/Tolak in-flight — approvalActionBusy + rerunPending', () => {
    assert.match(html, /var\s+approvalActionBusy\s*=\s*false/, 'flag aksi in-flight dideklarasikan');
    const setApprovalSrc = html.slice(html.indexOf('function setApproval'));
    assert.match(setApprovalSrc, /approvalActionBusy\s*=\s*true/, 'setApproval menandai aksi mulai');
    assert.match(setApprovalSrc, /\.finally/, 'aksi selalu membersihkan state di finally');
    assert.match(setApprovalSrc, /approvalActionBusy\s*=\s*false/, 'flag aksi dibersihkan');
    // Jalur polling: saat aksi berjalan, render antrean ditunda via rerunPending.
    const loadSrc = html.slice(html.indexOf('function loadApprovals'), html.indexOf('function markUpdated'));
    assert.match(loadSrc, /if\s*\(\s*approvalActionBusy\s*\)\s*\{[^}]*approvalRerunPending\s*=\s*true/s, 'poll saat aksi in-flow menunda render via approvalRerunPending');
});

test('T8/static: polling 5 detik tetap ada (perilaku realtime tidak hilang)', () => {
    assert.match(html, /setInterval\(loadApprovals,\s*5000\)/);
});

test('T8/static: update per-baris — baris punya data-mac dan applyApprovalRowOps mutasi in-place', () => {
    assert.match(html, /data-mac="/, 'baris data membawa identitas mac_address untuk update/remove in-place');
    const applySrc = html.match(/function\s+applyApprovalRowOps[\s\S]*?\n\}/);
    assert.ok(applySrc, 'fungsi penerapan operasi per-baris ada');
    assert.match(applySrc[0], /insertBefore/, 'baris baru disisipkan tanpa menimpa tbody');
    assert.match(applySrc[0], /removeChild|\.remove\(\)/, 'baris hilang dihapus satuan');
});

test('G8 tetap: stempel waktu, error polling tidak menghapus data, anti-overlap fetch', () => {
    assert.match(html, /function markUpdated\(\)/, 'stempel "Diperbarui HH:MM:SS" dipertahankan');
    assert.match(html, /markUpdated\(\);/, 'dan tetap dipanggil');
    assert.match(html, /Belum ada antrean\|Memuat\/\.test\(tbody\.textContent\)/, 'catch polling hanya menimpa placeholder, bukan baris data');
    assert.match(html, /var\s+approvalLoading\s*=\s*false/, 'guard anti-overlap fetch asli dipertahankan');
    assert.match(html, /var\s+approvalRerunPending\s*=\s*false/, 'jalur rerun anti-overlap asli dipertahankan');
});
