package com.examvan.app.helper

/**
 * Pusat ID holder dialog untuk AppDialogFlagRegistry (fix temuan review
 * mode medium ronde 4 #1: sebelumnya ID tersebar sebagai string literal /
 * konstanta privat di beberapa file, dan 3 dialog di SubmissionManager
 * bahkan tidak memegang flag sama sekali).
 *
 * ATURAN: setiap dialog yang mencuri fokus window WAJIB memegang flag lewat
 * SecurityEnforcer.holdAppDialog([id]) saat tampil dan releaseAppDialog(id)
 * saat tertutup — termasuk semua jalur exit-nya (positive/negative/cancel).
 * ID harus unik (dikunci AppDialogIdsTest): dua dialog aktif bersamaan tidak
 * boleh saling melepaskan proteksi.
 */
object AppDialogIds {
    /** Dialog konfirmasi keluar ujian (confirmAndLogout, strict & non-strict). */
    const val LOGOUT_CONFIRM = "logout_confirm"

    /** Prompt izin POST_NOTIFICATIONS — rationale in-app & dialog sistem. */
    const val PERMISSION_PROMPT = "permission_prompt"

    /** Dialog konfirmasi "Kumpulkan jawaban?" (SubmissionManager). */
    const val SUBMIT_CONFIRM = "submit_confirm"

    /** Dialog kegagalan submit manual (SubmissionManager). */
    const val SUBMIT_FAILED = "submit_failed"

    /** Dialog ucapan selamat fallback bila launch activity gagal (SubmissionManager). */
    const val SUBMIT_CONGRATS = "submit_congrats"
}
