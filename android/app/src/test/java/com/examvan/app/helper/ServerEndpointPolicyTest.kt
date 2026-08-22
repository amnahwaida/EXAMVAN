package com.examvan.app.helper

import org.junit.Assert.assertEquals
import org.junit.Test

/**
 * Mengunci kontrak endpoint server bawaan (permintaan pemilik produk:
 * "endpoint examvan.my.id jangan pernah dihapus baik ketika dihapus data
 * maupun reset data; bisa diganti jika memang ingin mengubah endpoint").
 *
 * Kontrak:
 *  - DEFAULT_SERVER_URL adalah satu-satunya sumber nilai default.
 *  - resolveDisplayUrl: field input TIDAK PERNAH kosong — bila tersimpan
 *    kosong (pasca clear/reset), default SELALU dipulihkan; URL kustom
 *    milik user tetap dipertahankan.
 *
 *   ./gradlew :app:testStudentDebugUnitTest
 */
class ServerEndpointPolicyTest {

    @Test
    fun defaultValue_isExamvanProduction() {
        assertEquals("https://examvan.my.id", ServerEndpointPolicy.DEFAULT_SERVER_URL)
    }

    @Test
    fun resolve_blankSavedUrl_restoresDefault() {
        // Pasca "Hapus Data" / reset: endpoint tidak boleh hilang.
        assertEquals(
            ServerEndpointPolicy.DEFAULT_SERVER_URL,
            ServerEndpointPolicy.resolveDisplayUrl(null)
        )
        assertEquals(
            ServerEndpointPolicy.DEFAULT_SERVER_URL,
            ServerEndpointPolicy.resolveDisplayUrl("")
        )
        assertEquals(
            ServerEndpointPolicy.DEFAULT_SERVER_URL,
            ServerEndpointPolicy.resolveDisplayUrl("   ")
        )
    }

    @Test
    fun resolve_customUrl_preserved() {
        // User yang sengaja mengganti endpoint tidak dilawan.
        assertEquals(
            "https://sekolah.example.com",
            ServerEndpointPolicy.resolveDisplayUrl("https://sekolah.example.com")
        )
    }

    @Test
    fun resolve_customUrlWithWhitespace_passedThroughAsIs() {
        // Tampilan tidak mengubah ketikan user (trim dilakukan validasi
        // saat connect) — hanya nilai KOSONG yang diganti default.
        assertEquals(
            "  https://x.id  ",
            ServerEndpointPolicy.resolveDisplayUrl("  https://x.id  ")
        )
    }
}
