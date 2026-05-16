package recipes

// Cursor recipe.
//
// Writes ~/.cursor/mcp.json with an "oryxai" MCP server entry that
// points at our hosted SSE endpoint. Cursor 0.45+ reads this file at
// startup and registers each entry as an available MCP server.
//
// The mcp.json file is owned by Cursor but lets users add custom
// entries; we merge our "oryxai" key without disturbing others.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const cursorName = "cursor"

type CursorRecipe struct{}

func (CursorRecipe) Name() string        { return cursorName }
func (CursorRecipe) DisplayName() string { return "Cursor" }
func (CursorRecipe) Mode() Mode          { return ModeAPIIntercept }

func (CursorRecipe) Detect() error {
	dir, err := HomePath(".cursor")
	if err != nil {
		return err
	}
	if FileExists(dir) {
		return nil
	}
	return ErrNotInstalled
}

func (CursorRecipe) Install(ctx InstallContext) error {
	path, err := HomePath(".cursor", "mcp.json")
	if err != nil {
		return err
	}

	cfg := map[string]any{}
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &cfg); err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		backup := filepath.Join(ctx.BackupsDir, BackupName(cursorName, ".json"))
		if !ctx.DryRun {
			if err := CopyFile(path, backup); err != nil && err != ErrNotInstalled {
				return fmt.Errorf("backup %s: %w", path, err)
			}
		}
	}

	servers, _ := cfg["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	servers["oryxai"] = map[string]any{
		"transport": "sse",
		"url":       ctx.Cfg.MCPURL,
		"headers": map[string]any{
			"Authorization": "Bearer " + ctx.Cfg.APIKey,
		},
		"__oryxai_managed": true,
	}
	cfg["mcpServers"] = servers

	if ctx.DryRun {
		return previewJSON(path, cfg)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return AtomicWriteFile(path, data, 0o600)
}

func (CursorRecipe) Uninstall() error {
	path, err := HomePath(".cursor", "mcp.json")
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
	if servers, ok := cfg["mcpServers"].(map[string]any); ok {
		if entry, ok := servers["oryxai"].(map[string]any); ok {
			if _, ours := entry["__oryxai_managed"]; ours {
				delete(servers, "oryxai")
			}
		}
		if len(servers) == 0 {
			delete(cfg, "mcpServers")
		} else {
			cfg["mcpServers"] = servers
		}
	}
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return AtomicWriteFile(path, out, 0o600)
}
