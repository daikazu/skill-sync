package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/daikazu/skill-sync/internal/classify"
	"github.com/daikazu/skill-sync/internal/item"
	"github.com/daikazu/skill-sync/internal/plan"
	"github.com/daikazu/skill-sync/internal/scan"
)

func key(s string) tea.Msg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func testModel() ReviewModel {
	conflicts := []classify.Result{
		{ID: item.ID("agent/a"), State: classify.Conflict, Local: "1", Remote: "2"},
		{ID: item.ID("agent/b"), State: classify.Conflict, Local: "1", Remote: "2"},
	}
	return NewReview(conflicts, map[item.ID]scan.Scanned{}, map[item.ID]scan.Scanned{})
}

func TestChoicesAndConfirm(t *testing.T) {
	m := testModel()
	var mm tea.Model = m
	mm, _ = mm.Update(key("l")) // agent/a → local, auto-advance
	mm, _ = mm.Update(key("r")) // agent/b → remote
	mm, _ = mm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	rm := mm.(ReviewModel)
	if !rm.Done() || rm.Aborted() {
		t.Fatalf("model should be done: %+v", rm)
	}
	ch := rm.Choices()
	if ch[item.ID("agent/a")] != plan.ResLocal || ch[item.ID("agent/b")] != plan.ResRemote {
		t.Fatalf("choices: %v", ch)
	}
}

func TestEnterDefaultsUnchosenToSkip(t *testing.T) {
	m := testModel()
	var mm tea.Model = m
	mm, _ = mm.Update(key("l"))
	mm, _ = mm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	rm := mm.(ReviewModel)
	if rm.Choices()[item.ID("agent/b")] != plan.ResSkip {
		t.Fatalf("unchosen must default to skip: %v", rm.Choices())
	}
}

func TestAbort(t *testing.T) {
	m := testModel()
	var mm tea.Model = m
	mm, _ = mm.Update(key("q"))
	rm := mm.(ReviewModel)
	if !rm.Aborted() {
		t.Fatal("q must abort")
	}
}

func TestRenderContentAbsent(t *testing.T) {
	if RenderContent(scan.Scanned{}) != "(absent)" {
		t.Fatal("absent side must render as (absent)")
	}
}
