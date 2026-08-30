// Package scan inventories a ~/.claude directory or a sync-repo checkout
// into items with content hashes.
package scan

import (
	"encoding/json"
	"errors"
	"io/fs"
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

// Unscannable reports whether id matched an entry of an unscannable set
// returned by Claude or Repo. Entries are either exact item IDs
// ("skill/humanizer") or type prefixes ending in "/" or ":" ("setting/",
// "plugins/enabledPlugins:") covering every item of that shape when the
// underlying source could not be read at all.
func Unscannable(id item.ID, unscannable []string) bool {
	s := string(id)
	for _, u := range unscannable {
		if s == u {
			return true
		}
		if (strings.HasSuffix(u, "/") || strings.HasSuffix(u, ":")) && strings.HasPrefix(s, u) {
			return true
		}
	}
	return false
}

// Claude inventories a local ~/.claude dir. Besides the item map it
// returns the unscannable set: IDs (or type prefixes) of items that exist
// but could not be read or parsed. Callers that delete or overwrite based
// on absence MUST treat those items as unknown, not absent.
func Claude(dir string, o settings.KeyOverrides) (map[item.ID]Scanned, []string, []string, error) {
	out := map[item.ID]Scanned{}
	var warns, unscannable []string
	fileWarns, fileUnscan, err := scanFiles(dir, filepath.Join(dir, "CLAUDE.md"), out)
	if err != nil {
		return nil, nil, nil, err
	}
	warns = append(warns, fileWarns...)
	unscannable = append(unscannable, fileUnscan...)
	doc, err := settings.Load(filepath.Join(dir, "settings.json"))
	if err != nil {
		unscannable = append(unscannable, "setting/", "plugins/")
		return out, unscannable, append(warns, "settings.json: "+err.Error()+" — settings and plugins skipped"), nil
	}
	for _, k := range settings.ShareableKeys(doc, o) {
		v, _ := doc.Get(k)
		id := item.NewID(item.TypeSetting, k)
		if err := addValue(out, id, v); err != nil {
			unscannable = append(unscannable, string(id))
			warns = append(warns, string(id)+": "+err.Error()+" — skipped")
		}
	}
	pw, pu := scanPluginDoc(doc, out)
	return out, append(unscannable, pu...), append(warns, pw...), nil
}

// Repo inventories a sync-repo checkout (or extracted skillpack). See
// Claude for the meaning of the unscannable set.
func Repo(dir string) (map[item.ID]Scanned, []string, []string, error) {
	out := map[item.ID]Scanned{}
	var warns, unscannable []string
	fileWarns, fileUnscan, err := scanFiles(dir, filepath.Join(dir, "rules", "CLAUDE.md"), out)
	if err != nil {
		return nil, nil, nil, err
	}
	warns = append(warns, fileWarns...)
	unscannable = append(unscannable, fileUnscan...)
	doc, err := settings.Load(filepath.Join(dir, "settings.json"))
	if err != nil {
		unscannable = append(unscannable, "setting/")
		warns = append(warns, "repo settings.json: "+err.Error()+" — settings skipped")
	} else {
		for _, k := range doc.Keys() {
			v, _ := doc.Get(k)
			id := item.NewID(item.TypeSetting, k)
			if err := addValue(out, id, v); err != nil {
				unscannable = append(unscannable, string(id))
				warns = append(warns, string(id)+": "+err.Error()+" — skipped")
			}
		}
	}
	plugins, err := settings.Load(filepath.Join(dir, "plugins.json"))
	if err != nil {
		unscannable = append(unscannable, "plugins/")
		warns = append(warns, "repo plugins.json: "+err.Error()+" — plugins skipped")
		return out, unscannable, warns, nil
	}
	pw, pu := scanPluginDoc(plugins, out)
	return out, append(unscannable, pu...), append(warns, pw...), nil
}

// scanPluginDoc expands both plugin keys' object entries into plugins items.
func scanPluginDoc(doc *settings.Doc, out map[item.ID]Scanned) (warns, unscannable []string) {
	for _, pk := range []string{settings.KeyEnabledPlugins, settings.KeyExtraMarketplaces} {
		entries, err := settings.PluginEntries(doc, pk)
		if err != nil {
			unscannable = append(unscannable, string(item.TypePlugins)+"/"+pk+":")
			warns = append(warns, pk+": "+err.Error()+" — skipped")
			continue
		}
		for name, v := range entries {
			id := item.NewID(item.TypePlugins, pk+":"+name)
			if err := addValue(out, id, v); err != nil {
				unscannable = append(unscannable, string(id))
				warns = append(warns, pk+":"+name+": "+err.Error()+" — skipped")
			}
		}
	}
	return warns, unscannable
}

// scanFiles handles the layout shared by both roots: skills/, agents/,
// commands/, plus the rules file at rulesPath.
func scanFiles(dir, rulesPath string, out map[item.ID]Scanned) (warns, unscannable []string, err error) {
	skillPath := filepath.Join(dir, "skills")
	skillDirs, err := os.ReadDir(skillPath)
	if err != nil {
		if !isNotExist(err) {
			unscannable = append(unscannable, string(item.TypeSkill)+"/")
			warns = append(warns, "skills/: "+err.Error()+" — skipped")
		}
	} else {
		for _, e := range skillDirs {
			if !e.IsDir() {
				continue
			}
			p := filepath.Join(skillPath, e.Name())
			h, err := hash.Tree(p)
			if err != nil {
				id := item.NewID(item.TypeSkill, e.Name())
				unscannable = append(unscannable, string(id))
				warns = append(warns, string(id)+": "+err.Error()+" — skipped")
				continue
			}
			id := item.NewID(item.TypeSkill, e.Name())
			out[id] = Scanned{ID: id, Hash: h, Path: p}
		}
	}
	for sub, t := range map[string]item.Type{"agents": item.TypeAgent, "commands": item.TypeCommand} {
		subPath := filepath.Join(dir, sub)
		entries, err := os.ReadDir(subPath)
		if err != nil {
			if !isNotExist(err) {
				unscannable = append(unscannable, string(t)+"/")
				warns = append(warns, sub+"/: "+err.Error()+" — skipped")
			}
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			p := filepath.Join(subPath, e.Name())
			h, err := hash.File(p)
			if err != nil {
				id := item.NewID(t, strings.TrimSuffix(e.Name(), ".md"))
				unscannable = append(unscannable, string(id))
				warns = append(warns, string(id)+": "+err.Error()+" — skipped")
				continue
			}
			id := item.NewID(t, strings.TrimSuffix(e.Name(), ".md"))
			out[id] = Scanned{ID: id, Hash: h, Path: p}
		}
	}
	if st, err := os.Stat(rulesPath); err == nil && st.Mode().IsRegular() {
		h, err := hash.File(rulesPath)
		if err != nil {
			id := item.NewID(item.TypeRules, "CLAUDE.md")
			unscannable = append(unscannable, string(id))
			warns = append(warns, "rules/CLAUDE.md: "+err.Error()+" — skipped")
		} else {
			id := item.NewID(item.TypeRules, "CLAUDE.md")
			out[id] = Scanned{ID: id, Hash: h, Path: rulesPath}
		}
	}
	return warns, unscannable, nil
}

// isNotExist checks if an error is a "not exist" error (including ErrNotExist from fs and os)
func isNotExist(err error) bool {
	return os.IsNotExist(err) || errors.Is(err, fs.ErrNotExist)
}

func addValue(out map[item.ID]Scanned, id item.ID, v json.RawMessage) error {
	h, err := hash.JSONValue(v)
	if err != nil {
		return err
	}
	out[id] = Scanned{ID: id, Hash: h, Value: v}
	return nil
}
