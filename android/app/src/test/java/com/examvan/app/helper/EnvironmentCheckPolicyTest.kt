package com.examvan.app.helper

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Mengunci heuristik deteksi lingkungan berbahaya yang diekstrak dari
 * SecurityEnforcer (fix temuan review: "root detection menjalankan
 * Runtime.exec + waitFor di UI thread tiap onResume").
 *
 * Logika keputusan dipisah dari I/O (File.exists, process exec) sehingga:
 * 1. Murni fungsi → dites di JVM tanpa perangkat.
 * 2. Bisa dijalankan di Dispatchers.IO tanpa menyentuh Activity.
 *
 *   ./gradlew :app:testStudentDebugUnitTest
 */
class EnvironmentCheckPolicyTest {

    // Perangkat fisik wajar (Pixel asli) sebagai baseline "tidak mencurigakan".
    private val physicalDevice = mapOf(
        "fingerprint" to "google/panther/panther:14/AP2A.240905.003/2024:user/release-keys",
        "model" to "Pixel 7",
        "manufacturer" to "Google",
        "hardware" to "zuma",
        "product" to "panther",
        "board" to "zuma",
        "brand" to "google",
        "device" to "panther"
    )

    private fun props(vararg overrides: Pair<String, String>): Map<String, String> {
        return physicalDevice.toMutableMap().apply { overrides.forEach { (k, v) -> put(k, v) } }
    }

    // ── Deteksi emulator ────────────────────────────────────────────────

    @Test
    fun isEmulator_physicalDevice_notDetected() {
        assertFalse(EnvironmentCheckPolicy.isEmulator(props()))
    }

    @Test
    fun isEmulator_genericFingerprint_detected() {
        assertTrue(EnvironmentCheckPolicy.isEmulator(props("fingerprint" to "generic/sdk_gphone16/generic:15/test-keys")))
    }

    @Test
    fun isEmulator_goldfishHardware_detected() {
        assertTrue(EnvironmentCheckPolicy.isEmulator(props("hardware" to "goldfish")))
        assertTrue(EnvironmentCheckPolicy.isEmulator(props("hardware" to "ranchu")))
    }

    @Test
    fun isEmulator_sdkGphoneProduct_detected() {
        assertTrue(EnvironmentCheckPolicy.isEmulator(props("product" to "sdk_gphone16_x86_64")))
    }

    @Test
    fun isEmulator_genymotion_detected() {
        assertTrue(EnvironmentCheckPolicy.isEmulator(props("manufacturer" to "Genymotion")))
    }

    @Test
    fun isEmulator_noxPlayer_detected() {
        // Board ATAU manufacturer mengandung "nox".
        assertTrue(EnvironmentCheckPolicy.isEmulator(props("board" to "nox")))
        assertTrue(EnvironmentCheckPolicy.isEmulator(props("manufacturer" to "bignox")))
    }

    @Test
    fun isEmulator_googleSdkModel_detected() {
        assertTrue(EnvironmentCheckPolicy.isEmulator(props("model" to "google_sdk")))
        assertTrue(EnvironmentCheckPolicy.isEmulator(props("model" to "Emulator")))
    }

    // ── Deteksi root dari marker path ───────────────────────────────────

    @Test
    fun isRootedByMarkers_cleanDevice_notRooted() {
        assertFalse(EnvironmentCheckPolicy.isRootedByMarkers(existingPaths = emptySet(), buildTags = "release-keys"))
    }

    @Test
    fun isRootedByMarkers_classicSuBinary_rooted() {
        val rooted = EnvironmentCheckPolicy.isRootedByMarkers(
            existingPaths = setOf("/system/bin/su"),
            buildTags = "release-keys"
        )
        assertTrue(rooted)
    }

    @Test
    fun isRootedByMarkers_modernMagiskMarker_rooted() {
        // Magisk v20+ menyembunyikan su; marker mount /data/adb/magisk tetep
        // ketahuan — ini yang membuat deteksi tahan DenyList klasik.
        val rooted = EnvironmentCheckPolicy.isRootedByMarkers(
            existingPaths = setOf("/data/adb/magisk"),
            buildTags = "release-keys"
        )
        assertTrue(rooted)
    }

    @Test
    fun isRootedByMarkers_kernelsuAndApatchMarkers_rooted() {
        assertTrue(EnvironmentCheckPolicy.isRootedByMarkers(setOf("/data/adb/ksu"), "release-keys"))
        assertTrue(EnvironmentCheckPolicy.isRootedByMarkers(setOf("/data/adb/apd"), "release-keys"))
    }

    @Test
    fun isRootedByMarkers_testKeysBuild_rooted() {
        val rooted = EnvironmentCheckPolicy.isRootedByMarkers(
            existingPaths = emptySet(),
            buildTags = "test-keys"
        )
        assertTrue(rooted)
    }

    @Test
    fun isRootedByMarkers_nullBuildTags_treatedAsClean() {
        assertFalse(EnvironmentCheckPolicy.isRootedByMarkers(emptySet(), null))
    }

    // Semua marker path yang dikenali harus terdaftar di konstanta publik
    // (SecurityEnforcer memakai list ini untuk scan File.exists).
    @Test
    fun rootMarkerPaths_coversClassicAndModernSolutions() {
        val expected = setOf(
            "/system/app/Superuser.apk", "/sbin/su", "/system/bin/su",
            "/system/xbin/su", "/data/local/xbin/su", "/data/local/bin/su",
            "/system/sd/xbin/su", "/system/bin/failsafe/su", "/data/local/su",
            "/data/adb/magisk", "/data/adb/ksu", "/data/adb/apd", "/sbin/.magisk"
        )
        assertEquals(expected, EnvironmentCheckPolicy.ROOT_MARKER_PATHS.toSet())
    }

    // ── Deteksi manipulasi jam (clock drift) ────────────────────────────

    @Test
    fun clockTamper_withinThreshold_notTampered() {
        // Drift bergeser 5 detik dari baseline → masih dalam toleransi 10s.
        val result = EnvironmentCheckPolicy.evaluateClockDrift(
            baselineDriftMs = 1_000L,
            currentDriftMs = 6_000L,
            thresholdMs = 10_000L
        )
        assertFalse(result.tampered)
        assertEquals(5_000L, result.driftDeltaMs)
    }

    @Test
    fun clockTamper_beyondThreshold_tampered() {
        // Jam sistem dimundurkan/maju 30 detik dari baseline → tampered.
        val result = EnvironmentCheckPolicy.evaluateClockDrift(
            baselineDriftMs = 1_000L,
            currentDriftMs = 31_000L,
            thresholdMs = 10_000L
        )
        assertTrue(result.tampered)
        assertEquals(30_000L, result.driftDeltaMs)
    }

    @Test
    fun clockTamper_exactAtThreshold_boundaryIsSafe() {
        // Tepat di ambang 10 detik → belum dilaporkan (>= threshold baru ya).
        val result = EnvironmentCheckPolicy.evaluateClockDrift(
            baselineDriftMs = 0L,
            currentDriftMs = 10_000L,
            thresholdMs = 10_000L
        )
        assertFalse(result.tampered)
    }

    @Test
    fun clockTamper_negativeShift_alsoDetected() {
        val result = EnvironmentCheckPolicy.evaluateClockDrift(
            baselineDriftMs = 5_000L,
            currentDriftMs = -20_000L,
            thresholdMs = 10_000L
        )
        assertTrue(result.tampered)
    }
}
