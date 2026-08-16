package com.examvan.app.helper

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Mengunci perbaikan review mode ujian (16 Agustus 2026).
 *
 * Kunci utama: mode LOW TIDAK auto-submit saat fokus hilang — sebelumnya
 * `onWindowFocusChanged` hanya cek `!strictMode`, sehingga siswa low yang
 * membuka laci notifikasi / berpindah aplikasi ikut ter-submit (melanggar
 * kontrak low: "bebas keluar masuk tanpa konsekuensi").
 *
 *   ./gradlew :app:testStudentDebugUnitTest
 */
class ExamModePolicyTest {

    // ── Auto-submit saat fokus hilang ────────────────────────────────────

    @Test
    fun focusLoss_lowMode_doesNotAutoSubmit() {
        assertFalse(
            "LOW harus bebas keluar-masuk — TIDAK boleh auto-submit saat fokus hilang",
            ExamModePolicy.shouldAutoSubmitOnFocusLoss(
                securityLevel = ExamModePolicy.LEVEL_LOW, strictMode = false
            )
        )
    }

    @Test
    fun focusLoss_mediumMode_autoSubmits() {
        assertTrue(
            "MEDIUM harus auto-submit saat fokus hilang",
            ExamModePolicy.shouldAutoSubmitOnFocusLoss(
                securityLevel = ExamModePolicy.LEVEL_MEDIUM, strictMode = false
            )
        )
    }

    @Test
    fun focusLoss_strictMode_neverAutoSubmits() {
        assertFalse(
            "STRICT tidak boleh auto-submit saat fokus hilang (lock task pin lah keamanannya)",
            ExamModePolicy.shouldAutoSubmitOnFocusLoss(
                securityLevel = ExamModePolicy.LEVEL_LOW, strictMode = true
            )
        )
        assertFalse(
            ExamModePolicy.shouldAutoSubmitOnFocusLoss(
                securityLevel = ExamModePolicy.LEVEL_MEDIUM, strictMode = true
            )
        )
    }

    @Test
    fun focusLoss_unknownLevel_defaultsToMediumBehavior() {
        // Level tidak dikenal (kosong/rusak) → fail-closed ke perilaku medium.
        assertTrue(
            ExamModePolicy.shouldAutoSubmitOnFocusLoss(securityLevel = "", strictMode = false)
        )
    }

    // ── Intercept tombol volume ──────────────────────────────────────────

    @Test
    fun volumeKey_lowMode_usesSystemDefault() {
        assertFalse(
            "LOW: tombol volume dibiarkan sistem (panel volume normal)",
            ExamModePolicy.shouldInterceptVolumeKeys(ExamModePolicy.LEVEL_LOW)
        )
    }

    @Test
    fun volumeKey_mediumAndStrict_intercepted() {
        assertTrue(
            ExamModePolicy.shouldInterceptVolumeKeys(ExamModePolicy.LEVEL_MEDIUM)
        )
        assertTrue(
            ExamModePolicy.shouldInterceptVolumeKeys("strict")
        )
    }
}
