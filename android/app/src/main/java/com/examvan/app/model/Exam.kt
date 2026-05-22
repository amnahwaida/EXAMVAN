package com.examvan.app.model

/**
 * Data class representing an exam from the API.
 */
data class Exam(
    val id: Int,
    val name: String,
    val status: String,
    val size_mb: Double,
    val token: String? = null,
    val questions: List<Map<String, Any>>? = null,
    val created_at: String
)

/**
 * API response wrapper for exam list.
 */
data class ExamListResponse(
    val success: Boolean,
    val data: List<Exam>
)

/**
 * API response for single exam by token.
 */
data class TokenExamResponse(
    val success: Boolean,
    val data: Exam? = null,
    val error: String? = null,
    val message: String? = null
)

/**
 * API health check response.
 */
data class HealthResponse(
    val status: String,
    val version: String,
    val lan_mode: Boolean,
    val timestamp: String? = null,
    val server_time_utc: String? = null
)
