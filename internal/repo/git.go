package repo

import (
	"errors"
	"fmt"
	"os"
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
	// Force English output so the substring checks for push rejection and
	// empty-remote work on every locale, and fail fast instead of hanging
	// on an interactive credential prompt.
	cmd.Env = append(os.Environ(), "LC_ALL=C", "LANG=C", "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("git %s: %w\n%s", args[0], err, out)
	}
	return string(out), nil
}
