package com.examvan.app.helper

/**
 * Keputusan render satu soal di lembar jawaban — murni fungsi agar seluruh
 * aturan validasi terkunci test JVM (fix review lembar jawaban ronde 2 #1 &
 * #2).
 *
 * Aturan:
 *  - single_choice / multiple_choice : WAJIB punya daftar pilihan valid.
 *                                      `choices` adalah nama kanonis, tetapi
 *                                      data legacy/API lama menyimpan daftar
 *                                      yang sama di `options` — fallback ini
 *                                      mencegah seluruh lembar jawaban hilang
 *                                      saat hanya `options` yang dikirim.
 *  - matching                        : WAJIB punya `left_items` DAN
 *                                      `right_items` valid.
 *  - true_false / short_answer       : tanpa data tambahan.
 *  - tipe tidak dikenal              : SKIP (dulu diam-diam masuk hitungan
 *                                      totalQuestions tanpa dirender).
 */
object QuestionRenderPolicy {

    /** Alasan soal dilewati — untuk audit log, agar skip tidak senyap. */
    enum class SkipReason { UNKNOWN_TYPE, NO_CHOICES, NO_MATCHING_ITEMS }

    /**
     * Daftar pilihan soal: utamakan `choices`, lalu fallback ke `options`.
     * Server menyimpan dua nama ini pada data historis (lihat
     * normalizeQuestionsPayload di webui); klien lama hanya membaca salah
     * satunya. Tanpa fallback, soal legacy ter-skip dan lembar jawaban kosong
     * walau PDF soal tetap tampil.
     */
    private fun choiceOptions(q: Map<String, Any>): List<String> {
        for (key in listOf("choices", "options", "pilihan", "pilihan_jawaban", "items")) {
            val choices = QuestionParsing.choiceList(q[key])
            if (choices.isNotEmpty()) return choices
        }
        return emptyList()
    }

    /** Hasil evaluasi: render atau skip — dideklarasikan di level objek. */
    sealed class Decision

    /** Soal layak dirender; data sudah tervalidasi siap pakai. */
    data class Render(
        val type: String,
        val choices: List<String>?,      // single/multiple choice
        val statements: List<String>?,   // true_false multi-pernyataan
        val leftItems: List<String>?,    // matching
        val rightItems: List<String>?,   // matching
        val numbering: String?           // matching: single/range
    ) : Decision()

    /** Soal dilewati; [reason] menjelaskan kenapa. */
    data class Skip(val reason: SkipReason) : Decision()

    fun evaluate(q: Map<String, Any>): Decision {
        val type = QuestionParsing.normalizeType(q["type"])

        val choices = when (type) {
            "single_choice", "multiple_choice" -> choiceOptions(q)
            else -> null
        }
        val statements = if (type == "true_false") {
            QuestionParsing.stringList(q["statements"]).takeIf { it.isNotEmpty() }
        } else {
            null
        }
        val leftItems = if (type == "matching") QuestionParsing.stringList(q["left_items"]) else null
        val rightItems = if (type == "matching") QuestionParsing.stringList(q["right_items"]) else null
        val numbering = if (type == "matching") QuestionParsing.normalizeNumbering(q["numbering"]) else null

        return when {
            type != "single_choice" && type != "multiple_choice" &&
                type != "true_false" && type != "matching" && type != "short_answer" ->
                Skip(SkipReason.UNKNOWN_TYPE)

            (type == "single_choice" || type == "multiple_choice") && choices.isNullOrEmpty() ->
                Skip(SkipReason.NO_CHOICES)

            type == "matching" && (leftItems.isNullOrEmpty() || rightItems.isNullOrEmpty()) ->
                Skip(SkipReason.NO_MATCHING_ITEMS)

            else -> Render(type, choices, statements, leftItems, rightItems, numbering)
        }
    }
}
