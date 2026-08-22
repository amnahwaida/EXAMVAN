package com.examvan.app

import android.annotation.SuppressLint
import android.os.Bundle
import android.view.View
import android.webkit.WebView
import android.webkit.WebViewClient
import com.examvan.app.databinding.ActivityResultsViewerBinding

/**
 * Halaman Hasil Ujian IN-APP (WebView).
 *
 * Fix temuan review: "token hasil ujian di URL browser tersimpan di history
 * browser". Sebelumnya hasil dibuka via Intent.ACTION_VIEW — URL berisi token
 * live session ikut tersimpan permanen di history browser eksternal yang tidak
 * terproteksi FLAG_SECURE (bisa di-screenshot, dibaca app lain).
 *
 * Dengan WebView in-app:
 *  - Token tidak pernah keluar dari sandbox app.
 *  - FLAG_SECURE aktif (diwarisi BaseSecureActivity) → screenshot & thumbnail
 *    recents diblokir.
 *  - Di kiosk flavor siswa tetap berada dalam kendali lock task.
 *
 * URL dibentuk oleh ResultsLinkPolicy.build() oleh pemanggil.
 */
class ResultsViewerActivity : BaseSecureActivity() {

    private lateinit var binding: ActivityResultsViewerBinding

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        binding = ActivityResultsViewerBinding.inflate(layoutInflater)
        setContentView(binding.root)
        applyEdgeToEdgeInsets(binding.root)

        val url = intent.getStringExtra(EXTRA_URL) ?: ""
        if (url.isEmpty()) {
            finish()
            return
        }

        binding.btnClose.setOnClickListener { finish() }
        setupWebView()
        binding.webView.loadUrl(url)
    }

    @SuppressLint("SetJavaScriptEnabled")
    private fun setupWebView() {
        binding.webView.webViewClient = object : WebViewClient() {
            override fun onPageFinished(view: WebView?, url: String?) {
                binding.progressLoading.visibility = View.GONE
            }
        }
        // Halaman hasil adalah SPA ringan milik server sendiri — JS wajib.
        binding.webView.settings.javaScriptEnabled = true
        binding.webView.settings.domStorageEnabled = true
    }

    /** Back fisik/gesture menutup halaman hasil, bukan keluar dari app. */
    @Deprecated("Deprecated in Java")
    override fun onBackPressed() {
        if (binding.webView.canGoBack()) {
            binding.webView.goBack()
        } else {
            super.onBackPressed()
        }
    }

    override fun onDestroy() {
        // Hentikan rendering & bebaskan memori WebView.
        binding.webView.apply {
            stopLoading()
            destroy()
        }
        super.onDestroy()
    }

    companion object {
        const val EXTRA_URL = "extra_result_url"
    }
}
