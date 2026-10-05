# Architecture

For contributors. It describes the data model, the invariants the code holds,
and what depends on each one.

Users want the [README](../README.md) instead.

Everything except `cmd/trenvy` lives under `internal/`, so nothing here is
importable and none of it is a public API. trenvy is a binary, and internal
packages mean the shape below can change without breaking a consumer.

## The three layers

```
                 cmd/trenvy          main, and nothing else
                      |
    +-----------------+-----------------+
    |          internal/cli             |   delivery: commands, flags, output
    | internal/render    internal/manage|
    | internal/session   internal/tui   |
    +-----------------+-----------------+
                      |
              internal/resolve              precedence: what is a value?
                      |
             internal/manifest               schema: what exists?
                      |
    +-----------------+-----------------+
    | internal/store    internal/config |   beneath: where things are kept
    |        internal/guard             |
    |        internal/fsutil            |
    +-----------------------------------+
```

Read it top to bottom as a question chain:

**`manifest` — what exists?** The per-project TOML schema: variable names,
which are secret, which are optional, plain values, per-profile overrides, and
the outputs. It is pure data with a `Validate`; it knows nothing about
keyrings, files or the filesystem.

**`resolve` — what is it worth?** Turns a manifest plus a profile into concrete
values, applying the precedence rule below. It reaches secrets through a
`SecretLookup func(varName string) (string, error)` rather than a `Store`,
which is why resolution needs to know nothing about service-name patterns and
why its tests need no store at all.

**`render` / `cli` — where does it go?** `render` turns resolved values into
bytes (dotenv or JSON) and computes the digests. `cli` wires the commands and
owns everything user-facing. `manage` is the interactive interface behind both
`trenvy ui` and `trenvy edit`; `session` is the UI-free logic it stands on;
`tui` is the flat table that is left, used only by the interactive review in
`trenvy import`.

### `internal/manage`, `internal/session` and `internal/tui`

`trenvy ui` and `trenvy edit` run the same function, `runManage`
(`internal/cli/manage.go`), which builds a `manage.Snapshot` from disk and hands
the interface a `manage.Deps`. `manage` is the interface: a root shell (header,
screen switch, message line, footer, a stack of overlays) with one file per
screen. Variables is `variables.go` and `varedit.go`, Outputs is `outputs.go`,
`outedit.go` and `outactions.go`, Profiles is `profiles.go` (the list and its
detail) and `profedit.go` (add, remove, use, default and the `.` menu), and the
save review is `review.go`. `typed.go` is the panel that asks for each removed
profile's name before the review saves. The line editor (`lineedit.go`), the
`.` menu and the key table (`keys.go`) are shared. `trenvy profile` remains the
command-line way to do the same.

`session` holds everything the interface needs that is not drawing: the staged
set (`Staged`, with `Change`), `Problems` and the state words, the
variable-by-profile `Matrix`, and the glob helpers behind output membership. It
imports no UI package, so its tests need no terminal and `cli` can use it
without pulling the interface in.

`manage` does no I/O inside `Update`. The keyring, the disk and the render
path are `Deps` functions — `Reveal`, `Commit`, `Reload`, `Ignored`, `Render`,
`Clean` and `Check` — each run inside a `tea.Cmd`, so a test drives the model
with key messages and feeds each command's result back itself. Saving does not
end the session: `Commit` runs, `Reload` rebuilds the snapshot from disk, the
staged set empties, and the cursor stays. `cli.Options` carries a swappable
`RunManage`, the way it carries `RunTUI`, so command tests never open a
terminal.

Presence is loaded through `Has` only, never `Get`, the same rule `status` and
the completion paths follow, so opening the interface never raises a keyring
authorization prompt by itself. A secret is read only by an explicit reveal,
by *Render now* on an output (it is `trenvy render`, which resolves every
value it writes), by *Check now* on an output (its menu label says it reads
secrets), and by `trenvy secret demote`.

`tui` is what is left of the first interactive view: one profile's variables
as a flat table. Its only caller is `reviewImport` (`internal/cli/edit.go`), the
review `trenvy import` opens before saving. It shares no code with `manage`.

### `internal/style`

`internal/style` owns the token vocabulary and the layout helpers, in two
layers. A `Styler` colours the status words
(`MISSING`, `stored`, `STALE`, `FOREIGN` and the rest) for the non-interactive
commands, and `Columns` aligns their tables. A `Theme` is every visual role the
interface uses — title, text, muted, accent, danger, ok, warn, staged, the key
and pill styles and the surfaces — and `Pane` draws a bordered box around a
screen's list or detail. The state words themselves are one set, defined in
`session` and printed by `trenvy status`, so a word cannot mean different things
in `trenvy status` and `trenvy ui`; the interface adds a muted gloss after each,
and `unverified` for what quick mode prints as `unknown (--quick)`.

Both are built over an injected `*lipgloss.Renderer` rather than lipgloss's
global, which keeps `internal/manage` free of I/O and makes the colour profile
testable: the global renderer resolves to Ascii under `go test`. A zero `Theme`
renders plain text with no escapes, which is the form the word-level tests use.
Screens reference roles, never colours; the hexadecimal values live only in
`internal/style/theme.go`, so a later palette only has to produce a `Theme`.
`TRENVY_THEME` (`light` or `dark`) overrides the background detection through
`style.ThemeFor`, and any other value is an error. Colour is additive: the Ascii
profile (`NO_COLOR`, or output that is not a terminal) removes escape sequences
and nothing else, and every state in the interface still reads as a word.

Known limit: `Pane` cuts a line to its width rather than wrapping it, and drops
lines beyond its height. Wrapping and scrolling belong to the caller
(`internal/manage`'s layout), which wraps long values with a hanging indent and
scrolls lists and detail panes, so no value is cut and no drawn line is wider
than the terminal.

**Beneath:** `store` holds secret values behind one small interface with an
implementation per platform. `config` owns everything in the config directory —
global settings, the project registry, and render state. `guard` holds the
refusals. `fsutil` is the atomic `0600` write, and imports nothing from within
trenvy, which is what lets both `render` and `store` depend on it without a
cycle.

## The two axes

This is the central distinction in the data model.

A **profile** decides *what the values are*, including which keyring item a
secret reads. An **output** decides *where values go, in what format, and which
subset*. They are independent.

In the schema this shows up as an asymmetry:

- `Var` has a `Profiles map[string]string` of per-profile values.
- `Output` has `Format`, `Path`, `Include`, `Exclude` — and **no values at
  all**.

There is no per-output value table. An output decides which variables are
written and in what syntax, never what they are worth. One would reintroduce
the `[targets]` model that profiles replaced, where a single noun conflated
both questions and "what is `DATABASE_URL` in prod?" could not be answered
without also naming a file.

`Var.Profiles` is sparse: most variables do not vary, and a dense table would
grow a row per variable per profile and stop being readable.

`exec` and `shell` therefore take **no output argument**. Process delivery is
the verb, not a configured destination. An output's include list
describes a file's audience, and a process wants its whole environment — so
those two commands call `resolve.All`, not `resolve.Values`.

`trenvy output` manages the outputs themselves: `list`, `add`, `rename`,
`default` and `rm`. An output's name is what render records key on, so
`output rename` moves a manifest entry and its render records together — the
render guards, below, cover what those records hold.

## The precedence rule

One rule, in one function: `resolve.Value`. Everything else defers to it.

For a variable under a profile, in order:

1. **the per-profile value** — `Var.Profiles[profile]`, if present;
2. **the shared default** — `Var.Value`, if non-nil;
3. **the keyring** — only if `Var.Secret`;
4. then: if the variable is **optional**, it resolves to nothing and is omitted
   from the set; otherwise it is an **error** that stops the render.

Two details follow from this:

`Var.Value` is a `*string`, not a `string`, so that an empty string — a legal
value — is distinguishable from no value at all. Step 2 tests `!= nil`, not
`!= ""`.

`Value` resolves exactly one variable and therefore reads at most one secret.
That is what makes `trenvy get` cost one keyring read rather than one per
variable, and what stops it failing because some unrelated secret has not been
stored yet.

`resolve.Value` does **not** check that `profile` is declared.
Every CLI command resolves and validates the active profile before its body
runs (see [The active profile](#the-active-profile)), so a name arriving here
has already been checked.

Selection is separate and happens **before** any value is fetched:
`resolve.Values` applies the output's include/exclude globs first, so a secret
an output excludes never costs an authorization prompt.

Both resolvers iterate `sortedVarNames`. Go randomises map iteration, so
without that sort a rendered file's byte order — and therefore its recorded
digest — would differ run to run, and every file would look modified.

## The active profile

Four sources can name the active profile, checked in this order, and each
lives in a different place:

1. **the `--profile`/`-p` flag** — an argument to one invocation; never
   persisted anywhere.
2. **`TRENVY_PROFILE`** — the process environment; in force for one shell,
   and any command it runs, until unset.
3. **`State.Active`** — `state/<id>.toml`, beside the manifest. Set by
   `trenvy profile use <name>`, cleared by `trenvy profile use --clear`; this
   machine's own selection, until changed again.
4. **`default_profile`** — the manifest itself, `projects/<id>.toml`. The
   project's baseline, in force wherever nothing more specific names a
   profile.

`resolve.Profile(m, flag, env, active)` is where the four meet: a pure
function over the manifest and three strings, with no filesystem access of
its own — the caller reads the environment variable and `State.Active` and
passes both in, the same way it already does for the flag. That is what lets
the whole chain be tested as a table of inputs and expected outputs. It
returns an `ActiveProfile{Name, Source}`, where `Source` is one of the four
words above: `--profile`, `TRENVY_PROFILE`, `active` or `default`. Keeping
the source alongside the name lets an error or a status line explain itself:
a stale exported `TRENVY_PROFILE` is invisible until something names it as
the reason.

## `Store`: presence and value are separate operations

```go
type Store interface {
	Has(service string) (bool, error)
	Get(service string) (string, error)
	Set(service, value string) error
	Delete(service string) error
}
```

The two carry different costs. On macOS, *reading* a keyring value can raise a
GUI authorization prompt; checking that an item *exists* cannot. An interface
with only `Get` would make `trenvy status` and the interactive table cost one
prompt per secret, every run.

Every path that needs only presence therefore asks with `Has`: `status` builds
its `present`/`MISSING` column from it, and the TUI builds its rows from it,
taking a lazy `Get` callback used only when the user reveals a value. `show` without
`--reveal` does not even reach the store: it substitutes the mask for secrets
and resolves plain variables with a nil lookup.

The macOS backend is hand-rolled around `security(1)` rather than a keyring
library for the same reason: `security find-generic-password` without `-w`
answers presence silently. It is also cgo-free, so trenvy cross-compiles from
any machine.

There is a second, narrower interface:

```go
type CheapPresence interface {
	Store
	cheapPresence()
}
```

This says `Has` is *fast and cannot prompt*, so a caller on a latency budget
may probe every variable it knows about. Shell completion is that caller: it
runs on every TAB. `Keychain` and `Mem` qualify. `File` does not — its `Has`
scrypt-decrypts the whole document. `SystemKeyring` does not — the library
answers `Has` with a read, which can raise an unlock prompt. The marker method is
unexported, so only a backend in this package can make the claim and no type
satisfies the interface by accident.

## The manifest is the authoritative inventory

The manifest is the only record that a secret exists. The keyring holds values
addressed by service name; nothing enumerates them back.

Two consequences run through the whole codebase.

**Writes go keyring-first, manifest-second.** `secret set` validates the new
manifest shape, then writes the keyring, then saves the manifest. Of the two
ways a partial write can end, an unreferenced keyring item is the benign one —
the manifest is the inventory, so nothing looks for it. The reverse, a manifest
entry whose secret was never stored, shows up in `status` as `MISSING` and
reads as data loss.

**Deletes go keyring-first, manifest-second,** for the same reason. `unset`,
`profile rm` and `project rm` all clear keyring items before touching the
manifest. Delete the manifest first and the surviving items become
unreachable — you have lost the only record of what exists and where.

This is also why a delete that fails partway reports how far it got. By the
time one delete fails, every item before it is already gone permanently, and
"nothing happened" would be false reassurance that leaves the rest uncleaned.

And it is why `project rm` is careful about scope: it reaches only the
currently selected backend, but the manifest it destroys was the inventory for
every backend.

`appContext.service(name)` binds the **active** profile, which makes it the
wrong call inside a loop over profiles. A loop clearing a variable across all
of them calls `Cfg.Keyring.ServiceName(project, p, name)` with its own `p`;
`service(name)` there would delete one profile's item N times and leave the
rest untouched.

## The staged commit

The interface holds every edit — a shared or per-profile value, a secret's new
value, a secret promotion, an added or removed variable, an added, renamed,
defaulted, deleted, re-formatted, re-pathed or re-membered output, an added,
removed, defaulted or selected profile — as a
`session.Change` in `session.Staged`, and applies none of them until the review's
`enter`. `Staged` is a set, not a log: a change deciding the same thing as an
earlier one replaces it, so the review lists what will happen rather than every
keypress that led there. `commitStaged` (`internal/cli/ui.go`) then applies the
whole staged slice, in a fixed order that does not depend on the order the
changes were staged in. First it validates the manifest half of the batch on a
clone, so a batch the manifest refuses writes nothing at all -- and so does
one that renames an output onto a held name, or leaves two outputs on one path
(the rule `trenvy output add` enforces); then it runs six phases:

1. **Structural additions.** A staged `add-var`, `add-output` or `add-profile`
   inserts the new entry before anything else runs, so a row this same session
   also renames, sets a value on or removes has an entry to act on from the
   start.
2. **Keyring writes.** Every staged secret value lands in the keyring —
   `set-secret`'s own value, and the value or values *Make secret* moves
   out of the manifest. A value on a profile the batch removes, or on a
   variable it deletes, is skipped: nothing would name the item.
3. **Keyring deletes.** A deleted variable's secret is removed under every
   declared profile, not only the one active in this session, built through
   `Cfg.Keyring.ServiceName(project, p, name)` for each profile `p` in turn.
   A removed profile's secrets are removed the same way, for every secret
   variable, with the list `trenvy profile rm` uses. An item both name is
   deleted once. Only an item `Has` finds is deleted or counted, so a
   partial failure's "deleted A of B" counts stored items alone, the same
   items the review lists.
4. **The state file.** A staged output rename repoints its render records to
   the new name; a staged output removal or path edit forgets its render
   records in every worktree that ever rendered it, and the commit later names
   each file left on disk; a staged format edit blanks its records' digests,
   so the file in the old format reads `STALE` rather than fresh. All are
   saved to `state/<id>.toml` here, ahead of the manifest edit in phase 5 —
   the same state-before-manifest order `trenvy output rename` already uses
   on its own. The machine's active profile lives in the same file: removing
   the selected profile clears it, and a staged use-profile sets it, after
   the clear so "use q, remove p" leaves q. Because this save lands before
   the manifest's, a use-profile may only name a profile the manifest on disk
   already declares and the batch keeps; one naming an unsaved profile would
   break every command if the manifest save then failed, so the batch is
   refused before any write.
5. **Every remaining manifest edit and removal.** Value edits, optional
   flags, secret promotions (flipping `Secret` and clearing `Value`/`Profiles`), output
   defaults, output format and path edits, output removals and glob changes
   are applied to the in-memory
   manifest. An output staged as both added and removed in the same session
   has no entry left after this phase, regardless of which the session did
   first, because phase 1 already created it and this phase deletes it. A
   default-profile change is applied here too.

   Output renames are the one exception: they run in their own pass, strictly
   after every case above, never interleaved with them in staged order. Every
   staged `Change` about an output keys by that row's ORIGINAL identity, but a
   rename is the one operation that moves the manifest key itself -- applying
   it in the same pass as a glob change or a new default meant whichever the
   user staged FIRST decided whether the other's edit landed, since a rename
   processed early deletes the key a later lookup needs. Running renames last
   means every other output mutation always sees the row under its pre-rename
   identity, so staged order can no longer change the outcome.
   Profile removals are the last pass of this phase (5c), after every other
   edit, so a value staged by hand on a removed profile leaves no override
   behind. A removal drops the profile from the manifest's list and from
   every variable's overrides, with the edit `trenvy profile rm` makes
   (`dropProfile`, `internal/cli/profile.go`).
6. **Validate, then save.** `Manifest.Validate` runs before the manifest
   reaches disk; only a manifest that passes is written.

Phases 2 through 4 commit real state — keyring items and the state file —
ahead of the manifest, extending the keyring-first, state-before-manifest
ordering above to the whole batch rather than one command's own write. A
commit that fails partway through reports progress in the units of the phase
it was in: secrets stored versus secrets deleted, or render records
repointed, forgotten or marked stale, never folded into one count that could
not say which kind stalled. Once a keyring write, a keyring delete or the state
file's save has landed, it stays; there is no rollback across phases.

Because the manifest is saved last and atomically, a failure leaves the manifest
on disk as it was before the commit, and every earlier phase can be repeated: a
keyring write overwrites, a keyring delete treats an absent item as done, an
addition checks whether the entry exists, and a render-record repoint that
already happened moves none. That is why the interface keeps the whole batch
staged after a partial failure and retries all of it, rather than tracking
which changes landed. A batch the manifest refuses before the first write
returns `manage.ErrNothingWritten`, which the review reports as "nothing was
written" instead of offering a retry that cannot succeed.

`commitStaged` returns its result (the count saved, and the files left in place
by output removals and path edits) instead of printing it, because stdout
belongs to the interface while it runs.

## The render guards

`render` writes secrets into a working repository. Two things can go wrong that
the user will not notice until much later: the file gets committed, or trenvy
silently destroys something.

Render now and Remove rendered file, on the interface's Outputs screen, call the
same `renderOutput` and `cleanOutput` the `render` and `clean` commands do, so
both guards below apply to them unchanged and a refusal reaches the screen as
the command's own message. They act on the definition on disk, not on staged
edits, which is why they are unavailable while an output has any.

**The git guard.** `render` refuses a path `git check-ignore` does not report
as ignored, and names the line to add to `.gitignore`. `--allow-tracked`
overrides it. Outside a repository — or with `git` missing entirely —
`GitIgnored` returns true and the write proceeds: nothing can be committed from
there, so the guard has nothing to protect and must not block a legitimate
render.

**The overwrite guard.** The state file records two hashes per render, and
keeping both is what separates three conditions a single in-file header would
conflate:

- `Sum` — an HMAC of the resolved values, keyed by the per-project salt.
- `File` — a plain SHA-256 of the bytes written.

Which gives five statuses:

| status | meaning |
|---|---|
| `absent` | no file on disk |
| `fresh` | trenvy wrote it, it is current, nobody touched it |
| `STALE` | trenvy wrote it, but the manifest or the keyring changed since |
| `MODIFIED` | trenvy wrote it, then a human edited it |
| `FOREIGN` | the file exists and trenvy has no record of writing it here |

The three that should give someone pause are upper-cased so they stand out in a
status table that is otherwise lower case. `MODIFIED` is checked *before*
`STALE`: a hand edit is the more urgent fact, because overwriting it loses
work, whereas staleness only means the file is behind.

`CheckWritable` refuses `FOREIGN` and `MODIFIED` without `--force`, and takes a
`verb` — "overwrite" for `render`, "delete" for `clean` — because telling
someone running `trenvy clean` to "re-run with --force to overwrite" gives them
the wrong mental model of what they are about to lose.

**The sum is keyed.** The digest lives in the state file. An unkeyed hash
of a low-entropy secret sitting there could be brute-forced, which would mean
trenvy's own bookkeeping weakened the secrets it exists to protect. The salt is
32 random bytes generated at `project init`.

**State lives beside the manifest, not in the rendered file.** A JSON
output has no comment syntax, and a rendered `public/env.json` may be served to
browsers and must carry nothing but the application's own configuration.

## Projects and worktrees

`Registry` maps a **project root** to an id. The project root is the directory
holding every worktree. `Lookup` walks up from the working directory and the
deepest registered ancestor wins, so a project nested in a monorepo shadows the
outer one. Matching is on path boundaries, not string prefixes: `/w/repo-old`
is not inside `/w/repo`.

`resolve.WorktreeRoot` is a different thing: the specific checkout an output's
relative path resolves against, from `git rev-parse --show-toplevel`.

Separating them lets one manifest serve every worktree, including ones created
long after the project was registered: a new `git worktree add` needs no trenvy
setup at all.

`project move` calls `State.RebaseWorktrees` for the same reason. Render records
store absolute worktree paths; without rebasing them, a move makes `State.Find`
stop matching and `guard.Inspect` classifies every already-rendered file as
`FOREIGN`, so the guard meant to protect the user starts fighting them.

## The config directory

| path | holds |
|---|---|
| `config.toml` | the keyring service-name pattern |
| `registry.toml` | project id → project root |
| `projects/<id>.toml` | the manifest: variables, outputs, profiles |
| `state/<id>.toml` | what was rendered where, with both digests |
| `secrets.age` | the file backend's encrypted document, when in use |

`config.Dir()` checks `TRENVY_CONFIG_DIR`, then `XDG_CONFIG_HOME`, then
`~/.config/trenvy`. Directories are created `0700` and every file goes through
`fsutil.WriteAtomic` at `0600`.

Keyring service names come from `KeyringConfig.ServiceName`, rendering
`trenvy/{project}/{profile}/{name}` by default. `Validate` rejects a custom
pattern with no `{profile}`: it would render one service name for every
profile, so `secret set` under `dev` and under `prod` would overwrite the same
item and a profile switch would silently read the wrong credential. Failing at
load is the only place that is visible before damage is done.

## Testability and hermeticity

Two seams carry the whole test suite.

**`TRENVY_CONFIG_DIR`.** Checked first in `config.Dir()`, ahead of
`XDG_CONFIG_HOME` and the home directory. A test sets it to `t.TempDir()` and
no real configuration can be read or written.

**`store.NewMem()`.** An in-memory `Store`, injected through
`cli.Options.NewStore`, so no test touches a real keyring.

Together those make the suite hermetic: it needs no unlocked login keychain,
which CI runners do not have, and it can run anywhere in any order.

`cli.Options` is the rest of the seam — streams, working directory, and two
function fields:

```go
Run    func(*exec.Cmd) error                                     // spawn a child
RunTUI func(tui.Model, func(string) (string, error)) (tui.Model, error)
```

Swapping `Run` lets a test assert on a child's `argv` and environment without
spawning anything, which is how the "`exec` strips `TRENVY_PASSPHRASE`" and
"`exec` exports `TRENVY_PROFILE`" guarantees are pinned. Swapping `RunTUI` lets
a test supply a finished model instead of a terminal session.

Everything runs through `cli.Execute(opts, args)`, so tests drive the whole CLI
in-process rather than shelling out to a binary.

The one test that is not hermetic is the macOS Keychain integration test, which
skips unless `TRENVY_INTEGRATION=1`. It is not in CI.

## Lazy store opening

`appContext.store()` opens the secret store on first use and memoizes the
result — including the error — via a `sync.Once`.

The memoized error matters as much as the memoized store. `NewStore` can fail
outright on a project with zero secrets, because the file backend errors when
`TRENVY_PASSPHRASE` is unset; nothing opens a store eagerly in `resolveApp`, so
a project that never touches a secret never pays for one.

It also places the failure usefully. In `secret set` the store is resolved
after the manifest has been validated and before it has been saved, so an
unusable backend reports itself while nothing has been written yet.
