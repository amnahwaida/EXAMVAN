package com.examvan.app.api

import com.examvan.app.model.HealthResponse
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okhttp3.mockwebserver.RecordedRequest
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import java.nio.file.Files
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicReference

/**
 * Simulasi kontrak server EXAMVAN di lapisan HTTP (tanpa emulator).
 *
 * Menjalankan [MockWebServer] lokal dan menggerakkan [ApiClient] melalui
 * SELURUH alur siswa yang juga dilalui tester pada closed test Play Store:
 *
 *   health → join by token → request-approval → unduh PDF → access-log
 *   (presence) → submit (async queued) → poll result
 *
 * Ini "simulasi" yang bisa dijalankan di CI / mesin developer kapan saja:
 *
 *   ./gradlew :app:testStudentDebugUnitTest
 *
 * Catatan: ApiClient adalah object dengan state statis — setiap test
 * meng-set baseUrl ke MockWebServer dan meresetnya di tearDown.
 */
class ApiClientFlowSimulationTest {

    private lateinit var server: MockWebServer

    // ── Fixtures (JSON sesuai kontrak server webui) ────────────────────────

    private val healthJson = """
        {"status":"ok","version":"2.6.0","required_app_version":"2.6.0",
         "server_time_utc":"2026-08-16T00:00:00Z"}
    """.trimIndent()

    private val examJson = """
        {"success":true,"exam":{
           "id":1,"name":"Ujian Akhir Semester","status":"active","size_mb":1.5,
           "token":"ABC12345","security_level":"medium","strict_mode":false,
           "identity_fields":[
              {"key":"student_name","label":"Nama Siswa","required":true},
              {"key":"exam_number","label":"Nomor Ujian","required":true},
              {"key":"student_class","label":"Kelas","required":true}],
           "created_at":"2026-08-16T00:00:00Z"}}
    """.trimIndent()

    private val approvalPendingJson = """{"status":"pending"}"""

    private val submitQueuedJson = """
        {"success":true,"message":"Jawaban diterima, sedang dikoreksi",
         "status":"queued","job_id":"job-7f3a"}
    """.trimIndent()

    private val resultDoneJson = """
        {"status":"done","score":80,"message":"Koreksi selesai"}
    """.trimIndent()

    private val minimalPdf = "%PDF-1.4\n1 0 obj<<>>endobj\ntrailer<<>>\n%%EOF".toByteArray()

    @Before
    fun setUp() {
        server = MockWebServer()
        server.start()
        // State statis ApiClient dibersihkan penuh — lihat ApiClient.resetForTests.
        ApiClient.resetForTests()
        ApiClient.setBaseUrl(server.url("/").toString().removeSuffix("/"))
    }

    @After
    fun tearDown() {
        server.shutdown()
        ApiClient.resetForTests()
    }

    @Test
    fun fullStudentFlow_simulatesClosedTestJourney() {
        // 1. health
        server.enqueue(MockResponse().setResponseCode(200).setBody(healthJson))
        // 2. join by token
        server.enqueue(MockResponse().setResponseCode(200).setBody(examJson))
        // 3. request-approval (pending)
        server.enqueue(MockResponse().setResponseCode(200).setBody(approvalPendingJson))
        // 4. PDF
        server.enqueue(MockResponse().setResponseCode(200)
            .setHeader("Content-Type", "application/pdf")
            .setBody(okio.Buffer().write(minimalPdf)))
        // 5. access-log (login)
        server.enqueue(MockResponse().setResponseCode(200).setBody("""{"success":true}"""))
        // 6. submit → queued
        server.enqueue(MockResponse().setResponseCode(200).setBody(submitQueuedJson))
        // 7. result → done
        server.enqueue(MockResponse().setResponseCode(200).setBody(resultDoneJson))

        // ── 1. Health ─────────────────────────────────────────────────────
        val healthResp = await { cb: (HealthResponse) -> Unit ->
            ApiClient.checkHealth(onSuccess = cb, onError = { error(it) })
        }
        assertEquals("2.6.0", healthResp.required_app_version)

        // ── 2. Join by token ─────────────────────────────────────────────
        val tokenResp = awaitObject<com.examvan.app.model.TokenExamResponse> { cb ->
            ApiClient.getExamByToken(token = "ABC12345", onSuccess = cb, onError = { _, msg -> error(msg) })
        }
        assertTrue(tokenResp.success)
        assertEquals(1, tokenResp.data?.id)
        assertEquals("Ujian Akhir Semester", tokenResp.data?.name)
        assertEquals("medium", tokenResp.data?.security_level)
        assertEquals(3, tokenResp.data?.identity_fields?.size)

        // ── 3. Request approval ──────────────────────────────────────────
        val approvalStatus = await<String> { cb ->
            ApiClient.requestApproval(
                examId = 1, macAddress = "DEVICE:test123", studentName = "Siswa Uji",
                examNumber = "01", studentClass = "9A", identityDataStr = "{}",
                token = "ABC12345", onSuccess = cb,
                onError = { _, msg -> cb("ERR:$msg") }
            )
        }
        assertEquals("pending", approvalStatus)

        // ── 4. Unduh PDF ─────────────────────────────────────────────────
        val cacheDir = Files.createTempDirectory("examvan-test").toFile()
        val pdfFile = awaitObject<java.io.File> { cb ->
            ApiClient.downloadPdf(
                examId = 1, token = "ABC12345", deviceId = "DEVICE:test123",
                cacheDir = cacheDir, onProgress = {}, onSuccess = cb,
                onError = { error(it) }
            )
        }
        assertTrue("file PDF harus terunduh", pdfFile.exists())
        assertEquals(minimalPdf.size.toLong(), pdfFile.length())

        // ── 5. Access-log (presence, fire-and-forget) ────────────────────
        // Diverifikasi di urutan request akhir (setelah semua callback selesai,
        // antrian MockWebServer berisi 7 request dalam urutan penerimaan).
        ApiClient.sendAccessLog(
            examId = 1, token = "ABC12345", macAddress = "DEVICE:test123",
            event = "login", studentName = "Siswa Uji", examNumber = "01",
            studentClass = "9A", deviceInfo = "TestDevice"
        )

        // ── 6. Submit (async queued) ─────────────────────────────────────
        val submit = awaitObject<ApiClient.SubmitResult> { cb ->
            ApiClient.submitExam(
                examId = 1, token = "ABC12345", studentName = "Siswa Uji",
                examNumber = "01", studentClass = "9A",
                answers = mapOf("1" to "A", "2" to "B"),
                startTime = "2026-08-16T00:00:00Z", macAddress = "DEVICE:test123",
                onSuccess = cb, onError = { error(it) }
            )
        }
        assertTrue(submit.success)
        assertEquals("queued", submit.status)
        assertEquals("job-7f3a", submit.jobId)

        // ── 7. Poll result ───────────────────────────────────────────────
        val result = awaitObject<ApiClient.ExamResultPoll> { cb ->
            ApiClient.getExamResult(
                examId = 1, token = "ABC12345", macAddress = "DEVICE:test123",
                identityData = null, jobId = "job-7f3a",
                onSuccess = cb, onError = { error(it) }
            )
        }
        assertEquals("done", result.status)
        assertEquals(80, result.score)

        // ── Verifikasi urutan request yang diterima server ───────────────
        assertPath(server.takeRequest(5, TimeUnit.SECONDS), "/api/health")
        assertPath(server.takeRequest(5, TimeUnit.SECONDS), "/api/exams/token/ABC12345")
        assertPath(server.takeRequest(5, TimeUnit.SECONDS), "/api/exams/request-approval")
        assertPath(server.takeRequest(5, TimeUnit.SECONDS), "/api/exams/1/pdf")
        val accessLogReq = server.takeRequest(5, TimeUnit.SECONDS)
        assertPath(accessLogReq, "/api/exams/1/access-log")
        assertEquals("ABC12345", accessLogReq!!.getHeader("X-Exam-Token"))
        assertPath(server.takeRequest(5, TimeUnit.SECONDS), "/api/exams/1/submit")
        val resultReq = server.takeRequest(5, TimeUnit.SECONDS)
        assertNotNull("result harus diterima", resultReq)
        assertEquals("/api/exams/1/result", resultReq!!.path?.substringBefore("?"))
        assertTrue("result harus membawa mac_address", resultReq.path?.contains("mac_address=") == true)
        assertTrue("result harus membawa job_id", resultReq.path?.contains("job_id=job-7f3a") == true)
    }

    @Test
    fun joinByToken_unknownToken_returnsNotFoundMessage() {
        server.enqueue(MockResponse().setResponseCode(404).setBody("""{"message":"Token tidak valid"}"""))

        val (code, message) = awaitPair<Int, String> { cb ->
            ApiClient.getExamByToken(token = "XXXX1234", onSuccess = {}, onError = { c, m -> cb(c to m) })
        }
        assertEquals(404, code)
        assertEquals("Token tidak valid atau ujian sudah berakhir", message)
    }

    @Test
    fun outdatedVersion_isDetectedByClient_againstRequiredVersion() {
        // health meminta versi lebih baru → client harus menolak ikut ujian
        server.enqueue(MockResponse().setResponseCode(200).setBody(
            """{"status":"ok","version":"2.7.0","required_app_version":"2.7.0"}"""
        ))
        val required = awaitObject<HealthResponse> { cb -> ApiClient.checkHealth(onSuccess = cb, onError = {}) }
        assertEquals("2.7.0", required.required_app_version)
        // VersionName lokal (BuildConfig) 2.6.0 < 2.7.0 → UpdateManager.isOutdated
        assertTrue(
            "2.6.0 harus dianggap outdated terhadap 2.7.0",
            com.examvan.app.helper.UpdateManager.isOutdated("2.6.0", "2.7.0")
        )
    }

    // ── Helpers ──────────────────────────────────────────────────────────

    private fun assertPath(req: RecordedRequest?, expected: String) {
        assertNotNull("request $expected harus diterima", req)
        assertEquals(expected, req!!.path)
    }

    /** Jalankan callback async, tunggu hasil, kembalikan. */
    private fun <T> await(block: ((T) -> Unit) -> Unit): T {
        val latch = CountDownLatch(1)
        val ref = AtomicReference<T>()
        block { value ->
            ref.set(value)
            latch.countDown()
        }
        assertTrue("callback tidak pernah dipanggil", latch.await(10, TimeUnit.SECONDS))
        return ref.get()
    }

    private fun <T> awaitObject(block: ((T) -> Unit) -> Unit): T = await(block)

    private fun <A, B> awaitPair(block: ((Pair<A, B>) -> Unit) -> Unit): Pair<A, B> = await(block)
}
