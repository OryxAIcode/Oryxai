package io.codesec.plugin

import com.google.gson.Gson
import com.google.gson.GsonBuilder
import com.google.gson.JsonObject
import com.google.gson.JsonParser
import java.io.IOException
import java.nio.file.Files
import java.nio.file.Path
import java.nio.file.Paths
import java.nio.file.StandardCopyOption
import java.nio.file.StandardOpenOption
import java.nio.file.attribute.PosixFilePermission
import java.nio.file.attribute.PosixFilePermissions

/**
 * Atomic writer for ~/.claude/settings.json — Kotlin port of
 * `adapters/ide-plugin-vscode/src/claudeCode.ts`. Same contract, same
 * design constraints (ENTERPRISE_IDE_INTEGRATION §A3):
 *
 *  - Plaintext upstream provider keys NEVER reach this file.
 *  - Existing user configuration survives: other mcpServers stay, other
 *    env vars stay. We only own the keys we wrote (sentinel-guarded).
 *  - First-enable backs up the pre-existing file once.
 *  - Atomic write (tmp + ATOMIC_MOVE) so a half-written settings.json
 *    can never brick Claude Code.
 *  - Refuse-to-clobber: a non-vanilla ANTHROPIC_BASE_URL throws — we
 *    won't silently override a corporate proxy.
 */
object ClaudeCodeConfig {

    /** Marker stamped into settings.json so disable() knows what we own. */
    private const val SENTINEL_KEY = "codesecManaged"
    private const val SENTINEL_VERSION = 1
    private val OWNED_ENV_KEYS = listOf("ANTHROPIC_BASE_URL", "ANTHROPIC_AUTH_TOKEN")
    private val OWNED_MCP_KEYS = listOf("codesec")

    private val gson: Gson = GsonBuilder().setPrettyPrinting().disableHtmlEscaping().create()

    data class Locations(val homedir: Path) {
        fun settings(): Path = homedir.resolve(".claude").resolve("settings.json")
        fun backup(): Path  = homedir.resolve(".claude").resolve("settings.json.codesec-backup")
    }

    fun defaultPaths(): Locations = Locations(Paths.get(System.getProperty("user.home")))

    data class WriteResult(val firstEnable: Boolean, val path: Path, val hint: String)

    /**
     * Stamp CodeSec config into settings.json. Idempotent — calling
     * twice just refreshes the values.
     *
     * @throws IllegalStateException when ANTHROPIC_BASE_URL is already
     *         set to a non-vanilla URL and we have no prior sentinel
     *         (i.e. the user manually pointed Claude Code somewhere
     *         bespoke; we'd rather error loudly than silently swap).
     */
    @Throws(IOException::class)
    fun enable(apiKey: String, proxyURL: String, mcpURL: String,
               paths: Locations = defaultPaths()): WriteResult {
        validateConfig(apiKey, proxyURL, mcpURL)

        val file = paths.settings()
        Files.createDirectories(file.parent)

        val loaded = loadOrInit(file, paths)
        val settings = loaded.settings
        val firstEnable = loaded.firstEnable

        val env = jsonOrEmpty(settings, "env")
        val existingBase = env.optString("ANTHROPIC_BASE_URL")
        val ours = settings.has(SENTINEL_KEY)
        if (existingBase.isNotBlank() && !ours && !isVanillaAnthropicURL(existingBase)) {
            throw IllegalStateException(
                "~/.claude/settings.json already sets ANTHROPIC_BASE_URL=$existingBase. " +
                "Refusing to overwrite a custom upstream. Remove it manually first."
            )
        }

        env.addProperty("ANTHROPIC_BASE_URL", proxyURL.trimEnd('/'))
        env.addProperty("ANTHROPIC_AUTH_TOKEN", apiKey)
        settings.add("env", env)

        val mcp = jsonOrEmpty(settings, "mcpServers")
        val codesec = JsonObject().apply {
            addProperty("type", "sse")
            addProperty("url", mcpURL)
            val headers = JsonObject()
            headers.addProperty("Authorization", "Bearer $apiKey")
            add("headers", headers)
        }
        mcp.add("codesec", codesec)
        settings.add("mcpServers", mcp)

        val sentinel = JsonObject()
        sentinel.addProperty("version", SENTINEL_VERSION)
        sentinel.add("envKeys", gson.toJsonTree(OWNED_ENV_KEYS))
        sentinel.add("mcpServerKeys", gson.toJsonTree(OWNED_MCP_KEYS))
        settings.add(SENTINEL_KEY, sentinel)

        atomicWriteJSON(file, settings)
        return WriteResult(
            firstEnable = firstEnable,
            path = file,
            hint = if (firstEnable)
                "Backup written to settings.json.codesec-backup. Restart Claude Code (claude /restart) to apply."
            else
                "Settings refreshed. Restart Claude Code if it's already running."
        )
    }

    /**
     * Remove only the keys CodeSec previously wrote. If a backup exists
     * we restore the original env values; otherwise we just delete.
     */
    @Throws(IOException::class)
    fun disable(paths: Locations = defaultPaths()): WriteResult {
        val file = paths.settings()
        if (!Files.exists(file)) {
            return WriteResult(false, file, "Nothing to disable.")
        }
        val raw = Files.readString(file)
        if (raw.isBlank()) {
            return WriteResult(false, file, "Nothing to disable.")
        }
        val settings = JsonParser.parseString(raw).asJsonObject
        if (!settings.has(SENTINEL_KEY)) {
            return WriteResult(false, file, "Settings file isn't managed by CodeSec — nothing changed.")
        }

        val restored = if (Files.exists(paths.backup())) {
            try {
                JsonParser.parseString(Files.readString(paths.backup())).asJsonObject
            } catch (_: Throwable) {
                JsonObject()
            }
        } else JsonObject()

        if (settings.has("env")) {
            val env = settings.getAsJsonObject("env")
            for (k in OWNED_ENV_KEYS) {
                val originalEnv = if (restored.has("env"))
                    restored.getAsJsonObject("env").get(k) else null
                if (originalEnv != null && !originalEnv.isJsonNull) {
                    env.add(k, originalEnv)
                } else {
                    env.remove(k)
                }
            }
            if (env.size() == 0) settings.remove("env")
        }
        if (settings.has("mcpServers")) {
            val mcp = settings.getAsJsonObject("mcpServers")
            for (k in OWNED_MCP_KEYS) {
                val originalMcp = if (restored.has("mcpServers"))
                    restored.getAsJsonObject("mcpServers").get(k) else null
                if (originalMcp != null && !originalMcp.isJsonNull) {
                    mcp.add(k, originalMcp)
                } else {
                    mcp.remove(k)
                }
            }
            if (mcp.size() == 0) settings.remove("mcpServers")
        }
        settings.remove(SENTINEL_KEY)

        atomicWriteJSON(file, settings)
        return WriteResult(false, file, "CodeSec disabled. Restart Claude Code to apply.")
    }

    data class Status(
        val exists: Boolean,
        val managed: Boolean,
        val anthropicBaseURL: String?,
        val mcpHasCodesec: Boolean,
        val path: Path
    )

    /** Snapshot for diagnose-style display. Never reveals the plaintext key. */
    @Throws(IOException::class)
    fun status(paths: Locations = defaultPaths()): Status {
        val file = paths.settings()
        if (!Files.exists(file)) {
            return Status(false, false, null, false, file)
        }
        val raw = Files.readString(file)
        if (raw.isBlank()) return Status(true, false, null, false, file)
        val settings = JsonParser.parseString(raw).asJsonObject
        val env = if (settings.has("env")) settings.getAsJsonObject("env") else null
        val mcp = if (settings.has("mcpServers")) settings.getAsJsonObject("mcpServers") else null
        return Status(
            exists = true,
            managed = settings.has(SENTINEL_KEY),
            anthropicBaseURL = env?.get("ANTHROPIC_BASE_URL")?.takeIf { !it.isJsonNull }?.asString,
            mcpHasCodesec = mcp?.has("codesec") == true,
            path = file
        )
    }

    // ─── helpers ─────────────────────────────────────────────────────

    private fun validateConfig(apiKey: String, proxyURL: String, mcpURL: String) {
        require(apiKey.matches(Regex("^csk_(live|test)_.+"))) {
            "apiKey must be a CodeSec key (csk_live_… or csk_test_…)"
        }
        require(proxyURL.matches(Regex("^https?://.+"))) {
            "proxyURL must start with http:// or https://"
        }
        require(mcpURL.matches(Regex("^https?://.+")) && mcpURL.contains("/sse")) {
            "mcpURL must be the SSE endpoint, e.g. http://host:8090/sse"
        }
    }

    private fun isVanillaAnthropicURL(u: String): Boolean {
        val norm = u.replace(Regex("^https?://"), "").trimEnd('/')
        return norm == "api.anthropic.com"
    }

    private data class Loaded(val settings: JsonObject, val firstEnable: Boolean)

    private fun loadOrInit(file: Path, paths: Locations): Loaded {
        if (!Files.exists(file)) return Loaded(JsonObject(), firstEnable = true)
        val raw = Files.readString(file)
        if (raw.isBlank()) return Loaded(JsonObject(), firstEnable = true)
        val settings = try {
            JsonParser.parseString(raw).asJsonObject
        } catch (e: Exception) {
            throw IOException(
                "~/.claude/settings.json is not valid JSON: ${e.message}. " +
                "Move it aside and retry."
            )
        }
        // First-enable backup. Sentinel tells subsequent runs to skip
        // overwriting the backup.
        val firstEnable = !settings.has(SENTINEL_KEY)
        if (firstEnable) {
            try {
                Files.write(paths.backup(), raw.toByteArray(),
                    StandardOpenOption.CREATE, StandardOpenOption.TRUNCATE_EXISTING)
                applyOwnerOnlyPerms(paths.backup())
            } catch (_: Throwable) { /* best-effort */ }
        }
        return Loaded(settings, firstEnable)
    }

    private fun jsonOrEmpty(parent: JsonObject, key: String): JsonObject =
        if (parent.has(key) && parent.get(key).isJsonObject)
            parent.getAsJsonObject(key)
        else JsonObject()

    private fun JsonObject.optString(key: String): String {
        if (!has(key)) return ""
        val v = get(key)
        return if (v.isJsonNull) "" else v.asString
    }

    @Throws(IOException::class)
    private fun atomicWriteJSON(file: Path, value: JsonObject) {
        val tmp = file.parent.resolve(file.fileName.toString() + ".tmp." + System.currentTimeMillis())
        val body = gson.toJson(value) + "\n"
        Files.write(tmp, body.toByteArray(),
            StandardOpenOption.CREATE, StandardOpenOption.TRUNCATE_EXISTING)
        applyOwnerOnlyPerms(tmp)
        try {
            Files.move(tmp, file, StandardCopyOption.ATOMIC_MOVE, StandardCopyOption.REPLACE_EXISTING)
        } catch (e: Exception) {
            try { Files.deleteIfExists(tmp) } catch (_: Throwable) {}
            throw e
        }
    }

    /** Best-effort 0o600. Silently skips on Windows / non-POSIX filesystems. */
    private fun applyOwnerOnlyPerms(file: Path) {
        try {
            Files.setPosixFilePermissions(file, PosixFilePermissions.fromString("rw-------"))
        } catch (_: UnsupportedOperationException) {
            // Windows — handled by ACL defaults; nothing to do.
        } catch (_: Throwable) {
            // Don't fail the whole write just because chmod failed.
        }
        // Compile-time reference so the enum stays linked even when the
        // setPosix path no-ops on Windows.
        val _check: PosixFilePermission = PosixFilePermission.OWNER_READ
        @Suppress("UNUSED_VARIABLE") val _ignore = _check
    }
}
