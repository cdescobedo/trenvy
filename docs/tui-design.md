# Interface design

For contributors changing `trenvy ui`, `trenvy edit` or the setup flow. It
records the rules the interface follows today, why each exists, and which
test holds it. [architecture.md](architecture.md) covers the packages and the
commit; [design-history.md](design-history.md) covers how the interface came
to look like this.

The code is `internal/manage` (the interface), `internal/session` (the
UI-free state it stands on) and `internal/style` (the theme). Test names
below are in `internal/manage` unless another package is named.

## Older references in code comments

Many comments in `internal/manage`, `internal/session`, `internal/style` and
`internal/cli` cite `spec §N`, `spec Risks`, `spec diagnosis` or
`(spec, "Agreed")`. They refer to the redesign's original design document
(dated 2026-09-30), which is not in the repository. This document replaces
it; the old section numbers map as follows:

| comment says | read |
|---|---|
| §0 | [Visual hierarchy and theme](#visual-hierarchy-and-theme) |
| §1 | [Anatomy](#anatomy), [Footer rules](#footer-rules), [The fixed verbs](#the-fixed-verbs), [The line editor](#the-line-editor) |
| §2 | [Variables](#variables) |
| §3 | [The save review and typed confirmation](#the-save-review-and-typed-confirmation) |
| §4 | [Outputs](#outputs) |
| §5 | [Profiles](#profiles) |
| §6 | [Setup](#setup) |
| §7 | [`Update` purity and the `Deps` pattern](#update-purity-and-the-deps-pattern), and [architecture.md](architecture.md#internalmanage-and-internalsession) |
| "Risks" | the failed-reload rule under [The save review](#the-save-review-and-typed-confirmation) ("save-and-stay"), and [`TRENVY_THEME`](#trenvy_theme) (detection can guess wrong) |
| "Agreed", "diagnosis" | [design-history.md](design-history.md), section 8: what we chose for the redesign, and the problems it answered |

A comment citing "the spec's table" in `internal/style` means the
command-line token colours from the restyle (design history, section 7).

## Visual hierarchy and theme

The first interface drew everything but the status words in one colour and
one weight, and it read as washed out: the eye had nowhere to go. The rule
now is that hierarchy is built in grey first, by weight and contrast, and
colour only reinforces it.

### Levels

Every piece of text belongs to one level. Screens name the level, never a
colour.

| level | treatment | used for |
|---|---|---|
| title | bold, strongest foreground | the selected item's name, the project and profile in the header, override and shared values |
| text | normal foreground | ordinary values, list names |
| muted | muted foreground | labels, provenance (`from shared`), tags (`secret`), key descriptions, inactive tabs |
| accent | bold accent | anything pressable: key letters, the active tab, the focused pane's border, the active profile's `*`, the cursor `>` |
| danger | bold red | `MISSING`, problems, irreversible review lines |
| ok | green | `stored`, `fresh` |
| warn | amber | `STALE`, `MODIFIED` |
| staged | bold purple | `~` and `x` marks, the unsaved count |

Layout rules that follow from the levels:

- A key is drawn in accent and its description in muted, so the key leads.
- In a label and value pair the label is muted, so the value leads.
- Only the focused pane's border is accent; the other is the muted border.
- The header's problem and unsaved counts are the loudest things on screen.
  They are tinted pills, and they appear only when non-zero.
- The terminal's own background is never painted. Only surfaces set a
  background: the header bar, the selected row, the active tab and the pills.
- Semantic marks (ok, warn, danger, staged) are drawn beside a selection
  surface, never on it. This is a contrast rule, not taste: green on the
  light selection surface is 4.29:1, below the bar below.

### Palette C

trenvy ships its own truecolor palette, light and dark, as
`lipgloss.AdaptiveColor` pairs. The values live only in
`internal/style/theme.go`; no other file may hold a hex colour literal.
(The command-line tokens in `internal/style/style.go` are ANSI colour
numbers, which follow the terminal's theme; see below.) That is
what let a second theme source, the terminal's own colours (below), replace
the palette by producing the same roles, without touching a screen. Palette C
is now the fallback and what `TRENVY_THEME=palette`, `light` and `dark` use.

| role | light | dark |
|---|---|---|
| title | `#0f172a` | `#f1f5f9` |
| text | `#334155` | `#cbd5e1` |
| muted | `#556275` | `#94a3b8` |
| accent | `#1d4ed8` | `#93c5fd` |
| danger | `#b91c1c` | `#fca5a5` |
| ok | `#15803d` | `#86efac` |
| warn | `#92400e` | `#fcd34d` |
| staged | `#7e22ce` | `#d8b4fe` |
| surface: header | `#f1f5f9` | `#1e293b` |
| surface: selection, active tab | `#e8eef5` | `#253247` |
| pill: danger (fg on bg) | `#991b1b` on `#fee2e2` | `#fecaca` on `#7f1d1d` |
| pill: staged (fg on bg) | `#6b21a8` on `#f3e8ff` | `#e9d5ff` on `#581c87` |
| border: unfocused | `#cbd5e1` | `#334155` |

The command-line output (`status`, `show`, the lists) does not use palette
C. It keeps `style.Styler`'s 16-colour tokens, which follow the terminal's
own theme. Bringing the two together is on the roadmap.

### The terminal theme

By default the interface is not palette C. `cli.pickTheme` asks the terminal
for its background (OSC 11), foreground (OSC 10) and the 16 palette colours
(OSC 4), then DA1, which every terminal answers, so a terminal that does not
support the query is known at once rather than after a timeout.
`style.ThemeFromTerminal` builds the same roles from the answer, and the
whole theme comes from one source: if the reply lacks the background, the
foreground or palette colours 1 to 5, the whole interface is palette C.
`internal/termcolor` has no UI imports; the roles and the nudge live in
`internal/style/theme.go`, which stays the only file that builds colours.

| role | from the terminal |
|---|---|
| text | foreground |
| title | foreground, or bright white (palette 15) when that contrasts with the background at least as much; bold as in palette C |
| muted (also footer key descriptions, inactive tabs) | foreground mixed 40% toward the background |
| accent (also key letters, the focused border) | blue (palette 4) |
| danger, ok, warn, staged | red (1), green (2), yellow (3), magenta (5) |
| surface: header | background mixed 6% toward the foreground |
| surface: selection, active tab | background mixed 12% toward the foreground |
| border: unfocused | foreground mixed 70% toward the background |
| pills (fg on bg) | the background colour on the danger or staged colour |

Dark or light is judged from the background's luminance alone
(`termcolor.Reply.Dark`), never from `CSI ? 996 n` or the `997` notification:
Ghostty answers those with the operating system's appearance, which is not
what the terminal shows. The renderer is told the same answer
(`SetHasDarkBackground`), so lipgloss makes no background query while the
interface runs. (Bubbletea's package init makes one background query at
process start, before trenvy's own; that is the library's behaviour, not
trenvy's, and it happens under every `TRENVY_THEME` value.)

**The nudge rule.** A mapped colour that does not reach 4.5:1 against every
surface it is drawn on is mixed toward white (dark background) or black
(light) in 1% steps until it does, so the result keeps the theme's hue.
Text roles (title, text, muted, accent) are checked against the background,
the header and the selection; the marks (danger, ok, warn, staged), as in
palette C, against the background only, and a pill's text (the background
colour) against its fill. Raw theme colours do
fail: rose-pine's yellow is 2.05:1 and catppuccin-latte's is 2.31:1.

**The shrinking surface shift.** The header and selection surfaces sit toward
the foreground (6% and 12%). Near a mid-grey background that can leave a
surface where neither white nor black reaches 4.5:1 on all three surfaces, so
the shift shrinks (12%, 10% and so on down to 0) until every role fits. At 0
the surfaces equal the background, and the better of white and black always
reaches at least 4.58:1 against a single colour, so a complete reply always
builds a theme.

**Tests.** `TestTerminalThemesPassContrast` (`internal/style`) builds the
theme for all 22 Omarchy themes (a pinned copy in `internal/style/testdata`)
plus synthetic black, white and grey backgrounds and a run of near mid-grey
ones with a white foreground, and checks every pair
`style.TerminalContrastPairs` returns, at least 17 of them, at 4.5:1; it also
checks the two halves of each swatch agree, since a terminal theme is not
adaptive; Ghostty's recorded answer on Tokyo Night Moon is checked the same
way. `TestThemeFromTerminalNeedsACompleteReply` pins the all-or-nothing
fallback, `TestThemeFromTerminalUsesTheThemesColours` that a readable
colour is used as the theme gave it, and `TestThemeFromTerminalMapsRolesOnMoon`
the mapping from the palette on that recording.

**Following a switch.** While the interface runs, input passes through
`termcolor.Filter`, which removes the terminal's replies and notifications
and hands every other byte on unchanged and in order. A re-query is a `Deps`
function (`Requery`) run in a `tea.Cmd`, and a changed theme arrives as a
`manage.ThemeMsg`, so `Update` stays pure. A re-query follows the terminal's
dark/light notification (the interface enables DEC mode 2031 and turns it off
on exit), `SIGUSR2`, a focus event, and a 5 second timer that is off when the
terminal announces changes (it answered the DECRQM for 2031) and over SSH.
Setup and the interactive import review re-query on the notification and
`SIGUSR2` only. Windows asks the terminal nothing and uses palette C until the
Windows Terminal probe ([roadmap](roadmap.md)) confirms how replies arrive
there: cancelreader cannot always cancel a console read, so a round could
wait for the next key and eat it. A switch changes the theme and nothing
else: staged edits, the cursor and any open overlay stay. Nothing is asked
once a program has exited (`TestLiveThemeAsksNothingAfterExit`,
`TestWatchSignalStopIsAFence`), but a query written in the last milliseconds
before quitting can still have its reply echoed at the shell prompt (rare).

The filter's contract, which the tests pin:

- No keystroke is lost or reordered; only complete reply sequences are
  removed.
- A reply cut across two reads is joined, not leaked as keys
  (`TestFilterJoinsAReplySplitAcrossReads`,
  `TestFilterJoinsARoundCutAnywhereInTwoArrivals`).
- A key right after a notification still arrives
  (`TestFilterPassesAKeyAfterANotification`).
- A read that ends inside a sequence waits up to 25 ms for the rest, so a
  lone `esc` reaches the interface up to 25 ms late
  (`TestFilterPassesTheEscKeyAtOnce`). A cut that never completes is
  released as keys
  (`TestFilterReleasesAnUnfinishedSequence`).
- Sequences that merely resemble ours pass through
  (`TestFilterPassesSequencesItDoesNotOwn`). `ESC O` is SS3 only when a
  final byte follows; alt+O right before a reply does not take the reply's
  `ESC` (`TestFilterTakesSS3OnlyWithAFinalByte`).

One consequence no test pins: `esc` followed by another key within 25 ms
arrives in one read and reads as `alt+` that key, as in most terminal
programs.

The startup query is one round with a 100 ms ceiling (`queryTimeout`,
`internal/cli/theme.go`). It is skipped when stdin or stdout is not a
terminal, when the colour profile is Ascii (`NO_COLOR`), and on Windows.
`TestQueryGivesUpWithoutStealingInput` (`internal/termcolor`) pins that a
terminal answering nothing, not even DA1, ends the round at its timeout and
leaves no reader behind to swallow the user's first key. `termcolor.Query`
returns a `Round`: the colours that came in time, whether DA1 ended it
(`Done`), and the bytes it read that are not replies (`Keys`). A round that
ended with DA1 and builds a theme is used and followed; one that ended with
DA1 and builds none is palette C with nothing following it, drawn on the
side its background is on when it has one (lipgloss, told the side, then
never asks the terminal itself). A round the timeout cut short still gets the input filter,
on palette C: the rest of the answer arrives through it, is removed, and
switches the interface to the terminal's colours
(`TestQueryHandsALateRoundToTheFilter`,
`TestPickThemeFiltersARoundThatTimedOut`,
`TestLiveThemeSwitchesWhenALateRoundCompletes`). The round's `Keys` are
handed to the filter first, so keys typed while it ran reach the interface
in order (`TestQueryKeepsKeysTypedDuringTheRound`); when no filter would
otherwise run, a filter that does nothing else carries them
(`TestPickThemeTakesNoForAnAnswer`).
`TestThemeMsgKeepsStateAndOverlays` (`internal/manage`) pins that a switch
keeps staged edits, the cursor and the open overlay.

### The contrast rule and its test

Every text role, on every surface it is drawn on, is at least 4.5:1 (WCAG AA
for normal text) in both light and dark, for palette C and for a theme built
from the terminal's colours alike. `style.ContrastPairs` lists the
foreground and background pairs palette C draws, terminal background
included, and `TestThemeContrast` (`internal/style`) computes WCAG relative
luminance for each pair in both modes; `TestTerminalThemesPassContrast`
(above) does the same for the terminal theme. It also refuses a list shorter than
17 pairs, so pairs cannot quietly disappear.

When the first draft of the palette was checked this way, three roles
failed (light ok, light muted, dark muted) and the selection surface was
retinted from blue to neutral so accent and danger clear it. Any palette
change, including the terminal theme's rules, has to keep these tests
passing.

Known gap: the pair tables are maintained by hand beside `newTheme`; both
read the same `palette` values, but a role recoloured in `newTheme` without
updating the pairs would still pass. It is in the [roadmap](roadmap.md) backlog.

### `TRENVY_THEME`

| value | colours | queries the terminal |
|---|---|---|
| unset or `auto` | the terminal's, else palette C | trenvy's own round, when stdin and stdout are terminals |
| `palette` | palette C, light or dark by lipgloss's detection | none of trenvy's own colour queries |
| `light` or `dark` | palette C, forced | none of trenvy's own colour queries |
| anything else | an error naming `auto`, `palette`, `light` and `dark` | none |

Detection by lipgloss can guess wrong, and with a fixed palette a wrong guess
makes text hard to read; `light` and `dark` are the escape hatch, and
`palette` keeps the old behaviour without forcing a side. Anything else is an
error rather than a guess. `cli.pickTheme` reads the variable for
`trenvy ui`, `trenvy edit`, setup and interactive `trenvy import`;
`style.ThemeFor` builds palette C for the other values. A stdin or stdout
that is not a terminal, and a terminal that answers too little, also give
palette C.

Tests: `TestThemeForAcceptsLightDarkOrEmpty` and
`TestThemeForForcesTheBackground` (`internal/style`), `TestPickThemeValues`
and `TestUiRejectsABadThemeName` (`internal/cli`).

### Why the renderer is injected

The theme is built over a `*lipgloss.Renderer` passed in, never lipgloss's
global one. The global renderer detects the process's stdout, and under
`go test` that resolves to no colour, so styles built on it would work in
production and be impossible to test. A zero `Theme` renders plain text with
no escapes; word-level tests use it (`TestZeroThemeRendersPlain`,
`internal/style`), and colour is asserted only where a test builds a
truecolor renderer on purpose (`TestTrueColorThemeColoursItsRoles`).

## Anatomy

```
 myapp · dev (default)                                ! 2 problems    3 unsaved  w review
 [ Variables ]  Outputs   Profiles
╭────────────────────────╮╭────────────────────────────────────────────────────────────╮
│> API_TOKEN    secret ! ││  API_TOKEN                               secret · required │
│  APP_MODE            ~ ││  ...the selected row's detail...                           │
╰────────────────────────╯╰────────────────────────────────────────────────────────────╯
 APP_MODE override for dev staged -- u to undo
 enter open   d delete   a add   . more   / filter   ? keys
```

**Header.** Left: the project, then the profile in effect and where that
choice came from (`default`, `active`, `TRENVY_PROFILE` or `--profile`).
Right: the problem count and, once anything is staged, the unsaved count
with `w review`. When the line is too narrow the left side gives way first:
the project is cut, then the profile, then the source is dropped. The right
side is what needs acting on, so it is kept. Tests:
`TestHeaderNamesProjectProfileAndSource`,
`TestHeaderKeepsItsCountsWhenTheProjectIsLong`,
`TestHeaderKeepsItsCountsWhenTheyFillTheLine`.

**Tabs.** Variables, Outputs, Profiles. The active one is bracketed outside
its style, so it reads without colour.

**Body.** A list pane and a detail pane for the list's selection.

- The Variables list is the longest name plus its marks, clamped to 24–36
  columns. Outputs clamps to 32–52 and Profiles to 28–48, because their rows
  carry a tag and a state or a note; both are also held to half the screen.
  The detail pane takes the rest.
- Below 80 columns the panes stack: the list fills the screen, `enter` or
  `→` opens the detail full width, and `←` returns.
  `TestNarrowTerminalStacksThePanes`.
- From 120 columns the Variables list gains a muted column with the active
  profile's value. `TestWideTerminalPreviewsTheActiveValue`.
- The list cursor is `>` while the keys go to the list and a muted `·` while
  they go to the detail, so focus reads without colour.
  `TestCursorAndPaneFocusShowWithoutColour`.

**Message line.** One line saying what just happened and, where there is
one, how to reverse it (`-- u to undo`). The next keypress clears it. Errors
are drawn in danger. A long message wraps to at most two lines, and only past
that is the last line cut with `…`; the body gives up a line to make room.
Wrapping matters because the end of a message is usually the way out.
`TestLongMessagesKeepTheirEnd`, `TestRefusalsFitTwoMessageLinesAt60`.

**Footer.** The keys that work right now; see the next section.

**Overlays.** Anything that needs more than a key — an edit field, the `.`
menu, the save review, a confirmation, `?` — opens as one panel over the
body, at most 76 columns wide. Only the top overlay gets keys. `esc` closes
the top overlay and does nothing else; the model handles it before the
overlay sees it.

## Footer rules

- **At most six keys**, `? keys` included and always last (`footer`,
  `internal/manage/keys.go`).
- **Only keys that act on the current selection.** A key that would do
  nothing is not listed. Pressing an unlisted key gives at most a one-line
  reason. `TestEveryFooterKeyDoesSomething` presses every key the footer
  lists, in every selection kind, and requires each to change the screen or
  return a command; `TestConditionalHints`,
  `TestProfilesFooterListsOnlyKeysThatAct` and
  `TestFooterOnARemovedProfilesRowOffersNoEdit` check that keys appear and
  disappear with the selection.
- **Ordered by importance.** Each screen lists its primary action first, so
  the six-key cap drops the least important; `?` lists everything.
- **Whole hints are dropped when narrow, never cut.** `footerLine` removes
  hints from the right, keeping the first and the last, until the line
  fits. Only if those two alone are too wide is the line cut.
  `TestNarrowFootersDropWholeHints`.
- **Overlays and setup have their own footers.** An overlay lists its own
  keys, usually ending with `esc` (the live filter ends with `ctrl+u`
  clear, since `enter` and `esc` both keep it). Setup lists the step's `enter` first and `esc`
  last, so a 40-column footer keeps the way forward and the way back.
  `TestSetupFooterListsOnlyKeysThatAct`,
  `TestSetupReviewFooterKeepsEnterAndEscAtFortyColumns`.

## The fixed verbs

A key means the same thing on every screen where it does anything. This is
the list `?` shows (`verbs`, `internal/manage/keys.go`):

| key | meaning |
|---|---|
| `↑↓` `j k` | move |
| `←→` `h l` | switch pane |
| `1 2 3`, `tab` | switch screen (`shift+tab` goes back) |
| `enter` | the primary action on the selection |
| `a` | add |
| `d` | mark deleted, or remove a value (again to unmark) |
| `u` | undo the staged change on the selection |
| `v` | reveal or hide the value under the cursor |
| `/` | filter by name |
| `!` | show only rows with problems |
| `.` | more actions for the selection |
| `w` | review and save |
| `q` | quit (asks first if anything is unsaved) |
| `ctrl+c` | quit now, writing nothing |
| `esc` | close the panel on top |
| `?` | this list |

Notes on the edges:

- `/` and `!` act on Variables only; `v` acts only on a stored secret's row;
  `space` toggles a membership box on an output's detail. The footer shows
  each only where it acts.
- `p` (switch project) was a key in the first interface. It is now *Switch
  project* in the `.` menu, so a key does not change meaning between screens.
- `q` with unsaved changes opens "N unsaved changes" with `w` review, `q`
  discard and quit, `esc` cancel. `TestQWithUnsavedAsksAndQAgainDiscards`.
- `ctrl+c` quits at once from anywhere, even mid-save. Outside a save it
  writes nothing; during one, the footer warns that quitting may leave the
  save partly applied. `TestCtrlCQuitsFromAnywhere`,
  `TestTheSavingHintSaysQuittingMayLeaveItPartlyApplied`.
- The `?` panel sizes its key column from the widest key, wraps
  descriptions and scrolls on a short terminal with `↑ more` / `↓ more`.
  `TestHelpKeyColumnNeverRunsIntoItsDescription`,
  `TestHelpScrollsWhenTallerThanTheTerminal`,
  `TestHelpAtTheMinimumSizeAlwaysShowsAVerb`.

The `.` menu is generated per screen. A disabled entry stays in the list,
marked `(unavailable)`, and `enter` on it says why, so an absent action is
never a silent one. `TestRenameIsShownDisabledWithItsReason`.

## The line editor

`internal/manage/lineedit.go`, hand-rolled because the redesign took no new
dependency (the `bubbles` text input was considered and declined).

- **It opens with the current value selected.** The first printable key
  replaces it; `←` or `→` keeps it for editing. The old editor appended to a
  prefilled value with no way to clear it, which is how `development` became
  `developmentproduction-ish`. `TestFieldTypingReplacesTheSelectedValue`,
  `TestFieldArrowKeepsTheValueForEditing`.
- **Editing keys:** `←` `→`; `ctrl+a`/`home` and `ctrl+e`/`end`; backspace
  and delete; `ctrl+u` clears to the start, `ctrl+k` clears to the end,
  `ctrl+w` deletes the word before the cursor. On a selected value, any of
  the clearing keys clears it all. `TestFieldEditingKeys` covers each key,
  but on a selected value only backspace; the others are in the
  [roadmap](roadmap.md) backlog (Tests 6).
- **A paste is one insertion, and line breaks are dropped:** every value
  edited here is one line, and a pasted newline would split a dotenv entry.
  `TestFieldPasteIsOneInsertionWithoutNewlines`.
- **Masked mode** draws `•` per rune and never the rune.
  `TestMaskedFieldNeverDrawsItsValue`.
- **Long values scroll** around the cursor, each cut side marked `…`. The
  selected form is bracketed as well as highlighted, and the editing cursor
  is a `▏`, so both states read without colour.
  `TestFieldViewFitsAndFollowsTheCursor`, `TestFieldStatesShowWithoutColour`.
- `enter` and `esc` belong to whoever owns the field.

## The screens

### Variables

The list has one row per variable, sorted by name: the name, a muted
`secret` tag, and one mark: `x` marked for deletion, `~` something staged,
`!` a problem. Marks are characters before they are colour.
`TestListShowsEveryVariableWithMarks`.

The detail shows the variable's value under **every** profile, and why:

- A plain variable has a `shared` row first, then one row per declared
  profile. An inherited value reads `from shared`; an override reads
  `override` and is drawn at title level. `*` marks the profile in effect.
  `TestDetailShowsValuesAndProvenance`.
- A secret has no shared row (the manifest refuses a secret with a value).
  Each profile reads `stored`, or `MISSING !`, with the value masked.
  `TestSecretDetailMasksAndFlagsMissing`.
- A missing optional value reads `unset`, muted, and is not a problem.
  `TestOptionalVariableReadsOptional`.
- The variable's problems are listed at the foot of its detail, each with
  an in-app fix ("enter on dev to store it"), never a CLI command. A missing
  value that a staged value will fill is not listed.
  `TestAStagedValueAnswersItsMissingProblem`.
- `written to` names the outputs that select it. A variable reaching
  several outputs is a plain fact, not a problem.

Editing:

- `enter` on a row edits that row, and the panel title names the scope:
  *Shared value -- used by dev, staging* on the shared row, *Override for
  prod only* on a profile's. Editing a shared value can never be mistaken
  for creating an override. `TestEnterOnSharedEditsTheSharedValueAndNamesWhoInherits`,
  `TestEnterOnAProfileRowStagesAnOverride`.
- On a secret, `enter` stores or rotates the value, masked, and refuses an
  empty one. `TestSecretEntryIsMaskedAndRefusesEmpty`.
- `v` reveals one secret's value through `Deps.Reveal` (one keyring read)
  and `v` again hides it. A revealed value is hidden again when the cursor
  leaves the variable, when the filter changes, and on every reload.
  `TestRevealFetchesOneCellAndHidesOnLeave`, `TestRevealDoesNotSurviveAReload`,
  `TestStaleRevealIsDroppedAfterAReload`.
- `d` on an override drops it, so the profile falls back to shared. `d` on a
  variable's only value is refused with what to do instead. `d` in the list
  marks the variable deleted, and again restores it.
  `TestDOnAnOverrideRevertsToShared`, `TestDOnTheOnlyValueRefusesWithAReason`,
  `TestDInTheListTogglesDeletionAndUUndoes`.
- `a` adds a variable: name, then `p` plain or `s` secret, then its first
  value (the shared value, or the active profile's secret).
- `.` offers *Make secret*, *Mark optional* / *Mark required*, *Rename*
  (shown disabled: renaming would have to move keyring items and every glob
  naming the variable), *Project info* and *Switch project*.

*Make secret* is one way inside the interface. A variable with a shared
value confirms first, saying it becomes one keyring item per profile that
can drift apart once any is edited; one with overrides only stages at once.
The way back is `trenvy secret demote`, on the command line, because it has
to read secrets and write them into a plaintext file — the one thing the
interface never does. `TestMakeSecretConfirmsWhenASharedValueExists`,
`TestMakeSecretIsDisabledOnASecret`.

### Outputs

The list is every output with a `default` tag, its state word, and the same
marks. `TestOutputsListShowsDefaultAndState`.

The state is the word `trenvy status` prints, with a muted gloss after it.
Using `status`'s own words means a state cannot mean one thing in the
command and another on screen.

| state | gloss |
|---|---|
| `fresh` | up to date with the manifest |
| `not rendered` | nothing written in this worktree yet |
| `STALE` | out of date -- render to update it |
| `MODIFIED` | edited by hand since the last render |
| `FOREIGN` | a file trenvy did not write is at this path |
| `unverified` | not checked, holds secrets -- . then Check now |
| `unknown` | can't tell -- a secret is missing or the file can't be read |

`unverified` is the interface's word for what `trenvy status --quick`
prints as `unknown (--quick)`. Opening the interface does not read secrets,
so it cannot tell a matching file from one whose secret changed; flagging
that as a problem would be a permanent alarm nobody could clear.
`TestOutputStateWordsAndGlosses`.

The detail shows format, path, state, what a variable added later does, and
a checkbox per variable. Membership is edited in place: `space` toggles a
box and `~` marks what changed. The sentence reads *new variables included
automatically* or *new variables not included -- only the checked ones*,
and the `.` item flips which future applies without moving any box.
`TestMembershipToggleStagesGlobsAndUndoes`, `TestSwitchingTheFutureModeKeepsTheBoxes`.

- `a` adds an output: name, then `d` dotenv or `j` json, then path. Name and
  path problems show in the panel as they are typed. A dotenv path starts as
  `.env`, or `<name>.env` when `.env` is taken.
  `TestAddOutputShowsProblemsWhileTyping`, `TestAddDotenvPrefillsEnvOnlyWhenItIsFree`.
- A path another output renders to is refused, as `trenvy output add`
  refuses it, because one would overwrite the other's file. A path git does
  not ignore is warned about; a second `enter` takes it anyway. Both checks
  apply to editing a path. `TestAddOutputRefusesAPathAnotherOutputRendersTo`,
  `TestPathEditWarnsWhenGitDoesNotIgnoreThePath`.
- `.` offers *Make default*, *Rename*, the new-variables item, then three
  actions that run **immediately**, not staged, because they touch this
  worktree's file rather than the manifest: *Render now* (the `render`
  command, guards included), *Remove rendered file* (`clean`, asks first)
  and *Check now (reads secrets)*. All three are unavailable while that
  output has staged changes, since they act on what is on disk.
  `TestActionsAreDisabledWhileTheOutputHasStagedChanges`,
  `TestRenderRefusalIsShownAndNothingIsWritten`,
  `TestCheckNowReplacesTheStateUntilReload`.

### Profiles

The list is every profile in manifest order, then any added this session,
with two tags — `default` (the manifest's) and `active` (this machine's
selection) — and, after the tags, `N missing` when required values have no
value under it. The count is never hidden by a tag or a staged mark.
`TestProfilesListShowsTagsMissingAndMarks`, `TestProfilesNoteShowsMissingBesideTags`.

The detail counts the profile's overrides, its secrets stored out of the
total (presence from `Has`, or a value staged this session), and names
every missing variable. `enter` on a missing name opens it in Variables, on
that profile's row. `TestProfileDetailCountsOverridesSecretsAndMissing`,
`TestEnterOnAMissingNameJumpsToTheCell`.

- `a` adds a profile by name. It gets its values in Variables after saving,
  because Variables lists the profiles on disk.
- `d` marks a profile for removal. It is refused for the last profile, the
  default, the profile this session is on, and when the removal would leave
  a required plain variable with no value. Values already staged on its
  cells are dropped, with the count in the message.
  `TestRemoveProfileRefusals`, `TestRemoveMessageCountsDroppedValues`.
- `.` offers *Use on this machine* (the state file's selection, as
  `trenvy profile use`), *Make default*, and *Rename*, always disabled: a
  profile's name is part of every keyring item stored under it.
  `TestProfileMenuUseAndDefault`, `TestUseOnThisMachineSaysWhenOutranked`.

### Setup

`manage.Setup` is a second, smaller model for `trenvy ui` or `trenvy edit`
outside a registered project, with four steps shown as a bracketed row:
*Project* (id and root), *Seed* (a detected sample or *Start empty*),
*Review* (one row per variable) and *Create* (a summary). Interactive
`trenvy import` runs only *Review* and *Import*. Nothing is written before
the summary's `enter`, and cancelling at any step writes nothing.
`TestSetupHappyPathKeepsTheReviewChoices`, `TestSetupCancelsFromAnyStep`.

- The root is checked with the registry's own containment rule, so setup
  never writes a project that would not then be found from the directory it
  ran in. `TestSetupRootMustBeAbsoluteFreeAndContainThisDirectory`.
- In the review a value guessed to be secret is never shown and never
  stored; unticking it leaves the row plain and empty, for `e` to fill.
  `TestSetupNeverShowsASecretsSampleValue`,
  `TestUntickingAGuessedSecretDropsItsSampleValue`.
- The import summary lists conflicts with the manifest; without `--force`
  it offers no `enter` and refuses one. `TestImportSummaryRefusesConflictsWithoutForce`.
- The summary scrolls with `j`/`k` when taller than the terminal.
  `TestSetupSummaryScrollsToEveryLine`.
- After creating, the interface opens on Variables with the problems-only
  filter (`!`) on when any value is missing — the filter then shows every row
  with a problem — and unfiltered otherwise, with a message naming how many
  secrets to store. `TestAfterSetupFiltersToWhatIsMissing`.

## The save review and typed confirmation

Nothing reaches disk until `w` opens the review and `enter` saves. The
review is the confirmation step.

- **Grouped by what it touches:** variables, then outputs, then profiles,
  each by name. `TestReviewGroupsVariablesOutputsThenProfiles`.
- **Before -> after** on every line that changes a value. Lines that are not
  a value say what happens (`store new value (keyring)`, `its values leave
  the manifest`). A plain value is shown in full; a secret's value never is.
  `TestWOpensAGroupedReviewWithBeforeAndAfter`, `TestReviewNeverShowsASecretValue`.
- **Irreversible lines** — deleting a variable, making one secret, removing
  a profile or an output — are drawn in danger, followed by `! cannot be
  undone after saving`. Keyring deletions name their count and the items
  `Has` found. `TestReviewMarksIrreversibleLines`,
  `TestReviewCountsOnlyStoredItemsForARemovedProfile`.
- **`u` drops the line under the cursor, and its dependents with it**, and
  the message says how many went: dropping an added variable drops its
  values too. Dropping the last change closes the review.
  `TestUInReviewSaysWhatDependentsWentWithIt`.
- **Typed confirmation.** When the batch removes a profile, `enter` first
  asks for each removed profile's name, one at a time. A wrong name keeps
  the panel open; `esc` returns to the review with nothing written. Profile
  removal deletes every keyring item stored under it, which is why it alone
  asks for more than a key. `TestRemovingAProfileNeedsItsNameTyped`,
  `TestTwoRemovalsAskForEachNameInTurn`, `TestEscFromTheTypedConfirmationWritesNothing`.

**Saving.** `Deps.Commit` runs inside a command; the review shows `saving…`
and ignores every key but `ctrl+c`. On success the review closes, the staged
set empties, `Deps.Reload` rebuilds the snapshot from disk, and the message
says `saved N changes`. **The session stays open** — saving is not quitting.
Input stays blocked from the commit until its reload lands, so nothing is
staged against the pre-commit snapshot. `TestEnterSavesStaysAndReloads`,
`TestKeysAreIgnoredWhileSaving`, `TestKeysAreIgnoredBetweenCommitAndReload`.

Files a save leaves on disk (a removed output's, or one whose path moved)
are listed in a panel of their own, where each path wraps in full.
`TestFilesLeftInPlaceAreListedInFull`.

**Failure.** If a save fails partway, the review stays open with how far it
got, the snapshot reloads, and **the whole batch stays staged**: `enter`
retries all of it. That is safe because the manifest is saved last and
atomically, so after any failure the manifest on disk is the pre-commit one,
and every phase before it can be repeated without harm. The phases and why
each repeats safely are in
[architecture.md, The staged commit](architecture.md#the-staged-commit). A
batch the manifest refuses outright wrote nothing, and the review says so
and points at the change to drop instead of offering a retry that cannot
succeed. `TestFailedCommitKeepsEverythingAndRetries`,
`TestARefusedBatchDoesNotOfferARetry`, and in `internal/cli` the
`TestCommitRetryAfter…MatchesOneCleanCommit` family.

**A failed reload** leaves the screen possibly out of step with disk, so
staging and saving are refused until *Reload from disk* (in the `.` menu)
succeeds. Showing stale state as truth is the failure save-and-stay would
otherwise invite. `TestFailedReloadBlocksStagingUntilReloaded`,
`TestStaleReviewDoesNotOfferARetryItWillRefuse`.

## Staging namespaces

Every edit is a `session.Change` in `session.Staged`. Three rules shape it.

**It is a set, not a log.** A change deciding the same thing as an earlier
one — the same cell's value, the same variable's optional flag, the default
output — replaces it, so the review lists what will happen rather than
every keypress. `TestStagedAddReplacesAChangeToTheSameCell`,
`TestStagedOneDefaultAtATime`.

**Variables, outputs and profiles are separate namespaces.** All three key
by `Change.Name`, and an output or a profile may share a variable's name, so
every rule keyed by name looks only at its own kinds. Staging, deleting or
undoing an output called `local` never touches a variable called `local`.
`TestStagedKeepsVariableAndOutputChangesApart` and
`TestProfileKindsAreTheirOwnNamespace` (both `internal/session`), and
`TestOutputAndVariableWithTheSameNameStageIndependently`.

**A delete and other changes on the same thing never both stand.** Deleting
a variable drops every other change staged on it, and one added this
session then stages nothing at all. Any other change on a variable marked
for deletion un-marks it. Outputs follow the same two rules, and removing a
profile also drops every value staged on its cells. Without this, a
promotion staged before a delete would store keyring items that the delete
then left unnamed. `TestStagedDeleteDropsEveryOtherChangeOnTheVariable`,
`TestStagedEditCancelsAPendingDelete`, `TestRemovingAProfileDropsItsStagedCells`
(`internal/session`).

Each change keys by its row's identity as it was when the session started,
or as it was added. A staged output rename changes what is drawn, never the
identity, which is why the commit applies renames in a pass of their own,
after every other output edit.

## Readability invariants

These hold on every screen, overlay and setup step.

| invariant | what it means | held by |
|---|---|---|
| Readable without colour | Every state is a word or a character first: `>` cursor, bracketed tab and step, `~ x !` marks, `[ ]` boxes, bracketed selected field. Colour is additive; `NO_COLOR` or a pipe loses nothing. | `TestCursorAndPaneFocusShowWithoutColour`, `TestMenuCursorShowsWithoutColour`, `TestSwitchProjectCursorShowsWithoutColour`, `TestFieldStatesShowWithoutColour`; `TestAsciiThemeEmitsNoEscapes` and `TestAsciiProfileKeepsEveryWord` (`internal/style`) |
| No line wider than the terminal | Measured in display cells. The layout tests run each screen at widths from 40 to 160, including the 79/80 and 119/120 boundaries. | `TestLayoutNeverExceedsTheTerminal`, `TestOutputsLayoutNeverExceedsTheTerminal`, `TestProfilesLayoutNeverExceedsTheTerminal`, `TestSetupLayoutNeverExceedsTheTerminal`, `TestDetailKeepsAGapBeforeTheRightBorder` |
| Values are never cut; names may be | A value, path or message end wraps under its own column with a hanging indent. A name (variable, output, profile, project in the header) may be cut with `…` when its column is full, because list columns are bounded and the detail shows it whole. | `TestLongValuesWrapRatherThanCut`, `TestOutputValuesWrapRatherThanCut`, `TestReviewWrapsLongValues`, `TestLongProfileNamesKeepLabelValueAndMark`, `TestProfilesNoteIsNeverCut`, `TestImportHeaderWrapsItsPath` |
| Text wraps at spaces | `wrap` breaks at spaces where it can and inside a word only when it must, and never after a hyphen, so `--force` stays whole. Runs of spaces inside a value are kept. | `TestSetupNeverSplitsADashAcrossLines`, `TestListNotesWrapInANarrowListPane`, `TestSetupWrapsNotesAndPathsAtFortyColumns` |
| Minimum size 40×10 | Below it the interface draws one sentence naming the minimum and the current size instead of garbled boxes, and never panics. | `TestTinyTerminalSaysSoInsteadOfDrawing`, `TestImportViewFitsTheMinimumTerminalWithALongPath`, `TestSetupNewWithNoSamplesSurvivesNarrowWidths` |

Control characters in a value are drawn in Go's escape notation (`\x1b`,
`\t`), so a value can never send the terminal a command; the value itself is
untouched. `TestControlCharactersInValuesAreDrawnVisibly`.

Known exceptions, all in the [roadmap](roadmap.md) backlog:

- The too-small sentence is itself about 53 cells wide, so on a terminal
  narrower than that it overflows the line it is warning about.
- At 40 columns the Variables detail header cuts its meta
  (`x marked for deletion · secret · required`) at the pane edge, and the
  variable's name above it drops to a bare `…`; the Outputs detail wraps
  the same text under the name instead.
- An import path that cannot fit the setup header's two lines is cut at its
  front with `…`, keeping the file name. The path is the user's own
  argument, already printed by the shell.

## `Update` purity and the `Deps` pattern

`Model.Update` and `Setup.Update` do no I/O: no file, no keyring, no git, no
registry. They move state and return commands.

Everything that touches the outside world is a `manage.Deps` function, run
inside a `tea.Cmd`, whose result comes back as a message:

| dep | does |
|---|---|
| `Reveal` | one keyring `Get`, for `v` |
| `Commit` | `commitStaged`, for the review's `enter` |
| `Reload` | rebuilds the snapshot from disk, after a save or an action |
| `Ignored` | asks git whether a typed output path is ignored |
| `Render`, `Clean`, `Check` | the `render` and `clean` commands and a full freshness check, for one output |

Setup goes further: `manage.NewSetup` takes a `SetupInput` holding every
fact it shows or checks (prefills, registered ids and roots, parsed samples
with their output previews), and hands back a `SetupResult`. It reads
nothing; `internal/cli/setup.go` gathers the facts and does the writing.

Why:

- **Tests drive the real model with keys.** A test builds a model with a
  fake `Deps`, sends key messages, runs the returned commands itself and
  feeds their messages back, then asserts on words from `View()`. No
  terminal, keyring or disk is involved (`fakeDeps`,
  `internal/manage/helpers_test.go`). `cli.Options.RunManage` and `RunSetup`
  are swappable the same way, so command tests never open a terminal.
- **Presence stays cheap.** The snapshot is built with `Has` only, so opening
  the interface never raises a keyring prompt. A value is read only by
  `Reveal`, *Render now*, *Check now*, and `trenvy secret demote` outside the
  interface. `TestUiOpensWithHasAndRevealsWithOneGet` (`internal/cli`).
- **A save cannot race the screen.** Because the commit and reload are
  messages, the model can hold input between them and drop a reveal that
  was requested before a reload (each carries the reload generation it was
  asked in).

Nothing checks the package's imports automatically: purity is held by
review and by the fact that every test of the model runs with fake `Deps`.
What is true today: `Update` is pure in both models; `run.go` is the one file
that touches the real terminal, running the bubbletea program; and from the
rest of trenvy the package uses only pure helpers — `manifest`'s validators
and types, `config.UnderOrEqual` (path containment), `sample.Entry`, plus
`session` and `style`.
