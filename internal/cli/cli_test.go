package cli

import (
	"strings"
	"testing"
)

func TestVersionCommand(t *testing.T) {
	var out strings.Builder
	if code := Run([]string{"version"}, &out); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(out.String(), "restic-coverage dev") {
		t.Errorf("output %q", out.String())
	}
}

func TestHelp(t *testing.T) {
	for _, args := range [][]string{{"help"}, {"-h"}} {
		var out strings.Builder
		if code := Run(args, &out); code != 0 {
			t.Fatalf("%v: exit %d", args, code)
		}
		if !strings.Contains(out.String(), "ignore PATTERN REASON") {
			t.Errorf("%v: usage missing from %q", args, out.String())
		}
	}
}

func TestUnknownCommand(t *testing.T) {
	var out strings.Builder
	if code := Run([]string{"bogus"}, &out); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if !strings.Contains(out.String(), "unknown command: bogus") {
		t.Errorf("output %q", out.String())
	}
}

func TestIgnoreArgValidation(t *testing.T) {
	cases := [][]string{
		{"ignore"},
		{"ignore", "pattern-only"},
		{"ignore", "", "reason"},
		{"ignore", "pattern", "  "},
		{"ignore", "pattern", "reason", "extra"},
	}
	for _, args := range cases {
		var out strings.Builder
		if code := Run(args, &out); code != 2 {
			t.Errorf("%v: exit %d, want 2", args, code)
		}
		if !strings.Contains(out.String(), "a reason is required") {
			t.Errorf("%v: output %q", args, out.String())
		}
	}
}

func TestShq(t *testing.T) {
	cases := map[string]string{
		"plain":        "'plain'",
		"with 'quote'": `'with '\''quote'\'''`,
		"multi\nline":  "'multi\nline'",
	}
	for in, want := range cases {
		if got := shq(in); got != want {
			t.Errorf("shq(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestEnvOr(t *testing.T) {
	t.Setenv("RC_TEST_KEY", "set")
	if envOr("RC_TEST_KEY", "def") != "set" {
		t.Error("env value should win")
	}
	if envOr("RC_TEST_MISSING", "def") != "def" {
		t.Error("default should apply")
	}
}
