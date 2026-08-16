package com.examvan.app.helper

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Mengunci guard seleksi-awal spinner di AnswerSheetBuilder (addMatchingQuestion).
 *
 * Android memicu `onItemSelected` SEKALI secara otomatis saat listener
 * dipasang (posisi default 0 = "-- Pilih --"). Guard harus menandai panggilan
 * pertama sebagai no-op — tanpa itu, build() memperlakukan posisi default
 * sebagai pilihan siswa: menghapus jawaban matching yang sudah dipulihkan
 * dari penyimpanan (onAnswerRemoved) dan menyentuh popup counter.
 *
 * Penting: hanya pemicu otomatis PERTAMA yang di-ignore. Callback dari
 * `spinner.setSelection(pos)` saat restore adalah pemicu KEDUA dan harus
 * tetap diproses — itulah yang mengisi `matchingAnswers` dengan jawaban
 * tersimpan (lihat SpinnerInitialSelectionGuard).
 *
 *   ./gradlew :app:testStudentDebugUnitTest
 */
class SpinnerInitialSelectionGuardTest {

    @Test
    fun firstCallback_isInitialSelection() {
        val guard = SpinnerInitialSelectionGuard()
        assertTrue(
            "panggilan pertama = pemicu otomatis, harus di-ignore",
            guard.isInitialSelection()
        )
    }

    @Test
    fun subsequentCallbacks_areRealSelections() {
        val guard = SpinnerInitialSelectionGuard()
        assertTrue(guard.isInitialSelection()) // pemicu otomatis pertama
        assertFalse("pilihan siswa berikutnya harus diproses", guard.isInitialSelection())
        assertFalse(guard.isInitialSelection())
        assertFalse(guard.isInitialSelection())
    }

    @Test
    fun eachSpinner_hasIndependentGuard() {
        // Satu instance per spinner/baris matching — pemicu otomatis baris A
        // tidak boleh menelan pilihan baris B.
        val rowA = SpinnerInitialSelectionGuard()
        val rowB = SpinnerInitialSelectionGuard()

        assertTrue(rowA.isInitialSelection())
        assertTrue("baris lain punya pemicu otomatisnya sendiri", rowB.isInitialSelection())

        assertFalse(rowA.isInitialSelection())
        assertFalse(rowB.isInitialSelection())
    }

    @Test
    fun restoreSetSelection_isNotSwallowed() {
        // Urutan nyata: build() → pemicu otomatis di-ignore → restore memanggil
        // spinner.setSelection(pos) yang memicu callback KEDUA. Callback itu
        // BUKAN pemicu otomatis → harus diproses (mengisi matchingAnswers +
        // menyimpan jawaban). Guard yang menelan callback kedua akan kehilangan
        // jawaban matching yang dipulihkan.
        val guard = SpinnerInitialSelectionGuard()

        // Callback 1: pemicu otomatis saat listener dipasang (build).
        assertTrue(guard.isInitialSelection())

        // Callback 2: setSelection saat restore (posisi jawaban tersimpan).
        assertFalse(
            "callback dari restore harus diproses, bukan di-ignore",
            guard.isInitialSelection()
        )
    }

    @Test
    fun rebuild_getsFreshGuard() {
        // build() berikutnya menghapus semua view dan membuat ulang (removeAllViews
        // → inflate baru) — setiap baris mendapat guard BARU, tanpa sisa state
        // dari build sebelumnya (pemicu otomatis muncul lagi dan di-ignore lagi).
        val firstBuild = SpinnerInitialSelectionGuard()
        assertTrue(firstBuild.isInitialSelection())
        assertFalse(firstBuild.isInitialSelection())

        val secondBuild = SpinnerInitialSelectionGuard()
        assertTrue("build baru = pemicu otomatis baru", secondBuild.isInitialSelection())
        assertFalse(secondBuild.isInitialSelection())
    }
}
