package com.examvan.app.helper

/**
 * Gerbang polling berbasis lifecycle (fix temuan review mode medium ronde
 * 5 #3: polling approval terus berjalan saat activity background).
 *
 * Kontrak:
 *  - [resume] = true bila transisi paused→aktif (caller mulai poll).
 *  - [pause]  = true bila transisi aktif→paused (caller removeCallbacks).
 *  - [shouldRun] = false → runnable langsung return tanpa menjadwalkan
 *    iterasi berikutnya.
 * Cek terminal (approved/rejected/cancel) tetap tanggung jawab caller.
 */
class PollingGate(initiallyPaused: Boolean) {

    @Volatile
    var paused: Boolean = initiallyPaused
        private set

    /** @return true bila ini transisi paused→aktif. */
    fun resume(): Boolean {
        if (!paused) return false
        paused = false
        return true
    }

    /** @return true bila ini transisi aktif→paused. */
    fun pause(): Boolean {
        if (paused) return false
        paused = true
        return true
    }

    fun shouldRun(): Boolean = !paused
}
