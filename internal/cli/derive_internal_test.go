package cli

import (
	"testing"

	"github.com/cdescobedo/trenvy/internal/manifest"
)

// The rows below are the spec's table, and were produced by running the
// algorithm rather than by reasoning about it.
func TestDeriveOutputName(t *testing.T) {
	cases := []struct{ rel, format, want string }{
		{"src/VerityPortal.Api/.env_example", "dotenv", "api"},
		{".env_example", "dotenv", "root"},
		{"public/env.json.example", "json", "public"},
		{"config/.env.example", "dotenv", "config"},
		{"weird..//.env_example", "dotenv", "dotenv"},
		{"my.app.v2/.env", "dotenv", "v2"},
	}
	for _, c := range cases {
		if got := deriveOutputName(c.rel, c.format, nil); got != c.want {
			t.Errorf("deriveOutputName(%q, %q) = %q, want %q", c.rel, c.format, got, c.want)
		}
	}
}

func TestDeriveOutputNameSuffixesOnCollision(t *testing.T) {
	taken := map[string]manifest.Output{"api": {}, "api-2": {}}
	if got := deriveOutputName("src/Foo.Api/.env", "dotenv", taken); got != "api-3" {
		t.Errorf("got %q, want api-3", got)
	}
}

func TestSelectsSummary(t *testing.T) {
	cases := []struct {
		o    manifest.Output
		want string
	}{
		{manifest.Output{}, "all"},
		{manifest.Output{Include: []string{"*"}}, "all"},
		{manifest.Output{Include: []string{"*"}, Exclude: []string{"A", "B"}}, "all except A, B"},
		{manifest.Output{Include: []string{"A", "B"}}, "A, B"},
		{manifest.Output{Include: []string{"PUBLIC_*"}, Exclude: []string{"PUBLIC_SECRET"}}, "PUBLIC_* except PUBLIC_SECRET"},
	}
	for _, c := range cases {
		if got := selectsSummary(c.o); got != c.want {
			t.Errorf("selectsSummary(%+v) = %q, want %q", c.o, got, c.want)
		}
	}
}
