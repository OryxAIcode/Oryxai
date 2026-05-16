// Package cli implements the oryxai subcommands.
//
// Each public function (Install, Uninstall, Hook, Status, Verify)
// returns a process exit code so main.go is a thin dispatcher.
package cli

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

// resolveControlURLs derives proxy/mcp URLs from the control-plane
// base when the user hasn't overridden them.
//
// Convention (matches docker-compose.prod.yml traefik routes):
//
//	control = https://oryxai.dev          → proxy = https://proxy.oryxai.dev
//	                                          mcp   = https://mcp.oryxai.dev/sse
//	control = https://app.example.com     → proxy = https://proxy.example.com
//	                                          mcp   = https://mcp.example.com/sse
//	control = http://localhost:8443       → proxy = http://localhost:8091
//	                                          mcp   = http://localhost:8090/sse
//
// The "swap app/control subdomain for proxy/mcp" rule matches how
// traefik is configured: each service has its own subdomain. For
// localhost / IP / single-host deployments we fall back to the port
// convention. Users on unusual setups override all three explicitly.
func resolveControlURLs(controlURL, proxyURL, mcpURL string) (string, string, string) {
	if controlURL == "" {
		controlURL = "https://oryxai.dev"
	}
	if proxyURL == "" {
		proxyURL = deriveSubdomain(controlURL, "proxy", 8091, "")
	}
	if mcpURL == "" {
		mcpURL = deriveSubdomain(controlURL, "mcp", 8090, "/sse")
	}
	return controlURL, proxyURL, mcpURL
}

// deriveSubdomain returns either:
//   - scheme://<sub>.<rest>          when the host has at least two
//     dots and looks like a real domain (oryxai.dev → proxy.oryxai.dev)
//   - scheme://host:port             when the host is localhost or
//     an IP (no dot or single-token), suffixed with the path arg
//
// Path is appended when non-empty (used for the "/sse" suffix on MCP).
func deriveSubdomain(controlURL, sub string, port int, path string) string {
	scheme, host := splitSchemeHost(controlURL)
	if scheme == "" {
		scheme = "https"
	}
	bare, _ := splitHostPort(host)
	if isLocalOrIP(bare) {
		return fmt.Sprintf("%s://%s:%d%s", scheme, bare, port, path)
	}
	// Real domain — prepend subdomain. If the host already has the
	// subdomain we want (e.g. user passed proxy.oryxai.dev as the
	// control URL, weird but tolerate), don't double-prefix.
	if strings.HasPrefix(bare, sub+".") {
		return fmt.Sprintf("%s://%s%s", scheme, bare, path)
	}
	// If host has only one segment (e.g. "myhost"), still fall back
	// to port-based — treating it as an internal hostname.
	if !strings.Contains(bare, ".") {
		return fmt.Sprintf("%s://%s:%d%s", scheme, bare, port, path)
	}
	return fmt.Sprintf("%s://%s.%s%s", scheme, sub, bare, path)
}

func splitSchemeHost(u string) (scheme, host string) {
	if i := strings.Index(u, "://"); i > 0 {
		return u[:i], u[i+3:]
	}
	return "", u
}

func splitHostPort(hostPort string) (host, port string) {
	// Strip trailing /path
	if i := strings.IndexByte(hostPort, '/'); i >= 0 {
		hostPort = hostPort[:i]
	}
	if i := strings.LastIndexByte(hostPort, ':'); i > 0 {
		return hostPort[:i], hostPort[i+1:]
	}
	return hostPort, ""
}

func isLocalOrIP(host string) bool {
	if host == "localhost" || host == "127.0.0.1" || host == "0.0.0.0" || host == "::1" {
		return true
	}
	// Crude IPv4 check — if every dot-segment is numeric, treat as IP.
	parts := strings.Split(host, ".")
	if len(parts) != 4 {
		return false
	}
	for _, p := range parts {
		if p == "" {
			return false
		}
		for _, c := range p {
			if c < '0' || c > '9' {
				return false
			}
		}
	}
	return true
}

// flagSet creates a new flag set with our standard error handling so
// subcommands behave consistently on bad input.
func flagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	return fs
}

// printErr writes a formatted error line to stderr.
func printErr(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "oryxai: "+format+"\n", args...)
}

// stringSliceFlag lets a flag be repeated, accumulating values. Used
// for --agent <name> --agent <name>.
type stringSliceFlag []string

func (f *stringSliceFlag) String() string {
	return fmt.Sprintf("%v", []string(*f))
}

func (f *stringSliceFlag) Set(v string) error {
	*f = append(*f, v)
	return nil
}
