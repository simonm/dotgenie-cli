package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/simonm/dotgenie/internal/config"
	"gopkg.in/yaml.v3"
)

// packageFile represents the YAML structure of a package list file
type packageFile struct {
	Packages []interface{} `yaml:"packages"`
}

// miseFile represents the YAML structure of the mise package list
type miseFile struct {
	MisePackages []string `yaml:"mise_packages"`
}

// resolvePackageName takes a raw YAML item and OS string, returning the
// resolved package name. Items can be plain strings or maps with OS-specific
// overrides (e.g. fd -> fd-find on debian).
func resolvePackageName(item interface{}, detectedOS string) string {
	if item == nil {
		return ""
	}

	// Plain string package name
	if s, ok := item.(string); ok {
		return s
	}

	// Map form. Two formats supported:
	//
	// Format 1 (nested): "- fd:\n    debian: fd-find"
	//   Parses as map[string]interface{}{"fd": map[string]interface{}{"debian": "fd-find"}}
	//
	// Format 2 (flat): "- name: fd\n  debian: fd-find\n  arch: fd"
	//   Parses as map[string]interface{}{"name": "fd", "debian": "fd-find", "arch": "fd"}
	if m, ok := item.(map[string]interface{}); ok {
		// Check for flat format first (has a "name" key alongside OS keys)
		if nameVal, hasName := m["name"]; hasName {
			defaultName, _ := nameVal.(string)
			if override, ok := m[detectedOS]; ok {
				if s, ok := override.(string); ok {
					return s
				}
			}
			return defaultName
		}

		// Nested format: outer key is default name, value is OS override map
		for defaultName, v := range m {
			if nested, ok := v.(map[string]interface{}); ok {
				if override, ok := nested[detectedOS]; ok {
					if s, ok := override.(string); ok {
						return s
					}
				}
			}
			if nested, ok := v.(map[interface{}]interface{}); ok {
				if override, ok := nested[detectedOS]; ok {
					if s, ok := override.(string); ok {
						return s
					}
				}
			}
			return defaultName
		}
	}

	return ""
}

// loadPackageFiles loads and merges package list YAML files from the dotfiles
// packages/ directory. It loads common.yml (required), then optional
// OS-specific and system-type-specific files. It also loads mise.yml if
// present.
func loadPackageFiles(dotfilesDir string, cfg *config.Config) (systemPkgs []string, misePkgs []string, err error) {
	pkgDir := filepath.Join(dotfilesDir, "packages")

	// Helper to load a single package file and resolve names
	loadAndResolve := func(path string, required bool) ([]string, error) {
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			if os.IsNotExist(readErr) && !required {
				return nil, nil
			}
			return nil, fmt.Errorf("reading %s: %w", path, readErr)
		}

		var pf packageFile
		if parseErr := yaml.Unmarshal(data, &pf); parseErr != nil {
			return nil, fmt.Errorf("parsing %s: %w", path, parseErr)
		}

		var resolved []string
		for _, item := range pf.Packages {
			name := resolvePackageName(item, cfg.OS)
			if name != "" {
				resolved = append(resolved, name)
			}
		}
		return resolved, nil
	}

	// 1. common.yml (required)
	common, err := loadAndResolve(filepath.Join(pkgDir, "common.yml"), true)
	if err != nil {
		return nil, nil, err
	}
	systemPkgs = append(systemPkgs, common...)

	// 2. OS-specific (optional)
	osPkgs, err := loadAndResolve(filepath.Join(pkgDir, cfg.OS+".yml"), false)
	if err != nil {
		return nil, nil, err
	}
	systemPkgs = append(systemPkgs, osPkgs...)

	// 3. System-type-specific (optional)
	typePkgs, err := loadAndResolve(filepath.Join(pkgDir, cfg.SystemType+".yml"), false)
	if err != nil {
		return nil, nil, err
	}
	systemPkgs = append(systemPkgs, typePkgs...)

	// 4. mise.yml (optional)
	misePath := filepath.Join(pkgDir, "mise.yml")
	miseData, miseErr := os.ReadFile(misePath)
	if miseErr == nil {
		var mf miseFile
		if parseErr := yaml.Unmarshal(miseData, &mf); parseErr != nil {
			return nil, nil, fmt.Errorf("parsing %s: %w", misePath, parseErr)
		}
		misePkgs = mf.MisePackages
	} else if !os.IsNotExist(miseErr) {
		return nil, nil, fmt.Errorf("reading %s: %w", misePath, miseErr)
	}

	return systemPkgs, misePkgs, nil
}

// installSystemPackages runs the appropriate batch install command for the
// detected OS. If continueOnError is set and the batch fails, it retries
// packages one by one, collecting errors.
func installSystemPackages(pkgs []string, detectedOS string, dryRun, continueOnError, verbose bool) error {
	if len(pkgs) == 0 {
		return nil
	}

	var cmdName string
	var baseArgs []string

	switch detectedOS {
	case "arch":
		cmdName = "yay"
		baseArgs = []string{"-S", "--needed", "--noconfirm"}
	case "debian", "ubuntu":
		cmdName = "sudo"
		baseArgs = append([]string{"apt-get", "install", "-y"}, pkgs...)
	case "macos":
		cmdName = "brew"
		baseArgs = []string{"install"}
	default:
		return fmt.Errorf("unsupported OS for package installation: %s", detectedOS)
	}

	// For arch and macos, append packages to baseArgs
	if detectedOS == "arch" || detectedOS == "macos" {
		baseArgs = append(baseArgs, pkgs...)
	}

	if dryRun {
		fmt.Printf("  [dry-run] would run: %s %s\n", cmdName, strings.Join(baseArgs, " "))
		return nil
	}

	fmt.Printf("  Installing %d system packages...\n", len(pkgs))

	cmd := exec.Command(cmdName, baseArgs...)
	cmd.Stdin = os.Stdin
	if verbose {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}

	if err := cmd.Run(); err != nil {
		if !continueOnError {
			return fmt.Errorf("package install failed: %w", err)
		}

		// Retry one by one
		fmt.Println("  Batch install failed, retrying packages individually...")
		var failures []string

		for _, pkg := range pkgs {
			var singleArgs []string
			switch detectedOS {
			case "arch":
				singleArgs = []string{"-S", "--needed", "--noconfirm", pkg}
			case "debian", "ubuntu":
				cmdName = "sudo"
				singleArgs = []string{"apt-get", "install", "-y", pkg}
			case "macos":
				singleArgs = []string{"install", pkg}
			}

			singleCmd := exec.Command(cmdName, singleArgs...)
			singleCmd.Stdin = os.Stdin
			if verbose {
				singleCmd.Stdout = os.Stdout
				singleCmd.Stderr = os.Stderr
			}

			if singleErr := singleCmd.Run(); singleErr != nil {
				fmt.Printf("  Warning: failed to install %s: %v\n", pkg, singleErr)
				failures = append(failures, pkg)
			}
		}

		if len(failures) > 0 {
			return fmt.Errorf("failed to install %d packages: %s", len(failures), strings.Join(failures, ", "))
		}
	}

	return nil
}

// installYay installs the yay AUR helper on Arch Linux if it is not already
// present.
func installYay(dryRun bool) error {
	if checkCommand("yay") {
		return nil
	}

	if dryRun {
		fmt.Println("  [dry-run] would install yay AUR helper")
		return nil
	}

	fmt.Println("  Installing yay AUR helper...")

	// Install prerequisites
	prereqCmd := exec.Command("sudo", "pacman", "-S", "--needed", "--noconfirm", "git", "base-devel")
	prereqCmd.Stdin = os.Stdin
	prereqCmd.Stdout = os.Stdout
	prereqCmd.Stderr = os.Stderr
	if err := prereqCmd.Run(); err != nil {
		return fmt.Errorf("installing yay prerequisites: %w", err)
	}

	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "dotgenie-yay-*")
	if err != nil {
		return fmt.Errorf("creating temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Clone yay-bin
	cloneCmd := exec.Command("git", "clone", "https://aur.archlinux.org/yay-bin.git", filepath.Join(tmpDir, "yay-bin"))
	cloneCmd.Stdout = os.Stdout
	cloneCmd.Stderr = os.Stderr
	if err := cloneCmd.Run(); err != nil {
		return fmt.Errorf("cloning yay-bin: %w", err)
	}

	// Build and install
	buildCmd := exec.Command("makepkg", "-si", "--noconfirm")
	buildCmd.Dir = filepath.Join(tmpDir, "yay-bin")
	buildCmd.Stdin = os.Stdin
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr
	if err := buildCmd.Run(); err != nil {
		return fmt.Errorf("building yay: %w", err)
	}

	// Verify
	if !checkCommand("yay") {
		return fmt.Errorf("yay was installed but is not found in PATH")
	}

	fmt.Println("  Successfully installed yay")
	return nil
}

// installMise installs the mise version manager if it is not already present.
func installMise(dryRun bool) error {
	if checkCommand("mise") {
		return nil
	}

	// Also check the default install location
	home, _ := os.UserHomeDir()
	miseBin := filepath.Join(home, ".local", "bin", "mise")
	if _, err := os.Stat(miseBin); err == nil {
		return nil
	}

	if dryRun {
		fmt.Println("  [dry-run] would install mise via https://mise.run")
		return nil
	}

	fmt.Println("  Installing mise...")

	installCmd := exec.Command("sh", "-c", "curl -fsSL https://mise.run | sh")
	installCmd.Stdin = os.Stdin
	installCmd.Stdout = os.Stdout
	installCmd.Stderr = os.Stderr
	if err := installCmd.Run(); err != nil {
		return fmt.Errorf("installing mise: %w", err)
	}

	// Verify
	if _, err := os.Stat(miseBin); err != nil {
		return fmt.Errorf("mise was installed but not found at %s", miseBin)
	}

	fmt.Println("  Successfully installed mise")
	return nil
}

// installMisePackages installs global mise packages using "mise use -g".
func installMisePackages(pkgs []string, miseBin string, dryRun, verbose bool) error {
	if len(pkgs) == 0 {
		return nil
	}

	if dryRun {
		for _, pkg := range pkgs {
			fmt.Printf("  [dry-run] would run: %s use -g %s\n", miseBin, pkg)
		}
		return nil
	}

	var failed []string
	for i, pkg := range pkgs {
		fmt.Printf("  [%d/%d] %s... ", i+1, len(pkgs), pkg)

		cmd := exec.Command(miseBin, "use", "-g", pkg)
		cmd.Stdin = os.Stdin
		if verbose {
			fmt.Println()
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
		}

		if err := cmd.Run(); err != nil {
			fmt.Println("skipped")
			failed = append(failed, pkg)
			continue
		}

		if !verbose {
			fmt.Println("ok")
		}
	}

	if len(failed) > 0 {
		fmt.Printf("\n  Warning: %d mise package(s) failed to install:\n", len(failed))
		for _, pkg := range failed {
			fmt.Printf("    - %s\n", pkg)
		}
	}

	return nil
}

// findMiseBin returns the path to the mise binary, checking PATH first and
// then the default install location. Returns empty string if not found.
func findMiseBin() string {
	if path, err := exec.LookPath("mise"); err == nil {
		return path
	}

	home, _ := os.UserHomeDir()
	miseBin := filepath.Join(home, ".local", "bin", "mise")
	if _, err := os.Stat(miseBin); err == nil {
		return miseBin
	}

	return ""
}

// applyPackagesNew is the main orchestrator for package installation,
// replacing the old Ansible-based applyPackages.
func applyPackagesNew(paths config.Paths, cfg *config.Config, dryRun, continueOnError, verbose bool) error {
	fmt.Println("\n--- Packages ---")

	// Load package files
	systemPkgs, misePkgs, err := loadPackageFiles(paths.DotfilesDir, cfg)
	if err != nil {
		return fmt.Errorf("loading package files: %w", err)
	}

	// Install system packages
	if len(systemPkgs) > 0 {
		switch cfg.OS {
		case "arch":
			if err := installYay(dryRun); err != nil {
				return fmt.Errorf("installing yay: %w", err)
			}
			if err := installSystemPackages(systemPkgs, cfg.OS, dryRun, continueOnError, verbose); err != nil {
				return err
			}
		case "debian", "ubuntu", "macos":
			if err := installSystemPackages(systemPkgs, cfg.OS, dryRun, continueOnError, verbose); err != nil {
				return err
			}
		default:
			fmt.Printf("  Skipping system packages (unsupported OS: %s)\n", cfg.OS)
		}
	} else {
		fmt.Println("  No system packages to install")
	}

	// Install mise packages
	if len(misePkgs) > 0 {
		if err := installMise(dryRun); err != nil {
			return fmt.Errorf("installing mise: %w", err)
		}

		miseBin := findMiseBin()
		if miseBin == "" && !dryRun {
			return fmt.Errorf("mise binary not found after installation")
		}
		if miseBin == "" {
			miseBin = "mise" // placeholder for dry-run output
		}

		if err := installMisePackages(misePkgs, miseBin, dryRun, verbose); err != nil {
			return err
		}
	}

	// Summary
	fmt.Printf("  System packages: %d, Mise packages: %d\n", len(systemPkgs), len(misePkgs))

	return nil
}
