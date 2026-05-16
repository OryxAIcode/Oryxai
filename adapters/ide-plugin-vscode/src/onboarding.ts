// onboarding.ts — drives the deep-link sign-in handshake with the
// CodeSec control plane (ENTERPRISE_IDE_INTEGRATION §B1).
//
// Flow:
//   1. start()         — mint random 32-byte hex `state`, open browser
//                         at /extension-signin?state=…
//   2. (browser leg)   — user picks workspace + key label, control
//                         plane mints API key, opens vscode://…/cb?state=…
//   3. handleCallback  — VS Code URI handler captures the deep-link,
//                         POSTs /extension-tokens/exchange, persists
//                         plaintext to SecretStorage.
//
// The plaintext key crosses ONE process boundary (control plane →
// extension) and lives only in OS keychain afterwards. It never
// touches settings.json or any log line.

import * as crypto from "crypto";
import * as vscode from "vscode";

const SECRET_STATE_KEY    = "codesec.onboarding.state";
const SECRET_API_KEY_NAME = "codesec.apiKey";
const GLOBAL_ONBOARDED    = "codesec.onboarded";

export interface OnboardingDeps {
  /** OS keychain — vscode.ExtensionContext.secrets. */
  secrets: vscode.SecretStorage;
  /** Persistent flags — vscode.ExtensionContext.globalState. */
  globalState: vscode.Memento;
  /** Output channel for diagnose-style logs. */
  log: vscode.OutputChannel;
}

export interface OnboardingURLs {
  controlURL: string; // e.g. http://localhost:5173
}

/** Persist a freshly-minted CodeSec API key into the OS keychain. */
export async function storeAPIKey(deps: OnboardingDeps, plain: string): Promise<void> {
  await deps.secrets.store(SECRET_API_KEY_NAME, plain);
  await deps.globalState.update(GLOBAL_ONBOARDED, true);
}

/** Read the stored key (returns "" when not yet onboarded). */
export async function readAPIKey(deps: OnboardingDeps): Promise<string> {
  return (await deps.secrets.get(SECRET_API_KEY_NAME)) ?? "";
}

/** Clear the stored key + onboarding flag. */
export async function forgetAPIKey(deps: OnboardingDeps): Promise<void> {
  await deps.secrets.delete(SECRET_API_KEY_NAME);
  await deps.globalState.update(GLOBAL_ONBOARDED, false);
}

/**
 * start opens the browser at /extension-signin?state=… and remembers
 * the state in SecretStorage so handleCallback can verify it survived
 * an editor restart.
 */
export async function start(
  deps: OnboardingDeps,
  urls: OnboardingURLs,
): Promise<void> {
  const state = crypto.randomBytes(16).toString("hex");
  await deps.secrets.store(SECRET_STATE_KEY, state);

  const url = vscode.Uri.parse(
    urls.controlURL.replace(/\/+$/, "") +
      "/extension-signin?state=" +
      encodeURIComponent(state),
  );
  deps.log.appendLine(
    `[onboarding] opening browser → ${url.toString(true)}`,
  );
  const opened = await vscode.env.openExternal(url);
  if (!opened) {
    vscode.window.showWarningMessage(
      `Couldn't auto-open browser. Open this URL manually: ${url.toString(true)}`,
    );
  } else {
    vscode.window.showInformationMessage(
      "CodeSec sign-in opened in your browser. Pick your workspace and we'll bring you back.",
    );
  }
}

/**
 * handleCallback runs when VS Code receives a vscode://codesec.codesec/cb
 * deep-link from the SPA. We verify the state matches the one we issued,
 * call /extension-tokens/exchange, and stash the returned plaintext in
 * SecretStorage.
 */
export async function handleCallback(
  uri: vscode.Uri,
  deps: OnboardingDeps,
  urls: OnboardingURLs,
): Promise<void> {
  const params = new URLSearchParams(uri.query);
  const state  = params.get("state") ?? "";
  if (!state) {
    vscode.window.showErrorMessage(
      "CodeSec callback missing state. Run 'CodeSec: Start Onboarding' again.",
    );
    return;
  }

  const remembered = (await deps.secrets.get(SECRET_STATE_KEY)) ?? "";
  if (!remembered || remembered !== state) {
    vscode.window.showErrorMessage(
      "CodeSec sign-in state didn't match. " +
        "If you started this flow in another window, switch to it and try again.",
    );
    return;
  }
  // One-shot — discard the remembered state regardless of outcome so
  // a leaked deep-link can't be replayed on a future onboarding run.
  await deps.secrets.delete(SECRET_STATE_KEY);

  const exchangeURL = urls.controlURL.replace(/\/+$/, "") +
    "/api/v1/extension-tokens/exchange";
  deps.log.appendLine(`[onboarding] exchanging state at ${exchangeURL}`);

  try {
    const ctrl = new AbortController();
    const to   = setTimeout(() => ctrl.abort(), 10_000);
    const res  = await fetch(exchangeURL, {
      method: "POST",
      signal: ctrl.signal,
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ state }),
    });
    clearTimeout(to);

    if (!res.ok) {
      const txt = await res.text().catch(() => "");
      throw new Error(`exchange: ${res.status} ${res.statusText} ${txt}`);
    }
    const body = (await res.json()) as {
      api_key?: string;
      org_slug?: string;
      key_name?: string;
    };
    if (!body.api_key) {
      throw new Error("exchange response missing api_key");
    }
    await storeAPIKey(deps, body.api_key);

    deps.log.appendLine(
      `[onboarding] stored key '${body.key_name ?? "(unlabelled)"}' for workspace '${body.org_slug ?? "(unknown)"}'`,
    );
    vscode.window.showInformationMessage(
      `CodeSec connected ✔ Workspace: ${body.org_slug ?? "(unknown)"}. ` +
        `Key '${body.key_name ?? "VS Code"}' is now in your OS keychain.`,
    );
  } catch (err) {
    const msg = (err as Error).message ?? String(err);
    deps.log.appendLine(`[onboarding] exchange failed: ${msg}`);
    vscode.window.showErrorMessage(
      `CodeSec onboarding failed: ${msg}. Try 'CodeSec: Start Onboarding' again.`,
    );
  }
}

/**
 * legacyMigrate moves a plaintext apiKey from workspace settings into
 * SecretStorage (Faz B2). Idempotent — running it twice is a no-op
 * once the settings field is cleared.
 */
export async function legacyMigrate(deps: OnboardingDeps): Promise<boolean> {
  const cfg     = vscode.workspace.getConfiguration("codesec");
  const inPlain = cfg.get<string>("apiKey", "").trim();
  if (!inPlain) return false;

  // If something is already in the keychain DON'T overwrite — the
  // settings.json value may be stale.
  const existing = await deps.secrets.get(SECRET_API_KEY_NAME);
  if (!existing) {
    await deps.secrets.store(SECRET_API_KEY_NAME, inPlain);
    await deps.globalState.update(GLOBAL_ONBOARDED, true);
  }

  // Clear from settings — at user scope first, then workspace, so we
  // catch wherever the operator put it.
  try {
    await cfg.update("apiKey", undefined, vscode.ConfigurationTarget.Global);
  } catch { /* ignore */ }
  try {
    await cfg.update("apiKey", undefined, vscode.ConfigurationTarget.Workspace);
  } catch { /* ignore */ }

  vscode.window.showInformationMessage(
    "CodeSec moved your API key from settings.json to the OS keychain — " +
      "it's no longer stored in plaintext on disk.",
  );
  return true;
}

/** Has the user finished onboarding at least once? */
export function isOnboarded(deps: OnboardingDeps): boolean {
  return deps.globalState.get<boolean>(GLOBAL_ONBOARDED, false);
}
