package com.examvan.app.helper

import com.examvan.app.api.ApiClient
import kotlinx.coroutines.runBlocking
import okhttp3.mockwebserver.Dispatcher
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okhttp3.mockwebserver.RecordedRequest
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test

/**
 * Mengunci perbaikan temuan review #1 (deadline polling /result tegas) dan
 * #5 (state statis ApiClient di-reset antar test).
 *
 *   ./gradlew :app:testStudentDebugUnitTest
 */
class QueuedResultPollingTest {

    private lateinit var server: MockWebServer

    @Before
    fun setUp() {
        server = MockWebServer()
        server.start()
        ApiClient.resetForTests()
        ApiClient.setBaseUrl(server.url("/").toString().removeSuffix("/"))
    }

    @After
    fun tearDown() {
        server.shutdown()
        ApiClient.resetForTests()
    }

    @Test
    fun awaitDurable_returnsTrue_whenWorkerConfirmsDone() {
        server.enqueue(MockResponse().setResponseCode(200).setBody("""{"status":"done","score":85}"""))

        val durable = runBlocking {
            QueuedResultPolling.awaitDurable(
                examId = 1, token = "ABC12345", macAddress = "DEVICE:t1",
                identityData = null, jobId = "job-1",
                deadlineMs = 5_000, maxAttempts = 2
            )
        }
        assertTrue("worker melaporkan done → jawaban durable", durable)
    }

    @Test
    fun awaitDurable_returnsFalse_whenWorkerReportsFailed() {
        server.enqueue(MockResponse().setResponseCode(200).setBody("""{"status":"failed"}"""))

        val durable = runBlocking {
            QueuedResultPolling.awaitDurable(
                examId = 1, token = "ABC12345", macAddress = "DEVICE:t1",
                identityData = null, jobId = "job-1",
                deadlineMs = 5_000, maxAttempts = 2
            )
        }
        assertFalse("worker gagal → tidak durable", durable)
    }

    /**
     * Inti temuan #1: server /result MENGGANTUNG jauh melewati deadline.
     * Sebelum perbaikan, `CompletableDeferred.await()` tanpa timeout menahan
     * loop hingga read-timeout OkHttp (30 dtk) per iterasi → poll bisa bertahan
     * jauh melewati deadline. Setelah perbaikan, deadline TEGAS: selesai
     * ≤ deadline + slack kecil.
     */
    @Test
    fun awaitDurable_enforcesDeadline_whenServerStalls() {
        // Latch membuat dispatcher /result menggantung sampai test selesai
        // assert — shutdown() di tearDown bisa berjalan bersih (tanpa thread
        // yang masih tidur).
        val releaseStall = java.util.concurrent.CountDownLatch(1)
        server.dispatcher = object : Dispatcher() {
            override fun dispatch(request: RecordedRequest): MockResponse {
                return if (request.path?.startsWith("/api/exams/1/result") == true) {
                    releaseStall.await() // gantung — jauh melewati deadline test
                    MockResponse().setResponseCode(200).setBody("""{"status":"done"}""")
                } else {
                    MockResponse().setResponseCode(404).setBody("""{"message":"not found"}""")
                }
            }
        }

        val start = System.currentTimeMillis()
        val durable = runBlocking {
            QueuedResultPolling.awaitDurable(
                examId = 1, token = "ABC12345", macAddress = "DEVICE:t1",
                identityData = null, jobId = "job-1",
                deadlineMs = 3_000, maxAttempts = 100
            )
        }
        val elapsed = System.currentTimeMillis() - start

        assertFalse("server menggantung → tidak boleh dianggap durable", durable)
        assertTrue("deadline harus ditegakkan (elapsed=${elapsed}ms)", elapsed < 8_000)

        releaseStall.countDown() // bebaskan dispatcher sebelum shutdown
    }

    /** Inti temuan #5: state statis ApiClient di-reset penuh antar test. */
    @Test
    fun resetForTests_clearsAllStaticState() {
        ApiClient.setBaseUrl("http://localhost:1234")
        ApiClient.EXPECTED_FINGERPRINT = "sha256/AA=="
        ApiClient.serverTimeSkewMs = 42L

        ApiClient.resetForTests()

        assertEquals("", ApiClient.getBaseUrl())
        assertNull(ApiClient.EXPECTED_FINGERPRINT)
        assertEquals(0L, ApiClient.serverTimeSkewMs)
    }
}
