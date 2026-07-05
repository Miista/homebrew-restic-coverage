package scan

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func writeFiles(t *testing.T, root string, paths ...string) {
	t.Helper()
	for _, p := range paths {
		full := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDataFiles(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root,
		"svc/data/state.json",          // under */data/
		"svc/data/nested/deep.txt",     // under */data/, any depth
		"svc/config.yaml",              // not data-like
		"svc/app.db",                   // loose *.db
		"svc/store.sqlite3",            // loose *.sqlite*
		"tunnel/credentials.json",      // loose credentials*
		"tls/private.key",              // loose *.key
		"tls/chain.pem",                // loose *.pem
		".git/objects/data/blob",       // pruned
		"web/node_modules/x/data/y.js", // pruned
		"README.md",                    // not data-like
	)
	got, skipped, err := DataFiles(Root(root), root, root)
	if skipped != 0 {
		t.Errorf("skipped = %d, want 0", skipped)
	}
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		root + "/svc/app.db",
		root + "/svc/data/nested/deep.txt",
		root + "/svc/data/state.json",
		root + "/svc/store.sqlite3",
		root + "/tls/chain.pem",
		root + "/tls/private.key",
		root + "/tunnel/credentials.json",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("DataFiles = %v\nwant %v", got, want)
	}
}

func TestDataFilesRootMapping(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, "svc/data/x")
	got, _, err := DataFiles(Root(root), root, "/host/docker")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/host/docker/svc/data/x"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("DataFiles = %v, want %v", got, want)
	}
}

func TestDataFilesCountsUnreadable(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, "svc/data/x", "locked/data/y")
	if err := os.Chmod(filepath.Join(root, "locked"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(root, "locked"), 0o755) })
	got, skipped, err := DataFiles(Root(root), root, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || skipped == 0 {
		t.Errorf("got %v, skipped %d — unreadable subtree must be counted", got, skipped)
	}
}
