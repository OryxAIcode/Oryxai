package io.codesec.plugin

import com.intellij.credentialStore.CredentialAttributes
import com.intellij.credentialStore.Credentials
import com.intellij.credentialStore.generateServiceName
import com.intellij.ide.passwordSafe.PasswordSafe

/**
 * Thin wrapper over IntelliJ's PasswordSafe so the rest of the plugin
 * doesn't have to know that the keychain attribute key is
 * "CodeSec.APIKey" or that delete-by-set-null is the idiom. Mirrors
 * VS Code's `onboarding.ts::{storeAPIKey, readAPIKey, forgetAPIKey}`
 * surface — same names, same semantics — so the two adapters stay
 * easy to reason about side-by-side.
 *
 * PasswordSafe defers to the OS keychain when one is available:
 *  - macOS Keychain
 *  - Windows Credential Manager
 *  - Linux Secret Service (libsecret) or KWallet
 *
 * On hosts without an OS keychain the data falls through to PasswordSafe's
 * in-memory store; this is acceptable for dev environments but operators
 * running in regulated contexts should pre-flight the keychain.
 */
object CodeSecCredentialStore {

    private const val SERVICE = "CodeSec"
    private const val USER    = "apiKey"

    private val attrs by lazy {
        // generateServiceName prefixes the IDE name so multiple CodeSec
        // installs across IntelliJ flavours don't collide.
        CredentialAttributes(generateServiceName(SERVICE, USER))
    }

    /** Read the stored CodeSec API key, or "" when nothing's saved. */
    fun readApiKey(): String =
        PasswordSafe.instance.getPassword(attrs).orEmpty()

    /** Persist (or overwrite) the CodeSec API key. */
    fun storeApiKey(plain: String) {
        PasswordSafe.instance.set(attrs, Credentials(USER, plain))
    }

    /** Remove the API key. Setting credentials to null is the documented PasswordSafe idiom. */
    fun forgetApiKey() {
        PasswordSafe.instance.set(attrs, null)
    }

    /** True when a non-empty key is on file. */
    fun hasApiKey(): Boolean = readApiKey().isNotBlank()
}
