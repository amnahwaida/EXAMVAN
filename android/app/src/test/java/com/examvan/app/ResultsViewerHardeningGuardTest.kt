package com.examvan.app

import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

/**
 * Guard hardening WebView halaman hasil (fix temuan review pasca-submit #1):
 *
 *  - allowFileAccess/allowContentAccess HARUS dimatikan eksplisit —
 *    default allowFileAccess = true di API <30 padahal WebView ini hanya
 *    boleh memuat URL server sendiri.
 *  - Error loading HARUS ditangani (onReceivedError / onReceivedHttpError)
 *    — dulu spinner berputar selamanya saat jaringan putus.
 *
 *   ./gradlew :app:testStudentDebugUnitTest
 */
class ResultsViewerHardeningGuardTest {

    private fun sourceFile(): File {
        val cwd = File(System.getProperty("user.dir") ?: ".")
        val module = when {
            File(cwd, "src/main/java").isDirectory -> cwd
            File(cwd, "app/src/main/java").isDirectory -> cwd.resolve("app")
            else -> error("Direktori sumber Android tidak ditemukan")
        }
        return module.resolve("src/main/java/com/examvan/app/ResultsViewerActivity.kt")
    }

    @Test
    fun fileAndContentAccess_disabled() {
        val src = sourceFile().readText()
        assertTrue(
            "allowFileAccess harus dinonaktifkan eksplisit",
            src.contains("allowFileAccess = false")
        )
        assertTrue(
            "allowContentAccess harus dinonaktifkan eksplisit",
            src.contains("allowContentAccess = false")
        )
    }

    @Test
    fun loadErrors_areHandled() {
        val src = sourceFile().readText()
        assertTrue(
            "onReceivedError harus di-handle agar spinner tidak abadi",
            src.contains("override fun onReceivedError")
        )
        assertTrue(
            "onReceivedHttpError harus di-handle (error 5xx server)",
            src.contains("override fun onReceivedHttpError")
        )
    }
}
