package dotfiles

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The 2026-10-26 case: ~/.grc was adopted whole, so ~/.grc was one symlink
// to the repo's common/home/.grc. apply then found ~/.grc/conf.dockerps (the
// repo file itself, through that link) to be "a real file", renamed it to
// conf.dockerps.dotgenie-bak.<date> inside the repo, and left a symlink to
// itself in its place.

type dirLinkCase struct {
	repo, home, repoFile, homeDir string
}

func dirLinked(t *testing.T) dirLinkCase {
	t.Helper()
	c := dirLinkCase{repo: t.TempDir(), home: t.TempDir()}
	c.repoFile = filepath.Join(c.repo, "dotfiles/common/home/.grc/conf.dockerps")
	write(t, c.repoFile, "colours\n")
	c.homeDir = filepath.Join(c.home, ".grc")
	if err := os.Symlink(filepath.Dir(c.repoFile), c.homeDir); err != nil {
		t.Fatal(err)
	}
	return c
}

func (c dirLinkCase) apply(t *testing.T, host string, dryRun bool) []FileAction {
	t.Helper()
	m := NewManager(c.repo, c.home, "server", host)
	acts, err := m.Apply([]Target{{Name: "home", RootPath: c.home}}, dryRun)
	if err != nil {
		t.Fatal(err)
	}
	return acts
}

// repoIntact checks the repo file is still a regular file with its content,
// and that nothing was backed up inside the repo.
func repoIntact(t *testing.T, c dirLinkCase, path, want string) {
	t.Helper()
	fi, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("repo file gone: %v", err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		l, _ := os.Readlink(path)
		t.Fatalf("repo file %s became a symlink to %s", path, l)
	}
	if got, _ := os.ReadFile(path); string(got) != want {
		t.Fatalf("repo file holds %q, want %q", got, want)
	}
	_ = filepath.WalkDir(filepath.Join(c.repo, "dotfiles"), func(p string, d os.DirEntry, err error) error {
		if err == nil && strings.Contains(d.Name(), "dotgenie-bak") {
			t.Errorf("backup made inside the repo: %s", p)
		}
		return nil
	})
}

func TestApplyDirectoryLinkIntoRepo(t *testing.T) {
	c := dirLinked(t)
	acts := c.apply(t, "xenon", false)

	repoIntact(t, c, c.repoFile, "colours\n")
	fi, err := os.Lstat(c.homeDir)
	if err != nil || fi.Mode()&os.ModeSymlink != 0 || !fi.IsDir() {
		t.Fatalf("~/.grc should now be a real directory: %v %v", fi.Mode(), err)
	}
	if l, err := os.Readlink(filepath.Join(c.homeDir, "conf.dockerps")); err != nil || l != c.repoFile {
		t.Fatalf("~/.grc/conf.dockerps -> %q (%v), want %s", l, err, c.repoFile)
	}
	var unlinked bool
	for _, a := range acts {
		if a.Action == "unlink_dir" && a.RelPath == ".grc" {
			unlinked = true
		}
		if a.Action == "error" {
			t.Errorf("error: %v", a.Error)
		}
	}
	if !unlinked {
		t.Errorf("no unlink_dir action reported: %+v", acts)
	}
	// A second run has nothing to do.
	for _, a := range c.apply(t, "xenon", false) {
		if a.Action != "skip" {
			t.Errorf("second run: %+v", a)
		}
	}
}

// Changing a file's scope: the same directory link, and the host layer now
// overrides the file. The old code renamed common's copy inside the repo and
// linked the host copy in its place.
func TestApplyDirectoryLinkWithHostOverride(t *testing.T) {
	c := dirLinked(t)
	hostFile := filepath.Join(c.repo, "dotfiles/hosts/xenon/home/.grc/conf.dockerps")
	write(t, hostFile, "host colours\n")
	c.apply(t, "xenon", false)

	repoIntact(t, c, c.repoFile, "colours\n")
	repoIntact(t, c, hostFile, "host colours\n")
	if l, _ := os.Readlink(filepath.Join(c.homeDir, "conf.dockerps")); l != hostFile {
		t.Fatalf("~/.grc/conf.dockerps -> %q, want the host copy", l)
	}
}

func TestApplyDirectoryLinkDryRunChangesNothing(t *testing.T) {
	c := dirLinked(t)
	acts := c.apply(t, "xenon", true)
	if fi, _ := os.Lstat(c.homeDir); fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("dry run replaced the directory link")
	}
	repoIntact(t, c, c.repoFile, "colours\n")
	var sawUnlink, sawLink bool
	for _, a := range acts {
		sawUnlink = sawUnlink || a.Action == "unlink_dir"
		sawLink = sawLink || (a.Action == "link" && a.RelPath == ".grc/conf.dockerps")
	}
	if !sawUnlink || !sawLink {
		t.Fatalf("dry run should report the unlink and the link: %+v", acts)
	}
}

// The last line of defence: whatever leads a target into the repo,
// processFile never touches it.
func TestProcessFileNeverTouchesTheRepo(t *testing.T) {
	c := dirLinked(t)
	m := NewManager(c.repo, c.home, "server", "xenon")
	for _, rel := range []string{".grc/conf.dockerps", ".grc/new-file"} {
		a := m.processFile(Target{Name: "home", RootPath: c.home}, rel, c.repoFile, false)
		if a.Action != "error" {
			t.Errorf("%s: got %q, want error", rel, a.Action)
		}
	}
	repoIntact(t, c, c.repoFile, "colours\n")
	if _, err := os.Lstat(filepath.Join(filepath.Dir(c.repoFile), "new-file")); err == nil {
		t.Error("created a file inside the repo")
	}
}

func TestStatusReportsDirectoryLink(t *testing.T) {
	c := dirLinked(t)
	m := NewManager(c.repo, c.home, "server", "xenon")
	acts, err := m.Status([]Target{{Name: "home", RootPath: c.home}})
	if err != nil {
		t.Fatal(err)
	}
	if len(acts) != 1 || acts[0].Action != "wrong_link" || acts[0].Error == nil || !strings.Contains(acts[0].Error.Error(), "directory link") {
		t.Fatalf("%+v", acts)
	}
}
