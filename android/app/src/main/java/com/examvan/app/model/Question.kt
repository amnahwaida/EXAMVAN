package com.examvan.app.model

import com.google.gson.annotations.SerializedName

/**
 * Canonical questions contract v1 as consumed by Android.
 *
 * The server emits this shape at the API boundary. Legacy aliases (`options`,
 * `pg`, `essay`, string/invalid numbers, object choices) are accepted only by
 * the intake parser and never leak further into the app.
 */
data class Question(
    val number: Int? = null,
    val type: String = "",
    val weight: Double = 1.0,
    @SerializedName("partial_scoring")
    val partialScoring: Boolean = false,
    val label: String? = null,
    val choices: List<String>? = null,
    val statements: List<String>? = null,
    @SerializedName("true_label")
    val trueLabel: String? = null,
    @SerializedName("false_label")
    val falseLabel: String? = null,
    @SerializedName("left_items")
    val leftItems: List<String>? = null,
    @SerializedName("right_items")
    val rightItems: List<String>? = null,
    /**
     * Matching/short-answer: "single" = whole card is one question number;
     * "range" = each field/item consumes a number (N..N+count-1).
     */
    val numbering: String? = null,
    /** short-answer: "any" = answer order is free; null/absent = ordered. */
    @SerializedName("answer_order")
    val answerOrder: String? = null
) {
    companion object {
        const val CONTRACT_VERSION = 1
    }
}
