package com.examvan.app.helper

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

/**
 * Mengunci temuan review: soal dengan `number` bertipe STRING di-skip
 * diam-diam oleh `(q["number"] as? Double)`. Server menyimpan
 * `Question.Number` sebagai `interface{}` — bisa JSON number ATAU string.
 * QuestionParsing harus menangani semua bentuk; hanya nilai non-numerik
 * yang menghasilkan null (soal dilewati dengan alasan sah).
 *
 *   ./gradlew :app:testStudentDebugUnitTest
 */
class QuestionParsingTest {

    @Test
    fun parsesJsonNumber_defaultGsonDouble() {
        // Gson mem-parse JSON number menjadi Double — bentuk paling umum.
        assertEquals(3, QuestionParsing.questionNumber(3.0))
        assertEquals(1, QuestionParsing.questionNumber(1.0))
    }

    @Test
    fun parsesIntegerDirectly() {
        assertEquals(7, QuestionParsing.questionNumber(7))
        assertEquals(0, QuestionParsing.questionNumber(0))
    }

    @Test
    fun parsesNumericString_withWhitespace() {
        // Akar temuan: nomor dikirim sebagai string ("3").
        assertEquals(3, QuestionParsing.questionNumber("3"))
        assertEquals(10, QuestionParsing.questionNumber(" 10 "))
        assertEquals(12, QuestionParsing.questionNumber("12"))
    }

    @Test
    fun rejectsNonNumericString() {
        assertNull(QuestionParsing.questionNumber("tiga"))
        assertNull(QuestionParsing.questionNumber(""))
        assertNull(QuestionParsing.questionNumber("3A"))
        assertNull(QuestionParsing.questionNumber("3.5"))
    }

    @Test
    fun rejectsNullAndNonNumericTypes() {
        assertNull(QuestionParsing.questionNumber(null))
        assertNull(QuestionParsing.questionNumber(true))
        assertNull(QuestionParsing.questionNumber(listOf(1, 2)))
        assertNull(QuestionParsing.questionNumber(emptyMap<String, Any>()))
    }

    @Test
    fun decimalDouble_truncates_likePreviousBehavior() {
        // 3.9 → 3 (perilaku toInt() lama dipertahankan).
        assertEquals(3, QuestionParsing.questionNumber(3.9))
    }

    // ── stringList: daftar pilihan dari konfigurasi soal ────────────────
    // (fix review lembar jawaban #2: soal tanpa pilihan valid harus
    // DILEWATI, bukan dibuatkan opsi A–E / 1-2-3 palsu.)

    @Test
    fun stringList_validListOfStrings_preserved() {
        assertEquals(
            listOf("A", "B", "C"),
            QuestionParsing.stringList(listOf("A", "B", "C"))
        )
    }

    @Test
    fun stringList_nullOrNonList_empty() {
        assertEquals(emptyList<String>(), QuestionParsing.stringList(null))
        assertEquals(emptyList<String>(), QuestionParsing.stringList("A,B"))
        assertEquals(emptyList<String>(), QuestionParsing.stringList(mapOf<String, Any>()))
    }

    @Test
    fun stringList_nonStringEntries_filtered() {
        // Konfigurasi rusak (angka di dalam pilihan) → entri dibuang;
        // bila hasil kosong, pemanggil melewatkan soal.
        assertEquals(
            listOf("A"),
            QuestionParsing.stringList(listOf("A", 1, true))
        )
    }

    @Test
    fun stringList_commaInsideItem_preserved() {
        // Teks pilihan mengandung koma — tidak boleh dipecah.
        assertEquals(
            listOf("Jakarta, Bandung"),
            QuestionParsing.stringList(listOf("Jakarta, Bandung"))
        )
    }
}
