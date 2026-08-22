package com.examvan.app.helper

import com.examvan.app.helper.ExamDeadline.TimerUrgency

/**
 * Kebijakan umpan balik haptic momen kritis (backlog aksesibilitas).
 *
 * Getar HANYA pada transisi masuk CRITICAL (<= 5 menit) — bukan tiap tick,
 * agar tidak bergetar tiap detik selama 5 menit terakhir. Transisi ke
 * WARNING/NORMAL tidak menggetarkan.
 */
internal object HapticPolicy {

    /** Durasi getar peringatan — pendek, bukan pola panjang. */
    const val BUZZ_DURATION_MS = 200L

    fun shouldBuzz(previous: TimerUrgency?, current: TimerUrgency): Boolean =
        current == TimerUrgency.CRITICAL && previous != TimerUrgency.CRITICAL
}
