# skill-sync Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `skill-sync`, a Go CLI+TUI that 3-way-syncs Claude Code skills/agents/commands/rules/settings between machines over a private git repo, and builds/installs `.skillpack` packages with ownership tracking.

**Architecture:** Manifest-driven engine: everything is an *item* (skill dir, agent/command/rules file, one settings key, one plugin entry) identified by a stable ID and a SHA-256 content hash. A local device-state file holds last-synced hashes (the 3-way base); classification per item drives an auto-apply plan plus a conflict queue reviewed in a Bubble Tea TUI. The git repo is a dumb versioned store the tool wraps entirely.

**Tech Stack:** Go 1.22+, cobra (CLI), charmbracelet bubbletea+bubbles+lipgloss (TUI), charmbracelet/huh (prompt forms), aymanbagabas/go-udiff (diffs), system `git` via os/exec, GoReleaser + GitHub Actions → `daikazu/homebrew-tap`.

**Spec:** `docs/superpowers/specs/2026-08-30-skill-sync-design.md`

## Global Constraints

- Module path: `github.com/daikazu/skill-sync`. Binary name: `skill-sync`.
- All engine packages take explicit root paths (no globals); tests use `t.TempDir()`. No mocking libraries.
- Item ID strings: `skill/<name>`, `agent/<name>`, `command/<name>`, `rules/CLAUDE.md`, `setting/<key>`, `plugins/enabledPlugins:<entry>`, `plugins/extraKnownMarketplaces:<entry>`.
- Default shareable settings keys: `model`, `effortLevel`, `permissions`, `skipDangerousModePermissionPrompt`, `skipWorkflowUsageWarning`, `skipAutoPermissionPrompt`. Default excluded: everything else (allowlist model). `enabledPlugins`/`extraKnownMarketplaces` are never setting items — they expand into plugins entry items.
- Sync repo layout: `skills/<name>/...`, `agents/<name>.md`, `commands/<name>.md`, `rules/CLAUDE.md`, `settings.json` (shareable keys only), `plugins.json` (`{"enabledPlugins":{...},"extraKnownMarketplaces":{...}}`), `manifest.json`.
- Local dirs: sync clone `~/.claude-sync/repo/`, device files `~/.claude-sync/{state,config,ledger}.json`, snapshots `~/.claude/backups/skill-sync/<RFC3339 timestamp>/`.
- Never force-push. Never write to `~/.claude` without snapshotting affected files first. Reject tar entries that are absolute, contain `..`, or are symlinks.
- Commit after every green test cycle. TDD for all engine packages; TUI models get message-driven unit tests.

---

### Task 1: Scaffold + item package

**Files:**
- Create: `go.mod`, `main.go`, `cmd/root.go`
- Create: `internal/item/item.go`
- Test: `internal/item/item_test.go`

**Interfaces:**
- Consumes: nothing (first task)
- Produces: `item.Type` (string enum: `TypeSkill/TypeAgent/TypeCommand/TypeRules/TypeSetting/TypePlugins`), `item.ID` (string), `item.NewID(t Type, name string) ID`, `(ID).Type() Type`, `(ID).Name() string`, `item.Parse(s string) (ID, error)`. For plugins IDs, `name` is `enabledPlugins:<entry>` or `extraKnownMarketplaces:<entry>`.

- [ ] **Step 1: Scaffold module**

```bash
cd /Users/mikewall/Code/skill-sync
go mod init github.com/daikazu/skill-sync
go get github.com/spf13/cobra@latest
```

`main.go`:
```go
package main

import "github.com/daikazu/skill-sync/cmd"

func main() { cmd.Execute() }
```

`cmd/root.go`:
```go
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "skill-sync",
	Short: "Sync Claude Code skills, agents, commands, rules, and settings between machines",
	SilenceUsage: true,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
```

- [ ] **Step 2: Write failing tests for item IDs**

`internal/item/item_test.go`:
```go
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
```

- [ ] **Step 3: Run to verify failure** — `go test ./internal/item/` → FAIL (undefined symbols).

- [ ] **Step 4: Implement**

`internal/item/item.go`:
```go
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
```

- [ ] **Step 5: Run tests** — `go test ./...` → PASS. Also `go build ./...` and `./skill-sync --help` via `go run . --help` prints usage.

- [ ] **Step 6: Commit** — `git add -A && git commit -m "feat: scaffold module and item ID model"`

---

### Task 2: hash package

**Files:**
- Create: `internal/hash/hash.go`
- Test: `internal/hash/hash_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: `hash.File(path string) (string, error)`; `hash.Tree(root string) (string, error)`; `hash.JSONValue(raw json.RawMessage) (string, error)`. All return lowercase hex SHA-256. `Tree` walks regular files only, sorted by slash-separated relative path, hashing `path\n<filehash>\n` lines. `JSONValue` canonicalizes (objects get sorted keys, recursively; numbers/strings via encoding/json round-trip) before hashing.

- [ ] **Step 1: Write failing tests**

`internal/hash/hash_test.go`:
```go
package hash

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f.txt")
	os.WriteFile(p, []byte("hello"), 0o644)
	h1, err := File(p)
	if err != nil || len(h1) != 64 {
		t.Fatalf("File: %q %v", h1, err)
	}
	os.WriteFile(p, []byte("hello!"), 0o644)
	h2, _ := File(p)
	if h1 == h2 {
		t.Fatal("different content, same hash")
	}
}

func TestTreeDeterministicAndContentSensitive(t *testing.T) {
	mk := func(files map[string]string) string {
		d := t.TempDir()
		for rel, content := range files {
			p := filepath.Join(d, rel)
			os.MkdirAll(filepath.Dir(p), 0o755)
			os.WriteFile(p, []byte(content), 0o644)
		}
		return d
	}
	a := mk(map[string]string{"SKILL.md": "x", "ref/notes.md": "y"})
	b := mk(map[string]string{"ref/notes.md": "y", "SKILL.md": "x"})
	ha, _ := Tree(a)
	hb, _ := Tree(b)
	if ha != hb {
		t.Fatal("same content should hash equal regardless of creation order")
	}
	c := mk(map[string]string{"SKILL.md": "x", "ref/notes.md": "CHANGED"})
	hc, _ := Tree(c)
	if hc == ha {
		t.Fatal("changed nested file must change tree hash")
	}
	d := mk(map[string]string{"SKILL.md": "x", "renamed.md": "y"})
	hd, _ := Tree(d)
	if hd == ha {
		t.Fatal("renamed file must change tree hash")
	}
}

func TestJSONValueCanonical(t *testing.T) {
	h1, _ := JSONValue(json.RawMessage(`{"b":1,"a":{"z":true,"y":"s"}}`))
	h2, _ := JSONValue(json.RawMessage(`{ "a": {"y":"s","z":true}, "b": 1 }`))
	if h1 != h2 {
		t.Fatal("key order / whitespace must not affect hash")
	}
	h3, _ := JSONValue(json.RawMessage(`{"b":2,"a":{"z":true,"y":"s"}}`))
	if h3 == h1 {
		t.Fatal("value change must change hash")
	}
}
```

- [ ] **Step 2: Run to verify failure** — `go test ./internal/hash/` → FAIL.

- [ ] **Step 3: Implement**

`internal/hash/hash.go`:
```go
// Package hash computes deterministic SHA-256 content hashes for items.
package hash

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Tree hashes a directory: sorted relative paths, each contributing
// "<path>\n<filehash>\n". Regular files only; empty dirs are ignored.
func Tree(root string) (string, error) {
	var lines []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		fh, err := File(p)
		if err != nil {
			return err
		}
		lines = append(lines, filepath.ToSlash(rel)+"\n"+fh+"\n")
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "")))
	return hex.EncodeToString(sum[:]), nil
}

func JSONValue(raw json.RawMessage) (string, error) {
	c, err := Canonical(raw)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(c)
	return hex.EncodeToString(sum[:]), nil
}

// Canonical re-marshals JSON with sorted object keys and no insignificant
// whitespace, recursively.
func Canonical(raw json.RawMessage) ([]byte, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("canonicalize: %w", err)
	}
	return json.Marshal(sortValue(v))
}

// sortValue relies on encoding/json marshaling map[string]any keys in
// sorted order; it exists to normalize nested structures uniformly.
func sortValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		m := make(map[string]any, len(t))
		for k, vv := range t {
			m[k] = sortValue(vv)
		}
		return m
	case []any:
		for i := range t {
			t[i] = sortValue(t[i])
		}
		return t
	default:
		return v
	}
}
```

- [ ] **Step 4: Run tests** — `go test ./internal/hash/` → PASS.

- [ ] **Step 5: Commit** — `git add -A && git commit -m "feat: content hashing (file, tree, canonical JSON)"`

---

### Task 3: settings package

**Files:**
- Create: `internal/settings/settings.go`
- Test: `internal/settings/settings_test.go`

**Interfaces:**
- Consumes: nothing
- Produces:
  - `settings.Doc` — ordered-agnostic JSON object wrapper: `Load(path string) (*Doc, error)` (missing file → empty doc), `(d *Doc).Get(key string) (json.RawMessage, bool)`, `(d *Doc).Set(key string, v json.RawMessage)`, `(d *Doc).Delete(key string)`, `(d *Doc).Keys() []string` (sorted), `(d *Doc).Save(path string) error` (2-space indent, sorted keys, trailing newline).
  - `settings.KeyOverrides{Include []string; Exclude []string}`
  - `settings.ShareableKeys(d *Doc, o KeyOverrides) []string` — sorted keys of `d` that are in (DefaultShareable ∪ o.Include) minus o.Exclude, and never `enabledPlugins`/`extraKnownMarketplaces`.
  - Constants `KeyEnabledPlugins = "enabledPlugins"`, `KeyExtraMarketplaces = "extraKnownMarketplaces"`.
  - `settings.PluginEntries(d *Doc, key string) (map[string]json.RawMessage, error)` — object entries of that key (missing key → empty map).
  - `settings.SetPluginEntry(d *Doc, key, entry string, v json.RawMessage)` and `settings.DeletePluginEntry(d *Doc, key, entry string)` — mutate the nested object, creating/pruning the top-level key as needed.

- [ ] **Step 1: Write failing tests**

`internal/settings/settings_test.go`:
```go
package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func write(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "settings.json")
	os.WriteFile(p, []byte(content), 0o644)
	return p
}

func TestLoadMissingIsEmpty(t *testing.T) {
	d, err := Load(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil || len(d.Keys()) != 0 {
		t.Fatalf("missing file should load empty: %v", err)
	}
}

func TestRoundTripPreservesUnknownKeys(t *testing.T) {
	p := write(t, `{"model":"opus","env":{"FOO":"bar"},"tui":{"theme":"dark"}}`)
	d, _ := Load(p)
	d.Set("model", json.RawMessage(`"fable"`))
	if err := d.Save(p); err != nil {
		t.Fatal(err)
	}
	d2, _ := Load(p)
	if v, _ := d2.Get("model"); string(v) != `"fable"` {
		t.Fatalf("model=%s", v)
	}
	if _, ok := d2.Get("env"); !ok {
		t.Fatal("unknown key env must survive round-trip")
	}
}

func TestShareableKeys(t *testing.T) {
	p := write(t, `{"model":"m","effortLevel":"high","env":{},"statusLine":{},"enabledPlugins":{"a@b":true},"customThing":1}`)
	d, _ := Load(p)
	got := ShareableKeys(d, KeyOverrides{})
	want := []string{"effortLevel", "model"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	got = ShareableKeys(d, KeyOverrides{Include: []string{"customThing"}, Exclude: []string{"model"}})
	want = []string{"customThing", "effortLevel"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("overrides: got %v want %v", got, want)
	}
	// plugin keys can never be settings items, even if included
	got = ShareableKeys(d, KeyOverrides{Include: []string{"enabledPlugins"}})
	if len(got) != 2 {
		t.Fatalf("enabledPlugins must never be shareable as a setting: %v", got)
	}
}

func TestPluginEntries(t *testing.T) {
	p := write(t, `{"enabledPlugins":{"x@m":true,"y@m":false}}`)
	d, _ := Load(p)
	e, err := PluginEntries(d, KeyEnabledPlugins)
	if err != nil || len(e) != 2 || string(e["x@m"]) != "true" {
		t.Fatalf("entries: %v %v", e, err)
	}
	e2, err := PluginEntries(d, KeyExtraMarketplaces)
	if err != nil || len(e2) != 0 {
		t.Fatalf("missing key should be empty map: %v %v", e2, err)
	}
	SetPluginEntry(d, KeyExtraMarketplaces, "mp", json.RawMessage(`{"source":"github"}`))
	e3, _ := PluginEntries(d, KeyExtraMarketplaces)
	if string(e3["mp"]) != `{"source":"github"}` {
		t.Fatalf("SetPluginEntry: %v", e3)
	}
	DeletePluginEntry(d, KeyEnabledPlugins, "x@m")
	e4, _ := PluginEntries(d, KeyEnabledPlugins)
	if _, ok := e4["x@m"]; ok {
		t.Fatal("DeletePluginEntry failed")
	}
}
```

- [ ] **Step 2: Run to verify failure** — `go test ./internal/settings/` → FAIL.

- [ ] **Step 3: Implement**

`internal/settings/settings.go`:
```go
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
```

- [ ] **Step 4: Run tests** — `go test ./internal/settings/` → PASS.

- [ ] **Step 5: Commit** — `git add -A && git commit -m "feat: settings.json key-level access and shareable-key filtering"`

---

### Task 4: scan package

**Files:**
- Create: `internal/scan/scan.go`
- Test: `internal/scan/scan_test.go`

**Interfaces:**
- Consumes: `item` (Task 1), `hash` (Task 2), `settings` (Task 3)
- Produces:
  - `scan.Scanned{ID item.ID; Hash string; Path string; Value json.RawMessage}` — `Path` set (absolute) for skill/agent/command/rules items; `Value` set for setting and plugins-entry items.
  - `scan.Claude(dir string, o settings.KeyOverrides) (map[item.ID]Scanned, []string, error)` — inventories a `~/.claude`-shaped dir: `skills/*/` (dirs only), `agents/*.md`, `commands/*.md`, `CLAUDE.md` → `rules/CLAUDE.md`, shareable settings keys → setting items, both plugin keys' entries → plugins items. Missing subdirs/files are fine (empty portions). The `[]string` return is warnings: an unparseable `settings.json` yields a warning and settings/plugins items are simply omitted (spec: flag and skip, never abort the sync); the error return is for real I/O failures only.
  - `scan.Repo(dir string) (map[item.ID]Scanned, []string, error)` — inventories a sync-repo-shaped dir: same for skills/agents/commands, `rules/CLAUDE.md` file, ALL keys in `settings.json` become setting items (the repo only ever holds shareable keys), entries of both objects in `plugins.json` become plugins items. Same warnings semantics.
  - Setting/plugins hashes use `hash.JSONValue`; file items `hash.File`; skills `hash.Tree`.

- [ ] **Step 1: Write failing tests**

`internal/scan/scan_test.go`:
```go
package scan

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/daikazu/skill-sync/internal/item"
	"github.com/daikazu/skill-sync/internal/settings"
)

func mkClaude(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	os.MkdirAll(filepath.Join(d, "skills/humanizer"), 0o755)
	os.WriteFile(filepath.Join(d, "skills/humanizer/SKILL.md"), []byte("# h"), 0o644)
	os.MkdirAll(filepath.Join(d, "agents"), 0o755)
	os.WriteFile(filepath.Join(d, "agents/php-pro.md"), []byte("a"), 0o644)
	os.MkdirAll(filepath.Join(d, "commands"), 0o755)
	os.WriteFile(filepath.Join(d, "commands/counselors.md"), []byte("c"), 0o644)
	os.WriteFile(filepath.Join(d, "CLAUDE.md"), []byte("rules"), 0o644)
	os.WriteFile(filepath.Join(d, "settings.json"),
		[]byte(`{"model":"opus","env":{"X":"1"},"enabledPlugins":{"p@m":true}}`), 0o644)
	return d
}

func TestClaudeInventory(t *testing.T) {
	m, warns, err := Claude(mkClaude(t), settings.KeyOverrides{})
	if err != nil || len(warns) != 0 {
		t.Fatal(err, warns)
	}
	for _, want := range []string{
		"skill/humanizer", "agent/php-pro", "command/counselors",
		"rules/CLAUDE.md", "setting/model", "plugins/enabledPlugins:p@m",
	} {
		s, ok := m[item.ID(want)]
		if !ok {
			t.Fatalf("missing %s in %v", want, keys(m))
		}
		if s.Hash == "" {
			t.Fatalf("%s has empty hash", want)
		}
	}
	if _, ok := m[item.ID("setting/env")]; ok {
		t.Fatal("env is not shareable, must not be scanned")
	}
	if _, ok := m[item.ID("setting/enabledPlugins")]; ok {
		t.Fatal("enabledPlugins must not appear as a setting item")
	}
}

func TestClaudeEmptyDir(t *testing.T) {
	m, _, err := Claude(t.TempDir(), settings.KeyOverrides{})
	if err != nil || len(m) != 0 {
		t.Fatalf("empty dir: %v %v", m, err)
	}
}

func TestClaudeUnparseableSettingsWarnsAndSkips(t *testing.T) {
	d := mkClaude(t)
	os.WriteFile(filepath.Join(d, "settings.json"), []byte(`{not json`), 0o644)
	m, warns, err := Claude(d, settings.KeyOverrides{})
	if err != nil {
		t.Fatalf("bad settings must not abort scan: %v", err)
	}
	if len(warns) == 0 {
		t.Fatal("expected a warning about settings.json")
	}
	if _, ok := m[item.ID("skill/humanizer")]; !ok {
		t.Fatal("file items must still be scanned")
	}
	if _, ok := m[item.ID("setting/model")]; ok {
		t.Fatal("settings items must be skipped when unparseable")
	}
}

func TestRepoInventory(t *testing.T) {
	d := t.TempDir()
	os.MkdirAll(filepath.Join(d, "skills/humanizer"), 0o755)
	os.WriteFile(filepath.Join(d, "skills/humanizer/SKILL.md"), []byte("# h"), 0o644)
	os.MkdirAll(filepath.Join(d, "rules"), 0o755)
	os.WriteFile(filepath.Join(d, "rules/CLAUDE.md"), []byte("rules"), 0o644)
	os.WriteFile(filepath.Join(d, "settings.json"), []byte(`{"model":"opus"}`), 0o644)
	os.WriteFile(filepath.Join(d, "plugins.json"),
		[]byte(`{"enabledPlugins":{"p@m":true},"extraKnownMarketplaces":{}}`), 0o644)
	m, _, err := Repo(d)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"skill/humanizer", "rules/CLAUDE.md", "setting/model", "plugins/enabledPlugins:p@m",
	} {
		if _, ok := m[item.ID(want)]; !ok {
			t.Fatalf("missing %s in %v", want, keys(m))
		}
	}
}

func TestSameContentHashesEqualAcrossClaudeAndRepo(t *testing.T) {
	c := mkClaude(t)
	r := t.TempDir()
	os.MkdirAll(filepath.Join(r, "skills/humanizer"), 0o755)
	os.WriteFile(filepath.Join(r, "skills/humanizer/SKILL.md"), []byte("# h"), 0o644)
	os.WriteFile(filepath.Join(r, "settings.json"), []byte(`{"model": "opus"}`), 0o644)
	cm, _, _ := Claude(c, settings.KeyOverrides{})
	rm, _, _ := Repo(r)
	if cm[item.ID("skill/humanizer")].Hash != rm[item.ID("skill/humanizer")].Hash {
		t.Fatal("identical skill must hash equal in both layouts")
	}
	if cm[item.ID("setting/model")].Hash != rm[item.ID("setting/model")].Hash {
		t.Fatal("identical setting must hash equal despite whitespace")
	}
}

func keys(m map[item.ID]Scanned) []item.ID {
	var out []item.ID
	for k := range m {
		out = append(out, k)
	}
	return out
}
```

- [ ] **Step 2: Run to verify failure** — `go test ./internal/scan/` → FAIL.

- [ ] **Step 3: Implement**

`internal/scan/scan.go`:
```go
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
```

- [ ] **Step 4: Run tests** — `go test ./internal/scan/` → PASS.

- [ ] **Step 5: Commit** — `git add -A && git commit -m "feat: item inventory scanning for claude dir and sync repo"`

---

### Task 5: state package (device state, config, ledger)

**Files:**
- Create: `internal/state/state.go`
- Test: `internal/state/state_test.go`

**Interfaces:**
- Consumes: `item` (Task 1)
- Produces:
  - `state.Device{LastSynced map[item.ID]string}`, `LoadDevice(path string) (*Device, error)` (missing → empty), `(d *Device).Save(path string) error`.
  - `state.Policy` string type: `PolicyNeverSync = "never-sync"`, `PolicyAlwaysAsk = "always-ask"`.
  - `state.Config{Remote string; IncludeKeys, ExcludeKeys []string; Policies map[item.ID]Policy}`, `LoadConfig(path)/Save` same pattern.
  - `state.Ledger{Packages map[string]PackageRecord}`; `PackageRecord{Version string; Items map[item.ID]string}` (hash at install time); `LoadLedger(path)/Save`; `(l *Ledger).Owner(id item.ID) (pkg string, installedHash string, ok bool)`.
  - All Save calls create parent dirs (`os.MkdirAll`) and write with 0o644, files as indented JSON.

- [ ] **Step 1: Write failing tests**

`internal/state/state_test.go`:
```go
package state

import (
	"path/filepath"
	"testing"

	"github.com/daikazu/skill-sync/internal/item"
)

func TestDeviceRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "nested", "state.json")
	d, err := LoadDevice(p)
	if err != nil || len(d.LastSynced) != 0 {
		t.Fatalf("missing should be empty: %v", err)
	}
	d.LastSynced[item.ID("skill/x")] = "abc"
	if err := d.Save(p); err != nil {
		t.Fatal(err)
	}
	d2, _ := LoadDevice(p)
	if d2.LastSynced[item.ID("skill/x")] != "abc" {
		t.Fatal("round trip failed")
	}
}

func TestConfigRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	c, _ := LoadConfig(p)
	c.Remote = "git@github.com:daikazu/claude-sync.git"
	c.Policies = map[item.ID]Policy{"skill/x": PolicyNeverSync}
	c.Save(p)
	c2, _ := LoadConfig(p)
	if c2.Remote != c.Remote || c2.Policies["skill/x"] != PolicyNeverSync {
		t.Fatal("round trip failed")
	}
}

func TestLedgerOwner(t *testing.T) {
	p := filepath.Join(t.TempDir(), "ledger.json")
	l, _ := LoadLedger(p)
	l.Packages["agency-tools"] = PackageRecord{
		Version: "1.0.0",
		Items:   map[item.ID]string{"skill/code-review": "h1"},
	}
	l.Save(p)
	l2, _ := LoadLedger(p)
	pkg, h, ok := l2.Owner(item.ID("skill/code-review"))
	if !ok || pkg != "agency-tools" || h != "h1" {
		t.Fatalf("owner: %s %s %v", pkg, h, ok)
	}
	if _, _, ok := l2.Owner(item.ID("skill/other")); ok {
		t.Fatal("unowned item reported owned")
	}
}
```

- [ ] **Step 2: Run to verify failure** — `go test ./internal/state/` → FAIL.

- [ ] **Step 3: Implement**

`internal/state/state.go`:
```go
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
```

- [ ] **Step 4: Run tests** — `go test ./internal/state/` → PASS.

- [ ] **Step 5: Commit** — `git add -A && git commit -m "feat: device state, config, and ownership ledger stores"`

---

### Task 6: classify package (3-way engine)

**Files:**
- Create: `internal/classify/classify.go`
- Test: `internal/classify/classify_test.go`

**Interfaces:**
- Consumes: `item` (Task 1), `scan.Scanned` (Task 4)
- Produces:
  - `classify.State` string enum: `InSync`, `Push`, `Pull`, `NewLocal`, `NewRemote`, `DeletedLocal`, `DeletedRemote`, `Conflict`, `ConflictBothNew`, `ConflictDeleteModify`.
  - `classify.Result{ID item.ID; State State; Local, Base, Remote string}` — hashes, `""` meaning absent.
  - `classify.All(local, remote map[item.ID]scan.Scanned, base map[item.ID]string) []Result` — covers the union of IDs across all three maps, sorted by ID. Items absent from all three never appear. Rules:
    - L==R (both present): `InSync` (regardless of base — base catches up on apply).
    - L,R present, L≠R: base==R → `Push`; base==L → `Pull`; base absent → `ConflictBothNew`; else → `Conflict`.
    - L present, R absent: base absent → `NewLocal`; base==L → `DeletedRemote`; else → `ConflictDeleteModify`.
    - L absent, R present: base absent → `NewRemote`; base==R → `DeletedLocal`; else → `ConflictDeleteModify`.
    - L,R absent, base present → dropped (stale base entry; caller prunes on apply).

- [ ] **Step 1: Write failing table-driven test**

`internal/classify/classify_test.go`:
```go
package classify

import (
	"testing"

	"github.com/daikazu/skill-sync/internal/item"
	"github.com/daikazu/skill-sync/internal/scan"
)

func TestAll(t *testing.T) {
	cases := []struct {
		name             string
		local, base, rem string // hashes; "" = absent
		want             State
	}{
		{"in sync", "a", "a", "a", InSync},
		{"in sync no base", "a", "", "a", InSync},
		{"local edit", "b", "a", "a", Push},
		{"remote edit", "a", "a", "b", Pull},
		{"both edited differently", "b", "a", "c", Conflict},
		{"both new same name diff content", "b", "", "c", ConflictBothNew},
		{"new local", "a", "", "", NewLocal},
		{"new remote", "", "", "a", NewRemote},
		{"deleted remotely, unchanged here", "a", "a", "", DeletedRemote},
		{"deleted locally, unchanged remote", "", "a", "a", DeletedLocal},
		{"modified locally but deleted remotely", "b", "a", "", ConflictDeleteModify},
		{"deleted locally but modified remotely", "", "a", "b", ConflictDeleteModify},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			id := item.ID("skill/x")
			local := map[item.ID]scan.Scanned{}
			remote := map[item.ID]scan.Scanned{}
			base := map[item.ID]string{}
			if c.local != "" {
				local[id] = scan.Scanned{ID: id, Hash: c.local}
			}
			if c.rem != "" {
				remote[id] = scan.Scanned{ID: id, Hash: c.rem}
			}
			if c.base != "" {
				base[id] = c.base
			}
			rs := All(local, remote, base)
			if len(rs) != 1 {
				t.Fatalf("want 1 result, got %v", rs)
			}
			if rs[0].State != c.want {
				t.Fatalf("got %s want %s", rs[0].State, c.want)
			}
		})
	}
}

func TestStaleBaseEntryDropped(t *testing.T) {
	base := map[item.ID]string{item.ID("skill/gone"): "a"}
	rs := All(map[item.ID]scan.Scanned{}, map[item.ID]scan.Scanned{}, base)
	if len(rs) != 0 {
		t.Fatalf("stale base entry must produce no result: %v", rs)
	}
}

func TestSortedOutput(t *testing.T) {
	local := map[item.ID]scan.Scanned{
		item.ID("skill/b"): {ID: item.ID("skill/b"), Hash: "x"},
		item.ID("agent/a"): {ID: item.ID("agent/a"), Hash: "y"},
	}
	rs := All(local, map[item.ID]scan.Scanned{}, map[item.ID]string{})
	if len(rs) != 2 || rs[0].ID != item.ID("agent/a") {
		t.Fatalf("results must be sorted by ID: %v", rs)
	}
}
```

- [ ] **Step 2: Run to verify failure** — `go test ./internal/classify/` → FAIL.

- [ ] **Step 3: Implement**

`internal/classify/classify.go`:
```go
// Package classify computes the per-item 3-way state between the local
// machine, the sync repo, and the last-synced base recorded on device.
package classify

import (
	"sort"

	"github.com/daikazu/skill-sync/internal/item"
	"github.com/daikazu/skill-sync/internal/scan"
)

type State string

const (
	InSync               State = "in-sync"
	Push                 State = "push"
	Pull                 State = "pull"
	NewLocal             State = "new-local"
	NewRemote            State = "new-remote"
	DeletedLocal         State = "deleted-local"
	DeletedRemote        State = "deleted-remote"
	Conflict             State = "conflict"
	ConflictBothNew      State = "conflict-both-new"
	ConflictDeleteModify State = "conflict-delete-modify"
)

// IsConflict reports whether the state requires a human decision.
func (s State) IsConflict() bool {
	return s == Conflict || s == ConflictBothNew || s == ConflictDeleteModify
}

type Result struct {
	ID     item.ID
	State  State
	Local  string // hash, "" = absent
	Base   string
	Remote string
}

func All(local, remote map[item.ID]scan.Scanned, base map[item.ID]string) []Result {
	ids := map[item.ID]bool{}
	for id := range local {
		ids[id] = true
	}
	for id := range remote {
		ids[id] = true
	}
	for id := range base {
		ids[id] = true
	}
	var out []Result
	for id := range ids {
		l := local[id].Hash
		r := remote[id].Hash
		b := base[id]
		st, ok := one(l, b, r)
		if !ok {
			continue
		}
		out = append(out, Result{ID: id, State: st, Local: l, Base: b, Remote: r})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func one(l, b, r string) (State, bool) {
	switch {
	case l != "" && r != "":
		if l == r {
			return InSync, true
		}
		switch b {
		case r:
			return Push, true
		case l:
			return Pull, true
		case "":
			return ConflictBothNew, true
		default:
			return Conflict, true
		}
	case l != "": // remote absent
		switch b {
		case "":
			return NewLocal, true
		case l:
			return DeletedRemote, true
		default:
			return ConflictDeleteModify, true
		}
	case r != "": // local absent
		switch b {
		case "":
			return NewRemote, true
		case r:
			return DeletedLocal, true
		default:
			return ConflictDeleteModify, true
		}
	default: // stale base entry
		return "", false
	}
}
```

- [ ] **Step 4: Run tests** — `go test ./internal/classify/` → PASS.

- [ ] **Step 5: Commit** — `git add -A && git commit -m "feat: 3-way classification engine"`

---

### Task 7: plan package

**Files:**
- Create: `internal/plan/plan.go`
- Test: `internal/plan/plan_test.go`

**Interfaces:**
- Consumes: `classify` (Task 6), `state` (Task 5), `item` (Task 1)
- Produces:
  - `plan.Action` string enum: `ActPull` ("pull" = write remote content locally), `ActPush` ("push" = write local content to repo), `ActDeleteLocal`, `ActDeleteRemote`, `ActBaseOnly` (just record base hash, used for InSync with stale base).
  - `plan.Change{Result classify.Result; Action Action}`
  - `plan.Plan{Auto []Change; Conflicts []classify.Result; Skipped []classify.Result}` — Skipped holds never-sync/package-owned items for status display.
  - `plan.Build(results []classify.Result, cfg *state.Config, ledger *state.Ledger) Plan`:
    - package-owned item (ledger.Owner ok) → Skipped, regardless of state.
    - `PolicyNeverSync` → Skipped.
    - `PolicyAlwaysAsk` → Conflicts (even one-sided changes).
    - conflict states → Conflicts. `InSync` → Auto/ActBaseOnly. One-sided states map: Push→ActPush, Pull→ActPull, NewLocal→ActPush, NewRemote→ActPull, DeletedRemote→ActDeleteLocal, DeletedLocal→ActDeleteRemote.
  - `plan.Resolution` string enum: `ResLocal`, `ResRemote`, `ResSkip`.
  - `plan.Resolve(p Plan, choices map[item.ID]Resolution) []Change` — returns p.Auto plus resolved conflicts: `ResLocal` → local side wins (local content pushed, or remote deletion when local absent: Local=="" → ActDeleteRemote, else ActPush); `ResRemote` → mirror (Remote=="" → ActDeleteLocal, else ActPull); `ResSkip`/missing → omitted.

- [ ] **Step 1: Write failing tests**

`internal/plan/plan_test.go`:
```go
package plan

import (
	"testing"

	"github.com/daikazu/skill-sync/internal/classify"
	"github.com/daikazu/skill-sync/internal/item"
	"github.com/daikazu/skill-sync/internal/state"
)

func res(id string, st classify.State, l, b, r string) classify.Result {
	return classify.Result{ID: item.ID(id), State: st, Local: l, Base: b, Remote: r}
}

func emptyCfg() *state.Config { return &state.Config{} }
func emptyLedger() *state.Ledger {
	return &state.Ledger{Packages: map[string]state.PackageRecord{}}
}

func TestBuildRouting(t *testing.T) {
	results := []classify.Result{
		res("agent/a", classify.Push, "b", "a", "a"),
		res("agent/b", classify.NewRemote, "", "", "x"),
		res("agent/c", classify.DeletedRemote, "a", "a", ""),
		res("agent/d", classify.Conflict, "b", "a", "c"),
		res("agent/e", classify.InSync, "a", "", "a"),
	}
	p := Build(results, emptyCfg(), emptyLedger())
	if len(p.Conflicts) != 1 || p.Conflicts[0].ID != item.ID("agent/d") {
		t.Fatalf("conflicts: %v", p.Conflicts)
	}
	actions := map[item.ID]Action{}
	for _, c := range p.Auto {
		actions[c.Result.ID] = c.Action
	}
	want := map[item.ID]Action{
		"agent/a": ActPush, "agent/b": ActPull,
		"agent/c": ActDeleteLocal, "agent/e": ActBaseOnly,
	}
	for id, w := range want {
		if actions[id] != w {
			t.Fatalf("%s: got %s want %s", id, actions[id], w)
		}
	}
}

func TestBuildPolicies(t *testing.T) {
	results := []classify.Result{
		res("skill/never", classify.Push, "b", "a", "a"),
		res("skill/ask", classify.Pull, "a", "a", "b"),
	}
	cfg := &state.Config{Policies: map[item.ID]state.Policy{
		"skill/never": state.PolicyNeverSync,
		"skill/ask":   state.PolicyAlwaysAsk,
	}}
	p := Build(results, cfg, emptyLedger())
	if len(p.Skipped) != 1 || p.Skipped[0].ID != item.ID("skill/never") {
		t.Fatalf("skipped: %v", p.Skipped)
	}
	if len(p.Conflicts) != 1 || p.Conflicts[0].ID != item.ID("skill/ask") {
		t.Fatalf("always-ask must route to conflicts: %v", p.Conflicts)
	}
}

func TestBuildPackageOwnedExcluded(t *testing.T) {
	results := []classify.Result{res("skill/team", classify.Push, "b", "a", "a")}
	led := &state.Ledger{Packages: map[string]state.PackageRecord{
		"agency": {Version: "1.0.0", Items: map[item.ID]string{"skill/team": "a"}},
	}}
	p := Build(results, emptyCfg(), led)
	if len(p.Auto) != 0 || len(p.Skipped) != 1 {
		t.Fatalf("package-owned must be skipped: %+v", p)
	}
}

func TestResolve(t *testing.T) {
	p := Plan{
		Auto: []Change{{Result: res("agent/auto", classify.Push, "b", "a", "a"), Action: ActPush}},
		Conflicts: []classify.Result{
			res("agent/mine", classify.Conflict, "b", "a", "c"),
			res("agent/theirs", classify.Conflict, "b", "a", "c"),
			res("agent/skip", classify.Conflict, "b", "a", "c"),
			res("agent/delmod", classify.ConflictDeleteModify, "", "a", "b"),
		},
	}
	got := Resolve(p, map[item.ID]Resolution{
		"agent/mine":   ResLocal,
		"agent/theirs": ResRemote,
		"agent/delmod": ResLocal,
	})
	acts := map[item.ID]Action{}
	for _, c := range got {
		acts[c.Result.ID] = c.Action
	}
	if acts["agent/auto"] != ActPush || acts["agent/mine"] != ActPush ||
		acts["agent/theirs"] != ActPull || acts["agent/delmod"] != ActDeleteRemote {
		t.Fatalf("resolve actions: %v", acts)
	}
	if _, ok := acts["agent/skip"]; ok {
		t.Fatal("unresolved conflict must be omitted")
	}
}
```

- [ ] **Step 2: Run to verify failure** — `go test ./internal/plan/` → FAIL.

- [ ] **Step 3: Implement**

`internal/plan/plan.go`:
```go
// Package plan turns classification results into an executable sync
// plan, honoring per-item policies and package ownership.
package plan

import (
	"github.com/daikazu/skill-sync/internal/classify"
	"github.com/daikazu/skill-sync/internal/item"
	"github.com/daikazu/skill-sync/internal/state"
)

type Action string

const (
	ActPull         Action = "pull"
	ActPush         Action = "push"
	ActDeleteLocal  Action = "delete-local"
	ActDeleteRemote Action = "delete-remote"
	ActBaseOnly     Action = "base-only"
)

type Change struct {
	Result classify.Result
	Action Action
}

type Plan struct {
	Auto      []Change
	Conflicts []classify.Result
	Skipped   []classify.Result
}

var oneSided = map[classify.State]Action{
	classify.Push:          ActPush,
	classify.Pull:          ActPull,
	classify.NewLocal:      ActPush,
	classify.NewRemote:     ActPull,
	classify.DeletedRemote: ActDeleteLocal,
	classify.DeletedLocal:  ActDeleteRemote,
	classify.InSync:        ActBaseOnly,
}

func Build(results []classify.Result, cfg *state.Config, ledger *state.Ledger) Plan {
	var p Plan
	for _, r := range results {
		if _, _, owned := ledger.Owner(r.ID); owned {
			p.Skipped = append(p.Skipped, r)
			continue
		}
		switch cfg.Policies[r.ID] {
		case state.PolicyNeverSync:
			p.Skipped = append(p.Skipped, r)
			continue
		case state.PolicyAlwaysAsk:
			if r.State != classify.InSync {
				p.Conflicts = append(p.Conflicts, r)
				continue
			}
		}
		if r.State.IsConflict() {
			p.Conflicts = append(p.Conflicts, r)
			continue
		}
		p.Auto = append(p.Auto, Change{Result: r, Action: oneSided[r.State]})
	}
	return p
}

type Resolution string

const (
	ResLocal  Resolution = "local"
	ResRemote Resolution = "remote"
	ResSkip   Resolution = "skip"
)

func Resolve(p Plan, choices map[item.ID]Resolution) []Change {
	out := append([]Change{}, p.Auto...)
	for _, r := range p.Conflicts {
		switch choices[r.ID] {
		case ResLocal:
			if r.Local == "" {
				out = append(out, Change{Result: r, Action: ActDeleteRemote})
			} else {
				out = append(out, Change{Result: r, Action: ActPush})
			}
		case ResRemote:
			if r.Remote == "" {
				out = append(out, Change{Result: r, Action: ActDeleteLocal})
			} else {
				out = append(out, Change{Result: r, Action: ActPull})
			}
		}
	}
	return out
}
```

- [ ] **Step 4: Run tests** — `go test ./internal/plan/` → PASS.

- [ ] **Step 5: Commit** — `git add -A && git commit -m "feat: sync plan builder with policies, ownership, and conflict resolution"`

---

### Task 8: repo package (layout writes, manifest, git wrapper)

**Files:**
- Create: `internal/repo/layout.go`, `internal/repo/manifest.go`, `internal/repo/git.go`, `internal/fsutil/fsutil.go`
- Test: `internal/repo/layout_test.go`, `internal/repo/git_test.go`

**Interfaces:**
- Consumes: `item`, `scan`, `settings`
- Produces:
  - `fsutil.CopyFile(src, dst string) error` (creates parent dirs), `fsutil.CopyTree(srcDir, dstDir string) error` (dst is replaced: removed first, then recreated; regular files only).
  - `repo.WriteItem(root string, s scan.Scanned) error` — skill: `CopyTree(s.Path, root/skills/<name>)`; agent/command: `CopyFile` to `root/agents|commands/<name>.md`; rules: `CopyFile` to `root/rules/CLAUDE.md`; setting: load `root/settings.json` doc, `Set(name, s.Value)`, save; plugins: split name on first `:` into key+entry, load `root/plugins.json`, `SetPluginEntry`, save.
  - `repo.DeleteItem(root string, id item.ID) error` — removes the file/dir, or deletes the key/entry (missing target is a no-op).
  - `repo.Manifest{Schema int; Items map[item.ID]string}`, `repo.LoadManifest(root string) (*Manifest, error)` (missing → `{Schema:1}` empty), `(m *Manifest).Save(root string) error` → `root/manifest.json`.
  - `repo.Git{Dir string}`: `repo.Clone(url, dir string) error`; methods `Pull() error` (ff-only, falls back to `--rebase` if ff fails; a rebase conflict aborts the rebase and returns an error naming the repo dir), `CommitAll(msg string) (committed bool, err error)` (no-op false when tree clean), `Push() error` (`git push -u origin HEAD`; rejection → `repo.ErrPushRejected`), `Log(n int) ([]LogEntry, error)` with `LogEntry{Hash, Date, Subject string}`, `HasCommits() bool`.
  - Network-ish failures (clone/pull/push with unreachable remote) return the underlying error wrapped with stderr text. Sentinel: `var ErrPushRejected = errors.New("push rejected: remote has new commits")` — detect via stderr containing `[rejected]` or `fetch first`.
  - All git calls: `exec.Command("git", args...)` with `cmd.Dir = g.Dir`, capture combined output, wrap errors as `fmt.Errorf("git %s: %w\n%s", args[0], err, out)`.

- [ ] **Step 1: Write failing layout tests**

`internal/repo/layout_test.go`:
```go
package repo

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/daikazu/skill-sync/internal/item"
	"github.com/daikazu/skill-sync/internal/scan"
	"github.com/daikazu/skill-sync/internal/settings"
)

func TestWriteAndDeleteFileItems(t *testing.T) {
	src := t.TempDir()
	os.MkdirAll(filepath.Join(src, "sub"), 0o755)
	os.WriteFile(filepath.Join(src, "SKILL.md"), []byte("s"), 0o644)
	os.WriteFile(filepath.Join(src, "sub", "x.md"), []byte("x"), 0o644)
	root := t.TempDir()

	s := scan.Scanned{ID: item.ID("skill/demo"), Path: src}
	if err := WriteItem(root, s); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "skills/demo/sub/x.md")); string(b) != "x" {
		t.Fatal("tree not copied")
	}
	// overwrite replaces stale files
	os.Remove(filepath.Join(src, "sub", "x.md"))
	if err := WriteItem(root, s); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "skills/demo/sub/x.md")); err == nil {
		t.Fatal("stale file must be gone after rewrite")
	}
	if err := DeleteItem(root, s.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "skills/demo")); err == nil {
		t.Fatal("delete failed")
	}
	if err := DeleteItem(root, s.ID); err != nil {
		t.Fatal("deleting a missing item must be a no-op, got", err)
	}
}

func TestWriteAndDeleteValueItems(t *testing.T) {
	root := t.TempDir()
	err := WriteItem(root, scan.Scanned{ID: item.ID("setting/model"), Value: json.RawMessage(`"opus"`)})
	if err != nil {
		t.Fatal(err)
	}
	err = WriteItem(root, scan.Scanned{ID: item.ID("plugins/enabledPlugins:p@m"), Value: json.RawMessage(`true`)})
	if err != nil {
		t.Fatal(err)
	}
	m, _, _ := scan.Repo(root)
	if _, ok := m[item.ID("setting/model")]; !ok {
		t.Fatal("setting not written")
	}
	if _, ok := m[item.ID("plugins/enabledPlugins:p@m")]; !ok {
		t.Fatal("plugin entry not written")
	}
	DeleteItem(root, item.ID("setting/model"))
	DeleteItem(root, item.ID("plugins/enabledPlugins:p@m"))
	m2, _, _ := scan.Repo(root)
	if len(m2) != 0 {
		t.Fatalf("value items not deleted: %v", m2)
	}
	_ = settings.KeyEnabledPlugins // keep import if unused elsewhere
}

func TestManifestRoundTrip(t *testing.T) {
	root := t.TempDir()
	m, err := LoadManifest(root)
	if err != nil || m.Schema != 1 || len(m.Items) != 0 {
		t.Fatalf("empty manifest: %+v %v", m, err)
	}
	m.Items[item.ID("skill/x")] = "h"
	if err := m.Save(root); err != nil {
		t.Fatal(err)
	}
	m2, _ := LoadManifest(root)
	if m2.Items[item.ID("skill/x")] != "h" {
		t.Fatal("round trip failed")
	}
}
```

- [ ] **Step 2: Write failing git tests**

`internal/repo/git_test.go`:
```go
package repo

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func bare(t *testing.T) string {
	t.Helper()
	d := filepath.Join(t.TempDir(), "origin.git")
	run(t, "", "git", "init", "--bare", "-b", "main", d)
	return d
}

func run(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	c := exec.Command(name, args...)
	if dir != "" {
		c.Dir = dir
	}
	c.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}

func TestCloneCommitPushLog(t *testing.T) {
	origin := bare(t)
	work := filepath.Join(t.TempDir(), "repo")
	if err := Clone(origin, work); err != nil {
		t.Fatal(err)
	}
	g := Git{Dir: work}
	if g.HasCommits() {
		t.Fatal("fresh clone of empty repo must have no commits")
	}
	os.WriteFile(filepath.Join(work, "a.txt"), []byte("1"), 0o644)
	committed, err := g.CommitAll("host: first sync")
	if err != nil || !committed {
		t.Fatalf("commit: %v %v", committed, err)
	}
	if committed, _ := g.CommitAll("empty"); committed {
		t.Fatal("clean tree must not commit")
	}
	if err := g.Push(); err != nil {
		t.Fatal(err)
	}
	entries, err := g.Log(5)
	if err != nil || len(entries) != 1 || entries[0].Subject != "host: first sync" {
		t.Fatalf("log: %+v %v", entries, err)
	}
}

func TestPushRejectedAndPullRecovers(t *testing.T) {
	origin := bare(t)
	a := filepath.Join(t.TempDir(), "a")
	b := filepath.Join(t.TempDir(), "b")
	Clone(origin, a)
	Clone(origin, b)
	ga, gb := Git{Dir: a}, Git{Dir: b}

	os.WriteFile(filepath.Join(a, "x.txt"), []byte("a"), 0o644)
	ga.CommitAll("from a")
	if err := ga.Push(); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(b, "y.txt"), []byte("b"), 0o644)
	gb.CommitAll("from b")
	err := gb.Push()
	if !errors.Is(err, ErrPushRejected) {
		t.Fatalf("want ErrPushRejected, got %v", err)
	}
	if err := gb.Pull(); err != nil {
		t.Fatal(err)
	}
	if err := gb.Push(); err != nil {
		t.Fatalf("push after pull: %v", err)
	}
}
```

- [ ] **Step 3: Run to verify failure** — `go test ./internal/repo/` → FAIL.

- [ ] **Step 4: Implement**

`internal/fsutil/fsutil.go`:
```go
// Package fsutil provides small file-copy helpers used by repo, apply,
// and pack.
package fsutil

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

func CopyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

// CopyTree replaces dstDir with the regular files under srcDir.
func CopyTree(srcDir, dstDir string) error {
	if err := os.RemoveAll(dstDir); err != nil {
		return err
	}
	return filepath.WalkDir(srcDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return err
		}
		rel, err := filepath.Rel(srcDir, p)
		if err != nil {
			return err
		}
		return CopyFile(p, filepath.Join(dstDir, rel))
	})
}
```

`internal/repo/layout.go`:
```go
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
```

`internal/repo/manifest.go`:
```go
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
```

`internal/repo/git.go`:
```go
package repo

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

var ErrPushRejected = errors.New("push rejected: remote has new commits")

type Git struct {
	Dir string
}

func Clone(url, dir string) error {
	_, err := gitRun("", "clone", url, dir)
	return err
}

func (g Git) HasCommits() bool {
	_, err := gitRun(g.Dir, "rev-parse", "HEAD")
	return err == nil
}

func (g Git) Pull() error {
	if !g.HasCommits() {
		// empty repo: nothing to pull yet
		if _, err := gitRun(g.Dir, "fetch", "origin"); err != nil {
			return err
		}
		_, err := gitRun(g.Dir, "pull", "--ff-only", "origin", "HEAD")
		if err != nil && strings.Contains(err.Error(), "couldn't find remote ref") {
			return nil // remote is empty too
		}
		return err
	}
	if _, err := gitRun(g.Dir, "pull", "--ff-only"); err == nil {
		return nil
	}
	// unpushed local commits (e.g. crash before push): rebase on top
	if _, err := gitRun(g.Dir, "pull", "--rebase"); err != nil {
		gitRun(g.Dir, "rebase", "--abort")
		return fmt.Errorf("could not reconcile sync repo %s: %w", g.Dir, err)
	}
	return nil
}

func (g Git) CommitAll(msg string) (bool, error) {
	if _, err := gitRun(g.Dir, "add", "-A"); err != nil {
		return false, err
	}
	out, err := gitRun(g.Dir, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(out) == "" {
		return false, nil
	}
	// explicit identity so commits work on machines/CI without git config
	_, err = gitRun(g.Dir, "-c", "user.name=skill-sync", "-c", "user.email=skill-sync@localhost",
		"commit", "-m", msg)
	return err == nil, err
}

func (g Git) Push() error {
	_, err := gitRun(g.Dir, "push", "-u", "origin", "HEAD")
	if err != nil && (strings.Contains(err.Error(), "[rejected]") ||
		strings.Contains(err.Error(), "fetch first") ||
		strings.Contains(err.Error(), "non-fast-forward")) {
		return ErrPushRejected
	}
	return err
}

type LogEntry struct {
	Hash    string
	Date    string
	Subject string
}

func (g Git) Log(n int) ([]LogEntry, error) {
	out, err := gitRun(g.Dir, "log", fmt.Sprintf("-%d", n), "--pretty=format:%h\t%ci\t%s")
	if err != nil {
		return nil, err
	}
	var entries []LogEntry
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) == 3 {
			entries = append(entries, LogEntry{parts[0], parts[1], parts[2]})
		}
	}
	return entries, nil
}

func gitRun(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("git %s: %w\n%s", args[0], err, out)
	}
	return string(out), nil
}
```

- [ ] **Step 5: Run tests** — `go test ./internal/repo/ ./internal/fsutil/...` → PASS. (Note: the empty-clone Pull path may need `git -c init.defaultBranch=main` tweaks depending on git version — fix until tests pass on this machine's git.)

- [ ] **Step 6: Commit** — `git add -A && git commit -m "feat: sync repo layout writes, manifest, and git wrapper"`

---

### Task 9: apply package (snapshot, restore, apply)

**Files:**
- Create: `internal/apply/apply.go`, `internal/apply/snapshot.go`
- Test: `internal/apply/apply_test.go`

**Interfaces:**
- Consumes: `plan`, `scan`, `repo`, `settings`, `fsutil`, `item`
- Produces:
  - `apply.Applier{ClaudeDir, RepoDir, BackupsDir string}`
  - `(a Applier).Snapshot(changes []plan.Change) (string, error)` — for changes whose Action touches the local side (`ActPull`, `ActDeleteLocal`), copies the affected local path into `BackupsDir/<UTC RFC3339 with colons replaced by dashes>/` preserving the claude-dir-relative layout; if ANY setting or plugins item is local-affected, snapshots `settings.json` once. Skips paths that don't exist locally (new items). Returns "" (and creates nothing) when no change touches local. Local path mapping: skill → `skills/<name>`, agent → `agents/<name>.md`, command → `commands/<name>.md`, rules → `CLAUDE.md`.
  - `apply.Restore(snapshotDir, claudeDir string) error` — copies every regular file in the snapshot back over the claude dir.
  - `(a Applier).Apply(changes []plan.Change, local, remote map[item.ID]scan.Scanned) (map[item.ID]string, error)` — executes each change; returns base updates: `newBaseHash` per ID (`""` value meaning delete the base entry). Rules per action:
    - `ActPull`: file items → `fsutil.CopyTree`/`CopyFile` from `remote[id].Path` to local path; setting → set key in local `settings.json`; plugins → `SetPluginEntry` in local `settings.json`. Base ← `remote[id].Hash`.
    - `ActPush`: `repo.WriteItem(RepoDir, local[id])`. Base ← `local[id].Hash`.
    - `ActDeleteLocal`: remove local path / key / entry. Base ← `""`.
    - `ActDeleteRemote`: `repo.DeleteItem(RepoDir, id)`. Base ← `""`.
    - `ActBaseOnly`: Base ← `local[id].Hash`.
    - Local settings.json is loaded once, mutated by all setting/plugins changes, saved once at the end.

- [ ] **Step 1: Write failing tests**

`internal/apply/apply_test.go`:
```go
package apply

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/daikazu/skill-sync/internal/classify"
	"github.com/daikazu/skill-sync/internal/item"
	"github.com/daikazu/skill-sync/internal/plan"
	"github.com/daikazu/skill-sync/internal/scan"
	"github.com/daikazu/skill-sync/internal/settings"
)

func setup(t *testing.T) (Applier, string, string) {
	t.Helper()
	claude, repoDir := t.TempDir(), t.TempDir()
	os.MkdirAll(filepath.Join(claude, "skills/local-skill"), 0o755)
	os.WriteFile(filepath.Join(claude, "skills/local-skill/SKILL.md"), []byte("local"), 0o644)
	os.WriteFile(filepath.Join(claude, "settings.json"),
		[]byte(`{"model":"opus","env":{"KEEP":"me"}}`), 0o644)
	os.MkdirAll(filepath.Join(repoDir, "skills/remote-skill"), 0o755)
	os.WriteFile(filepath.Join(repoDir, "skills/remote-skill/SKILL.md"), []byte("remote"), 0o644)
	os.WriteFile(filepath.Join(repoDir, "settings.json"), []byte(`{"model":"fable"}`), 0o644)
	return Applier{
		ClaudeDir:  claude,
		RepoDir:    repoDir,
		BackupsDir: filepath.Join(claude, "backups/skill-sync"),
	}, claude, repoDir
}

func change(id string, st classify.State, act plan.Action) plan.Change {
	return plan.Change{Result: classify.Result{ID: item.ID(id), State: st}, Action: act}
}

func TestApplyPullPushDelete(t *testing.T) {
	a, claude, repoDir := setup(t)
	local, _, _ := scan.Claude(claude, settings.KeyOverrides{})
	remote, _, _ := scan.Repo(repoDir)

	changes := []plan.Change{
		change("skill/remote-skill", classify.NewRemote, plan.ActPull),
		change("skill/local-skill", classify.NewLocal, plan.ActPush),
		change("setting/model", classify.Pull, plan.ActPull),
	}
	base, err := a.Apply(changes, local, remote)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(claude, "skills/remote-skill/SKILL.md")); string(b) != "remote" {
		t.Fatal("pull skill failed")
	}
	if b, _ := os.ReadFile(filepath.Join(repoDir, "skills/local-skill/SKILL.md")); string(b) != "local" {
		t.Fatal("push skill failed")
	}
	d, _ := settings.Load(filepath.Join(claude, "settings.json"))
	if v, _ := d.Get("model"); string(v) != `"fable"` {
		t.Fatalf("setting pull failed: %s", v)
	}
	if _, ok := d.Get("env"); !ok {
		t.Fatal("device-local key env must survive settings write")
	}
	if base[item.ID("skill/remote-skill")] != remote[item.ID("skill/remote-skill")].Hash {
		t.Fatal("base update for pull missing")
	}
	if base[item.ID("skill/local-skill")] != local[item.ID("skill/local-skill")].Hash {
		t.Fatal("base update for push missing")
	}

	del := []plan.Change{
		change("skill/remote-skill", classify.DeletedRemote, plan.ActDeleteLocal),
		change("skill/local-skill", classify.DeletedLocal, plan.ActDeleteRemote),
	}
	local2, _, _ := scan.Claude(claude, settings.KeyOverrides{})
	remote2, _, _ := scan.Repo(repoDir)
	base2, err := a.Apply(del, local2, remote2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(claude, "skills/remote-skill")); err == nil {
		t.Fatal("delete-local failed")
	}
	if _, err := os.Stat(filepath.Join(repoDir, "skills/local-skill")); err == nil {
		t.Fatal("delete-remote failed")
	}
	if v, ok := base2[item.ID("skill/remote-skill")]; !ok || v != "" {
		t.Fatal("deletion must clear base entry")
	}
}

func TestSnapshotAndRestore(t *testing.T) {
	a, claude, repoDir := setup(t)
	local, _, _ := scan.Claude(claude, settings.KeyOverrides{})
	remote, _, _ := scan.Repo(repoDir)

	// local-skill will be deleted locally; settings.json will change
	changes := []plan.Change{
		change("skill/local-skill", classify.DeletedRemote, plan.ActDeleteLocal),
		change("setting/model", classify.Pull, plan.ActPull),
	}
	snap, err := a.Snapshot(changes)
	if err != nil || snap == "" {
		t.Fatalf("snapshot: %q %v", snap, err)
	}
	if _, err := a.Apply(changes, local, remote); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(claude, "skills/local-skill")); err == nil {
		t.Fatal("precondition: skill should be deleted")
	}
	if err := Restore(snap, claude); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(claude, "skills/local-skill/SKILL.md")); string(b) != "local" {
		t.Fatal("restore did not bring skill back")
	}
	d, _ := settings.Load(filepath.Join(claude, "settings.json"))
	if v, _ := d.Get("model"); string(v) != `"opus"` {
		t.Fatalf("restore did not revert settings: %s", v)
	}
	var meta map[string]any
	b, err := os.ReadFile(filepath.Join(snap, "snapshot.json"))
	if err != nil || json.Unmarshal(b, &meta) != nil {
		t.Fatal("snapshot metadata missing")
	}
}

func TestSnapshotEmptyWhenNoLocalChanges(t *testing.T) {
	a, _, _ := setup(t)
	snap, err := a.Snapshot([]plan.Change{
		change("skill/local-skill", classify.NewLocal, plan.ActPush),
	})
	if err != nil || snap != "" {
		t.Fatalf("push-only plan must not snapshot: %q %v", snap, err)
	}
}
```

- [ ] **Step 2: Run to verify failure** — `go test ./internal/apply/` → FAIL.

- [ ] **Step 3: Implement**

`internal/apply/snapshot.go`:
```go
// Snapshotting copies the local files a sync is about to touch into a
// timestamped backup dir so any apply can be reversed.
package apply

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/daikazu/skill-sync/internal/fsutil"
	"github.com/daikazu/skill-sync/internal/item"
	"github.com/daikazu/skill-sync/internal/plan"
)

// localRelPath maps a filesystem item to its path inside the claude dir.
func localRelPath(id item.ID) string {
	switch id.Type() {
	case item.TypeSkill:
		return filepath.Join("skills", id.Name())
	case item.TypeAgent:
		return filepath.Join("agents", id.Name()+".md")
	case item.TypeCommand:
		return filepath.Join("commands", id.Name()+".md")
	case item.TypeRules:
		return "CLAUDE.md"
	}
	return ""
}

func touchesLocal(act plan.Action) bool {
	return act == plan.ActPull || act == plan.ActDeleteLocal
}

func (a Applier) Snapshot(changes []plan.Change) (string, error) {
	var rels []string
	needSettings := false
	for _, c := range changes {
		if !touchesLocal(c.Action) {
			continue
		}
		switch c.Result.ID.Type() {
		case item.TypeSetting, item.TypePlugins:
			needSettings = true
		default:
			rels = append(rels, localRelPath(c.Result.ID))
		}
	}
	if needSettings {
		rels = append(rels, "settings.json")
	}
	if len(rels) == 0 {
		return "", nil
	}
	stamp := strings.ReplaceAll(time.Now().UTC().Format(time.RFC3339), ":", "-")
	dir := filepath.Join(a.BackupsDir, stamp)
	copied := []string{}
	for _, rel := range rels {
		src := filepath.Join(a.ClaudeDir, rel)
		st, err := os.Stat(src)
		if err != nil {
			continue // nothing local to preserve
		}
		dst := filepath.Join(dir, rel)
		if st.IsDir() {
			err = fsutil.CopyTree(src, dst)
		} else {
			err = fsutil.CopyFile(src, dst)
		}
		if err != nil {
			return "", err
		}
		copied = append(copied, rel)
	}
	if len(copied) == 0 {
		return "", nil
	}
	meta, _ := json.MarshalIndent(map[string]any{
		"createdAt": time.Now().UTC().Format(time.RFC3339),
		"paths":     copied,
	}, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "snapshot.json"), append(meta, '\n'), 0o644); err != nil {
		return "", err
	}
	return dir, nil
}

func Restore(snapshotDir, claudeDir string) error {
	return filepath.WalkDir(snapshotDir, func(p string, d os.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() || d.Name() == "snapshot.json" && filepath.Dir(p) == snapshotDir {
			return err
		}
		rel, err := filepath.Rel(snapshotDir, p)
		if err != nil {
			return err
		}
		return fsutil.CopyFile(p, filepath.Join(claudeDir, rel))
	})
}
```

`internal/apply/apply.go`:
```go
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
			base[id] = remote[id].Hash
		case plan.ActPush:
			err = repo.WriteItem(a.RepoDir, local[id])
			base[id] = local[id].Hash
		case plan.ActDeleteLocal:
			err = a.deleteLocal(id, loadDoc)
			base[id] = ""
		case plan.ActDeleteRemote:
			err = repo.DeleteItem(a.RepoDir, id)
			base[id] = ""
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
		return fsutil.CopyTree(src.Path, filepath.Join(a.ClaudeDir, localRelPath(id)))
	case item.TypeAgent, item.TypeCommand, item.TypeRules:
		return fsutil.CopyFile(src.Path, filepath.Join(a.ClaudeDir, localRelPath(id)))
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
		return os.RemoveAll(filepath.Join(a.ClaudeDir, localRelPath(id)))
	case item.TypeAgent, item.TypeCommand, item.TypeRules:
		err := os.Remove(filepath.Join(a.ClaudeDir, localRelPath(id)))
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
```

- [ ] **Step 4: Run tests** — `go test ./internal/apply/` → PASS.

- [ ] **Step 5: Commit** — `git add -A && git commit -m "feat: plan application with pre-apply snapshots and restore"`

---

### Task 10: syncer package (orchestration + two-device integration test)

**Files:**
- Create: `internal/syncer/syncer.go`
- Test: `internal/syncer/syncer_test.go`

**Interfaces:**
- Consumes: everything above
- Produces:
  - `syncer.Syncer{ClaudeDir, SyncDir string}` with derived paths: `RepoDir() = SyncDir/repo`, state at `SyncDir/state.json`, config at `SyncDir/config.json`, ledger at `SyncDir/ledger.json`, backups at `ClaudeDir/backups/skill-sync`.
  - `syncer.Init(claudeDir, syncDir, remoteURL string) error` — errors if `SyncDir/repo` already exists; clones; writes config with Remote.
  - `syncer.Resolver = func(p plan.Plan) (map[item.ID]plan.Resolution, bool, error)` — returns choices and proceed=false to abort with nothing applied.
  - `(s *Syncer).Run(resolve Resolver) (*Summary, error)` — full loop: pull → scan both → classify → build → (resolver if conflicts; nil resolver leaves all conflicts unresolved) → resolve → snapshot → apply → manifest update (set pushed/deleted hashes) → device state update (base merge: `""` deletes entry) → commit (`<hostname>: <n> changes`) → push, retrying the whole loop up to 3 times on `repo.ErrPushRejected`.
  - `(s *Syncer).Status() (*plan.Plan, []string, error)` — pull (network errors downgrade to a warning; continue with stale clone) → scan → classify → build; applies nothing.
  - `syncer.Summary{Pulled, Pushed, DeletedLocal, DeletedRemote, Resolved, SkippedConflicts, SkippedItems int; Warnings []string; SnapshotDir string; UpToDate bool}`.

- [ ] **Step 1: Write failing integration test**

`internal/syncer/syncer_test.go`:
```go
package syncer

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/daikazu/skill-sync/internal/item"
	"github.com/daikazu/skill-sync/internal/plan"
)

type device struct {
	claude, sync string
	s            *Syncer
}

func newDevice(t *testing.T, origin string) device {
	t.Helper()
	claude, sync := t.TempDir(), filepath.Join(t.TempDir(), "sync")
	if err := Init(claude, sync, origin); err != nil {
		t.Fatal(err)
	}
	return device{claude, sync, &Syncer{ClaudeDir: claude, SyncDir: sync}}
}

func bare(t *testing.T) string {
	t.Helper()
	d := filepath.Join(t.TempDir(), "origin.git")
	if out, err := exec.Command("git", "init", "--bare", "-b", "main", d).CombinedOutput(); err != nil {
		t.Fatalf("bare: %v\n%s", err, out)
	}
	return d
}

func writeSkill(t *testing.T, claude, name, content string) {
	t.Helper()
	dir := filepath.Join(claude, "skills", name)
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644)
}

func readSkill(t *testing.T, claude, name string) string {
	b, _ := os.ReadFile(filepath.Join(claude, "skills", name, "SKILL.md"))
	return string(b)
}

func TestTwoDeviceSyncAndConflict(t *testing.T) {
	origin := bare(t)
	a := newDevice(t, origin)
	writeSkill(t, a.claude, "shared", "v1")
	os.WriteFile(filepath.Join(a.claude, "settings.json"), []byte(`{"model":"opus"}`), 0o644)

	sum, err := a.s.Run(nil)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Pushed == 0 {
		t.Fatalf("device A should push new items: %+v", sum)
	}

	b := newDevice(t, origin)
	if _, err := b.s.Run(nil); err != nil {
		t.Fatal(err)
	}
	if readSkill(t, b.claude, "shared") != "v1" {
		t.Fatal("device B should receive skill")
	}

	// no changes → up to date on both
	sum, _ = a.s.Run(nil)
	if !sum.UpToDate {
		t.Fatalf("A should be up to date: %+v", sum)
	}

	// divergent edits → conflict surfaced to resolver
	writeSkill(t, a.claude, "shared", "edited-on-A")
	if _, err := a.s.Run(nil); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, b.claude, "shared", "edited-on-B")
	var sawConflict bool
	resolver := func(p plan.Plan) (map[item.ID]plan.Resolution, bool, error) {
		for _, c := range p.Conflicts {
			if c.ID == item.ID("skill/shared") {
				sawConflict = true
			}
		}
		return map[item.ID]plan.Resolution{"skill/shared": plan.ResLocal}, true, nil
	}
	if _, err := b.s.Run(resolver); err != nil {
		t.Fatal(err)
	}
	if !sawConflict {
		t.Fatal("divergent edit must surface as conflict")
	}
	// B chose local → A pulls B's version
	if _, err := a.s.Run(nil); err != nil {
		t.Fatal(err)
	}
	if readSkill(t, a.claude, "shared") != "edited-on-B" {
		t.Fatal("conflict resolution did not propagate")
	}
}

func TestUnresolvedConflictLeavesBothSidesUntouched(t *testing.T) {
	origin := bare(t)
	a := newDevice(t, origin)
	writeSkill(t, a.claude, "s", "v1")
	a.s.Run(nil)
	b := newDevice(t, origin)
	b.s.Run(nil)
	writeSkill(t, a.claude, "s", "A")
	a.s.Run(nil)
	writeSkill(t, b.claude, "s", "B")
	sum, err := b.s.Run(nil) // nil resolver: conflict stays unresolved
	if err != nil {
		t.Fatal(err)
	}
	if sum.SkippedConflicts != 1 {
		t.Fatalf("want 1 skipped conflict: %+v", sum)
	}
	if readSkill(t, b.claude, "s") != "B" {
		t.Fatal("unresolved conflict must not modify local")
	}
}

func TestInitAdoptsRemoteOnFreshMachine(t *testing.T) {
	origin := bare(t)
	a := newDevice(t, origin)
	writeSkill(t, a.claude, "s", "v1")
	os.WriteFile(filepath.Join(a.claude, "settings.json"), []byte(`{"model":"opus"}`), 0o644)
	a.s.Run(nil)

	fresh := newDevice(t, origin)
	sum, err := fresh.s.Run(nil)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Pulled == 0 || readSkill(t, fresh.claude, "s") != "v1" {
		t.Fatalf("fresh machine must adopt remote: %+v", sum)
	}
}
```

- [ ] **Step 2: Run to verify failure** — `go test ./internal/syncer/` → FAIL.

- [ ] **Step 3: Implement**

`internal/syncer/syncer.go`:
```go
// Package syncer orchestrates the full sync loop: fetch, scan,
// classify, plan, resolve, snapshot, apply, commit, push.
package syncer

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/daikazu/skill-sync/internal/apply"
	"github.com/daikazu/skill-sync/internal/classify"
	"github.com/daikazu/skill-sync/internal/item"
	"github.com/daikazu/skill-sync/internal/plan"
	"github.com/daikazu/skill-sync/internal/repo"
	"github.com/daikazu/skill-sync/internal/scan"
	"github.com/daikazu/skill-sync/internal/settings"
	"github.com/daikazu/skill-sync/internal/state"
)

type Syncer struct {
	ClaudeDir string
	SyncDir   string
}

func (s *Syncer) RepoDir() string    { return filepath.Join(s.SyncDir, "repo") }
func (s *Syncer) statePath() string  { return filepath.Join(s.SyncDir, "state.json") }
func (s *Syncer) configPath() string { return filepath.Join(s.SyncDir, "config.json") }
func (s *Syncer) LedgerPath() string { return filepath.Join(s.SyncDir, "ledger.json") }
func (s *Syncer) backupsDir() string {
	return filepath.Join(s.ClaudeDir, "backups", "skill-sync")
}

func Init(claudeDir, syncDir, remoteURL string) error {
	repoDir := filepath.Join(syncDir, "repo")
	if _, err := os.Stat(repoDir); err == nil {
		return fmt.Errorf("%s already exists — already initialized", repoDir)
	}
	if err := os.MkdirAll(syncDir, 0o755); err != nil {
		return err
	}
	if err := repo.Clone(remoteURL, repoDir); err != nil {
		return err
	}
	cfg := &state.Config{Remote: remoteURL}
	return cfg.Save(filepath.Join(syncDir, "config.json"))
}

type Resolver func(p plan.Plan) (map[item.ID]plan.Resolution, bool, error)

type Summary struct {
	Pulled, Pushed, DeletedLocal, DeletedRemote int
	Resolved, SkippedConflicts, SkippedItems    int
	Warnings                                    []string
	SnapshotDir                                 string
	UpToDate                                    bool
}

func (s *Syncer) Run(resolve Resolver) (*Summary, error) {
	for attempt := 0; ; attempt++ {
		sum, err := s.runOnce(resolve)
		if err == repo.ErrPushRejected && attempt < 2 {
			continue // re-pull and re-classify
		}
		return sum, err
	}
}

func (s *Syncer) runOnce(resolve Resolver) (*Summary, error) {
	g := repo.Git{Dir: s.RepoDir()}
	if err := g.Pull(); err != nil {
		return nil, fmt.Errorf("fetching sync repo: %w", err)
	}
	cfg, err := state.LoadConfig(s.configPath())
	if err != nil {
		return nil, err
	}
	ledger, err := state.LoadLedger(s.LedgerPath())
	if err != nil {
		return nil, err
	}
	dev, err := state.LoadDevice(s.statePath())
	if err != nil {
		return nil, err
	}
	overrides := settings.KeyOverrides{Include: cfg.IncludeKeys, Exclude: cfg.ExcludeKeys}
	local, warnsL, err := scan.Claude(s.ClaudeDir, overrides)
	if err != nil {
		return nil, err
	}
	remote, warnsR, err := scan.Repo(s.RepoDir())
	if err != nil {
		return nil, err
	}
	sum := &Summary{Warnings: append(warnsL, warnsR...)}

	results := classify.All(local, remote, dev.LastSynced)
	p := plan.Build(results, cfg, ledger)
	sum.SkippedItems = len(p.Skipped)

	choices := map[item.ID]plan.Resolution{}
	if len(p.Conflicts) > 0 && resolve != nil {
		var proceed bool
		choices, proceed, err = resolve(p)
		if err != nil {
			return nil, err
		}
		if !proceed {
			return nil, fmt.Errorf("sync aborted")
		}
	}
	changes := plan.Resolve(p, choices)
	for _, c := range p.Conflicts {
		if r, ok := choices[c.ID]; ok && r != plan.ResSkip {
			sum.Resolved++
		} else {
			sum.SkippedConflicts++
		}
	}

	real := 0
	for _, c := range changes {
		switch c.Action {
		case plan.ActPull:
			sum.Pulled++
		case plan.ActPush:
			sum.Pushed++
		case plan.ActDeleteLocal:
			sum.DeletedLocal++
		case plan.ActDeleteRemote:
			sum.DeletedRemote++
		}
		if c.Action != plan.ActBaseOnly {
			real++
		}
	}

	a := apply.Applier{ClaudeDir: s.ClaudeDir, RepoDir: s.RepoDir(), BackupsDir: s.backupsDir()}
	if snap, err := a.Snapshot(changes); err != nil {
		return nil, err
	} else {
		sum.SnapshotDir = snap
	}
	baseUpdates, err := a.Apply(changes, local, remote)
	if err != nil {
		return sum, fmt.Errorf("apply failed (restore with: skill-sync rollback): %w", err)
	}

	man, err := repo.LoadManifest(s.RepoDir())
	if err != nil {
		return sum, err
	}
	for id, h := range baseUpdates {
		if h == "" {
			delete(dev.LastSynced, id)
			delete(man.Items, id)
		} else {
			dev.LastSynced[id] = h
			man.Items[id] = h
		}
	}
	if err := man.Save(s.RepoDir()); err != nil {
		return sum, err
	}

	host, _ := os.Hostname()
	if _, err := g.CommitAll(fmt.Sprintf("%s: %d change(s)", host, real)); err != nil {
		return sum, err
	}
	// Always push when commits exist: a prior run may have committed but
	// failed to push (network), and pushing an up-to-date branch is a no-op.
	if g.HasCommits() {
		if err := g.Push(); err != nil {
			return sum, err // ErrPushRejected triggers retry in Run
		}
	}
	if err := dev.Save(s.statePath()); err != nil {
		return sum, err
	}
	sum.UpToDate = real == 0
	return sum, nil
}

func (s *Syncer) Status() (*plan.Plan, []string, error) {
	g := repo.Git{Dir: s.RepoDir()}
	var warns []string
	if err := g.Pull(); err != nil {
		warns = append(warns, "could not reach remote — status may be stale: "+err.Error())
	}
	cfg, err := state.LoadConfig(s.configPath())
	if err != nil {
		return nil, nil, err
	}
	ledger, err := state.LoadLedger(s.LedgerPath())
	if err != nil {
		return nil, nil, err
	}
	dev, err := state.LoadDevice(s.statePath())
	if err != nil {
		return nil, nil, err
	}
	overrides := settings.KeyOverrides{Include: cfg.IncludeKeys, Exclude: cfg.ExcludeKeys}
	local, wl, err := scan.Claude(s.ClaudeDir, overrides)
	if err != nil {
		return nil, nil, err
	}
	remote, wr, err := scan.Repo(s.RepoDir())
	if err != nil {
		return nil, nil, err
	}
	warns = append(warns, append(wl, wr...)...)
	p := plan.Build(classify.All(local, remote, dev.LastSynced), cfg, ledger)
	return &p, warns, nil
}
```

- [ ] **Step 4: Run tests** — `go test ./internal/syncer/ -v` → PASS. Then full suite `go test ./...` → PASS.

- [ ] **Step 5: Commit** — `git add -A && git commit -m "feat: sync orchestration with push-rejection retry and two-device integration tests"`

---

### Task 11: CLI commands — init, sync, status

**Files:**
- Create: `cmd/init.go`, `cmd/sync.go`, `cmd/status.go`, `cmd/paths.go`
- Modify: `cmd/root.go` (register persistent flags)
- Test: manual smoke via temp dirs (engine already covered; cmd layer is wiring)

**Interfaces:**
- Consumes: `syncer`, `plan`, `classify`
- Produces:
  - Persistent flags on root: `--claude-dir` (default `~/.claude`), `--sync-dir` (default `~/.claude-sync`), expanded via `os.UserHomeDir`. Helper `cmd/paths.go`: `getSyncer(cmd *cobra.Command) *syncer.Syncer`.
  - `skill-sync init <git-remote-url>` → `syncer.Init`, then prints "initialized — run `skill-sync sync`".
  - `skill-sync sync` → `Syncer.Run(resolver)` where resolver is a placeholder printing conflicts and returning no choices (TUI lands in Task 12; conflicts are listed with a "run again after Task 12" note is NOT acceptable copy — print "left unresolved (interactive resolution: skill-sync sync in a terminal once TUI ships in this build)" → simply list conflicts as "unresolved, skipped"). Prints summary lines: pulled/pushed/deleted counts, warnings, snapshot dir when present, and each remote-originated deletion explicitly: `deleted locally (remote deletion): skill/x`.
  - `skill-sync status` → `Syncer.Status()`, prints a table grouped as: to push / to pull / deletions / conflicts / skipped, each line `<state>  <item-id>`; exits 0 always.

- [ ] **Step 1: Implement paths helper + init**

`cmd/paths.go`:
```go
package cmd

import (
	"os"
	"path/filepath"

	"github.com/daikazu/skill-sync/internal/syncer"
)

var flagClaudeDir, flagSyncDir string

func defaultDir(sub string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return sub
	}
	return filepath.Join(home, sub)
}

func getSyncer() *syncer.Syncer {
	return &syncer.Syncer{ClaudeDir: flagClaudeDir, SyncDir: flagSyncDir}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&flagClaudeDir, "claude-dir", defaultDir(".claude"), "Claude Code config directory")
	rootCmd.PersistentFlags().StringVar(&flagSyncDir, "sync-dir", defaultDir(".claude-sync"), "skill-sync data directory")
}
```

`cmd/init.go`:
```go
package cmd

import (
	"fmt"

	"github.com/daikazu/skill-sync/internal/syncer"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init <git-remote-url>",
	Short: "Set up syncing on this machine against a private git remote",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := syncer.Init(flagClaudeDir, flagSyncDir, args[0]); err != nil {
			return err
		}
		fmt.Println("initialized — run `skill-sync sync` to do the first sync")
		return nil
	},
}

func init() { rootCmd.AddCommand(initCmd) }
```

- [ ] **Step 2: Implement sync + status commands**

`cmd/sync.go`:
```go
package cmd

import (
	"fmt"

	"github.com/daikazu/skill-sync/internal/item"
	"github.com/daikazu/skill-sync/internal/plan"
	"github.com/spf13/cobra"
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync this machine with the shared repo",
	RunE: func(cmd *cobra.Command, args []string) error {
		s := getSyncer()
		resolver := func(p plan.Plan) (map[item.ID]plan.Resolution, bool, error) {
			for _, c := range p.Conflicts {
				fmt.Printf("conflict (unresolved, skipped): %s [%s]\n", c.ID, c.State)
			}
			return nil, true, nil
		}
		sum, err := s.Run(resolver)
		if err != nil {
			return err
		}
		for _, w := range sum.Warnings {
			fmt.Println("warning:", w)
		}
		if sum.UpToDate {
			fmt.Println("up to date")
			return nil
		}
		fmt.Printf("pulled %d, pushed %d, deleted %d local / %d remote",
			sum.Pulled, sum.Pushed, sum.DeletedLocal, sum.DeletedRemote)
		if sum.SkippedConflicts > 0 {
			fmt.Printf(", %d conflict(s) left unresolved", sum.SkippedConflicts)
		}
		fmt.Println()
		if sum.SnapshotDir != "" {
			fmt.Println("pre-apply snapshot:", sum.SnapshotDir)
		}
		return nil
	},
}

func init() { rootCmd.AddCommand(syncCmd) }
```

`cmd/status.go`:
```go
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show what a sync would do, without changing anything",
	RunE: func(cmd *cobra.Command, args []string) error {
		p, warns, err := getSyncer().Status()
		if err != nil {
			return err
		}
		for _, w := range warns {
			fmt.Println("warning:", w)
		}
		if len(p.Auto) == 0 && len(p.Conflicts) == 0 && len(p.Skipped) == 0 {
			fmt.Println("nothing to sync")
			return nil
		}
		for _, c := range p.Auto {
			fmt.Printf("%-14s %s\n", c.Action, c.Result.ID)
		}
		for _, c := range p.Conflicts {
			fmt.Printf("%-14s %s (%s)\n", "conflict", c.ID, c.State)
		}
		for _, c := range p.Skipped {
			fmt.Printf("%-14s %s\n", "skipped", c.ID)
		}
		return nil
	},
}

func init() { rootCmd.AddCommand(statusCmd) }
```

- [ ] **Step 3: Build and smoke test** —

```bash
go build ./... && go vet ./...
d=$(mktemp -d); git init --bare -b main "$d/origin.git"
mkdir -p "$d/claude/skills/demo"; echo hi > "$d/claude/skills/demo/SKILL.md"
go run . --claude-dir "$d/claude" --sync-dir "$d/sync" init "$d/origin.git"
go run . --claude-dir "$d/claude" --sync-dir "$d/sync" status
go run . --claude-dir "$d/claude" --sync-dir "$d/sync" sync
```
Expected: status lists `push skill/demo`; sync prints `pulled 0, pushed 1, ...`; second sync prints `up to date`.

- [ ] **Step 4: Commit** — `git add -A && git commit -m "feat: init, sync, and status commands"`

---

### Task 12: TUI conflict review

**Files:**
- Create: `internal/tui/content.go`, `internal/tui/review.go`
- Modify: `cmd/sync.go` (swap placeholder resolver for TUI when stdout is a TTY)
- Test: `internal/tui/review_test.go`

**Interfaces:**
- Consumes: `classify`, `plan`, `scan`, `item`
- Produces:
  - `tui.RenderContent(s scan.Scanned) string` — human-readable content for a side: value items → indented JSON of `s.Value`; agent/command/rules → file content; skill → every file under the dir, each preceded by `--- <relpath> ---\n`, sorted; absent side (zero Scanned) → `"(absent)"`.
  - `tui.NewReview(conflicts []classify.Result, local, remote map[item.ID]scan.Scanned) ReviewModel` — Bubble Tea model.
  - `tui.RunReview(...same args) (map[item.ID]plan.Resolution, bool, error)` — runs the program, returns (choices, proceed). Keys: `↑/k`,`↓/j` navigate; `l` keep local; `r` keep remote; `s` skip; choosing auto-advances; `enter` confirm (unchosen items become skip); `q`/`esc` abort (proceed=false).
  - Deps: `go get github.com/charmbracelet/bubbletea github.com/charmbracelet/lipgloss github.com/aymanbagabas/go-udiff golang.org/x/term`.

- [ ] **Step 1: Write failing model tests**

`internal/tui/review_test.go`:
```go
package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/daikazu/skill-sync/internal/classify"
	"github.com/daikazu/skill-sync/internal/item"
	"github.com/daikazu/skill-sync/internal/plan"
	"github.com/daikazu/skill-sync/internal/scan"
)

func key(s string) tea.Msg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func testModel() ReviewModel {
	conflicts := []classify.Result{
		{ID: item.ID("agent/a"), State: classify.Conflict, Local: "1", Remote: "2"},
		{ID: item.ID("agent/b"), State: classify.Conflict, Local: "1", Remote: "2"},
	}
	return NewReview(conflicts, map[item.ID]scan.Scanned{}, map[item.ID]scan.Scanned{})
}

func TestChoicesAndConfirm(t *testing.T) {
	m := testModel()
	var mm tea.Model = m
	mm, _ = mm.Update(key("l")) // agent/a → local, auto-advance
	mm, _ = mm.Update(key("r")) // agent/b → remote
	mm, _ = mm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	rm := mm.(ReviewModel)
	if !rm.Done() || rm.Aborted() {
		t.Fatalf("model should be done: %+v", rm)
	}
	ch := rm.Choices()
	if ch[item.ID("agent/a")] != plan.ResLocal || ch[item.ID("agent/b")] != plan.ResRemote {
		t.Fatalf("choices: %v", ch)
	}
}

func TestEnterDefaultsUnchosenToSkip(t *testing.T) {
	m := testModel()
	var mm tea.Model = m
	mm, _ = mm.Update(key("l"))
	mm, _ = mm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	rm := mm.(ReviewModel)
	if rm.Choices()[item.ID("agent/b")] != plan.ResSkip {
		t.Fatalf("unchosen must default to skip: %v", rm.Choices())
	}
}

func TestAbort(t *testing.T) {
	m := testModel()
	var mm tea.Model = m
	mm, _ = mm.Update(key("q"))
	rm := mm.(ReviewModel)
	if !rm.Aborted() {
		t.Fatal("q must abort")
	}
}

func TestRenderContentAbsent(t *testing.T) {
	if RenderContent(scan.Scanned{}) != "(absent)" {
		t.Fatal("absent side must render as (absent)")
	}
}
```

- [ ] **Step 2: Run to verify failure** — `go test ./internal/tui/` → FAIL.

- [ ] **Step 3: Implement**

`internal/tui/content.go`:
```go
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
```

`internal/tui/review.go`:
```go
package tui

import (
	"fmt"
	"strings"

	"github.com/aymanbagabas/go-udiff"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/daikazu/skill-sync/internal/classify"
	"github.com/daikazu/skill-sync/internal/item"
	"github.com/daikazu/skill-sync/internal/plan"
	"github.com/daikazu/skill-sync/internal/scan"
)

var (
	titleStyle  = lipgloss.NewStyle().Bold(true)
	chosenStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	hintStyle   = lipgloss.NewStyle().Faint(true)
)

type ReviewModel struct {
	conflicts []classify.Result
	diffs     map[item.ID]string
	idx       int
	choices   map[item.ID]plan.Resolution
	done      bool
	aborted   bool
}

func NewReview(conflicts []classify.Result, local, remote map[item.ID]scan.Scanned) ReviewModel {
	diffs := map[item.ID]string{}
	for _, c := range conflicts {
		l := RenderContent(local[c.ID])
		r := RenderContent(remote[c.ID])
		diffs[c.ID] = udiff.Unified("local", "remote", l+"\n", r+"\n")
	}
	return ReviewModel{
		conflicts: conflicts,
		diffs:     diffs,
		choices:   map[item.ID]plan.Resolution{},
	}
}

func (m ReviewModel) Init() tea.Cmd { return nil }

func (m ReviewModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch k.String() {
	case "q", "esc", "ctrl+c":
		m.aborted, m.done = true, true
		return m, tea.Quit
	case "up", "k":
		if m.idx > 0 {
			m.idx--
		}
	case "down", "j":
		if m.idx < len(m.conflicts)-1 {
			m.idx++
		}
	case "l":
		m.choose(plan.ResLocal)
	case "r":
		m.choose(plan.ResRemote)
	case "s":
		m.choose(plan.ResSkip)
	case "enter":
		for _, c := range m.conflicts {
			if _, ok := m.choices[c.ID]; !ok {
				m.choices[c.ID] = plan.ResSkip
			}
		}
		m.done = true
		return m, tea.Quit
	}
	return m, nil
}

func (m *ReviewModel) choose(r plan.Resolution) {
	m.choices[m.conflicts[m.idx].ID] = r
	if m.idx < len(m.conflicts)-1 {
		m.idx++
	}
}

func (m ReviewModel) View() string {
	if m.done {
		return ""
	}
	c := m.conflicts[m.idx]
	var b strings.Builder
	b.WriteString(titleStyle.Render(fmt.Sprintf("Conflict %d/%d: %s (%s)",
		m.idx+1, len(m.conflicts), c.ID, c.State)) + "\n\n")
	b.WriteString(m.diffs[c.ID] + "\n")
	for i, cf := range m.conflicts {
		marker := "  "
		if i == m.idx {
			marker = "> "
		}
		choice := string(m.choices[cf.ID])
		if choice != "" {
			choice = chosenStyle.Render(" [" + choice + "]")
		}
		b.WriteString(marker + string(cf.ID) + choice + "\n")
	}
	b.WriteString(hintStyle.Render("\nl keep local · r keep remote · s skip · ↑/↓ move · enter confirm · q abort\n"))
	return b.String()
}

func (m ReviewModel) Done() bool                          { return m.done }
func (m ReviewModel) Aborted() bool                       { return m.aborted }
func (m ReviewModel) Choices() map[item.ID]plan.Resolution { return m.choices }

func RunReview(conflicts []classify.Result, local, remote map[item.ID]scan.Scanned) (map[item.ID]plan.Resolution, bool, error) {
	p := tea.NewProgram(NewReview(conflicts, local, remote))
	out, err := p.Run()
	if err != nil {
		return nil, false, err
	}
	m := out.(ReviewModel)
	return m.Choices(), !m.Aborted(), nil
}
```

- [ ] **Step 4: Wire into sync command**

The syncer's `Resolver` receives only the `plan.Plan`, but `RunReview` needs the scan maps for diff content. Extend `plan.Plan` in `internal/plan/plan.go` with two fields populated by the syncer before calling the resolver:

```go
type Plan struct {
	Auto      []Change
	Conflicts []classify.Result
	Skipped   []classify.Result
	Local     map[item.ID]scan.Scanned // content lookup for review UIs
	Remote    map[item.ID]scan.Scanned
}
```
(Add `scan` import to plan package; in `syncer.runOnce` and `Status`, set `p.Local, p.Remote = local, remote` right after `plan.Build`.)

In `cmd/sync.go`, replace the placeholder resolver:

```go
resolver := func(p plan.Plan) (map[item.ID]plan.Resolution, bool, error) {
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		for _, c := range p.Conflicts {
			fmt.Printf("conflict (unresolved, skipped): %s [%s]\n", c.ID, c.State)
		}
		return nil, true, nil
	}
	return tui.RunReview(p.Conflicts, p.Local, p.Remote)
}
```
(Imports: `golang.org/x/term`, `github.com/daikazu/skill-sync/internal/tui`.)

- [ ] **Step 5: Run tests** — `go test ./...` → PASS (plan/syncer tests still green after the Plan struct extension).

- [ ] **Step 6: Manual TUI smoke** — repeat Task 11's smoke script but create the conflict from Task 10's integration test manually (two clones, divergent skill edit), run `go run . ... sync` in the terminal, and verify the review screen renders, keys work, and resolution propagates.

- [ ] **Step 7: Commit** — `git add -A && git commit -m "feat: interactive conflict review TUI"`

---

### Task 13: pack build/read/extract + pack command + picker

**Files:**
- Create: `internal/pack/pack.go`, `internal/tui/picker.go`, `cmd/pack.go`
- Test: `internal/pack/pack_test.go`, `internal/tui/picker_test.go`

**Interfaces:**
- Consumes: `item`, `scan`, `repo`, `settings`, `fsutil`
- Produces:
  - `pack.PackItem{Hash string; Description string `json:"description,omitempty"`}`
  - `pack.Manifest{Name, Version, Author, CreatedAt string; Items map[item.ID]PackItem}`
  - `pack.Build(outPath string, man Manifest, contents map[item.ID]scan.Scanned) error` — stages a temp dir via `repo.WriteItem` for every manifest item (the .skillpack layout IS the repo layout), writes `manifest.json`, then tar.gz's the staging dir (slash paths, regular files only).
  - `pack.Open(path string) (*Manifest, error)` — reads just the manifest from the archive.
  - `pack.Extract(path, destDir string) error` — rejects entries that are absolute, contain `..`, or are not regular files/dirs (symlinks → error `"unsafe entry"`).
  - `pack.Load(path, tmpDir string) (*Manifest, map[item.ID]scan.Scanned, error)` — Extract to tmpDir + `scan.Repo(tmpDir)` + Open; the returned map is keyed by manifest items only (ignore strays), erroring if a manifest item is missing from the archive or its hash mismatches (tamper check).
  - `tui.RunPicker(items []scan.Scanned) (selected []item.ID, proceed bool, err error)` and testable `tui.NewPicker(items []scan.Scanned) PickerModel` — checkbox list sorted by ID; skills/agents/commands start checked, settings/rules/plugins start unchecked; keys: space toggle, `a` all, `n` none, ↑/↓ move, enter confirm, q abort. Methods `Done()/Aborted()/Selected() []item.ID`.
  - `skill-sync pack [--all] [-o out] [--name N] [--version V]` — scans claude dir; `--all` selects everything; otherwise picker; default name `claude-profile`, version `0.1.0`, author from `git config user.name` (fallback `$USER`); default out `<name>-<version>.skillpack`.

- [ ] **Step 1: Write failing pack tests**

`internal/pack/pack_test.go`:
```go
package pack

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"

	"github.com/daikazu/skill-sync/internal/item"
	"github.com/daikazu/skill-sync/internal/scan"
	"github.com/daikazu/skill-sync/internal/settings"
)

func fixtureItems(t *testing.T) map[item.ID]scan.Scanned {
	t.Helper()
	d := t.TempDir()
	os.MkdirAll(filepath.Join(d, "skills/demo"), 0o755)
	os.WriteFile(filepath.Join(d, "skills/demo/SKILL.md"), []byte("demo"), 0o644)
	os.WriteFile(filepath.Join(d, "settings.json"), []byte(`{"model":"opus"}`), 0o644)
	m, _, err := scan.Claude(d, settings.KeyOverrides{})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestBuildOpenLoadRoundTrip(t *testing.T) {
	items := fixtureItems(t)
	man := Manifest{Name: "test-pack", Version: "1.0.0", Author: "t", CreatedAt: "2026-08-30T00:00:00Z",
		Items: map[item.ID]PackItem{}}
	for id, s := range items {
		man.Items[id] = PackItem{Hash: s.Hash}
	}
	out := filepath.Join(t.TempDir(), "test.skillpack")
	if err := Build(out, man, items); err != nil {
		t.Fatal(err)
	}
	got, err := Open(out)
	if err != nil || got.Name != "test-pack" || len(got.Items) != len(items) {
		t.Fatalf("open: %+v %v", got, err)
	}
	man2, contents, err := Load(out, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(contents) != len(man2.Items) {
		t.Fatalf("load contents: %v", contents)
	}
	for id, s := range contents {
		if s.Hash != man2.Items[id].Hash {
			t.Fatalf("%s hash mismatch after round trip", id)
		}
	}
}

func TestExtractRejectsTraversal(t *testing.T) {
	evil := filepath.Join(t.TempDir(), "evil.skillpack")
	f, _ := os.Create(evil)
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	body := []byte("owned")
	tw.WriteHeader(&tar.Header{Name: "../escape.txt", Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg})
	tw.Write(body)
	tw.Close()
	gz.Close()
	f.Close()
	if err := Extract(evil, t.TempDir()); err == nil {
		t.Fatal("traversal entry must be rejected")
	}
}

func TestLoadDetectsTamper(t *testing.T) {
	items := fixtureItems(t)
	man := Manifest{Name: "p", Version: "1", Items: map[item.ID]PackItem{}}
	for id := range items {
		man.Items[id] = PackItem{Hash: "wrong"}
	}
	out := filepath.Join(t.TempDir(), "p.skillpack")
	Build(out, man, items)
	if _, _, err := Load(out, t.TempDir()); err == nil {
		t.Fatal("hash mismatch must fail Load")
	}
}
```

- [ ] **Step 2: Run to verify failure** — `go test ./internal/pack/` → FAIL.

- [ ] **Step 3: Implement**

`internal/pack/pack.go`:
```go
// Package pack builds, validates, and installs .skillpack archives:
// portable bundles that double as backups and team distributions.
package pack

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/daikazu/skill-sync/internal/item"
	"github.com/daikazu/skill-sync/internal/repo"
	"github.com/daikazu/skill-sync/internal/scan"
)

type PackItem struct {
	Hash        string `json:"hash"`
	Description string `json:"description,omitempty"`
}

type Manifest struct {
	Name      string               `json:"name"`
	Version   string               `json:"version"`
	Author    string               `json:"author,omitempty"`
	CreatedAt string               `json:"createdAt"`
	Items     map[item.ID]PackItem `json:"items"`
}

func Build(outPath string, man Manifest, contents map[item.ID]scan.Scanned) error {
	stage, err := os.MkdirTemp("", "skillpack-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	for id := range man.Items {
		s, ok := contents[id]
		if !ok {
			return fmt.Errorf("manifest item %s has no content", id)
		}
		if err := repo.WriteItem(stage, s); err != nil {
			return err
		}
	}
	mb, err := json.MarshalIndent(man, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(stage, "manifest.json"), append(mb, '\n'), 0o644); err != nil {
		return err
	}

	out, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer out.Close()
	gz := gzip.NewWriter(out)
	tw := tar.NewWriter(gz)
	err = filepath.WalkDir(stage, func(p string, d fs.DirEntry, werr error) error {
		if werr != nil || !d.Type().IsRegular() {
			return werr
		}
		rel, err := filepath.Rel(stage, p)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		hdr := &tar.Header{Name: filepath.ToSlash(rel), Mode: 0o644, Size: info.Size(), Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(tw, f)
		return err
	})
	if err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

func eachEntry(path string, fn func(hdr *tar.Header, r io.Reader) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("not a .skillpack (gzip): %w", err)
	}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if err := fn(hdr, tr); err != nil {
			return err
		}
	}
}

func safeRel(name string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(name))
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("unsafe entry %q", name)
	}
	return clean, nil
}

func Open(path string) (*Manifest, error) {
	var man *Manifest
	err := eachEntry(path, func(hdr *tar.Header, r io.Reader) error {
		if hdr.Name != "manifest.json" {
			return nil
		}
		var m Manifest
		if err := json.NewDecoder(r).Decode(&m); err != nil {
			return fmt.Errorf("manifest.json: %w", err)
		}
		man = &m
		return nil
	})
	if err != nil {
		return nil, err
	}
	if man == nil {
		return nil, fmt.Errorf("%s: no manifest.json — not a skillpack", path)
	}
	if man.Items == nil {
		man.Items = map[item.ID]PackItem{}
	}
	return man, nil
}

func Extract(path, destDir string) error {
	return eachEntry(path, func(hdr *tar.Header, r io.Reader) error {
		if hdr.Typeflag == tar.TypeDir {
			return nil
		}
		if hdr.Typeflag != tar.TypeReg {
			return fmt.Errorf("unsafe entry %q: not a regular file", hdr.Name)
		}
		rel, err := safeRel(hdr.Name)
		if err != nil {
			return err
		}
		dst := filepath.Join(destDir, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		f, err := os.Create(dst)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(f, r)
		return err
	})
}

// Load extracts the pack and returns its manifest plus the scanned
// content for each manifest item, verifying hashes.
func Load(path, tmpDir string) (*Manifest, map[item.ID]scan.Scanned, error) {
	if err := Extract(path, tmpDir); err != nil {
		return nil, nil, err
	}
	man, err := Open(path)
	if err != nil {
		return nil, nil, err
	}
	all, _, err := scan.Repo(tmpDir)
	if err != nil {
		return nil, nil, err
	}
	contents := map[item.ID]scan.Scanned{}
	for id, pi := range man.Items {
		s, ok := all[id]
		if !ok {
			return nil, nil, fmt.Errorf("pack is missing item %s", id)
		}
		if s.Hash != pi.Hash {
			return nil, nil, fmt.Errorf("pack item %s content does not match its manifest hash", id)
		}
		contents[id] = s
	}
	return man, contents, nil
}
```

- [ ] **Step 4: Run pack tests** — `go test ./internal/pack/` → PASS.

- [ ] **Step 5: Write failing picker tests, then implement picker**

`internal/tui/picker_test.go`:
```go
package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/daikazu/skill-sync/internal/item"
	"github.com/daikazu/skill-sync/internal/scan"
)

func pickerItems() []scan.Scanned {
	return []scan.Scanned{
		{ID: item.ID("agent/a")},
		{ID: item.ID("setting/model")},
		{ID: item.ID("skill/s")},
	}
}

func TestPickerDefaults(t *testing.T) {
	m := NewPicker(pickerItems())
	var mm tea.Model = m
	mm, _ = mm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	pm := mm.(PickerModel)
	sel := map[item.ID]bool{}
	for _, id := range pm.Selected() {
		sel[id] = true
	}
	if !sel[item.ID("agent/a")] || !sel[item.ID("skill/s")] {
		t.Fatalf("content items must default checked: %v", sel)
	}
	if sel[item.ID("setting/model")] {
		t.Fatal("settings must default unchecked for curated packs")
	}
}

func TestPickerToggleAndAbort(t *testing.T) {
	m := NewPicker(pickerItems())
	var mm tea.Model = m
	mm, _ = mm.Update(tea.KeyMsg{Type: tea.KeySpace}) // toggles first row (agent/a) off
	mm, _ = mm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	pm := mm.(PickerModel)
	for _, id := range pm.Selected() {
		if id == item.ID("agent/a") {
			t.Fatal("space must toggle off")
		}
	}
	m2 := NewPicker(pickerItems())
	var mm2 tea.Model = m2
	mm2, _ = mm2.Update(key("q"))
	if !mm2.(PickerModel).Aborted() {
		t.Fatal("q must abort")
	}
}
```

`internal/tui/picker.go`:
```go
package tui

import (
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/daikazu/skill-sync/internal/item"
	"github.com/daikazu/skill-sync/internal/scan"
)

type PickerModel struct {
	ids     []item.ID
	checked map[item.ID]bool
	idx     int
	done    bool
	aborted bool
}

func defaultChecked(t item.Type) bool {
	return t == item.TypeSkill || t == item.TypeAgent || t == item.TypeCommand
}

func NewPicker(items []scan.Scanned) PickerModel {
	m := PickerModel{checked: map[item.ID]bool{}}
	for _, s := range items {
		m.ids = append(m.ids, s.ID)
		m.checked[s.ID] = defaultChecked(s.ID.Type())
	}
	sort.Slice(m.ids, func(i, j int) bool { return m.ids[i] < m.ids[j] })
	return m
}

func (m PickerModel) Init() tea.Cmd { return nil }

func (m PickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch k.String() {
	case "q", "esc", "ctrl+c":
		m.aborted, m.done = true, true
		return m, tea.Quit
	case "up", "k":
		if m.idx > 0 {
			m.idx--
		}
	case "down", "j":
		if m.idx < len(m.ids)-1 {
			m.idx++
		}
	case " ":
		id := m.ids[m.idx]
		m.checked[id] = !m.checked[id]
	case "a":
		for _, id := range m.ids {
			m.checked[id] = true
		}
	case "n":
		for _, id := range m.ids {
			m.checked[id] = false
		}
	case "enter":
		m.done = true
		return m, tea.Quit
	}
	return m, nil
}

func (m PickerModel) View() string {
	if m.done {
		return ""
	}
	var b strings.Builder
	b.WriteString(titleStyle.Render("Select items to pack") + "\n\n")
	for i, id := range m.ids {
		cursor := "  "
		if i == m.idx {
			cursor = "> "
		}
		box := "[ ]"
		if m.checked[id] {
			box = chosenStyle.Render("[x]")
		}
		b.WriteString(cursor + box + " " + string(id) + "\n")
	}
	b.WriteString(hintStyle.Render("\nspace toggle · a all · n none · enter confirm · q abort\n"))
	return b.String()
}

func (m PickerModel) Done() bool    { return m.done }
func (m PickerModel) Aborted() bool { return m.aborted }

func (m PickerModel) Selected() []item.ID {
	var out []item.ID
	for _, id := range m.ids {
		if m.checked[id] {
			out = append(out, id)
		}
	}
	return out
}

func RunPicker(items []scan.Scanned) ([]item.ID, bool, error) {
	p := tea.NewProgram(NewPicker(items))
	out, err := p.Run()
	if err != nil {
		return nil, false, err
	}
	m := out.(PickerModel)
	return m.Selected(), !m.Aborted(), nil
}
```

- [ ] **Step 6: Implement the pack command**

`cmd/pack.go`:
```go
package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/daikazu/skill-sync/internal/item"
	"github.com/daikazu/skill-sync/internal/pack"
	"github.com/daikazu/skill-sync/internal/scan"
	"github.com/daikazu/skill-sync/internal/settings"
	"github.com/daikazu/skill-sync/internal/tui"
	"github.com/spf13/cobra"
)

var (
	packAll     bool
	packOut     string
	packName    string
	packVersion string
)

func author() string {
	if out, err := exec.Command("git", "config", "user.name").Output(); err == nil {
		if s := strings.TrimSpace(string(out)); s != "" {
			return s
		}
	}
	return os.Getenv("USER")
}

var packCmd = &cobra.Command{
	Use:   "pack",
	Short: "Build a .skillpack (backup with --all, or a curated team package)",
	RunE: func(cmd *cobra.Command, args []string) error {
		items, warns, err := scan.Claude(flagClaudeDir, settings.KeyOverrides{})
		if err != nil {
			return err
		}
		for _, w := range warns {
			fmt.Println("warning:", w)
		}
		var selected []item.ID
		if packAll {
			for id := range items {
				selected = append(selected, id)
			}
		} else {
			var list []scan.Scanned
			for _, s := range items {
				list = append(list, s)
			}
			var proceed bool
			selected, proceed, err = tui.RunPicker(list)
			if err != nil || !proceed {
				return err
			}
		}
		if len(selected) == 0 {
			return fmt.Errorf("nothing selected")
		}
		man := pack.Manifest{
			Name: packName, Version: packVersion, Author: author(),
			CreatedAt: time.Now().UTC().Format(time.RFC3339),
			Items:     map[item.ID]pack.PackItem{},
		}
		for _, id := range selected {
			man.Items[id] = pack.PackItem{Hash: items[id].Hash}
		}
		out := packOut
		if out == "" {
			out = fmt.Sprintf("%s-%s.skillpack", packName, packVersion)
		}
		if err := pack.Build(out, man, items); err != nil {
			return err
		}
		fmt.Printf("packed %d item(s) → %s\n", len(selected), out)
		return nil
	},
}

func init() {
	packCmd.Flags().BoolVar(&packAll, "all", false, "include every shareable item (full backup)")
	packCmd.Flags().StringVarP(&packOut, "output", "o", "", "output file")
	packCmd.Flags().StringVar(&packName, "name", "claude-profile", "package name")
	packCmd.Flags().StringVar(&packVersion, "version", "0.1.0", "package version")
	rootCmd.AddCommand(packCmd)
}
```

- [ ] **Step 7: Run tests + smoke** — `go test ./...` → PASS; then `go run . --claude-dir ~/.claude pack --all -o /tmp/probe.skillpack` (read-only against the real claude dir) and `tar tzf /tmp/probe.skillpack | head` shows layout + manifest.json; delete the probe file.

- [ ] **Step 8: Commit** — `git add -A && git commit -m "feat: skillpack build/extract with picker and pack command"`

---

### Task 14: install, uninstall, packages

**Files:**
- Create: `internal/pack/install.go`, `cmd/install.go`, `cmd/uninstall.go`, `cmd/packages.go`
- Modify: `internal/apply/snapshot.go` (extract `SnapshotPaths` helper), `internal/apply/apply.go` (nothing structural — reuse Applier)
- Test: `internal/pack/install_test.go`

**Interfaces:**
- Consumes: `apply`, `scan`, `state`, `item`, `plan`, `classify`
- Produces:
  - In `apply`: `SnapshotPaths(claudeDir, backupsDir string, rels []string) (string, error)` — the generic core of `Snapshot` (which now calls it); returns "" when nothing existed to copy.
  - `pack.InstallPlan{Fresh, Upgrade, AlreadyCurrent, ModifiedOwned, Collisions []item.ID}` — categories:
    - `Fresh`: not present locally.
    - `AlreadyCurrent`: local content hash == pack hash (ownership recorded/refreshed for free).
    - `Upgrade`: owned by this package, local hash == ledger's installed hash, pack hash differs.
    - `ModifiedOwned`: owned by this package but local hash ≠ ledger hash (user edited it) — needs a keep-local / take-package decision.
    - `Collisions`: present locally, differing content, not owned by this package — needs rename / skip / replace.
  - `pack.BuildInstallPlan(man *Manifest, contents, local map[item.ID]scan.Scanned, led *state.Ledger, pkgName string) InstallPlan`
  - `pack.CollisionChoice` (`ChoiceRename`, `ChoiceSkip`, `ChoiceReplace`), `pack.ModifiedChoice` (`KeepLocal`, `TakePackage`)
  - `pack.RenamedID(id item.ID, pkgName string) item.ID` — `skill/code-review` + pkg `agency` → `skill/code-review-agency`; only meaningful for skill/agent/command (value items never rename).
  - `pack.ApplyInstall(claudeDir, backupsDir, ledgerPath string, man *Manifest, contents map[item.ID]scan.Scanned, ip InstallPlan, collisions map[item.ID]CollisionChoice, modified map[item.ID]ModifiedChoice) (*InstallSummary, error)` — snapshots affected local paths, then installs Fresh + Upgrade + TakePackage + renamed/replaced collision items by reusing `apply.Applier.Apply` with synthetic `ActPull` changes whose `remote` map is the pack contents (renamed items get a re-keyed Scanned with the new ID); updates the ledger record for the package (renamed IDs recorded under their new ID with the pack item's hash; skipped/keep-local items NOT recorded... except ModifiedOwned+KeepLocal which stays recorded at its OLD installed hash so a later upgrade still flags it). Value-item collisions with `ChoiceRename` are coerced to `ChoiceSkip`.
  - `pack.Uninstall(claudeDir, backupsDir, ledgerPath, pkgName string) (removed, kept []item.ID, err error)` — for each owned item: current local hash == ledger hash → delete locally (synthetic `ActDeleteLocal`); else keep (report). Package record removed either way. Snapshots first. When scanning local state, pass `settings.KeyOverrides{Include: <the record's setting-item key names>}` so a package-installed custom settings key (outside the default shareable list) is still visible to the scan; the `defaultOverrides` indirection shown in the code sketch must be replaced by that real construction.
  - `InstallSummary{Installed, Upgraded, Renamed, Replaced, Skipped, Current int; SnapshotDir string}`
  - `skill-sync install <file>` — non-conflicting categories proceed; each Collision/ModifiedOwned prompts via `huh.NewSelect` (dep: `go get github.com/charmbracelet/huh`); `--yes` accepts defaults (collision→skip, modified→keep-local) for scripting.
  - `skill-sync uninstall <name>`, `skill-sync packages` (lists ledger: name, version, item count, and per-item `modified` markers by comparing current local hash vs ledger hash).

- [ ] **Step 1: Write failing install-engine tests**

`internal/pack/install_test.go`:
```go
package pack

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/daikazu/skill-sync/internal/item"
	"github.com/daikazu/skill-sync/internal/scan"
	"github.com/daikazu/skill-sync/internal/settings"
	"github.com/daikazu/skill-sync/internal/state"
)

// buildPack creates a .skillpack containing one skill "tool" with the
// given content and returns its path.
func buildPack(t *testing.T, name, version, content string) string {
	t.Helper()
	src := t.TempDir()
	os.MkdirAll(filepath.Join(src, "skills/tool"), 0o755)
	os.WriteFile(filepath.Join(src, "skills/tool/SKILL.md"), []byte(content), 0o644)
	items, _, _ := scan.Claude(src, settings.KeyOverrides{})
	man := Manifest{Name: name, Version: version, Items: map[item.ID]PackItem{}}
	for id, s := range items {
		man.Items[id] = PackItem{Hash: s.Hash}
	}
	out := filepath.Join(t.TempDir(), name+".skillpack")
	if err := Build(out, man, items); err != nil {
		t.Fatal(err)
	}
	return out
}

func env(t *testing.T) (claude, backups, ledgerPath string) {
	claude = t.TempDir()
	return claude, filepath.Join(claude, "backups/skill-sync"), filepath.Join(t.TempDir(), "ledger.json")
}

func install(t *testing.T, pk, claude, backups, ledgerPath string,
	col map[item.ID]CollisionChoice, mod map[item.ID]ModifiedChoice) *InstallSummary {
	t.Helper()
	man, contents, err := Load(pk, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	local, _, _ := scan.Claude(claude, settings.KeyOverrides{})
	led, _ := state.LoadLedger(ledgerPath)
	ip := BuildInstallPlan(man, contents, local, led, man.Name)
	sum, err := ApplyInstall(claude, backups, ledgerPath, man, contents, ip, col, mod)
	if err != nil {
		t.Fatal(err)
	}
	return sum
}

func TestFreshInstallRecordsOwnership(t *testing.T) {
	pk := buildPack(t, "agency", "1.0.0", "v1")
	claude, backups, lp := env(t)
	sum := install(t, pk, claude, backups, lp, nil, nil)
	if sum.Installed != 1 {
		t.Fatalf("installed: %+v", sum)
	}
	if b, _ := os.ReadFile(filepath.Join(claude, "skills/tool/SKILL.md")); string(b) != "v1" {
		t.Fatal("content not installed")
	}
	led, _ := state.LoadLedger(lp)
	if _, _, ok := led.Owner(item.ID("skill/tool")); !ok {
		t.Fatal("ownership not recorded")
	}
}

func TestUpgradeOnlyTouchesUnmodifiedOwned(t *testing.T) {
	claude, backups, lp := env(t)
	install(t, buildPack(t, "agency", "1.0.0", "v1"), claude, backups, lp, nil, nil)
	sum := install(t, buildPack(t, "agency", "2.0.0", "v2"), claude, backups, lp, nil, nil)
	if sum.Upgraded != 1 {
		t.Fatalf("upgrade: %+v", sum)
	}
	if b, _ := os.ReadFile(filepath.Join(claude, "skills/tool/SKILL.md")); string(b) != "v2" {
		t.Fatal("upgrade did not apply")
	}
}

func TestModifiedOwnedKeepLocalSurvivesUpgrade(t *testing.T) {
	claude, backups, lp := env(t)
	install(t, buildPack(t, "agency", "1.0.0", "v1"), claude, backups, lp, nil, nil)
	os.WriteFile(filepath.Join(claude, "skills/tool/SKILL.md"), []byte("my edit"), 0o644)
	sum := install(t, buildPack(t, "agency", "2.0.0", "v2"), claude, backups, lp,
		nil, map[item.ID]ModifiedChoice{"skill/tool": KeepLocal})
	if sum.Skipped != 1 {
		t.Fatalf("keep-local: %+v", sum)
	}
	if b, _ := os.ReadFile(filepath.Join(claude, "skills/tool/SKILL.md")); string(b) != "my edit" {
		t.Fatal("local edit must never be silently reverted")
	}
}

func TestCollisionRename(t *testing.T) {
	claude, backups, lp := env(t)
	os.MkdirAll(filepath.Join(claude, "skills/tool"), 0o755)
	os.WriteFile(filepath.Join(claude, "skills/tool/SKILL.md"), []byte("mine"), 0o644)
	pk := buildPack(t, "agency", "1.0.0", "theirs")
	sum := install(t, pk, claude, backups, lp,
		map[item.ID]CollisionChoice{"skill/tool": ChoiceRename}, nil)
	if sum.Renamed != 1 {
		t.Fatalf("rename: %+v", sum)
	}
	if b, _ := os.ReadFile(filepath.Join(claude, "skills/tool/SKILL.md")); string(b) != "mine" {
		t.Fatal("user's skill must be untouched")
	}
	if b, _ := os.ReadFile(filepath.Join(claude, "skills/tool-agency/SKILL.md")); string(b) != "theirs" {
		t.Fatal("renamed install missing")
	}
	led, _ := state.LoadLedger(lp)
	if _, _, ok := led.Owner(item.ID("skill/tool-agency")); !ok {
		t.Fatal("renamed item must be ledger-owned")
	}
}

func TestUninstallKeepsModified(t *testing.T) {
	claude, backups, lp := env(t)
	install(t, buildPack(t, "agency", "1.0.0", "v1"), claude, backups, lp, nil, nil)
	removed, kept, err := Uninstall(claude, backups, lp, "agency")
	if err != nil || len(removed) != 1 || len(kept) != 0 {
		t.Fatalf("uninstall clean: %v %v %v", removed, kept, err)
	}
	if _, err := os.Stat(filepath.Join(claude, "skills/tool")); err == nil {
		t.Fatal("owned unmodified item must be removed")
	}

	install(t, buildPack(t, "agency", "1.0.0", "v1"), claude, backups, lp, nil, nil)
	os.WriteFile(filepath.Join(claude, "skills/tool/SKILL.md"), []byte("edited"), 0o644)
	removed, kept, err = Uninstall(claude, backups, lp, "agency")
	if err != nil || len(removed) != 0 || len(kept) != 1 {
		t.Fatalf("uninstall modified: %v %v %v", removed, kept, err)
	}
	if _, err := os.Stat(filepath.Join(claude, "skills/tool")); err != nil {
		t.Fatal("modified item must be kept")
	}
	led, _ := state.LoadLedger(lp)
	if len(led.Packages) != 0 {
		t.Fatal("package record must be gone")
	}
}
```

- [ ] **Step 2: Run to verify failure** — `go test ./internal/pack/` → FAIL.

- [ ] **Step 3: Implement**

First refactor `internal/apply/snapshot.go`: extract the copy loop into

```go
// SnapshotPaths copies the given claude-dir-relative paths into a new
// timestamped dir under backupsDir. Returns "" if nothing existed.
func SnapshotPaths(claudeDir, backupsDir string, rels []string) (string, error)
```
with `(a Applier).Snapshot` now computing its rel list and delegating to `SnapshotPaths(a.ClaudeDir, a.BackupsDir, rels)`. Also export the local path mapper as `apply.LocalRelPath(id item.ID) string` (rename `localRelPath`; update call sites) — the install engine needs it.

`internal/pack/install.go`:
```go
package pack

import (
	"fmt"

	"github.com/daikazu/skill-sync/internal/apply"
	"github.com/daikazu/skill-sync/internal/classify"
	"github.com/daikazu/skill-sync/internal/item"
	"github.com/daikazu/skill-sync/internal/plan"
	"github.com/daikazu/skill-sync/internal/scan"
	"github.com/daikazu/skill-sync/internal/state"
)

type InstallPlan struct {
	Fresh, Upgrade, AlreadyCurrent, ModifiedOwned, Collisions []item.ID
}

type CollisionChoice string

const (
	ChoiceRename  CollisionChoice = "rename"
	ChoiceSkip    CollisionChoice = "skip"
	ChoiceReplace CollisionChoice = "replace"
)

type ModifiedChoice string

const (
	KeepLocal   ModifiedChoice = "keep-local"
	TakePackage ModifiedChoice = "take-package"
)

func BuildInstallPlan(man *Manifest, contents, local map[item.ID]scan.Scanned,
	led *state.Ledger, pkgName string) InstallPlan {
	var ip InstallPlan
	for id := range man.Items {
		pkgHash := contents[id].Hash
		loc, present := local[id]
		if !present {
			ip.Fresh = append(ip.Fresh, id)
			continue
		}
		if loc.Hash == pkgHash {
			ip.AlreadyCurrent = append(ip.AlreadyCurrent, id)
			continue
		}
		owner, installedHash, owned := led.Owner(id)
		if owned && owner == pkgName {
			if loc.Hash == installedHash {
				ip.Upgrade = append(ip.Upgrade, id)
			} else {
				ip.ModifiedOwned = append(ip.ModifiedOwned, id)
			}
			continue
		}
		ip.Collisions = append(ip.Collisions, id)
	}
	return ip
}

func RenamedID(id item.ID, pkgName string) item.ID {
	return item.NewID(id.Type(), id.Name()+"-"+pkgName)
}

func canRename(id item.ID) bool {
	t := id.Type()
	return t == item.TypeSkill || t == item.TypeAgent || t == item.TypeCommand
}

type InstallSummary struct {
	Installed, Upgraded, Renamed, Replaced, Skipped, Current int
	SnapshotDir                                              string
}

func ApplyInstall(claudeDir, backupsDir, ledgerPath string, man *Manifest,
	contents map[item.ID]scan.Scanned, ip InstallPlan,
	collisions map[item.ID]CollisionChoice, modified map[item.ID]ModifiedChoice,
) (*InstallSummary, error) {
	sum := &InstallSummary{Current: len(ip.AlreadyCurrent)}
	led, err := state.LoadLedger(ledgerPath)
	if err != nil {
		return nil, err
	}
	rec := state.PackageRecord{Version: man.Version, Items: map[item.ID]string{}}
	if old, ok := led.Packages[man.Name]; ok {
		for id, h := range old.Items {
			rec.Items[id] = h // carry forward; overwritten below where reinstalled
		}
	}

	// synthetic pull changes against the pack contents
	var changes []plan.Change
	remote := map[item.ID]scan.Scanned{}
	pull := func(targetID item.ID, src scan.Scanned) {
		src.ID = targetID
		remote[targetID] = src
		changes = append(changes, plan.Change{
			Result: classify.Result{ID: targetID, State: classify.NewRemote},
			Action: plan.ActPull,
		})
		rec.Items[targetID] = src.Hash
	}

	for _, id := range ip.Fresh {
		pull(id, contents[id])
		sum.Installed++
	}
	for _, id := range ip.AlreadyCurrent {
		rec.Items[id] = contents[id].Hash
	}
	for _, id := range ip.Upgrade {
		pull(id, contents[id])
		sum.Upgraded++
	}
	for _, id := range ip.ModifiedOwned {
		if modified[id] == TakePackage {
			pull(id, contents[id])
			sum.Upgraded++
		} else {
			sum.Skipped++ // keep-local: ledger keeps the old installed hash
		}
	}
	for _, id := range ip.Collisions {
		choice := collisions[id]
		if choice == ChoiceRename && !canRename(id) {
			choice = ChoiceSkip
		}
		switch choice {
		case ChoiceRename:
			pull(RenamedID(id, man.Name), contents[id])
			sum.Renamed++
		case ChoiceReplace:
			pull(id, contents[id])
			sum.Replaced++
		default:
			sum.Skipped++
		}
	}

	// snapshot everything the pulls will touch
	var rels []string
	needSettings := false
	for _, c := range changes {
		switch c.Result.ID.Type() {
		case item.TypeSetting, item.TypePlugins:
			needSettings = true
		default:
			rels = append(rels, apply.LocalRelPath(c.Result.ID))
		}
	}
	if needSettings {
		rels = append(rels, "settings.json")
	}
	snap, err := apply.SnapshotPaths(claudeDir, backupsDir, rels)
	if err != nil {
		return nil, err
	}
	sum.SnapshotDir = snap

	a := apply.Applier{ClaudeDir: claudeDir, BackupsDir: backupsDir}
	if _, err := a.Apply(changes, nil, remote); err != nil {
		return sum, fmt.Errorf("install failed (restore with: skill-sync rollback): %w", err)
	}
	led.Packages[man.Name] = rec
	return sum, led.Save(ledgerPath)
}

func Uninstall(claudeDir, backupsDir, ledgerPath, pkgName string) (removed, kept []item.ID, err error) {
	led, err := state.LoadLedger(ledgerPath)
	if err != nil {
		return nil, nil, err
	}
	rec, ok := led.Packages[pkgName]
	if !ok {
		return nil, nil, fmt.Errorf("package %q is not installed", pkgName)
	}
	local, _, err := scan.Claude(claudeDir, defaultOverrides())
	if err != nil {
		return nil, nil, err
	}
	var changes []plan.Change
	var rels []string
	for id, installedHash := range rec.Items {
		loc, present := local[id]
		if !present {
			removed = append(removed, id) // already gone
			continue
		}
		if loc.Hash != installedHash {
			kept = append(kept, id)
			continue
		}
		changes = append(changes, plan.Change{
			Result: classify.Result{ID: id, State: classify.DeletedRemote},
			Action: plan.ActDeleteLocal,
		})
		if id.Type() == item.TypeSetting || id.Type() == item.TypePlugins {
			rels = append(rels, "settings.json")
		} else {
			rels = append(rels, apply.LocalRelPath(id))
		}
		removed = append(removed, id)
	}
	if _, err := apply.SnapshotPaths(claudeDir, backupsDir, rels); err != nil {
		return nil, nil, err
	}
	a := apply.Applier{ClaudeDir: claudeDir, BackupsDir: backupsDir}
	if _, err := a.Apply(changes, local, nil); err != nil {
		return removed, kept, err
	}
	delete(led.Packages, pkgName)
	return removed, kept, led.Save(ledgerPath)
}

func defaultOverrides() (o settingsOverrides) { return }

// settingsOverrides aliases settings.KeyOverrides via a tiny indirection
// to avoid importing settings here just for the zero value — replace
// with settings.KeyOverrides{} directly if you prefer the import.
```
(Implementation note: just import `settings` and use `settings.KeyOverrides{}`; drop the `defaultOverrides` indirection — it's shown here only to flag the choice. The real file should import settings directly.)

- [ ] **Step 4: Run tests** — `go test ./internal/pack/ ./internal/apply/` → PASS (apply tests still green after the SnapshotPaths refactor).

- [ ] **Step 5: Implement the three commands**

`cmd/install.go`:
```go
package cmd

import (
	"fmt"
	"os"

	"github.com/charmbracelet/huh"
	"github.com/daikazu/skill-sync/internal/item"
	"github.com/daikazu/skill-sync/internal/pack"
	"github.com/daikazu/skill-sync/internal/scan"
	"github.com/daikazu/skill-sync/internal/settings"
	"github.com/daikazu/skill-sync/internal/state"
	"github.com/spf13/cobra"
)

var installYes bool

var installCmd = &cobra.Command{
	Use:   "install <file.skillpack>",
	Short: "Install or upgrade a skill package (never clobbers your own items)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		tmp, err := os.MkdirTemp("", "skillpack-install-*")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmp)
		man, contents, err := pack.Load(args[0], tmp)
		if err != nil {
			return err
		}
		local, warns, err := scan.Claude(flagClaudeDir, settings.KeyOverrides{})
		if err != nil {
			return err
		}
		for _, w := range warns {
			fmt.Println("warning:", w)
		}
		led, err := state.LoadLedger(getSyncer().LedgerPath())
		if err != nil {
			return err
		}
		ip := pack.BuildInstallPlan(man, contents, local, led, man.Name)

		collisions := map[item.ID]pack.CollisionChoice{}
		for _, id := range ip.Collisions {
			choice := pack.ChoiceSkip
			if !installYes {
				opts := []huh.Option[pack.CollisionChoice]{
					huh.NewOption("skip (keep mine, don't install theirs)", pack.ChoiceSkip),
					huh.NewOption(fmt.Sprintf("install renamed as %s", pack.RenamedID(id, man.Name)), pack.ChoiceRename),
					huh.NewOption("replace mine (snapshotted, reversible)", pack.ChoiceReplace),
				}
				if err := huh.NewSelect[pack.CollisionChoice]().
					Title(fmt.Sprintf("%s already exists and differs from the package", id)).
					Options(opts...).Value(&choice).Run(); err != nil {
					return err
				}
			}
			collisions[id] = choice
		}
		modified := map[item.ID]pack.ModifiedChoice{}
		for _, id := range ip.ModifiedOwned {
			choice := pack.KeepLocal
			if !installYes {
				if err := huh.NewSelect[pack.ModifiedChoice]().
					Title(fmt.Sprintf("%s was installed by %s but you have edited it", id, man.Name)).
					Options(
						huh.NewOption("keep my edited version", pack.KeepLocal),
						huh.NewOption("take the package version (snapshotted)", pack.TakePackage),
					).Value(&choice).Run(); err != nil {
					return err
				}
			}
			modified[id] = choice
		}

		sum, err := pack.ApplyInstall(flagClaudeDir, getSyncer().ClaudeDir+"/backups/skill-sync",
			getSyncer().LedgerPath(), man, contents, ip, collisions, modified)
		if err != nil {
			return err
		}
		fmt.Printf("%s %s: %d installed, %d upgraded, %d renamed, %d replaced, %d skipped, %d already current\n",
			man.Name, man.Version, sum.Installed, sum.Upgraded, sum.Renamed, sum.Replaced, sum.Skipped, sum.Current)
		if sum.SnapshotDir != "" {
			fmt.Println("pre-install snapshot:", sum.SnapshotDir)
		}
		return nil
	},
}

func init() {
	installCmd.Flags().BoolVarP(&installYes, "yes", "y", false, "accept safe defaults (skip collisions, keep local edits)")
	rootCmd.AddCommand(installCmd)
}
```

`cmd/uninstall.go`:
```go
package cmd

import (
	"fmt"

	"github.com/daikazu/skill-sync/internal/pack"
	"github.com/spf13/cobra"
)

var uninstallCmd = &cobra.Command{
	Use:   "uninstall <package-name>",
	Short: "Remove a package's items (keeps anything you've modified)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		s := getSyncer()
		removed, kept, err := pack.Uninstall(flagClaudeDir,
			flagClaudeDir+"/backups/skill-sync", s.LedgerPath(), args[0])
		if err != nil {
			return err
		}
		fmt.Printf("removed %d item(s)\n", len(removed))
		for _, id := range kept {
			fmt.Printf("kept (you modified it): %s\n", id)
		}
		return nil
	},
}

func init() { rootCmd.AddCommand(uninstallCmd) }
```

`cmd/packages.go`:
```go
package cmd

import (
	"fmt"

	"github.com/daikazu/skill-sync/internal/scan"
	"github.com/daikazu/skill-sync/internal/settings"
	"github.com/daikazu/skill-sync/internal/state"
	"github.com/spf13/cobra"
)

var packagesCmd = &cobra.Command{
	Use:   "packages",
	Short: "List installed skill packages",
	RunE: func(cmd *cobra.Command, args []string) error {
		led, err := state.LoadLedger(getSyncer().LedgerPath())
		if err != nil {
			return err
		}
		if len(led.Packages) == 0 {
			fmt.Println("no packages installed")
			return nil
		}
		local, _, err := scan.Claude(flagClaudeDir, settings.KeyOverrides{})
		if err != nil {
			return err
		}
		for name, rec := range led.Packages {
			fmt.Printf("%s %s (%d items)\n", name, rec.Version, len(rec.Items))
			for id, h := range rec.Items {
				marker := ""
				if loc, ok := local[id]; !ok {
					marker = " (missing)"
				} else if loc.Hash != h {
					marker = " (modified)"
				}
				fmt.Printf("  %s%s\n", id, marker)
			}
		}
		return nil
	},
}

func init() { rootCmd.AddCommand(packagesCmd) }
```

- [ ] **Step 6: Run all tests + build** — `go test ./... && go build ./...` → PASS.

- [ ] **Step 7: Commit** — `git add -A && git commit -m "feat: package install/uninstall with ownership ledger and no-clobber prompts"`

---

### Task 15: log, rollback, and config commands

**Files:**
- Create: `cmd/log.go`, `cmd/rollback.go`, `cmd/config.go`
- Modify: `internal/apply/snapshot.go` (add `ListSnapshots`)
- Test: extend `internal/apply/apply_test.go` (ListSnapshots)

**Interfaces:**
- Consumes: `repo`, `apply`, `state`
- Produces:
  - `apply.ListSnapshots(backupsDir string) ([]string, error)` — snapshot dir names (not full paths), sorted newest first; missing backups dir → empty, no error.
  - `skill-sync log` — prints `repo.Git.Log(20)` as `<hash>  <date>  <subject>`.
  - `skill-sync rollback [name]` — `--list` prints snapshots; no arg restores the newest (with a huh confirm showing its name unless `--yes`); with arg restores that snapshot. Prints restored dir.
  - `skill-sync config` — `config show` prints effective config (defaults + overrides + policies); `config include-key <key>` / `config exclude-key <key>` / `config policy <item-id> <never-sync|always-ask|default>` mutate `config.json`; bare `config` = `config show`.

- [ ] **Step 1: Failing test for ListSnapshots** (append to `internal/apply/apply_test.go`)

```go
func TestListSnapshots(t *testing.T) {
	backups := filepath.Join(t.TempDir(), "b")
	names, err := ListSnapshots(backups)
	if err != nil || len(names) != 0 {
		t.Fatalf("missing dir: %v %v", names, err)
	}
	os.MkdirAll(filepath.Join(backups, "2026-08-30T10-00-00Z"), 0o755)
	os.MkdirAll(filepath.Join(backups, "2026-08-30T12-00-00Z"), 0o755)
	names, _ = ListSnapshots(backups)
	if len(names) != 2 || names[0] != "2026-08-30T12-00-00Z" {
		t.Fatalf("want newest first: %v", names)
	}
}
```

- [ ] **Step 2: Implement ListSnapshots** (in `internal/apply/snapshot.go`)

```go
func ListSnapshots(backupsDir string) ([]string, error) {
	entries, err := os.ReadDir(backupsDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	return names, nil
}
```

- [ ] **Step 3: Implement the commands**

`cmd/log.go`:
```go
package cmd

import (
	"fmt"

	"github.com/daikazu/skill-sync/internal/repo"
	"github.com/spf13/cobra"
)

var logCmd = &cobra.Command{
	Use:   "log",
	Short: "Show recent sync history",
	RunE: func(cmd *cobra.Command, args []string) error {
		g := repo.Git{Dir: getSyncer().RepoDir()}
		entries, err := g.Log(20)
		if err != nil {
			return err
		}
		if len(entries) == 0 {
			fmt.Println("no syncs yet")
		}
		for _, e := range entries {
			fmt.Printf("%s  %s  %s\n", e.Hash, e.Date, e.Subject)
		}
		return nil
	},
}

func init() { rootCmd.AddCommand(logCmd) }
```

`cmd/rollback.go`:
```go
package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/charmbracelet/huh"
	"github.com/daikazu/skill-sync/internal/apply"
	"github.com/spf13/cobra"
)

var rollbackList, rollbackYes bool

var rollbackCmd = &cobra.Command{
	Use:   "rollback [snapshot-name]",
	Short: "Restore ~/.claude files from a pre-apply snapshot",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		backups := filepath.Join(flagClaudeDir, "backups", "skill-sync")
		names, err := apply.ListSnapshots(backups)
		if err != nil {
			return err
		}
		if rollbackList {
			for _, n := range names {
				fmt.Println(n)
			}
			return nil
		}
		if len(names) == 0 {
			return fmt.Errorf("no snapshots found in %s", backups)
		}
		name := names[0]
		if len(args) == 1 {
			name = args[0]
		}
		if !rollbackYes {
			var ok bool
			if err := huh.NewConfirm().
				Title(fmt.Sprintf("Restore snapshot %s over %s?", name, flagClaudeDir)).
				Value(&ok).Run(); err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("aborted")
			}
		}
		if err := apply.Restore(filepath.Join(backups, name), flagClaudeDir); err != nil {
			return err
		}
		fmt.Println("restored", name)
		return nil
	},
}

func init() {
	rollbackCmd.Flags().BoolVar(&rollbackList, "list", false, "list snapshots")
	rollbackCmd.Flags().BoolVarP(&rollbackYes, "yes", "y", false, "skip confirmation")
	rootCmd.AddCommand(rollbackCmd)
}
```

`cmd/config.go`:
```go
package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/daikazu/skill-sync/internal/item"
	"github.com/daikazu/skill-sync/internal/settings"
	"github.com/daikazu/skill-sync/internal/state"
	"github.com/spf13/cobra"
)

func configPath() string { return filepath.Join(flagSyncDir, "config.json") }

func mutateConfig(f func(*state.Config) error) error {
	cfg, err := state.LoadConfig(configPath())
	if err != nil {
		return err
	}
	if err := f(cfg); err != nil {
		return err
	}
	return cfg.Save(configPath())
}

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Show or change what syncs",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := state.LoadConfig(configPath())
		if err != nil {
			return err
		}
		fmt.Println("remote:", cfg.Remote)
		fmt.Println("default shareable settings keys:", settings.DefaultShareable)
		fmt.Println("extra included keys:", cfg.IncludeKeys)
		fmt.Println("excluded keys:", cfg.ExcludeKeys)
		if len(cfg.Policies) == 0 {
			fmt.Println("policies: none")
		}
		for id, p := range cfg.Policies {
			fmt.Printf("policy: %s → %s\n", id, p)
		}
		return nil
	},
}

var configIncludeCmd = &cobra.Command{
	Use:   "include-key <settings-key>",
	Short: "Also sync this settings.json key",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return mutateConfig(func(c *state.Config) error {
			c.IncludeKeys = appendUnique(c.IncludeKeys, args[0])
			c.ExcludeKeys = remove(c.ExcludeKeys, args[0])
			return nil
		})
	},
}

var configExcludeCmd = &cobra.Command{
	Use:   "exclude-key <settings-key>",
	Short: "Stop syncing this settings.json key",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return mutateConfig(func(c *state.Config) error {
			c.ExcludeKeys = appendUnique(c.ExcludeKeys, args[0])
			c.IncludeKeys = remove(c.IncludeKeys, args[0])
			return nil
		})
	},
}

var configPolicyCmd = &cobra.Command{
	Use:   "policy <item-id> <never-sync|always-ask|default>",
	Short: "Set a per-item sync policy",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := item.Parse(args[0])
		if err != nil {
			return err
		}
		return mutateConfig(func(c *state.Config) error {
			if c.Policies == nil {
				c.Policies = map[item.ID]state.Policy{}
			}
			switch args[1] {
			case "never-sync":
				c.Policies[id] = state.PolicyNeverSync
			case "always-ask":
				c.Policies[id] = state.PolicyAlwaysAsk
			case "default":
				delete(c.Policies, id)
			default:
				return fmt.Errorf("unknown policy %q", args[1])
			}
			return nil
		})
	},
}

func appendUnique(s []string, v string) []string {
	for _, x := range s {
		if x == v {
			return s
		}
	}
	return append(s, v)
}

func remove(s []string, v string) []string {
	var out []string
	for _, x := range s {
		if x != v {
			out = append(out, x)
		}
	}
	return out
}

func init() {
	configCmd.AddCommand(configIncludeCmd, configExcludeCmd, configPolicyCmd)
	rootCmd.AddCommand(configCmd)
}
```

- [ ] **Step 4: Run tests + build** — `go test ./... && go vet ./...` → PASS.

- [ ] **Step 5: Commit** — `git add -A && git commit -m "feat: log, rollback, and config commands"`

---

### Task 16: release pipeline + README

**Files:**
- Create: `.goreleaser.yaml`, `.github/workflows/release.yml`, `README.md`, `.gitignore`

**Interfaces:**
- Consumes: nothing from code
- Produces: tagged pushes (`v*`) build darwin/linux × amd64/arm64 binaries and push a formula to `daikazu/homebrew-tap`, installable as `brew install daikazu/tap/skill-sync`.

- [ ] **Step 1: Write `.goreleaser.yaml`**

```yaml
version: 2
project_name: skill-sync

builds:
  - main: .
    binary: skill-sync
    env: [CGO_ENABLED=0]
    goos: [darwin, linux]
    goarch: [amd64, arm64]
    ldflags: ["-s -w -X github.com/daikazu/skill-sync/cmd.version={{.Version}}"]

archives:
  - formats: [tar.gz]
    name_template: "{{ .ProjectName }}_{{ .Os }}_{{ .Arch }}"

brews:
  - name: skill-sync
    repository:
      owner: daikazu
      name: homebrew-tap
      token: "{{ .Env.TAP_GITHUB_TOKEN }}"
    homepage: https://github.com/daikazu/skill-sync
    description: "Sync Claude Code skills, agents, commands, rules, and settings between machines"
    test: |
      system "#{bin}/skill-sync --help"

checksum:
  name_template: checksums.txt
```

Also add a `version` var + `--version` support in `cmd/root.go`:
```go
var version = "dev"
// in rootCmd definition:
// Version: version,
```
(cobra renders `skill-sync --version` automatically when `Version` is set.)

- [ ] **Step 2: Write `.github/workflows/release.yml`**

```yaml
name: release
on:
  push:
    tags: ["v*"]
permissions:
  contents: write
jobs:
  goreleaser:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with: { fetch-depth: 0 }
      - uses: actions/setup-go@v5
        with: { go-version: stable }
      - run: go test ./...
      - uses: goreleaser/goreleaser-action@v6
        with:
          version: "~> v2"
          args: release --clean
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          TAP_GITHUB_TOKEN: ${{ secrets.TAP_GITHUB_TOKEN }}
```

- [ ] **Step 3: Write `.gitignore`** (`skill-sync` binary, `dist/`) **and `README.md`** covering: what it is (one paragraph), install (`brew install daikazu/tap/skill-sync`), quickstart (create a private GitHub repo → `skill-sync init git@github.com:you/claude-sync.git` → `skill-sync sync` on each machine), the full command table from the spec's CLI surface section, how conflicts work (3-way, TUI choices), packages (backup `pack --all`, team `pack` + `install`, no-clobber promise), and where snapshots live (`~/.claude/backups/skill-sync`, `skill-sync rollback`). Note the `TAP_GITHUB_TOKEN` repo secret requirement (a PAT with write access to `daikazu/homebrew-tap`) for releasing.

- [ ] **Step 4: Validate** — `goreleaser check` if goreleaser is installed locally (skip otherwise; CI validates on first tag). `go build ./... && go test ./...` one final time.

- [ ] **Step 5: Commit** — `git add -A && git commit -m "chore: goreleaser + release workflow + README"`

---

## Plan Self-Review Notes

- **Spec coverage:** init/sync/status/log/rollback (T10-11, 15), TUI review with explicit deletion listing (T12 — deletions appear in the auto plan summary printed by `cmd/sync.go`), pack/install/uninstall/packages with ownership + rename/skip/replace + modified-item protection (T13-14), settings key-level items and plugins entry-level items (T3-4), snapshots before every local write including installs/uninstalls (T9, T14), push-rejection retry without force-push (T8, T10), tarball hygiene (T13), unparseable-settings flag-and-skip (T4), release via tap (T16). Config TUI from the spec is delivered as `config` subcommands (spec's intent — editing shareable keys and per-item policies — is fully covered; a full-screen form adds no capability).
- **Known simplifications (intentional):** `plan.Plan` carries `Local`/`Remote` scan maps for review UIs (added in T12); pack staging reuses `repo.WriteItem` so the pack layout is definitionally the repo layout; `install.go`'s `defaultOverrides` indirection must be replaced by a direct `settings.KeyOverrides{}` import as noted inline.
- **Type consistency check:** `scan.Claude/Repo` return `(map, []string, error)` everywhere (T4 updated; T9, T10, T13, T14 call sites match); `apply.LocalRelPath` exported in T14's refactor — T9 defines it lowercase, T14 renames it and updates call sites; `Syncer.LedgerPath()` exported because cmd/install and cmd/packages use it (T10 defines it exported).

