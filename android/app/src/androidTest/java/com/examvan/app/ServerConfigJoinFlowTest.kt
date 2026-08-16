package com.examvan.app

import android.view.View
import android.view.ViewGroup
import android.widget.EditText
import androidx.test.core.app.ActivityScenario
import androidx.test.espresso.Espresso.onView
import androidx.test.espresso.action.ViewActions.click
import androidx.test.espresso.action.ViewActions.replaceText
import androidx.test.espresso.assertion.ViewAssertions.matches
import androidx.test.espresso.matcher.ViewMatchers.isDescendantOfA
import androidx.test.espresso.matcher.ViewMatchers.isDisplayed
import androidx.test.espresso.matcher.ViewMatchers.withClassName
import androidx.test.espresso.matcher.ViewMatchers.withId
import androidx.test.espresso.matcher.ViewMatchers.withText
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import com.examvan.app.api.ApiClient
import okhttp3.mockwebserver.Dispatcher
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okhttp3.mockwebserver.RecordedRequest
import org.hamcrest.CoreMatchers.allOf
import org.hamcrest.CoreMatchers.endsWith
import org.hamcrest.Description
import org.hamcrest.Matcher
import org.hamcrest.TypeSafeMatcher
import org.junit.After
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith

/**
 * Simulasi alur join siswa — bagian dari persiapan closed test Play Store.
 *
 * Menjalankan MockWebServer LOKAL di perangkat (debug build mengizinkan
 * cleartext ke localhost; release tetap memblokir), lalu menelusuri layar
 * nyata: ServerConfig (URL + token) → dialog identitas → WaitingApproval.
 *
 * Jalankan di perangkat/emulator:
 *   ./gradlew :app:connectedStudentDebugAndroidTest
 */
@RunWith(AndroidJUnit4::class)
class ServerConfigJoinFlowTest {

    private lateinit var server: MockWebServer

    @Before
    fun setUp() {
        // Mulai dari state bersih — data tersimpan bisa mengubah alur UI.
        AppPrefs.clearAllData(InstrumentationRegistry.getInstrumentation().targetContext)
        ApiClient.setBaseUrl("")

        server = MockWebServer()
        server.dispatcher = object : Dispatcher() {
            override fun dispatch(request: RecordedRequest): MockResponse {
                val path = request.path ?: ""
                return when {
                    path == "/api/health" -> MockResponse().setResponseCode(200).setBody(HEALTH_JSON)
                    path.startsWith("/api/exams/token/") -> MockResponse().setResponseCode(200).setBody(EXAM_JSON)
                    path.contains("/request-approval") ->
                        MockResponse().setResponseCode(200).setBody("""{"status":"pending"}""")
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
    fun joinByToken_fillsIdentity_navigatesToWaitingApproval() {
        ActivityScenario.launch(ServerConfigActivity::class.java)

        val mockUrl = server.url("/").toString() // http://localhost:PORT/

        onView(withId(R.id.etServerUrl)).perform(replaceText(mockUrl))
        onView(withId(R.id.etToken)).perform(replaceText("ABC12345"))
        onView(withId(R.id.btnConnect)).perform(click())

        // Dialog identitas muncul — dengan judul ujian dari server (async)
        waitForView(withId(R.id.tvDialogExamTitle))
        onView(withId(R.id.tvDialogExamTitle)).check(matches(withText("Ujian Akhir Semester")))

        // Isi 3 field identitas dinamis dari identity_fields server
        onView(nthEditTextInContainer(0)).perform(replaceText("Siswa Uji Coba"))
        onView(nthEditTextInContainer(1)).perform(replaceText("01"))
        onView(nthEditTextInContainer(2)).perform(replaceText("9A"))

        onView(withId(R.id.btnConfirmStart)).perform(click())

        // Pindah ke layar menunggu persetujuan pengawas
        waitForView(withId(R.id.tvWaitingTitle))
        onView(withId(R.id.tvWaitingTitle)).check(matches(withText(R.string.approval_waiting_title)))
    }

    @Test
    fun joinWithInvalidToken_showsTokenError() {
        server.dispatcher = object : Dispatcher() {
            override fun dispatch(request: RecordedRequest): MockResponse {
                return when {
                    request.path == "/api/health" -> MockResponse().setResponseCode(200).setBody(HEALTH_JSON)
                    request.path?.startsWith("/api/exams/token/") == true ->
                        MockResponse().setResponseCode(404).setBody("""{"message":"Token tidak valid"}""")
                    else -> MockResponse().setResponseCode(404).setBody("""{"message":"not found"}""")
                }
            }
        }

        ActivityScenario.launch(ServerConfigActivity::class.java)

        onView(withId(R.id.etServerUrl)).perform(replaceText(server.url("/").toString()))
        onView(withId(R.id.etToken)).perform(replaceText("XXXX1234"))
        onView(withId(R.id.btnConnect)).perform(click())

        waitForView(withId(R.id.tvError))
        onView(withId(R.id.tvError)).check(matches(withText(R.string.error_token_not_found)))
    }

    // ── Helpers ──────────────────────────────────────────────────────────

    /**
     * Matcher deterministik: EditText ke-`index` (urutan tambah) di dalam
     * fieldsContainer dialog identitas — field dibuat dinamis tanpa ID.
     */
    private fun nthEditTextInContainer(index: Int): Matcher<View> = object : TypeSafeMatcher<View>() {
        override fun matchesSafely(item: View): Boolean {
            if (item !is EditText) return false
            var parent: android.view.ViewParent? = item.parent
            while (parent != null) {
                val container = parent
                if (container is ViewGroup && container.id == R.id.fieldsContainer) {
                    val editTexts = (0 until container.childCount)
                        .map { container.getChildAt(it) }
                        .filterIsInstance<EditText>()
                    return editTexts.indexOf(item) == index
                }
                parent = parent.parent
            }
            return false
        }

        override fun describeTo(description: Description) {
            description.appendText("EditText #$index di dalam fieldsContainer")
        }
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
        private const val HEALTH_JSON =
            """{"status":"ok","version":"2.6.0","required_app_version":"2.6.0"}"""
        private const val EXAM_JSON =
            """{"success":true,"exam":{"id":1,"name":"Ujian Akhir Semester","status":"active",
               "size_mb":1.5,"token":"ABC12345","security_level":"medium","strict_mode":false,
               "identity_fields":[
                 {"key":"student_name","label":"Nama Siswa","required":true},
                 {"key":"exam_number","label":"Nomor Ujian","required":true},
                 {"key":"student_class","label":"Kelas","required":true}],
               "created_at":"2026-08-16T00:00:00Z"}}"""
    }
}
