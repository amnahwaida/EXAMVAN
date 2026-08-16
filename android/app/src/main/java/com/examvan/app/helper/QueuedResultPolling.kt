package com.examvan.app.helper

import com.examvan.app.api.ApiClient
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.delay
import kotlinx.coroutines.withTimeoutOrNull

/**
 * Polling GET /api/exams/{exam_id}/result untuk jalur submit async (202).
 *
 * Di-extract dari SubmissionManager supaya logika deadline bisa diuji di JVM
 * (tanpa Android Context/ViewBinding). Deadline bersifat TEGAS: setiap iterasi
 * memakai `withTimeoutOrNull` dengan batas `min(HTTP_TIMEOUT, sisa deadline)`,
 * sehingga satu response yang menggantung tidak pernah menahan loop lebih lama
 * dari yang seharusnya — sebelumnya `CompletableDeferred.await()` tanpa timeout
 * bisa memblokir hingga read-timeout OkHttp (30 dtk) per iterasi dan membuat
 * layar "Mengirim…" bertahan ±15 menit pada jaringan tersendat.
 */
internal object QueuedResultPolling {

    /** Jeda antar poll. */
    const val POLL_INTERVAL_MS = 2_500L

    /** Batas maksimum iterasi poll (jaring pengaman di atas deadline). */
    const val POLL_MAX_ATTEMPTS = 30

    /** Total anggaran waktu polling sampai dianggap gagal (belum durable). */
    const val POLL_DEADLINE_MS = 75_000L

    /** Batas waktu tunggu satu respons HTTP /result (mencegah hang tak terbatas). */
    const val HTTP_TIMEOUT_MS = 10_000L

    /**
     * Poll /result sampai:
     *  - status "done"  → kembalikan `true` (jawaban durable di server);
     *  - status "failed" → kembalikan `false`;
     *  - deadline tercapai / percobaan habis → kembalikan `false`.
     *
     * @param deadlineMs total anggaran waktu (ms) untuk seluruh polling.
     * @param maxAttempts jaring pengaman jumlah iterasi.
     */
    suspend fun awaitDurable(
        examId: Int,
        token: String,
        macAddress: String,
        identityData: String?,
        jobId: String?,
        deadlineMs: Long,
        maxAttempts: Int = POLL_MAX_ATTEMPTS
    ): Boolean {
        val deadline = System.currentTimeMillis() + deadlineMs
        var attempts = 0
        while (attempts < maxAttempts) {
            val remaining = deadline - System.currentTimeMillis()
            if (remaining <= 0) break

            attempts++
            // Batas per-poll = sisa waktu sampai deadline (paling lambat HTTP_TIMEOUT).
            // Ini yang membuat deadline TEGAS: response yang menggantung tidak bisa
            // menahan loop melewati deadline + satu polling-timeout kecil.
            val status = withTimeoutOrNull(minOf(HTTP_TIMEOUT_MS, remaining)) {
                val pollDone = CompletableDeferred<String?>()
                ApiClient.getExamResult(
                    examId = examId,
                    token = token,
                    macAddress = macAddress,
                    identityData = identityData,
                    jobId = jobId,
                    onSuccess = { poll -> pollDone.complete(poll.status) },
                    onError = { pollDone.complete(null) }
                )
                pollDone.await()
            } ?: null

            when (status) {
                ApiClient.RESULT_STATUS_DONE -> return true
                ApiClient.RESULT_STATUS_FAILED -> return false
            }
            delay(POLL_INTERVAL_MS)
        }
        return false
    }
}
