package io.codesec.plugin.actions

import com.intellij.openapi.actionSystem.AnAction
import com.intellij.openapi.actionSystem.AnActionEvent
import io.codesec.plugin.Onboarding

class StartOnboardingAction : AnAction() {
    override fun actionPerformed(e: AnActionEvent) {
        Onboarding.start(e.project)
    }
}
