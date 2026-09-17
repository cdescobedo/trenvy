# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project intends to follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

**There are no releases yet.** Nothing has been tagged, there are no published
binaries, and the only way to install trenvy is from source at a commit of your
choosing. Everything below is unreleased work on the way to a first tag, and
the command surface may still move under you.

## [Unreleased]

### Breaking

- **The tool is now `trenvy`, not `envctl`.** The binary, the module path
  (`github.com/cdescobedo/trenvy`), the config directory, every environment
  variable prefix, and the keyring namespace all changed together.

  **You will lose access to secrets stored by `envctl`.** Secret values are
  addressed by a service name rendered from a pattern, and the default pattern
  is now `trenvy/{project}/{profile}/{name}`. Items written under the old
  `envctl/...` namespace are still in your keyring, but trenvy will not look
  for them and cannot find them. Before switching, either read the old values
  out with the old binary, or retrieve them from your platform's keyring UI
  (Keychain Access, Seahorse, Credential Manager) and re-enter them with
  `trenvy secret set`.

  The environment variables renamed with the tool: `ENVCTL_CONFIG_DIR`,
  `ENVCTL_BACKEND`, `ENVCTL_PASSPHRASE` and `ENVCTL_PROFILE` are now
  `TRENVY_CONFIG_DIR`, `TRENVY_BACKEND`, `TRENVY_PASSPHRASE` and
  `TRENVY_PROFILE`. The config directory moved from `~/.config/envctl` to
  `~/.config/trenvy`.

- **Targets are gone, split into profiles and outputs.** The old `[targets.*]`
  manifest block conflated two independent questions — what a value is, and
  where it gets written — into one noun. They are now separate axes:
  a *profile* decides what the values are, an *output* decides where they go,
  in what format, and which subset.

  A pre-profile manifest is **rejected, not migrated**. `[targets]` and
  `default_target` are captured on load purely so the error can name the
  problem; TOML would otherwise ignore the unknown keys and the manifest would
  silently parse as a project with no outputs. Recreate it with
  `trenvy project init`, then `trenvy import`, then re-enter the secrets.

- **Process targets are gone.** `exec` and `shell` no longer take an output
  argument, because process delivery is the verb rather than a configured
  destination. The old `trenvy exec local -- cmd` spelling is refused by name
  rather than silently treated as a command to run. Choose the values with
  `--profile`.

### Added

- **Profiles.** A project declares `profiles` and a `default_profile` in its
  manifest. The active profile comes from `--profile`/`-p`, then
  `TRENVY_PROFILE`, then the manifest default; a name from any source must be
  declared, and the error says which source supplied a bad one.
- **Per-profile values.** `trenvy set` writes a value for the active profile.
  `--shared` writes the default used by every profile that has no value of its
  own.
- **Per-profile secret namespacing.** Keyring items are addressed as
  `trenvy/{project}/{profile}/{name}`, so a variable's `dev` and `prod`
  credentials are separate items and cannot collide. A custom
  `keyring.service` pattern that omits `{profile}` is rejected at load, because
  it would make every profile share one item.
- **The `profile` command group:** `profile` (which one is active, and why),
  `profile list`, `profile add`, `profile rm`, `profile use` (emits a shell
  export for `eval`), and `profile default`.
- **The `project` command group:** `project init`, `project list`,
  `project move` (repoints a project after its directory moved, and rebases
  the render state so already-rendered files are not misread as FOREIGN), and
  `project rm`.
- **`trenvy get <NAME>`** prints one variable's value and nothing else, so it
  composes with `$(...)` and pipes. Only the named variable is resolved, so it
  costs at most one keyring read.
- **`trenvy show [output]`** lists the active profile's variables with secrets
  masked. A masked table never opens the keyring at all. `--reveal` prints real
  values; `--format dotenv|json` writes what `render` would write to stdout and
  implies `--reveal`.
- **Shell completion** for variable, output, profile and project names, served
  live by the binary on every TAB rather than baked into a generated script.
  Secrets are tagged `stored` or `MISSING` only where the backend can answer
  presence cheaply and without a prompt.
- **`status --quick`** skips reading secret values; freshness for an output
  holding at least one secret is then reported as unknown.
- **`trenvy output list`** lists a project's declared outputs -- name, format,
  path, and which variables each one selects, in plain prose (`all`, `all
  except X, Y`, or an explicit list) -- and marks the one `project.output`
  points at. A project with none, because it only ever feeds `exec` or
  `shell`, gets a hint instead of an error or a bare header.
- **`trenvy output add <name> --format <dotenv|json> --path <path>`** defines a
  new output, with `--include`/`--exclude` globs to narrow what it selects. It
  refuses a name that fails validation, a second output on a path another
  output already renders to, and always validates the whole manifest before
  writing, so a bad `add` leaves the manifest untouched. It deliberately does
  not claim `project.output` -- `add` is explicit, so `output default` is how
  a user says which output is the default.
- **`trenvy output default <name>`** sets `project.output`, the output
  `render` and `show` use when given no argument. It refuses a name that is
  not a defined output.
- **`trenvy output rename <old> <new>`** renames an output in the manifest and
  repoints its render records in the same operation, so a file already
  rendered under the old name keeps reading as fresh instead of `FOREIGN`.
  Render records key on `(worktree, output name)`; renaming the manifest key
  alone -- the only option before this command -- left the state file
  pointing at a name that no longer existed. It refuses a `<new>` that is
  already defined, and updates `project.output` when it named the output
  being renamed.
- **`trenvy output rm <name>`** removes an output. It refuses when the output
  has render records in any worktree, naming every affected worktree and both
  ways forward -- `trenvy clean <name>` in each one, or `--force` to drop the
  records and leave the files in place -- because a silent delete would orphan
  files under a name `trenvy clean` can no longer look up. `--yes` and
  `--force` are independent: `--yes` answers the confirmation prompt, `--force`
  answers the render-record refusal, and neither bypasses the other. Removing
  the output that held `project.output` clears it and says so, since a bare
  `trenvy render` then needs an explicit argument.
- **`trenvy ui`** opens the tabbed management interface: one grid of every
  variable against every profile, replacing `edit`'s single-profile table for
  projects that keep more than one. It refuses outside a registered project,
  the same as `edit` and `status`. Secret presence for the whole grid loads
  eagerly through `Has`, never `Get`, so opening it never costs a keyring
  authorization prompt; a cell's real value is only ever fetched on an
  explicit reveal. The Overview tab's per-output freshness reuses `status`'s
  own `guard.Inspect` judgment rather than a second implementation, always in
  `--quick` mode, for the same reason presence stays `Has`-only: an output
  that also selects a secret is reported `unknown`, with `trenvy status` as
  its fix, instead of paying for the read. Edits, secret values and new or
  removed variables are staged in memory and committed together with `w`:
  keyring writes land first, then keyring deletes (a deleted variable's
  secret is removed under every profile, not just the active one), and only
  then the manifest, once it passes the same validation `edit` and `set`
  already require -- so a rejected batch never leaves a manifest entry
  pointing at a secret that was never stored. Quitting any other way writes
  nothing at all. A keyring failure partway through a batch cannot be undone,
  so the error reports secrets stored and keyring items deleted as two
  separate counts, never folded into one number that could not say which
  kind stalled. The session that staged the batch has already ended by
  then, so the fix is not a resume: it names `trenvy ui` as a fresh session
  to redo whatever the failed run did not finish, the same honesty `edit`'s
  own partial-failure handling practices. The Overview tab is the landing
  screen: it repeats `status`'s own project, profile (and where it came
  from) and backend facts, then lists every detected problem with the exact
  command that fixes it, or says plainly that nothing needs attention when
  there are none. Pressing enter on a problem jumps straight to the
  Variables cell it names -- the right row and the right profile column --
  or, for a problem about an output's freshness, to the Outputs tab, since
  no single cell answers it. `p` opens a switcher over every registered
  project and, on a different choice, reloads the session against it in
  place -- no quitting and re-running `trenvy ui` from another directory.
  Register and remove stay off the panel; those remain `trenvy project`'s
  own, rare and destructive enough to be the user's explicit decision.
  Switching with staged changes asks first, because those changes live only
  in this session's memory (see the batch commit above) and switching away
  destroys them outright rather than deferring them; declining leaves both
  the changes and the current project exactly as they were. A registry of
  exactly one project -- the current one -- says plainly there is nothing to
  switch to, instead of a panel with nothing useful in it. `q` asks first when
  staged changes are present, the same y/anything-else gate `internal/tui`
  already uses for its own quit confirmation; declining leaves the staged
  changes and the cursor exactly where they were. Ctrl+C still cancels
  unconditionally, including from that confirmation itself, so a user who
  cannot remember any other key always has an exit that writes nothing. A
  status line, visible on every tab, names the staged count and the w/q keys
  that resolve it -- and says plainly that nothing is staged rather than
  offering to save or discard when there is nothing to lose. `d` marking a
  row deleted and `e` editing one of its cells no longer fight over the same
  variable: editing a deleted row cancels the delete instead of leaving it
  staged underneath the edit, and deleting a row that already has a staged
  edit is refused instead of silently discarding it -- either order used to
  be able to commit both, which read `Vars[name]` after a delete in the same
  batch had already removed it and rewrote the variable from scratch with
  every other profile's value gone. The Overview no longer flags a
  permanent, unfixable problem for every output that selects a secret: it
  always classifies freshness in `--quick`-style mode to avoid a keyring
  read on every launch, and that mode used to report "unknown" for such an
  output before even checking whether the file existed. It now checks
  first -- a file that was never rendered, or one `trenvy` has no render
  record for, or one hand-edited since, are all decidable without reading
  the secret, and a match against its own record is treated as fine rather
  than as an alarm nothing pressable inside `trenvy ui` could ever clear. A
  commit that fails manifest validation now always says the rest of the
  batch was lost, not only when the batch also touched the keyring: a bare
  `a NEW_VAR` (nothing else set on it) failing Validate used to reach the
  terminal as a raw manifest error with no word that every other edit
  staged that session -- a value typed into some other cell, say -- was
  gone too. A short key reminder is now visible on every tab regardless of
  staged count, and `?` opens the full key list -- the binary previously
  documented none of its own keys and relied entirely on the README, which
  works only for someone who already knew to go look.

### Changed

- `show` with no argument answers for the whole profile rather than inheriting
  `project.output`. An output is a filter on "what are my values", not a
  precondition for asking, so a project with no outputs can still be inspected.
- `exec` now exports `TRENVY_PROFILE` into the child environment, so a script
  that itself calls trenvy stays on the profile its parent was told to use
  instead of quietly falling back to the manifest default and reading a
  different keyring namespace.
- `project rm` states its blast radius before deleting, and its help and output
  now scope the promise to the currently selected backend rather than implying
  it reaches every backend.
- Outputs are optional: a project that only ever feeds `exec` and `shell` needs
  none, and the manifest no longer requires one.
- **`trenvy import` now creates an output by default.** It used to import
  variables and no destination, leaving nothing to render. The name is derived
  from the sample's directory (`src/Demo.Api/.env_example` becomes `api`), and
  `import` prints it along with the `output rename` command. `--no-output`
  restores the old behaviour; `--output <name>` still names it explicitly.
- **Re-importing an unchanged sample no longer needs `--force`.** `trenvy import`
  refused whenever a variable already existed in the manifest, even when the
  sample's value was identical and the import would have changed nothing. It now
  refuses only when a value or a secret flag actually differs, so picking up a
  newly added variable from a sample no longer means re-confirming every
  variable that was already there.

### Fixed

- **`trenvy status` lists every variable, not only secrets.** A plain variable
  was invisible, which hid the case that matters: a value set under one profile
  and not another is a valid manifest that `render` refuses the moment you
  switch profiles. Those rows now read `MISSING` and name the `set` that fixes
  them. Optional variables are shown as `optional`, because they are omitted
  from the rendered file and nothing said so.
- **The active profile's source reads `default` rather than
  `default_profile in the manifest`.** The three sources are now `--profile`,
  `TRENVY_PROFILE` and `default` — each naming its mechanism, so `trenvy
  profile` and `trenvy profile list` agree, and an error that blames a source
  reads the same way wherever it appears.

- **`trenvy set` warns when a variable name cannot reach a dotenv output.** A
  name that is not a valid shell identifier — `feature.flag`, say — is a legal
  key in a json output and is delivered to a child process fine under `exec`,
  where Python and Node read it from `os.environ` and `process.env`. Only
  dotenv needs an identifier, because its file is sourced by a shell. `set` now
  says so at the moment you type the name, listing the dotenv outputs that
  select it, instead of leaving you to discover it at `render` time. It warns
  and stores the value; it never refuses, so a json-only project is unaffected.

- `trenvy import --output <name>` now reports a bad name (e.g. `my.app`) as
  the argument problem it is, instead of letting it reach `Manifest.Validate`
  and coming back as `invalid manifest: output "my.app": ...` -- the same
  misattribution already fixed for `output add` and `output rename`.
- `trenvy import` no longer silently drops a variable whose name is not a
  valid shell identifier (e.g. `GROUP/KEY`) from the output it creates.
  It wrote a literal `["*"]` include glob, and glob `*` does not cross `/`,
  so such a variable matched nothing and was left out of the rendered file
  with no warning. `trenvy render` on that output now reports the same
  `"... is not a valid shell identifier; use a json output, or rename the
  variable"` error that `output add`'s outputs already reported for the same
  variable -- an error to fix, instead of a value silently missing from the
  file.
- `trenvy import --output <name>` now refuses a name that already names a
  defined output, matching `output add`'s guard. It used to silently replace
  the existing output's format and path, orphaning whatever file it used to
  render -- `trenvy clean <name>` would then target the wrong path, so a
  rendered file that may hold secrets could never be removed by trenvy.
  Importing into the same path a second time still reuses the existing output
  silently; only redefining one by name is refused.
- `exec` strips `TRENVY_PASSPHRASE` from the child environment. It unlocks the
  file secret backend, and a child process has no business inheriting it just
  because it inherits everything else.
- Partial keyring failures during `unset`, `edit`, `profile rm` and
  `project rm` now report how far the run actually got and what survived,
  instead of implying nothing happened. By the time a delete fails, every item
  before it is already gone permanently, and false reassurance is how the rest
  never gets cleaned up.
- A stale exported `TRENVY_PROFILE` names its own escape hatch
  (`unset TRENVY_PROFILE`) in the error, because every command — including
  `profile use`, the one that would fix it — validates the variable before its
  body runs.
- `profile list` no longer pads empty notes into trailing whitespace.
- `resolve.Value`'s "not in the keyring" hint now always includes
  `-p <profile>`. It used to suggest a bare `trenvy secret set <NAME>`, which
  stored the value under whichever profile happened to be active rather than
  the one the error was actually about — a paste-and-rerun that silently left
  a credential in the wrong keyring namespace and the original render still
  failing. (`status`, `set` and `import` had the same bug in their own hints;
  fixed below.)
- `status`'s `MISSING` hint, `set`'s secret-vs-plain refusal, and `import`'s
  post-import hint now include `-p <profile>` too, matching `resolve.Value`
  and `profile add`. Every command that suggests `secret set` now reproduces
  the invocation that actually works, instead of leaving three siblings
  behind the fix above.
- `set --shared` reported "set %s for every profile", but `resolve.Value`
  checks a profile's own override before the shared default it writes, so any
  profile holding one was unaffected. The success line now matches
  `--shared`'s own help text ("the default used by every profile without its
  own value") and names any profiles left unaffected by their own override.
- `unset` and `secret rm` now say what they actually did rather than what a
  fixed-text success line implies. `unset` reports how many profiles' secrets
  it cleared, matching the count `profile rm` and `project rm` already
  report. `secret rm` distinguishes a real deletion from finding nothing
  stored for the active profile — previously both printed the same "deleted"
  line, which in a tool whose headline hazard is acting on the wrong profile
  told a user who ran it against the wrong one that a secret was gone when it
  never existed there. `profile rm`'s partial-failure error now also names the
  full keyring service (matching `unset` and `project rm`) and states how many
  secrets were already deleted by the time it failed.
- The Keychain backend's `Set` sent a trailing `quit` line to `security -i`,
  which has no such command and terminates on EOF instead. `security` printed
  `unknown command "quit"` and exited 1 after the write had already
  succeeded, so every real Keychain write was reported as a failure.
- `report()`'s exit-code passthrough now matches only `*cli.ChildExitError`,
  the type `exec` and `shell` wrap around the error from running the user's
  own command, instead of any error that unwrapped to a bare
  `*exec.ExitError` anywhere in its chain. The Keychain backend also shells
  out to `security(1)` internally, so a bare-`*exec.ExitError` match treated a
  Keychain write failure as if it were the user's own command exiting —
  printing nothing and passing through security(1)'s exit code — which,
  combined with the `quit`-command bug above, could leave the keyring and the
  manifest silently diverged.
- `import` now sets `project.output` when the manifest has no default yet, so a
  fresh project's first output becomes the one `render`, `clean` and `show` use
  with no argument. This applies whether the output was named with `--output` or
  derived from the sample's directory. `project init` still sets no output — it
  creates none to default to — and an output a user already chose as the default
  is never replaced by a later import.
- **Output names are validated.** An output name must be letters, digits, `-`
  or `_`, and may not begin with `-` — the rule profile names already followed.
  A manifest whose output name does not conform, `[outputs.my.app]` for
  instance, now fails to load instead of being accepted and behaving oddly
  later. Rename an existing output with `trenvy output rename <old> <new>`
  rather than by hand -- it updates the manifest and the render state
  together, so an already-rendered file does not start reading as `FOREIGN`.
- **Project ids are validated.** An id is both a segment of the keyring service
  name and a path segment of the manifest and state files, so `--id 'a/b'`
  reshaped the keyring namespace and `--id '../../x'` wrote outside the config
  directory. `trenvy project init` now refuses an id that is not letters,
  digits, `.`, `-` or `_`, that begins with `.` or `-`, or that is a Windows
  device name (`con`, `aux`, `nul`, `com1`-`com9`, `lpt1`-`lpt9`, which Win32
  opens as devices even with a `.toml` extension), and refuses it before
  registering or writing anything. When the id is defaulted from the
  root directory's name, the error points at `--id`. Profile names already had
  this rule; project ids did not.
