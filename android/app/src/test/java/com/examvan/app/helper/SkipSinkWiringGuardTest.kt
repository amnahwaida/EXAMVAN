package com.examvan.app.helper

import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

/**
 * Guard pemasangan sink audit skip soal (fix review RecyclerView ronde 3
 * #1: "skipSink hanya dipasang oleh TEST — di production tetap null sehingga
 * alasan skip soal tetap senyap di logcat nyata").
 *
 * Kontrak: AnswerSheetBuilder wajib memasang sink PERMANEN (Log.w) saat
 * dibuat — lambda-nya statis tanpa referensi activity, aman dibiarkan
 * terpasang sepanjang proses.
 *
 *   ./gradlew :app:testStudentDebugUnitTest
 */
class SkipSinkWiringGuardTest {

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
    fun builder_installsPermanentSkipSink() {
        val src = sourceFile().readText()
        assertTrue(
            "AnswerSheetBuilder wajib memasang AnswerSheetPlanner.skipSink " +
                "(Log.w) saat inisialisasi — tanpa ini alasan skip soal tetap " +
                "senyap di production",
            src.contains("AnswerSheetPlanner.skipSink")
        )
    }
}
