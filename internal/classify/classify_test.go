package classify

import (
	"testing"

	"github.com/daikazu/skill-sync/internal/item"
	"github.com/daikazu/skill-sync/internal/scan"
)

func TestAll(t *testing.T) {
	cases := []struct {
		name             string
		local, base, rem string // hashes; "" = absent
		want             State
	}{
		{"in sync", "a", "a", "a", InSync},
		{"in sync no base", "a", "", "a", InSync},
		{"local edit", "b", "a", "a", Push},
		{"remote edit", "a", "a", "b", Pull},
		{"both edited differently", "b", "a", "c", Conflict},
		{"both new same name diff content", "b", "", "c", ConflictBothNew},
		{"new local", "a", "", "", NewLocal},
		{"new remote", "", "", "a", NewRemote},
		{"deleted remotely, unchanged here", "a", "a", "", DeletedRemote},
		{"deleted locally, unchanged remote", "", "a", "a", DeletedLocal},
		{"modified locally but deleted remotely", "b", "a", "", ConflictDeleteModify},
		{"deleted locally but modified remotely", "", "a", "b", ConflictDeleteModify},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			id := item.ID("skill/x")
			local := map[item.ID]scan.Scanned{}
			remote := map[item.ID]scan.Scanned{}
			base := map[item.ID]string{}
			if c.local != "" {
				local[id] = scan.Scanned{ID: id, Hash: c.local}
			}
			if c.rem != "" {
				remote[id] = scan.Scanned{ID: id, Hash: c.rem}
			}
			if c.base != "" {
				base[id] = c.base
			}
			rs := All(local, remote, base)
			if len(rs) != 1 {
				t.Fatalf("want 1 result, got %v", rs)
			}
			if rs[0].State != c.want {
				t.Fatalf("got %s want %s", rs[0].State, c.want)
			}
		})
	}
}

func TestStaleBaseEntryDropped(t *testing.T) {
	base := map[item.ID]string{item.ID("skill/gone"): "a"}
	rs := All(map[item.ID]scan.Scanned{}, map[item.ID]scan.Scanned{}, base)
	if len(rs) != 0 {
		t.Fatalf("stale base entry must produce no result: %v", rs)
	}
}

func TestSortedOutput(t *testing.T) {
	local := map[item.ID]scan.Scanned{
		item.ID("skill/b"): {ID: item.ID("skill/b"), Hash: "x"},
		item.ID("agent/a"): {ID: item.ID("agent/a"), Hash: "y"},
	}
	rs := All(local, map[item.ID]scan.Scanned{}, map[item.ID]string{})
	if len(rs) != 2 || rs[0].ID != item.ID("agent/a") {
		t.Fatalf("results must be sorted by ID: %v", rs)
	}
}
