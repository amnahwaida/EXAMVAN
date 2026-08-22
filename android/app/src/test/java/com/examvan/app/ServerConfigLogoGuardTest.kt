package com.examvan.app

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

/**
 * Guard logo layar Server & Token (permintaan pemilik produk: "logo di atas
 * tulisan server dan token harus sama dengan logo/ikon aplikasi").
 *
 * Kontrak:
 *  - Layout activity_server_config memakai IKON APLIKASI asli
 *    (@mipmap/ic_launcher), bukan placeholder huruf.
 *
 *   ./gradlew :app:testStudentDebugUnitTest
 */
class ServerConfigLogoGuardTest {

    private fun layoutFile(): File {
        val cwd = File(System.getProperty("user.dir") ?: ".")
        val module = when {
            File(cwd, "src/main/res").isDirectory -> cwd
            File(cwd, "app/src/main/res").isDirectory -> cwd.resolve("app")
            else -> error("Direktori sumber Android tidak ditemukan")
        }
        return module.resolve("src/main/res/layout/activity_server_config.xml")
    }

    @Test
    fun serverConfigLayout_usesRealAppIcon() {
        val layout = layoutFile().readText()
        assertTrue(
            "Layar Server & Token harus menampilkan ikon aplikasi asli (@mipmap/ic_launcher)",
            layout.contains("@mipmap/ic_launcher")
        )
    }

    @Test
    fun serverConfigLayout_placeholderLetterLogoRemoved() {
        val layout = layoutFile().readText()
        assertFalse(
            "Placeholder huruf 'E' sebagai logo harus dihapus",
            layout.contains("android:text=\"E\"")
        )
    }
}
