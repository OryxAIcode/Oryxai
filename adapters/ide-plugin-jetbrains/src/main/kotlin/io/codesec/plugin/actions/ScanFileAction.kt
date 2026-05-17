package io.codesec.plugin.actions

import com.intellij.openapi.actionSystem.AnAction
import com.intellij.openapi.actionSystem.AnActionEvent
import com.intellij.openapi.actionSystem.CommonDataKeys
import com.intellij.openapi.application.ApplicationManager
import com.intellij.openapi.wm.WindowManager
import io.codesec.plugin.CodeSecClient
import io.codesec.plugin.CodeSecStatusBarWidget

class ScanFileAction : AnAction() {

    private val client = CodeSecClient()

    override fun actionPerformed(e: AnActionEvent) {
        val editor = e.getData(CommonDataKeys.EDITOR) ?: return
        val file = e.getData(CommonDataKeys.VIRTUAL_FILE) ?: return
        val content = editor.document.text
        val language = file.extension?.lowercase() ?: ""
        val filePath = file.path
        val project = e.project ?: return

        val widget = WindowManager.getInstance().getStatusBar(project)
            ?.getWidget(CodeSecStatusBarWidget.ID) as? CodeSecStatusBarWidget
        widget?.setScanning()

        ApplicationManager.getApplication().executeOnPooledThread {
            try {
                val result = client.scanFullFile(content, mapLang(language), filePath)
                widget?.updateScanResult(result)

                ApplicationManager.getApplication().invokeLater {
                    val msg = if (result.findings.isEmpty()) {
                        "No issues found — no security issues detected."
                    } else {
                        buildString {
                            appendLine("${result.findings.size} finding(s):")
                            for (f in result.findings) {
                                appendLine("  [${f.severity}] ${f.rule_id}: ${f.message}")
                            }
                        }
                    }
                    com.intellij.openapi.ui.Messages.showInfoMessage(project, msg, "OryxAI Scan Result")
                }
            } catch (ex: Exception) {
                widget?.setError(ex.message ?: "unknown error")
                ApplicationManager.getApplication().invokeLater {
                    com.intellij.openapi.ui.Messages.showErrorDialog(
                        project,
                        "Could not reach OryxAI backend: ${ex.message}",
                        "OryxAI Error"
                    )
                }
            }
        }
    }

    private fun mapLang(ext: String): String = when (ext) {
        "py" -> "python"
        "go" -> "go"
        "js", "jsx" -> "javascript"
        "ts", "tsx" -> "typescript"
        "java" -> "java"
        "kt", "kts" -> "kotlin"
        "rs" -> "rust"
        "rb" -> "ruby"
        "php" -> "php"
        else -> ext
    }
}
