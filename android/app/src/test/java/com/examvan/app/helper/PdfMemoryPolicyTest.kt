package com.examvan.app.helper

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Mengunci kebijakan memori PDF:
 *
 * 1. `ActivityManager.isLowRamDevice` tidak selalu true di perangkat 2 GB
 *    (Realme C11) — `memoryClass` kecil harus ikut memicu profil low-RAM.
 * 2. Batas bitmap low-RAM lebih kecil dari normal (penghematan memori tanpa
 *    mengubah format ARGB_8888 yang diwajibkan PdfRenderer).
 *
 *   ./gradlew :app:testStudentDebugUnitTest
 */
class PdfMemoryPolicyTest {

    @Test
    fun lowRamFlag_forcesLowProfile_evenWithLargeHeap() {
        assertTrue(PdfMemoryPolicy.useLowRamProfile(isLowRamDevice = true, memoryClassMb = 512))
    }

    @Test
    fun smallHeap_forcesLowProfile_evenWithoutLowRamFlag() {
        assertTrue(PdfMemoryPolicy.useLowRamProfile(isLowRamDevice = false, memoryClassMb = 256))
        assertTrue(PdfMemoryPolicy.useLowRamProfile(isLowRamDevice = false, memoryClassMb = 192))
        assertTrue(PdfMemoryPolicy.useLowRamProfile(isLowRamDevice = false, memoryClassMb = 128))
    }

    @Test
    fun normalDeviceWithLargeHeap_usesNormalProfile() {
        assertFalse(PdfMemoryPolicy.useLowRamProfile(isLowRamDevice = false, memoryClassMb = 512))
    }

    @Test
    fun lowProfile_usesSmallerBitmapCaps() {
        assertEquals(1024, PdfMemoryPolicy.maxWidth(useLowRamProfile = true))
        assertEquals(1536, PdfMemoryPolicy.maxHeight(useLowRamProfile = true))
        assertEquals(1536, PdfMemoryPolicy.maxWidth(useLowRamProfile = false))
        assertEquals(2304, PdfMemoryPolicy.maxHeight(useLowRamProfile = false))
    }
}
