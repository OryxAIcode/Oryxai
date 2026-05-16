package recipes

// Claude Code recipe.
//
// Two pieces written into ~/.claude/settings.json:
//
//  1. env.ANTHROPIC_BASE_URL + env.ANTHROPIC_AUTH_TOKEN — every Claude
//     Code request now flows through https://proxy.oryxai.dev where
//     the existing codesec-proxy intercepts.
//
//  2. (Optional, --with-hook) hooks.PreToolUse — Claude Code spawns
//     oryxai hook --agent claude-code on every tool call.
//
// We preserve any user-set keys in settings.json by parsing into a
// generic map[string]any, merging our blocks, and rewriting. Backup
// is taken before each install run so uninstall is reliable.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const claudeCodeName = "claude-code"

type ClaudeCodeRecipe struct{}

func (ClaudeCodeRecipe) Name() string        { return claudeCodeName }
func (ClaudeCodeRecipe) DisplayName() string { return "Claude Code" }
func (ClaudeCodeRecipe) Mode() Mode          { return ModeAPIIntercept } // upgrades to ModeHook when --with-hook

func (ClaudeCodeRecipe) Detect() error {
	dir, err := HomePath(".claude")
	if err != nil {
		return err
	}
	if FileExists(dir) {
		return nil
	}
	return ErrNotInstalled
}

func (ClaudeCodeRecipe) Install(ctx InstallContext) error {
	path, err := HomePath(".claude", "settings.json")
	if err != nil {
		return err
	}

	settings := map[string]any{}
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &settings); err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		// Backup existing file before mutation. If the file doesn't
		// parse we wouldn't have made it this far — safe to back up.
		backup := filepath.Join(ctx.BackupsDir, BackupName(claudeCodeName, ".json"))
		if !ctx.DryRun {
			if err := CopyFile(path, backup); err != nil && err != ErrNotInstalled {
				return fmt.Errorf("backup %s: %w", path, err)
			}
		}
	}

	// Merge env block.
	env, _ := settings["env"].(map[string]any)
	if env == nil {
		env = map[string]any{}
	}
	env["ANTHROPIC_BASE_URL"] = ctx.Cfg.ProxyURL
	env["ANTHROPIC_AUTH_TOKEN"] = ctx.Cfg.APIKey
	settings["env"] = env

	// Hooks block: either add ours (--with-hook) OR remove any
	// previously-installed oryxai hook (re-running install without
	// --with-hook should turn the feature off).
	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	preToolUse, _ := hooks["PreToolUse"].([]any)
	// Strip any prior oryxai-managed entries regardless of WithHook.
	filtered := make([]any, 0, len(preToolUse))
	for _, e := range preToolUse {
		if m, ok := e.(map[string]any); ok {
			if _, ours := m["__oryxai_managed"]; ours {
				continue
			}
		}
		filtered = append(filtered, e)
	}
	if ctx.WithHook {
		// Add (or re-add) our entry with the latest hook path.
		ourEntry := map[string]any{
			"matcher": "*",
			"hooks": []any{
				map[string]any{
					"type":    "command",
					"command": fmt.Sprintf("%s hook --agent claude-code", ctx.HookPath),
				},
			},
			"__oryxai_managed": true,
		}
		filtered = append(filtered, ourEntry)
	}
	if len(filtered) == 0 {
		delete(hooks, "PreToolUse")
	} else {
		hooks["PreToolUse"] = filtered
	}
	if len(hooks) == 0 {
		delete(settings, "hooks")
	} else {
		settings["hooks"] = hooks
	}

	if ctx.DryRun {
		return previewJSON(path, settings)
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal settings: %w", err)
	}
	return AtomicWriteFile(path, data, 0o600)
}

func (ClaudeCodeRecipe) Uninstall() error {
	path, err := HomePath(".claude", "settings.json")
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}

	// Strip ANTHROPIC_BASE_URL + ANTHROPIC_AUTH_TOKEN ONLY if they
	// match an OryxAI host — never blow away a user-set custom URL.
	if env, ok := settings["env"].(map[string]any); ok {
		if v, ok := env["ANTHROPIC_BASE_URL"].(string); ok && isOryxAIHost(v) {
			delete(env, "ANTHROPIC_BASE_URL")
		}
		if v, ok := env["ANTHROPIC_AUTH_TOKEN"].(string); ok && hasCSKPrefix(v) {
			delete(env, "ANTHROPIC_AUTH_TOKEN")
		}
		if len(env) == 0 {
			delete(settings, "env")
		} else {
			settings["env"] = env
		}
	}

	// Strip our hook entry (the one tagged __oryxai_managed).
	if hooks, ok := settings["hooks"].(map[string]any); ok {
		if pre, ok := hooks["PreToolUse"].([]any); ok {
			filtered := make([]any, 0, len(pre))
			for _, e := range pre {
				if m, ok := e.(map[string]any); ok {
					if _, ours := m["__oryxai_managed"]; ours {
						continue
					}
				}
				filtered = append(filtered, e)
			}
			if len(filtered) == 0 {
				delete(hooks, "PreToolUse")
			} else {
				hooks["PreToolUse"] = filtered
			}
		}
		if len(hooks) == 0 {
			delete(settings, "hooks")
		} else {
			settings["hooks"] = hooks
		}
	}

	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return AtomicWriteFile(path, out, 0o600)
}

func isOryxAIHost(u string) bool {
	for _, h := range []string{"oryxai.dev", "proxy.oryxai.dev"} {
		if u == "https://"+h || u == "http://"+h ||
			u == "https://"+h+":8091" || u == "http://"+h+":8091" {
			return true
		}
	}
	return false
}

func hasCSKPrefix(s string) bool {
	return len(s) >= 8 && (s[:4] == "csk_" || s[:8] == "csk_live")
}

func previewJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Printf("--- would write %s ---\n%s\n", path, string(data))
	return nil
}
