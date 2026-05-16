// claudeCode.ts — atomic writer for ~/.claude/settings.json so that the
// Claude Code CLI routes its Anthropic calls through CodeSec proxy AND
// registers the CodeSec MCP server.
//
// Design constraints (ENTERPRISE_IDE_INTEGRATION §A3):
//  - Plaintext upstream provider keys NEVER reach this file. The proxy
//    holds them server-side; settings.json only carries the CodeSec
//    csk_live_… (which lets the proxy resolve org → upstream key).
//  - Existing user configuration MUST survive: other mcpServers stay,
//    other env vars stay. We only own the three keys we wrote.
//  - Operator should be able to revert with one command. We back up the
//    pre-existing file on the first enable so disable can restore it.
//  - Write must be atomic — a half-written settings.json would brick
//    Claude Code on next launch. tmp + rename pattern.

import * as fs from "fs/promises";
import * as os from "os";
import * as path from "path";

/** Resolved file paths. Override the homedir for unit tests. */
export interface ClaudeCodePaths {
  homedir: string;
}

export function defaultPaths(): ClaudeCodePaths {
  return { homedir: os.homedir() };
}

export function settingsPath(p: ClaudeCodePaths): string {
  return path.join(p.homedir, ".claude", "settings.json");
}

function backupPath(p: ClaudeCodePaths): string {
  return path.join(p.homedir, ".claude", "settings.json.codesec-backup");
}

/** Knobs the CodeSec extension stamps into the user's Claude Code config. */
export interface CodeSecClaudeConfig {
  /** CodeSec API key (csk_live_…). */
  apiKey: string;
  /** Proxy base URL (e.g. http://localhost:8091). MUST NOT end in /. */
  proxyURL: string;
  /** MCP SSE URL (e.g. http://localhost:8090/sse). */
  mcpURL: string;
}

/** Logger contract — the extension passes vscode.OutputChannel; tests pass a no-op. */
export interface Logger {
  info(msg: string): void;
  warn(msg: string): void;
  error(msg: string): void;
}

/**
 * Result describes what happened. Surfaced to the caller for status-bar
 * + InformationMessage purposes.
 */
export interface WriteResult {
  /** True the first time we enabled — backup was just written. */
  firstEnable: boolean;
  /** Resolved settings file path. */
  path: string;
  /** Hint for the user — e.g. "Restart Claude Code to apply". */
  hint: string;
}

const SENTINEL_KEY = "codesecManaged";

interface ClaudeSettings {
  env?: Record<string, string>;
  mcpServers?: Record<string, unknown>;
  // codesecManaged is OUR sentinel — we only own keys whose values match
  // what we wrote; everything else stays untouched.
  [SENTINEL_KEY]?: {
    version: number;
    envKeys: string[];
    mcpServerKeys: string[];
  };
  [k: string]: unknown;
}

const SENTINEL_VERSION = 1;
const OWNED_ENV_KEYS = ["ANTHROPIC_BASE_URL", "ANTHROPIC_AUTH_TOKEN"];
const OWNED_MCP_KEYS = ["codesec"];

/**
 * enable writes the CodeSec config into ~/.claude/settings.json. It is
 * idempotent and safe to call repeatedly; the second call simply refreshes
 * the values that drift (key rotated, URL changed).
 *
 * Throws on filesystem permissions failure. The caller decides whether to
 * show that to the user.
 */
export async function enable(
  cfg: CodeSecClaudeConfig,
  paths: ClaudeCodePaths = defaultPaths(),
  logger?: Logger,
): Promise<WriteResult> {
  validateConfig(cfg);

  const file = settingsPath(paths);
  await fs.mkdir(path.dirname(file), { recursive: true });

  const { settings, firstEnable } = await loadOrInit(file, paths, logger);

  // Refuse to clobber unless the user already opted-in OR the existing
  // ANTHROPIC_BASE_URL is the default upstream. This is the rare case
  // where a user manually pointed Claude Code somewhere bespoke (their
  // own proxy, an air-gapped endpoint) — we'd rather error loudly than
  // silently swap their pipeline.
  const existingBase = settings.env?.ANTHROPIC_BASE_URL?.trim();
  const ours = !!settings[SENTINEL_KEY];
  if (existingBase && !ours && !isVanillaAnthropicURL(existingBase)) {
    throw new Error(
      `~/.claude/settings.json already sets ANTHROPIC_BASE_URL=${existingBase}. ` +
        `Refusing to overwrite a custom upstream. Remove it manually or pass --force.`,
    );
  }

  if (!settings.env) settings.env = {};
  if (!settings.mcpServers) settings.mcpServers = {};

  settings.env.ANTHROPIC_BASE_URL = cfg.proxyURL.replace(/\/+$/, "");
  settings.env.ANTHROPIC_AUTH_TOKEN = cfg.apiKey;

  settings.mcpServers["codesec"] = {
    type: "sse",
    url: cfg.mcpURL,
    headers: {
      Authorization: `Bearer ${cfg.apiKey}`,
    },
  };

  settings[SENTINEL_KEY] = {
    version: SENTINEL_VERSION,
    envKeys: OWNED_ENV_KEYS.slice(),
    mcpServerKeys: OWNED_MCP_KEYS.slice(),
  };

  await atomicWriteJSON(file, settings);

  return {
    firstEnable,
    path: file,
    hint: firstEnable
      ? "Backup written to settings.json.codesec-backup. Restart Claude Code (claude /restart) to apply."
      : "Settings refreshed. Restart Claude Code (claude /restart) if it's already running.",
  };
}

/**
 * disable removes ONLY the keys CodeSec previously wrote, leaving the
 * rest of the user's config alone. When a backup exists and the current
 * managed envKeys exactly match what we'd touch, we additionally restore
 * any pre-existing values from the backup — so a user who had a custom
 * ANTHROPIC_AUTH_TOKEN before enabling sees it return.
 */
export async function disable(
  paths: ClaudeCodePaths = defaultPaths(),
  logger?: Logger,
): Promise<WriteResult> {
  const file = settingsPath(paths);
  let settings: ClaudeSettings;
  try {
    settings = await readJSON<ClaudeSettings>(file);
  } catch (err: any) {
    if (err?.code === "ENOENT") {
      logger?.info("disable: settings.json missing — nothing to do");
      return { firstEnable: false, path: file, hint: "Nothing to disable." };
    }
    throw err;
  }

  const sentinel = settings[SENTINEL_KEY];
  if (!sentinel) {
    return {
      firstEnable: false,
      path: file,
      hint: "Settings file isn't managed by CodeSec — nothing changed.",
    };
  }

  // Try to restore the backup. If it exists, we replace only the keys
  // sentinel says we own; everything else stays as it is right now.
  let restored: ClaudeSettings = {};
  try {
    restored = await readJSON<ClaudeSettings>(backupPath(paths));
  } catch {
    // No backup — fall back to plain deletion below.
  }

  if (settings.env) {
    for (const k of sentinel.envKeys ?? []) {
      const original = restored.env?.[k];
      if (original !== undefined) {
        settings.env[k] = original;
      } else {
        delete settings.env[k];
      }
    }
    if (Object.keys(settings.env).length === 0) delete settings.env;
  }
  if (settings.mcpServers) {
    for (const k of sentinel.mcpServerKeys ?? []) {
      const original = restored.mcpServers?.[k];
      if (original !== undefined) {
        settings.mcpServers[k] = original;
      } else {
        delete settings.mcpServers[k];
      }
    }
    if (Object.keys(settings.mcpServers).length === 0) delete settings.mcpServers;
  }
  delete settings[SENTINEL_KEY];

  await atomicWriteJSON(file, settings);
  return {
    firstEnable: false,
    path: file,
    hint: "CodeSec disabled. Restart Claude Code to apply.",
  };
}

/** status returns a snapshot of the current config — used by the diagnose command. */
export async function status(paths: ClaudeCodePaths = defaultPaths()): Promise<{
  exists: boolean;
  managed: boolean;
  anthropicBaseURL: string | undefined;
  mcpHasCodesec: boolean;
  path: string;
}> {
  const file = settingsPath(paths);
  try {
    const settings = await readJSON<ClaudeSettings>(file);
    return {
      exists: true,
      managed: !!settings[SENTINEL_KEY],
      anthropicBaseURL: settings.env?.ANTHROPIC_BASE_URL,
      mcpHasCodesec: !!settings.mcpServers?.["codesec"],
      path: file,
    };
  } catch (err: any) {
    if (err?.code === "ENOENT") {
      return {
        exists: false,
        managed: false,
        anthropicBaseURL: undefined,
        mcpHasCodesec: false,
        path: file,
      };
    }
    throw err;
  }
}

// ─── helpers ──────────────────────────────────────────────────────────

function validateConfig(c: CodeSecClaudeConfig): void {
  if (!c.apiKey || !/^csk_(live|test)_/.test(c.apiKey)) {
    throw new Error("apiKey must be a CodeSec key (csk_live_… or csk_test_…)");
  }
  if (!/^https?:\/\//.test(c.proxyURL)) {
    throw new Error("proxyURL must start with http:// or https://");
  }
  if (!/^https?:\/\//.test(c.mcpURL) || !c.mcpURL.includes("/sse")) {
    throw new Error("mcpURL must be the SSE endpoint, e.g. http://host:8090/sse");
  }
}

function isVanillaAnthropicURL(u: string): boolean {
  // Strip trailing slashes + protocol-only diff so api.anthropic.com,
  // https://api.anthropic.com, https://api.anthropic.com/ all match.
  const norm = u.replace(/^https?:\/\//, "").replace(/\/+$/, "");
  return norm === "api.anthropic.com";
}

async function loadOrInit(
  file: string,
  paths: ClaudeCodePaths,
  logger?: Logger,
): Promise<{ settings: ClaudeSettings; firstEnable: boolean }> {
  let raw: string | null = null;
  try {
    raw = await fs.readFile(file, "utf8");
  } catch (err: any) {
    if (err?.code !== "ENOENT") throw err;
  }

  if (!raw || raw.trim() === "") {
    return { settings: {}, firstEnable: true };
  }

  let settings: ClaudeSettings;
  try {
    settings = JSON.parse(raw);
  } catch (err) {
    throw new Error(
      `~/.claude/settings.json is not valid JSON: ${(err as Error).message}. ` +
        `Move it aside and retry.`,
    );
  }

  // First-enable backup. We back up the file BEFORE we mutate it, exactly
  // once. The sentinel tells subsequent runs that there's no need to
  // overwrite the existing backup with our own writes.
  const firstEnable = !settings[SENTINEL_KEY];
  if (firstEnable) {
    try {
      await fs.writeFile(backupPath(paths), raw, { mode: 0o600 });
      logger?.info(`Backed up existing settings to ${backupPath(paths)}`);
    } catch (err) {
      logger?.warn(`Could not write backup: ${(err as Error).message}`);
    }
  }
  return { settings, firstEnable };
}

async function readJSON<T>(file: string): Promise<T> {
  const raw = await fs.readFile(file, "utf8");
  if (!raw.trim()) return {} as T;
  return JSON.parse(raw) as T;
}

async function atomicWriteJSON(file: string, value: unknown): Promise<void> {
  const tmp = file + ".tmp." + process.pid + "." + Date.now();
  const body = JSON.stringify(value, null, 2) + "\n";
  // Restrictive perms: settings.json may contain the csk_… and Anthropic
  // OAuth tokens. 0o600 keeps it out of "other" reach.
  await fs.writeFile(tmp, body, { mode: 0o600 });
  try {
    await fs.rename(tmp, file);
  } catch (err) {
    // Best-effort cleanup on rename failure.
    try { await fs.unlink(tmp); } catch {}
    throw err;
  }
}
