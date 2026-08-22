package com.examvan.app.helper

import org.junit.Assert.assertFalse
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Mengunci kontrak guard generasi rantai poll konfirmasi pinning (fix
 * temuan review strict #1: "setiap retry aktivasi membuat rantai poll baru
 * tanpa membatalkan rantai lama — callback onResult ganda, entri audit
 * duplikat, dan balapan hasil antar rantai").
 *
 * Kontrak:
 *  - newChain() menghasilkan nomor generasi yang selalu bertambah.
 *  - Hanya generasi TERBARU yang dianggap current; callback rantai lama
 *    (stale) harus diabaikan oleh pemilik rantai.
 *  - Generasi aktif tetap current berapa kali pun dicek.
 *
 *   ./gradlew :app:testStudentDebugUnitTest
 */
class PollChainGuardTest {

    @Test
    fun newChain_generationsStrictlyIncrease() {
        val guard = PollChainGuard()
        val g1 = guard.newChain()
        val g2 = guard.newChain()
        val g3 = guard.newChain()

        assertTrue(g2 > g1)
        assertTrue(g3 > g2)
    }

    @Test
    fun onlyLatestGeneration_isCurrent() {
        val guard = PollChainGuard()
        val stale = guard.newChain()
        val current = guard.newChain()

        assertTrue(guard.isCurrent(current))
        assertFalse(
            "Rantai lama harus dianggap stale agar callback-nya diabaikan",
            guard.isCurrent(stale)
        )
    }

    @Test
    fun currentGeneration_staysCurrent_onRepeatedChecks() {
        val guard = PollChainGuard()
        val gen = guard.newChain()

        repeat(5) { assertTrue(guard.isCurrent(gen)) }
    }

    @Test
    fun scenario_rapidRetries_onlyLatestChainDrivesResult() {
        // Skenario ujung-ke-ujung: siswa menekan "Coba Lagi" tiga kali cepat.
        val guard = PollChainGuard()
        val results = mutableListOf<String>()

        val chain1 = guard.newChain()
        val chain2 = guard.newChain()
        val chain3 = guard.newChain()

        // Callback datang tidak berurutan (rantai lama bangun kesiangan).
        if (guard.isCurrent(chain1)) results.add("chain1")
        if (guard.isCurrent(chain3)) results.add("chain3")
        if (guard.isCurrent(chain2)) results.add("chain2")

        assertEquals(listOf("chain3"), results)
    }
}
