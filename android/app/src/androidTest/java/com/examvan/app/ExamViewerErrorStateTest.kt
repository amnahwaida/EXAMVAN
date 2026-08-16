package com.examvan.app

import android.content.Intent
import android.view.View
import androidx.test.core.app.ActivityScenario
import androidx.test.core.app.ApplicationProvider
import androidx.test.espresso.Espresso.onView
import androidx.test.espresso.assertion.ViewAssertions.matches
import androidx.test.espresso.matcher.ViewMatchers.isDisplayed
import androidx.test.espresso.matcher.ViewMatchers.withId
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.hamcrest.Matcher
import org.junit.Test
import org.junit.runner.RunWith

/**
 * Smoke test ExamViewerActivity dengan server tidak terjangkau.
 *
 * Dua jalur sah yang sama-sama berujung pada layout error yang recoverable:
 *  - server tak terjangkau → error unduhan PDF (btnRetryDownload);
 *  - emulator/root/ADB terdeteksi → blokir keamanan SecurityEnforcer
 *    (juga memakai layoutError + btnRetryDownload).
 * Keduanya menampilkan btnRetryDownload — itulah yang di-assert.
 *
 *   ./gradlew :app:connectedStudentDebugAndroidTest
 */
@RunWith(AndroidJUnit4::class)
class ExamViewerErrorStateTest {

    @Test
    fun examViewer_showsRecoverableErrorState_whenServerUnreachable() {
        val context = ApplicationProvider.getApplicationContext<android.content.Context>()
        val intent = Intent(context, ExamViewerActivity::class.java)
            .putExtra("server_url", "http://127.0.0.1:1") // tidak terjangkau
            .putExtra("exam_token", "ABC12345")
            .putExtra("exam_id", 1)
            .putExtra("exam_name", "Ujian Akhir Semester")
            .putExtra("security_level", "medium")
            .putExtra("strict_mode", false)
            .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)

        ActivityScenario.launch<ExamViewerActivity>(intent)

        waitForView(withId(R.id.btnRetryDownload))
        onView(withId(R.id.btnRetryDownload)).check(matches(isDisplayed()))
    }

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
}
