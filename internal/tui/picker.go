package tui

import (
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/daikazu/skill-sync/internal/item"
	"github.com/daikazu/skill-sync/internal/scan"
)

type PickerModel struct {
	ids     []item.ID
	checked map[item.ID]bool
	idx     int
	done    bool
	aborted bool
}

func defaultChecked(t item.Type) bool {
	return t == item.TypeSkill || t == item.TypeAgent || t == item.TypeCommand
}

func NewPicker(items []scan.Scanned) PickerModel {
	m := PickerModel{checked: map[item.ID]bool{}}
	for _, s := range items {
		m.ids = append(m.ids, s.ID)
		m.checked[s.ID] = defaultChecked(s.ID.Type())
	}
	sort.Slice(m.ids, func(i, j int) bool { return m.ids[i] < m.ids[j] })
	return m
}

func (m PickerModel) Init() tea.Cmd { return nil }

func (m PickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
		if m.idx < len(m.ids)-1 {
			m.idx++
		}
	case " ":
		id := m.ids[m.idx]
		m.checked[id] = !m.checked[id]
	case "a":
		for _, id := range m.ids {
			m.checked[id] = true
		}
	case "n":
		for _, id := range m.ids {
			m.checked[id] = false
		}
	case "enter":
		m.done = true
		return m, tea.Quit
	}
	return m, nil
}

func (m PickerModel) View() string {
	if m.done {
		return ""
	}
	var b strings.Builder
	b.WriteString(titleStyle.Render("Select items to pack") + "\n\n")
	for i, id := range m.ids {
		cursor := "  "
		if i == m.idx {
			cursor = "> "
		}
		box := "[ ]"
		if m.checked[id] {
			box = chosenStyle.Render("[x]")
		}
		b.WriteString(cursor + box + " " + string(id) + "\n")
	}
	b.WriteString(hintStyle.Render("\nspace toggle · a all · n none · enter confirm · q abort\n"))
	return b.String()
}

func (m PickerModel) Done() bool    { return m.done }
func (m PickerModel) Aborted() bool { return m.aborted }

func (m PickerModel) Selected() []item.ID {
	var out []item.ID
	for _, id := range m.ids {
		if m.checked[id] {
			out = append(out, id)
		}
	}
	return out
}

func RunPicker(items []scan.Scanned) ([]item.ID, bool, error) {
	p := tea.NewProgram(NewPicker(items))
	out, err := p.Run()
	if err != nil {
		return nil, false, err
	}
	m := out.(PickerModel)
	return m.Selected(), !m.Aborted(), nil
}
