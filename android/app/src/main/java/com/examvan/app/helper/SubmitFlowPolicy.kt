package com.examvan.app.helper

/**
 * Gate tunggal untuk memulai pengiriman jawaban — murni, bisa diuji JVM.
 *
 * Ada dua jalur submit yang bisa saling tumpang tindih:
 *  - MANUAL (tombol Kumpulkan): guard `isSubmitting`;
 *  - AUTO (deadline habis / keluar app / fokus hilang): guard `submittedOrExited`.
 *
 * Keduanya memakai flag yang BERBEDA, sehingga pada saat deadline berbarengan
 * dengan submit manual, dua POST /submit bisa terkirim. Kedua jalur kini
 * berkonsultasi ke satu keputusan yang sama: boleh mulai hanya bila TIDAK ada
 * submit yang sedang berjalan DAN belum ada submit/exit yang selesai.
 */
internal object SubmitFlowPolicy {

    /**
     * True bila jalur submit (manual maupun auto) boleh dimulai.
     *
     * @param isSubmitting     ada submit manual yang sedang berjalan.
     * @param submittedOrExited sudah ada submit sukses / exit yang diselesaikan.
     */
    fun canStartSubmission(isSubmitting: Boolean, submittedOrExited: Boolean): Boolean =
        !isSubmitting && !submittedOrExited
}
