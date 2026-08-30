package tui

import (
	"fmt"
	"strings"

	"github.com/aymanbagabas/go-udiff"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/daikazu/skill-sync/internal/classify"
	"github.com/daikazu/skill-sync/internal/item"
	"github.com/daikazu/skill-sync/internal/plan"
	"github.com/daikazu/skill-sync/internal/scan"
)

var (
	titleStyle  = lipgloss.NewStyle().Bold(true)
	chosenStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	hintStyle   = lipgloss.NewStyle().Faint(true)
	deleteStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("9"))
)

type ReviewModel struct {
	conflicts []classify.Result
	auto      []plan.Change // auto-applying changes (base-only excluded)
	diffs     map[item.ID]string
	idx       int // spans conflicts then auto items
	choices   map[item.ID]plan.Resolution
	done      bool
	aborted   bool
}

func NewReview(conflicts []classify.Result, auto []plan.Change, local, remote map[item.ID]scan.Scanned) ReviewModel {
	diffs := map[item.ID]string{}
	for _, c := range conflicts {
		l := RenderContent(local[c.ID])
		r := RenderContent(remote[c.ID])
		diffs[c.ID] = udiff.Unified("local", "remote", l+"\n", r+"\n")
	}
	var shown []plan.Change
	for _, c := range auto {
		if c.Action != plan.ActBaseOnly {
			shown = append(shown, c)
		}
	}
	return ReviewModel{
		conflicts: conflicts,
		auto:      shown,
		diffs:     diffs,
		choices:   map[item.ID]plan.Resolution{},
	}
}

func (m ReviewModel) Init() tea.Cmd { return nil }

func (m ReviewModel) total() int { return len(m.conflicts) + len(m.auto) }

func (m ReviewModel) onConflict() bool { return m.idx < len(m.conflicts) }

func (m ReviewModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch k.String() {
	case "q", "esc", "ctrl+c":
		m.aborted, m.done = true, true
		return m, tea.Quit
	case "up", "k":
		if m.idx > 0 {
			m.idx--
		}
	case "down", "j":
		if m.idx < m.total()-1 {
			m.idx++
		}
	case "l":
		if m.onConflict() {
			m.choose(plan.ResLocal)
		}
	case "r":
		if m.onConflict() {
			m.choose(plan.ResRemote)
		}
	case "s", " ":
		if m.onConflict() {
			if k.String() == "s" {
				m.choose(plan.ResSkip)
			}
		} else {
			m.toggleAutoSkip()
		}
	case "enter":
		for _, c := range m.conflicts {
			if _, ok := m.choices[c.ID]; !ok {
				m.choices[c.ID] = plan.ResSkip
			}
		}
		m.done = true
		return m, tea.Quit
	}
	return m, nil
}

func (m *ReviewModel) choose(r plan.Resolution) {
	m.choices[m.conflicts[m.idx].ID] = r
	if m.idx < len(m.conflicts)-1 {
		m.idx++
	}
}

// toggleAutoSkip demotes the selected auto change to skip, or restores
// it if already demoted. Demotions travel through the same choices map
// as conflict resolutions; plan.Resolve drops auto changes marked skip.
func (m *ReviewModel) toggleAutoSkip() {
	id := m.auto[m.idx-len(m.conflicts)].Result.ID
	if m.choices[id] == plan.ResSkip {
		delete(m.choices, id)
	} else {
		m.choices[id] = plan.ResSkip
	}
}

func autoLabel(c plan.Change) string {
	switch c.Action {
	case plan.ActDeleteLocal:
		return deleteStyle.Render("DELETE LOCALLY (deleted on another machine)")
	case plan.ActDeleteRemote:
		return deleteStyle.Render("delete from repo")
	default:
		return string(c.Action)
	}
}

func (m ReviewModel) View() string {
	if m.done {
		return ""
	}
	var b strings.Builder
	if m.onConflict() {
		c := m.conflicts[m.idx]
		b.WriteString(titleStyle.Render(fmt.Sprintf("Conflict %d/%d: %s (%s)",
			m.idx+1, len(m.conflicts), c.ID, c.State)) + "\n\n")
		b.WriteString(m.diffs[c.ID] + "\n")
	} else {
		c := m.auto[m.idx-len(m.conflicts)]
		b.WriteString(titleStyle.Render(fmt.Sprintf("Auto change: %s %s",
			c.Action, c.Result.ID)) + "\n\n")
	}
	for i, cf := range m.conflicts {
		marker := "  "
		if i == m.idx {
			marker = "> "
		}
		choice := string(m.choices[cf.ID])
		if choice != "" {
			choice = chosenStyle.Render(" [" + choice + "]")
		}
		b.WriteString(marker + string(cf.ID) + choice + "\n")
	}
	if len(m.auto) > 0 {
		b.WriteString("\n" + titleStyle.Render(fmt.Sprintf("Auto-applying (%d):", len(m.auto))) + "\n")
		for i, c := range m.auto {
			marker := "  "
			if len(m.conflicts)+i == m.idx {
				marker = "> "
			}
			row := marker + autoLabel(c) + " " + string(c.Result.ID)
			if m.choices[c.Result.ID] == plan.ResSkip {
				row += chosenStyle.Render(" [skip]")
			}
			b.WriteString(row + "\n")
		}
	}
	b.WriteString(hintStyle.Render("\nl keep local · r keep remote · s skip (toggles on auto items) · ↑/↓ move · enter confirm · q abort\n"))
	return b.String()
}

func (m ReviewModel) Done() bool                           { return m.done }
func (m ReviewModel) Aborted() bool                        { return m.aborted }
func (m ReviewModel) Choices() map[item.ID]plan.Resolution { return m.choices }

func RunReview(conflicts []classify.Result, auto []plan.Change, local, remote map[item.ID]scan.Scanned) (map[item.ID]plan.Resolution, bool, error) {
	p := tea.NewProgram(NewReview(conflicts, auto, local, remote))
	out, err := p.Run()
	if err != nil {
		return nil, false, err
	}
	m := out.(ReviewModel)
	return m.Choices(), !m.Aborted(), nil
}
