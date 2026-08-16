package com.examvan.app

import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File
import java.util.Locale

/**
 * Mengunci kebijakan UI aplikasi Android: **TIDAK boleh ada emoji** di
 * resource maupun kode sumber.
 *
 * Emoji (piktograf berwarna) terkesan kurang profesional untuk aplikasi ujian
 * dan dirender tidak konsisten antar perangkat/OEM. Kebijakan ini dipilih
 * sejak audit (16 Agustus 2026): semua emoji UI diganti teks polos, glyph
 * tipografis monokrom (ceklist, silang, panah), atau vector drawable
 * Material Design.
 *
 * Yang di-scan:
 *  - `src/main/res` — semua resource XML (strings, layout, drawable, dst.)
 *  - `src/main/java` — semua sumber Kotlin/Java
 *  - `src/student` & `src/kiosk` — manifest per-flavor
 *
 * Yang DIPERBOLEHKAN (allowlist): glyph tipografis yang dirender monokrom
 * sebagai teks — bukan emoji berwarna — dan merupakan tipografi standar UI
 * profesional. Menambah glyph baru ke allowlist WAJIB disertai alasan.
 */
class NoEmojiInAppTest {

    /**
     * Rentang Unicode yang dianggap emoji. Mencakup blok piktograf berwarna
     * dan simbol yang lazim dirender sebagai emoji oleh font perangkat.
     */
    private val emojiRanges = listOf(
        0x1F000..0x1FAFF, // blok piktograf: wajah, simbol, transportasi, dst.
        0x231A..0x231B,   // jam tangan, jam pasir (⌚⌛)
        0x23E9..0x23F3,   // piktograf media & jam (⏩..⏳)
        0x25AA..0x25AB,   // kotak kecil hitam/putih (▪▫)
        0x25B6..0x25C0,   // segitiga play/pause (▶◀◁)
        0x2600..0x27BF,   // simbol & dingbat (⚠ ⚙ ✅ ❌ ⭐ dst.)
        0x2764..0x2764,   // hati tebal (❤)
        0x2B50..0x2B50,   // bintang putih (⭐)
        0xFE0F..0xFE0F    // VS16 — memaksa presentasi emoji pada glyph teks
    )

    /**
     * Glyph tipografis yang DIPERBOLEHKAN — dirender monokrom sebagai teks:
     */
    private val allowedGlyphs = setOf(
        0x2713, // ✓ tanda ceklis (state sukses: approval, layar selesai)
        0x2715, // ✕ tanda silang (state ditolak/gagal)
        0x2794, // ➔ panah pemisah kolom menjodohkan (item_matching_row)
    )

    private fun isEmojiCodePoint(cp: Int): Boolean {
        if (cp in allowedGlyphs) return false
        return emojiRanges.any { cp in it }
    }

    @Test
    fun resourcesAndSourcesContainNoEmoji() {
        val moduleDir = resolveModuleDir()
        val offenders = StringBuilder()

        val roots = listOf(
            "src/main/res",
            "src/main/java",
            "src/student",
            "src/kiosk",
        )

        val files = roots
            .map { moduleDir.resolve(it) }
            .filter { it.isDirectory }
            .flatMap { root -> root.walkTopDown().filter { it.isFile }.toList() }
            .filter { it.extension in setOf("xml", "kt", "java", "properties") }
            .sortedBy { it.absolutePath }

        assertTrue(
            "Direktori sumber tidak ditemukan (cwd=${File("").absolutePath}) — test harus jalan dari direktori modul app/",
            files.isNotEmpty()
        )

        for (file in files) {
            val text = try {
                file.readText(Charsets.UTF_8)
            } catch (e: Exception) {
                continue // file biner/tidak terbaca — lewati
            }
            var offset = 0
            while (offset < text.length) {
                val cp = text.codePointAt(offset)
                if (isEmojiCodePoint(cp)) {
                    val line = text.substring(0, offset).count { it == '\n' } + 1
                    offenders.append(String.format(Locale.ROOT, "  %s:%d  U+%04X (%s)%n",
                        relativeTo(moduleDir, file), line, cp, String(Character.toChars(cp))))
                }
                offset += Character.charCount(cp)
            }
        }

        assertTrue(
            "Kebijakan 'tanpa emoji' dilanggar di sumber aplikasi Android " +
                "(ganti dengan teks polos, glyph tipografis monokrom, atau vector drawable):\n" +
                offenders,
            offenders.isEmpty()
        )
    }

    /** Unit test Gradle berjalan dari direktori modul (android/app). */
    private fun resolveModuleDir(): File {
        val cwd = File(System.getProperty("user.dir") ?: ".")
        return when {
            File(cwd, "src/main/res").isDirectory -> cwd
            File(cwd, "app/src/main/res").isDirectory -> cwd.resolve("app")
            else -> error("Tidak dapat menemukan direktori sumber Android (cwd=${cwd.absolutePath})")
        }
    }

    private fun relativeTo(base: File, file: File): String {
        return file.absolutePath.removePrefix(base.absolutePath + File.separator)
    }
}
