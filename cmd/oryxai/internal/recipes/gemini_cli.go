package recipes

// Gemini CLI recipe.
//
// Gemini CLI uses a config file at ~/.gemini/config.json with an
// `auth` block for the API endpoint. Some versions also expose
// `hooks.beforeTool` for tool-call interception — we attempt to set
// it when --with-hook is requested but warn that not all Gemini CLI
// versions honor it.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type GeminiCLIRecipe struct{}

func (GeminiCLIRecipe) Name() string        { return "gemini-cli" }
func (GeminiCLIRecipe) DisplayName() string { return "Gemini CLI" }
func (GeminiCLIRecipe) Mode() Mode          { return ModeAPIIntercept }

func (GeminiCLIRecipe) Detect() error {
	dir, err := HomePath(".gemini")
	if err != nil {
		return err
	}
	if FileExists(dir) {
		return nil
	}
	return ErrNotInstalled
}

func (GeminiCLIRecipe) Install(ctx InstallContext) error {
	path, err := HomePath(".gemini", "config.json")
	if err != nil {
		return err
	}
	cfg := map[string]any{}
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &cfg); err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		backup := filepath.Join(ctx.BackupsDir, BackupName("gemini-cli", ".json"))
		if !ctx.DryRun {
			if err := CopyFile(path, backup); err != nil && err != ErrNotInstalled {
				return fmt.Errorf("backup: %w", err)
			}
		}
	}
	// API endpoint override — Gemini CLI accepts an OpenAI-compatible
	// endpoint via this config key in recent versions.
	cfg["apiBaseUrl"] = ctx.Cfg.ProxyURL + "/v1"
	cfg["apiKey"] = ctx.Cfg.APIKey
	cfg["__oryxai_managed"] = true

	if ctx.WithHook {
		hooks, _ := cfg["hooks"].(map[string]any)
		if hooks == nil {
			hooks = map[string]any{}
		}
		hooks["beforeTool"] = fmt.Sprintf("%s hook --agent gemini-cli", ctx.HookPath)
		cfg["hooks"] = hooks
	}

	if ctx.DryRun {
		return previewJSON(path, cfg)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return AtomicWriteFile(path, data, 0o600)
}

func (GeminiCLIRecipe) Uninstall() error {
	path, err := HomePath(".gemini", "config.json")
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
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		return err
	}
	if _, ours := cfg["__oryxai_managed"]; ours {
		delete(cfg, "apiBaseUrl")
		delete(cfg, "apiKey")
		delete(cfg, "hooks")
		delete(cfg, "__oryxai_managed")
	}
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return AtomicWriteFile(path, out, 0o600)
}
