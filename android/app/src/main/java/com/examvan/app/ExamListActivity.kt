package com.examvan.app

import android.content.Intent
import android.os.Bundle
import android.view.View
import androidx.recyclerview.widget.LinearLayoutManager
import com.examvan.app.adapter.ExamAdapter
import com.examvan.app.api.ApiClient
import com.examvan.app.databinding.ActivityExamListBinding

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

        // Setup RecyclerView
        adapter = ExamAdapter { exam ->
            val intent = Intent(this, ExamViewerActivity::class.java)
            intent.putExtra("exam_id", exam.id)
            // Token dibaca dari EncryptedSharedPreferences oleh ExamViewerActivity, bukan dari Intent
            intent.putExtra("exam_token", "")
            intent.putExtra("exam_name", exam.name)
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
            onError = { errorMsg ->
                runOnUiThread {
                    binding.swipeRefresh.isRefreshing = false
                    showEmpty(getString(R.string.error_network) + "\n$errorMsg")
                }
            }
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
