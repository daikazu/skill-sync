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

func TestTreeIgnoresVCSDirs(t *testing.T) {
	mk := func(files map[string]string) string {
		d := t.TempDir()
		for rel, content := range files {
			p := filepath.Join(d, rel)
			os.MkdirAll(filepath.Dir(p), 0o755)
			os.WriteFile(p, []byte(content), 0o644)
		}
		return d
	}
	clean := mk(map[string]string{"SKILL.md": "x", "ref/notes.md": "y"})
	withGit := mk(map[string]string{
		"SKILL.md": "x", "ref/notes.md": "y",
		".git/config": "[core]\n\tbare = false\n",
		".git/HEAD":   "ref: refs/heads/main\n",
	})
	hClean, err := Tree(clean)
	if err != nil {
		t.Fatal(err)
	}
	hWithGit, err := Tree(withGit)
	if err != nil {
		t.Fatal(err)
	}
	if hClean != hWithGit {
		t.Fatal(".git/ contents must be excluded from the tree hash")
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
