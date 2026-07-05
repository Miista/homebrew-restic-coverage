// Package match implements the glob semantics of the coverage audit: plain
// shell-glob patterns (as in `sh case`, where * crosses path separators)
// matched against full host paths, additionally tried anchored at any path
// depth so `foo/data/*` matches `/srv/x/foo/data/y`.
package match

import (
	"regexp"
	"strings"
	"sync"
)

var (
	mu    sync.Mutex
	cache = map[string]*regexp.Regexp{}
)

// compile translates a shell glob into an anchored regexp. Unlike
// path.Match, * and ? cross "/" — this mirrors `case` in POSIX sh, which the
// original engine used, and keeps existing coverage-ignore files working.
func compile(glob string) *regexp.Regexp {
	mu.Lock()
	defer mu.Unlock()
	if re, ok := cache[glob]; ok {
		return re
	}
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(glob); i++ {
		switch c := glob[i]; c {
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteString(".")
		case '[':
			// pass character classes through, translating a leading ! to ^
			j := i + 1
			if j < len(glob) && (glob[j] == '!' || glob[j] == '^') {
				j++
			}
			if j < len(glob) && glob[j] == ']' { // literal ] as first member
				j++
			}
			for j < len(glob) && glob[j] != ']' {
				j++
			}
			if j >= len(glob) { // unterminated class -> literal [
				b.WriteString(regexp.QuoteMeta("["))
				continue
			}
			class := glob[i : j+1]
			class = strings.Replace(class, "[!", "[^", 1)
			b.WriteString(class)
			i = j
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	re := regexp.MustCompile(b.String())
	cache[glob] = re
	return re
}

// Glob reports whether path matches the shell glob exactly.
func Glob(path, glob string) bool {
	return compile(glob).MatchString(path)
}

// Path reports whether path matches the pattern under audit semantics: as
// written, anchored at any depth, or as a prefix of a deeper path. This is
// the Go port of the original engine's
//
//	case "$f" in $p|*/$p|*/$p/*|$p/*)
func Path(path, pattern string) bool {
	return Glob(path, pattern) ||
		Glob(path, "*/"+pattern) ||
		Glob(path, "*/"+pattern+"/*") ||
		Glob(path, pattern+"/*")
}

// Any reports whether path matches any of the patterns.
func Any(path string, patterns []string) bool {
	for _, p := range patterns {
		if Path(path, p) {
			return true
		}
	}
	return false
}
