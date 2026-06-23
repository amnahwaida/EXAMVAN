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

import com.examvan.app.BuildConfig

/**
 * API client for communicating with the EXAMVAN server.
 * Configured with extended timeouts for slow LAN connections.
 *
 * SECURITY NOTE: OkHttp defaults trust all system CAs. No certificate pinning.
 * For production over WAN (cloud/HTTPS deployment), ADD certificate pinning:
 *
 *   val pinner = CertificatePinner.Builder()
 *       .add("yourdomain.com", "sha256/AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
 *       .build()
 *   .certificatePinner(pinner)
 *
 * And enforce HTTPS-only URLs in ServerConfigActivity by rejecting http:// URLs
 * when not on a local network.
 *
 * TODO: Add runtime check — if server URL is HTTPS (not LAN IP), enable pinning
 *       using the server's provided certificate fingerprint from /api/health.
 */
object ApiClient {

    private val gson = Gson()

    private val client = OkHttpClient.Builder()
        .connectTimeout(15, TimeUnit.SECONDS)
        .readTimeout(60, TimeUnit.SECONDS)
        .writeTimeout(30, TimeUnit.SECONDS)
        .addInterceptor { chain ->
            val original = chain.request()
            val request = original.newBuilder()
                .header("X-App-Version", BuildConfig.VERSION_NAME)
                .build()
            chain.proceed(request)
        }
        .build()

    private var baseUrl: String = ""

    fun setBaseUrl(url: String) {
        baseUrl = url.trimEnd('/')
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
                        onSuccess(health)
                    } catch (e: Exception) {
                        onError("Response tidak valid")
                    }
                }
            }
        })
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
                    if (!it.isSuccessful) {
                        onError("Server error: ${it.code}")
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
        val request = Request.Builder()
            .url("$baseUrl/api/exams/$examId/pdf?token=$token")
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

    /**
     * Submit exam answers to the server.
     */
    fun submitExam(
        examId: Int,
        studentName: String,
        examNumber: String,
        studentClass: String,
        answers: Map<String, Any>,
        startTime: String? = null,
        macAddress: String? = null,
        identityData: String? = null,
        onSuccess: (String) -> Unit,
        onError: (String) -> Unit
    ) {
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
                val identityJson = org.json.JSONObject(identityData)
                payload["identity_data"] = identityJson
            } catch (_: Exception) {
                payload["identity_data"] = identityData
            }
        }

        val bodyStr = gson.toJson(payload)
        val mediaType = "application/json; charset=utf-8".toMediaTypeOrNull()
        val body = bodyStr.toRequestBody(mediaType)
        val request = Request.Builder()
            .url("$baseUrl/api/exams/$examId/submit")
            .post(body)
            .build()

        client.newCall(request).enqueue(object : Callback {
            override fun onFailure(call: Call, e: IOException) {
                onError(e.message ?: "Koneksi gagal")
            }

            override fun onResponse(call: Call, response: Response) {
                response.use {
                    try {
                        val bodyText = it.body?.string() ?: ""
                        val json = org.json.JSONObject(bodyText)
                        if (it.isSuccessful && json.optBoolean("success", false)) {
                            onSuccess(json.optString("message", "Ujian berhasil dikumpulkan"))
                        } else {
                            onError(json.optString("message", "Gagal mengumpulkan jawaban"))
                        }
                    } catch (e: Exception) {
                        onError("Gagal memproses respon server")
                    }
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
        studentName: String,
        examNumber: String,
        studentClass: String,
        answers: Map<String, Any>,
        startTime: String? = null,
        macAddress: String? = null,
        identityData: String? = null
    ): Pair<Boolean, String> {
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
                val identityJson = org.json.JSONObject(identityData)
                payload["identity_data"] = identityJson
            } catch (_: Exception) {
                payload["identity_data"] = identityData
            }
        }

        val bodyStr = gson.toJson(payload)
        val mediaType = "application/json; charset=utf-8".toMediaTypeOrNull()
        val body = bodyStr.toRequestBody(mediaType)
        val request = Request.Builder()
            .url("$baseUrl/api/exams/$examId/submit")
            .post(body)
            .build()

        return try {
            client.newCall(request).execute().use { response ->
                val bodyText = response.body?.string() ?: ""
                if (response.isSuccessful) {
                    val json = org.json.JSONObject(bodyText)
                    if (json.optBoolean("success", false)) {
                        Pair(true, json.optString("message", "Ujian berhasil dikumpulkan"))
                    } else {
                        Pair(false, json.optString("message", "Gagal mengumpulkan jawaban"))
                    }
                } else {
                    Pair(false, "Server error: ${response.code}")
                }
            }
        } catch (e: Exception) {
            Pair(false, e.message ?: "Koneksi gagal")
        }
    }
}
