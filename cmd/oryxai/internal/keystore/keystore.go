// Package keystore manages ~/.oryxai/config — the small file that
// holds the user's API key, org slug, and control-plane URLs after
// `oryxai install` has run.
//
// Format is intentionally simple key=value text (no JSON, no TOML
// dependency) so the file is hand-editable. Permissions are 0600 so
// only the user can read it.
package keystore

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Config is the on-disk representation. Empty fields mean "not set."
type Config struct {
	APIKey      string
	OrgSlug     string
	ControlURL  string
	ProxyURL    string
	MCPURL      string
	InstalledAt time.Time
}

// Path returns the canonical location of the config file
// (~/.oryxai/config) regardless of platform.
func Path() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("user home: %w", err)
	}
	return filepath.Join(home, ".oryxai", "config"), nil
}

// BackupsDir returns ~/.oryxai/backups/, creating it on demand.
// Side effect: opportunistically prunes backups older than 30 days so
// the directory doesn't grow unbounded over years of re-installs.
// 30 days is enough to recover from "I uninstalled last week and want
// my config back."
func BackupsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("user home: %w", err)
	}
	dir := filepath.Join(home, ".oryxai", "backups")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("mkdir backups: %w", err)
	}
	pruneOldBackups(dir, 30*24*time.Hour)
	return dir, nil
}

// pruneOldBackups removes files in dir whose mtime is older than max.
// Best-effort; errors swallowed (we never want install to fail because
// cleanup didn't work).
func pruneOldBackups(dir string, max time.Duration) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-max)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

// Load reads ~/.oryxai/config. Missing file returns an empty Config
// with no error — callers treat that as "not yet installed."
func Load() (Config, error) {
	p, err := Path()
	if err != nil {
		return Config{}, err
	}
	f, err := os.Open(p)
	if err != nil {
		if os.IsNotExist(err) {
			return Config{}, nil
		}
		return Config{}, fmt.Errorf("open config: %w", err)
	}
	defer f.Close()
	return parse(f)
}

// Save writes the config atomically: temp file in the same dir, fsync,
// rename. Permissions enforced at 0600.
func Save(cfg Config) error {
	p, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return fmt.Errorf("mkdir config dir: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), "config-*.tmp")
	if err != nil {
		return fmt.Errorf("temp file: %w", err)
	}
	rollback := func() { _ = os.Remove(tmp.Name()) }
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		rollback()
		return fmt.Errorf("chmod: %w", err)
	}
	if err := write(tmp, cfg); err != nil {
		_ = tmp.Close()
		rollback()
		return fmt.Errorf("write: %w", err)
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
	if err := os.Rename(tmp.Name(), p); err != nil {
		rollback()
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

// Exists is a cheap check used by `oryxai status` to tell whether the
// installer has been run on this machine.
func Exists() (bool, error) {
	p, err := Path()
	if err != nil {
		return false, err
	}
	if _, err := os.Stat(p); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func parse(r io.Reader) (Config, error) {
	var cfg Config
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.IndexByte(line, '=')
		if idx < 0 {
			// Unparseable line — skip silently so the user's manual
			// comments / annotations don't trip the loader.
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.Trim(strings.TrimSpace(line[idx+1:]), `"`)
		switch key {
		case "api_key":
			cfg.APIKey = val
		case "org_slug":
			cfg.OrgSlug = val
		case "control_url":
			cfg.ControlURL = val
		case "proxy_url":
			cfg.ProxyURL = val
		case "mcp_url":
			cfg.MCPURL = val
		case "installed_at":
			if t, err := time.Parse(time.RFC3339, val); err == nil {
				cfg.InstalledAt = t
			}
		}
	}
	if err := sc.Err(); err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	return cfg, nil
}

func write(w io.Writer, cfg Config) error {
	t := cfg.InstalledAt
	if t.IsZero() {
		t = time.Now().UTC()
	}
	_, err := fmt.Fprintf(w,
		"# Managed by oryxai install. Hand edits are preserved as long\n"+
			"# as keys stay on their own line. Re-run install --rotate-key\n"+
			"# to change the API key without losing other settings.\n"+
			"api_key      = %s\n"+
			"org_slug     = %s\n"+
			"control_url  = %s\n"+
			"proxy_url    = %s\n"+
			"mcp_url      = %s\n"+
			"installed_at = %s\n",
		cfg.APIKey, cfg.OrgSlug, cfg.ControlURL, cfg.ProxyURL, cfg.MCPURL,
		t.UTC().Format(time.RFC3339))
	return err
}
