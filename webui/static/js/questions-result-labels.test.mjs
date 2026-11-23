import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';

const HERE = path.dirname(fileURLToPath(import.meta.url));
const HASIL_HTML = fs.readFileSync(
    path.join(HERE, '..', '..', 'templates', 'public', 'hasil.html'), 'utf8'
);

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

function loadFormatAnswer() {
    const sandbox = {
        escapeHtml: (s) => String(s == null ? '' : s)
            .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;'),
    };
    vm.createContext(sandbox);
    for (const name of ['trueFalseAnswerLabel', 'formatAnswer']) {
        const fn = extractFunction(HASIL_HTML, name);
        assert.ok(fn, `${name} harus ada di hasil.html`);
        vm.runInContext(fn, sandbox);
    }
    return sandbox;
}

test('rincian hasil memakai label true/false kustom', () => {
    const h = loadFormatAnswer();
    const q = { type: 'true_false', true_label: 'Sesuai', false_label: 'Tidak Sesuai' };
    assert.equal(h.formatAnswer('TRUE', q), 'Sesuai');
    assert.equal(h.formatAnswer('FALSE', q), 'Tidak Sesuai');
    assert.equal(h.formatAnswer('true', q), 'Sesuai');
});

test('rincian hasil true/false multi memakai label kustom per pernyataan', () => {
    const h = loadFormatAnswer();
    const q = {
        type: 'true_false',
        statements: ['Air membeku pada 0 derajat.', '2 + 2 = 5'],
        true_label: 'Sesuai',
        false_label: 'Tidak Sesuai',
    };
    const out = h.formatAnswer(['TRUE', 'FALSE'], q);
    assert.match(out, /Air membeku pada 0 derajat/);
    assert.match(out, /Sesuai/);
    assert.match(out, /2 \+ 2 = 5/);
    assert.match(out, /Tidak Sesuai/);
    assert.ok(!out.includes('&gt; TRUE'), 'nilai mentah TRUE tidak boleh tampil');
});

test('rincian hasil short answer multi menampilkan label dan nilai per isian', () => {
    const h = loadFormatAnswer();
    const q = { type: 'short_answer', statements: ['Ibu kota', 'Hasil 3 + 4'] };
    const out = h.formatAnswer(['Jakarta', '7'], q);
    assert.match(out, /Ibu kota/);
    assert.match(out, /Jakarta/);
    assert.match(out, /Hasil 3 \+ 4/);
    assert.match(out, /7/);
});

test('rincian hasil tetap memakai fallback Benar/Salah', () => {
    const h = loadFormatAnswer();
    assert.equal(h.formatAnswer('TRUE', { type: 'true_false' }), 'Benar');
    assert.equal(h.formatAnswer('FALSE', { type: 'true_false' }), 'Salah');
});
