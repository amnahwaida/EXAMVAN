package com.examvan.app.helper

import android.view.View
import com.examvan.app.databinding.ActivityExamViewerBinding

/**
 * Deduplikasi manipulasi visibilitas kontrol layar ujian (fix temuan review:
 * "duplikasi blok 'hide all buttons' muncul 4x di 2 file").
 *
 * Sebelumnya urutan `visibility = View.GONE/VISIBLE` yang sama disalin di:
 *  - SecurityEnforcer.showStrictModeFailed / showGestureBlocked /
 *    showSecurityViolation
 *  - ExamViewerActivity.showPendingSubmitRecoveryScreen /
 *    showExamAlreadySubmittedScreen
 *
 * Semua titik itu kini memanggil helper ini, sehingga penambahan kontrol baru
 * pada layout cukup diubah di SATU tempat.
 */
object ExamScreenPanels {

    /** Sembunyikan seluruh kontrol utama (dipakai saat layar penuh error/blok). */
    fun hideAllControls(binding: ActivityExamViewerBinding) {
        binding.btnBack.visibility = View.GONE
        binding.btnToggleAnswerSheet.visibility = View.GONE
        binding.btnSubmitAnswers.visibility = View.GONE
        binding.btnPrev.visibility = View.GONE
        binding.btnNext.visibility = View.GONE
        binding.answerSheetToggle.visibility = View.GONE
        binding.layoutDownload.visibility = View.GONE
        binding.ivPdfPage.visibility = View.GONE
    }

    /** Tampilkan kembali kontrol navigasi utama (setelah layar blok ditutup). */
    fun showMainControls(binding: ActivityExamViewerBinding) {
        binding.btnBack.visibility = View.VISIBLE
        binding.btnToggleAnswerSheet.visibility = View.VISIBLE
        binding.btnSubmitAnswers.visibility = View.VISIBLE
        binding.btnPrev.visibility = View.VISIBLE
        binding.btnNext.visibility = View.VISIBLE
        binding.answerSheetToggle.visibility = View.VISIBLE
    }
}
