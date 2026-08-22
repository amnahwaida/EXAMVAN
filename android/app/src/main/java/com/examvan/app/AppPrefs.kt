package com.examvan.app

import android.content.Context
import android.content.SharedPreferences
import android.util.Log
import androidx.security.crypto.EncryptedSharedPreferences
import androidx.security.crypto.MasterKey
import java.io.IOException
import java.security.GeneralSecurityException

object AppPrefs {
    private const val PREFS_CONFIG = "app_config_encrypted"
    private const val PREFS_EXAM = "exam_questions_encrypted"
    private const val PREFS_DEVICE = "device_id_encrypted"

    const val KEY_SERVER_URL = "server_url"
    const val KEY_EXAM_ID = "exam_id"
    const val KEY_EXAM_TOKEN = "exam_token"
    const val KEY_REMEMBER_URL = "remember_url"
    const val KEY_IDENTITY_DATA = "identity_data"
    const val KEY_QUESTIONS_JSON = "questions_json"
    const val KEY_SECURITY_LEVEL = "security_level"
    const val KEY_STRICT_MODE = "strict_mode"
    const val KEY_PANEL_COLOR = "panel_color"
    const val KEY_DEVICE_UUID = "device_uuid"
    const val KEY_SAVED_ANSWERS = "saved_answers"
    const val KEY_SAVED_ANSWERS_EXAM_ID = "saved_answers_exam_id"
    const val KEY_SAVED_ANSWERS_TIMESTAMP = "saved_answers_timestamp"
    const val KEY_EXAM_START_TIME = "exam_start_time"
    const val KEY_SUBMITTED_OR_EXITED = "submitted_or_exited"
    const val KEY_LAST_VERSION_CHECK_TS = "last_version_check_ts"

    /**
     * Baseline clock-drift sesi ujian (wallClock - elapsedRealtime saat mulai).
     * Dipersist ke prefs terenkripsi agar bertahan process death — fix temuan
     * review: dulu baseline hanya ada di memory/savedInstanceState sehingga
     * siswa bisa memanipulasi jam setelah me-restart proses app.
     * Lihat ClockDriftPolicy.resolveBaseline.
     */
    const val KEY_CLOCK_DRIFT_BASELINE = "clock_drift_baseline"

    fun getSubmittedOrExitedKey(examId: Int): String {
        return "${KEY_SUBMITTED_OR_EXITED}_$examId"
    }

    @Volatile
    private var masterKey: MasterKey? = null
    private var configPrefs: SharedPreferences? = null
    private var examPrefs: SharedPreferences? = null
    private var devicePrefs: SharedPreferences? = null

    private fun getMasterKey(context: Context): MasterKey {
        return masterKey ?: synchronized(this) {
            masterKey ?: MasterKey.Builder(context)
                .setKeyScheme(MasterKey.KeyScheme.AES256_GCM)
                .build()
                .also { masterKey = it }
        }
    }

    /**
     * Reset data sesi ujian lama saat bergabung ke ujian (fix review ronde 5
     * #1 & #2). Keputusan reset & pemangkasan delegasi ke
     * ExamSessionResetPolicy:
     *  - KEY_EXAM_START_TIME dihapus bila [newExamId] berbeda dari sesi
     *    tersimpan (rejoin ujian sama tidak disentuh — resilience recovery);
     *  - flag submitted_or_exit_<id> milik ujian LAIN dipangkas.
     *
     * @return true bila ada key yang dihapus.
     */
    fun clearStaleExamSession(context: Context, storedExamId: Int, newExamId: Int): Boolean {
        if (!com.examvan.app.helper.ExamSessionResetPolicy.shouldResetSession(storedExamId, newExamId)) {
            return false
        }
        val prefs = getExamPrefsSafe(context)
        val editor = prefs.edit().remove(KEY_EXAM_START_TIME)
        val prunable = com.examvan.app.helper.ExamSessionResetPolicy
            .prunableSubmittedFlagKeys(prefs.all.keys, keepExamId = newExamId)
        prunable.forEach { editor.remove(it) }
        editor.apply()
        return true
    }

    fun getConfigPrefs(context: Context): SharedPreferences {
        return configPrefs ?: synchronized(this) {
            configPrefs ?: createPrefs(context, PREFS_CONFIG).also { configPrefs = it }
        }
    }

    fun getExamPrefs(context: Context): SharedPreferences {
        return examPrefs ?: synchronized(this) {
            examPrefs ?: createPrefs(context, PREFS_EXAM).also { examPrefs = it }
        }
    }

    fun getDevicePrefs(context: Context): SharedPreferences {
        return devicePrefs ?: synchronized(this) {
            devicePrefs ?: createPrefs(context, PREFS_DEVICE).also { devicePrefs = it }
        }
    }

    private fun createPrefs(context: Context, name: String): SharedPreferences {
        val key = getMasterKey(context)
        return EncryptedSharedPreferences.create(
            context,
            name,
            key,
            EncryptedSharedPreferences.PrefKeyEncryptionScheme.AES256_SIV,
            EncryptedSharedPreferences.PrefValueEncryptionScheme.AES256_GCM
        )
    }

    // ── Safe accessors (keystore-corruption resilient) ─────────────────────
    //
    // EncryptedSharedPreferences can throw GeneralSecurityException /
    // IOException when the Android Keystore key is invalidated or the
    // encrypted blob cannot be decrypted. ServerConfigActivity guards this
    // itself, but ExamList/WaitingApproval/ExamViewer read prefs at many
    // points. These accessors fall back to a plain (non-encrypted) prefs file
    // named "<name>_fallback" so the app still runs: the student re-enters
    // their config once and continues working; previously encrypted values are
    // simply gone. A corrupt keystore must never hard-crash the app.

    private val fallbacked = java.util.Collections.newSetFromMap(java.util.concurrent.ConcurrentHashMap<String, Boolean>())

    // ── Exam token (never persisted to the plain fallback) ─────────────────
    //
    // The exam token is a live session credential: it authenticates PDF
    // download, presence and submission. When the keystore is corrupt the
    // readable storage is the PLAINTEXT fallback file, so persisting the token
    // there would leak the credential to any process/backup that can read app
    // data. Instead the token is kept in memory only for the process lifetime
    // while the fallback is active — after a process death on such a device
    // the student simply re-enters the token (same as the other lost values).
    // The token is never READ from nor WRITTEN to the fallback file.

    @Volatile
    private var memoryExamToken: String? = null

    /**
     * Read the exam token. Memory (fallback mode) first, then the encrypted
     * prefs. Never reads the plain fallback file, so a corrupt keystore can
     * never serve a previously persisted plaintext token.
     */
    fun getExamToken(context: Context): String {
        memoryExamToken?.let { return it }
        return try {
            getConfigPrefs(context).getString(KEY_EXAM_TOKEN, "") ?: ""
        } catch (e: GeneralSecurityException) {
            ""
        } catch (e: IOException) {
            ""
        }
    }

    /**
     * Persist the exam token. Encrypted backend when healthy; in-memory only
     * when the keystore is corrupt (fallback active) so the token never lands
     * in the plaintext fallback file.
     */
    fun setExamToken(context: Context, token: String) {
        if (token.isEmpty()) {
            removeExamToken(context)
            return
        }
        if (isConfigFallbackInUse()) {
            memoryExamToken = token
            return
        }
        try {
            getConfigPrefs(context).edit().putString(KEY_EXAM_TOKEN, token).apply()
            memoryExamToken = null // encrypted prefs are the source of truth
        } catch (e: GeneralSecurityException) {
            memoryExamToken = token
        } catch (e: IOException) {
            memoryExamToken = token
        }
    }

    /** Clear the exam token from memory and the encrypted backend (never the fallback). */
    fun removeExamToken(context: Context) {
        memoryExamToken = null
        try {
            getConfigPrefs(context).edit().remove(KEY_EXAM_TOKEN).apply()
        } catch (e: GeneralSecurityException) {
            // nothing persisted — nothing to remove
        } catch (e: IOException) {
            // nothing persisted — nothing to remove
        }
    }

    fun getConfigPrefsSafe(context: Context): SharedPreferences {
        return try {
            getConfigPrefs(context)
        } catch (e: GeneralSecurityException) {
            fallback(context, PREFS_CONFIG, e)
        } catch (e: IOException) {
            fallback(context, PREFS_CONFIG, e)
        }
    }

    fun getExamPrefsSafe(context: Context): SharedPreferences {
        return try {
            getExamPrefs(context)
        } catch (e: GeneralSecurityException) {
            fallback(context, PREFS_EXAM, e)
        } catch (e: IOException) {
            fallback(context, PREFS_EXAM, e)
        }
    }

    fun getDevicePrefsSafe(context: Context): SharedPreferences {
        return try {
            getDevicePrefs(context)
        } catch (e: GeneralSecurityException) {
            fallback(context, PREFS_DEVICE, e)
        } catch (e: IOException) {
            fallback(context, PREFS_DEVICE, e)
        }
    }

    /**
     * True when the encrypted backend for this prefs file was substituted with
     * the plain fallback (i.e. the keystore is corrupt on this device). Callers
     * can use this to warn the user instead of silently losing data.
     */
    fun isFallbackInUse(key: String): Boolean = fallbacked.contains(key)

    /** Convenience: true when the config prefs run on the plain fallback. */
    fun isConfigFallbackInUse(): Boolean = isFallbackInUse(PREFS_CONFIG)

    /** Convenience: true when the exam prefs run on the plain fallback. */
    fun isExamFallbackInUse(): Boolean = isFallbackInUse(PREFS_EXAM)

    /** Convenience: true when the device prefs run on the plain fallback. */
    fun isDeviceFallbackInUse(): Boolean = isFallbackInUse(PREFS_DEVICE)

    private fun fallback(context: Context, prefsName: String, cause: Exception): SharedPreferences {
        Log.w("AppPrefs", "EncryptedSharedPreferences unavailable for $prefsName: ${cause.javaClass.simpleName} — using plain fallback")
        fallbacked.add(prefsName)
        return context.getSharedPreferences(prefsName + "_fallback", Context.MODE_PRIVATE)
    }

    // CATATAN RETENSI IDENTITAS (permintaan pemilik produk): kolom identitas
    // dipertahankan isinya sampai user menekan "Hapus Data Tersimpan"
    // (clearAllData). Tidak ada — dan tidak boleh ada — alur lain yang
    // menghapus KEY_IDENTITY_DATA; dikunci IdentityRetentionGuardTest.

    /**
     * Wipe every saved preference — the encrypted backends AND their plain
     * fallback copies. The fallback files are cleared unconditionally: when
     * the keystore is corrupt the encrypted prefs throw, but the readable
     * data actually lives in the fallback files and must not be left behind.
     *
     * @return true when at least one backend (encrypted or fallback) was
     *         cleared; the fallback clear cannot realistically fail, so this
     *         is effectively always true.
     */
    fun clearAllData(context: Context): Boolean {
        memoryExamToken = null
        var clearedAny = false
        try {
            getConfigPrefs(context).edit().clear().apply()
            getExamPrefs(context).edit().clear().apply()
            getDevicePrefs(context).edit().clear().apply()
            clearedAny = true
        } catch (e: GeneralSecurityException) {
            // Keystore corrupt — fallback di bawah tetap dibersihkan.
        } catch (e: IOException) {
            // Keystore corrupt — fallback di bawah tetap dibersihkan.
        }
        listOf(PREFS_CONFIG, PREFS_EXAM, PREFS_DEVICE).forEach { name ->
            context.getSharedPreferences(name + "_fallback", Context.MODE_PRIVATE)
                .edit().clear().apply()
            clearedAny = true
        }
        return clearedAny
    }
}
