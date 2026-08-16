package com.examvan.app

import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Bundle
import android.util.Log
import android.view.KeyEvent
import android.view.View
import androidx.activity.OnBackPressedCallback
import androidx.activity.viewModels
import androidx.appcompat.app.AlertDialog
import com.examvan.app.api.ApiClient
import com.examvan.app.api.WebSocketManager
import com.examvan.app.databinding.ActivityExamViewerBinding
import com.examvan.app.helper.AnswerSheetBuilder
import com.examvan.app.helper.ExamDeadline
import com.examvan.app.helper.ExamModePolicy
import com.examvan.app.helper.PdfRendererHelper
import com.examvan.app.helper.SecurityEnforcer
import com.examvan.app.helper.SubmissionManager
import com.google.gson.Gson
import android.widget.Toast
import com.google.gson.reflect.TypeToken
import androidx.lifecycle.lifecycleScope
import java.time.Instant

/**
 * Screen 3: Exam PDF Viewer + Digital Answer Sheet
 *
 * Refactored from a 1764-line god-class into focused helpers (#2 fix):
 * - PdfRendererHelper        : PDF download, rendering, page navigation (#3 OOM fix)
 * - AnswerSheetBuilder       : Answer sheet UI construction and restore (#6 view tagging)
 * - SecurityEnforcer         : Strict mode, lock task, immersive mode, focus detection
 * - SubmissionManager        : Submit answers, auto-submit, retry, notifications
 */
class ExamViewerActivity : BaseSecureActivity() {

    private lateinit var binding: ActivityExamViewerBinding
    private val viewModel: ExamViewerViewModel by viewModels()

    // Refactored helpers
    private lateinit var pdfRendererHelper: PdfRendererHelper
    private lateinit var answerSheetBuilder: AnswerSheetBuilder
    private lateinit var securityEnforcer: SecurityEnforcer
    private lateinit var submissionManager: SubmissionManager

    // Exam & student info
    private var examId = -1
    private var examName = ""
    private var studentName = ""
    private var studentNumber = ""
    private var studentClass = ""
    private var identityData: String? = null
    private var startTime = ""
    private var endTime: String? = null
    private var countDownTimer: android.os.CountDownTimer? = null

    // Watchdog deadline yang TIDAK dibatalkan di onPause (auto-submit tepat
    // di deadline walau activity sedang paused) — lihat startCountdownTimer.
    private var deadlineHandler: android.os.Handler? = null
    private var deadlineRunnable: Runnable? = null

    // Recovery re-entry (jawaban belum terkirim): cegah double-tap tombol
    // "Kirim Lagi" saat resubmit sedang berjalan.
    private var recoverySubmitting = false
    private var macAddress = ""
    private var serverUrl = ""
    private var examToken = ""
    private var securityLevel = "medium"
    private var examContentLoaded = false

    // Questions config
    private var questions: List<Map<String, Any>> = emptyList()

    // Student answers from ViewModel (survives rotation)
    private val studentAnswers: Map<String, Any>
        get() = viewModel.studentAnswers.value

    // Network callback
    private val networkCallback = object : android.net.ConnectivityManager.NetworkCallback() {
        override fun onAvailable(network: android.net.Network) {
            if (!viewModel.isPdfReady.value && !viewModel.submittedOrExited.value && examId != -1) {
                android.os.Handler(android.os.Looper.getMainLooper()).post {
                    if (!isFinishing && !isDestroyed && !viewModel.isPdfReady.value) {
                        Log.i(TAG, "Network restored — auto-retrying download")
                        pdfRendererHelper.downloadPdf(examId, examToken)
                    }
                }
            }
        }
    }

    companion object {
        private const val TAG = "ExamViewer"
        private val questionsListType = object : TypeToken<List<Map<String, Any>>>() {}.type
    }

    // ===== Lifecycle =====

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        binding = ActivityExamViewerBinding.inflate(layoutInflater)
        setContentView(binding.root)
        // includeIme=true: lembar jawaban punya input isian singkat — di API 35
        // keyboard tidak men-resize window, jadi inset IME ditangani manual agar
        // input tidak tertutup keyboard.
        applyEdgeToEdgeInsets(binding.root, includeIme = true)
        binding.btnSubmitAnswers.filterTouchesWhenObscured = true

        // Read intent extras
        examId = intent.getIntExtra("exam_id", -1)
        examName = intent.getStringExtra("exam_name") ?: getString(R.string.default_exam_name)
        studentName = intent.getStringExtra("student_name") ?: ""
        studentNumber = intent.getStringExtra("student_number") ?: ""
        studentClass = intent.getStringExtra("student_class") ?: ""
        identityData = intent.getStringExtra("identity_data")
        serverUrl = intent.getStringExtra("server_url") ?: ""
        examToken = intent.getStringExtra("exam_token") ?: ""
        endTime = intent.getStringExtra("end_time")

        // Defensive: pastikan ApiClient menunjuk ke server ujian ini — state
        // statis baseUrl hilang saat proses mati (process death), dan semua
        // panggilan HTTP (PDF, presence, submit) bergantung padanya.
        if (serverUrl.isNotEmpty()) ApiClient.setBaseUrl(serverUrl)

        val submittedKey = AppPrefs.getSubmittedOrExitedKey(examId)
        val wasSubmittedOrExited = AppPrefs.getExamPrefsSafe(this).getBoolean(submittedKey, false)
        if (wasSubmittedOrExited) {
            binding.tvExamTitle.text = examName
            if (SubmissionManager.hasPendingAnswers(this, examId)) {
                // Submit sebelumnya TIDAK tuntas (auto-submit background gagal /
                // proses mati sebelum sukses) — jawaban masih tersimpan lokal,
                // tawarkan kirim ulang (lihat showPendingSubmitRecoveryScreen).
                setupRecoverySubmissionManager()
                showPendingSubmitRecoveryScreen()
            } else {
                // Benar-benar selesai — jawaban sudah dibersihkan saat sukses.
                showExamAlreadySubmittedScreen()
            }
            return
        }

        // Read from Intent first (to avoid race conditions on fresh save), fallback to EncryptedSharedPreferences
        val strictMode = intent.getBooleanExtra("strict_mode", false) ||
                AppPrefs.getExamPrefsSafe(this).getBoolean(AppPrefs.KEY_STRICT_MODE, false)
        securityLevel = intent.getStringExtra("security_level") ?:
                AppPrefs.getExamPrefsSafe(this).getString(AppPrefs.KEY_SECURITY_LEVEL, "medium") ?: "medium"
        macAddress = DeviceIdResolver.resolveDeviceId(this)
        binding.tvExamTitle.text = ""

        // ---- Initialize helpers ----
        initializeHelpers(strictMode, savedInstanceState)

        // Start WebSocket for real-time communication
        if (serverUrl.isNotEmpty() && examToken.isNotEmpty() && examId > 0) {
            WebSocketManager.connect(
                baseUrl = serverUrl,
                examId = examId,
                token = examToken,
                deviceId = macAddress,
                student_name = studentName,
                exam_number = studentNumber,
                student_class = studentClass,
                device_info = android.os.Build.MODEL,
                onEvent = { event, data ->
                    Log.d(TAG, "WS event: $event $data")
                    runOnUiThread {
                        when (event) {
                            "exam_terminated" -> {
                                // Server terminated the exam
                                submissionManager.autoSubmitAndExit()
                            }
                            "notification" -> {
                                // Show toast with server message
                                val msg = data["message"]?.toString() ?: ""
                                if (msg.isNotEmpty()) {
                                    android.widget.Toast.makeText(
                                        this@ExamViewerActivity, msg, android.widget.Toast.LENGTH_LONG
                                    ).show()
                                }
                            }
                        }
                    }
                }
            )

            // Laporkan presence siswa via HTTP (login). Heartbeat WS di-ignore
            // server untuk klien token, jadi presence memakai /access-log.
            sendAccessLog("login")
        }

        if (examId == -1) {
            showError(getString(R.string.exam_invalid_id))
            return
        }

        // Watchdog deadline dijadwalkan SEGERA (sebelum PDF siap) — lihat
        // scheduleDeadlineFromStart. Menutup kasus PDF tidak pernah siap:
        // sebelumnya deadline lewat tanpa auto-submit bila siswa terjebak di
        // layar download/error. startCountdownTimer (saat PDF siap) tetap
        // mengganti jadwal ini dengan sisa waktu terbaru.
        scheduleDeadlineFromStart()

        // Strict mode activation
        if (strictMode) {
            handleStrictMode(savedInstanceState)
        } else {
            // Load exam content
            loadExamContent(savedInstanceState)
        }
    }

    /**
     * Initialize all 4 helper objects.
     */
    private fun initializeHelpers(strictMode: Boolean, savedInstanceState: Bundle?) {
        // PdfRendererHelper
        pdfRendererHelper = PdfRendererHelper(binding, lifecycleScope, this).apply {
            onPdfReady = {
                viewModel.setPdfReady(true)
                securityEnforcer.isPdfReady = true
                // One-time gesture warning in strict mode
                securityEnforcer.showGestureWarningOnce()
                startCountdownTimer()
            }
            onError = { msg ->
                runOnUiThread {
                    if (!isFinishing && !isDestroyed) showError(msg)
                }
            }
            onProgress = { percent ->
                runOnUiThread {
                    if (!isFinishing && !isDestroyed) {
                        binding.progressDownload.progress = percent
                        binding.tvDownloadPercent.text = "$percent%"
                        if (percent < 50) binding.btnCancel.visibility = View.VISIBLE
                    }
                }
            }
        }

        // SecurityEnforcer
        securityEnforcer = SecurityEnforcer(this, binding).apply {
            securityLevel = this@ExamViewerActivity.securityLevel
            onCreateTime = System.currentTimeMillis()
            onLogoutRequested = { confirmAndLogout() }
        }

        // AnswerSheetBuilder
        answerSheetBuilder = AnswerSheetBuilder(binding, this).apply {
            onAnswerChanged = { key, value ->
                viewModel.updateAnswer(key, value)
                submissionManager.triggerAutoSave(studentAnswers)
            }
            onAnswerRemoved = { key ->
                viewModel.removeAnswer(key)
                submissionManager.triggerAutoSave(studentAnswers)
            }
            getAnswer = { key -> studentAnswers[key] }
            onSpinnerPopupChanged = { delta ->
                securityEnforcer.activePopupCount = (securityEnforcer.activePopupCount + delta).coerceAtLeast(0)
            }
        }

        // SubmissionManager
        submissionManager = SubmissionManager(this, binding, this).apply {
            examId = this@ExamViewerActivity.examId
            examName = this@ExamViewerActivity.examName
            studentName = this@ExamViewerActivity.studentName
            studentNumber = this@ExamViewerActivity.studentNumber
            studentClass = this@ExamViewerActivity.studentClass
            identityData = this@ExamViewerActivity.identityData
            macAddress = this@ExamViewerActivity.macAddress
            token = this@ExamViewerActivity.examToken
            serverUrl = this@ExamViewerActivity.serverUrl
            this.strictMode = strictMode
            deactivateLockTask = {
                if (strictMode) LockTaskManager.deactivate(this@ExamViewerActivity)
            }
            getAnswers = { this@ExamViewerActivity.studentAnswers }
            isShowingAppDialog = { securityEnforcer.isShowingAppDialog }
            setShowingAppDialog = { v -> securityEnforcer.isShowingAppDialog = v }
            onFinish = { finish() }
            isActivityFinishing = { isFinishing || isDestroyed }
            onSubmitSuccess = {
                WebSocketManager.notifyExamCompleted()
                viewModel.setSubmittedOrExited(true)
                persistSubmittedState()
            }
            initNotificationChannel()
        }

        // Wire security to submission
        securityEnforcer.submittedOrExited = false

        // Setup back press callback SEJAK AWAL (sebelum aktivasi strict).
        // Dulu baru didaftarkan di loadExamContent — selama window aktivasi
        // (layar gesture-blocked, dialog konfirmasi lock task, layar
        // strict-failed) tombol/gesture back bisa me-finish activity tanpa
        // konsekuensi. Dengan registrasi awal, back di strict mode selalu
        // diblokir (handleBackPressed) dan di medium/low masuk ke dialog
        // keluar yang benar.
        onBackPressedDispatcher.addCallback(this, object : OnBackPressedCallback(true) {
            override fun handleOnBackPressed() {
                securityEnforcer.handleBackPressed()
            }
        })
    }

    /**
     * Handle strict mode activation.
     */
    private fun handleStrictMode(savedInstanceState: Bundle?) {
        if (securityEnforcer.isGestureNavigationEnabled()) {
            securityEnforcer.showGestureBlocked {
                handleStrictMode(savedInstanceState)
            }
            return
        }

        val activated = securityEnforcer.activateStrictMode {
            loadExamContent(savedInstanceState)
        }
        if (!activated) {
            securityEnforcer.showStrictModeFailed {
                securityEnforcer.retryStrictMode {
                    loadExamContent(savedInstanceState)
                }
            }
        }
    }

    /**
     * Load exam content: questions, answers, PDF download.
     */
    private fun loadExamContent(savedInstanceState: Bundle?) {
        if (examContentLoaded) {
            if (!viewModel.isPdfReady.value) {
                pdfRendererHelper.downloadPdf(examId, examToken)
            }
            return
        }
        examContentLoaded = true

        // Restore state after rotation / process death
        if (savedInstanceState != null) {
            pdfRendererHelper.setPendingRestorePage(savedInstanceState.getInt("currentPage", -1))
            val answerSheetExpanded = savedInstanceState.getBoolean("answerSheetExpanded", false)
            securityLevel = savedInstanceState.getString("securityLevel", "medium") ?: "medium"
            if (answerSheetExpanded) {
                binding.answerSheetPanel.visibility = View.VISIBLE
                binding.btnToggleAnswerSheet.text = getString(R.string.answer_sheet_close)
            }
        }

        // Start time process death resilience
        val prefs = AppPrefs.getExamPrefsSafe(this)
        val savedStartTime = savedInstanceState?.getString("startTime")
            ?: prefs.getString(AppPrefs.KEY_EXAM_START_TIME, null)
        if (savedStartTime != null) {
            startTime = savedStartTime
            Log.i(TAG, "Restored startTime: $startTime")
        } else {
            startTime = Instant.now().toString()
            prefs.edit().putString(AppPrefs.KEY_EXAM_START_TIME, startTime).apply()
            Log.i(TAG, "New startTime: $startTime")
        }
        submissionManager.startTime = startTime

        val savedDrift = savedInstanceState?.getLong("initialClockDrift", 0L) ?: 0L
        if (savedDrift != 0L) {
            securityEnforcer.initialClockDrift = savedDrift
        }

        val savedEndTime = savedInstanceState?.getString("endTime")
        if (savedEndTime != null) {
            endTime = savedEndTime
        }

        // submittedOrExited restore
        val submittedKeyToRestore = AppPrefs.getSubmittedOrExitedKey(examId)
        val wasSubmittedOrExited = savedInstanceState?.getBoolean("submittedOrExited")
            ?: prefs.getBoolean(submittedKeyToRestore, false)
        if (wasSubmittedOrExited) {
            viewModel.setSubmittedOrExited(true)
            submissionManager.submittedOrExited = true
            binding.btnSubmitAnswers.isEnabled = false
            binding.btnSubmitAnswers.text = getString(R.string.submitted_label)
        }

        // Setup button listeners
        setupButtonListeners()

        // Load questions and build answer sheet
        loadQuestionsAndBuildSheet()

        // Start PDF download
        try {
            pdfRendererHelper.downloadPdf(examId, examToken)
        } catch (e: Exception) {
            showError(getString(R.string.download_failed_format, e.message ?: ""))
        }

        // Register network callback for auto-retry
        registerNetworkCallback()
    }

    private fun setupButtonListeners() {
        binding.btnBack.setOnClickListener {
            if (securityEnforcer.strictMode) {
                android.widget.Toast.makeText(this, getString(R.string.strict_mode_cannot_exit), android.widget.Toast.LENGTH_SHORT).show()
                AuditLog.w(AuditLog.Events.USER_EXIT_ATTEMPT, "btnBack_blocked")
            } else {
                confirmAndLogout()
            }
        }

        binding.btnPrev.setOnClickListener { pdfRendererHelper.prevPage() }
        binding.btnNext.setOnClickListener { pdfRendererHelper.nextPage() }

        binding.btnRetryDownload.setOnClickListener {
            pdfRendererHelper.downloadPdf(examId, examToken)
        }

        binding.btnCancel.setOnClickListener {
            pdfRendererHelper.cancelDownload()
            viewModel.setSubmittedOrExited(true)
            sendAccessLog("logout")
            finish()
        }

        // Swipe gestures for page navigation
        binding.ivPdfPage.swipeListener = object : com.examvan.app.view.ZoomableImageView.OnSwipeListener {
            override fun onSwipeLeft() { pdfRendererHelper.nextPage() }
            override fun onSwipeRight() { pdfRendererHelper.prevPage() }
        }

        // Answer sheet toggle
        binding.btnToggleAnswerSheet.setOnClickListener {
            val expanded = binding.answerSheetPanel.visibility != View.VISIBLE
            binding.answerSheetPanel.visibility = if (expanded) View.VISIBLE else View.GONE
            binding.btnToggleAnswerSheet.text = if (expanded) getString(R.string.answer_sheet_close) else getString(R.string.answer_sheet_open)
        }

        // Submit button
        binding.btnSubmitAnswers.setOnClickListener { submissionManager.confirmAndSubmit() }
    }

    private fun loadQuestionsAndBuildSheet() {
        try {
            val prefs = AppPrefs.getExamPrefsSafe(this)
            val json = prefs.getString(AppPrefs.KEY_QUESTIONS_JSON, null)
            securityLevel = intent.getStringExtra("security_level") ?: prefs.getString(AppPrefs.KEY_SECURITY_LEVEL, "medium") ?: "medium"
            updateSecurityBanner()
            applyPanelColor()
            submissionManager.requestNotificationPermission()

            // Parse konfigurasi soal. Jika tidak ada, kosong, atau gagal parse,
            // lembar jawaban DISEMBUNYIKAN — tidak memalsukan 40 soal default:
            // jawaban palsu tidak akan cocok dengan koreksi server dan membuat
            // siswa bisa mengumpulkan asal.
            questions = if (json != null) {
                try {
                    Gson().fromJson(json, questionsListType) ?: emptyList()
                } catch (e: Throwable) {
                    emptyList()
                }
            } else {
                emptyList()
            }

            if (questions.isEmpty()) {
                hideAnswerOverlay()
                submissionManager.totalQuestions = 0
            } else {
                // totalQuestions = jumlah soal yang BENAR-BENAR dirender
                // (nomor valid), bukan ukuran list mentah — soal dengan nomor
                // tidak valid dilewati AnswerSheetBuilder (lihat QuestionParsing).
                submissionManager.totalQuestions = answerSheetBuilder.build(questions)
                // Restore saved answers (#6 fix: view tagging handles this)
                val savedAnswers = submissionManager.restoreAnswersFromPrefs()
                if (savedAnswers != null) {
                    viewModel.setStudentAnswers(savedAnswers)
                    answerSheetBuilder.restoreFromSaved(savedAnswers)
                }
            }
        } catch (e: Exception) {
            // Kegagalan membaca prefs — aman: sembunyikan lembar jawaban.
            questions = emptyList()
            submissionManager.totalQuestions = 0
            hideAnswerOverlay()
        }
    }

    private fun updateSecurityBanner() {
        if (securityEnforcer.strictMode) {
            binding.tvSecurityBanner.text = getString(R.string.strict_mode_active)
            binding.tvSecurityBanner.setBackgroundColor(android.graphics.Color.parseColor("#B71C1C"))
            binding.tvSecurityBanner.setTextColor(android.graphics.Color.parseColor("#FFFFFF"))
        } else if (securityLevel == "medium") {
            binding.tvSecurityBanner.text = getString(R.string.autosubmit_status_active)
            binding.tvSecurityBanner.setBackgroundColor(android.graphics.Color.parseColor("#D32F2F"))
            binding.tvSecurityBanner.setTextColor(android.graphics.Color.parseColor("#FFFFFF"))
        } else {
            binding.tvSecurityBanner.text = getString(R.string.autosubmit_status_inactive)
            binding.tvSecurityBanner.setBackgroundColor(android.graphics.Color.parseColor("#455A64"))
            binding.tvSecurityBanner.setTextColor(android.graphics.Color.parseColor("#FFFFFF"))
        }
    }

    private fun applyPanelColor() {
        val panelColor = AppPrefs.getExamPrefsSafe(this).getString(AppPrefs.KEY_PANEL_COLOR, "") ?: ""
        if (panelColor.isEmpty() || !panelColor.startsWith("#")) return

        try {
            val color = android.graphics.Color.parseColor(panelColor)
            val darkerColor = darkenColor(color, 0.85f)

            // Determine if background is dark or light using luminance helper
            val isDark = androidx.core.graphics.ColorUtils.calculateLuminance(color) < 0.5
            val textColor = if (isDark) android.graphics.Color.WHITE else android.graphics.Color.BLACK

            // Apply to answer sheet panel background
            binding.answerSheetPanel.setBackgroundColor(color)
            // Apply to toggle bar button
            binding.btnToggleAnswerSheet.setBackgroundColor(darkerColor)
            binding.btnToggleAnswerSheet.setTextColor(textColor)
            // Apply to bottom bar
            binding.bottomBar.setBackgroundColor(color)
            binding.tvPageCounter.setTextColor(textColor)

            // Set variables to answerSheetBuilder for dynamic text styling of generated views
            answerSheetBuilder.panelTextColor = textColor
            answerSheetBuilder.isPanelColorDark = isDark
            answerSheetBuilder.applyDynamicTextColors()
        } catch (_: Exception) { }
    }

    private fun darkenColor(color: Int, factor: Float): Int {
        val r = (android.graphics.Color.red(color) * factor).toInt().coerceIn(0, 255)
        val g = (android.graphics.Color.green(color) * factor).toInt().coerceIn(0, 255)
        val b = (android.graphics.Color.blue(color) * factor).toInt().coerceIn(0, 255)
        return android.graphics.Color.rgb(r, g, b)
    }

    private fun hideAnswerOverlay() {
        binding.answerSheetToggle.visibility = View.GONE
        binding.answerSheetPanel.visibility = View.GONE
        binding.btnSubmitAnswers.visibility = View.GONE
    }

    private fun confirmAndLogout() {
        if (securityEnforcer.strictMode) {
            securityEnforcer.isShowingAppDialog = true
            AlertDialog.Builder(this)
                .setTitle(getString(R.string.strict_mode_cannot_exit_title))
                .setMessage("Mode ketat: Anda tidak bisa keluar dari ujian. " +
                        "Selesaikan semua jawaban dan tekan tombol 'Kumpulkan' untuk menyelesaikan ujian.")
                .setPositiveButton(getString(R.string.dialog_ok)) { _, _ ->
                    securityEnforcer.isShowingAppDialog = false
                }
                .setOnCancelListener { securityEnforcer.isShowingAppDialog = false }
                .show()
            return
        }

        val title: String
        val message: String
        val positiveButtonText: String

        if (securityLevel == "low") {
            title = getString(R.string.logout_low_title)
            message = getString(R.string.logout_low_message)
            positiveButtonText = getString(R.string.logout_low_positive)
        } else {
            title = getString(R.string.logout_default_title)
            message = getString(R.string.logout_default_message)
            positiveButtonText = getString(R.string.logout_default_positive)
        }

        securityEnforcer.isShowingAppDialog = true
        AlertDialog.Builder(this)
            .setTitle(title)
            .setMessage(message)
            .setPositiveButton(positiveButtonText) { _, _ ->
                securityEnforcer.isShowingAppDialog = false
                if (securityLevel == "low") {
                    viewModel.setSubmittedOrExited(true)
                    sendAccessLog("logout")
                    finish()
                } else {
                    submissionManager.autoSubmitAndExit()
                }
            }
            .setNegativeButton(getString(R.string.btn_cancel)) { _, _ ->
                securityEnforcer.isShowingAppDialog = false
            }
            .setOnCancelListener { securityEnforcer.isShowingAppDialog = false }
            .show()
    }

    private fun showError(message: String) {
        pdfRendererHelper.showError(message)
    }

    /**
     * Laporkan event presence siswa (login/heartbeat/logout) ke server via
     * HTTP POST /access-log. Fire-and-forget — kegagalan tidak memengaruhi
     * jalannya ujian.
     */
    private fun sendAccessLog(event: String) {
        if (examId <= 0 || examToken.isEmpty() || serverUrl.isEmpty()) return
        ApiClient.sendAccessLog(
            examId = examId,
            token = examToken,
            macAddress = macAddress,
            event = event,
            studentName = studentName,
            examNumber = studentNumber,
            studentClass = studentClass,
            deviceInfo = android.os.Build.MODEL
        )
    }

    // ===== Lifecycle overrides =====

    override fun onStart() {
        super.onStart()
        if (::securityEnforcer.isInitialized) {
            securityEnforcer.verifyLockTask()
            if (securityEnforcer.strictMode) {
                securityEnforcer.enterImmersiveMode()
            }
        }
    }

    override fun onResume() {
        super.onResume()
        if (::securityEnforcer.isInitialized) {
            // Reset dialog flag
            android.os.Handler(android.os.Looper.getMainLooper()).post {
                if (!isFinishing && !isDestroyed) {
                    securityEnforcer.isShowingAppDialog = false
                }
            }
            securityEnforcer.onResume()
            // Banner mencerminkan mode aktif (juga menutup kasus strict-failed
            // yang sebelumnya tidak pernah di-update setelah aktivasi gagal).
            updateSecurityBanner()
            if (viewModel.isPdfReady.value) {
                startCountdownTimer()
            }
        }
    }

    override fun onPause() {
        super.onPause()
        countDownTimer?.cancel()
    }

    override fun onUserLeaveHint() {
        super.onUserLeaveHint()
        if (::securityEnforcer.isInitialized && ::submissionManager.isInitialized) {
            securityEnforcer.isPdfReady = viewModel.isPdfReady.value
            securityEnforcer.submittedOrExited = viewModel.submittedOrExited.value
            securityEnforcer.handleUserLeave()

            // STRICT MODE: Do NOT auto-submit. The lock task pin IS the security.
            // Calling autoSubmitAndExit releases the pin (stopLockTask) and lets students out freely.
            // Instead, let Android handle it: forced unpin → Android lockscreen → student can't open anything.
            // MEDIUM MODE: Auto-submit and exit immediately.
            if (!submissionManager.submittedOrExited && viewModel.isPdfReady.value) {
                if (securityLevel == "medium" && !securityEnforcer.strictMode) {
                    submissionManager.autoSubmitAndExit()
                }
            }
        }
    }

    override fun onWindowFocusChanged(hasFocus: Boolean) {
        super.onWindowFocusChanged(hasFocus)
        if (::securityEnforcer.isInitialized && ::submissionManager.isInitialized) {
            securityEnforcer.isPdfReady = viewModel.isPdfReady.value
            securityEnforcer.submittedOrExited = viewModel.submittedOrExited.value
            securityEnforcer.handleWindowFocusChanged(hasFocus) {
                // Auto-submit HANYA di medium mode (lihat ExamModePolicy): low
                // bebas keluar-masuk tanpa konsekuensi; strict memakai lock task
                // pin (auto-submit justru melepas pin dan membebaskan siswa).
                if (ExamModePolicy.shouldAutoSubmitOnFocusLoss(securityLevel, securityEnforcer.strictMode)) {
                    submissionManager.autoSubmitAndExit()
                }
            }
        }
    }

    override fun onKeyDown(keyCode: Int, event: KeyEvent?): Boolean {
        if (::securityEnforcer.isInitialized) {
            // Volume ditekan hanya di medium/strict (panel volume tidak boleh
            // dipakai sebagai jalur keluar/bypass); di low tombol volume
            // berfungsi normal lewat sistem.
            if (ExamModePolicy.shouldInterceptVolumeKeys(securityLevel) &&
                securityEnforcer.handleVolumeKey(keyCode)
            ) {
                return true
            }
            if (keyCode == KeyEvent.KEYCODE_POWER) securityEnforcer.handlePowerKey()
        }
        return super.onKeyDown(keyCode, event)
    }

    override fun onKeyLongPress(keyCode: Int, event: KeyEvent?): Boolean {
        if (::securityEnforcer.isInitialized) {
            if (keyCode == KeyEvent.KEYCODE_VOLUME_UP || keyCode == KeyEvent.KEYCODE_VOLUME_DOWN) {
                return securityEnforcer.handleVolumeKeyLongPress()
            }
            if (keyCode == KeyEvent.KEYCODE_POWER && securityEnforcer.strictMode) {
                return securityEnforcer.handlePowerKeyLongPress()
            }
        }
        return super.onKeyLongPress(keyCode, event)
    }

    override fun onSaveInstanceState(outState: Bundle) {
        super.onSaveInstanceState(outState)
        if (::pdfRendererHelper.isInitialized) {
            outState.putInt("currentPage", pdfRendererHelper.currentPage)
        }
        outState.putBoolean("answerSheetExpanded", binding.answerSheetPanel.visibility == View.VISIBLE)
        if (::securityEnforcer.isInitialized) {
            outState.putBoolean("strictMode", securityEnforcer.strictMode)
            outState.putLong("initialClockDrift", securityEnforcer.initialClockDrift)
        }
        outState.putBoolean("submittedOrExited", viewModel.submittedOrExited.value)
        if (::submissionManager.isInitialized) {
            outState.putBoolean("isSubmitting", submissionManager.isSubmitting)
        }
        outState.putBoolean("isPdfReady", viewModel.isPdfReady.value)
        outState.putString("securityLevel", securityLevel)
        outState.putString("startTime", startTime)
        outState.putString("endTime", endTime)
    }

    override fun onDestroy() {
        super.onDestroy()
        countDownTimer?.cancel()
        deadlineRunnable?.let { deadlineHandler?.removeCallbacks(it) }
        deadlineRunnable = null
        if (::pdfRendererHelper.isInitialized) pdfRendererHelper.cleanup()
        if (::securityEnforcer.isInitialized) securityEnforcer.cleanup()
        unregisterNetworkCallback()
        WebSocketManager.disconnect()
        AuditLog.reset()
    }

    override fun onRequestPermissionsResult(requestCode: Int, permissions: Array<String>, grantResults: IntArray) {
        super.onRequestPermissionsResult(requestCode, permissions, grantResults)
        if (requestCode == 1001) { // REQUEST_NOTIFICATION_PERMISSION
            val granted = grantResults.isNotEmpty() && grantResults[0] == PackageManager.PERMISSION_GRANTED
            Log.d(TAG, "Notification permission ${if (granted) "granted" else "denied"}")
        }
    }

    // ===== Network callback =====

    private fun registerNetworkCallback() {
        val cm = getSystemService(android.content.Context.CONNECTIVITY_SERVICE) as? android.net.ConnectivityManager ?: return
        val request = android.net.NetworkRequest.Builder()
            .addCapability(android.net.NetworkCapabilities.NET_CAPABILITY_INTERNET)
            .build()
        cm.registerNetworkCallback(request, networkCallback)
    }

    private fun startCountdownTimer() {
        val end = endTime ?: return
        countDownTimer?.cancel()

        // Sisa waktu dihitung ulang dari DEADLINE ABSOLUT + skew server setiap
        // dipanggil (resume / PDF siap) — lihat ExamDeadline. `null` bila
        // end_time tidak ada/rusak → countdown tidak ditampilkan (tidak crash).
        val remainingMs = ExamDeadline.remainingMs(
            endTimeIso = end,
            nowMs = System.currentTimeMillis(),
            skewMs = com.examvan.app.api.ApiClient.serverTimeSkewMs
        ) ?: run {
            Log.w(TAG, "end_time tidak dapat di-parse, countdown tidak ditampilkan: $end")
            return
        }

        // Deadline sudah lewat (mis. app di-background melewati deadline) →
        // auto-submit langsung; tidak perlu menjadwalkan watchdog delay 0.
        if (remainingMs <= 0) {
            triggerDeadlineAutoSubmit()
            return
        }

        // Watchdog deadline: handler utama yang TIDAK dibatalkan saat onPause.
        // Dulu CountDownTimer dibatalkan di onPause → kalau layar mati / app
        // di-background tepat di deadline, auto-submit tertunda sampai siswa
        // membuka app kembali. Watchdog menembak tepat di deadline walau
        // activity sedang paused (selama proses masih hidup).
        scheduleDeadlineWatchdog(remainingMs)

        binding.tvTimer.visibility = View.VISIBLE

        countDownTimer = object : android.os.CountDownTimer(remainingMs, 1000) {
            override fun onTick(millisUntilFinished: Long) {
                val seconds = millisUntilFinished / 1000
                val hours = seconds / 3600
                val minutes = (seconds % 3600) / 60
                val secs = seconds % 60
                val timeStr = String.format("Sisa: %02d:%02d:%02d", hours, minutes, secs)
                binding.tvTimer.text = timeStr
            }

            override fun onFinish() {
                triggerDeadlineAutoSubmit()
            }
        }.start()
    }

    /**
     * Jadwalkan watchdog deadline segera setelah helper siap, tanpa menunggu
     * PDF siap. Dulu watchdog hanya dijadwalkan dari startCountdownTimer
     * (onPdfReady / onResume) — kalau PDF tidak pernah siap (download gagal,
     * siswa di layar error/strict-failed), deadline lewat tanpa auto-submit dan
     * siswa terjebak. Dengan jadwal awal ini, deadline selalu menuntaskan alur:
     * auto-submit jawaban yang ada (mungkin kosong). startCountdownTimer tetap
     * mengganti jadwal dengan sisa waktu terbaru saat PDF akhirnya siap.
     */
    private fun scheduleDeadlineFromStart() {
        val end = endTime ?: return
        val remainingMs = ExamDeadline.remainingMs(
            endTimeIso = end,
            nowMs = System.currentTimeMillis(),
            skewMs = com.examvan.app.api.ApiClient.serverTimeSkewMs
        ) ?: run {
            Log.w(TAG, "end_time tidak dapat di-parse, watchdog deadline tidak dijadwalkan: $end")
            return
        }
        if (remainingMs <= 0) {
            triggerDeadlineAutoSubmit()
        } else {
            scheduleDeadlineWatchdog(remainingMs)
        }
    }

    /**
     * Jadwalkan watchdog deadline (tidak dibatalkan di onPause). Setiap
     * pemanggilan mengganti jadwal sebelumnya dengan sisa waktu terbaru.
     */
    private fun scheduleDeadlineWatchdog(remainingMs: Long) {
        val handler = deadlineHandler ?: android.os.Handler(android.os.Looper.getMainLooper())
            .also { deadlineHandler = it }
        deadlineRunnable?.let { handler.removeCallbacks(it) }
        deadlineRunnable = Runnable { triggerDeadlineAutoSubmit() }
        handler.postDelayed(deadlineRunnable!!, remainingMs.coerceAtLeast(0))
    }

    /**
     * Waktu ujian habis: batalkan timer & watchdog, lalu auto-submit.
     * Dipanggil dari onFinish CountDownTimer ATAU watchdog (yang menembak
     * bahkan saat activity paused). Idempoten via guard autoSubmitAndExit.
     */
    private fun triggerDeadlineAutoSubmit() {
        deadlineRunnable?.let { deadlineHandler?.removeCallbacks(it) }
        deadlineRunnable = null
        countDownTimer?.cancel()

        // Jalur lain sudah menuntaskan alur (submit manual sukses / auto-submit
        // dari onUserLeaveHint / event exam_terminated) atau submit manual masih
        // berjalan (isSubmitting) — jangan tampilkan toast deadline yang
        // menyesatkan; jalur yang aktif itulah yang menyelesaikan alur (lihat
        // SubmitFlowPolicy). autoSubmitAndExit sendiri sudah di-gate, guard ini
        // mencegah toast ganda di level UI.
        if (submissionManager.submittedOrExited || submissionManager.isSubmitting) return

        binding.tvTimer.text = "Sisa: 00:00:00"
        Toast.makeText(this@ExamViewerActivity, "Waktu habis! Menyerahkan jawaban...", Toast.LENGTH_LONG).show()
        submissionManager.autoSubmitAndExit()
    }

    // ===== Re-entry recovery (auto-submit gagal di background) =====

    /**
     * Inisialisasi SubmissionManager minimal untuk layar recovery. Jalur ini
     * TIDAK melalui initializeHelpers — tidak perlu PDF, answer sheet, atau
     * security enforcer; hanya mengirim ulang jawaban dari prefs.
     */
    private fun setupRecoverySubmissionManager() {
        submissionManager = SubmissionManager(this, binding, this).apply {
            examId = this@ExamViewerActivity.examId
            examName = this@ExamViewerActivity.examName
            studentName = this@ExamViewerActivity.studentName
            studentNumber = this@ExamViewerActivity.studentNumber
            studentClass = this@ExamViewerActivity.studentClass
            identityData = this@ExamViewerActivity.identityData
            // PENTING: field `macAddress` activity BELUM di-set di jalur ini
            // (DeviceIdResolver.resolveDeviceId dipanggil SETELAH early-return
            // submitted-check). Membaca field → macAddress kosong → server
            // meng-upsert baris dengan MAC "unknown", bukan perangkat — baris
            // jawaban asli tidak ter-update. Resolve langsung di sini.
            macAddress = DeviceIdResolver.resolveDeviceId(this@ExamViewerActivity)
            token = this@ExamViewerActivity.examToken
            serverUrl = this@ExamViewerActivity.serverUrl
            startTime = AppPrefs.getExamPrefsSafe(this@ExamViewerActivity)
                .getString(AppPrefs.KEY_EXAM_START_TIME, "") ?: ""
            deactivateLockTask = {}
            getAnswers = { emptyMap() }
            onFinish = { finish() }
            isActivityFinishing = { isFinishing || isDestroyed }
        }
    }

    /**
     * Layar recovery: jawaban masih tersimpan lokal karena pengumpulan
     * otomatis sebelumnya gagal (autoSubmitAndExit mem-persist "submitted"
     * SEBELUM submit jaringan). Tawarkan kirim ulang — server idempoten,
     * retry tidak menduplikasi baris.
     */
    private fun showPendingSubmitRecoveryScreen() {
        binding.btnBack.visibility = View.GONE
        binding.btnToggleAnswerSheet.visibility = View.GONE
        binding.btnSubmitAnswers.visibility = View.GONE
        binding.btnPrev.visibility = View.GONE
        binding.btnNext.visibility = View.GONE
        binding.answerSheetToggle.visibility = View.GONE
        binding.layoutDownload.visibility = View.GONE
        binding.ivPdfPage.visibility = View.GONE

        binding.tvErrorMsg.text = getString(R.string.recovery_pending_message)
        binding.btnRetryDownload.text = getString(R.string.recovery_retry_submit)
        binding.btnRetryDownload.setOnClickListener { submitPendingAnswers() }
        binding.btnOpenResult.text = getString(R.string.recovery_exit)
        binding.btnOpenResult.visibility = View.VISIBLE
        binding.btnOpenResult.setOnClickListener { finish() }

        binding.layoutError.visibility = View.VISIBLE
    }

    private fun submitPendingAnswers() {
        if (recoverySubmitting) return
        recoverySubmitting = true
        binding.btnRetryDownload.isEnabled = false
        binding.btnRetryDownload.text = getString(R.string.recovery_sending)
        submissionManager.resubmitPendingAnswers { result ->
            if (isFinishing || isDestroyed) return@resubmitPendingAnswers
            recoverySubmitting = false
            if (result.success) {
                submissionManager.showCongrats(result)
            } else {
                binding.btnRetryDownload.isEnabled = true
                binding.btnRetryDownload.text = getString(R.string.recovery_retry_submit)
                Toast.makeText(
                    this,
                    getString(R.string.toast_auto_submit_failed, result.message),
                    Toast.LENGTH_LONG
                ).show()
            }
        }
    }

    private fun showExamAlreadySubmittedScreen() {
        binding.btnBack.visibility = View.GONE
        binding.btnToggleAnswerSheet.visibility = View.GONE
        binding.btnSubmitAnswers.visibility = View.GONE
        binding.btnPrev.visibility = View.GONE
        binding.btnNext.visibility = View.GONE
        binding.answerSheetToggle.visibility = View.GONE
        binding.layoutDownload.visibility = View.GONE
        binding.ivPdfPage.visibility = View.GONE

        binding.tvErrorMsg.text = "Ujian Sudah Selesai!\n\nAnda telah mengumpulkan jawaban untuk ujian ini. Terima kasih!"
        binding.btnRetryDownload.text = "Keluar"
        binding.btnRetryDownload.setOnClickListener {
            finish()
        }

        // "Buka Halaman Hasil" — also available when re-entering an already
        // submitted exam, so the student can still reach their results page.
        binding.btnOpenResult.visibility = View.VISIBLE
        binding.btnOpenResult.setOnClickListener {
            val base = serverUrl.trim().trimEnd('/')
            val token = examToken.trim()
            if (base.isNotEmpty() && token.isNotEmpty()) {
                try {
                    startActivity(
                        Intent(Intent.ACTION_VIEW, Uri.parse("$base/$token"))
                            .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
                    )
                } catch (_: Exception) {
                    Toast.makeText(this, R.string.congrats_link_missing, Toast.LENGTH_SHORT).show()
                }
            } else {
                Toast.makeText(this, R.string.congrats_link_missing, Toast.LENGTH_SHORT).show()
            }
        }

        binding.layoutError.visibility = View.VISIBLE
    }

    private fun unregisterNetworkCallback() {
        try {
            val cm = getSystemService(android.content.Context.CONNECTIVITY_SERVICE) as? android.net.ConnectivityManager
            cm?.unregisterNetworkCallback(networkCallback)
        } catch (_: Exception) { }
    }
}
