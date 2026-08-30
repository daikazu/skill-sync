// Package scan inventories a ~/.claude directory or a sync-repo checkout
// into items with content hashes.
package scan

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/daikazu/skill-sync/internal/hash"
	"github.com/daikazu/skill-sync/internal/item"
	"github.com/daikazu/skill-sync/internal/settings"
)

type Scanned struct {
	ID    item.ID
	Hash  string
	Path  string          // filesystem items: absolute path to file or skill dir
	Value json.RawMessage // setting / plugins-entry items
}

func Claude(dir string, o settings.KeyOverrides) (map[item.ID]Scanned, []string, error) {
	out := map[item.ID]Scanned{}
	var warns []string
	if err := scanFiles(dir, filepath.Join(dir, "CLAUDE.md"), out); err != nil {
		return nil, nil, err
	}
	doc, err := settings.Load(filepath.Join(dir, "settings.json"))
	if err != nil {
		return out, append(warns, "settings.json: "+err.Error()+" — settings and plugins skipped"), nil
	}
	for _, k := range settings.ShareableKeys(doc, o) {
		v, _ := doc.Get(k)
		if err := addValue(out, item.NewID(item.TypeSetting, k), v); err != nil {
			return nil, nil, err
		}
	}
	warns = append(warns, scanPluginDoc(doc, out)...)
	return out, warns, nil
}

func Repo(dir string) (map[item.ID]Scanned, []string, error) {
	out := map[item.ID]Scanned{}
	var warns []string
	if err := scanFiles(dir, filepath.Join(dir, "rules", "CLAUDE.md"), out); err != nil {
		return nil, nil, err
	}
	doc, err := settings.Load(filepath.Join(dir, "settings.json"))
	if err != nil {
		warns = append(warns, "repo settings.json: "+err.Error()+" — settings skipped")
	} else {
		for _, k := range doc.Keys() {
			v, _ := doc.Get(k)
			if err := addValue(out, item.NewID(item.TypeSetting, k), v); err != nil {
				return nil, nil, err
			}
		}
	}
	plugins, err := settings.Load(filepath.Join(dir, "plugins.json"))
	if err != nil {
		warns = append(warns, "repo plugins.json: "+err.Error()+" — plugins skipped")
		return out, warns, nil
	}
	warns = append(warns, scanPluginDoc(plugins, out)...)
	return out, warns, nil
}

// scanPluginDoc expands both plugin keys' object entries into plugins items.
func scanPluginDoc(doc *settings.Doc, out map[item.ID]Scanned) []string {
	var warns []string
	for _, pk := range []string{settings.KeyEnabledPlugins, settings.KeyExtraMarketplaces} {
		entries, err := settings.PluginEntries(doc, pk)
		if err != nil {
			warns = append(warns, pk+": "+err.Error()+" — skipped")
			continue
		}
		for name, v := range entries {
			if err := addValue(out, item.NewID(item.TypePlugins, pk+":"+name), v); err != nil {
				warns = append(warns, pk+":"+name+": "+err.Error()+" — skipped")
			}
		}
	}
	return warns
}

// scanFiles handles the layout shared by both roots: skills/, agents/,
// commands/, plus the rules file at rulesPath.
func scanFiles(dir, rulesPath string, out map[item.ID]Scanned) error {
	skillDirs, err := os.ReadDir(filepath.Join(dir, "skills"))
	if err == nil {
		for _, e := range skillDirs {
			if !e.IsDir() {
				continue
			}
			p := filepath.Join(dir, "skills", e.Name())
			h, err := hash.Tree(p)
			if err != nil {
				return err
			}
			id := item.NewID(item.TypeSkill, e.Name())
			out[id] = Scanned{ID: id, Hash: h, Path: p}
		}
	}
	for sub, t := range map[string]item.Type{"agents": item.TypeAgent, "commands": item.TypeCommand} {
		entries, err := os.ReadDir(filepath.Join(dir, sub))
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			p := filepath.Join(dir, sub, e.Name())
			h, err := hash.File(p)
			if err != nil {
				return err
			}
			id := item.NewID(t, strings.TrimSuffix(e.Name(), ".md"))
			out[id] = Scanned{ID: id, Hash: h, Path: p}
		}
	}
	if st, err := os.Stat(rulesPath); err == nil && st.Mode().IsRegular() {
		h, err := hash.File(rulesPath)
		if err != nil {
			return err
		}
		id := item.NewID(item.TypeRules, "CLAUDE.md")
		out[id] = Scanned{ID: id, Hash: h, Path: rulesPath}
	}
	return nil
}

func addValue(out map[item.ID]Scanned, id item.ID, v json.RawMessage) error {
	h, err := hash.JSONValue(v)
	if err != nil {
		return err
	}
	out[id] = Scanned{ID: id, Hash: h, Value: v}
	return nil
}
