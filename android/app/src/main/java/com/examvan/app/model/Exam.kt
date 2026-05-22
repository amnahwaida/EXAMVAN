package com.examvan.app.model

/**
 * Data class representing an exam from the API.
 */
data class Exam(
    val id: Int,
    val name: String,
    val status: String,
    val size_mb: Double,
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
 * API health check response.
 */
data class HealthResponse(
    val status: String,
    val version: String,
    val lan_mode: Boolean
)
