# trenvy

One manifest per project, values scoped by profile, secrets in the OS
keyring, rendered into any worktree.

## Install

```bash
go install github.com/cdescobedo/trenvy/cmd/trenvy@latest
```

## Quick start

```bash
cd ~/workspace/demo/demo-backend   # the directory holding the worktrees
trenvy project init --id demo-backend
trenvy import main/src/DemoApp.Api/.env_example --output docker
trenvy secret set GAMMA_TOKEN
trenvy status
```

Then, from any worktree:

```bash
trenvy render docker                  # writes src/DemoApp.Api/.env, 0600
trenvy exec -- dotnet run             # injects into one process, writes nothing
trenvy shell                          # a subshell with the profile's variables loaded
trenvy clean docker                   # removes the rendered file
```

A worktree created later needs no setup — it is under the registered project
root, so it inherits the manifest.

## Commands

| command | does |
|---|---|
| `trenvy project init` | register this project and create its manifest |
| `trenvy project list` | list registered projects and their roots |
| `trenvy project move <id> <root>` | repoint a project after its directory moved |
| `trenvy project rm <id>` | delete a project, its state and its secrets in the current backend |
| `trenvy profile` | show the active profile and where it came from |
| `trenvy profile list` | list the project's profiles |
| `trenvy profile add <name>` | declare a new profile |
| `trenvy profile rm <name>` | remove a profile, its overrides and its secrets |
| `trenvy profile use <name>` | emit a shell export switching profile for this session |
| `trenvy profile default <name>` | set the project's default profile |
| `trenvy import <sample file>` | seed the manifest from a sample `.env` or `.json` file |
| `trenvy set <NAME> <VALUE>` | set a plain value for the active profile |
| `trenvy get <NAME>` | print one variable's value under the active profile |
| `trenvy show [output]` | list values, secrets masked unless `--reveal` |
| `trenvy secret set\|rm <NAME>` | store or delete a secret for the active profile |
| `trenvy unset <NAME>` | remove a variable and its secret in every profile |
| `trenvy edit` | edit this project's variables interactively |
| `trenvy render [output]` | write an output into this worktree |
| `trenvy clean [output]` | remove a rendered file from this worktree |
| `trenvy exec -- <command>` | run a command with the profile's variables in its environment |
| `trenvy shell` | start a subshell with the profile's variables loaded |
| `trenvy status` | show the resolved project, profile, secrets and outputs |

Run `trenvy <command> --help` for a command's flags.

`project rm` only ever reaches the currently selected secret backend (the
`--backend` flag, `TRENVY_BACKEND`, or the platform default) — the same
scoping every other command uses. A secret stored under a different backend
survives the deletion, but the manifest `project rm` destroys was that
secret's only inventory, so once it is gone there is no record left of the
secret, or of which backend it is in.

## Shell completion

```bash
trenvy completion zsh > ~/.local/share/zsh/completions/_trenvy   # or your fpath
trenvy completion bash > /usr/local/etc/bash_completion.d/trenvy
```

The generated script is a thin forwarder: it asks the binary for candidates on
every TAB, so it never goes stale and does not need regenerating when commands
change.

Variable, output and profile names complete from this project's manifest,
filtered to what the command can actually act on:

| command | offers |
|---|---|
| `trenvy get <TAB>` | every variable |
| `trenvy set <TAB>` | plain variables |
| `trenvy secret set\|rm <TAB>` | secret variables, tagged `stored` or `MISSING` |
| `trenvy unset <TAB>` | every variable |
| `trenvy render\|clean\|show <TAB>` | defined outputs, tagged with their path |
| `trenvy -p <TAB>` | declared profiles |
| `trenvy project move\|rm <TAB>` | registered project ids |

Secrets are tagged `stored` or `MISSING` where the backend can answer that
cheaply, which on macOS it can -- the Keychain reports presence without an
authorization prompt. The file and system backends answer only by decrypting
or reading, so there the tag is dropped rather than making TAB stall or raise
a dialog. Values are never read, and never shown.

Outside a registered project, or on a backend that will not open, completion
is silent rather than an error.

## Reading values back

`get` prints one value and nothing else, so it composes:

```bash
trenvy get DB_PASSWORD | pbcopy
PGPASSWORD=$(trenvy get DB_PASSWORD) psql
```

Only the named variable is resolved. Reading one value costs at most one
keyring read, and does not fail because some other secret has not been
stored yet under the active profile.

`show` lists the active profile's variables, or one output's subset when
named. Secrets are masked, and a masked table never opens the keyring at
all -- it is free, and it never prompts:

```
$ trenvy show
APP_MODE     development
DB_PASSWORD  ••••••••  (keyring)
LOG_LEVEL    info
```

`--reveal` prints the real values. `--format dotenv|json` writes what `render`
would write, to stdout instead of a file, and implies `--reveal` because a
masked dotenv file would be a file full of bullets.

Whether a secret is actually stored is `trenvy status`'s question, not
`show`'s: a masked row says "this is a secret", not "this is present".

## Profiles

A profile is a value set: `dev`, `prod`, `personal`. It decides what a
variable resolves to, including which keyring item a secret reads. An output
decides where values are written and in what shape, and carries no values at
all.

```bash
trenvy profile                        # which one am I on, and why
eval "$(trenvy profile use prod)"     # this shell, until it exits
trenvy profile default prod           # persisted in the manifest
trenvy -p prod render                 # one invocation
```

Precedence is `--profile`, then `TRENVY_PROFILE`, then `default_profile` in
the manifest.

`profile use` cannot change your shell by itself -- no child process can --
so it prints an export for the shell to evaluate. Its stdout carries the
export and nothing else, which is what makes the `eval` safe.

Secrets are namespaced per profile as `trenvy/{project}/{profile}/{name}`, so
`dev` and `prod` credentials never collide.

## Where things live

| path | holds |
|---|---|
| `~/.config/trenvy/config.toml` | the keyring service pattern |
| `~/.config/trenvy/registry.toml` | project id -> project root |
| `~/.config/trenvy/projects/<id>.toml` | variables, outputs and profiles |
| `~/.config/trenvy/state/<id>.toml` | what was rendered where |
| the OS keyring | secret values, under `trenvy/<project>/<profile>/<NAME>` |

Secret values are never in the manifest, never in a repo, and never in `argv`.

`TRENVY_CONFIG_DIR` overrides the config directory (used by tests); otherwise
trenvy follows `XDG_CONFIG_HOME`, falling back to `~/.config/trenvy`.

## Backends

macOS Keychain, Linux Secret Service / Windows Credential Manager (both
selected via the `system` backend), and an age-encrypted file for containers
and CI:

```bash
TRENVY_BACKEND=file TRENVY_PASSPHRASE=… trenvy render docker
```

Pass `--backend` on any command, or set `TRENVY_BACKEND`, to override the
platform default (`keychain`, `system`, `file`, or `mem` for tests).

The file backend has no inter-process locking: it re-encrypts and rewrites
the whole secrets file on every `Set`/`Delete`, so two concurrent
`trenvy secret set` runs against a shared file backend can race, with the
last writer winning.

## Guards

`render` refuses to write a path git would track, refuses to overwrite a file
trenvy did not write or one edited since, always writes `0600` atomically, and
`status` reports a rendered file as `fresh`, `STALE`, `MODIFIED`, or
`FOREIGN`. `clean` runs the same overwrite guard before removing a file.
`--allow-tracked` and `--force` opt out of the guards when you mean it (both
`render --force` and `clean --force`).

## Acceptance walkthrough

The project's acceptance criteria are written as a manual walkthrough against
your own repositories and your own OS keyring — see
[`docs/acceptance.md`](docs/acceptance.md). It is not part of CI: it writes
real secrets to your real keyring and files into your real working
repositories, so it is meant to be run by a person, deliberately, not as a
build step.
