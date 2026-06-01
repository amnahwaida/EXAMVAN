package com.examvan.app

import android.content.Context
import android.content.Intent
import android.content.SharedPreferences
import android.os.Bundle
import android.view.LayoutInflater
import android.view.View
import android.view.WindowManager
import android.widget.Button
import android.widget.EditText
import android.widget.TextView
import androidx.appcompat.app.AlertDialog
import androidx.appcompat.app.AppCompatActivity
import com.examvan.app.api.ApiClient
import com.examvan.app.databinding.ActivityServerConfigBinding
import com.examvan.app.model.Exam
import com.examvan.app.BuildConfig

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
        
        // Student identity keys
        const val KEY_STUDENT_NAME = "student_name"
        const val KEY_STUDENT_NUMBER = "student_number"
        const val KEY_STUDENT_CLASS = "student_class"
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

                if (requiredVersion != null && requiredVersion != appVersion) {
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
        val dialogView = LayoutInflater.from(this).inflate(R.layout.dialog_student_identity, null)
        
        val etName = dialogView.findViewById<EditText>(R.id.etStudentName)
        val etNumber = dialogView.findViewById<EditText>(R.id.etStudentNumber)
        val etClass = dialogView.findViewById<EditText>(R.id.etStudentClass)
        val tvDialogError = dialogView.findViewById<TextView>(R.id.tvDialogError)
        val btnConfirm = dialogView.findViewById<Button>(R.id.btnConfirmStart)

        // Pre-fill student identity if previously saved
        etName.setText(prefs.getString(KEY_STUDENT_NAME, ""))
        etNumber.setText(prefs.getString(KEY_STUDENT_NUMBER, ""))
        etClass.setText(prefs.getString(KEY_STUDENT_CLASS, ""))

        val builder = AlertDialog.Builder(this)
            .setView(dialogView)
            .setCancelable(true)

        val alertDialog = builder.create()

        btnConfirm.setOnClickListener {
            val name = etName.text.toString().trim()
            val number = etNumber.text.toString().trim()
            val studentClass = etClass.text.toString().trim()

            if (name.isEmpty() || number.isEmpty() || studentClass.isEmpty()) {
                tvDialogError.text = "Semua bidang identitas wajib diisi!"
                tvDialogError.visibility = View.VISIBLE
                return@setOnClickListener
            }

            tvDialogError.visibility = View.GONE
            
            // Save student identity in SharedPreferences for convenience next time
            prefs.edit()
                .putString(KEY_STUDENT_NAME, name)
                .putString(KEY_STUDENT_NUMBER, number)
                .putString(KEY_STUDENT_CLASS, studentClass)
                .apply()

            alertDialog.dismiss()

            // Save questions JSON and security level from token API response to SharedPreferences
            val questionsJson = com.google.gson.Gson().toJson(exam.questions ?: emptyList<Any>())
            val securityLevel = exam.security_level ?: "medium"
            getSharedPreferences("exam_questions", MODE_PRIVATE)
                .edit()
                .putString("questions_json", questionsJson)
                .putString("security_level", securityLevel)
                .apply()

            startExamViewer(exam.id, exam.name, serverUrl, name, number, studentClass)
        }

        alertDialog.show()
    }

    private fun startExamViewer(examId: Int, examName: String, serverUrl: String, name: String, number: String, studentClass: String) {
        val intent = Intent(this@ServerConfigActivity, ExamViewerActivity::class.java).apply {
            putExtra("exam_id", examId)
            putExtra("exam_name", examName)
            putExtra("server_url", serverUrl)
            putExtra("student_name", name)
            putExtra("student_number", number)
            putExtra("student_class", studentClass)
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
