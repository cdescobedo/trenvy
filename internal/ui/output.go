package ui

import (
	"strings"

	"github.com/cdescobedo/trenvy/internal/manifest"
)

// OutputRow is one line of the Outputs tab: the same facts `trenvy output
// list` prints -- name, format, path, and the globs summarized as prose --
// plus the render-freshness state internal/cli's outputStates already
// computed for the Overview's Problems. Built once by the cli host and handed
// in through Params, never recomputed here: this package does no I/O and
// cannot read a rendered file or a keyring to work out freshness for itself.
type OutputRow struct {
	Name    string
	Format  string
	Path    string
	Selects string // prose from SelectsSummary
	State   string // one of the State* constants in problem.go
	// IsDefault marks project.Output, so the tab can mark it with "*" the
	// same way `trenvy output list` does.
	IsDefault bool
	// Include and Exclude are the raw globs behind Selects -- Task 8's
	// editor needs the actual patterns, not the prose, to derive its
	// opening checkbox state through resolve.Selects (see
	// Model.deriveOutputGlobsChecked). Selects stays as its own field
	// rather than being recomputed from these here: SelectsSummary is a
	// pure function of an Output, and calling it twice from two different
	// inputs that are supposed to agree is exactly the "two places compute
	// one fact" shape that already produced a phase-1 Critical.
	Include, Exclude []string
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
