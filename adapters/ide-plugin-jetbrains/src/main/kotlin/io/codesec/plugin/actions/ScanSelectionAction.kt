package io.codesec.plugin.actions

import com.intellij.openapi.actionSystem.AnAction
import com.intellij.openapi.actionSystem.AnActionEvent
import com.intellij.openapi.actionSystem.CommonDataKeys
import com.intellij.openapi.application.ApplicationManager
import com.intellij.openapi.wm.WindowManager
import io.codesec.plugin.CodeSecClient
import io.codesec.plugin.CodeSecStatusBarWidget

class ScanSelectionAction : AnAction() {

    private val client = CodeSecClient()

    override fun actionPerformed(e: AnActionEvent) {
        val editor = e.getData(CommonDataKeys.EDITOR) ?: return
        val file = e.getData(CommonDataKeys.VIRTUAL_FILE) ?: return
        val project = e.project ?: return

        val selection = editor.selectionModel.selectedText
        if (selection.isNullOrBlank()) {
            com.intellij.openapi.ui.Messages.showWarningDialog(
                project, "Lütfen taramak istediğiniz kodu seçin.", "CodeSec"
            )
            return
        }

        val language = file.extension?.lowercase() ?: ""
        val filePath = file.path

        val widget = WindowManager.getInstance().getStatusBar(project)
            ?.getWidget(CodeSecStatusBarWidget.ID) as? CodeSecStatusBarWidget
        widget?.setScanning()

        ApplicationManager.getApplication().executeOnPooledThread {
            try {
                val result = client.scanCompletion(selection, mapLang(language), filePath)
                widget?.updateScanResult(result)

                ApplicationManager.getApplication().invokeLater {
                    val msg = if (result.findings.isEmpty()) {
                        "Seçili kod temiz — güvenlik sorunu bulunamadı."
                    } else {
                        buildString {
                            appendLine("${result.findings.size} bulgu:")
                            for (f in result.findings) {
                                appendLine("  [${f.severity}] ${f.rule_id}: ${f.message}")
                            }
                        }
                    }
                    com.intellij.openapi.ui.Messages.showInfoMessage(project, msg, "CodeSec Tarama Sonucu")
                }
            } catch (ex: Exception) {
                widget?.setError(ex.message ?: "bilinmeyen hata")
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
        else -> ext
    }
}
