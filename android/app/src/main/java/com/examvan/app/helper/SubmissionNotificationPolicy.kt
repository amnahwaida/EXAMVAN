package com.examvan.app.helper

/**
 * Kebijakan notifikasi pengiriman jawaban (manual vs auto-submit).
 * Fungsi murni agar dapat diuji di JVM test.
 */
object SubmissionNotificationPolicy {

    enum class SubmissionType {
        MANUAL,
        AUTO
    }

    data class NotificationContent(
        val title: String,
        val message: String,
        val isSuccess: Boolean,
        val allowRetryAction: Boolean = false
    )

    /**
     * Menentukan apakah notifikasi status bar wajib dipicu.
     * Semua submission sukses (manual maupun auto) WAJIB memicu notifikasi.
     * Auto submit gagal juga memicu notifikasi error + retry.
     */
    fun shouldNotify(type: SubmissionType, isSuccess: Boolean): Boolean {
        return isSuccess || type == SubmissionType.AUTO
    }

    /**
     * Membentuk payload teks notifikasi berdasarkan jenis submit dan hasil pengiriman.
     */
    fun getNotificationContent(
        type: SubmissionType,
        isSuccess: Boolean,
        examName: String = "",
        serverMessage: String? = null
    ): NotificationContent {
        return if (isSuccess) {
            val title = if (type == SubmissionType.MANUAL) {
                "Jawaban Berhasil Dikumpulkan"
            } else {
                "Ujian Telah Terkumpul Otomatis"
            }
            val msg = if (!serverMessage.isNullOrBlank()) {
                serverMessage
            } else if (examName.isNotBlank()) {
                "Jawaban untuk $examName berhasil dikirim ke server."
            } else {
                "Jawaban ujian berhasil dikirim ke server."
            }
            NotificationContent(
                title = title,
                message = msg,
                isSuccess = true,
                allowRetryAction = false
            )
        } else {
            val title = if (type == SubmissionType.MANUAL) {
                "Pengumpulan Jawaban Gagal"
            } else {
                "Gagal Mengumpulkan Jawaban Otomatis"
            }
            val msg = if (!serverMessage.isNullOrBlank()) {
                "Terjadi kesalahan: $serverMessage"
            } else {
                "Koneksi terputus saat mengumpulkan jawaban."
            }
            NotificationContent(
                title = title,
                message = msg,
                isSuccess = false,
                allowRetryAction = true
            )
        }
    }
}
