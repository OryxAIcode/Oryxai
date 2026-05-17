package tokensave

import "strings"

// rewriteRule appends flags to a command when the command starts with prefix.
type rewriteRule struct {
	prefix string
	append string
}

// rules is ordered: first match wins.
var rules = []rewriteRule{
	// Node / JS
	{"npm install", " --no-progress --loglevel=error"},
	{"npm ci", " --no-progress --loglevel=error"},
	{"npm run", " --silent"},
	{"npm test", " --silent"},
	{"npm build", " --silent"},
	{"yarn install", " --silent"},
	{"yarn add", " --silent"},
	{"yarn run", " --silent"},
	{"pnpm install", " --reporter=silent"},
	{"pnpm add", " --reporter=silent"},
	{"pnpm run", " --reporter=silent"},
	// Python
	{"pip install", " -q"},
	{"pip3 install", " -q"},
	{"poetry install", " -q"},
	{"poetry add", " -q"},
	{"pytest", " -q --tb=short"},
	{"python -m pytest", " -q --tb=short"},
	// Rust
	{"cargo build", " --message-format=short"},
	{"cargo test", " --message-format=short -q"},
	{"cargo install", " --message-format=short"},
	{"cargo check", " --message-format=short"},
	// Go
	{"go test -v", " 2>&1 | tail -30"},
	{"go build", ""},
	// Java / JVM
	{"mvn", " -q"},
	{"./mvnw", " -q"},
	{"gradle", " -q"},
	{"./gradlew", " -q"},
	// .NET
	{"dotnet restore", " --verbosity quiet"},
	{"dotnet build", " --verbosity quiet"},
	{"dotnet test", " --verbosity quiet"},
	// JS test runners
	{"jest", " --silent"},
	{"vitest", " --silent"},
	{"mocha", " --reporter min"},
	// Git (verbose output commands only)
	{"git log", " --oneline -20"},
	{"git diff --stat", ""},
	// Docker
	{"docker build", " --progress=plain 2>&1 | tail -20"},
	{"docker pull", " -q"},
	// Kubernetes
	{"kubectl get", " --no-headers"},
	{"kubectl describe", " 2>&1 | head -60"},
	// Terraform
	{"terraform init", " -input=false"},
	{"terraform plan", " -compact-warnings"},
	{"terraform apply", " -compact-warnings"},
	// System
	{"find ", " 2>/dev/null | head -50"},
	{"ls -la", " | head -40"},
	{"ls -l", " | head -40"},
	{"ps aux", " | head -30"},
	{"netstat", " | head -30"},
	{"env", " | sort | head -50"},
	{"printenv", " | sort | head -50"},
	// Cloud
	{"aws s3 ls", " | head -50"},
}

// bashToolNames are the tool names that carry a shell command in their input.
var bashToolNames = map[string]bool{
	"bash": true, "shell": true, "run_shell_command": true,
	"exec": true, "terminal": true, "execute_command": true,
	"computer": true,
}

// IsBashTool returns true when the tool name represents a shell execution tool.
func IsBashTool(toolName string) bool {
	return bashToolNames[strings.ToLower(toolName)]
}

// Rewrite returns a modified command with noise-reducing flags appended.
// changed is false when no rule matched or the command is empty.
func Rewrite(command string) (rewritten string, changed bool) {
	if command == "" {
		return command, false
	}
	trimmed := strings.TrimSpace(command)
	for _, r := range rules {
		if strings.HasPrefix(trimmed, r.prefix) {
			if r.append == "" {
				return command, false
			}
			if strings.Contains(trimmed, strings.TrimSpace(r.append)) {
				return command, false
			}
			return trimmed + r.append, true
		}
	}
	return command, false
}
