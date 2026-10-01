package session

import (
	"fmt"
	"sort"
	"strings"
)

// Problem is one thing wrong with the project, paired with the command that
// fixes it, for the Overview tab.
type Problem struct {
	Kind    string // "missing", "output", "membership"
	Name    string // variable or output name
	Profile string // empty for output problems
	Detail  string // human sentence
	Fix     string // the command or key that fixes it
}

// The states an entry in Problems' outputs map may hold. Both the caller
// (internal/cli's outputStates, which classifies an output and maps that
// classification here) and this file's own comparisons below reference these
// symbols, rather than each independently spelling "fresh" or "FOREIGN" and
// hoping the two sides never drift -- which is exactly how "unknown
// (--quick)" and "not rendered" reached this package as CLI-specific prose
// that Problems could not recognize.
const (
	StateFresh = "fresh"
	// StateAbsent matches the exact word `trenvy status` already prints for
	// this fact (describeOutput's own "not rendered", never guard.Status's
	// generic "absent") -- one vocabulary for one fact, not a second word
	// invented for the same thing on this tab.
	StateAbsent   = "not rendered"
	StateStale    = "STALE"
	StateModified = "MODIFIED"
	StateForeign  = "FOREIGN"
	// StateUnknown means the caller could not classify the output without
	// reading a secret it would rather not have read (see cli.outputStates).
	StateUnknown = "unknown"
	// StateUnverified means the output's file and render record both check
	// out -- it is not absent, FOREIGN or hand-edited -- but whether the
	// resolved values it was last rendered from still match is unknowable
	// without reading a secret the caller (cli.outputStates) deliberately
	// never reads. This is NOT the same claim as StateFresh, which says the
	// render matches now; StateUnverified says only that nothing OBSERVABLE
	// is wrong, echoing the exact words `trenvy status --quick` already uses
	// for this case rather than inventing a second one. It is treated as no
	// problem below for the same reason StateFresh and StateAbsent are: the
	// caller always classifies in quick mode, so flagging this here would be
	// a permanent alarm nothing pressable inside `trenvy ui` could ever
	// clear -- the exact defect an earlier fix on this state already removed
	// once (see cli.outputStates' own doc).
	StateUnverified = "unknown (--quick)"
)

// Problems walks the matrix for CellMissing entries and the outputs map for
// unhealthy render states, and returns both as a stable, sorted list.
//
// outputs maps an output name to one of the States above. Computing that
// state means reading the rendered file and the state file, which is I/O
// this package does not do — the caller passes the answer in, in this
// package's vocabulary: a caller must never pass through CLI-specific text
// (a flag name, for instance) that only makes sense in the command that
// produced it.
func Problems(mx *Matrix, outputs map[string]string) []Problem {
	var ps []Problem

	for _, name := range mx.Names {
		for _, profile := range mx.Profiles {
			if mx.Cell(name, profile) != CellMissing {
				continue
			}
			var fix string
			if mx.Vars[name].Secret {
				fix = fmt.Sprintf("trenvy -p %s secret set %s", profile, name)
			} else {
				fix = fmt.Sprintf("trenvy -p %s set %s <value>", profile, name)
			}
			ps = append(ps, Problem{
				Kind:    "missing",
				Name:    name,
				Profile: profile,
				Detail:  fmt.Sprintf("%s has no value under %s", name, profile),
				Fix:     fix,
			})
		}
	}

	// StateFresh means the render matches the manifest; StateAbsent means it
	// has never been rendered in this worktree, the normal state of a fresh
	// clone. StateUnverified means the caller could not tell fresh from
	// STALE without reading a secret it deliberately never reads (see its
	// own doc above) -- reported once, if at all, via the Overview's own
	// healthy-line caveat, never per output here. All three would put a
	// permanent false alarm in front of every new user, or every project
	// with a secret, if they reached this list.
	for name, state := range outputs {
		if state == StateFresh || state == StateAbsent || state == StateUnverified {
			continue
		}
		// StateUnknown is a different problem from STALE/MODIFIED/FOREIGN,
		// which are all answers. "trenvy render" is the wrong fix here: it
		// does not resolve not-knowing, and it can outright fail if the file
		// was hand-edited, which is exactly the case this state cannot rule
		// out. trenvy status is the command that actually reads the secret
		// and computes the real digest.
		if state == StateUnknown {
			ps = append(ps, Problem{
				Kind:   "output",
				Name:   name,
				Detail: fmt.Sprintf("%s: freshness needs reading its secrets", name),
				Fix:    "trenvy status",
			})
			continue
		}
		ps = append(ps, Problem{
			Kind:   "output",
			Name:   name,
			Detail: fmt.Sprintf("%s is %s", name, state),
			Fix:    fmt.Sprintf("trenvy render %s", name),
		})
	}

	// Membership: does a variable reach any output at all, or too many?
	// Neither is an error -- exec and shell deliver a project's whole
	// environment without naming a single output, so a project using only
	// those legitimately has every variable at zero, and nothing stops two
	// outputs from carrying the same variable on purpose. This only surfaces
	// a fact a user cannot otherwise get without reading every output's
	// globs by hand; a variable selected by exactly one output is the
	// unremarkable case and is not reported at all.
	//
	// Skipped entirely when the project defines no outputs -- the normal
	// shape of an exec/shell-only project (see viewOutputs' own placeholder
	// for the same case). In that case EVERY variable is at zero by
	// definition, and reporting that fact once per variable would bury an
	// otherwise healthy Overview in N copies of the same non-problem.
	if len(mx.Outputs) > 0 {
		byName := membershipOutputsForMatrix(mx)

		// Fix 6, round 2 (Minor, whole-branch review): "reaches several
		// outputs" exists to surface a variable whose reach differs from the
		// rest, not to describe the project's general shape -- but the first
		// version of this fix evaluated that over the WHOLE project: it
		// suppressed only when EVERY variable's count matched every other
		// variable's, so a single outlier switched every one of its peers
		// back on. That is not a rare shape; it is what the membership
		// editor produces the first time anyone uses it. A default project
		// starts uniform (every variable in every output), then someone
		// opens the editor to carve an exception into ONE variable -- the
		// entire purpose of the feature Task 8 built -- and the old rule
		// answered with a warning on every other variable too, burying the
		// one that actually carries information under ten that do not.
		//
		// The fix is per-variable: flag a count only when it differs from
		// the MODAL count -- the number of outputs most variables reach.
		// modeCount below is computed once over every variable's count
		// (zero included; a variable that reaches none is judged separately
		// by the case below and can never reach this branch, so including
		// it here cannot cause it to be double-flagged, and excluding it
		// would only complicate this loop for no behavioural difference).
		// ok is false when there is no single most-common count -- a tie
		// for the mode, e.g. half the variables at two outputs and half at
		// three -- and the honest answer there is silence: "differs from
		// the rest" presumes a rest to differ from, and a population split
		// evenly in two has no majority to be the exception to.
		counts := make(map[string]int, len(mx.Names))
		freq := map[int]int{}
		for _, name := range mx.Names {
			n := len(byName[name])
			counts[name] = n
			freq[n]++
		}
		var modeCount, maxFreq int
		var tied bool
		for n, f := range freq {
			switch {
			case f > maxFreq:
				maxFreq, modeCount = f, n
				tied = false
			case f == maxFreq:
				tied = true
			}
		}
		modeOK := maxFreq > 0 && !tied

		for _, name := range mx.Names {
			outs := byName[name]
			switch {
			case len(outs) == 0:
				ps = append(ps, Problem{
					Kind:   "membership",
					Name:   name,
					Detail: fmt.Sprintf("%s reaches no output", name),
					Fix: fmt.Sprintf(
						"Outputs tab (4): enter on an output, then space to include %s -- if it should reach a file",
						name),
				})
			case len(outs) > 1 && modeOK && counts[name] != modeCount:
				ps = append(ps, Problem{
					Kind:   "membership",
					Name:   name,
					Detail: fmt.Sprintf("%s reaches %d outputs: %s", name, len(outs), strings.Join(outs, ", ")),
					Fix:    "Outputs tab (4): enter on each listed output to review its globs, if that's not intended",
				})
			}
		}
	}

	// The Overview re-renders on every keystroke; an unstable order would
	// jump rows under the user's cursor and misdirect enter-to-jump later.
	sort.Slice(ps, func(i, j int) bool {
		if ps[i].Name != ps[j].Name {
			return ps[i].Name < ps[j].Name
		}
		return ps[i].Profile < ps[j].Profile
	})

	return ps
}
