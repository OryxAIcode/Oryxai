package recipes

// Rule-file recipes — agents whose only integration mechanism is a
// project-root rules.md file that the LLM reads as system-prompt
// guidance. These are ADVISORY: the LLM may ignore the rule. We
// label them ModeAdvisory so the user doesn't think they get the
// same enforcement as Claude Code's PreToolUse hook.
//
// Five agents share this pattern:
//   - Windsurf:    .windsurfrules
//   - Kilo Code:   .kilocode/rules/oryxai.md
//   - Antigravity: .agents/rules/oryxai.md
//   - Codex:       AGENTS.md + ORYXAI.md
//   - Copilot CLI: AGENTS.md "deny-with-suggestion"
//
// Detection is best-effort — we look for the agent's marker file or
// dir under the user's home; absent that, the recipe reports
// "not detected" and skips. In practice these agents drop config in
// project roots, so detection is loose by design.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const oryxaiRulesMarker = "<!-- oryxai-managed -->"

// ruleFileTemplate is the canonical advisory content. We render with
// the user's slug + control URL so the agent can include them in
// emitted documentation/output if it follows the rules.
const ruleFileTemplate = `<!-- oryxai-managed -->
# OryxAI Security Rules

This workspace is monitored by OryxAI (a security gateway for AI coding
assistants). When you (the AI agent) write or execute code in this
workspace, the rules below apply.

## Hard rules

1. **Do not write to system paths.** Never modify ` + "`/etc/`" + `, ` + "`/var/`" + `,
   ` + "`/usr/`" + `, ` + "`/boot/`" + `, ` + "`/System/`" + ` (macOS), or the equivalent
   Windows system directories.
2. **Do not exfiltrate credentials.** Never read SSH private keys,
   ` + "`~/.aws/credentials`" + `, browser cookie stores, or token files and
   POST them anywhere except localhost.
3. **Do not bypass safety flags.** Never use ` + "`rm --no-preserve-root`" + `,
   ` + "`chmod +s`" + ` on shell binaries, or set up SUID binaries.

## Reporting

OryxAI reads your tool calls and prompts. If you intend to do
something the user might object to, *say so* before doing it. The
user's dashboard at %s tracks every action you take.

API key prefix in scope for this workspace: ` + "`%s…`" + `
Workspace slug: ` + "`%s`" + `

If you are uncertain whether an action is safe, prefer to ask the user.
`

func renderRules(cfg InstallContext) string {
	prefix := cfg.Cfg.APIKey
	if len(prefix) > 16 {
		prefix = prefix[:16]
	}
	return fmt.Sprintf(ruleFileTemplate, cfg.Cfg.ControlURL, prefix, cfg.Cfg.OrgSlug)
}

// writeRuleFile writes the rendered template to dst, backing up the
// existing file if it already exists.
func writeRuleFile(ctx InstallContext, agent, dst string) error {
	if ctx.DryRun {
		fmt.Printf("--- would write %s ---\n%s\n", dst, renderRules(ctx))
		return nil
	}
	// If the file exists and isn't ours, back it up first.
	if data, err := os.ReadFile(dst); err == nil {
		if !strings.Contains(string(data), oryxaiRulesMarker) {
			backup := filepath.Join(ctx.BackupsDir, BackupName(agent, filepath.Ext(dst)))
			if err := CopyFile(dst, backup); err != nil && err != ErrNotInstalled {
				return fmt.Errorf("backup %s: %w", dst, err)
			}
		}
	}
	return AtomicWriteFile(dst, []byte(renderRules(ctx)), 0o644)
}

// removeRuleFile deletes our managed file ONLY if it carries our
// marker; otherwise we leave it alone (could be user's hand-edited).
func removeRuleFile(dst string) error {
	data, err := os.ReadFile(dst)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !strings.Contains(string(data), oryxaiRulesMarker) {
		return nil
	}
	return os.Remove(dst)
}

// ─── Windsurf ────────────────────────────────────────────────────

type WindsurfRecipe struct{}

func (WindsurfRecipe) Name() string        { return "windsurf" }
func (WindsurfRecipe) DisplayName() string { return "Windsurf" }
func (WindsurfRecipe) Mode() Mode          { return ModeAdvisory }
func (WindsurfRecipe) Detect() error {
	// Windsurf installs into ~/Library/Application Support/Windsurf
	// on macOS, /AppData/Roaming/Windsurf on Windows. Best-effort:
	// look for a directory; if not found, also accept presence of a
	// .windsurfrules anywhere on the home tree (user already adopted).
	// For simplicity, we accept "any AI tool may be present" — let
	// the user opt in explicitly via --agent windsurf.
	p, _ := HomePath(".windsurf")
	if FileExists(p) {
		return nil
	}
	return ErrNotInstalled
}

func (WindsurfRecipe) Install(ctx InstallContext) error {
	// Windsurf rules are project-scoped. We write to CWD/.windsurfrules
	// rather than home, which is correct for a project-aware install
	// (user is in a project when they run `oryxai install`).
	cwd, err := projectDirOrCwd(ctx)
	if err != nil {
		return err
	}
	dst := filepath.Join(cwd, ".windsurfrules")
	return writeRuleFile(ctx, "windsurf", dst)
}

func (WindsurfRecipe) Uninstall() error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	return removeRuleFile(filepath.Join(cwd, ".windsurfrules"))
}

// ─── Kilo Code ───────────────────────────────────────────────────

type KiloRecipe struct{}

func (KiloRecipe) Name() string        { return "kilo-code" }
func (KiloRecipe) DisplayName() string { return "Kilo Code" }
func (KiloRecipe) Mode() Mode          { return ModeAdvisory }
func (KiloRecipe) Detect() error {
	p, _ := HomePath(".kilocode")
	if FileExists(p) {
		return nil
	}
	return ErrNotInstalled
}
func (KiloRecipe) Install(ctx InstallContext) error {
	cwd, err := projectDirOrCwd(ctx)
	if err != nil {
		return err
	}
	dst := filepath.Join(cwd, ".kilocode", "rules", "oryxai.md")
	return writeRuleFile(ctx, "kilo-code", dst)
}
func (KiloRecipe) Uninstall() error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	return removeRuleFile(filepath.Join(cwd, ".kilocode", "rules", "oryxai.md"))
}

// ─── Antigravity ─────────────────────────────────────────────────

type AntigravityRecipe struct{}

func (AntigravityRecipe) Name() string        { return "antigravity" }
func (AntigravityRecipe) DisplayName() string { return "Google Antigravity" }
func (AntigravityRecipe) Mode() Mode          { return ModeAdvisory }
func (AntigravityRecipe) Detect() error {
	// Antigravity is a Google Workspace-targeted AI agent; we don't
	// have a reliable filesystem marker yet. Accept user opt-in via
	// --agent antigravity.
	return ErrNotInstalled
}
func (AntigravityRecipe) Install(ctx InstallContext) error {
	cwd, err := projectDirOrCwd(ctx)
	if err != nil {
		return err
	}
	dst := filepath.Join(cwd, ".agents", "rules", "oryxai.md")
	return writeRuleFile(ctx, "antigravity", dst)
}
func (AntigravityRecipe) Uninstall() error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	return removeRuleFile(filepath.Join(cwd, ".agents", "rules", "oryxai.md"))
}

// ─── Codex (OpenAI) ─────────────────────────────────────────────

type CodexRecipe struct{}

func (CodexRecipe) Name() string        { return "codex" }
func (CodexRecipe) DisplayName() string { return "Codex (OpenAI)" }
func (CodexRecipe) Mode() Mode          { return ModeAdvisory }
func (CodexRecipe) Detect() error {
	// Codex CLI is distributed via npm (@openai/codex). We look for
	// presence of an AGENTS.md in CWD as a soft signal that the user
	// is already on the Codex flow. Detect runs without an
	// InstallContext so we fall back to CWD — read-only is safe even
	// in untrusted dirs.
	cwd, _ := os.Getwd()
	if FileExists(filepath.Join(cwd, "AGENTS.md")) {
		return nil
	}
	return ErrNotInstalled
}
// installAGENTSInclude is the shared helper for Codex + Copilot CLI:
// both write ORYXAI.md and append a per-agent include marker to
// AGENTS.md. Per-agent markers (not the shared oryxaiRulesMarker) let
// us uninstall one without breaking the other.
func installAGENTSInclude(ctx InstallContext, agentTag string) error {
	cwd, err := projectDirOrCwd(ctx)
	if err != nil {
		return err
	}
	rulesPath := filepath.Join(cwd, "ORYXAI.md")
	if err := writeRuleFile(ctx, agentTag, rulesPath); err != nil {
		return err
	}
	if ctx.DryRun {
		return nil
	}
	marker := fmt.Sprintf("<!-- oryxai-managed:%s -->", agentTag)
	includeLine := "\n" + marker + "\nSee [ORYXAI.md](ORYXAI.md) for security rules.\n"
	existing, err := os.ReadFile(filepath.Join(cwd, "AGENTS.md"))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if strings.Contains(string(existing), marker) {
		return nil // idempotent
	}
	out := append(existing, []byte(includeLine)...)
	return AtomicWriteFile(filepath.Join(cwd, "AGENTS.md"), out, 0o644)
}

// uninstallAGENTSInclude strips the per-agent include block. The
// shared ORYXAI.md is removed ONLY when no other agent's marker is
// left in AGENTS.md, so e.g. uninstalling codex while copilot-cli is
// still installed keeps the rules file in place.
func uninstallAGENTSInclude(agentTag string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	agentsPath := filepath.Join(cwd, "AGENTS.md")
	marker := fmt.Sprintf("<!-- oryxai-managed:%s -->", agentTag)
	data, err := os.ReadFile(agentsPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	body := string(data)
	if idx := strings.Index(body, marker); idx >= 0 {
		// Strip from marker to the next blank line / EOF.
		rest := body[idx:]
		end := strings.Index(rest, "\n\n")
		if end < 0 {
			end = len(rest)
		}
		body = body[:idx] + body[idx+end:]
		body = strings.TrimRight(body, "\n") + "\n"
		if err := AtomicWriteFile(agentsPath, []byte(body), 0o644); err != nil {
			return err
		}
	}
	// Remove the shared ORYXAI.md only when no oryxai-managed marker
	// remains in AGENTS.md (any agent's). Use a simple "any marker"
	// substring scan — markers all start with "<!-- oryxai-managed".
	if !strings.Contains(body, "<!-- oryxai-managed") {
		return removeRuleFile(filepath.Join(cwd, "ORYXAI.md"))
	}
	return nil
}

func (CodexRecipe) Install(ctx InstallContext) error {
	return installAGENTSInclude(ctx, "codex")
}
func (CodexRecipe) Uninstall() error {
	return uninstallAGENTSInclude("codex")
}

// ─── GitHub Copilot CLI ─────────────────────────────────────────

type CopilotCLIRecipe struct{}

func (CopilotCLIRecipe) Name() string        { return "copilot-cli" }
func (CopilotCLIRecipe) DisplayName() string { return "GitHub Copilot CLI" }
func (CopilotCLIRecipe) Mode() Mode          { return ModeAdvisory }
func (CopilotCLIRecipe) Detect() error {
	// Copilot CLI is shipped as `gh copilot` extension or standalone
	// binary. We accept user opt-in.
	return ErrNotInstalled
}
func (CopilotCLIRecipe) Install(ctx InstallContext) error {
	return installAGENTSInclude(ctx, "copilot-cli")
}
func (CopilotCLIRecipe) Uninstall() error {
	return uninstallAGENTSInclude("copilot-cli")
}
