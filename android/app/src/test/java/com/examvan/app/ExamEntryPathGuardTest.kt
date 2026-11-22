package com.examvan.app

import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

/**
 * Guard arsitektur jalur masuk ujian (fix temuan review low-mode ronde 4 #1
 * & #2: "ServerConfigActivity.startExamViewer adalah kode mati yang
 * berbahaya — meluncurkan ExamViewerActivity langsung, MELEWATI gerbang
 * approval pengawas"; "ExamListActivity + ExamAdapter masih menggantung
 * dengan salinan kedua logika buka-ujian").
 *
 * Kontrak yang dikunci:
 *  1. ExamViewerActivity HANYA boleh diluncurkan dari:
 *     - WaitingApprovalActivity   (gerbang approval pengawas), atau
 *     - SubmissionManager         (content-intent notifikasi auto-submit,
 *       yang selalu mendarat di layar recovery / "sudah selesai" — bukan
 *       sesi ujian baru), atau
 *     - ActiveExamResumeLauncher  (auto-resume sesi yang SUDAH dimulai
 *       setelah proses dibunuh OEM; digerbangi ExamResumePolicy sehingga
 *       tidak pernah memulai ujian baru / melewati approval).
 *     Pintu lain = jalur bypass pengawas.
 *  2. Layar mati ExamListActivity / ExamAdapter tidak boleh kembali tanpa
 *     disadari (file sumber dan entri manifest harus bersih).
 *
 *   ./gradlew :app:testStudentDebugUnitTest
 */
class ExamEntryPathGuardTest {

    /** Direktori modul app/ — pola sama dengan NoEmojiInAppTest. */
    private fun resolveModuleDir(): File {
        val cwd = File(System.getProperty("user.dir") ?: ".")
        return when {
            File(cwd, "src/main/res").isDirectory -> cwd
            File(cwd, "app/src/main/res").isDirectory -> cwd.resolve("app")
            else -> error("Tidak dapat menemukan direktori sumber Android (cwd=${cwd.absolutePath})")
        }
    }

    private val allowedLauncherFiles = setOf(
        "WaitingApprovalActivity.kt",
        "SubmissionManager.kt",
        "ActiveExamResumeLauncher.kt"
    )

    private fun filesLaunchingExamViewer(): List<String> {
        val javaDir = resolveModuleDir().resolve("src/main/java")
        return javaDir.walkTopDown()
            .filter { it.isFile && it.extension == "kt" }
            .filter { it.readText().contains("ExamViewerActivity::class.java") }
            .map { it.name }
            .toList()
    }

    @Test
    fun examViewer_mayOnlyBeLaunchedFromApprovalGateOrNotification() {
        val offenders = filesLaunchingExamViewer() - allowedLauncherFiles
        assertTrue(
            "Ditemukan peluncur ExamViewerActivity di luar gerbang approval: $offenders. " +
                "Meluncurkan viewer langsung = melewati approval pengawas dan reset sesi.",
            offenders.isEmpty()
        )
    }

    @Test
    fun approvalGateFile_stillLaunchesExamViewer() {
        // Guard kebalikan: pastikan test ini tidak bisa lolos dengan cara
        // menghapus semua peluncur (mis. refactor keliru).
        assertTrue(
            "WaitingApprovalActivity harus tetap menjadi gerbang masuk ujian",
            filesLaunchingExamViewer().contains("WaitingApprovalActivity.kt")
        )
    }

    @Test
    fun approvalGate_clearsLocalSubmittedMarkerOnApproval() {
        // Regresi: attempt ke-2 (repeat) yang disetujui pengawas tetap
        // menampilkan layar "Ujian Sudah Selesai" karena flag lokal dari
        // attempt pertama tidak dibersihkan. Server sudah menjadi sumber
        // kebenaran gerbang repeat; gerbang approval wajib mereset penanda
        // lokal sebelum membuka ExamViewerActivity.
        val file = resolveModuleDir().resolve(
            "src/main/java/com/examvan/app/WaitingApprovalActivity.kt"
        )
        assertTrue(
            "onApproved harus memanggil AppPrefs.clearSubmittedState agar " +
                "repeat yang disetujui tidak memunculkan layar 'Ujian Sudah Selesai'",
            file.exists() && file.readText().contains("clearSubmittedState")
        )
    }

    @Test
    fun activeResumeLauncher_isGatedByResumePolicy() {
        // Auto-resume boleh menjadi peluncur ketiga HANYA karena digerbangi
        // ExamResumePolicy (sesi sudah mulai & belum selesai). Guard ini
        // mencegah seseorang menghapus gerbang tersebut lalu memakai launcher
        // untuk memulai ujian baru tanpa approval.
        val launcher = resolveModuleDir().resolve(
            "src/main/java/com/examvan/app/helper/ActiveExamResumeLauncher.kt"
        )
        assertTrue(
            "ActiveExamResumeLauncher harus memanggil ExamResumePolicy.shouldResume",
            launcher.exists() && launcher.readText().contains("ExamResumePolicy.shouldResume")
        )
    }

    @Test
    fun safeConfigWrite_mustActuallyCommit() {
        // Regresi: `edit().apply(block)` memanggil apply { } milik Kotlin,
        // bukan Editor.apply(), sehingga URL/exam_id/identity_data tidak
        // pernah tersimpan. Auto-resume lalu kehilangan identitas.
        val file = resolveModuleDir().resolve(
            "src/main/java/com/examvan/app/ServerConfigActivity.kt"
        )
        val text = file.readText()
        assertTrue(
            "safeConfigWrite tidak boleh memakai edit().apply(block) — editor tidak di-commit",
            !text.contains(".edit().apply(block)")
        )
        assertTrue(
            "safeConfigWrite harus memanggil Editor.apply() setelah block",
            text.contains("editor.apply()")
        )
    }

    @Test
    fun deadExamListScreen_isFullyRemoved() {
        val moduleDir = resolveModuleDir()
        val leftovers = listOf(
            "src/main/java/com/examvan/app/ExamListActivity.kt",
            "src/main/java/com/examvan/app/adapter/ExamAdapter.kt",
            "src/main/res/layout/activity_exam_list.xml"
        ).filter { moduleDir.resolve(it).exists() }

        val manifestText = moduleDir.resolve("src/main/AndroidManifest.xml").readText()

        assertTrue(
            "Sisa layar mati ExamList ditemukan: $leftovers",
            leftovers.isEmpty()
        )
        assertTrue(
            "Manifest masih mereferensikan ExamListActivity",
            !manifestText.contains("ExamListActivity")
        )
    }
}
