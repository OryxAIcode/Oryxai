package io.codesec.plugin

import com.intellij.openapi.application.ApplicationManager
import com.intellij.openapi.diagnostic.Logger
import com.intellij.openapi.editor.event.BulkAwareDocumentListener
import com.intellij.openapi.editor.event.DocumentEvent
import com.intellij.openapi.fileEditor.FileDocumentManager
import com.intellij.openapi.project.Project
import com.intellij.openapi.project.ProjectManager
import com.intellij.openapi.wm.WindowManager
import java.util.concurrent.ConcurrentHashMap

class CodeSecDocumentListener : BulkAwareDocumentListener.Simple {

    private val logger = Logger.getInstance(CodeSecDocumentListener::class.java)
    private val client = CodeSecClient()
    private val snapshots = ConcurrentHashMap<String, String>()

    override fun afterDocumentChange(event: DocumentEvent) {
        val settings = CodeSecSettings.getInstance().state
        if (!settings.enabled) return

        val doc = event.document
        val file = FileDocumentManager.getInstance().getFile(doc) ?: return
        val filePath = file.path
        val ext = file.extension?.lowercase() ?: return

        val supportedExtensions = setOf(
            "py", "go", "js", "ts", "jsx", "tsx", "java", "kt", "kts",
            "rs", "rb", "php", "c", "cpp", "cs", "swift", "sh", "tf"
        )
        if (ext !in supportedExtensions) return

        val inserted = event.newFragment.toString()
        val isPaste = inserted.length > 20 && inserted.contains('\n')

        if (isPaste && settings.scanOnPaste) {
            scanAsync(filePath, inserted, detectLang(ext), "")
        }
    }

    fun onFileSaved(filePath: String, content: String, ext: String) {
        val settings = CodeSecSettings.getInstance().state
        if (!settings.enabled || !settings.scanOnSave) return

        val oldContent = snapshots[filePath] ?: ""
        snapshots[filePath] = content
        scanAsync(filePath, content, detectLang(ext), oldContent)
    }

    fun captureSnapshot(filePath: String, content: String) {
        snapshots[filePath] = content
    }

    private fun scanAsync(filePath: String, content: String, language: String, oldContent: String) {
        ApplicationManager.getApplication().executeOnPooledThread {
            val widget = findWidget()
            widget?.setScanning()

            try {
                val result = client.scanCompletion(content, language, filePath, oldContent)
                widget?.updateScanResult(result)

                if (result.findings.isNotEmpty()) {
                    showNotification(result, filePath)
                }
            } catch (e: Exception) {
                logger.warn("CodeSec scan error: ${e.message}")
                widget?.setError(e.message ?: "bilinmeyen hata")
            }
        }
    }

    private fun showNotification(result: ScanResult, filePath: String) {
        val blocked = result.findings.count { it.action == "block" }
        val warnings = result.findings.count { it.action == "warn" || it.action == "flag" }
        val fileName = filePath.substringAfterLast('/')

        val title = when {
            blocked > 0 -> "⛔ CodeSec: $blocked güvenlik ihlali"
            warnings > 0 -> "⚠ CodeSec: $warnings uyarı"
            else -> return
        }

        val body = buildString {
            for (f in result.findings.take(5)) {
                appendLine("• [${f.severity}] ${f.rule_id}: ${f.message}")
            }
            if (result.findings.size > 5) {
                appendLine("… ve ${result.findings.size - 5} daha")
            }
        }

        val notificationType = if (blocked > 0) {
            com.intellij.notification.NotificationType.ERROR
        } else {
            com.intellij.notification.NotificationType.WARNING
        }

        com.intellij.notification.NotificationGroupManager.getInstance()
            .getNotificationGroup("CodeSec Notifications")
            .createNotification("$title ($fileName)", body, notificationType)
            .notify(currentProject())
    }

    private fun findWidget(): CodeSecStatusBarWidget? {
        val project = currentProject() ?: return null
        val bar = WindowManager.getInstance().getStatusBar(project)
        return bar?.getWidget(CodeSecStatusBarWidget.ID) as? CodeSecStatusBarWidget
    }

    private fun currentProject(): Project? =
        ProjectManager.getInstance().openProjects.firstOrNull()

    private fun detectLang(ext: String): String = when (ext) {
        "py" -> "python"
        "go" -> "go"
        "js", "jsx" -> "javascript"
        "ts", "tsx" -> "typescript"
        "java" -> "java"
        "kt", "kts" -> "kotlin"
        "rs" -> "rust"
        "rb" -> "ruby"
        "php" -> "php"
        "c", "h" -> "c"
        "cpp", "cc" -> "cpp"
        "cs" -> "csharp"
        "swift" -> "swift"
        "sh" -> "shell"
        "tf" -> "terraform"
        else -> ext
    }
}
