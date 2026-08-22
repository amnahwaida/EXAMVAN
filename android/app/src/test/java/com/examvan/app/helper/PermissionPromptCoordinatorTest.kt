package com.examvan.app.helper

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Mengunci urutan flag "app dialog sedang tampil" di sekitar permintaan izin
 * sistem (fix temuan review mode medium #1: "requestPermissions dipanggil
 * TANPA setShowingAppDialog(true) — dialog izin mencuri fokus window, dan
 * bila PDF sudah siap, jalur focus-loss >500ms memicu AUTO-SUBMIT palsu
 * hanya karena dialog izin muncul").
 *
 * Kontrak yang dikunci:
 * 1. Flag HARUS aktif SEBELUM requestPermissions dipanggil (dialog sistem
 *    sudah bisa mencuri fokus kapan saja setelahnya).
 * 2. Flag TIDAK boleh direset saat requestPermissions kembali (fungsi itu
 *    async — dialog MASIH tampil di layar).
 * 3. Flag hanya direset oleh onPermissionResult (onRequestPermissionsResult).
 * 4. Jalur tanpa dialog (izin sudah granted / API < 33) tidak menyentuh flag.
 *
 * Koordinator ini murni lambda-driven sehingga urutannya bisa dites di JVM;
 * SubmissionManager memakainya dengan callback setShowingAppDialog nyata.
 *
 *   ./gradlew :app:testStudentDebugUnitTest
 */
class PermissionPromptCoordinatorTest {

    private class Recorder {
        val events = mutableListOf<String>()
        fun setFlag(value: Boolean) { events.add(if (value) "flag+ON" else "flag+OFF") }
        fun request() { events.add("request") }
    }

    @Test
    fun viaSystemDialog_flagOnBeforeRequest_flagStaysOnAfterRequest() {
        val rec = Recorder()
        val coordinator = PermissionPromptCoordinator(rec::setFlag)

        coordinator.requestViaSystemDialog { rec.request() }

        assertEquals(
            listOf("flag+ON", "request"),
            rec.events
        )
    }

    @Test
    fun onPermissionResult_resetsFlag() {
        val rec = Recorder()
        val coordinator = PermissionPromptCoordinator(rec::setFlag)
        coordinator.requestViaSystemDialog { rec.request() }

        coordinator.onPermissionResult()

        assertEquals(
            listOf("flag+ON", "request", "flag+OFF"),
            rec.events
        )
    }

    @Test
    fun onPermissionResult_withoutPendingRequest_resetIsIdempotentSafe() {
        // Hasil permission bisa datang dua kali / tanpa prompt (mis. state
        // race) — reset ganda tidak boleh error.
        val rec = Recorder()
        val coordinator = PermissionPromptCoordinator(rec::setFlag)

        coordinator.onPermissionResult()
        coordinator.onPermissionResult()

        assertEquals(listOf("flag+OFF", "flag+OFF"), rec.events)
    }

    // ── Jalur rationale (fix review ronde 3 #1) ──────────────────────────

    /**
     * DIALOG RATIONALE = dialog penjelasan in-app SEBELUM prompt sistem.
     * Fix temuan review ronde 3 #1: dulu jalur ini set flag langsung TANPA
     * holder — safety-net reset kondisional di onResume bisa melepas flag
     * padahal dialog rationale masih terbuka (invariant "reset hanya saat
     * tidak ada dialog aktif" dilanggar).
     */

    @Test
    fun rationale_shown_holderRegistered_blocksIdleReset() {
        val rec = Recorder()
        var pending = false
        val registry = AppDialogFlagRegistry { rec.setFlag(it) }
        val coordinator = PermissionPromptCoordinator(
            setDialogFlag = { rec.setFlag(it) },
            markPending = { p ->
                pending = p
                if (p) registry.acquire("permission") else registry.release("permission")
            }
        )

        // Dialog rationale tampil → holder HARUS sudah terdaftar.
        coordinator.showRationaleWithHolder()

        assertTrue(pending)
        assertTrue(
            "Reset idle tidak boleh berhasil saat dialog rationale terbuka",
            !registry.resetIfIdle()
        )
    }

    @Test
    fun rationale_dismissedWithoutRequest_flagOffAndHolderReleased() {
        val rec = Recorder()
        val registry = AppDialogFlagRegistry { rec.setFlag(it) }
        val coordinator = PermissionPromptCoordinator(
            setDialogFlag = rec::setFlag,
            markPending = { p ->
                if (p) registry.acquire("permission") else registry.release("permission")
            }
        )
        coordinator.showRationaleWithHolder()
        rec.events.clear()

        // User menekan "Jangan Izinkan" / menutup dialog — tanpa prompt sistem.
        coordinator.dismissRationaleWithoutRequest()

        // Dua kanal (koordinator + registry) sama-sama mengaplikasikan OFF,
        // seperti wiring produksi yang menunjuk variabel flag yang sama.
        assertEquals(listOf("flag+OFF", "flag+OFF"), rec.events)
        assertTrue(
            "Setelah rationale ditutup tanpa request, reset idle harus berhasil",
            registry.resetIfIdle()
        )
    }

    @Test
    fun rationale_thenSystemPrompt_singleHolder_resultReleasesAll() {
        // Alur lengkap: rationale → user tap "Izinkan" → prompt sistem →
        // hasil. Acquire idempoten: satu holder, sekali release cukup.
        val rec = Recorder()
        val registry = AppDialogFlagRegistry { rec.setFlag(it) }
        val coordinator = PermissionPromptCoordinator(
            setDialogFlag = rec::setFlag,
            markPending = { p ->
                if (p) registry.acquire("permission") else registry.release("permission")
            }
        )

        coordinator.showRationaleWithHolder()
        coordinator.requestViaSystemDialog { /* requestPermissions */ }

        // Masih dalam prompt sistem → reset tetap diblokir.
        assertFalse(registry.resetIfIdle())

        coordinator.onPermissionResult()

        assertFalse(registry.hasHolders())
        assertTrue(registry.resetIfIdle())
    }
}
