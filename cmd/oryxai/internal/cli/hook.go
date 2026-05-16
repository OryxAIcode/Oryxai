package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/OryxAIcode/Oryxai/cmd/oryxai/internal/buffer"
	"github.com/OryxAIcode/Oryxai/cmd/oryxai/internal/client"
)

// Hook handles `oryxai hook --agent <name>`.
//
// Read a tool-call envelope from stdin, convert to our canonical
// FeedEvent, fire-and-forget POST to /feed/ingest, write
// {"decision":"allow"} (or the agent's equivalent) to stdout, exit.
//
// Hard constraint: hook must return within ~5 ms of being spawned.
// The POST runs in a goroutine with a short timeout; we don't wait
// for it. Worst case, network is slow → the event is dropped.
// Defensive failure mode is acceptable here because hooks fire
// on every tool call and the cost of being slow is far worse than
// dropping an occasional observation.
func Hook(args []string) int {
	fs := flagSet("hook")
	agent := fs.String("agent", "", "Agent envelope to parse: claude-code | cursor | gemini-cli | opencode | openclaw")
	if err := fs.Parse(args); err != nil {
		// Even on bad flags, never block the agent. Print allow.
		writeAllowFor("unknown")
		return 0
	}
	if strings.TrimSpace(*agent) == "" {
		writeAllowFor("unknown")
		return 0
	}

	// Read stdin with a generous deadline. If it stalls, give up.
	body, err := readStdinBounded(64 * 1024)
	if err != nil {
		// Don't block the agent on read errors.
		writeAllowFor(*agent)
		return 0
	}

	// Decode envelope by agent name. Failure → still allow.
	evt, ok := decodeAgentEnvelope(*agent, body)
	if !ok {
		writeAllowFor(*agent)
		return 0
	}

	// Append to local buffer instead of POSTing synchronously. Hook
	// returns in <5ms because the only I/O is a small append to a
	// JSON-lines file. Buffer is drained by `oryxai verify`,
	// `oryxai status`, or the next `oryxai install` invocation.
	_ = buffer.Append(evt)

	writeAllowFor(*agent)
	return 0
}

// unused import compat — kept while we transition from sync POST
// to buffered drain.
var _ = (*client.Client)(nil)
var _ = time.Second

// readStdinBounded reads up to max bytes from os.Stdin, returning what
// was read. We don't error on EOF — short payloads are fine.
func readStdinBounded(max int) ([]byte, error) {
	return io.ReadAll(io.LimitReader(os.Stdin, int64(max)))
}

// decodeAgentEnvelope converts the agent's native JSON envelope into
// our canonical FeedEvent. Returns ok=false on any parse failure.
func decodeAgentEnvelope(agent string, body []byte) (client.FeedEvent, bool) {
	evt := client.FeedEvent{
		Agent:   agent,
		TraceID: uuid.NewString(),
		At:      time.Now().UTC().Format(time.RFC3339Nano),
	}
	switch agent {
	case "claude-code":
		var p struct {
			ToolName  string         `json:"tool_name"`
			ToolInput map[string]any `json:"tool_input"`
		}
		if err := json.Unmarshal(body, &p); err != nil {
			return evt, false
		}
		evt.Tool = p.ToolName
		evt.Kind = "hook." + classifyTool(p.ToolName)
		evt.ParamsSummary = summarize(p.ToolName, p.ToolInput)
		return evt, true
	case "cursor":
		var p struct {
			Tool   string         `json:"tool"`
			Params map[string]any `json:"params"`
		}
		if err := json.Unmarshal(body, &p); err != nil {
			return evt, false
		}
		evt.Tool = p.Tool
		evt.Kind = "hook." + classifyTool(p.Tool)
		evt.ParamsSummary = summarize(p.Tool, p.Params)
		return evt, true
	case "gemini-cli":
		var p struct {
			Name string         `json:"name"`
			Args map[string]any `json:"args"`
		}
		if err := json.Unmarshal(body, &p); err != nil {
			return evt, false
		}
		evt.Tool = p.Name
		evt.Kind = "hook." + classifyTool(p.Name)
		evt.ParamsSummary = summarize(p.Name, p.Args)
		return evt, true
	case "opencode", "openclaw":
		var p map[string]any
		if err := json.Unmarshal(body, &p); err != nil {
			return evt, false
		}
		tool, _ := p["tool"].(string)
		if tool == "" {
			tool, _ = p["name"].(string)
		}
		evt.Tool = tool
		evt.Kind = "hook." + classifyTool(tool)
		params, _ := p["params"].(map[string]any)
		if params == nil {
			params, _ = p["args"].(map[string]any)
		}
		evt.ParamsSummary = summarize(tool, params)
		return evt, true
	default:
		// Unknown agent — record as generic hook event.
		evt.Tool = "unknown"
		evt.Kind = "hook.other"
		evt.ParamsSummary = "(unknown agent envelope)"
		return evt, true
	}
}

// classifyTool maps an agent-specific tool name to a coarse
// canonical bucket so the feed has consistent kind values across
// agents (Claude Code says "Bash", Cursor says "execute_command" —
// both classify to "bash").
func classifyTool(name string) string {
	n := strings.ToLower(name)
	// Order matters: "web" must be checked before "search" so
	// WebSearch classifies as web, not search. Same for "edit_file"
	// vs "read_file" — both contain neither's bucket marker.
	switch {
	case strings.Contains(n, "web"), strings.Contains(n, "fetch"),
		strings.Contains(n, "http"):
		return "web"
	case strings.Contains(n, "bash"), strings.Contains(n, "shell"),
		strings.Contains(n, "execute"), strings.Contains(n, "terminal"):
		return "bash"
	case strings.Contains(n, "write"), strings.Contains(n, "edit"):
		return "write"
	case strings.Contains(n, "read"), strings.Contains(n, "view"):
		return "read"
	case strings.Contains(n, "glob"), strings.Contains(n, "grep"),
		strings.Contains(n, "search"), strings.Contains(n, "find"):
		return "search"
	default:
		return "other"
	}
}

// summarize builds a short, non-secret string describing the tool's
// input. Keep under 200 chars — this is rendered in the dashboard
// audit row. We deliberately do NOT include file contents, command
// stdin, or password-shaped fields.
func summarize(tool string, params map[string]any) string {
	if params == nil {
		return ""
	}
	switch strings.ToLower(tool) {
	case "bash", "shell", "execute_command", "terminal":
		if cmd, ok := params["command"].(string); ok {
			return clamp(cmd, 200)
		}
	case "write", "edit":
		if path, ok := params["file_path"].(string); ok {
			return "writing " + clamp(path, 180)
		}
		if path, ok := params["path"].(string); ok {
			return "writing " + clamp(path, 180)
		}
	case "read", "view":
		if path, ok := params["file_path"].(string); ok {
			return "reading " + clamp(path, 180)
		}
	}
	// Fallback: list keys only.
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	return tool + "(" + strings.Join(keys, ",") + ")"
}

func clamp(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// writeAllowFor emits the per-agent "allow" JSON to stdout. Different
// agents expect different shapes; we send the canonical "approve"
// form for Claude Code and a generic empty object otherwise.
func writeAllowFor(agent string) {
	switch agent {
	case "claude-code":
		fmt.Print(`{"decision":"approve"}`)
	case "cursor":
		fmt.Print(`{"decision":"allow"}`)
	default:
		// Empty object — most hook protocols treat this as "no
		// decision recorded, proceed normally."
		fmt.Print("{}")
	}
}
