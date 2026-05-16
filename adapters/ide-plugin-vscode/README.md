# CodeSec — VS Code Extension

[![Tests](https://img.shields.io/badge/tests-27%20passing-brightgreen)](#tests)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue)](LICENSE)

> The security gateway for AI coding assistants. **Drop-in protection** for
> Cursor, Claude Code, Windsurf, GitHub Copilot, Continue.dev, Cody, and
> any tool that emits LLM-generated code.

---

## What it does

| | |
|---|---|
| 🛡️  **Real-time scanning** | every save / paste / accepted completion is checked against the CodeSec policy engine — secrets, backdoors, hallucinated dependencies, prompt injection. |
| 🔐 **Claude Code integration** | one command wires `~/.claude/settings.json` so every `claude` invocation routes through your org's CodeSec proxy + MCP server. |
| 📊 **Live activity feed** | every metered request shows up in the dashboard at `localhost:5173/o/<slug>/audit`. |
| 🗝️ **OS keychain** | API key stored in macOS Keychain / Windows Credential Manager / Linux Secret Service — **never** in `settings.json`. |

---

## Install

VS Code Marketplace (coming soon) or **install from .vsix**:

```bash
git clone https://github.com/codesec-io/codesec
cd codesec/adapters/ide-plugin-vscode
npm install && npm run build
npx vsce package
# → codesec-0.1.0.vsix
code --install-extension codesec-0.1.0.vsix
```

Self-host the backend first:

```bash
cd codesec && docker compose up --build
# → http://localhost:5173  (web console)
# → http://localhost:8080  (engine)
# → http://localhost:8091  (LLM proxy)
# → http://localhost:8090  (MCP SSE server)
```

---

## First-launch onboarding

When the extension activates the first time:

1. Notification appears: **"Connect your editor to your workspace"**
2. Click **Start onboarding** (or run `CodeSec: Start Onboarding`)
3. Browser opens at `http://localhost:5173/extension-signin?state=…`
4. Pick your workspace, give your editor an identity-friendly label
5. The web console mints a fresh API key, opens `vscode://codesec.codesec/cb`
6. Extension stores the key in your **OS keychain** — done.

The plaintext key is **never displayed** in the SPA and **never written** to
`settings.json`. The handoff is a single-use deep-link (`state` token expires
in 10 minutes and is consumed exactly once).

---

## Commands

Run from Command Palette (Cmd+Shift+P / Ctrl+Shift+P):

| Command | What it does |
|---|---|
| `CodeSec: Start Onboarding` | Deep-link sign-in. Mints + stores an API key in the keychain. |
| `CodeSec: Sign Out` | Forgets the keychain entry. |
| `CodeSec: Enable Claude Code Protection` | Atomic write to `~/.claude/settings.json`: sets `ANTHROPIC_BASE_URL`, `ANTHROPIC_AUTH_TOKEN`, registers the `codesec` MCP server. |
| `CodeSec: Disable Claude Code Protection` | Restores the pre-CodeSec backup. |
| `CodeSec: Diagnose Claude Code Integration` | Health-probes proxy / MCP / control plane and shows a report. |
| `CodeSec: Scan Current File` | Manual full-file scan. |
| `CodeSec: Scan Selection` | Scan selected text only. |
| `CodeSec: Show Security Dashboard` | Per-window findings dashboard. |
| `CodeSec: Toggle On/Off` | Enable / disable scanning. |

---

## Settings

| Setting | Default | Description |
|---|---|---|
| `codesec.enabled` | `true` | Master switch. |
| `codesec.backendAddress` | `http://127.0.0.1:8080` | CodeSec engine URL (or `unix://` socket path). |
| `codesec.proxyURL` | `http://localhost:8091` | Written to `ANTHROPIC_BASE_URL` when Claude Code protection is enabled. |
| `codesec.mcpURL` | `http://localhost:8090/sse` | MCP SSE endpoint registered in `~/.claude/settings.json`. |
| `codesec.controlURL` | `http://localhost:5173` | Web console — used for onboarding + deep-links. |
| `codesec.scanOnSave` | `true` | |
| `codesec.scanOnPaste` | `true` | |
| `codesec.scanOnCompletionAccept` | `true` | |
| `codesec.policyProfile` | `default` | Policy profile name. |
| `codesec.showInlineWarnings` | `true` | Inline decorations in the editor. |
| `codesec.apiKey` | `""` | **Deprecated** — migrated to OS keychain on next activation. |

---

## Privacy & telemetry

**No telemetry.** The extension reports to your CodeSec control plane only;
nothing leaves your network. The Marketplace listing carries no third-party
analytics.

---

## Architecture

```
VS Code Extension
  ├── extension.ts   — activation, command registration, URI handler
  ├── client.ts      — HTTP/Unix-socket scan client
  ├── statusbar.ts   — shield / warning / error indicator
  ├── diagnostics.ts — Problems panel integration
  ├── claudeCode.ts  — atomic ~/.claude/settings.json writer + backup
  ├── onboarding.ts  — deep-link sign-in handshake + SecretStorage
  └── types.ts       — shared types
```

---

## Development

```bash
npm install
npm run build       # one-shot build → dist/extension.js
npm run watch       # rebuild on change
npm run test        # 27 jest tests
npm run package     # → codesec-X.Y.Z.vsix via @vscode/vsce
```

Press **F5** in VS Code to launch an Extension Development Host with the
extension loaded.

---

## License

Apache-2.0 — see [LICENSE](LICENSE).
