package com.examvan.app.helper

import android.app.Activity
import android.content.Intent
import android.util.Log
import com.examvan.app.AppPrefs
import com.examvan.app.ExamViewerActivity
import org.json.JSONObject

/**
 * Satu-satunya pintu auto-resume ke ExamViewerActivity selain gerbang
 * approval (WaitingApprovalActivity) dan notifikasi (SubmissionManager).
 *
 * Aman karena hanya membuka sesi yang SUDAH dimulai (exam_start_time ada)
 * dan belum selesai — lihat [ExamResumePolicy]. Pintu ini tidak pernah
 * memulai ujian baru, sehingga tidak melewati approval pengawas.
 */
object ActiveExamResumeLauncher {

    private const val TAG = "ExamResume"

    fun resumeIfActive(activity: Activity): Boolean {
        val configPrefs = AppPrefs.getConfigPrefsSafe(activity)
        val examPrefs = AppPrefs.getExamPrefsSafe(activity)
        // Satu percobaan per sinyal destroy; jangan sampai flag basi memicu
        // resume berulang saat activity token dibuka lagi.
        examPrefs.edit().remove(AppPrefs.KEY_AUTO_RESUME_PENDING).apply()
        // Utamakan metadata sesi aktif (exam prefs) — tetap ada walau siswa
        // tidak mencentang "Ingat URL" (config prefs-nya dihapus).
        val examId = examPrefs.getInt(
            AppPrefs.KEY_ACTIVE_EXAM_ID,
            configPrefs.getInt(AppPrefs.KEY_EXAM_ID, -1)
        )
        val serverUrl = examPrefs.getString(AppPrefs.KEY_ACTIVE_SERVER_URL, null)
            ?.takeIf { it.isNotBlank() }
            ?: configPrefs.getString(AppPrefs.KEY_SERVER_URL, "") ?: ""
        val identityData = configPrefs.getString(AppPrefs.KEY_IDENTITY_DATA, "{}") ?: "{}"
        val token = AppPrefs.getActiveExamToken(activity).ifEmpty {
            // Sesi lama (sebelum fitur active token) / "Ingat URL" dicentang.
            AppPrefs.getExamToken(activity)
        }
        val startTime = examPrefs.getString(AppPrefs.KEY_EXAM_START_TIME, null)
        val submitted = examPrefs.getBoolean(
            AppPrefs.getSubmittedOrExitedKey(examId), false
        )
        val pendingAnswers = SubmissionManager.hasPendingAnswers(activity, examId)
        val snapshot = ExamResumePolicy.Snapshot(
            examId = examId,
            serverUrl = serverUrl,
            token = token,
            startTime = startTime,
            submittedOrExited = submitted,
            hasPendingAnswers = pendingAnswers,
            identityData = identityData
        )
        if (!ExamResumePolicy.shouldResume(snapshot)) {
            // Diagnostik untuk OEM yang membunuh proses: nilai ini terlihat
            // di `adb logcat -s ExamResume` saat siswa kembali ke layar token.
            Log.i(
                TAG,
                "skip resume: examId=$examId server=${serverUrl.isNotBlank()} " +
                    "token=${token.isNotBlank()} start=${!startTime.isNullOrBlank()} " +
                    "submitted=$submitted pending=$pendingAnswers " +
                    "identity=${identityData.length} " +
                    "fallbackConfig=${AppPrefs.isConfigFallbackInUse()} " +
                    "fallbackExam=${AppPrefs.isExamFallbackInUse()}"
            )
            return false
        }
        Log.i(TAG, "resuming examId=$examId (start=${startTime?.take(10)})")

        val identityJson = try {
            JSONObject(identityData)
        } catch (_: Exception) {
            JSONObject()
        }
        val legacy = IdentityFormPolicy.extractLegacy(identityJson)
        val examName = examPrefs.getString(AppPrefs.KEY_EXAM_NAME, "") ?: ""
        val endTime = examPrefs.getString(AppPrefs.KEY_END_TIME, null)
            ?.takeIf { it.isNotBlank() }
        val securityLevel = ExamModePolicy.normalize(
            examPrefs.getString(AppPrefs.KEY_SECURITY_LEVEL, null)
        )
        val strictMode = ExamModePolicy.isStrict(
            securityLevel,
            examPrefs.getBoolean(AppPrefs.KEY_STRICT_MODE, false)
        )
        val questionsJson = examPrefs.getString(AppPrefs.KEY_QUESTIONS_JSON, null)

        val intent = Intent(activity, ExamViewerActivity::class.java).apply {
            // Bila instance ExamViewer masih hidup di task (proses tidak
            // dibunuh, hanya task ditutup), pakai instance itu — jangan
            // menumpuk instance kedua di atas ServerConfigActivity.
            addFlags(Intent.FLAG_ACTIVITY_CLEAR_TOP or Intent.FLAG_ACTIVITY_SINGLE_TOP)
            putExtra("exam_id", examId)
            putExtra("exam_name", examName)
            putExtra("server_url", serverUrl)
            putExtra("exam_token", token)
            putExtra("student_name", legacy.name)
            putExtra("student_number", legacy.number)
            putExtra("student_class", legacy.studentClass)
            putExtra("identity_data", identityData)
            if (endTime != null) putExtra("end_time", endTime)
            putExtra("security_level", securityLevel)
            putExtra("strict_mode", strictMode)
            if (ExamIntentPolicy.canAttachQuestions(questionsJson)) {
                putExtra("questions_json", questionsJson)
            }
        }
        activity.startActivity(intent)
        activity.finish()
        return true
    }
}
