package com.examvan.app.helper

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Mengunci kontrak retry access-log (fix temuan review low-mode ronde 2 #2:
 * "logout fire-and-forget gagal → dasbor pengawas menampilkan siswa online
 * sampai TTL presence habis, karena /complete memang tidak dipanggil di
 * jalur keluar-bebas").
 *
 * Kontrak:
 *  - Hanya event LOGOUT yang layak retry — login/heartbeat berulang rutin
 *    sehingga kegagalan satu kali tertambal iterasi berikutnya.
 *  - Retry hanya bila attempt SEBELUMNYA gagal dan belum mencapai batas.
 *
 *   ./gradlew :app:testStudentDebugUnitTest
 */
class AccessLogRetryPolicyTest {

    @Test
    fun logout_failedFirstAttempt_retried() {
        assertTrue(AccessLogRetryPolicy.shouldRetry(event = "logout", attempt = 1, success = false))
    }

    @Test
    fun logout_success_noRetry() {
        assertFalse(AccessLogRetryPolicy.shouldRetry(event = "logout", attempt = 1, success = true))
    }

    @Test
    fun logout_maxAttemptsReached_noRetry() {
        assertFalse(AccessLogRetryPolicy.shouldRetry(event = "logout", attempt = AccessLogRetryPolicy.MAX_ATTEMPTS, success = false))
    }

    @Test
    fun loginAndHeartbeat_neverRetried() {
        // Kegagalan tunggal login/heartbeat tidak perlu retry: event ini
        // dikirim ulang rutin (heartbeat berkala / aksi berikutnya).
        for (event in listOf("login", "heartbeat")) {
            assertFalse(
                "event '$event' tidak boleh di-retry",
                AccessLogRetryPolicy.shouldRetry(event = event, attempt = 1, success = false)
            )
        }
    }
}
