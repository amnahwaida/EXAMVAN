package com.examvan.app.helper

import java.time.Instant

/**
 * Perhitungan sisa waktu ujian dari `end_time` (ISO-8601) dan server time
 * skew — murni, bisa diuji di JVM.
 *
 * Countdown harus dihitung ulang dari DEADLINE ABSOLUT setiap kali (resume,
 * PDF siap) menggunakan waktu server (`now + skew`) agar tidak melenceng
 * setelah pause/resume atau perubahan jam perangkat.
 */
internal object ExamDeadline {

    /**
     * Sisa waktu (ms) sampai ujian berakhir, atau `null` bila end_time
     * tidak ada / tidak dapat di-parse (maka countdown tidak ditampilkan).
     *
     * @param endTimeIso end_time dari server (ISO-8601, format UTC).
     * @param nowMs      waktu perangkat sekarang.
     * @param skewMs     selisih jam perangkat vs server (ApiClient.serverTimeSkewMs).
     */
    fun remainingMs(endTimeIso: String?, nowMs: Long, skewMs: Long): Long? {
        val end = endTimeIso ?: return null
        return try {
            val endInstantMs = Instant.parse(end).toEpochMilli()
            endInstantMs - (nowMs + skewMs)
        } catch (e: Exception) {
            null
        }
    }

    /**
     * Komponen jam/menit/detik untuk tampilan timer (fix review ronde 2 #1:
     * pemformatan pindah ke string resource agar i18n; object murni ini hanya
     * menyediakan komponen angka). Jam tidak dipotong modulo 24; negatif
     * dicukup ke nol.
     */
    fun remainingHms(millisRemaining: Long): List<Long> {
        val totalSeconds = millisRemaining.coerceAtLeast(0L) / 1000
        return listOf(
            totalSeconds / 3600,
            (totalSeconds % 3600) / 60,
            totalSeconds % 60
        )
    }
}
