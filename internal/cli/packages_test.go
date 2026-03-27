package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/simonm/dotgenie/internal/config"
)

func TestResolvePackageName(t *testing.T) {
	tests := []struct {
		name       string
		item       interface{}
		detectedOS string
		want       string
	}{
		{
			name:       "plain string",
			item:       "git",
			detectedOS: "arch",
			want:       "git",
		},
		{
			name: "map with matching OS override",
			item: map[string]interface{}{
				"fd": map[string]interface{}{
					"debian": "fd-find",
					"ubuntu": "fd-find",
				},
			},
			detectedOS: "debian",
			want:       "fd-find",
		},
		{
			name: "map falls back to outer key when OS not in overrides",
			item: map[string]interface{}{
				"fd": map[string]interface{}{
					"debian": "fd-find",
					"ubuntu": "fd-find",
				},
			},
			detectedOS: "arch",
			want:       "fd",
		},
		{
			name: "map falls back to outer key for unlisted OS",
			item: map[string]interface{}{
				"fd": map[string]interface{}{
					"debian": "fd-find",
					"ubuntu": "fd-find",
				},
			},
			detectedOS: "macos",
			want:       "fd",
		},
		{
			name:       "nil item returns empty string",
			item:       nil,
			detectedOS: "arch",
			want:       "",
		},
		{
			name: "map with interface keys from yaml.v3",
			item: map[string]interface{}{
				"fd": map[interface{}]interface{}{
					"debian": "fd-find",
				},
			},
			detectedOS: "debian",
			want:       "fd-find",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolvePackageName(tt.item, tt.detectedOS)
			if got != tt.want {
				t.Errorf("resolvePackageName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLoadPackageFiles(t *testing.T) {
	t.Run("merges common, OS, and mise files", func(t *testing.T) {
		tmpDir := t.TempDir()
		pkgDir := filepath.Join(tmpDir, "packages")
		if err := os.MkdirAll(pkgDir, 0o755); err != nil {
			t.Fatal(err)
		}

		writeFile(t, filepath.Join(pkgDir, "common.yml"), "packages:\n  - git\n  - curl\n")
		writeFile(t, filepath.Join(pkgDir, "arch.yml"), "packages:\n  - base-devel\n")
		writeFile(t, filepath.Join(pkgDir, "mise.yml"), "mise_packages:\n  - node@lts\n  - python@latest\n")

		cfg := &config.Config{
			OS:         "arch",
			SystemType: "server",
			Hostname:   "testhost",
		}

		systemPkgs, misePkgs, err := loadPackageFiles(tmpDir, cfg)
		if err != nil {
			t.Fatalf("loadPackageFiles() error: %v", err)
		}

		if len(systemPkgs) != 3 {
			t.Errorf("expected 3 system packages, got %d: %v", len(systemPkgs), systemPkgs)
		}
		assertContains(t, systemPkgs, "git")
		assertContains(t, systemPkgs, "curl")
		assertContains(t, systemPkgs, "base-devel")

		if len(misePkgs) != 2 {
			t.Errorf("expected 2 mise packages, got %d: %v", len(misePkgs), misePkgs)
		}
		assertContains(t, misePkgs, "node@lts")
		assertContains(t, misePkgs, "python@latest")
	})

	t.Run("missing optional files do not cause errors", func(t *testing.T) {
		tmpDir := t.TempDir()
		pkgDir := filepath.Join(tmpDir, "packages")
		if err := os.MkdirAll(pkgDir, 0o755); err != nil {
			t.Fatal(err)
		}

		writeFile(t, filepath.Join(pkgDir, "common.yml"), "packages:\n  - git\n")

		cfg := &config.Config{
			OS:         "arch",
			SystemType: "server",
			Hostname:   "testhost",
		}

		systemPkgs, misePkgs, err := loadPackageFiles(tmpDir, cfg)
		if err != nil {
			t.Fatalf("loadPackageFiles() error: %v", err)
		}

		if len(systemPkgs) != 1 {
			t.Errorf("expected 1 system package, got %d: %v", len(systemPkgs), systemPkgs)
		}
		if len(misePkgs) != 0 {
			t.Errorf("expected 0 mise packages, got %d: %v", len(misePkgs), misePkgs)
		}
	})

	t.Run("OS-specific name resolution during load", func(t *testing.T) {
		tmpDir := t.TempDir()
		pkgDir := filepath.Join(tmpDir, "packages")
		if err := os.MkdirAll(pkgDir, 0o755); err != nil {
			t.Fatal(err)
		}

		writeFile(t, filepath.Join(pkgDir, "common.yml"), "packages:\n  - fd:\n      debian: fd-find\n")

		cfg := &config.Config{
			OS:         "debian",
			SystemType: "workstation",
			Hostname:   "testhost",
		}

		systemPkgs, _, err := loadPackageFiles(tmpDir, cfg)
		if err != nil {
			t.Fatalf("loadPackageFiles() error: %v", err)
		}

		assertContains(t, systemPkgs, "fd-find")
	})
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertContains(t *testing.T, slice []string, want string) {
	t.Helper()
	for _, s := range slice {
		if s == want {
			return
		}
	}
	t.Errorf("slice %v does not contain %q", slice, want)
}
