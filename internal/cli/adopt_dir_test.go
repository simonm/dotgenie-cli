package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/simonm/dotgenie/internal/config"
	"github.com/simonm/dotgenie/internal/dotfiles"
)

// Adopt a directory, promote one file in it to the host layer, then sync:
// every home file is its own symlink, and the repo keeps both copies intact.
func TestAdoptDirectoryThenPromoteAFile(t *testing.T) {
	home := t.TempDir()
	repo := t.TempDir()
	paths := config.Paths{Home: home, DotfilesDir: repo}
	cfg := &config.Config{SystemType: "workstation", Hostname: "xenon"}

	writeRepoFile(t, filepath.Join(home, ".grc/conf.dockerps"), "colours\n")
	writeRepoFile(t, filepath.Join(home, ".grc/sub/conf.ps"), "ps\n")

	if err := adoptPath(filepath.Join(home, ".grc"), "common", paths, cfg, false, true); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Lstat(filepath.Join(home, ".grc")); fi.Mode()&os.ModeSymlink != 0 {
		t.Fatal("adopt linked the whole directory")
	}
	common := filepath.Join(repo, "dotfiles/common/home/.grc")
	for rel, want := range map[string]string{"conf.dockerps": "colours\n", "sub/conf.ps": "ps\n"} {
		if l, err := os.Readlink(filepath.Join(home, ".grc", rel)); err != nil || l != filepath.Join(common, rel) {
			t.Fatalf("%s -> %q, %v", rel, l, err)
		}
		if got, _ := os.ReadFile(filepath.Join(common, rel)); string(got) != want {
			t.Fatalf("repo %s holds %q", rel, got)
		}
	}

	// Promote one file to the host layer.
	if err := adoptPath(filepath.Join(home, ".grc/conf.dockerps"), "hosts/xenon", paths, cfg, false, true); err != nil {
		t.Fatal(err)
	}
	host := filepath.Join(repo, "dotfiles/hosts/xenon/home/.grc/conf.dockerps")

	m := dotfiles.NewManager(repo, home, cfg.SystemType, cfg.Hostname)
	acts, err := m.Apply([]dotfiles.Target{{Name: "home", RootPath: home}}, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range acts {
		if a.Action == "error" || a.Action == "backup_and_link" {
			t.Errorf("%s: %s %v", a.RelPath, a.Action, a.Error)
		}
	}
	if l, _ := os.Readlink(filepath.Join(home, ".grc/conf.dockerps")); l != host {
		t.Fatalf("conf.dockerps -> %q, want the host copy", l)
	}
	for _, p := range []string{filepath.Join(common, "conf.dockerps"), host} {
		fi, err := os.Lstat(p)
		if err != nil || fi.Mode()&os.ModeSymlink != 0 {
			t.Fatalf("repo copy %s damaged: %v %v", p, fi, err)
		}
		if got, _ := os.ReadFile(p); string(got) != "colours\n" {
			t.Fatalf("%s holds %q", p, got)
		}
	}
}
