package com.examvan.app

import android.app.Application

/**
 * Application entry point.
 *
 * Satu-satunya tugas: memanaskan backend EncryptedSharedPreferences di
 * background thread agar pembukaan pertama di Activity tidak memblokir main
 * thread (terasa sebagai jank/ANR di perangkat Android 10 RAM kecil).
 */
class ExamVanApp : Application() {
    override fun onCreate() {
        super.onCreate()
        Thread {
            AppPrefs.prewarm(this)
        }.apply {
            name = "AppPrefs-prewarm"
            priority = Thread.MIN_PRIORITY
            isDaemon = true
        }.start()
    }
}
