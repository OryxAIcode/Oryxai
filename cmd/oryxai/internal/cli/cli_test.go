package cli

import "testing"

// TestResolveControlURLs covers the subdomain-derivation rules for
// proxy/MCP URLs. The rules are non-trivial (subdomain swap for real
// domains, port fallback for localhost / IPs), so we want to lock
// the expected behavior down in tests.
func TestResolveControlURLs(t *testing.T) {
	tests := []struct {
		name         string
		control      string
		proxy        string
		mcp          string
		wantControl  string
		wantProxy    string
		wantMCP      string
	}{
		{
			name:        "canonical oryxai.dev",
			control:     "https://oryxai.dev",
			wantControl: "https://oryxai.dev",
			wantProxy:   "https://proxy.oryxai.dev",
			wantMCP:     "https://mcp.oryxai.dev/sse",
		},
		{
			name:        "custom domain — subdomain swap",
			control:     "https://app.example.com",
			wantControl: "https://app.example.com",
			wantProxy:   "https://proxy.app.example.com",
			wantMCP:     "https://mcp.app.example.com/sse",
		},
		{
			name:        "localhost — port fallback",
			control:     "http://localhost:8443",
			wantControl: "http://localhost:8443",
			wantProxy:   "http://localhost:8091",
			wantMCP:     "http://localhost:8090/sse",
		},
		{
			name:        "IP address — port fallback",
			control:     "http://10.0.0.5:8443",
			wantControl: "http://10.0.0.5:8443",
			wantProxy:   "http://10.0.0.5:8091",
			wantMCP:     "http://10.0.0.5:8090/sse",
		},
		{
			name:        "single-label hostname — port fallback",
			control:     "http://internal:8443",
			wantControl: "http://internal:8443",
			wantProxy:   "http://internal:8091",
			wantMCP:     "http://internal:8090/sse",
		},
		{
			name:        "user overrides — passed through",
			control:     "https://staging.oryxai.dev",
			proxy:       "https://proxy-staging.oryxai.dev",
			mcp:         "https://mcp-staging.oryxai.dev/sse",
			wantControl: "https://staging.oryxai.dev",
			wantProxy:   "https://proxy-staging.oryxai.dev",
			wantMCP:     "https://mcp-staging.oryxai.dev/sse",
		},
		{
			name:        "default (empty)",
			wantControl: "https://oryxai.dev",
			wantProxy:   "https://proxy.oryxai.dev",
			wantMCP:     "https://mcp.oryxai.dev/sse",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotC, gotP, gotM := resolveControlURLs(tc.control, tc.proxy, tc.mcp)
			if gotC != tc.wantControl {
				t.Errorf("control: got %q, want %q", gotC, tc.wantControl)
			}
			if gotP != tc.wantProxy {
				t.Errorf("proxy: got %q, want %q", gotP, tc.wantProxy)
			}
			if gotM != tc.wantMCP {
				t.Errorf("mcp: got %q, want %q", gotM, tc.wantMCP)
			}
		})
	}
}

// TestIsLocalOrIP locks down the "treat as localhost/IP" decision
// since it drives the port-vs-subdomain branch above.
func TestIsLocalOrIP(t *testing.T) {
	cases := map[string]bool{
		"localhost":      true,
		"127.0.0.1":      true,
		"0.0.0.0":        true,
		"::1":            true,
		"10.0.0.5":       true,
		"192.168.1.100":  true,
		"oryxai.dev":     false,
		"app.example.com": false,
		"internal":       false, // single-label — handled by len(parts)!=4 branch
		"":               false,
	}
	for host, want := range cases {
		if got := isLocalOrIP(host); got != want {
			t.Errorf("isLocalOrIP(%q): got %v, want %v", host, got, want)
		}
	}
}
