package sample

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDetectFindsOnlySampleNames(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{
		".env.example", ".env.sample", ".env.dist", ".env.local.example", "app.env.example",
		"appsettings.json.example", "env_example", ".env", "README.md", "config.example.json",
	} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("A=1\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "x.env.example"), 0o755); err != nil { // a directory is never a sample
		t.Fatal(err)
	}
	got, err := Detect(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{".env.dist", ".env.example", ".env.local.example", ".env.sample", "app.env.example", "appsettings.json.example"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Detect = %v, want %v", got, want)
	}
}
