package cli

import (
	"strings"
	"testing"
)

// TestDecodeAgentEnvelope verifies each agent's JSON envelope is
// correctly parsed into our canonical FeedEvent.
func TestDecodeAgentEnvelope(t *testing.T) {
	tests := []struct {
		agent    string
		body     string
		wantOK   bool
		wantTool string
		wantKind string
	}{
		{
			agent:    "claude-code",
			body:     `{"tool_name":"Bash","tool_input":{"command":"rm -rf node_modules"}}`,
			wantOK:   true,
			wantTool: "Bash",
			wantKind: "hook.bash",
		},
		{
			agent:    "claude-code",
			body:     `{"tool_name":"Write","tool_input":{"file_path":"/tmp/x","content":"hi"}}`,
			wantOK:   true,
			wantTool: "Write",
			wantKind: "hook.write",
		},
		{
			agent:    "cursor",
			body:     `{"tool":"execute_command","params":{"command":"ls"}}`,
			wantOK:   true,
			wantTool: "execute_command",
			wantKind: "hook.bash",
		},
		{
			agent:    "gemini-cli",
			body:     `{"name":"web_fetch","args":{"url":"https://example.com"}}`,
			wantOK:   true,
			wantTool: "web_fetch",
			wantKind: "hook.web",
		},
		{
			agent:    "opencode",
			body:     `{"tool":"read_file","params":{"path":"/tmp/x"}}`,
			wantOK:   true,
			wantTool: "read_file",
			wantKind: "hook.read",
		},
		{
			agent:  "claude-code",
			body:   `not json`,
			wantOK: false,
		},
		{
			agent:    "unknown-agent",
			body:     `{}`,
			wantOK:   true,
			wantTool: "unknown",
			wantKind: "hook.other",
		},
	}
	for _, tc := range tests {
		t.Run(tc.agent+"/"+tc.body[:min(20, len(tc.body))], func(t *testing.T) {
			evt, ok := decodeAgentEnvelope(tc.agent, []byte(tc.body))
			if ok != tc.wantOK {
				t.Fatalf("ok: got %v, want %v", ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if evt.Tool != tc.wantTool {
				t.Errorf("tool: got %q, want %q", evt.Tool, tc.wantTool)
			}
			if evt.Kind != tc.wantKind {
				t.Errorf("kind: got %q, want %q", evt.Kind, tc.wantKind)
			}
		})
	}
}

// TestClassifyTool covers the cross-agent normalization that maps
// "execute_command" / "Bash" / "run_terminal" → "bash".
func TestClassifyTool(t *testing.T) {
	cases := map[string]string{
		"Bash":            "bash",
		"execute_command": "bash",
		"run_terminal":    "bash",
		"Write":           "write",
		"edit_file":       "write",
		"Read":            "read",
		"view_file":       "read",
		"Glob":            "search",
		"grep_search":     "search",
		"web_fetch":       "web",
		"WebSearch":       "web",
		"frobnicate":      "other",
	}
	for in, want := range cases {
		if got := classifyTool(in); got != want {
			t.Errorf("classifyTool(%q): got %q, want %q", in, got, want)
		}
	}
}

// TestSummarize redacts sensitive content while keeping the tool +
// path/command visible.
func TestSummarize(t *testing.T) {
	got := summarize("Bash", map[string]any{"command": "rm -rf node_modules"})
	if !strings.Contains(got, "rm -rf node_modules") {
		t.Errorf("summarize should keep command: got %q", got)
	}
	got = summarize("Write", map[string]any{"file_path": "/tmp/x"})
	if !strings.Contains(got, "/tmp/x") {
		t.Errorf("summarize should keep path: got %q", got)
	}
	// Long command should be clamped. "…" is 3 bytes in UTF-8, so
	// the byte length can be up to 200 + 2 = 202.
	long := strings.Repeat("x", 500)
	got = summarize("Bash", map[string]any{"command": long})
	if len(got) > 210 {
		t.Errorf("summarize should clamp to ~200 chars: got len=%d", len(got))
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
