/* Regression contract tests untuk Batch 3 (area dashboard).
 * Referensi temuan: review_uiux_webui.md (ID: S1, S2, T10 poin 2, S19, S4
 * bagian dashboard).
 *
 * Run with:  node --test static/js/uiux-batch3-dashboard.test.mjs   (from webui/)
 *
 * Dua jenis test:
 *   1. Kontrak statik — membaca dashboard.html / admin.js ASLI dan memastikan
 *      properti kunci perbaikan tidak pernah regresi.
 *   2. Perilaku — fungsi tunggal (toggleExam, closeQuestionsModal, validator
 *      upload) diekstrak dari admin.js asli dan dijalankan dalam Node vm
 *      dengan mock DOM/api minimal, pola sama dengan uiux-batch1.test.mjs.
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

const adminJs = () => read('static/js/admin.js');
const dashboardHtml = () => read('templates/admin/dashboard.html');

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

// ---------------------------------------------------------------------------
// S1 — toggle status ujian wajib lewat konfirmasi + aria-pressed
// ---------------------------------------------------------------------------

test('S1: toggleExam menampilkan showConfirm SEBELUM apiFetch dengan label non-default', () => {
    const src = adminJs();
    const fn = extractFunction(src, 'toggleExam');
    assert.ok(fn, 'fungsi toggleExam harus ada di admin.js');

    const idxConfirm = fn.indexOf('showConfirm');
    const idxFetch = fn.indexOf('apiFetch');
    assert.ok(idxConfirm !== -1, 'toggleExam harus memanggil showConfirm');
    assert.ok(idxFetch !== -1, 'toggleExam tetap memanggil apiFetch');
    assert.ok(idxConfirm < idxFetch, 'showConfirm harus dipanggil SEBELUM apiFetch (tidak langsung POST)');

    // Label konfirmasi menyesuaikan arah toggle — bukan default "Ya, Hapus".
    assert.match(fn, /Ya, Nonaktifkan/, 'label konfirmasi arah nonaktifkan harus di-override');
    assert.match(fn, /Ya, Aktifkan/, 'label konfirmasi arah aktifkan harus di-override');
    assert.ok(!fn.includes("'Ya, Hapus'"), 'label default "Ya, Hapus" tidak boleh dipakai untuk aksi toggle');
    assert.match(fn, /Batal/, 'ada label tombol batal');

    // Pesan kontekstual menjelaskan konsekuensi ke siswa.
    assert.match(fn, /Nonaktifkan ujian/, 'pesan kontekstual arah nonaktif');
    assert.match(fn, /Aktifkan ujian/, 'pesan kontekstual arah aktif');
    assert.match(fn, /Siswa tidak bisa login/, 'konsekuensi ke siswa dijelaskan di dialog');
});

test('S1: guard double-click tidak mengunci permanen bila user batal di dialog', () => {
    const src = adminJs();
    const fn = extractFunction(src, 'toggleExam');
    assert.ok(fn, 'fungsi toggleExam harus ada');

    // Penguncian style terjadi SETELAH konfirmasi disetujui (di dalam .then),
    // sehingga batal = badge tidak pernah dikunci.
    const thenIdx = fn.indexOf('.then(');
    const lockIdx = fn.indexOf("pointerEvents = 'none'");
    assert.ok(lockIdx !== -1, 'guard pointerEvents tetap ada selama request berjalan');
    assert.ok(thenIdx !== -1 && lockIdx > thenIdx, 'penguncian pointerEvents harus terjadi setelah dialog disetujui');
    assert.match(fn, /finally[\s\S]*?pointerEvents = ''/, 'style badge dipulihkan di finally (sukses maupun gagal)');
});

test('S1: badge status di dashboard punya aria-pressed sesuai status awal', () => {
    const html = dashboardHtml();
    // Tiga varian badge yang bisa diklik (aktif / tombstoned / nonaktif).
    const active = html.match(/<span class="status-badge status-active" id="status-[^>]*>/);
    const tomb = html.match(/<span class="status-badge status-tombstoned" id="status-[^>]*>/);
    const inactive = html.match(/<span class="status-badge status-inactive" id="status-[^>]*>/);
    assert.ok(active && tomb && inactive, 'tiga varian badge toggleable harus ada');
    assert.match(active[0], /aria-pressed="true"/, 'badge aktif init aria-pressed="true"');
    assert.match(tomb[0], /aria-pressed="false"/, 'badge tombstoned init aria-pressed="false"');
    assert.match(inactive[0], /aria-pressed="false"/, 'badge nonaktif init aria-pressed="false"');

    // Pasca-sukses toggle, aria-pressed ikut diperbarui.
    const fn = extractFunction(adminJs(), 'toggleExam');
    assert.match(fn, /setAttribute\('aria-pressed'/, 'aria-pressed diperbarui setelah toggle sukses');
});

// ---------------------------------------------------------------------------
// S1 (perilaku) — toggleExam dijalankan dalam vm dengan mock
// ---------------------------------------------------------------------------

function makeBadge(classes) {
    const classSet = new Set(classes);
    return {
        classList: {
            contains: (c) => classSet.has(c),
            remove: (c) => classSet.delete(c),
            add: (c) => classSet.add(c),
            toggle: (c, force) => {
                if (force === undefined) { if (classSet.has(c)) classSet.delete(c); else classSet.add(c); }
                else if (force) classSet.add(c); else classSet.delete(c);
            }
        },
        style: {},
        dataset: {},
        attrs: {},
        textContent: '',
        title: '',
        setAttribute(k, v) { this.attrs[k] = String(v); },
        getAttribute(k) { return Object.prototype.hasOwnProperty.call(this.attrs, k) ? this.attrs[k] : null; }
    };
}

function flush() { return new Promise((r) => setTimeout(r, 20)); }

test('S1 perilaku: batal di dialog → apiFetch TIDAK dipanggil & badge tidak dikunci', async () => {
    const src = adminJs();
    const fn = extractFunction(src, 'toggleExam');
    const badge = makeBadge(['status-badge', 'status-active']);
    const confirmCalls = [];
    const apiCalls = [];
    const sandbox = {
        document: {
            getElementById(id) {
                if (id === 'status-7') return badge;
                if (id === 'exam-row-7') {
                    return { querySelector: () => ({ getAttribute: (a) => (a === 'data-name' ? 'UTS Matematika' : null), textContent: 'UTS Matematika' }) };
                }
                return null;
            }
        },
        showConfirm(msg, detail, okLabel, cancelLabel) {
            confirmCalls.push({ msg, detail, okLabel, cancelLabel });
            return Promise.resolve(false); // user BATAL
        },
        apiFetch(...args) { apiCalls.push(args); return Promise.resolve({ json: () => Promise.resolve({ success: true }) }); },
        showToast() {}
    };
    vm.createContext(sandbox);
    vm.runInContext(fn, sandbox, { filename: 'admin.js#toggleExam' });
    sandbox.toggleExam(7);
    await flush();

    assert.equal(confirmCalls.length, 1, 'dialog konfirmasi muncul sekali');
    assert.match(confirmCalls[0].msg, /Nonaktifkan ujian "UTS Matematika"\?/, 'pesan menyebut nama ujian');
    assert.match(confirmCalls[0].msg, /Nonaktifkan/, 'pesan kontekstual arah nonaktif (badge sebelumnya aktif)');
    assert.match(confirmCalls[0].detail, /Siswa tidak bisa login/, 'konsekuensi dijelaskan');
    assert.equal(confirmCalls[0].okLabel, 'Ya, Nonaktifkan', 'confirmLabel di-override, bukan default hapus');
    assert.equal(apiCalls.length, 0, 'batal → tidak ada POST ke server');
    assert.notEqual(badge.style.pointerEvents, 'none', 'badge tidak ikut terkunci saat batal');
    assert.equal(badge.dataset.toggling, undefined, 'flag re-entry tidak tertinggal saat batal');
});

test('S1 perilaku: setuju → POST terkirim, badge & aria-pressed diperbarui, style dipulihkan', async () => {
    const src = adminJs();
    const fn = extractFunction(src, 'toggleExam');
    const badge = makeBadge(['status-badge', 'status-active']);
    badge.attrs['aria-pressed'] = 'true';
    badge.textContent = 'Aktif';
    const apiCalls = [];
    const sandbox = {
        document: {
            getElementById(id) {
                if (id === 'status-7') return badge;
                if (id === 'exam-row-7') return { querySelector: () => null };
                return null;
            }
        },
        showConfirm() { return Promise.resolve(true); },
        apiFetch(url, opts) {
            apiCalls.push({ url, opts });
            return Promise.resolve({ json: () => Promise.resolve({ success: true, new_status: 'inactive', message: 'ok' }) });
        },
        showToast() {}
    };
    vm.createContext(sandbox);
    vm.runInContext(fn, sandbox, { filename: 'admin.js#toggleExam' });
    sandbox.toggleExam(7);
    await flush();

    assert.equal(apiCalls.length, 1, 'POST terkirim setelah setuju');
    assert.equal(apiCalls[0].url, '/admin/api/exams/7/toggle');
    assert.equal(apiCalls[0].opts.method, 'POST');
    assert.equal(badge.textContent, 'Nonaktif', 'badge diperbarui dari new_status server');
    assert.equal(badge.attrs['aria-pressed'], 'false', 'aria-pressed ikut berubah');
    assert.notEqual(badge.style.pointerEvents, 'none', 'pointerEvents dipulihkan setelah selesai');
    assert.equal(badge.dataset.toggling, undefined, 'flag re-entry dibersihkan setelah selesai');
});

// ---------------------------------------------------------------------------
// S2 — guard unsaved-changes modal konfigurasi soal
// ---------------------------------------------------------------------------

test('S2: dirty-tracking terpasang — listener input/change, reset di open & sukses simpan', () => {
    const src = adminJs();

    assert.match(src, /questionsConfigDirty/, 'flag dirty untuk modal konfigurasi soal harus ada');

    // Listener terdelegasi pada modal menandai kotor untuk SEMUA field
    // (bobot, level keamanan, jumlah soal, jadwal, identitas, pengawas).
    const open = extractFunction(src, 'openQuestionsModal');
    assert.ok(open, 'openQuestionsModal ada');
    assert.match(open, /questionsConfigDirty = false/, 'flag direset saat modal dibuka');

    const save = extractFunction(src, 'saveQuestionsConfig');
    assert.ok(save, 'saveQuestionsConfig ada');
    assert.match(save, /resetQuestionsConfigDirty\(\)/, 'flag direset setelah simpan sukses');

    const mark = extractFunction(src, 'markQuestionsConfigDirty');
    assert.ok(mark, 'helper penanda kotor ada');

    // Perubahan programatik yang tidak memicu event input/change juga menandai.
    for (const name of ['applyBulkWeight', 'insertQuestionAt', 'removeQuestionCard', 'addIdentityField', 'clearSchedule', 'setPanelColor']) {
        const f = extractFunction(src, name);
        assert.ok(f && f.includes('markQuestionsConfigDirty'), `${name} harus menandai state kotor (perubahan programatik)`);
    }
});

test('S2: tombol Batal/close lewat guard — showConfirm "Buang perubahan?" tanpa label default hapus', () => {
    const src = adminJs();
    const fn = extractFunction(src, 'closeQuestionsModal');
    assert.ok(fn, 'closeQuestionsModal ada');
    const idxConfirm = fn.indexOf('showConfirm');
    assert.ok(idxConfirm !== -1, 'closeQuestionsModal menampilkan konfirmasi saat kotor');
    assert.match(fn, /Buang perubahan\?/, 'pesan konfirmasi pembuangan');
    assert.ok(!fn.includes("'Ya, Hapus'"), 'label default "Ya, Hapus" tidak boleh dipakai untuk buang perubahan');
    assert.match(fn, /closeQuestionsModal\(true\)/, 'setelah setuju, modal ditutup paksa (tanpa konfirmasi ulang)');

    // Escape & backdrop: dicegah membuang lewat mekanisme existing (capture),
    // tanpa merefaktor admin-core.js.
    assert.match(src, /document\.addEventListener\('keydown', guardQuestionsModalEscape, true\)/, 'Escape di-intercept di fase capture');
    assert.match(src, /document\.addEventListener\('click', guardQuestionsModalBackdropClick, true\)/, 'backdrop click di-intercept di fase capture');
    const esc = extractFunction(src, 'guardQuestionsModalEscape');
    assert.ok(esc, 'guardQuestionsModalEscape ada');
    assert.match(esc, /Escape/, 'menangani tombol Escape');
    assert.match(esc, /stopPropagation/, 'menghentikan force-close milik Global Modal Manager');
    const backdrop = extractFunction(src, 'guardQuestionsModalBackdropClick');
    assert.ok(backdrop, 'guardQuestionsModalBackdropClick ada');
    assert.match(backdrop, /stopPropagation/, 'menghentikan force-close backdrop milik Global Modal Manager');
});

test('S2 perilaku: kotor + Batal → konfirmasi; batal → modal tetap terbuka; setuju → tertutup & flag reset', async () => {
    const src = adminJs();
    const script = [
        'var questionsConfigDirty = false;',
        'var questionsDiscardConfirmOpen = false;',
        extractFunction(src, 'markQuestionsConfigDirty'),
        extractFunction(src, 'resetQuestionsConfigDirty'),
        extractFunction(src, 'closeQuestionsModal')
    ].join('\n');
    const modal = { style: { display: 'flex' } };
    const confirmCalls = [];
    const sandbox = {
        document: { getElementById: (id) => (id === 'questionsModal' ? modal : null) },
        showConfirm(msg, detail, okLabel, cancelLabel) {
            confirmCalls.push({ msg, detail, okLabel, cancelLabel });
            return Promise.resolve(sandbox.__answer);
        },
        __answer: false
    };
    vm.createContext(sandbox);
    vm.runInContext(script, sandbox, { filename: 'admin.js#s2' });

    // Belum kotor → langsung tutup tanpa konfirmasi.
    sandbox.closeQuestionsModal();
    assert.equal(modal.style.display, 'none', 'modal bersih ditutup langsung');
    assert.equal(confirmCalls.length, 0, 'tidak ada konfirmasi saat tidak kotor');

    // Kotor → konfirmasi dulu.
    modal.style.display = 'flex';
    sandbox.markQuestionsConfigDirty();
    assert.equal(sandbox.questionsConfigDirty, true, 'markQuestionsConfigDirty menandai kotor');
    sandbox.__answer = false;
    sandbox.closeQuestionsModal();
    await flush();
    assert.equal(confirmCalls.length, 1, 'konfirmasi muncul saat kotor');
    assert.match(confirmCalls[0].msg, /Buang perubahan\?/);
    assert.notEqual(confirmCalls[0].okLabel, 'Ya, Hapus', 'label bukan default hapus');
    assert.equal(modal.style.display, 'flex', 'batal → modal tetap terbuka (perubahan tidak dibuang)');
    assert.equal(sandbox.questionsConfigDirty, true, 'batal → flag tetap kotor');

    // Setuju → tertutup dan flag bersih.
    sandbox.__answer = true;
    sandbox.closeQuestionsModal();
    await flush();
    assert.equal(confirmCalls.length, 2);
    assert.equal(modal.style.display, 'none', 'setuju → modal tertutup');
    assert.equal(sandbox.questionsConfigDirty, false, 'flag direset setelah ditutup');

    // force=true melewati konfirmasi (dipakai jalur internal pasca-setuju).
    modal.style.display = 'flex';
    sandbox.markQuestionsConfigDirty();
    sandbox.closeQuestionsModal(true);
    assert.equal(modal.style.display, 'none', 'force close tanpa konfirmasi ulang');
    assert.equal(confirmCalls.length, 2, 'force close tidak menampilkan dialog baru');
});

// ---------------------------------------------------------------------------
// T10 (poin 2) + S19 — validasi field-level form upload ujian
// ---------------------------------------------------------------------------

test('T10b: validator per-field ada & helper error inline dipanggil defensif', () => {
    const src = adminJs();
    for (const name of ['validateUploadExamName', 'validateUploadPdfFile', 'validateUploadCustomToken']) {
        const f = extractFunction(src, name);
        assert.ok(f, `fungsi validasi ${name} harus ada`);
    }
    assert.match(src, /typeof setFieldError === 'function'/, 'setFieldError dipanggil defensif (helper milik agen lain)');
    assert.match(src, /typeof clearFieldError === 'function'/, 'clearFieldError dipanggil defensif');
    assert.match(src, /setFieldError\(/, 'memakai helper inline per-field, bukan toast-only');
});

test('T10b: listener blur/input/change untuk validasi live terpasang di field upload', () => {
    const src = adminJs();
    assert.match(src, /addEventListener\('blur', validateUploadExamName\)/, 'nama ujian: validasi saat blur');
    assert.match(src, /addEventListener\('input'[\s\S]{0,120}validateUploadExamName/, 'nama ujian: error inline di-clear saat mengetik');
    assert.match(src, /addEventListener\('change', validateUploadPdfFile\)/, 'file PDF: divalidasi saat dipilih');
    assert.match(src, /addEventListener\('blur', validateUploadCustomToken\)/, 'token kustom: validasi saat blur');
    assert.match(src, /addEventListener\('input'[\s\S]{0,120}validateUploadCustomToken/, 'token kustom: error inline di-clear saat mengetik');
});

test('T10b: submit memakai validasi inline per-field (toast hanya pelengkap)', () => {
    const src = adminJs();
    const submit = src.slice(src.indexOf("uploadForm.addEventListener('submit'"));
    assert.ok(submit.length > 0, 'handler submit upload ada');
    assert.match(submit, /validateUploadFormFields\(\)/, 'submit memanggil validasi gabungan per-field');
    assert.match(submit, /ditandai merah/, 'toast pelengkap mengarah ke error inline');
});

test('T10b perilaku: validator nama/file/token menampilkan error per field & lolos saat valid', async () => {
    const src = adminJs();
    const script = [
        extractFunction(src, 'showUploadFieldError'),
        extractFunction(src, 'clearUploadFieldError'),
        extractFunction(src, 'validateUploadExamName'),
        extractFunction(src, 'validateUploadPdfFile'),
        extractFunction(src, 'validateUploadCustomToken')
    ].join('\n');

    function fakeInput(attrs) {
        return {
            value: '',
            files: [],
            attrs: attrs || {},
            getAttribute(k) { return Object.prototype.hasOwnProperty.call(this.attrs, k) ? this.attrs[k] : null; }
        };
    }
    const nameInput = fakeInput();
    const fileInput = fakeInput({ 'data-max-mb': '200' });
    const tokenInput = fakeInput();
    const errors = [];
    const cleared = [];
    const sandbox = {
        document: {
            getElementById(id) {
                if (id === 'examName') return nameInput;
                if (id === 'pdfFile') return fileInput;
                if (id === 'customToken') return tokenInput;
                return null;
            }
        },
        setFieldError: (el, msg) => errors.push({ el, msg }),
        clearFieldError: (el) => cleared.push({ el })
    };
    vm.createContext(sandbox);
    vm.runInContext(script, sandbox, { filename: 'admin.js#t10b' });

    // Nama wajib.
    nameInput.value = '   ';
    assert.equal(sandbox.validateUploadExamName(), false, 'nama kosong → invalid');
    assert.ok(errors.some((e) => e.el === nameInput && /wajib/.test(e.msg)), 'error inline di field nama');
    nameInput.value = 'UTS Matematika';
    cleared.length = 0;
    assert.equal(sandbox.validateUploadExamName(), true, 'nama terisi → valid');
    assert.ok(cleared.some((c) => c.el === nameInput), 'error nama di-clear saat valid');

    // File PDF wajib + max size.
    assert.equal(sandbox.validateUploadPdfFile(), false, 'tanpa file → invalid');
    assert.ok(errors.some((e) => e.el === fileInput && /PDF/.test(e.msg)), 'error inline di field file');
    fileInput.files = [{ size: 300 * 1048576 }];
    assert.equal(sandbox.validateUploadPdfFile(), false, 'file melebihi batas → invalid');
    assert.ok(errors.some((e) => e.el === fileInput && /batas/.test(e.msg)), 'pesan batas ukuran inline');
    fileInput.files = [{ size: 10 * 1048576 }];
    assert.equal(sandbox.validateUploadPdfFile(), true, 'file dalam batas → valid');

    // Token kustom: opsional, 8 karakter A-Z0-9 (huruf kecil dinormalisasi).
    assert.equal(sandbox.validateUploadCustomToken(), true, 'token kosong → valid (opsional)');
    tokenInput.value = 'MAT101BK';
    assert.equal(sandbox.validateUploadCustomToken(), true, 'token 8 char valid');
    tokenInput.value = 'mat101bk';
    assert.equal(sandbox.validateUploadCustomToken(), true, 'huruf kecil dinormalisasi ke uppercase');
    tokenInput.value = 'MAT101';
    assert.equal(sandbox.validateUploadCustomToken(), false, 'token 6 char → invalid');
    tokenInput.value = 'MAT101B!';
    assert.equal(sandbox.validateUploadCustomToken(), false, 'karakter non-alfanumerik → invalid');
    tokenInput.value = 'MAT101BK99';
    assert.equal(sandbox.validateUploadCustomToken(), false, 'token 10 char → invalid');
    assert.ok(errors.some((e) => e.el === tokenInput && /8 karakter/.test(e.msg)), 'error inline di field token');
});

// ---------------------------------------------------------------------------
// S4 (bagian dashboard) — sweep istilah EN→ID
// ---------------------------------------------------------------------------

test('S4: label "Ditombstone" diganti "Nonaktif Otomatis" (value teknis tetap)', () => {
    const html = dashboardHtml();
    assert.ok(!html.includes('Ditombstone'), 'istilah internal "Ditombstone" harus hilang dari dashboard');
    const hits = html.split('Nonaktif Otomatis').length - 1;
    assert.ok(hits >= 3, `label baru harus ada di filter + 2 varian badge (ditemukan ${hits})`);
    assert.match(html, /<option value="tombstoned"/, 'value mesin "tombstoned" tetap');
    assert.match(html, /Nonaktif otomatis oleh sistem saat paket berakhir/, 'judul tooltip tombstone tetap koheren dengan label baru');

    // admin.js memetakan value filter ke label badge untuk filter client-side —
    // peta harus ikut diganti atau filter status "Nonaktif Otomatis" rusak.
    const src = adminJs();
    assert.match(src, /tombstoned:\s*'Nonaktif Otomatis'/, 'peta label filter client-side ikut diganti');
    assert.ok(!src.includes('Ditombstone'), 'tidak ada lagi label "Ditombstone" di admin.js');
});

test('S4: opsi level keamanan Low/Medium/High → Rendah/Sedang/Tinggi (value tetap)', () => {
    const html = dashboardHtml();
    assert.ok(!html.includes('Low —'), 'label EN "Low" hilang');
    assert.ok(!html.includes('Medium —'), 'label EN "Medium" hilang');
    assert.ok(!/High \/ Strict|High —/.test(html), 'label EN "High" hilang');
    assert.match(html, /<option value="low">Rendah — /, 'opsi low berlabel Rendah');
    assert.match(html, /<option value="medium">Sedang — /, 'opsi medium berlabel Sedang');
    assert.match(html, /<option value="high">Tinggi — /, 'opsi high berlabel Tinggi');
});

test('S4: tombol "Set All Bobot" → "Terapkan ke Semua Bobot"', () => {
    const html = dashboardHtml();
    assert.ok(!html.includes('Set All Bobot'), 'label EN tombol bobot hilang');
    assert.match(html, /Terapkan ke Semua Bobot/, 'label ID tombol bobot ada');
});
