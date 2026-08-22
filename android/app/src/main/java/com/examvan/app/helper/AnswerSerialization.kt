package com.examvan.app.helper

import com.google.gson.Gson
import com.google.gson.reflect.TypeToken

/**
 * Serialisasi jawaban TERSTRUKTUR ke prefs (fix temuan review lembar jawaban
 * #1).
 *
 * LATAR: dulu jawaban disimpan sebagai `value.toString()` — multiple choice
 * menjadi "[A, B]" dan matching "{1=A}", lalu di-parsing balik dengan split
 * ", " / "=" saat restore. Teks pilihan yang mengandung koma atau '='
 * merusak pemulihan jawaban siswa.
 *
 * Sekarang: satu JSON object terstruktur (Gson) mempertahankan TIPE nilai —
 * String tetap String, List tetap List, Map tetap Map. Format lama (object
 * dengan value string semua) tetap terbaca karena bentuknya sama.
 */
object AnswerSerialization {

    private val type = object : TypeToken<Map<String, Any>>() {}.type
    private val gson = Gson()

    /** Serialize map jawaban (nilai: String / List / Map) menjadi JSON. */
    fun serialize(answers: Map<String, Any>): String = gson.toJson(answers)

    /**
     * Parse JSON jawaban menjadi map bertipe.
     * @return null bila JSON bukan object / rusak / kosong.
     */
    fun deserialize(json: String): Map<String, Any>? {
        if (json.isBlank()) return null
        return try {
            gson.fromJson<Map<String, Any>>(json, type)
        } catch (_: Exception) {
            null
        }
    }
}
