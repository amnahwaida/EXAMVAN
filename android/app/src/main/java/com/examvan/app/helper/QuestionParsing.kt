package com.examvan.app.helper

/**
 * Parsing nomor soal dari konfigurasi `questions_json`.
 *
 * Server menyimpan `Question.Number` sebagai `interface{}` — bisa JSON number
 * ATAU string tergantung sumber konfigurasi. Sebelumnya AnswerSheetBuilder
 * memakai `(q["number"] as? Double)?.toInt() ?: continue`, sehingga soal dengan
 * nomor string (mis. `"3"`) di-SKIP diam-diam: tidak muncul di lembar jawaban,
 * tidak bisa dijawab, dan dinilai kosong oleh server — tanpa error apa pun.
 *
 * Fungsi ini menangani semua bentuk (Double/Int/String) dan mengembalikan
 * `null` hanya untuk nilai yang benar-benar tidak valid.
 */
internal object QuestionParsing {

    /**
     * Konversi nilai `number` dari JSON soal menjadi nomor soal, atau `null`
     * bila tidak valid (nilai non-numerik / bukan angka).
     */
    fun questionNumber(raw: Any?): Int? = when (raw) {
        is Number -> raw.toInt() // mencakup Double (default Gson) dan Int
        is String -> raw.trim().toIntOrNull()
        else -> null
    }

    /**
     * Tipe soal kanonis. Data legacy/API lama memakai alias seperti `essay`,
     * `pg`, atau `benar_salah`; tanpa normalisasi soal-soal itu dianggap tipe
     * tak dikenal dan hilang dari lembar jawaban.
     */
    fun normalizeType(raw: Any?): String {
        val type = (raw as? String)
            ?.trim()
            ?.lowercase()
            ?.replace('-', '_')
            ?.replace(' ', '_')
            ?: ""
        return when (type) {
            "", "single_choice", "singlechoice", "single", "pg",
            "pilihan_ganda", "pilihanganda", "pilihan_ganda_biasa" -> "single_choice"
            "multiple_choice", "multiplechoice", "multiple",
            "pg_kompleks", "pgkompleks", "pilihan_ganda_kompleks",
            "multiple_response" -> "multiple_choice"
            "true_false", "truefalse", "tf", "benar_salah", "benarsalah" -> "true_false"
            "matching", "match", "menjodohkan", "jodoh", "jodohkan" -> "matching"
            "short_answer", "shortanswer", "short", "essay",
            "isian", "isian_singkat", "uraian" -> "short_answer"
            else -> type
        }
    }

    /**
     * Mode penomoran matching: "single" (satu nomor untuk seluruh kartu) atau
     * "range" (tiap item kiri memakai nomor). Nilai tak dikenal = null dan
     * dirender sebagai single (perilaku lama).
     */
    fun normalizeNumbering(raw: Any?): String? {
        val text = (raw as? String)?.trim()?.lowercase() ?: return null
        return when (text) {
            "single" -> "single"
            "range" -> "range"
            else -> null
        }
    }

    /**
     * Daftar pilihan yang toleran terhadap bentuk data legacy: string, angka,
     * atau objek `{text/label/value/...}`. Dipakai untuk `choices`/`options`;
     * entri yang tidak bisa menjadi teks dilewati.
     */
    fun choiceList(raw: Any?): List<String> {
        val list = raw as? List<*> ?: return emptyList()
        return list.mapNotNull { element ->
            when (element) {
                is String -> element.takeIf { it.isNotBlank() }
                is Number -> element.toString()
                is Map<*, *> -> objectText(element)
                else -> null
            }
        }
    }

    private fun objectText(map: Map<*, *>): String? {
        for (key in listOf("text", "label", "value", "content", "name", "option")) {
            val value = map[key] as? String
            if (!value.isNullOrBlank()) return value
        }
        return null
    }

    /**
     * Daftar pilihan dari konfigurasi soal — fix review lembar jawaban #2:
     * soal tanpa daftar valid HARUS dilewati, bukan dibuatkan opsi A–E /
     * 1-2-3 palsu yang tidak cocok dengan kunci jawaban server.
     * Entri non-string dibuang; item dengan koma di dalamnya utuh.
     */
    fun stringList(raw: Any?): List<String> {
        val list = raw as? List<*> ?: return emptyList()
        return list.filterIsInstance<String>()
    }
}
