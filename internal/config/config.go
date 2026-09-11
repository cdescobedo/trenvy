package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// DefaultServicePattern namespaces every item envctl stores under envctl/, so
// the tool never reads or writes a keyring item it did not create.
const DefaultServicePattern = "envctl/{project}/{name}"

// Config is the global settings file.
type Config struct {
	Keyring KeyringConfig `toml:"keyring"`
}

// KeyringConfig controls how a (project, variable) pair maps to a keyring
// service name. It is configurable so that an existing layout can be adopted
// deliberately, but the default is envctl's own namespace.
type KeyringConfig struct {
	Service string `toml:"service,omitempty"`
}

// ServiceName renders the service name for one variable. The zero value of
// KeyringConfig behaves as the default pattern.
func (k KeyringConfig) ServiceName(project, name string) string {
	pattern := k.Service
	if pattern == "" {
		pattern = DefaultServicePattern
	}
	r := strings.NewReplacer("{project}", project, "{name}", name)
	return r.Replace(pattern)
}

// Load reads config.toml, returning defaults when the file does not exist.
func Load() (*Config, error) {
	data, err := os.ReadFile(ConfigPath())
	if errors.Is(err, fs.ErrNotExist) {
		return &Config{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", ConfigPath(), err)
	}
	var cfg Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", ConfigPath(), err)
	}
	return &cfg, nil
}
