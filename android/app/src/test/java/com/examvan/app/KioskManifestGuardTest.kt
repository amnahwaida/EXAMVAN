package com.examvan.app

import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

/**
 * Guard manifest flavor kiosk (fix temuan review strict ronde 2 #1:
 * "SYSTEM_ALERT_WINDOW dan REORDER_TASKS dideklarasikan tapi TIDAK
 * dipakai di mana pun — SYSTEM_ALERT_WINDOW adalah izin overlay yang
 * identik dengan vektor tapjacking yang justru dibela aplikasi ini").
 *
 * Kontrak: manifest kiosk tidak boleh mendeklarasikan izin berisiko tinggi
 * yang tidak dipakai kode. Menambahkannya kembali akan meredupkan build.
 *
 *   ./gradlew :app:testStudentDebugUnitTest
 */
class KioskManifestGuardTest {

    private fun resolveModuleDir(): File {
        val cwd = File(System.getProperty("user.dir") ?: ".")
        return when {
            File(cwd, "src/main/res").isDirectory -> cwd
            File(cwd, "app/src/main/res").isDirectory -> cwd.resolve("app")
            else -> error("Direktori sumber Android tidak ditemukan (cwd=${cwd.absolutePath})")
        }
    }

    private fun kioskManifest(): String {
        // Overlay manifest kiosk relatif terhadap direktori modul app/.
        val candidate = resolveModuleDir().resolve("src/kiosk/AndroidManifest.xml")
        assertTrue("Manifest kiosk tidak ditemukan di ${candidate.path}", candidate.exists())
        return candidate.readText()
    }

    @Test
    fun noDangerousUnusedPermissions_declared() {
        val manifest = kioskManifest()
        for (permission in listOf(
            "android.permission.SYSTEM_ALERT_WINDOW",
            "android.permission.REORDER_TASKS"
        )) {
            assertTrue(
                "Izin '$permission' tidak dipakai kode dan berisiko — " +
                    "tidak boleh dideklarasikan di manifest kiosk",
                !manifest.contains(permission)
            )
        }
    }

    @Test
    fun deviceAdminReceiver_stillDeclared() {
        // Guard kebalikan: penghapusan izin tidak boleh ikut merusak
        // komponen inti kiosk.
        assertTrue(
            kioskManifest().contains("MyDeviceAdminReceiver")
        )
    }
}
