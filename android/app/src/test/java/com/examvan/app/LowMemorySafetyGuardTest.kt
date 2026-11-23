package com.examvan.app

import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

/**
 * Guard perilaku low-memory: bitmap PDF adalah konsumen terbesar, jadi harus
 * dilepas saat UI tidak terlihat / sistem meminta memori, dan referensi
 * Activity tidak boleh tertahan lama oleh singleton/handler.
 *
 *   ./gradlew :app:testStudentDebugUnitTest
 */
class LowMemorySafetyGuardTest {

    private fun resolveModuleDir(): File {
        val cwd = File(System.getProperty("user.dir") ?: ".")
        return when {
            File(cwd, "src/main/java").isDirectory -> cwd
            File(cwd, "app/src/main/java").isDirectory -> cwd.resolve("app")
            else -> error("Direktori sumber Android tidak ditemukan (cwd=${cwd.absolutePath})")
        }
    }

    private fun read(relativePath: String): String =
        resolveModuleDir().resolve(relativePath).readText()

    private fun examViewer(): String =
        read("src/main/java/com/examvan/app/ExamViewerActivity.kt")

    private fun pdfRenderer(): String =
        read("src/main/java/com/examvan/app/helper/PdfRendererHelper.kt")

    @Test
    fun examViewer_releasesPdfOnStopAndMemoryPressure() {
        val src = examViewer()
        val onStop = src.substringAfter("override fun onStop()")
            .substringBefore("override fun onTrimMemory(")
        val onTrim = src.substringAfter("override fun onTrimMemory(")
            .substringBefore("override fun onLowMemory(")
        val onLow = src.substringAfter("override fun onLowMemory(")
            .substringBefore("override fun onUserLeaveHint(")

        assertTrue(
            "onStop wajib melepas renderer/bitmap PDF",
            onStop.contains("releaseForBackground(force = true)")
        )
        assertTrue(
            "onTrimMemory wajib melepas PDF saat UI hidden",
            onTrim.contains("PdfMemoryPolicy.shouldReleaseForTrim(level)") &&
                onTrim.contains("releaseForBackground(force = true)")
        )
        assertTrue(
            "onLowMemory wajib melepas PDF saat activity tidak resumed",
            onLow.contains("releaseForBackground(force = true)")
        )
    }

    @Test
    fun pdfCleanup_clearsImageViewBeforeRecyclingBitmap() {
        val cleanup = pdfRenderer()
            .substringAfter("fun cleanup()")
            .substringBefore("companion object")
        assertTrue(
            "ImageView harus dilepas sebelum bitmap di-recycle",
            cleanup.contains("binding.ivPdfPage.setImageDrawable(null)") &&
                cleanup.contains("currentBitmap?.recycle()")
        )
    }

    @Test
    fun securityCleanup_cancelsFocusLossRunnable() {
        val cleanup = read("src/main/java/com/examvan/app/helper/SecurityEnforcer.kt")
            .substringAfter("fun cleanup()")
            .substringBefore("}\n}")
        assertTrue(
            "cleanup harus membatalkan focusLostRunnable agar Activity tidak tertahan",
            cleanup.contains("removeCallbacks") &&
                cleanup.contains("focusLostRunnable = null")
        )
    }

    @Test
    fun lockTaskDeactivate_cancelsPollChainHoldingActivity() {
        val deactivate = read("src/main/java/com/examvan/app/LockTaskManager.kt")
            .substringAfter("fun deactivate(activity: Activity)")
            .substringBefore("/**\n     * Cek apakah lock task")
        assertTrue(
            "deactivate harus membatalkan rantai poll yang menahan Activity",
            deactivate.contains("cancelPollChain()")
        )
    }
}
