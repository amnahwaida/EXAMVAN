package com.examvan.app

import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.content.Intent
import android.os.Bundle
import android.widget.LinearLayout
import android.widget.TextView
import android.widget.Toast
import androidx.core.content.ContextCompat
import com.examvan.app.databinding.ActivityCongratulationsBinding

/**
 * Halaman Ucapan Selamat (setelah siswa mengumpulkan jawaban)
 *
 * Ditampilkan setelah submit berhasil di ExamViewerActivity. Menampilkan
 * pesan ucapan selamat yang bisa di-custom oleh guru (congrats_message dari
 * server, fallback ke pesan bawaan), info siswa, serta tombol untuk menyalin
 * link hasil dan membukanya di browser eksternal.
 */
class CongratulationsActivity : BaseSecureActivity() {

    private lateinit var binding: ActivityCongratulationsBinding

    // Data passed via Intent
    private var serverUrl: String = ""
    private var examToken: String = ""
    private var examName: String = ""
    private var studentName: String = ""
    private var studentNumber: String = ""
    private var studentClass: String = ""

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        binding = ActivityCongratulationsBinding.inflate(layoutInflater)
        setContentView(binding.root)
        applyEdgeToEdgeInsets(binding.rootLayout)

        // Extract intent data
        serverUrl = intent.getStringExtra("server_url") ?: ""
        examToken = intent.getStringExtra("exam_token") ?: ""
        examName = intent.getStringExtra("exam_name") ?: ""
        studentName = intent.getStringExtra("student_name") ?: ""
        studentNumber = intent.getStringExtra("student_number") ?: ""
        studentClass = intent.getStringExtra("student_class") ?: ""
        val congratsMessage = intent.getStringExtra("congrats_message")

        // Custom message from server, fallback to the app default
        binding.tvCongratsMessage.text = congratsMessage
            ?.takeIf { it.isNotBlank() }
            ?: getString(R.string.congrats_default_message)

        // Exam name badge
        binding.tvExamName.text = examName.ifEmpty { getString(R.string.default_exam_name) }

        // Student identity rows
        populateIdentity()

        // Result link = short-link {serverUrl}/{examToken} → redirects to /hasil/<token>
        // Fix temuan review: format URL kini terpusat di ResultsLinkPolicy.
        val resultUrl = com.examvan.app.helper.ResultsLinkPolicy.build(serverUrl, examToken)

        binding.btnCopyLink.setOnClickListener {
            copyResultLink(resultUrl)
        }
        binding.btnOpenResult.setOnClickListener {
            openResultLink(resultUrl)
        }
    }

    private fun copyResultLink(resultUrl: String) {
        if (resultUrl.isEmpty()) {
            Toast.makeText(this, R.string.congrats_link_missing, Toast.LENGTH_SHORT).show()
            return
        }
        try {
            val clipboard = getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager
            clipboard.setPrimaryClip(ClipData.newPlainText("Examvan Result Link", resultUrl))
            Toast.makeText(this, R.string.congrats_link_copied, Toast.LENGTH_SHORT).show()
        } catch (_: Exception) {
            Toast.makeText(this, R.string.congrats_link_missing, Toast.LENGTH_SHORT).show()
        }
    }

    private fun openResultLink(resultUrl: String) {
        if (resultUrl.isEmpty()) {
            Toast.makeText(this, R.string.congrats_link_missing, Toast.LENGTH_SHORT).show()
            return
        }
        // Fix temuan review: buka hasil di WebView IN-APP — token tidak lagi
        // masuk history browser eksternal (yang tidak terproteksi FLAG_SECURE).
        try {
            startActivity(
                Intent(this, ResultsViewerActivity::class.java)
                    .putExtra(ResultsViewerActivity.EXTRA_URL, resultUrl)
            )
        } catch (_: Exception) {
            Toast.makeText(this, R.string.congrats_link_missing, Toast.LENGTH_SHORT).show()
        }
    }

    private fun populateIdentity() {
        val container = binding.containerIdentityFields
        container.removeAllViews()

        var added = 0
        if (studentName.isNotEmpty()) {
            addIdentityRow(container, getString(R.string.label_student_name), studentName)
            added++
        }
        if (studentNumber.isNotEmpty()) {
            addIdentityRow(container, getString(R.string.label_exam_number), studentNumber)
            added++
        }
        if (studentClass.isNotEmpty()) {
            addIdentityRow(container, getString(R.string.label_student_class), studentClass)
            added++
        }

        if (added == 0) {
            binding.layoutIdentityInfo.visibility = android.view.View.GONE
        }
    }

    private fun addIdentityRow(container: LinearLayout, label: String, value: String) {
        val rowLayout = LinearLayout(this).apply {
            orientation = LinearLayout.HORIZONTAL
            layoutParams = LinearLayout.LayoutParams(
                LinearLayout.LayoutParams.MATCH_PARENT,
                LinearLayout.LayoutParams.WRAP_CONTENT
            ).apply { bottomMargin = 6 }
        }

        val labelView = TextView(this).apply {
            text = "$label:"
            textSize = 13f
            setTextColor(ContextCompat.getColor(this@CongratulationsActivity, R.color.text_secondary))
            layoutParams = LinearLayout.LayoutParams(0, LinearLayout.LayoutParams.WRAP_CONTENT, 0.4f)
        }

        val valueView = TextView(this).apply {
            text = value
            textSize = 14f
            setTextColor(ContextCompat.getColor(this@CongratulationsActivity, R.color.on_surface))
            typeface = android.graphics.Typeface.DEFAULT_BOLD
            layoutParams = LinearLayout.LayoutParams(0, LinearLayout.LayoutParams.WRAP_CONTENT, 0.6f)
        }

        rowLayout.addView(labelView)
        rowLayout.addView(valueView)
        container.addView(rowLayout)
    }
}
