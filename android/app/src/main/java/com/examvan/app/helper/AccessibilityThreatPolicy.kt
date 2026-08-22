package com.examvan.app.helper

/**
 * Klasifikasi accessibility service (fix temuan review: "accessibility check
 * hanya Log.w, tidak pernah memberi peringatan ke user padahal komentar
 * bilang tampilkan peringatan — service berbahaya bisa membaca soal atau
 * melakukan gesture injection tanpa ketahuan").
 *
 * BaseSecureActivity memakai policy ini tiap onCreate:
 *  - SUSPICIOUS  → dialog peringatan ke siswa + catatan AuditLog.
 *  - WHITELISTED → layanan aksesibilitas legitimate bawaan sistem; diam.
 *  - BENIGN      → tidak punya kemampuan berbahaya; diam.
 *
 * Murni fungsi agar bisa dites di JVM (AccessibilityThreatPolicyTest).
 */
object AccessibilityThreatPolicy {

    enum class Threat { WHITELISTED, SUSPICIOUS, BENIGN }

    /**
     * Layanan aksesibilitas legitimate bawaan sistem. TalkBack & kawan-kawan
     * memiliki canRetrieveWindowContent — mereka di-whitelist agar siswa
     * disabilitas tetap bisa mengikuti ujian.
     */
    private val WHITELIST_IDS = listOf(
        "talkback",
        "select_to_speak",
        "selecttospeak", // id paket resmi: com.google.android.accessibility.selecttospeak
        "switchaccess"
    )

    /**
     * Klasifikasikan satu service berdasarkan id dan kapabilitasnya.
     *
     * @param serviceId id service (flattened ComponentName); null/blank aman.
     * @param canRetrieveWindowContent service bisa membaca isi layar.
     * @param canPerformGestures service bisa menyuntik gesture (CAPABILITY_CAN_PERFORM_GESTURES).
     */
    fun classify(
        serviceId: String?,
        canRetrieveWindowContent: Boolean,
        canPerformGestures: Boolean
    ): Threat {
        if (serviceId.isNullOrBlank()) return Threat.BENIGN

        val lower = serviceId.lowercase()
        if (WHITELIST_IDS.any { lower.contains(it) }) return Threat.WHITELISTED

        return if (canRetrieveWindowContent || canPerformGestures) Threat.SUSPICIOUS
        else Threat.BENIGN
    }
}
