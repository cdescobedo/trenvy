# trenvy

One manifest per project, secrets in the OS keyring, rendered into any worktree.

## Install

```bash
go install github.com/cdescobedo/trenvy/cmd/trenvy@latest
```

## Quick start

```bash
cd ~/workspace/demo/demo-backend   # the directory holding the worktrees
trenvy init --id demo-backend
trenvy import main/src/DemoApp.Api/.env_example --target docker
trenvy secret set ALPHA_TOKEN
trenvy status
```

Then, from any worktree:

```bash
trenvy render docker                  # writes src/DemoApp.Api/.env, 0600
trenvy exec local -- dotnet run       # injects into one process, writes nothing
trenvy shell local                    # a subshell with the variables loaded
trenvy clean docker                   # removes the rendered file
```

A worktree created later needs no setup — it is under the registered project
root, so it inherits the manifest.

## Commands

| command | does |
|---|---|
| `trenvy init` | register this project and create its manifest |
| `trenvy import <sample file>` | seed the manifest from a sample `.env` or `.json` file |
| `trenvy set <NAME> <VALUE>` | set a plain (non-secret) variable |
| `trenvy get <NAME> [target]` | print one variable's value, secret or plain |
| `trenvy show [target]` | list a target's variables, secrets masked unless `--reveal` |
| `trenvy secret set <NAME>` | store or rotate a secret in the keyring |
| `trenvy secret rm <NAME>` | delete a secret's value from the keyring, keeping the variable |
| `trenvy unset <NAME>` | remove a variable from the manifest and its secret from the keyring |
| `trenvy edit` | edit this project's variables interactively |
| `trenvy render [target]` | write a file target into this worktree |
| `trenvy clean [target]` | remove a rendered file from this worktree |
| `trenvy exec [target] -- <command>` | run a command with the target's variables in its environment |
| `trenvy shell [target]` | start a subshell with the target's variables loaded |
| `trenvy status` | show the resolved project, its secrets and its targets |
| `trenvy projects` | list registered projects and their roots |

Run `trenvy <command> --help` for a command's flags.

## Shell completion

```bash
trenvy completion zsh > ~/.local/share/zsh/completions/_trenvy   # or your fpath
trenvy completion bash > /usr/local/etc/bash_completion.d/trenvy
```

The generated script is a thin forwarder: it asks the binary for candidates on
every TAB, so it never goes stale and does not need regenerating when commands
change.

Variable names complete from this project's manifest, filtered to what the
command can actually act on:

| command | offers |
|---|---|
| `trenvy get <TAB>` | every variable |
| `trenvy set <TAB>` | plain variables (`secret set` handles the rest) |
| `trenvy secret set <TAB>` | secret variables |
| `trenvy secret rm <TAB>` | secret variables |
| `trenvy unset <TAB>` | every variable |

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
keyring read, and does not fail because some other secret in the target has
not been stored yet.

`show` lists a whole target. Secrets are masked, and a masked table never
opens the keyring at all -- it is free, and it never prompts:

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

## Where things live

| path | holds |
|---|---|
| `~/.config/trenvy/config.toml` | the keyring service pattern |
| `~/.config/trenvy/registry.toml` | project id -> project root |
| `~/.config/trenvy/projects/<id>.toml` | variables and targets |
| `~/.config/trenvy/state/<id>.toml` | what was rendered where |
| the OS keyring | secret values, under `trenvy/<project>/<NAME>` |

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

## Known v1 limitation: per-target overrides

A variable's per-target override (`vars.<NAME>.targets` in the manifest) is
authorable only by hand-editing the manifest in this version. There is no TUI
drill-in for it and no `--target` flag on `trenvy set` — editing an override
means opening the manifest file directly.

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
