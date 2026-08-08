package com.examvan.app

import android.content.Context
import android.provider.Settings
import java.util.UUID

object DeviceIdResolver {
    fun resolveDeviceId(context: Context): String {
        // Ambil Android ID (persisten meskipun clear data atau reinstall)
        var androidId = Settings.Secure.getString(context.contentResolver, Settings.Secure.ANDROID_ID)
        
        // Pengecekan jika null, kosong, atau bernilai "9774d56d682e549c" (bug emulator/perangkat tertentu)
        if (androidId.isNullOrBlank() || androidId.equals("9774d56d682e549c", ignoreCase = true)) {
            // Gunakan SharedPreferences sebagai fallback jika Android ID tidak tersedia
            val prefs = AppPrefs.getDevicePrefsSafe(context)
            var deviceId = prefs.getString(AppPrefs.KEY_DEVICE_UUID, null)
            if (deviceId.isNullOrBlank()) {
                deviceId = UUID.randomUUID().toString()
                prefs.edit().putString(AppPrefs.KEY_DEVICE_UUID, deviceId).apply()
            }
            androidId = deviceId
        }
        
        return "DEVICE:$androidId"
    }
}
