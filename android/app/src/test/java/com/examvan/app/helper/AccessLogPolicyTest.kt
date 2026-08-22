package com.examvan.app.helper

import org.junit.Assert.assertEquals
import org.junit.Test

/**
 * Mengunci kontrak event presence access-log (fix temuan review ronde 3 #4:
 * "auto-submit medium tidak mengirim access-log — server hanya tahu ujian
 * tamat dari kedatangan jawaban; audit presence tanpa penanda eksplisit").
 *
 * Kontrak: event auto-submit HARUS berbeda dari login/logout agar server
 * bisa membedakan penutupan ujian normal vs otomatis di log presence.
 *
 *   ./gradlew :app:testStudentDebugUnitTest
 */
class AccessLogPolicyTest {

    @Test
    fun autoSubmitEvent_hasExplicitValue() {
        assertEquals("auto_submit", AccessLogPolicy.EVENT_AUTO_SUBMIT)
    }

    @Test
    fun autoSubmitEvent_distinctFromLoginAndLogout() {
        // Server memakai event sebagai pembeda alur — tidak boleh tabrakan.
        val all = setOf(
            AccessLogPolicy.EVENT_LOGIN,
            AccessLogPolicy.EVENT_LOGOUT,
            AccessLogPolicy.EVENT_AUTO_SUBMIT
        )
        assertEquals(3, all.size)
    }
}
