package com.examvan.app.helper

/**
 * Heuristik deteksi lingkungan berbahaya (emulator, root, manipulasi jam) —
 * diekstrak dari SecurityEnforcer sebagai fungsi murni (fix temuan review:
 * "root detection menjalankan Runtime.exec + waitFor di UI thread tiap
 * onResume; stdout proses tidak dikonsumsi penuh").
 *
 * Pemisahan logika dari I/O memberi dua manfaat:
 * 1. Keputusan bisa dites di JVM (EnvironmentCheckPolicyTest).
 * 2. Scan I/O (File.exists, exec) bisa dijalankan di Dispatchers.IO oleh
 *    SecurityEnforcer tanpa menyentuh Activity / view.
 */
object EnvironmentCheckPolicy {

    /**
     * Path marker root. Marker mount menangkap solusi root modern walau
     * binary su-nya disembunyikan DenyList:
     *   - /data/adb/magisk -> Magisk v20+
     *   - /data/adb/ksu    -> KernelSU
     *   - /data/adb/apd    -> APatch
     *   - /sbin/.magisk    -> Magisk legacy
     */
    val ROOT_MARKER_PATHS: List<String> = listOf(
        "/system/app/Superuser.apk",
        "/sbin/su",
        "/system/bin/su",
        "/system/xbin/su",
        "/data/local/xbin/su",
        "/data/local/bin/su",
        "/system/sd/xbin/su",
        "/system/bin/failsafe/su",
        "/data/local/su",
        "/data/adb/magisk",
        "/data/adb/ksu",
        "/data/adb/apd",
        "/sbin/.magisk"
    )

    /** Ambang default pergeseran drift jam yang dianggap manipulasi. */
    const val CLOCK_DRIFT_THRESHOLD_MS = 10_000L

    /** Hasil evaluasi drift jam: tampered + besar pergeserannya. */
    data class ClockDriftResult(val tampered: Boolean, val driftDeltaMs: Long)

    /** Kunci properti Build yang dipakai [isEmulator]. */
    private val EMULATOR_PROP_KEYS = listOf(
        "fingerprint", "model", "manufacturer", "hardware",
        "product", "board", "brand", "device"
    )

    /**
     * Varian berbasis Map dari [isEmulator] — bentuk yang dipakai test dan
     * caller yang sudah mengumpulkan properti Build ke satu map.
     */
    fun isEmulator(buildProps: Map<String, String>): Boolean {
        return isEmulator(
            fingerprint = buildProps["fingerprint"] ?: "",
            model = buildProps["model"] ?: "",
            manufacturer = buildProps["manufacturer"] ?: "",
            hardware = buildProps["hardware"] ?: "",
            product = buildProps["product"] ?: "",
            board = buildProps["board"] ?: "",
            brand = buildProps["brand"] ?: "",
            device = buildProps["device"] ?: ""
        )
    }

    /**
     * Deteksi emulator dari properti Build. Semua parameter dikirim eksplisit
     * agar fungsi murni dan mudah dites (Build.* tidak bisa dimock di JVM).
     */
    fun isEmulator(
        fingerprint: String,
        model: String,
        manufacturer: String,
        hardware: String,
        product: String,
        board: String,
        brand: String,
        device: String
    ): Boolean {
        return (fingerprint.startsWith("generic")
                || fingerprint.startsWith("unknown")
                || model.contains("google_sdk")
                || model.contains("Emulator")
                || model.contains("Android SDK built for x86")
                || manufacturer.contains("Genymotion")
                || hardware.contains("goldfish")
                || hardware.contains("ranchu")
                || product.contains("sdk_gphone")
                || product.contains("google_sdk")
                || product.contains("emulator")
                || board.contains("nox")
                || manufacturer.contains("nox")
                || brand.startsWith("generic") && device.startsWith("generic")
                || "google_sdk" == product)
    }

    /**
     * Keputusan root dari hasil scan I/O yang sudah dikumpulkan caller:
     * [existingPaths] = subset ROOT_MARKER_PATHS yang benar-benar ada di disk,
     * [buildTags] = Build.TAGS (null aman). Build test-keys adalah ciri khas
     * custom ROM / build pre-rooted.
     */
    fun isRootedByMarkers(existingPaths: Set<String>, buildTags: String?): Boolean {
        if (existingPaths.isNotEmpty()) return true
        return buildTags != null && buildTags.contains("test-keys")
    }

    /**
     * Evaluasi manipulasi jam: bandingkan drift saat ini
     * (System.currentTimeMillis() - SystemClock.elapsedRealtime()) terhadap
     * baseline awal sesi. Pergeseran melebihi [thresholdMs] berarti jam sistem
     * diubah di tengah ujian.
     */
    fun evaluateClockDrift(baselineDriftMs: Long, currentDriftMs: Long, thresholdMs: Long): ClockDriftResult {
        val delta = currentDriftMs - baselineDriftMs
        return ClockDriftResult(tampered = kotlin.math.abs(delta) > thresholdMs, driftDeltaMs = delta)
    }
}
