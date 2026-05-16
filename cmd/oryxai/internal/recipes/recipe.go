// Package recipes contains per-agent installer logic.
//
// Each recipe implements Recipe and is registered in registry.go.
// A recipe is a pure value (no state) that knows how to:
//
//   - Detect whether the agent is installed on this machine.
//   - Install a config that points the agent at OryxAI.
//   - Optionally install a PreToolUse hook for finer-grained
//     telemetry.
//   - Uninstall by restoring the agent's previous config from the
//     backup we made on install.
package recipes

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/OryxAIcode/Oryxai/cmd/oryxai/internal/keystore"
)

// ErrNotInstalled is returned by Recipe.Detect's caller when the
// recipe's underlying agent isn't present on the machine.
var ErrNotInstalled = errors.New("agent not detected on this machine")

// Recipe is the contract every per-agent installer implements.
type Recipe interface {
	// Name is a short kebab-case identifier (e.g. "claude-code").
	// Also the value users pass to `oryxai install --agent <name>`.
	Name() string

	// DisplayName is human-friendly (e.g. "Claude Code").
	DisplayName() string

	// Detect returns nil if the agent is present, ErrNotInstalled if
	// not. Any other error means the detection itself failed (e.g.
	// permission denied on the home directory).
	Detect() error

	// Install writes the recipe's config files. Idempotent: re-running
	// re-writes the same files; existing backups are preserved.
	Install(cfg InstallContext) error

	// Uninstall restores the agent's config from the most recent backup
	// we created on install. Idempotent.
	Uninstall() error

	// Mode tells the caller whether this recipe gives real enforcement
	// (the agent will block tool calls when we ask) or advisory only
	// (we put a rule file in the project and hope the LLM reads it).
	// Surfaced to the user during `oryxai install` so expectations are
	// honest.
	Mode() Mode
}

// Mode is the integration's strength.
type Mode string

const (
	// ModeAPIIntercept — agent's LLM requests go through OryxAI's
	// proxy or MCP server. Real visibility at the API level.
	ModeAPIIntercept Mode = "api-intercept"

	// ModeHook — agent's PreToolUse / BeforeTool / before_tool_call
	// hook calls oryxai-hook. Real visibility at the tool-call level.
	// Most powerful but requires the agent to support hooks.
	ModeHook Mode = "hook"

	// ModeAdvisory — we drop a rule file in the project. The LLM may
	// read and follow it, or may ignore it. Honest label.
	ModeAdvisory Mode = "advisory"

	// ModeMixed — a recipe that uses more than one mechanism (e.g.
	// Cline gets both proxy env var AND .clinerules).
	ModeMixed Mode = "mixed"
)

// InstallContext is everything a recipe needs to write its config.
type InstallContext struct {
	Cfg        keystore.Config // API key, slug, URLs
	WithHook   bool            // install PreToolUse hooks where supported
	HookPath   string          // absolute path to the oryxai binary's hook command
	BackupsDir string          // ~/.oryxai/backups/
	DryRun     bool            // print intended changes, write nothing
}

// HomePath returns ~/<rel...>, expanding the user's home directory.
// Helper used by every recipe.
func HomePath(rel ...string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("user home: %w", err)
	}
	parts := append([]string{home}, rel...)
	return filepath.Join(parts...), nil
}

// FileExists is a cheap check used by Detect implementations.
func FileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// BackupName generates the per-agent backup filename: <agent>-<timestamp>.<ext>.
// Same agent installed twice produces two backups so the most recent
// uninstall always finds something to restore.
func BackupName(agent, ext string) string {
	return fmt.Sprintf("%s-%s%s", agent, time.Now().UTC().Format("20060102-150405"), ext)
}

// CopyFile is used to make a backup before modifying an agent's config.
// Returns ErrNotInstalled if the source doesn't exist (no backup needed).
func CopyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		if os.IsNotExist(err) {
			return ErrNotInstalled
		}
		return fmt.Errorf("open %s: %w", src, err)
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return fmt.Errorf("mkdir backup dir: %w", err)
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("create %s: %w", dst, err)
	}
	defer out.Close()
	buf := make([]byte, 32*1024)
	for {
		n, rerr := in.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				return fmt.Errorf("write backup: %w", werr)
			}
		}
		if rerr != nil {
			if rerr.Error() == "EOF" {
				break
			}
			// io.EOF imported elsewhere; use string-compare via Error()
			// to avoid pulling io into every file.
			break
		}
	}
	return nil
}

// AtomicWriteFile writes data to dst via a temp file in the same dir
// then renames into place. fsyncs before rename so a crash between
// rename and the OS flushing leaves the file consistent.
func AtomicWriteFile(dst string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("mkdir parent: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), "."+filepath.Base(dst)+".tmp-*")
	if err != nil {
		return fmt.Errorf("temp file: %w", err)
	}
	rollback := func() { _ = os.Remove(tmp.Name()) }
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		rollback()
		return fmt.Errorf("write: %w", err)
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		rollback()
		return fmt.Errorf("chmod: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		rollback()
		return fmt.Errorf("fsync: %w", err)
	}
	if err := tmp.Close(); err != nil {
		rollback()
		return fmt.Errorf("close: %w", err)
	}
	if err := os.Rename(tmp.Name(), dst); err != nil {
		rollback()
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}
