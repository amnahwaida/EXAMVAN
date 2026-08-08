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

    private fun fallback(context: Context, prefsName: String, cause: Exception): SharedPreferences {
        Log.w("AppPrefs", "EncryptedSharedPreferences unavailable for $prefsName: ${cause.javaClass.simpleName} — using plain fallback")
        fallbacked.add(prefsName)
        return context.getSharedPreferences(prefsName + "_fallback", Context.MODE_PRIVATE)
    }

    fun clearIdentityData(context: Context) {
        getConfigPrefs(context).edit()
            .remove(KEY_IDENTITY_DATA)
            .apply()
    }
}
