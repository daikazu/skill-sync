package item

import "testing"

func TestNewIDAndAccessors(t *testing.T) {
	cases := []struct {
		typ  Type
		name string
		want string
	}{
		{TypeSkill, "humanizer", "skill/humanizer"},
		{TypeAgent, "php-pro", "agent/php-pro"},
		{TypeCommand, "security-review", "command/security-review"},
		{TypeRules, "CLAUDE.md", "rules/CLAUDE.md"},
		{TypeSetting, "model", "setting/model"},
		{TypePlugins, "enabledPlugins:foo@bar", "plugins/enabledPlugins:foo@bar"},
	}
	for _, c := range cases {
		id := NewID(c.typ, c.name)
		if string(id) != c.want {
			t.Fatalf("NewID(%s,%s)=%s want %s", c.typ, c.name, id, c.want)
		}
		if id.Type() != c.typ {
			t.Fatalf("%s Type()=%s want %s", id, id.Type(), c.typ)
		}
		if id.Name() != c.name {
			t.Fatalf("%s Name()=%s want %s", id, id.Name(), c.name)
		}
	}
}

func TestParse(t *testing.T) {
	id, err := Parse("skill/humanizer")
	if err != nil || id != NewID(TypeSkill, "humanizer") {
		t.Fatalf("Parse: %v %v", id, err)
	}
	for _, bad := range []string{"", "skill", "bogus/x", "skill/"} {
		if _, err := Parse(bad); err == nil {
			t.Fatalf("Parse(%q) should fail", bad)
		}
	}
}
