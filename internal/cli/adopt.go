package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/simonm/dotgenie/internal/config"
	"github.com/simonm/dotgenie/internal/dotfiles"
	"github.com/spf13/cobra"
)

var (
	adoptScope    string
	adoptCopyOnly bool
	adoptYes      bool
)

var adoptCmd = &cobra.Command{
	Use:   "adopt <path>...",
	Short: "Adopt existing dotfiles into management",
	Long: `Adopt existing configuration files into dotgenie management.

The files are moved into your dotfiles repository and replaced with symlinks.
The target (home/, etc/, var/) is auto-detected based on the file path.

You can specify which layer to adopt into:
  - common:      Shared across all systems
  - workstation: Desktop/laptop systems only
  - host:        This specific host only

Examples:
  dotgenie adopt ~/.config/nvim                           # → common/home/
  dotgenie adopt --scope workstation ~/.config/hypr       # → workstation/home/
  dotgenie adopt --scope host /etc/modprobe.d/iwlwifi.conf  # → hosts/<hostname>/etc/
  dotgenie adopt --copy-only ~/.bashrc                    # Copy without symlinking`,
	Args: cobra.MinimumNArgs(1),
	RunE: runAdopt,
}

func init() {
	adoptCmd.Flags().StringVarP(&adoptScope, "scope", "s", "common", "Layer to adopt into (common, workstation, host)")
	adoptCmd.Flags().BoolVar(&adoptCopyOnly, "copy-only", false, "Copy files without creating symlinks")
	adoptCmd.Flags().BoolVarP(&adoptYes, "yes", "y", false, "Skip confirmation prompts")
}

func runAdopt(cmd *cobra.Command, args []string) error {
	paths := config.DefaultPaths()
	if dotfilesDir != "" {
		paths.DotfilesDir = dotfilesDir
	}

	// Load config
	cfg, err := config.Load(filepath.Join(paths.DotfilesDir, "config.yml"))
	if err != nil {
		return fmt.Errorf("loading config: %w\nRun 'dotgenie init' first", err)
	}

	// Determine target layer directory
	layer := dotfiles.GetLayerForPath(adoptScope, cfg.Hostname)

	fmt.Printf("Adopting into layer: %s\n", layer)

	// Expand globs and collect all files to adopt
	var filesToAdopt []string
	for _, pattern := range args {
		// Expand ~ to home directory
		if strings.HasPrefix(pattern, "~/") {
			pattern = filepath.Join(paths.Home, pattern[2:])
		}

		// Expand globs
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return fmt.Errorf("invalid glob pattern %s: %w", pattern, err)
		}

		if len(matches) == 0 {
			// Not a glob, treat as literal path
			filesToAdopt = append(filesToAdopt, pattern)
		} else {
			filesToAdopt = append(filesToAdopt, matches...)
		}
	}

	// Process each file/directory
	for _, sourcePath := range filesToAdopt {
		if err := adoptPath(sourcePath, layer, paths, cfg, adoptCopyOnly, adoptYes); err != nil {
			fmt.Printf("Error adopting %s: %v\n", sourcePath, err)
			// Hint if path looks like a scope name
			basename := filepath.Base(sourcePath)
			if basename == "common" || basename == "workstation" || basename == "host" {
				fmt.Printf("  Hint: Did you mean --scope %s?\n", basename)
			}
		}
	}

	// Auto-commit if enabled
	if cfg.AutoCommitAfterAdopt && !adoptCopyOnly {
		if err := gitCommitAdopted(paths.DotfilesDir); err != nil {
			fmt.Printf("Warning: auto-commit failed: %v\n", err)
		}
	}

	return nil
}

func adoptPath(sourcePath, layer string, paths config.Paths, cfg *config.Config, copyOnly, autoYes bool) error {
	// Get absolute path
	absSource, err := filepath.Abs(sourcePath)
	if err != nil {
		return err
	}

	// Check if source is a symlink (from stow, chezmoi, etc.)
	var existingLinkTarget string
	if linkTarget, err := os.Readlink(absSource); err == nil {
		// It's a symlink - resolve to absolute path
		if !filepath.IsAbs(linkTarget) {
			linkTarget = filepath.Join(filepath.Dir(absSource), linkTarget)
		}
		existingLinkTarget = linkTarget
	}

	// Resolve symlinks to get the real file
	realSource, err := filepath.EvalSymlinks(absSource)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("file does not exist: %s", absSource)
		}
		realSource = absSource // Use original if eval fails
	}

	// Auto-detect target (home, etc, var, usr) based on path
	targetName, relPath := dotfiles.GetTargetForPath(absSource, paths.Home)

	// Target path in dotfiles repo: dotfiles/<layer>/<target>/<relPath>
	destPath := filepath.Join(paths.DotfilesDir, "dotfiles", layer, targetName, relPath)

	// Check if already adopted
	if _, err := os.Stat(destPath); err == nil {
		// Check if source is already a symlink to destPath
		if linkTarget, err := os.Readlink(absSource); err == nil && linkTarget == destPath {
			return fmt.Errorf("already managed (symlinked to dotfiles)")
		}
		return fmt.Errorf("already exists in dotfiles: %s\n  Use 'dotgenie forget' first if you want to re-adopt", destPath)
	}

	// Show what will happen
	info, err := os.Stat(realSource)
	if err != nil {
		return err
	}

	if info.IsDir() {
		fmt.Printf("\nAdopting directory: %s\n", absSource)
	} else {
		fmt.Printf("\nAdopting file: %s\n", absSource)
	}

	// Show migration info if adopting from another tool
	if existingLinkTarget != "" {
		fmt.Printf("  Migrating from: %s (existing symlink)\n", existingLinkTarget)
		fmt.Printf("  Content from:   %s\n", realSource)
	}

	fmt.Printf("  Target: [%s] %s\n", targetName, relPath)
	fmt.Printf("  Destination: %s\n", destPath)

	if copyOnly {
		fmt.Println("  (copy only, no symlink)")
	}

	if dotfiles.TargetNeedsSudo(targetName) && !copyOnly {
		fmt.Println("  Note: System file - symlink creation will require sudo")
	}

	// Confirm unless -y flag
	if !autoYes {
		fmt.Print("Proceed? [y/N] ")
		reader := bufio.NewReader(os.Stdin)
		response, _ := reader.ReadString('\n')
		response = strings.TrimSpace(strings.ToLower(response))
		if response != "y" && response != "yes" {
			fmt.Println("Skipped")
			return nil
		}
	}

	// Create parent directory
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return fmt.Errorf("creating directory: %w", err)
	}

	// Copy or move the file/directory
	if info.IsDir() {
		if err := copyDir(realSource, destPath); err != nil {
			return fmt.Errorf("copying directory: %w", err)
		}
	} else {
		if err := copyFile(realSource, destPath); err != nil {
			return fmt.Errorf("copying file: %w", err)
		}
	}

	if copyOnly {
		fmt.Printf("Copied to %s\n", destPath)
		return nil
	}

	// Remove original and create symlink
	if dotfiles.TargetNeedsSudo(targetName) {
		// Need sudo to modify system files
		if err := removeAndLinkWithSudo(absSource, destPath); err != nil {
			return err
		}
	} else {
		if err := os.RemoveAll(absSource); err != nil {
			return fmt.Errorf("removing original: %w", err)
		}
		if err := os.Symlink(destPath, absSource); err != nil {
			return fmt.Errorf("creating symlink: %w", err)
		}
	}

	fmt.Printf("Adopted and linked: %s → %s\n", absSource, destPath)
	return nil
}

func removeAndLinkWithSudo(originalPath, destPath string) error {
	// Remove original with sudo
	rmCmd := execCommand("sudo", "rm", "-rf", originalPath)
	rmCmd.Stdout = os.Stdout
	rmCmd.Stderr = os.Stderr
	if err := rmCmd.Run(); err != nil {
		return fmt.Errorf("sudo rm failed: %w", err)
	}

	// Create symlink with sudo
	lnCmd := execCommand("sudo", "ln", "-s", destPath, originalPath)
	lnCmd.Stdout = os.Stdout
	lnCmd.Stderr = os.Stderr
	if err := lnCmd.Run(); err != nil {
		return fmt.Errorf("sudo ln failed: %w", err)
	}

	return nil
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}

	info, err := os.Stat(src)
	if err != nil {
		return err
	}

	return os.WriteFile(dst, data, info.Mode())
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		targetPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(targetPath, info.Mode())
		}

		return copyFile(path, targetPath)
	})
}

func gitCommitAdopted(dotfilesDir string) error {
	// Check if it's a git repo
	gitDir := filepath.Join(dotfilesDir, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		return nil
	}

	// Check for changes
	cmd := execCommand("git", "status", "--porcelain")
	cmd.Dir = dotfilesDir
	output, err := cmd.Output()
	if err != nil || len(output) == 0 {
		return nil
	}

	// Add and commit
	addCmd := execCommand("git", "add", "-A")
	addCmd.Dir = dotfilesDir
	if err := addCmd.Run(); err != nil {
		return err
	}

	commitCmd := execCommand("git", "commit", "-m", "Adopt dotfiles via dotgenie")
	commitCmd.Dir = dotfilesDir
	return commitCmd.Run()
}
