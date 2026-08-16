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
}
