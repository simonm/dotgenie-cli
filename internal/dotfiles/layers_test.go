package dotfiles

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// repo is Boron's shape: common files, a server package file but no server
// folder, and a host layer.
func repo(t *testing.T) string {
	d := t.TempDir()
	write(t, filepath.Join(d, "dotfiles/common/home/.bashrc"), "common")
	write(t, filepath.Join(d, "dotfiles/common/home/.config/x"), "common")
	write(t, filepath.Join(d, "dotfiles/common/home/.config/.gitkeep"), "")
	write(t, filepath.Join(d, "dotfiles/hosts/boron/home/.config/x"), "host")
	write(t, filepath.Join(d, "packages/common.yml"), "packages: []\n")
	write(t, filepath.Join(d, "packages/server.yml"), "packages: []\n")
	return d
}

func TestLayers(t *testing.T) {
	d := repo(t)
	got := NewManager(d, t.TempDir(), "server", "boron").Layers()
	want := []Layer{
		{Name: "common", Files: 2, Packages: "common.yml"},
		{Name: "server", Files: 0, Packages: "server.yml"},
		{Name: "hosts/boron", Files: 1},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d layers, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		g := got[i]
		if g.Name != w.Name || g.Files != w.Files || g.Packages != w.Packages {
			t.Errorf("layer %d: got %+v, want %+v", i, g, w)
		}
		if g.Dir != filepath.Join(d, "dotfiles", w.Name) {
			t.Errorf("layer %d dir: %s", i, g.Dir)
		}
	}
	if l := NewManager(d, t.TempDir(), "", "boron").Layers(); len(l) != 2 || l[1].Name != "hosts/boron" {
		t.Errorf("no system type: %+v", l)
	}
}

func TestCollectFilesOrder(t *testing.T) {
	d := repo(t)
	write(t, filepath.Join(d, "dotfiles/server/home/.config/x"), "server")
	write(t, filepath.Join(d, "dotfiles/server/home/.config/y"), "server")
	files, err := NewManager(d, t.TempDir(), "server", "boron").CollectFiles([]Target{{Name: "home"}})
	if err != nil {
		t.Fatal(err)
	}
	for key, layer := range map[string]string{"home:.bashrc": "common", "home:.config/x": "hosts/boron", "home:.config/y": "server"} {
		if !strings.Contains(files[key], "/dotfiles/"+layer+"/") {
			t.Errorf("%s comes from %s, want layer %s", key, files[key], layer)
		}
	}
}

func TestGetLayerForPath(t *testing.T) {
	for scope, want := range map[string]string{"common": "common", "workstation": "workstation", "server": "server", "SERVER": "server", "host": "hosts/boron"} {
		got, err := GetLayerForPath(scope, "boron")
		if err != nil || got != want {
			t.Errorf("%s: got %q, %v; want %q", scope, got, err, want)
		}
	}
	// These used to fall back to common, so adopt put a file on every
	// machine and forget removed it from all of them.
	for _, scope := range []string{"minimal", "container", "hosts", ""} {
		if got, err := GetLayerForPath(scope, "boron"); err == nil {
			t.Errorf("%q: got layer %q, want an error", scope, got)
		}
	}
}
