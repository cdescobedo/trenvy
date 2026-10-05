package session

import (
	"sort"

	"github.com/cdescobedo/trenvy/internal/manifest"
)

// Staged is the ordered set of changes a session holds until it saves.
//
// It is a set, not a log: a change deciding the same thing as an earlier
// one (the same cell's value, the same variable's optional flag) replaces
// it in place, so the review lists what will happen rather than every
// keypress that led there. Order is kept because commitStaged's phases
// already make the result independent of it, and a stable order keeps the
// review from reshuffling under the cursor.
//
// The zero value is an empty set, ready to use.
type Staged struct{ list []Change }

// slot names what a change decides. Two changes with the same slot cannot
// both stand, so Add keeps only the later.
func slot(c Change) string {
	switch c.Kind {
	case ChangeSetValue, ChangeSetShared, ChangeUnsetValue:
		return "value\x00" + c.Name + "\x00" + c.Profile
	case ChangeSetSecret:
		return "secret\x00" + c.Name + "\x00" + c.Profile
	case ChangeDefaultOutput:
		// One project, one default: a new default replaces the last.
		return c.Kind
	}
	return c.Kind + "\x00" + c.Name
}

// isOutputKind reports whether kind changes an output rather than a
// variable. The two share Change.Name and an output may share a
// variable's name, so every rule keyed by name looks only at its own kind
// (phase 1 ledger, Task 3).
func isOutputKind(kind string) bool {
	switch kind {
	case ChangeAddOutput, ChangeRenameOutput, ChangeRemoveOutput, ChangeDefaultOutput,
		ChangeSetGlobs, ChangeSetOutputFormat, ChangeSetOutputPath:
		return true
	}
	return false
}

// Add stages c. Variable kinds and output kinds are two namespaces: they
// share Change.Name, an output may be named like a variable, and each rule
// below looks only at its own kind. default-output occupies a single slot,
// so staging a new default replaces the previous one.
//
// The invariant: a delete-var is never staged beside another change on the
// same name, so commit never receives both. Two rules hold it:
//   - Deleting a variable drops every other change staged on it. One that
//     was only added this session then stages nothing at all: it never
//     existed.
//   - Any other change on a variable marked for deletion un-marks it, the
//     rule internal/ui's beginEdit holds.
//
// Without the first, a promotion staged before the delete would store its
// keyring items and the delete would then remove the manifest entry that
// names them. Outputs follow the same two rules with remove-output.
func (s *Staged) Add(c Change) {
	if isOutputKind(c.Kind) {
		s.addOutput(c)
		return
	}
	if c.Kind == ChangeDeleteVar {
		added := s.Has(ChangeAddVar, c.Name)
		s.DropOn(c.Name)
		if added {
			return
		}
	} else {
		s.DropKind(ChangeDeleteVar, c.Name)
	}
	s.put(c)
}

// addOutput is Add for output kinds, with the same two rules variables
// follow: removing an output drops its other changes (or nets to nothing
// when it was added this session), and any other change on a removed
// output restores it.
func (s *Staged) addOutput(c Change) {
	if c.Kind == ChangeRemoveOutput {
		added := s.Has(ChangeAddOutput, c.Name)
		s.DropOnOutput(c.Name)
		if added {
			return
		}
	} else {
		s.DropKind(ChangeRemoveOutput, c.Name)
	}
	s.put(c)
}

// put stages c, replacing a change in the same slot.
func (s *Staged) put(c Change) {
	k := slot(c)
	for i, x := range s.list {
		if slot(x) == k {
			s.list[i] = c
			return
		}
	}
	s.list = append(s.list, c)
}

// All returns a copy of every staged change, in staged order.
func (s Staged) All() []Change { return append([]Change(nil), s.list...) }

// Len is the number of staged changes.
func (s Staged) Len() int { return len(s.list) }

// Has reports whether a change of kind is staged on name.
func (s Staged) Has(kind, name string) bool {
	for _, c := range s.list {
		if c.Kind == kind && c.Name == name {
			return true
		}
	}
	return false
}

// On returns every change staged on the variable name.
func (s Staged) On(name string) []Change {
	var out []Change
	for _, c := range s.list {
		if c.Name == name && !isOutputKind(c.Kind) {
			out = append(out, c)
		}
	}
	return out
}

// OnCell returns the value and secret changes staged on one cell. Profile
// "" is the shared value.
func (s Staged) OnCell(name, profile string) []Change {
	var out []Change
	for _, c := range s.list {
		if c.Name == name && c.Profile == profile && isCellKind(c.Kind) {
			out = append(out, c)
		}
	}
	return out
}

func isCellKind(kind string) bool {
	switch kind {
	case ChangeSetValue, ChangeSetShared, ChangeUnsetValue, ChangeSetSecret:
		return true
	}
	return false
}

// Added returns every variable name added this session, sorted.
func (s Staged) Added() []string {
	var out []string
	for _, c := range s.list {
		if c.Kind == ChangeAddVar {
			out = append(out, c.Name)
		}
	}
	sort.Strings(out)
	return out
}

// dependsOn reports whether dep cannot stand once on is dropped.
func dependsOn(dep, on Change) bool {
	if dep.Name != on.Name {
		return false
	}
	switch on.Kind {
	case ChangeAddVar:
		return !isOutputKind(dep.Kind)
	case ChangeAddOutput:
		return isOutputKind(dep.Kind)
	case ChangePromoteSecret:
		return dep.Kind == ChangeSetSecret
	}
	return false
}

// Drop removes the change at index i and every change that depends on it.
// The returned slice starts with the change asked for.
func (s *Staged) Drop(i int) []Change {
	if i < 0 || i >= len(s.list) {
		return nil
	}
	target := s.list[i]
	dropped := []Change{target}
	keep := make([]Change, 0, len(s.list))
	for j, c := range s.list {
		switch {
		case j == i:
		case dependsOn(c, target):
			dropped = append(dropped, c)
		default:
			keep = append(keep, c)
		}
	}
	s.list = keep
	return dropped
}

func (s *Staged) remove(match func(Change) bool) []Change {
	var dropped []Change
	keep := make([]Change, 0, len(s.list))
	for _, c := range s.list {
		if match(c) {
			dropped = append(dropped, c)
		} else {
			keep = append(keep, c)
		}
	}
	s.list = keep
	return dropped
}

// DropOn removes every change on the variable name.
func (s *Staged) DropOn(name string) []Change {
	return s.remove(func(c Change) bool { return c.Name == name && !isOutputKind(c.Kind) })
}

// DropCell removes the value and secret changes on one cell.
func (s *Staged) DropCell(name, profile string) []Change {
	return s.remove(func(c Change) bool { return c.Name == name && c.Profile == profile && isCellKind(c.Kind) })
}

// DropKind removes every change of kind on name.
func (s *Staged) DropKind(kind, name string) []Change {
	return s.remove(func(c Change) bool { return c.Kind == kind && c.Name == name })
}

// Clear empties the set, after a save.
func (s *Staged) Clear() { s.list = nil }

// OnOutput returns every change staged on the output identity id.
func (s Staged) OnOutput(id string) []Change {
	var out []Change
	for _, c := range s.list {
		if c.Name == id && isOutputKind(c.Kind) {
			out = append(out, c)
		}
	}
	return out
}

// DropOnOutput removes every change on the output identity id.
func (s *Staged) DropOnOutput(id string) []Change {
	return s.remove(func(c Change) bool { return c.Name == id && isOutputKind(c.Kind) })
}

// AddedOutputs returns every output identity added this session, sorted.
func (s Staged) AddedOutputs() []string {
	var out []string
	for _, c := range s.list {
		if c.Kind == ChangeAddOutput {
			out = append(out, c.Name)
		}
	}
	sort.Strings(out)
	return out
}

// DefaultOutput names the output a staged default-output points at.
func (s Staged) DefaultOutput() (string, bool) {
	for _, c := range s.list {
		if c.Kind == ChangeDefaultOutput {
			return c.Name, true
		}
	}
	return "", false
}

// OutputView is one output as it will read once the staged changes land.
type OutputView struct {
	Output    manifest.Output
	Added     bool
	Removed   bool
	RenamedTo string          // "" when no rename is staged
	Default   bool            // after staged changes
	Changed   map[string]bool // "format", "path", "globs" -> a change is staged
}

// OutputView applies the changes staged on id to base. base's slices are
// copied first: they belong to the loaded manifest.
//
// An add-output resets Output to the added definition, and OnOutput returns
// staged order. An add-output is always staged first for its identity (the
// other kinds only ever act on an output that exists), so a later
// set-globs, set-output-format or set-output-path applies on top of it.
func (s Staged) OutputView(id string, base manifest.Output, diskDefault string) OutputView {
	v := OutputView{Output: base, Changed: map[string]bool{}}
	v.Output.Include = append([]string(nil), base.Include...)
	v.Output.Exclude = append([]string(nil), base.Exclude...)
	for _, c := range s.OnOutput(id) {
		switch c.Kind {
		case ChangeAddOutput:
			v.Added = true
			v.Output = manifest.Output{Format: c.Format, Path: c.Path}
		case ChangeRemoveOutput:
			v.Removed = true
		case ChangeRenameOutput:
			v.RenamedTo = c.Value
		case ChangeSetGlobs:
			v.Output.Include = append([]string(nil), c.Include...)
			v.Output.Exclude = append([]string(nil), c.Exclude...)
			v.Changed["globs"] = true
		case ChangeSetOutputFormat:
			v.Output.Format = c.Value
			v.Changed["format"] = true
		case ChangeSetOutputPath:
			v.Output.Path = c.Value
			v.Changed["path"] = true
		}
	}
	if d, ok := s.DefaultOutput(); ok {
		v.Default = d == id
	} else {
		v.Default = diskDefault == id
	}
	return v
}

// VarView is one variable as it will read once the staged changes land.
// The interface draws it; nothing is written by building one.
type VarView struct {
	Var       manifest.Var
	Added     bool
	Deleted   bool
	Promoted  bool
	NewSecret map[string]bool // profile -> a set-secret is staged
}

// View applies the changes staged on name to base. base is copied first:
// its Profiles map belongs to the loaded manifest and must never change
// before a save.
func (s Staged) View(name string, base manifest.Var) VarView {
	v := VarView{Var: base, NewSecret: map[string]bool{}}
	if base.Profiles != nil {
		v.Var.Profiles = make(map[string]string, len(base.Profiles))
		for p, val := range base.Profiles {
			v.Var.Profiles[p] = val
		}
	}
	for _, c := range s.On(name) {
		switch c.Kind {
		case ChangeAddVar:
			v.Added = true
		case ChangeDeleteVar:
			v.Deleted = true
		case ChangeSetShared:
			val := c.Value
			v.Var.Value = &val
		case ChangeSetValue:
			if v.Var.Profiles == nil {
				v.Var.Profiles = map[string]string{}
			}
			v.Var.Profiles[c.Profile] = c.Value
		case ChangeUnsetValue:
			if c.Profile == "" {
				v.Var.Value = nil
			} else {
				delete(v.Var.Profiles, c.Profile)
			}
		case ChangeSetOptional:
			v.Var.Optional = c.Value == "true"
		case ChangePromoteSecret:
			v.Promoted = true
			v.Var.Secret, v.Var.Value, v.Var.Profiles = true, nil, nil
		case ChangeSetSecret:
			v.NewSecret[c.Profile] = true
		}
	}
	return v
}

// PromotionChanges is what making name a secret stages: one set-secret per
// profile that has a value -- its override, else the shared value -- then
// the promote-secret that flips the manifest entry. Same semantics as
// internal/ui's stagePromotion, so both interfaces promote identically.
func PromotionChanges(name string, v manifest.Var, profiles []string) []Change {
	var out []Change
	for _, p := range profiles {
		value, ok := v.Profiles[p]
		if !ok {
			if v.Value == nil {
				continue
			}
			value = *v.Value
		}
		out = append(out, Change{Kind: ChangeSetSecret, Name: name, Profile: p, Value: value})
	}
	return append(out, Change{Kind: ChangePromoteSecret, Name: name})
}
