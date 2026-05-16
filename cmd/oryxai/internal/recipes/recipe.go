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
	"strings"
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

	// ProjectDir is where rule-file recipes (Windsurf, Kilo, Codex,
	// Copilot CLI, Antigravity) drop their advisory .md files. Default
	// is os.Getwd() at install time. Operators on shared / multi-user
	// machines should override via --project-dir to avoid an attacker
	// pre-creating a malicious file in the working directory.
	ProjectDir string
}

// projectDirOrCwd resolves the project directory a rule-file recipe
// should write to. Prefers ctx.ProjectDir (set by --project-dir or
// validated default) and falls back to os.Getwd() — used by recipes
// for the legacy/uncontextual case.
func projectDirOrCwd(ctx InstallContext) (string, error) {
	if p := strings.TrimSpace(ctx.ProjectDir); p != "" {
		return p, nil
	}
	return os.Getwd()
}

// ValidateProjectDir refuses path that's a symlink, world-writable,
// or unreadable. Best-effort defense for rule-file recipes that drop
// markdown in the user's working directory.
func ValidateProjectDir(p string) error {
	info, err := os.Lstat(p)
	if err != nil {
		return fmt.Errorf("project dir %s: %w", p, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("project dir %s is a symlink — pass a concrete path with --project-dir", p)
	}
	if !info.IsDir() {
		return fmt.Errorf("project dir %s is not a directory", p)
	}
	// World-writable check: 0002 bit (others-write) on POSIX.
	// /tmp typically has the sticky bit (1777) which is still world-
	// writable for new files — refuse.
	if info.Mode().Perm()&0o002 != 0 {
		return fmt.Errorf("project dir %s is world-writable (mode %v) — rule files would be unsafe; pick a directory you own", p, info.Mode().Perm())
	}
	return nil
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
// ErrSymlinkTarget is returned by AtomicWriteFile when dst is a
// symlink — writing through it would let a local attacker who
// pre-created the link redirect our output to an arbitrary file.
// We refuse and force the user to investigate.
var ErrSymlinkTarget = errors.New("refusing to write through a symlink")

func AtomicWriteFile(dst string, data []byte, perm os.FileMode) error {
	// Symlink defense: if dst already exists AS a symlink, refuse.
	// Don't follow it. Local attackers who chmod our config dir could
	// otherwise pre-create `~/.claude/settings.json` → `/etc/cron.d/x`
	// and we'd happily write attacker-controlled JSON to root paths.
	// The window is small (same-user-only escalation) but defense in
	// depth costs almost nothing here.
	if info, err := os.Lstat(dst); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: %s", ErrSymlinkTarget, dst)
		}
	}
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
	// Final-check before rename: dst could have been created as a
	// symlink between our Lstat above and now. The rename path is
	// fast but not atomic with the check. Re-Lstat closes the TOCTOU
	// gap to the extent we can on POSIX.
	if info, lerr := os.Lstat(dst); lerr == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			rollback()
			return fmt.Errorf("%w: %s (created mid-write)", ErrSymlinkTarget, dst)
		}
	}
	if err := os.Rename(tmp.Name(), dst); err != nil {
		rollback()
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}
