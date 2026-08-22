package com.examvan.app.helper

/**
 * Pembentukan link halaman hasil ujian — satu-satunya tempat format URL
 * hasil didefinisikan (fix temuan review: "token hasil ujian di URL browser
 * tersimpan di history browser").
 *
 * Setelah fix, token TIDAK PERNAH keluar dari app: ExamViewerActivity dan
 * CongratulationsActivity membuka ResultsViewerActivity (WebView in-app,
 * FLAG_SECURE aktif dari BaseSecureActivity) alih-alih browser eksternal.
 */
object ResultsLinkPolicy {

    /**
     * Bangun short-link hasil: {base}/{token} → server redirect ke
     * /hasil/<token>. Token selalu berupa path segment (bukan query param)
     * agar tidak ikut terkirim via header Referrer ke pihak ketiga.
     *
     * @return URL siap pakai, atau string kosong bila base/token kosong.
     */
    fun build(serverUrl: String, examToken: String): String {
        val base = serverUrl.trim().trimEnd('/')
        val token = examToken.trim()
        if (base.isEmpty() || token.isEmpty()) return ""
        return "$base/$token"
    }
}
