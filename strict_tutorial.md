# Strict Mode — Mengapa Sering Bermasalah & Cara Kerjanya

## Daftar Isi
- [Latar Belakang](#latar-belakang)
- [Akar Masalah](#akar-masalah-mengapa-strict-mode-terus-rusak)
- [Cara Kerja Yang Benar](#cara-kerja-yang-benar)
- [Tabel Perilaku per Mode Keamanan](#tabel-perilaku-per-mode-keamanan)
- [Alur Teknis Android Lock Task](#alur-teknis-android-lock-task)
- [Catatan untuk Pengembang](#catatan-untuk-pengembang)

---

## Latar Belakang

EXAMVAN memiliki 3 tingkat keamanan ujian:

| Level | Nama | Deskripsi |
|-------|------|-----------|
| `low` | Rendah | Tidak ada pembatasan. Siswa bebas keluar-masuk. |
| `medium` | Sedang | Jika siswa keluar dari aplikasi, jawaban langsung dikumpulkan otomatis dan ujian berakhir. |
| `high` | Ketat (Strict) | Layar HP di-pin (Lock Task Mode). Siswa **tidak bisa keluar** dari aplikasi sama sekali. |

Mode **strict** (`high`) menggunakan fitur bawaan Android bernama **Lock Task Mode** (Screen Pinning) yang mengunci layar pada satu aplikasi saja. Tombol Home, Recent Apps, dan notifikasi dinonaktifkan oleh sistem operasi Android.

---

## Akar Masalah: Mengapa Strict Mode Terus Rusak

### Fungsi `autoSubmitAndExit()`

Aplikasi memiliki fungsi `autoSubmitAndExit()` yang melakukan 3 hal berurutan:

```
1. onSubmitSuccess()      → Tandai ujian selesai di memori lokal
2. deactivateLockTask()   → stopLockTask() → MELEPAS PIN LAYAR
3. finish()               → Tutup halaman ujian
```

### Masalahnya

Setiap kali siswa menunjukkan perilaku mencurigakan (menekan Home, kehilangan fokus layar, dll), kode **memanggil `autoSubmitAndExit()`** sebagai "pertahanan". 

**Langkah nomor 2 (`stopLockTask()`) adalah biang keladinya.**

Memanggil `stopLockTask()` secara programatis memberitahu Android:
> *"Aplikasi ini sudah selesai mengunci layar, silakan lepaskan pin."*

Android pun melepas pin layar **secara mulus tanpa mengunci HP**. Siswa langsung dilempar ke Home Screen dalam keadaan **tidak terkunci**, sehingga mereka bebas membuka Chrome, WhatsApp, atau aplikasi apa pun.

### Kenapa Ini Terus Terulang?

Setiap upaya perbaikan sebelumnya tetap berfokus pada **memperbaiki `autoSubmitAndExit()`** — membuatnya lebih cepat, lebih agresif, lebih instant. Padahal masalahnya bukan *bagaimana* fungsi itu dipanggil, tapi **fungsi itu sendiri yang tidak boleh dipanggil di strict mode**.

Di strict mode, pin layar Android **adalah** mekanisme keamanannya. Melepas pin sama dengan menonaktifkan keamanan.

---

## Cara Kerja Yang Benar

### Strict Mode (High Security)

**Prinsip: JANGAN PERNAH melepas pin layar secara programatis.**

Biarkan Android yang menangani:

1. **Siswa tidak bisa keluar** — Tombol Home/Recent diblokir oleh OS Android
2. **Jika siswa memaksa unpin** (menahan tombol Back + Recent selama beberapa detik) — **Android otomatis mengunci layar HP** dengan PIN/pola/sidik jari
3. **Siswa tidak bisa membuka apa pun** — HP dalam keadaan terkunci
4. **Setelah unlock** — EXAMVAN masih di atas dan pin layar diaktifkan ulang otomatis
5. **Satu-satunya cara keluar normal** — Mengumpulkan ujian lewat tombol Submit di dalam aplikasi

### Medium Mode

**Prinsip: Auto-submit sebagai pertahanan.**

1. Siswa menekan Home atau meminimalkan aplikasi
2. `autoSubmitAndExit()` dipanggil
3. Jawaban dikirim ke server
4. Aplikasi ditutup
5. Siswa tidak bisa masuk kembali (status ujian = selesai)

---

## Tabel Perilaku per Mode Keamanan

| Kejadian | Low | Medium | Strict |
|----------|-----|--------|--------|
| Siswa menekan Home | Bebas keluar | Auto-submit & exit | **Diblokir OS** (tombol tidak berfungsi) |
| Siswa buka notifikasi | Bebas | Auto-submit & exit | **Re-activate pin layar** |
| Kehilangan fokus >500ms | Tidak ada aksi | Auto-submit & exit | **Re-activate pin layar** |
| Siswa paksa unpin (Back+Recent) | — | — | **Android mengunci layar HP** |
| Submit normal via tombol | Kirim & tutup | Kirim & tutup | Kirim, **lepas pin**, tutup |

---

## Alur Teknis Android Lock Task

### Student Flavor (HP Pribadi Siswa)

```
                    ┌─────────────────────────┐
                    │  startLockTask()         │
                    │  (muncul dialog "Pin?")  │
                    └────────────┬────────────┘
                                 │
                    ┌────────────▼────────────┐
                    │  Siswa ketuk "Yes/Pin"   │
                    │  → Tombol Home/Recent    │
                    │    DINONAKTIFKAN oleh OS │
                    └────────────┬────────────┘
                                 │
              ┌──────────────────┼──────────────────┐
              │                  │                   │
     ┌────────▼────────┐  ┌─────▼──────┐  ┌────────▼────────┐
     │ Submit Normal   │  │ Paksa Unpin│  │ Fokus Hilang    │
     │ (tombol Submit) │  │ (Back+Rec) │  │ (overlay/notif) │
     └────────┬────────┘  └─────┬──────┘  └────────┬────────┘
              │                 │                   │
     ┌────────▼────────┐  ┌────▼───────┐  ┌────────▼────────┐
     │ stopLockTask()  │  │ Android    │  │ Re-activate     │
     │ → Lepas pin     │  │ KUNCI HP   │  │ lock task       │
     │ → Kirim jawaban │  │ (PIN/Pola) │  │ → Siswa tetap   │
     │ → Tutup app     │  │ → Siswa    │  │   terkunci      │
     └─────────────────┘  │   TIDAK    │  └─────────────────┘
                          │   BISA     │
                          │   BUKA     │
                          │   APA PUN  │
                          └────────────┘
```

### Kiosk Flavor (HP Sekolah + Device Owner)

Jika sekolah mendaftarkan aplikasi sebagai **Device Owner** via ADB:

```bash
adb shell dpm set-device-owner com.examvan.app.kiosk/com.examvan.app.receiver.MyDeviceAdminReceiver
```

Maka:
- Pin layar aktif **tanpa dialog konfirmasi** (silent)
- Siswa **sama sekali tidak bisa** melakukan unpin
- 100% terkunci sampai ujian selesai

---

## Catatan untuk Pengembang

### ⛔ Jangan Lakukan

```kotlin
// SALAH: Memanggil autoSubmitAndExit di strict mode
// Ini akan melepas pin layar dan membuat siswa bebas!
if (securityEnforcer.strictMode) {
    submissionManager.autoSubmitAndExit() // ← JANGAN!
}
```

### ✅ Lakukan

```kotlin
// BENAR: Di strict mode, cukup re-activate lock task
if (securityEnforcer.strictMode) {
    LockTaskManager.activate(activity) // ← Tetap terkunci
} else {
    submissionManager.autoSubmitAndExit() // ← Hanya untuk medium
}
```

### File-file Terkait

| File | Peran |
|------|-------|
| `ExamViewerActivity.kt` | Mengatur kapan auto-submit dipanggil berdasarkan mode keamanan |
| `SecurityEnforcer.kt` | Mendeteksi pelanggaran keamanan (fokus hilang, user leave, overlay) |
| `SubmissionManager.kt` | Mengirim jawaban ke server dan menutup aplikasi |
| `LockTaskManager.kt` | Mengaktifkan/menonaktifkan Android Lock Task (screen pinning) |

### Kaidah Utama

> **Di strict mode, `stopLockTask()` hanya boleh dipanggil SATU KALI: saat siswa menekan tombol Submit secara normal di dalam aplikasi.** Tidak boleh dipanggil sebagai reaksi terhadap pelanggaran keamanan atau perilaku mencurigakan, karena justru itu yang membuat siswa bebas.
