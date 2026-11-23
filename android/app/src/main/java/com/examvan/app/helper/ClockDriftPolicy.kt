package com.examvan.app.helper

/**
 * Resolusi baseline clock-drift lintas process death (fix temuan review:
 * "baseline drift hanya ada di memory/savedInstanceState; app restart =
 * siswa bisa manipulasi waktu lagi").
 *
 * Baseline = System.currentTimeMillis() - SystemClock.elapsedRealtime() yang
 * dicatat saat sesi ujian dimulai. Nilai ini disimpan ke prefs terenkripsi
 * sehingga bertahan ketika proses mati; savedInstanceState tetap jadi sumber
 * pertama karena paling segar di proses yang sama.
 */
object ClockDriftPolicy {

    /**
     * Hasil resolusi baseline.
     * @param baselineDriftMs nilai baseline yang dipakai SecurityEnforcer.
     * @param isNew true bila baseline baru dihitung (sesi pertama) — caller
     *        WAJIB menyimpannya ke prefs untuk melindungi sesi berikutnya.
     */
    data class ResolvedBaseline(val baselineDriftMs: Long, val isNew: Boolean)

    /**
     * Pilih sumber baseline dengan prioritas:
     * 1. [savedStateDriftMs] — rotasi/re-create di proses yang sama.
     * 2. [persistedDriftMs]  — proses mati lalu dibuka lagi.
     * 3. [freshDriftMs]      — sesi baru; ditandai isNew agar dipersist.
     *
     * Nilai 0 diperlakukan sebagai sentinel "tidak ada" (default
     * Bundle.getLong / prefs kosong) — bukan baseline sah, karena wall-clock
     * hampir tidak mungkin persis sama dengan elapsedRealtime.
     *
     * [persistedBootCount] / [currentBootCount] adalah Settings.Global.BOOT_COUNT
     * (nilai tidak valid <= 0 diabaikan). Baseline lintas proses hanya sah pada
     * boot yang sama: setelah reboot elapsedRealtime kembali ke 0 sehingga
     * baseline lama selalu tampak sebagai lompatan jam raksasa. Bila boot
     * berubah — atau baseline lama tidak punya penanda boot — hitung baseline
     * baru agar siswa tidak dituduh memanipulasi waktu.
     */
    fun resolveBaseline(
        savedStateDriftMs: Long?,
        persistedDriftMs: Long?,
        freshDriftMs: Long,
        persistedBootCount: Int? = null,
        currentBootCount: Int? = null
    ): ResolvedBaseline {
        savedStateDriftMs?.takeIf { it != 0L }?.let { return ResolvedBaseline(it, isNew = false) }

        val persistedBoot = persistedBootCount?.takeIf { it > 0 }
        val currentBoot = currentBootCount?.takeIf { it > 0 }
        val persistedIsUsable = when {
            persistedDriftMs == null || persistedDriftMs == 0L -> false
            currentBoot == null -> true // tidak bisa diverifikasi; pakai best-effort
            persistedBoot == null -> false // data versi lama, jangan risiko false positive
            else -> persistedBoot == currentBoot
        }
        if (persistedIsUsable) {
            return ResolvedBaseline(persistedDriftMs ?: freshDriftMs, isNew = false)
        }
        return ResolvedBaseline(freshDriftMs, isNew = true)
    }
}
