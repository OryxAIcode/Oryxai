package io.codesec.plugin.actions

import com.intellij.openapi.actionSystem.AnAction
import com.intellij.openapi.actionSystem.AnActionEvent
import io.codesec.plugin.Onboarding

class SignOutAction : AnAction() {
    override fun actionPerformed(e: AnActionEvent) {
        Onboarding.signOut(e.project)
    }
}
