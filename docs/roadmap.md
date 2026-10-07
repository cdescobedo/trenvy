# Roadmap

What comes next, in order. Each item says what is known, what is not, and
the next concrete step. [design-history.md](design-history.md) explains how
the current design was reached; [tui-design.md](tui-design.md) is the
interface's rules, which any of this has to keep.

## 1. Follow the system theme

**Status: built.** The interface asks the terminal for its colours, builds
every role from them, nudges any role that would fall below 4.5:1 until it
passes, and follows a theme switch while it runs. Palette C is the fallback
and what `TRENVY_THEME=palette`, `light` and `dark` select. What was built,
and the rules and tests that hold it, is in
[tui-design.md](tui-design.md#the-terminal-theme); the user's side is in the
README's *Colour* section. The rest of this section is the reasoning it was
built from, and what is still open.

**Goal (met):** the interface takes its colours from the system theme, in
whatever terminal it runs in, and follows a theme change while it is open.
Every text role keeps at least 4.5:1 against the surface it is drawn on,
tinted by the theme rather than copied from it.

**What remains:**

- **Terminal colours on Windows.** Windows uses trenvy's own palette and asks
  the terminal nothing, until the Windows Terminal probe below confirms how
  replies and notifications arrive there: cancelreader cannot always cancel
  a console read, so a round could wait for the next key and eat it.
- **foot on Omarchy.** Whether foot answers the colour queries and what it
  does with DEC mode 2031 is confirmed by hand on first run on Omarchy, not
  assumed.
- **Whether dark-to-dark switches announce.** The terminal's dark/light
  notification may fire only when the mode flips. Two dark themes, or two
  light ones, may then be followed only by focus, the 5 second timer (off
  over SSH and on terminals that announce changes), or the Omarchy hook's
  `SIGUSR2`.

### The approach: ask the terminal

Whatever the platform, the terminal is what is already showing the system
theme, so trenvy asks it:

- **OSC 10 and OSC 11** for the foreground and background colours, and
  **OSC 4** for each of the 16 palette colours.
- **Nudge, then use.** Each role is built from those answers (mapping in
  tui-design.md), and any role that falls below 4.5:1 on a surface it is
  drawn on is moved toward white (on a dark background) or black (on a light
  one) just until it passes. The result is readable and still tinted by the
  theme.
- **Fall back to palette C** when the terminal does not answer in time, or
  answers only in part. All or nothing: a screen is never half one palette
  and half another.
- **Follow changes live.** Re-query when the terminal announces a dark/light
  change (DEC private mode 2031), on focus, on a 5 second timer where the
  terminal does not announce, and when Omarchy's theme-set hook signals
  trenvy (below). Dark or light is judged from the background's luminance,
  never from `CSI ? 996 n` or `997`, which Ghostty answers with the
  operating system's appearance.

`TRENVY_THEME=light` or `dark` keeps forcing palette C and always wins,
which remains the escape hatch when a terminal answers wrongly; `palette`
keeps palette C with the old light/dark detection, and unset or `auto` asks
the terminal. Whether to add a value that refuses the fallback, for testing,
is open.

The role mapping as built is in
[tui-design.md](tui-design.md#the-terminal-theme). It differs from the
proposal made here in one place: accent is blue (4) alone, with no
bright-blue (12) fallback, because the nudge makes one unnecessary.

### Background: Omarchy's own theme files

Under Omarchy the terminal (foot) already shows the current theme, so the
terminal approach covers it. Omarchy's theme files were studied first and
remain the reference for what a theme contains and for Omarchy's hook:

- Source: the `basecamp/omarchy` repository, default branch `quattro`, at
  commit `81145eb` (2026-10-05); latest release v4.0.4; tags v3.0.0 to v3.8.4
  also checked.
- **`colors.toml` is the theme's source of truth, not `btop.theme`.** The old
  belief that each theme ships a `btop.theme` swapped on a theme change held
  for 3.x; 4.x generates `btop.theme` from `colors.toml`. It lives at
  `~/.local/state/omarchy/current/theme/colors.toml` on 4.x and
  `~/.config/omarchy/current/theme/colors.toml` on 3.4 to 3.8. Since 4.0 every
  theme declares `mode` (light or dark). The keys are flat `key = "value"`
  lines: `foreground`, `background`, `bright_foreground`, `selection`,
  `accent`, the ANSI colour names and more.
- **A theme switch is not atomic.** Omarchy removes `current/theme` and then
  moves the new one into place, so anything reading the file must fall back
  quietly in between.
- **The hook.** Omarchy runs every script in
  `~/.config/omarchy/hooks/theme-set.d/` on a theme change (its
  `bin/omarchy-hook`), and restarts btop with `SIGUSR2`. A documented hook
  running `pkill -USR2 -x trenvy`, with trenvy re-querying on `SIGUSR2`,
  follows a switch even in a terminal that sends no change notification. The
  README documents the hook. This is Unix only; Windows has no `SIGUSR2`.

**Measured once, at that commit:** raw theme colours fail trenvy's bar — 94
role and surface pairs across the 22 themes fall below 4.5:1 — and nudging
each failing role in 1% steps fixed all 22, the largest shift being 44% (muted
on gruvbox). Omarchy's own `muted` is a border colour and fails as text in 19
of 22 themes, which is why muted is derived. To re-measure against a later
Omarchy: take every theme's `colors.toml`, map it to trenvy's roles as above,
compute WCAG contrast for each role on each surface it is drawn on, in both
modes, and move each failing role toward white or black in 1% steps until it
passes, recording the size of each move.

### Confirm with a terminal probe

A small probe run in real terminals settles what the approach assumes.
Ghostty on macOS has been probed, outside and inside tmux. foot on Omarchy
and Windows Terminal are still to do. Run it in each, with the system theme
set to light, then dark, then switched while the probe waits. Check:

- whether OSC 10, 11 and 4 (for all 16 colours) are answered, how fast, and
  in what format, and whether the answers match the theme on screen;
- whether DEC mode 2031 is supported, whether `CSI ? 996 n` is answered, and
  whether a notification actually arrives when the system theme changes;
- what XTVERSION reports, so a terminal that answers wrongly can be
  recognised;
- inside tmux: whether the queries reach the outer terminal or tmux answers
  them itself, and whether notifications pass through;
- whether lipgloss detects truecolor in each terminal: rounding to 256
  colours could undo the 4.5:1 nudge;
- what the nudged colours look like, and whether they still read as the
  theme.

Also unknown until then: how long Omarchy's remove-then-move gap lasts. (How
`SIGUSR2` and terminal replies fit bubbletea's input and signal handling is
settled: replies go through `termcolor.Filter` and the signal is watched only
while a program runs.)

The terminal theme's contrast pairs are built from the same palette values
as its theme (`style.TerminalContrastPairs`); the hand-kept pair table is
still a backlog item (Tests 1).

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
   end, and the variable's name above it drops to a bare `…`. The Outputs detail wraps the same kind of text under the name; do
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
2. **The list mark column sits flush against the right border** (`!`, `~`
   and `x` touch the `│`) on the Outputs and Profiles lists, and on the
   Variables list below 120 columns; from 120 the Variables preview column
   follows the mark.
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
   the theme's own values, or cross-check them. The terminal theme's pairs are
   already built from its palette (section 1); palette C's are not.
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
   validate-after-keyring order in the present tense. Another 54 comment
   lines (42 outside tests) cite sections of the redesign's design document (`spec §1`,
   `spec Risks`, `(spec, "Agreed")`) or the restyle's colour table, neither
   of which is in the repository; [tui-design.md](tui-design.md#older-references-in-code-comments)
   maps the old section numbers for now. Reword each to state the reason
   itself, or to point at the doc that holds it.
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

- Every work branch so far is merged into `main`, so its local worktree and
  branch can be removed (`git branch --merged main` lists them).
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
