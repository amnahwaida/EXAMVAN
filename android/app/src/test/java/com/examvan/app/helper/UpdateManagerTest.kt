package com.examvan.app.helper

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Mengunci perilaku UpdateManager (fix temuan review: "dialog update dipanggil
 * dari callback async tanpa cek isFinishing/isDestroyed → BadTokenException
 * saat activity sudah mati").
 *
 * canPresentDialog adalah fungsi murni sehingga guard-nya bisa dites di JVM;
 * showUpdateRequiredDialog memanggilnya sebelum builder.show().
 *
 *   ./gradlew :app:testStudentDebugUnitTest
 */
class UpdateManagerTest {

    // ── Perbandingan versi (isOutdated) ─────────────────────────────────

    @Test
    fun isOutdated_olderApp_true() {
        assertTrue(UpdateManager.isOutdated("2.7.1", "2.7.2"))
    }

    @Test
    fun isOutdated_sameVersion_false() {
        assertFalse(UpdateManager.isOutdated("2.7.2", "2.7.2"))
    }

    @Test
    fun isOutdated_newerApp_false() {
        assertFalse(UpdateManager.isOutdated("2.8.0", "2.7.2"))
    }

    @Test
    fun isOutdated_comparesSemantically_notLexicographically() {
        // 2.10.0 > 2.9.0 secara semantik, walau "10" < "9" secara string.
        assertFalse(UpdateManager.isOutdated("2.10.0", "2.9.0"))
        assertTrue(UpdateManager.isOutdated("2.9.0", "2.10.0"))
    }

    @Test
    fun isOutdated_differentSegmentCounts_missingSegmentsAreZero() {
        assertTrue(UpdateManager.isOutdated("2.7", "2.7.1"))
        assertFalse(UpdateManager.isOutdated("2.8", "2.8.0"))
        assertFalse(UpdateManager.isOutdated("3", "2.9.9"))
    }

    @Test
    fun isOutdated_majorMinorPatchPriority() {
        assertTrue(UpdateManager.isOutdated("1.99.99", "2.0.0"))
        assertTrue(UpdateManager.isOutdated("2.6.99", "2.7.0"))
        assertTrue(UpdateManager.isOutdated("2.7.1", "2.7.2"))
    }

    @Test
    fun isOutdated_nonNumericSuffix_ignoredOnBothSides() {
        // Sufiks "-beta" dibuang; hanya leading digits yang dihitung.
        assertFalse(UpdateManager.isOutdated("2.7.2-beta", "2.7.2"))
        assertTrue(UpdateManager.isOutdated("2.7.1-rc1", "2.7.2"))
    }

    @Test
    fun isOutdated_garbageInput_fallsBackWithoutCrash() {
        // Input tidak parse-able tidak boleh melempar exception.
        assertFalse(UpdateManager.isOutdated("", ""))
        // Fallback string compare: "abc" < "abd" → outdated.
        assertTrue(UpdateManager.isOutdated("abc", "abd"))
    }

    // ── Guard dialog async (temuan utama) ────────────────────────────────

    @Test
    fun canPresentDialog_aliveActivity_true() {
        assertTrue(UpdateManager.canPresentDialog(isFinishing = false, isDestroyed = false))
    }

    @Test
    fun canPresentDialog_finishingActivity_false() {
        // Activity sudah finish() — AlertDialog.show() akan lempar
        // WindowManager$BadTokenException karena window token sudah invalid.
        assertFalse(UpdateManager.canPresentDialog(isFinishing = true, isDestroyed = false))
    }

    @Test
    fun canPresentDialog_destroyedActivity_false() {
        assertFalse(UpdateManager.canPresentDialog(isFinishing = false, isDestroyed = true))
    }
}
