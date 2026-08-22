package com.examvan.app

import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

/**
 * Guard retensi kolom identitas (permintaan pemilik produk: "kolom
 * identitas sebaiknya dipertahankan isinya sampai user menghapus data /
 * reset data").
 *
 * Kontrak:
 *  1. Identitas (KEY_IDENTITY_DATA) TIDAK PERNAH dihapus oleh alur apapun
 *     selain tombol "Hapus Data Tersimpan" (AppPrefs.clearAllData).
 *  2. Fungsi penghapus identitas tunggal (clearIdentityData) yang tidak
 *     pernah dipanggil TIDAK BOLEH ada — mencegah pelanggaran kontrak
 *     di masa depan lewat pemakaian yang tidak disengaja.
 *
 *   ./gradlew :app:testStudentDebugUnitTest
 */
class IdentityRetentionGuardTest {

    private fun resolveModuleDir(): File {
        val cwd = File(System.getProperty("user.dir") ?: ".")
        return when {
            File(cwd, "src/main/res").isDirectory -> cwd
            File(cwd, "app/src/main/res").isDirectory -> cwd.resolve("app")
            else -> error("Direktori sumber Android tidak ditemukan (cwd=${cwd.absolutePath})")
        }
    }

    private fun kotlinSources(): List<File> {
        val javaDir = resolveModuleDir().resolve("src/main/java")
        return javaDir.walkTopDown()
            .filter { it.isFile && it.extension == "kt" }
            .toList()
    }

    @Test
    fun identityData_isNeverRemovedByAnyFlow() {
        // Satu-satunya penghapusan sah terjadi lewat wipe menyeluruh di
        // clearAllData() yang memakai edit().clear() — bukan remove().
        val offenders = kotlinSources()
            .flatMap { file ->
                file.readLines().mapIndexedNotNull { idx, line ->
                    val stripped = line.substringBefore("//").trim()
                    if (stripped.contains("remove(") && stripped.contains("KEY_IDENTITY_DATA")) {
                        "${file.name}:${idx + 1}"
                    } else null
                }
            }

        assertTrue(
            "KEY_IDENTITY_DATA tidak boleh dihapus oleh alur apapun: $offenders",
            offenders.isEmpty()
        )
    }

    @Test
    fun deadIdentityClearFunction_isAbsent() {
        val offenders = kotlinSources()
            .filter { it.readText().contains("fun clearIdentityData") }
            .map { it.name }

        assertTrue(
            "clearIdentityData adalah fungsi mati yang melanggar kontrak " +
                "retensi identitas jika kelak dipakai — harus dihapus",
            offenders.isEmpty()
        )
    }
}
