package com.examvan.app.helper

import com.examvan.app.LockTaskManager
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Test

/**
 * Mengunci kontrak label audit aktivasi pinning (fix temuan review strict
 * ronde 4 #1: "habisnya 15 poll dicatat sebagai user_cancelled_dialog
 * padahal penyebabnya bisa penolakan ATAU tidak sempat menjawab — label
 * lama mengklaim kepastian yang tidak ada").
 *
 * Kontrak: detail audit harus jujur terhadap ambiguitasnya.
 *
 *   ./gradlew :app:testStudentDebugUnitTest
 */
class LockTaskAuditLabelsTest {

    @Test
    fun rejectionDetail_isHonestAboutAmbiguity() {
        assertEquals(
            "rejected_or_no_response",
            LockTaskManager.REJECT_DETAIL_REJECTED_OR_NO_RESPONSE
        )
    }

    @Test
    fun rejectionDetail_noLongerClaimsCertainRejection() {
        // Label lama menuduh pasti penolakan user — tidak boleh kembali.
        assertNotEquals(
            "user_cancelled_dialog",
            LockTaskManager.REJECT_DETAIL_REJECTED_OR_NO_RESPONSE
        )
    }
}
