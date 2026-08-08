package com.examvan.app.helper

import android.app.Activity
import android.content.Intent
import android.net.Uri
import androidx.appcompat.app.AlertDialog
import com.examvan.app.R

/**
 * Centralized app-update handling.
 *
 * EXAMVAN ships as a sideloaded APK (no Play Store), so when the server
 * raises the required version (saas_settings android_version) the app must
 * block the student and point them at the server's download page.
 */
object UpdateManager {

    /**
     * Compare two version strings (major.minor.patch). Returns true when
     * [appVersion] is older than [requiredVersion] and an update is mandatory.
     * Non-numeric suffixes like "-beta" are ignored.
     */
    fun isOutdated(appVersion: String, requiredVersion: String): Boolean {
        return compareVersions(appVersion, requiredVersion) < 0
    }

    /**
     * -1 when a < b, 0 when equal, 1 when a > b. Falls back to string
     * comparison when either version cannot be parsed numerically.
     */
    private fun compareVersions(a: String, b: String): Int {
        return try {
            val aParts = parseVersion(a)
            val bParts = parseVersion(b)
            val length = maxOf(aParts.size, bParts.size)
            for (i in 0 until length) {
                val aPart = aParts.getOrElse(i) { 0 }
                val bPart = bParts.getOrElse(i) { 0 }
                if (aPart > bPart) return 1
                if (aPart < bPart) return -1
            }
            0
        } catch (e: Exception) {
            a.compareTo(b)
        }
    }

    private fun parseVersion(v: String): List<Int> {
        return v.split(".").map { seg ->
            // Take leading digits only (discard non-numeric suffix like "-beta")
            val digits = seg.takeWhile { it.isDigit() }
            if (digits.isEmpty()) 0 else digits.toInt()
        }
    }

    /**
     * Show a non-cancellable blocking dialog that forces the student to
     * update: "Buka Halaman Download" opens the server's /download page,
     * "Keluar" closes the app. Replaces the old passive error text so the
     * student cannot start an exam with an outdated APK.
     *
     * [requiredVersion] may be null when the server rejected the request with
     * HTTP 426 but the required version is unknown (falls back to a generic
     * message without the version line).
     */
    fun showUpdateRequiredDialog(
        activity: Activity,
        currentVersion: String,
        requiredVersion: String?,
        serverUrl: String
    ) {
        val message = if (requiredVersion.isNullOrBlank()) {
            activity.getString(R.string.update_required_message_generic)
        } else {
            activity.getString(R.string.update_required_message, currentVersion, requiredVersion)
        }
        val builder = AlertDialog.Builder(activity)
            .setTitle(activity.getString(R.string.update_required_title))
            .setMessage(message)
            .setCancelable(false)

        // "Keluar" — closes the app
        builder.setNegativeButton(activity.getString(R.string.update_required_exit)) { dialog, _ ->
            dialog.dismiss()
            activity.finishAffinity()
        }

        // "Buka Halaman Download" — opens the server download page
        val downloadUrl = serverUrl.trimEnd('/') + "/download"
        builder.setPositiveButton(activity.getString(R.string.update_required_action)) { _, _ ->
            if (serverUrl.isBlank()) {
                // No server URL resolved (e.g. config not yet saved): there is
                // nothing meaningful to open. Keep the dialog blocking so the
                // student can only exit, and surface the URL we would use.
                android.widget.Toast.makeText(
                    activity,
                    activity.getString(R.string.update_required_url_missing),
                    android.widget.Toast.LENGTH_LONG
                ).show()
            } else {
                openDownloadPage(activity, downloadUrl)
            }
        }

        builder.show()
    }

    /**
     * Open the server download page in the browser. Mirrors the pattern used
     * by CongratulationsActivity for the results link.
     */
    private fun openDownloadPage(activity: Activity, url: String) {
        try {
            val intent = Intent(Intent.ACTION_VIEW, Uri.parse(url))
                .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
            activity.startActivity(intent)
        } catch (_: Exception) {
            // No browser available — fall back to a plain toast.
            android.widget.Toast.makeText(
                activity,
                url,
                android.widget.Toast.LENGTH_LONG
            ).show()
        }
    }
}
