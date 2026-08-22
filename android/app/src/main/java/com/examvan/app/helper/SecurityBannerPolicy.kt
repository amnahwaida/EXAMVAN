package com.examvan.app.helper

import com.examvan.app.R

/**
 * Kebijakan warna banner keamanan di toolbar ujian (fix review UI/UX ronde
 * 2 #1: "medium mode memakai banner MERAH sepanjang ujian — alarm fatigue
 * membuat pelanggaran nyata kehilangan pembeda visual").
 *
 * Semantik:
 *  - STRICT            → merah kritis + teks putih (terkunci/pelanggaran).
 *  - MEDIUM & level
 *    aktif tak dikenal → AMBER + teks gelap (pemantauan auto-submit; berbeda
 *                        jelas dari strict, konsisten fail-closed ExamModePolicy).
 *  - LOW               → slate netrel (bebas keluar-masuk).
 */
object SecurityBannerPolicy {

    fun backgroundRes(strictMode: Boolean, securityLevel: String): Int = when {
        strictMode -> R.color.security_banner_critical
        securityLevel == "low" -> R.color.security_banner_info
        else -> R.color.warning // medium & level aktif tak dikenal: amber
    }

    fun textRes(strictMode: Boolean, securityLevel: String): Int = when {
        strictMode -> R.color.security_banner_text
        securityLevel == "low" -> R.color.security_banner_text
        else -> R.color.warning_text // teks gelap di atas amber
    }
}
