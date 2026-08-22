package com.examvan.app.helper

import com.examvan.app.helper.ExamDeadline.TimerUrgency
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Mengunci kontrak umpan balik haptic momen kritis (backlog aksesibilitas:
 * "haptic feedback momen kritical").
 *
 * Kontrak: getar HANYA pada TRANSISI masuk CRITICAL — bukan tiap tick
 * (tanpa ini perangkat bergetar tiap detik selama 5 menit terakhir).
 * Transisi ke WARNING / NORMAL tidak menggetarkan. Durasi satu nilai.
 *
 *   ./gradlew :app:testStudentDebugUnitTest
 */
class HapticPolicyTest {

    @Test
    fun enteringCritical_buzzes() {
        assertTrue(
            HapticPolicy.shouldBuzz(previous = TimerUrgency.NORMAL, current = TimerUrgency.CRITICAL)
        )
        assertTrue(
            "Dari null (tick pertama sejak build) langsung critical juga dapat getar",
            HapticPolicy.shouldBuzz(previous = null, current = TimerUrgency.CRITICAL)
        )
    }

    @Test
    fun stayingCritical_noRepeatedBuzz() {
        assertFalse(
            HapticPolicy.shouldBuzz(previous = TimerUrgency.CRITICAL, current = TimerUrgency.CRITICAL)
        )
    }

    @Test
    fun warningAndNormal_neverBuzz() {
        for (urgency in listOf(TimerUrgency.WARNING, TimerUrgency.NORMAL)) {
            assertFalse(
                HapticPolicy.shouldBuzz(previous = null, current = urgency)
            )
            assertFalse(
                HapticPolicy.shouldBuzz(previous = TimerUrgency.CRITICAL, current = urgency)
            )
        }
    }

    @Test
    fun buzzDuration_isShort() {
        // Getar pendek untuk peringatan, bukan pola panjang.
        assertEquals(200L, HapticPolicy.BUZZ_DURATION_MS)
    }
}
