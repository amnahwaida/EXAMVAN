package com.examvan.app

import android.annotation.SuppressLint
import android.content.Intent
import android.graphics.Bitmap
import android.net.Uri
import android.os.Bundle
import android.view.View
import android.webkit.WebResourceError
import android.webkit.WebResourceRequest
import android.webkit.WebResourceResponse
import android.webkit.WebView
import android.webkit.WebViewClient
import androidx.core.content.ContextCompat
import com.examvan.app.databinding.ActivityResultsViewerBinding
import com.examvan.app.helper.ResultsWebPolicy

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
 * Hardening (fix review pasca-submit #1):
 *  - allowFileAccess/allowContentAccess dinonaktifkan eksplisit.
 *  - onReceivedError/onReceivedHttpError menyembunyikan spinner + menampilkan
 *    pesan error (dulu spinner abadi saat jaringan putus).
 *  - Navigasi keluar host server dibuka di browser eksternal
 *    (ResultsWebPolicy.isSameHost) — WebView bukan jendela bebas.
 */
class ResultsViewerActivity : BaseSecureActivity() {

    private lateinit var binding: ActivityResultsViewerBinding

    /** URL awal hasil — acuan keputusan navigasi (host sama vs luar). */
    private var initialUrl: String = ""

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        binding = ActivityResultsViewerBinding.inflate(layoutInflater)
        setContentView(binding.root)
        applyEdgeToEdgeInsets(binding.root)

        initialUrl = intent.getStringExtra(EXTRA_URL) ?: ""
        if (initialUrl.isEmpty()) {
            finish()
            return
        }

        binding.btnClose.setOnClickListener { finish() }
        setupWebView()
        binding.webView.loadUrl(initialUrl)
    }

    @SuppressLint("SetJavaScriptEnabled")
    private fun setupWebView() {
        binding.webView.webViewClient = object : WebViewClient() {
            override fun shouldOverrideUrlLoading(
                view: WebView?,
                request: WebResourceRequest?
            ): Boolean {
                val target = request?.url?.toString() ?: return false
                // Host sama → muat di dalam; host lain → browser eksternal.
                return if (!ResultsWebPolicy.isSameHost(initialUrl, target)) {
                    openExternally(target)
                    true
                } else {
                    false
                }
            }

            override fun onPageFinished(view: WebView?, url: String?) {
                binding.progressLoading.visibility = View.GONE
            }

            override fun onReceivedError(
                view: WebView?,
                request: WebResourceRequest?,
                error: WebResourceError?
            ) {
                if (request?.isForMainFrame == true) showLoadError()
            }

            override fun onReceivedHttpError(
                view: WebView?,
                request: WebResourceRequest?,
                errorResponse: WebResourceResponse?
            ) {
                if (request?.isForMainFrame == true) showLoadError()
            }
        }

        // Halaman hasil adalah SPA ringan milik server sendiri — JS wajib.
        binding.webView.settings.javaScriptEnabled = true
        binding.webView.settings.domStorageEnabled = true
        // Hardening (fix review pasca-submit #1): WebView ini hanya boleh
        // memuat URL server — akses file/content tidak diperlukan.
        binding.webView.settings.allowFileAccess = false
        binding.webView.settings.allowContentAccess = false
    }

    private fun showLoadError() {
        binding.progressLoading.visibility = View.GONE
        binding.tvError.visibility = View.VISIBLE
        binding.tvError.text = getString(R.string.results_load_error)
    }

    private fun openExternally(url: String) {
        try {
            startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(url)))
        } catch (_: Exception) {
            android.widget.Toast.makeText(
                this, R.string.congrats_link_missing, android.widget.Toast.LENGTH_SHORT
            ).show()
        }
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
