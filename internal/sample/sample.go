// Package sample reads a .env or .json example file and guesses which of its
// variables are secret, so that importing a project starts from something
// close to right rather than from nothing.
package sample

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// Entry is one variable found in a sample file.
type Entry struct {
	Name        string
	Value       string
	GuessSecret bool
}

// Parse reads a sample file, choosing a parser from its name. The check is
// for ".json" anywhere in the name, not just as the final extension, because
// a sample is often named like "env.json.example" (the real extension
// belongs to the file it is a sample of, and the example suffix comes after).
func Parse(filename string, data []byte) ([]Entry, error) {
	if strings.Contains(strings.ToLower(filepath.Base(filename)), ".json") {
		return parseJSON(data)
	}
	return parseDotenv(data), nil
}

func parseDotenv(data []byte) []Entry {
	var out []Entry
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")

		name, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		value = unquote(strings.TrimSpace(value))
		out = append(out, Entry{Name: name, Value: value, GuessSecret: GuessSecret(name, value)})
	}
	return out
}

func parseJSON(data []byte) ([]Entry, error) {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse json sample: %w", err)
	}
	names := make([]string, 0, len(raw))
	for k := range raw {
		names = append(names, k)
	}
	sort.Strings(names)

	out := make([]Entry, 0, len(names))
	for _, name := range names {
		s, ok := raw[name].(string)
		if !ok {
			return nil, fmt.Errorf(
				"key %q is not a string; envctl renders flat json only", name)
		}
		out = append(out, Entry{Name: name, Value: s, GuessSecret: GuessSecret(name, s)})
	}
	return out, nil
}

func unquote(v string) string {
	if len(v) >= 2 {
		if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
			return v[1 : len(v)-1]
		}
	}
	return v
}

var placeholderMarkers = []string{
	"your-secret", "your_secret", "your value", "replace-with", "replace_with",
	"changeme", "change-me", "todo", "xxxx", "placeholder", "<",
}

var secretSuffixes = []string{"_KEY", "_SECRET", "_TOKEN", "_PASSWORD", "_PASSWD", "APIKEY"}

// GuessSecret is a starting point for the import review, never a decision.
//
// Placeholder text is the stronger signal: a sample file spells out the values
// that are safe to publish and leaves the others as instructions to the reader,
// so the file already encodes the distinction.
func GuessSecret(name, value string) bool {
	lower := strings.ToLower(value)
	for _, marker := range placeholderMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	upper := strings.ToUpper(name)
	for _, suffix := range secretSuffixes {
		if strings.HasSuffix(upper, suffix) {
			return true
		}
	}
	return false
}
