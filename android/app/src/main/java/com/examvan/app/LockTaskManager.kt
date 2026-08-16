package com.examvan.app

import android.app.Activity
import android.app.ActivityManager
import android.app.admin.DevicePolicyManager
import android.content.ComponentName
import android.content.Context
import android.os.Build
import android.os.Handler
import android.os.Looper
import android.util.Log

/**
 * Centralized Lock Task (screen pinning) manager for EXAMVAN Strict Mode.
 *
 * ## Strategy aktivasi (2-tier):
 *
 * 1. **DevicePolicyManager.setLockTaskPackages()** — silent pinning TANPA
 *    dialog konfirmasi. Hanya bekerja jika komponen DeviceAdmin aktif
 *    (component name: `com.examvan.app.receiver.MyDeviceAdminReceiver`).
 *    Ini adalah "kiosk mode" untuk perangkat sekolah.
 *
 * 2. **Activity.startLockTask()** — fallback untuk perangkat siswa.
 *    Menampilkan dialog konfirmasi "Pin [app]?" yang bisa ditolak user.
 *
 * ## Fitur:
 *  - Safe start/stop dengan try-catch penuh
 *  - Verifikasi aktivasi via ActivityManager.getLockTaskModeState()
 *  - Periodic health check untuk deteksi dini jika Lock Task hilang
 *    (process death, system intervention, OS update, OEM bypass)
 *
 * ## Keterbatasan:
 * Lock Task hanya memblokir tombol Home, Recent Apps, dan gesture navigasi
 * terkait. TIDAK memblokir:
 *  - Notification shade / Quick Settings
 *  - Google Assistant (voice atau corner swipe)
 *  - OEM Edge Panels (Samsung, OPPO, Xiaomi)
 *  - Power button long-press (Bixby, Emergency)
 *  - Volume key long-press (Accessibility)
 * Lihat [ExamViewerActivity] untuk perlindungan tambahan terhadap ini.
 *
 * Catatan: compileSdk 34 mengekspos isInLockTaskMode() via ActivityManager,
 * BUKAN Activity. Untuk API 29+ prefer getLockTaskModeState().
 */
object LockTaskManager {

    private const val TAG = "LockTaskManager"
    private const val HEALTH_CHECK_INTERVAL_MS = 15_000L // 15 detik

    private var healthCheckHandler: Handler? = null
    private var healthCheckRunnable: Runnable? = null
    private var healthCheckActive = false

    var isPinningPending = false
        private set

    // Component name untuk DeviceAdminReceiver — menggunakan string literal
    // agar kompatibel di kedua flavor (student & kiosk). Di student flavor
    // adminReceiver tidak terdaftar di manifest, jadi isAdminActive() = false.
    private const val ADMIN_RECEIVER_CLASS = "com.examvan.app.receiver.MyDeviceAdminReceiver"

    // ── Public API ──────────────────────────────────────────────────

    /**
     * Attempt to start lock task (screen pinning).
     *
     * Strategy:
     *  1. Coba DPM-based lock task (kiosk / device admin aktif) — silent, tanpa dialog
     *  2. Fallback ke startLockTask biasa — bisa muncul dialog konfirmasi
     *
     * @return true jika aktivasi berhasil (verifikasi tertunda tetap diperlukan
     *         via [isActive] di onResume/onWindowFocusChanged).
     *         false jika SecurityException terjadi.
     *
     * NOTE: minSdk=24, API 23+ guard tidak diperlukan.
     */
    fun activate(activity: Activity, onResult: ((Boolean) -> Unit)? = null): Boolean {
        if (isLockTaskActive(activity)) {
            Log.d(TAG, "Already in lock task mode")
            onResult?.invoke(true)
            return true
        }
        // Tier 1: DPM-based silent lock task (device admin aktif)
        if (tryDpmLockTask(activity)) {
            Log.i(TAG, "Lock task activated via DevicePolicyManager (silent)")
            AuditLog.i(AuditLog.Events.LOCKTASK_ACTIVATE, "DPM:silent")
            onResult?.invoke(true)
            return true
        }
        // Tier 2: Regular startLockTask (mungkin muncul dialog konfirmasi)
        isPinningPending = true
        val result = tryStartLockTask(activity) { success ->
            isPinningPending = false
            onResult?.invoke(success)
        }
        if (result) {
            AuditLog.i(AuditLog.Events.LOCKTASK_ACTIVATE, "regular")
        } else {
            isPinningPending = false
            AuditLog.e(AuditLog.Events.LOCKTASK_ACTIVATE, "FAILED")
            onResult?.invoke(false)
        }
        return result
    }

    /**
     * Stop lock task (unpin). Safe dipanggil meski tidak sedang pinned.
     * Setelah unpin, kunci layar bisa dibuka via tombol Home/Recents.
     */
    fun deactivate(activity: Activity) {
        try {
            stopHealthCheck()
            activity.stopLockTask()
            Log.d(TAG, "stopLockTask() called")
            AuditLog.i(AuditLog.Events.LOCKTASK_DEACTIVATE, "normal")
        } catch (e: Exception) {
            Log.w(TAG, "Error stopping lock task (mungkin sudah tidak aktif)", e)
            AuditLog.w(AuditLog.Events.LOCKTASK_DEACTIVATE, "already_inactive")
        }
    }

    /**
     * Cek apakah lock task sedang aktif.
     * Thread-safe, bisa dipanggil dari coroutine dispatcher mana pun.
     */
    fun isActive(activity: Activity): Boolean {
        return isLockTaskActive(activity)
    }

    // ── Periodic health check ──────────────────────────────────────

    /**
     * Start periodic lock task health monitoring.
     * Jika lock task hilang (process death, system service restart,
     * OS update, OEM bypass), coba re-activate via DPM dulu.
     *
     * Wajib dipanggil dari main thread. Auto-stop jika activity finish.
     */
    fun startHealthCheck(activity: Activity) {
        stopHealthCheck()
        healthCheckActive = true
        healthCheckHandler = Handler(Looper.getMainLooper())
        healthCheckRunnable = Runnable {
            if (!healthCheckActive || activity.isFinishing || activity.isDestroyed) {
                stopHealthCheck()
                return@Runnable
            }
            if (!isLockTaskActive(activity)) {
                Log.w(TAG, "HealthCheck: Lock task lost! Re-activating...")
                AuditLog.w(AuditLog.Events.LOCKTASK_LOST, "health_check")
                // Coba DPM dulu, baru fallback
                if (!tryDpmLockTask(activity)) {
                    tryStartLockTask(activity)
                }
                AuditLog.i(AuditLog.Events.LOCKTASK_HEALTH_RECOVER, "re-activation_requested")
            } else {
                AuditLog.i(AuditLog.Events.LOCKTASK_HEALTH_OK, "ok")
            }
            healthCheckHandler?.postDelayed(healthCheckRunnable!!, HEALTH_CHECK_INTERVAL_MS)
        }
        healthCheckHandler?.postDelayed(healthCheckRunnable!!, HEALTH_CHECK_INTERVAL_MS)
        Log.d(TAG, "Health check started (interval=${HEALTH_CHECK_INTERVAL_MS}ms)")
    }

    /**
     * Stop health monitoring. Aman dipanggil berkali-kali.
     */
    fun stopHealthCheck() {
        healthCheckActive = false
        healthCheckRunnable?.let { healthCheckHandler?.removeCallbacks(it) }
        healthCheckRunnable = null
        healthCheckHandler = null
    }

    // ── Private: DPM-based lock task (kiosk mode) ──────────────────

    /**
     * Attempt lock task via DevicePolicyManager.
     *
     * Keuntungan: setLockTaskPackages() mengaktifkan screen pinning
     * **tanpa dialog konfirmasi**. User tidak bisa menolak.
     *
     * Prasyarat:
     *  - Device Admin aktif (MyDeviceAdminReceiver terdaftar & di-enable)
     *  - API 23+
     *
     * Catatan: method ini menggunakan ComponentName via string FQN,
     * bukan class reference, sehingga tidak tergantung pada keberadaan
     * MyDeviceAdminReceiver di classpath (kompatibel student & kiosk).
     */
    private fun tryDpmLockTask(activity: Activity): Boolean {
        try {
            val dpm = activity.getSystemService(Context.DEVICE_POLICY_SERVICE) as? DevicePolicyManager
                ?: return false

            val cn = ComponentName(activity.packageName, ADMIN_RECEIVER_CLASS)

            // Cek apakah admin ini benar-benar aktif (device admin enabled)
            if (!dpm.isAdminActive(cn)) return false

            // Enable lock task untuk package kita — ini membuat startLockTask()
            // berikutnya tidak memunculkan dialog konfirmasi.
            val packageName = activity.packageName
            dpm.setLockTaskPackages(cn, arrayOf(packageName))
            Log.d(TAG, "DPM: setLockTaskPackages() berhasil untuk $packageName")

            // Cek apakah lock task policy didukung oleh admin.
            // Pada compileSdk 34, API ini menerima String (packageName),
            // bukan ComponentName (berubah di API 33+).
            if (!dpm.isLockTaskPermitted(packageName)) {
                Log.w(TAG, "Device admin aktif tapi lock task tidak diizinkan " +
                        "(cek device_admin_rules.xml)")
                return false
            }

            // Sekarang startLockTask() bisa dipanggil — tanpa dialog
            activity.startLockTask()
            Log.i(TAG, "DPM: startLockTask() siluman berhasil")

            // Verifikasi cepat
            if (isLockTaskActive(activity)) {
                Log.i(TAG, "DPM lock task aktif dan terverifikasi")
            } else {
                Log.w(TAG, "DPM lock task dipanggil tapi belum aktif " +
                        "(mungkin butuh waktu)")
            }
            return true
        } catch (e: SecurityException) {
            Log.w(TAG, "DPM lock task ditolak: ${e.message}")
        } catch (e: Exception) {
            Log.w(TAG, "DPM lock task error", e)
        }
        return false
    }

    // ── Private: Regular startLockTask (student mode) ──────────────

    /**
     * Fallback: startLockTask biasa. Mungkin menampilkan dialog konfirmasi.
     * @return true jika sukses dipanggil (verifikasi tetap via [isActive]).
     */
    private fun tryStartLockTask(activity: Activity, onResult: (Boolean) -> Unit = {}): Boolean {
        return try {
            activity.startLockTask()
            Log.d(TAG, "startLockTask() called (regular)")

            // Deferred verifikasi: jika user menolak dialog konfirmasi,
            // lock task tidak aktif tapi tidak ada exception.
            deferredLogOnReject(activity, onResult)
            true
        } catch (e: SecurityException) {
            Log.e(TAG, "SecurityException — mungkin device admin/policy block", e)
            false
        } catch (e: Exception) {
            Log.e(TAG, "Unexpected error saat startLockTask", e)
            false
        }
    }

    // ── Private: Status check ────────────────────────────────────

    /**
     * Check lock task state via ActivityManager (cross-API compatible).
     *
     * - API 29+: ActivityManager.getLockTaskModeState() → LOCK_TASK_MODE_PINNED
     * - API 23-28: ActivityManager.isInLockTaskMode() (deprecated tapi akurat)
     */
    private fun isLockTaskActive(activity: Activity): Boolean {
        try {
            val am = activity.getSystemService(Context.ACTIVITY_SERVICE) as? ActivityManager ?: return false
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
                val state = am.lockTaskModeState
                return state == ActivityManager.LOCK_TASK_MODE_PINNED || state == ActivityManager.LOCK_TASK_MODE_LOCKED
            } else {
                @Suppress("DEPRECATION")
                return am.isInLockTaskMode
            }
        } catch (e: SecurityException) {
            // SecurityException terjadi jika app tidak memiliki izin
            // yang diperlukan untuk query state lock task
            Log.w(TAG, "SecurityException cek lock task state", e)
            return false
        } catch (e: IllegalStateException) {
            // IllegalStateException jika ActivityManager tidak tersedia
            // atau dalam state tidak valid
            Log.w(TAG, "IllegalStateException cek lock task state", e)
            return false
        }
    }

    // ── Private: Activation logging ──────────────────────────────

    /**
     * Multi-poll verifikasi apakah startLockTask diterima user.
     *
     * Tidak ada callback dari sistem saat user menolak dialog konfirmasi
     * pinning, jadi kita polling isLockTaskActive() dengan interval.
     *
     * Strategi: poll 15 kali (400ms interval, total 6 detik).
     */
    private fun deferredLogOnReject(activity: Activity, onResult: (Boolean) -> Unit) {
        val pollHandler = Handler(Looper.getMainLooper())
        val pollIntervalMs = 400L
        val maxPolls = 15
        var pollCount = 0

        val pollRunnable = object : Runnable {
            override fun run() {
                pollCount++
                if (activity.isFinishing || activity.isDestroyed) {
                    onResult(false)
                    return
                }

                if (isLockTaskActive(activity)) {
                    // Lock task aktif — user menerima dialog
                    Log.d(TAG, "Lock task aktivasi dikonfirmasi user (poll #$pollCount)")
                    onResult(true)
                    return // stop polling — sukses
                }

                if (pollCount >= maxPolls) {
                    // Semua poll gagal — user menolak dialog atau timeout
                    Log.w(TAG, "startLockTask REGULAR ditolak user (${maxPolls}x polls failed)")
                    AuditLog.w(AuditLog.Events.LOCKTASK_REJECTED, "user_cancelled_dialog")
                    onResult(false)
                    return // stop polling — ditolak
                }

                // Poll lagi dengan interval tetap
                pollHandler.postDelayed(this, pollIntervalMs)
            }
        }
        pollHandler.postDelayed(pollRunnable, pollIntervalMs)
    }
}
