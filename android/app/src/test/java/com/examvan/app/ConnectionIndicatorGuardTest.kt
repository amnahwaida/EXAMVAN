package com.examvan.app

import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

/**
 * Guard kontrak indikator koneksi (fix review fitur indikator ronde 2 #1):
 * SETIAP event jaringan wajib membaca ulang state via
 * refreshConnectionIndicator() — keputusan online/offline harus selalu dari
 * jaringan aktif saat ini, tidak boleh dari semantik per-event.
 *
 * Saat ini ada 5 titik panggil yang sah:
 *   onCapabilitiesChanged, onLost, onUnavailable,
 *   onAvailable, dan onCreate (state awal).
 *
 *   ./gradlew :app:testStudentDebugUnitTest
 */
class ConnectionIndicatorGuardTest {

    private fun sourceFile(): File {
        val cwd = File(System.getProperty("user.dir") ?: ".")
        val module = when {
            File(cwd, "src/main/java").isDirectory -> cwd
            File(cwd, "app/src/main/java").isDirectory -> cwd.resolve("app")
            else -> error("Direktori sumber Android tidak ditemukan")
        }
        return module.resolve(
            "src/main/java/com/examvan/app/ExamViewerActivity.kt"
        )
    }

    @Test
    fun everyNetworkPath_refreshesIndicator() {
        val src = sourceFile().readText()
        // Regex menghitung DEFINISI fungsi juga — sehingga kontrak:
        // 5 titik panggilan + 1 definisi = minimal 6 kemunculan.
        val callCount = Regex("refreshConnectionIndicator\\(\\)").findAll(src).count()

        assertTrue(
            "refreshConnectionIndicator() harus dipanggil dari minimal 5 titik " +
                "(onAvailable, onCapabilitiesChanged, onLost, onUnavailable, onCreate); " +
                "kemunculan ditemukan $callCount (termasuk definisi)",
            callCount >= 6
        )
    }

    @Test
    fun decisionGoesThroughPolicy_notRawEvents() {
        // Keputusan online tidak boleh dihitung langsung dari argumen event;
        // harus lewat isOnlineFromActiveNetwork (state jaringan aktif).
        val src = sourceFile().readText()
        assertTrue(
            src.contains("DeviceStatusPolicy.isOnlineFromActiveNetwork")
        )
    }
}
