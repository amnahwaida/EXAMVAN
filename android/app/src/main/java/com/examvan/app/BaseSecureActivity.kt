package com.examvan.app

import android.accessibilityservice.AccessibilityServiceInfo
import android.content.ClipboardManager
import android.content.Context
import android.os.Build
import android.os.Bundle
import android.util.Log
import android.view.View
import android.view.WindowManager
import androidx.appcompat.app.AppCompatActivity
import androidx.core.view.ViewCompat
import androidx.core.view.WindowInsetsCompat

abstract class BaseSecureActivity : AppCompatActivity() {

    override fun onCreate(savedInstanceState: Bundle?) {
        // ── Android 15+ (API 35) forced edge-to-edge ─────────────────
        // Pada API 35, semua aplikasi dipaksa edge-to-edge secara default.
        // Konten bisa menggambar di belakang system bars (status bar,
        // navigation bar) jika tidak di-handle. Kita pasang listener
        // setelah setContentView untuk memberi padding yang benar.
        // Catatan: di API <35 listener ini no-op karena insets = 0.
        super.onCreate(savedInstanceState)
        window.setFlags(
            WindowManager.LayoutParams.FLAG_SECURE,
            WindowManager.LayoutParams.FLAG_SECURE
        )
        window.addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)

        // ── Lock screen visibility ──────────────────────────────────
        // Tampilkan activity di atas lock screen (kunci layar) sehingga
        // siswa bisa melihat ujian tanpa unlock. Juga hidupkan layar
        // jika mati saat activity berada di foreground.
        // Hanya aktif jika layar dalam keadaan ON (PowerManager.interactive).
        window.addFlags(WindowManager.LayoutParams.FLAG_SHOW_WHEN_LOCKED)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O_MR1) {
            window.addFlags(WindowManager.LayoutParams.FLAG_TURN_SCREEN_ON)
        }

        clearClipboard()
        // Apply tapjacking protection to all subclass activities automatically
        applyTapjackProtection()
        // Deteksi accessibility service berbahaya yang bisa membaca
        // konten layar (soal) atau melakukan gesture injection
        checkAccessibilityServices()
    }

    // ── Accessibility service detection ──────────────────────────

    /**
     * Deteksi accessibility service berbahaya yang:
     * 1. Bisa membaca konten layar (canRetrieveWindowContent)
     * 2. Bisa melakukan gesture injection (canPerformGestures)
     *
     * Jika terdeteksi service mencurigakan, tampilkan peringatan.
     * Catatan: deteksi ini tidak 100% akurat — beberapa aksesibilitas
     * legitimate (TalkBack, Select-to-Speak) juga memiliki flag ini.
     */
    protected fun checkAccessibilityServices() {
        try {
            val am = getSystemService(Context.ACCESSIBILITY_SERVICE) as?
                    android.view.accessibility.AccessibilityManager ?: return
            val enabledServices = am.getEnabledAccessibilityServiceList(
                    AccessibilityServiceInfo.FEEDBACK_ALL_MASK)

            for (service in enabledServices) {
                val id = service.id
                val canRetrieve = service.canRetrieveWindowContent
                // Cek apakah service bisa melakukan gesture injection
                val caps = service.capabilities
                val canPerformGestures = caps and
                        AccessibilityServiceInfo.CAPABILITY_CAN_PERFORM_GESTURES != 0

                // Skip service bawaan sistem yang legitimate
                if (id.contains("talkback", ignoreCase = true) ||
                    id.contains("select_to_speak", ignoreCase = true) ||
                    id.contains("switchaccess", ignoreCase = true)) continue

                if (canRetrieve || canPerformGestures) {
                    Log.w("BaseSecure",
                            "Accessibility service berbahaya: $id " +
                            "(retrieveWindow=$canRetrieve, performGestures=$canPerformGestures)")
                }
            }
        } catch (e: Exception) {
            Log.w("BaseSecure", "Gagal cek accessibility services", e)
        }
    }

    // ── edge-to-edge inset handling ─────────────────────────────────

    /**
     * Pasang OnApplyWindowInsetsListener pada root view untuk menambahkan
     * padding sebesar system bars + display cutout (dan IME bila diminta)
     * DI ATAS padding desain asli dari layout.
     *
     * Panggil dari subclass setelah setContentView(). Hanya berpengaruh di
     * Android 15+ (API 35) saat edge-to-edge dipaksa aktif (targetSdk 35):
     * di API < 35 edge-to-edge tidak dipaksa — decor sudah menangani system
     * bars dan insets yang dikirim ke view = 0, jadi helper ini no-op.
     *
     * Padding desain layout (mis. 24dp di activity_server_config) dipertahankan:
     * base padding ditangkap SEKALI, lalu insets ditambahkan di atasnya setiap
     * dispatch. Ini menghindari double-padding (insets ditumpuk berulang) dan
     * padding desain yang hilang (setPadding menggantikan total padding).
     *
     * @param includeIme tambahkan inset IME (keyboard) ke padding bawah —
     *   wajib untuk activity dengan input teks: di Android 15 edge-to-edge,
     *   windowSoftInputMode="adjustResize" tidak lagi men-resize window.
     */
    protected fun applyEdgeToEdgeInsets(rootView: View, includeIme: Boolean = false) {
        if (Build.VERSION.SDK_INT < 35) return // Legacy: decor sudah menangani

        // Base padding desain dari layout — ditangkap SEKALI agar tidak menumpuk.
        val baseLeft = rootView.paddingLeft
        val baseTop = rootView.paddingTop
        val baseRight = rootView.paddingRight
        val baseBottom = rootView.paddingBottom

        ViewCompat.setOnApplyWindowInsetsListener(rootView) { v, insets ->
            val systemBars = insets.getInsets(
                WindowInsetsCompat.Type.systemBars() or
                        WindowInsetsCompat.Type.displayCutout()
            )
            var bottom = systemBars.bottom
            if (includeIme) {
                val ime = insets.getInsets(WindowInsetsCompat.Type.ime())
                if (ime.bottom > bottom) bottom = ime.bottom
            }
            v.setPadding(
                baseLeft + systemBars.left,
                baseTop + systemBars.top,
                baseRight + systemBars.right,
                baseBottom + bottom
            )
            insets
        }
        // Minta sistem mengirimkan insets segera (terkadang listener
        // butuh trigger eksplisit jika view sudah ter-attach)
        rootView.requestApplyInsets()
    }

    /** Apply tapjacking protection to the root view of the content. */
    protected fun applyTapjackProtection() {
        window.decorView.rootView.filterTouchesWhenObscured = true
    }

    protected fun clearClipboard() {
        try {
            val clipboard = getSystemService(Context.CLIPBOARD_SERVICE) as? ClipboardManager
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.P) {
                clipboard?.clearPrimaryClip()
            } else {
                val clip = android.content.ClipData.newPlainText("", "")
                clipboard?.setPrimaryClip(clip)
            }
        } catch (_: Throwable) { }
    }
}
