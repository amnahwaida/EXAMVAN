package com.examvan.app.helper

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Mengunci kontrak gerbang polling lifecycle (fix temuan review mode medium
 * ronde 5 #3: "polling approval lanjut saat activity background — boros
 * baterai").
 *
 * Kontrak:
 *  - Awal: paused (sebelum activity pertama kali resume).
 *  - resume() → run() berjalan; pause() → run() berhenti dan rantai
 *    penjadwalan berikutnya dibatalkan.
 *  - Cek terminal (isWaiting=false: approved/rejected/cancel) tetap tanggung
 *    jawab caller — gate hanya mengatur paused/aktif.
 *
 *   ./gradlew :app:testStudentDebugUnitTest
 */
class PollingGateTest {

    @Test
    fun initiallyPaused_runBlocked() {
        val gate = PollingGate(initiallyPaused = true)
        assertFalse(gate.shouldRun())
    }

    @Test
    fun resume_allowsRun() {
        val gate = PollingGate(initiallyPaused = true)
        gate.resume()
        assertTrue(gate.shouldRun())
    }

    @Test
    fun pause_blocksRunAgain() {
        val gate = PollingGate(initiallyPaused = false)
        gate.pause()
        assertFalse(gate.shouldRun())
    }

    @Test
    fun resumeReturnsTrue_onlyWhenWasPaused() {
        // Return value dipakai caller untuk memutuskan post/removeCallbacks:
        // resume() = true bila transisi paused→aktif (mulai polling),
        // pause() = true bila transisi aktif→paused (hentikan rantai).
        val gate = PollingGate(initiallyPaused = true)
        assertTrue(gate.resume())    // paused → aktif: mulai polling
        assertFalse(gate.resume())   // sudah aktif → no-op
    }

    @Test
    fun pauseReturnsTrue_onlyWhenWasRunning() {
        val gate = PollingGate(initiallyPaused = false)
        assertTrue(gate.pause())     // sedang jalan → berhenti = transisi nyata
        assertFalse(gate.pause())    // sudah berhenti → no-op
    }
}
