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
 */
class ExamViewerViewModel(application: Application) : AndroidViewModel(application) {

    private val _studentAnswers = MutableStateFlow<Map<String, Any>>(emptyMap())
    val studentAnswers: StateFlow<Map<String, Any>> = _studentAnswers.asStateFlow()

    var currentPage: Int = 0

    @Volatile
    var isSubmitting: Boolean = false

    @Volatile
    var submittedOrExited: Boolean = false

    @Volatile
    var isPdfReady: Boolean = false

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
}
