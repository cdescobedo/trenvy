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
// the tool never reads or writes a keyring item it did not create. The
// profile segment is what keeps one variable's dev and prod credentials in
// separate items.
const DefaultServicePattern = "envctl/{project}/{profile}/{name}"

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

// ServiceName renders the service name for one variable in one profile. The
// zero value of KeyringConfig behaves as the default pattern.
func (k KeyringConfig) ServiceName(project, profile, name string) string {
	pattern := k.Service
	if pattern == "" {
		pattern = DefaultServicePattern
	}
	r := strings.NewReplacer("{project}", project, "{profile}", profile, "{name}", name)
	return r.Replace(pattern)
}

// Validate rejects a custom pattern that omits {profile}. Such a pattern
// would render one service name for every profile, so `secret set` under dev
// and under prod would overwrite the same keyring item and a profile switch
// would silently read the wrong credential. Failing at load is the only place
// this is visible before damage is done.
func (k KeyringConfig) Validate() error {
	if k.Service == "" {
		return nil
	}
	if !strings.Contains(k.Service, "{profile}") {
		return fmt.Errorf(
			"keyring.service pattern %q has no {profile}; every profile would share one keyring item",
			k.Service)
	}
	return nil
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
	if err := cfg.Keyring.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", ConfigPath(), err)
	}
	return &cfg, nil
}
