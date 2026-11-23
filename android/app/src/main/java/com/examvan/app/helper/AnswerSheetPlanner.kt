package com.examvan.app.helper

/**
 * Satu soal yang layak dirender di lembar jawaban — hasil [AnswerSheetPlanner.plan],
 * data sudah tervalidasi siap pakai oleh adapter RecyclerView.
 */
data class AnswerSheetItem(
    val number: Int,
    val type: String,
    /** single_choice / multiple_choice. */
    val choices: List<String>?,
    /** true_false multi-pernyataan atau short_answer multi-isian; null/empty = satu field. */
    val statements: List<String>?,
    /** matching. */
    val leftItems: List<String>?,
    /** matching. */
    val rightItems: List<String>?,
    /** Nomor terakhir yang dicakup (matching numbering=range); null = single. */
    val numberEnd: Int? = null,
    val trueLabel: String? = null,
    val falseLabel: String? = null
) {
    companion object {
        const val TYPE_SINGLE_CHOICE = "single_choice"
        const val TYPE_TRUE_FALSE = "true_false"
        const val TYPE_MULTIPLE_CHOICE = "multiple_choice"
        const val TYPE_MATCHING = "matching"
        const val TYPE_SHORT_ANSWER = "short_answer"
    }
}

/**
 * Perencana lembar jawaban (fix arsitektur: RecyclerView untuk penghematan
 * memori pada ujian 100+ soal).
 *
 * [AnswerSheetPlanner.plan] adalah SATU-SATUNYA tempat aturan validasi &
 * pengurutan soal: nomor invalid memakai nomor urut posisi (agar soal tidak
 * hilang), tipe tak dikenal/pilihan tak lengkap tidak menghasilkan item,
 * dan nomor duplikat tidak menghasilkan item. Adapter RecyclerView tinggal
 * meng-render daftar ini — view didaur ulang, memori konstan terhadap
 * jumlah soal.
 */
object AnswerSheetPlanner {

    /**
     * Sink observabilitas alasan skip (fix review RecyclerView ronde 2 #2):
     * setiap soal yang dilewati dilaporkan beserta nomor & alasannya.
     * Default null (tanpa pelaporan); production mengisi Log.w, test memakai
     * collector. Selalu dikembalikan ke null setelah pemakaian.
     */
    @Volatile
    var skipSink: ((number: Int, reason: QuestionRenderPolicy.SkipReason) -> Unit)? = null

    fun plan(questions: List<Map<String, Any>>): List<AnswerSheetItem> {
        val seenNumbers = mutableSetOf<Int>()
        val items = mutableListOf<AnswerSheetItem>()
        // Nomor eksplisit dipertahankan; nomor rusak mendapat nomor urut yang
        // belum dipakai. Server memakai aturan yang sama saat menilai.
        val explicitNumbers = questions.mapNotNull {
            QuestionParsing.questionNumber(it["number"])
        }.toSet()

        for ((index, q) in questions.withIndex()) {
            // Nomor bisa JSON number ATAU string; jika invalid, pakai nomor
            // urut posisi agar soal tetap bisa dijawab (bukan hilang diam-diam).
            val parsedNumber = QuestionParsing.questionNumber(q["number"])
            val number = parsedNumber ?: run {
                var candidate = index + 1
                while (candidate in explicitNumbers || candidate in seenNumbers) candidate++
                candidate
            }

            // Duplikat membuat dua view bertabrakan di key jawaban sama → skip.
            if (!seenNumbers.add(number)) continue

            when (
                val decision = QuestionRenderPolicy.evaluate(q)
            ) {
                is QuestionRenderPolicy.Render -> {
                    val span = when (decision.type) {
                        AnswerSheetItem.TYPE_MATCHING -> decision.leftItems?.size ?: 0
                        AnswerSheetItem.TYPE_SHORT_ANSWER -> decision.statements?.size ?: 0
                        else -> 0
                    }
                    val numberEnd = if (decision.numbering == "range" && span > 1) {
                        number + span - 1
                    } else {
                        null
                    }
                    // Nomor di dalam rentang juga terpakai: soal berikutnya
                    // (atau nomor fallback) tidak boleh menabrak nomor 2..N.
                    if (numberEnd != null) {
                        for (n in number + 1..numberEnd) seenNumbers.add(n)
                    }
                    items.add(
                        AnswerSheetItem(
                            number = number,
                            type = decision.type,
                            choices = decision.choices,
                            statements = decision.statements,
                            leftItems = decision.leftItems,
                            rightItems = decision.rightItems,
                            numberEnd = numberEnd,
                            trueLabel = decision.trueLabel,
                            falseLabel = decision.falseLabel
                        )
                    )
                }
                is QuestionRenderPolicy.Skip -> {
                    // Fix review RecyclerView ronde 2 #2: alasan skip tidak
                    // boleh senyap — laporkan lewat sink bila terpasang.
                    skipSink?.invoke(number, decision.reason)
                }
            }
        }
        return items
    }
}
