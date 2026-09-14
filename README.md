# envctl

One manifest per project, secrets in the OS keyring, rendered into any worktree.

## Install

```bash
go install github.com/cdescobedo/envctl/cmd/envctl@latest
```

## Quick start

```bash
cd ~/workspace/demo/demo-backend   # the directory holding the worktrees
envctl init --id demo-backend
envctl import main/src/DemoApp.Api/.env_example --target docker
envctl secret set ALPHA_TOKEN
envctl status
```

Then, from any worktree:

```bash
envctl render docker                  # writes src/DemoApp.Api/.env, 0600
envctl exec local -- dotnet run       # injects into one process, writes nothing
envctl shell local                    # a subshell with the variables loaded
envctl clean docker                   # removes the rendered file
```

A worktree created later needs no setup — it is under the registered project
root, so it inherits the manifest.

## Commands

| command | does |
|---|---|
| `envctl init` | register this project and create its manifest |
| `envctl import <sample file>` | seed the manifest from a sample `.env` or `.json` file |
| `envctl set <NAME> <VALUE>` | set a plain (non-secret) variable |
| `envctl secret set <NAME>` | store or rotate a secret in the keyring |
| `envctl secret rm <NAME>` | delete a secret's value from the keyring, keeping the variable |
| `envctl unset <NAME>` | remove a variable from the manifest and its secret from the keyring |
| `envctl edit` | edit this project's variables interactively |
| `envctl render [target]` | write a file target into this worktree |
| `envctl clean [target]` | remove a rendered file from this worktree |
| `envctl exec [target] -- <command>` | run a command with the target's variables in its environment |
| `envctl shell [target]` | start a subshell with the target's variables loaded |
| `envctl status` | show the resolved project, its secrets and its targets |
| `envctl projects` | list registered projects and their roots |

Run `envctl <command> --help` for a command's flags.

## Shell completion

```bash
envctl completion zsh > ~/.local/share/zsh/completions/_envctl   # or your fpath
envctl completion bash > /usr/local/etc/bash_completion.d/envctl
```

The generated script is a thin forwarder: it asks the binary for candidates on
every TAB, so it never goes stale and does not need regenerating when commands
change.

Variable names complete from this project's manifest, filtered to what the
command can actually act on:

| command | offers |
|---|---|
| `envctl set <TAB>` | plain variables (`secret set` handles the rest) |
| `envctl secret set <TAB>` | secret variables |
| `envctl secret rm <TAB>` | secret variables |
| `envctl unset <TAB>` | every variable |

Secrets are tagged `stored` or `MISSING` where the backend can answer that
cheaply, which on macOS it can -- the Keychain reports presence without an
authorization prompt. The file and system backends answer only by decrypting
or reading, so there the tag is dropped rather than making TAB stall or raise
a dialog. Values are never read, and never shown.

Outside a registered project, or on a backend that will not open, completion
is silent rather than an error.

## Where things live

| path | holds |
|---|---|
| `~/.config/envctl/config.toml` | the keyring service pattern |
| `~/.config/envctl/registry.toml` | project id -> project root |
| `~/.config/envctl/projects/<id>.toml` | variables and targets |
| `~/.config/envctl/state/<id>.toml` | what was rendered where |
| the OS keyring | secret values, under `envctl/<project>/<NAME>` |

Secret values are never in the manifest, never in a repo, and never in `argv`.

`ENVCTL_CONFIG_DIR` overrides the config directory (used by tests); otherwise
envctl follows `XDG_CONFIG_HOME`, falling back to `~/.config/envctl`.

## Backends

macOS Keychain, Linux Secret Service / Windows Credential Manager (both
selected via the `system` backend), and an age-encrypted file for containers
and CI:

```bash
ENVCTL_BACKEND=file ENVCTL_PASSPHRASE=… envctl render docker
```

Pass `--backend` on any command, or set `ENVCTL_BACKEND`, to override the
platform default (`keychain`, `system`, `file`, or `mem` for tests).

The file backend has no inter-process locking: it re-encrypts and rewrites
the whole secrets file on every `Set`/`Delete`, so two concurrent
`envctl secret set` runs against a shared file backend can race, with the
last writer winning.

## Known v1 limitation: per-target overrides

A variable's per-target override (`vars.<NAME>.targets` in the manifest) is
authorable only by hand-editing the manifest in this version. There is no TUI
drill-in for it and no `--target` flag on `envctl set` — editing an override
means opening the manifest file directly.

## Guards

`render` refuses to write a path git would track, refuses to overwrite a file
envctl did not write or one edited since, always writes `0600` atomically, and
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
