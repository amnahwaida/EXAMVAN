package com.examvan.app

import android.content.Context
import android.content.Intent
import android.content.SharedPreferences
import android.os.Bundle
import android.view.LayoutInflater
import android.view.View
import android.view.ViewGroup
import android.view.WindowManager
import android.widget.Button
import android.widget.EditText
import android.widget.LinearLayout
import android.widget.TextView
import androidx.appcompat.app.AlertDialog
import androidx.appcompat.app.AppCompatActivity
import com.examvan.app.api.ApiClient
import com.examvan.app.databinding.ActivityServerConfigBinding
import com.examvan.app.model.Exam
import com.examvan.app.model.IdentityField
import com.examvan.app.BuildConfig
import org.json.JSONObject

/**
 * Screen 1: Server & Token Configuration
 * - Input URL base server (e.g. http://192.168.1.100:5000)
 * - Input 6-character unique Exam Token
 * - Checkbox to persist URL/Token in SharedPreferences
 * - Validates server health and Token existence before showing student identity form
 */
class ServerConfigActivity : AppCompatActivity() {

    private lateinit var binding: ActivityServerConfigBinding
    private lateinit var prefs: SharedPreferences

    companion object {
        const val PREFS_NAME = "app_config"
        const val KEY_SERVER_URL = "server_url"
        const val KEY_EXAM_TOKEN = "exam_token"
        const val KEY_REMEMBER_URL = "remember_url"
        const val KEY_IDENTITY_DATA = "identity_data"
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        // FLAG_SECURE: prevent screenshots & screen recording
        window.setFlags(
            WindowManager.LayoutParams.FLAG_SECURE,
            WindowManager.LayoutParams.FLAG_SECURE
        )
        // Keep screen turned on during the exam
        window.addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)

        binding = ActivityServerConfigBinding.inflate(layoutInflater)
        setContentView(binding.root)

        prefs = getSharedPreferences(PREFS_NAME, MODE_PRIVATE)

        // Check if URL and Token were previously saved
        val rememberUrl = prefs.getBoolean(KEY_REMEMBER_URL, true)
        val savedUrl = prefs.getString(KEY_SERVER_URL, "") ?: ""
        val savedToken = prefs.getString(KEY_EXAM_TOKEN, "") ?: ""

        binding.cbRememberUrl.isChecked = rememberUrl
        if (savedUrl.isNotEmpty()) {
            binding.etServerUrl.setText(savedUrl)
        }
        if (savedToken.isNotEmpty()) {
            binding.etToken.setText(savedToken)
        }

        binding.btnConnect.setOnClickListener {
            var url = binding.etServerUrl.text.toString().trim()
            val token = binding.etToken.text.toString().trim().uppercase()
            
            if (url.isNotEmpty() && !url.startsWith("http://") && !url.startsWith("https://")) {
                url = "http://$url"
                binding.etServerUrl.setText(url)
            }

            if (validateInputs(url, token)) {
                connectAndFetchExam(url, token)
            }
        }
    }

    private fun isVersionCompatible(appVersion: String, requiredVersion: String): Boolean {
        try {
            val appParts = appVersion.split(".").map { it.toIntOrNull() ?: 0 }
            val reqParts = requiredVersion.split(".").map { it.toIntOrNull() ?: 0 }
            val length = maxOf(appParts.size, reqParts.size)
            for (i in 0 until length) {
                val appPart = appParts.getOrElse(i) { 0 }
                val reqPart = reqParts.getOrElse(i) { 0 }
                if (appPart > reqPart) return true
                if (appPart < reqPart) return false
            }
            return true
        } catch (e: Exception) {
            return appVersion == requiredVersion
        }
    }

    private fun validateInputs(url: String, token: String): Boolean {
        if (url.isEmpty()) {
            showError("URL tidak boleh kosong")
            return false
        }
        if (token.isEmpty()) {
            showError(getString(R.string.error_invalid_token))
            return false
        }
        if (token.length != 6) {
            showError("Token harus terdiri dari 6 karakter")
            return false
        }
        return true
    }

    private fun connectAndFetchExam(url: String, token: String) {
        setLoading(true)
        hideError()

        ApiClient.setBaseUrl(url)
        // First check server health
        ApiClient.checkHealth(
            onSuccess = { health ->
                // Version check: compare app version with server's required version
                val requiredVersion = health.required_app_version
                val appVersion = BuildConfig.VERSION_NAME

                if (requiredVersion != null && !isVersionCompatible(appVersion, requiredVersion)) {
                    runOnUiThread {
                        setLoading(false)
                        showError("Versi aplikasi tidak sesuai!\nAplikasi Anda: v$appVersion\nVersi yang dibutuhkan: v$requiredVersion\n\nSilakan update aplikasi EXAMVAN Anda ke versi terbaru.")
                    }
                    return@checkHealth
                }

                // If health is OK and version matches, validate token and fetch exam
                ApiClient.getExamByToken(
                    token = token,
                    onSuccess = { response ->
                        runOnUiThread {
                            setLoading(false)
                            val exam = response.data
                            if (exam != null) {
                                // Save connection preferences if remember is checked
                                if (binding.cbRememberUrl.isChecked) {
                                    prefs.edit()
                                        .putString(KEY_SERVER_URL, url)
                                        .putString(KEY_EXAM_TOKEN, token)
                                        .putBoolean(KEY_REMEMBER_URL, true)
                                        .apply()
                                } else {
                                    prefs.edit()
                                        .putBoolean(KEY_REMEMBER_URL, false)
                                        .remove(KEY_SERVER_URL)
                                        .remove(KEY_EXAM_TOKEN)
                                        .apply()
                                }

                                // Show student identity dialog
                                showStudentIdentityDialog(exam, url)
                            } else {
                                showError(getString(R.string.error_token_not_found))
                            }
                        }
                    },
                    onError = { errorMsg ->
                        runOnUiThread {
                            setLoading(false)
                            showError(errorMsg)
                        }
                    }
                )
            },
            onError = { errorMsg ->
                runOnUiThread {
                    setLoading(false)
                    showError("Tidak dapat terhubung ke server: $errorMsg")
                }
            }
        )
    }

    private fun showStudentIdentityDialog(exam: Exam, serverUrl: String) {
        // Use identity_fields from server, or fall back to defaults
        val fields = if (!exam.identity_fields.isNullOrEmpty()) {
            exam.identity_fields
        } else {
            listOf(
                IdentityField("student_name", "Nama Siswa", true),
                IdentityField("exam_number", "Nomor Ujian", true),
                IdentityField("student_class", "Kelas", true)
            )
        }

        // Build dialog form dynamically
        val scrollView = android.widget.ScrollView(this)
        val container = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(48, 32, 48, 24)
        }

        val titleView = TextView(this).apply {
            text = "Identitas Siswa"
            textSize = 20f
            setTextColor(0xff111827.toInt())
            typeface = android.graphics.Typeface.DEFAULT_BOLD
            gravity = android.view.Gravity.CENTER
        }
        container.addView(titleView)

        val subtitleView = TextView(this).apply {
            text = "Isi data diri Anda untuk memulai ujian"
            textSize = 13f
            setTextColor(0xff6b7280.toInt())
            gravity = android.view.Gravity.CENTER
            setPadding(0, 4, 0, 24)
        }
        container.addView(subtitleView)

        // Create EditText map for all fields
        val editTexts = mutableMapOf<String, EditText>()

        // Restore previously saved identity data
        val savedIdentityJson = prefs.getString(KEY_IDENTITY_DATA, "{}") ?: "{}"
        val savedIdentity = try { JSONObject(savedIdentityJson) } catch (_: Exception) { JSONObject() }

        for (field in fields) {
            val labelView = TextView(this).apply {
                text = field.label + if (field.required) " *" else ""
                textSize = 14f
                setTextColor(0xff374151.toInt())
                typeface = android.graphics.Typeface.DEFAULT_BOLD
                setPadding(0, 12, 0, 4)
            }
            container.addView(labelView)

            val editText = EditText(this).apply {
                hint = field.label
                setText(savedIdentity.optString(field.key, ""))
                setTextColor(0xff111827.toInt())
                setHintTextColor(0xff9ca3af.toInt())
                background = android.content.res.ColorStateList.valueOf(0xffe5e7eb.toInt()).let {
                    android.graphics.drawable.GradientDrawable().apply {
                        setStroke(1, 0xffd1d5db.toInt())
                        setColor(0xfff9fafb.toInt())
                        cornerRadius = 8f
                    }
                }
                setPadding(16, 12, 16, 12)
                textSize = 15f
            }
            container.addView(editText, LinearLayout.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT,
                ViewGroup.LayoutParams.WRAP_CONTENT
            ).apply { setMargins(0, 0, 0, 4) })
            editTexts[field.key] = editText
        }

        val tvDialogError = TextView(this).apply {
            textSize = 13f
            setTextColor(0xffdc2626.toInt())
            gravity = android.view.Gravity.CENTER
            setPadding(0, 12, 0, 4)
            visibility = View.GONE
        }
        container.addView(tvDialogError)

        val btnConfirm = Button(this).apply {
            text = "Mulai Ujian"
            setTextColor(0xffffffff.toInt())
            textSize = 15f
            typeface = android.graphics.Typeface.DEFAULT_BOLD
            setPadding(0, 14, 0, 14)
            setBackgroundColor(0xff6366f1.toInt())
            layoutParams = LinearLayout.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT,
                ViewGroup.LayoutParams.WRAP_CONTENT
            ).apply { setMargins(0, 16, 0, 0) }
        }
        container.addView(btnConfirm)

        scrollView.addView(container)

        val builder = AlertDialog.Builder(this)
            .setView(scrollView)
            .setCancelable(true)

        val alertDialog = builder.create()

        btnConfirm.setOnClickListener {
            // Build identity_data JSON from all fields
            val identityJson = JSONObject()
            var hasEmptyRequired = false
            var firstEmptyKey = ""

            for (field in fields) {
                val value = editTexts[field.key]?.text?.toString()?.trim() ?: ""
                if (field.required && value.isEmpty()) {
                    hasEmptyRequired = true
                    if (firstEmptyKey.isEmpty()) firstEmptyKey = field.label
                }
                identityJson.put(field.key, value)
            }

            if (hasEmptyRequired) {
                tvDialogError.text = "\"$firstEmptyKey\" wajib diisi!"
                tvDialogError.visibility = View.VISIBLE
                return@setOnClickListener
            }

            tvDialogError.visibility = View.GONE

            val identityDataStr = identityJson.toString()

            // Save identity data to SharedPreferences
            prefs.edit()
                .putString(KEY_IDENTITY_DATA, identityDataStr)
                .apply()

            alertDialog.dismiss()

            // Save questions JSON and security level
            val questionsJson = com.google.gson.Gson().toJson(exam.questions ?: emptyList<Any>())
            val securityLevel = exam.security_level ?: "medium"
            val strictMode = exam.strict_mode ?: false
            getSharedPreferences("exam_questions", MODE_PRIVATE)
                .edit()
                .putString("questions_json", questionsJson)
                .putString("security_level", securityLevel)
                .putBoolean("strict_mode", strictMode)
                .apply()

            // Extract legacy fields for backward compat with ExamViewer
            val name = identityJson.optString("student_name", "")
            val number = identityJson.optString("exam_number", "")
            val sClass = identityJson.optString("student_class", "")
            startExamViewer(exam.id, exam.name, serverUrl, name, number, sClass, identityDataStr)
        }

        alertDialog.show()
    }

    private fun startExamViewer(examId: Int, examName: String, serverUrl: String, name: String, number: String, studentClass: String, identityData: String = "{}") {
        val strictMode = getSharedPreferences("exam_questions", MODE_PRIVATE)
            .getBoolean("strict_mode", false)
        val intent = Intent(this@ServerConfigActivity, ExamViewerActivity::class.java).apply {
            putExtra("exam_id", examId)
            putExtra("exam_name", examName)
            putExtra("server_url", serverUrl)
            putExtra("student_name", name)
            putExtra("student_number", number)
            putExtra("student_class", studentClass)
            putExtra("identity_data", identityData)
            putExtra("strict_mode", strictMode)
        }
        startActivity(intent)
    }

    private fun setLoading(loading: Boolean) {
        binding.progressLoading.visibility = if (loading) View.VISIBLE else View.GONE
        binding.btnConnect.isEnabled = !loading
        binding.btnConnect.text = if (loading) "Memproses..." else getString(R.string.btn_start_exam)
    }

    private fun showError(msg: String) {
        binding.tvError.text = msg
        binding.tvError.visibility = View.VISIBLE
    }

    private fun hideError() {
        binding.tvError.visibility = View.GONE
    }
}
