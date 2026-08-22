package com.examvan.app.helper

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Mengunci kontrak reset sesi ujian saat siswa bergabung ke ujian (fix temuan
 * review mode medium ronde 5 #1 & #2):
 *
 * #1 — "Ujian A gagal submit → KEY_EXAM_START_TIME dipertahankan untuk
 *       recovery, lalu siswa bergabung ke ujian B → viewer B memuat
 *       start_time milik A". Reset session-scoped keys WAJIB terjadi hanya
 *       saat bergabung ke ujian yang BERBEDA; bergabung ulang ke ujian yang
 *       sama (rejoin pasca proses mati) tidak boleh menghapus apa pun agar
 *       resilience tetap berfungsi.
 *
 * #2 — "flag submitted_or_exit_<id> menumpuk tanpa pembersihan". Pemangkasan
 *       harus selektif: flag milik ujian LAIN dihapus, flag ujian aktif dan
 *       key non-flag tidak tersentuh.
 *
 *   ./gradlew :app:testStudentDebugUnitTest
 */
class ExamSessionResetPolicyTest {

    // ── Keputusan reset ──────────────────────────────────────────────────

    @Test
    fun reset_joiningDifferentExam_resets() {
        assertTrue(ExamSessionResetPolicy.shouldResetSession(storedExamId = 10, newExamId = 42))
    }

    @Test
    fun reset_rejoiningSameExam_doesNotReset() {
        assertFalse(
            "Rejoin ujian yang sama (proses mati) tidak boleh menghapus " +
                "start_time / state recovery",
            ExamSessionResetPolicy.shouldResetSession(storedExamId = 42, newExamId = 42)
        )
    }

    @Test
    fun reset_noStoredExam_resets() {
        // KEY_EXAM_ID tidak ada (remember unchecked / install baru) →
        // default -1 → selalu reset.
        assertTrue(ExamSessionResetPolicy.shouldResetSession(storedExamId = -1, newExamId = 42))
    }

    // ── Pengenalan kunci flag submitted ──────────────────────────────────

    @Test
    fun submittedFlagKey_recognized() {
        assertTrue(ExamSessionResetPolicy.isSubmittedOrExitedKey("submitted_or_exit_42"))
        assertTrue(ExamSessionResetPolicy.isSubmittedOrExitedKey("submitted_or_exit_7"))
    }

    @Test
    fun submittedFlagKey_otherKeysRejected() {
        assertFalse(ExamSessionResetPolicy.isSubmittedOrExitedKey("questions_json"))
        assertFalse(ExamSessionResetPolicy.isSubmittedOrExitedKey("exam_start_time"))
        assertFalse(ExamSessionResetPolicy.isSubmittedOrExitedKey("submitted_or_exit_extra"))
    }

    @Test
    fun submittedFlagKey_parsesExamId() {
        assertEquals(42, ExamSessionResetPolicy.submittedExamIdFromKey("submitted_or_exit_42"))
        assertNull(ExamSessionResetPolicy.submittedExamIdFromKey("submitted_or_exit_abc"))
        assertNull(ExamSessionResetPolicy.submittedExamIdFromKey("questions_json"))
    }

    // ── Pemangkasan selektif ─────────────────────────────────────────────

    @Test
    fun prune_removesOnlyOtherExamsFlags() {
        val keys = listOf(
            "submitted_or_exit_10",
            "submitted_or_exit_42",
            "submitted_or_exit_99",
            "questions_json",
            "exam_start_time"
        )
        val pruned = ExamSessionResetPolicy.prunableSubmittedFlagKeys(keys, keepExamId = 42)
        assertEquals(listOf("submitted_or_exit_10", "submitted_or_exit_99"), pruned)
    }

    @Test
    fun prune_unparseableFlagKeys_alsoPruned() {
        // Flag tanpa id numerik adalah sampah — aman dipangkas.
        val pruned = ExamSessionResetPolicy.prunableSubmittedFlagKeys(
            listOf("submitted_or_exit_x", "submitted_or_exit_5"), keepExamId = 5
        )
        assertEquals(listOf("submitted_or_exit_x"), pruned)
    }

    @Test
    fun prune_emptyInput_emptyOutput() {
        assertTrue(ExamSessionResetPolicy.prunableSubmittedFlagKeys(emptyList(), 1).isEmpty())
    }
}
