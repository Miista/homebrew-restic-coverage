// Package scan walks the audited tree collecting the files the audit must
// decide about. Two candidate sets exist: UntrackedFiles — everything not in
// the git index (complete: config is git's job, data is the backup's, the
// rest needs a decision) — and DataFiles, the heuristic fallback for trees
// not under git: anything under a */data/ directory plus loose database and
// credential files. Either way the scan only surfaces candidates; the
// coverage engine decides their fate.
package scan

import (
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"

	"restic-coverage/internal/match"
)

// prunedDirs are never descended into. .git is covered by its remote,
// node_modules is regenerable by definition; neither holds backupworthy
// state, and both are enormous.
var prunedDirs = map[string]bool{".git": true, "node_modules": true}

// looseGlobs mark data-like files outside */data/ dirs.
var looseGlobs = []string{"*.db", "*.sqlite*", "credentials*", "*.key", "*.pem"}

// DataFiles returns every data-like file under root, with the root prefix
// rewritten to hostRoot, sorted. Pass hostRoot == root for a host-side scan.
// Unreadable entries don't abort the walk, but they are counted: a scan that
// silently skips subtrees would report "clean" without having looked.
func DataFiles(fsys fs.FS, root, hostRoot string) (files []string, skipped int, _ error) {
	return walk(fsys, root, hostRoot, func(rel, full, base string) bool {
		return isDataLike(full, base)
	})
}

// UntrackedFiles returns every file under root that is not in tracked (a set
// of slash-separated paths relative to root — a git index), with the root
// prefix rewritten to hostRoot, sorted. This is the complete-partition
// candidate set: on a box where config lives in git and data in backups,
// whatever is in neither needs a decision — no guessing what data looks like.
func UntrackedFiles(fsys fs.FS, root, hostRoot string, tracked map[string]struct{}) (files []string, skipped int, _ error) {
	return walk(fsys, root, hostRoot, func(rel, full, base string) bool {
		_, ok := tracked[rel]
		return !ok
	})
}

// TrackedDataLike returns the tracked paths (relative, as in the git index)
// that look like data — candidates for the "should this really be in git?"
// advisory. Tracking a data or credential file makes the audit stop asking
// about it, so it deserves a heads-up rather than silence.
func TrackedDataLike(tracked map[string]struct{}) []string {
	var out []string
	for rel := range tracked {
		// leading slash so the /data/ rule sees a top-level data dir too
		if isDataLike("/"+rel, path.Base(rel)) {
			out = append(out, rel)
		}
	}
	sort.Strings(out)
	return out
}

// walk visits every regular file under root and keeps those the want
// predicate accepts, given (path relative to root, full path, basename).
func walk(fsys fs.FS, root, hostRoot string, want func(rel, full, base string) bool) (files []string, skipped int, _ error) {
	var out []string
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			skipped++
			return nil
		}
		if d.IsDir() {
			if prunedDirs[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		full := root + "/" + p
		if !want(p, full, d.Name()) {
			return nil
		}
		if hostRoot != root {
			full = hostRoot + strings.TrimPrefix(full, root)
		}
		out = append(out, full)
		return nil
	})
	sort.Strings(out)
	return out, skipped, err
}

func isDataLike(path, base string) bool {
	if strings.Contains(path, "/data/") {
		return true
	}
	for _, g := range looseGlobs {
		if match.Glob(base, g) {
			return true
		}
	}
	return false
}

// Root opens root as an fs.FS for DataFiles.
func Root(root string) fs.FS { return os.DirFS(root) }
