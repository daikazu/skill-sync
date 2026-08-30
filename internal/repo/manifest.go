package repo

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/daikazu/skill-sync/internal/item"
)

type Manifest struct {
	Schema int                `json:"schema"`
	Items  map[item.ID]string `json:"items"`
}

func LoadManifest(root string) (*Manifest, error) {
	m := &Manifest{Schema: 1, Items: map[item.ID]string{}}
	b, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	if errors.Is(err, fs.ErrNotExist) {
		return m, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, m); err != nil {
		return nil, err
	}
	if m.Items == nil {
		m.Items = map[item.ID]string{}
	}
	return m, nil
}

func (m *Manifest) Save(root string) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, "manifest.json"), append(b, '\n'), 0o644)
}
