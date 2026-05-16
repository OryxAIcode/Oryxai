import * as crypto from "crypto";
import * as vscode from "vscode";
import { CodeSecClient } from "./client";
import { StatusBarManager } from "./statusbar";
import { DiagnosticsManager } from "./diagnostics";
import { ScanResult, Finding } from "./types";
import * as claudeCode from "./claudeCode";
import * as onboarding from "./onboarding";

let client: CodeSecClient;
let statusBar: StatusBarManager;
let diagnostics: DiagnosticsManager;
let previousContent: Map<string, string> = new Map();
let onboardingDeps: onboarding.OnboardingDeps;
let onboardingLog: vscode.OutputChannel;

export function activate(context: vscode.ExtensionContext): void {
  const cfg = vscode.workspace.getConfiguration("codesec");
  if (!cfg.get<boolean>("enabled", true)) {
    return;
  }

  // Onboarding deps — SecretStorage backs the API key from Faz B1.
  // We instantiate this BEFORE the client so the client gets the
  // keychain value, not the settings.json value (Faz B2).
  onboardingLog = vscode.window.createOutputChannel("CodeSec — Onboarding");
  context.subscriptions.push(onboardingLog);
  onboardingDeps = {
    secrets: context.secrets,
    globalState: context.globalState,
    log: onboardingLog,
  };

  const backendAddr = cfg.get<string>(
    "backendAddress",
    "http://127.0.0.1:8080"
  );
  client = new CodeSecClient(backendAddr, 5000, "");

  // Async wiring: migrate any legacy settings.json key, then hydrate
  // the client from SecretStorage. We don't await — activate must stay
  // synchronous — but the first scan after this resolves will see the
  // hydrated key.
  void (async () => {
    try {
      await onboarding.legacyMigrate(onboardingDeps);
    } catch (err) {
      onboardingLog.appendLine(`legacyMigrate failed: ${(err as Error).message}`);
    }
    const key = await onboarding.readAPIKey(onboardingDeps);
    if (key) client.setApiKey(key);
  })();

  // Re-hydrate on SecretStorage change (onboarding finishes in another
  // window, key rotated, key forgotten).
  context.subscriptions.push(
    context.secrets.onDidChange(async (e) => {
      if (e.key !== "codesec.apiKey") return;
      const next = await onboarding.readAPIKey(onboardingDeps);
      client.setApiKey(next);
    }),
  );

  // Pick up live changes to backendAddress without requiring a reload.
  // The settings.json apiKey field is deprecated (Faz B2 migrated it
  // to SecretStorage); we no longer react to its changes.
  context.subscriptions.push(
    vscode.workspace.onDidChangeConfiguration((e) => {
      if (e.affectsConfiguration("codesec.apiKey")) {
        // If someone re-introduces a plaintext value, migrate again.
        void onboarding.legacyMigrate(onboardingDeps);
      }
    }),
  );
  statusBar = new StatusBarManager();
  diagnostics = new DiagnosticsManager();

  context.subscriptions.push(statusBar, diagnostics);

  context.subscriptions.push(
    vscode.commands.registerCommand("codesec.scanCurrentFile", scanCurrentFile)
  );
  context.subscriptions.push(
    vscode.commands.registerCommand("codesec.scanSelection", scanSelection)
  );
  context.subscriptions.push(
    vscode.commands.registerCommand("codesec.showDashboard", showDashboard)
  );
  context.subscriptions.push(
    vscode.commands.registerCommand("codesec.toggleEnabled", toggleEnabled)
  );

  // Faz A3 — Claude Code integration commands.
  context.subscriptions.push(
    vscode.commands.registerCommand("codesec.enableClaudeCode", enableClaudeCode),
  );
  context.subscriptions.push(
    vscode.commands.registerCommand("codesec.disableClaudeCode", disableClaudeCode),
  );
  context.subscriptions.push(
    vscode.commands.registerCommand("codesec.diagnoseClaudeCode", diagnoseClaudeCode),
  );

  // Faz B1 — onboarding deep-link flow. The URI handler catches
  // vscode://codesec.codesec/cb?state=… that the SPA opens at the end
  // of the browser leg.
  context.subscriptions.push(
    vscode.commands.registerCommand("codesec.startOnboarding", startOnboarding),
  );
  context.subscriptions.push(
    vscode.commands.registerCommand("codesec.signOut", signOutOnboarding),
  );
  context.subscriptions.push(
    vscode.window.registerUriHandler({
      handleUri: (uri) => {
        if (uri.path === "/cb" || uri.path === "/callback") {
          void onboarding.handleCallback(uri, onboardingDeps, {
            controlURL: readControlURL(),
          });
        }
      },
    }),
  );

  // First-launch nudge — surface the onboarding command if the user
  // has neither finished onboarding nor pasted a settings key.
  if (!onboarding.isOnboarded(onboardingDeps)) {
    setTimeout(() => {
      void promptInitialOnboarding();
    }, 1500);
  }

  if (cfg.get<boolean>("scanOnSave", true)) {
    context.subscriptions.push(
      vscode.workspace.onDidSaveTextDocument((doc) => {
        onDocumentEvent(doc, "save");
      })
    );
  }

  if (cfg.get<boolean>("scanOnPaste", true)) {
    context.subscriptions.push(
      vscode.workspace.onDidChangeTextDocument((e) => {
        if (isPasteEvent(e)) {
          onDocumentEvent(e.document, "paste");
        }
      })
    );
  }

  if (cfg.get<boolean>("scanOnCompletionAccept", true)) {
    context.subscriptions.push(
      vscode.workspace.onDidChangeTextDocument((e) => {
        if (isCompletionAccept(e)) {
          onDocumentEvent(e.document, "completion");
        }
      })
    );
  }

  // Snapshot content on focus for diff-based scanning
  context.subscriptions.push(
    vscode.window.onDidChangeActiveTextEditor((editor) => {
      if (editor) {
        previousContent.set(
          editor.document.uri.toString(),
          editor.document.getText()
        );
      }
    })
  );

  // Snapshot on open
  context.subscriptions.push(
    vscode.workspace.onDidOpenTextDocument((doc) => {
      previousContent.set(doc.uri.toString(), doc.getText());
    })
  );

  // Initial snapshot
  if (vscode.window.activeTextEditor) {
    const doc = vscode.window.activeTextEditor.document;
    previousContent.set(doc.uri.toString(), doc.getText());
  }

  client
    .healthCheck()
    .then((ok) => {
      statusBar.setConnected(ok);
      if (!ok) {
        vscode.window.showWarningMessage(
          "CodeSec: Backend not reachable at " + backendAddr
        );
      }
    })
    .catch(() => {
      statusBar.setConnected(false);
    });
}

export function deactivate(): void {
  previousContent.clear();
}

async function scanCurrentFile(): Promise<void> {
  const editor = vscode.window.activeTextEditor;
  if (!editor) {
    vscode.window.showInformationMessage("CodeSec: No active file");
    return;
  }
  await scanDocument(editor.document, "manual");
}

async function scanSelection(): Promise<void> {
  const editor = vscode.window.activeTextEditor;
  if (!editor || editor.selection.isEmpty) {
    vscode.window.showInformationMessage("CodeSec: No selection");
    return;
  }
  const text = editor.document.getText(editor.selection);
  const lang = languageIdToCodesec(editor.document.languageId);
  statusBar.setScanning();

  try {
    const meta = buildEgressTraceMetadata(
      editor.document.uri.fsPath,
      undefined,
      text
    );
    const result = await client.scanCompletion(
      text,
      lang,
      undefined,
      editor.document.uri.fsPath,
      meta
    );
    handleScanResult(result, editor.document);
  } catch (err) {
    statusBar.setError();
    vscode.window.showErrorMessage(`CodeSec scan failed: ${err}`);
  }
}

async function showDashboard(): Promise<void> {
  const panel = vscode.window.createWebviewPanel(
    "codesecDashboard",
    "CodeSec Security Dashboard",
    vscode.ViewColumn.Two,
    { enableScripts: false }
  );

  const allDiags = diagnostics.getAllFindings();
  panel.webview.html = buildDashboardHTML(allDiags);
}

function toggleEnabled(): void {
  const cfg = vscode.workspace.getConfiguration("codesec");
  const current = cfg.get<boolean>("enabled", true);
  cfg.update("enabled", !current, vscode.ConfigurationTarget.Workspace);
  vscode.window.showInformationMessage(
    `CodeSec: ${!current ? "Enabled" : "Disabled"}`
  );
  statusBar.setEnabled(!current);
}

async function onDocumentEvent(
  doc: vscode.TextDocument,
  trigger: string
): Promise<void> {
  if (doc.uri.scheme !== "file") return;
  if (doc.languageId === "plaintext" && trigger !== "manual") return;

  await scanDocument(doc, trigger);
}

async function scanDocument(
  doc: vscode.TextDocument,
  trigger: string
): Promise<void> {
  const content = doc.getText();
  const lang = languageIdToCodesec(doc.languageId);
  const oldContent = previousContent.get(doc.uri.toString());

  statusBar.setScanning();

  try {
    const meta = buildEgressTraceMetadata(doc.uri.fsPath, oldContent, content);
    const result = await client.scanCompletion(
      content,
      lang,
      oldContent,
      doc.uri.fsPath,
      meta
    );
    handleScanResult(result, doc);
    previousContent.set(doc.uri.toString(), content);
  } catch (err) {
    statusBar.setError();
  }
}

function handleScanResult(
  result: ScanResult,
  doc: vscode.TextDocument
): void {
  diagnostics.update(doc.uri, result.triggered_rules, doc);

  const critical = result.triggered_rules.filter(
    (r) => r.severity === "critical"
  );

  if (result.decision === "block") {
    statusBar.setBlocked(result.triggered_rules.length);
    if (critical.length > 0) {
      vscode.window
        .showErrorMessage(
          `CodeSec BLOCKED: ${critical[0].description}`,
          "Show Details"
        )
        .then((action) => {
          if (action === "Show Details") {
            vscode.commands.executeCommand("codesec.showDashboard");
          }
        });
    }
  } else if (result.decision === "sanitize" || result.triggered_rules.length > 0) {
    statusBar.setWarning(result.triggered_rules.length);
  } else {
    statusBar.setClean();
  }
}

function isPasteEvent(e: vscode.TextDocumentChangeEvent): boolean {
  if (e.contentChanges.length !== 1) return false;
  const change = e.contentChanges[0];
  const lines = change.text.split("\n").length;
  return lines > 2 || change.text.length > 100;
}

function isCompletionAccept(e: vscode.TextDocumentChangeEvent): boolean {
  if (e.contentChanges.length !== 1) return false;
  const change = e.contentChanges[0];
  return (
    change.text.length > 20 &&
    change.rangeLength === 0 &&
    change.text.includes("\n")
  );
}

function sha256Hex(s: string): string {
  return crypto.createHash("sha256").update(s, "utf8").digest("hex");
}

/**
 * Audit/trace fields forwarded to the engine as EgressRequest.metadata.
 * Enables correlation_id, prompt_hash / diff_hash on audit log records when the backend uses audit.Logger.
 */
function buildEgressTraceMetadata(
  filePath: string | undefined,
  oldContent: string | undefined,
  newContent: string
): Record<string, string> {
  const meta: Record<string, string> = {
    correlation_id: crypto.randomUUID(),
    prompt_hash: sha256Hex(newContent),
  };
  const roots = vscode.workspace.workspaceFolders
    ?.map((f) => f.uri.fsPath)
    .join(",");
  if (roots) {
    meta.workspace_roots = roots;
  }
  if (filePath) {
    meta.target_path = filePath;
  }
  if (oldContent !== undefined) {
    meta.diff_hash = sha256Hex(`${oldContent}\n---\n${newContent}`);
  }
  return meta;
}

function languageIdToCodesec(langId: string): string {
  const map: Record<string, string> = {
    python: "python",
    javascript: "javascript",
    javascriptreact: "javascript",
    typescript: "typescript",
    typescriptreact: "typescript",
    go: "go",
    java: "java",
    ruby: "ruby",
    rust: "rust",
    php: "php",
    shellscript: "shell",
    bash: "shell",
  };
  return map[langId] || langId;
}

function buildDashboardHTML(
  findings: Map<string, Finding[]>
): string {
  let rows = "";
  let total = 0;

  findings.forEach((items, uri) => {
    for (const f of items) {
      total++;
      const severityColor =
        f.severity === "critical"
          ? "#e74c3c"
          : f.severity === "high"
          ? "#e67e22"
          : f.severity === "medium"
          ? "#f1c40f"
          : "#95a5a6";

      rows += `<tr>
        <td><span style="color:${severityColor};font-weight:bold">${f.severity.toUpperCase()}</span></td>
        <td>${escapeHtml(f.rule_name)}</td>
        <td>${escapeHtml(f.category)}</td>
        <td>${escapeHtml(f.description)}</td>
        <td>${escapeHtml(uri.split("/").pop() || uri)}</td>
      </tr>`;
    }
  });

  return `<!DOCTYPE html>
<html>
<head>
  <style>
    body { font-family: var(--vscode-font-family); background: var(--vscode-editor-background); color: var(--vscode-editor-foreground); padding: 20px; }
    h1 { border-bottom: 2px solid var(--vscode-panel-border); padding-bottom: 10px; }
    table { width: 100%; border-collapse: collapse; margin-top: 16px; }
    th { text-align: left; padding: 8px; background: var(--vscode-editor-inactiveSelectionBackground); }
    td { padding: 8px; border-bottom: 1px solid var(--vscode-panel-border); }
    .stat { font-size: 1.2em; margin: 8px 0; }
  </style>
</head>
<body>
  <h1>CodeSec Security Dashboard</h1>
  <p class="stat">Total findings: <strong>${total}</strong></p>
  <table>
    <tr><th>Severity</th><th>Rule</th><th>Category</th><th>Description</th><th>File</th></tr>
    ${rows || "<tr><td colspan='5' style='text-align:center'>No findings — all clear!</td></tr>"}
  </table>
</body>
</html>`;
}

function escapeHtml(s: string): string {
  return s
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
}

// ─── Onboarding commands (Faz B1) ──────────────────────────────────────

function readControlURL(): string {
  return vscode.workspace
    .getConfiguration("codesec")
    .get<string>("controlURL", "http://localhost:5173");
}

async function startOnboarding(): Promise<void> {
  try {
    await onboarding.start(onboardingDeps, { controlURL: readControlURL() });
  } catch (err) {
    vscode.window.showErrorMessage(
      `Couldn't start onboarding: ${(err as Error).message}`,
    );
  }
}

async function signOutOnboarding(): Promise<void> {
  const choice = await vscode.window.showWarningMessage(
    "Forget the CodeSec API key stored in your OS keychain? Scans will fail until you onboard again.",
    { modal: true },
    "Forget key",
  );
  if (choice !== "Forget key") return;
  await onboarding.forgetAPIKey(onboardingDeps);
  client.setApiKey("");
  vscode.window.showInformationMessage(
    "CodeSec key removed. Run 'CodeSec: Start Onboarding' to reconnect.",
  );
}

async function promptInitialOnboarding(): Promise<void> {
  // Only nudge if no key landed during legacyMigrate (which already
  // showed its own message).
  const key = await onboarding.readAPIKey(onboardingDeps);
  if (key) return;
  const action = await vscode.window.showInformationMessage(
    "Welcome to CodeSec — connect your editor to your workspace to start scanning.",
    "Start onboarding",
    "Later",
  );
  if (action === "Start onboarding") {
    await startOnboarding();
  }
}

// ─── Claude Code integration commands (Faz A3) ─────────────────────────

interface ClaudeCodeSettings {
  apiKey: string;
  proxyURL: string;
  mcpURL: string;
  controlURL: string;
}

function readClaudeCodeSettings(): ClaudeCodeSettings {
  const cfg = vscode.workspace.getConfiguration("codesec");
  return {
    apiKey: cfg.get<string>("apiKey", "").trim(),
    proxyURL: cfg.get<string>("proxyURL", "http://localhost:8091").trim(),
    mcpURL: cfg.get<string>("mcpURL", "http://localhost:8090/sse").trim(),
    controlURL: cfg.get<string>("controlURL", "http://localhost:5173").trim(),
  };
}

const claudeLogger: claudeCode.Logger = {
  info: (m) => console.log("[codesec.claudeCode]", m),
  warn: (m) => console.warn("[codesec.claudeCode]", m),
  error: (m) => console.error("[codesec.claudeCode]", m),
};

async function enableClaudeCode(): Promise<void> {
  const s = readClaudeCodeSettings();

  if (!s.apiKey) {
    const choice = await vscode.window.showWarningMessage(
      "CodeSec API key not set. Mint one in the web console first.",
      "Open settings",
      "Open web console",
    );
    if (choice === "Open settings") {
      vscode.commands.executeCommand(
        "workbench.action.openSettings",
        "codesec.apiKey",
      );
    } else if (choice === "Open web console") {
      vscode.env.openExternal(vscode.Uri.parse(s.controlURL));
    }
    return;
  }

  // Preflight: confirm there's an upstream Anthropic key on file for
  // this org. Plaintext keys live in the web console (§10.1), not in
  // the extension — so if the org hasn't added one yet, we bounce the
  // user there instead of writing a Claude Code config that 503s on
  // every request.
  const upstreamOk = await checkUpstreamConfigured(s);
  if (!upstreamOk) {
    return; // checkUpstreamConfigured already showed the modal
  }

  try {
    const res = await claudeCode.enable(
      { apiKey: s.apiKey, proxyURL: s.proxyURL, mcpURL: s.mcpURL },
      claudeCode.defaultPaths(),
      claudeLogger,
    );
    statusBar.setEnabled(true);
    vscode.window.showInformationMessage(
      `CodeSec Claude Code protection ${res.firstEnable ? "enabled" : "refreshed"}. ${res.hint}`,
      "Open settings.json",
    ).then((action) => {
      if (action === "Open settings.json") {
        vscode.workspace.openTextDocument(res.path).then((doc) =>
          vscode.window.showTextDocument(doc),
        );
      }
    });
  } catch (err) {
    vscode.window.showErrorMessage(
      `Could not enable Claude Code protection: ${(err as Error).message}`,
    );
  }
}

async function disableClaudeCode(): Promise<void> {
  try {
    const res = await claudeCode.disable(claudeCode.defaultPaths(), claudeLogger);
    vscode.window.showInformationMessage(res.hint);
  } catch (err) {
    vscode.window.showErrorMessage(
      `Could not disable Claude Code protection: ${(err as Error).message}`,
    );
  }
}

async function diagnoseClaudeCode(): Promise<void> {
  const s = readClaudeCodeSettings();
  const out = vscode.window.createOutputChannel("CodeSec — Claude Code");
  out.clear();
  out.show(true);

  out.appendLine("Probing Claude Code integration…");
  out.appendLine("");

  const st = await claudeCode.status(claudeCode.defaultPaths());
  out.appendLine(`settings.json:     ${st.path}`);
  out.appendLine(`  exists:          ${st.exists}`);
  out.appendLine(`  managed by us:   ${st.managed}`);
  out.appendLine(`  ANTHROPIC_BASE:  ${st.anthropicBaseURL ?? "(unset)"}`);
  out.appendLine(`  MCP codesec:     ${st.mcpHasCodesec ? "registered" : "missing"}`);
  out.appendLine("");

  out.appendLine("Configured endpoints:");
  out.appendLine(`  proxyURL:    ${s.proxyURL}`);
  out.appendLine(`  mcpURL:      ${s.mcpURL}`);
  out.appendLine(`  controlURL:  ${s.controlURL}`);
  // Print only presence + the public "csk_live_" / "csk_test_" tier
  // prefix — never any of the secret body. The diagnose output is
  // often pasted into support chats or screen-shared.
  const apiKeyTier = !s.apiKey
    ? "NO"
    : s.apiKey.startsWith("csk_live_") ? "yes (live)"
    : s.apiKey.startsWith("csk_test_") ? "yes (test)"
    : "yes";
  out.appendLine(`  apiKey set:  ${apiKeyTier}`);
  out.appendLine("");

  // Live checks. fetch is available in Node 18+; activation engine is
  // node:18 (esbuild target).
  await probe(out, "proxy /healthz", s.proxyURL.replace(/\/+$/, "") + "/healthz");
  await probe(out, "mcp /healthz", new URL("/healthz", s.mcpURL).toString());
  await probe(out, "control /api/v1/health",
    s.controlURL.replace(/\/+$/, "") + "/api/v1/health");
  out.appendLine("");
  out.appendLine("Done.");
}

async function probe(out: vscode.OutputChannel, name: string, url: string): Promise<void> {
  try {
    const ctrl = new AbortController();
    const to = setTimeout(() => ctrl.abort(), 3000);
    const r = await fetch(url, { signal: ctrl.signal });
    clearTimeout(to);
    out.appendLine(`  ${name.padEnd(28)} ${r.status} ${r.statusText}  (${url})`);
  } catch (err) {
    out.appendLine(`  ${name.padEnd(28)} unreachable: ${(err as Error).message}  (${url})`);
  }
}

async function checkUpstreamConfigured(s: ClaudeCodeSettings): Promise<boolean> {
  // We can't reach /upstream-keys with just a csk_… because that
  // endpoint is session+CSRF gated (ENTERPRISE_IDE_INTEGRATION §10.1).
  // Instead we send a probe request through the proxy itself with a
  // tiny body — if the proxy answers 503 UPSTREAM_KEY_MISSING we
  // bounce the user to the web console; any other response means the
  // pipeline is healthy enough to keep going.
  const url = s.proxyURL.replace(/\/+$/, "") + "/v1/messages";
  try {
    const ctrl = new AbortController();
    const to = setTimeout(() => ctrl.abort(), 3000);
    const r = await fetch(url, {
      method: "POST",
      signal: ctrl.signal,
      headers: {
        "Content-Type": "application/json",
        Authorization: `Bearer ${s.apiKey}`,
        "anthropic-version": "2023-06-01",
      },
      body: JSON.stringify({
        model: "claude-3-5-sonnet-20241022",
        max_tokens: 1,
        messages: [{ role: "user", content: "ping" }],
      }),
    });
    clearTimeout(to);
    if (r.status === 503) {
      const text = await r.text().catch(() => "");
      if (text.includes("upstream_key_missing") || text.includes("UPSTREAM_KEY_MISSING")) {
        const action = await vscode.window.showWarningMessage(
          "Your workspace has no Anthropic API key on file yet. CodeSec proxy can't forward requests until you add one in the web console.",
          { modal: true, detail: "Plaintext upstream keys live only in the web console — never in this extension." },
          "Open web console",
        );
        if (action === "Open web console") {
          vscode.env.openExternal(vscode.Uri.parse(s.controlURL + "/o/settings/upstream-keys"));
        }
        return false;
      }
    }
    if (r.status === 401) {
      vscode.window.showErrorMessage(
        "CodeSec proxy rejected the API key. Check codesec.apiKey is the csk_live_… you minted in the web console.",
      );
      return false;
    }
    // 2xx / 4xx with a real Anthropic-shaped error both prove the
    // pipeline reaches Anthropic — that's enough preflight.
    return true;
  } catch (err) {
    vscode.window.showErrorMessage(
      `Could not reach CodeSec proxy at ${s.proxyURL}: ${(err as Error).message}. ` +
        "Is docker compose up?",
    );
    return false;
  }
}
