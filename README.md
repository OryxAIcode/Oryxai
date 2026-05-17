# oryxai

Security gateway and token-savings layer for AI coding assistants.

`oryxai` is a single binary that connects AI coding tools on your machine
(Claude Code, Cursor, Cline, Continue, Cody, Codeium, Gemini CLI, OpenCode,
OpenClaw, Windsurf, Kilo Code, Antigravity, Codex, Copilot CLI) into
OryxAI's hosted platform. It does two things:

**Security** — Every prompt, completion, and tool call is scanned for
prompt-injection, backdoors, secrets, and policy violations before it reaches
the LLM or lands on disk.

**Token savings** — Commands are rewritten before they run (`npm install`
→ `npm install --no-progress --loglevel=error`) and noisy tool outputs are
filtered before they fill up the context window. Less noise, lower cost,
faster responses.

## Install

```sh
curl -fsSL https://oryxai.dev/install | sh
oryxai install
```

The installer auto-detects which AI tools are installed on your machine and
configures each one. It prompts for your OryxAI API key (get one at
[oryxai.dev](https://oryxai.dev) → API keys).

Add `--with-hook` to also enable tool-call-level hooks, which give you
enforcement-grade visibility **and** activate command rewriting for token
savings:

```sh
oryxai install --with-hook
```

## What it does

| Command | What happens |
|---|---|
| `oryxai install` | Detects installed agents, prompts for API key, writes per-tool config pointing at `https://proxy.oryxai.dev` / `https://mcp.oryxai.dev`. Proxy-side token filter active. |
| `oryxai install --with-hook` | Same, plus installs PreToolUse hooks. Enables command rewriting (pre-execution token savings) and tool-call-level audit visibility. |
| `oryxai uninstall` | Restores each tool's original config from the backup made on install. |
| `oryxai status` | Shows what's installed + drains any locally-buffered events to the dashboard. |
| `oryxai verify` | Diagnostic: tests connectivity to the OryxAI control plane. |

## Token savings

OryxAI reduces context token usage at two layers:

### Layer A — Pre-execution rewriting (requires `--with-hook`)

Before a command runs, the PreToolUse hook rewrites it to suppress
unnecessary output. Examples:

| Original command | Rewritten command |
|---|---|
| `npm install` | `npm install --no-progress --loglevel=error` |
| `pytest` | `pytest -q --tb=short` |
| `cargo build` | `cargo build --message-format=short` |
| `pip install <pkg>` | `pip install -q <pkg>` |
| `docker pull <img>` | `docker pull -q <img>` |
| `kubectl get pods` | `kubectl get pods --no-headers` |

The rewrite is transparent — the agent runs the quieter command, the
output is shorter, and the next prompt turn costs fewer tokens. The
rewrite never changes what a command does, only how verbose it is.
Supported: Claude Code, Cursor, Gemini CLI.

### Layer B — Post-execution output filtering (always active)

After a command runs, the tool result travels back to the LLM through
`https://proxy.oryxai.dev`. The proxy filters the output before it enters
the context window:

- **Truncation** — output longer than a configurable limit is cut.
- **Head/tail** — for build output, keep the first N lines (build setup)
  and last M lines (errors/summary) and drop the middle.
- **Smart failures** — keep every line containing `FAIL`, `ERROR`, `PANIC`,
  `ASSERT`, plus a few lines of context after each, plus the last 5 summary
  lines. Drop the rest.
- **Deduplication** — repeated identical lines (e.g. progress bars) are
  collapsed to one.

Layer B is active for all tool modes. Layer A additionally requires
`--with-hook`.

## Supported agents

| Agent | Security mode | Token savings |
|---|---|---|
| Claude Code | API intercept + optional hook | Layer A + B |
| Cursor | API intercept + optional hook | Layer A + B |
| Gemini CLI | API intercept + optional hook | Layer A + B |
| Cline / Roo Code | API intercept | Layer B |
| Continue.dev | API intercept | Layer B |
| Cody (Sourcegraph) | API intercept | Layer B |
| Codeium | API intercept | Layer B |
| OpenCode | Hook (TypeScript plugin) | Layer B |
| OpenClaw | Hook (TypeScript plugin) | Layer B |
| Windsurf | Rule file (advisory) | Layer B |
| Kilo Code | Rule file (advisory) | Layer B |
| Google Antigravity | Rule file (advisory) | Layer B |
| Codex (OpenAI) | Rule file (advisory) | Layer B |
| GitHub Copilot CLI | Rule file (advisory) | Layer B |

**Security modes:**

- **API intercept** — the agent's LLM requests transit OryxAI's hosted proxy.
  Real visibility at the API layer.
- **Hook** — the agent's PreToolUse / BeforeTool mechanism invokes
  `oryxai hook`. Enforcement-grade visibility at the tool-call layer.
- **Rule file (advisory)** — a project-scoped markdown file with security
  rules injected into the agent's context. Defense-in-depth only.

## Build from source

Requires Go 1.25+.

```sh
git clone https://github.com/OryxAIcode/Oryxai
cd Oryxai
make build       # → bin/oryxai
```

## How it integrates

`oryxai install` writes config so your AI tools route through OryxAI's hosted
endpoints. The policy engine, ML risk scorer, and tool firewall run server-side
at `https://proxy.oryxai.dev`.

Events flow back via
`POST https://oryxai.dev/api/v1/orgs/{slug}/feed/ingest` and appear live in
the dashboard at `https://oryxai.dev/o/{slug}/audit`.

For tool-call-level hook events, `oryxai hook --agent <name>` is invoked by
the agent's PreToolUse machinery. It:

1. Rewrites the command (Layer A token savings) if applicable.
2. Buffers the event locally in `~/.oryxai/buffer.jsonl` (atomic appends, no
   blocking HTTP).
3. Returns the allow/approve response to the agent immediately.

The buffer drains on the next `oryxai status` / `oryxai verify` /
`oryxai install` invocation.

## What stays in this repo, what doesn't

This repo is the public surface: the installer binary, the IDE extensions,
the GitHub Action template, and version-info plumbing.

The OryxAI scanning engine, policy engine, ML risk model, and multi-tenant
control plane live in a separate (private) repository. Customers running
self-hosted deployments use Docker images
(`ghcr.io/oryxai/codesec:tag`) — source access is governed by commercial
license.

## Security considerations

- **API key prompt.** `oryxai install` reads your API key via terminal
  echo-off (`golang.org/x/term`). Never pipe a plaintext key from shell
  history.
- **Local trust boundary.** Hook events buffer in `~/.oryxai/buffer.jsonl`
  mode 0600. Same trust boundary as your `.bash_history`.
- **Symlink defense.** Recipe writers refuse to follow symlinks at their
  target paths to prevent write-through attacks.
- **Command rewriting scope.** Rewriting appends output-suppression flags
  only. It never changes what a command does (no target, file, or argument
  changes). The full original command is still logged to the audit trail.
- **Project-directory recipes.** Windsurf, Kilo Code, Codex, Antigravity,
  and Copilot CLI drop advisory markdown into the current project directory.
  World-writable directories (e.g. `/tmp`) are rejected. Pass `--project-dir`
  to pick a path.
- **No hard blocking at the hook layer.** The hook observes, rewrites for
  noise reduction, and reports. Catastrophic patterns are blocked server-side
  by the policy engine, not locally.
- **install.sh.** The shell installer verifies SHA256 against the release's
  `SHA256SUMS` file by default. `ORYXAI_NO_VERIFY=1` skips verification —
  don't set this.

## License

[MIT](./LICENSE) — see the file for the full text.

## Links

- Dashboard: [https://oryxai.dev](https://oryxai.dev)
- Issues: [https://github.com/OryxAIcode/Oryxai/issues](https://github.com/OryxAIcode/Oryxai/issues)
- Releases: [https://github.com/OryxAIcode/Oryxai/releases](https://github.com/OryxAIcode/Oryxai/releases)