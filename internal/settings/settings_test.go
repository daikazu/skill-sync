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

func TestKeyAllowed(t *testing.T) {
	cases := []struct {
		key  string
		o    KeyOverrides
		want bool
	}{
		{"model", KeyOverrides{}, true},
		{"effortLevel", KeyOverrides{}, true},
		{"env", KeyOverrides{}, false},
		{"customThing", KeyOverrides{}, false},
		{"customThing", KeyOverrides{Include: []string{"customThing"}}, true},
		{"model", KeyOverrides{Exclude: []string{"model"}}, false},
		// exclude wins over include
		{"model", KeyOverrides{Include: []string{"model"}, Exclude: []string{"model"}}, false},
		// plugin keys are never settings, even when included
		{KeyEnabledPlugins, KeyOverrides{Include: []string{KeyEnabledPlugins}}, false},
		{KeyExtraMarketplaces, KeyOverrides{}, false},
	}
	for _, c := range cases {
		if got := KeyAllowed(c.key, c.o); got != c.want {
			t.Errorf("KeyAllowed(%q, %+v) = %v, want %v", c.key, c.o, got, c.want)
		}
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
