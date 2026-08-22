package com.examvan.app.helper

/**
 * Kebijakan UI unduhan PDF (fix review UI/UX #3): tombol batal berbasis
 * WAKTU — tersembunyi selama grace period 3 detik pertama (mencegah batal
 * gegabah), lalu tampil untuk sisa durasi. Dulu `if (percent < 50) visible`
 * terbaca terbalik dan tidak pernah menyembunyikan kembali.
 */
object DownloadUiPolicy {

    const val CANCEL_GRACE_MS = 3_000L

    fun cancelVisible(elapsedMs: Long): Boolean = elapsedMs >= CANCEL_GRACE_MS
}
