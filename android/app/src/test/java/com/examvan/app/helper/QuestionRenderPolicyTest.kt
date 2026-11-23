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
    fun singleChoice_legacyOptions_rendered() {
        // Data legacy/admin lama menyimpan pilihan di `options`; tanpa
        // fallback seluruh lembar jawaban kosong walau PDF tetap tampil.
        val d = QuestionRenderPolicy.evaluate(
            q("number" to 1, "type" to "single_choice", "options" to listOf("Jakarta", "Bogor"))
        )
        assertTrue(d is QuestionRenderPolicy.Render)
        assertEquals(listOf("Jakarta", "Bogor"), (d as QuestionRenderPolicy.Render).choices)
    }

    @Test
    fun multipleChoice_emptyChoices_fallsBackToOptions() {
        val d = QuestionRenderPolicy.evaluate(
            q("number" to 2, "type" to "multiple_choice",
                "choices" to emptyList<String>(), "options" to listOf("A", "B", "C"))
        )
        assertTrue(d is QuestionRenderPolicy.Render)
        assertEquals(listOf("A", "B", "C"), (d as QuestionRenderPolicy.Render).choices)
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
    fun trueFalseMultiStatements_renderedWithStatements() {
        val d = QuestionRenderPolicy.evaluate(
            q("number" to 4, "type" to "true_false", "statements" to listOf("Satu", "Dua"))
        )
        assertTrue(d is QuestionRenderPolicy.Render)
        assertEquals(listOf("Satu", "Dua"), (d as QuestionRenderPolicy.Render).statements)
    }

    @Test
    fun trueFalseCustomLabels_areCarriedForRendering() {
        val d = QuestionRenderPolicy.evaluate(
            q("number" to 4, "type" to "true_false",
                "true_label" to "Sesuai", "false_label" to "Tidak Sesuai")
        )
        assertTrue(d is QuestionRenderPolicy.Render)
        assertEquals("Sesuai", (d as QuestionRenderPolicy.Render).trueLabel)
        assertEquals("Tidak Sesuai", d.falseLabel)
    }

    @Test
    fun trueFalseLegacyWithoutStatements_renderedAsSingle() {
        val d = QuestionRenderPolicy.evaluate(q("number" to 4, "type" to "true_false"))
        assertTrue(d is QuestionRenderPolicy.Render)
        assertEquals(null, (d as QuestionRenderPolicy.Render).statements)
    }

    @Test
    fun shortAnswer_renderedWithoutChoices() {
        val d = QuestionRenderPolicy.evaluate(q("number" to 5, "type" to "short_answer"))
        assertTrue(d is QuestionRenderPolicy.Render)
    }

    @Test
    fun shortAnswerMulti_carriesStatementsAndNumbering() {
        val d = QuestionRenderPolicy.evaluate(
            q("number" to 5, "type" to "short_answer", "numbering" to "range",
                "statements" to listOf("Ibu kota", "Hasil"))
        )
        assertTrue(d is QuestionRenderPolicy.Render)
        val r = d as QuestionRenderPolicy.Render
        assertEquals(listOf("Ibu kota", "Hasil"), r.statements)
        assertEquals("range", r.numbering)
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
    fun matchingRange_carriesNumbering() {
        val d = QuestionRenderPolicy.evaluate(
            q("number" to 1, "type" to "matching", "numbering" to "range",
                "left_items" to listOf("1", "2"), "right_items" to listOf("A", "B"))
        )
        assertTrue(d is QuestionRenderPolicy.Render)
        assertEquals("range", (d as QuestionRenderPolicy.Render).numbering)
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
        // FIX #1: tipe typo/tak dikenal ("file_upload", "singel_choice")
        // harus SKIP — dulu tetap masuk hitungan totalQuestions tanpa dirender.
        for (type in listOf("file_upload", "singel_choice", "upload")) {
            val d = QuestionRenderPolicy.evaluate(q("number" to 7, "type" to type))
            assertTrue(
                "Tipe '$type' harus di-skip",
                d is QuestionRenderPolicy.Skip && d.reason == QuestionRenderPolicy.SkipReason.UNKNOWN_TYPE
            )
        }
    }

    @Test
    fun legacyTypeAliases_mappedToCanonicalTypes() {
        val essay = QuestionRenderPolicy.evaluate(q("number" to 1, "type" to "essay"))
        assertTrue(essay is QuestionRenderPolicy.Render)
        assertEquals(AnswerSheetItem.TYPE_SHORT_ANSWER, (essay as QuestionRenderPolicy.Render).type)

        val pg = QuestionRenderPolicy.evaluate(
            q("number" to 2, "type" to "pg", "options" to listOf("A", "B"))
        )
        assertTrue(pg is QuestionRenderPolicy.Render)
        assertEquals(AnswerSheetItem.TYPE_SINGLE_CHOICE, (pg as QuestionRenderPolicy.Render).type)

        val benarSalah = QuestionRenderPolicy.evaluate(q("number" to 3, "type" to "benar_salah"))
        assertTrue(benarSalah is QuestionRenderPolicy.Render)
        assertEquals(AnswerSheetItem.TYPE_TRUE_FALSE, (benarSalah as QuestionRenderPolicy.Render).type)
    }

    @Test
    fun objectChoices_renderedFromTextFields() {
        val d = QuestionRenderPolicy.evaluate(
            q("number" to 1, "type" to "single_choice",
                "options" to listOf(
                    mapOf("text" to "Jakarta"),
                    mapOf("label" to "Bandung")
                ))
        )
        assertTrue(d is QuestionRenderPolicy.Render)
        assertEquals(
            listOf("Jakarta", "Bandung"),
            (d as QuestionRenderPolicy.Render).choices
        )
    }
}
