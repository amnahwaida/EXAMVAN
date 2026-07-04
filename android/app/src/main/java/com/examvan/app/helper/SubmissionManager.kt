package com.examvan.app.helper

import android.Manifest
import android.app.NotificationChannel
import android.app.NotificationManager
import android.content.Context
import android.content.pm.PackageManager
import android.os.Build
import android.text.Html
import android.util.Log
import android.widget.Toast
import androidx.appcompat.app.AlertDialog
import androidx.core.app.ActivityCompat
import androidx.core.app.NotificationCompat
import androidx.core.content.ContextCompat
import androidx.lifecycle.LifecycleOwner
import androidx.lifecycle.lifecycleScope
import com.examvan.app.AppPrefs
import com.examvan.app.AuditLog
import com.examvan.app.R
import com.examvan.app.api.ApiClient
import com.examvan.app.databinding.ActivityExamViewerBinding
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.GlobalScope
import kotlinx.coroutines.NonCancellable
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/**
 * Manages exam answer submission lifecycle:
 * - Manual submit via button
 * - Auto-submit on user exit
 * - Retry with exponential backoff
 * - Notification on submit result
 * - Answer persistence/restore
 */
class SubmissionManager(
    private val context: Context,
    private val binding: ActivityExamViewerBinding,
    private val lifecycleOwner: LifecycleOwner
) {
    var isSubmitting: Boolean = false
    var submittedOrExited: Boolean = false
    var strictMode: Boolean = false
    private var lastSubmitSuccess: Boolean = false

    /** Total number of questions in the exam (set by Activity after building answer sheet). */
    var totalQuestions: Int = 0

    // Student data
    var examId: Int = -1
    var examName: String = ""
    var studentName: String = ""
    var studentNumber: String = ""
    var studentClass: String = ""
    var identityData: String? = null
    var token: String = ""
    var startTime: String = ""
    var macAddress: String = ""

    // Callback to get current answers
    var getAnswers: (() -> Map<String, Any>)? = null

    // Callback called when submit is successful
    var onSubmitSuccess: (() -> Unit)? = null

    // Callback for lock task deactivation
    var deactivateLockTask: (() -> Unit)? = null

    // Callback to check if dialog is showing
    var isShowingAppDialog: () -> Boolean = { false }
    var setShowingAppDialog: (Boolean) -> Unit = {}

    // Callback for finish
    var onFinish: (() -> Unit)? = null

    // Callback for isFinishing/isDestroyed check
    var isActivityFinishing: () -> Boolean = { false }

    private var autoSaveJob: kotlinx.coroutines.Job? = null
    private val gsonForSave = com.google.gson.Gson()

    private var notificationAvailable: Boolean = false

    companion object {
        private const val TAG = "SubmissionManager"
        private const val CHANNEL_ID = "examvan_auto_submit_v2"
        private const val REQUEST_NOTIFICATION_PERMISSION = 1001
    }

    fun initNotificationChannel() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            val notificationManager = context.getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
            val channel = NotificationChannel(
                CHANNEL_ID,
                context.getString(R.string.notification_channel_name),
                NotificationManager.IMPORTANCE_HIGH
            ).apply {
                description = context.getString(R.string.notification_channel_desc)
                enableLights(true)
                enableVibration(true)
            }
            notificationManager.createNotificationChannel(channel)
        }
        notificationAvailable = true
    }

    // ---- Answer persistence ----

    /**
     * Trigger auto-save with debounce (500ms).
     */
    fun triggerAutoSave(answers: Map<String, Any>) {
        if (submittedOrExited) return
        autoSaveJob?.cancel()
        autoSaveJob = (lifecycleOwner as? androidx.lifecycle.LifecycleOwner)?.lifecycleScope?.launch {
            delay(500)
            saveAnswersToPrefs(answers)
        }
    }

    private fun saveAnswersToPrefs(answers: Map<String, Any>) {
        try {
            val prefs = AppPrefs.getExamPrefs(context)
            val stringMap = answers.mapValues { it.value.toString() }
            val json = gsonForSave.toJson(stringMap)
            prefs.edit()
                .putString(AppPrefs.KEY_SAVED_ANSWERS, json)
                .putInt(AppPrefs.KEY_SAVED_ANSWERS_EXAM_ID, examId)
                .putLong(AppPrefs.KEY_SAVED_ANSWERS_TIMESTAMP, System.currentTimeMillis())
                .apply()
        } catch (e: Exception) {
            Log.w(TAG, "Failed to auto-save answers", e)
        }
    }

    fun restoreAnswersFromPrefs(): Map<String, String>? {
        try {
            val prefs = AppPrefs.getExamPrefs(context)
            val savedExamId = prefs.getInt(AppPrefs.KEY_SAVED_ANSWERS_EXAM_ID, -1)
            if (savedExamId != examId) {
                clearSavedAnswers()
                return null
            }
            val timestamp = prefs.getLong(AppPrefs.KEY_SAVED_ANSWERS_TIMESTAMP, 0L)
            val now = System.currentTimeMillis()
            if (now - timestamp > 24 * 60 * 60 * 1000L) {
                clearSavedAnswers()
                return null
            }
            val json = prefs.getString(AppPrefs.KEY_SAVED_ANSWERS, null) ?: return null
            return gsonForSave.fromJson(json, object : com.google.gson.reflect.TypeToken<Map<String, String>>() {}.type)
        } catch (e: Exception) {
            Log.w(TAG, "Failed to restore answers", e)
            return null
        }
    }

    fun clearSavedAnswers() {
        val submittedKey = AppPrefs.getSubmittedOrExitedKey(examId)
        AppPrefs.getExamPrefs(context).edit()
            .remove(AppPrefs.KEY_SAVED_ANSWERS)
            .remove(AppPrefs.KEY_SAVED_ANSWERS_EXAM_ID)
            .remove(AppPrefs.KEY_SAVED_ANSWERS_TIMESTAMP)
            .remove(AppPrefs.KEY_EXAM_START_TIME)
            .remove(submittedKey)
            .apply()
    }

    fun persistSubmittedState() {
        val submittedKey = AppPrefs.getSubmittedOrExitedKey(examId)
        AppPrefs.getExamPrefs(context).edit()
            .putBoolean(submittedKey, true).apply()
    }

    // ---- Submit ----

    fun confirmAndSubmit() {
        val answers = getAnswers?.invoke() ?: emptyMap()
        val answered = answers.size
        val total = totalQuestions.coerceAtLeast(answered) // fallback jika totalQuestions belum di-set

        val message = if (answered < total) {
            context.getString(R.string.submit_answers_confirm_partial, answered, total)
        } else {
            context.getString(R.string.submit_answers_confirm_all, total)
        }

        setShowingAppDialog(true)
        AlertDialog.Builder(context)
            .setTitle(context.getString(R.string.submit_answers_title))
            .setMessage(message)
            .setPositiveButton(context.getString(R.string.submit_confirm_yes)) { _, _ ->
                setShowingAppDialog(false)
                submitAnswers()
            }
            .setNegativeButton(context.getString(R.string.btn_cancel)) { _, _ ->
                setShowingAppDialog(false)
            }
            .setOnCancelListener { setShowingAppDialog(false) }
            .show()
    }

    private fun submitAnswers() {
        if (isSubmitting) return
        isSubmitting = true
        lastSubmitSuccess = false
        binding.btnSubmitAnswers.isEnabled = false
        binding.btnSubmitAnswers.text = context.getString(R.string.submitting)



        val answers = getAnswers?.invoke() ?: emptyMap()

        ApiClient.submitExam(
            examId = examId,
            token = token,
            studentName = studentName,
            examNumber = studentNumber,
            studentClass = studentClass,
            answers = answers,
            startTime = startTime,
            macAddress = macAddress,
            identityData = identityData,
            onSuccess = { message ->
                android.os.Handler(android.os.Looper.getMainLooper()).post {
                    isSubmitting = false
                    lastSubmitSuccess = true
                    submittedOrExited = true
                    clearSavedAnswers()
                    onSubmitSuccess?.invoke()
                    AuditLog.i(AuditLog.Events.SUBMIT_SUCCESS, "examId=$examId")
                    deactivateLockTask?.invoke()

                    if (isActivityFinishing()) {
                        showAutoSubmitNotification(
                            context.getString(R.string.auto_submit_success_title),
                            context.getString(R.string.toast_auto_submit_success)
                        )
                        return@post
                    }

                    val sb = StringBuilder(message)
                    if (studentName.isNotEmpty()) {
                        sb.append("\n\n").append(context.getString(R.string.label_student_name)).append(": ").append(studentName)
                    }
                    if (studentNumber.isNotEmpty()) {
                        sb.append("\n").append(context.getString(R.string.label_exam_number)).append(": ").append(studentNumber)
                    }
                    if (studentClass.isNotEmpty()) {
                        sb.append("\n").append(context.getString(R.string.label_student_class)).append(": ").append(studentClass)
                    }
                    val msgText = sb.toString()

                    binding.btnSubmitAnswers.isEnabled = false
                    binding.btnSubmitAnswers.text = context.getString(R.string.submitted_label)

                    setShowingAppDialog(true)
                    AlertDialog.Builder(context)
                        .setTitle(context.getString(R.string.submit_success_title))
                        .setMessage(context.getString(R.string.submit_success_message, msgText))
                        .setCancelable(false)
                        .setPositiveButton(context.getString(R.string.submit_success_done)) { _, _ ->
                            setShowingAppDialog(false)
                            onFinish?.invoke()
                        }
                        .show()
                }
            },
            onError = { errorMsg ->
                android.os.Handler(android.os.Looper.getMainLooper()).post {
                    isSubmitting = false
                    lastSubmitSuccess = false
                    submittedOrExited = false
                    if (isActivityFinishing()) {
                        showAutoSubmitNotification(
                            context.getString(R.string.auto_submit_failed_title),
                            context.getString(R.string.toast_auto_submit_failed, errorMsg)
                        )
                        return@post
                    }

                    binding.btnSubmitAnswers.isEnabled = true
                    binding.btnSubmitAnswers.text = context.getString(R.string.submit_failed_retry)

                    val dialogTitle = if (strictMode) context.getString(R.string.submit_failed_title_strict) else context.getString(R.string.submit_failed_title)
                    val dialogMsg = if (strictMode) context.getString(R.string.submit_failed_message_strict, errorMsg) else context.getString(R.string.submit_failed_message, errorMsg)

                    setShowingAppDialog(true)
                    AlertDialog.Builder(context)
                        .setTitle(dialogTitle)
                        .setMessage(dialogMsg)
                        .setPositiveButton(context.getString(R.string.dialog_ok)) { _, _ ->
                            setShowingAppDialog(false)
                        }
                        .setOnCancelListener { setShowingAppDialog(false) }
                        .show()
                    AuditLog.e(AuditLog.Events.SUBMIT_FAILED, "examId=$examId error=${errorMsg.take(80)}")
                }
            }
        )
    }

    // ---- Auto-submit ----

    suspend fun submitWithRetry(): Pair<Boolean, String> {
        val delays = listOf(1000L, 2000L, 4000L)
        for (attempt in 0..3) {
            try {
                val answers = getAnswers?.invoke() ?: emptyMap()
                val result = withContext(Dispatchers.IO) {
                    ApiClient.submitExamSync(
                        examId = examId,
                        token = token,
                        studentName = studentName,
                        examNumber = studentNumber,
                        studentClass = studentClass,
                        answers = answers,
                        startTime = startTime,
                        macAddress = macAddress,
                        identityData = identityData
                    )
                }
                if (result.first) return result
                if (attempt < 3) delay(delays[attempt])
            } catch (e: Exception) {
                if (attempt < 3) {
                    delay(delays[attempt])
                } else {
                    return Pair(false, e.message ?: context.getString(R.string.answer_submit_error))
                }
            }
        }
        return Pair(false, context.getString(R.string.answer_submit_error))
    }

    fun autoSubmitAndExit() {
        if (submittedOrExited) return
        submittedOrExited = true

        AuditLog.i(AuditLog.Events.AUTO_SUBMIT, "strict=$strictMode answers=${getAnswers?.invoke()?.size}")

        // Immediately persist submitted/exited state in SharedPreferences before starting network request.
        // This prevents the student from re-entering the exam if they exit via network disconnection bypass.
        onSubmitSuccess?.invoke()

        // Use GlobalScope + NonCancellable so submit completes even if
        // activity is destroyed (process death, user swipe-away).
        if (isSubmitting) {
            GlobalScope.launch(NonCancellable + Dispatchers.Main) {
                val timeoutTime = System.currentTimeMillis() + 10000
                while (isSubmitting && System.currentTimeMillis() < timeoutTime) {
                    delay(100)
                }
                deactivateLockTask?.invoke()
                if (!isActivityFinishing()) onFinish?.invoke()
            }
            return
        }

        GlobalScope.launch(NonCancellable + Dispatchers.IO) {
            val result = submitWithRetry()

            // Always clear local answers and deactivate lock task when exiting
            clearSavedAnswers()
            deactivateLockTask?.invoke()

            val notifTitle = if (result.first) {
                context.getString(R.string.auto_submit_success_title)
            } else {
                context.getString(R.string.auto_submit_failed_title)
            }
            val notifMessage = if (result.first) {
                context.getString(R.string.toast_auto_submit_success)
            } else {
                context.getString(R.string.toast_auto_submit_failed, result.second)
            }

            withContext(Dispatchers.Main) {
                showAutoSubmitNotification(notifTitle, notifMessage)
                delay(400)
                if (!isActivityFinishing()) onFinish?.invoke()
            }
        }
    }

    private fun showAutoSubmitNotification(title: String, message: String) {
        try {
            val notificationManager = context.getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
            notificationAvailable

            val notification = NotificationCompat.Builder(context, CHANNEL_ID)
                .setSmallIcon(R.mipmap.ic_launcher)
                .setContentTitle(title)
                .setContentText(message)
                .setStyle(NotificationCompat.BigTextStyle().bigText(message))
                .setPriority(NotificationCompat.PRIORITY_HIGH)
                .setDefaults(NotificationCompat.DEFAULT_ALL)
                .setAutoCancel(true)
                .build()

            val uniqueNotifId = (System.currentTimeMillis() % 100000).toInt()
            notificationManager.notify(uniqueNotifId, notification)
        } catch (_: Throwable) {
            if (context is android.app.Activity && !context.isFinishing) {
                Toast.makeText(context, message, Toast.LENGTH_LONG).show()
            }
        }
    }

    // ---- Notification permission ----

    fun requestNotificationPermission() {
        if (Build.VERSION.SDK_INT < 33) return
        if (ContextCompat.checkSelfPermission(context, Manifest.permission.POST_NOTIFICATIONS)
            == PackageManager.PERMISSION_GRANTED) return

        if (context is ActivityCompat.OnRequestPermissionsResultCallback) {
            if (ActivityCompat.shouldShowRequestPermissionRationale(context as android.app.Activity, Manifest.permission.POST_NOTIFICATIONS)) {
                setShowingAppDialog(true)
                AlertDialog.Builder(context)
                    .setTitle("Izin Notifikasi")
                    .setMessage("Notifikasi digunakan hanya untuk memberi tahu status pengumpulan ujian. " +
                            "Tidak ada notifikasi iklan atau promosi.")
                    .setPositiveButton("Izinkan") { _, _ ->
                        setShowingAppDialog(false)
                        ActivityCompat.requestPermissions(
                            context as android.app.Activity,
                            arrayOf(Manifest.permission.POST_NOTIFICATIONS),
                            REQUEST_NOTIFICATION_PERMISSION
                        )
                    }
                    .setNegativeButton("Jangan Izinkan") { _, _ ->
                        setShowingAppDialog(false)
                    }
                    .setOnCancelListener { setShowingAppDialog(false) }
                    .show()
            } else {
                ActivityCompat.requestPermissions(
                    context as android.app.Activity,
                    arrayOf(Manifest.permission.POST_NOTIFICATIONS),
                    REQUEST_NOTIFICATION_PERMISSION
                )
            }
        }
    }
}
