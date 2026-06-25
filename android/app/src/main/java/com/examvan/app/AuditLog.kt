package com.examvan.app

import android.util.Log

/**
 * Security audit event logger untuk EXAMVAN Strict Mode.
 *
 * Mencatat semua event keamanan penting ke logcat dengan format:
 *   [AuditLog] <event_type> | <detail>
 *
 * Event types:
 *  - LOCKTASK_ACTIVATE   : Lock task diaktifkan (DPM / regular)
 *  - LOCKTASK_DEACTIVATE : Lock task dinonaktifkan
 *  - LOCKTASK_LOST       : Lock task hilang (detected by health check)
 *  - LOCKTASK_REJECTED   : User menolak dialog konfirmasi pinning
 *  - LOCKTASK_HEALTH     : Health check OK / re-activation
 *  - FOCUS_LOST_SUSPICIOUS : Focus hilang mencurigakan (overlay sistem)
 *  - USER_EXIT_ATTEMPT   : User mencoba keluar di strict mode
 *  - BACK_GESTURE_BLOCK  : Back gesture di-block oleh anti-spam
 *  - SUBMIT_SUCCESS      : Jawaban berhasil dikirim + stop lock task
 *  - SUBMIT_FAILED       : Jawaban gagal dikirim di strict mode
 *
 * Logging ini bisa di-extract oleh ADB logcat untuk audit forensik:
 *   adb logcat -s AuditLog
 */
object AuditLog {

    private const val TAG = "AuditLog"

    // Event counters — bisa dipantau secara real-time via logcat grep
    private val counters = mutableMapOf<String, Int>()

    private const val SEP = " | "

    // ── Public logging API ──────────────────────────────────────────

    fun i(event: String, detail: String) {
        increment(event)
        Log.i(TAG, "$event$SEP$detail")
    }

    fun w(event: String, detail: String) {
        increment(event)
        Log.w(TAG, "⚠️ $event$SEP$detail")
    }

    fun e(event: String, detail: String) {
        increment(event)
        Log.e(TAG, "❌ $event$SEP$detail")
    }

    /**
     * Reset all counters (panggil di akhir sesi ujian).
     */
    fun reset() {
        counters.clear()
    }

    // ── Event type constants ────────────────────────────────────────

    object Events {
        const val LOCKTASK_ACTIVATE = "LOCKTASK_ACTIVATE"
        const val LOCKTASK_DEACTIVATE = "LOCKTASK_DEACTIVATE"
        const val LOCKTASK_LOST = "LOCKTASK_LOST"
        const val LOCKTASK_REJECTED = "LOCKTASK_REJECTED"
        const val LOCKTASK_HEALTH_OK = "LOCKTASK_HEALTH_OK"
        const val LOCKTASK_HEALTH_RECOVER = "LOCKTASK_HEALTH_RECOVER"
        const val FOCUS_LOST_SUSPICIOUS = "FOCUS_LOST_SUSPICIOUS"
        const val FOCUS_RESTORED = "FOCUS_RESTORED"
        const val USER_EXIT_ATTEMPT = "USER_EXIT_ATTEMPT"
        const val BACK_BLOCKED = "BACK_BLOCKED"
        const val SUBMIT_SUCCESS = "SUBMIT_SUCCESS"
        const val SUBMIT_FAILED = "SUBMIT_FAILED"
        const val AUTO_SUBMIT = "AUTO_SUBMIT"
        const val USER_LEAVE_DETECTED = "USER_LEAVE_DETECTED"
        const val POWER_BUTTON_SCREEN_OFF = "SCREEN_OFF"
    }

    // ── Private ─────────────────────────────────────────────────────

    private fun increment(event: String) {
        counters[event] = (counters[event] ?: 0) + 1
    }
}
