// Command oryxai is the local agent installer + hook runner.
//
// Two modes a user runs into:
//
//	oryxai install              — interactive: paste API key, detect AI
//	                              tools on this machine, write per-tool
//	                              config that points each at OryxAI.
//	oryxai install --with-hook  — same, but also wire PreToolUse hooks
//	                              into each agent so tool calls (Bash,
//	                              Write, Edit, …) ship to the dashboard.
//
//	oryxai hook --agent <name>  — invoked by an agent's PreToolUse hook.
//	                              Reads tool-call JSON from stdin, ships
//	                              a non-blocking event to the dashboard,
//	                              returns allow. Never blocks.
//
// Architecture context: this binary does NOT run a long-lived daemon.
// `install` writes config files and exits. `hook` is invoked once per
// tool call by the agent itself. Events flow over HTTP to the existing
// /api/v1/orgs/{slug}/feed/ingest endpoint on the control plane.
package main

import (
	"fmt"
	"os"

	"github.com/OryxAIcode/Oryxai/cmd/oryxai/internal/cli"
	vinfo "github.com/OryxAIcode/Oryxai/internal/version"
)

func main() {
	if len(os.Args) < 2 {
		printUsage(os.Stderr)
		os.Exit(2)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "install":
		os.Exit(cli.Install(args))
	case "uninstall":
		os.Exit(cli.Uninstall(args))
	case "hook":
		os.Exit(cli.Hook(args))
	case "status":
		os.Exit(cli.Status(args))
	case "verify":
		os.Exit(cli.Verify(args))
	case "version", "--version", "-v":
		vinfo.Print(os.Stdout, "oryxai")
		os.Exit(0)
	case "help", "--help", "-h":
		printUsage(os.Stdout)
		os.Exit(0)
	default:
		fmt.Fprintf(os.Stderr, "oryxai: unknown command %q\n\n", cmd)
		printUsage(os.Stderr)
		os.Exit(2)
	}
}

func printUsage(w *os.File) {
	fmt.Fprint(w, `oryxai — local agent installer for OryxAI

USAGE
  oryxai <command> [flags]

COMMANDS
  install       Detect AI tools on this machine and configure each to route
                through OryxAI. Run once after creating an API key in the
                dashboard.

                Flags:
                  --api-key <csk_…>   non-interactive (otherwise prompts)
                  --slug <slug>       org slug (otherwise looked up from key)
                  --control-url URL   OryxAI control plane base (default:
                                      https://oryxai.dev)
                  --proxy-url URL     server-side proxy (default: derived
                                      from control-url)
                  --mcp-url URL       server-side MCP (default: derived
                                      from control-url)
                  --with-hook         also install per-tool PreToolUse hooks
                                      for fine-grained visibility
                  --agent <name>      install only one agent (default: all
                                      detected). Repeatable.
                  --dry-run           print planned changes, write nothing

  uninstall     Remove all OryxAI configuration from detected agents and
                restore their original config files from backups.

                Flags:
                  --agent <name>      uninstall only one agent (default: all)

  hook          Invoked by an AI agent's PreToolUse hook. Reads tool-call
                JSON from stdin, ships to the dashboard, returns allow.

                Flags:
                  --agent <name>      required: which agent's envelope to
                                      parse (claude-code, cursor, gemini, …)

  status        Show what's installed and recent activity.

  verify        Run a dry-run hook against each installed agent and print
                the round-trip latency. Useful diagnostic.

  version       Print version, commit, build time.

  help          Show this message.

WHERE THINGS LIVE
  ~/.oryxai/config            API key + org slug + control URL (mode 0600)
  ~/.oryxai/backups/          pre-install backups of every config file we
                              touched, named <agent>-<timestamp>.json

LINKS
  Dashboard:    https://oryxai.dev
  Source code:  https://github.com/OryxAIcode/Oryxai/tree/main/cmd/oryxai

`)
}
