# CodeSec — JetBrains Plugin

> The security gateway for AI coding assistants — JetBrains edition.
> Feature-parity with the VS Code extension (Faz B4 of
> `docs/ENTERPRISE_IDE_INTEGRATION.md`).

## Supported IDEs

IntelliJ IDEA (Community & Ultimate), PyCharm, GoLand, WebStorm, Rider,
CLion, PhpStorm, RubyMine. `sinceBuild = 241`, `untilBuild = 251.*`.

## What it does

| | |
|---|---|
| 🛡️ **Real-time scanning** | every save / paste runs through the CodeSec engine. |
| 🔐 **Claude Code integration** | one action rewrites `~/.claude/settings.json` so the Anthropic CLI routes through your CodeSec proxy + MCP server. Atomic write, automatic backup, refuses to clobber a non-vanilla `ANTHROPIC_BASE_URL`. |
| 🗝️ **PasswordSafe** | API key lives in IntelliJ's PasswordSafe (macOS Keychain / Windows Credential Manager / Linux Secret Service) — **not** in `codesec.xml` on disk. |
| 📊 **Activity tool window** | bottom tool window polls `/api/v1/orgs/{slug}/feed` every 5 seconds. |
| 🔄 **Browser-mediated sign-in** | 32-byte state, 10-minute single-use; copy-paste handoff (JetBrains URL handlers are too fragile for an enterprise rollout). |

## Quick start

1. Bring up the CodeSec stack (`docker compose up --build` at the repo root).
2. Build the plugin:
   ```bash
   cd adapters/ide-plugin-jetbrains
   ./gradlew buildPlugin
   ```
3. Install: IDE → Settings → Plugins → Install Plugin from Disk → pick
   `build/distributions/codesec-jetbrains-0.1.0.zip`.
4. Settings → Tools → CodeSec → confirm the URLs match your stack
   (Engine, Proxy, MCP, Control plane).
5. **Tools → CodeSec → Start Onboarding** — opens the web console at
   `/extension-signin?state=…&mode=copy`. Pick a workspace, copy the
   token back into the "Paste sign-in token" dialog. PasswordSafe gets
   the API key; you don't.

## Actions

Under **Tools → CodeSec**:

| Action | What it does |
|---|---|
| Start Onboarding | Mints a workspace-scoped API key via the web console; stores in OS keychain. |
| Sign Out | Forgets the keychain entry. |
| Enable Claude Code Protection | Atomic write to `~/.claude/settings.json`: sets `ANTHROPIC_BASE_URL` / `ANTHROPIC_AUTH_TOKEN` and registers the `codesec` MCP server. |
| Disable Claude Code Protection | Restores the pre-CodeSec backup. |
| Diagnose Claude Code Integration | Health-probes proxy / MCP / control plane and emits a balloon notification. |
| Scan Current File | Manual full-file scan. |
| Scan Selection | Scan selected code only. |
| Toggle CodeSec | Enable / disable scanning. |

## Tool window

**View → Tool Windows → CodeSec Activity** (or bottom toolbar).
Polls every 5 seconds. Pick your workspace slug once (button at the top
of the window); it's remembered per IDE project.

## Settings reference

Settings → Tools → CodeSec.

| Field | Default | Description |
|---|---|---|
| Engine URL | `http://127.0.0.1:8080` | CodeSec engine; scan/health endpoints. |
| Proxy URL | `http://localhost:8091` | Written into `ANTHROPIC_BASE_URL` when Claude Code protection is enabled. |
| MCP SSE URL | `http://localhost:8090/sse` | Registered as the `codesec` MCP server in `~/.claude/settings.json`. |
| Control plane URL | `http://localhost:5173` | Web console — deep links + onboarding. |
| API key | (status) | Read-only label. Stored in OS keychain via PasswordSafe. |
| Enabled / scan-on-save / scan-on-paste / inline | true | Scanning toggles. |
| Timeout (ms) | 5000 | Per-request backend timeout. |
| Policy profile | `default` | Profile name (sent with every scan). |

## Privacy

**No telemetry.** All telemetry goes to your CodeSec control plane only;
nothing leaves the network. The plugin emits no third-party analytics.

## Architecture

```
JetBrains IDE
  ├── CodeSecDocumentListener   on-save / on-paste hook
  ├── CodeSecInspection         inline Problem panel integration
  ├── CodeSecStatusBarFactory   status bar widget
  ├── CodeSecClient             HTTP client → engine
  ├── CodeSecSettings           persistent state (codesec.xml)
  ├── CodeSecCredentialStore    OS keychain via PasswordSafe
  ├── CodeSecConfigurable       Settings UI
  ├── ClaudeCodeConfig          atomic ~/.claude/settings.json writer
  ├── Onboarding                browser handoff + paste flow
  ├── ActivityToolWindow        feed poller + JBTable
  └── actions/                  8 commands (3 scan, 2 onboarding, 3 Claude Code)
```

## Development

```bash
./gradlew runIde         # spawns a sandbox IDE with the plugin loaded
./gradlew buildPlugin    # produces build/distributions/codesec-jetbrains-VERSION.zip
./gradlew publishPlugin  # uploads to the JetBrains Marketplace
```

Plugin signing uses `CERTIFICATE_CHAIN` / `PRIVATE_KEY` / `PRIVATE_KEY_PASSWORD`
env vars; publishing uses `PUBLISH_TOKEN`.

## License

Apache-2.0.
