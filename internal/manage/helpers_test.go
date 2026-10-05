package manage_test

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cdescobedo/trenvy/internal/manage"
	"github.com/cdescobedo/trenvy/internal/manifest"
	"github.com/cdescobedo/trenvy/internal/session"
	"github.com/cdescobedo/trenvy/internal/style"
)

func str(v string) *string { return &v }

func manifestVar(v string) manifest.Var { return manifest.Var{Value: &v} }

// key turns a readable name into the message a terminal sends.
func key(s string) tea.KeyMsg {
	named := map[string]tea.KeyType{
		"enter": tea.KeyEnter, "esc": tea.KeyEsc, "tab": tea.KeyTab, "shift+tab": tea.KeyShiftTab,
		"up": tea.KeyUp, "down": tea.KeyDown, "left": tea.KeyLeft, "right": tea.KeyRight,
		"←": tea.KeyLeft, "→": tea.KeyRight, "backspace": tea.KeyBackspace,
		"ctrl+c": tea.KeyCtrlC, "ctrl+u": tea.KeyCtrlU, "space": tea.KeySpace,
	}
	if t, ok := named[s]; ok {
		return tea.KeyMsg{Type: t}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// send delivers msg and then runs every Cmd that comes back, feeding its
// message in, the way the tea runtime would. A Quit is left to the model's
// own Quitting(), not run.
func send(m manage.Model, msg tea.Msg) manage.Model {
	next, cmd := m.Update(msg)
	m = next.(manage.Model)
	for cmd != nil {
		out := cmd()
		if out == nil {
			return m
		}
		if _, quit := out.(tea.QuitMsg); quit {
			return m
		}
		next, cmd = m.Update(out)
		m = next.(manage.Model)
	}
	return m
}

func press(m manage.Model, keys ...string) manage.Model {
	for _, k := range keys {
		m = send(m, key(k))
	}
	return m
}

func typeText(m manage.Model, s string) manage.Model {
	return send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)})
}

// fakeDeps records what the model asked for.
type fakeDeps struct {
	secrets   map[string]string // "NAME/profile" -> value
	commits   [][]session.Change
	commitErr error
	reloads   int
	next      *manage.Snapshot // what Reload returns; nil means fixture()
	reloadErr error
	ignored   map[string]bool // path -> git ignores it; missing means not ignored
}

func (f *fakeDeps) deps() manage.Deps {
	return manage.Deps{
		Reveal: func(name, profile string) (string, error) {
			v, ok := f.secrets[name+"/"+profile]
			if !ok {
				return "", fmt.Errorf("not stored")
			}
			return v, nil
		},
		Commit: func(cs []session.Change) (manage.CommitResult, error) {
			f.commits = append(f.commits, cs)
			if f.commitErr != nil {
				return manage.CommitResult{}, f.commitErr
			}
			return manage.CommitResult{Saved: len(cs)}, nil
		},
		Reload: func() (manage.Snapshot, error) {
			f.reloads++
			if f.reloadErr != nil {
				return manage.Snapshot{}, f.reloadErr
			}
			if f.next != nil {
				return *f.next, nil
			}
			return fixture(), nil
		},
		Ignored: func(path string) (bool, error) { return f.ignored[path], nil },
	}
}

// fixture: three profiles; APP_MODE shared plus a prod override; API_TOKEN
// secret stored under dev and prod only (one problem: staging); DATABASE_URL
// shared; LOG_LEVEL optional with a shared value; PUBLIC_URL shared. Sorted
// names: API_TOKEN, APP_MODE, DATABASE_URL, LOG_LEVEL, PUBLIC_URL. Two
// outputs: local (the default, everything) and web (only PUBLIC_*).
func fixtureManifest() *manifest.Manifest {
	return &manifest.Manifest{
		Project: manifest.Project{ID: "app", Profiles: []string{"dev", "staging", "prod"}, DefaultProfile: "dev", Output: "local"},
		Vars: map[string]manifest.Var{
			"API_TOKEN":    {Secret: true},
			"APP_MODE":     {Value: str("development"), Profiles: map[string]string{"prod": "production"}},
			"DATABASE_URL": {Value: str("postgres://localhost:5432/app")},
			"LOG_LEVEL":    {Value: str("info"), Optional: true},
			"PUBLIC_URL":   {Value: str("http://localhost:3000")},
		},
		Outputs: map[string]manifest.Output{
			"local": {Format: manifest.FormatDotenv, Path: ".env"},
			"web":   {Format: manifest.FormatJSON, Path: "web.json", Include: []string{"PUBLIC_*"}},
		},
	}
}

func snapshotOf(m *manifest.Manifest, stored map[string]map[string]bool) manage.Snapshot {
	mx := session.NewMatrix(m, stored)
	return manage.Snapshot{
		Project: "app", Profile: "dev", Source: "default",
		ManifestPath: "/cfg/projects/app.toml", Worktree: "/code/app", Backend: "file",
		Matrix: mx, Problems: session.Problems(mx, nil), OutputsFor: session.OutputsFor(mx),
		OutputPaths:   map[string]string{"local": ".env", "web": "web.json"},
		OutputStates:  map[string]string{"local": session.StateFresh, "web": session.StateAbsent},
		DefaultOutput: "local", Projects: []string{"app", "other"},
	}
}

func fixture() manage.Snapshot {
	return snapshotOf(fixtureManifest(), map[string]map[string]bool{"API_TOKEN": {"dev": true, "prod": true}})
}

func sized(m manage.Model, w, h int) manage.Model {
	return send(m, tea.WindowSizeMsg{Width: w, Height: h})
}

func newModel(f *fakeDeps) manage.Model {
	return sized(manage.New(fixture(), f.deps(), style.Theme{}), 100, 30)
}

type manageModel = manage.Model

// noTheme is the zero Theme: every role renders plain text, so a test can
// assert on words alone (every state reads without colour).
func noTheme() style.Theme { return style.Theme{} }
