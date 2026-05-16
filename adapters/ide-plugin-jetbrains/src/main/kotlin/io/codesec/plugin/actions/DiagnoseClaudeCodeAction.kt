package io.codesec.plugin.actions

import com.intellij.notification.NotificationGroupManager
import com.intellij.notification.NotificationType
import com.intellij.openapi.actionSystem.AnAction
import com.intellij.openapi.actionSystem.AnActionEvent
import com.intellij.openapi.application.ApplicationManager
import io.codesec.plugin.ClaudeCodeConfig
import io.codesec.plugin.CodeSecCredentialStore
import io.codesec.plugin.CodeSecSettings
import java.net.HttpURLConnection
import java.net.URI

class DiagnoseClaudeCodeAction : AnAction() {
    override fun actionPerformed(e: AnActionEvent) {
        // Heavy I/O on a background thread so the EDT stays responsive.
        ApplicationManager.getApplication().executeOnPooledThread {
            val s = CodeSecSettings.getInstance().state
            val st = try {
                ClaudeCodeConfig.status()
            } catch (ex: Throwable) {
                postBalloon(e, "Could not read settings.json: ${ex.message}", NotificationType.ERROR)
                return@executeOnPooledThread
            }
            val lines = buildString {
                appendLine("settings.json: ${st.path}")
                appendLine("  exists:         ${st.exists}")
                appendLine("  managed by us:  ${st.managed}")
                appendLine("  ANTHROPIC_BASE: ${st.anthropicBaseURL ?: "(unset)"}")
                appendLine("  MCP codesec:    ${if (st.mcpHasCodesec) "registered" else "missing"}")
                appendLine()
                appendLine("Endpoints:")
                appendLine("  proxyURL:   ${s.proxyURL}")
                appendLine("  mcpURL:     ${s.mcpURL}")
                appendLine("  controlURL: ${s.controlURL}")
                appendLine("  apiKey:     ${if (CodeSecCredentialStore.hasApiKey()) "stored" else "NOT stored"}")
                appendLine()
                appendLine("Probes:")
                appendLine("  proxy /healthz:   " + probe(s.proxyURL.trimEnd('/') + "/healthz"))
                appendLine("  mcp /healthz:     " + probe(stripPath(s.mcpURL) + "/healthz"))
                appendLine("  control /health:  " + probe(s.controlURL.trimEnd('/') + "/api/v1/health"))
            }
            postBalloon(e, lines, NotificationType.INFORMATION)
        }
    }

    private fun probe(url: String): String =
        try {
            val conn = (URI(url).toURL().openConnection() as HttpURLConnection).apply {
                requestMethod = "GET"
                connectTimeout = 3_000
                readTimeout = 3_000
            }
            "$url → HTTP ${conn.responseCode}"
        } catch (ex: Exception) {
            "$url → unreachable (${ex.message})"
        }

    private fun stripPath(url: String): String {
        val u = URI(url)
        return "${u.scheme}://${u.authority}"
    }

    private fun postBalloon(e: AnActionEvent, msg: String, type: NotificationType) {
        ApplicationManager.getApplication().invokeLater {
            NotificationGroupManager.getInstance()
                .getNotificationGroup("CodeSec Notifications")
                .createNotification(msg, type)
                .notify(e.project)
        }
    }
}
