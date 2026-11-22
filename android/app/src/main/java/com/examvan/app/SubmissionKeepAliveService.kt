package com.examvan.app

import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.os.Build
import android.os.Handler
import android.os.IBinder
import android.os.Looper
import android.util.Log
import androidx.core.app.NotificationCompat
import androidx.core.content.ContextCompat

/**
 * Foreground service singkat yang menjaga proses tetap hidup selama
 * auto-submit berjalan di background.
 *
 * Auto-submit menutup activity lebih dulu agar siswa tidak bisa kembali ke
 * ujian. Tanpa penjaga, proses bisa dibunuh OEM/low-memory sebelum HTTP
 * selesai, sehingga jawaban hanya tersisa di prefs dan baru terkirim saat
 * siswa membuka aplikasi lagi. Service ini TIDAK melakukan submit sendiri —
 * ia hanya memberi prioritas proses + notifikasi selama jendela submit.
 *
 * Batas waktu [MAX_LIFETIME_MS] mencegah service menggantung bila callback
 * submit tidak pernah datang. `START_NOT_STICKY`: bila proses mati, jangan
 * restart tanpa data — jalur recovery re-entry tetap menjadi jaring pengaman.
 */
class SubmissionKeepAliveService : Service() {

    private val handler = Handler(Looper.getMainLooper())
    private val stopRunnable = Runnable {
        stopSelf()
    }

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onCreate() {
        super.onCreate()
        createChannel()
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        val started = startForegroundSafely()
        if (!started) {
            stopSelf()
            return START_NOT_STICKY
        }
        handler.removeCallbacks(stopRunnable)
        handler.postDelayed(stopRunnable, MAX_LIFETIME_MS)
        return START_NOT_STICKY
    }

    override fun onDestroy() {
        handler.removeCallbacks(stopRunnable)
        super.onDestroy()
    }

    private fun startForegroundSafely(): Boolean {
        return try {
            val notification = NotificationCompat.Builder(this, CHANNEL_ID)
                .setSmallIcon(R.mipmap.ic_launcher)
                .setContentTitle(getString(R.string.submitting))
                .setContentText(getString(R.string.notification_channel_desc))
                .setPriority(NotificationCompat.PRIORITY_LOW)
                .setOngoing(true)
                .setSilent(true)
                .build()

            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
                startForeground(
                    NOTIFICATION_ID,
                    notification,
                    ServiceInfo.FOREGROUND_SERVICE_TYPE_DATA_SYNC
                )
            } else {
                startForeground(NOTIFICATION_ID, notification)
            }
            true
        } catch (t: Throwable) {
            Log.w(TAG, "Gagal memulai foreground service", t)
            false
        }
    }

    private fun createChannel() {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) return
        try {
            val manager = getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
            val channel = NotificationChannel(
                CHANNEL_ID,
                getString(R.string.notification_channel_name),
                NotificationManager.IMPORTANCE_LOW
            ).apply {
                description = getString(R.string.notification_channel_desc)
                setShowBadge(false)
            }
            manager.createNotificationChannel(channel)
        } catch (t: Throwable) {
            Log.w(TAG, "Gagal membuat notification channel", t)
        }
    }

    companion object {
        private const val TAG = "SubmissionKeepAlive"
        private const val CHANNEL_ID = "examvan_submission_keepalive"
        private const val NOTIFICATION_ID = 2002

        /**
         * Cukup untuk 4 percobaan HTTP + backoff + polling queued 75 detik,
         * dengan margin. Service berhenti lebih awal dari callback submit.
         */
        const val MAX_LIFETIME_MS = 5 * 60 * 1000L

        /**
         * Best-effort: Android 12+ dapat menolak startForegroundService dari
         * background. Kegagalan tidak boleh menggagalkan submit — pemanggil
         * tetap menjalankan jalur GlobalScope + recovery.
         */
        fun start(context: Context) {
            try {
                ContextCompat.startForegroundService(
                    context,
                    Intent(context, SubmissionKeepAliveService::class.java)
                )
            } catch (t: Throwable) {
                Log.w(TAG, "startForegroundService ditolak", t)
            }
        }

        fun stop(context: Context) {
            try {
                context.stopService(Intent(context, SubmissionKeepAliveService::class.java))
            } catch (_: Throwable) {
            }
        }
    }
}
