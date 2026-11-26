import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';

const HERE = path.dirname(fileURLToPath(import.meta.url));
const WEBUI_ROOT = path.join(HERE, '..', '..');
const ADMIN_JS = fs.readFileSync(path.join(WEBUI_ROOT, 'static', 'js', 'admin.js'), 'utf8');
const DASHBOARD = fs.readFileSync(path.join(WEBUI_ROOT, 'templates', 'admin', 'dashboard.html'), 'utf8');
const ADMIN_CSS = fs.readFileSync(path.join(WEBUI_ROOT, 'static', 'css', 'admin-base.css'), 'utf8');

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

function loadPreviewHelpers() {
    const sandbox = { console };
    vm.createContext(sandbox);
    vm.runInContext('const MAX_SHORT_ANSWER_FIELDS = 10;', sandbox);
    const names = [
        'answerPreviewTypeLabel',
        'answerPreviewStringList',
        'answerPreviewKeyValues',
        'answerPreviewOptions',
        'answerPreviewSameToken',
        'answerPreviewSpan',
        'answerPreviewTrueFalseLabel',
        'answerPreviewIssue',
        'collectQuestionPreviewIssues',
        'buildAnswerPreviewRows',
        'answerPreviewQuickAnswer',
        'answerPreviewNumberRanges',
        'buildAnswerPreview',
        'answerPreviewWeightText'
    ];
    for (const name of names) {
        const fn = extractFunction(ADMIN_JS, name);
        assert.ok(fn, `${name} harus ada di admin.js`);
        vm.runInContext(fn, sandbox);
    }
    return sandbox;
}

test('semua tipe soal valid tampil lengkap dengan kunci yang benar', () => {
    const h = loadPreviewHelpers();
    const preview = h.buildAnswerPreview([
        { number: 1, type: 'single_choice', weight: 1, options: ['A', 'B', 'C'], key: 'C' },
        { number: 2, type: 'multiple_choice', weight: 2, options: ['A', 'B', 'C'], key: ['A', 'C'], partial_scoring: true },
        {
            number: 3, type: 'true_false', weight: 1,
            statements: ['Air membeku pada 0 derajat.', '2 + 2 = 5'],
            key: ['TRUE', 'FALSE'], true_label: 'Sesuai', false_label: 'Tidak Sesuai'
        },
        {
            number: 4, type: 'matching', weight: 3,
            left_items: ['1', '2', '3'], right_items: ['A', 'B', 'C'],
            key: { '1': 'A', '2': 'B', '3': 'C' }, numbering: 'range'
        },
        {
            number: 7, type: 'short_answer', weight: 2,
            statements: ['Ibu kota', 'Hasil 3 + 4'], key: ['Jakarta', '7'],
            answer_order: 'any', partial_scoring: true
        }
    ]);

    assert.equal(preview.summary.cardCount, 5);
    assert.equal(preview.summary.numberCount, 7);
    assert.equal(preview.summary.totalWeight, 9);
    assert.equal(preview.summary.okCount, 5);
    assert.equal(preview.summary.warnCount, 0);
    assert.equal(preview.summary.errorCount, 0);

    assert.deepEqual(
        Array.from(preview.items[0].rows.filter(row => row.correct).map(row => row.text)),
        ['C']
    );
    assert.deepEqual(
        Array.from(preview.items[1].rows.filter(row => row.correct).map(row => row.text)),
        ['A', 'C']
    );
    assert.equal(preview.items[2].rows[0].answer, 'Sesuai');
    assert.equal(preview.items[2].rows[1].answer, 'Tidak Sesuai');
    assert.equal(preview.items[3].numberLabel, '4–6');
    assert.deepEqual(
        Array.from(preview.items[3].rows.map(row => `${row.text}->${row.answer}`)),
        ['1->A', '2->B', '3->C']
    );
    assert.deepEqual(
        Array.from(preview.items[4].rows.map(row => `${row.label}=${row.text}`)),
        ['Ibu kota=Jakarta', 'Hasil 3 + 4=7']
    );
    assert.ok(preview.items[4].meta.includes('Urutan bebas'));

    assert.equal(preview.items[0].quickAnswer, 'C');
    assert.equal(preview.items[1].quickAnswer, 'A · C');
    assert.equal(preview.items[2].quickAnswer, '1. Sesuai · 2. Tidak Sesuai');
    assert.equal(preview.items[3].quickAnswer, '1→A · 2→B · 3→C');
    assert.equal(preview.items[4].quickAnswer, 'Jakarta · 7');
});

test('kunci kosong dan kunci di luar pilihan terdeteksi', () => {
    const h = loadPreviewHelpers();
    const missing = h.collectQuestionPreviewIssues({
        number: 1, type: 'single_choice', weight: 1, options: ['A', 'B'], key: ''
    });
    assert.ok(missing.some(issue => issue.code === 'missing_key' && issue.level === 'error'));

    const outside = h.buildAnswerPreview([
        { number: 1, type: 'single_choice', weight: 1, options: ['A', 'B'], key: 'Z' }
    ]);
    assert.equal(outside.items[0].status, 'error');
    assert.equal(outside.items[0].missingKey, false);
    assert.ok(outside.items[0].issues.some(issue => issue.code === 'key_not_in_options'));

    const noKey = h.buildAnswerPreview([
        { number: 1, type: 'single_choice', weight: 1, options: ['A', 'B'], key: '' }
    ]);
    assert.equal(noKey.items[0].quickAnswer, '—');
});

test('jumlah kunci benar/salah dan isian harus cocok', () => {
    const h = loadPreviewHelpers();
    const tf = h.buildAnswerPreview([
        { number: 1, type: 'true_false', weight: 1, statements: ['Satu', 'Dua'], key: ['TRUE'] }
    ]);
    assert.ok(tf.items[0].issues.some(issue => issue.code === 'key_count_mismatch'));

    const short = h.buildAnswerPreview([
        { number: 1, type: 'short_answer', weight: 1, statements: ['Satu', 'Dua'], key: ['A'] }
    ]);
    assert.ok(short.items[0].issues.some(issue => issue.code === 'key_count_mismatch'));
});

test('pasangan menjodohkan yang belum lengkap berstatus perlu dicek', () => {
    const h = loadPreviewHelpers();
    const preview = h.buildAnswerPreview([
        {
            number: 1, type: 'matching', weight: 2,
            left_items: ['1', '2'], right_items: ['A', 'B'],
            key: { '1': 'A' }
        }
    ]);
    assert.equal(preview.items[0].status, 'warn');
    assert.equal(preview.items[0].missingKey, false);
    assert.ok(preview.items[0].issues.some(issue => issue.code === 'matching_missing_pair'));
});

test('nomor ganda dan nomor yang tidak terpakai dilaporkan', () => {
    const h = loadPreviewHelpers();
    const duplicate = h.buildAnswerPreview([
        { number: 1, type: 'single_choice', weight: 1, options: ['A'], key: 'A' },
        { number: 1, type: 'single_choice', weight: 1, options: ['A'], key: 'A' }
    ]);
    assert.ok(duplicate.items[1].issues.some(issue => issue.code === 'duplicate_number'));

    const gap = h.buildAnswerPreview([
        { number: 1, type: 'single_choice', weight: 1, options: ['A'], key: 'A' },
        { number: 3, type: 'single_choice', weight: 1, options: ['A'], key: 'A' }
    ]);
    assert.ok(gap.issues.some(issue => issue.code === 'number_gap'));
    assert.equal(h.answerPreviewNumberRanges([2, 3, 4, 7]), '2–4, 7');
});

test('rentang nomor mengikuti jumlah item atau isian', () => {
    const h = loadPreviewHelpers();
    assert.equal(h.answerPreviewSpan({
        type: 'matching', numbering: 'range', left_items: ['1', '2', '3'], right_items: ['A', 'B', 'C']
    }), 3);
    assert.equal(h.answerPreviewSpan({
        type: 'short_answer', numbering: 'range', statements: ['Satu', 'Dua'], key: ['A', 'B']
    }), 2);
    assert.equal(h.answerPreviewSpan({ type: 'single_choice', numbering: 'range' }), 1);
});

test('UI pratinjau, aksi, dan CSS print terpasang', () => {
    assert.match(DASHBOARD, /id="answerPreviewModal"/);
    assert.match(DASHBOARD, /id="btnPreviewAnswers"/);
    assert.match(DASHBOARD, /data-action="questions-preview-open"/);
    assert.match(DASHBOARD, /data-action="answer-preview-print"/);
    assert.match(DASHBOARD, /data-filter="issues"/);
    assert.match(DASHBOARD, /data-filter="missing"/);
    assert.match(DASHBOARD, /answer-preview-no-print/);
    assert.match(DASHBOARD, /id="answerPreviewQuickList"/);
    assert.match(DASHBOARD, /Ringkasan Jawaban/);
    assert.match(ADMIN_JS, /Actions\.register\('questions-preview-open'/);
    assert.match(ADMIN_JS, /Actions\.register\('answer-preview-filter'/);
    assert.match(ADMIN_JS, /Actions\.register\('answer-preview-print'/);
    assert.match(ADMIN_JS, /Actions\.register\('answer-preview-jump'/);
    assert.match(ADMIN_JS, /renderAnswerPreviewQuick\(/);
    assert.match(ADMIN_CSS, /@media print/);
    assert.match(ADMIN_CSS, /#answerPreviewModal/);
    assert.match(ADMIN_CSS, /break-inside: avoid/);
    assert.match(ADMIN_CSS, /\.answer-preview-quick-item/);
    assert.match(ADMIN_CSS, /html,\s*body,\s*body\.dashboard-page,\s*body\.modal-open\s*\{[^}]*overflow: visible;/s);
    assert.match(ADMIN_CSS, /body > \*:not\(#answerPreviewModal\)\s*\{\s*display: none;/);
    assert.match(ADMIN_CSS, /#answerPreviewModal \.answer-preview-stat-value\s*\{[^}]*color: rgb\(var\(--rgb-black\)\)/s);
    const renderer = extractFunction(ADMIN_JS, 'renderAnswerPreviewItem');
    assert.ok(renderer);
    assert.match(renderer, /escapeHtml\(/);
    const quickRenderer = extractFunction(ADMIN_JS, 'renderAnswerPreviewQuick');
    assert.ok(quickRenderer);
    assert.match(quickRenderer, /escapeHtml\(/);
});

test('bobot kosong atau nol ditampilkan sebagai bobot efektif 1', () => {
    const h = loadPreviewHelpers();
    const preview = h.buildAnswerPreview([
        { number: 1, type: 'single_choice', weight: 0, options: ['A'], key: 'A' },
        { number: 2, type: 'single_choice', options: ['A'], key: 'A' }
    ]);
    assert.equal(preview.items[0].weight, 1);
    assert.equal(preview.items[1].weight, 1);
    assert.equal(preview.summary.totalWeight, 2);
    assert.equal(preview.items[0].status, 'warn');
    assert.ok(preview.items[0].issues.some(issue => issue.code === 'weight_nonpositive'));
});
