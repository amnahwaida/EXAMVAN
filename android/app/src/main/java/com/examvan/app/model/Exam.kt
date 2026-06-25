package com.examvan.app.model

/**
 * Represents a configurable identity field for student data entry.
 */
data class IdentityField(
    val key: String = "",
    val label: String = "",
    val required: Boolean = false
)

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
    val security_level: String? = null,
    val strict_mode: Boolean? = null,
    val identity_fields: List<IdentityField>? = null,
    val panel_color: String? = null,
    val start_time: String? = null,
    val end_time: String? = null,
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
    val required_app_version: String? = null,
    val lan_mode: Boolean,
    val timestamp: String? = null,
    val server_time_utc: String? = null,
    val certificate_fingerprint: String? = null
)
