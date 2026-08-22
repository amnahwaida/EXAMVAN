package com.examvan.app.helper

import android.content.Context
import android.view.LayoutInflater
import android.view.View
import android.view.ViewGroup
import android.widget.*
import androidx.core.content.ContextCompat
import com.examvan.app.R
import com.examvan.app.databinding.ActivityExamViewerBinding

/**
 * Builds and manages the digital answer sheet UI for exam questions.
 *
 * Fixes #6: All question views are tagged with their question number
 * via setTag(R.id.tag_question_number, number), enabling reliable
 * answer restore without fragile string matching.
 */
class AnswerSheetBuilder(
    private val binding: ActivityExamViewerBinding,
    private val context: Context
) {
    private var questions: List<Map<String, Any>> = emptyList()

    /** Jumlah soal yang dirender pada build terakhir (nomor valid). */
    private var renderedCount: Int = 0

    // Dynamic coloring variables based on exam panel_color
    var panelTextColor: Int? = null
    var isPanelColorDark: Boolean = false

    // Callback when a student answer changes
    var onAnswerChanged: ((String, Any) -> Unit)? = null
    // Callback when a student answer is removed
    var onAnswerRemoved: ((String) -> Unit)? = null
    // Callback for checking if an answer exists
    var getAnswer: ((String) -> Any?)? = null
    // Callback for spinner popup open/close tracking
    var onSpinnerPopupChanged: ((Int) -> Unit)? = null

    /**
     * Build or rebuild the answer sheet from question config.
     * Clears existing views first.
     *
     * @return jumlah soal yang BENAR-BENAR dirender (nomor valid) — dipakai
     *         pemanggil untuk `totalQuestions` sehingga hitungan dialog
     *         konfirmasi tidak lebih besar dari soal yang tampil.
     */
    fun build(questions: List<Map<String, Any>>): Int {
        this.questions = questions
        renderedCount = 0
        val container = binding.answerListContainer
        container.removeAllViews()

        if (questions.isEmpty()) {
            hideAnswerOverlay()
            return 0
        }

        val seenNumbers = mutableSetOf<Int>()
        for (q in questions) {
            // Parsing robust: nomor bisa JSON number ATAU string (server
            // menyimpan Question.Number sebagai interface{}). Soal dengan nomor
            // tidak valid dilewati — bukan di-skip diam-diam karena salah tipe.
            val number = QuestionParsing.questionNumber(q["number"]) ?: continue

            // Fix review lembar jawaban ronde 2 #2: nomor duplikat membuat dua
            // view bertabrakan di key jawaban yang sama — lewati.
            if (!seenNumbers.add(number)) continue

            // Fix review lembar jawaban ronde 2 #1: keputusan render eksplisit
            // via policy — tipe tak dikenal & data pilihan tidak valid di-SKIP
            // DENGAN ALASAN, totalQuestions tetap jujur.
            when (val decision = com.examvan.app.helper.QuestionRenderPolicy.evaluate(q)) {
                is com.examvan.app.helper.QuestionRenderPolicy.Render -> {
                    renderedCount++
                    when (decision.type) {
                        "single_choice" -> addSingleChoiceQuestion(container, number, decision.choices!!)
                        "true_false" -> addTrueFalseQuestion(container, number)
                        "multiple_choice" -> addMultipleChoiceQuestion(container, number, decision.choices!!)
                        "matching" -> addMatchingQuestion(container, number, decision.leftItems!!, decision.rightItems!!)
                        "short_answer" -> addShortAnswerQuestion(container, number)
                    }
                }
                is com.examvan.app.helper.QuestionRenderPolicy.Skip -> {
                    android.util.Log.w(
                        "AnswerSheet",
                        "Soal $number dilewati: ${decision.reason}"
                    )
                }
            }
        }

        applyDynamicTextColors()
        return renderedCount
    }

    /**
     * Restore answers from saved data into the UI (fix review lembar jawaban
     * #1: nilai TERSTRUKTUR — multiple choice List, matching Map; format
     * string legacy "[A, B]"/"{1=A}" tetap didukung sebagai fallback).
     * Uses view tags to match question numbers efficiently.
     */
    fun restoreFromSaved(savedAnswers: Map<String, Any>) {
        for (i in 0 until binding.answerListContainer.childCount) {
            val view = binding.answerListContainer.getChildAt(i)
            val tag = view.getTag(R.id.tag_question_number)
            val questionNum = tag?.toString() ?: continue
            val savedValue = savedAnswers[questionNum] ?: continue

            restoreAnswerInView(view, questionNum, savedValue)
        }
    }

    /** Nilai jawaban bisa String (legacy/typed) / List / Map. */
    private fun asStringList(value: Any): List<String>? = when (value) {
        is List<*> -> value.filterIsInstance<String>().takeIf { it.isNotEmpty() }
        is String -> parseLegacyStringList(value)
        else -> null
    }

    @Suppress("UNCHECKED_CAST")
    private fun asStringMap(value: Any): Map<String, String>? = when (value) {
        is Map<*, *> -> value.entries
            .filter { it.key is String && it.value is String }
            .associate { it.key as String to it.value as String }
            .takeIf { it.isNotEmpty() }
        is String -> parseLegacyStringMap(value)
        else -> null
    }

    /** Fallback format lama "[A, B]" — hanya untuk data tersimpan versi lama. */
    private fun parseLegacyStringList(savedValue: String): List<String> =
        savedValue.removeSurrounding("[", "]")
            .split(", ")
            .map { it.trim() }
            .filter { it.isNotEmpty() }
            .ifEmpty { null } ?: emptyList()

    /** Fallback format lama "{k=v, ...}" — hanya untuk data versi lama. */
    private fun parseLegacyStringMap(savedValue: String): Map<String, String> =
        savedValue.removeSurrounding("{", "}")
            .split(", ")
            .mapNotNull { entry ->
                val parts = entry.split("=", limit = 2)
                if (parts.size == 2) parts[0].trim() to parts[1].trim() else null
            }
            .toMap()

    /**
     * Restore a single answer into its view by traversing known view types.
     */
    private fun restoreAnswerInView(view: View, questionNum: String, savedValue: Any) {
        when (view) {
            is RadioGroup -> {
                val text = savedValue as? String ?: return
                restoreRadioGroup(view, text)
            }
            is EditText -> {
                val text = savedValue as? String ?: return
                if (view.text.isNullOrEmpty()) {
                    view.setText(text)
                }
            }
            is ViewGroup -> {
                // Only go one level deep for known containers
                val radioGroup = view.findViewById<RadioGroup>(R.id.rgChoices)
                if (radioGroup != null && radioGroup.checkedRadioButtonId == -1) {
                    (savedValue as? String)?.let { restoreRadioGroup(radioGroup, it) }
                }
                val checkboxLayout = view.findViewById<LinearLayout>(R.id.layoutCheckboxes)
                if (checkboxLayout != null) {
                    asStringList(savedValue)?.let { restoreCheckboxes(checkboxLayout, it) }
                }
                val editText = view.findViewById<EditText>(R.id.etShortAnswer)
                if (editText != null && editText.text.isNullOrEmpty()) {
                    (savedValue as? String)?.let { editText.setText(it) }
                }
                val matchingContainer = view.findViewById<LinearLayout>(R.id.layoutMatchingContainer)
                if (matchingContainer != null) {
                    asStringMap(savedValue)?.let { restoreMatching(matchingContainer, it) }
                }
            }
        }
    }

    private fun restoreRadioGroup(group: RadioGroup, savedValue: String) {
        if (group.checkedRadioButtonId != -1) return
        for (i in 0 until group.childCount) {
            val rb = group.getChildAt(i) as? RadioButton ?: continue
            if (rb.text.toString() == savedValue) {
                rb.isChecked = true
                break
            }
        }
    }

    private fun restoreCheckboxes(layout: LinearLayout, selectedValues: List<String>) {
        for (i in 0 until layout.childCount) {
            val cb = layout.getChildAt(i) as? CheckBox ?: continue
            cb.isChecked = selectedValues.contains(cb.text.toString())
        }
    }

    private fun restoreMatching(container: LinearLayout, pairs: Map<String, String>) {
        for (i in 0 until container.childCount) {
            val row = container.getChildAt(i) as? ViewGroup ?: continue
            val tvLeft = row.findViewById<TextView>(R.id.tvLeftItem) ?: continue
            val spinner = row.findViewById<Spinner>(R.id.spinnerRightItem) ?: continue

            val matchedValue = pairs[tvLeft.text.toString()]
            if (matchedValue != null) {
                val adapter = spinner.adapter as? ArrayAdapter<String> ?: continue
                for (pos in 0 until adapter.count) {
                    if (adapter.getItem(pos) == matchedValue) {
                        spinner.setSelection(pos)
                        break
                    }
                }
            }
        }
    }

    private fun hideAnswerOverlay() {
        binding.answerSheetToggle.visibility = View.GONE
        binding.answerSheetPanel.visibility = View.GONE
        binding.btnSubmitAnswers.visibility = View.GONE
    }

    /** Jumlah soal yang berhasil dirender pada build terakhir (nomor valid). */
    fun getQuestionCount(): Int = renderedCount

    // ---- Question type builders with view tagging (#6 fix) ----

    private fun addSingleChoiceQuestion(container: LinearLayout, number: Int, choices: List<String>) {
        val view = LayoutInflater.from(context).inflate(R.layout.item_question_choice, container, false)
        view.setTag(R.id.tag_question_number, number.toString())

        val label = view.findViewById<TextView>(R.id.tvQuestionLabel)
        val radioGroup = view.findViewById<RadioGroup>(R.id.rgChoices)
        val checkboxLayout = view.findViewById<LinearLayout>(R.id.layoutCheckboxes)
        checkboxLayout.visibility = View.GONE
        radioGroup.visibility = View.VISIBLE

        label.text = context.getString(R.string.answer_sheet_question_number, number)

        for (choice in choices) {
            val rb = RadioButton(context).apply {
                text = choice
                setTextColor(ContextCompat.getColor(context, R.color.on_surface))
                buttonTintList = ContextCompat.getColorStateList(context, R.color.primary)
                textSize = 14f
                setPadding(4, 0, 16, 0)
            }
            radioGroup.addView(rb)
        }

        radioGroup.setOnCheckedChangeListener { group, checkedId ->
            val rb = group.findViewById<RadioButton>(checkedId)
            if (rb != null) {
                onAnswerChanged?.invoke(number.toString(), rb.text.toString())
            }
        }

        container.addView(view)
    }

    private fun addTrueFalseQuestion(container: LinearLayout, number: Int) {
        val view = LayoutInflater.from(context).inflate(R.layout.item_question_choice, container, false)
        view.setTag(R.id.tag_question_number, number.toString())

        val label = view.findViewById<TextView>(R.id.tvQuestionLabel)
        val radioGroup = view.findViewById<RadioGroup>(R.id.rgChoices)
        val checkboxLayout = view.findViewById<LinearLayout>(R.id.layoutCheckboxes)
        checkboxLayout.visibility = View.GONE
        radioGroup.visibility = View.VISIBLE

        label.text = context.getString(R.string.answer_sheet_question_number, number) + " " +
                context.getString(R.string.answer_sheet_true_false_suffix)

        for (choice in listOf("TRUE", "FALSE")) {
            val rb = RadioButton(context).apply {
                text = choice
                setTextColor(ContextCompat.getColor(context, R.color.on_surface))
                buttonTintList = ContextCompat.getColorStateList(context, R.color.primary)
                textSize = 14f
                setPadding(4, 0, 16, 0)
            }
            radioGroup.addView(rb)
        }

        radioGroup.setOnCheckedChangeListener { group, checkedId ->
            val rb = group.findViewById<RadioButton>(checkedId)
            if (rb != null) {
                onAnswerChanged?.invoke(number.toString(), rb.text.toString())
            }
        }

        container.addView(view)
    }

    private fun addMultipleChoiceQuestion(container: LinearLayout, number: Int, choices: List<String>) {
        val view = LayoutInflater.from(context).inflate(R.layout.item_question_choice, container, false)
        view.setTag(R.id.tag_question_number, number.toString())

        val label = view.findViewById<TextView>(R.id.tvQuestionLabel)
        val radioGroup = view.findViewById<RadioGroup>(R.id.rgChoices)
        val checkboxLayout = view.findViewById<LinearLayout>(R.id.layoutCheckboxes)
        radioGroup.visibility = View.GONE
        checkboxLayout.visibility = View.VISIBLE

        label.text = context.getString(R.string.answer_sheet_question_number, number) + " " +
                context.getString(R.string.answer_sheet_multiple_suffix)

        for (choice in choices) {
            val cb = CheckBox(context).apply {
                text = choice
                setTextColor(ContextCompat.getColor(context, R.color.on_surface))
                buttonTintList = ContextCompat.getColorStateList(context, R.color.primary)
                textSize = 14f
                setPadding(4, 0, 16, 0)
            }

            cb.setOnCheckedChangeListener { _, _ ->
                val selected = mutableListOf<String>()
                for (i in 0 until checkboxLayout.childCount) {
                    val child = checkboxLayout.getChildAt(i) as? CheckBox
                    if (child?.isChecked == true) {
                        selected.add(child.text.toString())
                    }
                }
                if (selected.isEmpty()) {
                    onAnswerRemoved?.invoke(number.toString())
                } else {
                    onAnswerChanged?.invoke(number.toString(), selected)
                }
            }

            checkboxLayout.addView(cb)
        }

        container.addView(view)
    }

    private fun addMatchingQuestion(container: LinearLayout, number: Int, leftItems: List<String>, rightItems: List<String>) {
        val view = LayoutInflater.from(context).inflate(R.layout.item_question_matching, container, false)
        view.setTag(R.id.tag_question_number, number.toString())

        val label = view.findViewById<TextView>(R.id.tvMatchingLabel)
        val matchingContainer = view.findViewById<LinearLayout>(R.id.layoutMatchingContainer)

        label.text = context.getString(R.string.answer_sheet_question_number, number) + " " +
                context.getString(R.string.answer_sheet_matching_suffix)

        val matchingAnswers = mutableMapOf<String, String>()

        for (leftItem in leftItems) {
            val rowView = LayoutInflater.from(context).inflate(R.layout.item_matching_row, matchingContainer, false)
            val tvLeft = rowView.findViewById<TextView>(R.id.tvLeftItem)
            val spinner = rowView.findViewById<Spinner>(R.id.spinnerRightItem)

            tvLeft.text = leftItem

            val spinnerItems = mutableListOf(context.getString(R.string.spinner_placeholder))
            spinnerItems.addAll(rightItems)

            val adapter = ArrayAdapter(context, android.R.layout.simple_spinner_item, spinnerItems)
            adapter.setDropDownViewResource(android.R.layout.simple_spinner_dropdown_item)
            spinner.adapter = adapter

            // Track spinner popup for focus-loss detection
            spinner.setOnTouchListener { _, event ->
                if (event.action == android.view.MotionEvent.ACTION_UP) {
                    onSpinnerPopupChanged?.invoke(1)
                }
                false
            }

            // Callback onItemSelected terpicu SEKALI secara otomatis saat
            // listener dipasang (posisi default 0 = "-- Pilih --"). Tanpa guard
            // ini, build() menghapus jawaban matching yang sudah ada di memori
            // (onAnswerRemoved) — rapuh karena bergantung pada urutan build →
            // restore. Guard memakai callback pertama itu sebagai no-op dan
            // tidak menyentuh jawaban/popup counter; callback berikutnya
            // (termasuk dari setSelection saat restore) tetap diproses.
            val initialSelectionGuard = SpinnerInitialSelectionGuard()
            spinner.onItemSelectedListener = object : AdapterView.OnItemSelectedListener {
                override fun onItemSelected(parent: AdapterView<*>?, v: View?, position: Int, id: Long) {
                    if (initialSelectionGuard.isInitialSelection()) return
                    onSpinnerPopupChanged?.invoke(-1)
                    if (v is TextView) {
                        v.setTextColor(panelTextColor ?: ContextCompat.getColor(context, R.color.on_surface))
                    }
                    if (position > 0) {
                        matchingAnswers[leftItem] = rightItems[position - 1]
                    } else {
                        matchingAnswers.remove(leftItem)
                    }
                    if (matchingAnswers.isEmpty()) {
                        onAnswerRemoved?.invoke(number.toString())
                    } else {
                        onAnswerChanged?.invoke(number.toString(), HashMap(matchingAnswers))
                    }
                }
                override fun onNothingSelected(parent: AdapterView<*>?) {
                    // Quirk Android: callback ini nyaris tidak pernah dipanggil
                    // (dropdown ditutup tanpa memilih biasanya tidak memicunya).
                    // Konsekuensi: popup counter bisa bocor +1 — tertutup oleh
                    // reset activePopupCount saat fokus kembali (SecurityEnforcer).
                    onSpinnerPopupChanged?.invoke(-1)
                }
            }

            matchingContainer.addView(rowView)
        }

        container.addView(view)
    }

    private fun addShortAnswerQuestion(container: LinearLayout, number: Int) {
        val view = LayoutInflater.from(context).inflate(R.layout.item_question_short_answer, container, false)
        view.setTag(R.id.tag_question_number, number.toString())

        val label = view.findViewById<TextView>(R.id.tvQuestionLabel)
        val editText = view.findViewById<EditText>(R.id.etShortAnswer)

        label.text = context.getString(R.string.answer_sheet_question_number, number) + " " +
                context.getString(R.string.answer_sheet_short_answer_suffix)

        // Restore answer if already saved
        val currentAns = getAnswer?.invoke(number.toString()) as? String
        if (currentAns != null) {
            editText.setText(currentAns)
        }

        editText.filters = arrayOf(android.text.InputFilter.LengthFilter(500))

        editText.addTextChangedListener(object : android.text.TextWatcher {
            override fun beforeTextChanged(s: CharSequence?, start: Int, count: Int, after: Int) {}
            override fun onTextChanged(s: CharSequence?, start: Int, before: Int, count: Int) {}
            override fun afterTextChanged(s: android.text.Editable?) {
                val ans = s?.toString()?.trim() ?: ""
                if (ans.isNotEmpty()) {
                    onAnswerChanged?.invoke(number.toString(), ans)
                } else {
                    onAnswerRemoved?.invoke(number.toString())
                }
            }
        })

        container.addView(view)
    }

    fun applyDynamicTextColors() {
        val textColor = panelTextColor ?: return
        val container = binding.answerListContainer
        applyColorToViewHierarchy(container, textColor, isPanelColorDark)
    }

    private fun applyColorToViewHierarchy(view: View, textColor: Int, isDark: Boolean) {
        if (view is ViewGroup) {
            for (i in 0 until view.childCount) {
                applyColorToViewHierarchy(view.getChildAt(i), textColor, isDark)
            }
        }
        if (view is TextView) {
            view.setTextColor(textColor)
            if (view is EditText) {
                view.setHintTextColor(if (isDark) androidx.core.content.ContextCompat.getColor(context, R.color.hint_on_dark_panel) else androidx.core.content.ContextCompat.getColor(context, R.color.hint_on_light_panel))
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

    companion object {
        // R.id.tag_question_number is defined in ids.xml for view tagging (#6 fix)
    }
}
