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
    /** matching. */
    val leftItems: List<String>?,
    /** matching. */
    val rightItems: List<String>?
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
 * pengurutan soal: nomor invalid, tipe tak dikenal, pilihan tak lengkap,
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

        for (q in questions) {
            // Nomor bisa JSON number ATAU string; invalid → skip.
            val number = QuestionParsing.questionNumber(q["number"]) ?: continue

            // Duplikat membuat dua view bertabrakan di key jawaban sama → skip.
            if (!seenNumbers.add(number)) continue

            when (
                val decision = QuestionRenderPolicy.evaluate(q)
            ) {
                is QuestionRenderPolicy.Render -> items.add(
                    AnswerSheetItem(
                        number = number,
                        type = decision.type,
                        choices = decision.choices,
                        leftItems = decision.leftItems,
                        rightItems = decision.rightItems
                    )
                )
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
