package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"restic-coverage/internal/run"
)

// fakeRunner emulates the resticprofile container for end-to-end CLI tests.
type fakeRunner struct {
	hostRoot string
	included []string // paths reported by the dry-run
	shells   []string // every command line seen
}

func (f *fakeRunner) Shell(cmdline string) (string, error) {
	f.shells = append(f.shells, cmdline)
	switch {
	case strings.Contains(cmdline, "resticprofile --dry-run"):
		return fmt.Sprintf(
			"dry-run: /usr/bin/restic backup --exclude=*.log --repo=/backup %s/svc/data %s/tunnel/credentials.json\n",
			f.hostRoot, f.hostRoot), nil
	case strings.Contains(cmdline, "--verbose=2"):
		var b strings.Builder
		for _, p := range f.included {
			fmt.Fprintf(&b, "new       %s, saved in 0.001s (1 KiB added)\n", p)
		}
		return b.String(), nil
	case strings.Contains(cmdline, "hostname"):
		return "testbox\n", nil
	default:
		return "", nil
	}
}

func (f *fakeRunner) Where() string { return "in test" }

// setup builds a temp tree with one backed-up file, one excluded file, one
// ignored file, and optionally one violation. Returns options wired to it.
func setup(t *testing.T, withViolation bool) (Options, *fakeRunner) {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"svc/data/state.db":       "backed up",
		"svc/data/noise.log":      "profile-excluded",
		"legacy/data/old.db":      "ignored",
		"tunnel/credentials.json": "backed up",
	}
	if withViolation {
		files["newsvc/data/undecided.db"] = "violation"
	}
	for p, content := range files {
		full := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ignoreFile := filepath.Join(root, "coverage-ignore")
	if err := os.WriteFile(ignoreFile, []byte("*/legacy/data/* # superseded service\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := &fakeRunner{
		hostRoot: root,
		included: []string{root + "/svc/data/state.db", root + "/tunnel/credentials.json"},
	}
	detect = func(explicit, fallback string) (run.Runner, error) { return f, nil }
	t.Cleanup(func() { detect = run.Detect })
	return Options{Profile: "default", HostRoot: root, IgnoreFile: ignoreFile}, f
}

func optArgs(o Options, extra ...string) []string {
	args := []string{
		"--profile", o.Profile,
		"--host-root", o.HostRoot,
		"--ignore-file", o.IgnoreFile,
	}
	return append(args, extra...)
}

func TestCheckClean(t *testing.T) {
	o, _ := setup(t, false)
	var out strings.Builder
	if code := Run(optArgs(o, "check"), &out); code != 0 {
		t.Fatalf("exit %d: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "coverage OK") {
		t.Errorf("output %q", out.String())
	}
}

func TestCheckViolation(t *testing.T) {
	o, _ := setup(t, true)
	var out strings.Builder
	if code := Run(optArgs(o), &out); code != 1 {
		t.Fatalf("exit %d, want 1: %s", code, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "newsvc/data/undecided.db") {
		t.Errorf("violation missing from %q", got)
	}
	if strings.Contains(got, "noise.log") || strings.Contains(got, "legacy/data/old.db") {
		t.Errorf("excluded/ignored paths leaked into %q", got)
	}
}

func TestCheckNotify(t *testing.T) {
	o, f := setup(t, true)
	var out strings.Builder
	if code := Run(optArgs(o, "--notify", "check"), &out); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	joined := strings.Join(f.shells, "\n")
	for _, want := range []string{
		"notify-status.sh coverage false",
		"notify-failure.sh coverage",
		"coverage audit (profile default)", // alerts identify themselves
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("notify commands missing %q:\n%s", want, joined)
		}
	}
}

func TestCheckNotifySuccess(t *testing.T) {
	o, f := setup(t, false)
	var out strings.Builder
	if code := Run(optArgs(o, "--notify"), &out); code != 0 {
		t.Fatalf("exit %d", code)
	}
	joined := strings.Join(f.shells, "\n")
	if !strings.Contains(joined, "notify-status.sh coverage true") {
		t.Errorf("success heartbeat missing:\n%s", joined)
	}
	if strings.Contains(joined, "notify-failure") {
		t.Errorf("failure alert must not fire on success:\n%s", joined)
	}
}

func TestIgnoreRoundTrip(t *testing.T) {
	o, _ := setup(t, true)
	var out strings.Builder
	code := Run(optArgs(o, "ignore", "*/newsvc/data/*", "decided: expendable"), &out)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out.String())
	}
	got := out.String()
	for _, want := range []string{"added to", "coverage OK", "remember to commit"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q: %q", want, got)
		}
	}
	data, _ := os.ReadFile(o.IgnoreFile)
	if !strings.Contains(string(data), "*/newsvc/data/* # decided: expendable") {
		t.Errorf("ignore file content: %q", data)
	}
	// duplicate refused
	out.Reset()
	if code := Run(optArgs(o, "ignore", "*/newsvc/data/*", "again"), &out); code != 1 {
		t.Errorf("duplicate ignore: exit %d, want 1 (%s)", code, out.String())
	}
}

func TestDefaultIgnorePathFromContainerHostname(t *testing.T) {
	o, f := setup(t, false)
	o.IgnoreFile = "" // force derivation: <host-root>/<box>/restic/coverage-ignore
	dir := filepath.Join(o.HostRoot, "testbox", "restic")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "coverage-ignore"), []byte("*/legacy/data/* # superseded\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if code := Run([]string{"--profile", o.Profile, "--host-root", o.HostRoot, "check"}, &out); code != 0 {
		t.Fatalf("exit %d: %s", code, out.String())
	}
	joined := strings.Join(f.shells, "\n")
	if !strings.Contains(joined, "hostname") {
		t.Error("container hostname should be queried for the default ignore path")
	}
}
