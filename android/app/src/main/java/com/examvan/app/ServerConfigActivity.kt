package com.examvan.app

import android.content.Intent
import android.content.SharedPreferences
import android.os.Bundle
import android.view.View
import android.view.WindowManager
import androidx.appcompat.app.AppCompatActivity
import com.examvan.app.api.ApiClient
import com.examvan.app.databinding.ActivityServerConfigBinding

/**
 * Screen 1: Server & Token Configuration
 * - Input URL base server (e.g. http://192.168.1.100:5000)
 * - Input 6-character unique Exam Token
 * - Checkbox to persist URL/Token in SharedPreferences
 * - Validates server health and Token existence before going to PDF viewer
 */
class ServerConfigActivity : AppCompatActivity() {

    private lateinit var binding: ActivityServerConfigBinding
    private lateinit var prefs: SharedPreferences

    companion object {
        const val PREFS_NAME = "app_config"
        const val KEY_SERVER_URL = "server_url"
        const val KEY_EXAM_TOKEN = "exam_token"
        const val KEY_REMEMBER_URL = "remember_url"
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        // FLAG_SECURE: prevent screenshots & screen recording
        window.setFlags(
            WindowManager.LayoutParams.FLAG_SECURE,
            WindowManager.LayoutParams.FLAG_SECURE
        )

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
            val url = binding.etServerUrl.text.toString().trim()
            val token = binding.etToken.text.toString().trim().uppercase()
            if (validateInputs(url, token)) {
                connectAndStartExam(url, token)
            }
        }
    }

    private fun validateInputs(url: String, token: String): Boolean {
        if (url.isEmpty()) {
            showError("URL tidak boleh kosong")
            return false
        }
        if (!url.startsWith("http://") && !url.startsWith("https://")) {
            showError(getString(R.string.error_invalid_url))
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

    private fun connectAndStartExam(url: String, token: String) {
        setLoading(true)
        hideError()

        ApiClient.setBaseUrl(url)
        // First check server health
        ApiClient.checkHealth(
            onSuccess = {
                // If health is OK, validate token and fetch exam
                ApiClient.getExamByToken(
                    token = token,
                    onSuccess = { response ->
                        runOnUiThread {
                            setLoading(false)
                            val exam = response.data
                            if (exam != null) {
                                // Save preferences if remember is checked
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

                                // Go directly to ExamViewerActivity
                                val intent = Intent(this@ServerConfigActivity, ExamViewerActivity::class.java).apply {
                                    putExtra("exam_id", exam.id)
                                    putExtra("exam_name", exam.name)
                                    putExtra("server_url", url)
                                }
                                startActivity(intent)
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
