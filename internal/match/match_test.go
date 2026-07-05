package match

import "testing"

func TestGlob(t *testing.T) {
	cases := []struct {
		path, glob string
		want       bool
	}{
		{"/a/b/c.log", "*.log", true},          // * crosses separators
		{"/a/b/c.log.1", "*.log.*", true},      // dots are literal
		{"/a/b/c.login", "*.log", false},       // anchored at end
		{"/a/data/x.db-wal", "*.db-wal", true}, // suffix match
		{"/a/data/x.db", "*.db-wal", false},    //
		{"file.dblog", "*.db", false},          // no partial suffix
		{"/x/pi/gatus/data/db", "*/pi/gatus/data/*", true},
		{"abc", "a?c", true},      // ? single char
		{"ac", "a?c", false},      //
		{"a.c", "a[.x]c", true},   // character class
		{"ayc", "a[!.x]c", true},  // negated class
		{"a.c", "a[!.x]c", false}, //
		{"a[c", "a[c", true},      // unterminated class = literal
	}
	for _, c := range cases {
		if got := Glob(c.path, c.glob); got != c.want {
			t.Errorf("Glob(%q, %q) = %v, want %v", c.path, c.glob, got, c.want)
		}
	}
}

func TestPath(t *testing.T) {
	cases := []struct {
		path, pattern string
		want          bool
	}{
		// anchored at any depth, mirroring the sh-case variants
		{"/home/admin/docker/pi/gatus/data/db", "*/pi/gatus/data/*", true},
		{"/home/admin/docker/pi/gatus/data/db", "pi/gatus/data/*", true}, // */p form
		{"/home/admin/docker/pi/gatus/data/db", "pi/gatus/data", true},   // */p/* form
		{"/home/admin/docker/pi/gatus/data", "pi/gatus/data", true},      // */p exact dir
		{"/home/admin/docker/pi/gatusx/data/db", "pi/gatus/data", false}, // component boundary
		{"pi/gatus/data/db", "pi/gatus/data", true},                      // p/* form
		{"/a/homeassistant/data/tts/x.mp3", "homeassistant/data/tts", true},
		{"/a/homeassistant/data/x", "homeassistant/data/tts", false},
		{"/a/b/thing.log", "*.log", true},
	}
	for _, c := range cases {
		if got := Path(c.path, c.pattern); got != c.want {
			t.Errorf("Path(%q, %q) = %v, want %v", c.path, c.pattern, got, c.want)
		}
	}
}

func TestAny(t *testing.T) {
	pats := []string{"*.log", "pi/gatus/data"}
	if !Any("/x/y.log", pats) {
		t.Error("Any should match *.log")
	}
	if Any("/x/y.txt", pats) {
		t.Error("Any should not match")
	}
	if Any("/x/y.txt", nil) {
		t.Error("Any with no patterns must be false")
	}
}
