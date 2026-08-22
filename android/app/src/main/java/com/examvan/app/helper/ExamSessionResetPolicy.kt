package com.examvan.app.helper

/**
 * Kebijakan reset sesi ujian saat siswa bergabung via token (fix temuan
 * review mode medium ronde 5 #1 & #2).
 *
 * #1: ujian sebelumnya yang GAGAL submit sengaja mempertahankan
 * KEY_EXAM_START_TIME untuk layar recovery — tanpa reset, ujian BERIKUTNYA
 * mewarisi start_time milik ujian lama. Reset hanya boleh terjadi saat
 * bergabung ke ujian yang berbeda; rejoin ke ujian yang sama (proses mati)
 * tidak boleh menghapus apa pun.
 *
 * #2: flag submitted_or_exit_<examId> menumpuk selamanya — dipangkas
 * selektif (flag ujian lain dihapus, flag ujian aktif dan key non-flag
 * tersentuh).
 */
object ExamSessionResetPolicy {

    private const val SUBMITTED_FLAG_PREFIX = "submitted_or_exit_"

    /**
     * True bila bergabung ke [newExamId] harus me-reset data sesi lama.
     * [storedExamId] = KEY_EXAM_ID dari prefs (-1 bila tidak ada — mis.
     * remember unchecked / install baru) → selalu reset.
     */
    fun shouldResetSession(storedExamId: Int, newExamId: Int): Boolean =
        storedExamId != newExamId

    /** True bila [key] adalah kunci flag submitted per-uji. */
    fun isSubmittedOrExitedKey(key: String): Boolean {
        if (!key.startsWith(SUBMITTED_FLAG_PREFIX)) return false
        val suffix = key.removePrefix(SUBMITTED_FLAG_PREFIX)
        return suffix.isNotEmpty() && suffix.all { it.isDigit() }
    }

    /** Ekstrak examId dari kunci flag; null bila bukan kunci flag / rusak. */
    fun submittedExamIdFromKey(key: String): Int? {
        if (!isSubmittedOrExitedKey(key)) return null
        return key.removePrefix(SUBMITTED_FLAG_PREFIX).toIntOrNull()
    }

    /**
     * Pilih kunci flag yang BOLEH dihapus dari [keys]: hanya flag milik
     * ujian lain. Flag [keepExamId] dan semua key non-flag dipertahankan;
     * key ber-prefix flag tanpa id numerik ("submitted_or_exit_x") dianggap
     * sampah peninggalan dan ikut dipangkas.
     */
    fun prunableSubmittedFlagKeys(keys: Collection<String>, keepExamId: Int): List<String> {
        return keys.filter { key ->
            if (!key.startsWith(SUBMITTED_FLAG_PREFIX)) return@filter false
            val id = submittedExamIdFromKey(key)
            id == null || id != keepExamId
        }
    }
}
