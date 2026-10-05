package cli

import (
	"io"
	"strings"
	"testing"

	"github.com/cdescobedo/trenvy/internal/config"
	"github.com/cdescobedo/trenvy/internal/manifest"
	"github.com/cdescobedo/trenvy/internal/session"
	"github.com/cdescobedo/trenvy/internal/store"
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
// commitStaged directly with a hand-built []session.Change, exactly the seam
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

	staged := []session.Change{
		// A genuine promotion: the value first, per profile, then the
		// row-level flip -- mirrors stagePromotion's own shape.
		{Kind: session.ChangeSetSecret, Name: "APP_URL", Profile: "dev", Value: "http://+:8080"},
		{Kind: session.ChangePromoteSecret, Name: "APP_URL"},
		{Kind: session.ChangeAddOutput, Name: "public", Format: "dotenv", Path: "public/.env"},
	}

	if _, err := commitStaged(&opts, app, staged); err != nil {
		t.Fatalf("commitStaged: %v", err)
	}

	if !witness.sawOutputBeforeWrite {
		t.Error("the keyring write happened before the structural addition landed in the manifest")
	}
}

// Fix 4 (Important, whole-branch review) taught internal/ui's own add-output
// wizard to refuse an illegal format live, at zero cost -- so a real
// `trenvy ui` session can no longer stage one. But commitStaged must still
// refuse it too: a []session.Change is a plain exported value (see this file's
// own doc above, and deletedNames' doc in ui.go), and nothing stops a
// caller other than the wizard from building one directly. This is a
// white-box test (package cli) precisely because black-box coverage of this
// exact path -- staging an illegal format through the wizard's own keys --
// is no longer reachable now that the wizard refuses it first; it moved
// here rather than disappearing.
func TestCommitStagedRefusesAnIllegalOutputFormatFromAHandBuiltChange(t *testing.T) {
	t.Setenv("TRENVY_CONFIG_DIR", t.TempDir())
	root := t.TempDir()

	m, err := manifest.Parse([]byte(`
[project]
id = "vpb"
profiles = ["dev"]
default_profile = "dev"
salt = "abc123"
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

	opts := Options{
		Dir:    root,
		Stdout: io.Discard,
		NewStore: func(*config.Config, string) (store.Store, error) {
			return store.NewMem(), nil
		},
	}
	app, err := opts.resolveApp()
	if err != nil {
		t.Fatal(err)
	}

	staged := []session.Change{
		{Kind: session.ChangeAddOutput, Name: "public", Format: "yaml", Path: "public/env.yaml"},
	}
	_, err = commitStaged(&opts, app, staged)
	if err == nil {
		t.Fatal("commitStaged succeeded with an illegal output format")
	}
	if !strings.Contains(err.Error(), `"dotenv"`) || !strings.Contains(err.Error(), `"json"`) {
		t.Errorf("error does not name the legal formats: %v", err)
	}

	got, loadErr := manifest.Load(config.ManifestPath("vpb"))
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if _, ok := got.Outputs["public"]; ok {
		t.Error("the invalid output was saved anyway")
	}
}
