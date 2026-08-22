package com.examvan.app.helper

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Mengunci kontrak keputusan render soal di lembar jawaban.
 *
 * Fix review lembar jawaban ronde 2:
 *  - #1 "tipe soal tak dikenal tetap dihitung tapi tidak dirender" —
 *    keputusan kini eksplisit: tipe tidak dikenal → SKIP dengan alasan,
 *    totalQuestions jujur.
 *  - #2 lanjutan: soal pilihan/matching tanpa data valid → SKIP
 *    (dulu dibuatkan opsi A–E / 1-2-3 palsu).
 *
 *   ./gradlew :app:testStudentDebugUnitTest
 */
class QuestionRenderPolicyTest {

    private fun q(vararg pairs: Pair<String, Any>) = mapOf(*pairs)

    @Test
    fun singleChoice_withChoices_rendered() {
        val d = QuestionRenderPolicy.evaluate(q("number" to 1, "type" to "single_choice", "choices" to listOf("A", "B")))
        assertTrue(d is QuestionRenderPolicy.Render)
        assertEquals(listOf("A", "B"), (d as QuestionRenderPolicy.Render).choices)
    }

    @Test
    fun singleChoice_withoutChoices_skipped() {
        val d = QuestionRenderPolicy.evaluate(q("number" to 1, "type" to "single_choice"))
        assertEquals(QuestionRenderPolicy.SkipReason.NO_CHOICES, (d as QuestionRenderPolicy.Skip).reason)
    }

    @Test
    fun multipleChoice_emptyList_skipped() {
        val d = QuestionRenderPolicy.evaluate(q("number" to 2, "type" to "multiple_choice", "choices" to emptyList<String>()))
        assertEquals(QuestionRenderPolicy.SkipReason.NO_CHOICES, (d as QuestionRenderPolicy.Skip).reason)
    }

    @Test
    fun missingType_defaultsToSingleChoiceRules() {
        // Tanpa "type" → single_choice; tanpa choices → skip.
        val d = QuestionRenderPolicy.evaluate(q("number" to 3))
        assertEquals(QuestionRenderPolicy.SkipReason.NO_CHOICES, (d as QuestionRenderPolicy.Skip).reason)
    }

    @Test
    fun trueFalse_renderedWithoutChoices() {
        val d = QuestionRenderPolicy.evaluate(q("number" to 4, "type" to "true_false"))
        assertTrue(d is QuestionRenderPolicy.Render)
        assertEquals(null, (d as QuestionRenderPolicy.Render).choices)
    }

    @Test
    fun shortAnswer_renderedWithoutChoices() {
        val d = QuestionRenderPolicy.evaluate(q("number" to 5, "type" to "short_answer"))
        assertTrue(d is QuestionRenderPolicy.Render)
    }

    @Test
    fun matching_withBothLists_rendered() {
        val d = QuestionRenderPolicy.evaluate(
            q("number" to 6, "type" to "matching", "left_items" to listOf("1"), "right_items" to listOf("A"))
        )
        assertTrue(d is QuestionRenderPolicy.Render)
        assertEquals(listOf("1"), (d as QuestionRenderPolicy.Render).leftItems)
    }

    @Test
    fun matching_missingRightItems_skipped() {
        val d = QuestionRenderPolicy.evaluate(
            q("number" to 6, "type" to "matching", "left_items" to listOf("1"))
        )
        assertEquals(QuestionRenderPolicy.SkipReason.NO_MATCHING_ITEMS, (d as QuestionRenderPolicy.Skip).reason)
    }

    @Test
    fun unknownType_skipped_notCounted() {
        // FIX #1: tipe typo/tak dikenal ("essay", "singel_choice") harus
        // SKIP — dulu tetap masuk hitungan totalQuestions tanpa dirender.
        for (type in listOf("essay", "singel_choice", "")) {
            val d = QuestionRenderPolicy.evaluate(q("number" to 7, "type" to type))
            assertTrue(
                "Tipe '$type' harus di-skip",
                d is QuestionRenderPolicy.Skip && d.reason == QuestionRenderPolicy.SkipReason.UNKNOWN_TYPE
            )
        }
    }
}
