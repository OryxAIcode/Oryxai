package io.codesec.plugin.actions

import com.intellij.openapi.actionSystem.AnAction
import com.intellij.openapi.actionSystem.AnActionEvent
import com.intellij.openapi.ui.Messages
import io.codesec.plugin.ClaudeCodeConfig

class DisableClaudeCodeAction : AnAction() {
    override fun actionPerformed(e: AnActionEvent) {
        try {
            val res = ClaudeCodeConfig.disable()
            Messages.showInfoMessage(e.project, res.hint, "CodeSec")
        } catch (ex: Throwable) {
            Messages.showErrorDialog(e.project,
                "Could not disable Claude Code protection: ${ex.message}",
                "CodeSec")
        }
    }
}
