# Developing trenvy

How we work on this repository. [CONTRIBUTING.md](../CONTRIBUTING.md) is the
short version for an outside contributor and still applies; this is the
fuller one. [architecture.md](architecture.md) explains the code,
[tui-design.md](tui-design.md) the interface's rules, and
[roadmap.md](roadmap.md) what is next.

## Repository layout

We keep the repository as a bare repository with every branch checked out
as a sibling directory:

```
<project-root>/
  .bare/        the bare repository
  .git          a file containing "gitdir: ./.bare"
  main/         the worktree for main
  <branch>/     one directory per branch
```

Add a worktree beside the others:

```bash
git -C <project-root>/main worktree add ../<branch> -b <branch>
```

**Never nest a worktree inside a checkout.** A nested worktree shows up in
its parent's `git status` and falls under its ignore rules, and no branch's
checkout should be buried in another's. Every worktree is a peer.

This is also the layout trenvy itself is built for: register
`<project-root>` once and every worktree, including one added later, finds
the project. `trenvy ui` run from a worktree of this layout, outside any
registered project, prefills setup's root with `<project-root>`.

## Build and install

```bash
go build ./...
go install ./cmd/trenvy        # puts trenvy in $(go env GOPATH)/bin
```

To try a build without replacing the installed one:

```bash
T=$(mktemp -d)
go build -o "$T/trenvy" ./cmd/trenvy
"$T/trenvy" --help
```

The Go version is the one in `go.mod` (`go 1.27`). There is no Makefile, no
code generation and no build tag to remember. trenvy is cgo-free on every
platform, and CI cross-compiles it with `CGO_ENABLED=0` for darwin, linux
and windows to keep it that way.

**Dependencies are not added casually.** Since the original design chose
them, every piece of work has left `go.mod` unchanged or said why up front:
the redesign hand-rolled its
line editor rather than take `bubbles`, and the restyle only promoted
lipgloss and termenv, already in the module graph through bubbletea, to
direct requirements. A new dependency needs a reason in its commit, and one
that needs cgo is almost certainly the wrong answer.

## Tests

### Test first

Every change starts with a test that fails for the reason we expect, then
the code that makes it pass. A test that has never failed proves only that
it compiles. When a test was written after its code, prove it can fail
before trusting it.

### Prove a test can fail without editing the tree

Apply a mutation to a copy of the source and point `go test -overlay` at it.
The working tree is never touched, so nothing can be committed by accident
and nothing needs reverting:

```bash
T=$(mktemp -d)
sed 's/swMuted  = Swatch{"#556275"/swMuted  = Swatch{"#64748b"/' \
  internal/style/theme.go > "$T/theme.go"
printf '{"Replace":{"%s":"%s"}}' "$PWD/internal/style/theme.go" "$T/theme.go" > "$T/overlay.json"
go test -overlay "$T/overlay.json" -run TestThemeContrast ./internal/style/
```

That run fails with `muted on header (light): #64748b on #f1f5f9 is 4.34:1,
want >= 4.5`, which is the contrast test doing its job. The same mechanism
adds a throwaway test file to a package (map a new path in the package
directory to a file elsewhere), which is how to probe a screen at a width no
test covers without leaving anything behind.

Watch for a test that passes for the wrong reason. Before a command or flag
exists, cobra's own "unknown command" or "unknown flag" error can satisfy an
assertion about the error text, so assert on a phrase only the new code can
produce.

### Commands

```bash
go test ./...                    # everything
go test -race ./internal/...     # the race detector over the packages that have tests
go test ./... -race              # exactly what CI runs
go vet ./...
gofmt -l .                       # must print nothing
```

CI runs `go vet` and `go test ./... -race` on macOS, Linux and Windows, plus
the cross-compile job.

### Hermetic runs

No test reads the real config directory or touches the real keyring.

- Tests point `TRENVY_CONFIG_DIR` at `t.TempDir()` and inject the in-memory
  store through `cli.Options.NewStore`.
- When you run the binary by hand, do the same from the shell:

  ```bash
  export TRENVY_CONFIG_DIR=$(mktemp -d)
  export TRENVY_BACKEND=file          # or mem, which keeps nothing
  export TRENVY_PASSPHRASE=scratch-only
  ```

  `TRENVY_CONFIG_DIR` alone does **not** sandbox the keyring: without the
  `file` or `mem` backend, a `secret set` in a scratch project writes a real
  keyring item.
- The macOS Keychain integration test is opt-in
  (`TRENVY_INTEGRATION=1 go test ./internal/store/`) and not in CI. Run it on
  a Mac if you touch `internal/store/darwin.go`.

### Tests that need git

Tests that need a repository create one in a temp directory with `git init`
(and `git worktree add` where a layout matters). The newer ones skip when
`git` is not installed (`requireGit` in `internal/resolve/worktree_test.go`,
and the setup tests in `internal/cli/setup_test.go`). The older ones fail
instead of skipping: `internal/guard`'s, and every `internal/cli` test that
calls the harness's `gitInit` (`render_test.go`): those in `render_test`,
`exec_test`, `status_test`, `manage_test`, `root_test`, `outputs_seam_test`
and `import_test` (which also runs `git init` directly). In practice git is a
prerequisite for the suite.

## Real-terminal checks

Unit tests drive the interface with key messages and read words back. They
cannot show what a real terminal draws, so every interface change is also
run in one.

- Build the binary to a temp directory, never over the installed one.
- Use a scratch config and the `file` backend, as above.
- Drive it under tmux on a **private socket only**: `tmux -L <name>`, and
  kill only that server. Never use the default server; killing it ends
  every tmux session on the machine.
- Capture at 80 and 120 columns, as plain text (`capture-pane -p`) and with
  escapes (`capture-pane -p -e`).
- Check widths in display cells with Python's `unicodedata`, not with `awk`
  or `wc`, which count bytes and report false overflows on `─` and `•`.
- Check that the `.ansi` captures contain `38;2;`, the truecolor escape:
  palette C is truecolor, and its absence means the theme was not applied.
- README screens are captured with `NO_COLOR=1`, so they read as plain text
  and prove the interface reads without colour.

A minimal script that does all of it:

```bash
#!/usr/bin/env bash
# Drive a freshly built trenvy in a real terminal and capture its screens.
# Scratch config and the file backend only: never the real config or keyring.
# tmux runs on a private socket (-L), never the default server.
set -euo pipefail
REPO=${1:?usage: drive.sh <worktree>}
OUT=${OUT:-$(mktemp -d)}
SOCK=trenvy-drive-$$
BIN=$(mktemp -d)/trenvy
(cd "$REPO" && go build -o "$BIN" ./cmd/trenvy)

SCRATCH=$(mktemp -d)
export TRENVY_CONFIG_DIR=$SCRATCH/cfg TRENVY_BACKEND=file TRENVY_PASSPHRASE=scratch-only
PROJ=$SCRATCH/myapp
mkdir -p "$PROJ" && git -C "$PROJ" init -q
printf '.env\n' > "$PROJ/.gitignore"
printf 'APP_MODE=development\nAPI_TOKEN=\n' > "$PROJ/.env.example"
(cd "$PROJ" && "$BIN" project init >/dev/null && "$BIN" import .env.example --no-tui >/dev/null)

tm() { tmux -L "$SOCK" "$@"; }
trap 'tm kill-server 2>/dev/null || true' EXIT   # the private server only

for W in 80 120; do
  tm new-session -d -s drive -x "$W" -y 30 -c "$PROJ" \
    "env TRENVY_CONFIG_DIR=$TRENVY_CONFIG_DIR TRENVY_BACKEND=file TRENVY_PASSPHRASE=scratch-only COLORTERM=truecolor $BIN ui"
  sleep 2
  tm capture-pane -p -t drive    > "$OUT/w$W-open.txt"    # words
  tm capture-pane -p -e -t drive > "$OUT/w$W-open.ansi"   # with escapes
  tm send-keys -t drive q; sleep 1
  tm kill-session -t drive 2>/dev/null || true
done

# No line wider than the terminal, measured in display cells.
python3 - "$OUT" <<'PY'
import sys, glob, os, unicodedata
def cells(s):
    return sum(0 if unicodedata.combining(c) else 2 if unicodedata.east_asian_width(c) in "WF" else 1 for c in s)
for f in sorted(glob.glob(os.path.join(sys.argv[1], "w*-*.txt"))):
    w = int(os.path.basename(f)[1:].split("-")[0])
    widest = max((cells(l) for l in open(f, encoding="utf-8").read().splitlines()), default=0)
    print(f"{os.path.basename(f)}: widest {widest} of {w}", "OK" if widest <= w else "TOO WIDE")
PY
# Palette C is truecolor: its escapes are 38;2;r;g;b.
grep -c '38;2;' "$OUT"/*.ansi
echo "captures in $OUT"
```

Add `tm send-keys -t drive …` and further captures between the `sleep` and
the `q` to walk a change. For a README screen, add `NO_COLOR=1` to the
`env` line.

### The terminal's colours

The tests for the terminal theme need no terminal: `internal/termcolor`
parses and filters recorded replies, `internal/style` builds the theme from
them (`TestTerminalThemesPassContrast`), and `internal/manage` takes a
`ThemeMsg` as any other message. What they cannot show is a real terminal
answering and then changing its answer, so that is checked by hand: open
`trenvy ui` in Ghostty and switch the Ghostty theme while it is open. The
colours should change with no keystroke, and the cursor, any staged edit and
any open panel should stay where they were. Do it with a theme that flips
dark/light, and again between two of the same mode: whether the second is
announced is open (roadmap, section 1). Ghostty answers the DECRQM for mode
2031, so the 5 second timer is off there, and an unannounced dark-to-dark
switch follows on focus or `SIGUSR2` only. To check the signal, send
`SIGUSR2` to the interface's process, by its pid (`kill -USR2 <pid>`):
`pkill -USR2 -x trenvy` would signal every trenvy on the machine. Inside an
attached tmux (3.7c), tmux answers the colour queries with the outer
terminal's colours and forwards the 997 notification, so trenvy follows a
Ghostty switch; a detached tmux (no client) answers DA1 only, so trenvy uses
its own palette, the expected fallback.

`TRENVY_THEME=palette` shows the old palette beside it for comparison.

## Invariants a change must keep

These are the properties the rest of the code relies on. A change that
breaks one is reworking the design, not adding a feature.

- **Presence through `Has`, never `Get`.** Anything that only needs to know a
  secret exists asks `Has`: `status`, completion, the interface's snapshot.
  On macOS a `Get` can raise an authorization prompt and a `Has` cannot.
  A value is read only to deliver it (`render`, `exec`, `shell`, `get`,
  `show --reveal`, and `status` without `--quick` to judge freshness) or when
  the user asks explicitly: `v` (reveal), *Render now* and *Check now* in the
  interface, and `trenvy secret demote`.
- **`Update` is pure.** `internal/manage` does no I/O inside `Update`; every
  side effect is a `Deps` function run in a `tea.Cmd`. Setup takes all its
  facts in `SetupInput` and writes nothing. See
  [tui-design.md](tui-design.md#update-purity-and-the-deps-pattern).
- **The commit's phase order, with the manifest saved last.** Validate the
  batch on a copy, then structural additions, keyring writes, keyring
  deletes, the state file, manifest edits (renames in their own pass,
  profile removals last), then validate and save the manifest, atomically.
  Retrying a failed batch is safe only because nothing runs after that save;
  a new change kind goes into an existing phase, never after it. See
  [architecture.md](architecture.md#the-staged-commit).
- **A partial failure may leave an unreferenced keyring item, never a
  manifest entry pointing at nothing, and never a value with no copy.** The
  manifest is the only inventory of what is in the keyring. For storing and
  deleting secrets that means keyring first, manifest last. `trenvy secret
  demote` is the case where it means the opposite: its values move *into* the
  manifest, so the manifest is saved first and the keyring items are deleted
  after, and a failure there leaves only unreferenced items. Either way, a
  failure partway reports how far it got.
- **Never print a secret value**: not in an error, a log line, a success
  message or a test failure. Report the name, or the length.
- **The two axes stay separate.** A profile decides values; an output
  decides where they go. No output carries values.
- **The interface's readability invariants**: readable without colour, no
  line wider than the terminal, values never cut, text wrapped at spaces,
  and a 40×10 minimum. See
  [tui-design.md](tui-design.md#readability-invariants).

## Commits and the CHANGELOG

[Conventional Commits](https://www.conventionalcommits.org/): `feat`, `fix`,
`test`, `refactor` and `docs`, with `!` for a breaking change. The scope is
the package: `cli`, `manage`, `session`, `manifest`, `resolve`, `sample`,
`style`, `store`, `config`, `render`. Documentation commits use `docs:` with
no scope.

```
fix(manage): keep setup's header safe below the minimum width
docs: describe the setup flow
```

Nothing parses these to cut a release; they are for people reading
`git log`. Bodies say why, and what was rejected.

Every user-visible change gets a CHANGELOG line under `## [Unreleased]` in
the same commit. A behaviour change to something that already exists is
said plainly, under *Breaking* when it breaks a habit or a script. Until the
first release, *Unreleased* describes what will ship, so an entry made
untrue by later work is rewritten rather than left as history.

## Comments

A comment says **why**, not what: what breaks if someone does the obvious
thing instead. A comment that contradicts its code is worse than none,
because a reader trusts it over the code, so re-read the comments near every
change. Do not cite work items, plans or review rounds in comments; they are
not in the repository, so state the reason itself.

## Docs

- The README's screens are **real captures** of a built binary, taken with
  `NO_COLOR=1` as described above, never composed by hand. A screen that
  drifts from the binary is worse than none.
- Every command, flag and key the docs name is one that was run. Tests do not
  read help text; only running the binary checks it.
- Docs describe the system as it stands. When a change makes a sentence
  false, fix the sentence in the same change.

### Recapturing the README screens

The screens are **100 columns wide**. Each is captured at the height that
leaves its pane without empty rows: **12** for the setup and import steps
and the screen setup lands on, and **14 to 23** for the other interface
screens (the Variables layout screen is 100×17). Use the private-socket tmux setup above with `NO_COLOR=1` on the
`env` line, and a short scratch path such as `/tmp/t6`, so the paths the
screens show stay readable.

**The interface screens** use one demo project. With the scratch
environment exported (`TRENVY_CONFIG_DIR=/tmp/t6/cfg`, the `file` backend):

```bash
mkdir -p /tmp/t6/myapp && cd /tmp/t6/myapp && git init -q
printf '.env\nweb.json\n' > .gitignore
cat > .env.example <<'EOF'
APP_MODE=development
API_TOKEN=
DATABASE_URL=postgres://app@db.example.test:5432/app
PUBLIC_URL=https://app.example.test
EOF
trenvy project init
trenvy import .env.example --output local --no-tui
trenvy output add web --format json --path web.json --include PUBLIC_URL
trenvy profile add staging
trenvy profile add prod
trenvy -p prod set APP_MODE production
printf 'tok-dev' | trenvy secret set API_TOKEN --stdin
```

`trenvy ui` there at 100×17 reproduces the README's layout screen line for
line: `API_TOKEN` stored under `dev` and missing under `staging` and
`prod`, two problems, `written to local (.env)`. The other interface
screens start from this state and stage or select what each shows; read the
screen for the steps (its header, marks and message line say what was
done).

**The setup screens** use a fresh directory instead, outside any registered
project: `/tmp/t6/proj/myapp`, a git repository whose `.gitignore` holds
`.env` and whose `.env.example` holds `APP_MODE=debug`,
`DATABASE_URL=postgres://app@db.example.test:5432/app` and `API_TOKEN=`, with
the config at `/tmp/t6/cfg`. Run `trenvy ui` there at 100×12; for the review
screen, `enter` twice, then `j` and `d` to leave `DATABASE_URL` out.
