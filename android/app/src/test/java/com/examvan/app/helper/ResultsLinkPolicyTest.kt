package com.examvan.app.helper

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Mengunci kontrak pembentukan link halaman hasil ujian (fix temuan review:
 * "token hasil ujian di URL browser tersimpan di history browser —
 * pertimbangkan WebView in-app").
 *
 * Setelah fix, token TIDAK PERNAH keluar dari app: ExamViewerActivity dan
 * CongratulationsActivity membuka ResultsViewerActivity (WebView in-app,
 * FLAG_SECURE aktif) alih-alih Intent.ACTION_VIEW ke browser eksternal.
 * Policy ini satu-satunya tempat format URL hasil didefinisikan.
 *
 *   ./gradlew :app:testStudentDebugUnitTest
 */
class ResultsLinkPolicyTest {

    @Test
    fun build_normalCase_shortLinkFormat() {
        // Format short-link server: {base}/{token} → redirect ke /hasil/<token>.
        assertEquals(
            "https://examvan.my.id/ABCD1234",
            ResultsLinkPolicy.build("https://examvan.my.id", "ABCD1234")
        )
    }

    @Test
    fun build_trimsTrailingSlashOnBase() {
        assertEquals(
            "https://examvan.my.id/ABCD1234",
            ResultsLinkPolicy.build("https://examvan.my.id/", "ABCD1234")
        )
        assertEquals(
            "https://examvan.my.id/ABCD1234",
            ResultsLinkPolicy.build("https://examvan.my.id///", "ABCD1234")
        )
    }

    @Test
    fun build_trimsWhitespaceOnBothParts() {
        assertEquals(
            "https://examvan.my.id/ABCD1234",
            ResultsLinkPolicy.build("  https://examvan.my.id  ", " ABCD1234 ")
        )
    }

    @Test
    fun build_blankBase_returnsEmpty() {
        assertEquals("", ResultsLinkPolicy.build("", "ABCD1234"))
        assertEquals("", ResultsLinkPolicy.build("   ", "ABCD1234"))
    }

    @Test
    fun build_blankToken_returnsEmpty() {
        assertEquals("", ResultsLinkPolicy.build("https://examvan.my.id", ""))
        assertEquals("", ResultsLinkPolicy.build("https://examvan.my.id", "  "))
    }

    @Test
    fun build_validResult_neverContainsQueryOrFragmentSeparator() {
        // Token selalu berupa path segment — bukan query param (?token=) yang
        // bisa ikut terkirim ke server pihak ketiga lewat Referrer header.
        val url = ResultsLinkPolicy.build("https://examvan.my.id", "ABCD1234")!!
        assertTrue(!url.contains("?") && !url.contains("#"))
    }
}
