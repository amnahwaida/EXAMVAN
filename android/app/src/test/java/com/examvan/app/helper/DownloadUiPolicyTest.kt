package com.examvan.app.helper

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Mengunci kontrak tombol batal unduhan (fix review UI/UX #3: "logika lama
 * `if (percent < 50) visible` terbaca terbalik dan tidak pernah
 * menyembunyikan lagi").
 *
 * Kontrak baru berbasis WAKTU, bukan persen: tombol disembunyikan selama
 * grace period 3 detik pertama (mencegah batal gegabah), lalu tampil untuk
 * sisa durasi unduhan.
 *
 *   ./gradlew :app:testStudentDebugUnitTest
 */
class DownloadUiPolicyTest {

    @Test
    fun withinGracePeriod_hidden() {
        assertFalse(DownloadUiPolicy.cancelVisible(elapsedMs = 0L))
        assertFalse(DownloadUiPolicy.cancelVisible(elapsedMs = 1_500L))
        assertFalse(DownloadUiPolicy.cancelVisible(elapsedMs = 2_999L))
    }

    @Test
    fun afterGracePeriod_visible() {
        assertTrue(DownloadUiPolicy.cancelVisible(elapsedMs = 3_000L))
        assertTrue(DownloadUiPolicy.cancelVisible(elapsedMs = 30_000L))
    }

    @Test
    fun graceBoundary_exact_threeSeconds_visible() {
        assertTrue(DownloadUiPolicy.cancelVisible(elapsedMs = DownloadUiPolicy.CANCEL_GRACE_MS))
    }

    @Test
    fun percentIsIrrelevant_timeIsTheOnlySignal() {
        // Persen 99 pada detik pertama tetap tersembunyi; persen 1 setelah
        // grace tetap tampil — kebijakan murni waktu.
        assertFalse(DownloadUiPolicy.cancelVisible(elapsedMs = 100L))
        assertTrue(DownloadUiPolicy.cancelVisible(elapsedMs = 10_000L))
    }
}
