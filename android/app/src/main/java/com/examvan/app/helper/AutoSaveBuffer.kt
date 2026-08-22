package com.examvan.app.helper

/**
 * Buffer jawaban auto-save ber-semantik debounce (fix temuan review low-mode
 * ronde 2 #1).
 *
 * LATAR: triggerAutoSave ber-debounce 500ms — siswa low yang mengisi satu
 * jawaban lalu langsung keluar dalam jendela itu kehilangan tulisan tsb
 * (re-entry memulihkan versi lama). Dengan buffer, nilai terakhir SELALU
 * tersimpan di memory dan bisa di-flush sinkron saat keluar
 * (SubmissionManager.flushPendingAutoSave) sebelum activity finish.
 *
 * Thread-safety: synchronized — update dari UI thread, drain bisa dari
 * mana saja.
 */
class AutoSaveBuffer<T> {

    private var latest: T? = null

    /** Catat nilai terbaru; menimpa yang belum ter-drain (semantik debounce). */
    @Synchronized
    fun update(value: T) {
        latest = value
    }

    /** Ambil dan kosongkan nilai terakhir. Panggilan kedua = null (idempoten). */
    @Synchronized
    fun drain(): T? {
        val value = latest
        latest = null
        return value
    }

    @Synchronized
    fun hasPending(): Boolean = latest != null
}
