package com.examvan.app

import android.content.ClipboardManager
import android.content.Context
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
 *   single_choice, multiple_choice, true_false, matching
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

    // Questions config from server (received via token API response, stored in prefs as JSON)
    private var questions: List<Map<String, Any>> = emptyList()

    // Student answers: map of question number (String) -> answer value (String, List, or Map)
    private val studentAnswers = mutableMapOf<String, Any>()

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        // FLAG_SECURE: prevent screenshots & screen recording
        window.setFlags(
            WindowManager.LayoutParams.FLAG_SECURE,
            WindowManager.LayoutParams.FLAG_SECURE
        )

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

        binding.tvExamTitle.text = examName

        if (examId == -1) {
            showError("ID ujian tidak valid")
            return
        }

        // Back button
        binding.btnBack.setOnClickListener { finish() }

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

        // Cancel button
        binding.btnCancel.setOnClickListener {
            downloadCall?.cancel()
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
        binding.btnToggleAnswerSheet.visibility = View.GONE
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
                setTextColor(resources.getColor(R.color.on_surface, null))
                buttonTintList = resources.getColorStateList(R.color.primary, null)
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
                setTextColor(resources.getColor(R.color.on_surface, null))
                buttonTintList = resources.getColorStateList(R.color.primary, null)
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
                setTextColor(resources.getColor(R.color.on_surface, null))
                buttonTintList = resources.getColorStateList(R.color.primary, null)
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

            spinner.onItemSelectedListener = object : AdapterView.OnItemSelectedListener {
                override fun onItemSelected(parent: AdapterView<*>?, v: View?, position: Int, id: Long) {
                    if (position > 0) {
                        matchingAnswers[leftItem] = rightItems[position - 1]
                    } else {
                        matchingAnswers.remove(leftItem)
                    }
                    studentAnswers[number.toString()] = HashMap(matchingAnswers)
                }
                override fun onNothingSelected(parent: AdapterView<*>?) {}
            }

            matchingContainer.addView(rowView)
        }

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

        AlertDialog.Builder(this)
            .setTitle("Kumpulkan Jawaban")
            .setMessage(message)
            .setPositiveButton("Ya, Kumpulkan") { _, _ ->
                submitAnswers()
            }
            .setNegativeButton("Batal", null)
            .show()
    }

    private fun submitAnswers() {
        binding.btnSubmitAnswers.isEnabled = false
        binding.btnSubmitAnswers.text = "Mengirim..."

        ApiClient.submitExam(
            examId = examId,
            studentName = studentName,
            examNumber = studentNumber,
            studentClass = studentClass,
            answers = studentAnswers,
            onSuccess = { message ->
                runOnUiThread {
                    binding.btnSubmitAnswers.isEnabled = false
                    binding.btnSubmitAnswers.text = "✅ Sudah Dikumpulkan"

                    AlertDialog.Builder(this)
                        .setTitle("Berhasil")
                        .setMessage("$message\n\nNama: $studentName\nNomor: $studentNumber\nKelas: $studentClass")
                        .setCancelable(false)
                        .setPositiveButton("Selesai") { _, _ ->
                            finish()
                        }
                        .show()
                }
            },
            onError = { errorMsg ->
                runOnUiThread {
                    binding.btnSubmitAnswers.isEnabled = true
                    binding.btnSubmitAnswers.text = "📤 Kumpulkan Jawaban"

                    AlertDialog.Builder(this)
                        .setTitle("Gagal")
                        .setMessage(errorMsg)
                        .setPositiveButton("OK", null)
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

    private fun renderPage(pageIndex: Int) {
        val renderer = pdfRenderer ?: return
        if (pageIndex < 0 || pageIndex >= renderer.pageCount) return

        val page = renderer.openPage(pageIndex)

        // Render at 2x density for clarity
        val scale = 2
        val bitmap = Bitmap.createBitmap(
            page.width * scale,
            page.height * scale,
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

        binding.ivPdfPage.setImageBitmap(bitmap)
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

    override fun onDestroy() {
        super.onDestroy()
        downloadCall?.cancel()
        try {
            pdfRenderer?.close()
            fileDescriptor?.close()
        } catch (_: Exception) { }
    }
}
