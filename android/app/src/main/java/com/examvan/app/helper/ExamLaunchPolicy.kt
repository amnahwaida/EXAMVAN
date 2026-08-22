package com.examvan.app.helper

/**
 * Validasi intent launch ExamViewerActivity (fix temuan review: "cek
 * examId == -1 dijalankan SETELAH WebSocket connect, access log, dan watchdog
 * deadline sudah jalan dengan data yang tidak valid").
 *
 * Kontrak baru: onCreate memvalidasi PERTAMA kali — sebelum efek samping
 * apapun (WebSocket connect, HTTP access-log, penjadwalan deadline, download
 * PDF). Policy ini murni fungsi tanpa dependensi Android sehingga bisa dites
 * di JVM (lihat ExamLaunchPolicyTest).
 */
object ExamLaunchPolicy {

    enum class Field { EXAM_ID, SERVER_URL, TOKEN }

    /** Hasil validasi: valid, atau field pertama yang tidak sah. */
    sealed class ValidationResult

    // Dideklarasikan di level objek agar pemanggil/test bisa menulis
    // ExamLaunchPolicy.Valid dan ExamLaunchPolicy.Invalid sebagai tipe.
    object Valid : ValidationResult()
    data class Invalid(val field: Field) : ValidationResult() {
        // Alias agar test bisa menulis ExamLaunchPolicy.Invalid.EXAM_ID.
        companion object {
            val EXAM_ID = Field.EXAM_ID
            val SERVER_URL = Field.SERVER_URL
            val TOKEN = Field.TOKEN
        }
    }

    /**
     * Validasi field inti intent ujian.
     * Prioritas pelaporan mengikuti tingkat kefundamental-an field:
     * examId (kunci prefs/PDF/submit) → serverUrl (semua HTTP) → token
     * (kredensial sesi).
     */
    fun validate(examId: Int, serverUrl: String, examToken: String): ValidationResult {
        if (examId <= 0) return Invalid(Field.EXAM_ID)
        if (serverUrl.isBlank()) return Invalid(Field.SERVER_URL)
        if (examToken.isBlank()) return Invalid(Field.TOKEN)
        return Valid
    }
}
