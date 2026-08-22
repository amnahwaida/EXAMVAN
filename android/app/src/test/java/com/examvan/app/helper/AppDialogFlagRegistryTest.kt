package com.examvan.app.helper

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Mengunci kontrak registri flag "app dialog sedang tampil" berbasis holder
 * (fix temuan review mode medium gelombang kedua #1: "onResume me-reset
 * isShowingAppDialog secara BUTA via posted runnable — flag bisa lepas
 * padahal dialog izin masih terbuka, membuka kembali celah auto-submit
 * palsu dari jalur focus-loss").
 *
 * Kontrak:
 *  - Flag aktif selama MINIMAL SATU holder terdaftar.
 *  - resetIfIdle() hanya mengaplikasikan false bila TIDAK ada holder;
 *    dengan holder aktif ia NO-OP dan melaporkan gagal.
 *
 *   ./gradlew :app:testStudentDebugUnitTest
 */
class AppDialogFlagRegistryTest {

    private class Recorder {
        val applied = mutableListOf<Boolean>()
        fun apply(value: Boolean) { applied.add(value) }
    }

    @Test
    fun acquire_appliesTrue_oncePerAcquire() {
        val rec = Recorder()
        val registry = AppDialogFlagRegistry(rec::apply)

        registry.acquire("dialog_a")

        assertEquals(listOf(true), rec.applied)
    }

    @Test
    fun release_lastHolder_appliesFalse() {
        val rec = Recorder()
        val registry = AppDialogFlagRegistry(rec::apply)
        registry.acquire("dialog_a")

        registry.release("dialog_a")

        assertEquals(listOf(true, false), rec.applied)
        assertFalse(registry.hasHolders())
    }

    @Test
    fun release_nonLastHolder_flagStaysTrue() {
        // Dua dialog aktif bersamaan (mis. izin notifikasi + konfirmasi
        // keluar): menutup satu TIDAK boleh mematikan flag.
        val rec = Recorder()
        val registry = AppDialogFlagRegistry(rec::apply)
        registry.acquire("permission")
        registry.acquire("logout")
        rec.applied.clear()

        registry.release("permission")

        assertEquals(emptyList<Boolean>(), rec.applied) // tidak ada perubahan
        assertTrue(registry.hasHolders())
    }

    @Test
    fun release_sameIdTwice_secondIsNoOp() {
        val rec = Recorder()
        val registry = AppDialogFlagRegistry(rec::apply)
        registry.acquire("dialog_a")
        rec.applied.clear()

        registry.release("dialog_a")
        registry.release("dialog_a") // hasil permission ganda / race

        assertEquals(listOf(false), rec.applied)
    }

    @Test
    fun resetIfIdle_noHolders_appliesFalseAndReportsSuccess() {
        // Kasus onResume biasa: tak ada dialog aktif — safety-net reset jalan.
        val rec = Recorder()
        val registry = AppDialogFlagRegistry(rec::apply)

        val reset = registry.resetIfIdle()

        assertTrue(reset)
        assertEquals(listOf(false), rec.applied)
    }

    @Test
    fun resetIfIdle_holderActive_isNoOpAndReportsFailure() {
        // KUNCI FIX #1: prompt izin masih pending → reset dari onResume
        // TIDAK boleh mematikan flag.
        val rec = Recorder()
        val registry = AppDialogFlagRegistry(rec::apply)
        registry.acquire("permission")
        rec.applied.clear()

        val reset = registry.resetIfIdle()

        assertFalse(reset)
        assertTrue(
            "resetIfIdle tidak boleh mengaplikasikan apapun saat ada holder",
            rec.applied.isEmpty()
        )
        assertTrue(registry.hasHolders())
    }

    @Test
    fun integration_withPermissionPromptCoordinator_resetBlockedWhilePending() {
        // Skenario ujung-ke-ujung fix #1 di level JVM: koordinator izin +
        // registri dipakai bersama seperti di SubmissionManager/Activity.
        val rec = Recorder()
        val registry = AppDialogFlagRegistry(rec::apply)
        val coordinator = PermissionPromptCoordinator(
            setDialogFlag = rec::apply,
            markPending = { pending ->
                if (pending) registry.acquire("permission") else registry.release("permission")
            }
        )

        // Prompt dimulai → flag ON, holder terdaftar.
        coordinator.requestViaSystemDialog { /* requestPermissions */ }

        // Activity di-resume di tengah prompt (rotasi dsb.) → reset harus
        // GAGAL: dialog sistem masih di layar.
        assertFalse(registry.resetIfIdle())

        // Hasil permission datang → holder lepas, flag mati. Kedua kanal
        // (koordinator & registry, seperti wiring produksi yang menunjuk
        // variabel flag yang sama) masing-masing mengaplikasikan OFF.
        coordinator.onPermissionResult()

        assertEquals(listOf(true, true, false, false), rec.applied)
        assertFalse(registry.hasHolders())
    }
}
