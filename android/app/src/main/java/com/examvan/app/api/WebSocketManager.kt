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
 * - Connects to server's /student SocketIO namespace
 * - Sends periodic heartbeats (replacing HTTP POST heartbeats)
 * - Receives real-time events (exam_terminated, config_changed)
 * - Auto-reconnects with exponential backoff on disconnect
 * - Graceful shutdown on exam completion
 *
 * Usage:
 *   WebSocketManager.connect(context, baseUrl, examId, token)
 *   WebSocketManager.sendHeartbeat(studentName, ...)
 *   WebSocketManager.disconnect()
 */
object WebSocketManager {

    private const val TAG = "WebSocketManager"
    private const val RECONNECT_BASE_DELAY_MS = 1000L
    private const val RECONNECT_MAX_DELAY_MS = 30000L
    private const val HEARTBEAT_INTERVAL_MS = 30000L

    private var webSocket: WebSocket? = null
    private var connected = false
    private var shouldReconnect = false
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
                startHeartbeat()
                // Send initial heartbeat immediately to register activity on dashboard
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
                stopHeartbeat()
                if (shouldReconnect) scheduleReconnect()
            }

            override fun onFailure(webSocket: WebSocket, t: Throwable, response: Response?) {
                connected = false
                Log.e(TAG, "WebSocket failure: ${t.message}")
                stopHeartbeat()
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
     * Send a SocketIO event in format: 42["event",{...}]
     */
    private fun sendSocketIOEvent(event: String, data: Map<String, Any>) {
        if (webSocket == null || !connected) return
        try {
            val wrapper = listOf(event, data)
            val message = gson.toJson(wrapper)
            webSocket?.send(message)
        } catch (e: Exception) {
            Log.e(TAG, "Failed to send event: ${e.message}")
        }
    }

    /**
     * Send heartbeat to server. Replaces HTTP POST heartbeats.
     * Uses stored student info by default, or overridden params if provided.
     */
    fun sendHeartbeat(
        studentName: String = this.studentName,
        examNumber: String = this.examNumber,
        studentClass: String = this.studentClass,
        deviceInfo: String = this.deviceInfo
    ) {
        val data = mapOf(
            "exam_id" to examId,
            "mac_address" to macAddress,
            "student_name" to studentName,
            "exam_number" to examNumber,
            "student_class" to studentClass,
            "device_info" to deviceInfo
        )
        sendSocketIOEvent("heartbeat", data)
    }

    /**
     * Notify server that exam is completed (student submitted).
     * Tries to send the event synchronously before closing.
     * If WS is already disconnected, sends via HTTP as fallback.
     */
    fun notifyExamCompleted() {
        val data = mapOf(
            "exam_id" to examId,
            "mac_address" to macAddress
        )
        // Send synchronously — flush() ensures it goes out before close
        if (connected && webSocket != null) {
            val wrapper = listOf("exam_completed", data)
            val message = gson.toJson(wrapper)
            val sent = webSocket!!.send(message)
            if (sent) {
                Log.d(TAG, "exam_completed sent via WebSocket")
            } else {
                Log.w(TAG, "WebSocket send failed — submitting via HTTP")
                notifyCompletedViaHttp()
            }
            shouldReconnect = false
            webSocket?.close(1000, "Exam completed")
        } else {
            Log.w(TAG, "WebSocket not connected — submitting via HTTP")
            notifyCompletedViaHttp()
        }
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
     * Start periodic heartbeat timer.
     */
    private fun startHeartbeat() {
        stopHeartbeat()
        heartbeatRunnable = Runnable {
            if (connected) {
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
     * Disconnect from WebSocket server.
     */
    fun disconnect() {
        shouldReconnect = false
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
