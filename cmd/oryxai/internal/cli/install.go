package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode"

	"golang.org/x/term"

	"github.com/OryxAIcode/Oryxai/cmd/oryxai/internal/client"
	"github.com/OryxAIcode/Oryxai/cmd/oryxai/internal/keystore"
	"github.com/OryxAIcode/Oryxai/cmd/oryxai/internal/recipes"
)

// slugRe matches the same shape the control plane's normalizeSlug
// produces: lowercase alphanumerics + dashes, 3–48 chars, can't start
// or end with a dash. We re-check on the client even though the
// server already validates — defense in depth so a compromised or
// buggy control plane can't smuggle path-segment metacharacters
// (e.g. ".." or "/") into the URL we POST to.
//
// Length math: leading [a-z0-9] (1) + middle [a-z0-9-]{1,46} (1–46) +
// trailing [a-z0-9] (1) = 3–48 chars total.
var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,46}[a-z0-9]$`)

// sanitizeForTerminal strips control characters (incl. ANSI CSI/OSC
// sequences) from a string before printing. Defensive against an
// upstream that managed to land newlines/escapes in display names —
// terminal hijack is cheap to block and the legitimate values are
// strict ASCII.
func sanitizeForTerminal(s string) string {
	b := make([]rune, 0, len(s))
	for _, r := range s {
		if unicode.IsControl(r) || r == 0x7f {
			continue
		}
		b = append(b, r)
	}
	return string(b)
}

// Install handles `oryxai install [flags]`.
//
// Flow:
//
//  1. Parse flags. Prompt for the API key if --api-key isn't set.
//  2. Resolve the workspace slug via /api/v1/key/whoami (bearer auth).
//     Fall back to an interactive prompt only when the control plane
//     is too old to ship the endpoint, or when --slug was passed.
//  3. Cross-check (key, slug) against /feed/ingest.
//  4. Write ~/.oryxai/config.
//  5. Detect installed agents (or use --agent list).
//  6. Validate --project-dir for rule-file recipes.
//  7. Run each recipe with a backup pass.
//  8. Print a summary.
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
	projectDir := fs.String("project-dir", "", "Where rule-file recipes (Windsurf, Kilo, Codex, Antigravity, Copilot CLI) drop advisory .md files. Default: $PWD. Refuses world-writable or symlinked dirs.")
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
		key, err = promptSecret(fmt.Sprintf("Paste your OryxAI API key (csk_…) from %s/keys: ", resolvedControl))
		if err != nil {
			printErr("could not read API key: %v", err)
			return 1
		}
	}
	if !strings.HasPrefix(key, "csk_") {
		printErr("API key must start with csk_ (got %q…). Re-run install.", truncate(key, 8))
		return 1
	}

	// 2. Resolve slug. If the user passed --slug, trust it; otherwise
	// ask the control plane (Bearer csk_ → org_slug). Falling back to
	// an interactive prompt only when whoami isn't available keeps the
	// installer backward-compatible with older control-plane builds.
	slug := strings.TrimSpace(*orgSlug)
	if !*dryRun && slug == "" {
		fmt.Printf("→ Resolving workspace against %s …\n", resolvedControl)
		probe := client.New(resolvedControl, "", key)
		whoamiCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		who, werr := probe.WhoamiByKey(whoamiCtx)
		cancel()
		switch {
		case werr == nil:
			// Defense-in-depth: re-validate the slug shape before
			// using it in a URL. The control plane normalizes slugs
			// on insert, so a malformed value here means something
			// went wrong upstream — better to fail loud than POST
			// to a tampered path.
			if !slugRe.MatchString(who.OrgSlug) {
				printErr("control plane returned a malformed slug %q — refusing to proceed", sanitizeForTerminal(who.OrgSlug))
				return 1
			}
			slug = who.OrgSlug
			label := sanitizeForTerminal(who.OrgName)
			if label == "" {
				label = who.OrgSlug
			}
			fmt.Printf("✓ Connecting as %q (%s)\n", label, who.OrgSlug)
		case client.IsWhoamiUnsupported(werr):
			fmt.Println("  (older control plane — falling back to slug prompt)")
			var perr error
			slug, perr = promptLine("Workspace slug (from your dashboard URL, e.g. 'my-team'): ")
			if perr != nil {
				printErr("could not read slug: %v", perr)
				return 1
			}
		default:
			printErr("%v", werr)
			return 1
		}
	}
	if slug == "" {
		// Dry-run with no --slug, or a corner-case empty whoami result —
		// dry-run still needs *some* slug for the recipe preview, so ask.
		var err error
		slug, err = promptLine("Workspace slug (from your dashboard URL, e.g. 'my-team'): ")
		if err != nil {
			printErr("could not read slug: %v", err)
			return 1
		}
	}

	// Validate slug shape regardless of source (flag, whoami, prompt).
	// Same charset/length the control plane enforces — keeps URL path
	// segments clean.
	slug = strings.ToLower(strings.TrimSpace(slug))
	if !slugRe.MatchString(slug) {
		printErr("workspace slug %q is malformed (lowercase letters, digits, dashes; 3–48 chars; no leading/trailing dash)", sanitizeForTerminal(slug))
		return 1
	}

	// 3. Cross-check (key, slug) against /feed/ingest. Whoami already
	// proved the key works; this catches the corner case where the user
	// passed an explicit --slug that doesn't match the key's org.
	if !*dryRun {
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

	// 4. Write ~/.oryxai/config.
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

	// 5. Decide which agents to install.
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

	// 6. Resolve + validate project dir (only matters for rule-file
	// recipes — Windsurf, Kilo, Codex, etc. — but we validate
	// universally to keep the install summary deterministic).
	pdir := strings.TrimSpace(*projectDir)
	if pdir == "" {
		var werr error
		pdir, werr = os.Getwd()
		if werr != nil {
			printErr("could not resolve current dir; pass --project-dir: %v", werr)
			return 1
		}
	}
	if err := recipes.ValidateProjectDir(pdir); err != nil {
		printErr("%v", err)
		fmt.Fprintln(os.Stderr, "  Rule-file recipes (Windsurf, Kilo, Codex, Antigravity, Copilot CLI) write into the project directory.")
		fmt.Fprintln(os.Stderr, "  Re-run from a directory you own, or pass --project-dir /safe/path.")
		return 1
	}

	// 7. Install each.
	ictx := recipes.InstallContext{
		Cfg:        cfg,
		WithHook:   *withHook,
		HookPath:   hookPath,
		BackupsDir: backupsDir,
		DryRun:     *dryRun,
		ProjectDir: pdir,
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

	// 8. Summary.
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
	fmt.Fprint(os.Stderr, prompt)
	// Disable terminal echo while the API key is typed so it doesn't
	// land in tmux scrollback, the shell's stdin-echo history, or any
	// screen recording. term.ReadPassword does the right ioctl on
	// macOS/Linux and the equivalent SetConsoleMode on Windows.
	// Non-terminal stdin (e.g. piped from --headless setup script)
	// falls back to plain readline with an explicit notice.
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		fmt.Fprintln(os.Stderr, "  (stdin not a tty — input will be echoed)")
		return promptLine("")
	}
	buf, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr) // line break after silent input
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(buf)), nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
