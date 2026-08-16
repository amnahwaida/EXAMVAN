package com.examvan.app.helper

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Mengunci temuan review #5: dua jalur submit (manual vs auto) memakai flag
 * BERBEDA (isSubmitting vs submittedOrExited) sehingga deadline yang
 * berbarengan dengan submit manual bisa mengirim DUA POST /submit. Semua
 * jalur kini berkonsultasi ke satu gate yang sama.
 *
 *   ./gradlew :app:testStudentDebugUnitTest
 */
class SubmitFlowPolicyTest {

    @Test
    fun allowsFirstSubmission_whenIdle() {
        assertTrue(SubmitFlowPolicy.canStartSubmission(isSubmitting = false, submittedOrExited = false))
    }

    @Test
    fun blocksSecondManualSubmit_whileFirstInFlight() {
        // Submit manual sedang berjalan → jalur auto (deadline) tidak boleh
        // memulai POST paralel.
        assertFalse(SubmitFlowPolicy.canStartSubmission(isSubmitting = true, submittedOrExited = false))
    }

    @Test
    fun blocksAnySubmit_afterSuccessOrExit() {
        // submittedOrExited di-set oleh jalur auto & jalur manual sukses —
        // tidak boleh ada POST lagi setelahnya.
        assertFalse(SubmitFlowPolicy.canStartSubmission(isSubmitting = false, submittedOrExited = true))
        assertFalse(SubmitFlowPolicy.canStartSubmission(isSubmitting = true, submittedOrExited = true))
    }

    @Test
    fun allowsRetryAfterFailedManualSubmit() {
        // Gagal → isSubmitting di-reset false, submittedOrExited tetap false →
        // tombol "Coba Lagi" boleh memulai ulang.
        assertTrue(SubmitFlowPolicy.canStartSubmission(isSubmitting = false, submittedOrExited = false))
    }
}
