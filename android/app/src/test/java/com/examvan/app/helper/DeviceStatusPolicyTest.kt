package com.examvan.app.helper

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Mengunci kontrak indikator status perangkat selama ujian (fitur baru:
 * indikator baterai + koneksi internet di ExamViewerActivity).
 *
 * Baterai — kategori berdasarkan persentase:
 *  - CRITICAL : < 15%  (merah — segera hubungkan charger)
 *  - LOW      : 15–29% (kuning — peringatan)
 *  - NORMAL   : >= 30% (netral/aman)
 * Nilai di luar 0..100 dicukup (clamp) — sumber sistem bisa aneh.
 *
 * Koneksi — online hanya bila network memiliki capability INTERNET dan
 * SUDAH Tervalidasi (VALIDATED): captive portal / login hotspot tidak
 * boleh dianggap online.
 *
 *   ./gradlew :app:testStudentDebugUnitTest
 */
class DeviceStatusPolicyTest {

    // ── Kategori baterai ─────────────────────────────────────────────────

    @Test
    fun battery_critical_below15() {
        assertEquals(
            DeviceStatusPolicy.BatteryLevel.CRITICAL,
            DeviceStatusPolicy.batteryCategory(14)
        )
        assertEquals(
            DeviceStatusPolicy.BatteryLevel.CRITICAL,
            DeviceStatusPolicy.batteryCategory(0)
        )
    }

    @Test
    fun battery_low_15to29() {
        assertEquals(
            DeviceStatusPolicy.BatteryLevel.LOW,
            DeviceStatusPolicy.batteryCategory(15)
        )
        assertEquals(
            DeviceStatusPolicy.BatteryLevel.LOW,
            DeviceStatusPolicy.batteryCategory(29)
        )
    }

    @Test
    fun battery_normal_30andAbove() {
        assertEquals(
            DeviceStatusPolicy.BatteryLevel.NORMAL,
            DeviceStatusPolicy.batteryCategory(30)
        )
        assertEquals(
            DeviceStatusPolicy.BatteryLevel.NORMAL,
            DeviceStatusPolicy.batteryCategory(100)
        )
    }

    @Test
    fun battery_outOfRangeValues_clamped() {
        // Sumber ACTION_BATTERY_CHANGED bisa memberi nilai aneh.
        assertEquals(
            DeviceStatusPolicy.BatteryLevel.CRITICAL,
            DeviceStatusPolicy.batteryCategory(-5)
        )
        assertEquals(
            DeviceStatusPolicy.BatteryLevel.NORMAL,
            DeviceStatusPolicy.batteryCategory(150)
        )
    }

    @Test
    fun battery_boundaries_exact() {
        assertEquals(DeviceStatusPolicy.BatteryLevel.CRITICAL, DeviceStatusPolicy.batteryCategory(-1))
        assertEquals(DeviceStatusPolicy.BatteryLevel.LOW, DeviceStatusPolicy.batteryCategory(14 + 1))
        assertEquals(DeviceStatusPolicy.BatteryLevel.NORMAL, DeviceStatusPolicy.batteryCategory(29 + 1))
    }

    // ── Keputusan online ─────────────────────────────────────────────────

    @Test
    fun online_internetPlusValidated() {
        assertTrue(DeviceStatusPolicy.isOnline(hasInternet = true, validated = true))
    }

    @Test
    fun offline_captivePortal_notValidated() {
        assertFalse(DeviceStatusPolicy.isOnline(hasInternet = true, validated = false))
    }

    @Test
    fun offline_noInternetCapability() {
        assertFalse(DeviceStatusPolicy.isOnline(hasInternet = false, validated = true))
        assertFalse(DeviceStatusPolicy.isOnline(hasInternet = false, validated = false))
    }

    // ── Keputusan dari JARINGAN AKTIF (fix review fitur #1) ──────────────

    /**
     * Perangkat bisa punya WiFi + seluler aktif bersamaan. Dulu onLost satu
     * network langsung menandai "Offline" padahal network lain masih
     * menghubungkan. Kontrak baru: keputusan diambil dari capabilities
     * jaringan AKTIF saat ini; tanpa jaringan aktif (null) = offline.
     */
    @Test
    fun fromActiveNetwork_online_whenInternetAndValidated() {
        assertTrue(
            DeviceStatusPolicy.isOnlineFromActiveNetwork(
                activeHasInternet = true, activeValidated = true
            )
        )
    }

    @Test
    fun fromActiveNetwork_captivePortal_offline() {
        assertFalse(
            DeviceStatusPolicy.isOnlineFromActiveNetwork(
                activeHasInternet = true, activeValidated = false
            )
        )
    }

    @Test
    fun fromActiveNetwork_noInternetCapability_offline() {
        assertFalse(
            DeviceStatusPolicy.isOnlineFromActiveNetwork(
                activeHasInternet = false, activeValidated = true
            )
        )
    }

    @Test
    fun fromActiveNetwork_nullMeansNoActiveNetwork_offline() {
        // Kehilangan WiFi tanpa pengganti → activeNetwork null.
        assertFalse(
            DeviceStatusPolicy.isOnlineFromActiveNetwork(
                activeHasInternet = null, activeValidated = null
            )
        )
        assertFalse(DeviceStatusPolicy.isOnlineFromActiveNetwork(null, true))
        assertFalse(DeviceStatusPolicy.isOnlineFromActiveNetwork(true, null))
    }
}
