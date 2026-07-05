// Package run abstracts where commands execute: directly (when the tool runs
// inside the resticprofile container, e.g. from cron) or through docker exec
// into the container (when it runs on the host). Everything above this layer
// is oblivious to the difference, which also makes it trivially fakeable in
// tests.
package run

import (
	"fmt"
	"os/exec"
	"strings"
)

// Runner executes a shell command line and returns its combined output.
type Runner interface {
	// Shell runs the command line via `sh -c` and returns combined
	// stdout+stderr. A non-nil error indicates a nonzero exit; output is
	// still returned.
	Shell(cmdline string) (string, error)
	// Where describes the execution context for error messages.
	Where() string
}

// Direct runs commands in the current environment (inside the container).
type Direct struct{}

func (Direct) Shell(cmdline string) (string, error) {
	out, err := exec.Command("sh", "-c", cmdline).CombinedOutput()
	return string(out), err
}

func (Direct) Where() string { return "locally" }

// Docker runs commands inside a container via docker exec.
type Docker struct{ Container string }

func (d Docker) Shell(cmdline string) (string, error) {
	out, err := exec.Command("docker", "exec", d.Container, "sh", "-c", cmdline).CombinedOutput()
	return string(out), err
}

func (d Docker) Where() string { return "in container " + d.Container }

// Detect picks the execution context: direct if resticprofile is on PATH
// (we're inside the container), otherwise docker exec into container —
// explicit name, or a container literally named fallbackName, or a unique
// running container whose image mentions "resticprofile".
func Detect(explicit, fallbackName string) (Runner, error) {
	if _, err := exec.LookPath("resticprofile"); err == nil {
		return Direct{}, nil
	}
	if explicit != "" {
		return Docker{Container: explicit}, nil
	}
	if exec.Command("docker", "inspect", fallbackName).Run() == nil {
		return Docker{Container: fallbackName}, nil
	}
	out, err := exec.Command("docker", "ps", "--format", "{{.Names}} {{.Image}}").Output()
	if err != nil {
		return nil, fmt.Errorf("docker ps: %w", err)
	}
	var found []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.Contains(fields[1], "resticprofile") {
			found = append(found, fields[0])
		}
	}
	switch len(found) {
	case 1:
		return Docker{Container: found[0]}, nil
	case 0:
		return nil, fmt.Errorf("no resticprofile container found — set RESTIC_CONTAINER or --container")
	default:
		return nil, fmt.Errorf("multiple resticprofile containers found (%s) — set RESTIC_CONTAINER or --container", strings.Join(found, ", "))
	}
}
