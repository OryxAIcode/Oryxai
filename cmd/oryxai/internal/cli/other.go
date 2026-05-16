package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/OryxAIcode/Oryxai/cmd/oryxai/internal/buffer"
	"github.com/OryxAIcode/Oryxai/cmd/oryxai/internal/client"
	"github.com/OryxAIcode/Oryxai/cmd/oryxai/internal/keystore"
	"github.com/OryxAIcode/Oryxai/cmd/oryxai/internal/recipes"
)

// Uninstall handles `oryxai uninstall`. Reverses every recipe and
// (optionally) deletes ~/.oryxai/config.
func Uninstall(args []string) int {
	fs := flagSet("uninstall")
	keepKey := fs.Bool("keep-key", false, "Don't delete ~/.oryxai/config (default: delete)")
	var agents stringSliceFlag
	fs.Var(&agents, "agent", "Uninstall only the named agent (repeatable)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	all := recipes.All()
	var selected []recipes.Recipe
	if len(agents) > 0 {
		for _, name := range agents {
			r := recipes.ByName(name)
			if r == nil {
				printErr("unknown agent %q", name)
				return 1
			}
			selected = append(selected, r)
		}
	} else {
		selected = all
	}

	failures := 0
	for _, r := range selected {
		err := r.Uninstall()
		if err != nil {
			if errors.Is(err, recipes.ErrNotInstalled) {
				continue
			}
			fmt.Printf("  ✗ %s — %v\n", r.DisplayName(), err)
			failures++
			continue
		}
		fmt.Printf("  ✓ %s\n", r.DisplayName())
	}

	if !*keepKey {
		p, _ := keystore.Path()
		if err := removeFile(p); err == nil {
			fmt.Printf("  ✓ Removed %s\n", p)
		}
	} else {
		fmt.Println("  (keeping ~/.oryxai/config — re-run install without --keep-key to remove)")
	}

	if failures > 0 {
		return 1
	}
	return 0
}

// Status handles `oryxai status`. One-shot summary: is install done,
// which agents are wired up, how recent is the last feed event.
func Status(args []string) int {
	fs := flagSet("status")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	installed, err := keystore.Exists()
	if err != nil {
		printErr("could not check install state: %v", err)
		return 1
	}
	if !installed {
		fmt.Println("Not installed. Run `oryxai install` to get started.")
		return 0
	}
	cfg, err := keystore.Load()
	if err != nil {
		printErr("could not load config: %v", err)
		return 1
	}
	fmt.Printf("Workspace:    %s\n", cfg.OrgSlug)
	fmt.Printf("Control URL:  %s\n", cfg.ControlURL)
	fmt.Printf("Proxy URL:    %s\n", cfg.ProxyURL)
	fmt.Printf("MCP URL:      %s\n", cfg.MCPURL)
	fmt.Printf("Installed at: %s\n", cfg.InstalledAt.Format(time.RFC3339))
	fmt.Println()
	fmt.Println("Agents:")
	for _, r := range recipes.All() {
		err := r.Detect()
		switch {
		case err == nil:
			fmt.Printf("  ✓ %-30s  %s\n", r.DisplayName(), badgeForMode(r.Mode()))
		case errors.Is(err, recipes.ErrNotInstalled):
			// Not on this machine — silent skip in status to keep
			// output tight.
		default:
			fmt.Printf("  ? %-30s  (detect error: %v)\n", r.DisplayName(), err)
		}
	}

	// Opportunistic drain — every status invocation also flushes
	// queued hook events. Quiet on success / failure: status is a
	// diagnostic command, not a transactional one.
	pending, _ := buffer.Count()
	if pending > 0 {
		fmt.Printf("\nPending events: %d\n", pending)
		c := client.New(cfg.ControlURL, cfg.OrgSlug, cfg.APIKey)
		sent, err := buffer.Drain(c, 100)
		if err != nil {
			fmt.Printf("  drain: %v (events kept for next try)\n", err)
		} else if sent > 0 {
			fmt.Printf("  drained %d events to %s\n", sent, cfg.ControlURL)
		}
	}
	return 0
}

// Verify handles `oryxai verify`. Round-trips a tiny ingest call and
// reports latency. Useful diagnostic if the user thinks something is
// wrong.
func Verify(args []string) int {
	fs := flagSet("verify")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cfg, err := keystore.Load()
	if err != nil || cfg.APIKey == "" {
		printErr("not installed; run `oryxai install` first")
		return 1
	}
	c := client.New(cfg.ControlURL, cfg.OrgSlug, cfg.APIKey)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Test 1: empty events array → 400 means auth path passed.
	t0 := time.Now()
	if err := c.VerifyKey(ctx); err != nil {
		printErr("verify failed: %v", err)
		return 1
	}
	authLat := time.Since(t0)

	// Test 2: a real ingest with a single client.* event.
	t1 := time.Now()
	resp, err := c.Ingest(ctx, []client.FeedEvent{{
		Kind:          "client.verify",
		At:            time.Now().UTC().Format(time.RFC3339),
		Tool:          "verify",
		Agent:         "oryxai-cli",
		ParamsSummary: "oryxai verify probe",
	}})
	if err != nil {
		printErr("ingest probe failed: %v", err)
		return 1
	}
	ingestLat := time.Since(t1)

	fmt.Println("✓ Connectivity OK")
	fmt.Printf("  Auth round-trip:   %s\n", authLat)
	fmt.Printf("  Ingest round-trip: %s   (accepted=%d, rejected=%d)\n",
		ingestLat, resp.Accepted, resp.Rejected)
	fmt.Println()
	fmt.Printf("View the probe event:  %s/o/%s/audit?kind=client.verify\n",
		cfg.ControlURL, cfg.OrgSlug)
	return 0
}

func removeFile(p string) error {
	if strings.TrimSpace(p) == "" {
		return fmt.Errorf("empty path")
	}
	// Use os.Remove via recipes.AtomicWriteFile? No — recipes is for
	// recipes. Just os.Remove here.
	return removeIfExists(p)
}
