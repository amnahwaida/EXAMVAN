package com.examvan.app.helper

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Mengunci kontrak resolusi baseline clock-drift (fix temuan review:
 * "baseline drift hanya ada di memory/savedInstanceState; app restart =
 * siswa bisa manipulasi waktu lagi").
 *
 * Prioritas sumber baseline:
 * 1. savedInstanceState  — rotasi / re-create activity di proses yang sama.
 * 2. Prefs terenkripsi   — proses mati lalu dibuka lagi (process death).
 * 3. Hitung baru         — sesi ujian pertama kali; HARUS ditandai baru agar
 *                          caller menyimpannya ke prefs untuk sesi berikut.
 *
 *   ./gradlew :app:testStudentDebugUnitTest
 */
class ClockDriftPolicyTest {

    @Test
    fun resolve_savedInstanceStateWins_overPersistedAndFresh() {
        val resolved = ClockDriftPolicy.resolveBaseline(
            savedStateDriftMs = 111L,
            persistedDriftMs = 222L,
            freshDriftMs = 333L
        )
        assertEquals(111L, resolved.baselineDriftMs)
        assertFalse(resolved.isNew)
    }

    @Test
    fun resolve_processDeath_usesPersistedBaseline() {
        // savedInstanceState == null setelah proses mati → pakai nilai dari
        // prefs terenkripsi. Inilah kasus yang DULU bocor: siswa matikan
        // proses app, baseline hilang, lalu bebas memanipulasi jam.
        val resolved = ClockDriftPolicy.resolveBaseline(
            savedStateDriftMs = null,
            persistedDriftMs = 222L,
            freshDriftMs = 333L
        )
        assertEquals(222L, resolved.baselineDriftMs)
        assertFalse(resolved.isNew)
    }

    @Test
    fun resolve_firstSession_computesFreshAndFlagsNew() {
        val resolved = ClockDriftPolicy.resolveBaseline(
            savedStateDriftMs = null,
            persistedDriftMs = null,
            freshDriftMs = 333L
        )
        assertEquals(333L, resolved.baselineDriftMs)
        assertTrue(resolved.isNew) // caller wajib persist ke prefs
    }

    @Test
    fun resolve_zeroSavedStateValue_isIgnoredAsSentinel() {
        // Bundle.getLong default-nya 0L; 0 bukan baseline sah (hampir mustahil
        // wall-clock == elapsed realtime persis). Nilai 0 dari savedInstanceState
        // diperlakukan sebagai "tidak ada" → jatuh ke sumber berikutnya.
        val resolvedFromPersisted = ClockDriftPolicy.resolveBaseline(
            savedStateDriftMs = 0L,
            persistedDriftMs = 222L,
            freshDriftMs = 333L
        )
        assertEquals(222L, resolvedFromPersisted.baselineDriftMs)

        val resolvedFresh = ClockDriftPolicy.resolveBaseline(
            savedStateDriftMs = 0L,
            persistedDriftMs = null,
            freshDriftMs = 333L
        )
        assertTrue(resolvedFresh.isNew)
    }

    @Test
    fun resolve_persistedZeroSentinel_isIgnored() {
        // Prefs juga bisa menyimpan sentinel 0 (belum pernah disimpan).
        val resolved = ClockDriftPolicy.resolveBaseline(
            savedStateDriftMs = null,
            persistedDriftMs = 0L,
            freshDriftMs = 333L
        )
        assertEquals(333L, resolved.baselineDriftMs)
        assertTrue(resolved.isNew)
    }

    @Test
    fun resolve_negativeBaselines_areValidValues() {
        // Baseline negatif sah (jam perangkat di belakang elapsedRealtime
        // setelah reboot dengan jam belum tersinkron NTP) — tidak boleh
        // diperlakukan sebagai sentinel.
        val resolved = ClockDriftPolicy.resolveBaseline(
            savedStateDriftMs = -50_000L,
            persistedDriftMs = 222L,
            freshDriftMs = 333L
        )
        assertEquals(-50_000L, resolved.baselineDriftMs)
    }
}
