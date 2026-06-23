package com.examvan.app

import android.content.Context
import java.util.UUID

object DeviceIdResolver {
    fun resolveDeviceId(context: Context): String {
        val prefs = AppPrefs.getDevicePrefs(context)
        var deviceId = prefs.getString(AppPrefs.KEY_DEVICE_UUID, null)
        if (deviceId.isNullOrBlank()) {
            deviceId = UUID.randomUUID().toString()
            prefs.edit().putString(AppPrefs.KEY_DEVICE_UUID, deviceId).apply()
        }
        return "DEVICE:$deviceId"
    }
}
