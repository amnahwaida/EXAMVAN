package com.examvan.app.helper

import android.Manifest
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
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
import com.examvan.app.CongratulationsActivity
import com.examvan.app.ExamViewerActivity
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
    var serverUrl: String = ""

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
            val prefs = AppPrefs.getExamPrefsSafe(context)
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
            val prefs = AppPrefs.getExamPrefsSafe(context)
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
        // CATATAN: flag submittedOrExited TIDAK dihapus di sini. Sebelumnya
        // dihapus → setelah submit SUKSES, re-entry mengira ujian belum selesai
        // → watchdog deadline (sudah lewat) langsung auto-submit KOSONG dalam
        // window grace server (end_time + 60 dtk) → menimpa jawaban asli yang
        // sudah terkirim. Flag di-persist terpisah (persistSubmittedState) dan
        // dipertahankan sebagai penanda selesai: re-entry menampilkan layar
        // "Ujian Sudah Selesai", bukan menjalankan ulang alur ujian.
        AppPrefs.getExamPrefsSafe(context).edit()
            .remove(AppPrefs.KEY_SAVED_ANSWERS)
            .remove(AppPrefs.KEY_SAVED_ANSWERS_EXAM_ID)
            .remove(AppPrefs.KEY_SAVED_ANSWERS_TIMESTAMP)
            .remove(AppPrefs.KEY_EXAM_START_TIME)
            .apply()
    }

    companion object {
        private const val TAG = "SubmissionManager"
        private const val CHANNEL_ID = "examvan_auto_submit_v2"
        private const val REQUEST_NOTIFICATION_PERMISSION = 1001

        /**
         * True bila jawaban untuk [examId] masih tersimpan di prefs — artinya
         * submit sebelumnya TIDAK tuntas (gagal / proses mati sebelum sukses),
         * karena clearSavedAnswers hanya berjalan saat submit sukses. Dipakai
         * onCreate untuk memilih layar recovery vs layar "sudah selesai".
         * Aturan kedaluwarsa sama dengan restoreAnswersFromPrefs (24 jam).
         */
        fun hasPendingAnswers(context: Context, examId: Int): Boolean {
            val prefs = AppPrefs.getExamPrefsSafe(context)
            if (prefs.getInt(AppPrefs.KEY_SAVED_ANSWERS_EXAM_ID, -1) != examId) return false
            val timestamp = prefs.getLong(AppPrefs.KEY_SAVED_ANSWERS_TIMESTAMP, 0L)
            if (System.currentTimeMillis() - timestamp > 24 * 60 * 60 * 1000L) return false
            return !prefs.getString(AppPrefs.KEY_SAVED_ANSWERS, null).isNullOrEmpty()
        }
    }

    fun persistSubmittedState() {
        val submittedKey = AppPrefs.getSubmittedOrExitedKey(examId)
        AppPrefs.getExamPrefsSafe(context).edit()
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
        // Gate tunggal (manual + auto) — cegah double POST saat deadline
        // berbarengan dengan submit manual (lihat SubmitFlowPolicy).
        if (!SubmitFlowPolicy.canStartSubmission(isSubmitting, submittedOrExited)) return
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
            onSuccess = { result ->
                android.os.Handler(android.os.Looper.getMainLooper()).post {
                    isSubmitting = false
                    lastSubmitSuccess = true
                    submittedOrExited = true
                    onSubmitSuccess?.invoke()
                    AuditLog.i(AuditLog.Events.SUBMIT_SUCCESS, "examId=$examId")
                    deactivateLockTask?.invoke()

                    if (isActivityFinishing()) {
                        showAutoSubmitNotification(
                            context.getString(R.string.auto_submit_success_title),
                            context.getString(R.string.toast_auto_submit_success),
                            isSuccess = true
                        )
                        return@post
                    }

                    binding.btnSubmitAnswers.isEnabled = false
                    binding.btnSubmitAnswers.text = context.getString(R.string.submitted_label)

                    if (result.status == ApiClient.SUBMIT_STATUS_QUEUED) {
                        // Async path (202): the server only QUEUED the answers —
                        // they are not durable yet. Keep the local copy until the
                        // worker confirms (poll /result); only then clear the saved
                        // sheet. Clearing early would lose the answers permanently
                        // if the queued job failed (revocation of the approval row
                        // also only happens AFTER the worker's commit).
                        waitForQueuedResult(result.jobId) { confirmed ->
                            if (confirmed) clearSavedAnswers()
                            showCongrats(result)
                        }
                        return@post
                    }

                    clearSavedAnswers()
                    showCongrats(result)
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
                            context.getString(R.string.toast_auto_submit_failed, errorMsg),
                            isSuccess = false
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

    // ---- Post-submit confirmation (async 202 path) ----

    /**
     * Poll GET /result after a queued submit until the worker confirms the
     * answers are durable ("done"). Returns true only on "done" — the caller
     * keeps the local answer copy otherwise, so a failed queued job can still
     * be recovered via re-entry and an idempotent resubmit.
     */
    private fun waitForQueuedResult(
        jobId: String?,
        onComplete: (confirmed: Boolean) -> Unit
    ) {
        // Deadline & timeout per-poll ditangani QueuedResultPolling (teguh
        // terhadap respons /result yang menggantung) — lihat helper tsb.
        GlobalScope.launch(NonCancellable + Dispatchers.IO) {
            val confirmed = QueuedResultPolling.awaitDurable(
                examId = examId,
                token = token,
                macAddress = macAddress,
                identityData = identityData,
                jobId = jobId,
                deadlineMs = QueuedResultPolling.POLL_DEADLINE_MS,
                maxAttempts = QueuedResultPolling.POLL_MAX_ATTEMPTS
            )
            android.os.Handler(android.os.Looper.getMainLooper()).post {
                onComplete(confirmed)
            }
        }
    }

    /**
     * Show the congratulations screen (or fallback dialog) after a successful
     * submit. Extracted so both the sync path and the post-poll async path use
     * the same UI. Internal: dipakai juga oleh jalur recovery re-entry.
     */
    internal fun showCongrats(result: ApiClient.SubmitResult) {
        setShowingAppDialog(true)
        try {
            val intent = Intent(context, CongratulationsActivity::class.java).apply {
                putExtra("server_url", serverUrl)
                putExtra("exam_token", token)
                putExtra("exam_name", examName)
                putExtra("student_name", studentName)
                putExtra("student_number", studentNumber)
                putExtra("student_class", studentClass)
                putExtra("congrats_message", result.congratsMessage)
                addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
            }
            context.startActivity(intent)
        } catch (_: Exception) {
            // Fallback if launching fails: plain dialog as before.
            AlertDialog.Builder(context)
                .setTitle(context.getString(R.string.submit_success_title))
                .setMessage(context.getString(R.string.submit_success_message, result.message))
                .setCancelable(false)
                .setPositiveButton(context.getString(R.string.submit_success_done)) { _, _ ->
                    setShowingAppDialog(false)
                    onFinish?.invoke()
                }
                .show()
            return
        }
        setShowingAppDialog(false)
        onFinish?.invoke()
    }

    // ---- Auto-submit ----

    /**
     * Submit dengan retry (1+3 percobaan, backoff 1/2/4 dtk).
     *
     * @param answersOverride jawaban pengganti — dipakai jalur recovery
     *        (resubmitPendingAnswers) yang mengirim dari prefs; default
     *        memakai jawaban dari getAnswers (alur normal / auto-submit).
     * @return SubmitResult — sukses hanya bila server menerima (sync) ATAU
     *         worker mengkonfirmasi durable via polling /result (async 202).
     */
    suspend fun submitWithRetry(answersOverride: Map<String, Any>? = null): ApiClient.SubmitResult {
        val delays = listOf(1000L, 2000L, 4000L)
        var last = ApiClient.SubmitResult(
            false, context.getString(R.string.answer_submit_error), null
        )
        for (attempt in 0..3) {
            try {
                val answers = answersOverride ?: (getAnswers?.invoke() ?: emptyMap())
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
                if (result.success) {
                    if (result.status != ApiClient.SUBMIT_STATUS_QUEUED) {
                        return result
                    }
                    // Async path: the server only queued the answers. Wait for
                    // the worker to make them durable before clearing the local
                    // copy (clearSavedAnswers runs in the caller only when
                    // this returns success).
                    return result.copy(success = awaitQueuedResultDurable())
                }
                last = result
                if (attempt < 3) delay(delays[attempt])
            } catch (e: Exception) {
                last = ApiClient.SubmitResult(
                    false, e.message ?: context.getString(R.string.answer_submit_error), null
                )
                if (attempt < 3) {
                    delay(delays[attempt])
                } else {
                    break
                }
            }
        }
        return last
    }

    /** Poll /result up to [maxPollMs]; true only when the worker reports "done". */
    private suspend fun awaitQueuedResultDurable(maxPollMs: Long = QueuedResultPolling.POLL_DEADLINE_MS): Boolean {
        return QueuedResultPolling.awaitDurable(
            examId = examId,
            token = token,
            macAddress = macAddress,
            identityData = identityData,
            jobId = null,
            deadlineMs = maxPollMs
        )
    }

    fun autoSubmitAndExit() {
        // Gate tunggal (manual + auto) — kalau submit manual sedang berjalan
        // (isSubmitting), jangan memulai jalur paralel; jalur manual yang
        // menyelesaikan alur (lihat SubmitFlowPolicy).
        if (!SubmitFlowPolicy.canStartSubmission(isSubmitting, submittedOrExited)) return
        submittedOrExited = true

        AuditLog.i(AuditLog.Events.AUTO_SUBMIT, "strict=$strictMode answers=${getAnswers?.invoke()?.size}")

        // 1. Immediately persist submitted/exited state in SharedPreferences
        onSubmitSuccess?.invoke()

        // 2. Immediately close the app and deactivate lock task.
        // Do not wait for the network request to finish, which prevents the student from reopening the activity during delays.
        deactivateLockTask?.invoke()
        onFinish?.invoke()

        // 3. Perform network submission in the background using GlobalScope
        GlobalScope.launch(NonCancellable + Dispatchers.IO) {
            // Jawaban efektif: utamakan memori (ViewModel), FALLBACK ke prefs
            // saat memori kosong. Kasus nyata: deadline sudah lewat saat
            // re-entry (proses mati) → watchdog menembak SEBELUM jawaban
            // dipulihkan ke ViewModel — tanpa fallback ini submit kosong akan
            // menimpa jawaban asli dalam window grace server (end_time + 60 dtk)
            // dan clearSavedAnswers menghapus jawaban tersimpan. Memori tidak
            // kosong → dipakai apa adanya (siswa mungkin baru saja mengubah
            // jawaban setelah auto-save terakhir).
            val memoryAnswers = getAnswers?.invoke()
            val answersOverride = if (memoryAnswers.isNullOrEmpty()) {
                restoreAnswersFromPrefs()
            } else {
                null
            }
            val result = submitWithRetry(answersOverride)

            // Only clear saved answers if submission was successful.
            // If failed, keep answers in SharedPreferences so they can be
            // recovered or retried later (e.g. via the recovery screen on
            // re-entry — lihat hasPendingAnswers / resubmitPendingAnswers).
            if (result.success) {
                clearSavedAnswers()
            } else {
                Log.w(TAG, "Auto-submit failed after retries, keeping saved answers for recovery: ${result.message}")
            }

            val notifTitle = if (result.success) {
                context.getString(R.string.auto_submit_success_title)
            } else {
                context.getString(R.string.auto_submit_failed_title)
            }
            val notifMessage = if (result.success) {
                context.getString(R.string.toast_auto_submit_success)
            } else {
                context.getString(R.string.toast_auto_submit_failed, result.message)
            }

            showAutoSubmitNotification(notifTitle, notifMessage, isSuccess = result.success)
        }
    }

    // ---- Re-entry recovery (auto-submit gagal di background) ----

    /**
     * Kirim ulang jawaban yang masih tersimpan di prefs — dipanggil layar
     * recovery saat re-entry dengan state "submitted" tapi jawaban belum
     * terkirim (auto-submit background gagal / proses mati di tengah jalan).
     *
     * Server idempoten (upsert per exam+mac — retry tidak menduplikasi baris),
     * jadi aman diulang. Sukses → jawaban lokal dibersihkan (clearSavedAnswers
     * juga menghapus flag submitted).
     */
    fun resubmitPendingAnswers(onResult: (ApiClient.SubmitResult) -> Unit) {
        val saved = restoreAnswersFromPrefs() ?: run {
            onResult(
                ApiClient.SubmitResult(
                    false, context.getString(R.string.answer_submit_error), null
                )
            )
            return
        }
        GlobalScope.launch(NonCancellable + Dispatchers.IO) {
            val result = submitWithRetry(saved)
            android.os.Handler(android.os.Looper.getMainLooper()).post {
                if (result.success) clearSavedAnswers()
                onResult(result)
            }
        }
    }

    /**
     * Notifikasi hasil auto-submit background.
     *
     * Sebelumnya notifikasi TANPA content intent → jalan buntu: tap tidak
     * melakukan apa pun, dan satu-satunya jalur retry adalah membuka app
     * manual lalu menavigasi ke ujian. Kini tap (dan action "Kirim Lagi" pada
     * notifikasi GAGAL) membuka ExamViewerActivity langsung ke layar yang
     * relevan: recovery (jawaban masih tersimpan — dipilih onCreate via
     * hasPendingAnswers) atau "Ujian Sudah Selesai" (submit sukses).
     */
    private fun showAutoSubmitNotification(title: String, message: String, isSuccess: Boolean) {
        try {
            val notificationManager = context.getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager

            // Buka ExamViewerActivity dengan extra yang dibutuhkan onCreate
            // (recovery / already-submitted). launchMode singleTop + CLEAR_TOP:
            // instance lama di stack dibersihkan, instance baru membaca extra.
            val target = Intent(context, ExamViewerActivity::class.java).apply {
                putExtra("exam_id", examId)
                putExtra("exam_name", examName)
                putExtra("student_name", studentName)
                putExtra("student_number", studentNumber)
                putExtra("student_class", studentClass)
                putExtra("identity_data", identityData)
                putExtra("server_url", serverUrl)
                putExtra("exam_token", token)
                flags = Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TOP
            }
            val uniqueNotifId = (System.currentTimeMillis() % 100000).toInt()
            val contentIntent = PendingIntent.getActivity(
                context,
                uniqueNotifId,
                target,
                PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE
            )

            val builder = NotificationCompat.Builder(context, CHANNEL_ID)
                .setSmallIcon(R.mipmap.ic_launcher)
                .setContentTitle(title)
                .setContentText(message)
                .setStyle(NotificationCompat.BigTextStyle().bigText(message))
                .setPriority(NotificationCompat.PRIORITY_HIGH)
                .setDefaults(NotificationCompat.DEFAULT_ALL)
                .setAutoCancel(true)
                .setContentIntent(contentIntent)

            if (!isSuccess) {
                // Action "Kirim Lagi": buka layar recovery langsung — onCreate
                // memilihnya karena jawaban masih tersimpan (hasPendingAnswers).
                builder.addAction(
                    NotificationCompat.Action.Builder(
                        R.mipmap.ic_launcher,
                        context.getString(R.string.notification_retry_action),
                        contentIntent
                    ).build()
                )
            }

            notificationManager.notify(uniqueNotifId, builder.build())
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
