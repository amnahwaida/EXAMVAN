package com.examvan.app.helper

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

/**
 * Mengunci perhitungan countdown: sisa waktu dihitung ulang dari end_time
 * ABSOLUT (ISO-8601 UTC) + server skew, bukan dari nilai countdown lama —
 * sehingga akurat setelah pause/resume dan perubahan jam perangkat.
 * end_time rusak / tidak ada → null (countdown tidak ditampilkan, tidak crash).
 *
 *   ./gradlew :app:testStudentDebugUnitTest
 */
class ExamDeadlineTest {

    // Titik waktu tetap: 2026-08-16T12:00:00Z (UTC) — epoch ms diverifikasi
    // (datetime(2026,8,16,12,0,0,UTC).timestamp()*1000).
    private val nowMs = 1786881600000L

    @Test
    fun remainingMs_positive_whenDeadlineInFuture() {
        val end = "2026-08-16T12:10:00Z" // +10 menit
        assertEquals(600_000L, ExamDeadline.remainingMs(end, nowMs, skewMs = 0L))
    }

    @Test
    fun remainingMs_negative_whenDeadlinePassed() {
        val end = "2026-08-16T11:50:00Z" // -10 menit
        assertEquals(-600_000L, ExamDeadline.remainingMs(end, nowMs, skewMs = 0L))
    }

    @Test
    fun remainingMs_appliesServerSkew() {
        val end = "2026-08-16T12:10:00Z" // +10 menit absolut
        // Jam perangkat 2 menit DI DEPAN server → sisa tampak lebih pendek.
        assertEquals(480_000L, ExamDeadline.remainingMs(end, nowMs, skewMs = 120_000L))
        // Jam perangkat 2 menit DI BELAKANG server → sisa tampak lebih panjang.
        assertEquals(720_000L, ExamDeadline.remainingMs(end, nowMs, skewMs = -120_000L))
    }

    @Test
    fun remainingMs_zero_atExactDeadline() {
        val end = "2026-08-16T12:00:00Z"
        assertEquals(0L, ExamDeadline.remainingMs(end, nowMs, skewMs = 0L))
    }

    @Test
    fun remainingMs_null_whenEndTimeMissingOrMalformed() {
        assertNull(ExamDeadline.remainingMs(null, nowMs, skewMs = 0L))
        assertNull(ExamDeadline.remainingMs("bukan-iso", nowMs, skewMs = 0L))
        assertNull(ExamDeadline.remainingMs("", nowMs, skewMs = 0L))
        assertNull(ExamDeadline.remainingMs("2026-13-99T99:99:99Z", nowMs, skewMs = 0L))
    }

    @Test
    fun remainingMs_acceptsOffsetFormats() {
        // Format dengan offset +07:00 (WIB) — Instant.parse menanganinya.
        // 2026-08-16T19:00:00+07:00 == 12:00:00Z → tepat deadline.
        assertEquals(0L, ExamDeadline.remainingMs("2026-08-16T19:00:00+07:00", nowMs, skewMs = 0L))
    }

    // ── Komponen tampilan timer (fix review ronde 2 #1: i18n via resource;
    //    object murni menyediakan komponen jam/menit/detik) ────────────────

    @Test
    fun components_zero_isAllZeros() {
        assertEquals(listOf(0L, 0L, 0L), ExamDeadline.remainingHms(0))
    }

    @Test
    fun components_oneHourOneMinuteOneSecond() {
        assertEquals(listOf(1L, 1L, 1L), ExamDeadline.remainingHms(3_661_000L))
    }

    @Test
    fun components_negative_clampedToZero() {
        assertEquals(listOf(0L, 0L, 0L), ExamDeadline.remainingHms(-5_000L))
    }

    @Test
    fun components_hoursBeyondTwoDigits_notTruncated() {
        // Ujian maraton 25 jam — jam tidak boleh terpotong modulo 24.
        assertEquals(listOf(25L, 0L, 0L), ExamDeadline.remainingHms(25 * 3_600_000L))
    }

    @Test
    fun components_oneHour21Minutes45Seconds() {
        assertEquals(listOf(1L, 21L, 45L), ExamDeadline.remainingHms(4_905_000L))
    }

    // ── Urgensi timer berjenjang (fix review UI/UX #2: merah bukan warna
    //    abadi — netral >10 menit, kuning <=10 menit, merah <=5 menit) ────

    private val MIN = 60_000L

    @Test
    fun urgency_farFromDeadline_normal() {
        assertEquals(
            ExamDeadline.TimerUrgency.NORMAL,
            ExamDeadline.timerUrgency(30 * MIN)
        )
        assertEquals(
            ExamDeadline.TimerUrgency.NORMAL,
            ExamDeadline.timerUrgency(10 * MIN + 1)
        )
    }

    @Test
    fun urgency_exactlyTenMinutes_warning() {
        assertEquals(ExamDeadline.TimerUrgency.WARNING, ExamDeadline.timerUrgency(10 * MIN))
        assertEquals(ExamDeadline.TimerUrgency.WARNING, ExamDeadline.timerUrgency(5 * MIN + 1))
    }

    @Test
    fun urgency_fiveMinutesAndBelow_critical() {
        assertEquals(ExamDeadline.TimerUrgency.CRITICAL, ExamDeadline.timerUrgency(5 * MIN))
        assertEquals(ExamDeadline.TimerUrgency.CRITICAL, ExamDeadline.timerUrgency(1_000L))
    }

    @Test
    fun urgency_negative_critical() {
        assertEquals(ExamDeadline.TimerUrgency.CRITICAL, ExamDeadline.timerUrgency(-1L))
    }
}
