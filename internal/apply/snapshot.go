// Snapshotting copies the local files a sync is about to touch into a
// timestamped backup dir so any apply can be reversed.
package apply

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/daikazu/skill-sync/internal/fsutil"
	"github.com/daikazu/skill-sync/internal/item"
	"github.com/daikazu/skill-sync/internal/plan"
)

// LocalRelPath maps a filesystem item to its path inside the claude dir.
func LocalRelPath(id item.ID) string {
	switch id.Type() {
	case item.TypeSkill:
		return filepath.Join("skills", id.Name())
	case item.TypeAgent:
		return filepath.Join("agents", id.Name()+".md")
	case item.TypeCommand:
		return filepath.Join("commands", id.Name()+".md")
	case item.TypeRules:
		return "CLAUDE.md"
	}
	return ""
}

func touchesLocal(act plan.Action) bool {
	return act == plan.ActPull || act == plan.ActDeleteLocal
}

func (a Applier) Snapshot(changes []plan.Change) (string, error) {
	var rels []string
	needSettings := false
	for _, c := range changes {
		if !touchesLocal(c.Action) {
			continue
		}
		switch c.Result.ID.Type() {
		case item.TypeSetting, item.TypePlugins:
			needSettings = true
		default:
			rels = append(rels, LocalRelPath(c.Result.ID))
		}
	}
	if needSettings {
		rels = append(rels, "settings.json")
	}
	return SnapshotPaths(a.ClaudeDir, a.BackupsDir, rels)
}

// SnapshotPaths copies the given claude-dir-relative paths into a new
// timestamped dir under backupsDir. Returns "" if nothing existed to copy.
func SnapshotPaths(claudeDir, backupsDir string, rels []string) (string, error) {
	if len(rels) == 0 {
		return "", nil
	}
	stamp := strings.ReplaceAll(time.Now().UTC().Format(time.RFC3339), ":", "-")
	dir := filepath.Join(backupsDir, stamp)
	copied := []string{}
	for _, rel := range rels {
		src := filepath.Join(claudeDir, rel)
		st, err := os.Stat(src)
		if err != nil {
			continue // nothing local to preserve
		}
		dst := filepath.Join(dir, rel)
		if st.IsDir() {
			err = fsutil.CopyTree(src, dst)
		} else {
			err = fsutil.CopyFile(src, dst)
		}
		if err != nil {
			return "", err
		}
		copied = append(copied, rel)
	}
	if len(copied) == 0 {
		return "", nil
	}
	meta, _ := json.MarshalIndent(map[string]any{
		"createdAt": time.Now().UTC().Format(time.RFC3339),
		"paths":     copied,
	}, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "snapshot.json"), append(meta, '\n'), 0o644); err != nil {
		return "", err
	}
	return dir, nil
}

func Restore(snapshotDir, claudeDir string) error {
	return filepath.WalkDir(snapshotDir, func(p string, d os.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() || d.Name() == "snapshot.json" && filepath.Dir(p) == snapshotDir {
			return err
		}
		rel, err := filepath.Rel(snapshotDir, p)
		if err != nil {
			return err
		}
		return fsutil.CopyFile(p, filepath.Join(claudeDir, rel))
	})
}

func ListSnapshots(backupsDir string) ([]string, error) {
	entries, err := os.ReadDir(backupsDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	return names, nil
}
