package com.examvan.app

import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

/**
 * Guard i18n contentDescription (backlog aksesibilitas: "contentDescription
 * i18n — sebagian besar hardcoded Indonesia di XML").
 *
 * Kontrak: nilai contentDescription di layout WAJIB berupa referensi
 * resource (@string/...) agar ikut diterjemahkan — teks literal dilarang.
 *
 *   ./gradlew :app:testStudentDebugUnitTest
 */
class ContentDescriptionI18nGuardTest {

    private fun layoutDir(): File {
        val cwd = File(System.getProperty("user.dir") ?: ".")
        val module = when {
            File(cwd, "src/main/res").isDirectory -> cwd
            File(cwd, "app/src/main/res").isDirectory -> cwd.resolve("app")
            else -> error("Direktori sumber Android tidak ditemukan")
        }
        return module.resolve("src/main/res/layout")
    }

    @Test
    fun contentDescriptions_useResourceReferences() {
        val offenders = mutableListOf<String>()
        layoutDir().listFiles { f -> f.extension == "xml" }?.forEach { file ->
            file.readLines().forEachIndexed { idx, line ->
                val stripped = line.substringBefore("<!--").trim()
                val m = Regex("android:contentDescription=\"([^\"]*)\"").find(stripped)
                val value = m?.groupValues?.get(1)
                if (value != null && !value.startsWith("@")) {
                    offenders.add("${file.name}:${idx + 1} \"$value\"")
                }
            }
        }

        assertTrue(
            "contentDescription harus memakai @string/... (ditemukan literal):\n" +
                offenders.joinToString("\n"),
            offenders.isEmpty()
        )
    }
}
