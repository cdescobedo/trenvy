# Design history

How trenvy came to be what it is, one piece of work at a time, in order. For
each: the problem it solved, what was built, the decisions that still shape
the code and why, and what was deliberately left out.

Read this when you want to know *why* something is the way it is before
changing it. For how things work now, the code and
[architecture.md](architecture.md) win; where this history and the code
disagree, the code is right and the section says what changed. The
interface's current rules are in [tui-design.md](tui-design.md), and what is
next is in [roadmap.md](roadmap.md).

All of it happened between 2026-09-11 and 2026-10-06, on one machine, with
no remote. Early commit hashes are not cited: that part of the history was
rewritten once, to scrub real project data out of test fixtures, so those
hashes no longer exist.

## 1. The original design: envctl (2026-09-11)

### The problem

Environment configuration for a multi-worktree repository was spread across
three places: a task runner's config sitting above the worktrees, a shared
shell script that read values out of the macOS Keychain, and a sample `.env`
inside the repository. The variable list existed three times, and adding a
variable meant editing all three with nothing to catch them disagreeing. On
top of that, the file-writing task was a shell heredoc that hard-coded the
variable order and silently stopped working when its `sed` pattern stopped
matching; the whole setup was macOS-only; and there was no way to edit
values in bulk or seed a project from an existing `.env`.

### What was built

The tool, then called **envctl**: one manifest per project, outside the
repository; secrets in the OS keyring and nowhere else; `render` to write a
dotenv or flat JSON file into any worktree, `exec` and `shell` to inject
values into a process with nothing on disk; `status` for every diagnostic;
and a table interface (`internal/tui`) for `init`, `import` and `edit`.

### Decisions that still shape the code

- **The manifest lives in the config directory, not the repository.** Every
  worktree, including one created months later, inherits it with no setup,
  and team repositories stay clean. This is why the registry exists: a
  project is found by walking up from the working directory to a registered
  root, with no git dependency, and the deepest registered root wins so a
  project nested in a monorepo shadows the outer one.
- **The project root and the worktree root are different things.** The
  project root selects the manifest; the worktree root
  (`git rev-parse --show-toplevel`, else the directory) is what an output's
  relative path resolves against. Keeping them apart is what makes a new
  worktree free.
- **One keyring item per secret.** Windows Credential Manager caps a blob at
  2560 bytes, which rules out one blob per project. The cap is enforced on
  every backend (`store.MaxValueBytes`), so a value that works on one
  machine does not fail on another.
- **The manifest is the authoritative inventory.** macOS cannot list keyring
  items without a prompt per item, so trenvy never enumerates the keyring.
  Every write and delete is ordered around that fact, so a partial failure
  can leave an unreferenced keyring item but never a manifest entry pointing
  at nothing (keyring first, manifest last; `secret demote`, added later,
  saves the manifest first because the values move into it); see
  [architecture.md](architecture.md#the-manifest-is-the-authoritative-inventory).
- **`Has` and `Get` are separate operations, and the macOS backend is
  hand-rolled over `security(1)`.** Reading a Keychain value can raise an
  authorization prompt; checking that it exists cannot, but only if nothing
  passes `-w`. A library's presence check that reads the value would have
  cost one prompt per secret on every `status`. Hand-rolling it also kept the
  build cgo-free, which CI now checks by cross-compiling.
- **No value transforms.** An early draft let a target rewrite `localhost`
  to `host.docker.internal`, copying the old shell task. Looking at the
  compose files showed the rewrite compensated for a network-topology
  accident, not a configuration need. The principle we kept: when a
  configuration tool needs to transform a value, it is usually papering over
  an inconsistency one layer down. Store the differing strings as plain
  values instead.
- **Delivery is a verb.** `render` writes a file, `exec` runs one process,
  `shell` starts a subshell. There is no `cd` hook, matching the opt-in
  stance of the script it replaced.
- **Render bookkeeping lives in the state file, not the rendered file.** JSON
  has no comment syntax, and a rendered `public/env.json` may be served to
  browsers. The state keeps two digests per render — a keyed HMAC of the
  values and a plain hash of the bytes — which is what separates *stale*,
  *hand-edited* and *not ours* (`FOREIGN`). The values digest is keyed by a
  per-project salt because an unkeyed hash of a short secret could be
  brute-forced from the state file.
- **Guards on by default:** refuse a path git would track, refuse to
  overwrite or delete a file trenvy did not write or that was edited by hand,
  write `0600` atomically, and warn about stale rendered files before `exec`
  and `shell`, because a stale `.env` silently beats an injected environment
  in many frameworks.
- **The interface's `Update` is pure**, with keyring access issued as
  commands, so the state machine is testable without a backend. The redesign
  kept this rule.
- **Test-driven, with an in-memory store from the first test**, so no test
  ever touches a real keyring.

### Deliberately left out

Team distribution (the manifest is personal), remote secret backends,
transforms and derived variables, nested JSON and a shell-export format,
renaming variables per output, a global scope that would absorb personal
shell scripts, and per-target secrets. The design was written so none of
these is foreclosed.

### What changed since

Targets were split into profiles and outputs (section 3), the keyring
pattern gained a `{profile}` segment, the tool was renamed (section 2), a
second interface joined the table (section 5), and the redesign replaced
both (section 8).

## 2. The rename to trenvy, and the release-facing decisions (2026-09-15)

### The problem

We renamed the tool partway through the profiles work (section 3), before
anything had been published under the old name.

### What was built

Everything was renamed together: the binary, the module path
(`github.com/cdescobedo/trenvy`), the config directory
(`~/.config/trenvy`), every environment variable prefix (`TRENVY_*`), the
keyring namespace (`trenvy/{project}/{profile}/{name}`) and the Keychain
item kind. The CHANGELOG's *Breaking* section tells an envctl user how to
carry their secrets over.

### Decisions that still shape the code

- **Rename everything at once, and do it then.** The two expensive renames
  are the keyring namespace and the config directory, and both were already
  being invalidated: the profiles work's `{profile}` segment orphaned every
  stored secret, and the manifests were being rewritten by hand anyway.
  Renaming in that window cost nothing extra. Because the config directory
  moved, the old one was left untouched as a rollback, and the stored values
  were carried across by piping the old tool's `get` into the new tool's
  `secret set --stdin`, so no value was retyped or passed on a command line.
- **No migration code.** A pre-profile manifest is rejected with a message
  naming the steps, and `status` lists each missing secret with the command
  that stores it. A one-shot migration command would be dead code the day
  after it ran.
- **Licence: Apache-2.0.** We wanted the tool usable by anyone, and no
  OSI-approved licence can forbid charging for software, so that was set
  aside. A non-commercial licence would have barred use at work, which
  defeated the point. Apache-2.0 adds an explicit patent grant and matches
  the Go ecosystem, cobra included.
- **No NOTICE file, no per-file licence headers, no year upkeep.** A NOTICE
  file's contents must be carried forward by every redistributor, which is a
  permanent obligation on forkers for a project with no third-party notices
  to preserve. Per-file headers would be four lines of noise at the top of
  every file in a codebase whose rule is that comments earn their place. The
  copyright year was set once and is never bumped.
- **The project documents itself like a well-run binary, not a library.**
  Everything but `cmd/trenvy` is under `internal/`, so there is no API to
  document on pkg.go.dev. The docs are a README for users, `CONTRIBUTING.md`,
  `SECURITY.md`, a CHANGELOG and `docs/architecture.md` for contributors.

Running the renamed binary against a real Keychain found two older bugs the
suite could not see, because every Keychain test swapped in a fake runner:

- the script sent to `security -i` ended with `quit`, which `security` does
  not implement, so every macOS `secret set` stored the item and then
  reported failure;
- `main` passed any error that unwrapped to an exit status straight through
  as the exit code with nothing printed, which is right for `exec`'s child
  and wrong for a keyring backend that shells out.

The fixes still shape the code: the Keychain test now inspects the script
sent to the fake runner, and only the child process's own exit status is
passed through (`ChildExitError`, `internal/cli/exec.go`). The lesson kept:
a test double that never looks at what it is sent hides bugs in the thing it
stands in for.

## 3. Profiles, and a reorganised command line (2026-09-14)

### The problem

Two requests arrived together and turned out to be one change. Values
needed to vary by environment (`dev`, `prod`, `personal`), and the only
place to put that was the per-target override, which would have made a
*target* mean both "where the file goes" and "which environment this is".
Separately: a way to move a registered project after its directory moved,
and to delete a project including its secrets.

### What was built

- **Profiles and outputs, two independent axes.** A profile decides what
  the values are, including which keyring item a secret reads. An output
  decides where values go, in what format and which subset, and holds no
  values at all. `[targets]` became `[outputs]`; `default_target` became
  `project.output`; `project.profiles` and `default_profile` were added.
- **Per-profile values** in a sparse, nested `[vars.X.profiles]` table, with
  the variable's `value` as the shared default.
- **Per-profile keyring items:** `trenvy/{project}/{profile}/{name}`.
- **A command line grouped by noun:** `project init|list|move|rm`,
  `profile list|add|rm|use|default`, `secret set|rm`, with the daily
  commands (`status`, `get`, `set`, `show`, `render`, `exec`…) left as one
  word. `exec` and `shell` lost their target, and `get` its output.
- `--profile`/`-p`, then `TRENVY_PROFILE`, then the manifest's default,
  with the source named in `status` and `trenvy profile`.
- `project move` and `project rm`; shell completion for profiles, outputs
  and projects; and, as a follow-up, validation of `project init --id`.

### Decisions that still shape the code

- **No per-output values, ever.** An output selects and formats; a profile
  supplies values. A per-output value table would bring back the conflation
  this work removed. `CONTRIBUTING.md` asks for an issue before any change
  that lets an output carry values.
- **The overrides table is nested** (`[vars.X.profiles]`) rather than bare
  keys on the variable, so a profile called `secret` or `optional` cannot
  collide with a field.
- **Profile names are `[A-Za-z0-9_-]+`** because a profile name is a segment
  of the keyring service name; a `/` would reshape the namespace. A custom
  service pattern without `{profile}` is rejected at load, or every profile
  would share one item.
- **A secret carries no plain value, not even per profile.** A dummy `dev`
  value for a real `prod` secret was a real want, and resolution would have
  served it for free, but it puts a plaintext secret in the manifest. The
  escape hatch is one prompt: `trenvy -p dev secret set NAME`.
- **`set` refuses a secret; `secret set` never takes a value in argv.**
  `argv` is visible to every user through `ps` and lands in shell history.
  A `--secret` flag on `set` was rejected because the obvious thing to type,
  `set TOKEN hunter2 --secret`, would be exactly the mistake.
- **`exec` and `shell` take no output.** An output's subset describes a
  file's audience; a process wants its whole environment. `exec` also exports
  `TRENVY_PROFILE` into the child, so a nested trenvy stays on its parent's
  profile instead of quietly reading another keyring namespace.
- **`show` with no argument answers for the whole profile**, not the default
  output: it is the command you reach for when confused, and a project with
  no outputs must still be inspectable.
- **Deletes go keyring-first, and a partial failure says how far it got.**
  Deleting the manifest first destroys the only record of which keyring items
  to clean up. Every deletion path reports the count already deleted and
  what survived, because "nothing happened" would stop a user re-running it.
- **A loop over profiles builds each service name itself.** The helper that
  builds a service name binds the *active* profile; inside a loop over
  profiles it would delete one profile's item N times. This was caught in
  review and is called out in architecture.md.
- **`project rm` reaches only the selected backend**, and says so: the
  manifest it deletes was the inventory for every backend.
- **`project move` rebases the render state**, or every already-rendered
  file would read as `FOREIGN` and the guard would start fighting the user.
- **`init` moved under `project` with no top-level alias**, by our choice:
  two spellings of one command in `--help` forever was the thing to avoid.
- **A missing-secret hint names the profile it is about**
  (`trenvy -p prod secret set API_TOKEN`). Without `-p`, pasting the hint
  stored the secret under the wrong profile and reported success.
- **Project ids** allow letters, digits, `.`, `-` and `_`, and refuse a
  leading `-`: an id is a path segment and a keyring segment, directory
  names routinely carry dots, and a leading `-` would read as a flag in the
  hints trenvy prints.

### Deliberately left out

A per-profile plain value for a secret (above); YAML and TOML outputs;
per-profile outputs; `unset NAME -p dev` (the interface covers it);
per-worktree current profiles; a shell wrapper function; migrating existing
manifests. Recorded for later: `--dry-run` and before/after inventories for
the removal commands. Rollback was judged the hard part and probably not
worth it: restoring a deleted keyring item means reading its value first and
holding plaintext for the life of the command, against the rule that a
secret is read only when needed. Release engineering (GoReleaser, a first
tag of `v0.1.0`) was also recorded for later; see the roadmap.

### What changed since

`profile use` originally printed `export TRENVY_PROFILE=...` for the shell
to `eval`. Section 6 replaced that with a persisted active profile.

## 4. Outputs management (2026-09-15)

### The problem

`import` with no `--output` created no output, so a user got variables and
nowhere to put them. Nothing listed, added, renamed or removed outputs short
of editing the manifest. A hand-made rename silently broke the render guard,
because render records key on the output's name. And output names were not
validated at all.

### What was built

- `import` creates an output by default, with a name derived from the
  sample's worktree-relative directory (`root` at the worktree root; a
  .NET-style directory such as `src/Demo.Api` becomes `api`, keeping the
  memorable half after the last dot), and `--no-output` for variables only.
- The `trenvy output` group: `list`, `add`, `rename`, `default`, `rm`.
- `ValidOutputName`, checked by the commands and by `Validate`.

### Decisions that still shape the code

- **A derived name agrees with the path that will be rendered**, falls back
  to the format's name when nothing usable is left, and takes `-2`, `-3`…
  when taken. `import` prints the name it chose and the `output rename`
  line, so a poor guess costs one command and never persists silently.
- **One output per path.** Import reuses an output already on the path;
  `output add` refuses a second. Two outputs on one path overwrite each
  other, and the second's record makes the first look hand-edited.
- **`output add` does not claim the default**; `output default` does. The
  first output any `import` creates does claim it when none is set.
- **`output rename` writes the state file first, then the manifest.** Either
  order can fail halfway, but only state-first is fixed by re-running the
  same command. The interface's commit follows the same order (phase 4
  before the manifest save).
- **`output rm` refuses while render records exist**, naming every worktree;
  `--force` drops the records and leaves the files, listing each. `--yes`
  and `--force` answer different questions and neither implies the other.
- **Output names follow the profile-name rule, not the project-id rule.** An
  output name is a TOML key and a state value, never a path or a keyring
  segment, so dots buy nothing, and a leading `-` would make
  `trenvy render <name>` parse as a flag.
- **The list's "selects" column covers all four include/exclude
  combinations.** The first version dropped the exclusions whenever the
  include was explicit, and so claimed a variable reached a file that
  `resolve` kept out of it. A summary that contradicts `resolve` is worse
  than none; it is now built from the same rules.
- **Import writes an empty include, never `['*']`.** An empty include means
  "everything" before any pattern is matched, while `*` goes through
  `path.Match`, whose `*` does not cross `/`. A variable named `GROUP/KEY`
  was silently left out of every imported output; with an empty include it
  reaches the dotenv writer, which refuses it with a clear error.
- **A bad name is reported as a bad argument, not a corrupt manifest**, by
  one pre-flight helper shared by `add`, `rename` and `import --output`.
  `import --output` onto an existing name is refused, as `output add` is.

### Deliberately left out

A membership editor (built in section 5), `init` chaining into `import`
(became setup, section 8), and an `exclusive` flag that would exclude a
variable from every other output, deferred until the editor showed whether
keeping disjoint outputs by hand actually hurt.

## 5. The first management interface, and its outputs phase (2026-09-16 to 2026-09-21)

### The problem

The table interface covered one noun, variables. We wanted one interface
for four purposes: auditing a project, daily use, bulk editing, and
discovering what trenvy can do.

### What was built

`trenvy ui`: an **Overview** tab listing every problem with the command
that fixes it; a **Variables** matrix of every variable against every
profile; a project switcher; and edits staged in memory and committed
together with `w`, which then quit. The second phase added an **Outputs**
tab, an include/exclude editor with an explicit mode, warnings for a
variable no output selects, and `s`, a one-way promotion of a plain variable
to a secret.

### Decisions that still shape the code

- **Stage everything, commit once.** Nothing touches disk until the user
  says so, and quitting any other way writes nothing. The redesign kept
  this and changed only what happens after the save.
- **Presence loads eagerly through `Has`.** One silent call per secret per
  profile costs no prompts; a value is read only on an explicit reveal.
- **The matrix may not invent a second precedence.** Its cell
  classification follows `resolve.Value`, and the code names the manifest
  rule it relies on.
- **Promotion is one way, and the value moves.** Making a variable secret
  moves each profile's value into its own keyring item, written before the
  manifest flips. Going back means reading secrets and writing them into a
  plaintext file, which deserved its own design (it became
  `trenvy secret demote`). A shared value promoted becomes one item per
  profile, and the confirmation says they can drift apart once one is edited.
- **The commit became phased:** structural additions, keyring writes,
  keyring deletes, the state file, manifest edits, then validate and save.
  Three existing rules met here for the first time — state before manifest,
  keyring writes before deletes, additions before removals — and each has a
  test that fails when its order is inverted. See
  [architecture.md](architecture.md#the-staged-commit).
- **Output renames run in a pass of their own**, after every other output
  edit. Applied in staged order, a rename followed by a membership edit
  silently lost the edit, because the rename moved the key first; the other
  order worked. Two keypress orders giving two results was the bug.
- **Removing an output forgets its render records in every worktree** and
  names each file left on disk. A leftover record would make a later output
  reusing the name look hand-edited. The interface does what `output rm
  --force` does rather than refusing, because refusing at save time is the
  wrong moment: `d` already asked for it.
- **The membership editor never writes a literal `*`.** "Only these" with
  nothing checked writes every name into both include and exclude, which
  cancels out, rather than an empty include (which means everything) or an
  exclude of `*` (which a slashed name would slip past).
- **Membership is computed through `resolve.Selects`**, the function render
  uses, never a second glob match.
- **States are typed, not prose.** The output-state words are constants
  shared by `status` and the interface, after a mismatch between two
  spellings of "not rendered" made every new project show a false problem.
- **Quick-mode freshness is reported as unverified, not as fresh or as a
  problem.** It reads no secrets, so it cannot see a changed secret, and an
  unfixable alarm on every secret-bearing output would be noise.
- **A partial failure reports in each phase's own units** — secrets stored,
  keyring items deleted, render records moved — never one blended count.

### Deliberately left out, and what changed since

The Profiles tab was deferred, with two decisions recorded for it: removing
a profile needs a typed confirmation, and renaming a profile is out (its name
is in every keyring item under it). Lazy secret loading was deferred until
eager loading proves slow. Bare `trenvy` opening the interface was left
undecided; it still prints help. The warning for a variable reaching two or more outputs was
later dropped: nothing stops two outputs carrying one variable on purpose.
The whole interface — Overview, matrix and save-quits — was replaced by the
redesign in section 8.

## 6. The active profile (2026-09-22)

### The problem

`trenvy profile use` did not switch profiles; it printed an `export` line
for the shell to `eval`, because a child process cannot change its parent's
environment. Underneath, `default_profile` was doing two jobs: the project's
baseline, and the only lasting way to say "I am working on this one now".

### What was built

A fourth source between the environment variable and the default: this
machine's **active** selection, stored as `active` in `state/<id>.toml`.
`profile use <name>` sets it; `profile use --clear` removes it. Precedence
is `--profile`, then `TRENVY_PROFILE`, then active, then `default_profile`.

### Decisions that still shape the code

- **The state file is the right home:** it is already per project and per
  machine, and holds what has been done here, while the manifest says what
  the project is.
- **`resolve.Profile` stays the single, pure place precedence is decided.**
  The caller reads the state file and passes the value in, as it already
  did for the flag and the environment, so the whole chain is a table test.
- **`--clear` is required, not optional.** Without it an active selection
  would shadow the default forever and the two concepts would collapse.
- **`profile use` writes nothing to stdout.** A legacy
  `eval "$(trenvy profile use x)"` then does nothing harmful. When
  `TRENVY_PROFILE` is set, `use` warns on stderr and names
  `unset TRENVY_PROFILE`, since the export would still win.
- **`profile rm` clears an active selection naming the removed profile**,
  and an unknown active name fails every command with an error naming
  `trenvy profile use --clear`. `--clear` itself bypasses profile
  validation, or the escape hatch would be routed through the check it
  escapes. The hatch still matters for a state file edited by hand or left
  by an older build, which `rm` never saw.

### Deliberately left out

Renaming a profile (each secret would need a read, a write and a delete,
each with a prompt and no atomicity), and any change to what `--profile` or
`TRENVY_PROFILE` mean.

## 7. The lipgloss restyle (2026-09-30)

### The problem

The interface and the command-line tables were aligned with
`text/tabwriter`, which counts runes. Any styling put escape sequences in a
cell, tabwriter counted them as width, and the row shifted — the same class
of bug as an earlier double-width glyph that broke the one view whose job was
alignment. Colour was impossible until the layout engine changed.

### What was built

`internal/style`: `Columns`, a lipgloss-measured replacement for every
`tabwriter` site, then a `Styler` that colours the status words (`MISSING`,
`stored`, `STALE`…) for both the commands and the interface, and bordered
panels for the interface.

### Decisions that still shape the code

- **Replace the layout engine first, add colour second**, in two separately
  mergeable phases, so a column that shifted could not be blamed on colour.
  Comparing `Columns` with `tabwriter` over 20,000 random grids found no
  difference beyond the deliberate ones.
- **An injected `*lipgloss.Renderer`, not lipgloss's global one.** The global
  renderer resolves to no colour under `go test`, so package-level styles
  would work in production and be untestable. The obvious way to force a
  profile, `NewRenderer` with `WithProfile`, silently does nothing in this
  lipgloss version; `SetColorProfile` works. The code says so where someone
  would reach for the wrong one. The redesign's `Theme` follows the same
  pattern.
- **The command line stays flat and 16-colour.** It is routinely piped and
  has no width guarantee, so it never draws a box, and base ANSI colours
  follow the user's terminal theme.
- **Colour is additive.** Every word, bracket and marker stays, so
  `NO_COLOR` or a pipe carries the same information. Machine-readable output
  (`get`, `show --format`) is byte-identical.
- **No `--no-color` flag**: `NO_COLOR` and a non-terminal stdout already
  do it.
- Three deliberate visible changes, all in the CHANGELOG: `status` hint
  arrows align in one column, `show` keeps its name column for an empty
  value, and no row ends in trailing whitespace.
- **A tab inside a value is measured as zero width and not rewritten.** It
  misaligns its row, which is better than silently changing a value the
  user set.

### What changed since

The redesign replaced the interface's styling with `Theme` and palette C,
and the 72-column panel floor went with the old interface. The commands
still use `Styler`.

## 8. The interface redesign (2026-09-30 to 2026-10-06)

### The problem

The interface was hard to use, and the problem was interaction and
hierarchy, not layout mechanics. Driving it against a scratch project
showed, among other things: the matrix showed states (`shared`, `stored`)
rather than values; saving quit; footer hints listed keys that did nothing
on the current tab; one key meant different things on different tabs; text
fields had no way to clear a prefilled value; editing a `shared` cell did not
say whether it changed the shared value or created an override; there were
two interfaces with two visual languages (`ui` and `edit`); and everything
was one colour, which read as washed out.

### What we chose, for the redesign as a whole

- **Serve four jobs equally:** see and change values, fix what is broken,
  set up a project, and manage outputs.
- **Merge `trenvy edit` into `trenvy ui`.** One interface, one visual
  language; both commands open it.
- **A list + detail layout**, so every value is on screen without a
  keypress, secrets masked.
- **Stage, review, save, stay.** Edits stage; `w` opens a review where each
  change can be dropped; saving keeps the session open; `q` with pending
  changes asks first.
- **Fixed verbs plus a `.` action menu.** A key means the same thing
  everywhere, the footer shows only keys that act now, and anything else is
  in a menu that says why an entry is unavailable.
- **One design, built in four phases**, each merged before the next was
  written against the code as it then stood.
- **No new dependency.** The line editor is hand-rolled; the `bubbles` text
  input was offered and declined.
- **Real visual hierarchy instead of one colour:** levels by weight and
  contrast, colour as reinforcement.
- **trenvy's own palette now, the system theme later.** Palette C first;
  following the system theme the way btop does under Omarchy was left for
  later, with the design keeping room for it (every colour behind a named
  role).

Every safety property carried over unchanged: presence through `Has`; the
commit's phase order; secrets masked while typed; promotion one way;
`ctrl+c` writing nothing.

### Phase 1: shell, Variables, review, line editor, theme

Built behind a hidden `trenvy ui --new`, beside the old interface:

- `internal/session`, the UI-free logic (staged changes, problems, the
  matrix), extracted so both interfaces shared it during the transition;
- `internal/manage`, the new interface, with every piece of I/O behind
  `manage.Deps`;
- the Variables screen, values first, with the scope of an edit in its
  title;
- the save review, and saving that reloads from disk and stays open;
- `style.Theme` and palette C with its contrast test.

Decisions that still shape the code:

- **The commit validates the whole batch on a copy before its first keyring
  write.** Before, a batch the manifest would refuse still stored its
  secrets first.
- **The commit returns its result instead of printing**, because the
  interface owns the terminal while it runs.
- **A partial failure keeps the whole batch staged and retries all of it.**
  The first design tracked which changes had landed; checking the code
  showed that unnecessary, because the manifest is saved last and
  atomically and every earlier phase can be repeated. This only stays true
  while nothing runs after the manifest save.
- **Save and stay means the snapshot must match disk.** Input is held from
  the commit until its reload lands, and a failed reload blocks staging
  until a reload succeeds.
- **A secret's revealed value is dropped on every reload and when the cursor
  leaves it**, and a reveal that arrives after a reload is discarded.
- **The detail cursor is held as a profile name, not an index**, because
  rows appear and disappear under it (a secret has no shared row).
- **The detail's label column is sized from the longest profile name**, since
  profile names have no length limit.

### Phase 2: Outputs, and the cutover

The Outputs screen with membership edited in place and three immediate
actions (*Render now*, *Remove rendered file*, *Check now*). Then the
cutover: `trenvy ui` and `trenvy edit` both open the new interface, the old
interface was deleted, and `--new` went away.

What we chose in this phase:

- **Un-secret moved to the command line as `trenvy secret demote`.** The old
  `edit` table was the only way back from a secret, and only for the active
  profile. The interface keeps promotion one way; `demote` reads each
  profile's value, writes them into the manifest, then deletes the keyring
  items, and asks first.
- **`TRENVY_THEME=light|dark`** overrides background detection, which can
  guess wrong; any other value is an error.
- **A field opens with its value selected**, so typing replaces it and an
  arrow keeps it, and **`ctrl+k`** clears to the end.
- **`p` (switch project) moved into the `.` menu**, so no key changes meaning
  between screens.
- **A failed reload blocks staging** until *Reload from disk* succeeds.

Other decisions that still shape the code:

- Variable, output and profile changes are separate namespaces in
  `session.Staged`, so an output named like a variable never disturbs it.
- The immediate actions act on the definition on disk and call the same
  functions the `render` and `clean` commands do, guards included, so they
  are unavailable while that output has staged changes.
- Saving a path edit forgets the old path's render records and lists the
  file left there; saving a format edit marks the record stale, because the
  file is still in the old format.
- Two outputs on one path are refused live and again by the commit.
- Output names may be cut with `…` in lists and titles; values never are.

### Phase 3: Profiles

The Profiles screen: tags, missing counts, add, remove, make default, use on
this machine, and `enter` on a missing name jumping to its cell.

Decisions that still shape the code:

- **A profile added this session gets its values after saving.** Variables
  lists the profiles on disk; showing unsaved ones would thread them through
  promotion, deletion counts and the matrix for a one-keystroke wait.
- **Removing a profile drops the values staged on it**, and keeping the
  profile does not bring them back; the message says so.
- **Use on this machine is refused for an unsaved profile.** The selection
  is saved in the state file before the manifest, so naming a profile the
  manifest does not yet declare would break every command if the manifest
  save then failed. The commit refuses the same batch built by hand.
- **Removal is refused** for the last profile, the default, the profile the
  session is on, and when it would leave a required plain variable with no
  shared value whose only remaining override is on that profile; edits on a
  removed profile's cells are refused.
- **Each removal is confirmed by typing the profile's name**, in turn.
- **Removal counts only keyring items `Has` finds**, in the review and in a
  partial failure's "deleted A of B", for removed profiles and deleted
  variables alike.
- **The manifest half of a profile removal is one function** shared by
  `trenvy profile rm` and the commit.

After phase 3, by our choice, **the Profiles list always shows a profile's
missing count beside its tags**. It had been hidden on tagged or staged rows,
so a profile marked `active ~` gave no sign it was missing anything.

### Phase 4: setup

`trenvy ui` or `trenvy edit` outside a registered project opens setup —
project id and root, a sample to seed from or *Start empty*, a review of its
variables, a summary — instead of failing. Interactive `trenvy import` runs
the same review and summary, and the old table interface was deleted.

What we chose in this phase:

- **In a bare-repository layout, setup prefills the root with the layout's
  root** (the directory holding `.bare` and the worktrees), so every sibling
  worktree finds the project. The field stays editable. Elsewhere it
  prefills the current directory, as `project init` does.
- **Interactive `trenvy import` saves and exits** with its usual report,
  rather than landing in the interface.

Other decisions that still shape the code:

- **Setup is a separate pure model.** The command gathers every fact first
  and writes only after the summary's `enter`, in `project init`'s order
  (validate, manifest, registry). Cancelling writes nothing.
- **Samples are detected in the worktree root only**, by exactly the names
  `import` can turn into an output path; one that does not parse or holds
  nothing is left out. A repeated name is read as dotenv reads it, last one
  wins. At a bare layout's root, which is no worktree, setup says to run it
  from a worktree.
- **The root is checked with the registry's own containment rule**, so setup
  never writes a project that would not then be found from here.
- **A value guessed secret is never shown and never stored**; unticking it
  leaves the row plain and empty. This costs a retype when a value was
  guessed wrongly, which we accepted.
- **Import conflicts need `--force`**; without it the summary offers no
  `enter`.
- **The review dropped the old table's add and filter keys**; both are one
  keystroke away in Variables after setup.
- **After creating, Variables opens with the problems-only filter on** when
  any value is missing (the filter then shows every row with a problem), or
  unfiltered when nothing is, because a list filtered to nothing reads as
  broken.
- **Footers drop whole hints, ordered by importance**, so a narrow footer
  keeps the first hint and the last: on the main screens that is the primary
  action and `? keys`; in setup and the overlays that ask something it is
  `enter` and `esc`, the way forward and the way back.

### After phase 4

Small fixes from running the merged result: the Variables "problems only"
banner wraps instead of being cut (it is the first thing setup shows); the
import header is held to two lines, with a path that cannot fit cut at its
front to keep the file name; a panic in setup's header below the minimum
width was fixed; and `trenvy ui --help` and `trenvy edit --help` say they
open setup outside a project.

### Deliberately left out of the redesign

Following the system theme; palette C in the command-line output; writing
`.gitignore` entries from setup; nested config output; renaming a variable
(shown disabled); renaming a profile. All but the last are on the
[roadmap](roadmap.md).
