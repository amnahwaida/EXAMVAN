package com.examvan.app

import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

/**
 * Guard penjaga proses auto-submit:
 *
 * 1. Service foreground + izin FGS dataSync wajib ada di manifest utama.
 * 2. SubmissionManager wajib start service sebelum activity finish dan
 *    stop di finally — bila salah satu dihapus, submit background kembali
 *    rentan dibunuh OEM/low-memory.
 *
 *   ./gradlew :app:testStudentDebugUnitTest
 */
class SubmissionKeepAliveGuardTest {

    private fun resolveModuleDir(): File {
        val cwd = File(System.getProperty("user.dir") ?: ".")
        return when {
            File(cwd, "src/main/res").isDirectory -> cwd
            File(cwd, "app/src/main/res").isDirectory -> cwd.resolve("app")
            else -> error("Direktori sumber Android tidak ditemukan (cwd=${cwd.absolutePath})")
        }
    }

    @Test
    fun manifest_declaresForegroundServiceAndPermissions() {
        val manifest = resolveModuleDir().resolve("src/main/AndroidManifest.xml")
        assertTrue("Manifest utama tidak ditemukan", manifest.exists())
        val text = manifest.readText()

        assertTrue(
            "Izin FOREGROUND_SERVICE wajib dideklarasikan",
            text.contains("android.permission.FOREGROUND_SERVICE")
        )
        assertTrue(
            "Izin FOREGROUND_SERVICE_DATA_SYNC wajib dideklarasikan",
            text.contains("android.permission.FOREGROUND_SERVICE_DATA_SYNC")
        )
        assertTrue(
            "Service SubmissionKeepAliveService wajib dideklarasikan",
            text.contains("SubmissionKeepAliveService")
        )
        assertTrue(
            "Service wajib memakai foregroundServiceType dataSync",
            text.contains("android:foregroundServiceType=\"dataSync\"")
        )
    }

    @Test
    fun submissionManager_wrapsAutoSubmitWithKeepAlive() {
        val source = resolveModuleDir().resolve(
            "src/main/java/com/examvan/app/helper/SubmissionManager.kt"
        )
        assertTrue("SubmissionManager.kt tidak ditemukan", source.exists())
        val text = source.readText()

        assertTrue(
            "autoSubmitAndExit wajib memanggil SubmissionKeepAliveService.start(context)",
            text.contains("SubmissionKeepAliveService.start(context)")
        )
        assertTrue(
            "Submit background wajib stop service di finally",
            text.contains("SubmissionKeepAliveService.stop(context)")
        )
    }
}
