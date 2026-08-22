package com.examvan.app.helper

import com.examvan.app.R
import org.junit.Assert.assertEquals
import org.junit.Test

/**
 * Mengunci kontrak warna banner keamanan (fix review UI/UX ronde 2 #1:
 * "medium mode memakai banner MERAH sepanjang ujian untuk kondisi normal —
 * satu keluarga dengan strict, sehingga siswa mengalami alarm fatigue dan
 * pelanggaran nyata kehilangan pembeda visual").
 *
 * Semantik yang dikunci:
 *  - STRICT          : merah kritis (kondisi terkunci/pelanggaran)
 *  - MEDIUM (aktif)  : AMBER dengan teks gelap — pemantauan aktif, bukan
 *                      bahaya; berbeda jelas dari strict
 *  - LOW / lainnya   : slate netral (tanpa auto-submit)
 *
 *   ./gradlew :app:testStudentDebugUnitTest
 */
class SecurityBannerPolicyTest {

    // ── Background ───────────────────────────────────────────────────────

    @Test
    fun background_strict_criticalRed() {
        assertEquals(
            R.color.security_banner_critical,
            SecurityBannerPolicy.backgroundRes(strictMode = true, securityLevel = "medium")
        )
        assertEquals(
            R.color.security_banner_critical,
            SecurityBannerPolicy.backgroundRes(strictMode = true, securityLevel = "low")
        )
    }

    @Test
    fun background_medium_amber_notRed() {
        val res = SecurityBannerPolicy.backgroundRes(strictMode = false, securityLevel = "medium")
        assertEquals(R.color.warning, res)
        assertNotEqualsCritical(res)
    }

    @Test
    fun background_unknownLevel_failClosed_matchesMediumAmber() {
        // Konsisten dengan ExamModePolicy: level tak dikenal diperlakukan
        // seperti mode aktif (pemantauan), bukan bebas.
        assertEquals(
            R.color.warning,
            SecurityBannerPolicy.backgroundRes(strictMode = false, securityLevel = "")
        )
        assertEquals(
            R.color.warning,
            SecurityBannerPolicy.backgroundRes(strictMode = false, securityLevel = "high")
        )
    }

    @Test
    fun background_low_slateInfo() {
        assertEquals(
            R.color.security_banner_info,
            SecurityBannerPolicy.backgroundRes(strictMode = false, securityLevel = "low")
        )
    }

    // ── Warna teks ───────────────────────────────────────────────────────

    @Test
    fun textColor_whiteOnStrictAndLow_darkOnMediumAmber() {
        assertEquals(
            R.color.security_banner_text,
            SecurityBannerPolicy.textRes(strictMode = true, securityLevel = "medium")
        )
        assertEquals(
            R.color.warning_text,
            SecurityBannerPolicy.textRes(strictMode = false, securityLevel = "medium")
        )
        assertEquals(
            R.color.security_banner_text,
            SecurityBannerPolicy.textRes(strictMode = false, securityLevel = "low")
        )
    }

    private fun assertNotEqualsCritical(res: Int) {
        org.junit.Assert.assertNotEquals(R.color.security_banner_critical, res)
        org.junit.Assert.assertNotEquals(R.color.security_banner_warning, res)
    }
}
