package io.codesec.plugin

import com.google.gson.Gson
import com.google.gson.JsonObject
import com.intellij.ide.BrowserUtil
import com.intellij.openapi.application.ApplicationManager
import com.intellij.openapi.progress.ProgressIndicator
import com.intellij.openapi.progress.Task
import com.intellij.openapi.project.Project
import com.intellij.openapi.ui.Messages
import java.net.HttpURLConnection
import java.net.URI
import java.net.URLEncoder
import java.security.SecureRandom

/**
 * Browser-mediated sign-in flow (ENTERPRISE_IDE_INTEGRATION §B4 — JetBrains
 * counterpart of `adapters/ide-plugin-vscode/src/onboarding.ts`).
 *
 * The VS Code flow uses a `vscode://` deep link because VS Code's URI
 * handler API is first-class. JetBrains URI handling is per-IDE flavour
 * and per-OS-registration, which is too fragile for an enterprise
 * rollout. We instead use a copy-paste handshake:
 *
 *   1. start()          generate state, open browser at
 *                       /extension-signin?state=…&mode=copy
 *   2. (browser leg)    user picks workspace + key label; the SPA
 *                       Finalize-s the row and shows the user the
 *                       state as a copyable token instead of opening
 *                       a vscode:// deep link.
 *   3. promptPaste()    user pastes the state back into a JetBrains
 *                       input dialog; we POST it to
 *                       /extension-tokens/exchange and stash the
 *                       returned plaintext in PasswordSafe.
 *
 * Same single-use + 10-min TTL guarantees as VS Code — the row in
 * Postgres is identical.
 */
object Onboarding {

    private val gson = Gson()
    private val random = SecureRandom()

    /**
     * Mint a fresh 32-byte hex state, open the browser, then prompt
     * the user for the same state on their return. Runs all I/O on a
     * background task — the UI thread never blocks on the HTTP exchange.
     */
    fun start(project: Project?) {
        val s = CodeSecSettings.getInstance().state
        val state = mintState()
        val url = s.controlURL.trimEnd('/') +
            "/extension-signin?state=" + URLEncoder.encode(state, "UTF-8") +
            "&mode=copy"
        BrowserUtil.browse(url)

        ApplicationManager.getApplication().invokeLater {
            val pasted = Messages.showInputDialog(
                project,
                """
                Finish sign-in in your browser, then paste the sign-in token
                here. The token starts with the same characters you can see
                in the URL — copy it from the SPA's "Copy token" button.

                The plaintext API key is delivered to this IDE directly and
                stored in your OS keychain. You will not see it on screen.
                """.trimIndent(),
                "CodeSec — Paste sign-in token",
                Messages.getInformationIcon(),
                state,
                null,
            )?.trim().orEmpty()
            if (pasted.isEmpty()) return@invokeLater
            if (!validState(pasted)) {
                Messages.showErrorDialog(
                    project,
                    "That doesn't look like a CodeSec sign-in token (expected 32+ hex characters).",
                    "CodeSec",
                )
                return@invokeLater
            }
            exchange(project, pasted)
        }
    }

    /** Wipe the stored key and reset the onboarded flag. */
    fun signOut(project: Project?) {
        val confirm = Messages.showYesNoDialog(
            project,
            "Forget the CodeSec API key in your OS keychain? Scans will fail until you onboard again.",
            "CodeSec — Sign out",
            Messages.getWarningIcon(),
        )
        if (confirm != Messages.YES) return
        CodeSecCredentialStore.forgetApiKey()
        CodeSecSettings.getInstance().state.onboarded = false
        Messages.showInfoMessage(project,
            "CodeSec key removed. Run 'Tools → CodeSec → Start Onboarding' to reconnect.",
            "CodeSec")
    }

    // ─── helpers ─────────────────────────────────────────────────────

    private fun exchange(project: Project?, state: String) {
        // Run the network call on a background task; the input dialog
        // already returned, so the UI thread is free again.
        val task = object : Task.Backgroundable(project, "CodeSec — finishing sign-in", false) {
            override fun run(indicator: ProgressIndicator) {
                val cfg = CodeSecSettings.getInstance().state
                val endpoint = cfg.controlURL.trimEnd('/') +
                    "/api/v1/extension-tokens/exchange"
                val body = "{\"state\":\"" + state.replace("\"", "") + "\"}"

                val (status, payload) = try {
                    postJSON(endpoint, body, 10_000)
                } catch (e: Exception) {
                    notifyExchangeError(project, "Couldn't reach $endpoint: ${e.message}")
                    return
                }
                if (status / 100 != 2) {
                    notifyExchangeError(project,
                        "Server responded $status. $payload")
                    return
                }
                val parsed: JsonObject? = try {
                    gson.fromJson(payload, JsonObject::class.java)
                } catch (_: Throwable) { null }
                val apiKey = parsed?.get("api_key")?.takeIf { !it.isJsonNull }?.asString
                if (apiKey.isNullOrBlank()) {
                    notifyExchangeError(project, "Exchange response had no api_key.")
                    return
                }
                val orgSlug = parsed.get("org_slug")?.takeIf { !it.isJsonNull }?.asString.orEmpty()
                val keyName = parsed.get("key_name")?.takeIf { !it.isJsonNull }?.asString.orEmpty()

                CodeSecCredentialStore.storeApiKey(apiKey)
                CodeSecSettings.getInstance().state.onboarded = true

                ApplicationManager.getApplication().invokeLater {
                    Messages.showInfoMessage(project,
                        "Workspace: $orgSlug\nKey: ${keyName.ifBlank { "(unlabelled)" }}\n\n" +
                            "The plaintext key is now in your OS keychain. You can revoke it any " +
                            "time from the CodeSec web console → Workspace → API keys.",
                        "CodeSec connected ✔")
                }
            }
        }
        task.queue()
    }

    private fun mintState(): String {
        val b = ByteArray(16)
        random.nextBytes(b)
        return b.joinToString("") { "%02x".format(it) }
    }

    private fun validState(s: String): Boolean =
        s.length in 32..128 && s.all {
            (it in '0'..'9') || (it in 'a'..'f') || (it in 'A'..'F')
        }

    private fun postJSON(url: String, body: String, timeoutMs: Int): Pair<Int, String> {
        val conn = (URI(url).toURL().openConnection() as HttpURLConnection).apply {
            requestMethod = "POST"
            setRequestProperty("Content-Type", "application/json")
            setRequestProperty("Accept", "application/json")
            doOutput = true
            connectTimeout = timeoutMs
            readTimeout = timeoutMs
        }
        conn.outputStream.use { it.write(body.toByteArray(Charsets.UTF_8)) }
        val status = conn.responseCode
        val stream = if (status / 100 == 2) conn.inputStream else conn.errorStream
        val payload = stream?.bufferedReader(Charsets.UTF_8)?.use { it.readText() }.orEmpty()
        return status to payload
    }

    private fun notifyExchangeError(project: Project?, msg: String) {
        ApplicationManager.getApplication().invokeLater {
            Messages.showErrorDialog(project,
                "CodeSec onboarding failed.\n\n$msg\n\nRun the action again with a fresh sign-in.",
                "CodeSec")
        }
    }
}
