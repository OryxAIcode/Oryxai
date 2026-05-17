# Changelog

All notable changes to the CodeSec VS Code extension are recorded here. The
format roughly follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [0.1.0] — 2026-05-17

First public preview. Pairs with control plane + engine ≥ `dev` (`docker
compose up --build` on the [codesec repo](https://github.com/codesec-io/codesec)).

### Added

- **Onboarding** — deep-link sign-in (`CodeSec: Start Onboarding`). Opens
  the browser, mints a workspace-scoped API key in a single-use 10-minute
  handshake (`POST /extension-tokens/exchange`), stores plaintext in the
  OS keychain via VS Code SecretStorage. Plaintext never reaches
  `settings.json` or any log line.
- **Claude Code integration** — three commands wire `~/.claude/settings.json`:
  - `CodeSec: Enable Claude Code Protection` — atomic write with backup.
    Sets `ANTHROPIC_BASE_URL` to the CodeSec proxy and registers the
    `codesec` MCP server. Refuses to clobber a non-vanilla
    `ANTHROPIC_BASE_URL` so corporate proxies aren't silently overridden.
  - `CodeSec: Disable Claude Code Protection` — restores backup, removes
    only the keys we wrote (sentinel-guarded).
  - `CodeSec: Diagnose Claude Code Integration` — health-probes proxy /
    MCP / control plane endpoints.
- **Activity feed** — every metered request is visible at
  `http://localhost:5173/o/<slug>/audit` (5-second polling, kind filter,
  configurable time window).
- **SecretStorage migration** — any plaintext `codesec.apiKey` left in
  `settings.json` is moved to the OS keychain on first activation and
  cleared from disk.
- **New settings**: `codesec.proxyURL`, `codesec.mcpURL`,
  `codesec.controlURL` — wired into the Claude Code config writer.

### Changed

- `codesec.apiKey` setting is **deprecated**. The migration runs once at
  activation; further plaintext values trigger another migration pass.
- Command registration adopts the `category: "CodeSec"` field so they
  group cleanly in the Command Palette.

### Security

- API key plaintext is **single-use** in transit (Exchange row sets
  `consumed_at` atomically) and **never** persisted to disk by the
  extension after the keychain write.
- `~/.claude/settings.json` writes are atomic (`tmp + rename`, mode
  `0o600`) — a half-written file can never brick Claude Code.
- Onboarding state token is 32 bytes of `crypto.randomBytes`,
  Postgres-keyed, TTL 10 minutes, single-use.
- No telemetry, no third-party analytics.

### Known limitations

- macOS Keychain access is the default; on Linux containers without
  libsecret the extension falls back to VS Code's encrypted store.
- The activity feed is poll-based today; the SSE upgrade is tracked in
  the next milestone.
- JetBrains parity is not in this release — see the parallel
  `ide-plugin-jetbrains` package.

[0.1.0]: https://github.com/codesec-io/codesec/releases/tag/vscode-extension-v0.1.0
