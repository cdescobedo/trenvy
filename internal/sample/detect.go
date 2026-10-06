package sample

import (
	"os"
	"sort"
	"strings"
)

// placeholderSuffixes are the endings a sample carries over the file it is a
// sample of -- the same list cli's outputFor strips to guess the rendered
// path, so every detected sample can become an output.
var placeholderSuffixes = []string{".example", "_example", ".sample", ".dist"}

// Detect returns the base names of the sample files directly in dir, sorted
// (spec §6: samples detected in the worktree root). A name counts when it
// ends in a placeholder suffix and, without it, names a dotenv file or a
// JSON file.
func Detect(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() || !isSampleName(e.Name()) {
			continue
		}
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out, nil
}

func isSampleName(name string) bool {
	for _, s := range placeholderSuffixes {
		if !strings.HasSuffix(name, s) {
			continue
		}
		real := strings.ToLower(strings.TrimSuffix(name, s))
		return real == ".env" || strings.HasPrefix(real, ".env.") || strings.HasSuffix(real, ".env") || strings.Contains(real, ".json")
	}
	return false
}
