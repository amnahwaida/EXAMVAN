package com.examvan.app

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File
import javax.xml.parsers.DocumentBuilderFactory

/**
 * Guard kelengkapan terjemahan (temuan review low-mode ronde 5 via Android
 * Lint: "MissingTranslation — 23 string tidak diterjemahkan di values-en,
 * membuat :app:lint gagal dan berisiko UI berbahasa campur di perangkat
 * berbahasa Inggris").
 *
 * Kontrak: SETIUP <string name="..."> di res/values/strings.xml HARUS punya
 * padanan di res/values-en/strings.xml. Menambah string baru tanpa
 * terjemahan akan meredupkan build.
 *
 *   ./gradlew :app:testStudentDebugUnitTest
 */
class TranslationCompletenessTest {

    private fun resolveResDir(): File {
        val cwd = File(System.getProperty("user.dir") ?: ".")
        val module = when {
            File(cwd, "src/main/res").isDirectory -> cwd
            File(cwd, "app/src/main/res").isDirectory -> cwd.resolve("app")
            else -> error("Direktori sumber Android tidak ditemukan (cwd=${cwd.absolutePath})")
        }
        return module.resolve("src/main/res")
    }

    private fun stringNames(file: File): Set<String> {
        if (!file.exists()) return emptySet()
        val doc = DocumentBuilderFactory.newInstance().newDocumentBuilder().parse(file)
        val nodes = doc.getElementsByTagName("string")
        val names = mutableSetOf<String>()
        for (i in 0 until nodes.length) {
            val name = nodes.item(i).attributes.getNamedItem("name")?.nodeValue ?: continue
            names.add(name)
        }
        return names
    }

    @Test
    fun everyDefaultString_hasEnglishTranslation() {
        val resDir = resolveResDir()
        val defaultNames = stringNames(resDir.resolve("values/strings.xml"))
        val englishNames = stringNames(resDir.resolve("values-en/strings.xml"))

        assertTrue(
            "res/values/strings.xml tidak boleh kosong",
            defaultNames.isNotEmpty()
        )

        val missing = (defaultNames - englishNames).sorted()
        assertEquals(
            "String berikut wajib punya padanan di res/values-en/strings.xml:\n" +
                missing.joinToString("\n") { "  - $it" },
            emptyList<String>(),
            missing
        )
    }

    @Test
    fun englishStrings_doNotIntroduceUnknownKeys() {
        // Arah sebaliknya: key di values-en yang tidak ada di default adalah
        // sisa penghapusan — bersihkan agar tidak menyesatkan.
        val resDir = resolveResDir()
        val defaultNames = stringNames(resDir.resolve("values/strings.xml"))
        val englishNames = stringNames(resDir.resolve("values-en/strings.xml"))

        val orphans = (englishNames - defaultNames).sorted()
        assertTrue(
            "Key Inggris tanpa padanan default (sisa penghapusan?):\n" +
                orphans.joinToString("\n") { "  - $it" },
            orphans.isEmpty()
        )
    }
}
