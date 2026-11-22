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
    // Data sesi aktif untuk auto-resume saat proses/task dibunuh OEM.
    // Token sesi dipisah dari KEY_EXAM_TOKEN (fitur "ingat token") supaya
    // auto-resume tetap bekerja walau siswa tidak mencentang "Ingat URL".
    const val KEY_EXAM_NAME = "exam_name"
    const val KEY_END_TIME = "end_time"
    const val KEY_ACTIVE_EXAM_TOKEN = "active_exam_token"
    // Exam ID + server URL sesi aktif. Disimpan terpisah dari config prefs
    // karena keduanya dihapus saat siswa tidak mencentang "Ingat URL" —
    // tanpa ini auto-resume tidak tahu harus membuka ujian yang mana.
    const val KEY_ACTIVE_EXAM_ID = "active_exam_id"
    const val KEY_ACTIVE_SERVER_URL = "active_server_url"
    // Set saat ExamViewer dihancurkan sistem (bukan finish) dalam sesi aktif.
    // ServerConfigActivity memakainya untuk auto-resume di onResume, karena
    // activity token bisa hanya di-resume dari back stack (onCreate tak jalan).
    const val KEY_AUTO_RESUME_PENDING = "auto_resume_pending"

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

    /**
     * Hapus penanda lokal "sudah submit/keluar" untuk [examId] saat pengawas
     * menyetujui attempt baru (termasuk repeat). Penanda ini hanya menjaga
     * re-entry ke attempt yang sudah selesai; server tetap menjadi sumber
     * kebenaran gerbang repeat.
     */
    fun clearSubmittedState(context: Context, examId: Int) {
        if (examId <= 0) return
        getExamPrefsSafe(context).edit()
            .remove(getSubmittedOrExitedKey(examId))
            .apply()
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
        val editor = prefs.edit()
            .remove(KEY_EXAM_START_TIME)
            .remove(KEY_EXAM_NAME)
            .remove(KEY_END_TIME)
            .remove(KEY_ACTIVE_EXAM_TOKEN)
            .remove(KEY_ACTIVE_EXAM_ID)
            .remove(KEY_ACTIVE_SERVER_URL)
            .remove(KEY_AUTO_RESUME_PENDING)
        val prunable = com.examvan.app.helper.ExamSessionResetPolicy
            .prunableSubmittedFlagKeys(prefs.all.keys, keepExamId = newExamId)
        prunable.forEach { editor.remove(it) }
        editor.apply()
        return true
    }

    /**
     * Warm up the encrypted prefs backends OFF the main thread (dipanggil dari
     * Application.onCreate). Membuka EncryptedSharedPreferences berarti disk
     * I/O + operasi Android Keystore; di perangkat Android 10 kelas bawah ini
     * bisa memakan ratusan milidetik. Dengan prewarm, pembacaan pertama di
     * Activity biasanya sudah memakai instance yang siap.
     */
    fun prewarm(context: Context) {
        try { getConfigPrefs(context) } catch (_: Exception) { }
        try { getExamPrefs(context) } catch (_: Exception) { }
        try { getDevicePrefs(context) } catch (_: Exception) { }
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

    // Token sesi ujian yang sedang berjalan. Disimpan terenkripsi di exam
    // prefs (bukan config prefs) dan hanya selama sesi aktif; dihapus saat
    // submit sukses. Fallback keystore corrupt tetap memory-only.
    @Volatile
    private var memoryActiveExamToken: String? = null

    /**
     * Read the exam token. Memory (fallback mode) first, then the encrypted
     * prefs. Never reads the plain fallback file, so a corrupt keystore can
     * never serve a previously persisted plaintext token.
     */
    fun getExamToken(context: Context): String {
        memoryExamToken?.let {
            Log.i("AppPrefs", "getExamToken: memory len=${it.length}")
            return it
        }
        return try {
            val value = getConfigPrefs(context).getString(KEY_EXAM_TOKEN, "") ?: ""
            Log.i(
                "AppPrefs",
                "getExamToken: encrypted len=${value.length} fallback=${isConfigFallbackInUse()}"
            )
            value
        } catch (e: GeneralSecurityException) {
            Log.w("AppPrefs", "getExamToken failed: ${e.javaClass.simpleName}")
            ""
        } catch (e: IOException) {
            Log.w("AppPrefs", "getExamToken failed: ${e.javaClass.simpleName}")
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
            Log.w("AppPrefs", "setExamToken: fallback active, memory-only len=${token.length}")
            return
        }
        try {
            // commit() (bukan apply) agar token benar-benar durable sebelum
            // ujian berjalan — proses bisa dibunuh OEM kapan saja.
            val ok = getConfigPrefs(context).edit()
                .putString(KEY_EXAM_TOKEN, token)
                .commit()
            memoryExamToken = null // encrypted prefs are the source of truth
            Log.i("AppPrefs", "setExamToken: committed=$ok len=${token.length}")
        } catch (e: GeneralSecurityException) {
            memoryExamToken = token
            Log.w("AppPrefs", "setExamToken failed: ${e.javaClass.simpleName} — memory-only")
        } catch (e: IOException) {
            memoryExamToken = token
            Log.w("AppPrefs", "setExamToken failed: ${e.javaClass.simpleName} — memory-only")
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

    // ── Active exam session token (auto-resume) ────────────────────────────
    //
    // The remembered token (KEY_EXAM_TOKEN) is optional: a student may
    // uncheck "Ingat URL". Auto-resume still needs the session token while an
    // exam is in progress, so it is stored separately in the encrypted exam
    // prefs and cleared as soon as the submission succeeds. Never persisted to
    // the plaintext fallback (same policy as KEY_EXAM_TOKEN).

    fun getActiveExamToken(context: Context): String {
        memoryActiveExamToken?.let {
            Log.i("AppPrefs", "getActiveExamToken: memory len=${it.length}")
            return it
        }
        return try {
            val value = getExamPrefs(context).getString(KEY_ACTIVE_EXAM_TOKEN, "") ?: ""
            Log.i(
                "AppPrefs",
                "getActiveExamToken: encrypted len=${value.length} fallback=${isExamFallbackInUse()}"
            )
            value
        } catch (e: GeneralSecurityException) {
            Log.w("AppPrefs", "getActiveExamToken failed: ${e.javaClass.simpleName}")
            ""
        } catch (e: IOException) {
            Log.w("AppPrefs", "getActiveExamToken failed: ${e.javaClass.simpleName}")
            ""
        }
    }

    fun setActiveExamToken(context: Context, token: String) {
        if (token.isEmpty()) {
            removeActiveExamToken(context)
            return
        }
        if (isExamFallbackInUse()) {
            memoryActiveExamToken = token
            Log.w("AppPrefs", "setActiveExamToken: fallback active, memory-only len=${token.length}")
            return
        }
        try {
            val ok = getExamPrefs(context).edit()
                .putString(KEY_ACTIVE_EXAM_TOKEN, token)
                .commit()
            memoryActiveExamToken = null
            Log.i("AppPrefs", "setActiveExamToken: committed=$ok len=${token.length}")
        } catch (e: GeneralSecurityException) {
            memoryActiveExamToken = token
            Log.w("AppPrefs", "setActiveExamToken failed: ${e.javaClass.simpleName} — memory-only")
        } catch (e: IOException) {
            memoryActiveExamToken = token
            Log.w("AppPrefs", "setActiveExamToken failed: ${e.javaClass.simpleName} — memory-only")
        }
    }

    fun removeActiveExamToken(context: Context) {
        memoryActiveExamToken = null
        try {
            getExamPrefs(context).edit().remove(KEY_ACTIVE_EXAM_TOKEN).apply()
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
        memoryActiveExamToken = null
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
