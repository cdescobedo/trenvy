package session

import (
	"strings"

	"github.com/cdescobedo/trenvy/internal/manifest"
	"github.com/cdescobedo/trenvy/internal/resolve"
)

// GlobMode says which of two futures a variable that does not exist yet
// falls into for one output: in the file (everything except the unchecked
// ones) or out of it (only the checked ones). WriteGlobs takes one; the
// Outputs screen shows it as a sentence.
type GlobMode int

const (
	GlobsEverythingExcept GlobMode = iota
	GlobsOnlyThese
)

// InferGlobMode reads which future an output's include list already
// promises: no include, or the single "*", is everything-except.
func InferGlobMode(include []string) GlobMode {
	if len(include) == 0 || (len(include) == 1 && include[0] == "*") {
		return GlobsEverythingExcept
	}
	return GlobsOnlyThese
}

// Checked reports which of names an output with these globs selects, by
// the rule render itself uses (resolve.Selects), so the grid can never
// claim a variable reaches a file that render would leave it out of.
func Checked(include, exclude, names []string) map[string]bool {
	scratch := &manifest.Manifest{Outputs: map[string]manifest.Output{
		"x": {Include: include, Exclude: exclude},
	}}
	out := make(map[string]bool, len(names))
	for _, n := range names {
		if sel, err := resolve.Selects(scratch, "x", n); err == nil {
			out[n] = sel
		}
	}
	return out
}

// SelectsSummary renders an output's globs as prose, because the globs
// themselves do not say which of two futures applies: whether a variable
// added tomorrow lands in this file or stays out of it.
//
// Include and Exclude are independent in resolve.selected -- include decides
// membership, then exclude is subtracted -- so the exclusions are appended
// whatever the include list says. Reporting only the include list would tell
// a reader a variable reaches this file when resolve keeps it out.
//
// Lives here, not in internal/cli, because Task 4's Outputs tab needs the
// exact same prose `trenvy output list` prints, and a second copy of this
// rule is the specific mistake a phase-1 Critical came from: two places
// computing one fact about outputs, free to disagree. internal/cli already
// imports this package for Matrix, Problems and the State* constants; this
// package imports nothing from internal/cli, so this is the only direction
// the move can go without introducing an import cycle.
func SelectsSummary(o manifest.Output) string {
	sel := "all"
	if len(o.Include) > 0 && !(len(o.Include) == 1 && o.Include[0] == "*") {
		sel = strings.Join(o.Include, ", ")
	}
	if len(o.Exclude) > 0 {
		sel += " except " + strings.Join(o.Exclude, ", ")
	}
	return sel
}

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
func WriteGlobs(mode GlobMode, all []string, checked map[string]bool) (include, exclude []string) {
	if mode == GlobsOnlyThese {
		for _, name := range all {
			if checked[name] {
				include = append(include, name)
			}
		}
		if len(include) == 0 && len(all) > 0 {
			include = append([]string{}, all...)
			exclude = append([]string{}, all...)
		}
		return include, exclude
	}

	// ModeEverythingExcept (and any other value, per the doc comment above).
	for _, name := range all {
		if !checked[name] {
			exclude = append(exclude, name)
		}
	}
	return nil, exclude
}
