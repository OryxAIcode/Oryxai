package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/OryxAIcode/Oryxai/cmd/oryxai/internal/client"
	"github.com/OryxAIcode/Oryxai/cmd/oryxai/internal/keystore"
	"github.com/OryxAIcode/Oryxai/cmd/oryxai/internal/recipes"
)

// Install handles `oryxai install [flags]`.
//
// Flow:
//
//  1. Parse flags. Fall back to interactive prompts for missing values.
//  2. Verify the API key + slug combination with the control plane.
//  3. Write ~/.oryxai/config.
//  4. Detect installed agents (or use --agent list).
//  5. For each: run recipe.Install with a backup pass.
//  6. Print a summary.
//
// Exits non-zero on validation errors or any recipe failure.
func Install(args []string) int {
	fs := flagSet("install")
	apiKey := fs.String("api-key", "", "csk_… API key (otherwise prompts)")
	orgSlug := fs.String("slug", "", "workspace slug (looked up from key if empty)")
	controlURL := fs.String("control-url", "", "OryxAI control plane base URL (default https://oryxai.dev)")
	proxyURL := fs.String("proxy-url", "", "Server-side proxy URL (derived from control-url if empty)")
	mcpURL := fs.String("mcp-url", "", "Server-side MCP URL (derived from control-url if empty)")
	withHook := fs.Bool("with-hook", false, "Install PreToolUse hooks for tool-call-level visibility")
	dryRun := fs.Bool("dry-run", false, "Print planned changes; write nothing")
	var agents stringSliceFlag
	fs.Var(&agents, "agent", "Install only the named agent (repeatable); default: all detected")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	// 1. Gather config.
	resolvedControl, resolvedProxy, resolvedMCP := resolveControlURLs(*controlURL, *proxyURL, *mcpURL)

	key := strings.TrimSpace(*apiKey)
	if key == "" {
		var err error
		key, err = promptSecret(fmt.Sprintf("Paste your OryxAI API key (csk_…) from %s/o/<your-slug>/api-keys: ", resolvedControl))
		if err != nil {
			printErr("could not read API key: %v", err)
			return 1
		}
	}
	if !strings.HasPrefix(key, "csk_") {
		printErr("API key must start with csk_ (got %q…). Re-run install.", truncate(key, 8))
		return 1
	}

	slug := strings.TrimSpace(*orgSlug)
	if slug == "" {
		var err error
		slug, err = promptLine("Workspace slug (from your dashboard URL, e.g. 'my-team'): ")
		if err != nil {
			printErr("could not read slug: %v", err)
			return 1
		}
	}

	// 2. Verify against control plane. Skipped in dry-run because the
	// whole point of dry-run is to preview without touching anything
	// external.
	if !*dryRun {
		fmt.Printf("→ Verifying API key against %s …\n", resolvedControl)
		c := client.New(resolvedControl, slug, key)
		verifyCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := c.VerifyKey(verifyCtx); err != nil {
			printErr("%v", err)
			return 1
		}
		fmt.Println("✓ API key verified")
	} else {
		fmt.Println("→ Dry-run: skipping API key verification")
	}

	// 3. Write ~/.oryxai/config.
	cfg := keystore.Config{
		APIKey:      key,
		OrgSlug:     slug,
		ControlURL:  resolvedControl,
		ProxyURL:    resolvedProxy,
		MCPURL:      resolvedMCP,
		InstalledAt: time.Now().UTC(),
	}
	if !*dryRun {
		if err := keystore.Save(cfg); err != nil {
			printErr("could not write config file: %v", err)
			return 1
		}
		p, _ := keystore.Path()
		fmt.Printf("✓ Wrote %s\n", p)
	}

	// 4. Decide which agents to install.
	hookPath, _ := os.Executable()
	backupsDir, err := keystore.BackupsDir()
	if err != nil {
		printErr("backups dir: %v", err)
		return 1
	}
	all := recipes.All()
	var selected []recipes.Recipe
	if len(agents) > 0 {
		// User-specified subset — install whether or not detect passes.
		for _, name := range agents {
			r := recipes.ByName(name)
			if r == nil {
				printErr("unknown agent %q. Available: %s", name, strings.Join(recipes.Names(), ", "))
				return 1
			}
			selected = append(selected, r)
		}
	} else {
		fmt.Println("→ Detecting AI tools on this machine …")
		for _, r := range all {
			if err := r.Detect(); err == nil {
				fmt.Printf("  ✓ %s\n", r.DisplayName())
				selected = append(selected, r)
			}
		}
		if len(selected) == 0 {
			fmt.Println()
			fmt.Println("  No AI tools auto-detected. You can still install for a specific tool with:")
			fmt.Println("      oryxai install --agent claude-code")
			fmt.Println("  Available agents:")
			for _, n := range recipes.Names() {
				fmt.Printf("      %s\n", n)
			}
			return 0
		}
	}

	// 5. Install each.
	ictx := recipes.InstallContext{
		Cfg:        cfg,
		WithHook:   *withHook,
		HookPath:   hookPath,
		BackupsDir: backupsDir,
		DryRun:     *dryRun,
	}
	failures := 0
	fmt.Println()
	fmt.Println("→ Installing …")
	for _, r := range selected {
		err := r.Install(ictx)
		if err != nil {
			if errors.Is(err, recipes.ErrNotInstalled) {
				fmt.Printf("  ⚠ %s — not detected, skipped (use --agent %s to force)\n", r.DisplayName(), r.Name())
				continue
			}
			fmt.Printf("  ✗ %s — %v\n", r.DisplayName(), err)
			failures++
			continue
		}
		modeBadge := badgeForMode(r.Mode())
		fmt.Printf("  ✓ %s   %s\n", r.DisplayName(), modeBadge)
	}

	// 6. Summary.
	fmt.Println()
	if *dryRun {
		fmt.Println("Dry-run: nothing written.")
		return 0
	}
	if failures > 0 {
		fmt.Printf("Installed with %d failure(s). Re-run with --agent <name> to retry.\n", failures)
		return 1
	}
	fmt.Println("OryxAI is active. View activity at:")
	fmt.Printf("    %s/o/%s/audit\n", resolvedControl, slug)
	fmt.Println()
	fmt.Println("To uninstall: oryxai uninstall")
	if !*withHook {
		fmt.Println("To enable PreToolUse hooks (finer-grained visibility): oryxai install --with-hook")
	}
	return 0
}

func badgeForMode(m recipes.Mode) string {
	switch m {
	case recipes.ModeAPIIntercept:
		return "[api-intercept]"
	case recipes.ModeHook:
		return "[hook]"
	case recipes.ModeAdvisory:
		return "[advisory — LLM may ignore]"
	case recipes.ModeMixed:
		return "[mixed]"
	}
	return ""
}

func promptLine(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	br := bufio.NewReader(os.Stdin)
	line, err := br.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func promptSecret(prompt string) (string, error) {
	// We don't import golang.org/x/term to keep the binary
	// dependency-free. Without it, we can't disable echo cleanly on
	// every shell — accept echoed input; user can re-paste if they
	// don't want history capture (most modern terminals don't capture
	// stdin into history anyway).
	return promptLine(prompt)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
