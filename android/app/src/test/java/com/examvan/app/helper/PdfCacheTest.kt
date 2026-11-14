package com.examvan.app.helper

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

/**
 * Mengunci kontrak cache PDF (fix temuan review render PDF #2 & #4):
 *
 * #4 — path cache terduplikasi di dua tempat ("exam_$examId.pdf") →
 *      satu sumber: [PdfCache.pdfFile].
 * #2 — cache TIDAK PERNAH kedaluwarsa (kunci hanya examId): pengawas yang
 *      mengganti file PDF tidak pernah sampai ke siswa. Kontrak baru:
 *      cache usable hanya bila file ada, tidak kosong, dan usianya masih
 *      dalam batas [PdfCache.MAX_AGE_MS] (default 12 jam) berdasarkan
 *      lastModified.
 *
 *   ./gradlew :app:testStudentDebugUnitTest
 */
class PdfCacheTest {

    private fun newTempDir(): File =
        File(System.getProperty("java.io.tmpdir"), "pdfcache-${System.nanoTime()}")
            .apply { mkdirs() }

    @Test
    fun pdfFile_nameFollowsConvention() {
        val dir = newTempDir()
        assertEquals("exam_42.pdf", PdfCache.pdfFile(dir, 42).name)
        dir.deleteRecursively()
    }

    @Test
    fun hasCachedPdf_existingNonEmpty_true() {
        val dir = newTempDir()
        val f = PdfCache.pdfFile(dir, 1).apply { writeText("%PDF-1.4") }

        assertTrue(PdfCache.hasCachedPdf(dir, 1))
        f.delete(); dir.deleteRecursively()
    }

    @Test
    fun hasCachedPdf_missingOrEmpty_false() {
        val dir = newTempDir()
        assertFalse(PdfCache.hasCachedPdf(dir, 1)) // tidak ada
        PdfCache.pdfFile(dir, 2).writeText("")     // ada tapi kosong
        assertFalse(PdfCache.hasCachedPdf(dir, 2))
        dir.deleteRecursively()
    }

    // ── Kedaluwarsa cache (#2) ───────────────────────────────────────────

    @Test
    fun isCacheUsable_freshFile_usable() {
        val dir = newTempDir()
        val f = PdfCache.pdfFile(dir, 1).apply { writeText("%PDF-1.4") }
        val lastMod = System.currentTimeMillis()
        f.setLastModified(lastMod)

        assertTrue(PdfCache.isCacheUsable(f, nowMs = lastMod + 5_000L))
        dir.deleteRecursively()
    }

    @Test
    fun isCacheUsable_olderThanMaxAge_notUsable() {
        val dir = newTempDir()
        val f = PdfCache.pdfFile(dir, 1).apply { writeText("%PDF-1.4") }
        val lastMod = System.currentTimeMillis()
        f.setLastModified(lastMod)

        // Sesi ulang keesokan hari: PDF hasil revisi pengawas harus terunduh ulang.
        assertFalse(
            PdfCache.isCacheUsable(f, nowMs = lastMod + PdfCache.MAX_AGE_MS + 60_000L)
        )
        dir.deleteRecursively()
    }

    @Test
    fun isCacheUsable_exactBoundary_stillUsable() {
        val dir = newTempDir()
        val f = PdfCache.pdfFile(dir, 1).apply { writeText("%PDF-1.4") }
        val lastMod = System.currentTimeMillis()
        f.setLastModified(lastMod)

        assertTrue(
            "Ambang eksklusif: tepat di batas usia masih dianggap usable",
            PdfCache.isCacheUsable(f, nowMs = lastMod + PdfCache.MAX_AGE_MS)
        )
        dir.deleteRecursively()
    }

    @Test
    fun isCacheUsable_missingEmptyOrDirectory_notUsable() {
        val dir = newTempDir()
        assertFalse(PdfCache.isCacheUsable(PdfCache.pdfFile(dir, 1), System.currentTimeMillis()))
        val empty = PdfCache.pdfFile(dir, 2).apply { writeText("") }
        assertFalse(PdfCache.isCacheUsable(empty, System.currentTimeMillis()))
        assertFalse(PdfCache.isCacheUsable(dir, System.currentTimeMillis())) // direktori
        dir.deleteRecursively()
    }

    // ------------------------------------------------------------------
    // Pembersihan pasca-ujian (review klien 14 Nov 2026): naskah soal
    // tidak boleh menetap di perangkat siswa setelah jawaban durable /
    // "Hapus Data Tersimpan". JUnit4: pesan di depan, kondisi di belakang.
    // ------------------------------------------------------------------

    @Test
    fun clear_removesExamPdfOnly() {
        val dir = newTempDir()
        val target = PdfCache.pdfFile(dir, 42).apply { writeText("%PDF-1.4") }
        val other = File(dir, "exam_99.pdf").apply { writeText("%PDF-1.4") }
        val unrelated = File(dir, "unrelated.txt").apply { writeText("x") }

        val removed = PdfCache.clear(dir, 42)

        assertTrue("pdf ujian terhapus", removed)
        assertFalse("target hilang", target.exists())
        assertTrue("cache ujian lain utuh", other.exists())
        assertTrue("berkas non-PDF utuh", unrelated.exists())
        dir.deleteRecursively()
    }

    @Test
    fun clear_returnsFalseWhenFileMissing() {
        val dir = newTempDir()
        assertFalse("tanpa file -> false", PdfCache.clear(dir, 7))
    }

    @Test
    fun clearAll_removesEveryExamPdfAndCounts() {
        val dir = newTempDir()
        listOf(1, 2, 3).forEach { PdfCache.pdfFile(dir, it).writeText("%PDF-1.4") }
        File(dir, "unrelated.txt").writeText("x")

        val removed = PdfCache.clearAll(dir)

        assertEquals("3 pdf ujian terhapus", 3, removed)
        assertTrue("berkas non-PDF utuh", File(dir, "unrelated.txt").exists())
        dir.deleteRecursively()
    }

    @Test
    fun clearAll_returnsZeroOnEmptyDir() {
        val dir = newTempDir()
        assertEquals("dir kosong -> nol", 0, PdfCache.clearAll(dir))
    }
}
