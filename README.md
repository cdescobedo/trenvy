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
- [Setting up a project](#setting-up-a-project)
- [Concepts](#concepts) — **start here**
- [Command reference](#command-reference)
- [The management interface](#the-management-interface)
- [Configuration](#configuration)
- [Security model](#security-model)
- [Shell completion](#shell-completion)
- [Troubleshooting](#troubleshooting)
- [Contributing](#contributing)
- [Further reading](#further-reading)
- [Licence](#licence)

---

## Install

**trenvy is not published yet.** There is no published module to
`go install` from by path, no tagged releases, no Homebrew formula, no
binaries and no checksums. Install it from a checkout of the source:

```bash
cd /path/to/trenvy
go install ./cmd/trenvy        # installs into $(go env GOPATH)/bin
```

Make sure `$(go env GOPATH)/bin` is on your `PATH`.

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

Two things worth noticing. `import` normally opens a review of the sample's
variables, so you can decide which are secret before anything is saved —
`--no-tui` above skips it only so the block is pasteable; drop the flag and you
get the review (see [Reviewing an import](#reviewing-an-import)). And
`--output local` names the output import creates, pointing at the sample
file's real path; drop the flag and import still creates one, named from the
sample's directory (`root` for a sample at the worktree root) — only
`--no-output` gets you variables and nowhere to put them.

From here, a second worktree needs **no setup at all**:

```bash
git worktree add ../myapp-feature && cd ../myapp-feature
trenvy render local                  # same manifest, same values
```

## Setting up a project

The quickstart above is the non-interactive path, and it stays: `trenvy project
init` followed by `trenvy import <file> --no-tui` does the same work with no
interface and is what scripts should use. If you would rather be walked
through it, run `trenvy ui` (or `trenvy edit`) **outside** a registered
project. Instead of failing, it opens setup, in four steps shown across the
top with the current one in brackets:

1. **Project** — the project id and its root.
2. **Seed** — a sample to start from, or *Start empty*.
3. **Review** — the sample's variables, one row each.
4. **Create** — a summary of what will be written.

Nothing is written until `enter` on the summary. `esc` goes back a step, and
at the first step it cancels; `ctrl+c` cancels from anywhere. Cancelling
prints `setup cancelled -- nothing was written` and leaves no manifest and no
registry entry. Every footer lists the keys that work at that moment, `enter`
first and `esc` last; on a narrow terminal whole hints are dropped, `enter` and
`esc` last of all, rather than one being cut.

```console
$ cd /tmp/t6/proj/myapp && trenvy ui
 Set up a project
 [ Project ]  Seed   Review   Create
╭──────────────────────────────────────────────────────────────────────────────────────────────────╮
│> project id  [myapp]                                                                             │
│                                                                                                  │
│  root        [/tmp/t6/proj/myapp]                                                                │
│                                                                                                  │
│                                                                                                  │
│                                                                                                  │
╰──────────────────────────────────────────────────────────────────────────────────────────────────╯

 enter next   tab switch field   esc cancel
```

**The root.** The root is the directory trenvy registers: every worktree under
it finds the project. It is prefilled from where you ran the command, and you
can edit both fields (`tab` switches between them). In a bare-repo layout —
`<root>/.bare` beside one directory per worktree — the root is prefilled with
`<root>`, the directory that holds the worktrees, and the id with that
directory's name, so a sibling worktree finds the project too. Anywhere else
the root is the current directory, as `project init` defaults. `enter` is
refused, with the reason shown under the field, when:

- the id is not a valid project id, or another project already uses it;
- the root is not an absolute path;
- another project is registered at that root;
- the directory you are in is not inside the root (the new project could not
  be found from here).

**The seed.** Setup lists the sample files in the worktree root, or in the
current directory outside git: a name that ends in `.example`, `_example`,
`.sample` or `.dist` and, without that ending, is `.env`, starts with `.env.`,
ends with `.env` or contains `.json` — the same names `import` can turn into an
output path. A sample that fails to parse or holds no variables is left out of
the list, and *Start empty* is always there. A sample that names a variable
twice is read as dotenv reads it: the last one wins. A bare layout's root is
not a worktree, so setup run there looks only in the root itself and, finding
nothing, says to run trenvy from a worktree to seed from its samples.

```console
 Set up a project
  Project  [ Seed ]  Review   Create
╭──────────────────────────────────────────────────────────────────────────────────────────────────╮
│> .env.example  3 variables                                                                       │
│  Start empty   add variables yourself afterwards                                                 │
│                                                                                                  │
│                                                                                                  │
│                                                                                                  │
│                                                                                                  │
╰──────────────────────────────────────────────────────────────────────────────────────────────────╯

 enter choose   j/k move   esc back
```

**The review.** One row per variable, `[x]` for a secret. A value that looks
like a secret is marked for you and its value is never shown and never stored;
you store secrets after setup. Unticking such a row makes it plain with an
empty value, for `e` to fill. Move with `j`/`k`, and:

| key | does |
|---|---|
| `space` | mark the row a secret, or plain again |
| `e` | edit a plain row's value (not for a secret, nor a row left out) |
| `d` | leave the row out of the import, or bring it back |
| `enter` | continue to the summary |
| `esc` | back a step |

```console
 Set up a project
  Project   Seed  [ Review ]  Create
╭──────────────────────────────────────────────────────────────────────────────────────────────────╮
│  [ ] APP_MODE      debug                                                                         │
│> [ ] DATABASE_URL  x left out                                                                    │
│  [x] API_TOKEN     you'll store this next                                                        │
│                                                                                                  │
│                                                                                                  │
│                                                                                                  │
╰──────────────────────────────────────────────────────────────────────────────────────────────────╯

 enter continue   d bring back   esc back
```

**The summary.** It shows the project, the root, the manifest file, the output
the sample creates and whether git ignores it (render refuses a file git would
track), and how many variables are plain and how many are secret. If it is
taller than the terminal, `j`/`k` scroll it. `enter` creates the project: it
validates, saves the manifest, then saves the registry, the order `project
init` uses.

```console
 Set up a project
  Project   Seed   Review  [ Create ]
╭──────────────────────────────────────────────────────────────────────────────────────────────────╮
│  project    myapp                                                                                │
│  root       /tmp/t6/proj/myapp                                                                   │
│  manifest   /tmp/t6/cfg/projects/myapp.toml                                                      │
│  output     root -> .env                                                                         │
│             git ignores it                                                                       │
│  variables  1 plain, 1 secret; you'll store the secrets next                                     │
╰──────────────────────────────────────────────────────────────────────────────────────────────────╯

 enter create   esc back
```

**Landing.** Setup then opens the interface on Variables. When any secret still
has no value, the list is filtered to those (`!` toggles the filter) and the
message says how many to store: `enter` on a row's profile stores its value.
When nothing is missing the list is unfiltered, and when you started empty the
message points at `a` to add a variable.

```console
 myapp · dev (default)                                                                 ! 1 problem
 [ Variables ]  Outputs   Profiles
╭──────────────────────╮╭──────────────────────────────────────────────────────────────────────────╮
│ problems only -- ! to││  API_TOKEN                                             secret · required │
│ show all             ││                                                                          │
│> API_TOKEN   secret !││  dev *       -                                            MISSING !      │
│                      ││                                                                          │
│                      ││  ! no value under dev -- enter on dev to store it                        │
│                      ││  written to  root (.env)                                                 │
╰──────────────────────╯╰──────────────────────────────────────────────────────────────────────────╯
 created myapp -- 1 secret to store: enter on each
 enter open   d delete   a add   . more   / filter   ? keys
```

### Reviewing an import

`trenvy import <file>` without `--no-tui` runs the same review and summary
over that file, as two steps, *Review* and *Import*, in an already registered
project. The keys are the review's; `esc` cancels the import, so nothing is
saved and the command exits with an `import cancelled` error. The summary has no
project or root rows, and adds a *conflicts* row: variables already in the
manifest with a different definition. Without `--force` the summary has no
`enter import`, `enter` is refused, and nothing is written; with `--force` it
says it will overwrite them.

```console
 Import .env.staging.example
  Review  [ Import ]
╭──────────────────────────────────────────────────────────────────────────────────────────────────╮
│  manifest   /tmp/t6/cfg/projects/myapp.toml                                                      │
│  output     root-2 -> .env.staging                                                               │
│             git does not ignore it; render will refuse until it does                             │
│  variables  1 plain, 1 secret; you'll store the secrets next                                     │
│  conflicts  APP_MODE: already in the manifest with a different definition; re-run with --force   │
│             to overwrite                                                                         │
╰──────────────────────────────────────────────────────────────────────────────────────────────────╯

 esc back
```

After the summary's `enter`, import saves and prints what it imported, as it
always has, and exits; it does not open the interface. The review has no
add-a-variable or filter keys: add variables in `trenvy ui` afterwards.

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
(default: the current directory); it asks nothing, while `trenvy ui` outside a
project does the same interactively. `project rm` takes `--yes`.

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
| `trenvy secret demote <NAME>` | Turn a secret back into a plain variable, moving its values into the manifest |
| `trenvy secret rm <NAME>` | Delete a secret's value from the keyring, keeping the variable |
| `trenvy unset <NAME>` | Remove a variable from the manifest and its secret from every profile's keyring |
| `trenvy edit` | Open the interactive interface (the same as trenvy ui); outside a project, open setup |
| `trenvy ui` | Manage this project's variables, outputs and profiles, interactively; outside a project, open setup |

Flags: `import` takes `--output <name>`, `--no-output`, `--no-tui` (skip the
review) and `--force`; `set` takes `--shared`; `show` takes `--reveal` and
`--format dotenv|json`; `secret set` takes `--stdin`; `secret demote`, `secret
rm` and `unset` take `--yes`.

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

`trenvy ui` and `trenvy edit` open the same interface. It is built around
four promises: every value is on screen without a keypress (secrets masked), a
key means the same thing on every screen, edits are **staged** until you
review them with `w`, and saving does **not** quit — you keep working in the
same session.

Presence is loaded with `Has`, never `Get`, so opening it never raises a
keyring authorization prompt. A secret's real value is read only when you ask:
`v` on a value, *Render now* and *Check now* on an output, and `trenvy secret
demote` outside the interface.

### The layout

```console
$ trenvy ui
 myapp · dev (default)                                                                ! 2 problems
 [ Variables ]  Outputs   Profiles
╭───────────────────────╮╭─────────────────────────────────────────────────────────────────────────╮
│> API_TOKEN    secret !││  API_TOKEN                                            secret · required │
│  APP_MODE             ││                                                                         │
│  DATABASE_URL         ││  dev *       ••••••••                                    stored         │
│  PUBLIC_URL           ││  staging     -                                           MISSING !      │
│                       ││  prod        -                                           MISSING !      │
│                       ││                                                                         │
│                       ││  ! no value under prod -- enter on prod to store it                     │
│                       ││  ! no value under staging -- enter on staging to store it               │
│                       ││  written to  local (.env)                                               │
│                       ││                                                                         │
│                       ││                                                                         │
╰───────────────────────╯╰─────────────────────────────────────────────────────────────────────────╯

 enter open   d delete   a add   . more   / filter   ? keys
```

- **Header.** The project, then the profile in effect and where that choice
  came from: `default` above, or `active`, `TRENVY_PROFILE` or `--profile`.
  On the right, the problem count, and — once something is staged — the
  unsaved count with the key that reviews it. Each appears only when it is
  non-zero.
- **Screens.** **Variables**, **Outputs** and **Profiles**, switched with
  `1`, `2` and `3`, or `tab` and `shift+tab`.
- **Size.** It needs a terminal of at least 40 by 10; smaller, it says so and
  does not draw.
- **Body.** A list on the left, the selected row's detail on the right. `→`
  or `l` moves into the detail, `←` or `h` back. Narrower than 80 columns the
  two stack, and `enter` opens the detail full width, `←` returns.
- **Message line.** One short line saying what just happened and how to
  reverse it, cleared by the next keypress.
- **Footer.** Only the keys that work for the current selection, with `? keys`
  always last.

Problems are marked where they live: a `!` beside the row, listed at the foot
of its detail with what to do about it, and counted in the header. `!` limits
the list to rows with a problem.

### Variables

The list holds one row per variable, with a `secret` tag on secrets and a mark
for what is going on: `!` a problem, `~` staged, `x` marked for deletion.
Marks are characters before they are colour, so they read the same with
`NO_COLOR` set — every screen in this section was captured that way.

The detail shows what the variable is worth under **every** profile, and why:

```console
 myapp · dev (default)                                          ! 2 problems    3 unsaved  w review
 [ Variables ]  Outputs   Profiles
╭───────────────────────╮╭─────────────────────────────────────────────────────────────────────────╮
│  API_TOKEN    secret ~││  APP_MODE                                              plain · required │
│· APP_MODE            ~││                                                                         │
│  DATABASE_URL         ││  shared      debug                                        ~             │
│  PUBLIC_URL           ││                                                                         │
│                       ││> dev *       staging                                     override ~     │
│                       ││  staging     debug                                       from shared    │
│                       ││  prod        production                                  override       │
│                       ││                                                                         │
│                       ││  written to  local (.env)                                               │
│                       ││                                                                         │
│                       ││                                                                         │
╰───────────────────────╯╰─────────────────────────────────────────────────────────────────────────╯
 APP_MODE override for dev staged -- u to undo
 enter edit   d remove   u undo   ← back   . more   ? keys
```

A plain variable gets a `shared` row — the default — and then one row per
profile. A value a profile inherits says `from shared`; one it sets itself says
`override`. `*` marks the profile in effect. `~` marks a value that is staged
but not yet saved. A secret has no shared row (the manifest refuses a secret
with a value in it); each profile's row reads `stored` or `MISSING`, and the
value is masked as `••••••••`.

`→` enters the detail and `enter` edits the row under the cursor. The edit
field says which thing you are changing — *Shared value -- used by dev,
staging* on the shared row, *Override for dev only* on a profile's — so
editing a `shared` value is never mistaken for creating an override. A secret's
field is masked while you type. `v` reveals one secret's value, and `v` again
hides it. `d` on an override removes it, so the profile falls back to `shared`;
`d` on a variable in the list marks it deleted, and again restores it. `a`
adds a variable: its name, then `p` plain or `s` secret, then its first value.

`.` opens the actions for the selection: *Make secret*, *Mark optional* (or
*Mark required*), *Rename*, *Project info* and *Switch project*. Two of these
are listed but unavailable, and say why when you press `enter` on them:
*Rename*, because renaming a variable is not supported yet, and *Switch
project* while no other project is registered. *Project info* is where the
manifest, worktree and backend paths went.

*Make secret* is one way **inside the interface**. A variable carrying a
shared value confirms first, naming that it becomes one keyring item per
declared profile, which can drift apart once any one of them is edited later;
declining stages nothing. A variable with only per-profile overrides stages at
once, with no such prompt. The way back is the command line:
`trenvy secret demote <NAME>` reads each profile's keyring value, writes it
into the manifest as plain text, and then deletes the keyring items. It asks
first unless you pass `--yes`, and it refuses a variable that is not a secret.

### Outputs

```console
 myapp · dev (default)                                                                ! 3 problems
  Variables  [ Outputs ]  Profiles
╭──────────────────────────────╮╭──────────────────────────────────────────────────────────────────╮
│  local default STALE        !││  web                                                        json │
│> web           FOREIGN      !││                                                                  │
│                              ││  format        json                                              │
│                              ││  path          web.json                                          │
│                              ││  state         FOREIGN  a file trenvy did not write is at this   │
│                              ││                         path                                     │
│                              ││                                                                  │
│                              ││  new variables not included -- only the checked ones             │
│                              ││                                                                  │
│                              ││  [ ] API_TOKEN                                                   │
╰──────────────────────────────╯╰──────────────────────────────────────────────────────────────────╯

 enter open   d remove   a add   . more   ? keys
```

The list is every output with its state; the detail is its format, path,
state, what new variables do, and the membership grid. The state is the word
`trenvy status` prints, with a muted gloss after it:

| state | gloss |
|---|---|
| `fresh` | up to date with the manifest |
| `not rendered` | nothing written in this worktree yet |
| `STALE` | out of date -- render to update it |
| `MODIFIED` | edited by hand since the last render |
| `FOREIGN` | a file trenvy did not write is at this path |
| `unverified` | not checked, holds secrets -- `.` then *Check now* |
| `unknown` | can't tell -- a secret is missing or the file can't be read |

`unverified` is this interface's word for what `trenvy status --quick` prints
as `unknown (--quick)`: the file is there, but telling a match from a changed
secret means reading secrets, and opening the interface does not.

**Membership** is edited in place. `→` enters the grid of checkboxes, `space`
toggles one, and a `~` marks a box you changed. The sentence above the grid says
what a variable added *later* does — `new variables included automatically`
or `new variables not included -- only the checked ones` — and the `.` menu
item *Stop including new variables* or *Include new variables automatically*
flips it, without moving any checked box.

`enter` on `format` or `path` edits it. `a` adds an output: name, then format,
then path. A dotenv output's path starts as `.env`, or as `<name>.env` when
another output already renders to `.env`. A path another output renders to is
refused, as `trenvy output add` refuses it, because one would overwrite the
other's file. A path git does not ignore is warned about on the message line
after the first `enter`; a second `enter` takes it anyway. Both checks apply
to editing a path too. `d` marks an output for removal.

Saving a path edit stops tracking the file at the old path: it stays on disk,
and the save lists it, as removing an output does. Saving a format edit marks
the rendered file `STALE`, because it is still in the old format; *Render now*
rewrites it.

`.` on an output offers *Make default*, *Rename*, the new-variables item,
*Render now*, *Remove rendered file* and *Check now (reads secrets)*. The last three
act **immediately**, not staged, because they touch this worktree's file and
not the manifest:

- *Render now* is `trenvy render` for that output, guards included: a path git
  does not ignore, or a file trenvy did not write, is refused with the
  command's own message, and nothing is written.
- *Remove rendered file* is `trenvy clean`, and asks first.
- *Check now* reads the secrets, which may prompt the keyring, and settles
  `unverified` into one of the other words until the next reload.

All three are unavailable while that output has staged changes, since they
would act on what is on disk rather than what the screen shows; the reason is
shown when you press `enter` on one — *save or undo local's staged changes
first -- they are not on disk yet*.

A variable that reaches several outputs is not a problem. It shows on the
variable's own `written to` line instead.

### Profiles

```console
 myapp · dev (active)                                                                  ! 1 problem
  Variables   Outputs  [ Profiles ]
╭────────────────────────────╮╭────────────────────────────────────────────────────────────────────╮
│  dev     default · active  ││  staging                                                           │
│> staging 1 missing        !││                                                                    │
│  prod                      ││  overrides   0                                                     │
│                            ││  secrets     0 of 1 stored                                         │
│                            ││  missing     API_TOKEN                                             │
│                            ││                                                                    │
│                            ││                                                                    │
│                            ││                                                                    │
╰────────────────────────────╯╰────────────────────────────────────────────────────────────────────╯

 enter open   d remove   a add   . more   ? keys
```

`3` opens it. The list is every profile in the manifest, in its order, with
two tags: `default` is the manifest's default profile and `active` is this
machine's selection (the one `trenvy profile use` saves). A profile with
required variables that have no value carries a `!`, and its note ends
in `N missing` after any tags (`default · active · 1 missing`), so the count
is never hidden by a tag or a staged mark; `~` marks one with staged changes
and `x` one marked for removal.

The detail shows the profile's **overrides** (plain variables with a value of
their own under it), its **secrets** stored out of the total, and the names
still **missing**. A secret counts as stored when it is in the keyring, which
the interface learns with `Has`, never by reading it, or when a value for it
is staged this session. `→` or `enter` on a profile moves into the detail,
and `enter` on a missing name opens it in Variables, on that profile's row.

- `a` adds a profile by name. It is staged, and it gets its values in
  Variables **after you save**: Variables lists the profiles on disk, so the
  new one's detail says *new -- save, then give it values in Variables*.
- `d` marks a profile for removal, and again keeps it. It is refused for the
  last profile, for the default profile (make another the default first), for
  the profile this session is on (use `trenvy -p <other> ui` to remove it),
  and when removing it would leave a required plain variable with no value.
  Values already staged on the profile's cells are dropped, with the count in
  the message, and keeping the profile does not bring them back. A *Use on
  this machine* staged on it is dropped too, and the message says so. On
  Variables the profile's cells read `x removed` and cannot be edited, and
  *Make secret* leaves it out.
- Saving a removal **asks for the profile's name to be typed**, one profile at
  a time, before anything is written; a wrong name keeps the panel open and
  `esc` returns to the review. It then deletes the profile's stored keyring
  items (only those that exist; the review lists them) and drops its
  overrides from the manifest, as `trenvy profile rm` does. Removing the
  profile this machine had selected clears the selection.
- `.` offers:
  - *Use on this machine* saves the selection to the state file, as `trenvy
    profile use` does. It is disabled for a profile not saved yet, one
    already active, and one marked for removal. If the session was opened with
    `--profile` or `$TRENVY_PROFILE`, those still outrank the selection, so
    the message and the review line say so and the header does not change
    after the save.
  - *Make default* changes the manifest's `default_profile`.
  - *Rename* is always disabled: a profile's name is part of every keyring
    item stored under it, and renaming would mean moving each secret, which
    is not supported.

### Saving

Nothing reaches disk until you press `w`, and the review shows exactly what
would happen:

```console
 myapp · dev (default)                                          ! 2 problems    4 unsaved  w review
 [ Variables ]  Outputs   Profiles
            ╭──────────────────────────────────────────────────────────────────────────╮
            │Review  4 changes                                                         │
            │                                                                          │
            │API_TOKEN                                                                 │
            │> ~ prod                  store new value (keyring)                       │
            │APP_MODE                                                                  │
            │  ~ shared value          development  ->  debug                          │
            │  ~ dev override          development  ->  staging                        │
            │PUBLIC_URL                                                                │
            │  x delete variable       its values leave the manifest                   │
            │                          ! cannot be undone after saving                 │
            ╰──────────────────────────────────────────────────────────────────────────╯


 enter save all   u drop this change   esc keep editing
```

Changes are grouped by what they touch, and a line that changes a value reads
*before -> after*; the others say what happens, such as `store new value
(keyring)` or `its values leave the manifest`.
A plain value is shown in full. A secret's never is — its line says `store new
value (keyring)`. Lines that cannot be undone after saving (a deleted
variable, a variable turned into a secret) carry `!` and say so. Every line
can be dropped with `u`, and anything that depended on it goes with it and
says so, so dropping a new variable also drops its value edits. Dropping the
last change closes the review.

`enter` saves everything left. While it writes, input is ignored except
`ctrl+c`. On success the review closes, the screen reloads from disk, and the
message line says `saved 3 changes`, and you are still in the interface. If a
save fails partway, the review stays open with how far it got, and the **whole
batch stays staged**: `enter` retries all of it, which is safe because the
manifest is saved last and everything before it can be repeated. A batch the
manifest refuses outright wrote nothing, and the review says so, pointing at
the change to drop with `u` or to edit instead of offering a retry. `esc`
leaves the review with nothing written.

The commit's order is unchanged from the interface it replaced, and is
described in [architecture.md](docs/architecture.md#the-staged-commit). A
batch the manifest would refuse is caught **before** the first keyring write.
`q` with unsaved changes asks first (`w` review, `q` discard and quit, `esc`
cancel). `ctrl+c` quits at once, from anywhere, writing nothing. There is no
rollback: a deleted keyring item cannot be undeleted.

### Keys

Press `?` for the list. It wraps rather than truncates:

```console
 myapp · dev (default)                                                                 ! 1 problem
 [ Variables ]  Outputs   Profiles
            ╭──────────────────────────────────────────────────────────────────────────╮
            │Keys                                                                      │
            │                                                                          │
            │↑↓  j k     move                                                          │
            │←→  h l     switch pane                                                   │
            │1 2 3  tab  switch screen (shift+tab goes back)                           │
            │enter       the primary action on the selection                           │
            │a           add                                                           │
            │d           mark deleted, or remove a value (again to unmark)             │
            │u           undo the staged change on the selection                       │
            │v           reveal or hide the value under the cursor                     │
            │/           filter by name                                                │
            │!           show only rows with problems                                  │
            │.           more actions for the selection                                │
            │w           review and save                                               │
            │q           quit (asks first if anything is unsaved)                      │
            │ctrl+c      quit now, writing nothing                                     │
            │esc         close the panel on top                                        │
            │?           this list                                                     │
            ╰──────────────────────────────────────────────────────────────────────────╯
 esc close
```

These mean the same on every screen. A key that does nothing for the current
selection is left out of the footer.

Anything that needs more than a key — a field to type into, the `.` menu, the
review, a confirmation, `?` — opens as one panel over the body, and `esc`
closes that panel and does nothing else. In a text field, `ctrl+u` clears
what is there.

### Colour

The interface uses your terminal's colours. At startup trenvy asks the
terminal for its background, foreground and palette, builds the interface's
colours from them, and moves any colour that would be hard to read just far
enough toward white (on a dark background) or black (on a light one) to be
readable, so every piece of text keeps at least 4.5:1 contrast against what
it is drawn on and still looks like your theme. Dark or light is judged from
the background's own brightness, not from the operating system's setting.

It follows a theme switch while it runs:

- at once, when the terminal announces that its dark/light mode changed
  (terminals that support DEC private mode 2031);
- on focus, when the terminal reports focus changes (best effort);
- within about 5 seconds, when the terminal does not announce changes. Over
  SSH this timer is off, so a switch is picked up on focus or on the signal
  below;
- at once, when trenvy receives `SIGUSR2` (below).

The setup flow and the interactive import review follow only the terminal's
announcement and `SIGUSR2`, not focus or the timer.

If the terminal does not answer in time, or answers only in part, trenvy uses
its own palette for the whole interface, never a mix of the two. The same
happens when `TRENVY_THEME` asks for it, and when stdin or stdout is not a
terminal. On Windows the colours are read at startup only, for now. With
`NO_COLOR` set, or output that is not a terminal, every state still reads in
words.

`TRENVY_THEME` chooses the source; see [Environment variables](#environment-variables).

#### Following an Omarchy theme switch

Omarchy runs the executable files in `~/.config/omarchy/hooks/theme-set.d/`
after a theme change. A hook that signals trenvy makes an open interface
re-read the terminal's colours at once:

```bash
mkdir -p ~/.config/omarchy/hooks/theme-set.d
printf '#!/bin/sh\npkill -USR2 -x trenvy\n' > ~/.config/omarchy/hooks/theme-set.d/trenvy
chmod +x ~/.config/omarchy/hooks/theme-set.d/trenvy
```

`pkill -USR2 -x trenvy` signals every process named `trenvy`. A `trenvy`
that is not showing the interface ignores it.

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
| `TRENVY_THEME` | where the interface's colours come from: unset or `auto` uses the terminal's colours and falls back to trenvy's own palette when the terminal does not answer; `palette` uses trenvy's own palette and detects light or dark itself; `light` or `dark` forces that palette and asks the terminal nothing; any other value is an error |
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
  `Has` operation, not a read, so `trenvy status` and the interface do
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
`trenvy ui` to be walked through [setting it up](#setting-up-a-project), or
`trenvy project list` to see what is registered and where.

**`.env is not ignored by git`**
The guard working as intended. Add the named line to `.gitignore`, or pass
`--allow-tracked` if you genuinely mean to track the file.

**`.env was modified after trenvy wrote it`**
Someone hand-edited the rendered file. Inspect it, then either fold the change
back into the manifest with `trenvy ui`, or discard it with
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
`--no-output`, or the manifest was built up with `trenvy ui` — or the output
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

## Further reading

For contributors, beside [`docs/architecture.md`](docs/architecture.md):

- [`docs/development.md`](docs/development.md) — how to build, test and
  check a change in a real terminal, and the invariants a change must keep.
- [`docs/tui-design.md`](docs/tui-design.md) — the interface's design rules
  and the tests that hold them.
- [`docs/design-history.md`](docs/design-history.md) — how trenvy came to be
  what it is, and why.
- [`docs/roadmap.md`](docs/roadmap.md) — what is next, including the polish
  backlog.

## Licence

[Apache License 2.0](LICENSE).
