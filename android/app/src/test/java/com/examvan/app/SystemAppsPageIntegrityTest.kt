package com.examvan.app

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

/**
 * Guard integritas halaman Aplikasi Sistem (bug nyata: saat penggabungan 5
 * halaman settings menjadi satu, MARKUP MODAL UNGGAH tertinggal — tombol
 * "Unggah Aplikasi Baru" tampak tapi mati karena openUploadModal() menabrak
 * elemen null).
 *
 * Kontrak: SETIAP id elemen yang dirujuk oleh modul
 * settings-system-apps.js WAJIB ada di settings.html.
 *
 *   ./gradlew :app:testStudentDebugUnitTest
 */
class SystemAppsPageIntegrityTest {

    /** Root repo (menaiki direktori sampai menemukan .git). */
    private fun repoRoot(): File {
        var dir = File(System.getProperty("user.dir") ?: ".").absoluteFile
        while (dir != null && !File(dir, ".git").exists()) {
            dir = dir.parentFile ?: error("Root repo tidak ditemukan dari $dir")
        }
        return dir
    }

    private fun webuiPage(): File =
        repoRoot().resolve("webui/templates/admin/settings.html")

    private fun webuiJs(): File =
        repoRoot().resolve("webui/static/js/settings-system-apps.js")

    /** Id yang wajib ada — diekstrak dari referensi settings-system-apps.js. */
    private val requiredIds = listOf(
        "btnOpenUploadApp",
        "uploadModal",
        "uploadAppForm",
        "appName",
        "platformSelect",
        "appVersion",
        "appFile",
        "file-name-display",
        "uploadError",
        "uploadErrorText",
        "uploadProgressContainer",
        "uploadProgressBar",
        "uploadPercentage",
        "uploadStatusText",
        "uploadSubmitBtn"
    )

    @Test
    fun systemAppsSection_containsEveryReferencedElementId() {
        val html = webuiPage().readText()

        val missing = requiredIds.filter { id ->
            !html.contains("""id="$id"""")
        }

        assertTrue(
            "Elemen berikut hilang dari webui/templates/admin/settings.html " +
                "(tombol/modal akan mati):\n" + missing.joinToString("\n") { "  - #$it" },
            missing.isEmpty()
        )
    }

    @Test
    fun jsModule_exists() {
        val js = webuiJs()
        assertTrue(
            "Modul settings-system-apps.js harus ada (dimuat lazy oleh tab Pengaturan)",
            js.exists() && js.length() > 0L
        )
    }

    // ── Fix review Aplikasi Sistem: drag-drop & modal-close guard ────────

    @Test
    fun dragAndDrop_isWired() {
        val js = webuiJs().readText()
        assertTrue(
            "Teks area file menjanjikan 'Seret File' tapi tidak ada handler " +
                "dragover/drop — wajib di-wire",
            js.contains("'dragover'") && js.contains("addEventListener('drop'")
        )
        assertTrue(
            "Handler drop harus meneruskan file ke input #appFile",
            js.contains("appFile")
        )
    }

    @Test
    fun modalClose_blockedWhileUploading() {
        val js = webuiJs().readText()
        assertTrue(
            "closeUploadModal harus menolak penutupan selama unggahan berjalan",
            js.contains("__uploadInProgress") && js.contains("canCloseUpload")
        )
    }

    // ── Fix review ronde 3: refresh daftar IN-PLACE tanpa reload ─────────

    @Test
    fun jsonListEndpoint_registered() {
        val mainGo = repoRoot().resolve("webui/cmd/server/main.go").readText()
        assertTrue(
            "Endpoint GET /admin/api/system-apps wajib terdaftar (sumber data " +
                "refresh kartu aplikasi tanpa reload halaman)",
            mainGo.contains("adminSettings.GET(\"/system-apps\", admin.ListSystemAppsJSON())")
        )
    }

    @Test
    fun gridRefreshedInPlace_noFullPageReload() {
        val js = webuiJs().readText()
        assertTrue(
            "loadApps() wajib ada — refresh kartu aplikasi via API JSON",
            js.contains("function loadApps()")
        )
        assertTrue(
            "renderAppsGrid wajib ada — merender ulang grid dari data",
            js.contains("function renderAppsGrid")
        )
        // Reload polos = kembali ke tab default & kehilangan konteks tab.
        assertFalse(
            "window.location.reload() tidak boleh lagi dipakai di modul ini",
            js.contains("window.location.reload()")
        )
    }
}
