package ui

import "github.com/cdescobedo/trenvy/internal/session"

// WriteGlobs turns one output's checkbox state into the Include/Exclude
// globs manifest.Output carries, in the exact vocabulary
// internal/resolve/values.go's selected already applies -- this function's
// only job is to produce something that means, under resolve.Selects, what
// the checkboxes say.
//
// mode picks which of two futures a variable that does not exist yet falls
// into: under ModeEverythingExcept it is selected by default (the manifest
// only ever names the exceptions); under ModeOnlyThese it is left out by
// default (the manifest only ever names the inclusions). The two modes are
// NOT two different selections of the variables in all -- switching between
// them for the same checked set must not move a single one of today's
// variables in or out. See TestWriteGlobsModeSwitchPreservesSelection.
// A mode value other than ModeOnlyThese is treated as ModeEverythingExcept,
// matching this vocabulary's own bias: resolve.selected already treats an
// absent include as "everything" by default.
//
// include is left nil rather than []string{"*"} whenever "everything" is
// meant. This is load-bearing, not a style choice: resolve.selected treats
// a zero-length include as "everything" before path.Match ever runs, while
// a literal "*" is instead matched with path.Match, whose "*" does not
// cross "/" -- silently dropping a variable named e.g. GROUP/KEY from the
// rendered file. Phase 1 shipped exactly that bug (see the comment on
// deriveOutput in internal/cli/import.go); this function must never
// reintroduce it.
//
// Unchecking every box under ModeOnlyThese cannot be written as an empty
// include: an empty include is indistinguishable from an absent one, and an
// absent one means everything -- the exact opposite of what the user just
// expressed. There is no pattern in this vocabulary that means "nothing" the
// way len(include) == 0 means "everything" for the default case, so this
// names every currently known variable (all) as both included and excluded:
// each one matches its own include entry, then the identical exclude entry
// removes it again, so resolve.Selects reports false for every name in all.
// It reaches for names already known to be real rather than a pattern
// presumed to never match anything -- assuming a pattern is "impossible" is
// exactly the mistake ("*" can't reach a name with a slash) that produced
// the bug the paragraph above describes.
func WriteGlobs(mode Mode, all []string, checked map[string]bool) (include, exclude []string) {
	m := session.GlobsEverythingExcept
	if mode == ModeOnlyThese {
		m = session.GlobsOnlyThese
	}
	return session.WriteGlobs(m, all, checked)
}
