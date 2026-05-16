package io.codesec.plugin.actions

import com.intellij.ide.BrowserUtil
import com.intellij.openapi.actionSystem.AnAction
import com.intellij.openapi.actionSystem.AnActionEvent
import com.intellij.openapi.ui.Messages
import io.codesec.plugin.ClaudeCodeConfig
import io.codesec.plugin.CodeSecCredentialStore
import io.codesec.plugin.CodeSecSettings

class EnableClaudeCodeAction : AnAction() {
    override fun actionPerformed(e: AnActionEvent) {
        val s = CodeSecSettings.getInstance().state
        val apiKey = CodeSecCredentialStore.readApiKey()
        if (apiKey.isBlank()) {
            val choice = Messages.showYesNoDialog(e.project,
                "No CodeSec API key on file. Run Start Onboarding first?",
                "CodeSec",
                Messages.getQuestionIcon())
            if (choice == Messages.YES) {
                io.codesec.plugin.Onboarding.start(e.project)
            }
            return
        }
        try {
            val res = ClaudeCodeConfig.enable(apiKey, s.proxyURL, s.mcpURL)
            Messages.showInfoMessage(e.project,
                "Claude Code protection ${if (res.firstEnable) "enabled" else "refreshed"}.\n\n" +
                    "Settings file: ${res.path}\n\n${res.hint}",
                "CodeSec")
        } catch (ex: IllegalStateException) {
            // Refuse-to-clobber path.
            Messages.showWarningDialog(e.project, ex.message ?: "Refusing to overwrite.",
                "CodeSec — won't touch existing config")
        } catch (ex: Throwable) {
            Messages.showErrorDialog(e.project,
                "Could not enable Claude Code protection: ${ex.message}",
                "CodeSec")
        }
        @Suppress("UNUSED_VARIABLE") val _keep = BrowserUtil::class // keep import alive
    }
}
