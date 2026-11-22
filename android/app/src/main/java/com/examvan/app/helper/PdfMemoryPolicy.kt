package com.examvan.app.helper

/**
 * Kebijakan memori render PDF — object murni (tanpa API Android) agar bisa
 * diuji di JVM.
 *
 * `ActivityManager.isLowRamDevice` TIDAK selalu true di perangkat 2 GB
 * (mis. Realme C11): flag itu utamanya untuk Android Go / perangkat yang
 * memang ditandai low-RAM oleh OEM. `memoryClass` (heap per-app) adalah
 * sinyal kedua yang lebih andal. Profil low-RAM dipakai bila salah satu
 * sinyal terpenuhi, sehingga perangkat ber-heap kecil tetap mendapat batas
 * bitmap yang aman walau flag-nya false.
 */
object PdfMemoryPolicy {

    /** Heap per-app (MB) di bawah/ sama dengan ini dianggap low-memory. */
    const val LOW_HEAP_THRESHOLD_MB = 256

    const val LOW_RAM_MAX_WIDTH = 1024
    const val LOW_RAM_MAX_HEIGHT = 1536
    // Cap normal diturunkan dari 2048x4096 ke 1536x2304: tetap tajam di HP
    // 1080p, tetapi bitmap turun ~44% sehingga perangkat ber-memori cukup
    // pun tidak menyimpan bitmap puluhan MB hanya untuk satu halaman PDF.
    const val NORMAL_MAX_WIDTH = 1536
    const val NORMAL_MAX_HEIGHT = 2304

    fun useLowRamProfile(isLowRamDevice: Boolean, memoryClassMb: Int): Boolean =
        isLowRamDevice || memoryClassMb <= LOW_HEAP_THRESHOLD_MB

    fun maxWidth(useLowRamProfile: Boolean): Int =
        if (useLowRamProfile) LOW_RAM_MAX_WIDTH else NORMAL_MAX_WIDTH

    fun maxHeight(useLowRamProfile: Boolean): Int =
        if (useLowRamProfile) LOW_RAM_MAX_HEIGHT else NORMAL_MAX_HEIGHT

    /**
     * Nilai `ComponentCallbacks2.TRIM_MEMORY_UI_HIDDEN` (20). Disalin sebagai
     * konstanta agar policy tetap murni/JVM-testable. Ambang ini dipakai
     * untuk melepas PDF di SEMUA perangkat saat UI sudah tidak terlihat —
     * bukan hanya `TRIM_MEMORY_BACKGROUND` (40).
     */
    const val TRIM_UI_HIDDEN = 20

    /**
     * Lepas renderer/bitmap PDF saat sistem mengirim trim level UI-hidden
     * atau lebih berat. Level `RUNNING_LOW`/`RUNNING_CRITICAL` sengaja TIDAK
     * memicu pelepasan: viewer masih terlihat dan tidak ada event resume
     * untuk membukanya kembali, sehingga layar soal akan kosong.
     */
    fun shouldReleaseForTrim(level: Int): Boolean = level >= TRIM_UI_HIDDEN
}
