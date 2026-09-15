package manifest

import (
	"fmt"
	"os"

	"github.com/pelletier/go-toml/v2"
)

// Parse decodes a manifest from TOML bytes. It does not validate; call
// Validate for that.
func Parse(data []byte) (*Manifest, error) {
	var m Manifest
	if err := toml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	if m.Outputs == nil {
		m.Outputs = map[string]Output{}
	}
	if m.Vars == nil {
		m.Vars = map[string]Var{}
	}
	return &m, nil
}

// Load reads and parses the manifest at path.
func Load(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read manifest %s: %w", path, err)
	}
	return Parse(data)
}
