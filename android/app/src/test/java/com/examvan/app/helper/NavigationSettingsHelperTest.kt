package com.examvan.app.helper

import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

/**
 * Guard: tombol "Buka Pengaturan" tidak boleh cuma membuka Settings umum
 * tanpa panduan. Path manual harus cocok dengan menu OEM yang umum dipakai.
 *
 *   ./gradlew :app:testStudentDebugUnitTest
 */
class NavigationSettingsHelperTest {

    @Test
    fun instructionText_coversMajorOemMenus() {
        assertTrue(
            NavigationSettingsHelper.instructionTextFor("samsung", "samsung")
                .contains("Display > Navigation bar")
        )
        assertTrue(
            NavigationSettingsHelper.instructionTextFor("Xiaomi", "Redmi")
                .contains("System navigation")
        )
        assertTrue(
            NavigationSettingsHelper.instructionTextFor("OPPO", "realme")
                .contains("3-button navigation")
        )
        assertTrue(
            NavigationSettingsHelper.instructionTextFor("vivo", "iQOO")
                .contains("System navigation")
        )
        assertTrue(
            NavigationSettingsHelper.instructionTextFor("HUAWEI", "HONOR")
                .contains("Three-key navigation")
        )
        assertTrue(
            NavigationSettingsHelper.instructionTextFor("asus", "ASUS")
                .contains("System navigation")
        )
        assertTrue(
            NavigationSettingsHelper.instructionTextFor("Infinix", "TECNO")
                .contains("3-button navigation")
        )
        assertTrue(
            NavigationSettingsHelper.instructionTextFor("motorola", "moto")
                .contains("System navigation")
        )
    }

    @Test
    fun unknownOem_stillGetsUsableFallbackPath() {
        val path = NavigationSettingsHelper.instructionTextFor("unknown", "device")
        assertTrue(path.contains("Settings"))
        assertTrue(path.contains("3-button navigation"))
    }

    @Test
    fun helper_triesOfficialActionAndOemSettingsBeforeGenericFallback() {
        val cwd = File(System.getProperty("user.dir") ?: ".")
        val moduleDir = when {
            File(cwd, "src/main/java").isDirectory -> cwd
            File(cwd, "app/src/main/java").isDirectory -> cwd.resolve("app")
            else -> error("Direktori sumber Android tidak ditemukan (cwd=${cwd.absolutePath})")
        }
        val source = moduleDir
            .resolve("src/main/java/com/examvan/app/helper/NavigationSettingsHelper.kt")
            .readText()

        assertTrue(
            "Harus memakai action resmi Android 11+",
            source.contains("android.settings.SYSTEM_NAVIGATION_SETTINGS")
        )
        assertTrue(
            "Harus punya kandidat activity OEM (Samsung/Xiaomi/AOSP)",
            source.contains("NavigationBarSettingsActivity") &&
                source.contains("FullScreenDisplayActivity")
        )
        assertTrue(
            "Harus menemukan activity Settings yang mengandung navigation",
            source.contains("getPackageInfo") &&
                source.contains("systemnavigation")
        )
        assertTrue(
            "Fallback terakhir harus Settings umum",
            source.contains("Settings.ACTION_SETTINGS")
        )
    }
}
