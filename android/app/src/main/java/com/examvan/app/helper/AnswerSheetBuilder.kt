package com.examvan.app.helper

import android.content.Context
import android.view.View
import androidx.recyclerview.widget.LinearLayoutManager
import com.examvan.app.R
import com.examvan.app.databinding.ActivityExamViewerBinding

/**
 * Fasad lembar jawaban (fix arsitektur: RecyclerView — memori konstan
 * terhadap jumlah soal; dulu SEMUA view di-inflate sekaligus ke
 * LinearLayout dalam ScrollView).
 *
 * API publik dipertahankan identik agar ExamViewerActivity tidak berubah:
 *  - [build]            : rencanakan & pasang item ke adapter, kembalikan
 *                         jumlah soal valid;
 *  - [restoreFromSaved] : paksa re-bind dari ViewModel (pemanggil sudah
 *                         men-set jawaban sebelumnya ke ViewModel);
 *  - [applyDynamicTextColors] / panelTextColor / isPanelColorDark.
 *
 * Logika validasi/urutan soal ada di AnswerSheetPlanner; rendering per-tipe
 * ada di AnswerSheetAdapter.
 */
class AnswerSheetBuilder(
    private val binding: ActivityExamViewerBinding,
    context: Context
) {
    private var renderedCount: Int = 0
    private var items: List<AnswerSheetItem> = emptyList()

    // Warna dinamis berdasarkan exam panel_color — dibaca adapter saat bind.
    var panelTextColor: Int? = null
    var isPanelColorDark: Boolean = false

    // Callback saat jawaban siswa berubah / dihapus
    var onAnswerChanged: ((String, Any) -> Unit)? = null
    var onAnswerRemoved: ((String) -> Unit)? = null
    // Callback untuk membaca jawaban tersimpan (ViewModel)
    var getAnswer: ((String) -> Any?)? = null
    // Callback untuk spinner popup open/close tracking
    var onSpinnerPopupChanged: ((Int) -> Unit)? = null

    private val adapter = AnswerSheetAdapter(
        context = context,
        getAnswer = { number -> getAnswer?.invoke(number.toString()) },
        onAnswerChanged = { number, value -> onAnswerChanged?.invoke(number.toString(), value) },
        onAnswerRemoved = { number -> onAnswerRemoved?.invoke(number.toString()) },
        onSpinnerPopupChanged = { delta -> onSpinnerPopupChanged?.invoke(delta) }
    )

    init {
        // Fix review RecyclerView ronde 3 #1: pasang sink audit PERMANEN —
        // setiap soal yang dilewati tercatat di logcat (lambda statis tanpa
        // referensi activity; aman dibiarkan terpasang sepanjang proses).
        AnswerSheetPlanner.skipSink = { number, reason ->
            android.util.Log.w("AnswerSheet", "Soal $number dilewati: $reason")
        }
        binding.answerListContainer.layoutManager = LinearLayoutManager(context)
        binding.answerListContainer.adapter = adapter
    }

    /**
     * Rencanakan & pasang soal ke RecyclerView.
     *
     * @return jumlah soal yang BENAR-BENAR dirender (nomor valid) — dipakai
     *         pemanggil untuk `totalQuestions` sehingga hitungan dialog
     *         konfirmasi tidak lebih besar dari soal yang tampil.
     */
    fun build(questions: List<Map<String, Any>>): Int {
        items = AnswerSheetPlanner.plan(questions)
        renderedCount = items.size

        // Fix review RecyclerView ronde 2 #1: selalu kirim daftar ke adapter —
        // termasuk saat KOSONG, agar view basi dari build sebelumnya
        // terbersihkan (dulu early-return tanpa submit).
        adapter.submit(items)

        if (items.isEmpty()) {
            hideAnswerOverlay()
            return 0
        }

        applyDynamicTextColors()
        return renderedCount
    }

    /**
     * Restore jawaban tersimpan: pemanggil WAJIB sudah men-set jawaban ke
     * ViewModel (setStudentAnswers) SEBELUM memanggil method ini — adapter
     * cukup di-refresh karena setiap bind membaca lewat callback getAnswer.
     */
    fun restoreFromSaved(savedAnswers: Map<String, Any>) {
        adapter.refresh()
    }

    /** Jumlah soal yang berhasil dirender pada build terakhir (nomor valid). */
    fun getQuestionCount(): Int = renderedCount

    private fun hideAnswerOverlay() {
        binding.answerSheetToggle.visibility = View.GONE
        binding.answerSheetPanel.visibility = View.GONE
        binding.btnSubmitAnswers.visibility = View.GONE
    }

    fun applyDynamicTextColors() {
        adapter.panelTextColor = panelTextColor
        adapter.isPanelColorDark = isPanelColorDark
        adapter.applyDynamicColors()
    }
}
