package dryrun

import (
	"errors"
	"reflect"
	"testing"
)

// fakeRunner returns canned output per command substring.
type fakeRunner struct {
	out map[string]string // substring of cmdline -> output
	err error
}

func (f fakeRunner) Shell(cmdline string) (string, error) {
	for sub, out := range f.out {
		if len(sub) == 0 {
			continue
		}
		if contains(cmdline, sub) {
			return out, f.err
		}
	}
	return "", f.err
}

func (f fakeRunner) Where() string { return "in test" }

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

const profileOut = `2026/07/05 00:17:55 using configuration file: profiles.yaml
2026/07/05 00:17:55 dry-run: /usr/bin/restic init --repo=/backup
2026/07/05 00:17:55 dry-run: /bin/sh /resticprofile/export.sh
2026/07/05 00:17:55 dry-run: /usr/bin/restic backup --exclude=data/tts --exclude=*.db-wal --exclude="*.log.*" --repo=/backup /home/admin/docker/pi/homeassistant/data /hostenv/pi/.env /home/admin/.ssh
2026/07/05 00:17:55 dry-run: /bin/sh /shared/notify-status.sh local true`

func TestCommand(t *testing.T) {
	r := fakeRunner{out: map[string]string{"--dry-run": profileOut}}
	cmd, err := Command(r, "default")
	if err != nil {
		t.Fatal(err)
	}
	want := `/usr/bin/restic backup --exclude=data/tts --exclude=*.db-wal --exclude="*.log.*" --repo=/backup /home/admin/docker/pi/homeassistant/data /hostenv/pi/.env /home/admin/.ssh`
	if cmd != want {
		t.Errorf("Command = %q, want %q", cmd, want)
	}
}

func TestCommandMissing(t *testing.T) {
	r := fakeRunner{out: map[string]string{"--dry-run": "no such profile"}}
	if _, err := Command(r, "nope"); err == nil {
		t.Fatal("expected error for missing backup command")
	}
}

func TestExcludes(t *testing.T) {
	cmd, _ := Command(fakeRunner{out: map[string]string{"--dry-run": profileOut}}, "default")
	got := Excludes(cmd)
	want := []string{"data/tts", "*.db-wal", "*.log.*"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Excludes = %v, want %v", got, want)
	}
}

func TestSourcesAndHostRoot(t *testing.T) {
	cmd, _ := Command(fakeRunner{out: map[string]string{"--dry-run": profileOut}}, "default")
	srcs := Sources(cmd)
	want := []string{"/home/admin/docker/pi/homeassistant/data", "/hostenv/pi/.env", "/home/admin/.ssh"}
	if !reflect.DeepEqual(srcs, want) {
		t.Errorf("Sources = %v, want %v", srcs, want)
	}
	if hr := HostRoot(cmd); hr != "/home/admin/docker" {
		t.Errorf("HostRoot = %q, want /home/admin/docker", hr)
	}
	if hr := HostRoot("restic backup /srv/data"); hr != "" {
		t.Errorf("HostRoot for non-home source = %q, want empty", hr)
	}
}

const vvOut = `open repository
using parent snapshot f2c19ebe
no parent snapshot found, will read all files
new       /home/admin/docker/pi/linkding/data/db.sqlite3, saved in 0.001s (8 KiB added)
new       /home/admin/docker/pi/linkding/data/favicons/, saved in 0.000s (0 B added, 0 B stored, 0 B metadata)
unchanged /home/admin/docker/pi/acme/data/account.conf
modified  /hostenv/pi/.env, saved in 0.002s (1 KiB added)
changed   /home/admin/.ssh/known_hosts, saved in 0.001s
Would add to the repository: 26.901 MiB

processed 129 files, 40.491 MiB in 0:02`

func TestIncluded(t *testing.T) {
	r := fakeRunner{out: map[string]string{"--verbose=2": vvOut}}
	got, err := Included(r, "restic backup ...", "/hostenv", "/home/admin/docker")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"/home/admin/.ssh/known_hosts",
		"/home/admin/docker/pi/.env", // /hostenv prefix mapped to host root
		"/home/admin/docker/pi/acme/data/account.conf",
		"/home/admin/docker/pi/linkding/data/db.sqlite3",
		"/home/admin/docker/pi/linkding/data/favicons", // trailing slash stripped
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Included = %v, want %v", got, want)
	}
}

func TestIncludedIdentityRoots(t *testing.T) {
	// host mode: scanRoot == hostRoot -> no rewriting
	r := fakeRunner{out: map[string]string{"--verbose=2": "new       /hostenv/pi/.env, saved in 0.002s"}}
	got, err := Included(r, "restic backup ...", "/x", "/x")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"/hostenv/pi/.env"}) {
		t.Errorf("Included = %v", got)
	}
}

func TestIncludedEmpty(t *testing.T) {
	r := fakeRunner{out: map[string]string{"--verbose=2": "processed 0 files"}, err: errors.New("boom")}
	if _, err := Included(r, "restic backup ...", "/hostenv", "/h"); err == nil {
		t.Fatal("expected error on empty include list")
	}
}
