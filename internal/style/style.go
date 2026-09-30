package style

import "github.com/charmbracelet/lipgloss"

// Colours are base ANSI, not 256-colour or hex: they follow the user's own
// terminal theme rather than fighting it, and degrade intact on a 16-colour
// terminal. The mapping is the spec's, and the reasoning belongs with it.
const (
	colRed    = lipgloss.Color("1")
	colGreen  = lipgloss.Color("2")
	colYellow = lipgloss.Color("3")
	colCyan   = lipgloss.Color("6")
	colDim    = lipgloss.Color("8")
	colStaged = lipgloss.Color("5")
)

// Styler colours trenvy's vocabulary.
//
// The zero value is deliberately usable and renders everything plain: a zero
// lipgloss.Style has no renderer and returns its input unchanged (verified
// 2026-09-30). That is what lets every existing test keep building a Params
// or an Options without a Styler and keep asserting on words -- the 126
// strings.Contains assertions in internal/ui survive this change because
// their Styler is the zero one.
//
// The renderer is injected rather than taken from lipgloss's global, because
// the global resolves from os.Stdout: under `go test` that is Ascii, so a
// package-level style would be correct in production and untestable in CI.
type Styler struct {
	r                                   *lipgloss.Renderer
	missing, optional, stored, override lipgloss.Style
	secretMark                          lipgloss.Style
	fresh, stale, modified, foreign     lipgloss.Style
	absent, unknown                     lipgloss.Style
	staged, cursor, activeTab           lipgloss.Style
}

// New builds a Styler that renders through r.
func New(r *lipgloss.Renderer) Styler {
	f := func(c lipgloss.TerminalColor) lipgloss.Style { return r.NewStyle().Foreground(c) }
	return Styler{
		r:          r,
		missing:    f(colRed),
		optional:   f(colDim),
		stored:     f(colGreen),
		override:   f(colCyan),
		secretMark: f(colYellow),
		fresh:      f(colGreen),
		stale:      f(colYellow),
		modified:   f(colYellow),
		foreign:    f(colRed),
		absent:     f(colDim),
		unknown:    f(colDim),
		staged:     f(colStaged),
		cursor:     r.NewStyle().Reverse(true),
		activeTab:  r.NewStyle().Bold(true).Underline(true),
	}
}

func (s Styler) Missing(t string) string    { return s.missing.Render(t) }
func (s Styler) Optional(t string) string   { return s.optional.Render(t) }
func (s Styler) Stored(t string) string     { return s.stored.Render(t) }
func (s Styler) Override(t string) string   { return s.override.Render(t) }
func (s Styler) SecretMark(t string) string { return s.secretMark.Render(t) }
func (s Styler) Fresh(t string) string      { return s.fresh.Render(t) }
func (s Styler) Stale(t string) string      { return s.stale.Render(t) }
func (s Styler) Modified(t string) string   { return s.modified.Render(t) }
func (s Styler) Foreign(t string) string    { return s.foreign.Render(t) }
func (s Styler) Absent(t string) string     { return s.absent.Render(t) }
func (s Styler) Unknown(t string) string    { return s.unknown.Render(t) }
func (s Styler) Staged(t string) string     { return s.staged.Render(t) }
func (s Styler) Cursor(t string) string     { return s.cursor.Render(t) }
func (s Styler) ActiveTab(t string) string  { return s.activeTab.Render(t) }

// Shared is the common case: it earns no ink. It exists as a method so every
// cell goes through the Styler and a reader does not have to work out which
// tokens are styled by looking for the ones that are not.
func (s Styler) Shared(t string) string { return t }

// Panel draws a bordered box. A zero Styler has no renderer, so it returns
// the body unchanged rather than panicking -- consistent with every other
// method on the zero value, and harmless in practice because a zero Styler
// only occurs in tests, where width is 0 and the caller renders flat anyway.
func (s Styler) Panel(title, body string, width int) string {
	if s.r == nil {
		return body
	}
	return panel(s.r, title, body, width)
}
