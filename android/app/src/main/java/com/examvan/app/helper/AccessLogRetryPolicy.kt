package com.examvan.app.helper

/**
 * Kebijakan retry access-log (fix temuan review low-mode ronde 2 #2).
 *
 * LATAR: jalur keluar-bebas (low) melaporkan kepergian via event logout
 * fire-and-forget. Bila gagal, dasbor pengawas menampilkan siswa "online"
 * sampai TTL presence (5 menit) habis — /complete memang tidak dipanggil
 * karena ujian belum selesai.
 *
 * Aturan: hanya LOGOUT yang di-retry — login/heartbeat dikirim ulang rutin
 * sehingga kegagalan satu kali tertambal sendiri; retry justru menambah
 * trafik tanpa manfaat.
 */
object AccessLogRetryPolicy {

    /** Total percobaan maksimum (1 asli + 1 retry). */
    const val MAX_ATTEMPTS = 2

    /** Jeda antar percobaan. */
    const val RETRY_DELAY_MS = 750L

    fun isRetryableEvent(event: String): Boolean =
        event == AccessLogPolicy.EVENT_LOGOUT

    /**
     * True bila [event] layak dicoba ulang setelah percobaan ke-[attempt]
     * gagal ([success] = false) dan belum mencapai batas.
     */
    fun shouldRetry(event: String, attempt: Int, success: Boolean): Boolean {
        if (success) return false
        if (!isRetryableEvent(event)) return false
        return attempt < MAX_ATTEMPTS
    }
}
