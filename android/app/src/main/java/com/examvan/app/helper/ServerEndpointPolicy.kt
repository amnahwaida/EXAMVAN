package com.examvan.app.helper

/**
 * Kebijakan endpoint server bawaan (permintaan pemilik produk: endpoint
 * examvan.my.id TIDAK BOLEH hilang — baik saat "Hapus Data Tersimpan",
 * reset, maupun kegagalan penyimpanan; user tetap bebas MENGGANTINYA
 * secara manual jika ingin pindah server).
 *
 * Sumber nilai default tunggal: [DEFAULT_SERVER_URL]. Dipakai oleh
 * ServerConfigActivity untuk fallback tampilan awal DAN pemulihan
 * setelah clear data.
 */
object ServerEndpointPolicy {

    const val DEFAULT_SERVER_URL = "https://examvan.my.id"

    /**
     * URL yang ditampilkan di field input.
     * @param savedUrl nilai tersimpan dari prefs (null/kosong/blank aman).
     * @return URL kustom milik user apa adanya, atau [DEFAULT_SERVER_URL]
     *         bila tidak ada — field tidak pernah dibiarkan kosong.
     */
    fun resolveDisplayUrl(savedUrl: String?): String {
        return if (savedUrl.isNullOrBlank()) DEFAULT_SERVER_URL else savedUrl
    }
}
