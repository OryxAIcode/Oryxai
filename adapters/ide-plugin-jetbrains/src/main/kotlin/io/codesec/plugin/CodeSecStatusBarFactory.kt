package io.codesec.plugin

import com.intellij.openapi.project.Project
import com.intellij.openapi.wm.StatusBar
import com.intellij.openapi.wm.StatusBarWidget
import com.intellij.openapi.wm.StatusBarWidgetFactory
import com.intellij.util.Consumer
import java.awt.event.MouseEvent
import javax.swing.Timer

class CodeSecStatusBarFactory : StatusBarWidgetFactory {
    override fun getId(): String = "CodeSecStatusBar"
    override fun getDisplayName(): String = "CodeSec"
    override fun isAvailable(project: Project): Boolean = true
    override fun createWidget(project: Project): StatusBarWidget = CodeSecStatusBarWidget(project)
    override fun disposeWidget(widget: StatusBarWidget) = widget.dispose()
    override fun canBeEnabledOn(statusBar: StatusBar): Boolean = true
}

class CodeSecStatusBarWidget(private val project: Project) : StatusBarWidget, StatusBarWidget.TextPresentation {

    companion object {
        const val ID = "CodeSecStatusBar"
    }

    @Volatile
    private var text = "$(shield) CodeSec"

    @Volatile
    private var tooltip = "CodeSec: starting…"

    @Volatile
    private var connected = false

    private var statusBar: StatusBar? = null
    private val client = CodeSecClient()

    private val healthTimer = Timer(15_000) { checkHealth() }.apply {
        isRepeats = true
        initialDelay = 2_000
    }

    override fun ID(): String = ID

    override fun getPresentation(): StatusBarWidget.WidgetPresentation = this

    override fun install(statusBar: StatusBar) {
        this.statusBar = statusBar
        healthTimer.start()
    }

    override fun dispose() {
        healthTimer.stop()
    }

    override fun getText(): String = text

    override fun getTooltipText(): String = tooltip

    override fun getAlignment(): Float = 0f

    override fun getClickConsumer(): Consumer<MouseEvent>? = Consumer {
        // future: popup menu
    }

    fun updateScanResult(result: ScanResult) {
        val blocked = result.findings.count { it.action == "block" }
        val warnings = result.findings.count { it.action == "warn" || it.action == "flag" }

        text = when {
            blocked > 0 -> "⛔ OryxAI: $blocked blocked"
            warnings > 0 -> "⚠ OryxAI: $warnings warning(s)"
            else -> "✅ OryxAI: clean"
        }
        tooltip = "Last scan: ${result.scan_time_ms}ms, decision: ${result.decision}"
        statusBar?.updateWidget(ID)
    }

    fun setScanning() {
        text = "⏳ OryxAI: scanning…"
        tooltip = "Scanning file"
        statusBar?.updateWidget(ID)
    }

    fun setError(msg: String) {
        text = "❌ OryxAI"
        tooltip = "Error: $msg"
        connected = false
        statusBar?.updateWidget(ID)
    }

    private fun checkHealth() {
        try {
            val h = client.healthCheck()
            connected = true
            if (text.startsWith("❌") || text.contains("starting")) {
                text = "✅ OryxAI v${h.version}"
                tooltip = "Connected — uptime ${h.uptime_seconds}s"
                statusBar?.updateWidget(ID)
            }
        } catch (e: Exception) {
            if (connected) {
                setError(e.message ?: "unknown error")
            }
        }
    }
}
