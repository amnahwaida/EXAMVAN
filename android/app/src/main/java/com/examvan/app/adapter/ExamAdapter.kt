package com.examvan.app.adapter

import android.view.LayoutInflater
import android.view.ViewGroup
import androidx.recyclerview.widget.RecyclerView
import com.examvan.app.databinding.ItemExamBinding
import com.examvan.app.model.Exam

/**
 * RecyclerView adapter for displaying exam list items.
 */
class ExamAdapter(
    private var exams: List<Exam> = emptyList(),
    private val onItemClick: (Exam) -> Unit
) : RecyclerView.Adapter<ExamAdapter.ExamViewHolder>() {

    fun updateData(newExams: List<Exam>) {
        exams = newExams
        notifyDataSetChanged()
    }

    override fun onCreateViewHolder(parent: ViewGroup, viewType: Int): ExamViewHolder {
        val binding = ItemExamBinding.inflate(
            LayoutInflater.from(parent.context), parent, false
        )
        return ExamViewHolder(binding)
    }

    override fun onBindViewHolder(holder: ExamViewHolder, position: Int) {
        holder.bind(exams[position])
    }

    override fun getItemCount(): Int = exams.size

    inner class ExamViewHolder(
        private val binding: ItemExamBinding
    ) : RecyclerView.ViewHolder(binding.root) {

        fun bind(exam: Exam) {
            binding.tvExamName.text = exam.name
            binding.tvExamSize.text = "${exam.size_mb} MB"
            binding.tvExamStatus.text = exam.status

            itemView.setOnClickListener { onItemClick(exam) }
        }
    }
}
