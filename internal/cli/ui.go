package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cdescobedo/trenvy/internal/config"
	"github.com/cdescobedo/trenvy/internal/manifest"
	"github.com/cdescobedo/trenvy/internal/session"
	"github.com/cdescobedo/trenvy/internal/store"
)

func newUiCmd(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "ui",
		Short: "Manage this project's variables and outputs, interactively",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			app, err := opts.resolveApp()
			if err != nil {
				return err
			}
			return runManage(opts, app)
		},
	}
}

// commitStaged writes w's staged changes in phases 0 to 6, because Task 6's
// new operations depend on each other in ways the four Change kinds Tasks
// 1-4 shipped never did:
//
//  0. Validate the whole batch on a clone (prevalidate), so a batch the
//     manifest refuses writes nothing at all.
//
//  1. Structural additions (add-var, add-output): pure in-memory map
//     inserts. These run FIRST because nothing else may read an added
//     entry's absence -- in particular, phase 5 below may stage a
//     rename-output or remove-output for a row THIS SAME SESSION also
//     added (a row added this session can be selected, so
//     selecting and acting on it before w is a normal session), and that
//     phase's mutation needs the key to already exist in the map.
//
//  2. Keyring writes (set-secret).
//
//  3. Keyring deletes (delete-var's secret removal), after every write --
//     phase 1's own rule, carried forward unchanged.
//
//  4. The state file: a staged rename repoints that output's render
//     records (State.RenameOutput), and a staged removal forgets them
//     outright (State.Forget, across every worktree) -- the staged-model
//     equivalent of `trenvy output rm --force`, not its refusal, since d
//     on this tab has already asked for that. Both save before the
//     manifest, for the same reason `trenvy output rename` already
//     established (see State.RenameOutput's doc): only this order is
//     recoverable by re-running the same session's equivalent by hand.
//
//  5. Every remaining manifest mutation, including every REMOVAL
//     (delete-var, remove-output) and the rest (set-value, promote-secret,
//     default-output, set-globs). Removals live here, never in phase 1's
//     loop, so an add-output and a remove-output for the SAME row in one
//     batch always nets to "never existed" regardless of which the caller
//     listed first in staged -- getting this backwards is the Fix-1-shaped
//     bug: an addition applied AFTER its own removal resurrects exactly what
//     the user staged getting rid of.
//
//     rename-output's manifest half runs in its OWN pass, 5b, strictly after
//     every case above -- never interleaved with them in staged order. This
//     is the fix for the second, closely related Fix-1-shaped bug a
//     whole-branch review found: identity.Name never changes when a row is
//     renamed (every other Change keys by the row's ORIGINAL identity), but the rename itself moves that
//     key in app.Manifest.Outputs. Applying rename-output in the same
//     single-pass loop as set-globs or default-output meant whichever the
//     user happened to press FIRST decided whether the other's edit landed:
//     rename-then-edit-membership deleted the old key before set-globs ever
//     looked it up, silently discarding the edit, while edit-then-rename
//     worked -- two staged edits, opposite results, by keypress order alone.
//     Running every other output-keyed mutation to completion in 5a before
//     any rename in 5b removes the ordering dependency entirely: set-globs
//     and default-output always see the row under its pre-rename identity,
//     and 5b's own rename then carries forward whatever those mutations just
//     wrote, exactly the way removedOutputNames (computed before phase 4)
//     already makes removal order-independent.
//
//  6. Validate, then save the manifest.
//
// The manifest is the only inventory of which keyring items exist, so a
// failure before phase 6 leaves at worst a keyring item nothing references
// -- inert, because nothing ever looks for it. The reverse (manifest before
// keyring) would risk the opposite: a manifest entry declaring a secret that
// was never stored, or missing one whose secret still sits in the keyring,
// either of which is data loss or an item nothing can ever find again.
//
// The manifest save is last and atomic, so after any failure the manifest on
// disk is the pre-commit one. Every earlier phase is safe to repeat: a
// keyring write or delete, and a state-file repoint or forget, find their
// work already done on a second run. So re-running the same batch is how
// internal/manage retries after a partial failure. The error reports
// exactly how much of THIS run already landed, so the message never claims
// something gone that a retry would actually find already done.
//
// commitStaged returns its result instead of printing it, because the
// interface calls it while it owns the terminal. A failure in phase 6, after the state
// save (phase 4), still returns the left-in-place paths beside its error,
// because that save forgot the render records and a retry would find none
// to name.
//
// Adding a kind (Task 8's set-globs) means adding one case to whichever
// phase above it belongs in -- never restructuring the phases themselves.
func commitStaged(opts *Options, app *appContext, staged []session.Change) (commitResult, error) {
	if len(staged) == 0 {
		return commitResult{}, nil
	}
	if err := prevalidate(app.Manifest, staged); err != nil {
		return commitResult{}, err
	}

	// Phase 1: structural additions -- see applyAdditions.
	applyAdditions(app.Manifest, staged)

	type keyOp struct {
		del     bool
		service string
		value   string
	}
	var writes, deletes []keyOp

	// A set-secret for a variable the batch also deletes is skipped: the
	// delete leaves no manifest entry naming the item, so storing it would
	// orphan it in the keyring. session.Staged never stages both; this
	// guards a hand-built batch, as applyManifestEdits' deletedNames does.
	deleted := deletedVarNames(staged)
	for _, c := range staged {
		switch c.Kind {
		case session.ChangeSetSecret:
			if deleted[c.Name] {
				continue
			}
			writes = append(writes, keyOp{
				service: app.Cfg.Keyring.ServiceName(app.Manifest.Project.ID, c.Profile, c.Name),
				value:   c.Value,
			})
		case session.ChangeDeleteVar:
			// Deleting a variable must remove its keyring item under EVERY
			// declared profile, so this loop uses its own p when building
			// each service name -- never app.service(c.Name), which binds
			// only the active profile and would delete that one profile's
			// item once per profile while leaving every other profile's
			// secret orphaned in the keyring with no manifest entry left to
			// find it by.
			//
			// Presence is read from app.Manifest as it stands after phase
			// 1 -- which never touches an EXISTING var's secretness, only
			// ever inserting brand new plain ones -- so this still agrees
			// with "as loaded" for every name delete-var could target: a
			// var deleted in the same batch it was added in (staged as
			// both add-var and delete-var) is correctly seen as never
			// having existed, never as secret.
			if v, ok := app.Manifest.Vars[c.Name]; ok && v.Secret {
				for _, p := range app.Manifest.Project.Profiles {
					deletes = append(deletes, keyOp{
						del:     true,
						service: app.Cfg.Keyring.ServiceName(app.Manifest.Project.ID, p, c.Name),
					})
				}
			}
		}
	}

	// Stored and deleted are counted separately, never folded into one
	// number: a blurred "applied 3 of 5" cannot tell a reader whether the
	// missing two are secrets never stored (something still needs a value)
	// or secrets never removed (a credential the user meant to revoke is
	// still live) -- and those two states call for opposite next steps. A
	// user reading this message is deciding what to do about their secrets
	// right now, and a count that hides which kind of operation stalled
	// gives back exactly what reporting partial progress was for.
	storedTotal, deletedTotal := len(writes), len(deletes)
	storedApplied, deletedApplied := 0, 0
	if storedTotal+deletedTotal > 0 {
		s, err := app.store()
		if err != nil {
			return commitResult{}, err
		}
		// Phase 2: keyring writes.
		for _, op := range writes {
			if err := s.Set(op.service, op.value); err != nil {
				return commitResult{}, partialCommitError("store", op.service, err,
					storedApplied, storedTotal, deletedApplied, deletedTotal, 0, 0, false)
			}
			storedApplied++
		}
		// Phase 3: keyring deletes, after every write above -- phase 1's
		// own rule, unchanged.
		for _, op := range deletes {
			if err := s.Delete(op.service); err != nil {
				if errors.Is(err, store.ErrNotFound) {
					// A declared secret that was never actually stored under
					// this profile is the ordinary case, not a failure, and
					// must not inflate the applied count.
					continue
				}
				return commitResult{}, partialCommitError("delete", op.service, err,
					storedApplied, storedTotal, deletedApplied, deletedTotal, 0, 0, false)
			}
			deletedApplied++
		}
	}

	// removedOutputNames marks every output this batch removes. Computed
	// before phase 4, which needs it: forgetting a removed output's render
	// records is itself state-file work, not phase 5's manifest mutation.
	// deletedNames (below, by phase 5) is its counterpart for the
	// Variables tab -- kept where it is, since nothing before phase 5
	// needs it.
	removedOutputNames := make(map[string]bool, len(staged))
	for _, c := range staged {
		if c.Kind == session.ChangeRemoveOutput {
			removedOutputNames[c.Name] = true
		}
	}

	// Phase 4: the state file. Two kinds touch it.
	//
	// A staged rename must repoint any render record under the output's
	// OLD name, and that repointing must be saved to disk before the
	// manifest ever reflects the new name -- see State.RenameOutput's own
	// doc and internal/cli/output.go's `output rename`, which established
	// this order first: only state-before-manifest is recoverable by
	// re-running the same rename, because the old name is still defined in
	// the manifest for the retry to find. Manifest-first would leave the
	// old name undefined and nothing left to re-rename.
	//
	// A staged removal forgets every render record under the removed name,
	// in every worktree that ever rendered it -- state/<id>.toml is one
	// file spanning all of them, the same file `trenvy output rm` reads,
	// and State.Find is keyed on the output NAME alone. Leaving a record
	// behind is not harmless: output names are short and meant to be
	// reused, and the next output created under this same name would have
	// its first render compared against the OLD output's stale digests --
	// reported MODIFIED or FOREIGN for a file nothing is wrong with. This
	// is the staged-model equivalent of `trenvy output rm --force`, not
	// its refusal without --force: refusing here would mean refusing after
	// w has already committed the rest of the batch, the wrong moment, and
	// pressing d on this tab has already asked for exactly this. What
	// --force does not skip, and neither does this, is naming every file
	// left on disk -- forgotten, named in the commit's result once the
	// whole commit succeeds (and in its error after a later failure, see
	// withLeftInPlace), in the exact words `output rm --force` already
	// uses: removing the manifest entry is not removing the file, and a
	// file that may carry secrets must never be silently forgotten about.
	//
	// Both RenameOutput and Forget only ever mutate app.State in memory;
	// Save runs once, unconditionally, whenever either kind was staged --
	// never only when a record actually moved or was found -- mirroring
	// `output rename`'s own unconditional Save after RenameOutput: a
	// project with no render records yet still deserves a state file that
	// agrees with the manifest the moment one is ever recorded.
	renamedRecords, renamedAny := 0, false
	for _, c := range staged {
		if c.Kind == session.ChangeRenameOutput {
			renamedRecords += app.State.RenameOutput(c.Name, c.Value)
			renamedAny = true
		}
	}
	var forgotten []config.RenderEntry
	for _, e := range app.State.Renders {
		if removedOutputNames[e.Output] {
			forgotten = append(forgotten, e)
		}
	}
	for _, e := range forgotten {
		app.State.Forget(e.Worktree, e.Output)
	}
	if renamedAny || len(forgotten) > 0 {
		if err := app.State.Save(); err != nil {
			return commitResult{}, partialCommitError("save state for output changes", config.StatePath(app.Entry.ID), err,
				storedApplied, storedTotal, deletedApplied, deletedTotal, 0, 0, false)
		}
	}
	// stateSaved records whether the Save above actually ran (and, since it
	// would have already returned on error, actually landed) -- distinct
	// from renamedRecords/len(forgotten) being nonzero, which are true the
	// instant the in-memory mutations above run, whether or not Save ever
	// gets called. Phase 6's failure path below must not credit a save
	// that never happened.
	stateSaved := renamedAny || len(forgotten) > 0

	// Naming the file, not just the removed output: matches `trenvy output
	// rm --force`'s own "left in place: %s/%s" verbatim, because a user who
	// has learned that command's wording should not have to learn a second
	// one here for the same fact. Computed here, once the state save has
	// landed, so a later failure returns the same paths alongside its error.
	left := commitResult{}
	for _, e := range forgotten {
		left.leftInPlace = append(left.leftInPlace, e.Worktree+"/"+e.Path)
	}

	// Phases 5a and 5b -- see applyManifestEdits.
	applyManifestEdits(app.Manifest, staged)

	// Phase 6: validate, then save the manifest.
	if err := app.Manifest.Validate(); err != nil {
		// The warning below must print regardless of whether this batch
		// touched the keyring or the state file at all: a batch of pure
		// manifest edits (an add-var with nothing else set on it, say) has
		// no progress to report, but it can still carry other staged edits
		// that are about to be lost right along with it. The bare error
		// used to reach the caller unwarned on exactly that batch shape --
		// the one Fix 3 was filed for.
		if progress := commitProgress(storedApplied, storedTotal, deletedApplied, deletedTotal, renamedRecords, len(forgotten), stateSaved); progress != "" {
			return left, withLeftInPlace(fmt.Errorf("%w\n  %s; the manifest was not saved",
				err, progress), forgotten)
		}
		return left, withLeftInPlace(err, forgotten)
	}

	if err := manifest.Save(config.ManifestPath(app.Entry.ID), app.Manifest); err != nil {
		// Fix 3 (Important, whole-branch review): this used to be a bare
		// `return err`, the one exit in this function that did not wrap with
		// partialCommitError -- including the Validate branch immediately
		// above, which already does. A manifest directory that cannot be
		// written to (0500, say) can fail here AFTER every keyring write,
		// keyring delete and state save above has already landed for real,
		// and the bare error told the user only "permission denied",
		// dropping every fact about what had already happened -- exactly
		// the loss commitProgress and partialCommitError exist to prevent
		// everywhere else in this function.
		return left, withLeftInPlace(partialCommitError("save manifest", config.ManifestPath(app.Entry.ID), err,
			storedApplied, storedTotal, deletedApplied, deletedTotal, renamedRecords, len(forgotten), stateSaved), forgotten)
	}

	left.saved = len(staged)
	return left, nil
}

// commitResult is what a commit reports: in full on success, and with only
// leftInPlace filled in beside a late error (see commitStaged). It is
// returned rather than printed because internal/manage calls commitStaged
// while its screen owns stdout.
type commitResult struct {
	saved       int
	leftInPlace []string // "<worktree>/<path>" of each removed output's file left on disk; also set with a late error
}

// invalidBatchError is a batch the manifest refuses, found before anything
// was written.
type invalidBatchError struct{ err error }

func (e *invalidBatchError) Error() string { return e.err.Error() + "\n  nothing was written" }
func (e *invalidBatchError) Unwrap() error { return e.err }

// prevalidate applies the manifest half of staged to a clone and validates
// it, so a batch the manifest refuses is refused before its first keyring
// write rather than after (spec 2026-09-30 §3).
func prevalidate(m *manifest.Manifest, staged []session.Change) error {
	c, err := m.Clone()
	if err != nil {
		return err
	}
	if err := renameCollision(m, staged); err != nil {
		return &invalidBatchError{err}
	}
	applyAdditions(c, staged)
	applyManifestEdits(c, staged)
	if err := c.Validate(); err != nil {
		return &invalidBatchError{err}
	}
	return nil
}

// renameCollision refuses a rename whose target is a name some output
// holds now -- on disk or added this batch -- or another rename's target.
// Phase 5b moves one row at a time with a map assignment, so landing on a
// held name overwrites that output, and even a swap (a->b, b->a) loses one
// of the two (traced 2026-10-05). Renaming through a free name is the way.
func renameCollision(m *manifest.Manifest, staged []session.Change) error {
	held := map[string]bool{}
	for name := range m.Outputs {
		held[name] = true
	}
	for _, c := range staged {
		if c.Kind == session.ChangeAddOutput {
			held[c.Name] = true
		}
	}
	targets := map[string]string{}
	for _, c := range staged {
		if c.Kind != session.ChangeRenameOutput {
			continue
		}
		// A rename back to the row's own name is a no-op in phase 5b, and
		// an older interface staged exactly that.
		if c.Value == c.Name {
			continue
		}
		if held[c.Value] {
			return fmt.Errorf("output %q: cannot rename %s onto it -- an output already has that name; rename through a free name", c.Value, c.Name)
		}
		if prev, ok := targets[c.Value]; ok {
			return fmt.Errorf("output %q: %s and %s would both be renamed to it", c.Value, prev, c.Name)
		}
		targets[c.Value] = c.Name
	}
	return nil
}

// applyAdditions is phase 1 (see commitStaged's doc): structural additions
// only, each inserting a key that does not exist yet.
func applyAdditions(m *manifest.Manifest, staged []session.Change) {
	// add-var and add-output only ever insert a key that does not exist yet,
	// so applying them before anything else is always safe -- nothing here
	// reads an added entry's OLD value, because there isn't one.
	for _, c := range staged {
		switch c.Kind {
		case session.ChangeAddVar:
			if _, exists := m.Vars[c.Name]; !exists {
				m.Vars[c.Name] = manifest.Var{}
			}
		case session.ChangeAddOutput:
			if m.Outputs == nil {
				m.Outputs = map[string]manifest.Output{}
			}
			if _, exists := m.Outputs[c.Name]; !exists {
				m.Outputs[c.Name] = manifest.Output{Format: c.Format, Path: c.Path}
			}
		}
	}
}

// applyManifestEdits is phases 5a and 5b (see commitStaged's doc): every
// manifest mutation after the structural additions, removals included,
// with rename-output's manifest half strictly last.
func applyManifestEdits(m *manifest.Manifest, staged []session.Change) {
	removedOutputNames := make(map[string]bool, len(staged))
	for _, c := range staged {
		if c.Kind == session.ChangeRemoveOutput {
			removedOutputNames[c.Name] = true
		}
	}

	// deletedNames marks every variable this batch also deletes, so the
	// set-value/set-secret cases below can refuse to read-modify-write a Var
	// that delete-var has removed, or is about to. That read-modify-write
	// against an already-deleted entry is the actual data-loss mechanism
	// behind the Fix 1 bug: Vars[c.Name] on a name delete already handled
	// returns a ZERO Var, and writing it back silently resurrects the
	// variable with every profile but the one this change touches missing.
	// The interface refuses to stage this combination in the first place
	// (beginEdit cancels a pending delete; toggleDelete refuses a new one
	// over a staged edit -- see their doc comments in internal/manage), so
	// reaching this branch from a real session should not be possible. It
	// stays here anyway: a Change slice is a plain exported value, and this
	// function must not trust that every caller built one the only way the
	// interface's keys allow.
	deletedNames := deletedVarNames(staged)

	// Phase 5a: every remaining manifest mutation EXCEPT rename-output,
	// including every removal. Only now does the manifest change, and only
	// in memory -- Validate runs before anything reaches disk, so a rejected
	// manifest cannot leave a half-applied shape behind.
	//
	// rename-output is deliberately NOT a case here -- see phase 5b below,
	// and commitStaged's own doc, for why a rename must never share this
	// loop with set-globs or default-output.
	for _, c := range staged {
		switch c.Kind {
		case session.ChangeSetValue:
			if deletedNames[c.Name] {
				continue
			}
			// A per-profile override, never the shared Value: editing one
			// cell in the matrix must not change what every other profile
			// resolves to, and Classify already reads Profiles[profile]
			// before Value for exactly this reason.
			v := m.Vars[c.Name]
			if v.Profiles == nil {
				v.Profiles = map[string]string{}
			}
			v.Profiles[c.Profile] = c.Value
			m.Vars[c.Name] = v
		case session.ChangeSetShared:
			if deletedNames[c.Name] {
				continue
			}
			v := m.Vars[c.Name]
			val := c.Value
			v.Value = &val
			m.Vars[c.Name] = v
		case session.ChangeUnsetValue:
			if deletedNames[c.Name] {
				continue
			}
			v := m.Vars[c.Name]
			if c.Profile == "" {
				v.Value = nil
			} else {
				delete(v.Profiles, c.Profile)
				if len(v.Profiles) == 0 {
					v.Profiles = nil
				}
			}
			m.Vars[c.Name] = v
		case session.ChangeSetOptional:
			if deletedNames[c.Name] {
				continue
			}
			v := m.Vars[c.Name]
			v.Optional = c.Value == "true"
			m.Vars[c.Name] = v
		case session.ChangeDeleteVar:
			delete(m.Vars, c.Name)
		case session.ChangePromoteSecret:
			// The row-level record from s (see stagePromotion's doc), never
			// ChangeSetSecret: a set-secret also arrives here from editing an
			// ALREADY-secret cell's value, which must not flip anything --
			// the var is secret already, and re-deriving that from the wrong
			// signal is exactly what would make a promotion that moves zero
			// values (every profile already secret with none to move, or a
			// freshly added var promoted before it is ever given a value)
			// silently fail to flip at all, because no set-secret is staged
			// to ride along with it in that case.
			if deletedNames[c.Name] {
				continue
			}
			v := m.Vars[c.Name]
			v.Secret = true
			v.Value = nil
			v.Profiles = nil
			m.Vars[c.Name] = v
		case session.ChangeRemoveOutput:
			delete(m.Outputs, c.Name)
			// Mirrors `trenvy output rm`'s own clearedDefault: removing the
			// output that held project.output must clear it, the same
			// reversible-edit reasoning that command already documents --
			// refusing until the user repoints would be ceremony here too.
			if m.Project.Output == c.Name {
				m.Project.Output = ""
			}
		case session.ChangeDefaultOutput:
			if removedOutputNames[c.Name] {
				continue
			}
			m.Project.Output = c.Name
		case session.ChangeSetOutputFormat, session.ChangeSetOutputPath:
			// Keyed by identity, applied before 5b renames for the same
			// reason set-globs is (see commitStaged's doc).
			if removedOutputNames[c.Name] {
				continue
			}
			o, ok := m.Outputs[c.Name]
			if !ok {
				continue
			}
			if c.Kind == session.ChangeSetOutputFormat {
				o.Format = c.Value
			} else {
				o.Path = c.Value
			}
			m.Outputs[c.Name] = o
		case session.ChangeSetGlobs:
			// Task 8's own kind (see commitStaged's doc): it did not exist
			// when the phases above it were built, so it lands here,
			// in the manifest-edits phase, as one more case -- never a
			// restructuring of the phases themselves.
			if removedOutputNames[c.Name] {
				continue
			}
			o, ok := m.Outputs[c.Name]
			if !ok {
				// Phase 1 already ran, and any staged rename for this
				// identity runs strictly AFTER this whole loop, in phase
				// 5b below -- never before it -- so a same-batch rename can
				// never be why this row is missing here. The only way it
				// is missing is a hand-built Change naming an identity
				// that never existed -- nothing to set globs on.
				continue
			}
			o.Include = c.Include
			o.Exclude = c.Exclude
			m.Outputs[c.Name] = o
		}
	}

	// Phase 5b: rename-output's manifest half, strictly after every case in
	// phase 5a above has already run for every identity -- see commitStaged's
	// own doc for the whole-branch-review bug this ordering fixes. Running
	// renames last, in their own pass, means set-globs and default-output
	// above always looked up the row under its PRE-rename identity (the one
	// every Change keys by), so
	// whichever the user staged first no longer decides whether the other's
	// edit survives.
	for _, c := range staged {
		if c.Kind != session.ChangeRenameOutput {
			continue
		}
		if removedOutputNames[c.Name] {
			continue
		}
		o, ok := m.Outputs[c.Name]
		if !ok {
			// Phase 1 already ran, and phase 5a above never deletes a key
			// under its OWN identity (only a rename or a removal does, and
			// removal is already ruled out just above) -- so the only way
			// this row is missing here is a hand-built Change naming an
			// identity that never existed. Nothing to rename.
			continue
		}
		delete(m.Outputs, c.Name)
		m.Outputs[c.Value] = o
		// Mirrors `trenvy output rename`'s own coupling
		// (internal/cli/output.go): a rename of the row holding
		// project.output must repoint it, or Validate refuses every
		// commit after this one for naming an output that no longer
		// exists. Reading Project.Output here, after phase 5a's own
		// default-output case has already run for every row, is what
		// makes THIS order-independent too: a same-batch default-output
		// staged for this identity in either keypress order already set
		// Project.Output to c.Name by the time this runs.
		if m.Project.Output == c.Name {
			m.Project.Output = c.Value
		}
	}
}

// deletedVarNames is every variable staged deletes.
func deletedVarNames(staged []session.Change) map[string]bool {
	out := make(map[string]bool, len(staged))
	for _, c := range staged {
		if c.Kind == session.ChangeDeleteVar {
			out[c.Name] = true
		}
	}
	return out
}

// withLeftInPlace names, in `trenvy output rm --force`'s own wording, every
// file whose render record phase 4 already forgot, on a failure after that
// save. A retry resolves a fresh state with nothing left to forget, so this
// error is the only place those paths, which may carry secrets, are ever
// named.
func withLeftInPlace(err error, forgotten []config.RenderEntry) error {
	if len(forgotten) == 0 {
		return err
	}
	var b strings.Builder
	b.WriteString(err.Error())
	for _, e := range forgotten {
		fmt.Fprintf(&b, "\n  left in place: %s/%s", e.Worktree, e.Path)
	}
	return &wrappedError{msg: b.String(), err: err}
}

// wrappedError carries a longer message than its cause while keeping the
// cause reachable through errors.Is and errors.As.
type wrappedError struct {
	msg string
	err error
}

func (e *wrappedError) Error() string { return e.msg }
func (e *wrappedError) Unwrap() error { return e.err }

// commitProgress renders how far a commit got as "stored X of Y secrets,
// deleted A of B keyring items, repointed N render record(s), forgot M
// render record(s)", omitting whichever clause the batch never touched at
// all: a batch with no deletions in it reporting "deleted 0 of 0" would be
// noise at the exact moment a reader needs a message they can trust. The
// two render-record clauses only ever appear when stateSaved is true -- the
// state file's Save actually landed -- because that is real, persisted
// progress a retry would find already done, unlike an in-memory manifest
// edit that a failed commit leaves nowhere at all. Returns "" when the
// batch touched none of these.
func commitProgress(storedApplied, storedTotal, deletedApplied, deletedTotal, movedRecords, forgottenRecords int, stateSaved bool) string {
	var parts []string
	if storedTotal > 0 {
		parts = append(parts, fmt.Sprintf("stored %d of %d secrets", storedApplied, storedTotal))
	}
	if deletedTotal > 0 {
		parts = append(parts, fmt.Sprintf("deleted %d of %d keyring items", deletedApplied, deletedTotal))
	}
	if stateSaved && movedRecords > 0 {
		parts = append(parts, fmt.Sprintf("repointed %d render record(s)", movedRecords))
	}
	if stateSaved && forgottenRecords > 0 {
		parts = append(parts, fmt.Sprintf("forgot %d render record(s)", forgottenRecords))
	}
	return strings.Join(parts, ", ")
}

// partialCommitError reports a keyring write, a keyring delete, or a state
// save that failed partway through committing staged changes, in the
// shape this codebase's other partial-failure errors (unset, profile rm,
// project rm) all use: name the item, say how far the run
// got, and say the manifest was not saved.
//
// It states facts only: the interface keeps the batch staged, so what the
// user does next is retry, which the caller says.
func partialCommitError(verb, service string, err error, storedApplied, storedTotal, deletedApplied, deletedTotal, movedRecords, forgottenRecords int, stateSaved bool) error {
	return fmt.Errorf(
		"%s %s: %w\n  %s; the manifest was not saved",
		verb, service, err, commitProgress(storedApplied, storedTotal, deletedApplied, deletedTotal, movedRecords, forgottenRecords, stateSaved))
}

// registryIDs lists every registered project id, in registration order --
// the same order `trenvy project list` already uses, so the switcher and the
// CLI's own listing never disagree about how projects are ordered.
func registryIDs(reg *config.Registry) []string {
	ids := make([]string, len(reg.Projects))
	for i, e := range reg.Projects {
		ids[i] = e.ID
	}
	return ids
}

// matrixFromManifest loads the presence-aware matrix behind the ui command.
//
// Presence must come from Has, never Get: a Get can raise a macOS
// authorization prompt, and this loop touches every secret for every
// profile -- a Get here would mean one prompt per secret per profile on
// every launch, the thing that makes a tool nobody opens.
//
// The loop uses its own profile p when building each service name, never
// app.service(name) -- app.service binds the ACTIVE profile, so calling it
// here would report the active profile's presence for every column instead
// of each column's own (the same hazard set.go's profile-rm sweep and
// profile.go's rm guard against).
func matrixFromManifest(app *appContext) (*session.Matrix, error) {
	hasSecrets := false
	for _, v := range app.Manifest.Vars {
		if v.Secret {
			hasSecrets = true
			break
		}
	}
	if !hasSecrets {
		return session.NewMatrix(app.Manifest, nil), nil
	}

	s, err := app.store()
	if err != nil {
		return nil, err
	}

	stored := make(map[string]map[string]bool, len(app.Manifest.Vars))
	for name, v := range app.Manifest.Vars {
		if !v.Secret {
			continue
		}
		row := make(map[string]bool, len(app.Manifest.Project.Profiles))
		for _, p := range app.Manifest.Project.Profiles {
			ok, err := s.Has(app.Cfg.Keyring.ServiceName(app.Manifest.Project.ID, p, name))
			if err != nil {
				return nil, err
			}
			row[p] = ok
		}
		stored[name] = row
	}

	return session.NewMatrix(app.Manifest, stored), nil
}

// outputStates computes each output's render-freshness word, reusing
// status.go's classifyOutput -- the same guard.Inspect decision status.go's
// describeOutput renders -- rather than a second implementation that could
// disagree with it. It switches on classifyOutput's typed outputState (via
// stateWord), never on describeOutput's prose: parsing "not rendered" or
// "unknown (--quick)" as if they were a stable interface is how this
// function and session.Problems once went out of sync, twice in one task.
//
// Whether a required variable is missing under the active profile is read
// off mx itself by anyMissing (status.go), the one place that loop lives.
//
// quick is always true here, unlike status's own default: detecting a keyring
// change requires reading the secret's value, and the interface computes
// this eagerly over every output, not on a deliberate ask the way `status`
// (without --quick) is. Passing false would mean one Get, and so one
// possible authorization prompt, per secret an output references on every
// open. Check now (checkOutput) is the explicit, non-quick path.
//
// Because quick is never the user's own choice here, outputQuickSkipped is
// not reported as fresh: stateWord maps it to session.StateUnverified, which
// renders as "unverified". It was never verified -- quick mode cannot tell
// "nothing changed" from "a value this output selects changed", because
// render.Sum hashes every selected value, plain and secret, into one digest --
// and flagging it as a problem instead would be a permanent alarm the user
// cannot clear from inside. session.Problems treats StateUnverified as no
// problem, the same as fresh and not rendered.
//
// The bool return says whether at least one output came back unverified, so
// the caller can caveat a healthy state once rather than per output.
func outputStates(opts *Options, app *appContext, mx *session.Matrix) (map[string]string, bool) {
	missing := anyMissing(mx, app.Profile.Name)

	unverified := false
	states := make(map[string]string, len(app.Manifest.Outputs))
	for name, o := range app.Manifest.Outputs {
		st := stateWord(classifyOutput(opts, app, name, o, missing, true))
		states[name] = st
		// StateUnverified is the one case that sets the caveat: see
		// stateWord. The others either are verified or already surface as
		// their own Problem, so the empty-list case this bool exists for
		// cannot occur alongside them.
		if st == session.StateUnverified {
			unverified = true
		}
	}
	return states, unverified
}
