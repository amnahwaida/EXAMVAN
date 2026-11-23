package com.examvan.app

import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

/**
 * Guard: semua layar blokir di strict mode harus punya jalan keluar.
 *
 * Kasus yang ditutup:
 *  - PDF gagal diunduh/dibuka: dulu hanya "Coba Lagi" dan back diblokir;
 *    bila end_time kosong, watchdog tidak pernah menutup ujian.
 *  - Soal gagal dimuat: lembar jawaban disembunyikan, jadi tombol kumpulkan
 *    dan toggle ikut hilang.
 *  - Submit manual gagal permanen: dialog hanya punya OK, back diblokir.
 *
 * Kontrak:
 *  - layar error strict menampilkan "Keluar",
 *  - pra-PDF logout + finish; setelah PDF siap auto-submit,
 *  - soal tidak tersedia punya "Coba Lagi" + "Keluar",
 *  - submit gagal strict punya "Keluar" yang tetap menyimpan jawaban.
 *
 *   ./gradlew :app:testStudentDebugUnitTest
 */
class StrictBlockingEscapeGuardTest {

    private fun resolveModuleDir(): File {
        val cwd = File(System.getProperty("user.dir") ?: ".")
        return when {
            File(cwd, "src/main/java").isDirectory -> cwd
            File(cwd, "app/src/main/java").isDirectory -> cwd.resolve("app")
            else -> error("Direktori sumber Android tidak ditemukan (cwd=${cwd.absolutePath})")
        }
    }

    private fun read(relativePath: String): String =
        resolveModuleDir().resolve(relativePath).readText()

    private fun examViewerActivity(): String =
        read("src/main/java/com/examvan/app/ExamViewerActivity.kt")

    private fun pdfRendererHelper(): String =
        read("src/main/java/com/examvan/app/helper/PdfRendererHelper.kt")

    private fun submissionManager(): String =
        read("src/main/java/com/examvan/app/helper/SubmissionManager.kt")

    @Test
    fun pdfErrorScreen_hasExitInStrictMode() {
        val showError = examViewerActivity()
            .substringAfter("private fun showError(")
            .substringBefore("private fun exitBlockingScreen()")
        assertTrue(
            "Layar error strict harus menampilkan tombol Keluar",
            showError.contains("if (securityEnforcer.strictMode)") &&
                showError.contains("binding.btnOpenResult.visibility = View.VISIBLE")
        )
        assertTrue(
            "Tombol Keluar layar error harus memakai jalur exit blokir",
            showError.contains("exitBlockingScreen()")
        )
    }

    @Test
    fun blockingExit_logsOutBeforePdfAndAutoSubmitsAfter() {
        val helper = examViewerActivity()
            .substringAfter("private fun exitBlockingScreen()")
            .substringBefore("private fun showQuestionsUnavailableDialog()")
        assertTrue(
            "Pra-PDF: keluar harus logout presence",
            helper.contains("sendAccessLog(\"logout\", retryOnFailure = true)")
        )
        assertTrue(
            "Pra-PDF: keluar harus menutup activity",
            helper.contains("finish()")
        )
        assertTrue(
            "Setelah PDF siap: keluar WAJIB auto-submit",
            helper.contains("viewModel.isPdfReady.value") &&
                helper.contains("submissionManager.autoSubmitAndExit()")
        )
    }

    @Test
    fun pdfViewer_clearsStaleErrorExit() {
        val showPdfViewer = pdfRendererHelper()
            .substringAfter("fun showPdfViewer()")
            .substringBefore("fun showError(")
        assertTrue(
            "Viewer PDF harus menyembunyikan tombol Keluar dari layar error lama",
            showPdfViewer.contains("binding.btnOpenResult.visibility = View.GONE")
        )
        assertTrue(
            "Listener tombol Keluar lama harus dilepas",
            showPdfViewer.contains("binding.btnOpenResult.setOnClickListener(null)")
        )
    }

    @Test
    fun questionsUnavailable_hasRetryAndExit() {
        val dialog = examViewerActivity()
            .substringAfter("private fun showQuestionsUnavailableDialog()")
            .substringBefore("private fun sendAccessLog(")
        assertTrue(
            "Dialog harus memegang flag app-dialog",
            dialog.contains("holdAppDialog(AppDialogIds.QUESTIONS_UNAVAILABLE)")
        )
        assertTrue(
            "Dialog tidak boleh bisa ditutup back tanpa aksi",
            dialog.contains("setCancelable(false)")
        )
        assertTrue(
            "Harus ada Coba Lagi yang memanggil fetch soal",
            dialog.contains("setPositiveButton") &&
                dialog.contains("fetchQuestionsFromServer()")
        )
        assertTrue(
            "Harus ada Keluar yang memakai jalur exit blokir",
            dialog.contains("setNegativeButton") &&
                dialog.contains("exitBlockingScreen()")
        )
    }

    @Test
    fun applyQuestions_allInvalid_showsBlockingDialog() {
        val apply = examViewerActivity()
            .substringAfter("private fun applyQuestions(")
            .substringBefore("private fun fetchQuestionsFromServer()")
        assertTrue(
            "Jumlah soal yang dirender harus dipakai, bukan ukuran list mentah",
            apply.contains("val renderedQuestions = answerSheetBuilder.build(questions)")
        )
        assertTrue(
            "Bila semua soal invalid (0 dirender), jangan sembunyikan panel tanpa dialog",
            apply.contains("if (renderedQuestions == 0)") &&
                apply.contains("showQuestionsUnavailableDialog()")
        )
    }

    @Test
    fun questionsFetchFailure_showsBlockingDialog() {
        val fetch = examViewerActivity()
            .substringAfter("private fun fetchQuestionsFromServer()")
            .substringBefore("private fun updateSecurityBanner()")
        val calls = Regex("showQuestionsUnavailableDialog\\(\\)").findAll(fetch).count()
        assertTrue(
            "Fetch soal kosong DAN error harus sama-sama menampilkan dialog (bukan diam)",
            calls >= 2
        )
    }

    @Test
    fun strictSubmitFailure_offersExitThatKeepsAnswers() {
        val dialog = submissionManager()
            .substringAfter("val dialogBuilder = AlertDialog.Builder(context)")
            .substringBefore("AuditLog.e(AuditLog.Events.SUBMIT_FAILED")
        assertTrue(
            "Submit gagal strict harus punya tombol Keluar",
            dialog.contains("if (strictMode)") &&
                dialog.contains("setNegativeButton")
        )
        assertTrue(
            "Keluar harus melepas holder dialog",
            dialog.contains("releaseAppDialog(AppDialogIds.SUBMIT_FAILED)")
        )
        assertTrue(
            "Keluar harus auto-submit agar jawaban tetap tersimpan/terkirim",
            dialog.contains("autoSubmitAndExit()")
        )
    }
}
