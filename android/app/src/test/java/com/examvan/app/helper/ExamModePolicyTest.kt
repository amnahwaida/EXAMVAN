package com.examvan.app.helper

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Mengunci perbaikan review mode ujian (16 Agustus 2026).
 *
 * Kunci utama: mode LOW TIDAK auto-submit saat fokus hilang — sebelumnya
 * `onWindowFocusChanged` hanya cek `!strictMode`, sehingga siswa low yang
 * membuka laci notifikasi / berpindah aplikasi ikut ter-submit (melanggar
 * kontrak low: "bebas keluar masuk tanpa konsekuensi").
 *
 *   ./gradlew :app:testStudentDebugUnitTest
 */
class ExamModePolicyTest {

    // ── Auto-submit saat fokus hilang ────────────────────────────────────

    @Test
    fun focusLoss_lowMode_doesNotAutoSubmit() {
        assertFalse(
            "LOW harus bebas keluar-masuk — TIDAK boleh auto-submit saat fokus hilang",
            ExamModePolicy.shouldAutoSubmitOnFocusLoss(
                securityLevel = ExamModePolicy.LEVEL_LOW, strictMode = false
            )
        )
    }

    @Test
    fun focusLoss_mediumMode_autoSubmits() {
        assertTrue(
            "MEDIUM harus auto-submit saat fokus hilang",
            ExamModePolicy.shouldAutoSubmitOnFocusLoss(
                securityLevel = ExamModePolicy.LEVEL_MEDIUM, strictMode = false
            )
        )
    }

    @Test
    fun focusLoss_strictMode_neverAutoSubmits() {
        assertFalse(
            "STRICT tidak boleh auto-submit saat fokus hilang (lock task pin lah keamanannya)",
            ExamModePolicy.shouldAutoSubmitOnFocusLoss(
                securityLevel = ExamModePolicy.LEVEL_LOW, strictMode = true
            )
        )
        assertFalse(
            ExamModePolicy.shouldAutoSubmitOnFocusLoss(
                securityLevel = ExamModePolicy.LEVEL_MEDIUM, strictMode = true
            )
        )
    }

    @Test
    fun focusLoss_unknownLevel_defaultsToMediumBehavior() {
        // Level tidak dikenal (kosong/rusak) → fail-closed ke perilaku medium.
        assertTrue(
            ExamModePolicy.shouldAutoSubmitOnFocusLoss(securityLevel = "", strictMode = false)
        )
    }

    // ── Intercept tombol volume (fix review low-mode ronde 3: strict-aware) ─

    @Test
    fun volumeKey_lowMode_usesSystemDefault() {
        assertFalse(
            "LOW non-strict: tombol volume dibiarkan sistem (panel volume normal)",
            ExamModePolicy.shouldInterceptVolumeKeys(
                securityLevel = ExamModePolicy.LEVEL_LOW, strictMode = false
            )
        )
    }

    @Test
    fun volumeKey_mediumAndStrict_intercepted() {
        assertTrue(
            ExamModePolicy.shouldInterceptVolumeKeys(
                securityLevel = ExamModePolicy.LEVEL_MEDIUM, strictMode = false
            )
        )
        assertTrue(
            ExamModePolicy.shouldInterceptVolumeKeys(
                securityLevel = "strict", strictMode = false
            )
        )
    }

    @Test
    fun volumeKey_lowWithStrictMode_intercepted() {
        // FIX temuan review low-mode ronde 3 #1: server bisa mengirim
        // security_level="low" + strict_mode=true — exam ter-pin via lock
        // task, panel volume TIDAK boleh jadi jalur keluar/bypass.
        // Dulu kebijakan hanya membaca level → low+strict bocor.
        assertTrue(
            ExamModePolicy.shouldInterceptVolumeKeys(
                securityLevel = ExamModePolicy.LEVEL_LOW, strictMode = true
            )
        )
    }

    @Test
    fun volumeKey_decision_consistentBetweenShortAndLongPress() {
        // FIX temuan review low-mode ronde 3 #2: dulu onKeyLongPress selalu
        // mengonsumsi long-press tanpa kebijakan — di low single-press normal
        // tapi long-press mati diam-diam. Kontrak baru: keduanya memakai
        // keputusan yang SAMA.
        for (level in listOf(ExamModePolicy.LEVEL_LOW, ExamModePolicy.LEVEL_MEDIUM, "")) {
            for (strict in listOf(true, false)) {
                assertEquals(
                    "Keputusan short vs long press harus identik untuk level=$level strict=$strict",
                    ExamModePolicy.shouldInterceptVolumeKeys(level, strict),
                    ExamModePolicy.shouldInterceptVolumeLongPress(level, strict)
                )
            }
        }
    }

    @Test
    fun volumeLongPress_lowNonStrict_passesThroughToSystem() {
        assertFalse(
            ExamModePolicy.shouldInterceptVolumeLongPress(
                securityLevel = ExamModePolicy.LEVEL_LOW, strictMode = false
            )
        )
    }

    // ── Auto-submit saat user leave / Home-Recents (fix temuan review #2) ─

    @Test
    fun userLeave_mediumMode_autoSubmits() {
        assertTrue(
            ExamModePolicy.shouldAutoSubmitOnUserLeave(
                securityLevel = ExamModePolicy.LEVEL_MEDIUM, strictMode = false
            )
        )
    }

    @Test
    fun userLeave_lowMode_doesNotAutoSubmit() {
        assertFalse(
            ExamModePolicy.shouldAutoSubmitOnUserLeave(
                securityLevel = ExamModePolicy.LEVEL_LOW, strictMode = false
            )
        )
    }

    @Test
    fun userLeave_strictMode_neverAutoSubmits() {
        assertFalse(
            ExamModePolicy.shouldAutoSubmitOnUserLeave(
                securityLevel = ExamModePolicy.LEVEL_MEDIUM, strictMode = true
            )
        )
    }

    @Test
    fun userLeave_unknownLevel_matchesFocusLossDecision() {
        // Kunci konsistensi antar jalur (fix temuan review #2): level tak
        // dikenal harus menghasilkan keputusan yang SAMA di jalur Home/
        // Recents dan jalur fokus hilang. Dulu onUserLeaveHint memakai
        // `== "medium"` hardcoded sementara onStop/fokus memakai != "low"
        // → level custom (mis. "high") berperilaku berbeda di dua jalur.
        for (level in listOf("", "high", "custom", "MEDIUM")) {
            val onLeave = ExamModePolicy.shouldAutoSubmitOnUserLeave(level, strictMode = false)
            val onFocusLoss = ExamModePolicy.shouldAutoSubmitOnFocusLoss(level, strictMode = false)
            assertTrue(
                "Keputusan level '$level' tidak konsisten antar jalur",
                onLeave == onFocusLoss
            )
        }
    }

    @Test
    fun userLeave_decision_alwaysMatchesFocusLoss_forAllModes() {
        // Untuk SEMUA kombinasi, kedua fungsi kebijakan harus setuju —
        // perbedaan hanya pada guard tambahan di titik panggilan (popup,
        // dialog app), bukan pada keputusan mode.
        for (level in listOf(ExamModePolicy.LEVEL_LOW, ExamModePolicy.LEVEL_MEDIUM, "", "x")) {
            for (strict in listOf(true, false)) {
                assertEquals(
                    ExamModePolicy.shouldAutoSubmitOnFocusLoss(level, strict),
                    ExamModePolicy.shouldAutoSubmitOnUserLeave(level, strict)
                )
            }
        }
    }

    // ── Grace period startup (fix temuan review #4) ──────────────────────

    @Test
    fun startupGrace_nullReadyAt_noSuppression() {
        // PDF belum pernah siap → jalur leave memang tidak akan submit
        // (guard isPdfReady); grace tidak boleh menambah efek samping.
        assertFalse(
            ExamModePolicy.isWithinStartupGrace(pdfReadyAtMs = null, nowMs = 1_000L)
        )
    }

    @Test
    fun startupGrace_withinGrace_suppressed() {
        // PDF siap di t=5000, sekarang t=6000, grace 2000 → dalam grace.
        assertTrue(
            ExamModePolicy.isWithinStartupGrace(
                pdfReadyAtMs = 5_000L, nowMs = 6_000L, graceMs = 2_000L
            )
        )
    }

    @Test
    fun startupGrace_beyondGrace_notSuppressed() {
        assertFalse(
            ExamModePolicy.isWithinStartupGrace(
                pdfReadyAtMs = 5_000L, nowMs = 7_001L, graceMs = 2_000L
            )
        )
    }

    @Test
    fun startupGrace_exactBoundary_notSuppressed() {
        // Tepat di batas grace → sudah boleh submit (konsisten dengan
        // konvensi ambang eksklusif seperti clock drift).
        assertFalse(
            ExamModePolicy.isWithinStartupGrace(
                pdfReadyAtMs = 5_000L, nowMs = 7_000L, graceMs = 2_000L
            )
        )
    }

    @Test
    fun startupGrace_defaultIsThreeSeconds() {
        // Kontrak nilai default — dipakai activity tanpa argumen eksplisit.
        assertTrue(
            ExamModePolicy.isWithinStartupGrace(
                pdfReadyAtMs = 10_000L, nowMs = 10_000L + ExamModePolicy.STARTUP_GRACE_MS - 1
            )
        )
        assertFalse(
            ExamModePolicy.isWithinStartupGrace(
                pdfReadyAtMs = 10_000L, nowMs = 10_000L + ExamModePolicy.STARTUP_GRACE_MS
            )
        )
    }

    // ── Delay focus-loss saat grace tombol volume (fix review #2 gel.2) ──

    @Test
    fun focusLossDelay_noRecentVolumePress_baseDelayOnly() {
        assertEquals(
            500L,
            ExamModePolicy.focusLossDelayMs(volumeKeyPressedAtMs = null, nowMs = 0L)
        )
    }

    @Test
    fun focusLossDelay_volumeJustPressed_fullGraceAdded() {
        // Volume ditekan tepat sekarang → evaluasi fokus ditunggu sampai
        // grace panel volume selesai, lalu delay dasar.
        assertEquals(
            500L + 1_500L,
            ExamModePolicy.focusLossDelayMs(
                volumeKeyPressedAtMs = 10_000L, nowMs = 10_000L,
                baseDelayMs = 500L, volumeGraceMs = 1_500L
            )
        )
    }

    @Test
    fun focusLossDelay_partialGraceRemaining_addedToBase() {
        // Volume ditekan 1000ms lalu → sisa grace 500ms → total delay 1000ms.
        assertEquals(
            1_000L,
            ExamModePolicy.focusLossDelayMs(
                volumeKeyPressedAtMs = 9_000L, nowMs = 10_000L,
                baseDelayMs = 500L, volumeGraceMs = 1_500L
            )
        )
    }

    @Test
    fun focusLossDelay_graceExpired_backToBaseDelay() {
        assertEquals(
            500L,
            ExamModePolicy.focusLossDelayMs(
                volumeKeyPressedAtMs = 8_000L, nowMs = 10_000L,
                baseDelayMs = 500L, volumeGraceMs = 1_500L
            )
        )
    }

    @Test
    fun focusLossDelay_exactGraceBoundary_backToBaseDelay() {
        // Ambang eksklusif — konsisten dengan konvensi grace lain.
        assertEquals(
            500L,
            ExamModePolicy.focusLossDelayMs(
                volumeKeyPressedAtMs = 8_500L, nowMs = 10_000L,
                baseDelayMs = 500L, volumeGraceMs = 1_500L
            )
        )
    }

    // ── Grace volume pada FOKUS-KEMBALI (fix review ronde 3 #2) ──────────

    @Test
    fun volumeGrace_focusGained_noRecentPress_notWithinGrace() {
        assertFalse(
            ExamModePolicy.isWithinVolumeGrace(volumeKeyPressedAtMs = null, nowMs = 0L)
        )
    }

    @Test
    fun volumeGrace_focusGained_recentlyPressed_withinGrace() {
        assertTrue(
            ExamModePolicy.isWithinVolumeGrace(
                volumeKeyPressedAtMs = 9_000L, nowMs = 10_000L
            )
        )
    }

    @Test
    fun volumeGrace_focusGained_afterGrace_notWithinGrace() {
        // Kontrak: hanya pembersihan (cancel runnable, reset popup) yang
        // berjalan dalam grace — aksi keamanan (re-pin, audit) dilewati.
        assertFalse(
            ExamModePolicy.isWithinVolumeGrace(
                volumeKeyPressedAtMs = 8_000L, nowMs = 10_000L
            )
        )
    }

    @Test
    fun volumeGrace_exactBoundary_notWithinGrace() {
        assertFalse(
            ExamModePolicy.isWithinVolumeGrace(
                volumeKeyPressedAtMs = 8_500L, nowMs = 10_000L
            )
        )
    }

    // ── Normalisasi security level (fix review low-mode #1) ──────────────

    @Test
    fun normalize_uppercaseServerValue_becomesLow() {
        // Bug inti: server mengirim "LOW" → dulu dibanding mentah, gagal
        // cocok dengan "low" → siswa low ikut ter-auto-submit seperti medium.
        assertEquals("low", ExamModePolicy.normalize("LOW"))
        assertEquals("low", ExamModePolicy.normalize("Low"))
    }

    @Test
    fun normalize_whitespaceAndNull_fallBackToMedium() {
        assertEquals("medium", ExamModePolicy.normalize(null))
        assertEquals("medium", ExamModePolicy.normalize(""))
        assertEquals("medium", ExamModePolicy.normalize("   "))
    }

    @Test
    fun normalize_mixedCaseAndPadding_trimmedAndLowercased() {
        assertEquals("medium", ExamModePolicy.normalize(" Medium "))
        assertEquals("strict", ExamModePolicy.normalize("STRICT"))
    }

    @Test
    fun normalize_unknownLevel_passesThroughLowercased() {
        // Level custom (mis. "high") tidak dipetakan paksa ke medium —
        // hanya dinormalisasi bentuknya; keputusan mode tetap fail-closed
        // lewat != LEVEL_LOW.
        assertEquals("high", ExamModePolicy.normalize("HIGH"))
    }

    // ── Penjadwalan focus-loss watch (fix review low-mode #4) ────────────

    @Test
    fun scheduleFocusLossWatch_lowNonStrict_skipped() {
        // Low tidak pernah auto-submit saat fokus hilang dan bukan strict →
        // runnable 500ms+ adalah kerja sia-sia; jangan dijadwalkan.
        assertFalse(
            ExamModePolicy.shouldScheduleFocusLossWatch(
                strictMode = false, securityLevel = ExamModePolicy.LEVEL_LOW
            )
        )
    }

    @Test
    fun scheduleFocusLossWatch_mediumNonStrict_scheduled() {
        assertTrue(
            ExamModePolicy.shouldScheduleFocusLossWatch(
                strictMode = false, securityLevel = ExamModePolicy.LEVEL_MEDIUM
            )
        )
    }

    @Test
    fun scheduleFocusLossWatch_strict_alwaysScheduled() {
        // Strict memakai runnable ini untuk re-pin saat fokus hilang,
        // terlepas dari security level.
        assertTrue(
            ExamModePolicy.shouldScheduleFocusLossWatch(
                strictMode = true, securityLevel = ExamModePolicy.LEVEL_LOW
            )
        )
        assertTrue(
            ExamModePolicy.shouldScheduleFocusLossWatch(
                strictMode = true, securityLevel = ""
            )
        )
    }

    @Test
    fun scheduleFocusLossWatch_normalizedConsistency_lowUppercaseSkipped() {
        // Setelah intake dinormalisasi, nilai apa pun ekuivalen "low"
        // harus menghasilkan keputusan yang sama.
        for (raw in listOf("low", "LOW", "Low", " low ")) {
            assertEquals(
                "Nilai '$raw' harus diperlakukan sebagai low",
                ExamModePolicy.shouldScheduleFocusLossWatch(false, ExamModePolicy.normalize(raw)),
                ExamModePolicy.shouldScheduleFocusLossWatch(false, ExamModePolicy.LEVEL_LOW)
            )
        }
    }
}
