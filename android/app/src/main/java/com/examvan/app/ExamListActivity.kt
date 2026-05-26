package com.examvan.app

import android.content.ClipboardManager
import android.content.Context
import android.content.Intent
import android.os.Bundle
import android.view.View
import android.view.WindowManager
import androidx.appcompat.app.AppCompatActivity
import androidx.recyclerview.widget.LinearLayoutManager
import com.examvan.app.adapter.ExamAdapter
import com.examvan.app.api.ApiClient
import com.examvan.app.databinding.ActivityExamListBinding
import java.text.SimpleDateFormat
import java.util.*

/**
 * Screen 2: Exam List
 * - Fetches active exams from /api/exams
 * - Displays in RecyclerView with pull-to-refresh
 * - Tap an exam to open PDF viewer
 */
class ExamListActivity : AppCompatActivity() {

    private lateinit var binding: ActivityExamListBinding
    private lateinit var adapter: ExamAdapter

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        // FLAG_SECURE
        window.setFlags(
            WindowManager.LayoutParams.FLAG_SECURE,
            WindowManager.LayoutParams.FLAG_SECURE
        )
        // Keep screen turned on during the exam
        window.addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)

        // Clear clipboard for security
        val clipboard = getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager
        try {
            clipboard.clearPrimaryClip()
        } catch (_: Exception) { }

        binding = ActivityExamListBinding.inflate(layoutInflater)
        setContentView(binding.root)

        // Setup server URL from intent
        val serverUrl = intent.getStringExtra("server_url") ?: ""
        if (serverUrl.isNotEmpty()) {
            ApiClient.setBaseUrl(serverUrl)
        }

        // Setup RecyclerView
        adapter = ExamAdapter { exam ->
            val intent = Intent(this, ExamViewerActivity::class.java)
            intent.putExtra("exam_id", exam.id)
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
                        adapter.updateData(response.data)
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
        val sdf = SimpleDateFormat("HH:mm:ss", Locale.getDefault())
        binding.tvLastSync.text = "Sync: ${sdf.format(Date())}"
    }
}
