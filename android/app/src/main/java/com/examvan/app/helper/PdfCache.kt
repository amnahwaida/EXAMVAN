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

    // ------------------------------------------------------------------
    // Pembersihan pasca-ujian (review klien 14 Nov 2026): naskah soal
    // (the exam paper) tidak boleh menetap di perangkat siswa setelah
    // ujian selesai. Dipanggil saat jawaban terkonfirmasi durable dan
    // saat "Hapus Data Tersimpan".
    // ------------------------------------------------------------------

    /** Hapus cache PDF satu ujian. Return true bila file (ada dan) terhapus. */
    fun clear(cacheDir: File, examId: Int): Boolean {
        val f = pdfFile(cacheDir, examId)
        return f.exists() && f.delete()
    }

    /** Hapus SEMUA cache PDF ujian (pola nama exam_*.pdf di cacheDir). */
    fun clearAll(cacheDir: File): Int {
        val files = cacheDir.listFiles { f -> f.name.startsWith("exam_") && f.name.endsWith(".pdf") }
            ?: return 0
        return files.count { it.delete() }
    }

    /**
     * Bersihkan sisa file PDF dari sesi lama saat app start.
     *
     * - `exam_*.pdf` yang lebih tua dari [maxAgeMs] dihapus (sama dengan
     *   batas [isCacheUsable]; file basi memang akan diunduh ulang).
     * - `temp_exam_*.pdf` (hasil unduhan gagal/terputus) selalu dihapus:
     *   dipanggil di background saat startup, sebelum ada unduhan baru.
     *
     * File yang tidak cocok pola dan file `exam_*.pdf` yang masih segar
     * tidak disentuh. Return jumlah file yang terhapus.
     */
    fun pruneStale(
        cacheDir: File,
        nowMs: Long,
        maxAgeMs: Long = MAX_AGE_MS
    ): Int {
        val files = cacheDir.listFiles { f ->
            f.isFile && (
                (f.name.startsWith("exam_") && f.name.endsWith(".pdf")) ||
                    (f.name.startsWith("temp_exam_") && f.name.endsWith(".pdf"))
                )
        } ?: return 0

        var deleted = 0
        for (file in files) {
            val isTemp = file.name.startsWith("temp_exam_")
            val age = nowMs - file.lastModified()
            val stale = isTemp || age > maxAgeMs
            if (stale && file.delete()) deleted++
        }
        return deleted
    }
}
