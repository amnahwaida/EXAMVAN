package com.examvan.app.api

import com.examvan.app.model.ExamListResponse
import com.examvan.app.model.HealthResponse
import com.google.gson.Gson
import okhttp3.*
import okhttp3.MediaType.Companion.toMediaTypeOrNull
import okhttp3.RequestBody.Companion.toRequestBody
import java.io.File
import java.io.FileOutputStream
import java.io.IOException
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicReference

import com.examvan.app.BuildConfig
import android.util.Log
import kotlin.coroutines.resume
import kotlin.coroutines.resumeWithException
import okhttp3.CertificatePinner

/**
 * API client for communicating with the EXAMVAN server.
 * Configured with generous timeouts for reliable connections over the internet.
 *
 * For production over HTTPS, set ApiClient.EXPECTED_FINGERPRINT to a known
 * certificate fingerprint at app startup to enable static certificate pinning.
 */
object ApiClient {

    private const val TAG = "ApiClient"

    private val gson = Gson()

    /**
     * HTTP status returned by the server (middleware.AndroidVersionCheck)
     * when the installed app version is older than the required one.
     */
    const val HTTP_UPGRADE_REQUIRED = 426

    /** Submit-response status of the async path (202): answers are QUEUED, not durable yet. */
    const val SUBMIT_STATUS_QUEUED = "queued"

    /** /result statuses reported by the worker/exam result endpoint. */
    const val RESULT_STATUS_DONE = "done"
    const val RESULT_STATUS_FAILED = "failed"

    /**
     * Set this to a known sha256/... certificate fingerprint to enable
     * static certificate pinning. When non-null, the fingerprint from
     * /api/health is validated against this expected value before being applied.
     * Leave null to use dynamic pinning only.
     */
    @JvmStatic
    var EXPECTED_FINGERPRINT: String? = null

    private fun defaultClient(): OkHttpClient = OkHttpClient.Builder()
        .connectTimeout(10, TimeUnit.SECONDS)
        .readTimeout(30, TimeUnit.SECONDS)
        .writeTimeout(30, TimeUnit.SECONDS)
        .addInterceptor { chain ->
            val original = chain.request()
            val request = original.newBuilder()
                .header("X-App-Version", BuildConfig.VERSION_NAME)
                .build()
            chain.proceed(request)
        }
        .addNetworkInterceptor { chain ->
            val original = chain.request()
            val baseHost = try {
                if (baseUrl.isNotEmpty()) java.net.URI(baseUrl).host else null
            } catch (_: Exception) { null }

            if (baseHost != null && original.url.host != baseHost && original.header("X-Exam-Token") != null) {
                val cleanRequest = original.newBuilder()
                    .removeHeader("X-Exam-Token")
                    .build()
                chain.proceed(cleanRequest)
            } else {
                chain.proceed(original)
            }
        }
        .build()

    private val clientRef = AtomicReference<OkHttpClient>(defaultClient())

    private val client: OkHttpClient get() = clientRef.get()

    private var certificateFingerprint: String? = null

    private var baseUrl: String = ""

    @JvmStatic
    var serverTimeSkewMs: Long = 0L

    /**
     * Reset seluruh state statis (baseUrl, certificate pinning, client, skew,
     * expected fingerprint). KHUSUS untuk test — dipanggil di setUp/tearDown
     * supaya test tidak saling mengontaminasi (state statis pada object).
     * Tidak dipakai di kode produksi.
     */
    @JvmStatic
    fun resetForTests() {
        baseUrl = ""
        certificateFingerprint = null
        serverTimeSkewMs = 0L
        EXPECTED_FINGERPRINT = null
        clientRef.set(defaultClient())
    }

    fun setBaseUrl(url: String) {
        var cleanUrl = url.trimEnd('/')
        // Cloud-only deployment: every connection uses HTTPS. Cleartext http:// is
        // blocked by the network security config, so upgrade it defensively here.
        // Loopback (localhost/127.0.0.1) is exempt: it is used by local tooling
        // and the instrumentation tests (MockWebServer). Release builds still
        // block ALL cleartext via the network security config, so a student can
        // never reach a production server over plain HTTP.
        if (cleanUrl.startsWith("http://") && !isLoopbackUrl(cleanUrl)) {
            cleanUrl = cleanUrl.replace("http://", "https://")
        }
        baseUrl = cleanUrl
    }

    /**
     * True when the URL points at the local machine (localhost / 127.0.0.1 / ::1).
     * Used to exempt loopback from the cloud-only https enforcement so local
     * tests and tooling can talk to a plain-HTTP MockWebServer.
     */
    @JvmStatic
    fun isLoopbackUrl(url: String): Boolean {
        return try {
            val host = java.net.URI(url).host?.lowercase() ?: return false
            host == "localhost" || host == "::1" || host == "127.0.0.1" || host.startsWith("127.")
        } catch (_: Exception) {
            false
        }
    }

    fun getBaseUrl(): String = baseUrl

    /**
     * Health check to verify server availability.
     */
    fun checkHealth(
        onSuccess: (HealthResponse) -> Unit,
        onError: (String) -> Unit
    ) {
        val request = Request.Builder()
            .url("$baseUrl/api/health")
            .get()
            .build()

        client.newCall(request).enqueue(object : Callback {
            override fun onFailure(call: Call, e: IOException) {
                onError(e.message ?: "Koneksi gagal")
            }

            override fun onResponse(call: Call, response: Response) {
                response.use {
                    if (!it.isSuccessful) {
                        onError(parseErrorBody(it))
                        return
                    }
                    try {
                        val body = it.body?.string() ?: ""
                        val health = gson.fromJson(body, HealthResponse::class.java)
                        health.server_time_utc?.let { utcStr ->
                            try {
                                val serverTime = java.time.Instant.parse(utcStr).toEpochMilli()
                                val deviceTime = System.currentTimeMillis()
                                serverTimeSkewMs = serverTime - deviceTime
                            } catch (_: Exception) {}
                        }

                        // Certificate pinning: if server provides a fingerprint and we're on HTTPS,
                        // validate against EXPECTED_FINGERPRINT if set, then rebuild with pinning.
                        val fp = health.certificate_fingerprint
                        if (!fp.isNullOrEmpty() && baseUrl.startsWith("https://")) {
                            when {
                                // Static pinning: verify fingerprint matches EXPECTED_FINGERPRINT first
                                EXPECTED_FINGERPRINT != null && !fp.equals(EXPECTED_FINGERPRINT, ignoreCase = true) -> {
                                    onError("Sidik jari sertifikat server tidak cocok dengan konfigurasi (static mismatch)")
                                    return
                                }
                                // Dynamic pinning (TOFU): first-fingerprint-wins
                                certificateFingerprint == null -> {
                                    synchronized(ApiClient) {
                                        if (certificateFingerprint == null) {
                                            certificateFingerprint = fp
                                            rebuildClientWithPinning(fp)
                                        }
                                    }
                                }
                                certificateFingerprint != fp -> {
                                    onError("Sidik jari sertifikat server berubah secara mencurigakan (dynamic mismatch)")
                                    return
                                }
                            }
                        }

                        onSuccess(health)
                    } catch (e: Exception) {
                        onError("Response tidak valid")
                    }
                }
            }
        })
    }

    /**
     * Rebuild the HTTP client with certificate pinning for the given server fingerprint.
     * Preserves all existing client configuration via newBuilder().
     * Only called when the server provides a certificate_fingerprint via /api/health
     * and the current connection uses HTTPS.
     */
    private fun rebuildClientWithPinning(fingerprint: String) {
        val hostname = try {
            java.net.URI(baseUrl).host
        } catch (e: Exception) { null }
        if (hostname == null) return

        // Use compareAndSet to safely swap the client reference (prevents lost updates
        // if rebuildClientWithPinning is called concurrently from multiple health-check callbacks)
        val oldClient = clientRef.get()
        val newClient = oldClient.newBuilder()
            .certificatePinner(
                CertificatePinner.Builder()
                    .add(hostname, fingerprint)
                    .build()
            )
            .build()
        clientRef.compareAndSet(oldClient, newClient)
    }

    /**
     * Fetch list of active exams.
     * onError receives (statusCode, message); statusCode is 0 for network
     * failures so callers can distinguish HTTP 426 (app update required).
     */
    fun getExams(
        onSuccess: (ExamListResponse) -> Unit,
        onError: (statusCode: Int, message: String) -> Unit
    ) {
        val request = Request.Builder()
            .url("$baseUrl/api/exams")
            .get()
            .build()

        client.newCall(request).enqueue(object : Callback {
            override fun onFailure(call: Call, e: IOException) {
                onError(0, e.message ?: "Koneksi gagal")
            }

            override fun onResponse(call: Call, response: Response) {
                response.use {
                    if (!it.isSuccessful) {
                        // Parse JSON error body (e.g. 426 "Versi aplikasi Anda ...")
                        // so the message is shown instead of a generic "Server error: <code>".
                        val msg = parseErrorBody(it)
                        onError(it.code, msg)
                        return
                    }
                    try {
                        val body = it.body?.string() ?: ""
                        val result = gson.fromJson(body, ExamListResponse::class.java)
                        onSuccess(result)
                    } catch (e: Exception) {
                        onError(it.code, "Response tidak valid")
                    }
                }
            }
        })
    }

    /** Extract a human-readable message from a JSON error body (message|error keys). */
    private fun parseErrorBody(response: Response): String {
        return try {
            val body = response.body?.string() ?: ""
            val errJson = org.json.JSONObject(body)
            errJson.optString("message", errJson.optString("error", "Server error: ${response.code}"))
        } catch (_: Exception) {
            "Server error: ${response.code}"
        }
    }

    /**
     * Request approval from pengawas.
     * onError receives (statusCode, message); statusCode is 0 for network
     * failures so callers can distinguish HTTP 426 (app update required).
     */
    fun requestApproval(
        examId: Int,
        macAddress: String,
        studentName: String,
        examNumber: String,
        studentClass: String,
        identityDataStr: String,
        reset: Boolean = false,
        token: String = "",
        onSuccess: (String) -> Unit, // returns status (pending, approved, rejected)
        onError: (statusCode: Int, message: String) -> Unit
    ) {
        val json = org.json.JSONObject().apply {
            put("exam_id", examId)
            put("mac_address", macAddress)
            put("student_name", studentName)
            put("exam_number", examNumber)
            put("student_class", studentClass)
            put("reset", reset)
            // Anti-spam: the server rejects request-approval without a valid
            // exam token. The device holds it (fetched the exam by token).
            put("token", token)
            try {
                put("identity_data", org.json.JSONObject(identityDataStr))
            } catch (e: Exception) {
                put("identity_data", org.json.JSONObject())
            }
        }

        val mediaType = "application/json; charset=utf-8".toMediaTypeOrNull()
        val request = Request.Builder()
            .url("$baseUrl/api/exams/request-approval")
            .post(json.toString().toRequestBody(mediaType))
            .build()

        client.newCall(request).enqueue(object : Callback {
            override fun onFailure(call: Call, e: IOException) {
                onError(0, e.message ?: "Koneksi gagal")
            }

            override fun onResponse(call: Call, response: Response) {
                response.use {
                    if (!it.isSuccessful) {
                        try {
                            val body = it.body?.string() ?: ""
                            val errJson = org.json.JSONObject(body)
                            val message = errJson.optString("message", errJson.optString("error", "Server error: ${it.code}"))
                            onError(it.code, message)
                        } catch (_: Exception) {
                            onError(it.code, "Server error: ${it.code}")
                        }
                        return
                    }
                    try {
                        val body = it.body?.string() ?: ""
                        val resJson = org.json.JSONObject(body)
                        val status = resJson.optString("status", "pending")
                        onSuccess(status)
                    } catch (e: Exception) {
                        onError(it.code, "Response tidak valid")
                    }
                }
            }
        })
    }

    /**
     * Fetch exam by Token.
     * onError receives (statusCode, message); statusCode is 0 for network
     * failures so callers can distinguish HTTP 426 (app update required).
     */
    fun getExamByToken(
        token: String,
        onSuccess: (com.examvan.app.model.TokenExamResponse) -> Unit,
        onError: (statusCode: Int, message: String) -> Unit
    ) {
        val request = Request.Builder()
            .url("$baseUrl/api/exams/token/$token")
            .get()
            .build()

        client.newCall(request).enqueue(object : Callback {
            override fun onFailure(call: Call, e: IOException) {
                onError(0, e.message ?: "Koneksi gagal")
            }

            override fun onResponse(call: Call, response: Response) {
                response.use {
                    if (it.code == 404) {
                        onError(it.code, "Token tidak valid atau ujian sudah berakhir")
                        return
                    }
                    if (it.code == 403) {
                        onError(it.code, "Ujian belum dimulai oleh pengawas")
                        return
                    }
                    if (!it.isSuccessful) {
                        onError(it.code, parseErrorBody(it))
                        return
                    }
                    try {
                        val body = it.body?.string() ?: ""
                        val result = gson.fromJson(body, com.examvan.app.model.TokenExamResponse::class.java)
                        onSuccess(result)
                    } catch (e: Exception) {
                        onError(it.code, "Response tidak valid")
                    }
                }
            }
        })
    }

    /**
     * Download exam PDF with progress callback.
     * Uses atomic save pattern: downloads to temp file first,
     * then renames to final path only if 100% complete.
     *
     * @param deviceId device identity sent in X-Device-Id — the SAME value used
     *                  at request-approval time (DEVICE:<AndroidId>). The server
     *                  refuses to serve the PDF to a device without an approved
     *                  exam_approvals row (14 Agustus 2026).
     */
    fun downloadPdf(
        examId: Int,
        token: String,
        deviceId: String,
        cacheDir: File,
        onProgress: (Int) -> Unit,
        onSuccess: (File) -> Unit,
        onError: (String) -> Unit
    ): Call {
        // SECURITY: token moved from URL query param to HTTP header to prevent
        // leaking the auth token in server logs, browser history, or proxies.
        val request = Request.Builder()
            .url("$baseUrl/api/exams/$examId/pdf")
            .header("X-Exam-Token", token)
            .header("X-Device-Id", deviceId)
            .get()
            .build()

        val call = client.newCall(request)

        call.enqueue(object : Callback {
            override fun onFailure(call: Call, e: IOException) {
                if (!call.isCanceled()) {
                    onError(e.message ?: "Download gagal")
                }
            }

            override fun onResponse(call: Call, response: Response) {
                response.use {
                    if (!it.isSuccessful) {
                        onError("Server error: ${it.code}")
                        return
                    }

                    val body = it.body ?: run {
                        onError("Response kosong")
                        return
                    }

                    val contentLength = body.contentLength()
                    val tempFile = File(cacheDir, "temp_exam_$examId.pdf")
                    val finalFile = File(cacheDir, "exam_$examId.pdf")

                    // Check if already cached
                    if (finalFile.exists() && contentLength > 0 &&
                        finalFile.length() == contentLength) {
                        onProgress(100)
                        onSuccess(finalFile)
                        return
                    }

                    try {
                        val inputStream = body.byteStream()
                        val outputStream = FileOutputStream(tempFile)
                        val buffer = ByteArray(8192)
                        var bytesRead: Long = 0

                        var len: Int
                        while (inputStream.read(buffer).also { r -> len = r } != -1) {
                            if (call.isCanceled()) {
                                outputStream.close()
                                tempFile.delete()
                                return
                            }
                            outputStream.write(buffer, 0, len)
                            bytesRead += len

                            if (contentLength > 0) {
                                val progress = ((bytesRead * 100) / contentLength).toInt()
                                onProgress(progress.coerceAtMost(100))
                            }
                        }

                        outputStream.flush()
                        outputStream.close()
                        inputStream.close()

                        // Atomic save: rename temp to final only if complete
                        if (contentLength <= 0 || bytesRead == contentLength) {
                            if (finalFile.exists()) finalFile.delete()
                            tempFile.renameTo(finalFile)
                            onProgress(100)
                            onSuccess(finalFile)
                        } else {
                            tempFile.delete()
                            onError("Download tidak lengkap")
                        }
                    } catch (e: Exception) {
                        tempFile.delete()
                        if (!call.isCanceled()) {
                            onError(e.message ?: "Download error")
                        }
                    }
                }
            }
        })

        return call
    }

    // -- Shared submit helpers -------------------------------------------------------

    /** Build the submit request body, shared by async and sync submit methods. */
    private fun buildSubmitRequest(
        examId: Int,
        token: String,
        studentName: String,
        examNumber: String,
        studentClass: String,
        answers: Map<String, Any>,
        startTime: String? = null,
        macAddress: String? = null,
        identityData: String? = null
    ): Request {
        val payload = mutableMapOf<String, Any>(
            "student_name" to studentName,
            "exam_number" to examNumber,
            "student_class" to studentClass,
            "answers" to answers
        )
        if (startTime != null) payload["start_time"] = startTime
        if (macAddress != null) payload["mac_address"] = macAddress
        if (identityData != null) {
            try {
                val type = object : com.google.gson.reflect.TypeToken<Map<String, Any>>() {}.type
                val identityMap: Map<String, Any> = gson.fromJson(identityData, type)
                payload["identity_data"] = identityMap
            } catch (_: Exception) {
                payload["identity_data"] = identityData
            }
        }

        val bodyStr = gson.toJson(payload)
        val mediaType = "application/json; charset=utf-8".toMediaTypeOrNull()
        return Request.Builder()
            .url("$baseUrl/api/exams/$examId/submit")
            .post(bodyStr.toRequestBody(mediaType))
            .header("X-Exam-Token", token)
            .build()
    }

    /**
     * Result of a submit-exam call: whether it succeeded, the server message,
     * and the optional custom congratulations message to show on the success
     * screen (null when the teacher left it unset — the app falls back to its
     * default wording).
     *
     * @param status submission state reported by the server: "queued" for the
     *               async path (202 — the answers are not durable yet, the
     *               client must poll [getExamResult] before discarding its
     *               local copy), "done" for the sync path.
     * @param jobId queue job id assigned by the server on the async path.
     */
    data class SubmitResult(
        val success: Boolean,
        val message: String,
        val congratsMessage: String?,
        val status: String? = null,
        val jobId: String? = null
    )

    /** Parse submit response, shared by async and sync submit methods. */
    private fun parseSubmitResponse(response: Response): SubmitResult {
        return try {
            val bodyText = response.body?.string() ?: ""
            val json = org.json.JSONObject(bodyText)
            if (response.isSuccessful && json.optBoolean("success", false)) {
                SubmitResult(
                    success = true,
                    message = json.optString("message", "Ujian berhasil dikumpulkan"),
                    congratsMessage = json.optString("congrats_message", "").ifBlank { null },
                    status = json.optString("status", "").ifBlank { null },
                    jobId = json.optString("job_id", "").ifBlank { null }
                )
            } else {
                SubmitResult(
                    success = false,
                    message = json.optString("message", "Gagal mengumpulkan jawaban"),
                    congratsMessage = null
                )
            }
        } catch (e: Exception) {
            SubmitResult(false, "Gagal memproses respon server", null)
        }
    }

    /**
     * Result of a result-poll: completion status ("unknown"/"pending"/"done")
     * plus the optional score when the worker has finished grading.
     */
    data class ExamResultPoll(
        val status: String,
        val score: Int? = null,
        val message: String? = null
    )

    /**
     * Poll GET /api/exams/{exam_id}/result for the status of a queued
     * submission (async 202 path). The endpoint is credential-gated: it needs
     * the exam token (X-Exam-Token) or a still-approved device + its own
     * submission; the token is sent so the poll survives the post-commit
     * approval revocation (14 Agustus 2026).
     */
    fun getExamResult(
        examId: Int,
        token: String,
        macAddress: String,
        identityData: String?,
        jobId: String?,
        onSuccess: (ExamResultPoll) -> Unit,
        onError: (String) -> Unit
    ) {
        val urlBuilder = StringBuilder("$baseUrl/api/exams/$examId/result")
            .append("?mac_address=").append(java.net.URLEncoder.encode(macAddress, "UTF-8"))
        if (!identityData.isNullOrEmpty()) {
            urlBuilder.append("&identity_data=").append(java.net.URLEncoder.encode(identityData, "UTF-8"))
        }
        if (!jobId.isNullOrEmpty()) {
            urlBuilder.append("&job_id=").append(java.net.URLEncoder.encode(jobId, "UTF-8"))
        }

        val request = Request.Builder()
            .url(urlBuilder.toString())
            .header("X-Exam-Token", token)
            .get()
            .build()

        client.newCall(request).enqueue(object : Callback {
            override fun onFailure(call: Call, e: IOException) {
                onError(e.message ?: "Koneksi gagal")
            }

            override fun onResponse(call: Call, response: Response) {
                response.use {
                    if (!it.isSuccessful) {
                        onError(parseErrorBody(it))
                        return
                    }
                    try {
                        val json = org.json.JSONObject(it.body?.string() ?: "")
                        onSuccess(
                            ExamResultPoll(
                                status = json.optString("status", "pending"),
                                score = if (json.has("score") && !json.isNull("score")) json.optInt("score") else null,
                                message = json.optString("message", "").ifBlank { null }
                            )
                        )
                    } catch (e: Exception) {
                        onError("Response tidak valid")
                    }
                }
            }
        })
    }

    /**
     * Submit exam answers to the server (async, callback-based).
     */
    fun submitExam(
        examId: Int,
        token: String,
        studentName: String,
        examNumber: String,
        studentClass: String,
        answers: Map<String, Any>,
        startTime: String? = null,
        macAddress: String? = null,
        identityData: String? = null,
        onSuccess: (SubmitResult) -> Unit,
        onError: (String) -> Unit
    ) {
        val request = buildSubmitRequest(examId, token, studentName, examNumber, studentClass, answers, startTime, macAddress, identityData)

        client.newCall(request).enqueue(object : Callback {
            override fun onFailure(call: Call, e: IOException) {
                onError(e.message ?: "Koneksi gagal")
            }

            override fun onResponse(call: Call, response: Response) {
                response.use {
                    val result = parseSubmitResponse(it)
                    if (result.success) onSuccess(result) else onError(result.message)
                }
            }
        })
    }

    /**
     * POST /api/exams/:exam_id/access-log — the HTTP channel for student
     * presence (login / heartbeat / logout).
     *
     * The server's WebSocket hub IGNORES heartbeat / exam_completed events from
     * token-authenticated clients (they are receive-only, so a class-wide shared
     * token cannot inject phantom presence), so presence MUST be reported over
     * HTTP. Fire-and-forget: a failure is logged and never blocks the exam flow.
     *
     * Server contract (webui/internal/handlers/api/exams.go):
     *   - event ∈ {"login", "heartbeat", "logout"}
     *   - login/heartbeat refresh the Redis presence (TTL 5 menit)
     *   - login/logout also append a row to student_access_logs
     */
    fun sendAccessLog(
        examId: Int,
        token: String,
        macAddress: String,
        event: String,
        studentName: String = "",
        examNumber: String = "",
        studentClass: String = "",
        deviceInfo: String = ""
    ) {
        val json = org.json.JSONObject().apply {
            put("event", event)
            put("mac_address", macAddress)
            put("student_name", studentName)
            put("exam_number", examNumber)
            put("student_class", studentClass)
            put("device_info", deviceInfo)
        }
        val mediaType = "application/json; charset=utf-8".toMediaTypeOrNull()
        val request = Request.Builder()
            .url("$baseUrl/api/exams/$examId/access-log")
            .post(json.toString().toRequestBody(mediaType))
            .header("X-Exam-Token", token)
            .build()

        client.newCall(request).enqueue(object : Callback {
            override fun onFailure(call: Call, e: IOException) {
                Log.w(TAG, "access-log ($event) gagal: ${e.message}")
            }

            override fun onResponse(call: Call, response: Response) {
                response.close()
            }
        })
    }

    /**
     * Submit exam answers to the server synchronously.
     * Used during app suspension/exit to prevent OkHttp tasks from being paused.
     */
    fun submitExamSync(
        examId: Int,
        token: String,
        studentName: String,
        examNumber: String,
        studentClass: String,
        answers: Map<String, Any>,
        startTime: String? = null,
        macAddress: String? = null,
        identityData: String? = null
    ): SubmitResult {
        return try {
            val request = buildSubmitRequest(examId, token, studentName, examNumber, studentClass, answers, startTime, macAddress, identityData)
            client.newCall(request).execute().use { response ->
                parseSubmitResponse(response)
            }
        } catch (e: Exception) {
            SubmitResult(false, e.message ?: "Koneksi gagal", null)
        }
    }
}
