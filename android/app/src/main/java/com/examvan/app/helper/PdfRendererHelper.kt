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
    private var currentPdfFile: File? = null
    private var downloadCall: Call? = null
    private var downloadStartedAtMs = 0L
    private var isCleanedUp = false

    /**
     * Dirilis saat app masuk background (profil low-memory / trim dari
     * sistem). Renderer + bitmap dibuang, lalu dibuka kembali dari cache
     * pada resume — menghemat ~6-20 MB saat proses hanya menunggu di LRU.
     */
    private var releasedForBackground = false
    private var releasedPage = 0
    private var releaseJob: kotlinx.coroutines.Job? = null

    /**
     * Fix review render PDF #3: increment terjadi di thread caller sedangkan
     * pembacaan di worker thread (di luar jangkauan mutex) — Long non-volatile
     * punya celah visibilitas.
     */
    @Volatile
    private var renderVersion = 0L

    /**
     * Mutex serializes PdfRenderer access (thread-safe — PdfRenderer is NOT thread-safe).
     *
     * Fixes #3: Unlike Job.cancel() which can race (old render continues on Default dispatcher
     * while new one starts), version counter ensures that even if an old render's bitmap
     * arrives late, it's discarded when [renderVersion] has advanced.
     */
    private val renderMutex = Mutex()

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

    private val memoryClassMb: Int by lazy {
        try {
            val am = context.getSystemService(Context.ACTIVITY_SERVICE) as? android.app.ActivityManager
            am?.memoryClass ?: 0
        } catch (_: Exception) { 0 }
    }

    private val useLowRamProfile: Boolean by lazy {
        PdfMemoryPolicy.useLowRamProfile(isLowRamDevice, memoryClassMb)
    }

    /**
     * Check if a page is already cached on disk.
     */
    fun hasCachedPdf(examId: Int): Boolean =
        PdfCache.hasCachedPdf(context.cacheDir, examId)

    /**
     * Start downloading the exam PDF.
     * Checks usable cache first (ada + tidak kosong + usia <= 12 jam — fix
     * review #2), then downloads with progress.
     */
    fun downloadPdf(examId: Int, token: String) {
        showDownloading()

        val cachedFile = PdfCache.pdfFile(context.cacheDir, examId)
        if (PdfCache.isCacheUsable(cachedFile, System.currentTimeMillis())) {
            binding.tvDownloadPercent.text = "100%"
            binding.progressDownload.progress = 100
            openPdfAsync(cachedFile)
            return
        }
        // Cache basi/rusak → buang agar tidak menabrak file unduhan baru.
        cachedFile.delete()

        downloadCall?.cancel()
        val mainHandler = android.os.Handler(android.os.Looper.getMainLooper())
        downloadCall = ApiClient.downloadPdf(
            examId = examId,
            token = token,
            // The server-side approval gate (14 Agustus 2026) serves the PDF
            // only to devices holding an approved exam_approvals row — the
            // device id must be the SAME value used at request-approval time.
            deviceId = com.examvan.app.DeviceIdResolver.resolveDeviceId(context),
            cacheDir = context.cacheDir,
            onProgress = { percent ->
                mainHandler.post {
                    val isActivityFinishing = (context as? android.app.Activity)?.let { it.isFinishing || it.isDestroyed } ?: false
                    if (isCleanedUp || isActivityFinishing) return@post
                    // Fix review UI/UX #3: batal berbasis WAKTU (grace 3 detik),
                    // bukan persen — dulu `if (percent < 50) visible` terbaca
                    // terbalik dan tidak pernah menyembunyikan kembali.
                    binding.btnCancel.visibility = if (
                        DownloadUiPolicy.cancelVisible(
                            System.currentTimeMillis() - downloadStartedAtMs
                        )
                    ) View.VISIBLE else View.GONE
                    onProgress?.invoke(percent)
                }
            },
            onSuccess = { file ->
                mainHandler.post {
                    val isActivityFinishing = (context as? android.app.Activity)?.let { it.isFinishing || it.isDestroyed } ?: false
                    if (isCleanedUp || isActivityFinishing) return@post
                    openPdfAsync(file)
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
        if (releasedForBackground) return
        if (currentPage > 0) {
            currentPage--
            renderPage(currentPage)
        }
    }

    /**
     * Navigate to next page if available.
     */
    fun nextPage() {
        if (releasedForBackground) return
        if (currentPage < totalPages - 1) {
            currentPage++
            renderPage(currentPage)
        }
    }

    /**
     * Open a PDF file for rendering — ASYNC di Dispatchers.Default
     * (fix review render PDF #1: dulu ParcelFileDescriptor.open + konstruktor
     * PdfRenderer — disk I/O + parse struktur PDF — berjalan di MAIN thread,
     * PDF besar = jank/ANR saat pembukaan). UI callback tetap di main.
     */
    private fun openPdfAsync(file: File) {
        currentPdfFile = file
        lifecycleScope.launch {
            val openedPageCount: Int? = renderMutex.withLock {
                if (isCleanedUp) return@withLock null
                withContext(Dispatchers.Default) {
                    try {
                        closeCurrentDocument()
                        fileDescriptor = ParcelFileDescriptor.open(
                            file, ParcelFileDescriptor.MODE_READ_ONLY
                        )
                        val renderer = PdfRenderer(fileDescriptor!!)
                        pdfRenderer = renderer
                        renderer.pageCount
                    } catch (t: Throwable) {
                        // Throwable (bukan Exception): OOM saat parse PDF besar
                        // juga harus ditangani, bukan meninggalkan layar putih.
                        Log.e(TAG, "Gagal membuka file PDF", t)
                        null
                    }
                }
            }

            android.os.Handler(android.os.Looper.getMainLooper()).post {
                if (isCleanedUp || !isMainThreadSafe()) return@post
                if (openedPageCount == null || openedPageCount <= 0) {
                    onError?.invoke("Gagal membuka file PDF")
                    return@post
                }
                totalPages = openedPageCount
                currentPage = if (pendingRestorePage in 0 until totalPages) {
                    val p = pendingRestorePage
                    pendingRestorePage = PENDING_PAGE_NONE
                    p
                } else 0
                // Viewer baru ditampilkan SETELAH bitmap pertama berhasil
                // dirender. Dulu showPdfViewer() dipanggil lebih dulu sehingga
                // perangkat yang gagal render (GPU/OOM) menampilkan layar putih
                // tanpa pesan apa pun.
                renderPage(currentPage, revealOnSuccess = true)
            }
        }
    }

    private fun isMainThreadSafe(): Boolean =
        (context as? android.app.Activity)?.let { !it.isFinishing && !it.isDestroyed } ?: true

    /** Tutup dokumen yang sedang terbuka dengan aman. */
    private fun closeCurrentDocument() {
        try { pdfRenderer?.close() } catch (_: Exception) { }
        try { fileDescriptor?.close() } catch (_: Exception) { }
        pdfRenderer = null
        fileDescriptor = null
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
    fun renderPage(pageIndex: Int, revealOnSuccess: Boolean = false) {
        if (releasedForBackground) return
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
                        renderPageBitmap(renderer, pageIndex)
                    } catch (t: Throwable) {
                        Log.e(TAG, "Error rendering page $pageIndex", t)
                        null
                    }
                }

                // Guard: only apply if this is still the most recent render request
                if (myVersion != renderVersion) {
                    bitmap?.recycle() // orphan bitmap — free immediately
                    return@withLock
                }
                if (bitmap == null) {
                    // Jangan tinggalkan viewer kosong/putih tanpa penjelasan.
                    if (revealOnSuccess) {
                        withContext(Dispatchers.Main) {
                            onError?.invoke("Gagal menampilkan halaman PDF")
                        }
                    }
                    return@withLock
                }

                val oldBitmap = currentBitmap
                currentBitmap = bitmap
                withContext(Dispatchers.Main) {
                    binding.ivPdfPage.resetZoom()
                    binding.ivPdfPage.setImageBitmap(bitmap)
                    if (revealOnSuccess) {
                        showPdfViewer()
                        onPdfReady?.invoke()
                    }
                }
                oldBitmap?.recycle()

                if (myVersion == renderVersion) {
                    updatePageIndicator()
                }
            }
        }
    }

    /**
     * Lepaskan renderer PDF + bitmap saat activity masuk background.
     *
     * Pada profil low-memory ini otomatis; pada perangkat lain dipanggil
     * dengan [force] = true saat sistem mengirim TRIM_MEMORY_BACKGROUND.
     * Bitmap + native renderer adalah konsumen memori terbesar app; saat
     * proses hanya menunggu di LRU, membuangnya menurunkan peluang proses
     * dibunuh OEM. PDF dibuka kembali dari cache pada resume.
     *
     * Aman terhadap render yang sedang berjalan: [renderVersion] membatalkan
     * hasil render lama, dan penutupan renderer menunggu [renderMutex].
     */
    fun releaseForBackground(force: Boolean = false) {
        if (isCleanedUp || pdfRenderer == null) return
        if (!force && !useLowRamProfile) return
        if (releasedForBackground) return

        releasedForBackground = true
        releasedPage = currentPage
        renderVersion++ // batalkan hasil render yang masih berjalan

        releaseJob = lifecycleScope.launch {
            renderMutex.withLock {
                withContext(Dispatchers.Main) {
                    if (isCleanedUp) return@withContext
                    closeCurrentDocument()
                    currentBitmap?.recycle()
                    currentBitmap = null
                    binding.ivPdfPage.setImageDrawable(null)
                    Log.i(
                        TAG,
                        "PDF dirilis saat background (lowRamProfile=$useLowRamProfile force=$force)"
                    )
                }
            }
        }
    }

    /**
     * Buka kembali PDF dari cache setelah [releaseForBackground].
     * Halaman terakhir dipertahankan. Bila cache sudah hilang, tampilkan
     * error agar siswa bisa menekan "Coba Lagi" (unduh ulang).
     */
    fun reopenIfNeeded() {
        if (isCleanedUp || !releasedForBackground) return
        val page = releasedPage
        val file = currentPdfFile
        val previousJob = releaseJob

        releaseJob = lifecycleScope.launch {
            previousJob?.join()
            if (isCleanedUp || !releasedForBackground) return@launch
            releasedForBackground = false
            if (file != null && file.exists() && file.length() > 0) {
                pendingRestorePage = page
                openPdfAsync(file)
            } else {
                onError?.invoke("Gagal membuka kembali PDF")
            }
        }
    }

    /**
     * Render satu halaman ke bitmap. Mencoba resolusi normal dulu; bila gagal
     * (termasuk OutOfMemoryError yang TIDAK tertangkap `catch (Exception)`)
     * ulangi dengan setengah resolusi.
     *
     * WAJIB ARGB_8888: PdfRenderer.Page.render() melempar
     * IllegalArgumentException("Unsupported pixel format") untuk konfigurasi
     * lain (mis. RGB_565) — inilah penyebab Realme C11 (low-RAM) gagal total.
     * Penghematan memori dilakukan lewat resolusi, bukan format bitmap.
     */
    private fun renderPageBitmap(renderer: PdfRenderer, pageIndex: Int): Bitmap? {
        val page = try {
            renderer.openPage(pageIndex)
        } catch (t: Throwable) {
            Log.e(TAG, "Gagal membuka halaman $pageIndex", t)
            return null
        }

        try {
            val screenWidth = context.resources.displayMetrics.widthPixels
            // Perangkat low-RAM (umumnya Android 10 RAM 2GB, mis. Realme C11)
            // diberi batas lebih rendah: teks soal tetap tajam di layar 720p,
            // tapi memori bitmap turun ~55% sehingga kecil kemungkinan OOM
            // saat dua bitmap sempat hidup bersamaan.
            val maxWidth = PdfMemoryPolicy.maxWidth(useLowRamProfile)
            val maxHeight = PdfMemoryPolicy.maxHeight(useLowRamProfile)
            var targetWidth = (screenWidth * 2).coerceAtMost(maxWidth)
            targetWidth = targetWidth.coerceAtMost(page.width * 2)

            val aspectRatio = page.height.toFloat() / page.width.toFloat()
            var targetHeight = (targetWidth * aspectRatio).toInt()
            if (targetHeight > maxHeight) {
                // Turunkan lebar proporsional — jangan hanya memotong tinggi,
                // karena halaman jadi gepeng.
                targetHeight = maxHeight
                targetWidth = (targetHeight / aspectRatio).toInt().coerceAtLeast(1)
            }
            if (targetWidth <= 0 || targetHeight <= 0) {
                Log.e(TAG, "Ukuran halaman tidak valid: ${page.width}x${page.height}")
                return null
            }

            return try {
                createAndRender(page, targetWidth, targetHeight)
            } catch (t: Throwable) {
                Log.e(TAG, "Render ${targetWidth}x$targetHeight gagal, coba resolusi lebih rendah", t)
                val w = (targetWidth / 2).coerceAtLeast(1)
                val h = (targetHeight / 2).coerceAtLeast(1)
                try {
                    createAndRender(page, w, h)
                } catch (t2: Throwable) {
                    Log.e(TAG, "Render fallback ${w}x$h juga gagal", t2)
                    null
                }
            }
        } finally {
            try { page.close() } catch (_: Throwable) { }
        }
    }

    private fun createAndRender(
        page: PdfRenderer.Page,
        width: Int,
        height: Int
    ): Bitmap {
        // ARGB_8888 wajib untuk PdfRenderer — jangan diganti RGB_565.
        val bmp = Bitmap.createBitmap(width, height, Bitmap.Config.ARGB_8888)
        try {
            bmp.eraseColor(Color.WHITE)
            page.render(bmp, null, null, PdfRenderer.Page.RENDER_MODE_FOR_DISPLAY)
            return bmp
        } catch (t: Throwable) {
            bmp.recycle()
            throw t
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
        downloadStartedAtMs = System.currentTimeMillis()
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
        releasedForBackground = false
        releaseJob?.cancel()
        releaseJob = null
        cancelDownload()
        setSwipeListener(null)
        renderVersion++ // invalidates any in-flight render
        closeCurrentDocument()
        currentBitmap?.recycle()
        currentBitmap = null
        currentPdfFile = null
    }

    companion object {
        private const val TAG = "PdfRendererHelper"
        private const val PENDING_PAGE_NONE = -1
    }
}
