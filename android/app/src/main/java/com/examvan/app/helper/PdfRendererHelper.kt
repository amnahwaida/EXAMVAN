package com.examvan.app.helper

import android.content.Context
import android.graphics.Bitmap
import android.graphics.Color
import android.graphics.pdf.PdfRenderer
import android.os.ParcelFileDescriptor
import android.util.Log
import android.view.View
import androidx.lifecycle.LifecycleCoroutineScope
import com.examvan.app.api.ApiClient
import com.examvan.app.databinding.ActivityExamViewerBinding
import com.examvan.app.view.ZoomableImageView
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import okhttp3.Call
import java.io.File

/**
 * Helper for PDF rendering lifecycle and page navigation.
 * Extracts PdfRenderer management from ExamViewerActivity.
 *
 * Fixes #3: Bitmap OOM prevention via Mutex — ensures only one
 * render coroutine runs at a time, preventing rapid-swipe bitmap pileup.
 */
class PdfRendererHelper(
    private val binding: ActivityExamViewerBinding,
    private val lifecycleScope: LifecycleCoroutineScope,
    private val context: Context
) {
    private var pdfRenderer: PdfRenderer? = null
    private var fileDescriptor: ParcelFileDescriptor? = null
    var currentPage = 0
        private set
    var totalPages = 0
        private set
    private var currentBitmap: Bitmap? = null
    private var downloadCall: Call? = null
    private var isCleanedUp = false

    /**
     * Mutex serializes PdfRenderer access (thread-safe — PdfRenderer is NOT thread-safe).
     *
     * Fixes #3: Unlike Job.cancel() which can race (old render continues on Default dispatcher
     * while new one starts), version counter ensures that even if an old render's bitmap
     * arrives late, it's discarded when [renderVersion] has advanced.
     */
    private val renderMutex = Mutex()
    private var renderVersion = 0L

    // Callback when PDF is fully ready and rendered
    var onPdfReady: (() -> Unit)? = null
    // Callback for errors
    var onError: ((String) -> Unit)? = null
    // Callback for download progress
    var onProgress: ((Int) -> Unit)? = null

    private val isLowRamDevice: Boolean by lazy {
        try {
            val am = context.getSystemService(Context.ACTIVITY_SERVICE) as? android.app.ActivityManager
            am?.isLowRamDevice == true
        } catch (_: Exception) { false }
    }

    /**
     * Check if a page is already cached on disk.
     */
    fun hasCachedPdf(examId: Int): Boolean {
        val cachedFile = File(context.cacheDir, "exam_$examId.pdf")
        return cachedFile.exists() && cachedFile.length() > 0
    }

    /**
     * Start downloading the exam PDF.
     * Checks cache first, then downloads with progress.
     */
    fun downloadPdf(examId: Int, token: String) {
        showDownloading()

        val cachedFile = File(context.cacheDir, "exam_$examId.pdf")
        if (cachedFile.exists() && cachedFile.length() > 0) {
            binding.tvDownloadPercent.text = "100%"
            binding.progressDownload.progress = 100
            openPdf(cachedFile)
            return
        }

        downloadCall?.cancel()
        val mainHandler = android.os.Handler(android.os.Looper.getMainLooper())
        downloadCall = ApiClient.downloadPdf(
            examId = examId,
            token = token,
            cacheDir = context.cacheDir,
            onProgress = { percent ->
                mainHandler.post {
                    val isActivityFinishing = (context as? android.app.Activity)?.let { it.isFinishing || it.isDestroyed } ?: false
                    if (isCleanedUp || isActivityFinishing) return@post
                    onProgress?.invoke(percent)
                }
            },
            onSuccess = { file ->
                mainHandler.post {
                    val isActivityFinishing = (context as? android.app.Activity)?.let { it.isFinishing || it.isDestroyed } ?: false
                    if (isCleanedUp || isActivityFinishing) return@post
                    openPdf(file)
                }
            },
            onError = { errorMsg ->
                mainHandler.post {
                    val isActivityFinishing = (context as? android.app.Activity)?.let { it.isFinishing || it.isDestroyed } ?: false
                    if (isCleanedUp || isActivityFinishing) return@post
                    onError?.invoke(errorMsg)
                }
            }
        )
    }

    fun cancelDownload() {
        downloadCall?.cancel()
    }

    /**
     * Navigate to previous page if available.
     */
    fun prevPage() {
        if (currentPage > 0) {
            currentPage--
            renderPage(currentPage)
        }
    }

    /**
     * Navigate to next page if available.
     */
    fun nextPage() {
        if (currentPage < totalPages - 1) {
            currentPage++
            renderPage(currentPage)
        }
    }

    /**
     * Open a PDF file for rendering.
     */
    private fun openPdf(file: File) {
        try {
            fileDescriptor = ParcelFileDescriptor.open(
                file, ParcelFileDescriptor.MODE_READ_ONLY
            )
            val fd = fileDescriptor ?: run {
                onError?.invoke("Gagal membuka file PDF")
                return
            }
            pdfRenderer = PdfRenderer(fd)
            val renderer = pdfRenderer ?: run {
                onError?.invoke("Gagal merender PDF")
                return
            }
            totalPages = renderer.pageCount
            // Restore pending page if set
            if (pendingRestorePage in 0 until totalPages) {
                currentPage = pendingRestorePage
                pendingRestorePage = PENDING_PAGE_NONE
            } else {
                currentPage = 0
            }
            renderPage(currentPage)
            showPdfViewer()
            onPdfReady?.invoke()
        } catch (e: Exception) {
            onError?.invoke("Gagal membuka file PDF: ${e.message}")
        }
    }

    /** Pending page to restore after configuration change */
    private var pendingRestorePage = -1

    fun setPendingRestorePage(page: Int) {
        pendingRestorePage = page
    }

    /**
     * Render a PDF page to bitmap on a background thread with Mutex + version counter.
     *
     * Fixes #3: Version counter approach is safer than Job.cancel():
     * - Mutex serializes PdfRenderer access (not thread-safe by design)
     * - [renderVersion] atomically increases; late bitmaps from old renders
     *   are discarded instead of updating the UI
     * - No race window where two renders run concurrently
     */
    fun renderPage(pageIndex: Int) {
        val renderer = pdfRenderer ?: return
        if (pageIndex < 0 || pageIndex >= renderer.pageCount) return

        val myVersion = ++renderVersion

        lifecycleScope.launch {
            // Mutex serializes PdfRenderer access (safe even if version check
            // rejects the result — the Mutex still protects openPage/close)
            renderMutex.withLock {
                // If version already advanced past us, skip the expensive render
                if (myVersion != renderVersion) return@withLock

                val bitmap = withContext(Dispatchers.Default) {
                    try {
                        val page = renderer.openPage(pageIndex)

                        val screenWidth = context.resources.displayMetrics.widthPixels
                        var targetWidth = (screenWidth * 2).coerceAtMost(2048)
                        targetWidth = targetWidth.coerceAtMost(page.width * 2)

                        val aspectRatio = page.height.toFloat() / page.width.toFloat()
                        val targetHeight = (targetWidth * aspectRatio).toInt().coerceAtMost(4096)

                        val bmpConfig = if (isLowRamDevice) Bitmap.Config.RGB_565 else Bitmap.Config.ARGB_8888
                        val bmp = Bitmap.createBitmap(targetWidth, targetHeight, bmpConfig)
                        bmp.eraseColor(Color.WHITE)

                        page.render(bmp, null, null, PdfRenderer.Page.RENDER_MODE_FOR_DISPLAY)
                        page.close()
                        bmp
                    } catch (e: Exception) {
                        Log.e(TAG, "Error rendering page $pageIndex", e)
                        null
                    }
                }

                // Guard: only apply if this is still the most recent render request
                if (myVersion != renderVersion || bitmap == null) {
                    bitmap?.recycle() // orphan bitmap — free immediately
                    return@withLock
                }

                val oldBitmap = currentBitmap
                currentBitmap = bitmap
                withContext(Dispatchers.Main) {
                    binding.ivPdfPage.resetZoom()
                    binding.ivPdfPage.setImageBitmap(bitmap)
                }
                oldBitmap?.recycle()

                if (myVersion == renderVersion) {
                    updatePageIndicator()
                }
            }
        }
    }

    fun updatePageIndicator() {
        val display = if (totalPages > 0) {
            "${currentPage + 1} / $totalPages"
        } else {
            "-"
        }
        binding.tvPageIndicator.text = display
        binding.tvPageCounter.text = display
        binding.btnPrev.isEnabled = currentPage > 0
        binding.btnNext.isEnabled = currentPage < totalPages - 1
    }

    fun showDownloading() {
        binding.layoutDownload.visibility = View.VISIBLE
        binding.layoutError.visibility = View.GONE
        binding.ivPdfPage.visibility = View.GONE
        binding.btnCancel.visibility = View.GONE
        binding.progressDownload.progress = 0
        binding.tvDownloadPercent.text = "0%"
    }

    fun showPdfViewer() {
        binding.layoutDownload.visibility = View.GONE
        binding.layoutError.visibility = View.GONE
        binding.ivPdfPage.visibility = View.VISIBLE
    }

    fun showError(message: String) {
        binding.layoutDownload.visibility = View.GONE
        binding.layoutError.visibility = View.VISIBLE
        binding.ivPdfPage.visibility = View.GONE
        binding.tvErrorMsg.text = message
    }

    fun setSwipeListener(listener: ZoomableImageView.OnSwipeListener?) {
        binding.ivPdfPage.swipeListener = listener
    }

    fun cleanup() {
        isCleanedUp = true
        cancelDownload()
        setSwipeListener(null)
        renderVersion++ // invalidates any in-flight render
        try {
            pdfRenderer?.close()
            fileDescriptor?.close()
        } catch (_: Exception) { }
        pdfRenderer = null
        fileDescriptor = null
        currentBitmap?.recycle()
        currentBitmap = null
    }

    companion object {
        private const val TAG = "PdfRendererHelper"
        private const val PENDING_PAGE_NONE = -1
    }
}
