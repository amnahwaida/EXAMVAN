package com.examvan.app.helper

/**
 * Guard untuk callback `onItemSelected` spinner Android — murni, bisa diuji JVM.
 *
 * Saat `onItemSelectedListener` dipasang, Android memicu `onItemSelected`
 * SEKALI secara otomatis dengan posisi saat ini (default 0 = "-- Pilih --").
 * Tanpa guard, pemasangan listener saat build lembar jawaban langsung
 * memperlakukan posisi default sebagai pilihan siswa: menghapus jawaban
 * matching yang sudah ada di memori (`onAnswerRemoved`) dan menyentuh popup
 * counter — rapuh karena bergantung pada urutan build → restore.
 *
 * Satu instance per spinner (per baris matching). Panggilan pertama
 * `isInitialSelection()` adalah pemicu otomatis (harus di-ignore); panggilan
 * berikutnya — termasuk yang berasal dari `setSelection` saat restore —
 * adalah seleksi sungguhan dan harus diproses.
 */
internal class SpinnerInitialSelectionGuard {

    private var isFirstCallback = true

    /**
     * True bila callback ini adalah pemicu otomatis pertama (harus di-ignore);
     * false untuk semua callback berikutnya.
     */
    fun isInitialSelection(): Boolean {
        if (!isFirstCallback) return false
        isFirstCallback = false
        return true
    }
}
