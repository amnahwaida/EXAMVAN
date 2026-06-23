package com.examvan.app

import android.content.ClipboardManager
import android.content.Context
import android.content.Intent
import android.content.SharedPreferences
import android.os.Bundle
import android.security.keystore.KeyPermanentlyInvalidatedException
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
import androidx.core.content.ContextCompat
import androidx.security.crypto.EncryptedSharedPreferences
import androidx.security.crypto.MasterKey
import com.examvan.app.api.ApiClient
import com.examvan.app.databinding.ActivityServerConfigBinding
import com.examvan.app.model.Exam
import com.examvan.app.model.IdentityField
import com.examvan.app.BuildConfig
import java.security.GeneralSecurityException
import org.json.JSONObject

/**
 * Screen 1: Server & Token Configuration
 * - Input URL base server (e.g. http://192.168.1.100:5000)
 * - Input 6-character unique Exam Token
 * - Checkbox to persist URL/Token in SharedPreferences
 * - Validates server health and Token existence before showing student identity form
 */
class ServerConfigActivity : BaseSecureActivity() {

    private lateinit var binding: ActivityServerConfigBinding


    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        binding = ActivityServerConfigBinding.inflate(layoutInflater)
        setContentView(binding.root)

        // Wrap first EncryptedSharedPreferences access in try-catch for keystore corruption
        val prefs = try {
            AppPrefs.getConfigPrefs(this)
        } catch (e: GeneralSecurityException) {
            showError("Gagal mengakses penyimpanan aman: ${e.message}")
            binding.btnConnect.isEnabled = false
            return
        }

        // Check if URL and Token were previously saved
        val rememberUrl = prefs.getBoolean(AppPrefs.KEY_REMEMBER_URL, true)
        val savedUrl = prefs.getString(AppPrefs.KEY_SERVER_URL, "") ?: ""
        val savedToken = prefs.getString(AppPrefs.KEY_EXAM_TOKEN, "") ?: ""

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
                url = "https://$url"
                binding.etServerUrl.setText(url)
            }
            // WARNING: Using HTTP for LAN servers is insecure — prefer HTTPS

            if (validateInputs(url, token)) {
                connectAndFetchExam(url, token)
            }
        }
    }

    private fun isVersionCompatible(appVersion: String, requiredVersion: String): Boolean {
        try {
            // Strip non-numeric pre-release suffixes (e.g. "2.0.0-beta" -> "2.0.0")
            fun stripSuffix(v: String): List<Int> =
                v.split(".").map { seg ->
                    seg.replace(Regex("[^0-9].*"), "").toIntOrNull() ?: 0
                }
            val appParts = stripSuffix(appVersion)
            val reqParts = stripSuffix(requiredVersion)
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
            showError(getString(R.string.url_empty_error))
            return false
        }
        if (token.isEmpty()) {
            showError(getString(R.string.error_invalid_token))
            return false
        }
        if (token.length != 6) {
            showError(getString(R.string.token_length_error))
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
                        showError(getString(R.string.version_mismatch_error, appVersion, requiredVersion))
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
                                val configPrefs = AppPrefs.getConfigPrefs(this@ServerConfigActivity)
                                if (binding.cbRememberUrl.isChecked) {
                                    configPrefs.edit()
                                        .putString(AppPrefs.KEY_SERVER_URL, url)
                                        .putString(AppPrefs.KEY_EXAM_TOKEN, token)
                                        .putBoolean(AppPrefs.KEY_REMEMBER_URL, true)
                                        .apply()
                                } else {
                                    configPrefs.edit()
                                        .putBoolean(AppPrefs.KEY_REMEMBER_URL, false)
                                        .remove(AppPrefs.KEY_SERVER_URL)
                                        .remove(AppPrefs.KEY_EXAM_TOKEN)
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
                    showError(getString(R.string.connection_error_format, errorMsg))
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
            text = getString(R.string.identity_dialog_title)
            textSize = 20f
            setTextColor(ContextCompat.getColor(this@ServerConfigActivity, R.color.on_surface))
            typeface = android.graphics.Typeface.DEFAULT_BOLD
            gravity = android.view.Gravity.CENTER
        }
        container.addView(titleView)

        val subtitleView = TextView(this).apply {
            text = getString(R.string.identity_dialog_subtitle)
            textSize = 13f
            setTextColor(ContextCompat.getColor(this@ServerConfigActivity, R.color.text_secondary))
            gravity = android.view.Gravity.CENTER
            setPadding(0, 4, 0, 24)
        }
        container.addView(subtitleView)

        // Create EditText map for all fields
        val editTexts = mutableMapOf<String, EditText>()

        // Restore previously saved identity data
        val savedIdentityJson = AppPrefs.getConfigPrefs(this).getString(AppPrefs.KEY_IDENTITY_DATA, "{}") ?: "{}"
        val savedIdentity = try { JSONObject(savedIdentityJson) } catch (_: Exception) { JSONObject() }

        for (field in fields) {
            val labelView = TextView(this).apply {
                text = field.label + if (field.required) " *" else ""
                textSize = 14f
                setTextColor(ContextCompat.getColor(this@ServerConfigActivity, R.color.text_secondary))
                typeface = android.graphics.Typeface.DEFAULT_BOLD
                setPadding(0, 12, 0, 4)
            }
            container.addView(labelView)

            val editText = EditText(this).apply {
                hint = field.label
                setText(savedIdentity.optString(field.key, ""))
                setTextColor(ContextCompat.getColor(this@ServerConfigActivity, R.color.on_surface))
                setHintTextColor(ContextCompat.getColor(this@ServerConfigActivity, R.color.text_muted))
                background = android.graphics.drawable.GradientDrawable().apply {
                    setStroke(1, ContextCompat.getColor(this@ServerConfigActivity, R.color.input_border))
                    setColor(ContextCompat.getColor(this@ServerConfigActivity, R.color.input_fill))
                    cornerRadius = 8f
                }
                setPadding(16, 12, 16, 12)
                textSize = 15f
                filters = arrayOf(android.text.InputFilter.LengthFilter(100))
            }
            container.addView(editText, LinearLayout.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT,
                ViewGroup.LayoutParams.WRAP_CONTENT
            ).apply { setMargins(0, 0, 0, 4) })
            editTexts[field.key] = editText
        }

        val tvDialogError = TextView(this).apply {
            textSize = 13f
            setTextColor(ContextCompat.getColor(this@ServerConfigActivity, R.color.danger))
            gravity = android.view.Gravity.CENTER
            setPadding(0, 12, 0, 4)
            visibility = View.GONE
        }
        container.addView(tvDialogError)

        val btnConfirm = Button(this).apply {
            text = getString(R.string.btn_start_exam)
            setTextColor(ContextCompat.getColor(this@ServerConfigActivity, R.color.on_primary))
            textSize = 15f
            typeface = android.graphics.Typeface.DEFAULT_BOLD
            setPadding(0, 14, 0, 14)
            setBackgroundColor(ContextCompat.getColor(this@ServerConfigActivity, R.color.primary))
            layoutParams = LinearLayout.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT,
                ViewGroup.LayoutParams.WRAP_CONTENT
            ).apply { setMargins(0, 16, 0, 0) }
        }
        container.addView(btnConfirm)

        scrollView.addView(container)

        val builder = AlertDialog.Builder(this)
            .setView(scrollView)
            .setCancelable(false)
        // Prevent dismissal without filling identity — student can close app entirely if needed

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
                tvDialogError.text = getString(R.string.field_required_error, firstEmptyKey)
                tvDialogError.visibility = View.VISIBLE
                return@setOnClickListener
            }

            tvDialogError.visibility = View.GONE

            val identityDataStr = identityJson.toString()

            // Save identity data to SharedPreferences
            AppPrefs.getConfigPrefs(this@ServerConfigActivity).edit()
                .putString(AppPrefs.KEY_IDENTITY_DATA, identityDataStr)
                .apply()

            alertDialog.dismiss()

            // Save questions JSON and security level
            val questionsJson = com.google.gson.Gson().toJson(exam.questions ?: emptyList<Any>())
            val securityLevel = exam.security_level ?: "medium"
            val strictMode = exam.strict_mode ?: false
            AppPrefs.getExamPrefs(this@ServerConfigActivity).edit()
                .putString(AppPrefs.KEY_QUESTIONS_JSON, questionsJson)
                .putString(AppPrefs.KEY_SECURITY_LEVEL, securityLevel)
                .putBoolean(AppPrefs.KEY_STRICT_MODE, strictMode)
                .apply()

            // Extract legacy fields for backward compat with ExamViewer
            val name = identityJson.optString("student_name", "")
            val number = identityJson.optString("exam_number", "")
            val sClass = identityJson.optString("student_class", "")
            startExamViewer(exam.id, exam.name, serverUrl, name, number, sClass, identityDataStr)
        }

        alertDialog.show()

        // Tapjacking protection for dynamically-created dialog
        alertDialog.window?.let { w ->
            w.setFlags(WindowManager.LayoutParams.FLAG_WATCH_OUTSIDE_TOUCH,
                WindowManager.LayoutParams.FLAG_WATCH_OUTSIDE_TOUCH)
        }
    }

    private fun startExamViewer(examId: Int, examName: String, serverUrl: String, name: String, number: String, studentClass: String, identityData: String = "{}") {
        val strictMode = AppPrefs.getExamPrefs(this).getBoolean(AppPrefs.KEY_STRICT_MODE, false)
        // Read exam_token directly from EncryptedSharedPreferences instead of Intent
        val examToken = AppPrefs.getConfigPrefs(this).getString(AppPrefs.KEY_EXAM_TOKEN, "") ?: ""
        val intent = Intent(this@ServerConfigActivity, ExamViewerActivity::class.java).apply {
            putExtra("exam_id", examId)
            putExtra("exam_name", examName)
            putExtra("server_url", serverUrl)
            putExtra("student_name", name)
            putExtra("student_number", number)
            putExtra("student_class", studentClass)
            putExtra("identity_data", identityData)

        }
        // Clean sensitive extras before launch — token is read from EncryptedSharedPreferences
        intent.putExtra("exam_token", "")
        startActivity(intent)
    }

    private fun setLoading(loading: Boolean) {
        binding.progressLoading.visibility = if (loading) View.VISIBLE else View.GONE
        binding.btnConnect.isEnabled = !loading
        binding.btnConnect.text = if (loading) getString(R.string.processing) else getString(R.string.btn_start_exam)
    }

    private fun showError(msg: String) {
        binding.tvError.text = msg
        binding.tvError.visibility = View.VISIBLE
    }

    private fun hideError() {
        binding.tvError.visibility = View.GONE
    }
}
