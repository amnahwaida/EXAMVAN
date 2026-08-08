package com.examvan.app

import android.content.Intent
import android.os.Bundle
import android.view.View
import androidx.recyclerview.widget.LinearLayoutManager
import com.examvan.app.adapter.ExamAdapter
import com.examvan.app.api.ApiClient
import com.examvan.app.databinding.ActivityExamListBinding
import com.examvan.app.helper.UpdateManager
import com.examvan.app.BuildConfig

/**
 * Screen 2: Exam List
 * - Fetches active exams from /api/exams
 * - Displays in RecyclerView with pull-to-refresh
 * - Tap an exam to open PDF viewer
 */
class ExamListActivity : BaseSecureActivity() {

    private lateinit var binding: ActivityExamListBinding
    private lateinit var adapter: ExamAdapter

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        binding = ActivityExamListBinding.inflate(layoutInflater)
        setContentView(binding.root)
        // Edge-to-edge inset handling untuk Android 15+ (forced edge-to-edge)
        applyEdgeToEdgeInsets(binding.root)

        // Setup server URL from intent
        val serverUrl = intent.getStringExtra("server_url") ?: ""
        if (serverUrl.isNotEmpty()) {
            ApiClient.setBaseUrl(serverUrl)
        }

        adapter = ExamAdapter { exam ->
            val configPrefs = AppPrefs.getConfigPrefsSafe(this)
            val resolvedServerUrl = if (serverUrl.isNotEmpty()) serverUrl else configPrefs.getString(AppPrefs.KEY_SERVER_URL, "") ?: ""
            val storedExamId = configPrefs.getInt(AppPrefs.KEY_EXAM_ID, -1)

            if (exam.id != storedExamId) {
                androidx.appcompat.app.AlertDialog.Builder(this)
                    .setTitle("Ujian Tidak Dapat Diakses")
                    .setMessage("Ujian ini memerlukan token yang berbeda. Silakan kembali ke layar awal untuk memasukkan token yang valid.")
                    .setPositiveButton("OK", null)
                    .show()
                return@ExamAdapter
            }

            val submittedKey = AppPrefs.getSubmittedOrExitedKey(exam.id)
            val submitted = AppPrefs.getExamPrefsSafe(this).getBoolean(submittedKey, false)
            if (submitted) {
                androidx.appcompat.app.AlertDialog.Builder(this)
                    .setTitle("Ujian Sudah Selesai")
                    .setMessage("Ujian ini sudah Anda kumpulkan dan tidak dapat dikerjakan kembali.")
                    .setPositiveButton("OK", null)
                    .show()
                return@ExamAdapter
            }

            val resolvedToken = configPrefs.getString(AppPrefs.KEY_EXAM_TOKEN, "") ?: ""
            val identityJsonStr = configPrefs.getString(AppPrefs.KEY_IDENTITY_DATA, "{}") ?: "{}"
            val identityJson = try { org.json.JSONObject(identityJsonStr) } catch (_: Exception) { org.json.JSONObject() }
            val name = identityJson.optString("student_name", "")
            val number = identityJson.optString("exam_number", "")
            val sClass = identityJson.optString("student_class", "")

            val intent = Intent(this, ExamViewerActivity::class.java).apply {
                putExtra("exam_id", exam.id)
                putExtra("exam_token", resolvedToken)
                putExtra("exam_name", exam.name)
                putExtra("server_url", resolvedServerUrl)
                putExtra("student_name", name)
                putExtra("student_number", number)
                putExtra("student_class", sClass)
                putExtra("identity_data", identityJsonStr)
                putExtra("end_time", exam.end_time)
            }
            startActivity(intent)
        }

        binding.rvExams.layoutManager = LinearLayoutManager(this)
        binding.rvExams.adapter = adapter

        // Pull to refresh
        binding.swipeRefresh.setColorSchemeColors(
            androidx.core.content.ContextCompat.getColor(this, R.color.primary)
        )
        binding.swipeRefresh.setOnRefreshListener { loadExams() }

        // Retry button
        binding.btnRetry.setOnClickListener { loadExams() }

        // Change server button — kembali ke ServerConfigActivity
        binding.btnChangeServer.setOnClickListener {
            androidx.appcompat.app.AlertDialog.Builder(this)
                .setTitle("Ganti Server")
                .setMessage("Apakah Anda yakin ingin kembali ke pengaturan server?")
                .setPositiveButton("Ya") { _, _ -> finish() }
                .setNegativeButton("Batal", null)
                .show()
        }

        // Load exams
        loadExams()
    }

    private fun loadExams() {
        showLoading()

        // Periodically re-check the app version against the server's required
        // version (max once per day) so students who are already logged in are
        // still blocked and sent to the download page when a new APK ships.
        maybeCheckVersion()

        ApiClient.getExams(
            onSuccess = { response ->
                runOnUiThread {
                    binding.swipeRefresh.isRefreshing = false

                    if (response.success && response.data.isNotEmpty()) {
                        adapter.submitList(response.data)
                        showContent()
                        updateSyncTime()
                    } else {
                        showEmpty(getString(R.string.empty_exams))
                    }
                }
            },
            onError = { statusCode, errorMsg ->
                runOnUiThread {
                    binding.swipeRefresh.isRefreshing = false
                    if (statusCode == ApiClient.HTTP_UPGRADE_REQUIRED) {
                        showUpdateDialog()
                    } else {
                        showEmpty(getString(R.string.error_network) + "\n$errorMsg")
                    }
                }
            }
        )
    }

    /** Server URL resolved from intent or saved config. */
    private fun currentServerUrl(): String {
        val serverUrl = intent.getStringExtra("server_url") ?: ""
        return if (serverUrl.isNotEmpty()) serverUrl
        else AppPrefs.getConfigPrefsSafe(this).getString(AppPrefs.KEY_SERVER_URL, "") ?: ""
    }

    /** Check health once per day; show blocking update dialog when outdated. */
    private fun maybeCheckVersion() {
        val configPrefs = AppPrefs.getConfigPrefsSafe(this)
        val lastCheck = configPrefs.getLong(AppPrefs.KEY_LAST_VERSION_CHECK_TS, 0L)
        val now = System.currentTimeMillis()
        if (now - lastCheck < 24 * 60 * 60 * 1000L) return

        configPrefs.edit().putLong(AppPrefs.KEY_LAST_VERSION_CHECK_TS, now).apply()

        val serverUrl = currentServerUrl()
        if (serverUrl.isEmpty()) return

        ApiClient.setBaseUrl(serverUrl)
        ApiClient.checkHealth(
            onSuccess = { health ->
                val required = health.required_app_version
                if (required != null && UpdateManager.isOutdated(BuildConfig.VERSION_NAME, required)) {
                    runOnUiThread {
                        UpdateManager.showUpdateRequiredDialog(this@ExamListActivity, BuildConfig.VERSION_NAME, required, serverUrl)
                    }
                }
            },
            onError = { /* jaringan — biarkan getExams menangani pesannya */ }
        )
    }

    private fun showUpdateDialog() {
        UpdateManager.showUpdateRequiredDialog(
            this,
            BuildConfig.VERSION_NAME,
            null,
            currentServerUrl()
        )
    }

    private fun showLoading() {
        binding.progressLoading.visibility = View.VISIBLE
        binding.rvExams.visibility = View.GONE
        binding.layoutEmpty.visibility = View.GONE
    }

    private fun showContent() {
        binding.progressLoading.visibility = View.GONE
        binding.rvExams.visibility = View.VISIBLE
        binding.layoutEmpty.visibility = View.GONE
    }

    private fun showEmpty(message: String) {
        binding.progressLoading.visibility = View.GONE
        binding.rvExams.visibility = View.GONE
        binding.layoutEmpty.visibility = View.VISIBLE
        binding.tvEmptyMessage.text = message
    }

    private fun updateSyncTime() {
        binding.tvLastSync.text = "Sync: " + java.time.LocalTime.now().format(java.time.format.DateTimeFormatter.ofPattern("HH:mm:ss"))
    }
}
