package com.examvan.app.adapter

import android.view.LayoutInflater
import android.view.ViewGroup
import androidx.recyclerview.widget.AsyncListDiffer
import androidx.recyclerview.widget.DiffUtil
import androidx.recyclerview.widget.RecyclerView
import com.examvan.app.databinding.ItemExamBinding
import com.examvan.app.model.Exam

/**
 * RecyclerView adapter for displaying exam list items.
 * Uses AsyncListDiffer with DiffUtil under the hood so only changed
 * items are re-bound, avoiding the full list flash of notifyDataSetChanged.
 */
class ExamAdapter(
    private val onItemClick: (Exam) -> Unit
) : RecyclerView.Adapter<ExamAdapter.ExamViewHolder>() {

    private val differ = AsyncListDiffer(this, DIFF_CALLBACK)

    /**
     * Submit a new list of exams. DiffUtil computes the minimal
     * set of insert/update/move/remove operations on a background thread.
     */
    fun submitList(list: List<Exam>) {
        differ.submitList(list)
    }

    override fun onCreateViewHolder(parent: ViewGroup, viewType: Int): ExamViewHolder {
        val binding = ItemExamBinding.inflate(
            LayoutInflater.from(parent.context), parent, false
        )
        return ExamViewHolder(binding)
    }

    override fun onBindViewHolder(holder: ExamViewHolder, position: Int) {
        holder.bind(differ.currentList[position])
    }

    override fun getItemCount(): Int = differ.currentList.size

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

    companion object {
        private val DIFF_CALLBACK = object : DiffUtil.ItemCallback<Exam>() {
            override fun areItemsTheSame(oldItem: Exam, newItem: Exam): Boolean {
                return oldItem.id == newItem.id
            }

            override fun areContentsTheSame(oldItem: Exam, newItem: Exam): Boolean {
                return oldItem == newItem
            }
        }
    }
}
