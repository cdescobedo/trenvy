package manifest_test

import (
	"bytes"
	"testing"

	"github.com/cdescobedo/trenvy/internal/manifest"
)

func TestCloneIsEqualAndIndependent(t *testing.T) {
	m, err := manifest.Parse([]byte(`
[project]
id = "vpb"
profiles = ["dev", "prod"]
default_profile = "dev"
salt = "abc"

[outputs.local]
format = "dotenv"
path = ".env"

[vars.A]
value = "1"
profiles = { prod = "2" }
`))
	if err != nil {
		t.Fatal(err)
	}
	c, err := m.Clone()
	if err != nil {
		t.Fatal(err)
	}
	a, _ := m.Encode()
	b, _ := c.Encode()
	if !bytes.Equal(a, b) {
		t.Fatalf("clone encodes differently:\n%s\n---\n%s", a, b)
	}
	c.Vars["A"].Profiles["prod"] = "changed"
	c.Vars["B"] = manifest.Var{}
	*c.Vars["A"].Value = "changed"
	if m.Vars["A"].Profiles["prod"] != "2" || *m.Vars["A"].Value != "1" {
		t.Error("mutating the clone's var changed the original")
	}
	if _, ok := m.Vars["B"]; ok {
		t.Error("adding to the clone's vars added to the original")
	}
}
