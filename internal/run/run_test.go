package run

import (
	"os"
	"strings"
	"testing"
)

func TestDirectShell(t *testing.T) {
	out, err := Direct{}.Shell("printf hello")
	if err != nil || out != "hello" {
		t.Errorf("Shell = %q, %v", out, err)
	}
	if _, err := (Direct{}).Shell("exit 3"); err == nil {
		t.Error("nonzero exit must return an error")
	}
	if (Direct{}).Where() != "locally" {
		t.Error("Where")
	}
}

func TestDockerWhere(t *testing.T) {
	d := Docker{Container: "restic"}
	if d.Where() != "in container restic" {
		t.Errorf("Where = %q", d.Where())
	}
}

func TestDetectExplicit(t *testing.T) {
	if _, err := os.Stat("/usr/bin/resticprofile"); err == nil {
		t.Skip("resticprofile installed; direct mode wins")
	}
	r, err := Detect("mycontainer", "restic")
	if err != nil {
		t.Fatal(err)
	}
	d, ok := r.(Docker)
	if !ok || d.Container != "mycontainer" {
		t.Errorf("Detect explicit = %#v", r)
	}
}

func TestDetectDirect(t *testing.T) {
	// fake a resticprofile on PATH
	dir := t.TempDir()
	fake := dir + "/resticprofile"
	if err := os.WriteFile(fake, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	r, err := Detect("", "restic")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := r.(Direct); !ok {
		t.Errorf("Detect = %#v, want Direct", r)
	}
	if !strings.Contains(r.Where(), "local") {
		t.Errorf("Where = %q", r.Where())
	}
}
