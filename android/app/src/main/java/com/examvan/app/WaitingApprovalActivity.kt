package com.examvan.app

import android.animation.ObjectAnimator
import android.animation.PropertyValuesHolder
import android.animation.ValueAnimator
import android.content.Intent
import android.graphics.drawable.GradientDrawable
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.view.View
import android.view.animation.AccelerateDecelerateInterpolator
import android.widget.LinearLayout
import android.widget.TextView
import androidx.appcompat.app.AlertDialog
import androidx.core.content.ContextCompat
import com.examvan.app.api.ApiClient
import com.examvan.app.databinding.ActivityWaitingApprovalBinding
import com.examvan.app.helper.UpdateManager
import org.json.JSONObject
import com.examvan.app.BuildConfig

/**
 * Halaman Menunggu Persetujuan Pengawas
 *
 * Ditampilkan setelah siswa mengisi token dan identitas.
 * Melakukan polling ke server setiap 5 detik untuk mengecek status approval.
 * Jika disetujui → masuk ke ExamViewerActivity
 * Jika ditolak → tampilkan pesan ditolak
 */
class WaitingApprovalActivity : BaseSecureActivity() {

    private lateinit var binding: ActivityWaitingApprovalBinding

    // Exam data passed via Intent
    private var examId: Int = -1
    private var examName: String = ""
    private var serverUrl: String = ""
    private var token: String = ""
    private var studentName: String = ""
    private var studentNumber: String = ""
    private var studentClass: String = ""
    private var identityDataStr: String = "{}"
    private var endTime: String? = null
    private var securityLevel: String = "medium"
    private var strictMode: Boolean = false

    private val handler = Handler(Looper.getMainLooper())
    private var isWaiting = true
    private var isFirstCheck = true
    private var pulseAnimator: ObjectAnimator? = null
    private var dotsAnimator: ValueAnimator? = null

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        binding = ActivityWaitingApprovalBinding.inflate(layoutInflater)
        setContentView(binding.root)
        applyEdgeToEdgeInsets(binding.rootLayout)

        // Extract intent data
        extractIntentData()

        // Setup server URL
        if (serverUrl.isNotEmpty()) {
            ApiClient.setBaseUrl(serverUrl)
        }

        // Populate UI
        populateExamInfo()
        populateIdentityInfo()

        // Make status dot circular
        makeStatusDotCircular()

        // Start animations
        startPulseAnimation()
        startDotsAnimation()

        // Setup cancel button
        binding.btnCancel.setOnClickListener {
            showCancelConfirmation()
        }

        // Start polling for approval
        handler.post(pollRunnable)

        // Belt-and-braces: catch an outdated build via health once per day
        // (the 426 path above uses server-side enforcement).
        maybeCheckVersion()
    }

    private fun extractIntentData() {
        examId = intent.getIntExtra("exam_id", -1)
        examName = intent.getStringExtra("exam_name") ?: ""
        serverUrl = intent.getStringExtra("server_url") ?: ""
        token = intent.getStringExtra("exam_token") ?: ""
        studentName = intent.getStringExtra("student_name") ?: ""
        studentNumber = intent.getStringExtra("student_number") ?: ""
        studentClass = intent.getStringExtra("student_class") ?: ""
        identityDataStr = intent.getStringExtra("identity_data") ?: "{}"
        endTime = intent.getStringExtra("end_time")
        securityLevel = intent.getStringExtra("security_level") ?: "medium"
        strictMode = intent.getBooleanExtra("strict_mode", false)
    }

    private fun populateExamInfo() {
        binding.tvExamName.text = examName.ifEmpty { getString(R.string.default_exam_name) }
    }

    private fun populateIdentityInfo() {
        val container = binding.containerIdentityFields
        container.removeAllViews()

        val identityJson = try {
            JSONObject(identityDataStr)
        } catch (_: Exception) {
            JSONObject()
        }

        // Show identity fields from the JSON data
        val keys = identityJson.keys()
        while (keys.hasNext()) {
            val key = keys.next()
            val value = identityJson.optString(key, "")
            if (value.isNotEmpty()) {
                addIdentityRow(container, formatFieldLabel(key), value)
            }
        }

        // Fallback if no identity data from JSON
        if (identityJson.length() == 0) {
            if (studentName.isNotEmpty()) {
                addIdentityRow(container, getString(R.string.label_student_name), studentName)
            }
            if (studentNumber.isNotEmpty()) {
                addIdentityRow(container, getString(R.string.label_exam_number), studentNumber)
            }
            if (studentClass.isNotEmpty()) {
                addIdentityRow(container, getString(R.string.label_student_class), studentClass)
            }
        }
    }

    private fun addIdentityRow(container: LinearLayout, label: String, value: String) {
        val rowLayout = LinearLayout(this).apply {
            orientation = LinearLayout.HORIZONTAL
            layoutParams = LinearLayout.LayoutParams(
                LinearLayout.LayoutParams.MATCH_PARENT,
                LinearLayout.LayoutParams.WRAP_CONTENT
            ).apply { bottomMargin = 6 }
        }

        val labelView = TextView(this).apply {
            text = "$label:"
            textSize = 13f
            setTextColor(ContextCompat.getColor(this@WaitingApprovalActivity, R.color.text_secondary))
            layoutParams = LinearLayout.LayoutParams(0, LinearLayout.LayoutParams.WRAP_CONTENT, 0.4f)
        }

        val valueView = TextView(this).apply {
            text = value
            textSize = 14f
            setTextColor(ContextCompat.getColor(this@WaitingApprovalActivity, R.color.on_surface))
            typeface = android.graphics.Typeface.DEFAULT_BOLD
            layoutParams = LinearLayout.LayoutParams(0, LinearLayout.LayoutParams.WRAP_CONTENT, 0.6f)
        }

        rowLayout.addView(labelView)
        rowLayout.addView(valueView)
        container.addView(rowLayout)
    }

    private fun formatFieldLabel(key: String): String {
        return key.replace("_", " ")
            .replaceFirstChar { it.uppercase() }
            .replace("Student name", "Nama")
            .replace("Exam number", "Nomor")
            .replace("Student class", "Kelas")
    }

    private fun makeStatusDotCircular() {
        val dot = binding.viewStatusDot
        val drawable = GradientDrawable().apply {
            shape = GradientDrawable.OVAL
            setColor(ContextCompat.getColor(this@WaitingApprovalActivity, R.color.success))
        }
        dot.background = drawable
    }

    // ── Animations ──────────────────────────────────────────────────

    private fun startPulseAnimation() {
        val scaleX = PropertyValuesHolder.ofFloat(View.SCALE_X, 1.0f, 1.08f)
        val scaleY = PropertyValuesHolder.ofFloat(View.SCALE_Y, 1.0f, 1.08f)
        pulseAnimator = ObjectAnimator.ofPropertyValuesHolder(binding.tvWaitingIcon, scaleX, scaleY).apply {
            duration = 1200
            repeatCount = ObjectAnimator.INFINITE
            repeatMode = ObjectAnimator.REVERSE
            interpolator = AccelerateDecelerateInterpolator()
            start()
        }
    }

    private fun startDotsAnimation() {
        val patterns = listOf("●  ○  ○", "○  ●  ○", "○  ○  ●", "○  ●  ○")
        var index = 0
        dotsAnimator = ValueAnimator.ofInt(0, patterns.size - 1).apply {
            duration = (patterns.size * 500).toLong()
            repeatCount = ValueAnimator.INFINITE
            addUpdateListener {
                val newIndex = (System.currentTimeMillis() / 500 % patterns.size).toInt()
                if (newIndex != index) {
                    index = newIndex
                    binding.tvPollingDots.text = patterns[index]
                }
            }
            start()
        }
    }

    // ── Polling ─────────────────────────────────────────────────────

    private val pollRunnable = object : Runnable {
        override fun run() {
            if (!isWaiting) return

            val macAddress = DeviceIdResolver.resolveDeviceId(this@WaitingApprovalActivity)

            ApiClient.requestApproval(
                examId = examId,
                macAddress = macAddress,
                studentName = studentName,
                examNumber = studentNumber,
                studentClass = studentClass,
                identityDataStr = identityDataStr,
                reset = false,
                token = token,
                onSuccess = { status ->
                    runOnUiThread {
                        if (!isWaiting) return@runOnUiThread
                        when (status) {
                            "approved" -> onApproved()
                            "rejected" -> onRejected()
                            else -> {
                                // Still pending, update UI to show connected
                                showConnected()
                                handler.postDelayed(this, 5000)
                            }
                        }
                    }
                },
                onError = { statusCode, errorMsg ->
                    runOnUiThread {
                        if (!isWaiting) return@runOnUiThread
                        if (statusCode == ApiClient.HTTP_UPGRADE_REQUIRED) {
                            onUpdateRequired()
                        } else {
                            showConnectionWarning(errorMsg)
                            handler.postDelayed(this, 5000)
                        }
                    }
                }
            )
        }
    }

    // ── Status Handlers ─────────────────────────────────────────────

    private fun onApproved() {
        isWaiting = false
        stopAnimations()

        // Brief approved animation before proceeding
        binding.tvWaitingIcon.text = "✅"
        binding.tvWaitingTitle.text = getString(R.string.approval_approved_title)
        binding.tvWaitingSubtitle.text = getString(R.string.approval_approved_message)
        binding.progressCircular.visibility = View.GONE
        binding.tvPollingDots.visibility = View.GONE

        handler.postDelayed({
            startExamViewer()
        }, 1200)
    }

    private fun onRejected() {
        isWaiting = false
        stopAnimations()

        // Update waiting card to rejected state
        binding.tvWaitingIcon.text = "🚫"
        binding.tvWaitingTitle.text = getString(R.string.approval_rejected_title)
        binding.tvWaitingSubtitle.text = getString(R.string.approval_rejected_subtitle)
        binding.progressCircular.visibility = View.GONE
        binding.tvPollingDots.visibility = View.GONE

        // Show rejected detail card
        binding.layoutRejected.visibility = View.VISIBLE
        binding.layoutWarning.visibility = View.GONE

        // Setup retry button
        binding.btnRetryRequest.setOnClickListener { retryApproval() }

        // Change cancel button to "Kembali"
        binding.btnCancel.text = getString(R.string.approval_btn_back)
        binding.btnCancel.setOnClickListener { finish() }
    }

    private fun retryApproval() {
        val macAddress = DeviceIdResolver.resolveDeviceId(this)

        // Send reset=true to flip rejected → pending on the server
        ApiClient.requestApproval(
            examId = examId,
            macAddress = macAddress,
            studentName = studentName,
            examNumber = studentNumber,
            studentClass = studentClass,
            identityDataStr = identityDataStr,
            reset = true,
            token = token,
            onSuccess = { _ ->
                runOnUiThread {
                    // Reset UI back to waiting state
                    binding.tvWaitingIcon.text = "⏳"
                    binding.tvWaitingTitle.text = getString(R.string.approval_waiting_title)
                    binding.tvWaitingSubtitle.text = getString(R.string.approval_waiting_subtitle)
                    binding.progressCircular.visibility = View.VISIBLE
                    binding.tvPollingDots.visibility = View.VISIBLE
                    binding.layoutRejected.visibility = View.GONE

                    // Restore cancel button
                    binding.btnCancel.text = getString(R.string.approval_btn_cancel)
                    binding.btnCancel.setOnClickListener { showCancelConfirmation() }

                    // Resume polling
                    isWaiting = true
                    startPulseAnimation()
                    startDotsAnimation()
                    handler.post(pollRunnable)
                }
            },
            onError = { statusCode, errorMsg ->
                runOnUiThread {
                    if (statusCode == ApiClient.HTTP_UPGRADE_REQUIRED) {
                        onUpdateRequired()
                    } else {
                        showConnectionWarning(errorMsg)
                    }
                }
            }
        )
    }

    private fun showConnected() {
        val dot = binding.viewStatusDot
        (dot.background as? GradientDrawable)?.setColor(
            ContextCompat.getColor(this, R.color.success)
        )
        binding.tvConnectionStatus.text = getString(R.string.approval_status_connected)
        binding.layoutWarning.visibility = View.GONE
    }

    private fun showConnectionWarning(errorMsg: String) {
        val dot = binding.viewStatusDot
        (dot.background as? GradientDrawable)?.setColor(
            ContextCompat.getColor(this, R.color.warning)
        )
        binding.tvConnectionStatus.text = getString(R.string.approval_status_retrying)
        binding.layoutWarning.visibility = View.VISIBLE
        binding.tvWarningMessage.text = getString(R.string.approval_warning_message, errorMsg)
    }

    /**
     * Called when the approval polling (or retry) receives HTTP 426 (app
     * update required). Stops polling and shows the blocking update dialog so
     * an outdated student cannot sit on the waiting screen forever — they must
     * update the APK. The dialog's "Buka Halaman Download" points at the
     * server's /download page (hosted on the same URL as the exam API).
     */
    private fun onUpdateRequired() {
        isWaiting = false
        stopAnimations()
        UpdateManager.showUpdateRequiredDialog(
            this,
            BuildConfig.VERSION_NAME,
            null,
            serverUrl
        )
    }

    /**
     * Check health once per day; show the blocking update dialog when this
     * build is older than the required version. A belt-and-braces layer on top
     * of the HTTP 426 handling above: the 426 only appears once the server's
     * android_version setting is raised, whereas this catches a mismatch the
     * moment health reports it (e.g. system_apps moved ahead first).
     */
    private fun maybeCheckVersion() {
        if (serverUrl.isEmpty()) return

        val configPrefs = AppPrefs.getConfigPrefsSafe(this)
        val lastCheck = configPrefs.getLong(AppPrefs.KEY_LAST_VERSION_CHECK_TS, 0L)
        val now = System.currentTimeMillis()
        if (now - lastCheck < 24 * 60 * 60 * 1000L) return

        configPrefs.edit().putLong(AppPrefs.KEY_LAST_VERSION_CHECK_TS, now).apply()

        ApiClient.checkHealth(
            onSuccess = { health ->
                val required = health.required_app_version
                if (required != null && UpdateManager.isOutdated(BuildConfig.VERSION_NAME, required)) {
                    runOnUiThread { onUpdateRequired() }
                }
            },
            onError = { /* jaringan — biarkan polling menangani pesannya */ }
        )
    }

    private fun showCancelConfirmation() {
        AlertDialog.Builder(this)
            .setTitle(getString(R.string.approval_cancel_title))
            .setMessage(getString(R.string.approval_cancel_message))
            .setPositiveButton(getString(R.string.approval_cancel_yes)) { _, _ ->
                isWaiting = false
                stopAnimations()
                finish()
            }
            .setNegativeButton(getString(R.string.approval_cancel_no), null)
            .show()
    }

    // ── Navigation ──────────────────────────────────────────────────

    private fun startExamViewer() {
        val intent = Intent(this, ExamViewerActivity::class.java).apply {
            putExtra("exam_id", examId)
            putExtra("exam_name", examName)
            putExtra("server_url", serverUrl)
            putExtra("exam_token", token)
            putExtra("student_name", studentName)
            putExtra("student_number", studentNumber)
            putExtra("student_class", studentClass)
            putExtra("identity_data", identityDataStr)
            putExtra("end_time", endTime)
            putExtra("security_level", securityLevel)
            putExtra("strict_mode", strictMode)
        }
        startActivity(intent)
        finish() // Close this waiting screen
    }

    // ── Lifecycle ───────────────────────────────────────────────────

    private fun stopAnimations() {
        pulseAnimator?.cancel()
        dotsAnimator?.cancel()
        handler.removeCallbacksAndMessages(null)
    }

    override fun onDestroy() {
        isWaiting = false
        stopAnimations()
        super.onDestroy()
    }

    @Deprecated("Deprecated in Java")
    override fun onBackPressed() {
        if (isWaiting) {
            showCancelConfirmation()
        } else {
            @Suppress("DEPRECATION")
            super.onBackPressed()
        }
    }
}
