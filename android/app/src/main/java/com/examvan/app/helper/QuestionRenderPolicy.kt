package com.examvan.app.helper

/**
 * Keputusan render satu soal di lembar jawaban — murni fungsi agar seluruh
 * aturan validasi terkunci test JVM (fix review lembar jawaban ronde 2 #1 &
 * #2).
 *
 * Aturan:
 *  - single_choice / multiple_choice : WAJIB punya daftar `choices` valid.
 *  - matching                        : WAJIB punya `left_items` DAN
 *                                      `right_items` valid.
 *  - true_false / short_answer       : tanpa data tambahan.
 *  - tipe tidak dikenal              : SKIP (dulu diam-diam masuk hitungan
 *                                      totalQuestions tanpa dirender).
 */
object QuestionRenderPolicy {

    /** Alasan soal dilewati — untuk audit log, agar skip tidak senyap. */
    enum class SkipReason { UNKNOWN_TYPE, NO_CHOICES, NO_MATCHING_ITEMS }

    /** Hasil evaluasi: render atau skip — dideklarasikan di level objek. */
    sealed class Decision

    /** Soal layak dirender; data sudah tervalidasi siap pakai. */
    data class Render(
        val type: String,
        val choices: List<String>?,      // single/multiple choice
        val leftItems: List<String>?,    // matching
        val rightItems: List<String>?    // matching
    ) : Decision()

    /** Soal dilewati; [reason] menjelaskan kenapa. */
    data class Skip(val reason: SkipReason) : Decision()

    fun evaluate(q: Map<String, Any>): Decision {
        val type = q["type"] as? String ?: "single_choice"

        val choices = when (type) {
            "single_choice", "multiple_choice" -> QuestionParsing.stringList(q["choices"])
            else -> null
        }
        val leftItems = if (type == "matching") QuestionParsing.stringList(q["left_items"]) else null
        val rightItems = if (type == "matching") QuestionParsing.stringList(q["right_items"]) else null

        return when {
            type != "single_choice" && type != "multiple_choice" &&
                type != "true_false" && type != "matching" && type != "short_answer" ->
                Skip(SkipReason.UNKNOWN_TYPE)

            (type == "single_choice" || type == "multiple_choice") && choices.isNullOrEmpty() ->
                Skip(SkipReason.NO_CHOICES)

            type == "matching" && (leftItems.isNullOrEmpty() || rightItems.isNullOrEmpty()) ->
                Skip(SkipReason.NO_MATCHING_ITEMS)

            else -> Render(type, choices, leftItems, rightItems)
        }
    }
}
