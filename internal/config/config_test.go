package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cdescobedo/trenvy/internal/config"
)

func TestDirPrefersTrenvyConfigDir(t *testing.T) {
	t.Setenv("TRENVY_CONFIG_DIR", "/tmp/explicit")
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg")
	if got := config.Dir(); got != "/tmp/explicit" {
		t.Errorf("Dir() = %q, want /tmp/explicit", got)
	}
}

func TestDirFallsBackToXDG(t *testing.T) {
	t.Setenv("TRENVY_CONFIG_DIR", "")
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg")
	if got := config.Dir(); got != filepath.Join("/tmp/xdg", "trenvy") {
		t.Errorf("Dir() = %q", got)
	}
}

func TestDirFallsBackToHome(t *testing.T) {
	t.Setenv("TRENVY_CONFIG_DIR", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	if got := config.Dir(); got != filepath.Join(home, ".config", "trenvy") {
		t.Errorf("Dir() = %q", got)
	}
}

func TestPathHelpers(t *testing.T) {
	t.Setenv("TRENVY_CONFIG_DIR", "/cfg")
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"ConfigPath", config.ConfigPath(), "/cfg/config.toml"},
		{"RegistryPath", config.RegistryPath(), "/cfg/registry.toml"},
		{"ManifestPath", config.ManifestPath("demo-project"), "/cfg/projects/demo-project.toml"},
		{"StatePath", config.StatePath("demo-project"), "/cfg/state/demo-project.toml"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("got %q, want %q", tc.got, tc.want)
			}
		})
	}
}

func TestServiceNameUsesDefaultPattern(t *testing.T) {
	var k config.KeyringConfig // zero value must behave as the default
	got := k.ServiceName("demo-backend", "dev", "ALPHA_TOKEN")
	want := "trenvy/demo-backend/dev/ALPHA_TOKEN"
	if got != want {
		t.Errorf("ServiceName = %q, want %q", got, want)
	}
}

func TestServiceNameHonoursCustomPattern(t *testing.T) {
	k := config.KeyringConfig{Service: "{project}/{profile}/{name}"}
	got := k.ServiceName("p", "dev", "TOKEN")
	if got != "p/dev/TOKEN" {
		t.Errorf("ServiceName = %q, want p/dev/TOKEN", got)
	}
}

func TestLoadReturnsDefaultsWhenFileMissing(t *testing.T) {
	t.Setenv("TRENVY_CONFIG_DIR", t.TempDir())
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Keyring.ServiceName("p", "dev", "N"); got != "trenvy/p/dev/N" {
		t.Errorf("default ServiceName = %q", got)
	}
}

func TestLoadReadsCustomPattern(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TRENVY_CONFIG_DIR", dir)
	body := "[keyring]\nservice = \"custom/{profile}/{name}\"\n"
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Keyring.ServiceName("p", "dev", "N"); got != "custom/dev/N" {
		t.Errorf("ServiceName = %q, want custom/dev/N", got)
	}
}

func TestServiceNameIncludesTheProfile(t *testing.T) {
	var k config.KeyringConfig

	got := k.ServiceName("vpb", "prod", "API_KEY")

	if want := "trenvy/vpb/prod/API_KEY"; got != want {
		t.Errorf("ServiceName = %q, want %q", got, want)
	}
}

func TestServiceNameHonoursACustomPattern(t *testing.T) {
	k := config.KeyringConfig{Service: "me:{project}:{profile}:{name}"}

	got := k.ServiceName("vpb", "dev", "API_KEY")

	if want := "me:vpb:dev:API_KEY"; got != want {
		t.Errorf("ServiceName = %q, want %q", got, want)
	}
}

// A pattern without {profile} would map every profile's secret onto one
// keyring item, so switching profiles would silently read the wrong value.
// That has to fail at load, not at read.
func TestKeyringConfigRejectsAPatternWithoutAProfile(t *testing.T) {
	k := config.KeyringConfig{Service: "trenvy/{project}/{name}"}

	err := k.Validate()

	if err == nil || !strings.Contains(err.Error(), "{profile}") {
		t.Errorf("error = %v, want a complaint about {profile}", err)
	}
}

func TestKeyringConfigAcceptsTheZeroValue(t *testing.T) {
	var k config.KeyringConfig
	if err := k.Validate(); err != nil {
		t.Errorf("Validate on the default pattern: %v", err)
	}
}
