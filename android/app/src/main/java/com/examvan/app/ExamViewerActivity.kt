package com.examvan.app

import android.content.ClipboardManager
import android.content.Context
import android.graphics.Bitmap
import android.graphics.pdf.PdfRenderer
import android.os.Bundle
import android.os.ParcelFileDescriptor
import android.view.View
import android.view.WindowManager
import androidx.appcompat.app.AppCompatActivity
import com.examvan.app.api.ApiClient
import com.examvan.app.databinding.ActivityExamViewerBinding
import okhttp3.Call
import java.io.File

/**
 * Screen 3: Exam PDF Viewer
 * - Downloads PDF with progress indicator
 * - Renders pages via PdfRenderer to Bitmap (no text layer = anti-copy)
 * - Prev/Next navigation with page counter
 * - FLAG_SECURE active to prevent screenshots
 */
class ExamViewerActivity : AppCompatActivity() {

    private lateinit var binding: ActivityExamViewerBinding

    private var pdfRenderer: PdfRenderer? = null
    private var fileDescriptor: ParcelFileDescriptor? = null
    private var currentPage = 0
    private var totalPages = 0
    private var downloadCall: Call? = null

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        // FLAG_SECURE: prevent screenshots & screen recording
        window.setFlags(
            WindowManager.LayoutParams.FLAG_SECURE,
            WindowManager.LayoutParams.FLAG_SECURE
        )

        // Clear clipboard
        val clipboard = getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager
        try { clipboard.clearPrimaryClip() } catch (_: Exception) { }

        binding = ActivityExamViewerBinding.inflate(layoutInflater)
        setContentView(binding.root)

        val examId = intent.getIntExtra("exam_id", -1)
        val examName = intent.getStringExtra("exam_name") ?: "Ujian"

        binding.tvExamTitle.text = examName

        if (examId == -1) {
            showError("ID ujian tidak valid")
            return
        }

        // Back button
        binding.btnBack.setOnClickListener { finish() }

        // Navigation buttons
        binding.btnPrev.setOnClickListener {
            if (currentPage > 0) {
                currentPage--
                renderPage(currentPage)
            }
        }

        binding.btnNext.setOnClickListener {
            if (currentPage < totalPages - 1) {
                currentPage++
                renderPage(currentPage)
            }
        }

        // Retry button
        binding.btnRetryDownload.setOnClickListener {
            downloadPdf(examId)
        }

        // Cancel button
        binding.btnCancel.setOnClickListener {
            downloadCall?.cancel()
            finish()
        }

        // Start download
        downloadPdf(examId)
    }

    private fun downloadPdf(examId: Int) {
        showDownloading()

        // Check cache first
        val cachedFile = File(cacheDir, "exam_$examId.pdf")
        if (cachedFile.exists() && cachedFile.length() > 0) {
            binding.tvDownloadPercent.text = "100%"
            binding.progressDownload.progress = 100
            openPdf(cachedFile)
            return
        }

        downloadCall = ApiClient.downloadPdf(
            examId = examId,
            cacheDir = cacheDir,
            onProgress = { percent ->
                runOnUiThread {
                    binding.progressDownload.progress = percent
                    binding.tvDownloadPercent.text = "$percent%"

                    // Show cancel button if download takes long
                    if (percent < 50) {
                        binding.btnCancel.visibility = View.VISIBLE
                    }
                }
            },
            onSuccess = { file ->
                runOnUiThread {
                    openPdf(file)
                }
            },
            onError = { errorMsg ->
                runOnUiThread {
                    showError(errorMsg)
                }
            }
        )
    }

    private fun openPdf(file: File) {
        try {
            fileDescriptor = ParcelFileDescriptor.open(
                file, ParcelFileDescriptor.MODE_READ_ONLY
            )
            pdfRenderer = PdfRenderer(fileDescriptor!!)
            totalPages = pdfRenderer!!.pageCount
            currentPage = 0
            renderPage(0)
            showPdfViewer()
        } catch (e: Exception) {
            showError(getString(R.string.error_pdf) + ": ${e.message}")
        }
    }

    private fun renderPage(pageIndex: Int) {
        val renderer = pdfRenderer ?: return
        if (pageIndex < 0 || pageIndex >= renderer.pageCount) return

        val page = renderer.openPage(pageIndex)

        // Render at 2x density for clarity
        val scale = 2
        val bitmap = Bitmap.createBitmap(
            page.width * scale,
            page.height * scale,
            Bitmap.Config.ARGB_8888
        )
        bitmap.eraseColor(android.graphics.Color.WHITE)

        page.render(
            bitmap,
            null,
            null,
            PdfRenderer.Page.RENDER_MODE_FOR_DISPLAY
        )
        page.close()

        binding.ivPdfPage.setImageBitmap(bitmap)
        updatePageIndicator()
    }

    private fun updatePageIndicator() {
        val display = "${currentPage + 1} / $totalPages"
        binding.tvPageIndicator.text = display
        binding.tvPageCounter.text = display

        binding.btnPrev.isEnabled = currentPage > 0
        binding.btnNext.isEnabled = currentPage < totalPages - 1
    }

    private fun showDownloading() {
        binding.layoutDownload.visibility = View.VISIBLE
        binding.layoutError.visibility = View.GONE
        binding.ivPdfPage.visibility = View.GONE
        binding.btnCancel.visibility = View.GONE
        binding.progressDownload.progress = 0
        binding.tvDownloadPercent.text = "0%"
    }

    private fun showPdfViewer() {
        binding.layoutDownload.visibility = View.GONE
        binding.layoutError.visibility = View.GONE
        binding.ivPdfPage.visibility = View.VISIBLE
    }

    private fun showError(message: String) {
        binding.layoutDownload.visibility = View.GONE
        binding.layoutError.visibility = View.VISIBLE
        binding.ivPdfPage.visibility = View.GONE
        binding.tvErrorMsg.text = message
    }

    override fun onDestroy() {
        super.onDestroy()
        downloadCall?.cancel()
        try {
            pdfRenderer?.close()
            fileDescriptor?.close()
        } catch (_: Exception) { }
    }
}
