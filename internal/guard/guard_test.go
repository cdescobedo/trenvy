package guard_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cdescobedo/envctl/internal/config"
	"github.com/cdescobedo/envctl/internal/guard"
	"github.com/cdescobedo/envctl/internal/render"
)

func newRepoWithGitignore(t *testing.T, lines string) string {
	t.Helper()
	dir := t.TempDir()
	cmd := exec.Command("git", "init", "-q", "-b", "main")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestGitIgnoredTrueForAnIgnoredPath(t *testing.T) {
	repo := newRepoWithGitignore(t, "src/DemoApp.Api/.env\n")
	ok, err := guard.GitIgnored(repo, "src/DemoApp.Api/.env")
	if err != nil {
		t.Fatalf("GitIgnored: %v", err)
	}
	if !ok {
		t.Error("GitIgnored = false for an ignored path")
	}
}

func TestGitIgnoredFalseForATrackablePath(t *testing.T) {
	repo := newRepoWithGitignore(t, "node_modules/\n")
	ok, err := guard.GitIgnored(repo, "public/env.json")
	if err != nil {
		t.Fatalf("GitIgnored: %v", err)
	}
	if ok {
		t.Error("GitIgnored = true for a path git would track")
	}
}

func TestGitIgnoredTrueOutsideAGitRepo(t *testing.T) {
	// Nothing can be committed from a directory that is not a repository, so
	// the guard has nothing to protect against and must not block the write.
	ok, err := guard.GitIgnored(t.TempDir(), ".env")
	if err != nil {
		t.Fatalf("GitIgnored: %v", err)
	}
	if !ok {
		t.Error("GitIgnored = false outside a repository")
	}
}

func writeFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestInspectStatuses(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, ".env", "A=1\n")
	fileSum := render.FileSum([]byte("A=1\n"))

	t.Run("absent when the file does not exist", func(t *testing.T) {
		st, err := guard.Inspect(filepath.Join(dir, "nope"), "sum", config.RenderEntry{}, false)
		if err != nil {
			t.Fatal(err)
		}
		if st != guard.StatusAbsent {
			t.Errorf("Status = %v, want Absent", st)
		}
	})

	t.Run("foreign when we have no record of writing it", func(t *testing.T) {
		st, err := guard.Inspect(path, "sum", config.RenderEntry{}, false)
		if err != nil {
			t.Fatal(err)
		}
		if st != guard.StatusForeign {
			t.Errorf("Status = %v, want Foreign", st)
		}
	})

	t.Run("fresh when both hashes agree", func(t *testing.T) {
		entry := config.RenderEntry{Sum: "sum", File: fileSum}
		st, err := guard.Inspect(path, "sum", entry, true)
		if err != nil {
			t.Fatal(err)
		}
		if st != guard.StatusFresh {
			t.Errorf("Status = %v, want Fresh", st)
		}
	})

	t.Run("stale when the values changed since the render", func(t *testing.T) {
		entry := config.RenderEntry{Sum: "old", File: fileSum}
		st, err := guard.Inspect(path, "new", entry, true)
		if err != nil {
			t.Fatal(err)
		}
		if st != guard.StatusStale {
			t.Errorf("Status = %v, want Stale", st)
		}
	})

	t.Run("modified when the file was hand edited", func(t *testing.T) {
		entry := config.RenderEntry{Sum: "sum", File: "some-other-hash"}
		st, err := guard.Inspect(path, "sum", entry, true)
		if err != nil {
			t.Fatal(err)
		}
		if st != guard.StatusModified {
			t.Errorf("Status = %v, want Modified", st)
		}
	})

	// A hand edit is the more urgent fact: overwriting loses work, whereas
	// staleness only means the file is behind.
	t.Run("modified wins over stale", func(t *testing.T) {
		entry := config.RenderEntry{Sum: "old", File: "some-other-hash"}
		st, err := guard.Inspect(path, "new", entry, true)
		if err != nil {
			t.Fatal(err)
		}
		if st != guard.StatusModified {
			t.Errorf("Status = %v, want Modified", st)
		}
	})
}

func TestCheckWritable(t *testing.T) {
	const p = "/w/src/.env"

	t.Run("Absent should be writable", func(t *testing.T) {
		if err := guard.CheckWritable(guard.StatusAbsent, p, "overwrite", false); err != nil {
			t.Errorf("Absent should be writable: %v", err)
		}
	})

	t.Run("Fresh should be writable", func(t *testing.T) {
		if err := guard.CheckWritable(guard.StatusFresh, p, "overwrite", false); err != nil {
			t.Errorf("Fresh should be writable: %v", err)
		}
	})

	t.Run("Stale should be writable", func(t *testing.T) {
		if err := guard.CheckWritable(guard.StatusStale, p, "overwrite", false); err != nil {
			t.Errorf("Stale should be writable: %v", err)
		}
	})

	t.Run("Foreign should be refused without --force", func(t *testing.T) {
		err := guard.CheckWritable(guard.StatusForeign, p, "overwrite", false)
		if err == nil {
			t.Fatal("Foreign should be refused without --force")
		}
		if !strings.Contains(err.Error(), "--force") {
			t.Errorf("error does not mention the escape hatch: %v", err)
		}
		if !strings.Contains(err.Error(), p) {
			t.Errorf("error does not name the path: %v", err)
		}
	})

	t.Run("--force should permit overwriting a foreign file", func(t *testing.T) {
		if err := guard.CheckWritable(guard.StatusForeign, p, "overwrite", true); err != nil {
			t.Errorf("--force should permit overwriting a foreign file: %v", err)
		}
	})

	t.Run("Modified should be refused without --force", func(t *testing.T) {
		err := guard.CheckWritable(guard.StatusModified, p, "overwrite", false)
		if err == nil {
			t.Error("Modified should be refused without --force")
		}
	})
}
