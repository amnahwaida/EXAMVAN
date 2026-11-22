package com.examvan.app.helper

import android.text.InputType
import com.examvan.app.model.IdentityField
import org.json.JSONObject

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

    data class LegacyIdentity(
        val name: String,
        val number: String,
        val studentClass: String
    )

    /**
     * Ekstrak field identitas legacy (nama/nomor/kelas) dari identity_data
     * dengan fallback kunci kustom. Dipakai saat submit DAN auto-resume agar
     * dua jalur tidak pernah mengirim identitas yang berbeda.
     */
    fun extractLegacy(identityJson: JSONObject): LegacyIdentity {
        var name = identityJson.optString("student_name", "")
        if (name.isEmpty()) {
            for (key in identityJson.keys()) {
                val lowerKey = key.lowercase()
                if (lowerKey.contains("nama") || lowerKey.contains("name")) {
                    name = identityJson.optString(key, "")
                    if (name.isNotEmpty()) break
                }
            }
        }
        if (name.isEmpty() && identityJson.length() > 0) {
            val firstKey = identityJson.keys().next()
            name = identityJson.optString(firstKey, "")
        }

        var number = identityJson.optString("exam_number", "")
        if (number.isEmpty()) {
            for (key in identityJson.keys()) {
                val lowerKey = key.lowercase()
                if (lowerKey.contains("nomor") || lowerKey.contains("no") ||
                    lowerKey.contains("nis") || lowerKey.contains("number")
                ) {
                    number = identityJson.optString(key, "")
                    if (number.isNotEmpty()) break
                }
            }
        }
        if (number.isEmpty() && identityJson.length() > 1) {
            val keys = identityJson.keys()
            keys.next()
            if (keys.hasNext()) {
                number = identityJson.optString(keys.next(), "")
            }
        }

        var sClass = identityJson.optString("student_class", "")
        if (sClass.isEmpty()) {
            for (key in identityJson.keys()) {
                val lowerKey = key.lowercase()
                if (lowerKey.contains("kelas") || lowerKey.contains("class")) {
                    sClass = identityJson.optString(key, "")
                    if (sClass.isNotEmpty()) break
                }
            }
        }

        return LegacyIdentity(name, number, sClass)
    }
}
