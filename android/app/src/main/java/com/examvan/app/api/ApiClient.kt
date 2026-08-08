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

    private val gson = Gson()

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

    fun setBaseUrl(url: String) {
        var cleanUrl = url.trimEnd('/')
        // Cloud-only deployment: every connection uses HTTPS. Cleartext http:// is
        // blocked by the network security config, so upgrade it defensively here.
        if (cleanUrl.startsWith("http://")) {
            cleanUrl = cleanUrl.replace("http://", "https://")
        }
        baseUrl = cleanUrl
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
                        onError("Server error: ${it.code}")
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
     */
    fun getExams(
        onSuccess: (ExamListResponse) -> Unit,
        onError: (String) -> Unit
    ) {
        val request = Request.Builder()
            .url("$baseUrl/api/exams")
            .get()
            .build()

        client.newCall(request).enqueue(object : Callback {
            override fun onFailure(call: Call, e: IOException) {
                onError(e.message ?: "Koneksi gagal")
            }

            override fun onResponse(call: Call, response: Response) {
                response.use {
                    if (!it.isSuccessful) {
                        onError("Server error: ${it.code}")
                        return
                    }
                    try {
                        val body = it.body?.string() ?: ""
                        val result = gson.fromJson(body, ExamListResponse::class.java)
                        onSuccess(result)
                    } catch (e: Exception) {
                        onError("Response tidak valid")
                    }
                }
            }
        })
    }

    /**
     * Request approval from pengawas.
     */
    fun requestApproval(
        examId: Int,
        macAddress: String,
        studentName: String,
        examNumber: String,
        studentClass: String,
        identityDataStr: String,
        reset: Boolean = false,
        onSuccess: (String) -> Unit, // returns status (pending, approved, rejected)
        onError: (String) -> Unit
    ) {
        val json = org.json.JSONObject().apply {
            put("exam_id", examId)
            put("mac_address", macAddress)
            put("student_name", studentName)
            put("exam_number", examNumber)
            put("student_class", studentClass)
            put("reset", reset)
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
                onError(e.message ?: "Koneksi gagal")
            }

            override fun onResponse(call: Call, response: Response) {
                response.use {
                    if (!it.isSuccessful) {
                        try {
                            val body = it.body?.string() ?: ""
                            val errJson = org.json.JSONObject(body)
                            val message = errJson.optString("message", errJson.optString("error", "Server error: ${it.code}"))
                            onError(message)
                        } catch (_: Exception) {
                            onError("Server error: ${it.code}")
                        }
                        return
                    }
                    try {
                        val body = it.body?.string() ?: ""
                        val resJson = org.json.JSONObject(body)
                        val status = resJson.optString("status", "pending")
                        onSuccess(status)
                    } catch (e: Exception) {
                        onError("Response tidak valid")
                    }
                }
            }
        })
    }

    /**
     * Fetch exam by Token.
     */
    fun getExamByToken(
        token: String,
        onSuccess: (com.examvan.app.model.TokenExamResponse) -> Unit,
        onError: (String) -> Unit
    ) {
        val request = Request.Builder()
            .url("$baseUrl/api/exams/token/$token")
            .get()
            .build()

        client.newCall(request).enqueue(object : Callback {
            override fun onFailure(call: Call, e: IOException) {
                onError(e.message ?: "Koneksi gagal")
            }

            override fun onResponse(call: Call, response: Response) {
                response.use {
                    if (it.code == 404) {
                        onError("Token tidak valid atau ujian sudah berakhir")
                        return
                    }
                    if (it.code == 403) {
                        onError("Ujian belum dimulai oleh pengawas")
                        return
                    }
                    if (!it.isSuccessful) {
                        try {
                            val body = it.body?.string() ?: ""
                            val json = org.json.JSONObject(body)
                            val message = json.optString("message", json.optString("error", "Server error: ${it.code}"))
                            onError(message)
                        } catch (_: Exception) {
                            onError("Server error: ${it.code}")
                        }
                        return
                    }
                    try {
                        val body = it.body?.string() ?: ""
                        val result = gson.fromJson(body, com.examvan.app.model.TokenExamResponse::class.java)
                        onSuccess(result)
                    } catch (e: Exception) {
                        onError("Response tidak valid")
                    }
                }
            }
        })
    }

    /**
     * Download exam PDF with progress callback.
     * Uses atomic save pattern: downloads to temp file first,
     * then renames to final path only if 100% complete.
     */
    fun downloadPdf(
        examId: Int,
        token: String,
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
     */
    data class SubmitResult(
        val success: Boolean,
        val message: String,
        val congratsMessage: String?
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
                    congratsMessage = json.optString("congrats_message", "").ifBlank { null }
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
