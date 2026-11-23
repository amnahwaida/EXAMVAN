package com.examvan.app.helper

import com.examvan.app.model.Question
import com.google.gson.Gson
import com.google.gson.GsonBuilder
import com.google.gson.JsonDeserializationContext
import com.google.gson.JsonDeserializer
import com.google.gson.JsonElement
import com.google.gson.JsonObject
import com.google.gson.reflect.TypeToken

/**
 * Intake/egress boundary for the questions contract v1.
 *
 * Server -> app: legacy aliases are accepted here once, then converted to
 * [Question] so validation/rendering/scoring never sees two field names.
 * App -> prefs/Intent: [encodeForClient] always emits the canonical shape
 * (including the deprecated `options` mirror for older consumers).
 */
object QuestionContract {

    /** Shared with [Question.CONTRACT_VERSION]; keep both in sync. */
    const val SCHEMA_VERSION = Question.CONTRACT_VERSION

    val deserializer: JsonDeserializer<Question> = QuestionDeserializer

    private val gson: Gson = GsonBuilder()
        .registerTypeAdapter(Question::class.java, deserializer)
        .create()

    private val questionListType = object : TypeToken<List<Question>>() {}.type

    /** Decode a JSON array into normalized [Question] values. */
    fun decode(rawJson: String?): List<Question> {
        if (rawJson.isNullOrBlank()) return emptyList()
        return try {
            normalize(gson.fromJson<List<Question>>(rawJson, questionListType) ?: emptyList())
        } catch (_: Throwable) {
            emptyList()
        }
    }

    /**
     * Fill missing numbers with the same positional fallback as
     * AnswerSheetPlanner/server, canonicalize types, and trim list fields.
     * Idempotent.
     */
    fun normalize(questions: List<Question>): List<Question> {
        if (questions.isEmpty()) return emptyList()

        val explicitNumbers = questions.mapNotNull { it.number }.toSet()
        val seenNumbers = mutableSetOf<Int>()

        return questions.mapIndexed { index, question ->
            val number = question.number ?: run {
                var candidate = index + 1
                while (candidate in explicitNumbers || candidate in seenNumbers) candidate++
                candidate
            }
            seenNumbers.add(number)
            val normalizedType = QuestionParsing.normalizeType(question.type)
            val numbering = QuestionParsing.normalizeNumbering(question.numbering)
            val numberingSpan = if (numbering == "range") {
                when (normalizedType) {
                    "matching" -> question.leftItems?.count { it.isNotBlank() } ?: 0
                    "short_answer" -> question.statements?.count { it.isNotBlank() } ?: 0
                    else -> 0
                }.coerceAtLeast(1)
            } else {
                1
            }
            for (n in number + 1 until number + numberingSpan) seenNumbers.add(n)

            question.copy(
                number = number,
                type = normalizedType,
                weight = question.weight.takeIf { it > 0 } ?: 1.0,
                choices = question.choices.cleanList(),
                statements = question.statements.cleanList(),
                trueLabel = question.trueLabel.cleanText(),
                falseLabel = question.falseLabel.cleanText(),
                leftItems = question.leftItems.cleanList(),
                rightItems = question.rightItems.cleanList(),
                numbering = numbering,
                answerOrder = question.answerOrder?.trim()?.lowercase()?.takeIf { it == "any" }
            )
        }
    }

    /** Canonical JSON for prefs/Intent; includes `options` as a legacy mirror. */
    fun encodeForClient(questions: List<Question>): String {
        return gson.toJson(toMaps(questions))
    }

    /** Convert typed questions to the map shape still used by the UI planner. */
    fun toMaps(questions: List<Question>): List<Map<String, Any>> {
        return normalize(questions).mapNotNull { question ->
            val number = question.number ?: return@mapNotNull null
            val map = mutableMapOf<String, Any>(
                "number" to number,
                "type" to question.type,
                "weight" to question.weight,
                "partial_scoring" to question.partialScoring
            )
            question.label?.takeIf { it.isNotBlank() }?.let { map["label"] = it }
            question.choices?.takeIf { it.isNotEmpty() }?.let {
                map["choices"] = it
                map["options"] = it
            }
            question.statements?.takeIf { it.isNotEmpty() }?.let { map["statements"] = it }
            question.trueLabel?.takeIf { it.isNotBlank() }?.let { map["true_label"] = it }
            question.falseLabel?.takeIf { it.isNotBlank() }?.let { map["false_label"] = it }
            question.leftItems?.takeIf { it.isNotEmpty() }?.let { map["left_items"] = it }
            question.rightItems?.takeIf { it.isNotEmpty() }?.let { map["right_items"] = it }
            question.numbering?.let { map["numbering"] = it }
            question.answerOrder?.takeIf { it == "any" }?.let { map["answer_order"] = it }
            map
        }
    }

    private fun List<String>?.cleanList(): List<String>? =
        this?.map { it.trim() }?.filter { it.isNotEmpty() }?.takeIf { it.isNotEmpty() }

    private fun String?.cleanText(): String? =
        this?.trim()?.takeIf { it.isNotEmpty() }

    private object QuestionDeserializer : JsonDeserializer<Question> {
        override fun deserialize(
            json: JsonElement,
            typeOfT: java.lang.reflect.Type,
            context: JsonDeserializationContext
        ): Question {
            if (!json.isJsonObject) return Question()
            val obj = json.asJsonObject

            val type = QuestionParsing.normalizeType(obj.string("type"))
            val number = obj.intValue(obj.get("number"))
            val weight = obj.doubleValue(obj.get("weight"))?.takeIf { it > 0 } ?: 1.0
            val partialRaw = obj.member("partial_scoring")
            val partial = if (partialRaw == null) {
                QuestionParsing.supportsPartialScoring(type)
            } else {
                obj.boolValue(partialRaw)
            }
            val label = obj.string("label")?.trim()?.takeIf { it.isNotEmpty() }
            val choices = listOf("choices", "options", "pilihan", "pilihan_jawaban", "items")
                .firstNotNullOfOrNull { obj.stringList(it) }
            val statements = obj.stringList("statements")
            val trueLabel = obj.string("true_label")?.trim()?.takeIf { it.isNotEmpty() }
            val falseLabel = obj.string("false_label")?.trim()?.takeIf { it.isNotEmpty() }
            val leftItems = obj.stringList("left_items")
            val rightItems = obj.stringList("right_items")
            val numbering = QuestionParsing.normalizeNumbering(obj.string("numbering"))
            val answerOrder = obj.string("answer_order")?.trim()?.lowercase()?.takeIf { it == "any" }

            return Question(
                number = number,
                type = type,
                weight = weight,
                partialScoring = partial,
                label = label,
                choices = choices,
                statements = statements,
                trueLabel = trueLabel,
                falseLabel = falseLabel,
                leftItems = leftItems,
                rightItems = rightItems,
                numbering = numbering,
                answerOrder = answerOrder
            )
        }

        private fun JsonObject.member(name: String): JsonElement? {
            val value = get(name) ?: return null
            return value.takeUnless { it.isJsonNull }
        }

        private fun JsonObject.string(name: String): String? {
            val value = member(name) ?: return null
            if (!value.isJsonPrimitive) return null
            val primitive = value.asJsonPrimitive
            return if (primitive.isString || primitive.isNumber) primitive.asString else null
        }

        private fun JsonObject.intValue(value: JsonElement?): Int? {
            if (value == null || value.isJsonNull || !value.isJsonPrimitive) return null
            val primitive = value.asJsonPrimitive
            return when {
                primitive.isNumber -> primitive.asDouble.toInt()
                primitive.isString -> primitive.asString.trim().toIntOrNull()
                else -> null
            }
        }

        private fun JsonObject.doubleValue(value: JsonElement?): Double? {
            if (value == null || value.isJsonNull || !value.isJsonPrimitive) return null
            val primitive = value.asJsonPrimitive
            return when {
                primitive.isNumber -> primitive.asDouble
                primitive.isString -> primitive.asString.trim().toDoubleOrNull()
                else -> null
            }
        }

        private fun JsonObject.boolValue(value: JsonElement?): Boolean {
            if (value == null || value.isJsonNull || !value.isJsonPrimitive) return false
            val primitive = value.asJsonPrimitive
            return when {
                primitive.isBoolean -> primitive.asBoolean
                primitive.isNumber -> primitive.asDouble != 0.0
                primitive.isString -> primitive.asString.trim().lowercase() in
                    setOf("true", "1", "yes", "ya")
                else -> false
            }
        }

        private fun JsonObject.stringList(name: String): List<String>? {
            val value = member(name) ?: return null
            if (!value.isJsonArray) return null
            val out = mutableListOf<String>()
            for (element in value.asJsonArray) {
                textOf(element)?.let { out.add(it) }
            }
            return out.takeIf { it.isNotEmpty() }
        }

        private fun textOf(element: JsonElement): String? {
            if (element.isJsonNull) return null
            if (element.isJsonPrimitive) {
                val primitive = element.asJsonPrimitive
                val text = if (primitive.isString || primitive.isNumber) primitive.asString else ""
                return text.trim().takeIf { it.isNotEmpty() }
            }
            if (!element.isJsonObject) return null
            val obj = element.asJsonObject
            for (key in listOf("text", "label", "value", "content", "name", "option")) {
                val candidate = obj.string(key)?.trim()
                if (!candidate.isNullOrEmpty()) return candidate
            }
            return null
        }
    }
}
