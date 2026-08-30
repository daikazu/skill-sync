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
)

type ReviewModel struct {
	conflicts []classify.Result
	diffs     map[item.ID]string
	idx       int
	choices   map[item.ID]plan.Resolution
	done      bool
	aborted   bool
}

func NewReview(conflicts []classify.Result, local, remote map[item.ID]scan.Scanned) ReviewModel {
	diffs := map[item.ID]string{}
	for _, c := range conflicts {
		l := RenderContent(local[c.ID])
		r := RenderContent(remote[c.ID])
		diffs[c.ID] = udiff.Unified("local", "remote", l+"\n", r+"\n")
	}
	return ReviewModel{
		conflicts: conflicts,
		diffs:     diffs,
		choices:   map[item.ID]plan.Resolution{},
	}
}

func (m ReviewModel) Init() tea.Cmd { return nil }

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
		if m.idx < len(m.conflicts)-1 {
			m.idx++
		}
	case "l":
		m.choose(plan.ResLocal)
	case "r":
		m.choose(plan.ResRemote)
	case "s":
		m.choose(plan.ResSkip)
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

func (m ReviewModel) View() string {
	if m.done {
		return ""
	}
	c := m.conflicts[m.idx]
	var b strings.Builder
	b.WriteString(titleStyle.Render(fmt.Sprintf("Conflict %d/%d: %s (%s)",
		m.idx+1, len(m.conflicts), c.ID, c.State)) + "\n\n")
	b.WriteString(m.diffs[c.ID] + "\n")
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
	b.WriteString(hintStyle.Render("\nl keep local · r keep remote · s skip · ↑/↓ move · enter confirm · q abort\n"))
	return b.String()
}

func (m ReviewModel) Done() bool                           { return m.done }
func (m ReviewModel) Aborted() bool                        { return m.aborted }
func (m ReviewModel) Choices() map[item.ID]plan.Resolution { return m.choices }

func RunReview(conflicts []classify.Result, local, remote map[item.ID]scan.Scanned) (map[item.ID]plan.Resolution, bool, error) {
	p := tea.NewProgram(NewReview(conflicts, local, remote))
	out, err := p.Run()
	if err != nil {
		return nil, false, err
	}
	m := out.(ReviewModel)
	return m.Choices(), !m.Aborted(), nil
}
