package io.codesec.plugin.actions

import com.intellij.openapi.actionSystem.AnAction
import com.intellij.openapi.actionSystem.AnActionEvent
import io.codesec.plugin.CodeSecSettings

class ToggleEnabledAction : AnAction() {

    override fun actionPerformed(e: AnActionEvent) {
        val state = CodeSecSettings.getInstance().state
        state.enabled = !state.enabled

        val status = if (state.enabled) "aktif" else "devre dışı"
        com.intellij.notification.NotificationGroupManager.getInstance()
            .getNotificationGroup("CodeSec Notifications")
            .createNotification(
                "CodeSec $status",
                "",
                if (state.enabled) com.intellij.notification.NotificationType.INFORMATION
                else com.intellij.notification.NotificationType.WARNING
            )
            .notify(e.project)
    }

    override fun update(e: AnActionEvent) {
        val enabled = CodeSecSettings.getInstance().state.enabled
        e.presentation.text = if (enabled) "CodeSec'i Devre Dışı Bırak" else "CodeSec'i Aktif Et"
    }
}
