package session_test

import (
	"reflect"
	"testing"

	"github.com/cdescobedo/trenvy/internal/manifest"
	"github.com/cdescobedo/trenvy/internal/session"
)

// Review Focus 1: an output and a variable may share a name. Every rule
// keyed by name must look only at its own kind of change.
func TestStagedKeepsVariableAndOutputChangesApart(t *testing.T) {
	var s session.Staged
	s.Add(session.Change{Kind: session.ChangeSetShared, Name: "local", Value: "v"})
	s.Add(session.Change{Kind: session.ChangeSetOutputPath, Name: "local", Value: "x.env"})
	s.Add(session.Change{Kind: session.ChangeDeleteVar, Name: "local"})
	if got := kinds(s.All()); !reflect.DeepEqual(got, []string{"set-output-path:local::x.env", "delete-var:local::"}) {
		t.Fatalf("deleting the variable touched the output: %v", got)
	}
	s.Add(session.Change{Kind: session.ChangeRemoveOutput, Name: "local"})
	if !s.Has(session.ChangeDeleteVar, "local") {
		t.Error("removing the output un-marked the variable's delete")
	}
	if n := len(s.On("local")); n != 1 {
		t.Errorf("On(local) = %d changes, want only the variable's delete", n)
	}
	if n := len(s.OnOutput("local")); n != 1 {
		t.Errorf("OnOutput(local) = %d changes, want only the output's remove", n)
	}
	s.DropOn("local")
	if !s.Has(session.ChangeRemoveOutput, "local") {
		t.Error("DropOn(variable) dropped the output's change")
	}
}

func TestStagedOneDefaultAtATime(t *testing.T) {
	var s session.Staged
	s.Add(session.Change{Kind: session.ChangeDefaultOutput, Name: "local"})
	s.Add(session.Change{Kind: session.ChangeDefaultOutput, Name: "web"})
	if d, ok := s.DefaultOutput(); !ok || d != "web" || s.Len() != 1 {
		t.Errorf("DefaultOutput = %q, %v with %d staged; want web, true, 1", d, ok, s.Len())
	}
}

// Removing an output drops its other staged changes, the same rule F2 gave
// variables; removing one added this session nets to nothing.
func TestStagedRemoveOutputDropsItsOtherChanges(t *testing.T) {
	var s session.Staged
	s.Add(session.Change{Kind: session.ChangeRenameOutput, Name: "local", Value: "root"})
	s.Add(session.Change{Kind: session.ChangeDefaultOutput, Name: "local"})
	s.Add(session.Change{Kind: session.ChangeSetGlobs, Name: "local", Exclude: []string{"A"}})
	s.Add(session.Change{Kind: session.ChangeRemoveOutput, Name: "local"})
	if got := kinds(s.All()); !reflect.DeepEqual(got, []string{"remove-output:local::"}) {
		t.Errorf("left = %v, want only the remove", got)
	}

	var a session.Staged
	a.Add(session.Change{Kind: session.ChangeAddOutput, Name: "web", Format: "json", Path: "web.json"})
	a.Add(session.Change{Kind: session.ChangeSetGlobs, Name: "web", Include: []string{"PUBLIC_*"}})
	a.Add(session.Change{Kind: session.ChangeRemoveOutput, Name: "web"})
	if a.Len() != 0 {
		t.Errorf("an output added and removed in one session left %v", kinds(a.All()))
	}
}

func TestStagedEditOnARemovedOutputRestoresIt(t *testing.T) {
	var s session.Staged
	s.Add(session.Change{Kind: session.ChangeRemoveOutput, Name: "local"})
	s.Add(session.Change{Kind: session.ChangeSetOutputPath, Name: "local", Value: "x.env"})
	if s.Has(session.ChangeRemoveOutput, "local") {
		t.Error("an edit did not cancel the pending remove")
	}
}

func TestStagedDropOfAnAddedOutputDropsItsDependents(t *testing.T) {
	var s session.Staged
	s.Add(session.Change{Kind: session.ChangeAddOutput, Name: "web", Format: "json", Path: "web.json"})
	s.Add(session.Change{Kind: session.ChangeDefaultOutput, Name: "web"})
	s.Add(session.Change{Kind: session.ChangeAddVar, Name: "web"})
	if dropped := s.Drop(0); len(dropped) != 2 {
		t.Errorf("Drop(add-output) = %v, want it and its default", kinds(dropped))
	}
	if !s.Has(session.ChangeAddVar, "web") {
		t.Error("dropping the output dropped a variable that shares its name")
	}
}

func TestOutputViewAppliesStagedChanges(t *testing.T) {
	base := manifest.Output{Format: "dotenv", Path: ".env", Include: []string{"*"}}
	var s session.Staged
	s.Add(session.Change{Kind: session.ChangeRenameOutput, Name: "local", Value: "root"})
	s.Add(session.Change{Kind: session.ChangeSetOutputFormat, Name: "local", Value: "json"})
	s.Add(session.Change{Kind: session.ChangeSetGlobs, Name: "local", Exclude: []string{"A"}})
	v := s.OutputView("local", base, "local")
	if v.RenamedTo != "root" || v.Output.Format != "json" || v.Output.Path != ".env" || !v.Default {
		t.Errorf("view = %+v", v)
	}
	if !v.Changed["format"] || !v.Changed["globs"] || v.Changed["path"] {
		t.Errorf("Changed = %v", v.Changed)
	}
	if !reflect.DeepEqual(base.Include, []string{"*"}) {
		t.Error("OutputView mutated its base")
	}
	s.Add(session.Change{Kind: session.ChangeDefaultOutput, Name: "web"})
	if s.OutputView("local", base, "local").Default {
		t.Error("a staged default elsewhere left local marked default")
	}
	add := s.OutputView("web", manifest.Output{}, "local")
	if add.Added || !add.Default {
		t.Errorf("web view = %+v (not added, but the staged default)", add)
	}
}

func TestAddedOutputs(t *testing.T) {
	var s session.Staged
	s.Add(session.Change{Kind: session.ChangeAddOutput, Name: "zed", Format: "json", Path: "z.json"})
	s.Add(session.Change{Kind: session.ChangeAddOutput, Name: "alpha", Format: "dotenv", Path: "a.env"})
	if got := s.AddedOutputs(); !reflect.DeepEqual(got, []string{"alpha", "zed"}) {
		t.Errorf("AddedOutputs = %v", got)
	}
}
