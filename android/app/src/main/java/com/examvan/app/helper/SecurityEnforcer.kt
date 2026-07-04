package com.examvan.app.helper

import android.app.Activity
import android.content.Context
import android.content.Intent
import android.os.Build
import android.os.Handler
import android.os.Looper
import android.provider.Settings
import android.util.Log
import android.view.View
import android.view.WindowInsets
import android.view.WindowInsetsController
import android.widget.Toast
import com.examvan.app.AuditLog
import com.examvan.app.LockTaskManager
import com.examvan.app.R
import com.examvan.app.databinding.ActivityExamViewerBinding
import com.google.android.material.snackbar.Snackbar

/**
 * Handles security enforcement for exam sessions:
 * - Strict mode (lock task / screen pinning)
 * - Immersive mode (navigation bar hiding)
 * - Focus loss detection (system overlay protection)
 * - Volume key interception (prevent volume panel bypass)
 * - Gesture navigation warning
 */
class SecurityEnforcer(
    private val activity: Activity,
    private val binding: ActivityExamViewerBinding
) {
    var securityLevel: String = "medium"

    /** Strict mode: lock task required. */
    var strictMode: Boolean = false
        private set

    var strictModeFailed: Boolean = false
        private set
    var lockTaskActivated: Boolean = false
        private set

    // Anti-spam: prevent rapid back gesture bypass
    private var lastBackPressTime = 0L

    // Volume key grace period
    var volumeKeyPressedAt: Long = 0L

    // Popup window counter for spinner dropdowns
    var activePopupCount: Int = 0

    // Focus lost runnable
    private var focusLostRunnable: Runnable? = null

    // Gesture warning flag
    private var gestureWarningShown = false

    // Whether app dialog is showing
    var isShowingAppDialog: Boolean = false

    private var gestureRetryCallback: (() -> Unit)? = null
    var isGestureBlockedShowing: Boolean = false

    var onCreateTime: Long = 0L
    var isPdfReady: Boolean = false
    var submittedOrExited: Boolean = false

    // Callback when user requests to logout
    var onLogoutRequested: (() -> Unit)? = null

    // Callback for back button press (used in strict mode)
    var onBackBlocked: (() -> Unit)? = null

    // Hidden API intent action for system navigation settings
    companion object {
        private const val TAG = "SecurityEnforcer"
        private const val ACTION_SYSTEM_NAVIGATION_SETTINGS =
            "android.settings.SYSTEM_NAVIGATION_SETTINGS"
        private const val BACK_PRESS_COOLDOWN_MS = 1200L
    }

    /**
     * Attempt to activate strict mode (lock task).
     * Returns true if successful, false otherwise.
     */
    fun activateStrictMode(onConfirmed: () -> Unit = {}): Boolean {
        strictMode = true
        val activated = LockTaskManager.activate(activity) { success ->
            if (success) {
                strictModeFailed = false
                lockTaskActivated = true
                enterImmersiveMode()
                restoreButtonVisibility()
                binding.layoutError.visibility = View.GONE
                binding.btnRetryDownload.text = activity.getString(R.string.btn_retry)
                LockTaskManager.startHealthCheck(activity)
                onConfirmed()
            } else {
                strictModeFailed = true
                showStrictModeFailed {
                    retryStrictMode(onConfirmed)
                }
            }
        }
        lockTaskActivated = activated

        if (activated) {
            enterImmersiveMode()
        } else {
            strictModeFailed = true
            Log.e(TAG, "CRITICAL: Lock task gagal diaktifkan di strict mode!")
        }
        return activated
    }

    /**
     * Show strict mode failure UI and configure retry button.
     */
    fun showStrictModeFailed(retryCallback: () -> Unit) {
        binding.btnBack.visibility = View.GONE
        binding.btnToggleAnswerSheet.visibility = View.GONE
        binding.btnSubmitAnswers.visibility = View.GONE
        binding.btnPrev.visibility = View.GONE
        binding.btnNext.visibility = View.GONE
        binding.answerSheetToggle.visibility = View.GONE
        binding.layoutDownload.visibility = View.GONE
        binding.ivPdfPage.visibility = View.GONE

        binding.tvErrorMsg.text = "Mode STRICT GAGAL diaktifkan!\n\n" +
                "Ketuk 'Coba Lagi' untuk mencoba mengaktifkan ulang.\n" +
                "Jika terus gagal, tekan 'Keluar' dan hubungi pengawas."
        binding.btnRetryDownload.text = "Coba Lagi"
        binding.layoutError.visibility = View.VISIBLE

        binding.btnRetryDownload.setOnClickListener {
            retryCallback()
        }
    }

    /**
     * Retry strict mode activation after failure.
     */
    fun retryStrictMode(onConfirmed: () -> Unit = {}): Boolean {
        val retryActivated = LockTaskManager.activate(activity) { success ->
            if (success) {
                strictModeFailed = false
                lockTaskActivated = true
                enterImmersiveMode()
                restoreButtonVisibility()
                binding.layoutError.visibility = View.GONE
                binding.btnRetryDownload.text = activity.getString(R.string.btn_retry)
                LockTaskManager.startHealthCheck(activity)
                onConfirmed()
            } else {
                strictModeFailed = true
                showStrictModeFailed {
                    retryStrictMode(onConfirmed)
                }
            }
        }
        if (!retryActivated) {
            strictModeFailed = true
        }
        return retryActivated
    }

    private fun restoreButtonVisibility() {
        binding.btnBack.visibility = View.VISIBLE
        binding.btnToggleAnswerSheet.visibility = View.VISIBLE
        binding.btnSubmitAnswers.visibility = View.VISIBLE
        binding.btnPrev.visibility = View.VISIBLE
        binding.btnNext.visibility = View.VISIBLE
        binding.answerSheetToggle.visibility = View.VISIBLE
    }

    /**
     * Enter immersive sticky mode to hide navigation bar.
     * Prevents gesture navigation bypass in strict mode.
     */
    fun enterImmersiveMode() {
        if (!strictMode && !strictModeFailed) return
        if (submittedOrExited) return
        try {
            if (Build.VERSION.SDK_INT >= 30) {
                val controller = activity.window.decorView.windowInsetsController ?: return
                controller.hide(
                    WindowInsets.Type.systemBars()
                        or WindowInsets.Type.statusBars()
                        or WindowInsets.Type.navigationBars()
                )
                controller.systemBarsBehavior =
                    WindowInsetsController.BEHAVIOR_SHOW_TRANSIENT_BARS_BY_SWIPE
            } else {
                @Suppress("DEPRECATION")
                activity.window.decorView.systemUiVisibility = (
                    View.SYSTEM_UI_FLAG_IMMERSIVE_STICKY or
                    View.SYSTEM_UI_FLAG_HIDE_NAVIGATION or
                    View.SYSTEM_UI_FLAG_FULLSCREEN or
                    View.SYSTEM_UI_FLAG_LAYOUT_STABLE or
                    View.SYSTEM_UI_FLAG_LAYOUT_HIDE_NAVIGATION or
                    View.SYSTEM_UI_FLAG_LAYOUT_FULLSCREEN
                )
            }
        } catch (_: Throwable) { }
    }

    /**
     * Handle back button press with anti-spam.
     */
    fun handleBackPressed() {
        val now = System.currentTimeMillis()
        if (now - lastBackPressTime < BACK_PRESS_COOLDOWN_MS) return
        lastBackPressTime = now

        if (strictMode) {
            Toast.makeText(activity, activity.getString(R.string.strict_mode_cannot_exit), Toast.LENGTH_SHORT).show()
            AuditLog.w(AuditLog.Events.USER_EXIT_ATTEMPT, "back_gesture_blocked")
            onBackBlocked?.invoke()
            return
        }
        onLogoutRequested?.invoke()
    }

    /**
     * Handle Home button / Recent Apps (onUserLeaveHint in strict mode).
     */
    fun handleUserLeave() {
        if (submittedOrExited) return
        if (!isPdfReady) return
        if (isShowingAppDialog) return
        if (System.currentTimeMillis() - onCreateTime < 3000) return

        if (strictMode) {
            Log.w(TAG, "Defense-in-depth: onUserLeaveHint di strict mode!")
            AuditLog.w(AuditLog.Events.USER_LEAVE_DETECTED, "strict_mode")
            LockTaskManager.activate(activity)
            return
        }
    }

    /**
     * Handle window focus changes for system overlay detection.
     */
    fun handleWindowFocusChanged(hasFocus: Boolean, autoSubmitCallback: () -> Unit) {
        if (hasFocus) {
            if (System.currentTimeMillis() - volumeKeyPressedAt < 1500) return

            focusLostRunnable?.let {
                Handler(Looper.getMainLooper()).removeCallbacks(it)
                focusLostRunnable = null
            }
            activePopupCount = 0

            if (strictMode && !LockTaskManager.isPinningPending && !LockTaskManager.isActive(activity)) {
                Log.w(TAG, "Lock task inactive on focus gained — re-activating")
                LockTaskManager.activate(activity) { success ->
                    if (!success) {
                        strictModeFailed = true
                        showStrictModeFailed { retryStrictMode() }
                    }
                }
            }
            if (strictMode) {
                enterImmersiveMode()
            }
            AuditLog.i(AuditLog.Events.FOCUS_RESTORED,
                "strict=$strictMode isActive=${LockTaskManager.isActive(activity)}")
        } else {
            if (strictMode && !isShowingAppDialog && isPdfReady && !submittedOrExited) {
                if (System.currentTimeMillis() - volumeKeyPressedAt < 1500) return
                if (activePopupCount > 0) return

                AuditLog.w(AuditLog.Events.FOCUS_LOST_SUSPICIOUS,
                    "strict=$strictMode activePopup=$activePopupCount")

                focusLostRunnable?.let {
                    Handler(Looper.getMainLooper()).removeCallbacks(it)
                }

                focusLostRunnable = Runnable {
                    if (activity.isFinishing || activity.isDestroyed) {
                        focusLostRunnable = null
                        return@Runnable
                    }
                    if (activity.hasWindowFocus()) {
                        focusLostRunnable = null
                        return@Runnable
                    }
                    if (strictMode && !LockTaskManager.isPinningPending && !LockTaskManager.isActive(activity)) {
                        Log.w(TAG, "Focus lost + lock task inactive — kemungkinan system overlay/bypass")
                        LockTaskManager.activate(activity) { success ->
                            if (!success) {
                                strictModeFailed = true
                                showStrictModeFailed { retryStrictMode() }
                            }
                        }
                    }
                    focusLostRunnable = null
                }
                Handler(Looper.getMainLooper()).postDelayed(focusLostRunnable!!, 500)
            }
        }
    }

    /**
     * Handle volume key press (suppress system UI).
     */
    fun handleVolumeKey(keyCode: Int): Boolean {
        val handled = (keyCode == android.view.KeyEvent.KEYCODE_VOLUME_UP ||
                keyCode == android.view.KeyEvent.KEYCODE_VOLUME_DOWN)
        if (handled) {
            volumeKeyPressedAt = System.currentTimeMillis()
            try {
                val audioManager = activity.getSystemService(Context.AUDIO_SERVICE) as android.media.AudioManager
                val direction = if (keyCode == android.view.KeyEvent.KEYCODE_VOLUME_UP) {
                    android.media.AudioManager.ADJUST_RAISE
                } else {
                    android.media.AudioManager.ADJUST_LOWER
                }
                audioManager.adjustStreamVolume(android.media.AudioManager.STREAM_MUSIC, direction, 0)
            } catch (e: Throwable) {
                Log.w(TAG, "Volume adjustment failed", e)
            }
        }
        return handled
    }

    /**
     * Block volume key long-press (prevent accessibility/Assistant trigger).
     */
    fun handleVolumeKeyLongPress(): Boolean = true

    /**
     * Log power button press in strict mode.
     */
    fun handlePowerKey() {
        if (strictMode) {
            Log.w(TAG, "Power button pressed in strict mode")
            AuditLog.w(AuditLog.Events.POWER_BUTTON_SCREEN_OFF, "strict_mode")
        }
    }

    /**
     * Block power button long-press (prevent Assistant/Bixby trigger).
     */
    fun handlePowerKeyLongPress(): Boolean {
        if (strictMode) {
            Log.w(TAG, "Power button long-press — potensi bypass via Assistant/Bixby")
            AuditLog.w(AuditLog.Events.POWER_BUTTON_SCREEN_OFF, "strict_mode_long_press")
            return true
        }
        return false
    }

    fun isGestureNavigationEnabled(): Boolean {
        return try {
            val resources = activity.resources
            val resourceId = resources.getIdentifier("config_navBarInteractionMode", "integer", "android")
            val mode = if (resourceId > 0) {
                resources.getInteger(resourceId)
            } else {
                Settings.Secure.getInt(activity.contentResolver, "navigation_mode", 0)
            }
            mode == 2
        } catch (e: Exception) {
            false
        }
    }

    fun showGestureBlocked(retryCallback: () -> Unit) {
        gestureRetryCallback = retryCallback
        isGestureBlockedShowing = true

        binding.btnBack.visibility = View.GONE
        binding.btnToggleAnswerSheet.visibility = View.GONE
        binding.btnSubmitAnswers.visibility = View.GONE
        binding.btnPrev.visibility = View.GONE
        binding.btnNext.visibility = View.GONE
        binding.answerSheetToggle.visibility = View.GONE
        binding.layoutDownload.visibility = View.GONE
        binding.ivPdfPage.visibility = View.GONE

        binding.tvErrorMsg.text = "Navigasi Gesture Terdeteksi!\n\n" +
                "Untuk keamanan ujian, Anda WAJIB mengaktifkan Navigasi 3 Tombol (3-Button Navigation) terlebih dahulu.\n\nSilakan ubah mode navigasi Anda."

        val isGesture = isGestureNavigationEnabled()
        if (isGesture) {
            binding.btnRetryDownload.text = "Buka Settings"
            binding.btnRetryDownload.setOnClickListener {
                val navIntent = buildNavigationSettingsIntent()
                safeStartSettings(navIntent)
                showGestureBlocked(retryCallback)
            }
        } else {
            binding.btnRetryDownload.text = "Mulai Ujian"
            binding.btnRetryDownload.setOnClickListener {
                binding.layoutError.visibility = View.GONE
                isGestureBlockedShowing = false
                retryCallback()
            }
        }
        binding.layoutError.visibility = View.VISIBLE
    }

    /**
     * Show one-time gesture navigation warning in strict mode.
     */
    fun showGestureWarningOnce() {
        if (!strictMode || gestureWarningShown || activity.isFinishing || activity.isDestroyed) return
        if (!isGestureNavigationEnabled()) return
        gestureWarningShown = true
        if (Build.VERSION.SDK_INT < 28) return

        try {
            Handler(Looper.getMainLooper()).postDelayed({
                if (activity.isFinishing || activity.isDestroyed) return@postDelayed

                val navIntent = buildNavigationSettingsIntent()
                val canResolve = navIntent.resolveActivity(activity.packageManager) != null

                val message = if (canResolve && Build.VERSION.SDK_INT >= 30) {
                    "Gunakan 3-Button Navigation agar ujian aman"
                } else {
                    "Nonaktifkan gesture navigasi: Settings -> Sistem -> Gestur -> 3 tombol"
                }

                Snackbar.make(binding.root, message, Snackbar.LENGTH_INDEFINITE).apply {
                    if (canResolve) {
                        setAction("Buka Settings") {
                            safeStartSettings(navIntent)
                        }
                    } else {
                        setAction("Tutup", null)
                    }
                    setDuration(10000)
                    show()
                }
            }, 2000)
        } catch (_: Exception) { }
    }

    private fun buildNavigationSettingsIntent(): Intent {
        val action = if (Build.VERSION.SDK_INT >= 30) {
            ACTION_SYSTEM_NAVIGATION_SETTINGS
        } else {
            Settings.ACTION_SETTINGS
        }
        val intent = Intent(action).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        if (intent.resolveActivity(activity.packageManager) != null) {
            return intent
        }
        return Intent(Settings.ACTION_SETTINGS).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
    }

    private fun safeStartSettings(intent: Intent) {
        try {
            activity.startActivity(intent)
        } catch (e1: Exception) {
            try {
                activity.startActivity(
                    Intent(Settings.ACTION_SETTINGS).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
                )
            } catch (e2: Exception) {
                Log.w(TAG, "Gagal membuka Settings", e2)
            }
        }
    }

    /**
     * Verify lock task is still active; re-activate if needed.
     * Returns true if lock task was restored.
     */
    fun verifyLockTask(): Boolean {
        if (LockTaskManager.isPinningPending) return false
        if (strictMode && lockTaskActivated && !LockTaskManager.isActive(activity)) {
            Log.w(TAG, "Lock task inactive — re-activating")
            return LockTaskManager.activate(activity) { success ->
                if (!success) {
                    strictModeFailed = true
                    showStrictModeFailed { retryStrictMode() }
                }
            }
        }
        return false
    }

    /**
     * Re-apply immersive mode and start health check.
     */
    fun onResume() {
        if (isGestureBlockedShowing) {
            gestureRetryCallback?.let { showGestureBlocked(it) }
            return
        }
        if (strictMode) {
            LockTaskManager.stopHealthCheck()
            if (LockTaskManager.isPinningPending) return
            if (!LockTaskManager.isActive(activity)) {
                Log.w(TAG, "Lock task inactive di onResume — re-activating")
                LockTaskManager.activate(activity) { success ->
                    if (!success) {
                        strictModeFailed = true
                        showStrictModeFailed { retryStrictMode() }
                    }
                }
            } else {
                LockTaskManager.startHealthCheck(activity)
            }
        }
    }

    fun deactivateLockTask() {
        if (strictMode) {
            strictMode = false
            LockTaskManager.deactivate(activity)
        }
    }

    fun stopHealthCheck() {
        LockTaskManager.stopHealthCheck()
    }

    fun cleanup() {
        stopHealthCheck()
    }
}
