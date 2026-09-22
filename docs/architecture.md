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
    |   internal/render   internal/tui  |
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
owns everything user-facing. `tui` is the interactive table behind `edit` and
`import`; `ui` is the tabbed management interface behind `trenvy ui`.

### `internal/ui` and `internal/tui`

`internal/cli` drives two interactive views. `tui` is the flat table `edit`
and `import` open: one profile, one column of values per variable. `ui` is
the matrix `trenvy ui` opens: every variable against every profile, with an
Overview tab of detected problems, an Outputs tab for defining and editing
which files get written and what each one selects, and a placeholder
Profiles tab around them -- profile management is still `trenvy profile`'s
job from the command line.

The two packages share no code and no state. A list of one profile's
variables and a matrix over every profile are different shapes, so each
package owns its own model, view and `Update`; a change to one cannot alter
what the other renders. `ui` loads secret presence through `Has` only, never
`Get`, the same rule `cli`'s `status` command and its completion paths
already follow, so opening `trenvy ui` never raises a keyring authorization
prompt by itself.

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

`trenvy ui` holds every edit — a cell value, a secret promotion, an added or
removed variable, an added, renamed, defaulted, deleted or re-membered
output — as a `ui.Change` in memory, and applies none of them until `w`.
`commitStaged` (`internal/cli/ui.go`) then applies the whole staged slice in
six phases, in a fixed order that does not depend on the order the changes
were staged in:

1. **Structural additions.** A staged `add-var` or `add-output` inserts the
   new entry before anything else runs, so a row this same session also
   renames, sets a value on or removes has an entry to act on from the start.
2. **Keyring writes.** Every staged secret value lands in the keyring —
   `set-secret`'s own value, and the value or values an `s` promotion moves
   out of the manifest.
3. **Keyring deletes.** A deleted variable's secret is removed under every
   declared profile, not only the one active in this session, built through
   `Cfg.Keyring.ServiceName(project, p, name)` for each profile `p` in turn.
4. **The state file.** A staged output rename repoints its render records to
   the new name; a staged output removal forgets its render records in every
   worktree that ever rendered it, and the commit later names each file the
   removal leaves on disk. Both are saved to `state/<id>.toml` here, ahead of
   the manifest edit in phase 5 — the same state-before-manifest order
   `trenvy output rename` already uses on its own.
5. **Every remaining manifest edit and removal.** Cell values, secret
   promotions (flipping `Secret` and clearing `Value`/`Profiles`), output
   defaults, output removals and glob changes are applied to the in-memory
   manifest. An output staged as both added and removed in the same session
   has no entry left after this phase, regardless of which the session did
   first, because phase 1 already created it and this phase deletes it.

   Output renames are the one exception: they run in their own pass, strictly
   after every case above, never interleaved with them in staged order. Every
   staged `Change` about an output keys by that row's ORIGINAL identity, but a
   rename is the one operation that moves the manifest key itself -- applying
   it in the same pass as a glob change or a new default meant whichever the
   user staged FIRST decided whether the other's edit landed, since a rename
   processed early deletes the key a later lookup needs. Running renames last
   means every other output mutation always sees the row under its pre-rename
   identity, so staged order can no longer change the outcome.
6. **Validate, then save.** `Manifest.Validate` runs before the manifest
   reaches disk; only a manifest that passes is written.

Phases 2 through 4 commit real state — keyring items and the state file —
ahead of the manifest, extending the keyring-first, state-before-manifest
ordering above to the whole batch rather than one command's own write. A
commit that fails partway through reports progress in the units of the phase
it was in: secrets stored versus secrets deleted, or render records
repointed versus forgotten, never folded into one count that could not say
which kind stalled. Once a keyring write, a keyring delete or the state
file's save has landed, it stays; there is no rollback across phases.

## The render guards

`render` writes secrets into a working repository. Two things can go wrong that
the user will not notice until much later: the file gets committed, or trenvy
silently destroys something.

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
