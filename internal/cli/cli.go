// Package cli parses commands and wires the audit together: derive the
// backup command, collect restic's included set, scan the tree, diff, and
// report. It is the only package that talks to the outside world (flags,
// stdout, notify hooks).
package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"restic-coverage/internal/coverage"
	"restic-coverage/internal/dryrun"
	"restic-coverage/internal/ignore"
	"restic-coverage/internal/run"
	"restic-coverage/internal/scan"
)

// Version is the build version, overridden at release time via
// -ldflags "-X restic-coverage/internal/cli.Version=...".
var Version = "dev"

// Options collects everything configurable, resolved from flags with
// environment fallbacks.
type Options struct {
	Container  string // resticprofile container (empty = autodetect)
	Profile    string // resticprofile profile holding the backup section
	ScanRoot   string // in-container mount of the audited tree
	HostRoot   string // host path ScanRoot corresponds to (empty = derive)
	IgnoreFile string // coverage-ignore path (empty = derive)
	Notify     bool   // push results to the notify hooks (cron mode)
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// Run is the entry point: Run(os.Args[1:], os.Stdout).
func Run(args []string, out io.Writer) int {
	fs := flag.NewFlagSet("restic-coverage", flag.ContinueOnError)
	fs.SetOutput(out)
	var o Options
	fs.StringVar(&o.Container, "container", envOr("RESTIC_CONTAINER", ""), "resticprofile container name (default: autodetect)")
	fs.StringVar(&o.Profile, "profile", envOr("COVERAGE_PROFILE", "default"), "resticprofile profile with the backup section")
	fs.StringVar(&o.ScanRoot, "scan-root", envOr("COVERAGE_SCAN_ROOT", "/hostenv"), "in-container mount of the audited tree")
	fs.StringVar(&o.HostRoot, "host-root", envOr("COVERAGE_HOST_ROOT", ""), "host path the scan root corresponds to (default: derived from sources)")
	fs.StringVar(&o.IgnoreFile, "ignore-file", envOr("COVERAGE_IGNORE_FILE", ""), "coverage-ignore file (default: <host-root>/<box>/restic/coverage-ignore)")
	fs.BoolVar(&o.Notify, "notify", os.Getenv("COVERAGE_NOTIFY") == "on", "push result to the notify hooks (scheduled runs)")
	fs.Usage = func() { usage(out) }
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}

	cmd := fs.Arg(0)
	rest := fs.Args()
	if len(rest) > 0 {
		rest = rest[1:]
	}

	switch cmd {
	case "", "check":
		return check(o, out)
	case "ignore":
		return ignoreCmd(o, rest, out)
	case "version":
		fmt.Fprintf(out, "restic-coverage %s\n", Version)
		return 0
	case "help":
		usage(out)
		return 0
	default:
		fmt.Fprintf(out, "unknown command: %s\n\n", cmd)
		usage(out)
		return 2
	}
}

func usage(out io.Writer) {
	fmt.Fprint(out, `restic-coverage — audit that everything on disk is deliberately backed up,
excluded, or ignored (with a reason) by a resticprofile setup.

Usage:
  restic-coverage [flags] [check]                 run the audit
  restic-coverage [flags] ignore PATTERN REASON   add a documented exception,
                                                  then re-run the audit
  restic-coverage version

Flags (env fallback in parentheses):
  --container   (RESTIC_CONTAINER)      resticprofile container; default:
                                        "restic", else autodetect by image
  --profile     (COVERAGE_PROFILE)      profile with the backup section [default]
  --scan-root   (COVERAGE_SCAN_ROOT)    in-container mount of the tree [/hostenv]
  --host-root   (COVERAGE_HOST_ROOT)    host path of the tree [derived]
  --ignore-file (COVERAGE_IGNORE_FILE)  exception file
                                        [<host-root>/<box>/restic/coverage-ignore]
  --notify      (COVERAGE_NOTIFY=on)    push result to notify hooks (cron mode)

Runs directly when resticprofile is on PATH (inside the container), otherwise
through docker exec.
`)
}

// setup resolves the runner and everything derived from the backup command.
type audit struct {
	runner   run.Runner
	cmdline  string
	hostRoot string
	scanRoot string // where to walk; equals hostRoot in host mode
	inside   bool   // running inside the container
}

// detect is swappable in tests.
var detect = run.Detect

func prepare(o Options) (*audit, error) {
	r, err := detect(o.Container, "restic")
	if err != nil {
		return nil, err
	}
	_, inside := r.(run.Direct)
	cmdline, err := dryrun.Command(r, o.Profile)
	if err != nil {
		return nil, err
	}
	hostRoot := o.HostRoot
	if hostRoot == "" {
		hostRoot = dryrun.HostRoot(cmdline)
	}
	if hostRoot == "" {
		return nil, fmt.Errorf("could not derive the host root from the backup sources; set --host-root")
	}
	scanRoot := hostRoot
	if inside {
		scanRoot = o.ScanRoot
	}
	return &audit{runner: r, cmdline: cmdline, hostRoot: hostRoot, scanRoot: scanRoot, inside: inside}, nil
}

func (a *audit) ignorePath(o Options) (string, error) {
	if o.IgnoreFile != "" {
		return o.IgnoreFile, nil
	}
	if a.inside {
		return "/resticprofile/coverage-ignore", nil
	}
	box, err := a.runner.Shell("hostname")
	if err != nil {
		return "", fmt.Errorf("resolving container hostname: %w", err)
	}
	return filepath.Join(a.hostRoot, strings.TrimSpace(box), "restic", "coverage-ignore"), nil
}

func check(o Options, out io.Writer) int {
	a, err := prepare(o)
	if err != nil {
		return fail(o, nil, out, err.Error())
	}
	rep, ignoreFile, err := a.audit(o)
	if err != nil {
		return fail(o, a, out, err.Error())
	}
	if !rep.OK() {
		msg := fmt.Sprintf("%d path(s) on disk are neither backed up, profile-excluded, nor in %s:\n%s\n-> add to backup sources in profiles.yaml, or run: restic-coverage ignore PATTERN REASON",
			len(rep.Violations), ignoreFile, strings.Join(rep.Violations, "\n"))
		return fail(o, a, out, msg)
	}
	fmt.Fprintf(out, "coverage OK — %d data path(s) all backed up, excluded, or ignored with a reason\n", rep.Checked)
	if rep.Skipped > 0 {
		fmt.Fprintf(out, "warning: %d unreadable path(s) skipped — run inside the container (or as root) for a complete audit\n", rep.Skipped)
	}
	if o.Notify {
		notify(a.runner, o.Profile, true, "")
	}
	return 0
}

func (a *audit) audit(o Options) (coverage.Report, string, error) {
	included, err := dryrun.Included(a.runner, a.cmdline, o.ScanRoot, a.hostRoot)
	if err != nil {
		return coverage.Report{}, "", err
	}
	ignoreFile, err := a.ignorePath(o)
	if err != nil {
		return coverage.Report{}, "", err
	}
	ignores, err := a.ignorePatterns(ignoreFile)
	if err != nil {
		return coverage.Report{}, ignoreFile, err
	}
	patterns := append(dryrun.Excludes(a.cmdline), ignores...)
	ondisk, skipped, err := scan.DataFiles(scan.Root(a.scanRoot), a.scanRoot, a.hostRoot)
	if err != nil {
		return coverage.Report{}, ignoreFile, fmt.Errorf("scanning %s: %w", a.scanRoot, err)
	}
	rep := coverage.Audit(included, ondisk, patterns)
	rep.Skipped = skipped
	return rep, ignoreFile, nil
}

// ignorePatterns reads the ignore file where it lives: on the local
// filesystem, in both direct mode and host mode (in host mode the derived
// path is a host path).
func (a *audit) ignorePatterns(path string) ([]string, error) {
	return ignore.Patterns(path)
}

func ignoreCmd(o Options, args []string, out io.Writer) int {
	if len(args) != 2 || strings.TrimSpace(args[0]) == "" || strings.TrimSpace(args[1]) == "" {
		fmt.Fprintln(out, "usage: restic-coverage ignore PATTERN REASON")
		fmt.Fprintln(out, "a reason is required — the ignore file documents every omission")
		return 2
	}
	a, err := prepare(o)
	if err != nil {
		fmt.Fprintln(out, err)
		return 1
	}
	path, err := a.ignorePath(o)
	if err != nil {
		fmt.Fprintln(out, err)
		return 1
	}
	if err := ignore.Append(path, args[0], args[1]); err != nil {
		fmt.Fprintln(out, err)
		return 1
	}
	fmt.Fprintf(out, "added to %s — re-running the audit:\n", path)
	code := check(Options{ // interactive re-run: never notify
		Container: o.Container, Profile: o.Profile, ScanRoot: o.ScanRoot,
		HostRoot: o.HostRoot, IgnoreFile: o.IgnoreFile,
	}, out)
	fmt.Fprintln(out, "remember to commit + push (and pull on the other box)")
	return code
}

// fail prints the message and, in notify mode, pushes the failure.
func fail(o Options, a *audit, out io.Writer, msg string) int {
	fmt.Fprintln(out, msg)
	if o.Notify {
		var r run.Runner = run.Direct{}
		if a != nil {
			r = a.runner
		}
		notify(r, o.Profile, false, msg)
	}
	return 1
}

// notify calls the optional alerting hooks (heartbeat + failure alert) if
// they exist where commands run. The identity string names the check and the
// profile so alerts are attributable at a glance. Message size is capped:
// alert transports reject huge bodies.
func notify(r run.Runner, profile string, ok bool, msg string) {
	const maxLines = 40
	if lines := strings.Split(msg, "\n"); len(lines) > maxLines {
		msg = strings.Join(lines[:maxLines], "\n") + "\n…"
	}
	identity := fmt.Sprintf("coverage audit (profile %s)", profile)
	status := "false"
	if ok {
		status = "true"
	}
	cmd := fmt.Sprintf("[ -x /shared/notify-status.sh ] && ERROR_MESSAGE=%s PROFILE_NAME=%s sh /shared/notify-status.sh coverage %s; :",
		shq(msg), shq(identity), status)
	_, _ = r.Shell(cmd)
	if !ok {
		// A coverage finding is not a failed backup — title it as what it is.
		cmd = fmt.Sprintf(`[ -x /shared/notify-failure.sh ] && NTFY_TITLE="Backup coverage: paths not covered on $(hostname)" ERROR_MESSAGE=%s PROFILE_NAME=%s sh /shared/notify-failure.sh coverage; :`,
			shq(msg), shq(identity))
		_, _ = r.Shell(cmd)
	}
}

// shq single-quotes s for POSIX sh.
func shq(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
