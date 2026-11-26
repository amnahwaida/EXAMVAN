import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';

const ROOT = new URL('.', import.meta.url).pathname.replace(/\/static\/js\/$/, '');
const ADMIN_JS = readFileSync(join(ROOT, 'static/js/admin.js'), 'utf8');

function openEditUserModalBlock() {
    const start = ADMIN_JS.indexOf('function openEditUserModal(');
    assert.ok(start > -1, 'openEditUserModal harus ada');
    const end = ADMIN_JS.indexOf('\nfunction syncEditLimitFields', start);
    assert.ok(end > start, 'batas openEditUserModal harus ditemukan');
    return ADMIN_JS.slice(start, end);
}

test('kuota PDF 0 (tidak terbatas) tidak diubah menjadi 1 MB saat modal dibuka', () => {
    const block = openEditUserModalBlock();
    assert.match(block, /editUserPdfSize'\)\.value\s*=\s*user\.max_pdf_size == null\s*\?\s*'1'/,
        'hanya nilai kosong/null yang memakai fallback 1 MB');
    assert.doesNotMatch(block, /editUserPdfSize'\)\.value\s*=\s*user\.max_pdf_size\s*\?/,
        'nilai 0 tidak boleh dianggap kosong');
});

test('paket kustom tidak direset saat modal Atur User dibuka', () => {
    const block = openEditUserModalBlock();
    assert.match(block, /var currentPackage = user\.package \|\| 'free';/);
    assert.match(block, /Paket khusus: /, 'label paket kustom harus tetap terlihat');
    assert.match(block, /editPkgSelect\.value = currentPackage;/, 'paket kustom harus tetap terpilih');
    assert.doesNotMatch(block, /getElementById\('editUserPackage'\)\.value = user\.package \|\| 'free';/,
        'paket kustom tidak boleh jatuh ke opsi default');
});
