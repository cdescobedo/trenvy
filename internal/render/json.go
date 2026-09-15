package render

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/cdescobedo/trenvy/internal/resolve"
)

// JSON renders values as a flat object of strings.
//
// Nothing is added to the object. A json output's typical destination is a
// file served to a browser, so it carries the application's configuration and
// nothing else; trenvy's own bookkeeping lives in the state file.
func JSON(r resolve.Resolved) ([]byte, error) {
	obj := make(map[string]string, len(r.Names))
	for _, name := range r.Names {
		obj[name] = r.Values[name]
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	// Keep URLs readable: no &, < or > escaping.
	enc.SetEscapeHTML(false)
	if err := enc.Encode(obj); err != nil {
		return nil, fmt.Errorf("encode json: %w", err)
	}
	return buf.Bytes(), nil
}
