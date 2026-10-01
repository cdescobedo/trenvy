package manage

import tea "github.com/charmbracelet/bubbletea"

// Replaced in Task 10.
func (m Model) openReview() (tea.Model, tea.Cmd)              { return m, nil }
func (m *Model) openReviewPtr() (bool, tea.Cmd)               { return false, nil }
func (m Model) afterCommit(committedMsg) (tea.Model, tea.Cmd) { return m, nil }
func (m Model) afterReload(reloadedMsg) (tea.Model, tea.Cmd)  { return m, nil }
