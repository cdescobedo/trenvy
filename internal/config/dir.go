// Package config owns everything under the envctl config directory: global
// settings, the project registry, and per-project render state.
package config

import (
	"os"
	"path/filepath"
)

// Dir returns the envctl configuration directory.
//
// ENVCTL_CONFIG_DIR takes precedence over everything, which is what makes the
// whole test suite hermetic: a test points it at t.TempDir() and no real
// configuration is ever read or written.
func Dir() string {
	if d := os.Getenv("ENVCTL_CONFIG_DIR"); d != "" {
		return d
	}
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "envctl")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".envctl"
	}
	return filepath.Join(home, ".config", "envctl")
}

// ConfigPath is the global settings file.
func ConfigPath() string { return filepath.Join(Dir(), "config.toml") }

// RegistryPath maps project roots to project ids.
func RegistryPath() string { return filepath.Join(Dir(), "registry.toml") }

// ManifestPath is one project's manifest.
func ManifestPath(id string) string { return filepath.Join(Dir(), "projects", id+".toml") }

// StatePath is one project's render bookkeeping.
func StatePath(id string) string { return filepath.Join(Dir(), "state", id+".toml") }
