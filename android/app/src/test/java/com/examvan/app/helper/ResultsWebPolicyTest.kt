package com.examvan.app.helper

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Mengunci kontrak navigasi WebView halaman hasil (fix temuan review
 * pasca-submit #1):
 *  - Navigasi ke host yang SAMA dengan URL hasil awal tetap di dalam
 *    WebView (sandbox app, FLAG_SECURE).
 *  - Host lain TIDAK boleh dimuat di dalam WebView — dibuka eksternal.
 *  - URL rusak/null diperlakukan sebagai host berbeda (aman).
 *
 *   ./gradlew :app:testStudentDebugUnitTest
 */
class ResultsWebPolicyTest {

    private val initial = "https://examvan.my.id/ABCD1234"

    @Test
    fun sameHost_differentPath_sameHost() {
        assertTrue(
            ResultsWebPolicy.isSameHost(initial, "https://examvan.my.id/hasil/ABCD1234")
        )
        assertTrue(
            ResultsWebPolicy.isSameHost(initial, "https://examvan.my.id/")
        )
    }

    @Test
    fun exactSameUrl_sameHost() {
        assertTrue(ResultsWebPolicy.isSameHost(initial, initial))
    }

    @Test
    fun differentHost_notSameHost() {
        assertFalse(
            ResultsWebPolicy.isSameHost(initial, "https://evil.example.com/phish")
        )
    }

    @Test
    fun subdomainIsDifferentHost_notSameHost() {
        // examvan.my.id ≠ www.examvan.my.id — host harus identik persis.
        assertFalse(
            ResultsWebPolicy.isSameHost(initial, "https://www.examvan.my.id/hasil")
        )
    }

    @Test
    fun malformedTargetUrl_treatedAsDifferentHost() {
        assertFalse(ResultsWebPolicy.isSameHost(initial, "bukan-url"))
        assertFalse(ResultsWebPolicy.isSameHost(initial, ""))
    }

    @Test
    fun nullUrls_treatedAsDifferentHost() {
        assertFalse(ResultsWebPolicy.isSameHost(null, "https://examvan.my.id"))
        assertFalse(ResultsWebPolicy.isSameHost(initial, null))
        assertFalse(ResultsWebPolicy.isSameHost(null, null))
    }
}
