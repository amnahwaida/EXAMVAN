package com.examvan.app

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File
import kotlin.io.path.Path
import kotlin.io.path.readText

/**
 * Guard RTL (backlog: "RTL penuh").
 *
 * Kontrak:
 *  1. Layout XML tidak boleh memakai atribut sisi absolut (left/right) —
 *     wajib start/end agar ter-cerminkan di locale RTL.
 *  2. Kode Kotlin tidak boleh memanggil setPadding() dengan left != right
 *     (asimetri horizontal) — wajib setPaddingRelative.
 *
 *   ./gradlew :app:testStudentDebugUnitTest
 */
class RtlLayoutGuardTest {

    private fun moduleDir(): File {
        val cwd = File(System.getProperty("user.dir") ?: ".")
        return when {
            File(cwd, "src/main").isDirectory -> cwd.resolve("src/main")
            File(cwd, "app/src/main").isDirectory -> cwd.resolve("app/src/main")
            else -> error("Direktori sumber Android tidak ditemukan")
        }
    }

    @Test
    fun layouts_noAbsoluteLeftRightAttributes() {
        val layoutDir = moduleDir().resolve("res/layout")
        val offenders = mutableListOf<String>()
        layoutDir.listFiles { f -> f.extension == "xml" }?.forEach { file ->
            file.readLines().forEachIndexed { idx, line ->
                for (attr in listOf(
                    "android:paddingLeft", "android:paddingRight",
                    "android:layout_marginLeft", "android:layout_marginRight"
                )) {
                    if (line.contains(attr)) offenders.add("${file.name}:${idx + 1} $attr")
                }
            }
        }

        assertTrue(
            "Atribut sisi absolut dilarang — gunakan start/end:\n" +
                offenders.joinToString("\n"),
            offenders.isEmpty()
        )
    }

    @Test
    fun kotlinSources_noAsymmetricLiteralSetPadding() {
        val javaDir = moduleDir().resolve("java")
        val offenders = mutableListOf<String>()
        javaDir.walkTopDown().filter { it.extension == "kt" }.forEach { file ->
            file.readLines().forEachIndexed { idx, line ->
                val m = Regex("""setPadding\((\d+),\s*(\d+),\s*(\d+),\s*(\d+)\)""").find(line)
                if (m != null) {
                    val (l, _, r) = m.destructured
                    @Suppress("UNUSED_EXPRESSION")
                    if (l != r) offenders.add("${file.name}:${idx + 1} ${line.trim()}")
                }
            }
        }

        assertTrue(
            "setPadding literal dengan left != right dilarang — gunakan " +
                "setPaddingRelative(start, top, end, bottom):\n" +
                offenders.joinToString("\n"),
            offenders.isEmpty()
        )
    }
}
