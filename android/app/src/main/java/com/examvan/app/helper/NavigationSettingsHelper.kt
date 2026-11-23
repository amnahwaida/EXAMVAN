package com.examvan.app.helper

import android.app.Activity
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import android.provider.Settings
import java.util.Locale

/**
 * Membuka halaman navigasi sistem sedekat mungkin dengan setting yang
 * dimaksud. Tidak ada satu intent resmi yang bekerja di semua OEM:
 *
 * - Android 11+ punya `android.settings.SYSTEM_NAVIGATION_SETTINGS`.
 * - Android 10 ke bawah harus lewat activity Settings milik OEM.
 * - Beberapa OEM mengganti nama activity/fragment-nya.
 *
 * Karena itu helper ini mencoba beberapa kandidat berurutan dan selalu
 * menyediakan fallback ke Settings umum + teks path manual per merek.
 */
object NavigationSettingsHelper {

    private const val ACTION_SYSTEM_NAVIGATION_SETTINGS =
        "android.settings.SYSTEM_NAVIGATION_SETTINGS"
    private const val SETTINGS_FRAGMENT_EXTRA = ":settings:show_fragment"
    private const val AOSP_SYSTEM_NAVIGATION_FRAGMENT =
        "com.android.settings.gestures.SystemNavigationGestureSettings"

    private val NAVIGATION_KEYWORDS = listOf(
        "systemnavigation",
        "navigationbar",
        "fullscreendisplay",
        "navigationmode",
        "navigationgesture"
    )

    /**
     * Hasil discovery kecil (hanya ComponentName) dan dipakai berulang saat
     * siswa menekan tombol. Cache ini mencegah PackageManager dipanggil
     * berkali-kali di perangkat low-memory.
     */
    @Volatile
    private var discoveredComponentsCache: List<ComponentName>? = null

    /**
     * Path manual yang ditampilkan di layar blokir. Ini penting sebagai
     * jaring pengaman bila intent OEM tidak bisa dibuka (mis. activity tidak
     * exported atau vendor mengganti nama menu).
     */
    fun instructionText(): String =
        instructionTextFor(Build.MANUFACTURER, Build.BRAND)

    internal fun instructionTextFor(manufacturer: String?, brand: String?): String {
        val id = "${manufacturer.orEmpty()} ${brand.orEmpty()}".lowercase(Locale.ROOT)
        return when {
            id.contains("samsung") ->
                "Settings > Display > Navigation bar > Buttons"
            id.contains("xiaomi") || id.contains("redmi") || id.contains("poco") ->
                "Settings > Home screen / Display > System navigation > Buttons"
            id.contains("oppo") || id.contains("realme") || id.contains("oneplus") ->
                "Settings > Additional settings / System settings > System navigation > 3-button navigation"
            id.contains("vivo") || id.contains("iqoo") ->
                "Settings > System navigation > 3-button navigation"
            id.contains("huawei") || id.contains("honor") ->
                "Settings > System & updates > System navigation > Three-key navigation"
            id.contains("asus") ->
                "Settings > System > Gestures > System navigation > 3-button"
            id.contains("infinix") || id.contains("tecno") || id.contains("itel") ->
                "Settings > System > Navigation > 3-button navigation"
            id.contains("motorola") || id.contains("nokia") ->
                "Settings > System > Gestures > System navigation > 3-button"
            else ->
                "Settings > System > Gestures > System navigation > 3-button navigation"
        }
    }

    /**
     * Coba buka halaman navigasi. Return true bila salah satu intent berhasil
     * diluncurkan; false berarti pemanggil harus menampilkan instruksi manual.
     */
    fun open(activity: Activity): Boolean {
        // 1. Action resmi Android 11+ (paling aman di Pixel/AOSP dan OEM modern).
        if (tryStart(activity, Intent(ACTION_SYSTEM_NAVIGATION_SETTINGS))) return true

        // 2. Activity Settings yang umum di OEM tertentu (Android 10 ke bawah).
        knownComponents().forEach { component ->
            if (tryStart(activity, Intent().setComponent(component))) return true
        }

        // 3. Temukan activity Settings yang namanya mengandung "navigation".
        //    Discovery hanya dijalankan bila kandidat di atas gagal, sehingga
        //    perangkat modern tidak membayar biaya PackageManager sama sekali.
        discoverComponents(activity).forEach { component ->
            if (tryStart(activity, Intent().setComponent(component))) return true
        }

        // 4. Fragment AOSP/Pixel.
        val aospFragment = Intent(Settings.ACTION_SETTINGS)
            .putExtra(SETTINGS_FRAGMENT_EXTRA, AOSP_SYSTEM_NAVIGATION_FRAGMENT)
        if (tryStart(activity, aospFragment)) return true

        // 5. Jaring pengaman terakhir: Settings umum.
        return tryStart(activity, Intent(Settings.ACTION_SETTINGS))
    }

    private fun tryStart(activity: Activity, intent: Intent): Boolean {
        val candidate = intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        if (candidate.resolveActivity(activity.packageManager) == null) return false
        return try {
            activity.startActivity(candidate)
            true
        } catch (_: Exception) {
            false
        }
    }

    private fun knownComponents(): List<ComponentName> = listOf(
        // Samsung
        ComponentName(
            "com.android.settings",
            "com.android.settings.Settings\$NavigationBarSettingsActivity"
        ),
        // AOSP / Pixel
        ComponentName(
            "com.android.settings",
            "com.android.settings.Settings\$NavigationModeSettingsActivity"
        ),
        // Xiaomi / MIUI / HyperOS
        ComponentName(
            "com.android.settings",
            "com.android.settings.Settings\$FullScreenDisplayActivity"
        ),
        ComponentName(
            "com.android.settings",
            "com.android.settings.display.FullScreenDisplayActivity"
        )
    )

    private fun discoverComponents(context: Context): List<ComponentName> {
        discoveredComponentsCache?.let { return it }

        val packageManager = context.packageManager
        val found = candidateSettingsPackages(context).flatMap { packageName ->
            try {
                @Suppress("DEPRECATION")
                val packageInfo = packageManager.getPackageInfo(
                    packageName,
                    PackageManager.GET_ACTIVITIES
                )
                packageInfo.activities.orEmpty()
                    .asSequence()
                    .filter { matchesNavigation(it.name) }
                    .sortedByDescending { navigationScore(it.name) }
                    .take(3)
                    .map { ComponentName(packageName, it.name) }
                    .toList()
            } catch (_: Exception) {
                emptyList()
            }
        }.distinct()

        discoveredComponentsCache = found
        return found
    }

    /**
     * Hanya query package Settings utama + package vendor yang relevan.
     * Sebelumnya 12 package di-query; di perangkat low-memory itu boros dan
     * memblokir main thread saat tombol ditekan.
     */
    private fun candidateSettingsPackages(context: Context): List<String> {
        val packages = linkedSetOf<String>()
        val settingsIntent = Intent(Settings.ACTION_SETTINGS)
        @Suppress("DEPRECATION")
        val resolvedPackage = context.packageManager
            .resolveActivity(settingsIntent, 0)
            ?.activityInfo
            ?.packageName
        if (resolvedPackage != null) packages += resolvedPackage

        val id = "${Build.MANUFACTURER} ${Build.BRAND}".lowercase(Locale.ROOT)
        when {
            id.contains("samsung") -> packages += "com.android.settings"
            id.contains("xiaomi") || id.contains("redmi") || id.contains("poco") -> {
                packages += "com.android.settings"
                packages += "com.xiaomi.misettings"
            }
            id.contains("oppo") || id.contains("realme") || id.contains("oneplus") -> {
                packages += "com.coloros.settings"
                packages += "com.oplus.settings"
            }
            id.contains("vivo") || id.contains("iqoo") -> packages += "com.vivo.settings"
            id.contains("huawei") -> packages += "com.huawei.settings"
            id.contains("honor") -> packages += "com.hihonor.settings"
            id.contains("asus") -> packages += "com.asus.settings"
            id.contains("infinix") || id.contains("tecno") || id.contains("itel") ->
                packages += "com.transsion.settings"
            id.contains("motorola") -> packages += "com.motorola.settings"
            id.contains("nokia") -> packages += "com.nokia.settings"
        }
        packages += "com.android.settings"
        return packages.toList()
    }

    private fun matchesNavigation(activityName: String): Boolean {
        val name = activityName.lowercase(Locale.ROOT)
        if (name.contains("buttonorder")) return false
        return NAVIGATION_KEYWORDS.any { name.contains(it) }
    }

    private fun navigationScore(activityName: String): Int {
        val name = activityName.lowercase(Locale.ROOT)
        var score = 0
        if (name.contains("systemnavigation")) score += 5
        if (name.contains("navigationbar")) score += 4
        if (name.contains("fullscreendisplay")) score += 4
        if (name.contains("navigationmode")) score += 3
        if (name.contains("navigationgesture")) score += 3
        return score
    }
}
