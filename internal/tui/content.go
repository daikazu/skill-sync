// Package tui holds the Bubble Tea interfaces: conflict review and the
// pack item picker.
package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/daikazu/skill-sync/internal/item"
	"github.com/daikazu/skill-sync/internal/scan"
)

func RenderContent(s scan.Scanned) string {
	if s.ID == "" {
		return "(absent)"
	}
	switch s.ID.Type() {
	case item.TypeSetting, item.TypePlugins:
		var v any
		if json.Unmarshal(s.Value, &v) == nil {
			if b, err := json.MarshalIndent(v, "", "  "); err == nil {
				return string(b)
			}
		}
		return string(s.Value)
	case item.TypeSkill:
		var parts []string
		filepath.WalkDir(s.Path, func(p string, d os.DirEntry, err error) error {
			if err != nil || !d.Type().IsRegular() {
				return err
			}
			rel, _ := filepath.Rel(s.Path, p)
			b, _ := os.ReadFile(p)
			parts = append(parts, "--- "+filepath.ToSlash(rel)+" ---\n"+string(b))
			return nil
		})
		sort.Strings(parts)
		return strings.Join(parts, "\n")
	default:
		b, err := os.ReadFile(s.Path)
		if err != nil {
			return "(unreadable: " + err.Error() + ")"
		}
		return string(b)
	}
}
