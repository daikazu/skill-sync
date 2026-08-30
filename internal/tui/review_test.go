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
	return NewReview(conflicts, nil, map[item.ID]scan.Scanned{}, map[item.ID]scan.Scanned{})
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

func autoModel() (ReviewModel, plan.Plan) {
	conflicts := []classify.Result{
		{ID: item.ID("agent/a"), State: classify.Conflict, Local: "1", Remote: "2"},
	}
	auto := []plan.Change{
		{Result: classify.Result{ID: item.ID("agent/keep"), State: classify.Push}, Action: plan.ActPush},
		{Result: classify.Result{ID: item.ID("skill/gone"), State: classify.DeletedRemote}, Action: plan.ActDeleteLocal},
		{Result: classify.Result{ID: item.ID("agent/insync"), State: classify.InSync}, Action: plan.ActBaseOnly},
	}
	p := plan.Plan{Conflicts: conflicts, Auto: auto}
	m := NewReview(conflicts, auto, map[item.ID]scan.Scanned{}, map[item.ID]scan.Scanned{})
	return m, p
}

func TestDemoteAutoExcludesFromResolve(t *testing.T) {
	m, p := autoModel()
	var mm tea.Model = m
	mm, _ = mm.Update(key("l"))                       // resolve the conflict, stays on last conflict row
	mm, _ = mm.Update(tea.KeyMsg{Type: tea.KeyDown})  // → auto: agent/keep
	mm, _ = mm.Update(tea.KeyMsg{Type: tea.KeyDown})  // → auto: skill/gone (the remote deletion)
	mm, _ = mm.Update(key("s"))                       // demote it
	mm, _ = mm.Update(tea.KeyMsg{Type: tea.KeyEnter}) // confirm
	rm := mm.(ReviewModel)
	if !rm.Done() || rm.Aborted() {
		t.Fatalf("model should be done: %+v", rm)
	}
	if rm.Choices()[item.ID("skill/gone")] != plan.ResSkip {
		t.Fatalf("demotion must record ResSkip: %v", rm.Choices())
	}
	changes := plan.Resolve(p, rm.Choices())
	for _, c := range changes {
		if c.Result.ID == item.ID("skill/gone") {
			t.Fatal("demoted auto change must not survive Resolve")
		}
	}
	// undemoted auto changes still apply
	var kept bool
	for _, c := range changes {
		if c.Result.ID == item.ID("agent/keep") && c.Action == plan.ActPush {
			kept = true
		}
	}
	if !kept {
		t.Fatalf("undemoted auto change lost: %+v", changes)
	}
}

func TestDemoteToggleRestoresAuto(t *testing.T) {
	m, p := autoModel()
	var mm tea.Model = m
	mm, _ = mm.Update(tea.KeyMsg{Type: tea.KeyDown}) // → agent/keep
	mm, _ = mm.Update(key("s"))                      // demote
	mm, _ = mm.Update(key(" "))                      // space toggles back
	mm, _ = mm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	rm := mm.(ReviewModel)
	if _, chosen := rm.Choices()[item.ID("agent/keep")]; chosen {
		t.Fatalf("toggled-back auto item must have no choice: %v", rm.Choices())
	}
	changes := plan.Resolve(p, rm.Choices())
	var kept bool
	for _, c := range changes {
		if c.Result.ID == item.ID("agent/keep") {
			kept = true
		}
	}
	if !kept {
		t.Fatalf("restored auto change must apply: %+v", changes)
	}
}

func TestAutoListSkipsBaseOnlyAndNavigationStops(t *testing.T) {
	m, _ := autoModel()
	if m.total() != 3 { // 1 conflict + 2 real auto changes; base-only hidden
		t.Fatalf("base-only changes must not be reviewable: total=%d", m.total())
	}
	var mm tea.Model = m
	for i := 0; i < 10; i++ {
		mm, _ = mm.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	rm := mm.(ReviewModel)
	if rm.idx != rm.total()-1 {
		t.Fatalf("navigation must clamp at the last row: idx=%d", rm.idx)
	}
	if rm.View() == "" {
		t.Fatal("view must render on an auto row")
	}
	// l/r on an auto row must not record anything
	mm, _ = mm.Update(key("l"))
	mm, _ = mm.Update(key("r"))
	rm = mm.(ReviewModel)
	if len(rm.Choices()) != 0 {
		t.Fatalf("l/r must be inert on auto rows: %v", rm.Choices())
	}
}
