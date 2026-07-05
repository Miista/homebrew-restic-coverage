// Package ignore reads and appends the coverage-ignore file: one glob per
// line, everything after # is the (mandatory, human) reason. The file is the
// documented record of deliberate omissions — patterns that overlap a parent
// of a backed-up path live here rather than as restic excludes, because
// restic excludes are live and have no negation.
package ignore

import (
	"fmt"
	"os"
	"strings"
)

// Patterns returns the glob patterns of an ignore file, comments and blank
// lines stripped. A missing file is not an error — it means no exceptions.
func Patterns(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var pats []string
	for _, line := range strings.Split(string(data), "\n") {
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if line != "" {
			pats = append(pats, line)
		}
	}
	return pats, nil
}

// Contains reports whether the file already carries the pattern (as a
// pattern, not as a substring of a reason).
func Contains(path, pattern string) (bool, error) {
	pats, err := Patterns(path)
	if err != nil {
		return false, err
	}
	for _, p := range pats {
		if p == pattern {
			return true, nil
		}
	}
	return false, nil
}

// Append adds a documented exception. The reason is mandatory: the whole
// value of the file is that every omission explains itself.
func Append(path, pattern, reason string) error {
	if strings.TrimSpace(pattern) == "" || strings.TrimSpace(reason) == "" {
		return fmt.Errorf("both a pattern and a reason are required")
	}
	if ok, err := Contains(path, pattern); err != nil {
		return err
	} else if ok {
		return fmt.Errorf("pattern already present in %s", path)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "%s # %s\n", pattern, reason)
	return err
}
