package com.examvan.app.helper

/**
 * Batas aman data soal yang ikut Intent. Binder transaction dibatasi ~1MB
 * (UTF-16 = 2 byte/char), jadi 200k char (~400KB) memberi ruang untuk extras
 * lain. Di atas ini, prefs/re-fetch yang menjadi sumber.
 */
object ExamIntentPolicy {
    const val MAX_QUESTIONS_EXTRA_CHARS = 200_000

    fun canAttachQuestions(json: String?): Boolean =
        !json.isNullOrBlank() && json.length <= MAX_QUESTIONS_EXTRA_CHARS
}
