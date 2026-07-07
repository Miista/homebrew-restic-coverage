package gitindex

import (
	"bytes"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// ── against real git ────────────────────────────────────────────────

// gitRepo initializes a repo in a temp dir and returns its root plus a
// helper running git in it. Tests using it skip when git is unavailable.
func gitRepo(t *testing.T, initArgs ...string) (string, func(args ...string) string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	root := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	git(append([]string{"init", "-q"}, initArgs...)...)
	return root, git
}

func write(t *testing.T, root string, paths ...string) {
	t.Helper()
	for _, p := range paths {
		full := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("content of "+p), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// lsFiles is the ground truth Tracked must reproduce.
func lsFiles(t *testing.T, git func(...string) string) []string {
	t.Helper()
	var out []string
	for _, p := range strings.Split(git("ls-files", "-z"), "\x00") {
		if p != "" {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

func trackedSorted(t *testing.T, root string) []string {
	t.Helper()
	set, err := Tracked(os.DirFS(root), ".git")
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func TestTrackedMatchesLsFiles(t *testing.T) {
	root, git := gitRepo(t)
	write(t, root,
		"compose.yaml",
		"svc/profiles.yaml",
		"svc/nested/deep/config.toml",
		"with space/a b.txt",
		"unicode/æøå.txt",
		"untracked.txt", // never added
	)
	git("add", "compose.yaml", "svc", "with space", "unicode")
	git("commit", "-q", "-m", "x")
	// a staged-but-uncommitted file is tracked too
	write(t, root, "staged.txt")
	git("add", "staged.txt")

	got, want := trackedSorted(t, root), lsFiles(t, git)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Tracked = %v\nls-files = %v", got, want)
	}
	for _, p := range got {
		if p == "untracked.txt" {
			t.Error("untracked file reported as tracked")
		}
	}
}

func TestTrackedManyFiles(t *testing.T) {
	// enough entries with varied path lengths to exercise padding math
	root, git := gitRepo(t)
	var paths []string
	for i := 0; i < 300; i++ {
		p := "dir" + strings.Repeat("x", i%17) + "/f" + strings.Repeat("y", i%23) + ".txt"
		paths = append(paths, p)
	}
	write(t, root, paths...)
	git("add", ".")
	got, want := trackedSorted(t, root), lsFiles(t, git)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Tracked diverges from ls-files: got %d, want %d entries", len(got), len(want))
	}
}

func TestTrackedV3ExtendedFlags(t *testing.T) {
	root, git := gitRepo(t)
	write(t, root, "a.txt", "b.txt")
	git("add", ".")
	git("commit", "-q", "-m", "x")
	git("update-index", "--skip-worktree", "a.txt") // forces v3 extended flags
	got, want := trackedSorted(t, root), lsFiles(t, git)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Tracked = %v, want %v", got, want)
	}
}

func TestTrackedConflictStages(t *testing.T) {
	root, git := gitRepo(t)
	write(t, root, "seed.txt")
	git("add", ".")
	git("commit", "-q", "-m", "x")
	// inject a 3-stage conflict for one path via --index-info
	empty := "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391" // hash of the empty blob
	cmd := exec.Command("git", "update-index", "--index-info")
	cmd.Dir = root
	cmd.Stdin = strings.NewReader(
		"100644 " + empty + " 1\tconflicted.txt\n" +
			"100644 " + empty + " 2\tconflicted.txt\n" +
			"100644 " + empty + " 3\tconflicted.txt\n")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("index-info: %v\n%s", err, out)
	}
	got := trackedSorted(t, root)
	want := []string{"conflicted.txt", "seed.txt"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Tracked = %v, want %v", got, want)
	}
}

func TestTrackedSHA256Repo(t *testing.T) {
	root, git := gitRepo(t, "--object-format=sha256")
	write(t, root, "a.txt", "sub/b.txt")
	git("add", ".")
	got, want := trackedSorted(t, root), lsFiles(t, git)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Tracked = %v, want %v", got, want)
	}
}

func TestTrackedRejectsV4(t *testing.T) {
	root, git := gitRepo(t)
	write(t, root, "a.txt")
	git("add", ".")
	git("update-index", "--index-version", "4")
	if _, err := Tracked(os.DirFS(root), ".git"); err == nil || !strings.Contains(err.Error(), "version 4") {
		t.Errorf("v4 index must be rejected, got %v", err)
	}
}

func TestTrackedRejectsSplitIndex(t *testing.T) {
	root, git := gitRepo(t)
	write(t, root, "a.txt")
	git("add", ".")
	git("update-index", "--split-index")
	if _, err := Tracked(os.DirFS(root), ".git"); err == nil {
		t.Error("split index must be rejected")
	}
}

func TestTrackedRejectsGitFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".git"), []byte("gitdir: /elsewhere\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Tracked(os.DirFS(root), ".git"); err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Errorf(".git file must be rejected, got %v", err)
	}
}

func TestTrackedMissingGit(t *testing.T) {
	if _, err := Tracked(os.DirFS(t.TempDir()), ".git"); err == nil {
		t.Error("missing .git must error")
	}
}

// ── against hand-built bytes (no git needed) ────────────────────────

// buildIndex assembles a syntactically valid v2 index for the given paths
// (which must be pre-sorted unless testing the order check).
func buildIndex(paths ...string) []byte {
	var b bytes.Buffer
	b.WriteString(sigDIRC)
	binary.Write(&b, binary.BigEndian, uint32(2))
	binary.Write(&b, binary.BigEndian, uint32(len(paths)))
	for _, p := range paths {
		start := b.Len()
		b.Write(make([]byte, statLen)) // zeroed stat data
		b.Write(make([]byte, 20))      // zeroed sha1
		binary.Write(&b, binary.BigEndian, uint16(len(p)&nameMask))
		b.WriteString(p)
		for (b.Len()-start)%8 != 0 || b.Len() == start { // ≥1 NUL, pad to 8
			b.WriteByte(0)
		}
		if bytes.IndexByte(b.Bytes()[start+statLen+20+flagsLen:b.Len()], 0) < 0 {
			panic("entry not NUL-terminated")
		}
	}
	b.Write(make([]byte, 20)) // dummy trailing checksum (not verified)
	return b.Bytes()
}

func TestParseHandBuilt(t *testing.T) {
	set, err := parse(buildIndex("a.txt", "dir/b.txt", "dir/c.txt"), 20)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]struct{}{"a.txt": {}, "dir/b.txt": {}, "dir/c.txt": {}}
	if !reflect.DeepEqual(set, want) {
		t.Errorf("parse = %v, want %v", set, want)
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	cases := map[string][]byte{
		"bad signature": append([]byte("JUNK"), buildIndex("a")[4:]...),
		"truncated":     buildIndex("a.txt", "b.txt")[:30],
		"empty":         {},
	}
	for name, data := range cases {
		if _, err := parse(data, 20); err == nil {
			t.Errorf("%s: parse must fail", name)
		}
	}
}

func TestParseRejectsOutOfOrder(t *testing.T) {
	// out-of-order entries are how a misread layout (e.g. wrong hash
	// length) manifests — must fail, never return a wrong set
	if _, err := parse(buildIndex("b.txt", "a.txt"), 20); err == nil || !strings.Contains(err.Error(), "out of order") {
		t.Errorf("want out-of-order error, got %v", err)
	}
}

func TestParseWrongHashLenFailsLoudly(t *testing.T) {
	if set, err := parse(buildIndex("svc/a.txt", "svc/b.txt", "web/c.txt"), 32); err == nil {
		t.Errorf("sha1 index parsed with hashLen 32 must fail, got %v", set)
	}
}
