package com.examvan.app.helper

/**
 * Keputusan indikator status perangkat selama ujian (fitur baru: indikator
 * baterai + koneksi internet di toolbar ExamViewerActivity).
 *
 * Murni fungsi — wiring receiver/network callback ada di activity; object
 * ini hanya memutuskan kategori & keadaan online agar terkunci test JVM.
 */
object DeviceStatusPolicy {

    /** Kategori level baterai untuk pemilihan warna/prioritas tampilan. */
    enum class BatteryLevel { CRITICAL, LOW, NORMAL }

    /** Ambang kategori (persen). */
    const val CRITICAL_THRESHOLD = 15
    const val LOW_THRESHOLD = 30

    /**
     * Kategori baterai dari persentase. Nilai di luar 0..100 dicukup —
     * sumber ACTION_BATTERY_CHANGED sesekali memberi nilai aneh.
     *  - < 15%  : CRITICAL
     *  - 15–29% : LOW
     *  - >= 30% : NORMAL
     */
    fun batteryCategory(levelPercent: Int): BatteryLevel {
        val level = levelPercent.coerceIn(0, 100)
        return when {
            level < CRITICAL_THRESHOLD -> BatteryLevel.CRITICAL
            level < LOW_THRESHOLD -> BatteryLevel.LOW
            else -> BatteryLevel.NORMAL
        }
    }

    /**
     * True bila jaringan benar-benar bisa dipakai ke internet:
     * capability INTERNET saja belum cukup (captive portal / login hotspot
     * memiliki capability tanpa VALIDATED).
     */
    fun isOnline(hasInternet: Boolean, validated: Boolean): Boolean =
        hasInternet && validated

    /**
     * Keputusan online dari JARINGAN AKTIF saat ini (fix review fitur #1:
     * perangkat bisa punya WiFi + seluler bersamaan — kehilangan satu
     * transport tidak berarti offline; keputusan harus dari capabilities
     * jaringan aktif, bukan semantik per-event).
     *
     * @param activeHasInternet null = tidak ada jaringan aktif.
     * @param activeValidated   null = tidak ada jaringan aktif.
     */
    fun isOnlineFromActiveNetwork(activeHasInternet: Boolean?, activeValidated: Boolean?): Boolean {
        if (activeHasInternet == null || activeValidated == null) return false
        return isOnline(activeHasInternet, activeValidated)
    }
}
