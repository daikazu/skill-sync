// Package repo reads and writes the sync repo: item layout, manifest,
// and the git operations that move it between devices.
package repo

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/daikazu/skill-sync/internal/fsutil"
	"github.com/daikazu/skill-sync/internal/item"
	"github.com/daikazu/skill-sync/internal/scan"
	"github.com/daikazu/skill-sync/internal/settings"
)

func WriteItem(root string, s scan.Scanned) error {
	switch s.ID.Type() {
	case item.TypeSkill:
		return fsutil.CopyTree(s.Path, filepath.Join(root, "skills", s.ID.Name()))
	case item.TypeAgent:
		return fsutil.CopyFile(s.Path, filepath.Join(root, "agents", s.ID.Name()+".md"))
	case item.TypeCommand:
		return fsutil.CopyFile(s.Path, filepath.Join(root, "commands", s.ID.Name()+".md"))
	case item.TypeRules:
		return fsutil.CopyFile(s.Path, filepath.Join(root, "rules", "CLAUDE.md"))
	case item.TypeSetting:
		return editDoc(filepath.Join(root, "settings.json"), func(d *settings.Doc) {
			d.Set(s.ID.Name(), s.Value)
		})
	case item.TypePlugins:
		key, entry, _ := strings.Cut(s.ID.Name(), ":")
		return editDoc(filepath.Join(root, "plugins.json"), func(d *settings.Doc) {
			settings.SetPluginEntry(d, key, entry, s.Value)
		})
	}
	return nil
}

func DeleteItem(root string, id item.ID) error {
	switch id.Type() {
	case item.TypeSkill:
		return os.RemoveAll(filepath.Join(root, "skills", id.Name()))
	case item.TypeAgent:
		return rmIfExists(filepath.Join(root, "agents", id.Name()+".md"))
	case item.TypeCommand:
		return rmIfExists(filepath.Join(root, "commands", id.Name()+".md"))
	case item.TypeRules:
		return rmIfExists(filepath.Join(root, "rules", "CLAUDE.md"))
	case item.TypeSetting:
		return editDoc(filepath.Join(root, "settings.json"), func(d *settings.Doc) {
			d.Delete(id.Name())
		})
	case item.TypePlugins:
		key, entry, _ := strings.Cut(id.Name(), ":")
		return editDoc(filepath.Join(root, "plugins.json"), func(d *settings.Doc) {
			settings.DeletePluginEntry(d, key, entry)
		})
	}
	return nil
}

func rmIfExists(p string) error {
	err := os.Remove(p)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func editDoc(path string, f func(*settings.Doc)) error {
	d, err := settings.Load(path)
	if err != nil {
		return err
	}
	f(d)
	return d.Save(path)
}
