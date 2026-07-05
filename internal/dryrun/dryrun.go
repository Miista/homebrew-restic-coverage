// Package dryrun extracts the source of truth for the audit from restic
// itself: the exact backup command resticprofile would run, the files that
// command would include, and the exclude patterns it carries. Nothing here
// reimplements restic's glob logic — we only parse restic's own answers.
package dryrun

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"restic-coverage/internal/run"
)

var (
	cmdRe      = regexp.MustCompile(`dry-run: (.*restic backup .*)$`)
	includedRe = regexp.MustCompile(`^(?:new|changed|modified|unchanged) +(.*?)(?:, saved in .*)?$`)
)

// Command asks resticprofile for the exact restic backup command of the
// given profile. Hooks (run-before etc.) are displayed but not executed.
func Command(r run.Runner, profile string) (string, error) {
	out, _ := r.Shell(fmt.Sprintf("resticprofile --dry-run --no-ansi -n %q backup 2>&1", profile))
	for _, line := range strings.Split(out, "\n") {
		if m := cmdRe.FindStringSubmatch(line); m != nil {
			return m[1], nil
		}
	}
	return "", fmt.Errorf("could not derive the restic backup command for profile %q (%s):\n%s",
		profile, r.Where(), strings.TrimSpace(out))
}

// Excludes returns the --exclude patterns of a backup command line, with
// shell quoting stripped.
func Excludes(cmdline string) []string {
	var pats []string
	for _, tok := range strings.Fields(cmdline) {
		if v, ok := strings.CutPrefix(tok, "--exclude="); ok {
			pats = append(pats, strings.Trim(v, `"'`))
		}
	}
	return pats
}

// Sources returns the positional (non-flag) arguments of a backup command
// line — the backup sources.
func Sources(cmdline string) []string {
	fields := strings.Fields(cmdline)
	var srcs []string
	for i, tok := range fields {
		if i < 2 { // "restic backup" (possibly with a leading path)
			continue
		}
		if strings.HasPrefix(tok, "-") {
			continue
		}
		srcs = append(srcs, strings.Trim(tok, `"'`))
	}
	return srcs
}

// HostRoot derives the host-side root of the audited tree from the backup
// sources: the shortest .../docker prefix among them. Returns "" when no
// source matches, in which case the caller must be told explicitly.
func HostRoot(cmdline string) string {
	re := regexp.MustCompile(`^(/home/[^/]+/docker)/`)
	for _, s := range Sources(cmdline) {
		if m := re.FindStringSubmatch(s); m != nil {
			return m[1]
		}
	}
	return ""
}

// Included runs the backup command with --dry-run -vv and returns every path
// restic would include, normalized: the scanRoot prefix (the in-container
// mount of the audited tree) is rewritten to hostRoot, and directory entries
// lose their trailing slash. The result is sorted and de-duplicated.
func Included(r run.Runner, cmdline, scanRoot, hostRoot string) ([]string, error) {
	out, _ := r.Shell(cmdline + " --dry-run --verbose=2 2>/dev/null")
	set := map[string]struct{}{}
	for _, line := range strings.Split(out, "\n") {
		m := includedRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		p := strings.TrimSuffix(m[1], "/")
		if scanRoot != "" && scanRoot != hostRoot {
			if rest, ok := strings.CutPrefix(p, scanRoot); ok {
				p = hostRoot + rest
			}
		}
		set[p] = struct{}{}
	}
	if len(set) == 0 {
		return nil, fmt.Errorf("restic dry-run produced no file list (%s)", r.Where())
	}
	paths := make([]string, 0, len(set))
	for p := range set {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths, nil
}
