package session

import (
	"sort"

	"github.com/cdescobedo/trenvy/internal/manifest"
	"github.com/cdescobedo/trenvy/internal/resolve"
)

// Membership counts, for every variable m defines, how many of m's outputs
// select it.
//
// A count of zero is not a defect: exec and shell deliver a project's whole
// environment without naming a single output, so a project that only ever
// feeds those legitimately has every variable at zero. A count above one is
// equally legitimate -- nothing stops two outputs from carrying the same
// variable on purpose. Deciding which counts are worth a user's attention is
// Problems' job, not this function's; Membership only supplies the fact,
// which today a user can only get by reading every output's globs by hand.
//
// Selection goes through resolve.Selects, the same rule the render path
// applies -- never a second glob match here. Two implementations of "does
// this output write this variable" free to disagree is exactly how phase 1
// shipped a Critical (see this task's CONTROLLER AMENDMENT).
func Membership(m *manifest.Manifest) map[string]int {
	byName := membershipOutputs(m)
	counts := make(map[string]int, len(m.Vars))
	for name := range m.Vars {
		counts[name] = len(byName[name])
	}
	return counts
}

// membershipOutputs maps each variable name to the sorted list of output
// names whose Include/Exclude globs select it. Unexported: Membership above
// (a plain count) and Problems (which needs the actual names for its Detail
// text, not just how many) both build on this one scan, so "which outputs
// select this variable" is computed in exactly one place.
func membershipOutputs(m *manifest.Manifest) map[string][]string {
	byName := make(map[string][]string, len(m.Vars))
	for name := range m.Vars {
		var outs []string
		for outName := range m.Outputs {
			sel, err := resolve.Selects(m, outName, name)
			if err != nil {
				// An invalid glob is a problem with that output's own
				// definition, not something a membership scan resolves by
				// guessing an answer -- skip it rather than fail the whole
				// scan over one bad pattern elsewhere in the manifest.
				continue
			}
			if sel {
				outs = append(outs, outName)
			}
		}
		sort.Strings(outs)
		byName[name] = outs
	}
	return byName
}

// membershipOutputsForMatrix applies membershipOutputs to the fields Matrix
// already carries (Outputs and Vars), so Problems can ask "which outputs
// select this variable" without threading a second *manifest.Manifest
// through its own signature. Matrix is already the one artifact built once
// from the manifest and handed to Problems -- the same reason it already
// carries Vars -- so its Outputs field (added alongside this function) is
// the natural home rather than a parallel copy of the manifest.
func membershipOutputsForMatrix(mx *Matrix) map[string][]string {
	return membershipOutputs(&manifest.Manifest{Outputs: mx.Outputs, Vars: mx.Vars})
}
