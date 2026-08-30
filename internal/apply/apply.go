// Package apply executes a resolved sync plan against the local claude
// dir and the sync repo checkout.
package apply

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/daikazu/skill-sync/internal/fsutil"
	"github.com/daikazu/skill-sync/internal/item"
	"github.com/daikazu/skill-sync/internal/plan"
	"github.com/daikazu/skill-sync/internal/repo"
	"github.com/daikazu/skill-sync/internal/scan"
	"github.com/daikazu/skill-sync/internal/settings"
)

type Applier struct {
	ClaudeDir  string
	RepoDir    string
	BackupsDir string
}

func (a Applier) Apply(changes []plan.Change, local, remote map[item.ID]scan.Scanned) (map[item.ID]string, error) {
	base := map[item.ID]string{}
	var doc *settings.Doc
	loadDoc := func() (*settings.Doc, error) {
		if doc != nil {
			return doc, nil
		}
		d, err := settings.Load(filepath.Join(a.ClaudeDir, "settings.json"))
		if err != nil {
			return nil, err
		}
		doc = d
		return doc, nil
	}

	for _, c := range changes {
		id := c.Result.ID
		var err error
		switch c.Action {
		case plan.ActPull:
			err = a.writeLocal(id, remote[id], loadDoc)
			if err == nil {
				base[id] = remote[id].Hash
			}
		case plan.ActPush:
			err = repo.WriteItem(a.RepoDir, local[id])
			if err == nil {
				base[id] = local[id].Hash
			}
		case plan.ActDeleteLocal:
			err = a.deleteLocal(id, loadDoc)
			if err == nil {
				base[id] = ""
			}
		case plan.ActDeleteRemote:
			err = repo.DeleteItem(a.RepoDir, id)
			if err == nil {
				base[id] = ""
			}
		case plan.ActBaseOnly:
			base[id] = local[id].Hash
		}
		if err != nil {
			return base, fmt.Errorf("apply %s %s: %w", c.Action, id, err)
		}
	}
	if doc != nil {
		if err := doc.Save(filepath.Join(a.ClaudeDir, "settings.json")); err != nil {
			return base, err
		}
	}
	return base, nil
}

func (a Applier) writeLocal(id item.ID, src scan.Scanned, loadDoc func() (*settings.Doc, error)) error {
	switch id.Type() {
	case item.TypeSkill:
		return fsutil.CopyTree(src.Path, filepath.Join(a.ClaudeDir, LocalRelPath(id)))
	case item.TypeAgent, item.TypeCommand, item.TypeRules:
		return fsutil.CopyFile(src.Path, filepath.Join(a.ClaudeDir, LocalRelPath(id)))
	case item.TypeSetting:
		d, err := loadDoc()
		if err != nil {
			return err
		}
		d.Set(id.Name(), src.Value)
	case item.TypePlugins:
		d, err := loadDoc()
		if err != nil {
			return err
		}
		key, entry, _ := strings.Cut(id.Name(), ":")
		settings.SetPluginEntry(d, key, entry, src.Value)
	}
	return nil
}

func (a Applier) deleteLocal(id item.ID, loadDoc func() (*settings.Doc, error)) error {
	switch id.Type() {
	case item.TypeSkill:
		return os.RemoveAll(filepath.Join(a.ClaudeDir, LocalRelPath(id)))
	case item.TypeAgent, item.TypeCommand, item.TypeRules:
		err := os.Remove(filepath.Join(a.ClaudeDir, LocalRelPath(id)))
		if os.IsNotExist(err) {
			return nil
		}
		return err
	case item.TypeSetting:
		d, err := loadDoc()
		if err != nil {
			return err
		}
		d.Delete(id.Name())
	case item.TypePlugins:
		d, err := loadDoc()
		if err != nil {
			return err
		}
		key, entry, _ := strings.Cut(id.Name(), ":")
		settings.DeletePluginEntry(d, key, entry)
	}
	return nil
}
