package io.codesec.plugin

import com.google.gson.Gson
import com.google.gson.JsonObject
import com.intellij.openapi.application.ApplicationManager
import com.intellij.openapi.project.Project
import com.intellij.openapi.wm.ToolWindow
import com.intellij.openapi.wm.ToolWindowFactory
import com.intellij.ui.components.JBLabel
import com.intellij.ui.components.JBScrollPane
import com.intellij.ui.table.JBTable
import java.awt.BorderLayout
import java.net.HttpURLConnection
import java.net.URI
import java.net.URLEncoder
import java.time.Instant
import java.time.format.DateTimeFormatter
import javax.swing.JButton
import javax.swing.JPanel
import javax.swing.SwingUtilities
import javax.swing.Timer
import javax.swing.table.AbstractTableModel

/**
 * "CodeSec Activity" tool window — JetBrains counterpart of the SPA's
 * Audit page (Faz A4). Polls
 * `GET /api/v1/orgs/{slug}/feed?since=...` every 5 seconds and renders
 * each event as a row.
 *
 * The org slug comes from a workspace-level setting (we ask on first
 * open). We deliberately avoid a complex multi-org selector here — most
 * developers stay on one workspace.
 */
class ActivityToolWindowFactory : ToolWindowFactory {

    override fun createToolWindowContent(project: Project, toolWindow: ToolWindow) {
        val panel = ActivityPanel(project)
        val content = toolWindow.contentManager.factory.createContent(panel, "Activity", false)
        toolWindow.contentManager.addContent(content)
        panel.startPolling()
    }
}

private class ActivityPanel(private val project: Project) : JPanel(BorderLayout()) {

    private val gson = Gson()
    private val tableModel = FeedTableModel()
    private val table = JBTable(tableModel)
    private val statusLabel = JBLabel("Idle.")
    private val refreshBtn = JButton("Refresh now")
    private val orgSlugBtn = JButton("Org: (not set)")
    private var pollTimer: Timer? = null

    /** Workspace-scoped slug; kept in project user data so multi-project IDEs stay isolated. */
    private var orgSlug: String
        get() = project.getUserData(SLUG_KEY).orEmpty()
        set(value) {
            project.putUserData(SLUG_KEY, value)
            orgSlugBtn.text = "Org: ${if (value.isBlank()) "(not set)" else value}"
        }

    init {
        val toolbar = JPanel().apply {
            add(orgSlugBtn)
            add(refreshBtn)
            add(statusLabel)
        }
        add(toolbar, BorderLayout.NORTH)
        add(JBScrollPane(table), BorderLayout.CENTER)

        refreshBtn.addActionListener { refresh() }
        orgSlugBtn.addActionListener {
            val input = com.intellij.openapi.ui.Messages.showInputDialog(
                project,
                "Workspace slug — find it in the CodeSec web console URL: /o/<slug>",
                "CodeSec Activity — pick workspace",
                null,
                orgSlug,
                null,
            )?.trim().orEmpty()
            if (input.isNotEmpty()) {
                orgSlug = input
                refresh()
            }
        }
        orgSlugBtn.text = "Org: ${if (orgSlug.isBlank()) "(not set)" else orgSlug}"
    }

    fun startPolling() {
        if (pollTimer != null) return
        pollTimer = Timer(5_000) { refresh() }.also {
            it.isRepeats = true
            it.start()
        }
    }

    private fun refresh() {
        val slug = orgSlug
        if (slug.isBlank()) {
            setStatus("Pick a workspace to start polling.")
            return
        }
        val apiKey = CodeSecCredentialStore.readApiKey()
        if (apiKey.isBlank()) {
            setStatus("Not signed in — run Tools → CodeSec → Start Onboarding.")
            return
        }
        val cfg = CodeSecSettings.getInstance().state
        // 1 hour rolling window.
        val since = Instant.now().minusSeconds(60 * 60).toString()
        val url = cfg.controlURL.trimEnd('/') +
            "/api/v1/orgs/" + URLEncoder.encode(slug, "UTF-8") +
            "/feed?since=" + URLEncoder.encode(since, "UTF-8") + "&limit=200"

        // Network call off the EDT.
        ApplicationManager.getApplication().executeOnPooledThread {
            try {
                val (status, body) = getJSON(url, apiKey, cfg.timeoutMs)
                SwingUtilities.invokeLater {
                    when (status) {
                        200 -> applyFeed(body)
                        401, 403 -> setStatus("Sign-in expired — run Start Onboarding.")
                        else -> setStatus("Feed fetch failed (HTTP $status).")
                    }
                }
            } catch (e: Exception) {
                SwingUtilities.invokeLater {
                    setStatus("Control plane unreachable: ${e.message}")
                }
            }
        }
    }

    private fun applyFeed(body: String) {
        val parsed = try {
            gson.fromJson(body, FeedResponse::class.java)
        } catch (_: Throwable) {
            setStatus("Couldn't parse feed JSON.")
            return
        }
        tableModel.replace(parsed.rows ?: emptyList())
        setStatus("Last refresh ${DateTimeFormatter.ISO_LOCAL_TIME.format(java.time.LocalTime.now())} · ${tableModel.rowCount} rows")
    }

    private fun setStatus(msg: String) { statusLabel.text = msg }

    private fun getJSON(url: String, bearer: String, timeoutMs: Int): Pair<Int, String> {
        val conn = (URI(url).toURL().openConnection() as HttpURLConnection).apply {
            requestMethod = "GET"
            setRequestProperty("Accept", "application/json")
            setRequestProperty("Authorization", "Bearer $bearer")
            connectTimeout = timeoutMs
            readTimeout = timeoutMs
        }
        val status = conn.responseCode
        val stream = if (status / 100 == 2) conn.inputStream else conn.errorStream
        val payload = stream?.bufferedReader(Charsets.UTF_8)?.use { it.readText() }.orEmpty()
        return status to payload
    }

    companion object {
        private val SLUG_KEY = com.intellij.openapi.util.Key.create<String>("codesec.toolwindow.slug")
    }
}

// ─── feed JSON shape ────────────────────────────────────────────────

private data class FeedRow(
    val id: Long = 0,
    val at: String = "",
    val kind: String = "",
    val api_key_id: String = "",
    val api_key_name: String = "",
    val api_key_prefix: String = "",
    val tenant_id: String = ""
)

private data class FeedResponse(
    val rows: List<FeedRow> = emptyList(),
    val counts_by_kind: Map<String, Long> = emptyMap(),
    val window_since: String = "",
    val generated_at: String = ""
)

private class FeedTableModel : AbstractTableModel() {
    private val columns = arrayOf("Time", "Kind", "API key", "Tenant")
    private var rows: List<FeedRow> = emptyList()

    fun replace(next: List<FeedRow>) {
        rows = next
        fireTableDataChanged()
    }

    override fun getRowCount(): Int = rows.size
    override fun getColumnCount(): Int = columns.size
    override fun getColumnName(c: Int): String = columns[c]
    override fun getValueAt(row: Int, col: Int): Any {
        val r = rows[row]
        return when (col) {
            0 -> r.at
            1 -> r.kind
            2 -> if (r.api_key_name.isNotBlank())
                "${r.api_key_name} (${r.api_key_prefix}…)"
            else "(anonymous)"
            3 -> r.tenant_id.ifBlank { "—" }
            else -> ""
        }
    }
}

// Silence "JsonObject unused" — keeps the import live for future
// per-row inspection.
@Suppress("unused") private val _keep: JsonObject = JsonObject()
