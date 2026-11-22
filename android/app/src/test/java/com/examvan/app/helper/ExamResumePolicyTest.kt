package com.examvan.app.helper

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Auto-resume sesi ujian setelah proses dibunuh OEM (MIUI/HyperOS).
 *
 * Kontrak: kembali ke aplikasi saat ujian masih berjalan harus langsung
 * membuka soal, tanpa token ulang. Jangan resume bila sesi sudah selesai
 * (kecuali masih ada jawaban tertunda untuk layar recovery).
 */
class ExamResumePolicyTest {

    private fun snapshot(
        examId: Int = 7,
        serverUrl: String = "https://examvan.my.id",
        token: String = "ABCD1234",
        startTime: String? = "2026-11-20T09:00:00Z",
        submittedOrExited: Boolean = false,
        hasPendingAnswers: Boolean = false,
        identityData: String = "{\"student_name\":\"Siswa\"}"
    ) = ExamResumePolicy.Snapshot(
        examId, serverUrl, token, startTime, submittedOrExited,
        hasPendingAnswers, identityData
    )

    @Test
    fun activeSession_resumesWithoutToken() {
        assertTrue(ExamResumePolicy.shouldResume(snapshot()))
    }

    @Test
    fun submittedSessionWithoutPendingAnswers_doesNotResume() {
        assertFalse(
            ExamResumePolicy.shouldResume(
                snapshot(submittedOrExited = true, hasPendingAnswers = false)
            )
        )
    }

    @Test
    fun submittedSessionWithPendingAnswers_resumesForRecovery() {
        assertTrue(
            ExamResumePolicy.shouldResume(
                snapshot(submittedOrExited = true, hasPendingAnswers = true)
            )
        )
    }

    @Test
    fun missingStartTime_doesNotResume() {
        assertFalse(ExamResumePolicy.shouldResume(snapshot(startTime = null)))
        assertFalse(ExamResumePolicy.shouldResume(snapshot(startTime = "   ")))
    }

    @Test
    fun missingServerTokenOrExamId_doesNotResume() {
        assertFalse(ExamResumePolicy.shouldResume(snapshot(serverUrl = "")))
        assertFalse(ExamResumePolicy.shouldResume(snapshot(token = "")))
        assertFalse(ExamResumePolicy.shouldResume(snapshot(examId = 0)))
        assertFalse(ExamResumePolicy.shouldResume(snapshot(examId = -1)))
    }

    @Test
    fun missingOrEmptyIdentity_doesNotResume() {
        assertFalse(ExamResumePolicy.shouldResume(snapshot(identityData = "")))
        assertFalse(ExamResumePolicy.shouldResume(snapshot(identityData = "   ")))
        assertFalse(ExamResumePolicy.shouldResume(snapshot(identityData = "{}")))
        assertFalse(ExamResumePolicy.shouldResume(snapshot(identityData = "null")))
    }

    @Test
    fun questionsMayBeMissing_serverFetchHandlesIt() {
        // Snapshot tidak menyimpan questions: ExamViewer akan fetch dari
        // server memakai examId+token. Resume tetap valid.
        assertTrue(ExamResumePolicy.shouldResume(snapshot()))
    }
}
