// Package coverage is the audit core: everything data-like on disk must be
// included by restic, matched by a profile exclude, or matched by a
// documented ignore pattern. What remains is new, undecided data.
package coverage

import (
	"restic-coverage/internal/match"
)

// Report is the outcome of one audit.
type Report struct {
	Violations []string // undecided paths (sorted, as ondisk was sorted)
	Checked    int      // data-like files considered
	Skipped    int      // unreadable entries the scan could not look into
}

// OK reports whether everything on disk is accounted for.
func (r Report) OK() bool { return len(r.Violations) == 0 }

// Audit diffs the on-disk data files against the included set, then filters
// the remainder through the accounted-for patterns (profile excludes +
// ignore file).
func Audit(included, ondisk, patterns []string) Report {
	inc := make(map[string]struct{}, len(included))
	for _, p := range included {
		inc[p] = struct{}{}
	}
	rep := Report{Checked: len(ondisk)}
	for _, f := range ondisk {
		if _, ok := inc[f]; ok {
			continue
		}
		if match.Any(f, patterns) {
			continue
		}
		rep.Violations = append(rep.Violations, f)
	}
	return rep
}
