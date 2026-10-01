package session_test

import (
	"reflect"
	"testing"

	"github.com/cdescobedo/trenvy/internal/manifest"
	"github.com/cdescobedo/trenvy/internal/session"
)

func kinds(cs []session.Change) []string {
	var out []string
	for _, c := range cs {
		out = append(out, c.Kind+":"+c.Name+":"+c.Profile+":"+c.Value)
	}
	return out
}

// Editing the same cell twice keeps one change, the latest: the review lists
// what will happen, not the history of keypresses.
func TestStagedAddReplacesAChangeToTheSameCell(t *testing.T) {
	var s session.Staged
	s.Add(session.Change{Kind: session.ChangeSetValue, Name: "A", Profile: "prod", Value: "1"})
	s.Add(session.Change{Kind: session.ChangeSetValue, Name: "A", Profile: "dev", Value: "2"})
	s.Add(session.Change{Kind: session.ChangeUnsetValue, Name: "A", Profile: "prod"})
	got := kinds(s.All())
	want := []string{"unset-value:A:prod:", "set-value:A:dev:2"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("All() = %v, want %v", got, want)
	}
}

// The shared value and a profile's override are different cells.
func TestStagedSharedAndOverrideAreSeparateCells(t *testing.T) {
	var s session.Staged
	s.Add(session.Change{Kind: session.ChangeSetShared, Name: "A", Value: "x"})
	s.Add(session.Change{Kind: session.ChangeSetValue, Name: "A", Profile: "dev", Value: "y"})
	if s.Len() != 2 {
		t.Errorf("Len() = %d, want 2: %v", s.Len(), kinds(s.All()))
	}
	if n := len(s.OnCell("A", "")); n != 1 {
		t.Errorf("OnCell(A, shared) = %d changes, want 1", n)
	}
}

// Dropping a staged add-var drops everything staged on that variable: the
// edits have nothing to land on without it (spec §3).
func TestStagedDropOfAnAdditionDropsItsDependents(t *testing.T) {
	var s session.Staged
	s.Add(session.Change{Kind: session.ChangeSetShared, Name: "OTHER", Value: "o"})
	s.Add(session.Change{Kind: session.ChangeAddVar, Name: "NEW"})
	s.Add(session.Change{Kind: session.ChangeSetShared, Name: "NEW", Value: "v"})
	dropped := s.Drop(1)
	if len(dropped) != 2 || dropped[0].Kind != session.ChangeAddVar {
		t.Fatalf("Drop(1) = %v, want the add-var first, then its set-shared", kinds(dropped))
	}
	if got := kinds(s.All()); !reflect.DeepEqual(got, []string{"set-shared:OTHER::o"}) {
		t.Errorf("left = %v, want only OTHER's change", got)
	}
}

// Dropping a promotion drops the set-secrets that move its values: without
// the flip, they would write keyring items for a variable that stays plain.
func TestStagedDropOfAPromotionDropsItsSecretWrites(t *testing.T) {
	var s session.Staged
	for _, c := range session.PromotionChanges("A", manifest.Var{Value: ptr("v")}, []string{"dev", "prod"}) {
		s.Add(c)
	}
	idx := -1
	for i, c := range s.All() {
		if c.Kind == session.ChangePromoteSecret {
			idx = i
		}
	}
	if dropped := s.Drop(idx); len(dropped) != 3 {
		t.Errorf("Drop(promote) removed %v, want the promotion and both set-secrets", kinds(dropped))
	}
	if s.Len() != 0 {
		t.Errorf("left = %v, want nothing", kinds(s.All()))
	}
}

// Deleting a variable that was only added this session nets to nothing.
func TestStagedDeleteOfAnAddedVariableRemovesEverything(t *testing.T) {
	var s session.Staged
	s.Add(session.Change{Kind: session.ChangeAddVar, Name: "NEW"})
	s.Add(session.Change{Kind: session.ChangeSetShared, Name: "NEW", Value: "v"})
	s.Add(session.Change{Kind: session.ChangeDeleteVar, Name: "NEW"})
	if s.Len() != 0 {
		t.Errorf("left = %v, want nothing", kinds(s.All()))
	}
}

// Deleting a variable drops every other change staged on it, so commit
// never receives a delete alongside a change that would write for the same
// name -- a promotion's set-secrets would store keyring items the delete
// then leaves nothing in the manifest to find.
func TestStagedDeleteDropsEveryOtherChangeOnTheVariable(t *testing.T) {
	var s session.Staged
	s.Add(session.Change{Kind: session.ChangeSetShared, Name: "OTHER", Value: "o"})
	for _, c := range session.PromotionChanges("A", manifest.Var{Value: ptr("v")}, []string{"dev", "prod"}) {
		s.Add(c)
	}
	s.Add(session.Change{Kind: session.ChangeSetOptional, Name: "A", Value: "true"})
	s.Add(session.Change{Kind: session.ChangeDeleteVar, Name: "A"})
	want := []string{"set-shared:OTHER::o", "delete-var:A::"}
	if got := kinds(s.All()); !reflect.DeepEqual(got, want) {
		t.Errorf("All() = %v, want %v", got, want)
	}
}

// Editing a variable marked for deletion un-marks it -- the same rule
// internal/ui's beginEdit holds, so commit never sees both on one name.
func TestStagedEditCancelsAPendingDelete(t *testing.T) {
	var s session.Staged
	s.Add(session.Change{Kind: session.ChangeDeleteVar, Name: "A"})
	s.Add(session.Change{Kind: session.ChangeSetShared, Name: "A", Value: "v"})
	if s.Has(session.ChangeDeleteVar, "A") {
		t.Error("delete-var survived an edit to the same variable")
	}
}

func TestStagedViewAppliesChangesWithoutMutatingTheBase(t *testing.T) {
	base := manifest.Var{Value: ptr("development"), Profiles: map[string]string{"prod": "production"}}
	var s session.Staged
	s.Add(session.Change{Kind: session.ChangeSetShared, Name: "A", Value: "dev2"})
	s.Add(session.Change{Kind: session.ChangeSetValue, Name: "A", Profile: "staging", Value: "stg"})
	s.Add(session.Change{Kind: session.ChangeUnsetValue, Name: "A", Profile: "prod"})
	s.Add(session.Change{Kind: session.ChangeSetOptional, Name: "A", Value: "true"})

	v := s.View("A", base)
	if *v.Var.Value != "dev2" || v.Var.Profiles["staging"] != "stg" || !v.Var.Optional {
		t.Errorf("View = %+v", v.Var)
	}
	if _, ok := v.Var.Profiles["prod"]; ok {
		t.Error("prod override survived unset-value")
	}
	if *base.Value != "development" || base.Profiles["prod"] != "production" || len(base.Profiles) != 1 {
		t.Errorf("View mutated its base: %+v", base)
	}
}

func TestStagedViewMarksPromotionAndNewSecrets(t *testing.T) {
	var s session.Staged
	for _, c := range session.PromotionChanges("A", manifest.Var{Value: ptr("v")}, []string{"dev"}) {
		s.Add(c)
	}
	v := s.View("A", manifest.Var{Value: ptr("v")})
	if !v.Promoted || !v.Var.Secret || v.Var.Value != nil || !v.NewSecret["dev"] {
		t.Errorf("View = %+v", v)
	}
}

// Same semantics as internal/ui's stagePromotion: each profile's override,
// else the shared value, else nothing for that profile.
func TestPromotionChangesUseOverrideThenShared(t *testing.T) {
	v := manifest.Var{Value: ptr("shared"), Profiles: map[string]string{"prod": "p"}}
	got := kinds(session.PromotionChanges("A", v, []string{"dev", "prod"}))
	want := []string{"set-secret:A:dev:shared", "set-secret:A:prod:p", "promote-secret:A::"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	onlyProd := kinds(session.PromotionChanges("B", manifest.Var{Profiles: map[string]string{"prod": "p"}}, []string{"dev", "prod"}))
	if !reflect.DeepEqual(onlyProd, []string{"set-secret:B:prod:p", "promote-secret:B::"}) {
		t.Errorf("overrides only = %v", onlyProd)
	}
}

func TestStagedAddedListsNewNamesSorted(t *testing.T) {
	var s session.Staged
	s.Add(session.Change{Kind: session.ChangeAddVar, Name: "ZED"})
	s.Add(session.Change{Kind: session.ChangeAddVar, Name: "ALPHA"})
	if got := s.Added(); !reflect.DeepEqual(got, []string{"ALPHA", "ZED"}) {
		t.Errorf("Added() = %v", got)
	}
}

func ptr(v string) *string { return &v }
