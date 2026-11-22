package com.examvan.app.helper

/**
 * Kebijakan auto-resume sesi ujian setelah proses/task aplikasi dibunuh OEM
 * (MIUI, dll) saat siswa keluar sebentar di mode strict/gesture.
 *
 * Tujuan: kembali ke aplikasi langsung membuka soal yang sedang dikerjakan,
 * tanpa meminta token lagi. Hanya sesi yang BENAR-BENAR aktif yang di-resume:
 *  - sudah mulai (exam_start_time tersimpan),
 *  - punya server + token sesi + identitas,
 *  - belum submit, ATAU sudah ditandai submit tetapi jawaban masih tertunda
 *    (jalur recovery — jangan biarkan siswa mengetik token hanya untuk
 *    melihat layar "kirim ulang").
 *
 * Fungsi murni supaya bisa diuji di JVM tanpa Activity.
 */
object ExamResumePolicy {

    data class Snapshot(
        val examId: Int,
        val serverUrl: String,
        val token: String,
        val startTime: String?,
        val submittedOrExited: Boolean,
        val hasPendingAnswers: Boolean,
        val identityData: String
    )

    fun shouldResume(snapshot: Snapshot): Boolean {
        if (snapshot.examId <= 0) return false
        if (snapshot.serverUrl.isBlank()) return false
        if (snapshot.token.isBlank()) return false
        if (snapshot.startTime.isNullOrBlank()) return false
        if (!hasUsableIdentity(snapshot.identityData)) return false
        if (snapshot.submittedOrExited) return snapshot.hasPendingAnswers
        return true
    }

    private fun hasUsableIdentity(raw: String): Boolean {
        val trimmed = raw.trim()
        return trimmed.isNotEmpty() && trimmed != "{}" && trimmed != "null"
    }
}
