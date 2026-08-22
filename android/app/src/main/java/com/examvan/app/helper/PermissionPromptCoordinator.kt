package com.examvan.app.helper

/**
 * Koordinator urutan flag "app dialog sedang tampil" di sekitar permintaan
 * izin sistem (fix temuan review mode medium #1).
 *
 * LATAR: dialog izin POST_NOTIFICATIONS mencuri fokus window. Jalur
 * anti-cheat focus-loss (>500 ms tanpa fokus) tidak boleh menganggapnya
 * pelarian siswa — flag isShowingAppDialog harus aktif SELAMA dialog sistem
 * tampil, termasuk jeda async setelah requestPermissions() kembali.
 *
 * Kontrak (dikunci PermissionPromptCoordinatorTest):
 *  - requestViaSystemDialog: flag ON → baru requestPermissions.
 *  - Flag TIDAK direset saat requestPermissions kembali (dialog masih di
 *    layar); reset HANYA lewat onPermissionResult dari
 *    onRequestPermissionsResult.
 *
 * Murni lambda-driven agar urutannya bisa dites di JVM; SubmissionManager
 * memakainya dengan callback setShowingAppDialog milik SecurityEnforcer.
 */
class PermissionPromptCoordinator(
    private val setDialogFlag: (Boolean) -> Unit,
    private val markPending: (Boolean) -> Unit = {}
) {

    /**
     * Minta izin lewat dialog sistem dengan proteksi focus-loss aktif.
     * Flag dinyalakan SEBELUM [requestPermission] dan dibiarkan menyala —
     * penelepon wajib memanggil [onPermissionResult] di
     * onRequestPermissionsResult untuk memadamkannya.
     *
     * [markPending] (opsional) menandai status "prompt masih berjalan" ke
     * AppDialogFlagRegistry sehingga safety-net reset dari onResume tahu
     * bahwa dialog sistem masih di layar (fix review gel. 2 #1).
     */
    fun requestViaSystemDialog(requestPermission: () -> Unit) {
        markPending(true)
        setDialogFlag(true)
        requestPermission()
    }

    /** Reset flag ketika hasil permission sudah diterima. Idempoten. */
    fun onPermissionResult() {
        setDialogFlag(false)
        markPending(false)
    }

    /**
     * Jalur rationale (fix review ronde 3 #1): dialog penjelasan in-app
     * tampil SEBELUM prompt sistem. Holder didaftarkan sejak dialog ini
     * tampil sehingga safety-net reset dari onResume tidak bisa melepas
     * flag di tengah dialog. Acquire idempoten — tumpukan dengan fase
     * prompt sistem aman.
     */
    fun showRationaleWithHolder() {
        markPending(true)
        setDialogFlag(true)
    }

    /**
     * Dialog rationale ditutup TANPA lanjut ke prompt sistem (user menolak
     * atau menutup) — flag OFF dan holder dilepas; reset idle kembali sah.
     */
    fun dismissRationaleWithoutRequest() {
        setDialogFlag(false)
        markPending(false)
    }
}
