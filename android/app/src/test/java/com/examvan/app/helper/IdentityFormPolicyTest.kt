package com.examvan.app.helper

import android.text.InputType
import com.examvan.app.model.IdentityField
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Mengunci kontrak form identitas (fix temuan review form identitas & timer
 * #2 dan #3).
 *
 * #2 — Input field: tanpa inputType, EditText default memperbolehkan
 *      newline → nama multi-baris ikut tersimpan ke JSON. Kontrak:
 *      teks biasa + kapitalisasi kata, TANPA multiline.
 *
 * #3 — Kunci field duplikat dari konfigurasi server: dulu kedua kolom
 *      dirender tapi hanya isi kolom TERAKHIR yang dibaca (isi kolom
 *      pertama hilang senyap). Kontrak: dedupe menjaga kemunculan PERTAMA,
 *      urutan dipertahankan.
 *
 *   ./gradlew :app:testStudentDebugUnitTest
 */
class IdentityFormPolicyTest {

    // ── Input flags (#2) ─────────────────────────────────────────────────

    @Test
    fun inputType_isClassText() {
        assertTrue(
            IdentityFormPolicy.INPUT_TYPE and InputType.TYPE_CLASS_TEXT != 0
        )
    }

    @Test
    fun inputType_capitalizesWords() {
        assertTrue(
            IdentityFormPolicy.INPUT_TYPE and InputType.TYPE_TEXT_FLAG_CAP_WORDS != 0
        )
    }

    @Test
    fun inputType_noMultilineFlag() {
        assertEquals(
            0,
            IdentityFormPolicy.INPUT_TYPE and InputType.TYPE_TEXT_FLAG_MULTI_LINE
        )
    }

    @Test
    fun inputType_noSuggestions_keyboardBersih() {
        // Fix review ronde 3 #1: saran keyboard tidak relevan untuk nama/
        // nomor ujian — samakan perlakuan dengan field token.
        assertTrue(
            IdentityFormPolicy.INPUT_TYPE and InputType.TYPE_TEXT_FLAG_NO_SUGGESTIONS != 0
        )
    }

    @Test
    fun maxLines_singleLine() {
        assertEquals(1, IdentityFormPolicy.MAX_LINES)
    }

    @Test
    fun maxLength_consistentWithExistingFilter() {
        // Filter panjang lama di ServerConfigActivity adalah 100 karakter.
        assertEquals(100, IdentityFormPolicy.MAX_LENGTH)
    }

    // ── Dedupe kunci field (#3) ──────────────────────────────────────────

    private fun f(key: String, label: String = key) = IdentityField(key, label, true)

    @Test
    fun dedupe_emptyList_unchanged() {
        assertEquals(emptyList<IdentityField>(), IdentityFormPolicy.dedupeFields(emptyList()))
    }

    @Test
    fun dedupe_uniqueKeys_unchangedAndInOrder() {
        val fields = listOf(f("student_name", "Nama"), f("exam_number", "Nomor"))
        assertEquals(fields, IdentityFormPolicy.dedupeFields(fields))
    }

    @Test
    fun dedupe_duplicateKeys_keepsFirstOccurrence() {
        val fields = listOf(
            f("student_name", "Nama Siswa"),
            f("exam_number"),
            f("student_name", "Nama Pendek") // duplikat — harus dibuang
        )
        val result = IdentityFormPolicy.dedupeFields(fields)

        assertEquals(2, result.size)
        assertEquals("Nama Siswa", result[0].label) // yang pertama menang
        assertEquals("exam_number", result[1].key)
    }

    // ── Ekstraksi field legacy (submit + auto-resume) ────────────────────

    @Test
    fun extractLegacy_standardKeys() {
        val json = JSONObject()
            .put("student_name", "Budi")
            .put("exam_number", "12")
            .put("student_class", "XII-A")

        val legacy = IdentityFormPolicy.extractLegacy(json)
        assertEquals("Budi", legacy.name)
        assertEquals("12", legacy.number)
        assertEquals("XII-A", legacy.studentClass)
    }

    @Test
    fun extractLegacy_customKeys() {
        val json = JSONObject()
            .put("nama_lengkap", "Siti")
            .put("nis", "99")
            .put("kelas", "XI-B")

        val legacy = IdentityFormPolicy.extractLegacy(json)
        assertEquals("Siti", legacy.name)
        assertEquals("99", legacy.number)
        assertEquals("XI-B", legacy.studentClass)
    }

    @Test
    fun extractLegacy_fallbackFirstAndSecondKey() {
        val json = JSONObject()
            .put("foo", "Nama Pertama")
            .put("bar", "Nomor Kedua")

        val legacy = IdentityFormPolicy.extractLegacy(json)
        assertEquals(setOf("Nama Pertama", "Nomor Kedua"), setOf(legacy.name, legacy.number))
        assertEquals("", legacy.studentClass)
    }

    @Test
    fun extractLegacy_emptyObject_returnsEmptyStrings() {
        val legacy = IdentityFormPolicy.extractLegacy(JSONObject())
        assertEquals("", legacy.name)
        assertEquals("", legacy.number)
        assertEquals("", legacy.studentClass)
    }
}
