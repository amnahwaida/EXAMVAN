package com.examvan.app.receiver

import android.app.admin.DeviceAdminReceiver
import android.content.Context
import android.content.Intent

/**
 * DeviceAdminReceiver to support Android Enterprise Device Owner mode.
 * When enabled via ADB or MDM, the app can lock the screen completely
 * without allowing the user to unpin manually (Managed LockTask Mode).
 */
class MyDeviceAdminReceiver : DeviceAdminReceiver() {

    override fun onEnabled(context: Context, intent: Intent) {
        super.onEnabled(context, intent)
        // Toast konfirmasi hanya saat AKTIF — berguna bagi admin sekolah
        // saat setup kiosk. Penonaktifan tidak diberi toast (fix review
        // strict ronde 2 #3): perubahan admin di perangkat sekolah jarang
        // dan bukan sesuatu yang perlu diumumkan ke siswa.
    }
}
