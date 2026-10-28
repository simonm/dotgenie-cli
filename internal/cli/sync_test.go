package cli

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simonm/dotgenie/internal/config"
)

// These run real git: a bare remote and two clones, as two machines.

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := git(dir, args...)
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return out
}

func writeRepoFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// machines returns a bare remote with one commit on main, and two clones.
func machines(t *testing.T) (remote, a, b string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	t.Setenv("GIT_AUTHOR_NAME", "test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	root := t.TempDir()
	remote = filepath.Join(root, "remote.git")
	seed := filepath.Join(root, "seed")
	a = filepath.Join(root, "a")
	b = filepath.Join(root, "b")
	run(t, root, "init", "-q", "--bare", "-b", "main", remote)
	run(t, root, "init", "-q", "-b", "main", seed)
	writeRepoFile(t, filepath.Join(seed, "dotfiles/common/home/.bashrc"), "one\n")
	run(t, seed, "add", "-A")
	run(t, seed, "commit", "-q", "-m", "seed")
	run(t, seed, "remote", "add", "origin", remote)
	run(t, seed, "push", "-q", "origin", "main")
	run(t, root, "clone", "-q", remote, a)
	run(t, root, "clone", "-q", remote, b)
	return remote, a, b
}

func TestSyncCommitsPushesAndPulls(t *testing.T) {
	remote, a, b := machines(t)
	writeRepoFile(t, filepath.Join(a, "dotfiles/common/home/.bashrc"), "two\n")
	r, err := gitSync(a, gitSyncOpts{Host: "boron"})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Committed) != 1 || r.Pushed != 1 {
		t.Fatalf("a: %+v", r)
	}
	if msg := run(t, remote, "log", "-1", "--format=%s", "main"); msg != "Update .bashrc (boron)" {
		t.Fatalf("remote head: %q", msg)
	}
	r, err = gitSync(b, gitSyncOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Pulled != 1 || r.Pushed != 0 {
		t.Fatalf("b: %+v", r)
	}
	if got, _ := os.ReadFile(filepath.Join(b, "dotfiles/common/home/.bashrc")); string(got) != "two\n" {
		t.Fatalf("b has %q", got)
	}
}

func TestSyncRebasesDifferentFiles(t *testing.T) {
	remote, a, b := machines(t)
	writeRepoFile(t, filepath.Join(a, "dotfiles/common/home/.vimrc"), "a\n")
	if _, err := gitSync(a, gitSyncOpts{}); err != nil {
		t.Fatal(err)
	}
	writeRepoFile(t, filepath.Join(b, "dotfiles/common/home/.zshrc"), "b\n")
	r, err := gitSync(b, gitSyncOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Pulled != 1 || r.Pushed != 1 {
		t.Fatalf("b: %+v", r)
	}
	// Linear history on the remote: seed, a, b.
	if n := run(t, remote, "rev-list", "--count", "main"); n != "3" {
		t.Fatalf("remote has %s commits", n)
	}
	if merges := run(t, remote, "rev-list", "--merges", "main"); merges != "" {
		t.Fatalf("merge commits: %s", merges)
	}
}

func TestSyncStopsOnConflict(t *testing.T) {
	_, a, b := machines(t)
	writeRepoFile(t, filepath.Join(a, "dotfiles/common/home/.bashrc"), "from a\n")
	if _, err := gitSync(a, gitSyncOpts{}); err != nil {
		t.Fatal(err)
	}
	writeRepoFile(t, filepath.Join(b, "dotfiles/common/home/.bashrc"), "from b\n")
	_, err := gitSync(b, gitSyncOpts{Host: "lithium"})
	if !errors.Is(err, errConflict) {
		t.Fatalf("want a conflict, got %v", err)
	}
	if !strings.Contains(err.Error(), "dotfiles/common/home/.bashrc") {
		t.Fatalf("conflict doesn't name the file: %v", err)
	}
	// The rebase is undone and b's own commit is kept, with its content.
	if _, err := os.Stat(filepath.Join(b, ".git/rebase-merge")); err == nil {
		t.Fatal("left mid-rebase")
	}
	if msg := run(t, b, "log", "-1", "--format=%s"); msg != "Update .bashrc (lithium)" {
		t.Fatalf("b's head: %q", msg)
	}
	if got, _ := os.ReadFile(filepath.Join(b, "dotfiles/common/home/.bashrc")); string(got) != "from b\n" {
		t.Fatalf("b's file: %q", got)
	}
}

func TestSyncPushRejectedIsANote(t *testing.T) {
	remote, a, _ := machines(t)
	hook := filepath.Join(remote, "hooks/pre-receive")
	writeRepoFile(t, hook, "#!/bin/sh\necho no push access >&2\nexit 1\n")
	if err := os.Chmod(hook, 0o755); err != nil {
		t.Fatal(err)
	}
	writeRepoFile(t, filepath.Join(a, "dotfiles/common/home/.bashrc"), "two\n")
	r, err := gitSync(a, gitSyncOpts{})
	if err != nil {
		t.Fatalf("a failed push should be a note: %v", err)
	}
	if r.Pushed != 0 || !strings.Contains(strings.Join(r.Notes, "\n"), "Committed, not pushed") {
		t.Fatalf("%+v", r)
	}
	if run(t, a, "status", "--porcelain") != "" {
		t.Fatal("the change wasn't committed")
	}
}

func TestSyncOffline(t *testing.T) {
	_, a, _ := machines(t)
	run(t, a, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "gone.git"))
	writeRepoFile(t, filepath.Join(a, "dotfiles/common/home/.bashrc"), "two\n")
	r, err := gitSync(a, gitSyncOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Committed) != 1 || !strings.Contains(strings.Join(r.Notes, "\n"), "Fetch failed") {
		t.Fatalf("%+v", r)
	}
}

func TestSyncNeverCreatesARemoteBranch(t *testing.T) {
	remote, a, _ := machines(t)
	run(t, a, "switch", "-q", "-c", "try-something")
	writeRepoFile(t, filepath.Join(a, "dotfiles/common/home/.bashrc"), "two\n")
	r, err := gitSync(a, gitSyncOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Pushed != 0 {
		t.Fatalf("%+v", r)
	}
	if out, _ := git(remote, "rev-parse", "--verify", "--quiet", "try-something"); out != "" {
		t.Fatal("branch created on the remote")
	}
}

func TestSyncDryRunChangesNothing(t *testing.T) {
	remote, a, b := machines(t)
	writeRepoFile(t, filepath.Join(b, "dotfiles/common/home/.vimrc"), "b\n")
	if _, err := gitSync(b, gitSyncOpts{}); err != nil {
		t.Fatal(err)
	}
	before := run(t, remote, "rev-parse", "main")
	writeRepoFile(t, filepath.Join(a, "dotfiles/common/home/.bashrc"), "two\n")
	r, err := gitSync(a, gitSyncOpts{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	notes := strings.Join(r.Notes, "\n")
	for _, want := range []string{"Would commit", "Would pull 1", "Would push 1"} {
		if !strings.Contains(notes, want) {
			t.Errorf("notes lack %q:\n%s", want, notes)
		}
	}
	if run(t, a, "status", "--porcelain") == "" {
		t.Error("dry run committed")
	}
	if run(t, remote, "rev-parse", "main") != before {
		t.Error("dry run pushed")
	}
	if _, err := os.Stat(filepath.Join(a, "dotfiles/common/home/.vimrc")); err == nil {
		t.Error("dry run pulled")
	}
}

func TestPackagesHash(t *testing.T) {
	d := t.TempDir()
	writeRepoFile(t, filepath.Join(d, "packages/common.yml"), "packages: [git]\n")
	writeRepoFile(t, filepath.Join(d, "packages/server.yml"), "packages: []\n")
	cfg := &config.Config{OS: "debian", SystemType: "server"}
	h := packagesHash(d, cfg)
	if packagesHash(d, cfg) != h {
		t.Fatal("not stable")
	}
	writeRepoFile(t, filepath.Join(d, "packages/server.yml"), "packages: [tmux]\n")
	if packagesHash(d, cfg) == h {
		t.Error("type file change not seen")
	}
	h = packagesHash(d, cfg)
	writeRepoFile(t, filepath.Join(d, "packages/server.yml"), "packages: [vim]\n\n")
	h2 := packagesHash(d, cfg)
	writeRepoFile(t, filepath.Join(d, "packages/server.yml"), "packages: [git]\n\n")
	if packagesHash(d, cfg) == h2 {
		t.Error("same-length change not seen")
	}
	writeRepoFile(t, filepath.Join(d, "packages/server.yml"), "packages: [tmux]\n")
	writeRepoFile(t, filepath.Join(d, "packages/workstation.yml"), "packages: [firefox]\n")
	if packagesHash(d, cfg) != h {
		t.Error("another type's file changed the hash")
	}
	if packagesHash(d, &config.Config{OS: "arch", SystemType: "server"}) == h {
		t.Error("OS change not seen")
	}
	if packagesHash(d, &config.Config{OS: "debian", SystemType: "workstation"}) == h {
		t.Error("type change not seen")
	}
}
