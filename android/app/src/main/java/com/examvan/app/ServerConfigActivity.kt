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
        // Edge-to-edge inset handling untuk Android 15+ (forced edge-to-edge)
        applyEdgeToEdgeInsets(binding.root)

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
        } else {
            binding.etServerUrl.setText("https://examvan.my.id")
        }
        if (savedToken.isNotEmpty()) {
            binding.etToken.setText(savedToken)
        }

        // Enforce UPPERCASE as the user types
        binding.etToken.addTextChangedListener(object : android.text.TextWatcher {
            private var isUpdating = false
            override fun beforeTextChanged(s: CharSequence?, start: Int, count: Int, after: Int) {}
            override fun onTextChanged(s: CharSequence?, start: Int, before: Int, count: Int) {}
            override fun afterTextChanged(s: android.text.Editable?) {
                if (isUpdating) return
                s?.let {
                    val str = it.toString()
                    val upper = str.uppercase()
                    if (str != upper) {
                        isUpdating = true
                        val selectionStart = binding.etToken.selectionStart
                        val selectionEnd = binding.etToken.selectionEnd
                        it.replace(0, it.length, upper)
                        binding.etToken.setSelection(
                            selectionStart.coerceAtMost(upper.length),
                            selectionEnd.coerceAtMost(upper.length)
                        )
                        isUpdating = false
                    }
                }
            }
        })

        // Set version from BuildConfig instead of hardcoded string
        binding.tvVersion.text = "EXAMVAN v${BuildConfig.VERSION_NAME}"

        // Tampilkan tombol clear data hanya jika ada data tersimpan
        val hasSavedUrl = savedUrl.isNotEmpty() || savedToken.isNotEmpty()
        binding.btnClearData.visibility = if (hasSavedUrl) View.VISIBLE else View.GONE
        binding.btnClearData.setOnClickListener {
            AlertDialog.Builder(this)
                .setTitle("Hapus Data Tersimpan")
                .setMessage("Hapus URL server, token, dan data identitas yang tersimpan?")
                .setPositiveButton("Ya, Hapus") { _, _ ->
                    AppPrefs.getConfigPrefs(this).edit().clear().apply()
                    AppPrefs.getExamPrefs(this).edit().clear().apply()
                    AppPrefs.getDevicePrefs(this).edit().clear().apply()
                    binding.etServerUrl.setText("")
                    binding.etToken.setText("")
                    binding.btnClearData.visibility = View.GONE
                    showError("Data tersimpan berhasil dihapus")
                }
                .setNegativeButton("Batal", null)
                .show()
        }

        binding.btnConnect.setOnClickListener {
            var url = binding.etServerUrl.text.toString().trim()
            val token = binding.etToken.text.toString().trim().uppercase()

            if (url.isNotEmpty() && !url.startsWith("http://") && !url.startsWith("https://")) {
                val isLocalOrIp = url.startsWith("localhost") ||
                        url.startsWith("127.0.0.1") ||
                        url.matches(Regex("^(\\d{1,3}\\.\\d{1,3}\\.\\d{1,3}\\.\\d{1,3})(:\\d+)?.*"))
                url = if (isLocalOrIp) {
                    "http://$url"
                } else {
                    "https://$url"
                }
                binding.etServerUrl.setText(url)
            }

            if (!validateInputs(url, token)) return@setOnClickListener

            // Warn if using plain HTTP (cleartext) — MITM risk on public networks
            if (url.startsWith("http://")) {
                AlertDialog.Builder(this)
                    .setTitle("Peringatan Keamanan")
                    .setMessage("HTTP tidak aman di jaringan publik. Gunakan HTTPS jika tersedia.\n\n" +
                            "Koneksi HTTP dapat disadap (man-in-the-middle) oleh pihak ketiga.")
                    .setPositiveButton("Tetap Lanjutkan") { _, _ ->
                        connectAndFetchExam(url, token)
                    }
                    .setNegativeButton("Batal", null)
                    .show()
            } else {
                connectAndFetchExam(url, token)
            }
        }
    }

    private fun isVersionCompatible(appVersion: String, requiredVersion: String): Boolean {
        try {
            // Parse version into integer segments, stripping non-numeric suffixes
            // e.g. "2.1.10-beta" -> [2, 1, 10]
            fun parseVersion(v: String): List<Int> {
                return v.split(".").map { seg ->
                    // Take leading digits only (discard non-numeric suffix like "-beta")
                    val digits = seg.takeWhile { it.isDigit() }
                    if (digits.isEmpty()) 0 else digits.toInt()
                }
            }
            val appParts = parseVersion(appVersion)
            val reqParts = parseVersion(requiredVersion)
            val length = maxOf(appParts.size, reqParts.size)
            for (i in 0 until length) {
                val appPart = appParts.getOrElse(i) { 0 }
                val reqPart = reqParts.getOrElse(i) { 0 }
                if (appPart > reqPart) return true
                if (appPart < reqPart) return false
            }
            // All segments equal — version is compatible
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
        if (token.length != 8) {
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
                                val tokenToUse = exam.token ?: token
                                if (binding.cbRememberUrl.isChecked) {
                                    configPrefs.edit()
                                        .putString(AppPrefs.KEY_SERVER_URL, url)
                                        .putInt(AppPrefs.KEY_EXAM_ID, exam.id)
                                        .putString(AppPrefs.KEY_EXAM_TOKEN, tokenToUse)
                                        .putBoolean(AppPrefs.KEY_REMEMBER_URL, true)
                                        .apply()
                                } else {
                                    configPrefs.edit()
                                        .putBoolean(AppPrefs.KEY_REMEMBER_URL, false)
                                        .remove(AppPrefs.KEY_SERVER_URL)
                                        .remove(AppPrefs.KEY_EXAM_ID)
                                        .remove(AppPrefs.KEY_EXAM_TOKEN)
                                        .apply()
                                }

                                // Show student identity dialog
                                showStudentIdentityDialog(exam, url, tokenToUse)
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

    private fun showStudentIdentityDialog(exam: Exam, serverUrl: String, token: String) {
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

        // Inflate XML layout template — lebih maintainable daripada build 100% programmatic
        val dialogView = LayoutInflater.from(this).inflate(R.layout.dialog_student_identity, null)
        val fieldsContainer = dialogView.findViewById<LinearLayout>(R.id.fieldsContainer)
        val tvDialogError = dialogView.findViewById<TextView>(R.id.tvDialogError)
        val btnConfirm = dialogView.findViewById<Button>(R.id.btnConfirmStart)

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
            fieldsContainer.addView(labelView)

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
            fieldsContainer.addView(editText, LinearLayout.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT,
                ViewGroup.LayoutParams.WRAP_CONTENT
            ).apply { setMargins(0, 0, 0, 4) })
            editTexts[field.key] = editText
        }

        val builder = AlertDialog.Builder(this)
            .setView(dialogView)
            .setCancelable(false)
            .setNegativeButton("Ganti Server") { dialog, _ ->
                dialog.dismiss()
            // Allow user to go back to server config if they entered wrong server details
            }

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
            val panelColor = exam.panel_color ?: ""
            AppPrefs.getExamPrefs(this@ServerConfigActivity).edit()
                .putString(AppPrefs.KEY_QUESTIONS_JSON, questionsJson)
                .putString(AppPrefs.KEY_SECURITY_LEVEL, securityLevel)
                .putBoolean(AppPrefs.KEY_STRICT_MODE, strictMode)
                .putString(AppPrefs.KEY_PANEL_COLOR, panelColor)
                .apply()

            // Extract legacy fields for backward compat with ExamViewer with smart fallbacks for custom keys
            var name = identityJson.optString("student_name", "")
            if (name.isEmpty()) {
                for (key in identityJson.keys()) {
                    val lowerKey = key.lowercase()
                    if (lowerKey.contains("nama") || lowerKey.contains("name")) {
                        name = identityJson.optString(key, "")
                        if (name.isNotEmpty()) break
                    }
                }
            }
            if (name.isEmpty() && identityJson.length() > 0) {
                val firstKey = identityJson.keys().next()
                name = identityJson.optString(firstKey, "")
            }

            var number = identityJson.optString("exam_number", "")
            if (number.isEmpty()) {
                for (key in identityJson.keys()) {
                    val lowerKey = key.lowercase()
                    if (lowerKey.contains("nomor") || lowerKey.contains("no") || lowerKey.contains("nis") || lowerKey.contains("number")) {
                        number = identityJson.optString(key, "")
                        if (number.isNotEmpty()) break
                    }
                }
            }
            if (number.isEmpty() && identityJson.length() > 1) {
                val keys = identityJson.keys()
                keys.next()
                if (keys.hasNext()) {
                    number = identityJson.optString(keys.next(), "")
                }
            }

            var sClass = identityJson.optString("student_class", "")
            if (sClass.isEmpty()) {
                for (key in identityJson.keys()) {
                    val lowerKey = key.lowercase()
                    if (lowerKey.contains("kelas") || lowerKey.contains("class")) {
                        sClass = identityJson.optString(key, "")
                        if (sClass.isNotEmpty()) break
                    }
                }
            }

            startExamViewer(exam.id, exam.name, serverUrl, token, name, number, sClass, identityDataStr, exam.end_time)
        }

        alertDialog.show()

        // Tapjacking protection for dynamically-created dialog
        alertDialog.window?.let { w ->
            w.setFlags(WindowManager.LayoutParams.FLAG_WATCH_OUTSIDE_TOUCH,
                WindowManager.LayoutParams.FLAG_WATCH_OUTSIDE_TOUCH)
        }
    }

    private fun startExamViewer(examId: Int, examName: String, serverUrl: String, token: String, name: String, number: String, studentClass: String, identityData: String = "{}", endTime: String? = null) {
        val intent = Intent(this@ServerConfigActivity, ExamViewerActivity::class.java).apply {
            putExtra("exam_id", examId)
            putExtra("exam_name", examName)
            putExtra("server_url", serverUrl)
            putExtra("exam_token", token)
            putExtra("student_name", name)
            putExtra("student_number", number)
            putExtra("student_class", studentClass)
            putExtra("identity_data", identityData)
            putExtra("end_time", endTime)
        }
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
