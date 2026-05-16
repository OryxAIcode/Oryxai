package io.codesec.plugin

import com.google.gson.Gson
import com.google.gson.reflect.TypeToken
import java.net.HttpURLConnection
import java.net.URI

data class Finding(
    val rule_id: String = "",
    val severity: String = "",
    val message: String = "",
    val action: String = "",
    val matched_value: String = "",
    val confidence: Double = 0.0,
    val category: String = ""
)

data class ScanResult(
    val decision: String = "",
    val risk_score: Double = 0.0,
    val findings: List<Finding> = emptyList(),
    val scan_time_ms: Long = 0
)

data class HealthResponse(
    val status: String = "",
    val version: String = "",
    val uptime_seconds: Long = 0
)

class CodeSecClient {

    private val gson = Gson()

    private fun baseUrl(): String =
        CodeSecSettings.getInstance().state.backendAddress.trimEnd('/')

    private fun timeout(): Int =
        CodeSecSettings.getInstance().state.timeoutMs

    fun healthCheck(): HealthResponse {
        val conn = openConnection("${baseUrl()}/health", "GET")
        conn.connect()
        if (conn.responseCode != 200) {
            throw RuntimeException("Backend unreachable: HTTP ${conn.responseCode}")
        }
        val body = conn.inputStream.bufferedReader().readText()
        return gson.fromJson(body, HealthResponse::class.java)
    }

    fun scanCompletion(content: String, language: String, filePath: String, oldContent: String = ""): ScanResult {
        val payload = mapOf(
            "completion" to content,
            "language" to language,
            "file_path" to filePath,
            "diff_against" to oldContent
        )
        return post("${baseUrl()}/api/v1/mcp/scan-completion", payload)
    }

    fun scanPrompt(prompt: String, sessionId: String = ""): ScanResult {
        val payload = mutableMapOf<String, Any>(
            "prompt" to prompt
        )
        if (sessionId.isNotBlank()) {
            payload["session_id"] = sessionId
        }
        return post("${baseUrl()}/api/v1/mcp/scan-prompt", payload)
    }

    fun scanFullFile(content: String, language: String, filePath: String): ScanResult {
        val payload = mapOf(
            "completion" to content,
            "language" to language,
            "file_path" to filePath
        )
        return post("${baseUrl()}/api/v1/mcp/scan-completion", payload)
    }

    private fun post(url: String, payload: Map<String, Any>): ScanResult {
        val conn = openConnection(url, "POST")
        conn.doOutput = true
        conn.setRequestProperty("Content-Type", "application/json; charset=utf-8")

        val json = gson.toJson(payload)
        conn.outputStream.bufferedWriter().use { it.write(json) }

        if (conn.responseCode !in 200..299) {
            val errBody = try {
                conn.errorStream?.bufferedReader()?.readText() ?: ""
            } catch (_: Exception) { "" }
            throw RuntimeException("Scan failed: HTTP ${conn.responseCode} — $errBody")
        }

        val body = conn.inputStream.bufferedReader().readText()
        return gson.fromJson(body, ScanResult::class.java)
    }

    private fun openConnection(url: String, method: String): HttpURLConnection {
        val conn = URI(url).toURL().openConnection() as HttpURLConnection
        conn.requestMethod = method
        conn.connectTimeout = timeout()
        conn.readTimeout = timeout()
        conn.setRequestProperty("Accept", "application/json")
        conn.setRequestProperty("User-Agent", "CodeSec-JetBrains/0.1.0")
        return conn
    }
}
