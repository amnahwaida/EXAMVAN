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
            onSuccess = {
                // If health is OK, validate token and fetch exam
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

            if (securityLevel == "strict") {
                checkAndStartStrictExam(exam.id, exam.name, serverUrl, name, number, studentClass)
            } else {
                startExamViewer(exam.id, exam.name, serverUrl, name, number, studentClass)
            }
        }

        alertDialog.show()
    }

    override fun onResume() {
        super.onResume()
        val pendingId = prefs.getInt("pending_exam_id", -1)
        if (pendingId != -1) {
            val examName = prefs.getString("pending_exam_name", "") ?: ""
            val serverUrl = prefs.getString("pending_server_url", "") ?: ""
            val name = prefs.getString("pending_student_name", "") ?: ""
            val number = prefs.getString("pending_student_number", "") ?: ""
            val studentClass = prefs.getString("pending_student_class", "") ?: ""

            checkAndStartStrictExam(pendingId, examName, serverUrl, name, number, studentClass)
        }
    }

    private fun checkAndStartStrictExam(examId: Int, examName: String, serverUrl: String, name: String, number: String, studentClass: String) {
        if (isGestureNavigationEnabled(this)) {
            // Save state to preferences
            prefs.edit()
                .putInt("pending_exam_id", examId)
                .putString("pending_exam_name", examName)
                .putString("pending_server_url", serverUrl)
                .putString("pending_student_name", name)
                .putString("pending_student_number", number)
                .putString("pending_student_class", studentClass)
                .apply()
            
            showGestureNavigationWarningDialog()
        } else if (!android.provider.Settings.canDrawOverlays(this)) {
            // Save state to preferences
            prefs.edit()
                .putInt("pending_exam_id", examId)
                .putString("pending_exam_name", examName)
                .putString("pending_server_url", serverUrl)
                .putString("pending_student_name", name)
                .putString("pending_student_number", number)
                .putString("pending_student_class", studentClass)
                .apply()

            showOverlayPermissionDialog()
        } else {
            // All green! Clear pending and start
            prefs.edit()
                .remove("pending_exam_id")
                .remove("pending_exam_name")
                .remove("pending_server_url")
                .remove("pending_student_name")
                .remove("pending_student_number")
                .remove("pending_student_class")
                .apply()

            startExamViewer(examId, examName, serverUrl, name, number, studentClass)
        }
    }

    private fun isGestureNavigationEnabled(context: Context): Boolean {
        // 1. Check WindowInsets for system gestures (Left/Right swipe back zones) - API 29+
        if (android.os.Build.VERSION.SDK_INT >= android.os.Build.VERSION_CODES.Q) {
            val insets = window.decorView.rootWindowInsets
            if (insets != null) {
                try {
                    val gestureInsets = insets.getInsets(android.view.WindowInsets.Type.systemGestures())
                    if (gestureInsets.left > 0 || gestureInsets.right > 0) {
                        return true
                    }
                } catch (_: Exception) {}
            }
        }

        // 2. Check standard Secure setting navigation_mode
        try {
            val navMode = android.provider.Settings.Secure.getInt(context.contentResolver, "navigation_mode", -1)
            if (navMode == 2) return true
        } catch (_: Exception) {}
        
        // 3. Check MIUI specific settings (Xiaomi Redmi Note 8 Pro / HyperOS / MIUI)
        try {
            val miuiGestureNavBar = android.provider.Settings.Global.getInt(context.contentResolver, "force_fsg_nav_bar", 0)
            if (miuiGestureNavBar != 0) return true
        } catch (_: Exception) {}

        try {
            val miuiGestureNav = android.provider.Settings.Global.getInt(context.contentResolver, "force_fsg_navigation", 0)
            if (miuiGestureNav != 0) return true
        } catch (_: Exception) {}
        
        // 4. Check standard system resource config_navBarInteractionMode
        try {
            val resourceId = context.resources.getIdentifier("config_navBarInteractionMode", "integer", "android")
            if (resourceId > 0) {
                val interactionMode = context.resources.getInteger(resourceId)
                if (interactionMode == 2) return true
            }
        } catch (_: Exception) {}

        // 5. Check Vivo/Oppo specific keys
        try {
            val vivoGesture = android.provider.Settings.Secure.getInt(context.contentResolver, "navigation_gesture_on", 0)
            if (vivoGesture != 0) return true
        } catch (_: Exception) {}

        return false
    }

    private fun showGestureNavigationWarningDialog() {
        AlertDialog.Builder(this)
            .setTitle("Navigasi Gestur Terdeteksi")
            .setMessage("Ujian ini menggunakan Keamanan Strict. Anda wajib mengubah navigasi HP Anda dari 'Gestur Layar Penuh' menjadi 'Tombol Navigasi Klasik (3 Tombol)' agar sistem pengunci layar berjalan dengan aman.\n\nSilakan buka Pengaturan HP Anda, ubah ke Tombol Navigasi, lalu kembali ke aplikasi ini.")
            .setCancelable(false)
            .setPositiveButton("Buka Pengaturan") { _, _ ->
                val intent = Intent(android.provider.Settings.ACTION_SETTINGS)
                startActivity(intent)
            }
            .setNegativeButton("Batal") { _, _ ->
                // Clear pending to prevent loops
                prefs.edit().remove("pending_exam_id").apply()
            }
            .show()
    }

    private fun showOverlayPermissionDialog() {
        AlertDialog.Builder(this)
            .setTitle("Izin Diperlukan")
            .setMessage("Ujian ini menggunakan Keamanan Strict. Aplikasi membutuhkan izin 'Tampilkan di atas aplikasi lain' untuk mengunci layar dan mencegah kecurangan.\n\nSilakan aktifkan izin ini pada layar pengaturan berikutnya.")
            .setCancelable(false)
            .setPositiveButton("Buka Pengaturan") { _, _ ->
                val intent = Intent(android.provider.Settings.ACTION_MANAGE_OVERLAY_PERMISSION, android.net.Uri.parse("package:$packageName"))
                startActivity(intent)
            }
            .setNegativeButton("Batal") { _, _ ->
                // Clear pending to prevent loops
                prefs.edit().remove("pending_exam_id").apply()
            }
            .show()
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
