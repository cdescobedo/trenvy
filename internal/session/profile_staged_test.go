package session

import (
	"reflect"
	"testing"
)

func TestProfileKindsAreTheirOwnNamespace(t *testing.T) {
	var s Staged
	// A variable and a profile may share a name.
	s.Add(Change{Kind: ChangeSetShared, Name: "dev", Value: "x"})
	s.Add(Change{Kind: ChangeRemoveProfile, Name: "dev"})
	if got := len(s.On("dev")); got != 1 {
		t.Errorf("On(dev) = %d changes, want only the variable's 1", got)
	}
	if got := len(s.OnProfile("dev")); got != 1 {
		t.Errorf("OnProfile(dev) = %d, want 1", got)
	}
	s.Add(Change{Kind: ChangeDeleteVar, Name: "dev"})
	if !s.Has(ChangeRemoveProfile, "dev") {
		t.Error("deleting the variable dev dropped the profile dev's removal")
	}
}

func TestDefaultAndUseProfileHoldOneSlotEach(t *testing.T) {
	var s Staged
	s.Add(Change{Kind: ChangeDefaultProfile, Name: "staging"})
	s.Add(Change{Kind: ChangeDefaultProfile, Name: "prod"})
	s.Add(Change{Kind: ChangeUseProfile, Name: "staging"})
	s.Add(Change{Kind: ChangeUseProfile, Name: "prod"})
	if d, _ := s.DefaultProfile(); d != "prod" {
		t.Errorf("DefaultProfile = %q, want prod", d)
	}
	if u, _ := s.UseProfile(); u != "prod" {
		t.Errorf("UseProfile = %q, want prod", u)
	}
	if s.Len() != 2 {
		t.Errorf("Len = %d, want 2", s.Len())
	}
}

func TestRemovingAProfileDropsItsStagedCells(t *testing.T) {
	var s Staged
	s.Add(Change{Kind: ChangeSetValue, Name: "APP_MODE", Profile: "prod", Value: "p"})
	s.Add(Change{Kind: ChangeSetSecret, Name: "TOKEN", Profile: "prod", Value: "s"})
	s.Add(Change{Kind: ChangeSetValue, Name: "APP_MODE", Profile: "dev", Value: "d"})
	if got := len(s.CellsOn("prod")); got != 2 {
		t.Fatalf("CellsOn(prod) = %d, want 2", got)
	}
	s.Add(Change{Kind: ChangeRemoveProfile, Name: "prod"})
	if got := len(s.CellsOn("prod")); got != 0 {
		t.Errorf("CellsOn(prod) after removal = %d, want 0", got)
	}
	if got := len(s.CellsOn("dev")); got != 1 {
		t.Errorf("dev's cell was dropped too: CellsOn(dev) = %d", got)
	}
	// Undoing the removal does not bring the values back (ruling 2).
	s.DropOnProfile("prod")
	if s.Len() != 1 {
		t.Errorf("Len = %d after undo, want only dev's value", s.Len())
	}
}

// Removing a profile clears a use or a default staged on it: neither may
// name a profile the batch removes.
func TestRemovingAProfileClearsAUseOrDefaultOnIt(t *testing.T) {
	var s Staged
	s.Add(Change{Kind: ChangeUseProfile, Name: "prod"})
	s.Add(Change{Kind: ChangeDefaultProfile, Name: "prod"})
	s.Add(Change{Kind: ChangeRemoveProfile, Name: "prod"})
	if u, ok := s.UseProfile(); ok {
		t.Errorf("UseProfile = %q after removing prod, want none", u)
	}
	if d, ok := s.DefaultProfile(); ok {
		t.Errorf("DefaultProfile = %q after removing prod, want none", d)
	}
	if got := s.All(); len(got) != 1 || got[0].Kind != ChangeRemoveProfile {
		t.Errorf("staged = %+v, want only the removal", got)
	}
}

func TestRemovingAnAddedProfileNetsToNothing(t *testing.T) {
	var s Staged
	s.Add(Change{Kind: ChangeAddProfile, Name: "qa"})
	s.Add(Change{Kind: ChangeDefaultProfile, Name: "qa"})
	s.Add(Change{Kind: ChangeRemoveProfile, Name: "qa"})
	if s.Len() != 0 {
		t.Errorf("staged = %+v, want nothing", s.All())
	}
}

func TestAnotherChangeOnARemovedProfileRestoresIt(t *testing.T) {
	var s Staged
	s.Add(Change{Kind: ChangeRemoveProfile, Name: "prod"})
	s.Add(Change{Kind: ChangeDefaultProfile, Name: "prod"})
	if s.Has(ChangeRemoveProfile, "prod") {
		t.Error("making prod the default left it marked for removal")
	}
}

func TestDroppingAnAddedProfileDropsWhatPointsAtIt(t *testing.T) {
	var s Staged
	s.Add(Change{Kind: ChangeAddProfile, Name: "qa"})
	s.Add(Change{Kind: ChangeDefaultProfile, Name: "qa"})
	s.Add(Change{Kind: ChangeSetShared, Name: "qa", Value: "a variable named qa"})
	dropped := s.Drop(0)
	if len(dropped) != 2 {
		t.Errorf("dropped = %+v, want the add and the default", dropped)
	}
	if got := s.All(); len(got) != 1 || got[0].Kind != ChangeSetShared {
		t.Errorf("left = %+v, want only the variable's change", got)
	}
}

func TestProfileView(t *testing.T) {
	var s Staged
	if got := s.ProfileView("dev", "dev", ""); !reflect.DeepEqual(got, ProfileView{Default: true}) {
		t.Errorf("disk default = %+v", got)
	}
	s.Add(Change{Kind: ChangeAddProfile, Name: "qa"})
	s.Add(Change{Kind: ChangeDefaultProfile, Name: "qa"})
	s.Add(Change{Kind: ChangeUseProfile, Name: "prod"})
	s.Add(Change{Kind: ChangeRemoveProfile, Name: "staging"})
	cases := map[string]ProfileView{
		"dev":     {},
		"qa":      {Added: true, Default: true},
		"prod":    {Active: true},
		"staging": {Removed: true},
	}
	for p, want := range cases {
		if got := s.ProfileView(p, "dev", "staging"); !reflect.DeepEqual(got, want) {
			t.Errorf("ProfileView(%s) = %+v, want %+v", p, got, want)
		}
	}
	if got := s.AddedProfiles(); !reflect.DeepEqual(got, []string{"qa"}) {
		t.Errorf("AddedProfiles = %v", got)
	}
}
