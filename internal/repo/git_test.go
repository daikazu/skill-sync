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
