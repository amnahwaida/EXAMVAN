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
 */
internal object ExamModePolicy {

    const val LEVEL_LOW = "low"
    const val LEVEL_MEDIUM = "medium"

    /**
     * True bila kehilangan fokus (>500 ms) harus memicu auto-submit.
     * Hanya MEDIUM (non-strict): LOW bebas, STRICT memakai lock task pin
     * (auto-submit justru melepas pin dan membebaskan siswa).
     */
    fun shouldAutoSubmitOnFocusLoss(securityLevel: String, strictMode: Boolean): Boolean =
        !strictMode && securityLevel != LEVEL_LOW

    /**
     * True bila tombol volume di-intercept (ditekan) oleh aplikasi.
     * LOW: biarkan sistem menangani (panel volume normal). MEDIUM/STRICT:
     * ditekan agar panel volume tidak dipakai sebagai jalur keluar/bypass.
     */
    fun shouldInterceptVolumeKeys(securityLevel: String): Boolean =
        securityLevel != LEVEL_LOW
}
