package com.examvan.app.helper

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Mengunci kontrak bitmask fitur lock task untuk tier Device-Owner/DPM
 * (fix temuan review strict ronde 5 #1: "setLockTaskFeatures API 30+ tidak
 * dimanfaatkan — notification shade & recents tetap bisa diakses siswa
 * saat ter-pin di perangkat kiosk").
 *
 * Kontrak (bit = nilai resmi DevicePolicyManager, API 30):
 *  - DINONAKTIFKAN selama pinned: NOTIFICATIONS (shade/quick settings —
 *    jalur bypass terdokumentasi), RECENT_TASKS, HOME.
 *  - DIPERTAHANKAN: GLOBAL_ACTIONS (power menu darurat), SYSTEM_INFO
 *    (panel volume), KEYGUARD (layar kunci normal).
 *
 *   ./gradlew :app:testStudentDebugUnitTest
 */
class LockTaskFeaturePolicyTest {

    @Test
    fun bitValues_matchDevicePolicyManagerApi30() {
        // Mirror nilai resmi AOSP — perubahan angka harus disadari.
        assertEquals(1, LockTaskFeaturePolicy.FEATURE_HOME)
        assertEquals(2, LockTaskFeaturePolicy.FEATURE_RECENT_TASKS)
        assertEquals(4, LockTaskFeaturePolicy.FEATURE_GLOBAL_ACTIONS)
        assertEquals(8, LockTaskFeaturePolicy.FEATURE_NOTIFICATIONS)
        assertEquals(16, LockTaskFeaturePolicy.FEATURE_SYSTEM_INFO)
        assertEquals(32, LockTaskFeaturePolicy.FEATURE_KEYGUARD)
    }

    @Test
    fun features_areDistinctPowersOfTwo() {
        val all = listOf(
            LockTaskFeaturePolicy.FEATURE_HOME,
            LockTaskFeaturePolicy.FEATURE_RECENT_TASKS,
            LockTaskFeaturePolicy.FEATURE_GLOBAL_ACTIONS,
            LockTaskFeaturePolicy.FEATURE_NOTIFICATIONS,
            LockTaskFeaturePolicy.FEATURE_SYSTEM_INFO,
            LockTaskFeaturePolicy.FEATURE_KEYGUARD
        )
        assertEquals(all.size, all.toSet().size)
        all.forEach { assertTrue("bit bukan power of two: $it", it and (it - 1) == 0 && it != 0) }
    }

    @Test
    fun allowedFeatures_notificationsDisabled() {
        assertEquals(
            0,
            LockTaskFeaturePolicy.allowedFeatures() and LockTaskFeaturePolicy.FEATURE_NOTIFICATIONS
        )
    }

    @Test
    fun allowedFeatures_recentTasksDisabled() {
        assertEquals(
            0,
            LockTaskFeaturePolicy.allowedFeatures() and LockTaskFeaturePolicy.FEATURE_RECENT_TASKS
        )
    }

    @Test
    fun allowedFeatures_homeDisabled() {
        assertEquals(
            0,
            LockTaskFeaturePolicy.allowedFeatures() and LockTaskFeaturePolicy.FEATURE_HOME
        )
    }

    @Test
    fun allowedFeatures_globalActionsEnabled_powerMenuDarurat() {
        assertNotEquals(
            0,
            LockTaskFeaturePolicy.allowedFeatures() and LockTaskFeaturePolicy.FEATURE_GLOBAL_ACTIONS
        )
    }

    @Test
    fun allowedFeatures_systemInfoEnabled_panelVolume() {
        assertNotEquals(
            0,
            LockTaskFeaturePolicy.allowedFeatures() and LockTaskFeaturePolicy.FEATURE_SYSTEM_INFO
        )
    }

    @Test
    fun allowedFeatures_keyguardEnabled_layarKunciNormal() {
        assertNotEquals(
            0,
            LockTaskFeaturePolicy.allowedFeatures() and LockTaskFeaturePolicy.FEATURE_KEYGUARD
        )
    }
}
