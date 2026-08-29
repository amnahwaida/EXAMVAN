package com.examvan.app

import android.content.Intent
import androidx.test.core.app.ActivityScenario
import androidx.test.core.app.ApplicationProvider
import androidx.test.espresso.Espresso.onView
import androidx.test.espresso.assertion.ViewAssertions.matches
import androidx.test.espresso.matcher.ViewMatchers.isDisplayed
import androidx.test.espresso.matcher.ViewMatchers.withId
import androidx.test.espresso.matcher.ViewMatchers.withText
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Test
import org.junit.runner.RunWith

/**
 * Smoke test layar pasca-submit (CongratulationsActivity) — jalur yang
 * dilihat siswa setelah jawaban berhasil dikumpulkan. Murni UI, tanpa server.
 *
 *   ./gradlew :app:connectedStudentDebugAndroidTest
 */
@RunWith(AndroidJUnit4::class)
class CongratulationsSmokeTest {

    @Test
    fun congratsScreen_rendersTitleAndActions() {
        val context = ApplicationProvider.getApplicationContext<android.content.Context>()
        val intent = Intent(context, CongratulationsActivity::class.java)
            .putExtra("server_url", "https://examvan.my.id")
            .putExtra("exam_token", "ABC12345")
            .putExtra("exam_name", "Ujian Akhir Semester")
            .putExtra("student_name", "Siswa Uji")
            .putExtra("student_number", "01")
            .putExtra("student_class", "9A")
            .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)

        ActivityScenario.launch<CongratulationsActivity>(intent)

        onView(withId(R.id.tvCongratsTitle)).check(matches(withText(R.string.congrats_title)))
        onView(withId(R.id.tvExamName)).check(matches(withText("Ujian Akhir Semester")))
        onView(withId(R.id.tvCongratsMessage)).check(matches(isDisplayed()))
        onView(withId(R.id.btnCopyLink)).check(matches(isDisplayed()))
    }
}
