package manage

import (
	"io"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cdescobedo/trenvy/internal/style"
)

// ThemeMsg replaces the theme while the program runs (spec 2026-10-06 §3).
type ThemeMsg struct{ Theme style.Theme }

// RunOptions are how the program is started: the input to read (the
// termcolor filter, when following the terminal's colours), focus
// reporting, the re-query interval, and hooks around the program's life.
type RunOptions struct {
	Input       io.Reader
	ReportFocus bool
	Poll        time.Duration            // 0: no timer
	OnStart     func(send func(tea.Msg)) // called with the program's Send before it runs
	OnExit      func()                   // called after the program returns
}

// pollMsg is the re-query timer firing.
type pollMsg struct{}

// WithRun returns m carrying the options Run starts the program with.
func (m Model) WithRun(o RunOptions) Model { m.run = o; return m }

// WithRun returns s carrying the options RunSetup starts the program with.
func (s Setup) WithRun(o RunOptions) Setup { s.run = o; return s }

func (o RunOptions) programOptions() []tea.ProgramOption {
	opts := []tea.ProgramOption{tea.WithAltScreen()}
	if o.Input != nil {
		opts = append(opts, tea.WithInput(o.Input))
	}
	if o.ReportFocus {
		opts = append(opts, tea.WithReportFocus())
	}
	return opts
}

func (m Model) pollCmd() tea.Cmd {
	if m.run.Poll <= 0 {
		return nil
	}
	return tea.Tick(m.run.Poll, func(time.Time) tea.Msg { return pollMsg{} })
}

// requeryCmd asks the terminal again. The answer is not a message of its
// own: it arrives as a ThemeMsg from the input filter.
func (m Model) requeryCmd() tea.Cmd {
	q := m.deps.Requery
	if q == nil {
		return nil
	}
	return func() tea.Msg { _ = q(); return nil }
}

// start runs p with the options' hooks around it.
func (o RunOptions) start(p *tea.Program) (tea.Model, error) {
	if o.OnStart != nil {
		o.OnStart(p.Send)
	}
	final, err := p.Run()
	if o.OnExit != nil {
		o.OnExit()
	}
	return final, err
}
