package com.examvan.app

import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

/**
 * Guard false positive clock-drift di perangkat seperti Oppo A18:
 *  - baseline sesi lama tidak boleh diwarisi ujian/attempt berikutnya;
 *  - baseline lintas process death hanya sah pada boot yang sama;
 *  - perangkat yang baru reboot tidak boleh langsung dituduh memanipulasi jam.
 *
 *   ./gradlew :app:testStudentDebugUnitTest
 */
class ClockDriftFalsePositiveGuardTest {

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

    @Test
    fun newExamSession_clearsStaleClockBaseline() {
        val reset = read("src/main/java/com/examvan/app/AppPrefs.kt")
            .substringAfter("fun clearStaleExamSession(")
            .substringBefore("fun prewarm(")
        assertTrue(
            "Sesi ujian baru harus membuang baseline drift lama",
            reset.contains(".remove(KEY_CLOCK_DRIFT_BASELINE)") &&
                reset.contains(".remove(KEY_CLOCK_DRIFT_BOOT_COUNT)")
        )
    }

    @Test
    fun finishedAttempt_clearsClockBaselineForRepeat() {
        val clear = read("src/main/java/com/examvan/app/helper/SubmissionManager.kt")
            .substringAfter("fun clearSavedAnswers()")
            .substringBefore("companion object")
        assertTrue(
            "Attempt/repeat berikutnya tidak boleh mewarisi baseline drift lama",
            clear.contains(".remove(AppPrefs.KEY_CLOCK_DRIFT_BASELINE)") &&
                clear.contains(".remove(AppPrefs.KEY_CLOCK_DRIFT_BOOT_COUNT)")
        )
    }

    @Test
    fun examViewer_passesBootCountIntoBaselineResolution() {
        val src = read("src/main/java/com/examvan/app/ExamViewerActivity.kt")
        assertTrue(
            "Activity harus membaca Settings.Global.BOOT_COUNT",
            src.contains("Settings.Global.BOOT_COUNT")
        )
        assertTrue(
            "Boot count harus diteruskan ke ClockDriftPolicy",
            src.contains("persistedBootCount = persistedBoot") &&
                src.contains("currentBootCount = currentBoot")
        )
    }
}
