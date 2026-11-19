package com.examvan.app.helper

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

/**
 * Guard konfigurasi bitmap PdfRenderer (bug perangkat Realme C11: layar putih
 * + "Gagal menampilkan halaman PDF").
 *
 * Kontrak platform: PdfRenderer.Page.render() hanya menerima
 * Bitmap.Config.ARGB_8888. Konfigurasi lain (mis. RGB_565) melempar
 * IllegalArgumentException("Unsupported pixel format") di semua versi Android,
 * sehingga halaman gagal dirender. Penghematan memori wajib lewat resolusi.
 *
 *   ./gradlew :app:testStudentDebugUnitTest
 */
class PdfRendererConfigGuardTest {

    private fun sourceFile(): File {
        val cwd = File(System.getProperty("user.dir") ?: ".")
        val module = when {
            File(cwd, "src/main/java").isDirectory -> cwd
            File(cwd, "app/src/main/java").isDirectory -> cwd.resolve("app")
            else -> error("Direktori sumber Android tidak ditemukan")
        }
        return module.resolve(
            "src/main/java/com/examvan/app/helper/PdfRendererHelper.kt"
        )
    }

    @Test
    fun pdfRenderer_usesOnlyArgb8888() {
        val src = sourceFile().readText()

        assertTrue(
            "PdfRendererHelper wajib membuat bitmap ARGB_8888",
            src.contains("Bitmap.Config.ARGB_8888")
        )
        assertFalse(
            "PdfRendererHelper tidak boleh memakai RGB_565 — PdfRenderer " +
                "melempar IllegalArgumentException untuk format selain ARGB_8888",
            src.contains("Bitmap.Config.RGB_565")
        )
    }
}
