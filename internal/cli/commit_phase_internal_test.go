package cli

import (
	"io"
	"testing"

	"github.com/cdescobedo/trenvy/internal/config"
	"github.com/cdescobedo/trenvy/internal/manifest"
	"github.com/cdescobedo/trenvy/internal/store"
	"github.com/cdescobedo/trenvy/internal/ui"
)

// structuralWitnessStore watches, at the moment its own Set is called,
// whether outputName has already landed in the SAME *manifest.Manifest
// commitStaged is mutating. This is a white-box test (package cli, not
// cli_test) because ordering rule 1 -- structural additions before keyring
// writes -- has no black-box consequence yet: add-output only ever inserts
// a brand new map key, and a keyring write never reads or depends on
// app.Manifest at all, so nothing observable from outside this package
// would differ if the two phases ran in the opposite order today. The rule
// is here for what Task 8 adds next (set-globs will need its output to
// already exist in the map), so this probe is what pins the order down now,
// before a kind exists that would actually break if it were wrong.
type structuralWitnessStore struct {
	*store.Mem
	manifest             *manifest.Manifest
	outputName           string
	sawOutputBeforeWrite bool
	checked              bool
}

func (s *structuralWitnessStore) Set(service, value string) error {
	if !s.checked {
		_, s.sawOutputBeforeWrite = s.manifest.Outputs[s.outputName]
		s.checked = true
	}
	return s.Mem.Set(service, value)
}

// TestCommitStagedAppliesStructuralAdditionsBeforeKeyringWrites calls
// commitStaged directly with a hand-built []ui.Change, exactly the seam
// deletedNames' own doc already leans on ("a Change slice is a plain
// exported value, and this function must not trust that every caller built
// one the only way internal/ui's keys currently allow"). The slice lists
// the secret promotion BEFORE the output addition -- the opposite of
// staged order a real session would produce if a user promoted first and
// added the output second -- to prove the result does not depend on that
// order, only on commitStaged's own phase order.
func TestCommitStagedAppliesStructuralAdditionsBeforeKeyringWrites(t *testing.T) {
	t.Setenv("TRENVY_CONFIG_DIR", t.TempDir())
	root := t.TempDir()

	m, err := manifest.Parse([]byte(`
[project]
id = "vpb"
profiles = ["dev"]
default_profile = "dev"
salt = "abc123"

[vars.APP_URL]
value = "http://+:8080"
`))
	if err != nil {
		t.Fatal(err)
	}

	reg, err := config.LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Add(config.Entry{ID: "vpb", Root: root}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Save(); err != nil {
		t.Fatal(err)
	}
	if err := manifest.Save(config.ManifestPath("vpb"), m); err != nil {
		t.Fatal(err)
	}

	witness := &structuralWitnessStore{Mem: store.NewMem(), outputName: "public"}
	opts := Options{
		Dir:    root,
		Stdout: io.Discard,
		NewStore: func(*config.Config, string) (store.Store, error) {
			return witness, nil
		},
	}

	app, err := opts.resolveApp()
	if err != nil {
		t.Fatal(err)
	}
	witness.manifest = app.Manifest

	staged := []ui.Change{
		// A genuine promotion: the value first, per profile, then the
		// row-level flip -- mirrors stagePromotion's own shape.
		{Kind: ui.ChangeSetSecret, Name: "APP_URL", Profile: "dev", Value: "http://+:8080"},
		{Kind: ui.ChangePromoteSecret, Name: "APP_URL"},
		{Kind: ui.ChangeAddOutput, Name: "public", Format: "dotenv", Path: "public/.env"},
	}

	if err := commitStaged(&opts, app, staged); err != nil {
		t.Fatalf("commitStaged: %v", err)
	}

	if !witness.sawOutputBeforeWrite {
		t.Error("the keyring write happened before the structural addition landed in the manifest")
	}
}
