package coverage

import (
	"reflect"
	"testing"
)

func TestAudit(t *testing.T) {
	included := []string{
		"/d/pi/linkding/data/db.sqlite3",
		"/d/pi/pihole/data/toml/pihole.toml",
	}
	ondisk := []string{
		"/d/pi/linkding/data/db.sqlite3",     // backed up
		"/d/pi/pihole/data/toml/pihole.toml", // backed up
		"/d/pi/pihole/data/gravity.db",       // ignore pattern
		"/d/pi/homeassistant/data/tts/x.mp3", // profile exclude
		"/d/pi/newservice/data/state.db",     // VIOLATION
		"/d/pi/other/data/thing.log",         // profile exclude *.log
	}
	patterns := []string{
		"homeassistant/data/tts", // profile exclude style (relative)
		"*.log",                  // profile exclude style (glob)
		"*/pi/pihole/data/*",     // ignore file style (anchored glob)
	}
	rep := Audit(included, ondisk, patterns)
	if rep.Checked != len(ondisk) {
		t.Errorf("Checked = %d, want %d", rep.Checked, len(ondisk))
	}
	want := []string{"/d/pi/newservice/data/state.db"}
	if !reflect.DeepEqual(rep.Violations, want) {
		t.Errorf("Violations = %v, want %v", rep.Violations, want)
	}
	if rep.OK() {
		t.Error("OK must be false with violations")
	}
}

func TestAuditClean(t *testing.T) {
	rep := Audit([]string{"/a"}, []string{"/a"}, nil)
	if !rep.OK() || rep.Violations != nil {
		t.Errorf("clean audit: %+v", rep)
	}
}

func TestAuditIgnoreOverlapsSource(t *testing.T) {
	// an ignore pattern covering a parent of a backed-up path must not hide
	// the backed-up file (it is included) but must absorb its siblings
	included := []string{"/d/pihole/data/toml/pihole.toml"}
	ondisk := []string{
		"/d/pihole/data/toml/pihole.toml",
		"/d/pihole/data/gravity.db",
	}
	rep := Audit(included, ondisk, []string{"*/pihole/data/*"})
	if !rep.OK() {
		t.Errorf("Violations = %v, want none", rep.Violations)
	}
}
