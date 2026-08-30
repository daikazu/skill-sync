// Package classify computes the per-item 3-way state between the local
// machine, the sync repo, and the last-synced base recorded on device.
package classify

import (
	"sort"

	"github.com/daikazu/skill-sync/internal/item"
	"github.com/daikazu/skill-sync/internal/scan"
)

type State string

const (
	InSync               State = "in-sync"
	Push                 State = "push"
	Pull                 State = "pull"
	NewLocal             State = "new-local"
	NewRemote            State = "new-remote"
	DeletedLocal         State = "deleted-local"
	DeletedRemote        State = "deleted-remote"
	Conflict             State = "conflict"
	ConflictBothNew      State = "conflict-both-new"
	ConflictDeleteModify State = "conflict-delete-modify"
)

// IsConflict reports whether the state requires a human decision.
func (s State) IsConflict() bool {
	return s == Conflict || s == ConflictBothNew || s == ConflictDeleteModify
}

type Result struct {
	ID     item.ID
	State  State
	Local  string // hash, "" = absent
	Base   string
	Remote string
}

func All(local, remote map[item.ID]scan.Scanned, base map[item.ID]string) []Result {
	ids := map[item.ID]bool{}
	for id := range local {
		ids[id] = true
	}
	for id := range remote {
		ids[id] = true
	}
	for id := range base {
		ids[id] = true
	}
	var out []Result
	for id := range ids {
		l := local[id].Hash
		r := remote[id].Hash
		b := base[id]
		st, ok := one(l, b, r)
		if !ok {
			continue
		}
		out = append(out, Result{ID: id, State: st, Local: l, Base: b, Remote: r})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func one(l, b, r string) (State, bool) {
	switch {
	case l != "" && r != "":
		if l == r {
			return InSync, true
		}
		switch b {
		case r:
			return Push, true
		case l:
			return Pull, true
		case "":
			return ConflictBothNew, true
		default:
			return Conflict, true
		}
	case l != "": // remote absent
		switch b {
		case "":
			return NewLocal, true
		case l:
			return DeletedRemote, true
		default:
			return ConflictDeleteModify, true
		}
	case r != "": // local absent
		switch b {
		case "":
			return NewRemote, true
		case r:
			return DeletedLocal, true
		default:
			return ConflictDeleteModify, true
		}
	default: // stale base entry
		return "", false
	}
}
