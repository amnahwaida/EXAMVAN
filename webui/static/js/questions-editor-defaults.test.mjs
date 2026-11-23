import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';

const HERE = path.dirname(fileURLToPath(import.meta.url));
const WEBUI_ROOT = path.join(HERE, '..', '..');
const ADMIN_JS = fs.readFileSync(path.join(WEBUI_ROOT, 'static', 'js', 'admin.js'), 'utf8');

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

function loadHelpers() {
    const sandbox = {};
    vm.createContext(sandbox);
    for (const name of ['questionTypeSupportsPartial', 'resolvePartialScoring', 'defaultTrueFalseStatements']) {
        const fn = extractFunction(ADMIN_JS, name);
        assert.ok(fn, `${name} harus ada di admin.js`);
        vm.runInContext(fn, sandbox);
    }
    return sandbox;
}

test('partial scoring default aktif hanya untuk tipe yang mendukung', () => {
    const h = loadHelpers();
    assert.equal(h.resolvePartialScoring('multiple_choice', undefined), true);
    assert.equal(h.resolvePartialScoring('matching', undefined), true);
    assert.equal(h.resolvePartialScoring('true_false', undefined), true);
    assert.equal(h.resolvePartialScoring('single_choice', undefined), false);
    assert.equal(h.resolvePartialScoring('short_answer', undefined), true);
    assert.equal(h.resolvePartialScoring('multiple_choice', false), false);
    assert.equal(h.resolvePartialScoring('multiple_choice', 'false'), false);
});

test('generate true/false mengisi dua pernyataan default dan kunci yang sesuai', () => {
    const gen = extractFunction(ADMIN_JS, 'quickGenerateQuestions');
    assert.ok(gen, 'quickGenerateQuestions ada');
    assert.match(gen, /defaultTrueFalseStatements\(\)/);
    assert.match(gen, /q\.key = \['TRUE', 'FALSE'\]/);
    assert.match(gen, /q\.partial_scoring = resolvePartialScoring\(type, undefined\)/);
    assert.doesNotMatch(gen, /weight: 1\.0/, 'bobot awal harus auto agar rentang mengikuti jumlah field');
});

test('kartu editor memuat input label true/false dan default parsial', () => {
    const card = extractFunction(ADMIN_JS, 'createNewQuestionCard');
    assert.ok(card, 'createNewQuestionCard ada');
    assert.match(card, /q-tf-true-label/);
    assert.match(card, /q-tf-false-label/);
    assert.match(card, /resolvePartialScoring\(type, q\.partial_scoring\)/);
});

test('kartu short answer memuat daftar isian, kunci per baris, dan numbering', () => {
    const card = extractFunction(ADMIN_JS, 'createNewQuestionCard');
    assert.ok(card, 'createNewQuestionCard ada');
    assert.match(card, /q-short-statements-input/);
    assert.match(card, /q-short-keys-input/);
    assert.match(card, /q-short-order-select/);
    assert.match(card, /q-short-numbering-select/);
    assert.match(card, /MAX_SHORT_ANSWER_FIELDS/);
    assert.match(card, /partial_scoring\)/);
});

test('editor mengirim answer_order any hanya saat guru memilih urutan bebas', () => {
    const getFn = extractFunction(ADMIN_JS, 'getQuestionsFromEditor');
    assert.ok(getFn, 'getQuestionsFromEditor ada');
    assert.match(getFn, /q-short-order-select/);
    assert.match(getFn, /q\.answer_order = 'any'/);
});

test('rentang nomor menyinkronkan bobot default mengikuti jumlah field', () => {
    const spanFn = extractFunction(ADMIN_JS, 'questionStructureSpan');
    const syncFn = extractFunction(ADMIN_JS, 'syncQuestionAutoWeight');
    assert.ok(spanFn, 'questionStructureSpan ada');
    assert.ok(syncFn, 'syncQuestionAutoWeight ada');
    assert.match(spanFn, /q-short-statements-input/);
    assert.match(spanFn, /q-short-keys-input/);
    assert.match(spanFn, /q-matching-numbering-select/);
    assert.match(syncFn, /dataset\.autoWeight/);
    assert.match(syncFn, /weightInput\.value = span > 1 \? String\(span\) : '1\.0'/);
});

test('export dan import XML membawa label true/false serta default parsial', () => {
    const exportFn = extractFunction(ADMIN_JS, 'exportXMLQuestions');
    assert.ok(exportFn, 'exportXMLQuestions ada');
    assert.match(exportFn, /<true_label>\$\{xmlEscape\(q\.true_label\)\}<\/true_label>/);
    assert.match(exportFn, /<false_label>\$\{xmlEscape\(q\.false_label\)\}<\/false_label>/);
    assert.match(exportFn, /q\.type === 'short_answer' && Array\.isArray\(q\.key\)/);
    assert.match(exportFn, /q\.answer_order === 'any'/);
    assert.match(exportFn, /answer_order="any"/);

    const importStart = ADMIN_JS.indexOf('function importXMLQuestions');
    assert.ok(importStart !== -1, 'importXMLQuestions ada');
    const importFn = ADMIN_JS.slice(importStart, importStart + 8000);
    assert.match(importFn, /parseTextNode\('true_label'\)/);
    assert.match(importFn, /parseTextNode\('false_label'\)/);
    assert.match(importFn, /partialAttr === null \|\| partialAttr\.trim\(\) === ''/);
    assert.match(importFn, /resolvePartialScoring\(type, undefined\)/);
    assert.match(importFn, /type === 'short_answer'[\s\S]*statements = parseLinesNode\('statements'\)/);
    assert.match(importFn, /qData\.numbering = numbering/);
    assert.match(importFn, /answer_order/);
    assert.match(importFn, /numbering === "range"[\s\S]*weight = Math\.max\(/);

    const problemsFn = extractFunction(ADMIN_JS, 'findQuestionCardProblems');
    assert.ok(problemsFn, 'findQuestionCardProblems ada');
    assert.match(problemsFn, /MAX_SHORT_ANSWER_FIELDS/);
});
