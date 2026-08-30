// Package settings reads and writes Claude Code settings.json at the
// key level, and decides which keys are shareable across devices.
package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
)

const (
	KeyEnabledPlugins    = "enabledPlugins"
	KeyExtraMarketplaces = "extraKnownMarketplaces"
)

// DefaultShareable is the allowlist of settings keys synced by default.
// Everything else stays device-local unless the user includes it.
var DefaultShareable = []string{
	"model", "effortLevel", "permissions",
	"skipDangerousModePermissionPrompt", "skipWorkflowUsageWarning",
	"skipAutoPermissionPrompt",
}

type Doc struct {
	m map[string]json.RawMessage
}

func Load(path string) (*Doc, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return &Doc{m: map[string]json.RawMessage{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if m == nil {
		m = map[string]json.RawMessage{}
	}
	return &Doc{m: m}, nil
}

func (d *Doc) Get(key string) (json.RawMessage, bool) { v, ok := d.m[key]; return v, ok }
func (d *Doc) Set(key string, v json.RawMessage)      { d.m[key] = v }
func (d *Doc) Delete(key string)                      { delete(d.m, key) }

func (d *Doc) Keys() []string {
	ks := make([]string, 0, len(d.m))
	for k := range d.m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func (d *Doc) Save(path string) error {
	b, err := json.MarshalIndent(d.m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

type KeyOverrides struct {
	Include []string
	Exclude []string
}

func ShareableKeys(d *Doc, o KeyOverrides) []string {
	allowed := map[string]bool{}
	for _, k := range DefaultShareable {
		allowed[k] = true
	}
	for _, k := range o.Include {
		allowed[k] = true
	}
	for _, k := range o.Exclude {
		delete(allowed, k)
	}
	// plugin keys are handled as plugins items, never as settings
	delete(allowed, KeyEnabledPlugins)
	delete(allowed, KeyExtraMarketplaces)

	var out []string
	for k := range d.m {
		if allowed[k] {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func PluginEntries(d *Doc, key string) (map[string]json.RawMessage, error) {
	raw, ok := d.Get(key)
	if !ok {
		return map[string]json.RawMessage{}, nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("parse %s: %w", key, err)
	}
	if m == nil {
		m = map[string]json.RawMessage{}
	}
	return m, nil
}

func SetPluginEntry(d *Doc, key, entry string, v json.RawMessage) {
	m, err := PluginEntries(d, key)
	if err != nil {
		m = map[string]json.RawMessage{}
	}
	m[entry] = v
	b, _ := json.Marshal(m)
	d.Set(key, b)
}

func DeletePluginEntry(d *Doc, key, entry string) {
	m, err := PluginEntries(d, key)
	if err != nil {
		return
	}
	delete(m, entry)
	if len(m) == 0 {
		d.Delete(key)
		return
	}
	b, _ := json.Marshal(m)
	d.Set(key, b)
}
