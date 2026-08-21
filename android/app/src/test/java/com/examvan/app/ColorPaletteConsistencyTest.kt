package com.examvan.app

import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Test
import java.io.File

/**
 * Mengunci kebijakan konsistensi warna (palette) untuk light & dark mode.
 *
 * Tujuan:
 *  1. Semua nama warna di `values/colors.xml` HARUS juga ada di
 *     `values-night/colors.xml` (dan sebaliknya) — menjamin tidak ada
 *     warna yang hilang di salah satu mode.
 *  2. Layout XML TIDAK BOLEH mengandung hex color hardcoded (#RRGGBB /
 *     #AARRGGBB) — harus pakai `@color/` resource agar responsif
 *     terhadap perubahan mode.
 *  3. Drawable XML TIDAK BOLEH mengandung hex color hardcoded
 *     — kecuali atribut `android:color` pada elemen `<ripple>` yang
 *     merupakan warna overlay transparan dan aman di kedua mode.
 *  4. File Kotlin/Java TIDAK BOLEH menggunakan `Color.parseColor("#…")`
 *     — kecuali dalam `applyPanelColor()` yang menerima warna dinamis
 *     dari server.
 *
 * Test ini berjalan sebagai JVM unit test (source set `test/`), tanpa
 * memerlukan emulator atau device. Ia membaca file sumber langsung dari
 * disk menggunakan konvensi direktori Gradle.
 */
class ColorPaletteConsistencyTest {

    // ────────────────────────────────────────────────────────────────
    // Test 1: Semua nama warna harus ada di kedua mode
    // ────────────────────────────────────────────────────────────────

    @Test
    fun lightAndDarkColorsMustHaveSameNames() {
        val moduleDir = resolveModuleDir()
        val lightColors = parseColorNames(moduleDir.resolve("src/main/res/values/colors.xml"))
        val darkColors = parseColorNames(moduleDir.resolve("src/main/res/values-night/colors.xml"))

        assertTrue(
            "colors.xml (light) tidak ditemukan atau kosong",
            lightColors.isNotEmpty()
        )
        assertTrue(
            "colors.xml (night) tidak ditemukan atau kosong",
            darkColors.isNotEmpty()
        )

        val onlyInLight = lightColors - darkColors
        val onlyInDark = darkColors - lightColors

        val errors = StringBuilder()
        if (onlyInLight.isNotEmpty()) {
            errors.appendLine("Warna hanya ada di LIGHT (values/colors.xml), hilang di DARK:")
            onlyInLight.sorted().forEach { errors.appendLine("  - $it") }
        }
        if (onlyInDark.isNotEmpty()) {
            errors.appendLine("Warna hanya ada di DARK (values-night/colors.xml), hilang di LIGHT:")
            onlyInDark.sorted().forEach { errors.appendLine("  - $it") }
        }

        if (errors.isNotEmpty()) {
            fail(
                "Palette light/dark TIDAK sinkron — setiap warna harus ada " +
                "di kedua file:\n$errors"
            )
        }
    }

    // ────────────────────────────────────────────────────────────────
    // Test 2: Tidak boleh ada hardcoded hex di layout XML
    // ────────────────────────────────────────────────────────────────

    /**
     * Regex menangkap atribut XML yang nilainya berupa hex color literal.
     * Contoh yang ter-capture:
     *   android:textColor="#E53935"
     *   android:background="#80000000"
     * Contoh yang TIDAK ter-capture (sudah benar):
     *   android:textColor="@color/danger"
     */
    private val hexColorInAttrRegex = Regex(
        """(android:|app:)\w+\s*=\s*"(#[0-9A-Fa-f]{6,8})""""
    )

    @Test
    fun layoutXmlMustNotContainHardcodedHexColors() {
        val moduleDir = resolveModuleDir()
        val layoutDir = moduleDir.resolve("src/main/res/layout")
        if (!layoutDir.isDirectory) return // tidak ada layout — lewati

        val offenders = StringBuilder()

        layoutDir.listFiles()
            ?.filter { it.extension == "xml" }
            ?.sorted()
            ?.forEach { file ->
                file.readLines().forEachIndexed { idx, line ->
                    hexColorInAttrRegex.findAll(line).forEach { match ->
                        offenders.appendLine(
                            "  ${file.name}:${idx + 1}  ${match.value}"
                        )
                    }
                }
            }

        assertTrue(
            "Layout XML mengandung hardcoded hex color (harus pakai @color/ resource):\n$offenders",
            offenders.isEmpty()
        )
    }

    // ────────────────────────────────────────────────────────────────
    // Test 3: Drawable XML tidak boleh hardcoded hex (kecuali ripple)
    // ────────────────────────────────────────────────────────────────

    /**
     * Regex menangkap atribut `android:color="#..."` di drawable, KECUALI
     * yang ada di elemen `<ripple>`. Untuk menyederhanakan parsing tanpa
     * XML parser, kita cek apakah baris mengandung `<ripple` — jika ya,
     * maka itu atribut ripple overlay yang aman.
     */
    @Test
    fun drawableXmlMustNotContainHardcodedHexColors() {
        val moduleDir = resolveModuleDir()
        val drawableDir = moduleDir.resolve("src/main/res/drawable")
        if (!drawableDir.isDirectory) return

        val offenders = StringBuilder()

        drawableDir.listFiles()
            ?.filter { it.extension == "xml" }
            ?.sorted()
            ?.forEach { file ->
                // Track whether we're inside a <ripple ...> opening tag
                // (can span multiple lines). The android:color on a ripple
                // element is a semi-transparent overlay — safe in both modes.
                var insideRippleTag = false

                file.readLines().forEachIndexed { idx, line ->
                    val trimmed = line.trimStart()
                    if (trimmed.startsWith("<ripple")) insideRippleTag = true
                    if (insideRippleTag) {
                        if (trimmed.contains(">")) insideRippleTag = false
                        return@forEachIndexed
                    }

                    hexColorInAttrRegex.findAll(line).forEach { match ->
                        offenders.appendLine(
                            "  ${file.name}:${idx + 1}  ${match.value}"
                        )
                    }
                }
            }

        assertTrue(
            "Drawable XML mengandung hardcoded hex color (harus pakai @color/ resource):\n$offenders",
            offenders.isEmpty()
        )
    }

    // ────────────────────────────────────────────────────────────────
    // Test 4: Kotlin tidak boleh Color.parseColor (kecuali allowlist)
    // ────────────────────────────────────────────────────────────────

    /**
     * Fungsi yang diperbolehkan menggunakan `Color.parseColor` karena
     * menerima warna dinamis dari server (bukan hardcoded di source).
     */
    private val allowedFunctions = setOf(
        "applyPanelColor",
    )

    private val parseColorRegex = Regex(
        """Color\.parseColor\s*\(\s*"(#[0-9A-Fa-f]{6,8})"\s*\)"""
    )

    @Test
    fun kotlinSourceMustNotUseHardcodedColorParseColor() {
        val moduleDir = resolveModuleDir()
        val javaDir = moduleDir.resolve("src/main/java")
        if (!javaDir.isDirectory) return

        val offenders = StringBuilder()

        javaDir.walkTopDown()
            .filter { it.isFile && it.extension == "kt" }
            .sorted()
            .forEach { file ->
                val lines = file.readLines()
                // Track which function we're in (simple heuristic)
                var currentFunction = ""
                lines.forEachIndexed { idx, line ->
                    // Simple function detection: "fun functionName("
                    val funMatch = Regex("""fun\s+(\w+)\s*\(""").find(line)
                    if (funMatch != null) {
                        currentFunction = funMatch.groupValues[1]
                    }

                    if (currentFunction in allowedFunctions) return@forEachIndexed

                    parseColorRegex.findAll(line).forEach { match ->
                        val relPath = file.absolutePath.removePrefix(
                            moduleDir.absolutePath + File.separator
                        )
                        offenders.appendLine(
                            "  $relPath:${idx + 1}  ${match.value}"
                        )
                    }
                }
            }

        assertTrue(
            "Kotlin source mengandung hardcoded Color.parseColor " +
            "(gunakan ContextCompat.getColor(context, R.color.xxx)):\n$offenders",
            offenders.isEmpty()
        )
    }

    // ────────────────────────────────────────────────────────────────
    // Test 5: text_secondary dan text_muted harus berbeda di kedua mode
    // ────────────────────────────────────────────────────────────────

    @Test
    fun textSecondaryAndTextMutedMustDiffer() {
        val moduleDir = resolveModuleDir()

        for (qualifier in listOf("values", "values-night")) {
            val colorsFile = moduleDir.resolve("src/main/res/$qualifier/colors.xml")
            if (!colorsFile.exists()) continue

            val colorMap = parseColorMap(colorsFile)
            val secondary = colorMap["text_secondary"]
            val muted = colorMap["text_muted"]

            if (secondary != null && muted != null) {
                assertTrue(
                    "text_secondary dan text_muted identik ($secondary) di $qualifier/colors.xml " +
                    "— hirarki teks hilang; harus berbeda agar ada pembedaan visual",
                    !secondary.equals(muted, ignoreCase = true)
                )
            }
        }
    }

    // ────────────────────────────────────────────────────────────────
    // Helpers
    // ────────────────────────────────────────────────────────────────

    /** Ekstrak semua nama warna (`<color name="xxx">`) dari file XML. */
    private fun parseColorNames(file: File): Set<String> {
        if (!file.exists()) return emptySet()
        val regex = Regex("""<color\s+name="(\w+)">""")
        return file.readLines()
            .mapNotNull { regex.find(it)?.groupValues?.get(1) }
            .toSet()
    }

    /** Ekstrak map nama→nilai warna dari file XML. */
    private fun parseColorMap(file: File): Map<String, String> {
        if (!file.exists()) return emptyMap()
        val regex = Regex("""<color\s+name="(\w+)">(#[0-9A-Fa-f]+)</color>""")
        return file.readLines()
            .mapNotNull { line ->
                regex.find(line)?.let { it.groupValues[1] to it.groupValues[2] }
            }
            .toMap()
    }

    /** Unit test Gradle berjalan dari direktori modul (android/app). */
    private fun resolveModuleDir(): File {
        val cwd = File(System.getProperty("user.dir") ?: ".")
        return when {
            File(cwd, "src/main/res").isDirectory -> cwd
            File(cwd, "app/src/main/res").isDirectory -> cwd.resolve("app")
            else -> error(
                "Tidak dapat menemukan direktori sumber Android (cwd=${cwd.absolutePath})"
            )
        }
    }
}
