package com.examvan.app.helper

import com.examvan.app.model.Question
import com.google.gson.JsonParser
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Contract v1 intake/egress. The legacy/canonical fixtures are the SAME files
 * used by the Go contract test (contracts/questions_v1_*.json), so server and
 * Android cannot drift without one side failing.
 */
class QuestionContractTest {

    private fun fixture(name: String): String {
        val stream = javaClass.classLoader!!.getResourceAsStream(name)
        assertNotNull("Fixture $name tidak ada di test resources", stream)
        return stream!!.bufferedReader().use { it.readText() }
    }

    @Test
    fun legacyFixture_normalizesToCanonicalQuestions() {
        val legacy = QuestionContract.decode(fixture("questions_v1_legacy.json"))
        val canonical = QuestionContract.decode(fixture("questions_v1_canonical.json"))

        assertEquals(canonical, legacy)
        assertEquals(listOf(1, 2, 3, 4), legacy.map { it.number })
        assertEquals("single_choice", legacy[0].type)
        assertEquals(listOf("Jakarta", "Bandung"), legacy[0].choices)
        assertEquals("short_answer", legacy[1].type)
        assertEquals("true_false", legacy[2].type)
        assertEquals(listOf("Satu", "Dua"), legacy[2].statements)
        assertEquals("Sesuai", legacy[2].trueLabel)
        assertEquals("Tidak Sesuai", legacy[2].falseLabel)
        assertEquals(listOf("Ya", "Tidak"), legacy[3].choices)
    }

    @Test
    fun unknownType_isPreservedForVisibleSkipNotSilentLoss() {
        val decoded = QuestionContract.decode(
            """[{"number":1,"type":"file_upload"}]"""
        )
        assertEquals("file_upload", decoded.single().type)
    }

    @Test
    fun numberFallback_matchesServerAndAvoidsExplicitCollision() {
        val decoded = QuestionContract.decode(
            """[
                {"number":"rusak","type":"short_answer"},
                {"number":1,"type":"short_answer"},
                {"type":"short_answer"}
            ]"""
        )
        assertEquals(listOf(2, 1, 3), decoded.map { it.number })
    }

    @Test
    fun numberParsing_matchesGoRule() {
        val decoded = QuestionContract.decode(
            """[
                {"number":3.9,"type":"short_answer"},
                {"number":" 10 ","type":"short_answer"},
                {"number":"3.5","type":"short_answer"},
                {"number":"3A","type":"short_answer"}
            ]"""
        )
        assertEquals(listOf(3, 10, 4, 5), decoded.map { it.number })
    }

    @Test
    fun missingPartialScoring_defaultsTrueForEligibleTypes() {
        val decoded = QuestionContract.decode(
            """[
                {"number":1,"type":"multiple_choice","choices":["A","B"]},
                {"number":2,"type":"true_false","statements":["Satu"],"key":["TRUE"]},
                {"number":3,"type":"matching","left_items":["Satu"],"right_items":["A"]},
                {"number":4,"type":"single_choice","choices":["A","B"]},
                {"number":5,"type":"multiple_choice","partial_scoring":false,"choices":["A","B"]},
                {"number":6,"type":"short_answer","key":"Jakarta"}
            ]"""
        )
        assertEquals(listOf(true, true, true, false, false, true), decoded.map { it.partialScoring })
    }

    @Test
    fun shortAnswerMulti_carriesStatementsNumberingAndPartialDefault() {
        val decoded = QuestionContract.decode(
            """[{"number":1,"type":"short_answer","numbering":"range",
                "statements":[" Ibu kota ","Hasil"],"key":["Jakarta","7"],
                "answer_order":"ANY"}]"""
        )
        val q = decoded.single()
        assertEquals(listOf("Ibu kota", "Hasil"), q.statements)
        assertEquals("range", q.numbering)
        assertEquals("any", q.answerOrder)
        assertEquals(true, q.partialScoring)

        val encoded = QuestionContract.encodeForClient(decoded)
        val obj = JsonParser.parseString(encoded).asJsonArray.single().asJsonObject
        assertEquals("any", obj["answer_order"].asString)
    }

    @Test
    fun trueFalseLabels_areParsedAndEncoded() {
        val decoded = QuestionContract.decode(
            """[{"number":1,"type":"true_false","true_label":" Sesuai ",
                "false_label":"Tidak Sesuai"}]"""
        )
        assertEquals("Sesuai", decoded.single().trueLabel)
        assertEquals("Tidak Sesuai", decoded.single().falseLabel)

        val encoded = QuestionContract.encodeForClient(decoded)
        val obj = JsonParser.parseString(encoded).asJsonArray.single().asJsonObject
        assertEquals("Sesuai", obj["true_label"].asString)
        assertEquals("Tidak Sesuai", obj["false_label"].asString)
    }

    @Test
    fun numericLegacyPartialScoring_matchesServerTruthiness() {
        val decoded = QuestionContract.decode(
            """[
                {"number":1,"type":"multiple_choice","partial_scoring":1,"choices":["A","B"]},
                {"number":2,"type":"multiple_choice","partial_scoring":0,"choices":["A","B"]}
            ]"""
        )
        assertEquals(true, decoded[0].partialScoring)
        assertEquals(false, decoded[1].partialScoring)
    }

    @Test
    fun matchingNumbering_isParsedAndEncoded() {
        val decoded = QuestionContract.decode(
            """[{"number":1,"type":"matching","left_items":["Satu","Dua"],
                "right_items":["A","B"],"numbering":"RANGE"}]"""
        )
        assertEquals("range", decoded.single().numbering)

        val encoded = QuestionContract.encodeForClient(decoded)
        val obj = JsonParser.parseString(encoded).asJsonArray.single().asJsonObject
        assertEquals("range", obj["numbering"].asString)
    }

    @Test
    fun normalize_rangeNumbering_reservesInnerNumbersForFallback() {
        val normalized = QuestionContract.decode(
            """[{"type":"matching","numbering":"range","left_items":["Satu","Dua"],
                "right_items":["A","B"]},{"type":"short_answer"}]"""
        )
        assertEquals(listOf(1, 3), normalized.map { it.number })
    }

    @Test
    fun normalize_shortAnswerRange_reservesOneNumberPerField() {
        val normalized = QuestionContract.decode(
            """[{"number":1,"type":"short_answer","numbering":"range",
                "statements":["Satu","Dua","Tiga"]},{"type":"short_answer"}]"""
        )
        assertEquals(listOf(1, 4), normalized.map { it.number })
    }

    @Test
    fun unknownMatchingNumbering_dropsToNull() {
        val decoded = QuestionContract.decode(
            """[{"number":1,"type":"matching","left_items":["Satu"],
                "right_items":["A"],"numbering":"rentang"}]"""
        )
        assertEquals(null, decoded.single().numbering)
    }

    @Test
    fun encodeForClient_emitsCanonicalTypeAndLegacyOptionsMirror() {
        val encoded = QuestionContract.encodeForClient(
            listOf(
                Question(
                    number = 1,
                    type = "pg",
                    choices = listOf("A", "B")
                )
            )
        )
        val obj = JsonParser.parseString(encoded).asJsonArray.single().asJsonObject
        assertEquals(1, obj["number"].asInt)
        assertEquals("single_choice", obj["type"].asString)
        assertEquals("A", obj["choices"].asJsonArray[0].asString)
        assertEquals("A", obj["options"].asJsonArray[0].asString)
        assertTrue(obj["partial_scoring"].asBoolean.not())
        assertEquals(1.0, obj["weight"].asDouble, 0.0001)
    }
}
