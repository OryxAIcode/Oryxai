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
	"github.com/OryxAIcode/Oryxai/cmd/oryxai/internal/tokensave"
)

// Hook handles `oryxai hook --agent <name>`.
//
// Read a tool-call envelope from stdin, fire-and-forget event to the
// dashboard buffer, optionally rewrite the command to reduce token
// waste, and write the agent-specific allow/approve response.
//
// Hard constraint: hook must return within ~5 ms.
// Rewrite logic is pure in-memory (no I/O) so it never adds latency.
func Hook(args []string) int {
	fs := flagSet("hook")
	agent := fs.String("agent", "", "Agent envelope to parse: claude-code | cursor | gemini-cli | opencode | openclaw")
	if err := fs.Parse(args); err != nil {
		writeAllowFor("unknown", "")
		return 0
	}
	if strings.TrimSpace(*agent) == "" {
		writeAllowFor("unknown", "")
		return 0
	}

	body, err := readStdinBounded(64 * 1024)
	if err != nil {
		writeAllowFor(*agent, "")
		return 0
	}

	evt, ok := decodeAgentEnvelope(*agent, body)
	if !ok {
		writeAllowFor(*agent, "")
		return 0
	}

	_ = buffer.Append(evt)

	// Rewrite noisy commands before they run — reduces tool result size
	// in the next turn and cuts context token usage.
	rewrittenCmd := tryRewrite(*agent, body)

	writeAllowFor(*agent, rewrittenCmd)
	return 0
}

// tryRewrite extracts the bash command from the tool envelope, applies
// the rewrite rule table, and returns the modified command string.
// Returns "" when no rewrite is needed or the agent doesn't support
// tool_input override (only claude-code and cursor support it today).
func tryRewrite(agent string, body []byte) string {
	switch agent {
	case "claude-code":
		var p struct {
			ToolName  string         `json:"tool_name"`
			ToolInput map[string]any `json:"tool_input"`
		}
		if err := json.Unmarshal(body, &p); err != nil {
			return ""
		}
		if !tokensave.IsBashTool(p.ToolName) {
			return ""
		}
		cmd, _ := p.ToolInput["command"].(string)
		rewritten, changed := tokensave.Rewrite(cmd)
		if !changed {
			return ""
		}
		return rewritten

	case "cursor":
		var p struct {
			Tool   string         `json:"tool"`
			Params map[string]any `json:"params"`
		}
		if err := json.Unmarshal(body, &p); err != nil {
			return ""
		}
		if !tokensave.IsBashTool(p.Tool) {
			return ""
		}
		cmd, _ := p.Params["command"].(string)
		rewritten, changed := tokensave.Rewrite(cmd)
		if !changed {
			return ""
		}
		return rewritten

	case "gemini-cli":
		var p struct {
			Name string         `json:"name"`
			Args map[string]any `json:"args"`
		}
		if err := json.Unmarshal(body, &p); err != nil {
			return ""
		}
		if !tokensave.IsBashTool(p.Name) {
			return ""
		}
		cmd, _ := p.Args["command"].(string)
		rewritten, changed := tokensave.Rewrite(cmd)
		if !changed {
			return ""
		}
		return rewritten

	default:
		return ""
	}
}

// unused import compat — kept while we transition from sync POST
// to buffered drain.
var _ = (*client.Client)(nil)
var _ = time.Second

func readStdinBounded(max int) ([]byte, error) {
	return io.ReadAll(io.LimitReader(os.Stdin, int64(max)))
}

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
		evt.Tool = "unknown"
		evt.Kind = "hook.other"
		evt.ParamsSummary = "(unknown agent envelope)"
		return evt, true
	}
}

func classifyTool(name string) string {
	n := strings.ToLower(name)
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

// writeAllowFor emits the per-agent allow/approve JSON to stdout.
// When rewrittenCmd is non-empty and the agent supports tool_input
// override, the modified command is included so the agent runs
// the quieter version instead of the original.
func writeAllowFor(agent, rewrittenCmd string) {
	switch agent {
	case "claude-code":
		if rewrittenCmd != "" {
			data, _ := json.Marshal(map[string]any{
				"decision": "approve",
				"tool_input": map[string]any{
					"command": rewrittenCmd,
				},
			})
			os.Stdout.Write(data)
		} else {
			fmt.Print(`{"decision":"approve"}`)
		}
	case "cursor":
		if rewrittenCmd != "" {
			data, _ := json.Marshal(map[string]any{
				"decision": "allow",
				"params": map[string]any{
					"command": rewrittenCmd,
				},
			})
			os.Stdout.Write(data)
		} else {
			fmt.Print(`{"decision":"allow"}`)
		}
	default:
		fmt.Print("{}")
	}
}
