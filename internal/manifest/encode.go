package manifest

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

// Encode serialises the manifest to TOML. Map keys are emitted in sorted
// order by the encoder, so output is stable across runs — which matters
// because the manifest is rewritten wholesale on every save.
func (m *Manifest) Encode() ([]byte, error) {
	data, err := toml.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("encode manifest: %w", err)
	}
	return data, nil
}

// Save writes the manifest to path, creating parent directories, with mode
// 0600. The manifest holds no secret values, but it does describe a project's
// shape, so it stays private. If the file already exists, its permissions are
// tightened to 0o600 (WriteFile does not chmod existing files).
func Save(path string, m *Manifest) error {
	data, err := m.Encode()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("chmod %s: %w", path, err)
	}
	return nil
}

// NewSalt returns 32 random bytes as hex, for Project.Salt.
func NewSalt() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
