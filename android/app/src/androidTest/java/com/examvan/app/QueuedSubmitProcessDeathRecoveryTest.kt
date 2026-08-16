package com.examvan.app

import android.content.Intent
import android.view.View
import androidx.test.core.app.ActivityScenario
import androidx.test.espresso.Espresso.onView
import androidx.test.espresso.action.ViewActions.click
import androidx.test.espresso.assertion.ViewAssertions.matches
import androidx.test.espresso.matcher.ViewMatchers.isDisplayed
import androidx.test.espresso.matcher.ViewMatchers.withId
import androidx.test.espresso.matcher.ViewMatchers.withText
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import com.examvan.app.api.ApiClient
import okhttp3.mockwebserver.Dispatcher
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okhttp3.mockwebserver.RecordedRequest
import org.hamcrest.Matcher
import org.junit.After
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith

/**
 * Simulasi process death di jalur submit 202 queued: jawaban sudah diterima
 * server (queued — BELUM durable) dan polling /result mati bersama proses.
 * State yang bertahan hanya prefs: submitted=true (di-persist SEBELUM polling
 * dimulai) + jawaban tersimpan (belum di-clear). Re-entry harus:
 *
 *  1. menampilkan layar RECOVERY ("Kirim Lagi"), bukan dead-end "sudah selesai";
 *  2. "Kirim Lagi" mengirim ulang jawaban yang SAMA PERSIS dengan yang
 *     disubmit (flush sebelum HTTP di submitAnswers menjamin copy prefs ==
 *     jawaban asli) DAN dengan mac_address perangkat yang TIDAK kosong
 *     (macAddress di-resolve di setupRecoverySubmissionManager — kalau kosong,
 *     server meng-upsert baris dengan MAC "unknown" dan baris asli tak
 *     ter-update);
 *  3. /result "done" → layar congrats.
 *
 * Jalankan di perangkat/emulator:
 *   ./gradlew :app:connectedStudentDebugAndroidTest
 */
@RunWith(AndroidJUnit4::class)
class QueuedSubmitProcessDeathRecoveryTest {

    private lateinit var server: MockWebServer
    private var submitRequestBody: String? = null

    @Before
    fun setUp() {
        AppPrefs.clearAllData(InstrumentationRegistry.getInstrumentation().targetContext)
        ApiClient.setBaseUrl("")
        submitRequestBody = null

        server = MockWebServer()
        server.dispatcher = object : Dispatcher() {
            override fun dispatch(request: RecordedRequest): MockResponse {
                val path = request.path ?: ""
                return when {
                    path.endsWith("/submit") -> {
                        submitRequestBody = request.body.readUtf8()
                        MockResponse().setResponseCode(202).setBody(SUBMIT_QUEUED_JSON)
                    }
                    path.contains("/result") ->
                        MockResponse().setResponseCode(200).setBody(RESULT_DONE_JSON)
                    else -> MockResponse().setResponseCode(404).setBody("""{"message":"not found"}""")
                }
            }
        }
        server.start()
    }

    @After
    fun tearDown() {
        server.shutdown()
        ApiClient.setBaseUrl("")
    }

    @Test
    fun reentryAfterQueuedProcessDeath_showsRecovery_resubmitsSameAnswers_andCongrats() {
        val context = InstrumentationRegistry.getInstrumentation().targetContext
        val mockUrl = server.url("/").toString()

        // Simulasi state yang bertahan setelah proses mati di tengah polling:
        //  - submitted=true (di-persist sinkron sebelum waitForQueuedResult);
        //  - jawaban masih tersimpan (clearSavedAnswers hanya saat /result "done").
        val prefs = AppPrefs.getExamPrefsSafe(context)
        prefs.edit()
            .putBoolean(AppPrefs.getSubmittedOrExitedKey(EXAM_ID), true)
            .putString(AppPrefs.KEY_SAVED_ANSWERS, SAVED_ANSWERS_JSON)
            .putInt(AppPrefs.KEY_SAVED_ANSWERS_EXAM_ID, EXAM_ID)
            .putLong(AppPrefs.KEY_SAVED_ANSWERS_TIMESTAMP, System.currentTimeMillis())
            .apply()
        ApiClient.setBaseUrl(mockUrl)

        val intent = Intent(context, ExamViewerActivity::class.java).apply {
            putExtra("exam_id", EXAM_ID)
            putExtra("exam_name", "Ujian Akhir Semester")
            putExtra("student_name", "Siswa Uji Coba")
            putExtra("student_number", "01")
            putExtra("student_class", "9A")
            putExtra("identity_data", """{"student_name":"Siswa Uji Coba"}""")
            putExtra("server_url", mockUrl)
            putExtra("exam_token", "ABC12345")
            addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        }
        ActivityScenario.launch<ExamViewerActivity>(intent)

        // 1) Layar recovery muncul (bukan dead-end "Ujian Sudah Selesai")
        waitForView(withId(R.id.btnRetryDownload))
        onView(withId(R.id.tvErrorMsg)).check(matches(withText(R.string.recovery_pending_message)))
        onView(withId(R.id.btnRetryDownload)).check(matches(withText(R.string.recovery_retry_submit)))

        // 2) Tap "Kirim Lagi" → resubmit mengirim ulang jawaban tersimpan
        onView(withId(R.id.btnRetryDownload)).perform(click())

        // Tunggu POST /submit tiba di MockWebServer
        val deadline = System.currentTimeMillis() + 15_000
        while (submitRequestBody == null && System.currentTimeMillis() < deadline) {
            Thread.sleep(200)
        }
        assertTrue("POST /submit harus diterima dari layar recovery", submitRequestBody != null)

        // 3) Body submit = jawaban SAMA PERSIS dengan yang tersimpan (bukan
        //    kosong / copy stale) DAN mac_address perangkat TIDAK kosong.
        val body = submitRequestBody!!
        assertTrue("answers '1' harus terkirim: $body", body.contains("\"1\":\"A\""))
        assertTrue("answers '2' harus terkirim: $body", body.contains("\"2\":\"B\""))
        assertTrue("answers '3' harus terkirim: $body", body.contains("\"3\":\"Benar\""))
        assertTrue("mac_address harus ada di body: $body", body.contains("\"mac_address\":\""))
        assertFalse("mac_address TIDAK boleh kosong (server akan memakai 'unknown'): $body",
            body.contains("\"mac_address\":\"\""))

        // 4) /result "done" → layar congrats
        waitForView(withId(R.id.tvCongratsTitle))
        onView(withId(R.id.tvCongratsTitle)).check(matches(withText(R.string.congrats_title)))
    }

    /** Poll sampai view cocok muncul (toleransi async network + navigasi). */
    private fun waitForView(matcher: Matcher<View>, timeoutMs: Long = 15_000) {
        val deadline = System.currentTimeMillis() + timeoutMs
        while (System.currentTimeMillis() < deadline) {
            try {
                onView(matcher).check(matches(isDisplayed()))
                return
            } catch (_: Throwable) {
                Thread.sleep(200)
            }
        }
        onView(matcher).check(matches(isDisplayed())) // gagal dengan pesan jelas
    }

    companion object {
        private const val EXAM_ID = 1
        private const val SAVED_ANSWERS_JSON = """{"1":"A","2":"B","3":"Benar"}"""
        private const val SUBMIT_QUEUED_JSON =
            """{"success":true,"message":"Jawaban diterima, sedang dikoreksi","status":"queued","job_id":"job-1"}"""
        private const val RESULT_DONE_JSON = """{"status":"done","score":80}"""
    }
}
