# Roadmap

What comes next, in order. Each item says what is known, what is not, and
the next concrete step. [design-history.md](design-history.md) explains how
the current design was reached; [tui-design.md](tui-design.md) is the
interface's rules, which any of this has to keep.

## 1. Follow the system theme

**Goal:** under Omarchy, the interface takes its colours from the current
system theme, the way btop does, and changes with it.

The interface already keeps every colour behind a named role on one
`style.Theme`, with the values only in `internal/style/theme.go`, so a new
colour source only has to produce those roles. Palette C stays as the
fallback, and the contrast check (`TestThemeContrast`, every text role at
least 4.5:1 on every surface) stays the gate any loaded palette must pass.

### What a read-only look at Omarchy found

Source: the `basecamp/omarchy` repository, default branch `quattro`, at
commit `81145eb` (2026-10-05). The latest release at the time was v4.0.4;
tags v3.0.0 to v3.8.4 were also checked. Nothing below has been tried on a
machine running Omarchy.

- **`colors.toml` is the source, not `btop.theme`.** We had believed each
  theme ships a `btop.theme` that is swapped on a theme change. That held for
  3.x and is wrong now: 4.x generates `btop.theme` from `colors.toml`, and
  only 4 of its 22 themes still ship one. A `btop.theme` is a poor source
  anyway: its background is often empty, its keys are optional, and it has
  no red/green/yellow meanings and no light/dark mode. Every theme has had a
  `colors.toml` since 3.4.
- **Where to read it depends on the Omarchy version:**
  - 4.x: `~/.local/state/omarchy/current/theme/colors.toml`;
  - 3.4 to 3.8: `~/.config/omarchy/current/theme/colors.toml` (a copy);
  - 3.0 to 3.2: the same path as a symlink, with no `colors.toml` before 3.4.

  4.0 moved the directory with a migration and left no compatibility link,
  so read the 4.x path first and fall back to the 3.x one.
- **Light or dark comes from the file.** Since 4.0 every theme declares
  `mode` (5 light, 17 dark). Omarchy's own order, which trenvy should copy:
  the `mode` key, then the legacy `theme_type`, then a `light.mode` file,
  then the background's brightness (r+g+b above 382 is light), else dark.
- **The format is flat `key = "value"` lines.** Every 4.x theme has `mode`,
  `accent`, `selection`, `muted`, `background`, `dark_background`,
  `darker_background`, `lighter_background`, `foreground`,
  `dark_foreground`, `light_foreground`, `bright_foreground`, `red`,
  `yellow`, `green`, `cyan`, `blue`, `magenta` and the `bright_*` colours;
  `orange` and `brown` are missing from three. Older themes use the ANSI
  `color0`..`color15` layout. Omarchy's `bin/omarchy-theme-color` resolves
  aliases, light/dark and the legacy layout; trenvy should resolve them the
  same way rather than invent its own.
- **A theme switch is not atomic.** Omarchy copies the theme into
  `next-theme`, derives `colors.toml` from `alacritty.toml` when a theme
  lacks one, renders its templates (never overwriting a file the theme
  ships), then removes `current/theme` and moves `next-theme` into its
  place, then writes `theme.name`. Between the remove and the move there is
  no file, so a reader must fall back quietly rather than error.

### The proposal

**Raw theme colours do not pass trenvy's contrast bar.** Measured against the
22 shipped themes, 94 role and surface pairs fall below 4.5:1. Moving each
failing role toward white (on a dark theme) or black (on a light one), just
until it passes on every surface it is drawn on, fixed all 22; the largest
shift was 44%, for muted on gruvbox. So: nudge per role, and use palette C
only when the whole file is missing or malformed, never role by role, so a
screen is never half one palette and half another.

Role mapping:

| trenvy role | from `colors.toml` |
|---|---|
| title | `bright_foreground` |
| text | `foreground` |
| muted | derived: `foreground` mixed 40% toward `background` (Omarchy's `muted` is a border colour and fails as text in 19 of 22 themes) |
| accent | `accent` |
| danger, ok, warn, staged | `red`, `green`, `yellow`, `magenta` |
| surface: header | `dark_background` |
| surface: selection | `selection`, or `background` mixed 12% toward `foreground` |
| border: unfocused | `muted` |
| pills | `background` text on a `red` (danger) or `magenta` (staged) fill |

`TRENVY_THEME` would become:

- `light` or `dark`: palette C, forced, as today. Always wins.
- `omarchy`: use the file; if it is missing or does not parse, warn once on
  stderr and fall back to palette C.
- unset: use the file when it exists and parses, otherwise palette C, with
  no message.

There is no operating-system check: the file is the signal.

**Read once at startup** for the first version. Following a live switch can
come later, by watching `~/.local/state/omarchy/current/` or by handling
`SIGUSR2` and documenting an Omarchy hook (a script in
`~/.config/omarchy/hooks/theme-set.d/` running `pkill -USR2 -x trenvy`,
which Omarchy's `bin/omarchy-hook` runs on every theme change). Omarchy
already restarts btop this way.

### Before building

None of this can be verified on macOS. On a machine running Omarchy, check:

- the installed version (`ls ~/.local/state/omarchy/current/theme/`);
- whether lipgloss detects truecolor in Omarchy's terminals: rounding to 256
  colours could undo the 4.5:1 nudge;
- what the nudged colours look like, and whether they still read as the
  theme;
- whether lipgloss re-detects the background inside tmux.

Also unknown: how long the remove-then-move gap lasts, how `SIGUSR2` fits
bubbletea's own signal handling, third-party themes, and how stable an
undocumented path is (it moved once, in 2026-06).

The contrast gate has a gap to close first: `style.ContrastPairs` is kept by
hand beside `NewTheme`, so a theme built from loaded colours needs its pairs
derived from the same values, or the test proves nothing about it (backlog,
Tests).

## 2. Polish backlog

Every deferred minor finding and parked item recorded during the work so
far, checked against the code at `219dcd6`. Items already fixed were
dropped. Within each group the first item matters most to someone using
trenvy; groups follow the order of the screens, then the command line,
tests and docs.

### Variables

1. **Pasting only line breaks into a field whose value is still selected
   clears the value.** The paste drops the line breaks, leaving nothing to
   insert, but the selected value is replaced anyway, and an empty value is
   staged. `field.update` should leave the field unchanged when the paste
   inserts nothing (`internal/manage/lineedit.go`).
2. **At 40 columns the detail header cuts its meta.** `x marked for
   deletion · secret · required` runs past the pane and is cut, losing the
   end. The Outputs detail wraps the same kind of text under the name; do
   the same here (`detailBody`, `internal/manage/variables.go`).
3. **`v` on a plain variable says "nothing stored to reveal".** It should say
   only secrets can be revealed. The footer does not offer `v` there.

### Outputs

1. **A path already in use is refused twice at once**: in the panel and on
   the message line. At 40 columns the two-line message cap then cuts the
   tail of that refusal and of the gitignore warning. Show a panel's live
   problem in the panel only.
2. `Staged.DefaultOutput` repeats the loop that `single` already provides for
   the profile kinds (`internal/session/staged.go`).
3. The Outputs list measures names without `display()`. No effect today,
   because output names are validated to letters, digits, `-` and `_`.

### Profiles

1. `missingFor` and `profNote` scan every problem for every row on every
   frame. Harmless at realistic sizes.
2. `undoProfile` clamps the cursor even when nothing was added, and `New`
   sets the Profiles state explicitly to its zero value. No behaviour effect.

### Setup and import

1. **A sample at the worktree root creates an output named `root`**, and a
   second one `root-2`. Accurate, but odd to read; the user can rename it.
2. `resolve.ProjectRootGuess` treats any git common directory beside the
   worktree (for example `proj.git`) as a bare layout, not only `.bare`.
   Arguably right, but untested, as is a worktree outside the bare root.
3. `importInto` applies variables to the manifest in memory before a
   conflict later in the list stops it. Every caller passes a fresh or
   throwaway manifest, but its doc does not say they must.

### Review

1. `j` past the last change moves an offset into the failure report even
   when everything already fits, so presses do nothing visible on a tall
   terminal.

### Layout

1. **Below 40×10 the "needs at least 40×10" sentence is itself about 53
   cells wide**, wider than the terminal it describes. Wrap it.
2. **The list mark column sits flush against the right border** on every
   screen (`!`, `~` and `x` touch the `│`).
3. **Overlay panels have no inner left padding**: titles such as `Review` and
   `More` touch the border.
4. `style.Columns` reslices the caller's rows (`row[:end]`). Safe today,
   since it never writes through them.
5. A field's view returns three cells for widths below three. Unreachable
   at the 40-column minimum.

### Command line

1. **Partial-failure messages do not pluralise**: `1 items were already
   deleted`, `1 secrets were already deleted`, and `0 items were already
   deleted` when the first delete fails (`project.go`, `profile.go`,
   `set.go` in `internal/cli`).
2. `prevalidate` returns a manifest-clone error unwrapped, so the interface
   would offer it as a retry rather than as a refused batch. Practically
   unreachable.
3. `session.Problem.Fix` is still computed for every problem, but nothing
   reads it: the interface words its own fixes.
4. `manifest.Validate` repeats `CheckProfileName`'s message instead of
   calling it.
5. `project list` would misalign if a registry root were empty; `project
   init` cannot produce one.
6. The completers sort `name\ttag` strings rather than names. Equivalent
   today, since a tab sorts below every character a name may hold.
7. `profile add`'s duplicate and invalid-name errors carry no sentinel.
   Needed only if a caller has to tell them apart.

### Tests

1. **`style.ContrastPairs` is not tied to `NewTheme`.** A role recoloured in
   `NewTheme` alone still passes `TestThemeContrast`. Derive the pairs from
   the theme's own values, or cross-check them. Do this before section 1.
2. No direct tests for `Staged.DropCell`, `DropKind`, `Clear`, or `Drop` out
   of range; they are covered only through the interface's tests.
3. `stateWord` (`internal/cli/status.go`) has no direct test of its mapping
   or its panic on an unknown state.
4. No test pins `errors.Is` and `errors.As` through `wrappedError`
   (`internal/cli/ui.go`).
5. `Theme.Pane` has no tests for a zero `Theme`, the focused border colour,
   sizes below three, or `OnHeader`.
6. Line editor: the clearing keys (`ctrl+u`, `ctrl+k`, `ctrl+w`, delete) on a
   selected value are untested, and `display` has no case for DEL (`0x7f`).
7. Interface paths with no test: `a` and `enter` refused while a reload has
   failed (only `d` is tested); `d` on a secret's detail row; `d` on a
   deleted variable's row; the singular `saved 1 change`.
8. Assertions that could pass for the wrong reason:
   - `TestFilteringOrTogglingProblemsHidesRevealedValues` never re-enters
     the detail before checking;
   - the `cursorLines` helper matches any `>`, not a cursor at a row start;
   - `TestSwitchingTheFutureModeKeepsTheBoxes` never inspects the staged
     globs, and `undoOutputRow` has no direct test;
   - the add-output panel's live problem line is untested at 40 columns;
   - `actionBlocked`'s not-saved, removed and stale branches, and input held
     during an action, are untested;
   - `TestReviewUseLineSaysWhenOutranked` compares collapsed whitespace, so
     the `  ->  ` spacing is not pinned;
   - `TestEditingACellOnARemovedProfileIsRefused` asserts exactly one staged
     change, coupling it to its setup, and nothing checks that reveal still
     works on a removed profile's row;
   - `TestProfilesNoteIsNeverCut` picks its row loosely, never asserts the
     name's `…`, and builds names with `string(rune)`;
   - `TestCommitRefusesAnInvalidProfileName` checks only the error's type;
   - `TestSetupFromAWorktreeIsFoundFromItsSibling` checks only that the
     output contains `dev`;
   - `secret demote`'s never-print checks use the value `d`, which can match
     by accident;
   - `TestColumnsHandlesRaggedRows` checks containment and trimming, not the
     exact output.
9. `internal/style`'s exact-colour table runs only on the ANSI profile, and
   tokens sharing a colour (`MISSING` and `FOREIGN`; `optional`,
   `not rendered` and `unknown`) cannot be told apart by it. Its `ansiRE`
   is declared mid-file.
10. A failure message in `internal/render/json_test.go` says
    "HTML-escaped ampersand" where Go emits a JSON unicode escape.

### Docs

1. **The CHANGELOG's *Unreleased* section still describes the first
   interface** in its *Added* entry for `trenvy ui` (the Overview tab, the
   matrix, `w` quitting) and mentions the Overview under *Changed*, which
   contradicts the *Breaking* entry. Rewrite it before a first release.
2. **Code comments cite internal task numbers and review rounds** that are
   not in the repository: 21 places in 8 non-test files (among them
   `internal/session/change.go`, `staged.go`, `membership.go`, `matrix.go`
   and `commitStaged`'s doc in `internal/cli/ui.go`), and more in tests.
   Some are also stale: `matrix.go` still calls itself "behind the
   management TUI", a session test speaks of two membership conditions where
   one remains, and `commit_kinds_internal_test.go` describes the old
   validate-after-keyring order in the present tense. Reword each to state
   the reason itself.
3. `outputStates` still documents and returns a flag for a "caveat" that
   nothing reads since the Overview went (`buildSnapshot` discards it).
4. `commitStaged`'s doc does not say a retry must start from a re-resolved
   app (it mutates the manifest and state in memory). The interface does
   this, and says so at its one call site in `internal/cli/manage.go`.
5. The README does not describe setup's summary after *Start empty*
   (`variables  none; add them in Variables afterwards`).
6. `resolve.ErrUnknownProfile`'s doc says what it is, not why it exists.

## 3. Housekeeping

Done by the owner, locally, and listed only so nothing is forgotten:

- Every work branch is merged into `main`. Their worktrees can be removed:
  `profiles`, `validate-project-id`, `outputs-management`,
  `status-and-profile`, `management-tui`, `tui-phase-2a`, `active-profile`,
  `lipgloss-restyle`, `tui-redesign`, `tui-redesign-phase2`,
  `tui-redesign-phase3`, `tui-redesign-phase4` and
  `fix-setup-narrow-panic`.
- An old note said `trenvy profile use` printed an `export` instead of
  switching. That is resolved: it persists the active profile (design
  history, section 6).

## 4. Larger features not started

### Nested config output (for Pydantic settings and YAML)

Settled in outline, not designed in detail: a `nest = '.'` setting on an
output turns dotted variable names into nested keys, so `database.username`
renders as `{"database": {"username": …}}`.

- **Values can stay strings.** Pydantic coerces `"20"` to an int and
  `"true"` to a bool, which was checked when this was first discussed, so
  resolution does not need typed values.
- **Open: YAML or JSON.** Either add a YAML dependency, or render JSON into a
  `.yaml` path (YAML is a superset of JSON): no new dependency, but ugly for
  people reading the file.
- **Open: the collision rule.** `database` and `database.username` both set
  cannot both be written; decide whether that is a validation error or a
  render-time refusal.

### The palette in the command-line output

`status`, `show` and the lists still use `style.Styler`'s 16-colour tokens,
so the commands and the interface use different palettes. Bringing palette C
(or the system theme) to them has to keep the command line flat and
pipe-safe and machine-readable output byte-identical.

### Writing `.gitignore` entries from setup

Setup's summary says whether git ignores the output it will create, and
`render` refuses a path git would track, but setup never offers to add the
line. Doing it means deciding which `.gitignore` to edit and saying so before
`enter`.

### Renaming a variable

Shown disabled in the Variables `.` menu. A rename has to move a secret's
keyring item under every profile (a read, a write and a delete each) and
every output glob that names the variable, and survive failing halfway.

## 5. First release

On hold, by choice. There is no remote and no tag yet, and the CHANGELOG has
a single *Unreleased* section holding everything. Recorded for when it
happens:

- The first tag should be `v0.1.0`, not `v1.0.0`: the manifest schema has
  already changed once in a way that orphaned stored secrets.
- GoReleaser was the intended tool: builds for every platform, checksums,
  archives, GitHub releases and a Homebrew tap from one file. CI already
  cross-compiles with `CGO_ENABLED=0`, and that must stay true.
- Several files say the project is unpublished and must change when it is
  pushed: the README's *Install* section, `CONTRIBUTING.md` ("there is no
  remote to clone from") and `SECURITY.md` (reporting by email until GitHub
  private vulnerability reporting is live).
- The CHANGELOG's stale *Added* entry (backlog, Docs 1) should be rewritten
  first.

## 6. Open threads

**Variable names are not validated, by design.** `trenvy set 'GROUP/KEY'`
is accepted. The rule belongs to each format, not to trenvy: JSON takes any
key, and a process environment carries names a shell cannot address (Python
and Node read dotted names fine). So the dotenv writer refuses a name that is
not a shell identifier, naming a JSON output or a rename as the fix, and
`set` warns when a dotenv output would select such a name. A trenvy-wide
validator was considered and rejected: it would refuse names a JSON-only
project uses legitimately.

**A literal `*` include does not select a name containing `/`.** Include and
exclude patterns use `path.Match`, whose `*` does not cross `/`. trenvy
itself writes an empty include to mean "everything", which has no such
limit, but a manifest written by hand with `include = ['*']`, or
`trenvy output add … --include '*'`, silently leaves `GROUP/KEY` out of that
output, and no warning says so for a JSON output. Either treat `*` alone as
"everything", or warn when a pattern list leaves out a name with `/`.
