// Package ui is the tabbed management interface behind `trenvy ui`.
//
// It is separate from internal/tui, which is the flat single-profile table
// that edit and import drive. The two share no state: a matrix over profiles
// and a list of one profile's variables are different shapes, and keeping
// them apart means edit cannot regress when this changes.
package ui

import "github.com/cdescobedo/trenvy/internal/manifest"

// Cell is one variable's state under one profile.
type Cell string

const (
	CellShared   Cell = "shared"
	CellOverride Cell = "override"
	CellStored   Cell = "stored"
	CellMissing  Cell = "MISSING"
	CellOptional Cell = "optional"
)

// Classify reports how a variable resolves under one profile.
//
// The order below is resolve.Value's precedence and must stay that way: the
// matrix's whole claim is that it shows what render will do, so a second
// precedence living here would make it confidently wrong. `stored` is the
// keyring answer from Has, supplied by the caller, because this package does
// no I/O.
//
// The secret branch does not consult Profiles or Value, where resolve.Value
// consults them for every variable. That is safe rather than a divergence:
// manifest.Validate refuses a secret carrying either, so in a manifest that
// loads there is nothing there to consult. Relaxing that rule would make this
// function wrong, which is why the dependency is named here.
func Classify(v manifest.Var, profile string, stored bool) Cell {
	if !v.Secret {
		if _, ok := v.Profiles[profile]; ok {
			return CellOverride
		}
		if v.Value != nil {
			return CellShared
		}
	} else if stored {
		return CellStored
	}
	if v.Optional {
		return CellOptional
	}
	return CellMissing
}
