package com.examvan.app.helper

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

/**
 * Mengunci kontrak serialisasi jawaban TERSTRUKTUR (fix temuan review lembar
 * jawaban #1: "jawaban ke prefs memakai value.toString() — multiple choice
 * '[A, B]' dan matching '{1=A}' di-parsing balik dengan split ', ' / '=';
 * pilihan yang mengandung koma atau '=' merusak restore saat re-entry").
 *
 * Kontrak:
 *  - Round-trip mempertahankan TIPE nilai: String tetap String,
 *    List<String> tetap List, Map<String,String> tetap Map — teks yang
 *    mengandung ", " atau "=" tidak lagi berbahaya.
 *  - Format LAMA (JSON object berisi string semua) tetap bisa dibaca
 *    (backward compat dengan data tersimpan versi sebelumnya).
 *  - JSON rusak → null, bukan crash.
 *
 *   ./gradlew :app:testStudentDebugUnitTest
 */
class AnswerSerializationTest {

    @Test
    fun roundTrip_stringValue() {
        val answers = mapOf<String, Any>("1" to "A")
        assertEquals(answers, AnswerSerialization.deserialize(AnswerSerialization.serialize(answers)))
    }

    @Test
    fun roundTrip_multipleChoice_survivesCommasInChoiceText() {
        // Kasus yang DULU rusak: "[Jakarta, Bandung]" di-split koma.
        val answers = mapOf<String, Any>(
            "2" to listOf("Jakarta, Bandung", "Medan")
        )
        val restored = AnswerSerialization.deserialize(AnswerSerialization.serialize(answers))

        val list = restored?.get("2") as? List<*>
        assertEquals(listOf("Jakarta, Bandung", "Medan"), list)
    }

    @Test
    fun roundTrip_matching_survivesEqualsAndCommaInItems() {
        // Kasus yang DULU rusak: "{A=B}" di-split '=' dan ', '.
        val answers = mapOf<String, Any>(
            "3" to mapOf("1" to "A=B", "2, 3" to "C")
        )
        val restored = AnswerSerialization.deserialize(AnswerSerialization.serialize(answers))

        @Suppress("UNCHECKED_CAST")
        val matching = restored?.get("3") as? Map<String, String>
        assertEquals(mapOf("1" to "A=B", "2, 3" to "C"), matching)
    }

    @Test
    fun legacyFormat_allStringValues_stillReadable() {
        // Format lama: Gson dari Map<String,String> — bentuknya JSON object
        // dengan value string. Harus tetap terbaca (data tersimpan lama).
        val legacyJson = """{"1":"A","4":"TRUE"}"""
        val restored = AnswerSerialization.deserialize(legacyJson)

        assertEquals("A", restored?.get("1"))
        assertEquals("TRUE", restored?.get("4"))
    }

    @Test
    fun malformedJson_returnsNull() {
        assertNull(AnswerSerialization.deserialize("bukan json"))
        assertNull(AnswerSerialization.deserialize("[1,2,3]")) // array bukan object
        assertNull(AnswerSerialization.deserialize(""))
    }

    @Test
    fun serialize_emptyMap_validEmptyObject() {
        val json = AnswerSerialization.serialize(emptyMap())
        assertEquals(emptyMap<String, Any>(), AnswerSerialization.deserialize(json))
    }
}
