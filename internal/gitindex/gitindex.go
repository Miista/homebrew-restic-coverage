// Package gitindex reads the set of tracked paths from a git index file
// (.git/index), without a git binary. Only the entry list is needed — no
// objects, no history — so the parser covers index versions 2 and 3 and
// fails loudly on anything it does not fully understand (version 4, split
// index, sparse index, unknown mandatory extensions). A wrong answer here
// would silently corrupt the audit's candidate set; an error cannot.
package gitindex

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io/fs"
	"regexp"
)

const (
	sigDIRC     = "DIRC"
	flagsLen    = 2
	statLen     = 40     // ctime, mtime, dev, ino, mode, uid, gid, size — 10 × 4 bytes
	nameMask    = 0x0FFF // low 12 bits of flags: path length, 0xFFF = "longer"
	extendedBit = 0x4000
)

var sha256Re = regexp.MustCompile(`(?im)^\s*objectformat\s*=\s*sha256\s*$`)

// Tracked returns every path in the repo's index, relative to the repo root,
// slash-separated, as a set. fsys is the filesystem containing the repo;
// gitDir is the repo's .git directory within fsys (usually ".git").
func Tracked(fsys fs.FS, gitDir string) (map[string]struct{}, error) {
	if fi, err := fs.Stat(fsys, gitDir); err != nil {
		return nil, fmt.Errorf("reading %s: %w", gitDir, err)
	} else if !fi.IsDir() {
		// worktree/submodule pointer file ("gitdir: ...") — the real git
		// dir may not be visible in this filesystem; refuse rather than guess
		return nil, fmt.Errorf("%s is a file (linked worktree or submodule), not a directory — not supported", gitDir)
	}
	data, err := fs.ReadFile(fsys, gitDir+"/index")
	if err != nil {
		return nil, fmt.Errorf("reading git index: %w", err)
	}
	hashLen := 20
	// SHA-256 repos use the same index layout with 32-byte object names; the
	// repository format is declared in the config, not the index header.
	if cfg, err := fs.ReadFile(fsys, gitDir+"/config"); err == nil && sha256Re.Match(cfg) {
		hashLen = 32
	}
	return parse(data, hashLen)
}

func parse(data []byte, hashLen int) (map[string]struct{}, error) {
	if len(data) < 12+hashLen {
		return nil, fmt.Errorf("git index: truncated (%d bytes)", len(data))
	}
	if string(data[:4]) != sigDIRC {
		return nil, fmt.Errorf("git index: bad signature %q", data[:4])
	}
	version := binary.BigEndian.Uint32(data[4:8])
	if version != 2 && version != 3 {
		return nil, fmt.Errorf("git index: unsupported version %d (only 2 and 3 are supported)", version)
	}
	count := binary.BigEndian.Uint32(data[8:12])
	body := data[12 : len(data)-hashLen] // trailing hash excluded
	set := make(map[string]struct{}, count)

	pos := 0
	prev := ""
	for i := uint32(0); i < count; i++ {
		fixed := statLen + hashLen + flagsLen
		if pos+fixed > len(body) {
			return nil, fmt.Errorf("git index: truncated at entry %d/%d", i+1, count)
		}
		flags := binary.BigEndian.Uint16(body[pos+statLen+hashLen:])
		nameOff := pos + fixed
		if flags&extendedBit != 0 {
			if version < 3 {
				return nil, fmt.Errorf("git index: extended flags in a version-%d index", version)
			}
			nameOff += 2 // extended flags word (skip-worktree, intent-to-add)
		}
		nameLen := int(flags & nameMask)
		if nameLen == nameMask { // path is 0xFFF bytes or longer: NUL-delimited
			n := bytes.IndexByte(body[nameOff:], 0)
			if n < 0 {
				return nil, fmt.Errorf("git index: unterminated path at entry %d/%d", i+1, count)
			}
			nameLen = n
		}
		if nameOff+nameLen > len(body) {
			return nil, fmt.Errorf("git index: truncated path at entry %d/%d", i+1, count)
		}
		name := string(body[nameOff : nameOff+nameLen])
		// Entries are sorted; an out-of-order name means the layout was
		// misread (e.g. wrong hash length) — never return a wrong set.
		if name < prev {
			return nil, fmt.Errorf("git index: entries out of order (%q after %q) — misparsed index", name, prev)
		}
		prev = name
		set[name] = struct{}{} // stages of a conflict collapse into one path

		// entries are NUL-padded to a multiple of 8 bytes, ≥1 NUL after the path
		entryLen := (nameOff - pos + nameLen + 8) / 8 * 8
		pos += entryLen
	}

	if err := checkExtensions(body[pos:]); err != nil {
		return nil, err
	}
	return set, nil
}

// checkExtensions walks the extension records after the entries. Extensions
// whose signature starts with an uppercase letter are optional and skipped;
// anything else (link = split index, sdir = sparse index, ...) changes the
// meaning of the entry list and must abort the audit.
func checkExtensions(rest []byte) error {
	for len(rest) > 0 {
		if len(rest) < 8 {
			return fmt.Errorf("git index: truncated extension header")
		}
		sig := string(rest[:4])
		size := binary.BigEndian.Uint32(rest[4:8])
		if int64(size) > int64(len(rest)-8) {
			return fmt.Errorf("git index: extension %q overruns the file", sig)
		}
		if sig[0] < 'A' || sig[0] > 'Z' {
			return fmt.Errorf("git index: mandatory extension %q not supported (split/sparse index?) — run `git ls-files` based audit instead", sig)
		}
		rest = rest[8+size:]
	}
	return nil
}
