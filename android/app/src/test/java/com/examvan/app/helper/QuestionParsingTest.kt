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
}
