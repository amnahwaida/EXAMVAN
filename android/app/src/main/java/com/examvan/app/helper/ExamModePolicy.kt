package com.examvan.app.helper

/**
 * Kebijakan mode ujian (low / medium / strict) sebagai fungsi murni.
 *
 * Di-extract dari ExamViewerActivity supaya keputusan keamanan bisa diuji di
 * JVM tanpa emulator. Perbaikan review mode (16 Agustus 2026):
 *  - LOW TIDAK auto-submit saat fokus hilang (sebelumnya ikut ter-submit
 *    karena hanya cek `!strictMode`) — siswa low bebas keluar-masuk;
 *  - tombol volume berfungsi normal di LOW (panel sistem muncul), ditekan
 *    hanya di MEDIUM/STRICT untuk mencegah bypass lewat panel volume.
 *
 * KONTRAK LINTAS-MODE yang disengaja (review low-mode #2 & #3 — bukan bug):
 *  1. DEADLINE & REMOTE KILL berlaku di SEMUA mode: watchdog end_time dan
 *     event WS exam_terminated tetap men-submit jawaban siswa low. Kontrak
 *     "low tanpa auto-submit" hanya mencakup pemicu keluar-app, bukan
 *     penutupan ujian oleh waktu/server.
 *  2. PEMERIKSAAN LINGKUNGAN & PROTEKSI KONTEN berlaku di SEMUA mode:
 *     deteksi root/emulator/USB-debug/cast/manipulasi jam, FLAG_SECURE,
 *     anti-tapjack, dan peringatan accessibility aktif juga di low — ini
 *     integritas perangkat + perlindungan soal, independen dari kebijakan
 *     keluar-app.
 */
internal object ExamModePolicy {

    const val LEVEL_LOW = "low"
    const val LEVEL_MEDIUM = "medium"
    const val DEFAULT_LEVEL = LEVEL_MEDIUM

    /**
     * Normalisasi security level dari server (fix temuan review low-mode #1):
     * trim + lowercase, fallback [DEFAULT_LEVEL] bila kosong/null.
     *
     * Dulu nilai mentah dibandingkan langsung ("LOW" != "low") sehingga
     * kapitalisasi dari server melumpuhkan kontrak low — siswa ikut
     * ter-auto-submit saat fokus hilang dan volume key-nya di-intercept.
     * Panggil SATU kali di titik intake (ServerConfigActivity /
     * WaitingApprovalActivity); semua keputusan mode di hilir memakai hasil
     * normalisasi ini.
     */
    fun normalize(securityLevel: String?): String {
        val cleaned = securityLevel?.trim()?.lowercase()
        return if (cleaned.isNullOrBlank()) DEFAULT_LEVEL else cleaned
    }

    /**
     * True bila runnable watch focus-loss perlu dijadwalkan (fix temuan
     * review low-mode #4): strict memakainya untuk re-pin saat fokus hilang;
     * medium memakainya untuk auto-submit. Low tidak punya keduanya →
     * penjadwalan adalah kerja sia-sia dan di-skip.
     */
    fun shouldScheduleFocusLossWatch(strictMode: Boolean, securityLevel: String): Boolean =
        strictMode || shouldAutoSubmitOnFocusLoss(securityLevel, strictMode)

    /**
     * True bila kehilangan fokus (>500 ms) harus memicu auto-submit.
     * Hanya MEDIUM (non-strict): LOW bebas, STRICT memakai lock task pin
     * (auto-submit justru melepas pin dan membebaskan siswa).
     */
    fun shouldAutoSubmitOnFocusLoss(securityLevel: String, strictMode: Boolean): Boolean =
        !strictMode && securityLevel != LEVEL_LOW

    /**
     * True bila event Home/Recents (onUserLeaveHint) harus memicu auto-submit.
     *
     * Fix temuan review mode medium #2: sebelumnya onUserLeaveHint memakai
     * `securityLevel == "medium"` hardcoded sementara jalur onStop/fokus
     * memakai `!= LEVEL_LOW` — level custom dari server (mis. "high")
     * berperilaku BERBEDA di dua jalur (Home tidak submit, overlay submit).
     * Kini kedua jalur berkonsultasi ke keputusan mode yang sama; perbedaan
     * hanya pada guard tambahan di titik panggilan (dialog app, popup,
     * grace startup) — bukan pada keputusan mode.
     */
    fun shouldAutoSubmitOnUserLeave(securityLevel: String, strictMode: Boolean): Boolean =
        shouldAutoSubmitOnFocusLoss(securityLevel, strictMode)

    /**
     * Grace period pasca-PDF-ready (fix temuan review mode medium #4):
     * Home tidak sengaja beberapa detik setelah ujian terbuka tidak lagi
     * langsung men-submit jawaban yang mungkin masih kosong — siswa punya
     * waktu [STARTUP_GRACE_MS] untuk "masuk" dulu sebelum aturan keluar
     * berlaku. Deadline watchdog TIDAK terpengaruh (tetap men-submit tepat
     * di deadline apa pun yang terjadi).
     *
     * CATATAN (review ronde 3 #3): ini grace window YANG BERBEDA dari grace
     * 3 detik onCreateTime di SecurityEnforcer.handleUserLeave (yang hanya
     * mengatur re-pin strict). Sumber waktu: pdfReadyAtMs (soal siap), bukan
     * waktu activity dibuat.
     */
    const val STARTUP_GRACE_MS = 3_000L

    /**
     * True bila saat [nowMs] kita masih dalam grace period setelah PDF siap
     * di [pdfReadyAtMs]. Ambang eksklusif: tepat di batas → grace selesai.
     * [pdfReadyAtMs] null (PDF belum siap) → tidak dalam grace (jalur leave
     * memang sudah diguard isPdfReady tersendiri).
     */
    fun isWithinStartupGrace(pdfReadyAtMs: Long?, nowMs: Long, graceMs: Long = STARTUP_GRACE_MS): Boolean {
        val readyAt = pdfReadyAtMs ?: return false
        return nowMs - readyAt < graceMs
    }

    /**
     * Grace window tombol volume: panel volume sistem butuh waktu turun
     * sebelum event fokus dianggap mencurigakan.
     */
    const val VOLUME_KEY_FOCUS_GRACE_MS = 1_500L

    /** Delay dasar evaluasi focus-loss (dialog overlay harus stabil dulu). */
    const val FOCUS_LOST_BASE_DELAY_MS = 500L

    /**
     * Hitung delay evaluasi focus-loss (fix temuan review mode medium
     * gelombang kedua #2: DULU event fokus hilang dalam 1.5s pasca tekanan
     * volume DIBUANG sepenuhnya — overlay yang muncul dalam window itu lolos
     * dari auto-submit sampai event berikutnya).
     *
     * Kini event TIDAK pernah dibuang: sisa grace volume hanya MENUNDA
     * evaluasi. Delay = baseDelay + sisa grace volume (bila masih dalam
     * grace), atau baseDelay saja.
     */
    fun focusLossDelayMs(
        volumeKeyPressedAtMs: Long?,
        nowMs: Long,
        baseDelayMs: Long = FOCUS_LOST_BASE_DELAY_MS,
        volumeGraceMs: Long = VOLUME_KEY_FOCUS_GRACE_MS
    ): Long {
        val pressedAt = volumeKeyPressedAtMs ?: return baseDelayMs
        val elapsed = nowMs - pressedAt
        if (elapsed >= volumeGraceMs) return baseDelayMs
        return baseDelayMs + (volumeGraceMs - elapsed)
    }

    /**
     * True bila fokus-KEMBALI terjadi dalam grace tombol volume (fix temuan
     * review ronde 3 #2). Kontrak: pembersihan (cancel pending runnable,
     * reset popup count) tetap berjalan; hanya aksi keamanan lanjutan
     * (re-pin strict, audit FOCUS_RESTORED) yang dilewati — dulu seluruh
     * branch early-return sehingga pembersihan pun terlewat.
     */
    fun isWithinVolumeGrace(
        volumeKeyPressedAtMs: Long?,
        nowMs: Long,
        graceMs: Long = VOLUME_KEY_FOCUS_GRACE_MS
    ): Boolean {
        val pressedAt = volumeKeyPressedAtMs ?: return false
        return nowMs - pressedAt < graceMs
    }

    /**
     * True bila tombol volume di-intercept (ditekan) oleh aplikasi.
     *
     * LOW non-strict: biarkan sistem menangani (panel volume normal).
     * MEDIUM/STRICT: ditekan agar panel volume tidak dipakai sebagai jalur
     * keluar/bypass.
     *
     * Fix temuan review low-mode ronde 3 #1: kini STRICT-AWARE — kombinasi
     * security_level="low" + strict_mode=true membuat exam ter-pin via lock
     * task, dan panel volume tidak boleh jadi jalur keluar. Dulu kebijakan
     * hanya membaca level sehingga kombinasi itu bocor.
     */
    fun shouldInterceptVolumeKeys(securityLevel: String, strictMode: Boolean): Boolean =
        strictMode || securityLevel != LEVEL_LOW

    /**
     * Keputusan untuk LONG-PRESS volume (fix temuan review low-mode ronde 3
     * #2): dulu onKeyLongPress selalu mengonsumsi event tanpa melihat
     * kebijakan — di low single-press normal tapi long-press mati diam-diam.
     * Kontrak baru: identik dengan [shouldInterceptVolumeKeys], sehingga
     * pengalaman short/long press konsisten per mode.
     */
    fun shouldInterceptVolumeLongPress(securityLevel: String, strictMode: Boolean): Boolean =
        shouldInterceptVolumeKeys(securityLevel, strictMode)
}
