package com.examvan.app

import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

/**
 * Guard: layar "Navigasi Gesture Terdeteksi" tidak boleh menjadi jalan buntu.
 *
 * Layar ini tampil SEBELUM lock task aktif dan sebelum soal dimuat. Dulu
 * back diblokir strict, navigation bar disembunyikan immersive, dan satu-
 * satunya tombol adalah "Periksa Lagi" — siswa di Vivo Y21 tidak bisa keluar
 * untuk mengubah navigasi ke 3 tombol dan harus restart perangkat.
 *
 * Kontrak:
 *  - ada tombol "Keluar" + callback yang menutup activity,
 *  - back pada layar ini keluar (bukan diblokir strict),
 *  - immersive tidak menyembunyikan navigation bar selagi layar ini tampil.
 *
 *   ./gradlew :app:testStudentDebugUnitTest
 */
class GestureBlockedEscapeGuardTest {

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
    fun gestureBlockedScreen_hasExitButtonWiredToCallback() {
        val src = securityEnforcer()
        assertTrue(
            "SecurityEnforcer harus punya callback onGestureBlockedExit",
            src.contains("var onGestureBlockedExit")
        )
        assertTrue(
            "Layar gesture-blocked harus menampilkan tombol Keluar (btnOpenResult)",
            src.contains("binding.btnOpenResult.visibility = View.VISIBLE")
        )
        assertTrue(
            "Tombol Keluar harus memanggil callback atau finish sebagai fallback",
            src.contains("onGestureBlockedExit?.invoke() ?: activity.finish()")
        )
    }

    @Test
    fun backOnGestureBlocked_exitsInsteadOfBeingBlockedByStrict() {
        val src = securityEnforcer()
        val handleBack = src.substringAfter("fun handleBackPressed()")
            .substringBefore("fun handleUserLeave()")
        assertTrue(
            "handleBackPressed harus menangani isGestureBlockedShowing sebelum blokir strict",
            handleBack.contains("if (isGestureBlockedShowing)") &&
                handleBack.contains("onGestureBlockedExit?.invoke() ?: activity.finish()")
        )
    }

    @Test
    fun gestureBlockedScreen_doesNotEnterImmersiveMode() {
        val src = securityEnforcer()
        val immersive = src.substringAfter("fun enterImmersiveMode()")
            .substringBefore("fun handleBackPressed()")
        assertTrue(
            "enterImmersiveMode harus no-op saat layar gesture-blocked tampil",
            immersive.contains("if (isGestureBlockedShowing) return")
        )
    }

    @Test
    fun activity_exitLogsOutWithoutMarkingSubmitted() {
        val src = examViewerActivity()
        val wiring = src.substringAfter("onGestureBlockedExit = {")
            .substringBefore("}\n        }")
        assertTrue(
            "Exit gesture-blocked harus logout presence",
            wiring.contains("sendAccessLog(\"logout\", retryOnFailure = true)")
        )
        assertTrue(
            "Exit gesture-blocked harus menutup activity",
            wiring.contains("finish()")
        )
        assertTrue(
            "Exit gesture-blocked TIDAK boleh menandai submitted (agar bisa masuk lagi)",
            !wiring.contains("persistSubmittedState") &&
                !wiring.contains("setSubmittedOrExited(true)")
        )
    }

    @Test
    fun gestureBlockedScreen_canOpenNavigationSettingsBeforeExam() {
        val src = securityEnforcer()
        val showGesture = src.substringAfter("fun showGestureBlocked(")
            .substringBefore("fun showGestureWarningOnce()")
        assertTrue(
            "Tombol utama layar gesture-blocked harus membuka pengaturan navigasi",
            showGesture.contains("binding.btnRetryDownload.text = \"Buka Pengaturan\"") &&
                showGesture.contains("NavigationSettingsHelper.open(activity)")
        )
        assertTrue(
            "Layar gesture-blocked harus menampilkan path manual per merek",
            showGesture.contains("NavigationSettingsHelper.instructionText()")
        )
    }

    @Test
    fun postStartGestureWarning_doesNotOfferSettingsEscape() {
        val src = securityEnforcer()
        val warning = src.substringAfter("fun showGestureWarningOnce()")
            .substringBefore("fun verifyLockTask()")
        assertTrue(
            "Snackbar pasca-ujian berjalan TIDAK boleh menawarkan jalur keluar Settings",
            !warning.contains("safeStartSettings(") &&
                !warning.contains("startActivity(") &&
                !warning.contains("setAction(\"Buka Settings\"")
        )
    }

    @Test
    fun gestureCleared_hidesExitButtonBeforeStartingExam() {
        val src = securityEnforcer()
        val showGesture = src.substringAfter("fun showGestureBlocked(")
            .substringBefore("fun showGestureWarningOnce()")
        assertTrue(
            "Saat gesture sudah nonaktif, tombol Keluar harus disembunyikan",
            showGesture.contains("binding.btnOpenResult.visibility = View.GONE")
        )
        assertTrue(
            "Saat gesture sudah nonaktif, isGestureBlockedShowing harus direset",
            showGesture.contains("isGestureBlockedShowing = false")
        )
    }
}
