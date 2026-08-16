package com.examvan.app.api

import android.content.Context
import android.os.Handler
import android.os.Looper
import android.util.Log
import com.examvan.app.BuildConfig
import com.google.gson.Gson
import okhttp3.*
import okhttp3.MediaType.Companion.toMediaTypeOrNull
import okhttp3.RequestBody.Companion.toRequestBody
import java.util.concurrent.TimeUnit

/**
 * Manages a persistent WebSocket connection to the EXAMVAN server
 * for real-time communication during exams.
 *
 * Features:
 * - Connects to the server's /ws/:examId endpoint
 * - Receives real-time events (exam_terminated, notification, student_update)
 * - Sends periodic HTTP heartbeats (POST /access-log) — the WS hub IGNORES
 *   heartbeats from token-authenticated clients, so presence is reported over
 *   HTTP while the socket is used purely as a receive channel
 * - Auto-reconnects with exponential backoff on disconnect
 * - Graceful shutdown on exam completion (HTTP POST /complete clears presence)
 *
 * Usage:
 *   WebSocketManager.connect(baseUrl, examId, token, deviceId, ...)
 *   WebSocketManager.sendHeartbeat()
 *   WebSocketManager.notifyExamCompleted()
 *   WebSocketManager.disconnect()
 */
object WebSocketManager {

    private const val TAG = "WebSocketManager"
    private const val RECONNECT_BASE_DELAY_MS = 1000L
    private const val RECONNECT_MAX_DELAY_MS = 30000L
    // 60 detik: cukup untuk presence Redis (TTL server 5 menit) dan tetap di
    // bawah rate limit per-IP access-log server (30 req/menit) untuk ruangan
    // ~28 siswa di belakang satu NAT sekolah.
    private const val HEARTBEAT_INTERVAL_MS = 60000L

    private var webSocket: WebSocket? = null
    private var connected = false
    private var shouldReconnect = false
    // True selama sesi ujian berjalan (connect() → disconnect()/complete).
    // Heartbeat HTTP berjalan selama sesi, TERLEPAS dari state socket —
    // presence siswa tidak boleh berhenti hanya karena WS mati.
    private var sessionActive = false
    private var reconnectAttempts = 0
    private var baseUrl: String = ""
    private var examId: Int = -1
    private var token: String = ""
    private var macAddress: String = ""
    private val gson = Gson()
    private val mainHandler = Handler(Looper.getMainLooper())
    private var heartbeatRunnable: Runnable? = null
    private var reconnectRunnable: Runnable? = null
    private var onEventCallback: ((event: String, data: Map<String, Any>) -> Unit)? = null

    // Student info for heartbeat
    private var studentName: String = ""
    private var examNumber: String = ""
    private var studentClass: String = ""
    private var deviceInfo: String = ""

    /** OkHttp client with longer timeouts for WebSocket. */
    private val client: OkHttpClient by lazy {
        OkHttpClient.Builder()
            .readTimeout(0, TimeUnit.MILLISECONDS) // WebSocket needs no read timeout
            .writeTimeout(10, TimeUnit.SECONDS)
            .connectTimeout(10, TimeUnit.SECONDS)
            .addInterceptor { chain ->
                val original = chain.request()
                val request = original.newBuilder()
                    .header("X-App-Version", BuildConfig.VERSION_NAME)
                    .build()
                chain.proceed(request)
            }
            .build()
    }

    /**
     * Connect to the EXAMVAN WebSocket server.
     *
     * @param baseUrl The server base URL (e.g. https://examvan.my.id)
     * @param examId The exam ID
     * @param token The exam token for authentication
     * @param deviceId The device identifier (macAddress/device UUID)
     * @param onEvent Callback for received events
     * @param student_name Student name for heartbeat data
     * @param exam_number Exam number for heartbeat data
     * @param student_class Student class for heartbeat data
     * @param device_info Device info string for heartbeat data
     */
    fun connect(
        baseUrl: String,
        examId: Int,
        token: String,
        deviceId: String,
        onEvent: ((event: String, data: Map<String, Any>) -> Unit)? = null,
        student_name: String = "",
        exam_number: String = "",
        student_class: String = "",
        device_info: String = ""
    ) {
        if (connected) {
            Log.d(TAG, "Already connected")
            return
        }

        this.baseUrl = baseUrl.trimEnd('/')
        this.examId = examId
        this.token = token
        this.macAddress = deviceId
        this.onEventCallback = onEvent
        this.studentName = student_name
        this.examNumber = exam_number
        this.studentClass = student_class
        this.deviceInfo = device_info
        this.shouldReconnect = true
        this.reconnectAttempts = 0
        this.sessionActive = true

        // Heartbeat dilaporkan via HTTP (access-log) dan berjalan selama sesi,
        // bukan hanya saat socket terhubung.
        startHeartbeat()

        doConnect()
    }

    /**
     * Perform the actual WebSocket connection.
     * Connects to SocketIO namespace /student with token auth.
     */
    private fun doConnect() {
        val wsUrl = baseUrl
            .replace("http://", "ws://")
            .replace("https://", "wss://")
            .removeSuffix("/")

        val socketUrl = "$wsUrl/ws/$examId"

        val request = Request.Builder()
            .url(socketUrl)
            .header("X-Exam-Token", token)
            .build()

        Log.d(TAG, "Connecting to $socketUrl")

        webSocket = client.newWebSocket(request, object : WebSocketListener() {
            override fun onOpen(webSocket: WebSocket, response: Response) {
                connected = true
                reconnectAttempts = 0
                Log.d(TAG, "WebSocket connected")
                // Heartbeat pertama segera dikirim agar presence tampil cepat
                // (timer HTTP sudah berjalan dari connect()).
                sendHeartbeat()
            }

            override fun onMessage(webSocket: WebSocket, text: String) {
                handleMessage(text)
            }

            override fun onClosing(webSocket: WebSocket, code: Int, reason: String) {
                Log.d(TAG, "WebSocket closing: $code $reason")
            }

            override fun onClosed(webSocket: WebSocket, code: Int, reason: String) {
                connected = false
                Log.d(TAG, "WebSocket closed: $code $reason")
                // Heartbeat HTTP tetap berjalan — presence tidak boleh berhenti
                // hanya karena socket mati.
                if (shouldReconnect) scheduleReconnect()
            }

            override fun onFailure(webSocket: WebSocket, t: Throwable, response: Response?) {
                connected = false
                Log.e(TAG, "WebSocket failure: ${t.message}")
                if (shouldReconnect) scheduleReconnect()
            }
        })
    }

    /**
     * Handle incoming SocketIO messages.
     * SocketIO protocol:
     *   0 = open, 2 = event, 3 = ack, 40 = connect to namespace, 42 = event with namespace
     */
    private fun handleMessage(text: String) {
        Log.d(TAG, "WS message: $text")
        try {
            if (text.startsWith("[")) {
                // Raw JSON array event: "[\"event_name\",{...}]"
                val arr = gson.fromJson(text, com.google.gson.JsonArray::class.java)
                if (arr != null && arr.size() >= 2) {
                    val eventName = arr[0].asString
                    @Suppress("UNCHECKED_CAST")
                    val eventData = gson.fromJson(arr[1], Map::class.java) as? Map<String, Any> ?: emptyMap()
                    handleServerEvent(eventName, eventData)
                }
            } else if (text.startsWith("42")) {
                // Legacy SocketIO event: "42[\"event_name\",{...}]"
                val jsonStr = text.substring(2)
                val arr = gson.fromJson(jsonStr, com.google.gson.JsonArray::class.java)
                if (arr != null && arr.size() >= 2) {
                    val eventName = arr[0].asString
                    @Suppress("UNCHECKED_CAST")
                    val eventData = gson.fromJson(arr[1], Map::class.java) as? Map<String, Any> ?: emptyMap()
                    handleServerEvent(eventName, eventData)
                }
            }
        } catch (e: Exception) {
            Log.e(TAG, "Failed to parse message: ${e.message}")
        }
    }

    /**
     * Handle a parsed server event.
     */
    private fun handleServerEvent(event: String, data: Map<String, Any>) {
        Log.d(TAG, "Server event: $event → $data")

        when (event) {
            "exam_terminated" -> {
                // Admin terminated the exam - notify user
                onEventCallback?.invoke("exam_terminated", data)
            }
            "student_update" -> {
                // Status update (echo of our own heartbeat)
                onEventCallback?.invoke("student_update", data)
            }
            "notification" -> {
                // General notification
                onEventCallback?.invoke("notification", data)
            }
        }
    }

    /**
     * Send heartbeat to server via HTTP POST /access-log (event "heartbeat").
     * The WS hub ignores heartbeats from token-authenticated clients, so
     * presence must go over HTTP. Uses stored student info by default, or
     * overridden params if provided.
     */
    fun sendHeartbeat(
        studentName: String = this.studentName,
        examNumber: String = this.examNumber,
        studentClass: String = this.studentClass,
        deviceInfo: String = this.deviceInfo
    ) {
        ApiClient.sendAccessLog(
            examId = examId,
            token = token,
            macAddress = macAddress,
            event = "heartbeat",
            studentName = studentName,
            examNumber = examNumber,
            studentClass = studentClass,
            deviceInfo = deviceInfo
        )
    }

    /**
     * Notify server that the exam is completed (student submitted).
     *
     * HTTP POST /complete is the PRIMARY channel: the WS hub ignores
     * exam_completed from token-authenticated (non-privileged) clients, so a
     * WS send that merely reaches the socket would silently drop the
     * completion. The HTTP endpoint deletes the Redis presence so the student
     * shows offline immediately on the monitoring dashboard.
     */
    fun notifyExamCompleted() {
        notifyCompletedViaHttp()
        shouldReconnect = false
        sessionActive = false
        stopHeartbeat()
        webSocket?.close(1000, "Exam completed")
        webSocket = null
        connected = false
        Log.d(TAG, "Exam completed — presence cleared via HTTP")
    }

    /**
     * Fallback: notify server via HTTP POST if WebSocket is unavailable.
     */
    private fun notifyCompletedViaHttp() {
        try {
            val httpUrl = baseUrl.replace("ws://", "http://").replace("wss://", "https://")
            val payload = gson.toJson(mapOf(
                "exam_id" to examId,
                "mac_address" to macAddress,
                "token" to token
            ))
            val mediaType = "application/json; charset=utf-8".toMediaTypeOrNull()
            val request = Request.Builder()
                .url("$httpUrl/api/exams/$examId/complete")
                .post(payload.toRequestBody(mediaType))
                .header("X-Exam-Token", token)
                .build()
            client.newCall(request).enqueue(object : Callback {
                override fun onFailure(call: Call, e: java.io.IOException) {
                    Log.w(TAG, "HTTP notify completed failed: ${e.message}")
                }
                override fun onResponse(call: Call, response: Response) {
                    response.close()
                    Log.d(TAG, "HTTP notify completed OK")
                }
            })
        } catch (e: Exception) {
            Log.w(TAG, "HTTP notify completed exception: ${e.message}")
        }
    }

    /**
     * Start the periodic HTTP heartbeat timer. Runs for the whole exam session
     * (not just while the socket is connected), because presence is reported
     * over HTTP and must survive WS downtime.
     */
    private fun startHeartbeat() {
        stopHeartbeat()
        heartbeatRunnable = Runnable {
            if (sessionActive) {
                sendHeartbeat()
                mainHandler.postDelayed(heartbeatRunnable!!, HEARTBEAT_INTERVAL_MS)
            }
        }
        mainHandler.postDelayed(heartbeatRunnable!!, HEARTBEAT_INTERVAL_MS)
    }

    /**
     * Stop periodic heartbeat timer.
     */
    private fun stopHeartbeat() {
        heartbeatRunnable?.let { mainHandler.removeCallbacks(it) }
        heartbeatRunnable = null
    }

    /**
     * Schedule reconnection with exponential backoff.
     */
    private fun scheduleReconnect() {
        reconnectRunnable?.let { mainHandler.removeCallbacks(it) }
        val delay = (RECONNECT_BASE_DELAY_MS * (1 shl reconnectAttempts))
            .coerceAtMost(RECONNECT_MAX_DELAY_MS)
        reconnectAttempts++

        Log.d(TAG, "Reconnecting in ${delay}ms (attempt $reconnectAttempts)")
        reconnectRunnable = Runnable {
            if (shouldReconnect && !connected) {
                doConnect()
            }
        }
        mainHandler.postDelayed(reconnectRunnable!!, delay)
    }

    /**
     * Disconnect from WebSocket server and end the presence session.
     */
    fun disconnect() {
        shouldReconnect = false
        sessionActive = false
        stopHeartbeat()
        reconnectRunnable?.let { mainHandler.removeCallbacks(it) }
        webSocket?.close(1000, "Client disconnect")
        webSocket = null
        connected = false
        Log.d(TAG, "Disconnected")
    }

    /**
     * Check if WebSocket is currently connected.
     */
    fun isConnected(): Boolean = connected
}
