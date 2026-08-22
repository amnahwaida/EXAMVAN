package com.examvan.app.helper

import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

/**
 * Guard kontrak AnswerSheetBuilder pasca-refactor RecyclerView (fix review
 * RecyclerView ronde 2 #1: "build() dengan hasil kosong tidak membersihkan
 * adapter — view basi tetap tertahan di belakang overlay").
 *
 * Kontrak urutan di build(): adapter.submit(items) WAJIB terjadi SEBELUM
 * percabangan items.isEmpty() — pengosongan daftar tidak boleh terlewati
 * pada jalur manapun.
 *
 *   ./gradlew :app:testStudentDebugUnitTest
 */
class AnswerSheetBuilderGuardTest {

    private fun sourceFile(): File {
        val cwd = File(System.getProperty("user.dir") ?: ".")
        val module = when {
            File(cwd, "src/main/java").isDirectory -> cwd
            File(cwd, "app/src/main/java").isDirectory -> cwd.resolve("app")
            else -> error("Direktori sumber Android tidak ditemukan")
        }
        return module.resolve(
            "src/main/java/com/examvan/app/helper/AnswerSheetBuilder.kt"
        )
    }

    @Test
    fun submitHappensBeforeEmptyCheck() {
        val src = sourceFile().readText()

        val submitIdx = src.indexOf("adapter.submit(items)")
        val emptyCheckIdx = src.indexOf("items.isEmpty()")
        val hideOverlayIdx = src.indexOf("hideAnswerOverlay()")

        assertTrue(
            "build() wajib memanggil adapter.submit(items) — ditemukan di " +
                "index=$submitIdx",
            submitIdx >= 0
        )
        assertTrue(
            "build() wajib memiliki jalur items.isEmpty()",
            emptyCheckIdx >= 0 && hideOverlayIdx > emptyCheckIdx
        )
        assertTrue(
            "adapter.submit(items) harus TERJADI SEBELUM cek items.isEmpty() " +
                "(fix: daftar kosong juga dikirim ke adapter agar view basi " +
                "terbersihkan)",
            submitIdx in 0 until emptyCheckIdx
        )
    }
}
