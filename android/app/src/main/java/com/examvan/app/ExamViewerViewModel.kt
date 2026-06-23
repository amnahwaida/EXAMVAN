package com.examvan.app

import android.app.Application
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

/**
 * ViewModel for ExamViewerActivity.
 * Holds student answers and submission state, surviving configuration changes (rotation).
 * Student answers are kept as StateFlow to survive rotation.
 * Process-death survival for currentPage is handled via onSaveInstanceState in the Activity.
 */
class ExamViewerViewModel(application: Application) : AndroidViewModel(application) {

    // Student answers as StateFlow — survives configuration changes (rotation)
    private val _studentAnswers = MutableStateFlow<Map<String, Any>>(emptyMap())
    val studentAnswers: StateFlow<Map<String, Any>> = _studentAnswers.asStateFlow()

    private val _isSubmitting = MutableStateFlow(false)
    val isSubmitting: StateFlow<Boolean> = _isSubmitting.asStateFlow()

    private val _submittedOrExited = MutableStateFlow(false)
    val submittedOrExited: StateFlow<Boolean> = _submittedOrExited.asStateFlow()

    private val _isPdfReady = MutableStateFlow(false)
    val isPdfReady: StateFlow<Boolean> = _isPdfReady.asStateFlow()

    fun setStudentAnswers(answers: Map<String, Any>) {
        _studentAnswers.value = answers.toMap()
    }

    fun updateAnswer(key: String, value: Any) {
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
