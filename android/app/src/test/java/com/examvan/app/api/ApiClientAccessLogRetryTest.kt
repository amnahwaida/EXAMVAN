package com.examvan.app.api

import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicReference

/**
 * Verifikasi lapisan HTTP untuk retry access-log (fix temuan review
 * low-mode ronde 2 #2): logout yang gagal (5xx / putus) diulang SEKALI,
 * dan hasil akhir dilaporkan ke caller.
 *
 *   ./gradlew :app:testStudentDebugUnitTest
 */
class ApiClientAccessLogRetryTest {

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
    fun sendAccessLogWithRetry_logoutFailsOnce_thenSucceeds_twoRequestsHitServer() {
        server.enqueue(MockResponse().setResponseCode(500))
        server.enqueue(MockResponse().setResponseCode(200).setBody("""{"success":true}"""))

        val finalSuccess = AtomicReference<Boolean?>(null)
        val latch = CountDownLatch(1)

        ApiClient.sendAccessLogWithRetry(
            examId = 1, token = "ABC12345", macAddress = "DEVICE:test123",
            event = "logout", studentName = "Siswa Uji",
            onResult = { success ->
                finalSuccess.set(success)
                latch.countDown()
            }
        )

        assertTrue("callback hasil akhir tidak pernah dipanggil", latch.await(10, TimeUnit.SECONDS))
        assertEquals(true, finalSuccess.get())

        val first = server.takeRequest(5, TimeUnit.SECONDS)
        assertNotNull(first)
        assertEquals("/api/exams/1/access-log", first!!.path)
        assertTrue(first.body.readUtf8().contains("\"event\":\"logout\""))

        val second = server.takeRequest(5, TimeUnit.SECONDS)
        assertNotNull("request retry harus diterima server", second)
        assertEquals("/api/exams/1/access-log", second!!.path)
    }

    @Test
    fun sendAccessLogWithRetry_bothAttemptsFail_reportsFailure_afterTwoRequests() {
        server.enqueue(MockResponse().setResponseCode(500))
        server.enqueue(MockResponse().setResponseCode(500))

        val finalSuccess = AtomicReference<Boolean?>(null)
        val latch = CountDownLatch(1)

        ApiClient.sendAccessLogWithRetry(
            examId = 1, token = "ABC12345", macAddress = "DEVICE:test123",
            event = "logout",
            onResult = { success ->
                finalSuccess.set(success)
                latch.countDown()
            }
        )

        assertTrue(latch.await(10, TimeUnit.SECONDS))
        assertEquals(false, finalSuccess.get())

        // Tepat dua percobaan — tidak lebih.
        assertNotNull(server.takeRequest(5, TimeUnit.SECONDS))
        assertNotNull(server.takeRequest(5, TimeUnit.SECONDS))
        assertNull(server.takeRequest(250, TimeUnit.MILLISECONDS))
    }

    @Suppress("NAME_SHADOWING")
    private fun assertNull(req: okhttp3.mockwebserver.RecordedRequest?) {
        if (req != null) throw AssertionError("tidak boleh ada request tambahan")
    }
}
