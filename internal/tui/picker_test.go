package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/daikazu/skill-sync/internal/item"
	"github.com/daikazu/skill-sync/internal/scan"
)

func pickerItems() []scan.Scanned {
	return []scan.Scanned{
		{ID: item.ID("agent/a")},
		{ID: item.ID("setting/model")},
		{ID: item.ID("skill/s")},
	}
}

func TestPickerDefaults(t *testing.T) {
	m := NewPicker(pickerItems())
	var mm tea.Model = m
	mm, _ = mm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	pm := mm.(PickerModel)
	sel := map[item.ID]bool{}
	for _, id := range pm.Selected() {
		sel[id] = true
	}
	if !sel[item.ID("agent/a")] || !sel[item.ID("skill/s")] {
		t.Fatalf("content items must default checked: %v", sel)
	}
	if sel[item.ID("setting/model")] {
		t.Fatal("settings must default unchecked for curated packs")
	}
}

func TestPickerToggleAndAbort(t *testing.T) {
	m := NewPicker(pickerItems())
	var mm tea.Model = m
	mm, _ = mm.Update(tea.KeyMsg{Type: tea.KeySpace}) // toggles first row (agent/a) off
	mm, _ = mm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	pm := mm.(PickerModel)
	for _, id := range pm.Selected() {
		if id == item.ID("agent/a") {
			t.Fatal("space must toggle off")
		}
	}
	m2 := NewPicker(pickerItems())
	var mm2 tea.Model = m2
	mm2, _ = mm2.Update(key("q"))
	if !mm2.(PickerModel).Aborted() {
		t.Fatal("q must abort")
	}
}
