# trenvy

**One manifest per project. Values scoped by profile, secrets in the OS
keyring, delivered to any worktree as a file or straight into a process.**

Project environment variables have a way of going wrong in the same two ways.
A `.env` file gets copied into a second worktree and the two quietly drift, so
the bug you are chasing on a feature branch is a stale variable and not your
code. And the credentials those files hold end up somewhere durable and
unencrypted — a dotfile, a shell profile, a note — where they outlive the job
they were for and get committed by accident.

trenvy keeps one manifest per project, outside the repository. Names, formats
and plain values live there; secret **values** live in your OS keyring and
nowhere else. Any worktree, including one created months later, renders the
same file with no setup — or skips the file entirely and gets the values
injected into a single process.

```console
$ trenvy status
project   myapp
profile   dev  (default)
manifest  /Users/you/.config/trenvy/projects/myapp.toml
worktree  /Users/you/code/myapp
backend   keychain (darwin)

vars      3 total, 1 secret
  present  API_TOKEN

outputs
  local  .env  fresh
```

---

## Contents

- [Install](#install)
- [Quickstart](#quickstart)
- [Concepts](#concepts) — **start here**
- [Command reference](#command-reference)
- [The management interface](#the-management-interface)
- [Configuration](#configuration)
- [Security model](#security-model)
- [Shell completion](#shell-completion)
- [Troubleshooting](#troubleshooting)
- [Contributing](#contributing)
- [Licence](#licence)

---

## Install

**trenvy is not published yet.** There is no module to `go install` from, no
tagged releases, no Homebrew formula, no binaries and no checksums. Build it
from your copy of the source:

```bash
cd /path/to/trenvy
go build -o ~/.local/bin/trenvy ./cmd/trenvy
```

Requires Go 1.27 or newer. trenvy is cgo-free on every platform, so it
cross-compiles anywhere Go does. There is no `trenvy --version`; the commit
you built is the version.

Once it is published this becomes
`go install github.com/cdescobedo/trenvy/cmd/trenvy@latest`, and the warning
about `@latest` tracking the default branch will apply.

## Quickstart

Five minutes, from a repository that has a `.env.example` to a running process.
This sequence is executed end to end against a real binary before release; it
works as written.

```bash
cd ~/code/myapp          # any git repository

# 0. trenvy will refuse to write a file git would track. Say so up front.
echo '.env' >> .gitignore

# 1. Register the project. The id defaults to the directory name.
trenvy project init

# 2. Seed the manifest from your sample file, and define where it renders.
trenvy import .env.example --output local --no-tui

# 3. Store the secret. It is prompted for, never typed on the command line.
trenvy secret set API_TOKEN

# 4. See where you stand.
trenvy status
```

```console
$ trenvy status
project   myapp
profile   dev  (default)
manifest  /Users/you/.config/trenvy/projects/myapp.toml
worktree  /Users/you/code/myapp
backend   keychain (darwin)

vars      3 total, 1 secret
  present  API_TOKEN

outputs
  local  .env  not rendered
```

Now deliver the values, whichever way suits:

```bash
trenvy render local                  # writes .env, mode 0600
trenvy exec -- npm start             # injects into one process, writes nothing
trenvy shell                         # a subshell with the values loaded
trenvy clean local                   # removes the rendered file
```

```console
$ trenvy render local
wrote .env  (3 vars, 1 from the keyring, 0600)
```

Two things worth noticing. `import` normally opens an interactive table so you
can review which variables are secret before anything is saved — `--no-tui`
above skips it only so the block is pasteable; drop the flag and you get the
review, or run `trenvy edit` later. And `--output local` names the output
import creates, pointing at the sample file's real path; drop the flag and
import still creates one, named from the sample's directory (`root` for a
sample at the worktree root) — only `--no-output` gets you variables and
nowhere to put them.

From here, a second worktree needs **no setup at all**:

```bash
git worktree add ../myapp-feature && cd ../myapp-feature
trenvy render local                  # same manifest, same values
```

## Concepts

Almost every mistake people make with trenvy comes from collapsing two ideas
that it deliberately keeps apart. Read this section before the flag lists.

### A profile decides *what the values are*

A **profile** is a named value set: `dev`, `staging`, `prod`, `personal`. It
decides what each variable resolves to — including **which keyring item a
secret reads**.

```console
$ trenvy get APP_MODE
development
$ trenvy -p prod get APP_MODE
production
```

That second point is the one that surprises people. Secrets are namespaced per
profile, as `trenvy/{project}/{profile}/{name}`, so a variable's `dev` and
`prod` credentials are genuinely different keyring items and cannot collide:

```console
$ trenvy status | grep API_TOKEN
  present  API_TOKEN
$ trenvy -p prod status | grep API_TOKEN
  MISSING  API_TOKEN  -> trenvy -p prod secret set API_TOKEN
```

Same variable, same project, same command — a different profile, so a
different item, which happens not to be filled in yet.

Four sources can name the active profile. The first one set wins:

| source | for |
|---|---|
| `--profile`/`-p` flag | one command |
| `TRENVY_PROFILE` environment variable | one shell, and everything it runs |
| active selection (`trenvy profile use <name>`) | this machine, until changed again |
| `default_profile` in the manifest | the project, for anyone with no override of their own |

Because a stale exported `TRENVY_PROFILE` is invisible until something names
it, `trenvy profile` tells you both which one is active and why:

```console
$ trenvy profile
dev  (default)
```

```bash
trenvy -p prod render local           # one invocation
trenvy profile use prod               # this machine, until changed again
trenvy profile default prod           # persisted in the manifest
```

`profile use` persists the selection to the project's state file rather than
printing anything to run — it takes effect for every later command against
this project from this machine, not just the shell that ran it. `trenvy
profile use --clear` forgets the selection. `TRENVY_PROFILE` still outranks
it if you have that set, and `profile use` says so when it does.

### An output decides *where they go*

An **output** is a named destination: a format, a path, and optionally a subset
of variables.

```toml
[outputs.local]
format = 'dotenv'          # or 'json'
path = '.env'              # relative to the worktree root
include = ['*']            # optional globs; exclude works too
```

An output holds **no values whatsoever**. It cannot; there is no field for one.
It decides which variables are written and in what syntax, never what they are
worth.

### They are independent axes

That is the whole design:

|  | decided by the **profile** | decided by the **output** |
|---|---|---|
| what `DATABASE_URL` equals | ✅ | ❌ |
| which keyring item a secret reads | ✅ | ❌ |
| dotenv or JSON | ❌ | ✅ |
| which file, and where | ❌ | ✅ |
| which variables are included | ❌ | ✅ |

So they compose freely. The same profile can feed several outputs; the same
output renders differently under each profile:

```bash
trenvy -p prod render docker     # prod values, docker's format/path/subset
trenvy -p prod render web        # prod values, web's format/path/subset
trenvy -p dev  render docker     # dev values, same file shape
```

Precedence for a single variable, in order: the **per-profile value**, then the
**shared default** (`set --shared`), then the **keyring** if it is a secret.
If nothing matches: omitted when the variable is optional, an error otherwise.

### `exec` and `shell` take no output — on purpose

```bash
trenvy exec -- npm start
trenvy shell
```

Neither accepts an output argument, and that is not an oversight. An output's
include list describes **a file's audience** — which variables belong in that
particular file. A process wants its whole environment. So process delivery is
its own verb rather than a configured destination, and these two commands
resolve every variable the profile defines.

Choose the values with `--profile`. There is nothing else to choose.

```bash
trenvy -p prod exec -- ./deploy.sh
```

`exec` also exports `TRENVY_PROFILE` into the child, so if `deploy.sh` itself
calls trenvy it stays on the same profile instead of quietly falling back to
the default and reading a different keyring namespace.

### Projects and worktrees

A **project root** is the directory holding your worktrees; it is what gets
registered, and it maps to one manifest. A **worktree root** is the particular
checkout you are standing in, and it is what an output's relative path resolves
against.

Keeping them separate is what lets one manifest serve every worktree —
including ones created long after `project init` — with no per-worktree setup.
Lookup walks up from your working directory and the deepest registered ancestor
wins, so a project nested inside a monorepo shadows the outer one.

## Command reference

Every description below is the command's own `Short` text. Run
`trenvy <command> --help` for the full text and flags.

**Global flags**, accepted by every command:

| flag | meaning |
|---|---|
| `-p, --profile <name>` | profile to use: overrides `TRENVY_PROFILE`, the active selection and the manifest default |
| `--backend <name>` | secret backend: `keychain`, `system`, `file`, `mem` |

### Projects

| command | does |
|---|---|
| `trenvy project init` | Register this project and create its manifest |
| `trenvy project list` | List registered projects and their roots |
| `trenvy project move <id> <new-root>` | Repoint a project at a new root after its directory moved |
| `trenvy project rm <id>` | Delete a project: its manifest, state, registration and its secrets in the current backend |

`project init` takes `--id` (default: the root directory's name) and `--root`
(default: the current directory). `project rm` takes `--yes`.

### Profiles

| command | does |
|---|---|
| `trenvy profile` | Show the active profile; manage the project's profiles |
| `trenvy profile list` | List the project's profiles |
| `trenvy profile add <name>` | Declare a new profile |
| `trenvy profile rm <name>` | Remove a profile, its overrides and its secrets |
| `trenvy profile use <name>` | Set this machine's active profile for the project |
| `trenvy profile default <name>` | Set the project's default profile |

`profile rm` takes `--yes`. `profile use --clear` forgets this machine's
active selection instead of setting one.

### Outputs

| command | does |
|---|---|
| `trenvy output list` | List the project's outputs and which is the default |
| `trenvy output add <name> --format <f> --path <p>` | Define a new output |
| `trenvy output rename <old> <new>` | Rename it, keeping its render records |
| `trenvy output default <name>` | Set the output bare `render` uses |
| `trenvy output rm <name>` | Remove it |

`output add` also takes `--include` and `--exclude` globs. `output rm` takes
`--yes` and `--force`.

### Values

| command | does |
|---|---|
| `trenvy import <sample file>` | Seed the manifest from a sample `.env` or `.json` file |
| `trenvy set <NAME> <VALUE>` | Set a plain value for the active profile |
| `trenvy get <NAME>` | Print one variable's value |
| `trenvy show [output]` | List the profile's variables, or one output's subset, secrets masked |
| `trenvy secret set <NAME>` | Store or rotate a secret in the keyring |
| `trenvy secret rm <NAME>` | Delete a secret's value from the keyring, keeping the variable |
| `trenvy unset <NAME>` | Remove a variable from the manifest and its secret from every profile's keyring |
| `trenvy edit` | Edit this project's variables interactively |
| `trenvy ui` | Manage this project's variables across every profile, interactively |

Flags: `import` takes `--output <name>`, `--no-output`, `--no-tui` and
`--force`; `set` takes `--shared`; `show` takes `--reveal` and `--format
dotenv|json`; `secret set` takes `--stdin`; `secret rm` and `unset` take
`--yes`.

### Delivery

| command | does |
|---|---|
| `trenvy render [output]` | Write an output's file into this worktree |
| `trenvy clean [output]` | Remove a rendered file from this worktree |
| `trenvy exec -- <command>` | Run a command with the profile's variables in its environment |
| `trenvy shell` | Start a subshell with the profile's variables loaded |
| `trenvy status` | Show the resolved project, its secrets and its outputs |

Flags: `render` takes `--allow-tracked` and `--force`; `clean` takes `--force`;
`status` takes `--quick`.

Omitting `[output]` uses `project.output` from the manifest. `project init`
creates no outputs and so sets none; the first output any `import` creates —
named or not, unless run with `--no-output` — claims the default, unless one
is already set. `trenvy output default <name>` changes it afterwards. Name
the output explicitly, or set `output = '<name>'` under `[project]` yourself,
if neither of those gives you the default you want.

### Reading values back

`get` prints one value and nothing else, so it composes:

```bash
trenvy get DB_PASSWORD | pbcopy
PGPASSWORD=$(trenvy get DB_PASSWORD) psql
```

Only the named variable is resolved, so reading one value costs at most one
keyring read and does not fail because some *other* secret has not been stored
yet under the active profile.

`show` lists the active profile's variables, or one output's subset when named.
Secrets are masked, and a masked table never opens the keyring at all — it is
free, and it never prompts:

```console
$ trenvy show
API_TOKEN     ••••••••  (keyring)
APP_MODE      development
DATABASE_URL  postgres://app@db.example.test:5432/app
```

`--reveal` prints the real values. `--format dotenv|json` writes what `render`
would write, to stdout instead of a file, and implies `--reveal` because a
masked dotenv file would be a file full of bullets.

Whether a secret is actually *stored* is `trenvy status`'s question, not
`show`'s: a masked row says "this is a secret", not "this is present".

## The management interface

`trenvy edit` shows one profile at a time. `trenvy ui` shows all of them at
once, as a matrix — rows are variables, columns are profiles:

```console
$ trenvy ui
1:Overview  >2:Variables   3:Profiles   4:Outputs

   NAME                  dev       prod
>  API_TOKEN     secret  [stored]  MISSING
   APP_MODE              override  shared
   DATABASE_URL          shared    shared

no staged changes
```

A cell reads `shared` (the variable's default value), `override` (a value set
just for that profile), `stored` (a secret present in the keyring), `MISSING`
or `optional`. The `secret` column marks which rows are secrets. Presence is
loaded with `Has`, never `Get`, so opening the view never raises a keyring
authorization prompt — a cell's real value is only fetched when you reveal it.

`s` on a plain variable's row promotes it to a secret, one way: there is no
key that turns a secret back into a plain variable. On `w`, the value —
the shared default, or each profile's own override — moves into the keyring,
one item per profile that had a value, and the manifest is left with
`secret = true` and no plain value on it. A variable carrying a shared value
confirms first, naming that it becomes one keyring item per declared profile
that can drift apart once any one of them is edited later; declining stages
nothing. A variable with only per-profile overrides and no shared value
stages immediately, with no such prompt. `s` on a row that is already a
secret says why it did nothing.

The four tabs: **Overview** repeats `status`'s own project, profile, manifest,
worktree and backend facts, lists every detected problem with the exact
command that fixes it (or says the project is healthy), and flags a variable
that reaches no output or reaches several — a plain fact, not an error, since
`exec` and `shell` read a whole profile with no output at all and nothing
stops two outputs from carrying the same variable on purpose. Enter on a
problem jumps to the cell, or the tab, that explains it.

**Variables** is the matrix above. **Outputs** lists every declared
output — name, format, path, what it selects, and its render state — and
opens a membership editor on `enter`:

```console
$ trenvy ui
 1:Overview   2:Variables   3:Profiles  >4:Outputs

   NAME     FORMAT  PATH      SELECTS   STATE
>  local *  dotenv  .env      all       absent
   web      json    web.json  PUBLIC_*  absent

D does nothing here: local is already the default -- there is no key that clears the default without setting a new one

no staged changes
a add · r rename · D default · d delete · enter edit membership · ? help
```

`*` marks the output `render` and `show` use with no argument, the same mark
`trenvy output list` uses. `a` prompts for a name, then a format, then a
path, staging all three as one addition; `r` renames the row in place; `D`
(capital — `d` already means delete everywhere else in this UI) stages
making the row the default; `d` marks the row deleted. Each of these shows in
the table the instant it is staged, before `w` is ever pressed. A `fresh`
STATE can still miss one thing: a secret's value changing since the last
render while the file itself went untouched — `trenvy status` is the command
that reads secrets and can tell that apart from a real match.

`enter` opens the membership editor: one checkbox per variable, for exactly
which of them this output writes — a hand edit of the manifest's
`include`/`exclude` globs until now.

```console
Editing membership for local

> [x] API_TOKEN
  [x] APP_MODE
  [x] DATABASE_URL
  [x] PUBLIC_URL

everything except: a variable added later WILL appear in this file

space toggle · m switch mode · enter save · esc cancel
```

`space` toggles the variable under the cursor. `m` switches which of two
futures a variable that does not exist yet falls into — *everything except*,
shown above, where it lands in the file automatically, or *only these*,
where it does not — and the sentence on screen changes with it; switching
modes never moves any of today's checked boxes, only what a future addition
does. `enter` saves the result as a staged glob change; `esc` abandons the
whole edit. A row with a staged delete refuses to open the editor, and the
reverse holds too: marking a row deleted while its membership edit is open is
also refused, since editing and removing the same output in one batch has no
single right answer.

**Profiles** remains a placeholder in this release — it names `trenvy
profile` as the command that does that job today.

Keys: `1`-`4` or `tab`/`shift+tab` switch tabs; `j`/`k`/`h`/`l` and the arrow
keys move the cursor (clamped at the edges, not wrapping); `e` edits the
selected cell, masked while typing if it is a secret; `r` reveals a cell's
real value on the Variables tab, or renames the selected row on the Outputs
tab; `a` adds a variable or an output; `d` marks the selected variable or
output deleted; `D` makes the selected output the default; `s` promotes the
selected variable to a secret; `enter` jumps to a problem's cell on the
Overview tab, or opens the membership editor on the Outputs tab; `/` filters
rows by substring; `p` opens a switcher over every registered project and
reloads the session against your choice, asking first if you have staged
changes; `w` commits everything staged; `q` quits, asking first if anything
is staged; `ctrl+c` quits immediately from anywhere, without asking, and
without writing anything; `?` shows the full key list. A short reminder of
the relevant keys stays on screen on every tab, so none of this has to be
memorized up front.

Nothing reaches disk until `w`. It commits in phases: a new variable or
output is added first, so a row this same session also renames, defaults or
removes has something to act on; keyring writes follow, then keyring
deletes — a deleted variable's secret is removed under every profile, not
just the active one; the render-state file comes next, so a renamed output's
already-rendered file keeps reading as fresh, and a removed output's render
records are forgotten in every worktree that ever rendered it, naming any
file the removal leaves on disk; every remaining edit and removal follows in
its own pass; and only then is the whole manifest validated and saved. A
commit that fails partway reports exactly how far it got — secrets stored,
secrets deleted, render records repointed or forgotten — rather than one
number that cannot say which kind stalled, and there is no rollback: a
deleted keyring item cannot be undeleted. Quitting any other way — `q`
confirmed, or `ctrl+c` — writes nothing at all.

## Configuration

### Where things live

| path | holds |
|---|---|
| `~/.config/trenvy/config.toml` | the keyring service-name pattern |
| `~/.config/trenvy/registry.toml` | project id → project root |
| `~/.config/trenvy/projects/<id>.toml` | the manifest: variables, outputs, profiles |
| `~/.config/trenvy/state/<id>.toml` | what was rendered where |
| `~/.config/trenvy/secrets.age` | the file backend's encrypted document, when used |
| the OS keyring | secret values, as `trenvy/<project>/<profile>/<NAME>` |

Directories are created `0700`; every file is written atomically at `0600`.

### Environment variables

| variable | effect |
|---|---|
| `TRENVY_CONFIG_DIR` | overrides the config directory entirely; ahead of `XDG_CONFIG_HOME` |
| `TRENVY_PROFILE` | names the profile, unless `--profile` is passed; outranks the active selection and the manifest default |
| `TRENVY_BACKEND` | the secret backend, unless `--backend` is passed |
| `TRENVY_PASSPHRASE` | unlocks the `file` backend (**stripped from `exec`'s child environment**) |
| `TRENVY_PROJECT` | *set by* `trenvy shell` in the subshell, so your prompt can mark it |

Config directory resolution: `TRENVY_CONFIG_DIR`, then `$XDG_CONFIG_HOME/trenvy`,
then `~/.config/trenvy`.

### Keyring service names

Secrets are addressed by a service name rendered from a pattern. The default:

```
trenvy/{project}/{profile}/{name}
```

Override it in `config.toml` to adopt an existing layout:

```toml
[keyring]
service = "myorg/{project}/{profile}/{name}"
```

**A pattern without `{profile}` is rejected when the config loads.** Every
profile would otherwise share one keyring item, so `secret set` under `dev` and
under `prod` would overwrite each other and a profile switch would silently
read the wrong credential.

### Backends

| backend | what it is |
|---|---|
| `keychain` | macOS login Keychain, via `security(1)`. The default on darwin. |
| `system` | Secret Service (Linux) / Credential Manager (Windows). The default there. |
| `file` | an age-encrypted document under a passphrase, for containers, CI, WSL, or an SSH session with no D-Bus. |
| `mem` | in-memory, for tests. Nothing survives the process. |

Selection order: `--backend`, then `TRENVY_BACKEND`, then the platform default.

```bash
TRENVY_BACKEND=file TRENVY_PASSPHRASE=… trenvy render docker
```

Two limits worth knowing. Values are capped at **2560 bytes** on every backend,
not just Windows, so a value that works on your machine does not mysteriously
fail on a teammate's. And the file backend has **no inter-process locking** —
it re-encrypts and rewrites the whole document on every `Set`/`Delete`, so two
concurrent writers can race, last writer winning.

## Security model

trenvy holds credentials, so here is what it actually does — and, just as
plainly, what it does not. [SECURITY.md](SECURITY.md) has the full threat model
and how to report a vulnerability.

### What it protects

- **Secret values are never in the manifest.** The manifest holds names,
  secret-ness and plain values. The manifest, registry and state file are all
  safe to read; none contains a secret.
- **Secrets never reach `argv`.** `secret set` prompts without echoing, or
  reads stdin with `--stdin`. `trenvy set` **refuses** a variable marked secret
  rather than accepting a value on the command line — `argv` is visible to any
  process via `ps`, and lands in shell history. Success reports the length,
  never the value: `stored API_TOKEN (16 bytes)`.
- **Listing secrets does not prompt on macOS.** Presence checks use a separate
  `Has` operation, not a read, so `trenvy status` and the interactive table do
  not raise one authorization dialog per secret. A masked `trenvy show` does
  not open the keyring at all.
- **Rendered files are `0600`, written atomically** via temp-file-and-rename,
  so a secret never exists briefly with default permissions or half-written.
- **`render` refuses a path git would track**, naming the `.gitignore` line to
  add. `--allow-tracked` overrides it.
- **`render` and `clean` refuse to clobber what trenvy did not write** — a
  `FOREIGN` or `MODIFIED` file needs `--force`.
- **`exec` strips `TRENVY_PASSPHRASE`** from the child environment. It unlocks
  the file backend, and a child has no business inheriting it.
- **Staleness digests are keyed**, by a per-project random salt, so the state
  file cannot be used to brute-force a low-entropy secret.
- **The file backend is real encryption**: age with an scrypt passphrase
  recipient.

### What it does not protect against

- **Anything that can read your keyring can read these secrets.** This is the
  fundamental limit. trenvy adds no second factor and no access control of its
  own. Any program running as you can read every secret it stored, without
  going through trenvy.
- **A rendered `.env` is plaintext on disk.** `0600` means your user and root;
  it does not mean encrypted. Prefer `exec`/`shell` when you do not need a
  file.
- **Dotenv output is deliberately not safe to `source`.** `$` is left
  unescaped, because the file targets dotenv readers (docker compose
  `env_file:`, DotNetEnv) that treat contents literally. In bash, `source .env`
  performs parameter expansion inside the double quotes — a value of
  `pa$$word-literal` becomes the shell's PID spliced into the string. **Use
  `trenvy exec` or `trenvy shell` instead of sourcing rendered files.**
- **`exec`/`shell` put secrets in a process environment**, inherited by every
  descendant and readable via `/proc` on Linux by the same user.
- **Presence checks are only prompt-free on macOS.** Elsewhere the system
  backend answers `Has` with a read, which can raise an unlock prompt.
- **`project rm` only reaches the currently selected backend.** A secret under
  a different backend survives — but the manifest it deletes was that secret's
  only inventory, so afterwards nothing records that it exists, or where.
- **The git guard fails open** outside a repository, or if `git` is missing.

## Shell completion

```bash
trenvy completion zsh > ~/.local/share/zsh/completions/_trenvy   # or your fpath
trenvy completion bash > /usr/local/etc/bash_completion.d/trenvy
```

`fish` and `powershell` are also supported. The generated script is a thin
forwarder: it asks the binary for candidates on every TAB, so it never goes
stale and does not need regenerating when commands change.

Names complete from *this project's* manifest, filtered to what the command can
actually act on:

| command | offers |
|---|---|
| `trenvy get <TAB>` | every variable |
| `trenvy set <TAB>` | plain variables only |
| `trenvy secret set\|rm <TAB>` | secret variables, tagged `stored` or `MISSING` where that is free |
| `trenvy unset <TAB>` | every variable |
| `trenvy render\|clean\|show <TAB>` | defined outputs, tagged with their path |
| `trenvy -p <TAB>` | declared profiles |
| `trenvy profile use\|default\|rm <TAB>` | declared profiles |
| `trenvy project move\|rm <TAB>` | registered project ids |
| `trenvy project move <id> <TAB>` | left to the shell (directories) |

The `stored`/`MISSING` tag appears only where the backend can answer presence
cheaply and without a prompt — on macOS it can. The file and system backends
answer only by decrypting or reading, so the tag is dropped there rather than
making TAB stall or raise a dialog. Values are never read, and never shown.

Outside a registered project, or on a backend that will not open, completion is
silent rather than an error.

## Troubleshooting

**`X is not inside a registered trenvy project`**
You are outside any registered project root. Run `trenvy project init`, or
`trenvy project list` to see what is registered and where.

**`.env is not ignored by git`**
The guard working as intended. Add the named line to `.gitignore`, or pass
`--allow-tracked` if you genuinely mean to track the file.

**`.env was modified after trenvy wrote it`**
Someone hand-edited the rendered file. Inspect it, then either fold the change
back into the manifest with `trenvy edit`, or discard it with
`trenvy render <output> --force`.

**`.env exists and trenvy has no record of writing it`** (`FOREIGN`)
A pre-existing file at the output's path. Seed the manifest from it with
`trenvy import .env`, or overwrite it with `--force`.

**`unknown profile "nope" (from TRENVY_PROFILE)`**
A stale export. Every command validates the variable before running, including
the one that would fix it, so the error names the escape hatch:
`unset TRENVY_PROFILE`.

**`unknown profile "nope" (from active)`**
This machine's active selection names a profile the manifest no longer
declares — usually because it was removed by an older build, or the state
file was hand-edited. Every command validates the active selection before
running, so the error names the escape hatch: `trenvy profile use --clear`.

**`no output given and project.output is not set`**
The first output any `import` creates claims the default, so this usually
means either no output was ever created that way — every `import` ran with
`--no-output`, or the manifest was built up with `trenvy edit` — or the output
that held the default was removed with `trenvy output rm`. Name the output —
`trenvy render local` — or run `trenvy output default local` to make it the
default again.

**`no passphrase for the file backend`**
Set `TRENVY_PASSPHRASE`, or use a platform keyring.

**`this manifest predates profiles`**
A pre-profile manifest, from when `[targets]` conflated values and
destinations. It is rejected rather than migrated: recreate it with
`trenvy project init`, then `trenvy import`, then re-enter the secrets.

**A framework is reading stale values.** A rendered `.env` silently wins over
an injected environment in frameworks that load dotenv files over the process
environment — the most confusing failure this tool can produce. `exec` and
`shell` warn about stale rendered files in the worktree for exactly this
reason; `trenvy status` shows the same thing, and `trenvy clean <output>`
removes the file.

**Your keyring stopped having your secrets after upgrading.** If you used this
tool when it was called `envctl`, the keyring namespace changed with the name.
See [CHANGELOG.md](CHANGELOG.md).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for how to build, test and what a good
change looks like here, and [`docs/architecture.md`](docs/architecture.md) for
the data model and why it is shaped the way it is.

Security issues: **do not open a public issue.** See [SECURITY.md](SECURITY.md).

## Licence

[Apache License 2.0](LICENSE).
