package ignore

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPatterns(t *testing.T) {
	f := filepath.Join(t.TempDir(), "coverage-ignore")
	content := `# header comment

*/pi/gatus/data/*   # history, expendable
*/timemachine/*# no space before comment
   */padded/*    # whitespace trimmed
`
	if err := os.WriteFile(f, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Patterns(f)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"*/pi/gatus/data/*", "*/timemachine/*", "*/padded/*"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Patterns = %v, want %v", got, want)
	}
}

func TestPatternsMissingFile(t *testing.T) {
	got, err := Patterns(filepath.Join(t.TempDir(), "nope"))
	if err != nil || got != nil {
		t.Errorf("missing file: got %v, %v; want nil, nil", got, err)
	}
}

func TestAppendAndContains(t *testing.T) {
	f := filepath.Join(t.TempDir(), "coverage-ignore")
	if err := Append(f, "*/x/data/*", "because reasons"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(f)
	if want := "*/x/data/* # because reasons\n"; string(data) != want {
		t.Errorf("file = %q, want %q", data, want)
	}
	ok, err := Contains(f, "*/x/data/*")
	if err != nil || !ok {
		t.Errorf("Contains = %v, %v; want true, nil", ok, err)
	}
	// duplicate refused
	if err := Append(f, "*/x/data/*", "again"); err == nil {
		t.Error("duplicate Append should fail")
	}
	// substring of a reason is not a pattern match
	ok, _ = Contains(f, "because")
	if ok {
		t.Error("Contains must not match inside reasons")
	}
}

func TestAppendValidation(t *testing.T) {
	f := filepath.Join(t.TempDir(), "coverage-ignore")
	for _, c := range []struct{ pat, reason string }{
		{"", "reason"}, {"pat", ""}, {"  ", "reason"}, {"pat", "   "},
	} {
		if err := Append(f, c.pat, c.reason); err == nil || !strings.Contains(err.Error(), "required") {
			t.Errorf("Append(%q, %q) should fail validation", c.pat, c.reason)
		}
	}
}
