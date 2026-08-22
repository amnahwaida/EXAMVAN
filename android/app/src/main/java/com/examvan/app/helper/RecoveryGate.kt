package com.examvan.app.helper

/**
 * Gerbang recovery lock task berbasis lifecycle (fix temuan review strict
 * ronde 2 #2: recovery health check mencoba startLockTask saat activity
 * background — gagal terus dan log spam tiap 15 detik sampai app dibuka).
 *
 * Pemilik gate (LockTaskManager) meng-enable saat activity resume dan
 * me-disable saat pause; runnable health check hanya mencoba re-aktivasi
 * bila [shouldAttemptRecovery] true. Default DISABLED karena health check
 * pertama dimulai dari onCreate — sebelum onResume pertama.
 */
class RecoveryGate(initiallyEnabled: Boolean = false) {

    @Volatile
    var enabled: Boolean = initiallyEnabled
        private set

    /**
     * @return true bila ini transisi nyata (caller bisa mencatat audit
     * sekali per perubahan, bukan spam tiap siklus poll).
     */
    @Synchronized
    fun setEnabled(value: Boolean): Boolean {
        if (enabled == value) return false
        enabled = value
        return true
    }

    fun shouldAttemptRecovery(): Boolean = enabled
}
