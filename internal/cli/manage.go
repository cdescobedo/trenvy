package cli

import (
	"errors"
	"io"
	"os"
	"sync"

	"github.com/charmbracelet/lipgloss"

	"github.com/cdescobedo/trenvy/internal/config"
	"github.com/cdescobedo/trenvy/internal/manage"
	"github.com/cdescobedo/trenvy/internal/session"
	"github.com/cdescobedo/trenvy/internal/store"
	"github.com/cdescobedo/trenvy/internal/style"
)

// runManage is `trenvy ui --new`: the redesigned interface (spec
// 2026-09-30). Unlike the original, saving happens inside the session, so
// commit and reload are handed to the model as Deps rather than run after
// it returns.
func runManage(opts *Options, app *appContext) error {
	th, err := style.ThemeFor(lipgloss.NewRenderer(opts.Stdout), os.Getenv("TRENVY_THEME"))
	if err != nil {
		return err
	}
	reg, err := config.LoadRegistry()
	if err != nil {
		return err
	}
	projects := registryIDs(reg)

	// The screen owns the terminal while the model runs. Anything the
	// snapshot and commit paths write to stderr (outputStates logs read
	// failures there) would land in the middle of it, so those paths get a
	// quiet copy of opts; every such failure also surfaces as a problem on
	// screen.
	quiet := *opts
	quiet.Stderr = io.Discard

	for {
		snap, err := buildSnapshot(&quiet, app, projects)
		if err != nil {
			return err
		}
		cur := app
		// Reveal can run while Reload swaps cur, so cur is only read or
		// assigned under mu.
		var mu sync.Mutex
		// A project switch starts a fresh carry.
		var carry leftCarry
		current := func() *appContext {
			mu.Lock()
			defer mu.Unlock()
			return cur
		}
		deps := manage.Deps{
			Reveal: func(name, profile string) (string, error) {
				cur := current()
				s, err := cur.store()
				if err != nil {
					return "", err
				}
				return s.Get(cur.Cfg.Keyring.ServiceName(cur.Manifest.Project.ID, profile, name))
			},
			Commit: func(cs []session.Change) (manage.CommitResult, error) {
				return carry.commit(&quiet, current(), cs)
			},
			// Reload re-reads the project from disk into a fresh appContext.
			// commitStaged mutates cur.Manifest in memory, succeeding or not,
			// so the next commit must never start from that copy.
			Reload: func() (manage.Snapshot, error) {
				next, err := quiet.resolveAppByID(current().Entry.ID)
				if err != nil {
					return manage.Snapshot{}, err
				}
				mu.Lock()
				cur = next
				mu.Unlock()
				return buildSnapshot(&quiet, next, projects)
			},
		}
		final, err := opts.RunManage(manage.New(snap, deps, th))
		if err != nil {
			return err
		}
		id, ok := final.SwitchTo()
		if !ok {
			return nil
		}
		if app, err = opts.resolveAppByID(id); err != nil {
			return err
		}
	}
}

// manageCommit is Deps.Commit: commitStaged, with its result in the
// interface's terms.
func manageCommit(opts *Options, app *appContext, cs []session.Change) (manage.CommitResult, error) {
	res, err := commitStaged(opts, app, cs)
	if err != nil {
		var invalid *invalidBatchError
		if errors.As(err, &invalid) {
			return manage.CommitResult{}, &refusedBatch{invalid.err}
		}
		return manage.CommitResult{LeftInPlace: res.leftInPlace}, err
	}
	return manage.CommitResult{Saved: res.saved, LeftInPlace: res.leftInPlace}, nil
}

// leftCarry carries the left-in-place paths a failed commit named to the
// retry that succeeds. A failure after the state save forgets render
// records, so the retry finds none to forget and would otherwise report no
// files at all (phase 1 ledger, Task 4). Only Commit uses it, and the model
// never runs two commits at once, so it needs no lock.
type leftCarry struct{ pending []string }

func (c *leftCarry) commit(opts *Options, app *appContext, cs []session.Change) (manage.CommitResult, error) {
	res, err := manageCommit(opts, app, cs)
	if err != nil {
		c.pending = append(c.pending, res.LeftInPlace...)
		return res, err
	}
	res.LeftInPlace = append(c.pending, res.LeftInPlace...)
	c.pending = nil
	return res, nil
}

// refusedBatch is a batch the manifest refuses, as the new interface
// receives it: the manifest's reason, matching manage.ErrNothingWritten so
// the review knows a retry cannot succeed. invalidBatchError's own "nothing
// was written" line is left out; the review words that with the next step.
type refusedBatch struct{ reason error }

func (e *refusedBatch) Error() string   { return e.reason.Error() }
func (e *refusedBatch) Unwrap() []error { return []error{manage.ErrNothingWritten, e.reason} }

// buildSnapshot loads what the interface shows, reusing the original
// interface's loaders so both read presence and freshness the same way:
// Has, never Get, and freshness in quick mode.
func buildSnapshot(opts *Options, app *appContext, projects []string) (manage.Snapshot, error) {
	mx, err := matrixFromManifest(app)
	if err != nil {
		return manage.Snapshot{}, err
	}
	states, _ := outputStates(opts, app, mx)
	paths := make(map[string]string, len(app.Manifest.Outputs))
	for name, o := range app.Manifest.Outputs {
		paths[name] = o.Path
	}
	return manage.Snapshot{
		Project:      app.Entry.ID,
		Profile:      app.Profile.Name,
		Source:       app.Profile.Source,
		ManifestPath: config.ManifestPath(app.Entry.ID),
		Worktree:     app.Worktree,
		Backend:      store.ResolveBackendName(opts.Backend),
		Matrix:       mx,
		Problems:     session.Problems(mx, states),
		OutputsFor:   session.OutputsFor(mx),
		OutputPaths:  paths,
		Projects:     projects,
	}, nil
}
