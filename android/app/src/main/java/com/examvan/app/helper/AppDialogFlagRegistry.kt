package com.examvan.app.helper

/**
 * Registri flag "app dialog sedang tampil" berbasis holder.
 *
 * Fix temuan review mode medium gelombang kedua #1: onResume me-reset flag
 * isShowingAppDialog secara BUTA lewat posted runnable — flag bisa lepas
 * padahal dialog izin notifikasi masih terbuka, sehingga jalur focus-loss
 * (>500 ms) memicu auto-submit palsu.
 *
 * Solusi: setiap pemilik dialog mendaftar sebagai holder (acquire/release).
 * Reset dari onResume menjadi KONDISIONAL — hanya efektif bila tidak ada
 * holder aktif. Flag boolean diaplikasikan lewat callback [apply] setiap
 * kali status holder berubah (aktif selama minimal satu holder terdaftar).
 *
 * Thread-safety: synchronized sederhana — semua akses dari main thread,
 * tapi diamankan untuk keadaan luar biasa.
 */
class AppDialogFlagRegistry(private val apply: (Boolean) -> Unit) {

    private val holders = LinkedHashSet<String>()

    /** Daftarkan holder; flag diaplikasikan true. Idempoten per id. */
    @Synchronized
    fun acquire(id: String) {
        if (holders.add(id)) apply(true)
    }

    /**
     * Lepaskan holder; flag diaplikasikan false HANYA bila itu holder
     * terakhir. Melepas id yang tidak terdaftar adalah no-op (tanpa apply) —
     * hasil permission ganda / race tidak boleh mematikan flag milik
     * dialog lain yang masih aktif.
     */
    @Synchronized
    fun release(id: String) {
        if (holders.remove(id) && holders.isEmpty()) {
            apply(false)
        }
    }

    @Synchronized
    fun hasHolders(): Boolean = holders.isNotEmpty()

    /**
     * Safety-net reset dari onResume: hanya efektif bila TIDAK ada holder.
     * @return true bila reset dijalankan; false bila ada dialog/prompt
     *         aktif (reset di-skip agar flag tetap melindungi jalur fokus).
     */
    @Synchronized
    fun resetIfIdle(): Boolean {
        if (holders.isNotEmpty()) return false
        apply(false)
        return true
    }
}
