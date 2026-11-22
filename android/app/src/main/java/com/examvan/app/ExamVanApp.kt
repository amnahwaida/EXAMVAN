package com.examvan.app

import android.app.Application
import android.util.Log
import com.examvan.app.helper.PdfCache

/**
 * Application entry point.
 *
 * Tugas di background thread (tidak memblokir main thread — penting di
 * perangkat Android 10 RAM kecil):
 *  1. memanaskan backend EncryptedSharedPreferences;
 *  2. membersihkan cache PDF basi / file temp unduhan yang terputus.
 */
class ExamVanApp : Application() {
    override fun onCreate() {
        super.onCreate()
        Thread {
            AppPrefs.prewarm(this)
            try {
                val removed = PdfCache.pruneStale(cacheDir, System.currentTimeMillis())
                if (removed > 0) {
                    Log.i("ExamVanApp", "Cache PDF basi dibersihkan: $removed file")
                }
            } catch (_: Exception) {
                // Cache bukan hal kritis — jangan pernah menggagalkan startup.
            }
        }.apply {
            name = "Startup-maintenance"
            priority = Thread.MIN_PRIORITY
            isDaemon = true
        }.start()
    }
}
