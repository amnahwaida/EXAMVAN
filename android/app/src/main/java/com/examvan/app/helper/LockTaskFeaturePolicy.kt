package com.examvan.app.helper

/**
 * Kebijakan fitur sistem yang aktif/nonaktif SELAMA lock task (fix temuan
 * review strict ronde 5 #1: "setLockTaskFeatures API 30+ tidak dimanfaatkan
 * — notification shade & recents tetap bisa diakses siswa saat ter-pin di
 * perangkat kiosk").
 *
 * HANYA berlaku untuk tier Device-Owner/DPM (flavor kiosk, device admin
 * aktif): dipanggil via DevicePolicyManager.setLockTaskFeatures() sebelum
 * startLockTask(). Regular pinning (student flavor) tidak mendukung API ini
 * dan tetap bergantung pada proteksi aplikasi.
 *
 * Konstanta adalah MIRROR nilai resmi DevicePolicyManager API 30 (stabil,
 * bagian dari public API) — dikunci LockTaskFeaturePolicyTest agar perubahan
 * angka selalu disadari.
 */
object LockTaskFeaturePolicy {

    /** Tombol/gesture Home. Redundan dengan lock task sendiri — mati. */
    const val FEATURE_HOME = 1

    /** Layar Recent Apps. Mati. */
    const val FEATURE_RECENT_TASKS = 2

    /** Power menu (matikan/restart/emergency). PERTAHANKAN — keselamatan. */
    const val FEATURE_GLOBAL_ACTIONS = 4

    /** Notification shade + Quick Settings. Jalur bypass utama — MATI. */
    const val FEATURE_NOTIFICATIONS = 8

    /** Panel volume/system info. Dikelola app (intercept volume key). */
    const val FEATURE_SYSTEM_INFO = 16

    /** Layar kunci tetap berfungsi normal. Pertahankan. */
    const val FEATURE_KEYGUARD = 32

    /**
     * Bitmask yang dikirim ke DevicePolicyManager.setLockTaskFeatures():
     * semua fitur sistem pengalih perhatian dimatikan, fungsi darurat dan
     * keamanan dasar dipertahankan.
     */
    fun allowedFeatures(): Int =
        FEATURE_GLOBAL_ACTIONS or FEATURE_SYSTEM_INFO or FEATURE_KEYGUARD
}
