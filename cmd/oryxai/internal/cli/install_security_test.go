package cli

import (
	"strings"
	"testing"
)

func TestSlugRe_AcceptsGoodSlugs(t *testing.T) {
	good := []string{
		"abc",
		"my-team",
		"user-becceb",
		"org-2025",
		"a-b",
		"123",
		strings.Repeat("a", 48),
	}
	for _, s := range good {
		if !slugRe.MatchString(s) {
			t.Errorf("slugRe rejected legitimate slug %q", s)
		}
	}
}

func TestSlugRe_RejectsMalicious(t *testing.T) {
	bad := []string{
		"",
		"a",
		"ab",
		"-leading",
		"trailing-",
		"a/b",
		"a..b",
		"../../etc/passwd",
		"a?b",
		"a b",
		"UPPER",
		strings.Repeat("a", 49),
		"a\nb",
		"a\x1b[31m",
		"a%20b",
	}
	for _, s := range bad {
		if slugRe.MatchString(s) {
			t.Errorf("slugRe accepted bad slug %q", s)
		}
	}
}

func TestSanitizeForTerminal_StripsControl(t *testing.T) {
	cases := map[string]string{
		"plain":              "plain",
		"with\x1b[31mansi":   "with[31mansi",
		"new\nline":          "newline",
		"tab\there":          "tabhere",
		"del\x7fchar":        "delchar",
		"keep ünıcödé":       "keep ünıcödé",
	}
	for in, want := range cases {
		if got := sanitizeForTerminal(in); got != want {
			t.Errorf("sanitizeForTerminal(%q) = %q, want %q", in, got, want)
		}
	}
}
