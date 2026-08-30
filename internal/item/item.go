// Package item defines the atomic unit of syncing, diffing, and packaging.
package item

import (
	"fmt"
	"strings"
)

type Type string

const (
	TypeSkill   Type = "skill"
	TypeAgent   Type = "agent"
	TypeCommand Type = "command"
	TypeRules   Type = "rules"
	TypeSetting Type = "setting"
	TypePlugins Type = "plugins"
)

var validTypes = map[Type]bool{
	TypeSkill: true, TypeAgent: true, TypeCommand: true,
	TypeRules: true, TypeSetting: true, TypePlugins: true,
}

// ID is "<type>/<name>", e.g. "skill/humanizer" or "plugins/enabledPlugins:foo@bar".
type ID string

func NewID(t Type, name string) ID { return ID(string(t) + "/" + name) }

func (id ID) Type() Type {
	t, _, _ := strings.Cut(string(id), "/")
	return Type(t)
}

func (id ID) Name() string {
	_, name, _ := strings.Cut(string(id), "/")
	return name
}

func Parse(s string) (ID, error) {
	t, name, ok := strings.Cut(s, "/")
	if !ok || name == "" || !validTypes[Type(t)] {
		return "", fmt.Errorf("invalid item id %q", s)
	}
	return ID(s), nil
}
