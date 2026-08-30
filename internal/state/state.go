// Package state persists the three local stores: device sync state,
// tool config, and the package-ownership ledger. All are per-device
// JSON files that never enter the sync repo.
package state

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/daikazu/skill-sync/internal/item"
)

type Policy string

const (
	PolicyNeverSync Policy = "never-sync"
	PolicyAlwaysAsk Policy = "always-ask"
)

type Device struct {
	LastSynced map[item.ID]string `json:"lastSynced"`
}

type Config struct {
	Remote      string             `json:"remote"`
	IncludeKeys []string           `json:"includeKeys,omitempty"`
	ExcludeKeys []string           `json:"excludeKeys,omitempty"`
	Policies    map[item.ID]Policy `json:"policies,omitempty"`
}

type PackageRecord struct {
	Version string             `json:"version"`
	Items   map[item.ID]string `json:"items"`
}

type Ledger struct {
	Packages map[string]PackageRecord `json:"packages"`
}

func (l *Ledger) Owner(id item.ID) (string, string, bool) {
	for name, rec := range l.Packages {
		if h, ok := rec.Items[id]; ok {
			return name, h, true
		}
	}
	return "", "", false
}

func LoadDevice(path string) (*Device, error) {
	d := &Device{LastSynced: map[item.ID]string{}}
	return d, load(path, d)
}
func (d *Device) Save(path string) error { return save(path, d) }

func LoadConfig(path string) (*Config, error) {
	c := &Config{}
	return c, load(path, c)
}
func (c *Config) Save(path string) error { return save(path, c) }

func LoadLedger(path string) (*Ledger, error) {
	l := &Ledger{Packages: map[string]PackageRecord{}}
	return l, load(path, l)
}
func (l *Ledger) Save(path string) error { return save(path, l) }

func load(path string, v any) error {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func save(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}
