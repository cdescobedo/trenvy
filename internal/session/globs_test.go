package session_test

import (
	"reflect"
	"testing"

	"github.com/cdescobedo/trenvy/internal/manifest"
	"github.com/cdescobedo/trenvy/internal/resolve"
	"github.com/cdescobedo/trenvy/internal/session"
)

// checked builds the WriteGlobs checkbox map from the names that are on,
// out of a fixed universe of {A, B, C}. Leaving a name out of names (rather
// than requiring callers to spell every false) is what makes the
// none-checked case ({}) readable as an intentional, empty set rather than
// an oversight.
func checked(names ...string) map[string]bool {
	m := map[string]bool{}
	for _, n := range names {
		m[n] = true
	}
	return m
}

// TestWriteGlobsFields pins the exact Include/Exclude values WriteGlobs
// produces, including the shape of the two edge cases that are load-bearing
// rather than stylistic:
//
//   - "all checked" in ModeEverythingExcept must leave BOTH lists empty, not
//     write a redundant []string{} exclude.
//   - "none checked" in ModeOnlyThese cannot be an empty include: an empty
//     include is resolve.selected's spelling for "everything", the opposite
//     of what unchecking every box means. See the doc comment on WriteGlobs
//     for what this writes instead and why.
func TestWriteGlobsFields(t *testing.T) {
	all := []string{"A", "B", "C"}

	cases := []struct {
		name        string
		mode        session.GlobMode
		checked     map[string]bool
		wantInclude []string
		wantExclude []string
	}{
		{
			name:        "everything except, some unchecked",
			mode:        session.GlobsEverythingExcept,
			checked:     checked("A", "C"),
			wantInclude: nil,
			wantExclude: []string{"B"},
		},
		{
			name:        "everything except, all checked collapses to nothing excluded",
			mode:        session.GlobsEverythingExcept,
			checked:     checked("A", "B", "C"),
			wantInclude: nil,
			wantExclude: nil,
		},
		{
			name:        "everything except, none checked excludes everything known today",
			mode:        session.GlobsEverythingExcept,
			checked:     checked(),
			wantInclude: nil,
			wantExclude: []string{"A", "B", "C"},
		},
		{
			name:        "only these, some checked",
			mode:        session.GlobsOnlyThese,
			checked:     checked("A", "C"),
			wantInclude: []string{"A", "C"},
			wantExclude: nil,
		},
		{
			name:        "only these, all checked",
			mode:        session.GlobsOnlyThese,
			checked:     checked("A", "B", "C"),
			wantInclude: []string{"A", "B", "C"},
			wantExclude: nil,
		},
		{
			name:        "only these, none checked writes every known name as both included and excluded",
			mode:        session.GlobsOnlyThese,
			checked:     checked(),
			wantInclude: []string{"A", "B", "C"},
			wantExclude: []string{"A", "B", "C"},
		},
	}
	for _, c := range cases {
		gotInclude, gotExclude := session.WriteGlobs(c.mode, all, c.checked)
		if !reflect.DeepEqual(gotInclude, c.wantInclude) {
			t.Errorf("%s: include = %#v, want %#v", c.name, gotInclude, c.wantInclude)
		}
		if !reflect.DeepEqual(gotExclude, c.wantExclude) {
			t.Errorf("%s: exclude = %#v, want %#v", c.name, gotExclude, c.wantExclude)
		}
		// The empty-vs-["*"] rule is the specific defect a phase-1 Critical
		// shipped: a literal "*" goes through path.Match, whose "*" does not
		// cross "/", silently dropping a GROUP/KEY-style name. WriteGlobs
		// must never reach for "*" to mean "everything" -- only len == 0.
		for _, g := range gotInclude {
			if g == "*" {
				t.Errorf("%s: include contains literal \"*\" -- must be empty instead: %#v", c.name, gotInclude)
			}
		}
	}
}

// selectsAll runs every name in all through resolve.Selects against the
// globs WriteGlobs wrote, and returns the subset that came back true. This
// is the check that matters per the task brief: a table asserting field
// values proves the function wrote what it meant, but only resolve.Selects
// -- the same function render.Dotenv and render.JSON depend on -- proves
// the rendered file will actually contain what the user checked.
func selectsAll(t *testing.T, include, exclude []string, names []string) map[string]bool {
	t.Helper()
	m := &manifest.Manifest{Outputs: map[string]manifest.Output{
		"out": {Include: include, Exclude: exclude},
	}}
	got := map[string]bool{}
	for _, n := range names {
		sel, err := resolve.Selects(m, "out", n)
		if err != nil {
			t.Fatalf("resolve.Selects(%q): %v", n, err)
		}
		got[n] = sel
	}
	return got
}

// TestWriteGlobsSelectsThroughResolve verifies membership the way render
// actually determines it -- through resolve.Selects -- rather than trusting
// that the right-looking Include/Exclude fields mean the right thing.
func TestWriteGlobsSelectsThroughResolve(t *testing.T) {
	all := []string{"A", "B", "C"}

	cases := []struct {
		name    string
		mode    session.GlobMode
		checked map[string]bool
		want    map[string]bool // wanted resolve.Selects result per name in `all`
	}{
		{
			name:    "everything except, some unchecked",
			mode:    session.GlobsEverythingExcept,
			checked: checked("A", "C"),
			want:    map[string]bool{"A": true, "B": false, "C": true},
		},
		{
			name:    "everything except, all checked",
			mode:    session.GlobsEverythingExcept,
			checked: checked("A", "B", "C"),
			want:    map[string]bool{"A": true, "B": true, "C": true},
		},
		{
			name:    "only these, some checked",
			mode:    session.GlobsOnlyThese,
			checked: checked("A", "C"),
			want:    map[string]bool{"A": true, "B": false, "C": true},
		},
		{
			name:    "only these, none checked selects nothing, not everything",
			mode:    session.GlobsOnlyThese,
			checked: checked(),
			want:    map[string]bool{"A": false, "B": false, "C": false},
		},
	}
	for _, c := range cases {
		include, exclude := session.WriteGlobs(c.mode, all, c.checked)
		got := selectsAll(t, include, exclude, all)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: resolve.Selects = %#v, want %#v (include=%#v exclude=%#v)",
				c.name, got, c.want, include, exclude)
		}
	}
}

// TestWriteGlobsModeSwitchPreservesSelection is the property the two modes
// must hold: they are two futures for a variable that does not exist yet,
// not two different selections of the variables that exist today. For the
// SAME checkbox set, switching mode must not move a single existing
// variable in or out -- it only changes what happens to a name added later.
func TestWriteGlobsModeSwitchPreservesSelection(t *testing.T) {
	all := []string{"A", "B", "C"}
	boxes := checked("A", "C") // B left unchecked

	incOnly, excOnly := session.WriteGlobs(session.GlobsOnlyThese, all, boxes)
	incExcept, excExcept := session.WriteGlobs(session.GlobsEverythingExcept, all, boxes)

	gotOnly := selectsAll(t, incOnly, excOnly, all)
	gotExcept := selectsAll(t, incExcept, excExcept, all)

	if !reflect.DeepEqual(gotOnly, gotExcept) {
		t.Fatalf("switching mode changed today's selection: only-these = %#v (include=%#v exclude=%#v), "+
			"everything-except = %#v (include=%#v exclude=%#v)",
			gotOnly, incOnly, excOnly, gotExcept, incExcept, excExcept)
	}
	want := map[string]bool{"A": true, "B": false, "C": true}
	if !reflect.DeepEqual(gotOnly, want) {
		t.Fatalf("selection = %#v, want %#v", gotOnly, want)
	}

	// The two futures do genuinely differ for a name neither checkbox set
	// ever saw: ModeOnlyThese's D stays out (it was never checked),
	// ModeEverythingExcept's D falls on the "everything" side (it was never
	// marked an exception). This is the intended difference the modes
	// exist to express, not a bug -- asserted here so it isn't confused
	// with the property above.
	future := selectsAll(t, incOnly, excOnly, []string{"D"})
	if future["D"] {
		t.Errorf("only-these: an unseen future variable must stay out, got selected")
	}
	future = selectsAll(t, incExcept, excExcept, []string{"D"})
	if !future["D"] {
		t.Errorf("everything-except: an unseen future variable must default in, got excluded")
	}
}

func TestInferGlobMode(t *testing.T) {
	for _, c := range []struct {
		include []string
		want    session.GlobMode
	}{
		{nil, session.GlobsEverythingExcept},
		{[]string{"*"}, session.GlobsEverythingExcept},
		{[]string{"PUBLIC_*"}, session.GlobsOnlyThese},
		{[]string{"A", "B"}, session.GlobsOnlyThese},
	} {
		if got := session.InferGlobMode(c.include); got != c.want {
			t.Errorf("InferGlobMode(%v) = %v, want %v", c.include, got, c.want)
		}
	}
}

func TestCheckedFollowsResolveSelects(t *testing.T) {
	got := session.Checked([]string{"PUBLIC_*"}, []string{"PUBLIC_SECRET"}, []string{"PUBLIC_URL", "PUBLIC_SECRET", "DB"})
	want := map[string]bool{"PUBLIC_URL": true, "PUBLIC_SECRET": false, "DB": false}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Checked = %v, want %v", got, want)
	}
}
