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

func TestParseAuthor(t *testing.T) {
	good := map[string][2]string{
		"Simon Mullis <simon@mullis.co.uk>":     {"Simon Mullis", "simon@mullis.co.uk"},
		"  Simon Mullis   <simon@mullis.co.uk> ": {"Simon Mullis", "simon@mullis.co.uk"},
		"simonm <s@example.com>":                {"simonm", "s@example.com"},
	}
	for in, want := range good {
		name, email, ok := parseAuthor(in)
		if !ok || name != want[0] || email != want[1] {
			t.Errorf("%q: got %q %q %v", in, name, email, ok)
		}
	}
	for _, in := range []string{"", "Simon Mullis", "<simon@mullis.co.uk>", "Simon <not-an-email>", "Simon <a@b> extra", "Simon <<a@b>>"} {
		if _, _, ok := parseAuthor(in); ok {
			t.Errorf("%q parsed as an author", in)
		}
	}
}

// noIdentity is lithium on 2026-10-28: git with no user.name or user.email
// anywhere, and none in the environment.
func noIdentity(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	for _, k := range []string{"GIT_AUTHOR_NAME", "GIT_AUTHOR_EMAIL", "GIT_COMMITTER_NAME", "GIT_COMMITTER_EMAIL", "EMAIL"} {
		t.Setenv(k, "") // restored after the test
		_ = os.Unsetenv(k)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	dir := t.TempDir()
	run(t, dir, "init", "-q", "-b", "main")
	// Stop git guessing an identity from the user and host name.
	run(t, dir, "config", "user.useConfigOnly", "true")
	writeRepoFile(t, filepath.Join(dir, "dotfiles/server/home/.config/btop/btop.conf"), "x\n")
	run(t, dir, "add", "-A")
	return dir
}

func TestCommitWithNoIdentityExplainsTheFix(t *testing.T) {
	dir := noIdentity(t)
	err := gitCommit(dir, "", "Adopt btop.conf")
	if !errors.Is(err, errNoIdentity) {
		t.Fatalf("got %v", err)
	}
	for _, want := range []string{"git_author", "config.yml"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message lacks %q: %v", want, err)
		}
	}
}

func TestCommitAsGitAuthor(t *testing.T) {
	dir := noIdentity(t)
	if err := gitCommit(dir, "Simon Mullis <simon@mullis.co.uk>", "Adopt btop.conf"); err != nil {
		t.Fatal(err)
	}
	if got := run(t, dir, "log", "-1", "--format=%an <%ae>|%cn <%ce>"); got != "Simon Mullis <simon@mullis.co.uk>|Simon Mullis <simon@mullis.co.uk>" {
		t.Fatalf("committed as %q", got)
	}
	// git's own config is untouched.
	if id := gitIdentity(dir); id != "" {
		t.Fatalf("git config gained an identity: %q", id)
	}
}

func TestCommitBadAuthor(t *testing.T) {
	dir := noIdentity(t)
	err := gitCommit(dir, "Simon Mullis", "x")
	if err == nil || !strings.Contains(err.Error(), "should look like") {
		t.Fatalf("got %v", err)
	}
}

// A git failure carries git's words, not "exit status 128".
func TestCommitFailureShowsGitMessage(t *testing.T) {
	dir := noIdentity(t)
	if err := gitCommit(dir, "A B <a@b.c>", "first"); err != nil {
		t.Fatal(err)
	}
	err := gitCommit(dir, "A B <a@b.c>", "nothing staged")
	if err == nil {
		t.Fatal("empty commit succeeded")
	}
	if strings.Contains(err.Error(), "exit status") || !strings.Contains(err.Error(), "nothing") {
		t.Fatalf("got %v", err)
	}
}

func TestSyncCommitsAsGitAuthorWithoutIdentity(t *testing.T) {
	dir := noIdentity(t)
	run(t, dir, "reset", "-q")
	r, err := gitSync(dir, gitSyncOpts{Host: "lithium", Author: "Simon Mullis <simon@mullis.co.uk>"})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Committed) != 1 {
		t.Fatalf("%+v", r)
	}
	if got := run(t, dir, "log", "-1", "--format=%an|%s"); got != "Simon Mullis|Update btop.conf (lithium)" {
		t.Fatalf("got %q", got)
	}
}

func TestRecordGitAuthor(t *testing.T) {
	dir := noIdentity(t)
	paths := config.Paths{DotfilesDir: dir}
	cfgPath := filepath.Join(dir, "config.yml")

	// No identity here: nothing recorded.
	cfg := &config.Config{RepoVersion: 6}
	recordGitAuthor(paths, cfg)
	if cfg.GitAuthor != "" {
		t.Fatalf("recorded %q from nothing", cfg.GitAuthor)
	}

	// A machine with an identity (Xenon) records it for the others.
	run(t, dir, "config", "user.name", "Simon Mullis")
	run(t, dir, "config", "user.email", "simon@mullis.co.uk")
	recordGitAuthor(paths, cfg)
	loaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.GitAuthor != "Simon Mullis <simon@mullis.co.uk>" || loaded.RepoVersion != 6 {
		t.Fatalf("config.yml: %+v", loaded)
	}

	// An existing git_author is never replaced.
	run(t, dir, "config", "user.email", "work@example.com")
	recordGitAuthor(paths, loaded)
	if again, _ := config.Load(cfgPath); again.GitAuthor != "Simon Mullis <simon@mullis.co.uk>" {
		t.Fatalf("git_author replaced: %q", again.GitAuthor)
	}
}

func TestSyncGitOptsCarriesGitAuthor(t *testing.T) {
	o := syncGitOpts(&config.Config{Hostname: "lithium", GitAuthor: "Simon Mullis <simon@mullis.co.uk>"})
	if o.Author != "Simon Mullis <simon@mullis.co.uk>" || o.Host != "lithium" {
		t.Fatalf("%+v", o)
	}
}
