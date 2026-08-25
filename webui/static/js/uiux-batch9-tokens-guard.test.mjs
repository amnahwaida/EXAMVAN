/* Guard Batch 9 (S43) — rgba literal di file JS statis.
 *
 * Latar belakang & dampak bisnis:
 *   Guard token Batch 7 hanya mencakup templates/ + admin.js hex. Re-review
 *   ronde 3 (S43) menemukan ±69 rgba literal dan ±25 hex tambahan tersebar di
 *   settings-*.js yang sama sekali tidak ter-guard: fitur baru bebas menambah
 *   warna hardcoded tanpa test merah. Test ini MENGUNCI baseline per file —
 *   angka tidak boleh NAIK; setiap warna baru wajib memakai var(--token).
 *
 *   Literal diukur dengan regex digit-pembuka /rgba\(\s*[0-9]/g sehingga
 *   pemakaian token rgba(var(--rgb-*), α) TIDAK dihitung (self-test di bawah).
 *
 * Pengecualian: fingerprintjs.min.js (vendor) dan *.test.mjs (suite ini).
 *
 * Batch 10 (S58 lanjutan): migrasi substitusi-persis diperluas ke sisi CSS —
 * admin-base.css kini di-guard plafon rgba literalnya (60 → 17) oleh
 * uiux-batch7-tokens.test.mjs + asersi token di uiux-batch10-tokens-guard.test.mjs.
 * Suite ini tetap mencakup file JS saja.
 *
 * Run with:  node --test static/js/uiux-batch9-tokens-guard.test.mjs   (from webui/)
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));

const HEX_RE = /#[0-9a-fA-F]{3,8}\b/g;
// S43: hanya rgba dengan DIGIT pembuka = literal sungguhan;
// rgba(var(--rgb-*), α) adalah pemakaian token, bukan literal.
const RGBA_RE = /rgba\(\s*[0-9]/g;

function countIn(file, re) {
    const src = fs.readFileSync(path.join(__dirname, file), 'utf8');
    return (src.match(re) || []).length;
}

test('S43 (self-test): regex rgba literal tidak menghitung rgba(var( sebagai literal', () => {
    assert.equal(('rgba(var(--rgb-white), 0.1)'.match(RGBA_RE) || []).length, 0,
        'rgba(var(...) harus dianggap pemakaian token, bukan literal');
    assert.equal(('rgba(  var(--rgb-black), .5 )'.match(RGBA_RE) || []).length, 0,
        'spasi sebelum var( tetap bukan literal');
    assert.equal(('rgba(255,255,255,0.1)'.match(RGBA_RE) || []).length, 1,
        'rgba digit pembuka adalah literal sungguhan dan harus terhitung');
});

// Baseline terkunci hasil ukur langsung (24 Agustus 2026). Jangan dinaikkan
// tanpa alasan terdokumentasi; turunkan begitu agen migrasi mengurangi angka.
const BASELINES = {
    // admin.js: hex ≤8 sudah diguard batch7-tokens; rgba ditambahkan di sini.
    'admin.js': { rgba: 33 }, /* Batch 16: dropdown/chip migrasi token */
    // R95 (ronde 8): cap hex dikunci ke angka aktual terukur — 3× #fca5a5
    // bermigrasi var(--color-danger-light) sehingga plafon turun 22→9.
    'settings-vouchers.js': { rgba: 9, hex: 9 },
    'settings-voucher-audit.js': { rgba: 2, hex: 2 },
    // Catatan S43: estimasi awal "billing 1" ternyata angka HEX-nya;
    // rgba aktual billing = 8 — dikunci pada baseline hari ini.
    'settings-billing.js': { rgba: 8, hex: 1 },
    // S88 (ronde 8): seluruh modul JS statis kini punya entri eksplisit —
    // penambahan warna hardcoded PERTAMA pada modul mana pun memerah test.
    'settings-system-apps.js': { rgba: 2, hex: 0 }, // empty-state render-JS
    'settings-users.js': { rgba: 0, hex: 0 },
    'settings-general.js': { rgba: 0, hex: 0 },
    'settings-packages.js': { rgba: 0, hex: 0 },
    'admin-core.js': { rgba: 0, hex: 0 },
    // S99 (ronde 9): blind-spot terakhir ditutup — kedua modul pengawasan
    // ini sebelumnya TANPA entri padahal klaim S88 "semua modul". Aktual
    // hasil ukur Batch 15 = 0/0; penambahan warna hardcoded pertama memerah.
    'pengawas-detail.js': { rgba: 0, hex: 0 },
    'device-fingerprint.js': { rgba: 0, hex: 0 },
};

for (const [file, caps] of Object.entries(BASELINES)) {
    if (caps.rgba !== undefined) {
        test(`S43 (guard): rgba literal di ${file} tidak naik dari baseline`, () => {
            const n = countIn(file, RGBA_RE);
            assert.ok(n <= caps.rgba,
                `rgba literal ${file} = ${n}, baseline ≤ ${caps.rgba} — pakai rgba(var(--rgb-*), α)`);
        });
    }
    if (caps.hex !== undefined) {
        test(`S43 (guard): hex literal di ${file} tidak naik dari baseline`, () => {
            const n = countIn(file, HEX_RE);
            assert.ok(n <= caps.hex,
                `hex literal ${file} = ${n}, baseline ≤ ${caps.hex} — pakai var(--token)`);
        });
    }
}
