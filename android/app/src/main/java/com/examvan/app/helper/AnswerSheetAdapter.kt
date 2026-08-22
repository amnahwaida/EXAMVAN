package com.examvan.app.helper

import android.content.Context
import android.view.LayoutInflater
import android.view.View
import android.view.ViewGroup
import android.widget.ArrayAdapter
import android.widget.CheckBox
import android.widget.EditText
import android.widget.LinearLayout
import android.widget.RadioButton
import android.widget.RadioGroup
import android.widget.Spinner
import android.widget.TextView
import androidx.core.content.ContextCompat
import androidx.recyclerview.widget.RecyclerView
import com.examvan.app.R

/**
 * Adapter lembar jawaban (fix arsitektur: RecyclerView — view didaur ulang,
 * memori konstan terhadap jumlah soal; dulu SEMUA soal di-inflate sekaligus
 * ke LinearLayout).
 *
 * Sumber kebenaran jawaban adalah callback [getAnswer] (ViewModel) — setiap
 * bind membaca nilai terkini sehingga scroll naik-turun selalu menampilkan
 * jawaban benar dan restore cukup dengan notifyDataSetChanged().
 *
 * PERHATIAN RECYCLING: EditText mempertahankan TextWatcher antar re-bind —
 * watcher lama wajib dilepas SEBELUM setText (teks milik soal lain tidak
 * boleh memicu event jawaban soal sebelumnya).
 */
internal class AnswerSheetAdapter(
    private val context: Context,
    private val getAnswer: (Int) -> Any?,
    private val onAnswerChanged: (Int, Any) -> Unit,
    private val onAnswerRemoved: (Int) -> Unit,
    private val onSpinnerPopupChanged: (Int) -> Unit
) : RecyclerView.Adapter<AnswerSheetAdapter.QuestionViewHolder>() {

    private var items: List<AnswerSheetItem> = emptyList()
    var panelTextColor: Int? = null
    var isPanelColorDark: Boolean = false

    fun submit(newItems: List<AnswerSheetItem>) {
        items = newItems
        notifyDataSetChanged()
    }

    /** Paksa re-bind (mis. setelah restore jawaban dari prefs). */
    fun refresh() = notifyDataSetChanged()

    override fun getItemCount(): Int = items.size

    override fun getItemViewType(position: Int): Int = when (items[position].type) {
        AnswerSheetItem.TYPE_MATCHING -> TYPE_MATCHING
        AnswerSheetItem.TYPE_SHORT_ANSWER -> TYPE_SHORT_ANSWER
        else -> TYPE_CHOICE
    }

    override fun onCreateViewHolder(parent: ViewGroup, viewType: Int): QuestionViewHolder {
        val layoutRes = when (viewType) {
            TYPE_MATCHING -> R.layout.item_question_matching
            TYPE_SHORT_ANSWER -> R.layout.item_question_short_answer
            else -> R.layout.item_question_choice
        }
        val view = LayoutInflater.from(context).inflate(layoutRes, parent, false)
        return QuestionViewHolder(view)
    }

    override fun onBindViewHolder(holder: QuestionViewHolder, position: Int) {
        val item = items[position]
        holder.view.setTag(R.id.tag_question_number, item.number.toString())
        holder.bind(item)
        applyColors(holder.view)
    }

    // ── Warna dinamis panel ──────────────────────────────────────────────

    fun applyDynamicColors() {
        notifyDataSetChanged() // recolor jarang terjadi — re-bind semua murah
    }

    private fun applyColors(root: View) {
        val textColor = panelTextColor ?: return
        recolorHierarchy(root, textColor, isPanelColorDark)
    }

    private fun recolorHierarchy(view: View, textColor: Int, isDark: Boolean) {
        if (view is ViewGroup) {
            for (i in 0 until view.childCount) recolorHierarchy(view.getChildAt(i), textColor, isDark)
        }
        if (view is TextView) {
            view.setTextColor(textColor)
            if (view is EditText) {
                view.setHintTextColor(
                    ContextCompat.getColor(
                        context,
                        if (isDark) R.color.hint_on_dark_panel else R.color.hint_on_light_panel
                    )
                )
                view.backgroundTintList = android.content.res.ColorStateList.valueOf(textColor)
            }
            if (view is RadioButton) {
                view.buttonTintList = android.content.res.ColorStateList.valueOf(textColor)
            }
            if (view is CheckBox) {
                view.buttonTintList = android.content.res.ColorStateList.valueOf(textColor)
            }
        }
    }

    // ── ViewHolder ───────────────────────────────────────────────────────

    inner class QuestionViewHolder(val view: View) : RecyclerView.ViewHolder(view) {

        /** Watcher teks aktif — dilepas sebelum re-bind (lihat catatan kelas). */
        private var shortAnswerWatcher: android.text.TextWatcher? = null

        fun bind(item: AnswerSheetItem) {
            when (item.type) {
                AnswerSheetItem.TYPE_SINGLE_CHOICE ->
                    bindRadio(item.number, item.choices!!, labelSuffix = null)
                AnswerSheetItem.TYPE_TRUE_FALSE ->
                    bindRadio(item.number, listOf("TRUE", "FALSE"),
                        labelSuffix = R.string.answer_sheet_true_false_suffix)
                AnswerSheetItem.TYPE_MULTIPLE_CHOICE ->
                    bindMultiple(item.number, item.choices!!)
                AnswerSheetItem.TYPE_MATCHING ->
                    bindMatching(item.number, item.leftItems!!, item.rightItems!!)
                AnswerSheetItem.TYPE_SHORT_ANSWER ->
                    bindShortAnswer(item.number)
            }
        }

        private fun questionLabel(number: Int, suffixRes: Int? = null): String =
            context.getString(R.string.answer_sheet_question_number, number) +
                (suffixRes?.let { " " + context.getString(it) } ?: "")

        private fun currentText(number: Int): String? = getAnswer(number) as? String

        private fun currentStringList(number: Int): List<String>? =
            getAnswer(number) as? List<String>

        @Suppress("UNCHECKED_CAST")
        private fun currentStringMap(number: Int): Map<String, String>? =
            getAnswer(number) as? Map<String, String>

        // ── Single choice / true-false ───────────────────────────────────

        private fun bindRadio(number: Int, options: List<String>, labelSuffix: Int?) {
            view.findViewById<TextView>(R.id.tvQuestionLabel).text =
                questionLabel(number, labelSuffix)
            val group = view.findViewById<RadioGroup>(R.id.rgChoices)
            val checkboxes = view.findViewById<LinearLayout>(R.id.layoutCheckboxes)
            checkboxes.visibility = View.GONE
            group.visibility = View.VISIBLE
            group.removeAllViews()

            val selected = currentText(number)
            for (option in options) {
                val rb = RadioButton(context).apply {
                    text = option
                    setTextColor(ContextCompat.getColor(context, R.color.on_surface))
                    buttonTintList = ContextCompat.getColorStateList(context, R.color.primary)
                    textSize = 14f
                    setPaddingRelative(4, 0, 16, 0)
                    // State dulu, listener belakangan — bind tidak memicu event.
                    isChecked = option == selected
                }
                rb.setOnCheckedChangeListener { _, _ -> onAnswerChanged(number, option) }
                group.addView(rb)
            }
        }

        // ── Multiple choice ──────────────────────────────────────────────

        private fun bindMultiple(number: Int, choices: List<String>) {
            view.findViewById<TextView>(R.id.tvQuestionLabel).text =
                questionLabel(number, R.string.answer_sheet_multiple_suffix)
            val group = view.findViewById<RadioGroup>(R.id.rgChoices)
            val layout = view.findViewById<LinearLayout>(R.id.layoutCheckboxes)
            group.visibility = View.GONE
            layout.visibility = View.VISIBLE
            layout.removeAllViews()

            val selected = currentStringList(number)?.toSet() ?: emptySet()

            for (choice in choices) {
                val cb = CheckBox(context).apply {
                    text = choice
                    setTextColor(ContextCompat.getColor(context, R.color.on_surface))
                    buttonTintList = ContextCompat.getColorStateList(context, R.color.primary)
                    textSize = 14f
                    setPaddingRelative(4, 0, 16, 0)
                    isChecked = choice in selected
                }
                cb.setOnCheckedChangeListener { _, _ -> emitMultiple(layout, number) }
                layout.addView(cb)
            }
        }

        private fun emitMultiple(layout: LinearLayout, number: Int) {
            val selected = mutableListOf<String>()
            for (i in 0 until layout.childCount) {
                val child = layout.getChildAt(i) as? CheckBox
                if (child?.isChecked == true) selected.add(child.text.toString())
            }
            if (selected.isEmpty()) onAnswerRemoved(number) else onAnswerChanged(number, selected)
        }

        // ── Matching ─────────────────────────────────────────────────────

        private fun bindMatching(number: Int, leftItems: List<String>, rightItems: List<String>) {
            view.findViewById<TextView>(R.id.tvMatchingLabel).text =
                questionLabel(number, R.string.answer_sheet_matching_suffix)
            val container = view.findViewById<LinearLayout>(R.id.layoutMatchingContainer)
            container.removeAllViews()

            val saved = currentStringMap(number) ?: emptyMap()

            for (leftItem in leftItems) {
                val rowView = LayoutInflater.from(context)
                    .inflate(R.layout.item_matching_row, container, false)
                rowView.findViewById<TextView>(R.id.tvLeftItem).text = leftItem
                val spinner = rowView.findViewById<Spinner>(R.id.spinnerRightItem)

                val spinnerItems = mutableListOf(context.getString(R.string.spinner_placeholder))
                spinnerItems.addAll(rightItems)
                val spinnerAdapter =
                    ArrayAdapter(context, android.R.layout.simple_spinner_item, spinnerItems)
                spinnerAdapter.setDropDownViewResource(android.R.layout.simple_spinner_dropdown_item)
                spinner.adapter = spinnerAdapter

                spinner.setOnTouchListener { _, event ->
                    if (event.action == android.view.MotionEvent.ACTION_UP) onSpinnerPopupChanged(1)
                    false
                }

                val initialGuard = SpinnerInitialSelectionGuard()
                spinner.onItemSelectedListener = object : android.widget.AdapterView.OnItemSelectedListener {
                    override fun onItemSelected(parent: android.widget.AdapterView<*>?, v: View?, position: Int, id: Long) {
                        if (initialGuard.isInitialSelection()) return
                        onSpinnerPopupChanged(-1)
                        if (v is TextView) {
                            v.setTextColor(panelTextColor ?: ContextCompat.getColor(context, R.color.on_surface))
                        }
                        // Sumber kebenaran = ViewModel: baca terkini, ubah satu
                        // kunci, tulis kembali — aman terhadap recycling.
                        val answers = currentStringMap(number)?.toMutableMap() ?: mutableMapOf()
                        if (position > 0) answers[leftItem] = rightItems[position - 1] else answers.remove(leftItem)
                        if (answers.isEmpty()) onAnswerRemoved(number) else onAnswerChanged(number, answers)
                    }

                    override fun onNothingSelected(parent: android.widget.AdapterView<*>?) {
                        // Quirk Android: nyaris tidak pernah dipanggil; bocor
                        // counter popup tertutup reset saat fokus kembali.
                        onSpinnerPopupChanged(-1)
                    }
                }

                // Restore pilihan tersimpan untuk baris ini (posisi = idx + 1
                // karena index 0 adalah placeholder).
                saved[leftItem]?.let { rightValue ->
                    rightItems.indexOf(rightValue).takeIf { it >= 0 }?.let { idx ->
                        spinner.setSelection(idx + 1)
                    }
                }

                container.addView(rowView)
            }
        }

        // ── Short answer ─────────────────────────────────────────────────

        private fun bindShortAnswer(number: Int) {
            view.findViewById<TextView>(R.id.tvQuestionLabel).text =
                questionLabel(number, R.string.answer_sheet_short_answer_suffix)
            val editText = view.findViewById<EditText>(R.id.etShortAnswer)
            editText.filters = arrayOf(android.text.InputFilter.LengthFilter(IdentityFormPolicy.MAX_LENGTH))

            // KUNCI RECYCLING: lepas watcher lama SEBELUM menimpa teks, agar
            // teks soal lain tidak tercatat sebagai jawaban soal ini.
            shortAnswerWatcher?.let { editText.removeTextChangedListener(it) }
            editText.setText(currentText(number) ?: "")

            val watcher = object : android.text.TextWatcher {
                override fun beforeTextChanged(s: CharSequence?, st: Int, c: Int, a: Int) {}
                override fun onTextChanged(s: CharSequence?, st: Int, b: Int, c: Int) {}
                override fun afterTextChanged(s: android.text.Editable?) {
                    val ans = s?.toString()?.trim() ?: ""
                    if (ans.isNotEmpty()) onAnswerChanged(number, ans) else onAnswerRemoved(number)
                }
            }
            editText.addTextChangedListener(watcher)
            shortAnswerWatcher = watcher
        }
    }

    companion object {
        private const val TYPE_CHOICE = 0
        private const val TYPE_MATCHING = 1
        private const val TYPE_SHORT_ANSWER = 2
    }
}
