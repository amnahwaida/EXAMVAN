package com.examvan.app.helper

import android.text.InputType
import com.examvan.app.model.IdentityField

/**
 * Kebijakan form identitas (fix review form identitas & timer #2 & #3).
 *
 * #2 — Input field: teks satu baris + kapitalisasi kata. Dulu EditText
 *      dibuat tanpa inputType → newline bisa masuk ke nilai identitas.
 *
 * #3 — Dedupe kunci field dari konfigurasi server: dulu kedua kolom
 *      dirender tapi hanya isi kolom TERAKHIR yang dibaca saat konfirmasi.
 *      Kini kemunculan PERTAMA dipertahankan, duplikat dibuang.
 */
object IdentityFormPolicy {

    /** Teks satu baris + kapitalisasi kata + tanpa saran keyboard. */
    val INPUT_TYPE: Int =
        InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_FLAG_CAP_WORDS or
            InputType.TYPE_TEXT_FLAG_NO_SUGGESTIONS

    const val MAX_LINES = 1

    /** Sinkron dengan filter panjang yang selama ini dipakai (100). */
    const val MAX_LENGTH = 100

    /**
     * Buang field berkunci duplikat — kemunculan pertama menang, urutan
     * asli dipertahankan.
     */
    fun dedupeFields(fields: List<IdentityField>): List<IdentityField> {
        val seen = mutableSetOf<String>()
        return fields.filter { seen.add(it.key) }
    }
}
