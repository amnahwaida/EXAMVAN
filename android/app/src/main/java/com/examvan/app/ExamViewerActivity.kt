package com.examvan.app

import android.Manifest
import android.app.NotificationChannel
import android.app.NotificationManager
import android.content.ClipboardManager
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.graphics.Bitmap
import android.graphics.Color
import android.graphics.pdf.PdfRenderer
import android.os.Build
import android.os.Bundle
import android.os.ParcelFileDescriptor
import android.util.Log
import android.view.KeyEvent
import android.view.LayoutInflater
import android.view.View
import android.view.WindowManager
import android.widget.*
import androidx.activity.OnBackPressedCallback
import androidx.appcompat.app.AlertDialog
import androidx.appcompat.app.AppCompatActivity
import androidx.core.app.ActivityCompat
import androidx.core.app.NotificationCompat
import androidx.core.content.ContextCompat
import com.examvan.app.api.ApiClient
import com.examvan.app.databinding.ActivityExamViewerBinding
import com.google.gson.Gson
import com.google.gson.reflect.TypeToken
import androidx.lifecycle.lifecycleScope
import androidx.security.crypto.EncryptedSharedPreferences
import androidx.security.crypto.MasterKey
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import okhttp3.Call
import java.io.File
import java.util.UUID

/**
 * Screen 3: Exam PDF Viewer + Digital Answer Sheet
 * - Downloads PDF with progress indicator
 * - Renders pages via PdfRenderer to Bitmap (no text layer = anti-copy)
 * - Prev/Next navigation with page counter and swipe gestures
 * - Collapsible digital answer sheet panel supporting:
 *   single_choice, multiple_choice, true_false, matching, short_answer
 * - Submit answers to server with student identity
 * - FLAG_SECURE active to prevent screenshots
 */
class ExamViewerActivity : AppCompatActivity() {

    private lateinit var binding: ActivityExamViewerBinding

    private var pdfRenderer: PdfRenderer? = null
    private var fileDescriptor: ParcelFileDescriptor? = null
    private var currentPage = 0
    private var totalPages = 0
    private var downloadCall: Call? = null
    private var answerSheetExpanded = false

    // Exam & student info from Intent
    private var examId = -1
    private var examToken = ""
    private var examName = ""
    private var studentName = ""
    private var studentNumber = ""
    private var studentClass = ""
    private var identityData: String? = null
    private var startTime = ""
    private var macAddress = ""

    // Questions config from server (received via token API response, stored in prefs as JSON)
    private var questions: List<Map<String, Any>> = emptyList()

    // Student answers: map of question number (String) -> answer value (String, List, or Map)
    private val studentAnswers = mutableMapOf<String, Any>()
    private var submittedOrExited = false
    private var securityLevel = "medium"
    private var strictMode = false
    private var isShowingAppDialog = false
    private var isSubmitting = false
    private var isPdfReady = false

    // Track active popup windows (Spinner dropdowns, etc.) to prevent false focus-loss detection
    private var activePopupCount = 0
    private var onCreateTime = 0L

    private val notificationChannelCreated: Boolean by lazy {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            val notificationManager = getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
            val channel = NotificationChannel(
                CHANNEL_ID,
                getString(R.string.notification_channel_name),
                NotificationManager.IMPORTANCE_HIGH
            ).apply {
                description = getString(R.string.notification_channel_desc)
                enableLights(true)
                enableVibration(true)
            }
            notificationManager.createNotificationChannel(channel)
        }
        true
    }

    companion object {
        private const val CHANNEL_ID = "examvan_auto_submit_v2"
        private const val REQUEST_NOTIFICATION_PERMISSION = 1001
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        onCreateTime = System.currentTimeMillis()

        // FLAG_SECURE: prevent screenshots & screen recording
        window.setFlags(
            WindowManager.LayoutParams.FLAG_SECURE,
            WindowManager.LayoutParams.FLAG_SECURE
        )
        // Keep screen turned on during the exam
        window.addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)

        // Clear clipboard
        try {
            val clipboard = getSystemService(Context.CLIPBOARD_SERVICE) as? ClipboardManager
            clipboard?.clearPrimaryClip()
        } catch (e: Throwable) {
            Log.w("ExamViewer", "Failed to clear clipboard", e)
        }

        // Request notification permission for Android 13+ (API 33+)
        // Without this runtime request, notifications are silently blocked
        if (Build.VERSION.SDK_INT >= 33) {
            if (ContextCompat.checkSelfPermission(this, Manifest.permission.POST_NOTIFICATIONS)
                != PackageManager.PERMISSION_GRANTED) {
                ActivityCompat.requestPermissions(this, arrayOf(Manifest.permission.POST_NOTIFICATIONS), REQUEST_NOTIFICATION_PERMISSION)
            }
        }

        binding = ActivityExamViewerBinding.inflate(layoutInflater)
        setContentView(binding.root)

        // Prevent overlay/tapjacking attacks
        binding.root.filterTouchesWhenObscured = true
        // Protect answer submission button
        binding.btnSubmitAnswers.filterTouchesWhenObscured = true

        // Read intent extras
        examId = intent.getIntExtra("exam_id", -1)
        examToken = intent.getStringExtra("exam_token") ?: ""
        examName = intent.getStringExtra("exam_name") ?: getString(R.string.default_exam_name)
        studentName = intent.getStringExtra("student_name") ?: ""
        studentNumber = intent.getStringExtra("student_number") ?: ""
        studentClass = intent.getStringExtra("student_class") ?: ""
        identityData = intent.getStringExtra("identity_data")
        strictMode = intent.getBooleanExtra("strict_mode", false)

        // In strict mode: enable Android Lock Task (screen pinning) to prevent leaving
        if (strictMode) {
            startLockTask()
        }

        // Record start time in UTC ISO 8601 format
        startTime = java.time.Instant.now().toString()

        // Retrieve MAC address/Device ID
        macAddress = resolveExamDeviceId()

        binding.tvExamTitle.text = examName

        if (examId == -1) {
            showError(getString(R.string.exam_invalid_id))
            return
        }

        onBackPressedDispatcher.addCallback(this, object : OnBackPressedCallback(true) {
            override fun handleOnBackPressed() {
                if (strictMode) {
                    Toast.makeText(this@ExamViewerActivity, getString(R.string.strict_mode_cannot_exit), Toast.LENGTH_SHORT).show()
                    return
                }
                confirmAndLogout()
            }
        })

        // Back button (acted as Logout, but blocked in strict mode)
        binding.btnBack.setOnClickListener {
            if (strictMode) {
                Toast.makeText(this, getString(R.string.strict_mode_cannot_exit), Toast.LENGTH_SHORT).show()
            } else {
                confirmAndLogout()
            }
        }

        // Navigation buttons
        binding.btnPrev.setOnClickListener {
            if (currentPage > 0) {
                currentPage--
                renderPage(currentPage)
            }
        }

        binding.btnNext.setOnClickListener {
            if (currentPage < totalPages - 1) {
                currentPage++
                renderPage(currentPage)
            }
        }

        // Retry button
        binding.btnRetryDownload.setOnClickListener {
            downloadPdf(examId, examToken)
        }

        binding.btnCancel.setOnClickListener {
            downloadCall?.cancel()
            submittedOrExited = true
            finish()
        }

        // Swipe gesture navigation for pages
        binding.ivPdfPage.swipeListener = object : com.examvan.app.view.ZoomableImageView.OnSwipeListener {
            override fun onSwipeLeft() {
                if (currentPage < totalPages - 1) {
                    currentPage++
                    renderPage(currentPage)
                }
            }

            override fun onSwipeRight() {
                if (currentPage > 0) {
                    currentPage--
                    renderPage(currentPage)
                }
            }
        }

        // Answer sheet toggle
        binding.btnToggleAnswerSheet.setOnClickListener {
            answerSheetExpanded = !answerSheetExpanded
            binding.answerSheetPanel.visibility = if (answerSheetExpanded) View.VISIBLE else View.GONE
            binding.btnToggleAnswerSheet.text = if (answerSheetExpanded) getString(R.string.answer_sheet_close) else getString(R.string.answer_sheet_open)
        }

        // Submit button
        binding.btnSubmitAnswers.setOnClickListener {
            confirmAndSubmit()
        }

        // Load questions from SharedPreferences (set by ServerConfigActivity after token response)
        try {
            loadQuestionsFromPrefs()
        } catch (e: Exception) {
            // Fallback: if questions fail to load, use defaults
            try { generateDefaultQuestions() } catch (e: Exception) { Log.w("ExamViewer", "Failed to load questions", e) }
        }

        // Start download
        try {
            downloadPdf(examId, examToken)
        } catch (e: Exception) {
            showError(getString(R.string.download_failed_format, e.message ?: ""))
        }
    }

    private fun loadQuestionsFromPrefs() {
        val prefs = androidx.security.crypto.EncryptedSharedPreferences.create(
            this,
            "exam_questions_encrypted",
            androidx.security.crypto.MasterKey.Builder(this)
                .setKeyScheme(androidx.security.crypto.MasterKey.KeyScheme.AES256_GCM)
                .build(),
            androidx.security.crypto.EncryptedSharedPreferences.PrefKeyEncryptionScheme.AES256_SIV,
            androidx.security.crypto.EncryptedSharedPreferences.PrefValueEncryptionScheme.AES256_GCM
        )
        val json = prefs.getString("questions_json", null)
        securityLevel = prefs.getString("security_level", "medium") ?: "medium"
        updateSecurityBanner()
        if (json != null) {
            try {
                val type = object : TypeToken<List<Map<String, Any>>>() {}.type
                questions = Gson().fromJson(json, type)
                if (questions.isEmpty()) {
                    // 0 questions configured: PDF-only mode, hide answer overlay
                    hideAnswerOverlay()
                } else {
                    buildAnswerSheet()
                }
            } catch (e: Throwable) {
                // Fallback: generate 40 default MC questions
                generateDefaultQuestions()
            }
        } else {
            generateDefaultQuestions()
        }
    }

    private fun updateSecurityBanner() {
        if (strictMode) {
            binding.tvSecurityBanner.text = getString(R.string.strict_mode_active)
            binding.tvSecurityBanner.setBackgroundColor(Color.parseColor("#B71C1C")) // Darker Red for strict
            binding.tvSecurityBanner.setTextColor(Color.parseColor("#FFFFFF"))
        } else if (securityLevel == "medium") {
            binding.tvSecurityBanner.text = getString(R.string.autosubmit_status_active)
            binding.tvSecurityBanner.setBackgroundColor(Color.parseColor("#D32F2F")) // Warning Red
            binding.tvSecurityBanner.setTextColor(Color.parseColor("#FFFFFF"))
        } else {
            binding.tvSecurityBanner.text = getString(R.string.autosubmit_status_inactive)
            binding.tvSecurityBanner.setBackgroundColor(Color.parseColor("#455A64")) // Cool Dark Blue Grey
            binding.tvSecurityBanner.setTextColor(Color.parseColor("#FFFFFF"))
        }
    }

    private fun hideAnswerOverlay() {
        binding.answerSheetToggle.visibility = View.GONE
        binding.answerSheetPanel.visibility = View.GONE
        binding.btnSubmitAnswers.visibility = View.GONE
    }

    private fun generateDefaultQuestions() {
        val defaultList = mutableListOf<Map<String, Any>>()
        for (i in 1..40) {
            defaultList.add(mapOf(
                "number" to i.toDouble(),
                "type" to "single_choice",
                "choices" to listOf("A", "B", "C", "D", "E")
            ))
        }
        questions = defaultList
        buildAnswerSheet()
    }

    private fun buildAnswerSheet() {
        val container = binding.answerListContainer
        container.removeAllViews()

        for (q in questions) {
            val number = (q["number"] as? Double)?.toInt() ?: continue
            val type = q["type"] as? String ?: "single_choice"

            when (type) {
                "single_choice" -> addSingleChoiceQuestion(container, number, q)
                "true_false" -> addTrueFalseQuestion(container, number)
                "multiple_choice" -> addMultipleChoiceQuestion(container, number, q)
                "matching" -> addMatchingQuestion(container, number, q)
                "short_answer" -> addShortAnswerQuestion(container, number)
            }
        }
    }

    @Suppress("UNCHECKED_CAST")
    private fun addSingleChoiceQuestion(container: LinearLayout, number: Int, q: Map<String, Any>) {
        val view = LayoutInflater.from(this).inflate(R.layout.item_question_choice, container, false)
        val label = view.findViewById<TextView>(R.id.tvQuestionLabel)
        val radioGroup = view.findViewById<RadioGroup>(R.id.rgChoices)
        val checkboxLayout = view.findViewById<LinearLayout>(R.id.layoutCheckboxes)
        checkboxLayout.visibility = View.GONE
        radioGroup.visibility = View.VISIBLE

        label.text = getString(R.string.question_label, number)

        val choices = (q["choices"] as? List<*>)?.filterIsInstance<String>() ?: listOf("A", "B", "C", "D", "E")

        for (choice in choices) {
            val rb = RadioButton(this).apply {
                text = choice
                setTextColor(androidx.core.content.ContextCompat.getColor(this@ExamViewerActivity, R.color.on_surface))
                buttonTintList = androidx.core.content.ContextCompat.getColorStateList(this@ExamViewerActivity, R.color.primary)
                textSize = 14f
                setPadding(4, 0, 16, 0)
            }
            radioGroup.addView(rb)
        }

        radioGroup.setOnCheckedChangeListener { group, checkedId ->
            val rb = group.findViewById<RadioButton>(checkedId)
            if (rb != null) {
                studentAnswers[number.toString()] = rb.text.toString()
            }
        }

        container.addView(view)
    }

    private fun addTrueFalseQuestion(container: LinearLayout, number: Int) {
        val view = LayoutInflater.from(this).inflate(R.layout.item_question_choice, container, false)
        val label = view.findViewById<TextView>(R.id.tvQuestionLabel)
        val radioGroup = view.findViewById<RadioGroup>(R.id.rgChoices)
        val checkboxLayout = view.findViewById<LinearLayout>(R.id.layoutCheckboxes)
        checkboxLayout.visibility = View.GONE
        radioGroup.visibility = View.VISIBLE

        label.text = getString(R.string.question_label_truefalse, number)

        for (choice in listOf("TRUE", "FALSE")) {
            val rb = RadioButton(this).apply {
                text = choice
                setTextColor(androidx.core.content.ContextCompat.getColor(this@ExamViewerActivity, R.color.on_surface))
                buttonTintList = androidx.core.content.ContextCompat.getColorStateList(this@ExamViewerActivity, R.color.primary)
                textSize = 14f
                setPadding(4, 0, 16, 0)
            }
            radioGroup.addView(rb)
        }

        radioGroup.setOnCheckedChangeListener { group, checkedId ->
            val rb = group.findViewById<RadioButton>(checkedId)
            if (rb != null) {
                studentAnswers[number.toString()] = rb.text.toString()
            }
        }

        container.addView(view)
    }

    @Suppress("UNCHECKED_CAST")
    private fun addMultipleChoiceQuestion(container: LinearLayout, number: Int, q: Map<String, Any>) {
        val view = LayoutInflater.from(this).inflate(R.layout.item_question_choice, container, false)
        val label = view.findViewById<TextView>(R.id.tvQuestionLabel)
        val radioGroup = view.findViewById<RadioGroup>(R.id.rgChoices)
        val checkboxLayout = view.findViewById<LinearLayout>(R.id.layoutCheckboxes)
        radioGroup.visibility = View.GONE
        checkboxLayout.visibility = View.VISIBLE

        label.text = getString(R.string.question_label_multiple, number)

        val choices = (q["choices"] as? List<*>)?.filterIsInstance<String>() ?: listOf("A", "B", "C", "D", "E")

        for (choice in choices) {
            val cb = CheckBox(this).apply {
                text = choice
                setTextColor(androidx.core.content.ContextCompat.getColor(this@ExamViewerActivity, R.color.on_surface))
                buttonTintList = androidx.core.content.ContextCompat.getColorStateList(this@ExamViewerActivity, R.color.primary)
                textSize = 14f
                setPadding(4, 0, 16, 0)
            }

            cb.setOnCheckedChangeListener { _, _ ->
                // Collect all checked
                val selected = mutableListOf<String>()
                for (i in 0 until checkboxLayout.childCount) {
                    val child = checkboxLayout.getChildAt(i) as? CheckBox
                    if (child?.isChecked == true) {
                        selected.add(child.text.toString())
                    }
                }
                studentAnswers[number.toString()] = selected
            }

            checkboxLayout.addView(cb)
        }

        container.addView(view)
    }

    @Suppress("UNCHECKED_CAST")
    private fun addMatchingQuestion(container: LinearLayout, number: Int, q: Map<String, Any>) {
        val view = LayoutInflater.from(this).inflate(R.layout.item_question_matching, container, false)
        val label = view.findViewById<TextView>(R.id.tvMatchingLabel)
        val matchingContainer = view.findViewById<LinearLayout>(R.id.layoutMatchingContainer)

        label.text = getString(R.string.question_label_matching, number)

        val leftItems = (q["left_items"] as? List<*>)?.filterIsInstance<String>() ?: listOf("1", "2", "3")
        val rightItems = (q["right_items"] as? List<*>)?.filterIsInstance<String>() ?: listOf("A", "B", "C")

        val matchingAnswers = mutableMapOf<String, String>()

        for (leftItem in leftItems) {
            val rowView = LayoutInflater.from(this).inflate(R.layout.item_matching_row, matchingContainer, false)
            val tvLeft = rowView.findViewById<TextView>(R.id.tvLeftItem)
            val spinner = rowView.findViewById<Spinner>(R.id.spinnerRightItem)

            tvLeft.text = leftItem

            val spinnerItems = mutableListOf(getString(R.string.spinner_default))
            spinnerItems.addAll(rightItems)

            val adapter = ArrayAdapter(this, android.R.layout.simple_spinner_item, spinnerItems)
            adapter.setDropDownViewResource(android.R.layout.simple_spinner_dropdown_item)
            spinner.adapter = adapter

            // Track spinner popup open/close to prevent false focus-loss detection.
            // When a Spinner dropdown opens, it creates a popup window that triggers
            // onWindowFocusChanged(false), which was incorrectly interpreted as the user
            // leaving the app, causing auto-submit or force-return loops.
            spinner.setOnTouchListener { _, event ->
                if (event.action == android.view.MotionEvent.ACTION_UP) {
                    activePopupCount++
                }
                false
            }

            spinner.onItemSelectedListener = object : AdapterView.OnItemSelectedListener {
                override fun onItemSelected(parent: AdapterView<*>?, v: View?, position: Int, id: Long) {
                    // Decrement popup count when selection is made (dropdown closed)
                    if (activePopupCount > 0) activePopupCount--
                    if (position > 0) {
                        matchingAnswers[leftItem] = rightItems[position - 1]
                    } else {
                        matchingAnswers.remove(leftItem)
                    }
                    studentAnswers[number.toString()] = HashMap(matchingAnswers)
                }
                override fun onNothingSelected(parent: AdapterView<*>?) {
                    if (activePopupCount > 0) activePopupCount--
                }
            }

            matchingContainer.addView(rowView)
        }

        container.addView(view)
    }

    private fun addShortAnswerQuestion(container: LinearLayout, number: Int) {
        val view = LayoutInflater.from(this).inflate(R.layout.item_question_short_answer, container, false)
        val label = view.findViewById<TextView>(R.id.tvQuestionLabel)
        val editText = view.findViewById<EditText>(R.id.etShortAnswer)

        label.text = getString(R.string.question_label_shortanswer, number)

        // Restore answer if already filled
        val currentAns = studentAnswers[number.toString()] as? String
        if (currentAns != null) {
            editText.setText(currentAns)
        }

        editText.filters = arrayOf(android.text.InputFilter.LengthFilter(500))

        editText.addTextChangedListener(object : android.text.TextWatcher {
            override fun beforeTextChanged(s: CharSequence?, start: Int, count: Int, after: Int) {}
            override fun onTextChanged(s: CharSequence?, start: Int, before: Int, count: Int) {}
            override fun afterTextChanged(s: android.text.Editable?) {
                val ans = s?.toString()?.trim() ?: ""
                if (ans.isNotEmpty()) {
                    studentAnswers[number.toString()] = ans
                } else {
                    studentAnswers.remove(number.toString())
                }
            }
        })

        container.addView(view)
    }

    private fun confirmAndSubmit() {
        // Count answered questions
        val answered = studentAnswers.size
        val total = questions.size

        val message = if (answered < total) {
            getString(R.string.submit_answers_confirm_partial, answered, total)
        } else {
            getString(R.string.submit_answers_confirm_all, total)
        }

        isShowingAppDialog = true
        AlertDialog.Builder(this)
            .setTitle(getString(R.string.submit_answers_title))
            .setMessage(message)
            .setPositiveButton(getString(R.string.submit_confirm_yes)) { _, _ ->
                isShowingAppDialog = false
                submitAnswers()
            }
            .setNegativeButton(getString(R.string.btn_cancel)) { _, _ ->
                isShowingAppDialog = false
            }
            .setOnCancelListener {
                isShowingAppDialog = false
            }
            .show()
    }

    private fun submitAnswers() {
        if (isSubmitting) return
        isSubmitting = true
        binding.btnSubmitAnswers.isEnabled = false
        binding.btnSubmitAnswers.text = getString(R.string.submitting)

        ApiClient.submitExam(
            examId = examId,
            studentName = studentName,
            examNumber = studentNumber,
            studentClass = studentClass,
            answers = studentAnswers,
            startTime = startTime,
            macAddress = macAddress,
            identityData = identityData,
            onSuccess = { message ->
                isSubmitting = false
                submittedOrExited = true
                // Stop lock task (screen pinning) if strict mode was enabled
                if (strictMode) {
                    try { stopLockTask() } catch (_: Throwable) { }
                }
                runOnUiThread {
                    if (isFinishing || isDestroyed) return@runOnUiThread
                    binding.btnSubmitAnswers.isEnabled = false
                    binding.btnSubmitAnswers.text = getString(R.string.submitted_label)

                    isShowingAppDialog = true
                    AlertDialog.Builder(this)
                        .setTitle(getString(R.string.submit_success_title))
                        .setMessage(getString(R.string.submit_success_message, message, studentName, studentNumber, studentClass))
                        .setCancelable(false)
                        .setPositiveButton(getString(R.string.submit_success_done)) { _, _ ->
                            isShowingAppDialog = false
                            finish()
                        }
                        .show()
                }
            },
            onError = { errorMsg ->
                isSubmitting = false
                runOnUiThread {
                    if (isFinishing || isDestroyed) return@runOnUiThread
                    binding.btnSubmitAnswers.isEnabled = true
                    binding.btnSubmitAnswers.text = getString(R.string.submit_failed_retry)

                    val dialogTitle = if (strictMode) getString(R.string.submit_failed_title_strict) else getString(R.string.submit_failed_title)
                    val dialogMsg = if (strictMode) getString(R.string.submit_failed_message_strict, errorMsg) else getString(R.string.submit_failed_message, errorMsg)
                    isShowingAppDialog = true
                    AlertDialog.Builder(this)
                        .setTitle(dialogTitle)
                        .setMessage(dialogMsg)
                        .setPositiveButton(getString(R.string.dialog_ok)) { _, _ ->
                            isShowingAppDialog = false
                        }
                        .setOnCancelListener {
                            isShowingAppDialog = false
                        }
                        .show()
                }
            }
        )
    }

    private fun downloadPdf(examId: Int, token: String = "") {
        showDownloading()

        // Check cache first
        val cachedFile = File(cacheDir, "exam_$examId.pdf")
        if (cachedFile.exists() && cachedFile.length() > 0) {
            binding.tvDownloadPercent.text = "100%"
            binding.progressDownload.progress = 100
            isPdfReady = true
            openPdf(cachedFile)
            return
        }

        downloadCall = ApiClient.downloadPdf(
            examId = examId,
            token = token,
            cacheDir = cacheDir,
            onProgress = { percent ->
                runOnUiThread {
                    if (isFinishing || isDestroyed) return@runOnUiThread
                    binding.progressDownload.progress = percent
                    binding.tvDownloadPercent.text = "$percent%"

                    // Show cancel button if download takes long
                    if (percent < 50) {
                        binding.btnCancel.visibility = View.VISIBLE
                    }
                }
            },
            onSuccess = { file ->
                runOnUiThread {
                    if (isFinishing || isDestroyed) return@runOnUiThread
                    isPdfReady = true
                    openPdf(file)
                }
            },
            onError = { errorMsg ->
                runOnUiThread {
                    if (isFinishing || isDestroyed) return@runOnUiThread
                    showError(errorMsg)
                }
            }
        )
    }

    private fun openPdf(file: File) {
        try {
            fileDescriptor = ParcelFileDescriptor.open(
                file, ParcelFileDescriptor.MODE_READ_ONLY
            )
            val fd = fileDescriptor ?: run { showError(getString(R.string.error_pdf)); return }
            pdfRenderer = PdfRenderer(fd)
            val renderer = pdfRenderer ?: run { showError(getString(R.string.error_pdf_render)); return }
            totalPages = renderer.pageCount
            currentPage = 0
            renderPage(0)
            showPdfViewer()
            isPdfReady = true // Crucial: ensures isPdfReady is true for cached files as well
        } catch (e: Exception) {
            showError(getString(R.string.error_pdf) + ": ${e.message}")
        }
    }

    private var currentBitmap: Bitmap? = null

    private fun renderPage(pageIndex: Int) {
        val renderer = pdfRenderer ?: return
        if (pageIndex < 0 || pageIndex >= renderer.pageCount) return

        val page = renderer.openPage(pageIndex)

        // Dynamically calculate scale to prevent OutOfMemoryError on large/scanned pages.
        // We target 2x the device's screen width for perfect clarity, capped at a safe maximum of 2048 pixels.
        val screenWidth = resources.displayMetrics.widthPixels
        var targetWidth = (screenWidth * 2).coerceAtMost(2048)
        
        // If the original page is smaller than the target, don't upscale it beyond 2x its original size
        targetWidth = targetWidth.coerceAtMost(page.width * 2)
        
        // Calculate proportional height to keep the original aspect ratio
        val aspectRatio = page.height.toFloat() / page.width.toFloat()
        val targetHeight = (targetWidth * aspectRatio).toInt()

        val bitmap = Bitmap.createBitmap(
            targetWidth,
            targetHeight,
            Bitmap.Config.ARGB_8888
        )
        bitmap.eraseColor(Color.WHITE)

        page.render(
            bitmap,
            null,
            null,
            PdfRenderer.Page.RENDER_MODE_FOR_DISPLAY
        )
        page.close()

        // Recycle previous bitmap to free memory immediately (important for low-end devices)
        val oldBitmap = currentBitmap
        currentBitmap = bitmap
        binding.ivPdfPage.resetZoom()
        binding.ivPdfPage.setImageBitmap(bitmap)
        oldBitmap?.recycle()

        updatePageIndicator()
    }

    private fun updatePageIndicator() {
        val display = getString(R.string.page_indicator_format, currentPage + 1, totalPages)
        binding.tvPageIndicator.text = display
        binding.tvPageCounter.text = display

        binding.btnPrev.isEnabled = currentPage > 0
        binding.btnNext.isEnabled = currentPage < totalPages - 1
    }

    private fun showDownloading() {
        binding.layoutDownload.visibility = View.VISIBLE
        binding.layoutError.visibility = View.GONE
        binding.ivPdfPage.visibility = View.GONE
        binding.btnCancel.visibility = View.GONE
        binding.progressDownload.progress = 0
        binding.tvDownloadPercent.text = "0%"
    }

    private fun showPdfViewer() {
        binding.layoutDownload.visibility = View.GONE
        binding.layoutError.visibility = View.GONE
        binding.ivPdfPage.visibility = View.VISIBLE
    }

    private fun showError(message: String) {
        binding.layoutDownload.visibility = View.GONE
        binding.layoutError.visibility = View.VISIBLE
        binding.ivPdfPage.visibility = View.GONE
        binding.tvErrorMsg.text = message
    }

    override fun onRequestPermissionsResult(requestCode: Int, permissions: Array<String>, grantResults: IntArray) {
        super.onRequestPermissionsResult(requestCode, permissions, grantResults)
        if (requestCode == REQUEST_NOTIFICATION_PERMISSION) {
            // Notification permission result — auto-submit will use Toast fallback if denied
            val granted = grantResults.isNotEmpty() && grantResults[0] == PackageManager.PERMISSION_GRANTED
            Log.d("ExamViewer", "Notification permission ${if (granted) "granted" else "denied"}")
        }
    }

    override fun onPause() {
        super.onPause()
        // NOTE: onPause() fires for many non-exit scenarios (system dialogs, notification shade,
        // multi-window, screen off), so we do NOT auto-submit here to avoid false positives.
        // Auto-submit on user-initiated exit is handled in onUserLeaveHint() below.
        // For securityLevel "high" / strictMode, the Lock Task (screen pinning) prevents
        // the user from leaving the app entirely — they must submit to exit.
    }

    override fun onUserLeaveHint() {
        super.onUserLeaveHint()
        if (submittedOrExited) return
        if (!isPdfReady) return
        if (System.currentTimeMillis() - onCreateTime < 3000) return

        // onUserLeaveHint() fires ONLY when the user intentionally navigates away
        // (Home button, Recent Apps, or a new Activity starting).
        // This is the correct signal for auto-submit on securityLevel "medium".
        if (securityLevel == "medium") {
            autoSubmitAndExit()
        }
    }

    override fun onWindowFocusChanged(hasFocus: Boolean) {
        super.onWindowFocusChanged(hasFocus)
        if (hasFocus) {
            activePopupCount = 0
        }
    }

    override fun onKeyDown(keyCode: Int, event: KeyEvent?): Boolean {
        if (keyCode == KeyEvent.KEYCODE_VOLUME_UP || keyCode == KeyEvent.KEYCODE_VOLUME_DOWN) {
            // Adjust volume programmatically without showing the system overlay UI.
            // This prevents the system volume panel from triggering a false onWindowFocusChanged(false) anti-cheat submission.
            try {
                val audioManager = getSystemService(Context.AUDIO_SERVICE) as android.media.AudioManager
                val direction = if (keyCode == KeyEvent.KEYCODE_VOLUME_UP) {
                    android.media.AudioManager.ADJUST_RAISE
                } else {
                    android.media.AudioManager.ADJUST_LOWER
                }
                audioManager.adjustStreamVolume(android.media.AudioManager.STREAM_MUSIC, direction, 0) // 0 suppresses UI
            } catch (e: Throwable) { Log.w("ExamViewer", "Volume adjustment failed", e); }
            return true
        }
        return super.onKeyDown(keyCode, event)
    }

    private fun confirmAndLogout() {
        // In strict mode, user cannot exit — they must submit answers first
        if (strictMode) {
            isShowingAppDialog = true
            AlertDialog.Builder(this)
                .setTitle(getString(R.string.strict_mode_cannot_exit_title))
                .setMessage(getString(R.string.strict_mode_cannot_exit_msg))
                .setPositiveButton(getString(R.string.dialog_ok)) { _, _ ->
                    isShowingAppDialog = false
                }
                .setOnCancelListener {
                    isShowingAppDialog = false
                }
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

        isShowingAppDialog = true
        AlertDialog.Builder(this)
            .setTitle(title)
            .setMessage(message)
            .setPositiveButton(positiveButtonText) { _, _ ->
                isShowingAppDialog = false
                if (securityLevel == "low") {
                    // Just exit without auto-submitting
                    submittedOrExited = true
                    finish()
                } else {
                    autoSubmitAndExit()
                }
            }
            .setNegativeButton(getString(R.string.btn_cancel)) { _, _ ->
                isShowingAppDialog = false
            }
            .setOnCancelListener {
                isShowingAppDialog = false
            }
            .show()
    }

    private fun autoSubmitAndExit() {
        if (submittedOrExited) return
        submittedOrExited = true

        // Stop lock task if strict mode was enabled (auto-submit == exit)
        if (strictMode) {
            try { stopLockTask() } catch (_: Throwable) { }
        }

        if (isSubmitting) {
            // Already submitting via normal route. Let the existing request finish.
            lifecycleScope.launch {
                delay(1500)
                if (!isFinishing) finish()
            }
            return
        }

        // Submit synchronously in a background coroutine, then post result to main thread.
        lifecycleScope.launch(Dispatchers.IO) {
            val result = try {
                ApiClient.submitExamSync(
                    examId = examId,
                    studentName = studentName,
                    examNumber = studentNumber,
                    studentClass = studentClass,
                    answers = studentAnswers,
                    startTime = startTime,
                    macAddress = macAddress,
                    identityData = identityData
                )
            } catch (_: Throwable) {
                Pair(false, getString(R.string.answer_submit_error))
            }

            val notifTitle = if (result.first) getString(R.string.auto_submit_success_title) else getString(R.string.auto_submit_failed_title)
            val notifMessage = if (result.first) {
                getString(R.string.toast_auto_submit_success)
            } else {
                getString(R.string.toast_auto_submit_failed, result.second)
            }

            withContext(Dispatchers.Main) {
                showAutoSubmitNotification(notifTitle, notifMessage)
                // Small delay to let the OS register the notification
                delay(400)
                if (!isFinishing) finish()
            }
        }
    }

    private fun showAutoSubmitNotification(title: String, message: String) {
        try {
            val notificationManager = getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager

            // Ensure notification channel is created (lazy init happens on first access)
            notificationChannelCreated

            val notification = NotificationCompat.Builder(applicationContext, CHANNEL_ID)
                .setSmallIcon(R.mipmap.ic_launcher)
                .setContentTitle(title)
                .setContentText(message)
                .setStyle(NotificationCompat.BigTextStyle().bigText(message))
                .setPriority(NotificationCompat.PRIORITY_HIGH)
                .setDefaults(NotificationCompat.DEFAULT_ALL)
                .setAutoCancel(true)
                .build()

            // Use a unique notification ID every time to prevent the OS from grouping or silencing subsequent notifications
            val uniqueNotifId = (System.currentTimeMillis() % 100000).toInt()
            notificationManager.notify(uniqueNotifId, notification)
        } catch (_: Throwable) {
            // Fallback to Toast if notification fails
            runOnUiThread {
                if (isFinishing || isDestroyed) return@runOnUiThread
                Toast.makeText(applicationContext, message, Toast.LENGTH_LONG).show()
            }
        }
    }

    override fun onDestroy() {
        super.onDestroy()
        downloadCall?.cancel()
        binding.ivPdfPage.swipeListener = null
        try {
            pdfRenderer?.close()
            fileDescriptor?.close()
        } catch (_: Exception) { }
    }

    private fun resolveExamDeviceId(): String {
        val prefs = androidx.security.crypto.EncryptedSharedPreferences.create(
            this,
            "device_id_encrypted",
            androidx.security.crypto.MasterKey.Builder(this)
                .setKeyScheme(androidx.security.crypto.MasterKey.KeyScheme.AES256_GCM)
                .build(),
            androidx.security.crypto.EncryptedSharedPreferences.PrefKeyEncryptionScheme.AES256_SIV,
            androidx.security.crypto.EncryptedSharedPreferences.PrefValueEncryptionScheme.AES256_GCM
        )
        var deviceId = prefs.getString("device_uuid", null)
        if (deviceId.isNullOrBlank()) {
            deviceId = UUID.randomUUID().toString()
            prefs.edit().putString("device_uuid", deviceId).apply()
        }
        return "DEVICE:$deviceId"
    }
}
