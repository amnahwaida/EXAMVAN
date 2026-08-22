package com.examvan.app.helper

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Mengunci kontrak gerbang recovery lock task berbasis lifecycle (fix
 * temuan review strict ronde 2 #2: "recovery health check mencoba
 * startLockTask saat activity background — gagal terus dan log spam tiap
 * 15 detik sampai siswa membuka app lagi").
 *
 * Kontrak:
 *  - Default: DISABLED (activity belum resume saat health check pertama
 *    kali dimulai dari onCreate).
 *  - shouldAttemptRecovery() hanya true setelah di-enable.
 *  - setEnabled() melaporkan transisi agar caller bisa mencatat audit
 *    sekali per perubahan, bukan spam tiap siklus.
 *
 *   ./gradlew :app:testStudentDebugUnitTest
 */
class RecoveryGateTest {

    @Test
    fun initiallyDisabled_noRecoveryAttempt() {
        val gate = RecoveryGate()
        assertFalse(gate.shouldAttemptRecovery())
    }

    @Test
    fun enable_allowsRecovery() {
        val gate = RecoveryGate()
        gate.setEnabled(true)
        assertTrue(gate.shouldAttemptRecovery())
    }

    @Test
    fun disable_blocksRecoveryAgain() {
        val gate = RecoveryGate(initiallyEnabled = true)
        gate.setEnabled(false)
        assertFalse(gate.shouldAttemptRecovery())
    }

    @Test
    fun setEnabled_reportsRealTransitionsOnly() {
        val gate = RecoveryGate(initiallyEnabled = false)

        assertTrue(gate.setEnabled(true))    // false→true: transisi nyata
        assertFalse(gate.setEnabled(true))   // sudah aktif → no-op
        assertTrue(gate.setEnabled(false))   // true→false: transisi nyata
        assertFalse(gate.setEnabled(false))  // sudah mati → no-op
    }
}
