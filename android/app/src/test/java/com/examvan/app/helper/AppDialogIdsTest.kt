package com.examvan.app.helper

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Mengunci kontrak ID holder dialog di AppDialogFlagRegistry (fix temuan
 * review mode medium ronde 4 #1: "3 kelompok dialog di SubmissionManager
 * masih set flag mentah tanpa holder — invariant 'flag = ada dialog aktif'
 * belum penuh").
 *
 * Aturan yang dikunci:
 * 1. Semua dialog yang memegang flag WAJIB punya ID unik — dua dialog aktif
 *    bersamaan tidak boleh saling melepaskan flag (release id-A tidak boleh
 *    menjatuhkan holder id-B).
 * 2. ID tidak boleh kosong.
 *
 *   ./gradlew :app:testStudentDebugUnitTest
 */
class AppDialogIdsTest {

    @Test
    fun allDialogIds_areUnique() {
        val ids = listOf(
            AppDialogIds.LOGOUT_CONFIRM,
            AppDialogIds.PERMISSION_PROMPT,
            AppDialogIds.SUBMIT_CONFIRM,
            AppDialogIds.SUBMIT_FAILED,
            AppDialogIds.SUBMIT_CONGRATS
        )
        assertEquals(
            "ID holder dialog harus unik — duplikat membuat release satu " +
                "dialog ikut menjatuhkan proteksi dialog lain",
            ids.size,
            ids.toSet().size
        )
    }

    @Test
    fun allDialogIds_areNotBlank() {
        val ids = listOf(
            AppDialogIds.LOGOUT_CONFIRM,
            AppDialogIds.PERMISSION_PROMPT,
            AppDialogIds.SUBMIT_CONFIRM,
            AppDialogIds.SUBMIT_FAILED,
            AppDialogIds.SUBMIT_CONGRATS
        )
        assertTrue(ids.all { it.isNotBlank() })
    }

    @Test
    fun permissionPromptId_matchesWiringInExamViewerActivity_contract() {
        // ID ini dipakai bersama oleh ExamViewerActivity (wiring markPending)
        // dan SubmissionManager (koordinator izin) melalui registry yang sama
        // — harus konstanta yang sama, bukan string literal terpisah.
        assertEquals("permission_prompt", AppDialogIds.PERMISSION_PROMPT)
    }
}
