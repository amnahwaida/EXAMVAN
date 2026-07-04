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
     */
    fun build(questions: List<Map<String, Any>>) {
        this.questions = questions
        val container = binding.answerListContainer
        container.removeAllViews()

        if (questions.isEmpty()) {
            hideAnswerOverlay()
            return
        }

        for (q in questions) {
            val number = (q["number"] as? Double)?.toInt() ?: continue
            val type = q["type"] as? String ?: "single_choice"

            when (type) {
                "single_choice" -> addSingleChoiceQuestion(container, number, q)
                "true_false" -> addTrueFalseQuestion(container, number)
                "multiple_choice" -> addMultipleChoiceQuestion(container, number, q)
                "matching" -> addMatchingQuestion(container, number, q)
                "short_answer" -> addShortAnswerQuestion(container, number)
            }
        }

        applyDynamicTextColors()
    }

    /**
     * Restore answers from saved data into the UI.
     * Uses view tags to match question numbers efficiently.
     */
    fun restoreFromSaved(savedAnswers: Map<String, String>) {
        for (i in 0 until binding.answerListContainer.childCount) {
            val view = binding.answerListContainer.getChildAt(i)
            val tag = view.getTag(R.id.tag_question_number)
            val questionNum = tag?.toString() ?: continue
            val savedValue = savedAnswers[questionNum] ?: continue

            restoreAnswerInView(view, questionNum, savedValue)
        }
    }

    /**
     * Restore a single answer into its view by traversing known view types.
     */
    private fun restoreAnswerInView(view: View, questionNum: String, savedValue: String) {
        when (view) {
            is RadioGroup -> {
                restoreRadioGroup(view, savedValue)
            }
            is EditText -> {
                if (view.text.isNullOrEmpty()) {
                    view.setText(savedValue)
                }
            }
            is ViewGroup -> {
                // Only go one level deep for known containers
                val radioGroup = view.findViewById<RadioGroup>(R.id.rgChoices)
                if (radioGroup != null && radioGroup.checkedRadioButtonId == -1) {
                    restoreRadioGroup(radioGroup, savedValue)
                }
                val checkboxLayout = view.findViewById<LinearLayout>(R.id.layoutCheckboxes)
                if (checkboxLayout != null) {
                    restoreCheckboxes(checkboxLayout, savedValue)
                }
                val editText = view.findViewById<EditText>(R.id.etShortAnswer)
                if (editText != null && editText.text.isNullOrEmpty()) {
                    editText.setText(savedValue)
                }
                val matchingContainer = view.findViewById<LinearLayout>(R.id.layoutMatchingContainer)
                if (matchingContainer != null) {
                    restoreMatching(matchingContainer, savedValue)
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

    private fun restoreCheckboxes(layout: LinearLayout, savedValue: String) {
        // Parse saved value as a list string "[A, B, C]"
        val selectedValues = savedValue
            .removeSurrounding("[", "]")
            .split(", ")
            .map { it.trim() }
            .filter { it.isNotEmpty() }

        for (i in 0 until layout.childCount) {
            val cb = layout.getChildAt(i) as? CheckBox ?: continue
            cb.isChecked = selectedValues.contains(cb.text.toString())
        }
    }

    private fun restoreMatching(container: LinearLayout, savedValue: String) {
        // Parse saved value as a map string "{key=value, ...}"
        val pairs = savedValue
            .removeSurrounding("{", "}")
            .split(", ")
            .map { it.split("=").take(2) }
            .filter { it.size == 2 }
            .associate { it[0].trim() to it[1].trim() }

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

    fun getQuestionCount(): Int = questions.size

    // ---- Question type builders with view tagging (#6 fix) ----

    @Suppress("UNCHECKED_CAST")
    private fun addSingleChoiceQuestion(container: LinearLayout, number: Int, q: Map<String, Any>) {
        val view = LayoutInflater.from(context).inflate(R.layout.item_question_choice, container, false)
        view.setTag(R.id.tag_question_number, number.toString())

        val label = view.findViewById<TextView>(R.id.tvQuestionLabel)
        val radioGroup = view.findViewById<RadioGroup>(R.id.rgChoices)
        val checkboxLayout = view.findViewById<LinearLayout>(R.id.layoutCheckboxes)
        checkboxLayout.visibility = View.GONE
        radioGroup.visibility = View.VISIBLE

        label.text = "Soal $number"

        val choices = (q["choices"] as? List<*>)?.filterIsInstance<String>() ?: listOf("A", "B", "C", "D", "E")

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

        label.text = "Soal $number (Benar/Salah)"

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

    @Suppress("UNCHECKED_CAST")
    private fun addMultipleChoiceQuestion(container: LinearLayout, number: Int, q: Map<String, Any>) {
        val view = LayoutInflater.from(context).inflate(R.layout.item_question_choice, container, false)
        view.setTag(R.id.tag_question_number, number.toString())

        val label = view.findViewById<TextView>(R.id.tvQuestionLabel)
        val radioGroup = view.findViewById<RadioGroup>(R.id.rgChoices)
        val checkboxLayout = view.findViewById<LinearLayout>(R.id.layoutCheckboxes)
        radioGroup.visibility = View.GONE
        checkboxLayout.visibility = View.VISIBLE

        label.text = "Soal $number (Pilih beberapa)"

        val choices = (q["choices"] as? List<*>)?.filterIsInstance<String>() ?: listOf("A", "B", "C", "D", "E")

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

    @Suppress("UNCHECKED_CAST")
    private fun addMatchingQuestion(container: LinearLayout, number: Int, q: Map<String, Any>) {
        val view = LayoutInflater.from(context).inflate(R.layout.item_question_matching, container, false)
        view.setTag(R.id.tag_question_number, number.toString())

        val label = view.findViewById<TextView>(R.id.tvMatchingLabel)
        val matchingContainer = view.findViewById<LinearLayout>(R.id.layoutMatchingContainer)

        label.text = "Soal $number (Menjodohkan)"

        val leftItems = (q["left_items"] as? List<*>)?.filterIsInstance<String>() ?: listOf("1", "2", "3")
        val rightItems = (q["right_items"] as? List<*>)?.filterIsInstance<String>() ?: listOf("A", "B", "C")

        val matchingAnswers = mutableMapOf<String, String>()

        for (leftItem in leftItems) {
            val rowView = LayoutInflater.from(context).inflate(R.layout.item_matching_row, matchingContainer, false)
            val tvLeft = rowView.findViewById<TextView>(R.id.tvLeftItem)
            val spinner = rowView.findViewById<Spinner>(R.id.spinnerRightItem)

            tvLeft.text = leftItem

            val spinnerItems = mutableListOf("-- Pilih --")
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

            spinner.onItemSelectedListener = object : AdapterView.OnItemSelectedListener {
                override fun onItemSelected(parent: AdapterView<*>?, v: View?, position: Int, id: Long) {
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

        label.text = "Soal $number (Isian Singkat)"

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
                view.setHintTextColor(if (isDark) android.graphics.Color.parseColor("#B0FFFFFF") else android.graphics.Color.parseColor("#80000000"))
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
