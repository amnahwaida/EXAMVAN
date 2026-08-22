package com.examvan.app.helper

import java.io.File

/**
 * Sumber tunggal cache PDF per ujian (fix temuan review render PDF #2 & #4).
 *
 * #4 — path "exam_$examId.pdf" dulu terduplikasi di dua titik.
 * #2 — dulu cache TIDAK PERNAH kedaluwarsa (kunci hanya examId): PDF hasil
 *      revisi pengawas tidak pernah sampai ke siswa. Kini cache usable
 *      hanya bila file ada, tidak kosong, dan usia lastModified masih
 *      dalam [MAX_AGE_MS].
 */
object PdfCache {

    /** Umur maksimum cache PDF sebelum dianggap basi. */
    const val MAX_AGE_MS: Long = 12 * 60 * 60 * 1000L // 12 jam

    /** File cache untuk satu ujian — satu-satunya tempat pola nama. */
    fun pdfFile(cacheDir: File, examId: Int): File =
        File(cacheDir, "exam_$examId.pdf")

    /** True bila file cache ada dan tidak kosong (tanpa cek usia). */
    fun hasCachedPdf(cacheDir: File, examId: Int): Boolean {
        val f = pdfFile(cacheDir, examId)
        return f.exists() && f.length() > 0
    }

    /**
     * True bila cache layak dipakai: file ada, tidak kosong, dan usianya
     * belum melewati [MAX_AGE_MS] (ambang eksklusif berdasar lastModified).
     */
    fun isCacheUsable(file: File, nowMs: Long, maxAgeMs: Long = MAX_AGE_MS): Boolean {
        if (!file.exists() || !file.isFile || file.length() <= 0) return false
        val age = nowMs - file.lastModified()
        return age in 0..maxAgeMs
    }
}
