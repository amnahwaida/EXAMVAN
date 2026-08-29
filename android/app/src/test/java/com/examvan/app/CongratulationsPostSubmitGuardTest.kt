package com.examvan.app

import org.junit.Test
import org.junit.Assert.*
import java.io.File

/**
 * Guard: setelah submit, siswa hanya boleh menyalin link hasil + melihat
 * ucapan selamat. Tombol "Buka Halaman Hasil" (open in-app WebView) wajib
 * dihapus dari CongratulationsActivity + layout.
 *
 * TDD: test ini harus FAIL sebelum fix (karena btnOpenResult masih ada),
 * dan PASS setelah fix (hanya btnCopyLink + ucapan yang tersisa).
 */
class CongratulationsPostSubmitGuardTest {

    private fun read(rel: String): String {
        val candidates = listOf(
            File("app/src/main/$rel"),
            File("src/main/$rel"),
            File(rel)
        )
        val f = candidates.firstOrNull { it.exists() } ?: candidates.first()
        assertTrue("File tidak ditemukan: $rel (dicari di ${candidates.map { it.absolutePath }})", f.exists())
        return f.readText()
    }

    @Test
    fun layout_hanyaPunyaCopyLink_tanpaOpenResult() {
        val xml = read("res/layout/activity_congratulations.xml")
        assertTrue("layout harus punya btnCopyLink (salin link)", xml.contains("btnCopyLink"))
        assertTrue("layout harus punya tvCongratsMessage / tvCongratsTitle (ucapan)", xml.contains("tvCongratsMessage") && xml.contains("tvCongratsTitle"))
        assertFalse("layout TIDAK boleh punya btnOpenResult — fitur open hasil dihapus, sisa copas link + ucapan", xml.contains("btnOpenResult"))
        assertFalse("layout TIDAK boleh refer congrats_open_result", xml.contains("congrats_open_result"))
    }

    @Test
    fun activity_tidakPunyaOpenResult_hanyaCopyLink() {
        val kt = read("java/com/examvan/app/CongratulationsActivity.kt")
        assertTrue("activity harus punya copyResultLink / btnCopyLink", kt.contains("btnCopyLink") && kt.contains("copyResultLink"))
        assertTrue("activity harus tetap tampilkan congratsMessage", kt.contains("congratsMessage") || kt.contains("tvCongratsMessage"))
        assertFalse("activity TIDAK boleh refer btnOpenResult", kt.contains("btnOpenResult"))
        assertFalse("activity TIDAK boleh punya openResultLink()", kt.contains("openResultLink"))
        assertFalse("activity TIDAK boleh launch ResultsViewerActivity", kt.contains("ResultsViewerActivity"))
    }

    @Test
    fun strings_tetapAda_copyLink_dan_ucapan() {
        val strings = read("res/values/strings.xml")
        assertTrue("strings harus punya congrats_copy_link", strings.contains("congrats_copy_link"))
        assertTrue("strings harus punya congrats_title / congrats_default_message", strings.contains("congrats_title") && strings.contains("congrats_default_message"))
    }
}
