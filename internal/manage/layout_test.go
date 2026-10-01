package manage_test

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/cdescobedo/trenvy/internal/manage"
	"github.com/cdescobedo/trenvy/internal/style"
)

func asciiTheme() style.Theme {
	r := lipgloss.NewRenderer(nil)
	r.SetColorProfile(termenv.Ascii)
	return style.NewTheme(r)
}

// Review Focus 5 and spec §1: at every width tier, with a very long value
// and a long name, no line is wider than the terminal and the frame fits
// its height.
func TestLayoutNeverExceedsTheTerminal(t *testing.T) {
	mf := fixtureManifest()
	mf.Vars["DATABASE_URL"] = manifestVar("postgres://user:pass@" + strings.Repeat("very-long-host-segment.", 12) + "example.com:5432/app")
	mf.Vars["A_VARIABLE_NAME_THAT_IS_LONGER_THAN_THE_LIST"] = manifestVar("x")
	for _, w := range []int{40, 60, 79, 80, 100, 119, 120, 160} {
		for _, detail := range []bool{false, true} {
			m := sized(manage.New(snapshotOf(mf, nil), (&fakeDeps{}).deps(), asciiTheme()), w, 24)
			m = press(m, "j", "j") // DATABASE_URL
			if detail {
				m = press(m, "enter")
			}
			lines := strings.Split(m.View(), "\n")
			if len(lines) > 24 {
				t.Errorf("w=%d detail=%v: %d lines, want <= 24", w, detail, len(lines))
			}
			for i, l := range lines {
				if lw := lipgloss.Width(l); lw > w {
					t.Errorf("w=%d detail=%v line %d is %d wide: %q", w, detail, i, lw, l)
				}
			}
		}
	}
}

// Spec §2: long values wrap inside the pane, never cut.
func TestLongValuesWrapRatherThanCut(t *testing.T) {
	mf := fixtureManifest()
	long := "postgres://" + strings.Repeat("abcdefghij", 15) + "/app"
	mf.Vars["DATABASE_URL"] = manifestVar(long)
	// Stacked width, detail open: only the detail pane is on screen, so the
	// wrapped fragments are consecutive once borders and padding go.
	m := sized(manage.New(snapshotOf(mf, nil), (&fakeDeps{}).deps(), asciiTheme()), 70, 40)
	flat := strings.NewReplacer("│", "", " ", "", "\n", "").Replace(press(m, "j", "j", "enter").View())
	if !strings.Contains(flat, strings.ReplaceAll(long, " ", "")) {
		t.Errorf("the full value is not on screen once wrapping is undone")
	}
}

// Spec §1: below 80 columns the panes stack; enter opens the detail full
// width and left returns.
func TestNarrowTerminalStacksThePanes(t *testing.T) {
	m := sized(manage.New(fixture(), (&fakeDeps{}).deps(), asciiTheme()), 70, 24)
	if v := m.View(); strings.Contains(v, "written to") {
		t.Errorf("list view already shows the detail:\n%s", v)
	}
	m = press(m, "enter")
	if v := m.View(); !strings.Contains(v, "written to") || strings.Contains(v, "DATABASE_URL") {
		t.Errorf("enter did not open the detail full width:\n%s", v)
	}
	if v := press(m, "left").View(); !strings.Contains(v, "DATABASE_URL") {
		t.Errorf("left did not return to the list:\n%s", v)
	}
}

// Spec §1: from 120 columns the list shows the active profile's value.
func TestWideTerminalPreviewsTheActiveValue(t *testing.T) {
	m := sized(manage.New(fixture(), (&fakeDeps{}).deps(), asciiTheme()), 130, 24)
	if !strings.Contains(lineWith(t, m.View(), "DATABASE_URL"), "postgres://") {
		t.Errorf("wide list row lacks the value preview: %q", lineWith(t, m.View(), "DATABASE_URL"))
	}
}

// Profile names have no length limit (manifest validProfileName). A long
// one must not glue its label to the value, nor push the row's annotation
// past the pane's edge where it would be cut.
func TestLongProfileNamesKeepLabelValueAndMark(t *testing.T) {
	const long = "staging-eu-west-region-1"
	for _, active := range []string{"dev", long} {
		for _, w := range []int{60, 100} {
			mf := fixtureManifest()
			mf.Project.Profiles = []string{"dev", long, "prod"}
			snap := snapshotOf(mf, map[string]map[string]bool{"API_TOKEN": {"dev": true, "prod": true}})
			snap.Profile = active
			m := sized(manage.New(snap, (&fakeDeps{}).deps(), style.Theme{}), w, 24)
			if w < 80 {
				m = press(m, "enter") // stacked: open the detail
			}
			for i, tc := range []struct{ name, value string }{{"API_TOKEN", "-"}, {"APP_MODE", "development"}} {
				if i > 0 { // to the next variable
					if w < 80 {
						m = press(m, "left", "j", "enter")
					} else {
						m = press(m, "j")
					}
				}
				v := m.View()
				for i, l := range strings.Split(v, "\n") {
					if lw := lipgloss.Width(l); lw > w {
						t.Errorf("active=%s w=%d %s: line %d is %d wide: %q", active, w, tc.name, i, lw, l)
					}
				}
				// Below the header and tab row, which name the active profile.
				body := strings.SplitN(v, "\n", 3)[2]
				row := lineWith(t, body, "staging-eu-west")
				fields := strings.Fields(row[strings.Index(row, "staging-eu-west"):])
				label := strings.TrimSuffix(fields[0], "…")
				if !strings.HasPrefix(long, label) || len(fields) < 2 {
					t.Errorf("active=%s w=%d %s: label glued to the value: %q", active, w, tc.name, row)
					continue
				}
				next := fields[1]
				if next == "*" {
					next = fields[2]
				}
				if next != tc.value {
					t.Errorf("active=%s w=%d %s: value after the label = %q, want %q: %q", active, w, tc.name, next, tc.value, row)
				}
				if tc.name == "API_TOKEN" && !strings.Contains(row, "MISSING !") {
					t.Errorf("active=%s w=%d: MISSING ! not fully on the row: %q", active, w, row)
				}
				if active == long && !strings.Contains(row, "*") {
					t.Errorf("w=%d %s: the active long profile lost its *: %q", w, tc.name, row)
				}
			}
		}
	}
}
