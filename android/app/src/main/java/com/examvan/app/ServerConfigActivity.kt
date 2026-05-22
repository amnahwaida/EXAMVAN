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
 * Screen 1: Server Configuration
 * - Input URL base server (e.g. http://192.168.1.100:5000)
 * - Checkbox to persist URL in SharedPreferences
 * - Validates server with /api/health before proceeding
 */
class ServerConfigActivity : AppCompatActivity() {

    private lateinit var binding: ActivityServerConfigBinding
    private lateinit var prefs: SharedPreferences

    companion object {
        const val PREFS_NAME = "app_config"
        const val KEY_SERVER_URL = "server_url"
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

        // Check if URL was previously saved
        val rememberUrl = prefs.getBoolean(KEY_REMEMBER_URL, true)
        val savedUrl = prefs.getString(KEY_SERVER_URL, "") ?: ""

        if (rememberUrl && savedUrl.isNotEmpty()) {
            // Auto-connect with saved URL
            binding.etServerUrl.setText(savedUrl)
            binding.cbRememberUrl.isChecked = true
            connectToServer(savedUrl)
        }

        binding.cbRememberUrl.isChecked = rememberUrl

        binding.btnConnect.setOnClickListener {
            val url = binding.etServerUrl.text.toString().trim()
            if (validateUrl(url)) {
                connectToServer(url)
            }
        }
    }

    private fun validateUrl(url: String): Boolean {
        if (url.isEmpty()) {
            showError("URL tidak boleh kosong")
            return false
        }
        if (!url.startsWith("http://") && !url.startsWith("https://")) {
            showError(getString(R.string.error_invalid_url))
            return false
        }
        return true
    }

    private fun connectToServer(url: String) {
        setLoading(true)
        hideError()

        ApiClient.setBaseUrl(url)
        ApiClient.checkHealth(
            onSuccess = { health ->
                runOnUiThread {
                    setLoading(false)

                    // Save URL based on checkbox
                    if (binding.cbRememberUrl.isChecked) {
                        prefs.edit()
                            .putString(KEY_SERVER_URL, url)
                            .putBoolean(KEY_REMEMBER_URL, true)
                            .apply()
                    } else {
                        prefs.edit()
                            .putBoolean(KEY_REMEMBER_URL, false)
                            .remove(KEY_SERVER_URL)
                            .apply()
                    }

                    // Navigate to exam list
                    val intent = Intent(this, ExamListActivity::class.java)
                    intent.putExtra("server_url", url)
                    startActivity(intent)
                }
            },
            onError = { errorMsg ->
                runOnUiThread {
                    setLoading(false)
                    showError("Tidak dapat terhubung: $errorMsg")
                }
            }
        )
    }

    private fun setLoading(loading: Boolean) {
        binding.progressLoading.visibility = if (loading) View.VISIBLE else View.GONE
        binding.btnConnect.isEnabled = !loading
        binding.btnConnect.text = if (loading) "Menghubungkan..." else getString(R.string.btn_connect)
    }

    private fun showError(msg: String) {
        binding.tvError.text = msg
        binding.tvError.visibility = View.VISIBLE
    }

    private fun hideError() {
        binding.tvError.visibility = View.GONE
    }
}
