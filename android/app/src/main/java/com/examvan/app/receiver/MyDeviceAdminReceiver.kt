package com.examvan.app.receiver

import android.app.admin.DeviceAdminReceiver
import android.content.Context
import android.content.Intent
import android.widget.Toast

/**
 * DeviceAdminReceiver to support Android Enterprise Device Owner mode.
 * When enabled via ADB or MDM, the app can lock the screen completely
 * without allowing the user to unpin manually (Managed LockTask Mode).
 */
class MyDeviceAdminReceiver : DeviceAdminReceiver() {

    override fun onEnabled(context: Context, intent: Intent) {
        super.onEnabled(context, intent)
        Toast.makeText(context, "EXAMVAN Administrator Aktif", Toast.LENGTH_SHORT).show()
    }

    override fun onDisabled(context: Context, intent: Intent) {
        super.onDisabled(context, intent)
        Toast.makeText(context, "EXAMVAN Administrator Nonaktif", Toast.LENGTH_SHORT).show()
    }
}
