package com.examvan.app.helper

import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Mengunci kebijakan notifikasi pengiriman jawaban:
 * Notifikasi sistem (drawer / status bar) WAJIB dimunculkan pada SEMUA skenario
 * pengiriman jawaban, baik:
 *  1. Disengaja (Manual Submit oleh siswa melalui tombol "Kirim Jawaban")
 *  2. Tidak disengaja / otomatis (Auto-Submit karena deadline habis, keluar aplikasi, atau violation).
 */
class SubmissionNotificationPolicyTest {

    @Test
    fun manualSubmitSuccess_mustProduceNotificationPayload() {
        val payload = SubmissionNotificationPolicy.getNotificationContent(
            type = SubmissionNotificationPolicy.SubmissionType.MANUAL,
            isSuccess = true,
            examName = "Ujian Matematika",
            serverMessage = "Jawaban berhasil dikumpulkan"
        )

        assertNotNull(payload)
        assertTrue("Judul tidak boleh kosong", payload.title.isNotBlank())
        assertTrue("Pesan tidak boleh kosong", payload.message.isNotBlank())
        assertTrue("Status harus sukses", payload.isSuccess)
        assertTrue(
            "Pesan harus memuat konfirmasi atau nama ujian",
            payload.message.contains("Ujian Matematika") || payload.message.contains("Jawaban") || payload.title.contains("Berhasil")
        )
    }

    @Test
    fun autoSubmitSuccess_mustProduceNotificationPayload() {
        val payload = SubmissionNotificationPolicy.getNotificationContent(
            type = SubmissionNotificationPolicy.SubmissionType.AUTO,
            isSuccess = true,
            examName = "Ujian Fisika",
            serverMessage = "Jawaban berhasil dikumpulkan otomatis"
        )

        assertNotNull(payload)
        assertTrue("Judul tidak boleh kosong", payload.title.isNotBlank())
        assertTrue("Pesan tidak boleh kosong", payload.message.isNotBlank())
        assertTrue("Status harus sukses", payload.isSuccess)
    }

    @Test
    fun autoSubmitFailed_mustProduceNotificationPayloadWithRetry() {
        val payload = SubmissionNotificationPolicy.getNotificationContent(
            type = SubmissionNotificationPolicy.SubmissionType.AUTO,
            isSuccess = false,
            examName = "Ujian Biologi",
            serverMessage = "Koneksi terputus"
        )

        assertNotNull(payload)
        assertFalse("Status harus gagal", payload.isSuccess)
        assertTrue("Pesan harus menyertakan info kegagalan", payload.message.contains("Koneksi terputus") || payload.title.contains("Gagal"))
        assertTrue("Harus menyediakan opsi aksi retry", payload.allowRetryAction)
    }

    @Test
    fun shouldTriggerNotification_alwaysTrueOnSuccess() {
        // Baik manual maupun auto, notifikasi harus selalu dipicu saat submit sukses
        assertTrue(
            "Manual submit sukses WAJIB memicu notifikasi",
            SubmissionNotificationPolicy.shouldNotify(SubmissionNotificationPolicy.SubmissionType.MANUAL, isSuccess = true)
        )
        assertTrue(
            "Auto submit sukses WAJIB memicu notifikasi",
            SubmissionNotificationPolicy.shouldNotify(SubmissionNotificationPolicy.SubmissionType.AUTO, isSuccess = true)
        )
    }
}
