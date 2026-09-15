# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project intends to follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

**There are no releases yet.** Nothing has been tagged, there are no published
binaries, and the only way to install trenvy is from source at a commit of your
choosing. Everything below is unreleased work on the way to a first tag, and
the command surface may still move under you.

## [Unreleased]

### Fixed

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

### Fixed

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
- `import --output <name>` now sets `project.output` when the manifest has no
  default yet, so a fresh project's first output becomes the one `render`,
  `clean` and `show` use with no argument. `project init` still sets no
  output — it creates none to default to — and an output a user already chose
  as the default is never replaced by a later `import --output`.
