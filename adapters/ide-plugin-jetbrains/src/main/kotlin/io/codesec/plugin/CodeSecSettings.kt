package io.codesec.plugin

import com.intellij.openapi.application.ApplicationManager
import com.intellij.openapi.components.PersistentStateComponent
import com.intellij.openapi.components.State
import com.intellij.openapi.components.Storage

/**
 * Persistent settings.
 *
 * The API key is INTENTIONALLY ABSENT from this state — it lives in the
 * PasswordSafe credential store (see [CodeSecCredentialStore]) so it
 * never ends up in IDE workspace XML on disk or in cloud-synced
 * settings. This mirrors VS Code's SecretStorage migration (Faz B2 of
 * ENTERPRISE_IDE_INTEGRATION).
 */
@State(name = "CodeSecSettings", storages = [Storage("codesec.xml")])
class CodeSecSettings : PersistentStateComponent<CodeSecSettings.State> {

    data class State(
        // Engine endpoint — scan / health.
        var backendAddress: String = "http://127.0.0.1:8080",

        // CodeSec proxy endpoint — written into ~/.claude/settings.json
        // as ANTHROPIC_BASE_URL when Claude Code protection is enabled.
        var proxyURL: String = "http://localhost:8091",

        // MCP SSE endpoint — registered as the `codesec` MCP server in
        // ~/.claude/settings.json.
        var mcpURL: String = "http://localhost:8090/sse",

        // Web console — used for deep links from the IDE (onboarding,
        // "open in browser" buttons).
        var controlURL: String = "http://localhost:5173",

        var enabled: Boolean = true,
        var scanOnSave: Boolean = true,
        var scanOnPaste: Boolean = true,
        var policyProfile: String = "default",
        var showInlineWarnings: Boolean = true,
        var timeoutMs: Int = 5000,

        // True once the user has finished the onboarding handshake at
        // least once. Surfaced as a one-time notification gate.
        var onboarded: Boolean = false
    )

    private var myState = State()

    override fun getState(): State = myState

    override fun loadState(state: State) {
        myState = state
    }

    companion object {
        fun getInstance(): CodeSecSettings =
            ApplicationManager.getApplication().getService(CodeSecSettings::class.java)
    }
}
