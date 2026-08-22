package com.examvan.app.helper

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Mengunci kontrak buffer auto-save (fix temuan review low-mode ronde 2 #1:
 * "siswa low yang mengisi jawaban lalu langsung keluar dalam <500ms —
 * tulisan itu tidak pernah sampai ke prefs karena debounce; re-entry
 * memulihkan versi lama").
 *
 * Kontrak:
 *  - update() menyimpan nilai TERBARU (menimpa yang belum ter-drain) —
 *    semantik debounce: hanya jawaban terakhir yang penting.
 *  - drain() mengembalikan nilai terakhir SEKALI lalu kosong → flush ganda
 *    aman, tidak ada penulisan ulang.
 *  - drain pada buffer kosong = null.
 *
 *   ./gradlew :app:testStudentDebugUnitTest
 */
class AutoSaveBufferTest {

    @Test
    fun drain_returnsLatestUpdate_once() {
        val buffer = AutoSaveBuffer<String>()
        buffer.update("jawaban_v1")
        buffer.update("jawaban_v2")

        assertEquals("jawaban_v2", buffer.drain())
    }

    @Test
    fun drain_secondCall_empty() {
        val buffer = AutoSaveBuffer<String>()
        buffer.update("jawaban")

        buffer.drain()

        assertNull(buffer.drain())
        assertFalse(buffer.hasPending())
    }

    @Test
    fun drain_emptyBuffer_null() {
        val buffer = AutoSaveBuffer<String>()
        assertNull(buffer.drain())
        assertFalse(buffer.hasPending())
    }

    @Test
    fun hasPending_reflectsState() {
        val buffer = AutoSaveBuffer<String>()
        assertFalse(buffer.hasPending())

        buffer.update("jawaban")
        assertTrue(buffer.hasPending())

        buffer.drain()
        assertFalse(buffer.hasPending())
    }

    @Test
    fun scenario_exitWithinDebounceWindow_flushRecoversLastEdit() {
        // Skenario ujung-ke-ujung fix #1: siswa menulis di detik terakhir
        // (update tercatat, job debounce belum jalan), lalu keluar →
        // flushPendingAutoSave mem-drain dan menulis nilai TERAKHIR.
        val buffer = AutoSaveBuffer<Map<String, String>>()
        val written = mutableListOf<Map<String, String>>()

        // tick 1: auto-save rutin sudah berjalan untuk versi lama
        buffer.update(mapOf("1" to "A"))
        written.add(buffer.drain()!!)

        // tick 2: edit terakhir 300ms sebelum exit (belum ter-drain)
        buffer.update(mapOf("1" to "A", "2" to "B"))

        // exit low → flush sinkron
        buffer.drain()?.let { written.add(it) }

        assertEquals(
            listOf(mapOf("1" to "A"), mapOf("1" to "A", "2" to "B")),
            written
        )
        assertFalse(buffer.hasPending())
    }
}
