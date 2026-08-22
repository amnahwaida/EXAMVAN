package com.examvan.app

import android.content.BroadcastReceiver
import android.content.Intent
import android.content.IntentFilter
import android.content.pm.PackageManager
import android.os.BatteryManager
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
import com.examvan.app.helper.AccessLogPolicy
import com.examvan.app.helper.AnswerSheetBuilder
import com.examvan.app.helper.AppDialogIds
import com.examvan.app.helper.ExamDeadline
import com.examvan.app.helper.ExamLaunchPolicy
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
    private var securityLevel = ExamModePolicy.DEFAULT_LEVEL
    private var examContentLoaded = false

    /**
     * Timestamp saat PDF pertama kali siap — dasar grace period startup
     * auto-submit mode medium (ExamModePolicy.isWithinStartupGrace).
     * Null = PDF belum siap.
     */
    private var pdfReadyAtMs: Long? = null

    /**
     * Grace startup aktif → jalur keluar (Home/fokus/onStop) MENUNDA
     * auto-submit. Deadline watchdog tidak pernah terpengaruh.
     */
    private fun suppressedByStartupGrace(): Boolean =
        ExamModePolicy.isWithinStartupGrace(pdfReadyAtMs, System.currentTimeMillis())

    // Questions config
    private var questions: List<Map<String, Any>> = emptyList()

    // Student answers from ViewModel (survives rotation)
    private val studentAnswers: Map<String, Any>
        get() = viewModel.studentAnswers.value

    // Network callback
    private val networkCallback = object : android.net.ConnectivityManager.NetworkCallback() {
        override fun onAvailable(network: android.net.Network) {
            // Fix review fitur indikator ronde 2 #1: SEMUA event jaringan
            // membaca ulang state — kontrak seragam dengan callback lain.
            refreshConnectionIndicator()
            if (!viewModel.isPdfReady.value && !viewModel.submittedOrExited.value && examId != -1) {
                android.os.Handler(android.os.Looper.getMainLooper()).post {
                    if (!isFinishing && !isDestroyed && !viewModel.isPdfReady.value) {
                        Log.i(TAG, "Network restored — auto-retrying download")
                        pdfRendererHelper.downloadPdf(examId, examToken)
                    }
                }
            }
        }

        // Indikator koneksi (fitur baru): keputusan SELALU dari jaringan
        // aktif saat ini — dulu onLost satu network (WiFi hilang, seluler
        // masih aktif) langsung menandai "Offline" palsu.
        override fun onCapabilitiesChanged(
            network: android.net.Network,
            networkCapabilities: android.net.NetworkCapabilities
        ) {
            refreshConnectionIndicator()
        }

        override fun onLost(network: android.net.Network) {
            refreshConnectionIndicator()
        }

        override fun onUnavailable() {
            refreshConnectionIndicator()
        }
    }

    // Receiver baterai (fitur baru: indikator baterai selama ujian)
    private var batteryReceiver: BroadcastReceiver? = null

    // Haptic momen kritis (backlog aksesibilitas): getar hanya pada
    // TRANSISI masuk CRITICAL, bukan tiap tick (HapticPolicy).
    private var lastTimerUrgency: com.examvan.app.helper.ExamDeadline.TimerUrgency? = null

    companion object {
        private const val TAG = "ExamViewer"
        private val questionsListType = object : TypeToken<List<Map<String, Any>>>() {}.type

        // ID holder dialog terpusat di AppDialogIds (fix review ronde 4 #1).
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

        // Indikator baterai & koneksi (fitur baru) — tampil selama ujian,
        // termasuk di fase aktivasi strict sebelum callback jaringan
        // terdaftar (fix review fitur #2: tidak ada indikator kosong).
        registerBatteryIndicator()
        refreshConnectionIndicator()

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
        // Normalisasi defensif (fix review low-mode #1): sumber hulu sudah
        // dinormalisasi, tapi nilai lama dari prefs versi sebelumnya bisa
        // belum ternormalisasi.
        securityLevel = ExamModePolicy.normalize(
            intent.getStringExtra("security_level")
                ?: AppPrefs.getExamPrefsSafe(this).getString(AppPrefs.KEY_SECURITY_LEVEL, null)
        )
        macAddress = DeviceIdResolver.resolveDeviceId(this)
        binding.tvExamTitle.text = ""

        // ---- Initialize helpers ----
        initializeHelpers(strictMode, savedInstanceState)

        // Validasi intent SEKARANG — sebelum efek samping apapun (fix temuan
        // review: dulu cek examId == -1 baru jalan SETELAH WebSocket connect,
        // access log, dan penjadwalan deadline watchdog).
        val launchCheck = ExamLaunchPolicy.validate(examId, serverUrl, examToken)
        if (launchCheck is ExamLaunchPolicy.Invalid) {
            Log.e(TAG, "Intent ujian tidak valid: field=${launchCheck.field}")
            showError(getString(R.string.exam_invalid_id))
            return
        }

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
                // Catat waktu PDF siap — dasar grace period startup agar
                // Home tidak sengaja sesaat setelah ujian terbuka tidak
                // langsung memicu auto-submit (fix temuan review mode
                // medium #4; lihat ExamModePolicy.isWithinStartupGrace).
                pdfReadyAtMs = System.currentTimeMillis()
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
            // "Keluar" di layar strict-failed = auto-submit: jawaban tetap
            // dikumpulkan walau lock task tidak pernah aktif. submissionManager
            // dibaca saat dipanggil (sudah di-initialize sebelum layar ini
            // bisa muncul — handleStrictMode berjalan setelah initializeHelpers).
            onStrictFailedExit = { submissionManager.autoSubmitAndExit() }
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
            // Fix review gel. 2 #1: status prompt izin terdaftar sebagai
            // holder di registry — safety-net reset onResume jadi kondisional.
            markPermissionPromptPending = { pending ->
                if (pending) securityEnforcer.holdAppDialog(AppDialogIds.PERMISSION_PROMPT)
                else securityEnforcer.releaseAppDialog(AppDialogIds.PERMISSION_PROMPT)
            }
            // Fix review ronde 4 #1: dialog milik SubmissionManager (konfirmasi
            // submit, submit-gagal, congrats) kini juga memegang flag via
            // holder — invariant reset kondisional berlaku tanpa pengecualian.
            holdAppDialog = { securityEnforcer.holdAppDialog(it) }
            releaseAppDialog = { securityEnforcer.releaseAppDialog(it) }
            // Fix review ronde 3 #4: log presence server dapat penanda
            // eksplisit "auto_submit" saat ujian ditutup otomatis.
            onAutoSubmitAccessLog = { sendAccessLog(AccessLogPolicy.EVENT_AUTO_SUBMIT) }
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
            securityLevel = savedInstanceState.getString("securityLevel", ExamModePolicy.DEFAULT_LEVEL) ?: ExamModePolicy.DEFAULT_LEVEL
            if (answerSheetExpanded) {
                binding.answerSheetPanel.visibility = View.VISIBLE
                binding.btnToggleAnswerSheet.text = getString(R.string.answer_sheet_close)
            }
            // Fix review gel. 2 #3: restore grace startup agar rotasi di
            // dalam window 3s pertama tidak menghilangkan perlindungan
            // anti-auto-submit-dini. -1 = belum pernah PDF siap.
            val savedReadyAt = savedInstanceState.getLong("pdfReadyAtMs", -1L)
            if (savedReadyAt > 0) pdfReadyAtMs = savedReadyAt
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

        // Baseline clock-drift (fix temuan review: dulu baseline hanya ada di
        // savedInstanceState — proses mati = baseline hilang = siswa bebas
        // memanipulasi jam setelah restart). Prioritas via ClockDriftPolicy:
        // savedInstanceState → prefs terenkripsi → hitung baru (+persist).
        val savedDriftRaw = savedInstanceState?.getLong("initialClockDrift", 0L)
        val persistedDrift = if (prefs.contains(AppPrefs.KEY_CLOCK_DRIFT_BASELINE)) {
            prefs.getLong(AppPrefs.KEY_CLOCK_DRIFT_BASELINE, 0L)
        } else null
        val resolvedDrift = com.examvan.app.helper.ClockDriftPolicy.resolveBaseline(
            savedStateDriftMs = savedDriftRaw,
            persistedDriftMs = persistedDrift,
            freshDriftMs = System.currentTimeMillis() - android.os.SystemClock.elapsedRealtime()
        )
        securityEnforcer.initialClockDrift = resolvedDrift.baselineDriftMs
        if (resolvedDrift.isNew) {
            prefs.edit().putLong(AppPrefs.KEY_CLOCK_DRIFT_BASELINE, resolvedDrift.baselineDriftMs).apply()
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
            // Jalur keluar-bebas (batal unduh): logout dengan retry agar
            // presence dasbor akurat (fix review low-mode ronde 2 #2).
            viewModel.setSubmittedOrExited(true)
            sendAccessLog("logout", retryOnFailure = true)
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
            securityLevel = ExamModePolicy.normalize(
                intent.getStringExtra("security_level")
                    ?: prefs.getString(AppPrefs.KEY_SECURITY_LEVEL, null)
            )
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
        // Fix review UI/UX ronde 2 #1: warna via SecurityBannerPolicy —
        // medium kini AMBER (bukan merah) agar pelanggaran nyata di strict
        // tetap punya pembeda visual.
        binding.tvSecurityBanner.text = if (securityEnforcer.strictMode) {
            getString(R.string.strict_mode_active)
        } else if (securityLevel == ExamModePolicy.LEVEL_MEDIUM) {
            getString(R.string.autosubmit_status_active)
        } else {
            getString(R.string.autosubmit_status_inactive)
        }
        binding.tvSecurityBanner.setBackgroundColor(
            androidx.core.content.ContextCompat.getColor(
                this,
                com.examvan.app.helper.SecurityBannerPolicy.backgroundRes(
                    strictMode = securityEnforcer.strictMode,
                    securityLevel = securityLevel
                )
            )
        )
        binding.tvSecurityBanner.setTextColor(
            androidx.core.content.ContextCompat.getColor(
                this,
                com.examvan.app.helper.SecurityBannerPolicy.textRes(
                    strictMode = securityEnforcer.strictMode,
                    securityLevel = securityLevel
                )
            )
        )
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
            // Holder-based flag (fix review gel. 2 #1): dialog terdaftar di
            // registry sehingga safety-net reset onResume tidak mematikan
            // proteksi selama dialog masih terbuka.
            securityEnforcer.holdAppDialog(AppDialogIds.LOGOUT_CONFIRM)
            AlertDialog.Builder(this)
                .setTitle(getString(R.string.strict_mode_cannot_exit_title))
                .setMessage("Mode ketat: Anda tidak bisa keluar dari ujian. " +
                        "Selesaikan semua jawaban dan tekan tombol 'Kumpulkan' untuk menyelesaikan ujian.")
                .setPositiveButton(getString(R.string.dialog_ok)) { _, _ ->
                    securityEnforcer.releaseAppDialog(AppDialogIds.LOGOUT_CONFIRM)
                }
                .setOnCancelListener { securityEnforcer.releaseAppDialog(AppDialogIds.LOGOUT_CONFIRM) }
                .show()
            return
        }

        val title: String
        val message: String
        val positiveButtonText: String

        if (securityLevel == ExamModePolicy.LEVEL_LOW) {
            title = getString(R.string.logout_low_title)
            message = getString(R.string.logout_low_message)
            positiveButtonText = getString(R.string.logout_low_positive)
        } else {
            title = getString(R.string.logout_default_title)
            message = getString(R.string.logout_default_message)
            positiveButtonText = getString(R.string.logout_default_positive)
        }

        securityEnforcer.holdAppDialog(AppDialogIds.LOGOUT_CONFIRM)
        AlertDialog.Builder(this)
            .setTitle(title)
            .setMessage(message)
            .setPositiveButton(positiveButtonText) { _, _ ->
                securityEnforcer.releaseAppDialog(AppDialogIds.LOGOUT_CONFIRM)
                if (securityLevel == ExamModePolicy.LEVEL_LOW) {
                    // Flush jawaban dalam jendela debounce (fix review
                    // low-mode ronde 2 #1): edit 500ms terakhir tidak hilang.
                    submissionManager.flushPendingAutoSave()
                    viewModel.setSubmittedOrExited(true)
                    sendAccessLog("logout", retryOnFailure = true)
                    finish()
                } else {
                    submissionManager.autoSubmitAndExit()
                }
            }
            .setNegativeButton(getString(R.string.btn_cancel)) { _, _ ->
                securityEnforcer.releaseAppDialog(AppDialogIds.LOGOUT_CONFIRM)
            }
            .setOnCancelListener { securityEnforcer.releaseAppDialog(AppDialogIds.LOGOUT_CONFIRM) }
            .show()
    }

    private fun showError(message: String) {
        pdfRendererHelper.showError(message)
    }

    /**
     * Laporkan event presence siswa (login/heartbeat/logout) ke server via
     * HTTP POST /access-log. Fire-and-forget — kegagalan tidak memengaruhi
     * jalannya ujian.
     *
     * [retryOnFailure] = true untuk event LOGOUT di jalur keluar-bebas:
     * kegagalan membuat siswa tampak "online" di dasbor sampai TTL presence
     * habis (fix review low-mode ronde 2 #2 — retry sekali).
     */
    private fun sendAccessLog(event: String, retryOnFailure: Boolean = false) {
        if (examId <= 0 || examToken.isEmpty() || serverUrl.isEmpty()) return
        if (retryOnFailure) {
            ApiClient.sendAccessLogWithRetry(
                examId = examId,
                token = examToken,
                macAddress = macAddress,
                event = event,
                studentName = studentName,
                examNumber = studentNumber,
                studentClass = studentClass,
                deviceInfo = android.os.Build.MODEL
            )
            return
        }
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
            // Reset dialog flag — KONDISIONAL (fix review gel. 2 #1): dulu
            // posted runnable ini memaksa flag false secara buta, sehingga
            // flag bisa lepas padahal prompt izin / dialog masih terbuka dan
            // jalur focus-loss memicu auto-submit palsu. Kini reset no-op
            // bila ada holder aktif di AppDialogFlagRegistry.
            android.os.Handler(android.os.Looper.getMainLooper()).post {
                if (!isFinishing && !isDestroyed) {
                    securityEnforcer.resetAppDialogIfIdle()
                }
            }
            securityEnforcer.onResume()
            // Fix review strict ronde 2 #2: recovery lock task aktif hanya
            // saat foreground.
            securityEnforcer.setLockRecoveryEnabled(true)
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
        if (::securityEnforcer.isInitialized) {
            // Fix review strict ronde 2 #2: matikan recovery lock task saat
            // background agar health check tidak spam startLockTask gagal.
            securityEnforcer.setLockRecoveryEnabled(false)
        }
        countDownTimer?.cancel()
    }

    override fun onStop() {
        super.onStop()
        if (::securityEnforcer.isInitialized && ::submissionManager.isInitialized) {
            if (!submissionManager.submittedOrExited && viewModel.isPdfReady.value) {
                if (suppressedByStartupGrace()) return  // grace startup (fix #4)
                if (ExamModePolicy.shouldAutoSubmitOnFocusLoss(securityLevel, securityEnforcer.strictMode)) {
                    submissionManager.autoSubmitAndExit()
                }
            }
        }
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
            //
            // Fix temuan review mode medium #2: keputusan mode kini lewat
            // ExamModePolicy.shouldAutoSubmitOnUserLeave (bukan
            // `== "medium"` hardcoded) agar konsisten dengan jalur
            // onStop/fokus untuk level level tak dikenal/custom.
            // Fix #4: dalam grace startup, Home tidak sengaja tidak langsung
            // men-submit — siswa diberi waktu masuk terlebih dahulu.
            if (!submissionManager.submittedOrExited && viewModel.isPdfReady.value) {
                if (suppressedByStartupGrace()) return
                if (ExamModePolicy.shouldAutoSubmitOnUserLeave(securityLevel, securityEnforcer.strictMode)) {
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
                // Grace startup juga berlaku di sini (fix #4): dialog sistem
                // yang muncul beberapa detik pertama tak boleh men-submit.
                if (suppressedByStartupGrace()) return@handleWindowFocusChanged
                if (ExamModePolicy.shouldAutoSubmitOnFocusLoss(securityLevel, securityEnforcer.strictMode)) {
                    submissionManager.autoSubmitAndExit()
                }
            }
        }
    }

    override fun onKeyDown(keyCode: Int, event: KeyEvent?): Boolean {
        if (::securityEnforcer.isInitialized) {
            // Volume ditekan di medium/strict ATAU saat lock task aktif
            // (low+strict) — panel volume tidak boleh jadi jalur keluar/
            // bypass. Di low non-strict tombol volume berfungsi normal.
            // Fix review low-mode ronde 3 #1: kebijakan kini strict-aware.
            if (ExamModePolicy.shouldInterceptVolumeKeys(securityLevel, securityEnforcer.strictMode) &&
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
                // Fix review low-mode ronde 3 #2: long-press kini mengikuti
                // kebijakan yang sama dengan short-press — dulu SELALU
                // dikonsumsi tanpa melihat mode (di low, single-press normal
                // tapi long-press mati diam-diam).
                if (ExamModePolicy.shouldInterceptVolumeLongPress(securityLevel, securityEnforcer.strictMode)) {
                    return securityEnforcer.handleVolumeKeyLongPress()
                }
                return super.onKeyLongPress(keyCode, event)
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
        // Fix review gel. 2 #3: grace startup bertahan lintas rotasi.
        outState.putLong("pdfReadyAtMs", pdfReadyAtMs ?: -1L)
    }

    override fun onDestroy() {
        super.onDestroy()
        countDownTimer?.cancel()
        deadlineRunnable?.let { deadlineHandler?.removeCallbacks(it) }
        deadlineRunnable = null
        if (::pdfRendererHelper.isInitialized) pdfRendererHelper.cleanup()
        if (::securityEnforcer.isInitialized) securityEnforcer.cleanup()
        unregisterNetworkCallback()
        batteryReceiver?.let {
            try { unregisterReceiver(it) } catch (_: Exception) { }
        }
        batteryReceiver = null
        WebSocketManager.disconnect()
        AuditLog.reset()
    }

    override fun onRequestPermissionsResult(requestCode: Int, permissions: Array<String>, grantResults: IntArray) {
        super.onRequestPermissionsResult(requestCode, permissions, grantResults)
        if (requestCode == 1001) { // REQUEST_NOTIFICATION_PERMISSION
            // Fix temuan review mode medium #1: dialog sistem izin sudah
            // tertutup — lepas flag app-dialog agar jalur focus-loss kembali
            // aktif secara normal.
            if (::submissionManager.isInitialized) {
                submissionManager.onNotificationPermissionResult()
            }
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

    /** Getar pendek peringatan kritis (aman untuk semua API level). */
    private fun buzz(durationMs: Long) {
        val vibrator = getSystemService(android.content.Context.VIBRATOR_SERVICE) as? android.os.Vibrator ?: return
        try {
            if (android.os.Build.VERSION.SDK_INT >= android.os.Build.VERSION_CODES.O) {
                vibrator.vibrate(
                    android.os.VibrationEffect.createOneShot(
                        durationMs, android.os.VibrationEffect.DEFAULT_AMPLITUDE
                    )
                )
            } else {
                @Suppress("DEPRECATION")
                vibrator.vibrate(durationMs)
            }
        } catch (_: Exception) { }
    }

    // ===== Indikator baterai & koneksi (fitur baru) =====

    /**
     * Baca ulang status jaringan AKTIF dan perbarui indikator — dipakai
     * semua event callback agar multi-network tidak menandai offline palsu.
     */
    private fun refreshConnectionIndicator() {
        val cm = getSystemService(android.content.Context.CONNECTIVITY_SERVICE) as?
                android.net.ConnectivityManager
        val caps = cm?.getNetworkCapabilities(cm.activeNetwork)
        val online = com.examvan.app.helper.DeviceStatusPolicy.isOnlineFromActiveNetwork(
            activeHasInternet = caps?.hasCapability(
                android.net.NetworkCapabilities.NET_CAPABILITY_INTERNET
            ),
            activeValidated = caps?.hasCapability(
                android.net.NetworkCapabilities.NET_CAPABILITY_VALIDATED
            )
        )
        updateConnectionIndicator(online)
    }

    /**
     * Indikator koneksi: hijau "Online" / merah "Offline". Callback jaringan
     * bisa datang dari thread non-UI — selalu lewat runOnUiThread.
     */
    private fun updateConnectionIndicator(online: Boolean) {
        runOnUiThread {
            if (isFinishing || isDestroyed) return@runOnUiThread
            binding.tvConnectionStatus.text =
                getString(if (online) R.string.status_online else R.string.status_offline)
            binding.tvConnectionStatus.setTextColor(
                androidx.core.content.ContextCompat.getColor(
                    this,
                    if (online) R.color.success else R.color.danger
                )
            )
        }
    }

    /**
     * Receiver sticky ACTION_BATTERY_CHANGED — level & status charging
     * terkirim ulang setiap perubahan; keputusan kategori via
     * DeviceStatusPolicy.batteryCategory.
     */
    private fun registerBatteryIndicator() {
        val receiver = object : BroadcastReceiver() {
            override fun onReceive(ctx: android.content.Context?, intent: Intent?) {
                if (intent == null) return
                val level = intent.getIntExtra(BatteryManager.EXTRA_LEVEL, -1)
                val scale = intent.getIntExtra(BatteryManager.EXTRA_SCALE, 100)
                val status = intent.getIntExtra(BatteryManager.EXTRA_STATUS, -1)
                val charging = status == BatteryManager.BATTERY_STATUS_CHARGING ||
                        status == BatteryManager.BATTERY_STATUS_FULL
                if (scale <= 0 || level < 0) return
                updateBatteryIndicator((level * 100) / scale, charging)
            }
        }
        batteryReceiver = receiver
        registerReceiver(receiver, IntentFilter(Intent.ACTION_BATTERY_CHANGED))
    }

    private fun updateBatteryIndicator(percent: Int, charging: Boolean) {
        val category = com.examvan.app.helper.DeviceStatusPolicy.batteryCategory(percent)
        // Fix review UI/UX #1: label ringkas "45%" — warna sudah membawa
        // status; toolbar padat tidak boleh makin sesak.
        binding.tvBatteryStatus.text = if (charging) {
            getString(R.string.status_battery_charging)
        } else {
            getString(R.string.status_battery_percent, percent)
        }
        binding.tvBatteryStatus.setTextColor(
            androidx.core.content.ContextCompat.getColor(
                this,
                when (category) {
                    com.examvan.app.helper.DeviceStatusPolicy.BatteryLevel.CRITICAL -> R.color.danger
                    com.examvan.app.helper.DeviceStatusPolicy.BatteryLevel.LOW -> R.color.warning
                    com.examvan.app.helper.DeviceStatusPolicy.BatteryLevel.NORMAL ->
                        if (charging) R.color.success else R.color.text_secondary
                }
            )
        )
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
                // Fix review ronde 2 #1: pemformatan via string resource
                // (i18n); komponen jam/menit/detik dari object murni.
                val (h, m, s) = ExamDeadline.remainingHms(millisUntilFinished)
                binding.tvTimer.text = getString(R.string.timer_remaining, h, m, s)
                // Fix review UI/UX #2: merah bukan warna abadi — berjenjang
                // netral → kuning (<=10 menit) → merah (<=5 menit).
                val colorRes = when (ExamDeadline.timerUrgency(millisUntilFinished)) {
                    ExamDeadline.TimerUrgency.NORMAL -> R.color.on_surface
                    ExamDeadline.TimerUrgency.WARNING -> R.color.warning
                    ExamDeadline.TimerUrgency.CRITICAL -> R.color.timer_text
                }
                binding.tvTimer.setTextColor(
                    androidx.core.content.ContextCompat.getColor(this@ExamViewerActivity, colorRes)
                )
                val urgency = ExamDeadline.timerUrgency(millisUntilFinished)
                if (com.examvan.app.helper.HapticPolicy.shouldBuzz(lastTimerUrgency, urgency)) {
                    buzz(com.examvan.app.helper.HapticPolicy.BUZZ_DURATION_MS)
                }
                lastTimerUrgency = urgency
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
     *
     * CATATAN (review ronde 4 #2, perilaku disengaja): deadline TIDAK
     * mengecek flag/holder dialog aktif — bila siswa sedang di dialog
     * konfirmasi submit tepat saat waktu habis, auto-submit langsung
     * menuntaskan alur dan menutup activity beserta dialognya. Deadline
     * selalu mengalahkan interaksi apapun.
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

        binding.tvTimer.text = getString(R.string.timer_remaining, 0L, 0L, 0L)
        buzz(com.examvan.app.helper.HapticPolicy.BUZZ_DURATION_MS)
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
        com.examvan.app.helper.ExamScreenPanels.hideAllControls(binding)

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
        com.examvan.app.helper.ExamScreenPanels.hideAllControls(binding)

        binding.tvErrorMsg.text = "Ujian Sudah Selesai!\n\nAnda telah mengumpulkan jawaban untuk ujian ini. Terima kasih!"
        binding.btnRetryDownload.text = "Keluar"
        binding.btnRetryDownload.setOnClickListener {
            finish()
        }

        // "Buka Halaman Hasil" — juga tersedia saat membuka ulang ujian yang
        // sudah dikumpulkan. Fix temuan review: hasil dibuka di WebView
        // IN-APP (ResultsViewerActivity, FLAG_SECURE) — token tidak lagi
        // bocor ke history browser eksternal.
        binding.btnOpenResult.visibility = View.VISIBLE
        binding.btnOpenResult.setOnClickListener { openResultsInApp() }

        binding.layoutError.visibility = View.VISIBLE
    }

    /**
     * Buka halaman hasil di dalam app (WebView). Token tetap di sandbox app:
     * tidak masuk browser eksternal, history, atau Referrer header.
     */
    private fun openResultsInApp() {
        val url = com.examvan.app.helper.ResultsLinkPolicy.build(serverUrl, examToken)
        if (url.isEmpty()) {
            Toast.makeText(this, R.string.congrats_link_missing, Toast.LENGTH_SHORT).show()
            return
        }
        try {
            startActivity(
                Intent(this, ResultsViewerActivity::class.java)
                    .putExtra(ResultsViewerActivity.EXTRA_URL, url)
            )
        } catch (_: Exception) {
            Toast.makeText(this, R.string.congrats_link_missing, Toast.LENGTH_SHORT).show()
        }
    }

    private fun unregisterNetworkCallback() {
        try {
            val cm = getSystemService(android.content.Context.CONNECTIVITY_SERVICE) as? android.net.ConnectivityManager
            cm?.unregisterNetworkCallback(networkCallback)
        } catch (_: Exception) { }
    }
}
