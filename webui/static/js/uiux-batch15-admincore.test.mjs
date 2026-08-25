/**
 * ══════════════════════════════════════════════════════════════════════════
 * Suite UI/UX BATCH 15 — ADMIN CORE (agen batch15-admincore)
 * ══════════════════════════════════════════════════════════════════════════
 *
 * Referensi: review_uiux_webui.md bagian 5.12 RE-REVIEW RONDE 9 (basis
 * f0ab8d7, pasca Batch 14). Cakupan temuan milik agen ini:
 *
 *   T28 — Divider "Sisipkan Soal" menyisipkan DUA soal per klik pasca-
 *         reindex. reindexQuestions menempel ulang atribut onclick
 *         (insertQuestionAt(n)) pada tombol divider, padahal tombol yang
 *         sama sudah punya data-action="question-insert-at" yang tertangani
 *         delegasi Actions. Kedua jalur aktif bersamaan → satu klik =
 *         dua panggilan insertQuestionAt (korupsi draft ujian).
 *         Kontrak: (a) reindex TIDAK boleh menulis atribut onclick;
 *         (b) satu klik fisik pada tombol hasil reindex memanggil
 *             insertQuestionAt TEPAT satu kali, dengan index terbaru
 *             (data-index tombol wajib ikut diperbarui saat reindex,
 *             bukan hanya dataset.index pembungkusnya).
 *
 *   S90 — Dropdown multi-select pengawas mati total untuk keyboard:
 *         header dibangun sebagai div dengan .onclick saja (tanpa
 *         tabindex/role/keydown), chip hapus berupa span dengan .onclick
 *         (tidak fokusable, tanpa nama aksesibel). WCAG 2.1.1 — alur
 *         delegasi ujian tak bisa dioperasikan tanpa mouse.
 *         Kontrak: (a) pemicu dropdown punya tabindex="0", role="button",
 *             aria-expanded yang BERUBAH saat toggle, dan merespons
 *             keydown Enter/Space (memanggil togglePengawasDropdown);
 *         (b) tombol hapus chip adalah <button> (native fokusable) dengan
 *             aria-label yang memuat nama pengawas; mengaktifkannya
 *             menghapus centang checkbox pengawas tsb dan merender ulang
 *             chip (tanpa membunuh listener lewat innerHTML +=).
 *
 *   S91 — showSubmissionDetail melakukan fetch detail tanpa sequence-token:
 *         respons lambat permintaan pertama bisa mendarat TERAKHIR dan
 *         menimpa isi modal dengan detail submission lain (kelas race S78
 *         yang di Batch 14 hanya dibasmi pada empat loader daftar).
 *         Kontrak: token monoton submissionDetailSeq; guard
 *         "seq !== submissionDetailSeq → return" ada di THEN dan CATCH;
 *         perilaku vm membuktikan respons basi diabaikan.
 *
 *   S102 — openDelegateExamModal sama persis: fetch data delegasi tanpa
 *         token generasi pada modal aksi TULIS (penugasan pengawas/guru).
 *         Kontrak: token delegateModalSeq + guard then/catch; vm
 *         membuktikan isi modal milik permintaan TERAKHIR.
 *
 *   R100 — Atribut event inline hidup lagi di generator HTML admin.js:
 *         onfocus/onblur pada input instansi (editUserInstansi) dan
 *         onsubmit pada form editUserForm. Melanggar kontrak delegasi
 *         Actions + CSP hygiene. Kontrak: admin.js BEBAS atribut on*
 *         bergaya HTML (pola: spasi + on<nama> + "=" + kutip) — kalibrasi:
 *         regex dikondisikan diakhiri kutip sehingga identifier JS biasa
 *         seperti `onReplaced ===` tidak salah tangkap; penggantinya
 *         listener terprogram (addEventListener) pasca-render.
 *
 *   R101 — Pagination dashboard menandai batas halaman hanya dengan class
 *         "disabled"; screen reader tidak merasakan apa pun. Beda pola
 *         dengan submissions.html yang benar (class + aria-disabled).
 *         Kontrak: anchor Sebelumnya/Berikutnya dashboard punya
 *         aria-disabled="true" pada kondisi boundary yang sama dengan
 *         class disabled-nya (paritas submissions.html).
 *
 * Kepemilikan file agen ini: static/js/admin.js,
 *   templates/admin/dashboard.html (hanya blok paginasi R101),
 *   dan suite ini sendiri.
 *
 * Cara kalibrasi ulang bila test ini MEMERAH setelah edit sah:
 *   - T28: jika model interaksi divider berubah (mis. pindah ke listener
 *     langsung), pastikan prinsipnya tetap: SATU klik = SATU sisipan.
 *   - S90: jika struktur dropdown berubah, jangan lepas tabindex/role/
 *     aria-expanded/keydown pada pemicu dan aria-label pada chip hapus.
 *   - S91/S102: nama token (submissionDetailSeq/delegateModalSeq) boleh
 *     berubah asal pola ++token + guard then/catch dipertahankan — update
 *     regex statik sesuai nama baru.
 *   - R100: regex inline-handler sengaja menuntut kutip penutup nilai agar
 *     tidak vakum maupun over-capture; jangan melonggarkan tanpa alasan.
 *
 * Run with:  node --test static/js/uiux-batch15-admincore.test.mjs   (from webui/)
 */

import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';

const HERE = path.dirname(fileURLToPath(import.meta.url));
const WEBUI_ROOT = path.join(HERE, '..', '..');
const read = (...p) => fs.readFileSync(path.join(WEBUI_ROOT, ...p), 'utf8');

const ADMIN_JS = read('static', 'js', 'admin.js');
const DASHBOARD_HTML = read('templates', 'admin', 'dashboard.html');

/** Ambil badan fungsi top-level `function NAME(...)` sampai `\nfunction ` berikutnya. */
function functionBody(src, name) {
    const start = src.indexOf(`function ${name}(`);
    assert.ok(start !== -1, `function ${name} ditemukan`);
    const end = src.indexOf('\nfunction ', start + 1);
    return src.slice(start, end === -1 ? undefined : end);
}

/** Ekstrak statement registrasi Actions tunggal agar bisa dieksekusi di vm. */
function extractRegistration(src, name) {
    const re = new RegExp("Actions\\.register\\(\\s*'" + name + "'[\\s\\S]*?\\}\\);");
    const m = src.match(re);
    assert.ok(m, `registrasi Actions('${name}') harus ditemukan`);
    return m[0];
}

// ════════════════════════════════════════════════════════════════════════
// T28 — double-insert divider soal (onclick arwah vs delegasi Actions)
// ════════════════════════════════════════════════════════════════════════

test('T28 (statik): reindexQuestions tidak menempel atribut onclick pada tombol divider', () => {
    const body = functionBody(ADMIN_JS, 'reindexQuestions');
    assert.doesNotMatch(body, /setAttribute\(\s*['"]onclick['"]/,
        'reindexQuestions menghidupkan ulang onclick arwah padahal tombol sudah tertangani ' +
        'delegasi question-insert-at — satu klik menjadi DUA panggilan insertQuestionAt');
});

test('T28 (perilaku vm): satu klik pada tombol divider hasil reindex menyisipkan TEPAT satu soal', () => {
    // DOM palsu minimal untuk reindexQuestions: satu kartu soal + satu divider.
    const attrs = {};
    const btn = {
        setAttribute(k, v) { attrs[k] = String(v); },
        getAttribute(k) { return k in attrs ? attrs[k] : null; },
    };
    const divider = {
        classList: { contains(c) { return c === 'q-editor-divider'; } },
        dataset: {},
        querySelector(sel) { return sel === '.btn-add-inline' ? btn : null; },
    };
    const card = {
        classList: { contains(c) { return c === 'question-editor-card'; } },
        querySelector(sel) {
            if (sel === '.q-num-badge') return { textContent: '' };
            if (sel === '.q-number') return { value: '' };
            return null;
        },
    };
    const container = { children: [card, divider] };

    const calls = [];
    const registry = {};
    const sandbox = {
        document: { getElementById(id) { return id === 'questionsList' ? container : null; } },
        Actions: { register(name, fn) { registry[name] = fn; } },
        console,
    };
    sandbox.globalThis = sandbox;
    vm.createContext(sandbox);

    // Jalankan reindexQuestions ASLI + registrasi delegasi ASLI.
    vm.runInContext(functionBody(ADMIN_JS, 'reindexQuestions') + '\nreindexQuestions();', sandbox,
        { filename: 'admin.js#reindexQuestions' });
    vm.runInContext(extractRegistration(ADMIN_JS, 'question-insert-at'), sandbox, { filename: 'admin.js#reg' });
    // Spy pengganti insertQuestionAt (diselesaikan lewat scope global vm saat klik).
    sandbox.insertQuestionAt = (idx) => calls.push(idx);

    // Simulasi SATU klik fisik: jalur inline (atribut onclick, bila ada)
    // DAN jalur delegasi Actions aktif bersamaan — persis kondisi browser.
    const inlineHandler = btn.getAttribute('onclick');
    assert.equal(inlineHandler, null,
        `tombol divider masih membawa atribut onclick="${inlineHandler}" — arwah jalur lama; ` +
        'hapus penulisan setAttribute onclick di reindexQuestions');
    registry['question-insert-at'](btn);

    assert.equal(calls.length, 1,
        `${calls.length}× insertQuestionAt per klik — double-insert T28`);
    // Index yang dipakai harus index DIVIDER terbaru (data-index tombol ikut
    // diperbarui reindex), bukan nilai basi dari render awal.
    assert.equal(String(calls[0]), String(divider.dataset.index),
        `insertQuestionAt dipanggil dengan index ${calls[0]}, padahal index divider hasil reindex ${divider.dataset.index} — data-index tombol wajib ikut diperbarui`);
});

// ════════════════════════════════════════════════════════════════════════
// S90 — dropdown multi-select pengawas hidup untuk keyboard
// ════════════════════════════════════════════════════════════════════════

test('S90 (statik): pemicu dropdown pengawas punya tabindex, role=button, aria-expanded, aria-haspopup, dan keydown Enter/Space', () => {
    const body = functionBody(ADMIN_JS, 'renderPengawasSelection');
    assert.match(body, /setAttribute\(\s*['"]tabindex['"],\s*['"]0['"]\)/,
        'pemicu dropdown tidak fokusable — mati untuk keyboard (WCAG 2.1.1)');
    assert.match(body, /setAttribute\(\s*['"]role['"],\s*['"]button['"]\)/,
        'pemicu dropdown tanpa role="button" — screen reader tidak tahu ini bisa diaktifkan');
    assert.match(body, /setAttribute\(\s*['"]aria-haspopup['"]/,
        'tanpa aria-haspopup screen reader tidak mendapat hint listbox');
    assert.match(body, /setAttribute\(\s*['"]aria-expanded['"]/,
        'status buka/tutup dropdown tak terumumkan tanpa aria-expanded');
    assert.match(body, /addEventListener\(\s*['"]keydown['"][\s\S]{0,200}Enter/,
        'pemicu hanya bereaksi klik — tambahkan keydown Enter/Space → togglePengawasDropdown()');
});

test('S90 (statik): chip hapus adalah <button> ber-aria-label nama pengawas, dirender tanpa innerHTML += yang membunuh listener', () => {
    const body = functionBody(ADMIN_JS, 'renderPengawasSelection');
    assert.match(body, /createElement\(\s*['"]button['"]\)[\s\S]{0,400}aria-label/i,
        'chip hapus masih span non-fokusable — jadikan <button type="button"> ber-aria-label');
    assert.match(body, /aria-label[\s\S]{0,80}Hapus pengawas/i,
        'aria-label chip hapus wajib memuat nama pengawas (kontrak "Hapus pengawas {nama}")');
    assert.doesNotMatch(body, /header\.innerHTML\s*\+=/,
        'header.innerHTML += menserialisasi chip yang baru dibuat dan MEMBUNUH semua listener ' +
        'elemen chip (termasuk tombol hapus) — append elemen panah, jangan konkatenasi innerHTML');
    // Migrasi warna chip ke token: larang literal ungu hardcoded di builder ini.
    assert.doesNotMatch(body, /#c084fc|168,\s*85,\s*247/,
        'warna chip masih literal hex/rgb — pakai var(--color-accent-light) / rgba(var(--rgb-accent), α)');
});

test('S90 (perilaku vm): keydown Enter/Space men-toggle dropdown + aria-expanded berubah; aktivasi tombol chip menghapus centang pengawas', () => {
    function makeEl(tag) {
        const el = {
            tagName: tag.toUpperCase(),
            style: { cssText: '' },
            dataset: {},
            _attrs: {},
            _listeners: {},
            children: [],
            _html: '',
            setAttribute(k, v) { this._attrs[k] = String(v); },
            getAttribute(k) { return k in this._attrs ? this._attrs[k] : null; },
            addEventListener(t, f) { (this._listeners[t] = this._listeners[t] || []).push(f); },
            appendChild(c) { this.children.push(c); return c; },
            // Option list membangun <label> lalu memasang listener pada
            // checkbox anaknya — sediakan elemen input palsu untuk itu.
            querySelector(sel) { return sel === 'input' ? makeEl('input') : null; },
            focus() {},
            fire(type, ev) { (this._listeners[type] || []).forEach((f) => f(ev)); },
        };
        el.classList = { contains() { return false; } };
        Object.defineProperty(el, 'innerHTML', {
            get() { return this._html; },
            set(v) { this._html = v; this.children = []; },
        });
        Object.defineProperty(el, 'textContent', {
            get() { return this._text ?? ''; },
            set(v) { this._text = v; },
        });
        return el;
    }

    const byId = {};
    const cbWawan = { value: '3', checked: true }; // checkbox pengawas id=3 tercentang
    const doc = {
        createElement: (t) => makeEl(t),
        getElementById: (id) => (id in byId ? byId[id] : null),
        querySelectorAll(sel) { return sel.includes('pengawas-checkbox:checked') ? [cbWawan] : []; },
        querySelector(sel) { return sel.includes('[value="3"]') ? cbWawan : null; },
        addEventListener() {},
    };

    const sandbox = {
        document: doc,
        window: {},
        setTimeout,
        clearTimeout,
        escapeHtml: (s) => String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;'),
        console,
    };
    sandbox.globalThis = sandbox;
    vm.createContext(sandbox);

    const containerEl = makeEl('div');
    byId['pengawasList'] = containerEl;

    const renderPengawasSelection = vm.runInContext('(' + functionBody(ADMIN_JS, 'renderPengawasSelection') + ')', sandbox,
        { filename: 'admin.js#renderPengawasSelection' });
    const togglePengawasDropdown = vm.runInContext('(' + functionBody(ADMIN_JS, 'togglePengawasDropdown') + ')', sandbox,
        { filename: 'admin.js#togglePengawasDropdown' });

    // Bungkus toggle dengan spy untuk membuktikan keydown benar-benar memanggilnya.
    let toggles = 0;
    sandbox.togglePengawasDropdown = function (...a) { toggles += 1; return togglePengawasDropdown.apply(this, a); };

    const available = [{ id: 3, username: 'pakwawan', role: 'Pengawas' }];
    renderPengawasSelection([{ id: 3 }], available);

    const wrapper = containerEl.children.at(-1);
    assert.ok(wrapper, 'wrapper dropdown dirender ke container');
    const header = wrapper.children[0];
    const dropdown = wrapper.children[1];
    // Elemen ber-id terdaftar di registry dokumen (toggle mencari via getElementById).
    if (header.id) byId[header.id] = header;
    if (dropdown.id) byId[dropdown.id] = dropdown;

    // 1) Pemicu accessible-by-construction.
    assert.equal(header.getAttribute('tabindex'), '0', 'pemicu wajib tabindex=0');
    assert.equal(header.getAttribute('role'), 'button', 'pemicu wajib role=button');
    assert.equal(header.getAttribute('aria-expanded'), 'false', 'aria-expanded awal false');

    // 2) Keyboard: Enter membuka, Space menutup; aria-expanded mengikuti.
    header.fire('keydown', { key: 'Enter', preventDefault() {}, stopPropagation() {} });
    assert.equal(toggles, 1, 'Enter harus memanggil togglePengawasDropdown');
    assert.equal(dropdown.style.display, 'block', 'Enter membuka dropdown');
    assert.equal(header.getAttribute('aria-expanded'), 'true', 'aria-expanded=true saat terbuka');

    header.fire('keydown', { key: ' ', preventDefault() {}, stopPropagation() {} });
    assert.equal(toggles, 2, 'Space juga harus men-toggle');
    assert.equal(dropdown.style.display, 'none', 'Space menutup dropdown');
    assert.equal(header.getAttribute('aria-expanded'), 'false', 'aria-expanded kembali false saat tertutup');

    // 3) Chip hapus: <button> fokusable ber-aria-label nama, dan aktivasi
    //    menghapus centang + merender ulang chip (listener tetap hidup).
    const chip = header.children.find(
        (c) => c.tagName !== 'BUTTON' && c.children.some((x) => x.tagName === 'BUTTON'));
    assert.ok(chip, 'chip pengawas tercentang dirender di header');
    const chipBtn = chip.children.find((x) => x.tagName === 'BUTTON');
    assert.ok(chipBtn, 'tombol hapus chip berupa <button> (native fokusable)');
    assert.match(chipBtn.getAttribute('aria-label'), /Hapus pengawas/i,
        'aria-label tombol hapus harus menyebut aksi hapus');
    assert.match(chipBtn.getAttribute('aria-label'), /pakwawan/,
        'aria-label tombol hapus harus memuat nama pengawas');

    assert.equal(cbWawan.checked, true, 'prasyarat: checkbox pengawas tercentang');
    chipBtn.fire('click', { stopPropagation() {} });
    assert.equal(cbWawan.checked, false, 'aktivasi tombol chip menghapus centang pengawas');
});

// ════════════════════════════════════════════════════════════════════════
// S91 — showSubmissionDetail tanpa sequence-token (kelas race S78)
// ════════════════════════════════════════════════════════════════════════

test('S91 (statik): showSubmissionDetail punya token submissionDetailSeq + guard di THEN dan CATCH', () => {
    const body = functionBody(ADMIN_JS, 'showSubmissionDetail');
    const hasIncrement = /\+\+\s*submissionDetailSeq|submissionDetailSeq\s*\+\+/.test(body);
    assert.ok(hasIncrement, 'fetch tanpa token generasi — pola: var seq = ++submissionDetailSeq;');
    const guards = (body.match(/seq\s*!==\s*submissionDetailSeq/g) || []).length;
    assert.ok(guards >= 2,
        `guard seq !== submissionDetailSeq hanya ${guards}× — wajib ada di THEN dan CATCH (respons gagal basi juga tidak boleh menutup modal milik permintaan lain)`);
});

test('S91 (perilaku vm): respons lambat submission PERTAMA tidak menimpa modal permintaan kedua', async () => {
    const deferred = {};
    function defer(url) {
        let resolve, reject;
        const p = new Promise((res, rej) => { resolve = res; reject = rej; });
        deferred[url] = { resolve, reject };
        return p.then((body) => ({ json: () => body }));
    }

    const els = {};
    const mk = () => ({ textContent: '', innerHTML: '', appendChild() {} });
    const toasts = [];
    let closed = 0;

    const sandbox = {
        document: { getElementById: (id) => (els[id] ??= mk()) },
        Modal: { open() {} },
        apiFetch: (url) => defer(url),
        showToast: (m, t) => toasts.push([m, t]),
        closeDetailModal: () => { closed += 1; },
        formatDateTimeID: (s) => 'fmt:' + s,
        escapeHtml: (s) => String(s),
        console,
    };
    sandbox.globalThis = sandbox;
    vm.createContext(sandbox);
    // Token generasi hidup sebagai global modul admin.js — deklarasikan di
    // konteks vm sebelum fungsi dievaluasi.
    vm.runInContext('var submissionDetailSeq = 0;', sandbox);

    const showSubmissionDetail = vm.runInContext('(' + functionBody(ADMIN_JS, 'showSubmissionDetail') + ')',
        sandbox, { filename: 'admin.js#showSubmissionDetail' });
    sandbox.showSubmissionDetail = showSubmissionDetail;

    const URL_A = '/admin/api/submissions/1/detail';
    const URL_B = '/admin/api/submissions/2/detail';
    const tick = () => new Promise((r) => setImmediate(r));

    showSubmissionDetail(1); // klik A — responsnya LAMBAT
    await tick();
    showSubmissionDetail(2); // klik B — responsnya CEPAT
    await tick();

    deferred[URL_B].resolve({ success: true, student_name: 'Siswa-B', student_class: 'XI-B', questions: [] });
    await tick();
    assert.equal(els['detailStudentName'].textContent, 'Siswa-B', 'modal menampilkan data B');

    // Respons A datang belakangan (LAN lambat) — wajib DIABAIKAN.
    deferred[URL_A].resolve({ success: true, student_name: 'Siswa-A', student_class: 'XI-A', questions: [] });
    await tick();

    assert.equal(els['detailStudentName'].textContent, 'Siswa-B',
        'respons basi A menimpa modal dengan detail submission orang lain — guard seq hilang?');
    assert.equal(els['detailStudentClass'].textContent, 'XI-B', 'kelas ikut milik B');
    assert.equal(closed, 0, 'jalur basi tidak boleh menutup modal milik B');

    // Guard CATCH: kegagalan request basi tidak boleh menutup modal aktif.
    deferred[URL_B] && delete deferred.URL_B;
    showSubmissionDetail(3); // permintaan aktif sekarang
    await tick();
    deferred['/admin/api/submissions/3/detail'].resolve({ success: true, student_name: 'Siswa-C', student_class: 'XI-C', questions: [] });
    await tick();
    assert.equal(els['detailStudentName'].textContent, 'Siswa-C');
    // Gagal request BASI (generasi lama) — modal C harus tetap terbuka.
    deferred[URL_B].reject(new Error('basi'));
    await tick();
    assert.equal(closed, 0, 'catch dari request basi tidak boleh menutup modal milik permintaan lain');
});

// ════════════════════════════════════════════════════════════════════════
// S102 — openDelegateExamModal tanpa token generasi (modal aksi tulis)
// ════════════════════════════════════════════════════════════════════════

test('S102 (statik): openDelegateExamModal punya token delegateModalSeq + guard di THEN dan CATCH', () => {
    const body = functionBody(ADMIN_JS, 'openDelegateExamModal');
    const hasIncrement = /\+\+\s*delegateModalSeq|delegateModalSeq\s*\+\+/.test(body);
    assert.ok(hasIncrement, 'fetch data delegasi tanpa token generasi — pola: const seq = ++delegateModalSeq;');
    const guards = (body.match(/seq\s*!==\s*delegateModalSeq/g) || []).length;
    assert.ok(guards >= 2,
        `guard seq !== delegateModalSeq hanya ${guards}× — wajib ada di THEN dan CATCH`);
});

test('S102 (perilaku vm): isi modal delegasi milik ujian TERAKHIR yang dibuka, bukan respons basi', async () => {
    const deferred = {};
    function defer(url) {
        let resolve, reject;
        const p = new Promise((res, rej) => { resolve = res; reject = rej; });
        deferred[url] = { resolve, reject };
        return p.then((body) => ({ json: () => body }));
    }

    const els = {};
    const mk = () => ({ textContent: '', innerHTML: '', disabled: false, appendChild() {} });
    const guruOptions = [];
    const renders = [];

    const sandbox = {
        document: {
            createElement: () => ({ value: '', textContent: '', selected: false }),
            // Hanya id nyata yang boleh terwujud — id opsional seperti
            // delegateCurrentLabel wajib null persis DOM sungguhan.
            getElementById: (id) => {
                if (!['delegateExamModal', 'delegateExamId', 'delegateOwnerSelect',
                    'delegateCurrentOwner', 'delegatePengawasList'].includes(id)) return null;
                if (id === 'delegateOwnerSelect') {
                    const el = els[id] ??= mk();
                    el.appendChild = (o) => guruOptions.push(o);
                    return el;
                }
                return els[id] ??= mk();
            },
        },
        Modal: { open() {} },
        apiFetch: (url) => defer(url),
        renderDelegatePengawas: (...a) => renders.push(a),
        console,
    };
    sandbox.globalThis = sandbox;
    vm.createContext(sandbox);
    // Token generasi hidup sebagai global modul admin.js — deklarasikan di
    // konteks vm sebelum fungsi dievaluasi.
    vm.runInContext('var delegateModalSeq = 0;', sandbox);

    const open = vm.runInContext('(' + functionBody(ADMIN_JS, 'openDelegateExamModal') + ')',
        sandbox, { filename: 'admin.js#openDelegateExamModal' });
    sandbox.openDelegateExamModal = open;

    const tick = () => new Promise((r) => setImmediate(r));
    const payload = (tag) => ({
        success: true,
        data: {
            current_owner: { username: 'Owner-' + tag },
            available_gurus: [{ id: 9, username: 'Guru-' + tag, instansi: 'Ins-' + tag }],
            available_pengawas: [],
            assigned_pengawas_ids: [],
        },
    });

    open(11); // ujian A — respons LAMBAT
    await tick();
    open(22); // ujian B — respons CEPAT
    await tick();

    deferred['/admin/api/exams/22/delegate-data'].resolve(payload('B'));
    await tick();
    assert.equal(els['delegateCurrentOwner'].textContent, 'Pembuat: Owner-B', 'modal milik B');

    // Respons A datang terlambat — wajib diabaikan.
    deferred['/admin/api/exams/11/delegate-data'].resolve(payload('A'));
    await tick();

    assert.equal(els['delegateCurrentOwner'].textContent, 'Pembuat: Owner-B',
        'respons basi ujian A menimpa konteks modal aksi tulis (risiko salah assign) — guard seq hilang?');
    const stale = guruOptions.filter((o) => /Guru-A/.test(o.textContent));
    assert.equal(stale.length, 0, 'opsion guru dari respons basi A bocor ke modal B');
    const fresh = guruOptions.filter((o) => /Guru-B/.test(o.textContent));
    assert.equal(fresh.length, 1, 'opsion guru milik B tetap terpasang tepat satu kali');

    // Guard CATCH: kegagalan request basi tidak boleh menimpa pesan error modal aktif.
    open(33);
    await tick();
    deferred['/admin/api/exams/22/delegate-data'].reject(new Error('basi'));
    await tick();
    assert.doesNotMatch(els['delegateOwnerSelect'].innerHTML, /Gagal memuat/,
        'catch dari request basi tidak boleh menimpa isi select modal milik permintaan lain');
});

// ════════════════════════════════════════════════════════════════════════
// R100 — atribut event inline hidup lagi di generator HTML admin.js
// ════════════════════════════════════════════════════════════════════════

test('R100 (statik): admin.js bebas atribut event inline bergaya HTML (onfocus=/onblur=/onsubmit=…)', () => {
    const hits = [...ADMIN_JS.matchAll(/\son[a-z]+\s*=\s*["']/gi)].map((m) => m[0].trim());
    assert.deepEqual(hits, [],
        `atribut event inline ditemukan (${hits.join(', ')}) — ganti data-action + delegasi Actions ` +
        'atau listener terprogram pasca-render (CSP hygiene + kontrak satu-jalur-wiring)');
});

test('R100 (statik): pengganti terprogram terpasang — form editUserForm via addEventListener submit, input instansi via addEventListener focus/blur', () => {
    const formBuilder = functionBody(ADMIN_JS, 'createEditUserModal');
    assert.match(formBuilder, /getElementById\(\s*['"]editUserForm['"]\s*\)[\s\S]{0,120}addEventListener\(\s*['"]submit['"]/,
        'form Atur User harus ter-wire lewat addEventListener("submit", submitEditUser)');
    const instansiBuilder = functionBody(ADMIN_JS, 'editUserInstansi');
    assert.match(instansiBuilder, /input\.addEventListener\(\s*['"]focus['"]/,
        'highlight fokus input instansi pindah ke listener focus terprogram');
    assert.match(instansiBuilder, /input\.addEventListener\(\s*['"]blur['"]/,
        'reset border input instansi pindah ke listener blur terprogram');
    assert.doesNotMatch(instansiBuilder, /rgba\(99,\s*102,\s*247/,
        'gunakan var(--color-primary-light), jangan literal rgba baru');
});

// ════════════════════════════════════════════════════════════════════════
// R101 — pagination dashboard: boundary pakai aria-disabled (paritas submissions)
// ════════════════════════════════════════════════════════════════════════

test('R101 (statik): anchor Sebelumnya/Berikutnya dashboard punya aria-disabled="true" pada boundary yang sama dengan class disabled', () => {
    // Dikalibrasi Batch 17/R135: href kini KONDISIONAL (omit saat disabled).
    const prev = DASHBOARD_HTML.match(/<a \{\{if gt \.page 1\}\}href="\?page=\{\{sub \.page 1\}\}[^\n]*>/);
    assert.ok(prev, 'anchor Sebelumnya dashboard eksis');
    assert.match(prev[0], /\{\{if le \.page 1\}\}disabled\{\{end\}\}/,
        'prasyarat: penanda class disabled boundary bawah tetap ada');
    assert.match(prev[0], /\{\{if le \.page 1\}\}aria-disabled="true"\{\{end\}\}/,
        'boundary bawah (halaman 1) wajib aria-disabled="true" — class CSS tak terasa screen reader (paritas submissions.html)');

    const next = DASHBOARD_HTML.match(/<a \{\{if lt \.page \.total_pages\}\}href="\?page=\{\{add \.page 1\}\}[^\n]*>/);
    assert.ok(next, 'anchor Berikutnya dashboard eksis');
    assert.match(next[0], /\{\{if ge \.page \.total_pages\}\}disabled\{\{end\}\}/,
        'prasyarat: penanda class disabled boundary atas tetap ada');
    assert.match(next[0], /\{\{if ge \.page \.total_pages\}\}aria-disabled="true"\{\{end\}\}/,
        'boundary atas (halaman terakhir) wajib aria-disabled="true"');
});
