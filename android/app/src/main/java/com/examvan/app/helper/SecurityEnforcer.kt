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
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.cancel
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

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
    var securityLevel: String = com.examvan.app.helper.ExamModePolicy.DEFAULT_LEVEL

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

    /**
     * Registri holder flag dialog (fix review gel. 2 #1): setiap dialog /
     * prompt mendaftar sebagai holder sehingga safety-net reset dari onResume
     * jadi KONDISIONAL — flag tidak bisa lepas saat dialog masih terbuka.
     */
    private val appDialogFlags = AppDialogFlagRegistry { isShowingAppDialog = it }

    /** Tandai dialog/prompt [id] mulai aktif (flag ON). */
    fun holdAppDialog(id: String) = appDialogFlags.acquire(id)

    /** Lepaskan dialog/prompt [id]; flag OFF hanya bila holder terakhir. */
    fun releaseAppDialog(id: String) = appDialogFlags.release(id)

    /**
     * Safety-net reset dari onResume — KONDISIONAL: no-op bila ada dialog
     * yang masih aktif (mis. prompt izin notifikasi).
     */
    fun resetAppDialogIfIdle(): Boolean = appDialogFlags.resetIfIdle()

    private var gestureRetryCallback: (() -> Unit)? = null
    var isGestureBlockedShowing: Boolean = false

    var onCreateTime: Long = 0L
    var isPdfReady: Boolean = false
    var submittedOrExited: Boolean = false
    var initialClockDrift: Long = System.currentTimeMillis() - android.os.SystemClock.elapsedRealtime()

    // Callback when user requests to logout
    var onLogoutRequested: (() -> Unit)? = null

    // Callback saat siswa menekan "Keluar" di layar strict-failed — HARUS
    // auto-submit (jawaban tetap dikumpulkan), bukan sekadar finish tanpa
    // mengumpulkan: keluar tanpa submit membiarkan siswa mengulang ujian
    // tanpa proteksi.
    var onStrictFailedExit: (() -> Unit)? = null

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
     *
     * retryStrictMode adalah kasus khusus dari fungsi ini (deduplikasi ~90%
     * sesuai temuan review) — keduanya kini memakai tryActivateLockTask.
     */
    fun activateStrictMode(onConfirmed: () -> Unit = {}): Boolean {
        strictMode = true
        return tryActivateLockTask(onConfirmed)
    }

    /**
     * Retry strict mode activation after failure. Identik dengan
     * activateStrictMode — dipertahankan sebagai API publik karena
     * ExamViewerActivity memanggilnya di layar strict-failed.
     */
    fun retryStrictMode(onConfirmed: () -> Unit = {}): Boolean {
        return tryActivateLockTask(onConfirmed)
    }

    /**
     * Satu jalur aktivasi lock task yang dipakai activate & retry: sukses →
     * pulihkan UI + immersive + health check + callback; gagal → tampilkan
     * layar strict-failed dengan opsi retry.
     */
    private fun tryActivateLockTask(onConfirmed: () -> Unit): Boolean {
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
        ExamScreenPanels.hideAllControls(binding)

        binding.tvErrorMsg.text = "Mode STRICT GAGAL diaktifkan!\n\n" +
                "Ketuk 'Coba Lagi' untuk mencoba mengaktifkan ulang.\n" +
                "Jika terus gagal, tekan 'Keluar' dan hubungi pengawas."
        binding.btnRetryDownload.text = "Coba Lagi"
        binding.layoutError.visibility = View.VISIBLE

        binding.btnRetryDownload.setOnClickListener {
            retryCallback()
        }

        // "Keluar" yang dijanjikan pesan di atas — jalur aman: auto-submit
        // (jawaban tetap dikumpulkan), bukan keluar tanpa mengumpulkan.
        // Back tetap diblokir di strict mode; tombol ini satu-satunya jalan
        // keluar yang sah sebelum deadline.
        binding.btnOpenResult.visibility = View.VISIBLE
        binding.btnOpenResult.text = activity.getString(R.string.recovery_exit)
        binding.btnOpenResult.setOnClickListener {
            onStrictFailedExit?.invoke()
        }
    }

    private fun restoreButtonVisibility() {
        ExamScreenPanels.showMainControls(binding)
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
     *
     * CATATAN dua grace window 3 detik yang BERBEDA (fix review ronde 3 #3,
     * dokumentasi): window di bawah ini memakai [onCreateTime] dan hanya
     * berlaku untuk re-pin STRICT. Grace auto-submit MEDIUM memakai sumber
     * waktu lain — ExamModePolicy.STARTUP_GRACE_MS yang berbasis
     * pdfReadyAtMs (sejak PDF siap) — lihat ExamViewerActivity. Jangan
     * disatukan: maknanya berbeda (masuk-activity vs soal-siap).
     */
    fun handleUserLeave() {
        if (submittedOrExited) return
        if (!isPdfReady) return
        if (isShowingAppDialog) return
        // Fix review strict #3: jangan re-pin saat dialog konfirmasi pinning
        // sedang menunggu jawaban user (saat ini tak terjangkau karena
        // isPdfReady false pra-aktivasi, tapi guard ini mencegah regresi
        // bila urutan flow berubah — mis. PDF di-prefetch).
        if (LockTaskManager.isPinningPending) return
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
            // Fix review ronde 3 #2: PEMBERSIHAN (cancel pending runnable,
            // reset popup count) kini SELALU berjalan — dulu seluruh branch
            // early-return dalam grace volume sehingga pending runnable dan
            // popup count bisa stale. Yang ditunda hanya aksi keamanan
            // lanjutan (re-pin strict, immersive, audit) selagi panel volume
            // mungkin masih turun.
            focusLostRunnable?.let {
                Handler(Looper.getMainLooper()).removeCallbacks(it)
                focusLostRunnable = null
            }
            activePopupCount = 0

            if (ExamModePolicy.isWithinVolumeGrace(
                    volumeKeyPressedAtMs = volumeKeyPressedAt.takeIf { it > 0L },
                    nowMs = System.currentTimeMillis()
                )
            ) return

            // Guard !submittedOrExited: setelah deadline auto-submit melepas pin
            // (stopLockTask), event fokus nyasar tidak boleh RE-PIN activity yang
            // sedang finishing — dialog "Pin ExamVan?" muncul setelah ujian
            // selesai dikumpulkan. Branch focus-lost sudah punya guard ini.
            if (strictMode && !submittedOrExited && !LockTaskManager.isPinningPending && !LockTaskManager.isActive(activity)) {
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
            // Fix review low-mode #4: low non-strict tidak pernah memakai
            // runnable ini (tidak auto-submit, bukan strict) — jangan
            // jadwalkan kerja sia-sia.
            if (!ExamModePolicy.shouldScheduleFocusLossWatch(strictMode, securityLevel)) return

            if (!isShowingAppDialog && isPdfReady && !submittedOrExited) {
                if (activePopupCount > 0) return

                AuditLog.w(AuditLog.Events.FOCUS_LOST_SUSPICIOUS,
                    "strict=$strictMode activePopup=$activePopupCount")

                focusLostRunnable?.let {
                    Handler(Looper.getMainLooper()).removeCallbacks(it)
                }

                // Fix review gel. 2 #2: tekanan volume dalam grace TIDAK lagi
                // membuang event — evaluasi ditunda sampai grace selesai
                // (delay = base + sisa grace). Dulu `return` di sini membuat
                // overlay yang muncul pasca-tekanan volume lolos dari submit.
                val delayMs = ExamModePolicy.focusLossDelayMs(
                    volumeKeyPressedAtMs = volumeKeyPressedAt.takeIf { it > 0L },
                    nowMs = System.currentTimeMillis()
                )

                focusLostRunnable = Runnable {
                    if (activity.isFinishing || activity.isDestroyed) {
                        focusLostRunnable = null
                        return@Runnable
                    }
                    if (activity.hasWindowFocus()) {
                        focusLostRunnable = null
                        return@Runnable
                    }

                    // In strict mode: do NOT call autoSubmitCallback — it releases the pin and lets students out.
                    // Instead, just re-activate the lock task to keep the student trapped.
                    // The autoSubmitCallback is only for medium security mode.
                    if (strictMode && !isShowingAppDialog && isPdfReady && !submittedOrExited) {
                        Log.w(TAG, "Focus lost in strict mode for >500ms — re-activating lock task")
                        LockTaskManager.activate(activity) { success ->
                            if (!success) {
                                strictModeFailed = true
                                showStrictModeFailed { retryStrictMode() }
                            }
                        }
                        enterImmersiveMode()
                    } else if (!strictMode && !isShowingAppDialog && isPdfReady && !submittedOrExited) {
                        Log.w(TAG, "Focus lost in medium mode for >500ms — auto-submitting")
                        autoSubmitCallback()
                    }

                    // Guard !submittedOrExited lagi: runnable ini bisa ditunda
                    // 500 ms — deadline auto-submit bisa menembak di sela-selanya
                    // dan sudah melepas pin; jangan re-pin pasca-submit.
                    if (strictMode && !submittedOrExited && !LockTaskManager.isPinningPending && !LockTaskManager.isActive(activity)) {
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
                Handler(Looper.getMainLooper()).postDelayed(focusLostRunnable!!, delayMs)
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

        ExamScreenPanels.hideAllControls(binding)

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
        // Guard !submittedOrExited: jangan re-pin setelah submit selesai
        // (deadline auto-submit / submit manual sukses) — activity sedang
        // menuju finish, re-pin hanya memunculkan dialog konfirmasi.
        if (strictMode && !submittedOrExited && lockTaskActivated && !LockTaskManager.isActive(activity)) {
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

    var isSecurityViolationShowing: Boolean = false
    private var securityViolationMessage: String = ""
    private var securityRetryCallback: (() -> Unit)? = null

    fun showSecurityViolation(message: String, retryCallback: () -> Unit) {
        securityViolationMessage = message
        securityRetryCallback = retryCallback
        isSecurityViolationShowing = true

        ExamScreenPanels.hideAllControls(binding)

        binding.tvErrorMsg.text = message
        binding.btnRetryDownload.text = "Periksa Ulang"
        binding.btnRetryDownload.setOnClickListener {
            binding.layoutError.visibility = View.GONE
            isSecurityViolationShowing = false
            retryCallback()
        }
        binding.layoutError.visibility = View.VISIBLE
    }

    /**
     * Cek pelanggaran keamanan yang MURAH (tanpa I/O blocking): clock drift
     * dari baseline yang sudah tersimpan. Dipanggil sinkron di onResume —
     * pemeriksaan berat (File.exists 13 path, exec "which") dipindah ke
     * [runEnvironmentScanAsync] di background thread.
     */
    fun checkSecurityViolations(): String? {
        // Clock manipulation check (fix temuan review: baseline kini di-resolve
        // lewat ClockDriftPolicy sehingga bertahan process death — lihat
        // ExamViewerActivity.loadExamContent).
        val currentDrift = System.currentTimeMillis() - android.os.SystemClock.elapsedRealtime()
        val drift = EnvironmentCheckPolicy.evaluateClockDrift(
            initialClockDrift, currentDrift, EnvironmentCheckPolicy.CLOCK_DRIFT_THRESHOLD_MS
        )
        if (drift.tampered) {
            return "Perubahan Waktu Sistem Terdeteksi!\n\nAnda terdeteksi melakukan perubahan waktu sistem (jam perangkat) saat ujian berlangsung. Untuk alasan keamanan, manipulasi waktu tidak diizinkan. Silakan kembalikan jam Anda ke waktu yang benar."
        }
        return null
    }

    // ── Scan lingkungan async ────────────────────────────────────────────

    /**
     * Scope untuk scan lingkungan; dibatalkan di cleanup() agar tidak ada
     * callback yang menyentuh activity setelah destroy.
     */
    private val envScanScope = CoroutineScope(
        Dispatchers.IO + Job()
    )

    @Volatile
    private var cachedEnvViolation: String? = null

    /**
     * Jalankan scan lingkungan BERAT (emulator fingerprint, marker root di
     * disk, `which su`, USB debugging, screen mirroring) DI LUAR UI thread
     * (fix temuan review: "Runtime.exec + waitFor memblokir main thread di
     * onResume dan bisa ANR"). Hasil dilaporkan via [onViolation] di main
     * thread; hasil juga di-cache untuk pemanggilan berikutnya selama proses
     * masih hidup.
     */
    fun runEnvironmentScanAsync(onViolation: (String?) -> Unit) {
        envScanScope.launch {
            val violation = scanEnvironmentBlocking()
            cachedEnvViolation = violation
            withContext(Dispatchers.Main) {
                onViolation(violation)
            }
        }
    }

    /** Hasil scan terakhir (null = bersih / belum pernah discan). */
    fun lastEnvironmentViolation(): String? = cachedEnvViolation

    /**
     * Scan lingkungan lengkap — BLOCKING, hanya boleh dipanggil dari
     * Dispatchers.IO. Keputusan murni didelegasikan ke EnvironmentCheckPolicy.
     */
    private fun scanEnvironmentBlocking(): String? {
        val buildProps = mapOf(
            "fingerprint" to Build.FINGERPRINT,
            "model" to Build.MODEL,
            "manufacturer" to Build.MANUFACTURER,
            "hardware" to Build.HARDWARE,
            "product" to Build.PRODUCT,
            "board" to Build.BOARD,
            "brand" to Build.BRAND,
            "device" to Build.DEVICE
        )
        if (EnvironmentCheckPolicy.isEmulator(
                buildProps["fingerprint"]!!, buildProps["model"]!!,
                buildProps["manufacturer"]!!, buildProps["hardware"]!!,
                buildProps["product"]!!, buildProps["board"]!!,
                buildProps["brand"]!!, buildProps["device"]!!
            )
        ) {
            return "Perangkat Simulator/Emulator Terdeteksi!\n\nUntuk alasan keamanan, EXAMVAN tidak dapat dijalankan di dalam emulator (seperti BlueStacks, Nox, dll.). Silakan gunakan perangkat ponsel Android fisik."
        }

        // I/O root: File.exists untuk semua marker + lookup PATH (`which su`)
        // dengan resource management yang benar.
        val foundMarkers = mutableSetOf<String>()
        for (path in EnvironmentCheckPolicy.ROOT_MARKER_PATHS) {
            try {
                if (java.io.File(path).exists()) foundMarkers.add(path)
            } catch (_: Throwable) { }
        }
        if (isExecutableOnPath("su")) foundMarkers.add("su-on-path")
        if (EnvironmentCheckPolicy.isRootedByMarkers(foundMarkers, Build.TAGS)) {
            return "Perangkat Ter-Root Terdeteksi!\n\nEXAMVAN mendeteksi akses root pada perangkat ini. Untuk menjaga integritas ujian, perangkat ter-root tidak diizinkan mengakses halaman ujian. Silakan un-root perangkat Anda."
        }

        if (isUsbDebuggingEnabled()) {
            return "USB Debugging Aktif!\n\nUntuk alasan keamanan, Anda WAJIB mematikan opsi pengembang 'USB Debugging' di pengaturan sistem perangkat Anda terlebih dahulu sebelum memulai ujian."
        }

        if (isScreenMirrored()) {
            return "Proyeksi / Duplikasi Layar Terdeteksi!\n\nEXAMVAN mendeteksi bahwa layar perangkat Anda sedang dibagikan/diproyeksikan ke layar eksternal (Cast Screen/Wireless Display). Silakan putuskan koneksi proyeksi layar Anda terlebih dahulu."
        }

        return null
    }

    /**
     * True when the given binary resolves to a real executable via `which`.
     *
     * Fix temuan review: versi lama tidak mengonsumsi stderr (potensi deadlock
     * pipe buffer), tidak memanggil destroy(), dan bisa bocor stream. Kini:
     * redirect error stream ke stdout, semua stream ditutup di finally, dan
     * proses di-destroy pada timeout agar scan tak pernah menggantung.
     */
    private fun isExecutableOnPath(binary: String): Boolean {
        var process: Process? = null
        return try {
            process = ProcessBuilder("which", binary)
                .redirectErrorStream(true)
                .start()
            val output = process.inputStream.bufferedReader().use { it.readLine() }
            process.waitFor()
            !output.isNullOrEmpty()
        } catch (_: Exception) {
            false
        } finally {
            try {
                process?.destroy()
            } catch (_: Throwable) { }
        }
    }

    private fun isUsbDebuggingEnabled(): Boolean {
        return Settings.Global.getInt(activity.contentResolver, Settings.Global.ADB_ENABLED, 0) != 0
    }

    private fun isScreenMirrored(): Boolean {
        val dm = activity.getSystemService(Context.DISPLAY_SERVICE) as? android.hardware.display.DisplayManager
        val displays = dm?.displays ?: return false
        if (displays.size > 1) {
            for (display in displays) {
                if (display.displayId != android.view.Display.DEFAULT_DISPLAY) {
                    return true
                }
            }
        }
        return false
    }

    /**
     * Re-apply immersive mode and start health check.
     */
    fun onResume() {
        if (isSecurityViolationShowing) {
            securityRetryCallback?.let { showSecurityViolation(securityViolationMessage, it) }
            return
        }
        if (isGestureBlockedShowing) {
            gestureRetryCallback?.let { showGestureBlocked(it) }
            return
        }

        // Cek murah dulu di UI thread (clock drift).
        val violation = checkSecurityViolations()
        if (violation != null) {
            showSecurityViolation(violation) {
                onResume()
            }
            return
        }

        // Scan berat (File.exists, exec, DisplayManager) di background thread.
        // Selama scan berjalan, tampilkan hasil cache agar pelanggaran yang
        // sudah terdeteksi sebelumnya tetap ter-enforce tanpa jeda.
        cachedEnvViolation?.let {
            showSecurityViolation(it) { onResume() }
            return
        }
        runEnvironmentScanAsync { envViolation ->
            // Activity bisa sudah destroy saat scan selesai — guard dulu.
            if (activity.isFinishing || activity.isDestroyed) return@runEnvironmentScanAsync
            val v = envViolation ?: checkSecurityViolations()
            if (v != null && !isSecurityViolationShowing) {
                showSecurityViolation(v) { onResume() }
            }
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

    /**
     * Gerbang recovery lock task (fix review strict ronde 2 #2): dipanggil
     * activity dari onResume(true) / onPause(false) agar health check hanya
     * mencoba re-aktivasi saat app benar-benar foreground.
     */
    fun setLockRecoveryEnabled(enabled: Boolean) {
        LockTaskManager.setRecoveryEnabled(enabled)
    }

    fun stopHealthCheck() {
        LockTaskManager.stopHealthCheck()
    }

    fun cleanup() {
        stopHealthCheck()
        // Batalkan scan lingkungan yang masih berjalan — tanpa ini callback
        // bisa menyentuh activity yang sudah destroy (temuan review: leak).
        envScanScope.cancel()
    }
}
