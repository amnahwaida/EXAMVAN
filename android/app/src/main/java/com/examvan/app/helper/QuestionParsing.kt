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
