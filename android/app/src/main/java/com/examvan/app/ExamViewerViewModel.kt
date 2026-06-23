package com.examvan.app

import android.app.Application
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

/**
 * ViewModel for ExamViewerActivity.
 * Holds student answers and submission state, surviving configuration changes.
 * All answer values are stored as strings to avoid Gson type-loss on serialization.
 */
class ExamViewerViewModel(application: Application) : AndroidViewModel(application) {

    private val _studentAnswers = MutableStateFlow<Map<String, String>>(emptyMap())
    val studentAnswers: StateFlow<Map<String, String>> = _studentAnswers.asStateFlow()

    var currentPage: Int = 0

    private val _isSubmitting = MutableStateFlow(false)
    val isSubmitting: StateFlow<Boolean> = _isSubmitting.asStateFlow()

    private val _submittedOrExited = MutableStateFlow(false)
    val submittedOrExited: StateFlow<Boolean> = _submittedOrExited.asStateFlow()

    private val _isPdfReady = MutableStateFlow(false)
    val isPdfReady: StateFlow<Boolean> = _isPdfReady.asStateFlow()

    fun setStudentAnswers(answers: Map<String, String>) {
        _studentAnswers.value = answers.toMap()
    }

    fun updateAnswer(key: String, value: String) {
        val current = _studentAnswers.value.toMutableMap()
        current[key] = value
        _studentAnswers.value = current
    }

    fun removeAnswer(key: String) {
        val current = _studentAnswers.value.toMutableMap()
        current.remove(key)
        _studentAnswers.value = current
    }

    fun setSubmitting(submitting: Boolean) {
        _isSubmitting.value = submitting
    }

    fun setSubmittedOrExited(value: Boolean) {
        _submittedOrExited.value = value
    }

    fun setPdfReady(ready: Boolean) {
        _isPdfReady.value = ready
    }
}
