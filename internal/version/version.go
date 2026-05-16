// Package version exposes build-time identification for every codesec binary.
//
// Values are filled by -ldflags at link time. Example invocation (see
// Makefile and .goreleaser.yaml):
//
//	go build -ldflags "
//	    -X github.com/OryxAIcode/Oryxai/internal/version.Version=v0.1.0
//	    -X github.com/OryxAIcode/Oryxai/internal/version.Commit=abc1234
//	    -X github.com/OryxAIcode/Oryxai/internal/version.BuildTime=2026-05-15T12:00:00Z
//	" ./cmd/codesec-mcp
//
// Unset values fall back to "dev" / "unknown" so a `go run` or `go build`
// without flags still produces something printable.
package version

import (
	"fmt"
	"io"
	"runtime"
)

// These are var (not const) so -ldflags -X can override them. Default
// values let `go run ./cmd/...` work without a release build.
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = "unknown"
)

// GoVersion is the toolchain that compiled the binary. Useful when a user
// reports a bug and we need to know if they hit a runtime issue specific
// to the Go version we shipped.
func GoVersion() string {
	return runtime.Version()
}

// Short returns a single-line identifier suitable for log lines and
// User-Agent headers.
func Short() string {
	return fmt.Sprintf("%s (%s)", Version, Commit)
}

// Print writes a multi-line --version block to w. Each cmd's main calls
// this from its --version flag handler so the format stays consistent.
func Print(w io.Writer, programName string) {
	fmt.Fprintf(w, "%s %s\n", programName, Version)
	fmt.Fprintf(w, "  commit:     %s\n", Commit)
	fmt.Fprintf(w, "  built:      %s\n", BuildTime)
	fmt.Fprintf(w, "  go:         %s\n", GoVersion())
	fmt.Fprintf(w, "  os/arch:    %s/%s\n", runtime.GOOS, runtime.GOARCH)
}
