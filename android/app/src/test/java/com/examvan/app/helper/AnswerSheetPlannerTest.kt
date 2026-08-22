package com.examvan.app.helper

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Mengunci kontrak perencanaan lembar jawaban (fix arsitektur: RecyclerView
 * untuk penghematan memori pada ujian 100+ soal — view tidak lagi di-inflate
 * semua sekaligus ke LinearLayout).
 *
 * [AnswerSheetPlanner.plan] adalah sumber tunggal urutan & validitas item:
 *  - nomor invalid / tipe tak dikenal / pilihan tak lengkap / nomor
 *    duplikat → TIDAK menghasilkan item;
 *  - soal valid dipetakan ke tipe kanonis dengan data siap-pakai,
 *    URUTAN konfigurasi dipertahankan.
 *
 *   ./gradlew :app:testStudentDebugUnitTest
 */
class AnswerSheetPlannerTest {

    @Test
    fun plan_validMixedQuestions_preservedInOrder() {
        val items = AnswerSheetPlanner.plan(
            listOf(
                mapOf("number" to 1, "type" to "single_choice", "choices" to listOf("A", "B")),
                mapOf("number" to 2, "type" to "true_false"),
                mapOf("number" to 3, "type" to "multiple_choice", "choices" to listOf("A", "B", "C")),
                mapOf("number" to 4, "type" to "matching",
                    "left_items" to listOf("1"), "right_items" to listOf("X")),
                mapOf("number" to 5, "type" to "short_answer")
            )
        )

        assertEquals(
            listOf(1, 2, 3, 4, 5),
            items.map { it.number }
        )
        assertEquals(
            listOf(
                AnswerSheetItem.TYPE_SINGLE_CHOICE,
                AnswerSheetItem.TYPE_TRUE_FALSE,
                AnswerSheetItem.TYPE_MULTIPLE_CHOICE,
                AnswerSheetItem.TYPE_MATCHING,
                AnswerSheetItem.TYPE_SHORT_ANSWER
            ),
            items.map { it.type }
        )
    }

    @Test
    fun plan_singleChoice_carriesChoices() {
        val items = AnswerSheetPlanner.plan(
            listOf(mapOf("number" to 1, "type" to "single_choice", "choices" to listOf("A")))
        )
        assertEquals(listOf("A"), items.single().choices)
    }

    @Test
    fun plan_matching_carriesLeftAndRight() {
        val items = AnswerSheetPlanner.plan(
            listOf(mapOf("number" to 1, "type" to "matching",
                "left_items" to listOf("1", "2"), "right_items" to listOf("A", "B")))
        )
        assertEquals(listOf("1", "2"), items.single().leftItems)
        assertEquals(listOf("A", "B"), items.single().rightItems)
    }

    @Test
    fun plan_invalidNumber_skipped() {
        val items = AnswerSheetPlanner.plan(
            listOf(mapOf("number" to "bukan-angka", "type" to "short_answer"))
        )
        assertEquals(emptyList<AnswerSheetItem>(), items)
    }

    @Test
    fun plan_unknownType_skipped() {
        val items = AnswerSheetPlanner.plan(
            listOf(mapOf("number" to 1, "type" to "essay"))
        )
        assertEquals(emptyList<AnswerSheetItem>(), items)
    }

    @Test
    fun plan_choiceWithoutChoices_skipped() {
        val items = AnswerSheetPlanner.plan(
            listOf(
                mapOf("number" to 1, "type" to "single_choice"),
                mapOf("number" to 2, "type" to "multiple_choice", "choices" to emptyList<String>())
            )
        )
        assertEquals(emptyList<AnswerSheetItem>(), items)
    }

    @Test
    fun plan_matchingIncomplete_skipped() {
        val items = AnswerSheetPlanner.plan(
            listOf(mapOf("number" to 1, "type" to "matching", "left_items" to listOf("1")))
        )
        assertEquals(emptyList<AnswerSheetItem>(), items)
    }

    @Test
    fun plan_duplicateNumbers_onlyFirstKept() {
        val items = AnswerSheetPlanner.plan(
            listOf(
                mapOf("number" to 7, "type" to "true_false"),
                mapOf("number" to 7, "type" to "short_answer")
            )
        )
        assertEquals(1, items.size)
        assertEquals(AnswerSheetItem.TYPE_TRUE_FALSE, items.single().type)
    }

    @Test
    fun plan_emptyConfig_emptyResult() {
        assertEquals(emptyList<AnswerSheetItem>(), AnswerSheetPlanner.plan(emptyList()))
    }

    @Test
    fun plan_missingType_defaultsToSingleChoice() {
        val items = AnswerSheetPlanner.plan(
            listOf(mapOf("number" to 9, "choices" to listOf("A")))
        )
        assertEquals(AnswerSheetItem.TYPE_SINGLE_CHOICE, items.single().type)
        assertNull(items.single().leftItems)
    }

    // ── Observabilitas alasan skip (fix review RecyclerView ronde 2 #2) ──

    @Test
    fun skipEvents_reported_withNumberAndReason() {
        // Dulu skip senyap setelah refactor ke planner — auditabilitas hilang.
        val events = mutableListOf<Pair<Int, QuestionRenderPolicy.SkipReason>>()
        AnswerSheetPlanner.skipSink = { number, reason -> events.add(number to reason) }
        try {
            AnswerSheetPlanner.plan(
                listOf(
                    mapOf("number" to "rusak"),                                     // nomor invalid — TIDAK dilaporkan (tak ada nomor)
                    mapOf("number" to 1, "type" to "essay"),                        // UNKNOWN_TYPE
                    mapOf("number" to 2, "type" to "single_choice"),                // NO_CHOICES
                    mapOf("number" to 3, "type" to "matching", "left_items" to listOf("1")), // NO_MATCHING_ITEMS
                    mapOf("number" to 4, "type" to "true_false")                    // valid
                )
            )
        } finally {
            AnswerSheetPlanner.skipSink = null
        }

        assertEquals(
            listOf(
                1 to QuestionRenderPolicy.SkipReason.UNKNOWN_TYPE,
                2 to QuestionRenderPolicy.SkipReason.NO_CHOICES,
                3 to QuestionRenderPolicy.SkipReason.NO_MATCHING_ITEMS
            ),
            events
        )
    }

    @Test
    fun skipEvents_validQuestions_produceNoEvents() {
        val events = mutableListOf<Pair<Int, QuestionRenderPolicy.SkipReason>>()
        AnswerSheetPlanner.skipSink = { number, reason -> events.add(number to reason) }
        try {
            AnswerSheetPlanner.plan(
                listOf(
                    mapOf("number" to 1, "type" to "true_false"),
                    mapOf("number" to 2, "type" to "short_answer")
                )
            )
        } finally {
            AnswerSheetPlanner.skipSink = null
        }
        assertTrue(events.isEmpty())
    }
}
