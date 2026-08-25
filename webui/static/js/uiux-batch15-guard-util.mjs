/**
 * Util bersama guard UI/UX — dipakai lintas suite uiux-batch*.test.mjs.
 * BUKAN bagian dari aplikasi (tidak dimuat halaman mana pun); hanya asset
 * pengujian. Nama berawalan `uiux-batch` agar pengecualian guard folder-wide
 * (mis. scan JS uiux-batch14-tokens-guard) tetap berlaku.
 */

/**
 * S104 (review_uiux_webui.md §5.12): pola `html.slice(html.indexOf(marker))`
 * VAKUM ketika marker tidak ada di sumber — `indexOf` mengembalikan −1,
 * `slice(-1)` menyisakan SATU karakter terakhir, `assert.ok(length > 0)`
 * lolos trivially, dan seluruh asersi di atas "blok" itu mengamati string
 * kosong tanpa suara. Preseden: T29 — guard escape kartu identitas hasil
 * Batch 14 ternyata tidak pernah mengamati apa pun (marker
 * `detail-identity-items` plural = 0 hit di hasil.html).
 *
 * Kontrak: sliceBlock THROW bila marker tidak ditemukan. Guard yang kehilangan
 * anchor harus MEMERAH dengan pesan yang jelas, bukan diam-diam memeriksa
 * string kosong. Pesan error sengaja menyebut T29/S104 supaya eksekutor batch
 * berikutnya langsung diarahkan ke akar masalah.
 */
export function sliceBlock(html, marker, label = marker) {
    const idx = html.indexOf(marker);
    if (idx < 0) {
        throw new Error(
            `sliceBlock: marker ${JSON.stringify(String(label))} TIDAK ditemukan di sumber — ` +
            'guard ini sedang VAKUM (pola T29/S104: slice(indexOf(marker)) dengan marker 0 hit ' +
            'menjadi no-op). Perbarui anchor ke id/class markup aktual, JANGAN hapus asertinya.');
    }
    return html.slice(idx);
}
