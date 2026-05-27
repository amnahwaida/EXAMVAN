package com.examvan.app

import android.content.ClipboardManager
import android.content.Context
import android.content.Intent
import android.graphics.Bitmap
import android.graphics.Color
import android.graphics.pdf.PdfRenderer
import android.os.Bundle
import android.os.ParcelFileDescriptor
import android.view.LayoutInflater
import android.view.View
import android.view.WindowManager
import android.widget.*
import androidx.appcompat.app.AlertDialog
import androidx.appcompat.app.AppCompatActivity
import com.examvan.app.api.ApiClient
import com.examvan.app.databinding.ActivityExamViewerBinding
import com.google.gson.Gson
import com.google.gson.reflect.TypeToken
import okhttp3.Call
import java.io.File

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
    private var examName = ""
    private var studentName = ""
    private var studentNumber = ""
    private var studentClass = ""
    private var startTime = ""
    private var macAddress = ""

    // Questions config from server (received via token API response, stored in prefs as JSON)
    private var questions: List<Map<String, Any>> = emptyList()

    // Student answers: map of question number (String) -> answer value (String, List, or Map)
    private val studentAnswers = mutableMapOf<String, Any>()
    private var submittedOrExited = false
    private var securityLevel = "medium"
    private var isShowingAppDialog = false
    private var isSubmitting = false

    // Track active popup windows (Spinner dropdowns, etc.) to prevent false focus-loss detection
    private var activePopupCount = 0

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        // FLAG_SECURE: prevent screenshots & screen recording
        window.setFlags(
            WindowManager.LayoutParams.FLAG_SECURE,
            WindowManager.LayoutParams.FLAG_SECURE
        )
        // Keep screen turned on during the exam
        window.addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)

        // Clear clipboard
        val clipboard = getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager
        try { clipboard.clearPrimaryClip() } catch (_: Exception) { }

        binding = ActivityExamViewerBinding.inflate(layoutInflater)
        setContentView(binding.root)

        // Read intent extras
        examId = intent.getIntExtra("exam_id", -1)
        examName = intent.getStringExtra("exam_name") ?: "Ujian"
        studentName = intent.getStringExtra("student_name") ?: ""
        studentNumber = intent.getStringExtra("student_number") ?: ""
        studentClass = intent.getStringExtra("student_class") ?: ""

        // Record start time in UTC ISO 8601 format
        val df = java.text.SimpleDateFormat("yyyy-MM-dd'T'HH:mm:ss'Z'", java.util.Locale.US)
        df.timeZone = java.util.TimeZone.getTimeZone("UTC")
        startTime = df.format(java.util.Date())

        // Retrieve MAC address/Device ID
        macAddress = getDeviceMacAddress()

        binding.tvExamTitle.text = examName

        if (examId == -1) {
            showError("ID ujian tidak valid")
            return
        }

        // Back button (acted as Logout)
        binding.btnBack.setOnClickListener { confirmAndLogout() }

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
            downloadPdf(examId)
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
            binding.btnToggleAnswerSheet.text = if (answerSheetExpanded) "📝 Tutup Lembar Jawaban" else "📝 Buka Lembar Jawaban"
        }

        // Submit button
        binding.btnSubmitAnswers.setOnClickListener {
            confirmAndSubmit()
        }

        // Load questions from SharedPreferences (set by ServerConfigActivity after token response)
        loadQuestionsFromPrefs()


        // Start download
        downloadPdf(examId)
    }

    private fun loadQuestionsFromPrefs() {
        val prefs = getSharedPreferences("exam_questions", MODE_PRIVATE)
        val json = prefs.getString("questions_json", null)
        securityLevel = prefs.getString("security_level", "medium") ?: "medium"
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
            } catch (e: Exception) {
                // Fallback: generate 40 default MC questions
                generateDefaultQuestions()
            }
        } else {
            generateDefaultQuestions()
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

        label.text = "Soal $number"

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

        label.text = "Soal $number (Benar/Salah)"

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

        label.text = "Soal $number (Pilih beberapa)"

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

        label.text = "Soal $number (Menjodohkan)"

        val leftItems = (q["left_items"] as? List<*>)?.filterIsInstance<String>() ?: listOf("1", "2", "3")
        val rightItems = (q["right_items"] as? List<*>)?.filterIsInstance<String>() ?: listOf("A", "B", "C")

        val matchingAnswers = mutableMapOf<String, String>()

        for (leftItem in leftItems) {
            val rowView = LayoutInflater.from(this).inflate(R.layout.item_matching_row, matchingContainer, false)
            val tvLeft = rowView.findViewById<TextView>(R.id.tvLeftItem)
            val spinner = rowView.findViewById<Spinner>(R.id.spinnerRightItem)

            tvLeft.text = leftItem

            val spinnerItems = mutableListOf("-- Pilih --")
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

        label.text = "Soal $number (Isian Singkat)"

        // Restore answer if already filled
        val currentAns = studentAnswers[number.toString()] as? String
        if (currentAns != null) {
            editText.setText(currentAns)
        }

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
            "Anda baru menjawab $answered dari $total soal.\nYakin ingin mengumpulkan sekarang?"
        } else {
            "Anda sudah menjawab semua $total soal.\nKumpulkan jawaban?"
        }

        isShowingAppDialog = true
        AlertDialog.Builder(this)
            .setTitle("Kumpulkan Jawaban")
            .setMessage(message)
            .setPositiveButton("Ya, Kumpulkan") { _, _ ->
                isShowingAppDialog = false
                submitAnswers()
            }
            .setNegativeButton("Batal") { _, _ ->
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
        binding.btnSubmitAnswers.text = "Mengirim..."

        ApiClient.submitExam(
            examId = examId,
            studentName = studentName,
            examNumber = studentNumber,
            studentClass = studentClass,
            answers = studentAnswers,
            startTime = startTime,
            macAddress = macAddress,
            onSuccess = { message ->
                isSubmitting = false
                submittedOrExited = true
                runOnUiThread {
                    binding.btnSubmitAnswers.isEnabled = false
                    binding.btnSubmitAnswers.text = "✅ Sudah Dikumpulkan"

                    isShowingAppDialog = true
                    AlertDialog.Builder(this)
                        .setTitle("Berhasil")
                        .setMessage("$message\n\nNama: $studentName\nNomor: $studentNumber\nKelas: $studentClass")
                        .setCancelable(false)
                        .setPositiveButton("Selesai") { _, _ ->
                            isShowingAppDialog = false
                            finish()
                        }
                        .show()
                }
            },
            onError = { errorMsg ->
                isSubmitting = false
                runOnUiThread {
                    binding.btnSubmitAnswers.isEnabled = true
                    binding.btnSubmitAnswers.text = "📤 Kumpulkan Jawaban"

                    isShowingAppDialog = true
                    AlertDialog.Builder(this)
                        .setTitle("Gagal")
                        .setMessage(errorMsg)
                        .setPositiveButton("OK") { _, _ ->
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

    private fun downloadPdf(examId: Int) {
        showDownloading()

        // Check cache first
        val cachedFile = File(cacheDir, "exam_$examId.pdf")
        if (cachedFile.exists() && cachedFile.length() > 0) {
            binding.tvDownloadPercent.text = "100%"
            binding.progressDownload.progress = 100
            openPdf(cachedFile)
            return
        }

        downloadCall = ApiClient.downloadPdf(
            examId = examId,
            cacheDir = cacheDir,
            onProgress = { percent ->
                runOnUiThread {
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
                    openPdf(file)
                }
            },
            onError = { errorMsg ->
                runOnUiThread {
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
            pdfRenderer = PdfRenderer(fileDescriptor!!)
            totalPages = pdfRenderer!!.pageCount
            currentPage = 0
            renderPage(0)
            showPdfViewer()
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
        val display = "${currentPage + 1} / $totalPages"
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

    override fun onResume() {
        super.onResume()
    }

    override fun onPause() {
        super.onPause()
        if (submittedOrExited) return

        if (securityLevel == "medium") {
            autoSubmitAndExit()
        }
    }

    override fun onUserLeaveHint() {
        super.onUserLeaveHint()
        if (submittedOrExited) return

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

    @Deprecated("Deprecated in Java")
    override fun onBackPressed() {
        // Prevent default back button, show logout confirmation
        confirmAndLogout()
    }

    private fun confirmAndLogout() {
        val title: String
        val message: String
        val positiveButtonText: String

        if (securityLevel == "low") {
            title = "Keluar Ujian"
            message = "Apakah Anda yakin ingin keluar dari ujian?\n\nJawaban Anda TIDAK akan dikumpulkan secara otomatis (Anda dapat melanjutkan nanti)."
            positiveButtonText = "Ya, Keluar"
        } else {
            title = "Logout / Keluar Ujian"
            message = "Apakah Anda yakin ingin logout dan keluar dari ujian?\n\nJawaban yang sudah Anda isi akan dikumpulkan secara otomatis sebelum keluar."
            positiveButtonText = "Ya, Logout & Kirim"
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
            .setNegativeButton("Batal") { _, _ ->
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

        if (isSubmitting) {
            // Already submitting via normal route. Let the existing request finish.
            try {
                Thread.sleep(1000)
            } catch (_: Exception) {}
            finish()
            return
        }

        // Start synchronous submission in a background thread to block the main thread for a maximum of 2 seconds.
        // This keeps the process active and ensures the network request is fully sent before the OS suspends the app.
        val thread = Thread {
            val result = ApiClient.submitExamSync(
                examId = examId,
                studentName = studentName,
                examNumber = studentNumber,
                studentClass = studentClass,
                answers = studentAnswers,
                startTime = startTime,
                macAddress = macAddress
            )
            runOnUiThread {
                if (result.first) {
                    Toast.makeText(applicationContext, getString(R.string.toast_auto_submit_success), Toast.LENGTH_LONG).show()
                } else {
                    Toast.makeText(applicationContext, getString(R.string.toast_auto_submit_failed, result.second), Toast.LENGTH_LONG).show()
                }
            }
        }
        thread.start()
        try {
            // Block the main thread for up to 2.5 seconds to allow the request to finish before calling finish()
            thread.join(2500)
        } catch (_: Exception) {}

        finish()
    }

    override fun onDestroy() {
        super.onDestroy()
        downloadCall?.cancel()
        try {
            pdfRenderer?.close()
            fileDescriptor?.close()
        } catch (_: Exception) { }
    }

    private fun getDeviceMacAddress(): String {
        // 1. Try reading network interfaces for wlan0
        try {
            val interfaces = java.util.Collections.list(java.net.NetworkInterface.getNetworkInterfaces())
            for (networkInterface in interfaces) {
                if (networkInterface.name.equals("wlan0", ignoreCase = true)) {
                    val macBytes = networkInterface.hardwareAddress
                    if (macBytes != null) {
                        val res = StringBuilder()
                        for (b in macBytes) {
                            res.append(String.format("%02X:", b))
                        }
                        if (res.length > 0) {
                            res.deleteCharAt(res.length - 1)
                        }
                        val mac = res.toString()
                        if (mac.isNotEmpty() && !mac.equals("02:00:00:00:00:00", ignoreCase = true)) {
                            return mac
                        }
                    }
                }
            }
        } catch (_: Exception) {}

        // 2. Fallback to Settings.Secure.ANDROID_ID
        try {
            val androidId = android.provider.Settings.Secure.getString(contentResolver, android.provider.Settings.Secure.ANDROID_ID)
            if (!androidId.isNullOrEmpty()) {
                return "ID:$androidId"
            }
        } catch (_: Exception) {}

        return "UNKNOWN"
    }
}
