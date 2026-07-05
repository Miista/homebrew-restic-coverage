// Package scan walks the audited tree looking for files that plausibly hold
// data worth deciding about: anything under a */data/ directory, plus loose
// database and credential files. The scan is deliberately heuristic — its
// job is to surface candidates, the coverage engine decides their fate.
package scan

import (
	"io/fs"
	"os"
	"sort"
	"strings"

	"restic-coverage/internal/match"
)

// prunedDirs are never descended into.
var prunedDirs = map[string]bool{".git": true, "node_modules": true}

// looseGlobs mark data-like files outside */data/ dirs.
var looseGlobs = []string{"*.db", "*.sqlite*", "credentials*", "*.key", "*.pem"}

// DataFiles returns every data-like file under root, with the root prefix
// rewritten to hostRoot, sorted. Pass hostRoot == root for a host-side scan.
// Unreadable entries don't abort the walk, but they are counted: a scan that
// silently skips subtrees would report "clean" without having looked.
func DataFiles(fsys fs.FS, root, hostRoot string) (files []string, skipped int, _ error) {
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
		if !isDataLike(full, d.Name()) {
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
