package render

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"

	"github.com/cdescobedo/envctl/internal/resolve"
)

// Sum returns a keyed digest of an output's resolved values, used to detect
// that a rendered file is stale.
//
// It is keyed by the project salt rather than a plain hash. The digest is
// stored in the state file; an unkeyed digest of a low-entropy secret could be
// brute-forced from there, which would mean envctl's own bookkeeping weakened
// the secrets it exists to protect.
func Sum(salt string, r resolve.Resolved) string {
	mac := hmac.New(sha256.New, []byte(salt))
	for _, name := range r.Names {
		// Length prefixes keep {AB: "c"} distinct from {A: "Bc"}.
		fmt.Fprintf(mac, "%d:%s=%d:%s\n", len(name), name, len(r.Values[name]), r.Values[name])
	}
	return hex.EncodeToString(mac.Sum(nil))
}

// FileSum digests the bytes of a rendered file, used to detect that someone
// hand-edited it after envctl wrote it.
func FileSum(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// FileSumAt digests the file at path.
func FileSumAt(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	return FileSum(data), nil
}
