package com.examvan.app.helper

import org.junit.Assert.assertEquals
import org.junit.Test

/**
 * Mengunci kontrak klasifikasi accessibility service (fix temuan review:
 * "accessibility check hanya Log.w, tidak pernah memberi peringatan ke user
 * padahal komentar bilang tampilkan peringatan").
 *
 * BaseSecureActivity memakai policy ini: service SUSPICIOUS → peringatan
 * dialog + AuditLog; service WHITELISTED / BENIGN → diam.
 *
 *   ./gradlew :app:testStudentDebugUnitTest
 */
class AccessibilityThreatPolicyTest {

    // ── Whitelist layanan aksesibilitas legitimate bawaan sistem ─────────

    @Test
    fun classify_talkback_whitelistedEvenWithFullCapabilities() {
        // TalkBack punya canRetrieveWindowContent — tetap di-whitelist agar
        // siswa disabilitas netra bisa mengikuti ujian.
        val threat = AccessibilityThreatPolicy.classify(
            serviceId = "com.google.android.marvin.talkback/com.google.android.marvin.talkback.TalkBackService",
            canRetrieveWindowContent = true,
            canPerformGestures = true
        )
        assertEquals(AccessibilityThreatPolicy.Threat.WHITELISTED, threat)
    }

    @Test
    fun classify_selectToSpeak_whitelisted() {
        val threat = AccessibilityThreatPolicy.classify(
            serviceId = "com.google.android.accessibility.selecttospeak/...SelectToSpeakService",
            canRetrieveWindowContent = true,
            canPerformGestures = false
        )
        assertEquals(AccessibilityThreatPolicy.Threat.WHITELISTED, threat)
    }

    @Test
    fun classify_switchAccess_whitelisted_caseInsensitive() {
        val threat = AccessibilityThreatPolicy.classify(
            serviceId = "com.something/SWITCHACCESS Service",
            canRetrieveWindowContent = true,
            canPerformGestures = true
        )
        assertEquals(AccessibilityThreatPolicy.Threat.WHITELISTED, threat)
    }

    // ── Layanan mencurigakan ────────────────────────────────────────────

    @Test
    fun classify_unknownServiceReadingScreen_suspicious() {
        // Bisa membaca konten layar (= soal ujian) tapi bukan whitelist.
        val threat = AccessibilityThreatPolicy.classify(
            serviceId = "com.evil.spy/com.evil.spy.ReaderService",
            canRetrieveWindowContent = true,
            canPerformGestures = false
        )
        assertEquals(AccessibilityThreatPolicy.Threat.SUSPICIOUS, threat)
    }

    @Test
    fun classify_unknownServiceWithGestureInjection_suspicious() {
        // Gesture injection tanpa membaca layar juga berbahaya (bisa menekan
        // tombol submit / membuka app lain).
        val threat = AccessibilityThreatPolicy.classify(
            serviceId = "com.auto.clicker/AutoClickService",
            canRetrieveWindowContent = false,
            canPerformGestures = true
        )
        assertEquals(AccessibilityThreatPolicy.Threat.SUSPICIOUS, threat)
    }

    @Test
    fun classify_passwordManagerAutofill_suspicious() {
        // Contoh nyata: password manager dengan aksesibilitas aktif bisa
        // membaca layar — harus diberi peringatan walau intent-nya baik.
        val threat = AccessibilityThreatPolicy.classify(
            serviceId = "com.passwordmanager/com.passwordmanager.AutofillAccessibility",
            canRetrieveWindowContent = true,
            canPerformGestures = false
        )
        assertEquals(AccessibilityThreatPolicy.Threat.SUSPICIOUS, threat)
    }

    // ── Layanan aman ────────────────────────────────────────────────────

    @Test
    fun classify_unknownServiceWithoutDangerousCapabilities_benign() {
        val threat = AccessibilityThreatPolicy.classify(
            serviceId = "com.example.simple/NotificationReader",
            canRetrieveWindowContent = false,
            canPerformGestures = false
        )
        assertEquals(AccessibilityThreatPolicy.Threat.BENIGN, threat)
    }

    @Test
    fun classify_nullOrBlankServiceId_benign() {
        assertEquals(
            AccessibilityThreatPolicy.Threat.BENIGN,
            AccessibilityThreatPolicy.classify(null, true, true)
        )
        assertEquals(
            AccessibilityThreatPolicy.Threat.BENIGN,
            AccessibilityThreatPolicy.classify("", true, true)
        )
    }
}
