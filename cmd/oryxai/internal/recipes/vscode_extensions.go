package recipes

// VS Code-resident extensions all share the same install pattern:
// merge a small block into the user's VS Code settings.json. We have
// four of them — Cline, Continue, Cody, Codeium.
//
// VS Code user settings lives at one of these paths depending on OS:
//   macOS:   ~/Library/Application Support/Code/User/settings.json
//   Linux:   ~/.config/Code/User/settings.json
//   Windows: %APPDATA%\Code\User\settings.json
//
// Cursor uses the same layout under "Cursor" so this helper works
// there too, but Cursor has its own dedicated MCP recipe above.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// vscodeUserSettingsPath returns the platform-specific path to the
// VS Code user settings file. Returns ErrNotInstalled if no VS Code
// directory exists on this machine.
func vscodeUserSettingsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	var p string
	switch runtime.GOOS {
	case "darwin":
		p = filepath.Join(home, "Library", "Application Support", "Code", "User", "settings.json")
	case "windows":
		appdata := os.Getenv("APPDATA")
		if appdata == "" {
			appdata = filepath.Join(home, "AppData", "Roaming")
		}
		p = filepath.Join(appdata, "Code", "User", "settings.json")
	default:
		// linux + others — XDG_CONFIG_HOME or ~/.config
		xdg := os.Getenv("XDG_CONFIG_HOME")
		if xdg == "" {
			xdg = filepath.Join(home, ".config")
		}
		p = filepath.Join(xdg, "Code", "User", "settings.json")
	}
	if !FileExists(filepath.Dir(p)) {
		return "", ErrNotInstalled
	}
	return p, nil
}

// mergeVSCodeSettings applies keys to the VS Code user settings file
// atomically, backing up first. Recipes use this so each one is just
// 30 lines.
func mergeVSCodeSettings(ctx InstallContext, agent string, updates map[string]any) error {
	path, err := vscodeUserSettingsPath()
	if err != nil {
		return err
	}
	settings := map[string]any{}
	if data, err := os.ReadFile(path); err == nil {
		// VS Code settings.json supports comments + trailing commas
		// (json-c), but most users never use those. We do a strict
		// parse; if it fails, we skip writing rather than rewrite a
		// file we don't understand. (Honest fail-loud.)
		if err := json.Unmarshal(data, &settings); err != nil {
			return fmt.Errorf("VS Code settings.json contains comments or trailing commas; oryxai can't safely merge — please install %s manually or remove comments. detail: %v", agent, err)
		}
		backup := filepath.Join(ctx.BackupsDir, BackupName("vscode-"+agent, ".json"))
		if !ctx.DryRun {
			if err := CopyFile(path, backup); err != nil && err != ErrNotInstalled {
				return fmt.Errorf("backup %s: %w", path, err)
			}
		}
	}
	for k, v := range updates {
		settings[k] = v
	}
	// Tag the file so uninstall can remove only our keys without
	// guessing.
	managed, _ := settings["__oryxai_managed_keys"].([]any)
	for k := range updates {
		if !containsString(managed, k) {
			managed = append(managed, k)
		}
	}
	settings["__oryxai_managed_keys"] = managed

	if ctx.DryRun {
		return previewJSON(path, settings)
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return AtomicWriteFile(path, data, 0o600)
}

// removeFromVSCodeSettings strips the keys we set during install,
// using the __oryxai_managed_keys marker.
func removeFromVSCodeSettings(agent string) error {
	path, err := vscodeUserSettingsPath()
	if err != nil {
		if err == ErrNotInstalled {
			return nil
		}
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
		return err
	}
	if managed, ok := settings["__oryxai_managed_keys"].([]any); ok {
		for _, k := range managed {
			if ks, ok := k.(string); ok {
				delete(settings, ks)
			}
		}
		delete(settings, "__oryxai_managed_keys")
	}
	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	_ = agent // currently unused; kept for symmetry with install signature
	return AtomicWriteFile(path, out, 0o600)
}

func containsString(items []any, target string) bool {
	for _, it := range items {
		if s, ok := it.(string); ok && s == target {
			return true
		}
	}
	return false
}

// detectVSCodeExtension checks `code --list-extensions` (or its
// JetBrains/Cursor variant) for the given publisher.extensionId.
// We don't actually shell out by default — too slow and brittle. We
// fall back to checking the VS Code user settings file existence
// (any VSCode install) as a proxy for "the extension may be installed
// here."
func detectVSCodeExtension(_ /* extensionID */ string) error {
	_, err := vscodeUserSettingsPath()
	return err
}

// ─── Cline ───────────────────────────────────────────────────────

type ClineRecipe struct{}

func (ClineRecipe) Name() string        { return "cline" }
func (ClineRecipe) DisplayName() string { return "Cline" }
func (ClineRecipe) Mode() Mode          { return ModeAPIIntercept }
func (ClineRecipe) Detect() error       { return detectVSCodeExtension("saoudrizwan.claude-dev") }

func (ClineRecipe) Install(ctx InstallContext) error {
	return mergeVSCodeSettings(ctx, "cline", map[string]any{
		"cline.openaiBaseUrl": ctx.Cfg.ProxyURL + "/v1",
		"cline.apiKey":        ctx.Cfg.APIKey,
	})
}
func (ClineRecipe) Uninstall() error { return removeFromVSCodeSettings("cline") }

// ─── Continue ────────────────────────────────────────────────────

type ContinueRecipe struct{}

func (ContinueRecipe) Name() string        { return "continue" }
func (ContinueRecipe) DisplayName() string { return "Continue" }
func (ContinueRecipe) Mode() Mode          { return ModeAPIIntercept }

func (ContinueRecipe) Detect() error {
	// Continue stores config at ~/.continue/config.json (or .yaml).
	p, err := HomePath(".continue", "config.json")
	if err != nil {
		return err
	}
	if FileExists(p) {
		return nil
	}
	if py, err := HomePath(".continue", "config.yaml"); err == nil && FileExists(py) {
		return nil
	}
	return ErrNotInstalled
}

func (ContinueRecipe) Install(ctx InstallContext) error {
	path, err := HomePath(".continue", "config.json")
	if err != nil {
		return err
	}
	cfg := map[string]any{}
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &cfg); err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		backup := filepath.Join(ctx.BackupsDir, BackupName("continue", ".json"))
		if !ctx.DryRun {
			if err := CopyFile(path, backup); err != nil && err != ErrNotInstalled {
				return fmt.Errorf("backup: %w", err)
			}
		}
	}
	// Continue's "models" array — append (or replace) an OryxAI entry.
	// Provider "openai" with custom apiBase is the standard pattern.
	entry := map[string]any{
		"title":            "OryxAI (proxy)",
		"provider":         "openai",
		"model":            "auto",
		"apiBase":          ctx.Cfg.ProxyURL + "/v1",
		"apiKey":           ctx.Cfg.APIKey,
		"__oryxai_managed": true,
	}
	models, _ := cfg["models"].([]any)
	filtered := make([]any, 0, len(models)+1)
	for _, m := range models {
		if mm, ok := m.(map[string]any); ok {
			if _, ours := mm["__oryxai_managed"]; ours {
				continue
			}
		}
		filtered = append(filtered, m)
	}
	filtered = append(filtered, entry)
	cfg["models"] = filtered

	if ctx.DryRun {
		return previewJSON(path, cfg)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return AtomicWriteFile(path, data, 0o600)
}

func (ContinueRecipe) Uninstall() error {
	path, err := HomePath(".continue", "config.json")
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
	if models, ok := cfg["models"].([]any); ok {
		filtered := make([]any, 0, len(models))
		for _, m := range models {
			if mm, ok := m.(map[string]any); ok {
				if _, ours := mm["__oryxai_managed"]; ours {
					continue
				}
			}
			filtered = append(filtered, m)
		}
		cfg["models"] = filtered
	}
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return AtomicWriteFile(path, out, 0o600)
}

// ─── Cody (Sourcegraph) ──────────────────────────────────────────

type CodyRecipe struct{}

func (CodyRecipe) Name() string        { return "cody" }
func (CodyRecipe) DisplayName() string { return "Cody (Sourcegraph)" }
func (CodyRecipe) Mode() Mode          { return ModeAPIIntercept }
func (CodyRecipe) Detect() error       { return detectVSCodeExtension("sourcegraph.cody-ai") }

func (CodyRecipe) Install(ctx InstallContext) error {
	return mergeVSCodeSettings(ctx, "cody", map[string]any{
		"cody.experimental.openaiCompatibleEndpoint": ctx.Cfg.ProxyURL + "/v1",
		"cody.experimental.openaiCompatibleAPIKey":   ctx.Cfg.APIKey,
	})
}
func (CodyRecipe) Uninstall() error { return removeFromVSCodeSettings("cody") }

// ─── Codeium ────────────────────────────────────────────────────

type CodeiumRecipe struct{}

func (CodeiumRecipe) Name() string        { return "codeium" }
func (CodeiumRecipe) DisplayName() string { return "Codeium" }
func (CodeiumRecipe) Mode() Mode          { return ModeAPIIntercept }
func (CodeiumRecipe) Detect() error       { return detectVSCodeExtension("Codeium.codeium") }

func (CodeiumRecipe) Install(ctx InstallContext) error {
	return mergeVSCodeSettings(ctx, "codeium", map[string]any{
		"codeium.apiBaseUrl": ctx.Cfg.ProxyURL,
		"codeium.apiKey":     ctx.Cfg.APIKey,
	})
}
func (CodeiumRecipe) Uninstall() error { return removeFromVSCodeSettings("codeium") }
