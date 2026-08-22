package com.examvan.app

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Mengunci kontrak counter AuditLog + accessor [AuditLog.countFor] yang
 * ditambahkan agar penolakan re-pin dari health check bisa diverifikasi
 * (fix temuan review strict #2: "penolakan dialog re-pin saat recovery
 * health check SENYAP — tidak ada LOCKTASK_REJECTED di audit log, berbeda
 * dengan aktivasi awal").
 *
 * android.util.Log di-stub no-op oleh unitTests.isReturnDefaultValues
 * (build.gradle.kts), sehingga object ini bisa diuji penuh di JVM.
 *
 *   ./gradlew :app:testStudentDebugUnitTest
 */
class AuditLogTest {

    @Test
    fun countFor_incrementsPerEvent() {
        AuditLog.reset()

        repeat(3) { AuditLog.w(AuditLog.Events.LOCKTASK_REJECTED, "test") }
        repeat(1) { AuditLog.i(AuditLog.Events.LOCKTASK_ACTIVATE, "test") }

        assertEquals(3, AuditLog.countFor(AuditLog.Events.LOCKTASK_REJECTED))
        assertEquals(1, AuditLog.countFor(AuditLog.Events.LOCKTASK_ACTIVATE))
    }

    @Test
    fun countFor_unknownEvent_zero() {
        AuditLog.reset()
        assertEquals(0, AuditLog.countFor("EVENT_TIDAK_ADA"))
    }

    @Test
    fun reset_clearsAllCounters() {
        AuditLog.reset()
        AuditLog.w(AuditLog.Events.SUBMIT_SUCCESS, "test")

        AuditLog.reset()

        assertEquals(0, AuditLog.countFor(AuditLog.Events.SUBMIT_SUCCESS))
    }

    // ── Thread-safety (fix review strict ronde 3 #1) ─────────────────────

    /**
     * Pemanggil AuditLog kini tersebar multi-thread: Dispatchers.IO (scan
     * lingkungan), GlobalScope (submit background), dan main thread.
     * Counter HARUS aman konkurensi — HashMap akan kehilangan update atau
     * melempar exception pada beban seperti ini.
     */
    @Test
    fun concurrentWrites_allCounted_exactlyOnce() {
        AuditLog.reset()

        val threads = 8
        val perThread = 500
        val threadsList = (0 until threads).map { t ->
            Thread {
                repeat(perThread) { i ->
                    if (i % 2 == 0) {
                        AuditLog.w(AuditLog.Events.LOCKTASK_LOST, "t$t-$i")
                    } else {
                        AuditLog.i(AuditLog.Events.LOCKTASK_HEALTH_OK, "t$t-$i")
                    }
                }
            }
        }
        threadsList.forEach { it.start() }
        threadsList.forEach { it.join(10_000) }

        assertEquals(
            "Update yang hilang = counter tidak thread-safe",
            (threads * perThread / 2).toLong(),
            AuditLog.countFor(AuditLog.Events.LOCKTASK_LOST).toLong()
        )
        assertEquals(
            (threads * perThread / 2).toLong(),
            AuditLog.countFor(AuditLog.Events.LOCKTASK_HEALTH_OK).toLong()
        )
    }

    @Test
    fun concurrentReadDuringWrite_doesNotThrow() {
        AuditLog.reset()
        val firstWrite = java.util.concurrent.CountDownLatch(1)
        val stop = java.util.concurrent.atomic.AtomicBoolean(false)
        val writer = Thread {
            while (!stop.get()) {
                AuditLog.w(AuditLog.Events.FOCUS_LOST_SUSPICIOUS, "stress")
                firstWrite.countDown()
            }
        }
        writer.start()
        try {
            // Pastikan penulis benar-benar sudah menulis sebelum mulai membaca.
            assertTrue("penulis tidak pernah menulis", firstWrite.await(5_000, java.util.concurrent.TimeUnit.MILLISECONDS))
            repeat(1000) { AuditLog.countFor(AuditLog.Events.FOCUS_LOST_SUSPICIOUS) }
        } finally {
            stop.set(true)
            writer.join(5_000)
        }
        assertTrue(AuditLog.countFor(AuditLog.Events.FOCUS_LOST_SUSPICIOUS) > 0)
    }
}
