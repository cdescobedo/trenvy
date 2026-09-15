package guard

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/cdescobedo/trenvy/internal/config"
	"github.com/cdescobedo/trenvy/internal/render"
)

// Status describes a rendered file's relationship to what trenvy would write
// now. Keeping two hashes in the state file — the values' and the file's —
// separates three conditions that a single in-file header would conflate.
type Status int

const (
	// StatusAbsent: no file on disk.
	StatusAbsent Status = iota
	// StatusFresh: written by trenvy, current, and untouched since.
	StatusFresh
	// StatusStale: written by trenvy, but the manifest or keyring changed.
	StatusStale
	// StatusModified: written by trenvy, then hand edited.
	StatusModified
	// StatusForeign: exists, but trenvy has no record of writing it here.
	StatusForeign
)

func (s Status) String() string {
	switch s {
	case StatusAbsent:
		return "absent"
	case StatusFresh:
		return "fresh"
	case StatusStale:
		return "STALE"
	case StatusModified:
		return "MODIFIED"
	case StatusForeign:
		return "FOREIGN"
	}
	return "unknown"
}

// Inspect classifies the file at absPath given what the manifest would produce
// now (wantSum) and what was recorded for this worktree and output.
func Inspect(absPath, wantSum string, entry config.RenderEntry, haveEntry bool) (Status, error) {
	if _, err := os.Stat(absPath); errors.Is(err, fs.ErrNotExist) {
		return StatusAbsent, nil
	} else if err != nil {
		return StatusAbsent, fmt.Errorf("stat %s: %w", absPath, err)
	}

	if !haveEntry {
		return StatusForeign, nil
	}

	onDisk, err := render.FileSumAt(absPath)
	if err != nil {
		return StatusAbsent, err
	}
	// Checked before staleness on purpose: a hand edit is the more urgent
	// fact, because overwriting it loses work, whereas staleness only means
	// the file is behind.
	if onDisk != entry.File {
		return StatusModified, nil
	}
	if entry.Sum != wantSum {
		return StatusStale, nil
	}
	return StatusFresh, nil
}

// CheckWritable refuses to touch a file trenvy did not write, or one that was
// hand edited after it did.
//
// verb names what the caller is about to do to the file ("overwrite" for
// render, "delete" for clean). The distinction is not cosmetic: told to
// "re-run with --force to overwrite" while running trenvy clean, a user has
// been given the wrong mental model of what they are about to lose.
func CheckWritable(st Status, absPath, verb string, force bool) error {
	if force {
		return nil
	}
	if verb == "" {
		verb = "overwrite"
	}
	switch st {
	case StatusForeign:
		return fmt.Errorf(
			"%s exists and trenvy has no record of writing it\n"+
				"  inspect it first, then re-run with --force to %s it\n"+
				"  or seed a manifest from it:  trenvy import %s",
			absPath, verb, absPath)
	case StatusModified:
		return fmt.Errorf(
			"%s was modified after trenvy wrote it\n"+
				"  re-run with --force to %s it and discard those edits",
			absPath, verb)
	}
	return nil
}
