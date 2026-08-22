package com.examvan.app.helper

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Mengunci kontrak validasi launch ExamViewerActivity (fix temuan review:
 * "cek examId == -1 dijalankan SETELAH WebSocket connect, access log, dan
 * watchdog deadline sudah jalan").
 *
 * Kontrak baru: validasi dilakukan PERTAMA kali di onCreate, sebelum efek
 * samping apapun. Policy ini murni fungsi — mudah dites di JVM.
 *
 *   ./gradlew :app:testStudentDebugUnitTest
 */
class ExamLaunchPolicyTest {

    // ── Kasus valid ──────────────────────────────────────────────────────

    @Test
    fun validate_validIntent_passes() {
        val result = ExamLaunchPolicy.validate(
            examId = 42,
            serverUrl = "https://examvan.my.id",
            examToken = "ABCD1234"
        )
        assertTrue(result is ExamLaunchPolicy.Valid)
    }

    // ── examId tidak valid ───────────────────────────────────────────────

    @Test
    fun validate_missingExamId_reportsInvalidExamId() {
        // Default Intent.getIntExtra adalah -1 saat extra tidak ada.
        val result = ExamLaunchPolicy.validate(-1, "https://examvan.my.id", "ABCD1234")
        assertEquals(ExamLaunchPolicy.Invalid.EXAM_ID, (result as ExamLaunchPolicy.Invalid).field)
    }

    @Test
    fun validate_zeroExamId_reportsInvalidExamId() {
        val result = ExamLaunchPolicy.validate(0, "https://examvan.my.id", "ABCD1234")
        assertEquals(ExamLaunchPolicy.Invalid.EXAM_ID, (result as ExamLaunchPolicy.Invalid).field)
    }

    // ── serverUrl kosong ────────────────────────────────────────────────

    @Test
    fun validate_blankServerUrl_reportsServerUrl() {
        val result = ExamLaunchPolicy.validate(1, "", "ABCD1234")
        assertEquals(ExamLaunchPolicy.Invalid.SERVER_URL, (result as ExamLaunchPolicy.Invalid).field)
    }

    // ── token kosong ────────────────────────────────────────────────────

    @Test
    fun validate_blankToken_reportsToken() {
        val result = ExamLaunchPolicy.validate(1, "https://examvan.my.id", "")
        assertEquals(ExamLaunchPolicy.Invalid.TOKEN, (result as ExamLaunchPolicy.Invalid).field)
    }

    // ── Prioritas error: field paling fundamental dilaporkan lebih dulu ──

    @Test
    fun validate_allFieldsInvalid_examIdReportedFirst() {
        // examId adalah kunci utama (PDF, submit, prefs) → dilaporkan duluan.
        val result = ExamLaunchPolicy.validate(-1, "", "")
        assertEquals(ExamLaunchPolicy.Invalid.EXAM_ID, (result as ExamLaunchPolicy.Invalid).field)
    }

    @Test
    fun validate_urlAndTokenInvalid_urlReportedBeforeToken() {
        val result = ExamLaunchPolicy.validate(5, "", "")
        assertEquals(ExamLaunchPolicy.Invalid.SERVER_URL, (result as ExamLaunchPolicy.Invalid).field)
    }

    // ── Normalisasi whitespace ───────────────────────────────────────────

    @Test
    fun validate_whitespaceOnlyValues_treatedAsBlank() {
        val urlResult = ExamLaunchPolicy.validate(1, "   ", "ABCD1234")
        assertEquals(ExamLaunchPolicy.Invalid.SERVER_URL, (urlResult as ExamLaunchPolicy.Invalid).field)

        val tokenResult = ExamLaunchPolicy.validate(1, "https://x.id", "  \t ")
        assertEquals(ExamLaunchPolicy.Invalid.TOKEN, (tokenResult as ExamLaunchPolicy.Invalid).field)
    }
}
