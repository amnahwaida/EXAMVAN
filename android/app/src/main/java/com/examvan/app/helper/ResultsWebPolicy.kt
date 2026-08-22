package com.examvan.app.helper

import java.net.URI

/**
 * Kebijakan navigasi WebView halaman hasil (fix temuan review pasca-submit
 * #1): navigasi ke host yang sama dengan URL hasil tetap di dalam sandbox
 * app (FLAG_SECURE aktif); host lain dibuka di browser eksternal agar
 * WebView tidak menjadi jendela bebas ke internet.
 */
object ResultsWebPolicy {

    /**
     * True bila [targetUrl] berada di host yang PERSIS sama dengan
     * [initialUrl] (skema/host dibandingkan; subdomain dianggap berbeda).
     * URL rusak/null diperlakukan sebagai host berbeda (sisi aman).
     */
    fun isSameHost(initialUrl: String?, targetUrl: String?): Boolean {
        val initialHost = hostOf(initialUrl) ?: return false
        val targetHost = hostOf(targetUrl) ?: return false
        return initialHost.equals(targetHost, ignoreCase = true)
    }

    private fun hostOf(url: String?): String? = try {
        if (url.isNullOrBlank()) null else URI(url).host?.lowercase()
    } catch (_: Exception) {
        null
    }
}
