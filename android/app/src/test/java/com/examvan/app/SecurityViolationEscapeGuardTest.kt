package com.examvan.app

import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

/**
 * Guard: layar security violation (root, USB debugging, screen mirroring,
 * clock drift, emulator) tidak boleh menjadi jalan buntu.
 *
 * Sebelum fix, layar ini hanya punya "Periksa Ulang" dan back diblokir
 * strict — siswa tidak bisa keluar untuk memperbaiki setting perangkat.
 *
 * Kontrak:
 *  - ada tombol "Keluar",
 *  - back mengikuti jalur keluar yang sama,
 *  - pra-ujian: logout + finish; setelah PDF siap: auto-submit + exit.
 *
 *   ./gradlew :app:testStudentDebugUnitTest
 */
class SecurityViolationEscapeGuardTest {

    private fun resolveModuleDir(): File {
        val cwd = File(System.getProperty("user.dir") ?: ".")
        return when {
            File(cwd, "src/main/java").isDirectory -> cwd
            File(cwd, "app/src/main/java").isDirectory -> cwd.resolve("app")
            else -> error("Direktori sumber Android tidak ditemukan (cwd=${cwd.absolutePath})")
        }
    }

    private fun securityEnforcer(): String =
        resolveModuleDir()
            .resolve("src/main/java/com/examvan/app/helper/SecurityEnforcer.kt")
            .readText()

    private fun examViewerActivity(): String =
        resolveModuleDir()
            .resolve("src/main/java/com/examvan/app/ExamViewerActivity.kt")
            .readText()

    @Test
    fun securityViolationScreen_hasExitButtonWiredToCallback() {
        val src = securityEnforcer()
        assertTrue(
            "SecurityEnforcer harus punya callback onSecurityViolationExit",
            src.contains("var onSecurityViolationExit")
        )
        val showViolation = src.substringAfter("fun showSecurityViolation(")
            .substringBefore("fun checkSecurityViolations()")
        assertTrue(
            "Layar security violation harus menampilkan tombol Keluar (btnOpenResult)",
            showViolation.contains("binding.btnOpenResult.visibility = View.VISIBLE")
        )
        assertTrue(
            "Tombol Keluar harus memanggil callback atau finish sebagai fallback",
            showViolation.contains("onSecurityViolationExit?.invoke() ?: activity.finish()")
        )
    }

    @Test
    fun backOnSecurityViolation_exitsInsteadOfBeingBlockedByStrict() {
        val src = securityEnforcer()
        val handleBack = src.substringAfter("fun handleBackPressed()")
            .substringBefore("fun handleUserLeave()")
        assertTrue(
            "handleBackPressed harus menangani isSecurityViolationShowing sebelum blokir strict",
            handleBack.contains("if (isSecurityViolationShowing)") &&
                handleBack.contains("onSecurityViolationExit?.invoke() ?: activity.finish()")
        )
    }

    @Test
    fun activity_exitLogsOutBeforeExamAndAutoSubmitsAfterPdfReady() {
        val src = examViewerActivity()
        val wiring = src.substringAfter("onSecurityViolationExit = {")
            .substringBefore("}\n        }")
        assertTrue(
            "Security violation harus memakai jalur exit blokir yang sama",
            wiring.contains("exitBlockingScreen()")
        )

        val helper = src.substringAfter("private fun exitBlockingScreen()")
            .substringBefore("private fun showQuestionsUnavailableDialog()")
        assertTrue(
            "Pra-ujian: exit harus logout presence",
            helper.contains("sendAccessLog(\"logout\", retryOnFailure = true)")
        )
        assertTrue(
            "Pra-ujian: exit harus menutup activity",
            helper.contains("finish()")
        )
        assertTrue(
            "Saat PDF siap: exit harus auto-submit, bukan keluar tanpa mengumpulkan",
            helper.contains("viewModel.isPdfReady.value") &&
                helper.contains("submissionManager.autoSubmitAndExit()")
        )
    }

    @Test
    fun retry_clearsStaleExitButton() {
        val src = securityEnforcer()
        val showViolation = src.substringAfter("fun showSecurityViolation(")
            .substringBefore("fun checkSecurityViolations()")
        assertTrue(
            "Periksa Ulang harus menyembunyikan tombol Keluar yang lama",
            showViolation.contains("binding.btnOpenResult.visibility = View.GONE")
        )
        assertTrue(
            "Setelah retry berhasil, listener tombol Keluar harus dilepas",
            showViolation.contains("binding.btnOpenResult.setOnClickListener(null)")
        )
    }
}
