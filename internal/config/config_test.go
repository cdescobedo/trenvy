package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cdescobedo/envctl/internal/config"
)

func TestDirPrefersEnvctlConfigDir(t *testing.T) {
	t.Setenv("ENVCTL_CONFIG_DIR", "/tmp/explicit")
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg")
	if got := config.Dir(); got != "/tmp/explicit" {
		t.Errorf("Dir() = %q, want /tmp/explicit", got)
	}
}

func TestDirFallsBackToXDG(t *testing.T) {
	t.Setenv("ENVCTL_CONFIG_DIR", "")
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg")
	if got := config.Dir(); got != filepath.Join("/tmp/xdg", "envctl") {
		t.Errorf("Dir() = %q", got)
	}
}

func TestDirFallsBackToHome(t *testing.T) {
	t.Setenv("ENVCTL_CONFIG_DIR", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	if got := config.Dir(); got != filepath.Join(home, ".config", "envctl") {
		t.Errorf("Dir() = %q", got)
	}
}

func TestPathHelpers(t *testing.T) {
	t.Setenv("ENVCTL_CONFIG_DIR", "/cfg")
	cases := map[string]string{
		config.ConfigPath():                    "/cfg/config.toml",
		config.RegistryPath():                  "/cfg/registry.toml",
		config.ManifestPath("demo-project"):   "/cfg/projects/demo-project.toml",
		config.StatePath("demo-project"):      "/cfg/state/demo-project.toml",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
}

func TestServiceNameUsesDefaultPattern(t *testing.T) {
	var k config.KeyringConfig // zero value must behave as the default
	got := k.ServiceName("demo-backend", "ALPHA_TOKEN")
	want := "envctl/demo-backend/ALPHA_TOKEN"
	if got != want {
		t.Errorf("ServiceName = %q, want %q", got, want)
	}
}

func TestServiceNameHonoursCustomPattern(t *testing.T) {
	k := config.KeyringConfig{Service: "{project}/{name}"}
	got := k.ServiceName("p", "TOKEN")
	if got != "p/TOKEN" {
		t.Errorf("ServiceName = %q, want p/TOKEN", got)
	}
}

func TestLoadReturnsDefaultsWhenFileMissing(t *testing.T) {
	t.Setenv("ENVCTL_CONFIG_DIR", t.TempDir())
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Keyring.ServiceName("p", "N"); got != "envctl/p/N" {
		t.Errorf("default ServiceName = %q", got)
	}
}

func TestLoadReadsCustomPattern(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ENVCTL_CONFIG_DIR", dir)
	body := "[keyring]\nservice = \"custom/{name}\"\n"
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Keyring.ServiceName("p", "N"); got != "custom/N" {
		t.Errorf("ServiceName = %q, want custom/N", got)
	}
}
